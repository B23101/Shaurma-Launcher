package main

import (
	"runtime"
	"testing"
)

// TestWailsSanitizeAppName — формула application-id у Wails v3 зашита в
// бібліотеку (pkg/application/application_linux.go: sanitizeAppName), і від
// неї залежить ІМ'Я .desktop-файлу в пакетах. Тест фіксує копію формули
// (app_identity.go) на випадок, якщо хтось колись змінить Options.Name або
// «причеше» sanitize: мовчазна розбіжність = знову узагальнена іконка в
// таскбарі замість іконки лаунчера.
func TestWailsSanitizeAppName(t *testing.T) {
	cases := map[string]string{
		"Shaurma Launcher":     "shaurma_launcher",
		"Shaurma Launcher Alt": "shaurma_launcher_alt",
		"  Shaurma  ":          "shaurma",
		// Цифра на початку: sanitize підставляє "_" перед нею, а потім
		// strings.Trim("_") цей "_" знімає — тобто правило в wails v3
		// фактично нейтральне. Тест фіксує САМЕ ту поведінку, що в бібліотеці
		// (інакше наш id .desktop розійшовся б з application-id).
		"2 Fast": "2_fast",
		"":       "wailsapp",
		"---":    "---",
		"a.b/c":  "a_b_c",
	}
	for in, want := range cases {
		if got := wailsSanitizeAppName(in); got != want {
			t.Errorf("wailsSanitizeAppName(%q) = %q, очікували %q", in, got, want)
		}
	}
}

// TestLinuxAppIDMatchesPackaging — id, який рахує shell, мусить збігатися з
// linuxAppIDTokens у pack-manager/internal/launcherbuild/paths.go
// (пакувальник саме під цим іменем кладе .desktop у пакет). Розбіжність =
// оболонка не знайде .desktop для вікна.
func TestLinuxAppIDMatchesPackaging(t *testing.T) {
	prev := editionChannel
	defer func() { editionChannel = prev }()

	cases := map[string]string{
		"shaurma": "org.wails.shaurma_launcher",
		"base":    "org.wails.shaurma_launcher_alt",
	}
	for edition, want := range cases {
		editionChannel = edition
		if got := linuxAppID(); got != want {
			t.Errorf("linuxAppID() для редакції %q = %q, очікували %q", edition, got, want)
		}
		// appOptionsName() — суто технічний токен для application-id, тож
		// на Linux він МАЄ відрізнятись між редакціями (інакше обидві
		// ділили б один .desktop-файл). Водночас людяна назва
		// (launcherDisplayName(), яку реально показуємо користувачу) має
		// лишатись ОДНАКОВОЮ для всіх редакцій — жодного "Base"/"повна".
		if runtime.GOOS == "linux" {
			wantToken := linuxAppIDTokenShaurma
			if edition == "base" {
				wantToken = linuxAppIDTokenBase
			}
			if appOptionsName() != wantToken {
				t.Errorf("appOptionsName() = %q, очікували технічний токен %q", appOptionsName(), wantToken)
			}
		}
		if launcherDisplayName() != launcherDisplayNameShaurma {
			t.Errorf("launcherDisplayName() = %q, має бути однаковим для всіх редакцій: %q", launcherDisplayName(), launcherDisplayNameShaurma)
		}
	}
}
