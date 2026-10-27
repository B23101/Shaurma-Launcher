//go:build integration

package servers

import (
	"os/exec"
	"testing"
)

// TestExtraCoreInstallE2E — інсталяція решти ядер (Purpur/Quilt/NeoForge)
// через реальний InstallCore — той самий шлях, яким GUI ставить сервер.
// Ці три ядра не покриті TestInstallAndLaunch*: тут перевіряємо, що
// інсталятор реально качає ядро з мережі й кладе робочий jar.
//
// Запуск: go test -tags integration -run TestExtraCoreInstallE2E -v -count=1 -timeout 900s
func TestExtraCoreInstallE2E(t *testing.T) {
	javaPath, err := FindJavaPath(0)
	if err != nil {
		p, e := exec.LookPath("java")
		if e != nil {
			t.Skipf("Java не знайдена: %v", err)
		}
		javaPath = p
	}
	t.Logf("Java: %s", javaPath)

	cases := []struct {
		name     string
		core     string
		mc       string
		needJava bool
	}{
		{"purpur/1.20.1", "purpur", "1.20.1", false},
		{"quilt/1.20.1", "quilt", "1.20.1", true},
		{"neoforge/1.21.1", "neoforge", "1.21.1", true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			jp := ""
			if c.needJava {
				jp = javaPath
			}
			srv := &Server{
				ID:        "extra-" + c.core,
				Name:      "Extra " + c.core,
				Dir:       dir,
				Core:      c.core,
				MCVersion: c.mc,
				RAMMB:     2048,
			}
			err := InstallCore(srv, jp, func(p ServerInstallProgress) {
				if p.Percent == 100 {
					t.Logf("install: %s", p.Message)
				}
			})
			if err != nil {
				t.Fatalf("InstallCore(%s %s): %v", c.core, c.mc, err)
			}
			det, derr := DetectCore(dir)
			if derr != nil {
				t.Fatalf("DetectCore(%s): %v", c.core, derr)
			}
			t.Logf("detected: core=%s mc=%s loader=%s", det.Core, det.MCVersion, det.LoaderVer)
			// Purpur/Paper/Vanilla кладуть jar як server.jar — до першого
			// запуску детектор не може їх розрізнити (paper.yml/purpur.yml
			// з'являються при старті), тож для purpur перевіряємо лише jar.
			if c.core == "purpur" {
				if det.Core != "vanilla" && det.Core != "paper" && det.Core != "purpur" {
					t.Errorf("purpur: неочікуваний детект %s", det.Core)
				}
			} else if det.Core != c.core {
				t.Errorf("core mismatch: хочемо %s, отримали %s", c.core, det.Core)
			}
			if findServerJar(dir) == "" {
				t.Errorf("після інсталяції %s у папці немає жодного jar", c.core)
			}
		})
	}
}
