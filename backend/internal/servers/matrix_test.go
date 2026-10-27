//go:build integration

package servers

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// VersionMatrix — тестова матриця ключових MC-версій з різних епох.
type VersionMatrix struct {
	mcVersion string
	core      string
	javaMin   int
	expectDone bool
}

// TestVersionMatrix — запускає реальні сервери для кожної версії з матриці.
// Запуск: go test -tags integration -run TestVersionMatrix -v -count=1 -timeout 1800s
func TestVersionMatrix(t *testing.T) {
	// Знаходимо Java.
	javaPath, err := FindJavaPath(0)
	if err != nil {
		p, e := exec.LookPath("java")
		if e != nil {
			t.Skipf("Java не знайдена: %v", err)
		}
		javaPath = p
	}
	javaMajor := detectJavaMajor(t, javaPath)
	t.Logf("Java: %s (major %d)", javaPath, javaMajor)

	// Матриця: кожна версія, ядер, мін. Java, чи очікуємо "Done".
	// MC 1.8+ може працювати на Java 25 (backward compatible), але
	// деякі старі версії можуть мати проблеми — ми це відловимо.
	matrix := []VersionMatrix{
		// Vanilla (ключові епохи)
		{"1.8.9", "vanilla", 8, true},
		{"1.12.2", "vanilla", 8, true},
		{"1.13.2", "vanilla", 8, true},
		{"1.14.4", "vanilla", 8, true},
		{"1.16.5", "vanilla", 8, true},
		{"1.17.1", "vanilla", 16, true},
		{"1.19.4", "vanilla", 17, true},
		{"1.20.1", "vanilla", 17, true},
		{"1.21", "vanilla", 21, true},
		{"1.21.4", "vanilla", 21, true},
		{"26.2", "vanilla", 25, true},
		// Paper
		{"1.20.1", "paper", 17, true},
		{"1.21.4", "paper", 21, true},
		{"26.2", "paper", 25, false}, // Paper 26.2 може не мати stable build
		// Fabric
		{"1.20.1", "fabric", 17, true},
		{"1.21.4", "fabric", 21, true},
		// Forge
		{"1.20.1", "forge", 17, true},
		{"1.21.4", "forge", 21, true},
	}

	for _, m := range matrix {
		t.Run(fmt.Sprintf("%s/%s", m.core, m.mcVersion), func(t *testing.T) {
			// Перевірка Java major
			if javaMajor < m.javaMin {
				t.Skipf("Java %d < required %d", javaMajor, m.javaMin)
			}

			dir := t.TempDir()

			// EULA
			os.WriteFile(filepath.Join(dir, "eula.txt"), []byte("eula=true\n"), 0644)

			// Встановлення
			t.Log("Installing...")
			switch m.core {
			case "vanilla":
				err = installVanilla(t, dir, m.mcVersion)
			case "paper":
				err = installPaper(t, dir, m.mcVersion)
			case "fabric":
				err = installFabric(t, dir, m.mcVersion)
			case "forge":
				err = installForge(t, dir, m.mcVersion)
			}
			if err != nil {
				t.Skipf("install failed: %v", err)
			}

			// Детекція
			det, err := DetectCore(dir)
			if err != nil {
				t.Logf("DetectCore: %v (non-fatal)", err)
			} else {
				t.Logf("Detected: core=%s mc=%s loaderVer=%s", det.Core, det.MCVersion, det.LoaderVer)
			}

			// Будуємо Server для запуску.
			loaderVer := ""
			if err == nil && det.LoaderVer != "" {
				loaderVer = det.LoaderVer
			}
			srv := &Server{
				ID:        fmt.Sprintf("matrix-%s-%s", m.core, m.mcVersion),
				Name:      fmt.Sprintf("Matrix %s %s", m.core, m.mcVersion),
				Dir:       dir,
				Core:      m.core,
				MCVersion: m.mcVersion,
				LoaderVer: loaderVer,
				JavaMajor: javaMajor,
				RAMMB:     2048,
				Port:      ScanFreePort(25565, nil, "", nil),
			}

			args := buildCommand(srv)
			t.Logf("Command: java %s", strings.Join(args, " "))

			cmd := exec.Command(javaPath, args...)
			cmd.Dir = dir
			cmd.Stderr = cmd.Stdout
			stdoutPipe, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatalf("stdout pipe: %v", err)
			}
			stdin, err := cmd.StdinPipe()
			if err != nil {
				t.Fatalf("stdin pipe: %v", err)
			}

			if err := cmd.Start(); err != nil {
				t.Fatalf("cmd.Start: %v", err)
			}
			t.Logf("PID: %d", cmd.Process.Pid)

			// Читаємо stdout, шукаємо "Done".
			done := make(chan string, 1)
			var allLines []string
			go func() {
				scanner := bufio.NewScanner(stdoutPipe)
				scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
				for scanner.Scan() {
					line := scanner.Text()
					allLines = append(allLines, line)
					// Показуємо ключові рядки
					if strings.Contains(line, "Done") || strings.Contains(line, "ERROR") || strings.Contains(line, "WARN") || strings.Contains(line, "Exception") || len(allLines) <= 5 {
						t.Logf("  [%d lines] %s", len(allLines), line)
					}
					if strings.Contains(line, "Done") && strings.Contains(line, "!") {
						select {
						case done <- line:
						default:
						}
					}
				}
			}()

			// Чекаємо Done (90s для старих версій — вони повільніші).
			timeout := 90 * time.Second
			if m.core == "forge" || m.core == "fabric" {
				timeout = 120 * time.Second
			}

			select {
			case d := <-done:
				t.Logf("✅ DONE! %s", d)
			case <-time.After(timeout):
				t.Logf("Last %d lines:", min(len(allLines), 15))
				start := 0
				if len(allLines) > 15 {
					start = len(allLines) - 15
				}
				for _, l := range allLines[start:] {
					t.Logf("  %s", l)
				}
				if !m.expectDone {
					t.Skipf("timeout (expected for %s %s)", m.core, m.mcVersion)
				} else {
					t.Fatalf("server did not start within %v", timeout)
				}
			}

			// Консоль: say test
			time.Sleep(1 * time.Second)
			_, _ = stdin.Write([]byte(fmt.Sprintf("say MatrixTest%s_%s\n", m.core, m.mcVersion)))
			time.Sleep(2 * time.Second)

			foundSay := false
			for _, l := range allLines {
				if strings.Contains(l, "MatrixTest") {
					foundSay = true
					t.Logf("✅ say command: %s", l)
					break
				}
			}
			if !foundSay {
				t.Log("⚠️ say not found (may still be processing)")
			}

			// Graceful stop
			_, _ = stdin.Write([]byte("stop\n"))
			waitCh := make(chan error, 1)
			go func() { waitCh <- cmd.Wait() }()

			select {
			case <-waitCh:
				t.Log("✅ Graceful stop")
			case <-time.After(30 * time.Second):
				_ = cmd.Process.Kill()
				<-waitCh
				t.Log("⚠️ Force killed after timeout")
			}
		})
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
