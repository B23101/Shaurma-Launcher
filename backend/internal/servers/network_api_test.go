// network_api_test.go — unit-тести API-рівня мережі Velocity (без запуску
// реальних процесів): NetworkSave пише network.json + velocity.toml,
// NetworkGetView повертає ролі, NetworkStop без збереженого конфіга —
// тихий no-op, а не помилка «мережу не збережено».
package servers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestNetworkEnv(t *testing.T) (*API, *Server, *Server) {
	t.Helper()
	dir := t.TempDir()
	api := NewAPI(dir, nil, nil)

	proxy := &Server{
		ID:        "proxy1",
		Name:      "Проксі",
		Dir:       filepath.Join(dir, "proxy"),
		Core:      CoreVelocity,
		MCVersion: "",
		Port:      25577,
	}
	backend := &Server{
		ID:        "back1",
		Name:      "Виживання",
		Dir:       filepath.Join(dir, "back"),
		Core:      CorePaper,
		MCVersion: "1.21.1",
		Port:      25566,
	}
	for _, s := range []*Server{proxy, backend} {
		if err := os.MkdirAll(filepath.Join(s.Dir, ".shaurma"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := api.store.Save(s); err != nil {
			t.Fatal(err)
		}
	}
	return api, proxy, backend
}

func TestNetworkSaveGeneratesConfig(t *testing.T) {
	api, proxy, backend := newTestNetworkEnv(t)

	// Зберігаємо мережу: backend включений, order 1, пауза 5с.
	members := []NetworkMemberView{
		{
			ServerID:  backend.ID,
			Name:      backend.Name,
			Core:      backend.Core,
			MCVersion: backend.MCVersion,
			Port:      backend.Port,
			InNetwork: true,
			Order:     1,
			DelaySec:  5,
		},
	}
	if err := api.NetworkSave(proxy.ID, members); err != nil {
		t.Fatalf("NetworkSave: %v", err)
	}

	// network.json існує і містить проксі + backend.
	net, err := LoadNetwork(proxy.Dir)
	if err != nil {
		t.Fatalf("LoadNetwork: %v", err)
	}
	if len(net.Servers) != 2 {
		t.Fatalf("очікували 2 сервери в мережі, маємо %d", len(net.Servers))
	}
	if !net.Servers[0].IsProxy || net.Servers[0].ServerID != proxy.ID {
		t.Errorf("перший у мережі має бути проксі")
	}
	if net.Servers[1].ServerID != backend.ID || net.Servers[1].Order != 1 || net.Servers[1].DelaySec != 5 {
		t.Errorf("backend записаний невірно: %+v", net.Servers[1])
	}

	// velocity.toml згенеровано: bind з портом проксі + backend-сервер.
	toml, err := os.ReadFile(filepath.Join(proxy.Dir, "velocity.toml"))
	if err != nil {
		t.Fatalf("velocity.toml не створено: %v", err)
	}
	txt := string(toml)
	if want := `bind = "0.0.0.0:25577"`; !strings.Contains(txt, want) {
		t.Errorf("velocity.toml не містить %q:\n%s", want, txt)
	}
	// Ім'я в мережі — транслітерація назви, ключ у лапках.
	if !strings.Contains(txt, `[servers]`) || !strings.Contains(txt, `"vyzhyvannia" = "127.0.0.1:25566"`) {
		t.Errorf("velocity.toml не містить backend-сервер:\n%s", txt)
	}
	if strings.Contains(txt, "forced-defauls") {
		t.Errorf("velocity.toml містить зламану секцію forced-defauls:\n%s", txt)
	}

	// «Склеювання» (glue): секрет + modern forwarding у toml + конфіг
	// Paper backend-а.
	if !strings.Contains(txt, `player-info-forwarding-mode = "modern"`) || !strings.Contains(txt, `config-version = "2.7"`) {
		t.Errorf("velocity.toml не містить modern forwarding:\n%s", txt)
	}
	secretBytes, err := os.ReadFile(filepath.Join(proxy.Dir, "forwarding.secret"))
	if err != nil {
		t.Fatalf("forwarding.secret не створено: %v", err)
	}
	secret := strings.TrimSpace(string(secretBytes))
	if len(secret) < 16 {
		t.Errorf("forwarding.secret закороткий: %q", secret)
	}

	// Backend: config/paper-global.yml з нашим секретом (1.21.1 → новий шлях).
	glob, err := os.ReadFile(filepath.Join(backend.Dir, "config", "paper-global.yml"))
	if err != nil {
		t.Fatalf("paper-global.yml не створено: %v", err)
	}
	gtxt := string(glob)
	if !strings.Contains(gtxt, "proxies:") || !strings.Contains(gtxt, "velocity:") ||
		!strings.Contains(gtxt, "enabled: true") || !strings.Contains(gtxt, secret) {
		t.Errorf("paper-global.yml без forwarding-конфіга:\n%s", gtxt)
	}
	// Backend: server.properties online-mode=false (автентифікує проксі).
	props, err := os.ReadFile(filepath.Join(backend.Dir, "server.properties"))
	if err != nil {
		t.Fatalf("server.properties не створено: %v", err)
	}
	if !strings.Contains(string(props), "online-mode=false") {
		t.Errorf("server.properties без online-mode=false:\n%s", string(props))
	}
}

func TestNetworkGlueOldPaperUsesPaperYml(t *testing.T) {
	api, proxy, backend := newTestNetworkEnv(t)
	// Старий Paper (≤1.18.2) — конфіг у paper.yml: settings.velocity-support.
	backend.MCVersion = "1.18.2"
	if err := api.store.Save(backend); err != nil {
		t.Fatal(err)
	}
	if err := api.NetworkSave(proxy.ID, []NetworkMemberView{{
		ServerID: backend.ID, InNetwork: true, Order: 1,
	}}); err != nil {
		t.Fatalf("NetworkSave: %v", err)
	}
	yml, err := os.ReadFile(filepath.Join(backend.Dir, "paper.yml"))
	if err != nil {
		t.Fatalf("paper.yml не створено: %v", err)
	}
	ytxt := string(yml)
	if !strings.Contains(ytxt, "velocity-support:") || !strings.Contains(ytxt, "enabled: true") {
		t.Errorf("paper.yml без velocity-support:\n%s", ytxt)
	}
	if _, err := os.Stat(filepath.Join(backend.Dir, "config", "paper-global.yml")); err == nil {
		t.Errorf("для 1.18.2 не має створюватись config/paper-global.yml")
	}
}

func TestNetworkGetView(t *testing.T) {
	api, proxy, backend := newTestNetworkEnv(t)

	view, err := api.NetworkGetView(proxy.ID)
	if err != nil {
		t.Fatalf("NetworkGetView: %v", err)
	}
	if view.ProxyServerID != proxy.ID || view.NetworkSaved {
		t.Errorf("очікували незбережену мережу, маємо %+v", view)
	}
	if len(view.Members) != 1 || view.Members[0].ServerID != backend.ID || view.Members[0].InNetwork {
		t.Errorf("члени мережі невірні: %+v", view.Members)
	}

	if err := api.NetworkSave(proxy.ID, []NetworkMemberView{{
		ServerID: backend.ID, InNetwork: true, Order: 1,
	}}); err != nil {
		t.Fatalf("NetworkSave: %v", err)
	}

	view2, _ := api.NetworkGetView(proxy.ID)
	if !view2.NetworkSaved {
		t.Errorf("після збереження NetworkSaved має бути true")
	}
	if len(view2.Members) != 1 || !view2.Members[0].InNetwork || view2.Members[0].Order != 1 {
		t.Errorf("ролі після збереження невірні: %+v", view2.Members)
	}
}

func TestNetworkSaveRejectsTwoProxies(t *testing.T) {
	api, proxy, backend := newTestNetworkEnv(t)
	// Робимо backend «проксі» і пробуємо включити його в мережу.
	backend.Core = CoreVelocity
	if err := api.store.Save(backend); err != nil {
		t.Fatal(err)
	}
	err := api.NetworkSave(proxy.ID, []NetworkMemberView{{
		ServerID: backend.ID, InNetwork: true, Order: 1,
	}})
	if err == nil {
		t.Fatalf("очікували помилку про два проксі, отримали nil")
	}
}

func TestNetworkStopWithoutSavedConfig(t *testing.T) {
	api, proxy, _ := newTestNetworkEnv(t)

	// Нічого не запущено і конфіга немає → зупинка = тихий no-op без помилки.
	if err := api.NetworkStop(proxy.ID); err != nil {
		t.Fatalf("NetworkStop без конфіга має бути no-op, отримали: %v", err)
	}
}
