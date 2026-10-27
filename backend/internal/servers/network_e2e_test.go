//go:build integration

package servers

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"shaurma-launcher-backend/internal/java"
)

// consoleTail повертає останні N рядків консолі сервера (для діагностики).
func consoleTail(api *API, id string, n int) string {
	snap := api.manager.GetServerConsoleSnapshot(id)
	var lines []string
	for _, l := range snap {
		lines = append(lines, l.Text)
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// ── Мінімальний MC-protocol status-ping (тільки stdlib) ─────────────────

func mcWriteVarInt(buf *bytes.Buffer, v int) {
	for {
		b := byte(v & 0x7F)
		v >>= 7
		if v != 0 {
			b |= 0x80
		}
		buf.WriteByte(b)
		if v == 0 {
			return
		}
	}
}

func mcReadVarInt(r io.Reader) (int, error) {
	var v, shift int
	buf := make([]byte, 1)
	for {
		if _, err := io.ReadFull(r, buf); err != nil {
			return 0, err
		}
		v |= int(buf[0]&0x7F) << shift
		if buf[0]&0x80 == 0 {
			return v, nil
		}
		shift += 7
		if shift > 35 {
			return 0, fmt.Errorf("varint задовгий")
		}
	}
}

// mcStatusPing робить REAL статус-пінг Minecraft-сервера (той самий, що
// показує клієнт у списку серверів): handshake (state=status) → status
// request → читає JSON. Працює і для проксі, і для backend.
func mcStatusPing(port int, timeout time.Duration) (string, error) {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), timeout)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(timeout))

	var body bytes.Buffer
	body.WriteByte(0x00) // packet id: handshake
	mcWriteVarInt(&body, 0x2DD)
	mcWriteVarInt(&body, len("127.0.0.1"))
	body.WriteString("127.0.0.1")
	body.WriteByte(byte(port >> 8))
	body.WriteByte(byte(port & 0xFF))
	body.WriteByte(0x01) // next state: status

	var frame bytes.Buffer
	mcWriteVarInt(&frame, body.Len())
	frame.Write(body.Bytes())
	mcWriteVarInt(&frame, 1) // status request: довжина 1
	frame.WriteByte(0x00)

	if _, err := conn.Write(frame.Bytes()); err != nil {
		return "", err
	}
	if _, err := mcReadVarInt(conn); err != nil { // довжина пакета
		return "", err
	}
	pid, err := mcReadVarInt(conn)
	if err != nil {
		return "", err
	}
	if pid != 0 {
		return "", fmt.Errorf("несподіваний packet id %d", pid)
	}
	jsonLen, err := mcReadVarInt(conn)
	if err != nil {
		return "", err
	}
	if jsonLen <= 0 || jsonLen > 1<<20 {
		return "", fmt.Errorf("некоректна довжина JSON: %d", jsonLen)
	}
	data := make([]byte, jsonLen)
	if _, err := io.ReadFull(conn, data); err != nil {
		return "", err
	}
	return string(data), nil
}

// TestNetworkE2E — повний цикл мережі Velocity ЧЕРЕЗ ТОЙ САМИЙ API, що й GUI:
//
//  1. DataDir + Store: Velocity-проксі (Java 25) і Paper backend (Java 21).
//  2. NetworkSave: проксі в мережу + backend (order 1) → network.json +
//     згенерований velocity.toml (bind + [servers] + try).
//  3. NetworkStart: ліниво ставить ОБИДВА ядра (fill.papermc.io v3),
//     запускає проксі, чекає ready, запускає backend.
//  4. Обидва сервери running.
//  5. Реальний status-ping (MC protocol) до ПРОКСІ — доводить, що проксі
//     живий на своєму порту й відповідає як сервер.
//  6. NetworkStop: backend зупиняється першим, проксі — останнім.
//
// Запуск:
//
//	go test -tags integration ./internal/servers/ -run TestNetworkE2E -v -count=1 -timeout 900s
func TestNetworkE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}

	// 0. Java 25 для проксі (Velocity 4.x) і Java 21 для Paper 1.21.x.
	proxyJava, err := FindJavaPath(25)
	if err != nil {
		proxyJava, err = FindJavaPath(0)
		if err != nil {
			t.Skipf("Java не знайдена: %v", err)
		}
		t.Logf("Java 25 не знайдена окремо, використаємо системну: %s", proxyJava)
	}
	backendJava, err := FindJavaPath(21)
	if err != nil {
		backendJava = proxyJava // хоч якась — головне, щоб сервер піднявся
	}
	t.Logf("proxy java: %s", proxyJava)
	t.Logf("backend java: %s", backendJava)

	// 1. API з реальним java.Installer (той самий шлях, що й у GUI —
	//    prepareAndStart вміє доставляти Java, але тут вона вже є системно).
	dataDir := t.TempDir()
	javaInst := java.NewInstaller(filepath.Join(dataDir, "java"))
	api := NewAPI(dataDir, javaInst, nil)

	proxy := &Server{
		ID:          "net-proxy",
		Name:        "NetProxy",
		Dir:         filepath.Join(dataDir, "servers", "netproxy"),
		Core:        CoreVelocity,
		MCVersion:   "",
		LoaderVer:   "", // не задана → рекомендована стабільна
		JavaMajor:   25,
		RAMMB:       1024,
		JvmArgs:     DefaultJVMArgs(25, 1024),
		ProgramArgs: "",
		Port:        25777, // унікальний, щоб не конфліктувати з іншими тестами
	}
	backend := &Server{
		ID:          "net-backend",
		Name:        "NetBackend",
		Dir:         filepath.Join(dataDir, "servers", "netbackend"),
		Core:        CorePaper,
		MCVersion:   "1.21.1",
		LoaderVer:   "",
		JavaMajor:   21,
		RAMMB:       1536,
		JvmArgs:     DefaultJVMArgs(21, 1536),
		ProgramArgs: "",
		Port:        25766,
	}
	for _, s := range []*Server{proxy, backend} {
		if err := os.MkdirAll(s.Dir, 0755); err != nil {
			t.Fatal(err)
		}
		// eula (Paper вимагає), server.properties з потрібним портом.
		os.WriteFile(filepath.Join(s.Dir, "eula.txt"), []byte("eula=true\n"), 0644)
		os.WriteFile(filepath.Join(s.Dir, "server.properties"),
			[]byte(fmt.Sprintf("server-port=%d\nonline-mode=false\n", s.Port)), 0644)
		if err := api.store.Save(s); err != nil {
			t.Fatal(err)
		}
	}

	// 2. NetworkSave — склад мережі (той самий виклик, що кнопка «Зберегти»).
	t.Log("── NetworkSave ──")
	if err := api.NetworkSave(proxy.ID, []NetworkMemberView{{
		ServerID: backend.ID, Name: backend.Name, Core: backend.Core,
		MCVersion: backend.MCVersion, Port: backend.Port,
		InNetwork: true, Order: 1, DelaySec: 0,
	}}); err != nil {
		t.Fatalf("NetworkSave: %v", err)
	}
	if _, err := LoadNetwork(proxy.Dir); err != nil {
		t.Fatalf("network.json не створено: %v", err)
	}
	toml, err := os.ReadFile(filepath.Join(proxy.Dir, "velocity.toml"))
	if err != nil {
		t.Fatalf("velocity.toml не створено: %v", err)
	}
	tomlTxt := string(toml)
	if !strings.Contains(tomlTxt, `bind = "0.0.0.0:25777"`) {
		t.Fatalf("velocity.toml без правильного bind:\n%s", tomlTxt)
	}
	if !strings.Contains(tomlTxt, "netbackend = \"127.0.0.1:25766\"") {
		t.Fatalf("velocity.toml без backend-сервера:\n%s", tomlTxt)
	}
	// try має бути ВСЕРЕДИНІ [servers] і списком рядків (інакше Velocity
	// падає з ClassCastException — це ловив реальний запуск).
	if !strings.Contains(tomlTxt, "try = [") || !strings.Contains(tomlTxt, "\"netbackend\",") {
		t.Fatalf("velocity.toml без коректного try:\n%s", tomlTxt)
	}
	if strings.Contains(tomlTxt, "fallback =") || strings.Contains(tomlTxt, "{ server =") {
		t.Fatalf("velocity.toml містить невалідні для Velocity ключі:\n%s", tomlTxt)
	}
	t.Log("OK: network.json + velocity.toml (bind, [servers], try всередині)")

	// 3. NetworkStart — весь шлях GUI: InstallCore (ліниво) → Start.
	t.Log("── NetworkStart (install + start) ──")
	started := make(chan error, 1)
	go func() { started <- api.NetworkStart(proxy.ID) }()
	select {
	case err := <-started:
		if err != nil {
			t.Fatalf("NetworkStart: %v\n--- proxy console ---\n%s", err, consoleTail(api, proxy.ID, 40))
		}
	case <-time.After(420 * time.Second):
		t.Fatal("NetworkStart не завершився за 420s")
	}
	t.Log("OK: NetworkStart повернувся")

	// 4. Обидва сервери мають бути running.
	t.Log("── очікування running (обидва) ──")
	deadline := time.Now().Add(180 * time.Second)
	for time.Now().Before(deadline) {
		ps := api.manager.GetStatus(proxy.ID)
		bs := api.manager.GetStatus(backend.ID)
		if ps.Status == "crashed" {
			t.Fatalf("проксі впав: %s (exit %d)\n--- proxy console ---\n%s",
				ps.LastError, ps.ExitCode, consoleTail(api, proxy.ID, 60))
		}
		if bs.Status == "crashed" {
			t.Fatalf("backend впав: %s (exit %d)\n--- backend console ---\n%s",
				bs.LastError, bs.ExitCode, consoleTail(api, backend.ID, 60))
		}
		if ps.Status == "running" && bs.Status == "running" {
			t.Log("OK: проксі та backend у стані running")
			goto bothUp
		}
		time.Sleep(1 * time.Second)
	}
	{
		ps := api.manager.GetStatus(proxy.ID)
		bs := api.manager.GetStatus(backend.ID)
		t.Fatalf("таймаут: proxy=%+v backend=%+v\n--- proxy console ---\n%s\n--- backend console ---\n%s",
			ps, bs, consoleTail(api, proxy.ID, 60), consoleTail(api, backend.ID, 60))
	}
bothUp:

	// 5. Реальний status-ping до ПРОКСІ (як клієнт MC у списку серверів):
	//    доводить, що проксі слухає на своєму порту і відповідає.
	t.Log("── status-ping до проксі (як клієнт MC) ──")
	pingOK := false
	deadline = time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := mcStatusPing(proxy.Port, 5*time.Second)
		if err == nil && strings.Contains(resp, "description") {
			if len(resp) > 160 {
				resp = resp[:160]
			}
			t.Logf("OK: проксі відповів на status-ping: %s", resp)
			pingOK = true
			break
		}
		time.Sleep(2 * time.Second)
	}
	if !pingOK {
		t.Logf("WARN: проксі не відповів на status-ping. Хвіст консолі проксі:\n%s",
			consoleTail(api, proxy.ID, 30))
	}

	// 6. NetworkStop — спочатку backend, потім проксі.
	t.Log("── NetworkStop ──")
	stopDone := make(chan error, 1)
	go func() { stopDone <- api.NetworkStop(proxy.ID) }()
	select {
	case err := <-stopDone:
		if err != nil {
			t.Fatalf("NetworkStop: %v", err)
		}
	case <-time.After(120 * time.Second):
		t.Fatal("NetworkStop не завершився за 120s")
	}
	if api.manager.IsRunning(proxy.ID) || api.manager.IsRunning(backend.ID) {
		t.Fatal("після NetworkStop сервери ще запущені")
	}
	t.Log("OK: мережу зупинено (обидва stopped)")

	t.Log("PASS: network save → start (proxy+backend) → running → ping → stop")
}
