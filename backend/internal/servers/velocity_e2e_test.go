//go:build integration

package servers

import (
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"shaurma-launcher-backend/internal/model"
)

// TestVelocityE2ERoundtrip — кінцевий тест проксі Velocity ЧЕРЕЗ ТОЙ САМИЙ
// код, що й GUI: InstallCore (fill.papermc.io v3) → Manager.Start →
// дочекатись running (детект "Done") → SendCommand → graceful Stop.
//
// Раніше Velocity качався з api.papermc.io/v2 (закритий, 410 Gone) — тест
// ловив би цей регрес одразу.
//
// Запуск:
//
//	go test -tags integration ./internal/servers/ -run TestVelocityE2ERoundtrip -v -count=1 -timeout 300s
func TestVelocityE2ERoundtrip(t *testing.T) {
	javaPath, err := FindJavaPath(0)
	if err != nil {
		p, e := exec.LookPath("java")
		if e != nil {
			t.Skipf("Java не знайдена: %v", err)
		}
		javaPath = p
	}
	t.Logf("Java: %s", javaPath)

	dir := t.TempDir()
	srv := &Server{
		ID:        "velocity-e2e",
		Name:      "Velocity E2E",
		Dir:       dir,
		Core:      CoreVelocity,
		MCVersion: "", // проксі: версія гри не застосовується
		LoaderVer: "", // не задана → рекомендована стабільна
		JavaMajor: 25,
		RAMMB:     1024,
		Port:      25577,
		ProgramArgs: "",
	}

	// 1. Встановлення velocity.jar (той самий шлях, що InstallCore при старті).
	t.Log("── InstallCore (velocity) ──")
	if err := InstallCore(srv, javaPath, func(p ServerInstallProgress) {
		t.Logf("  install: [%s] %d%% %s", p.Stage, p.Percent, p.Message)
	}); err != nil {
		t.Fatalf("InstallCore: %v", err)
	}
	if !fileExists(filepath.Join(dir, "velocity.jar")) {
		t.Fatal("velocity.jar не створено після InstallCore")
	}
	t.Logf("LoaderVer після install: %q", srv.LoaderVer)

	// 2. Запуск через Manager (як кнопка «Старт» у GUI).
	mgr := NewManager()
	mgr.SetCallbacks(func(id string, _ model.ConsoleLine) {}, func(id string, st ServerStatus) {
		t.Logf("  status event: %s", st.Status)
	})
	if err := mgr.Start(srv, javaPath); err != nil {
		t.Fatalf("Manager.Start: %v", err)
	}

	// 3. Дочекатись "running" (детект "Done ...!" у readOutput).
	t.Log("── очікування running (до 120s) ──")
	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		st := mgr.GetStatus(srv.ID)
		if st.Status == "running" {
			t.Log("OK: статус running")
			break
		}
		if st.Status == "crashed" {
			t.Fatalf("сервер впав: %s (exit %d)", st.LastError, st.ExitCode)
		}
		time.Sleep(1 * time.Second)
	}
	if st := mgr.GetStatus(srv.ID); st.Status != "running" {
		t.Fatalf("статус не дійшов до running: %+v", st)
	}

	// 4. Команда в консоль проксі.
	if err := mgr.SendCommand(srv.ID, "list"); err != nil {
		t.Logf("SendCommand: %v", err) // не критично для завершення тесту
	}

	// 5. Graceful зупинка ("end" для velocity) — той самий шлях StopServer.
	t.Log("── graceful stop ──")
	stopDone := make(chan error, 1)
	go func() { stopDone <- mgr.Stop(srv.ID, true) }()
	select {
	case err := <-stopDone:
		if err != nil {
			t.Fatalf("Stop: %v", err)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("Stop не завершився за 60s")
	}
	if mgr.IsRunning(srv.ID) {
		t.Fatal("IsRunning == true після зупинки")
	}
	if st := mgr.GetStatus(srv.ID); st.Status != "stopped" {
		t.Fatalf("очікували stopped, отримали %+v", st)
	}
	t.Log("PASS: velocity install → start → running → command → graceful stop")
}
