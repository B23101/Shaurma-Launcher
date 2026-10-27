package core

// flatpak_test.go — фільтр self-update у Flatpak: shell живе в read-only
// /app/bin і оновлюється тільки `flatpak update`, тож його компонент не
// повинен потрапляти в качку (див. flatpak.go).

import (
	"testing"

	"shrm-updater-backend/internal/manifest"
)

func TestDropComponentByBaseNameDropsShellOnly(t *testing.T) {
	list := []manifest.Component{
		{Path: "ShaurmLauncher", SHA256: "a", Size: 1},
		{Path: "backend.so", SHA256: "b", Size: 2},
		{Path: "updater.so", SHA256: "c", Size: 3},
		{Path: "frontend.zip", SHA256: "d", Size: 4, Kind: "archive", ExtractTo: "frontend"},
	}
	got := dropComponentByBaseName(list, "ShaurmLauncher")
	if len(got) != 3 {
		t.Fatalf("очікували 3 компоненти без shell, отримали %d: %v", len(got), got)
	}
	for _, c := range got {
		if c.Path == "ShaurmLauncher" {
			t.Fatalf("shell не мав лишитись у списку: %v", got)
		}
	}
	// Вхідний слайс не мутуємо — інакше решта логіки Run бачила б
	// «вже відфільтрований» список там, де це не потрібно.
	if len(list) != 4 {
		t.Fatalf("вхідний список змінено: %v", list)
	}
}

func TestDropComponentByBaseNameIsCaseInsensitive(t *testing.T) {
	list := []manifest.Component{
		{Path: "ShaurmLauncherBase.exe", SHA256: "a", Size: 1},
		{Path: "backend.dll", SHA256: "b", Size: 2},
	}
	got := dropComponentByBaseName(list, "ShaurmLauncherBase.exe")
	if len(got) != 1 || got[0].Path != "backend.dll" {
		t.Fatalf("shell іншої редакції мав бути прибраний, отримали: %v", got)
	}
}

func TestDropComponentByBaseNameKeepsEverythingWhenNoMatch(t *testing.T) {
	list := []manifest.Component{{Path: "backend.so"}, {Path: "updater.so"}}
	got := dropComponentByBaseName(list, "ShaurmLauncher")
	if len(got) != 2 {
		t.Fatalf("без shell у списку нічого прибирати не можна: %v", got)
	}
}

// dropFlatpakShellComponent поза Flatpak (/.flatpak-info немає — типовий
// прогін тестів на будь-якій машині) не повинен чіпати список: інакше
// Windows/AppImage/DEB/RPM втратили б self-update shell.
func TestDropFlatpakShellComponentOutsideFlatpakIsNoOp(t *testing.T) {
	if runningInFlatpak() {
		t.Skip("тест має сенс лише поза Flatpak-пісочницею")
	}
	list := []manifest.Component{{Path: "ShaurmLauncher.exe"}, {Path: "backend.dll"}}
	got := dropFlatpakShellComponent(list, ChannelShaurma)
	if len(got) != 2 {
		t.Fatalf("поза Flatpak список качки не має змінюватись: %v", got)
	}
}
