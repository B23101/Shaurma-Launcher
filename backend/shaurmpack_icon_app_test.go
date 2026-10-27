package main

// ── .shaurmpack import відновлює персоналізацію збірки ──────────────────
// Коли експорт поклав іконку в корінь архіву (ExportOptions.IconFile →
// icon.png), ImportShaurmpackFile має відновити її в теці нової збірки
// (IconPath="icon.png"), а колір взяти з маніфесту — інакше при імпорті
// лаунчер ставив дефолт-пресет (#ff8a00/ti-puzzle) і оригінальний вигляд
// збірки губився («оригінальна персоналізація не зберігається»).

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"shaurma-launcher-backend/internal/shaurmpack"
)

func TestShaurmpackImportRestoresIconAndColor(t *testing.T) {
	// 1. Фейкова збірка: один bundled-мод + іконка PNG.
	gameDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(gameDir, "mods"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(gameDir, "mods", "sodium.jar"), []byte("fake sodium"), 0644); err != nil {
		t.Fatalf("write jar: %v", err)
	}
	iconBytes := []byte("\x89PNG\r\n\x1a\noriginal-pack-icon")
	iconFile := filepath.Join(t.TempDir(), "icon.png")
	if err := os.WriteFile(iconFile, iconBytes, 0644); err != nil {
		t.Fatalf("write icon: %v", err)
	}

	archive := filepath.Join(t.TempDir(), "orig.shaurmpack")
	if _, err := shaurmpack.Export(shaurmpack.ExportOptions{
		GameDir:       gameDir,
		PackID:        "orig-pack",
		Name:          "Orig Pack",
		VersionID:     "2.5.0",
		GameVersion:   "1.20.1",
		Loader:        "fabric",
		LoaderVersion: "0.15.11",
		Color:         "#3b82f6",
		IconFile:      iconFile,
		OutPath:       archive,
	}); err != nil {
		t.Fatalf("Export: %v", err)
	}

	// 2. Імпортуємо в App без власного вибору іконки/кольору (так само, як
	// фронтенд після детекції: icon='', iconSrcPath='', color='').
	dir := t.TempDir()
	a, _ := newTestApp(t, dir, "seed-pack", "1.20.1", struct{ Loader, Version string }{"fabric", "0.15.11"})
	pack, err := a.ImportShaurmpackFile(archive, "Imported Pack", "", "", "")
	if err != nil {
		t.Fatalf("ImportShaurmpackFile: %v", err)
	}
	if pack.ID == "" {
		t.Fatalf("порожній ID імпортованої збірки")
	}

	// 3. Колір — з маніфесту, а не дефолт лаунчера.
	if pack.Color != "#3b82f6" {
		t.Errorf("Color = %q, хотіли збережений #3b82f6 (дефолт лаунчера підмінив персоналізацію?)", pack.Color)
	}

	// 4. Іконка відновлена: IconPath вказує на icon.png у теці збірки,
	// файл існує і збігається з оригіналом.
	if pack.IconPath != "icon.png" {
		t.Errorf("IconPath = %q, хотіли icon.png (іконка збірки не відновлена)", pack.IconPath)
	}
	stored, ok := a.customPacks.Get(pack.ID)
	if !ok {
		t.Fatalf("збірка %s не знайдена в Store", pack.ID)
	}
	if stored.IconPath != "icon.png" {
		t.Errorf("Store IconPath = %q, хотіли icon.png", stored.IconPath)
	}
	gotIcon, err := os.ReadFile(filepath.Join(a.cfg.InstancesDir(), pack.ID, "icon.png"))
	if err != nil {
		t.Fatalf("icon.png не лежить у теці збірки: %v", err)
	}
	if !bytes.Equal(gotIcon, iconBytes) {
		t.Errorf("відновлена іконка = %d байт, оригінал = %d байт", len(gotIcon), len(iconBytes))
	}

	// 5. Контент збірки теж розпаковано (bundled-шлях без мережі).
	if _, err := os.Stat(filepath.Join(a.cfg.InstancesDir(), pack.ID, ".minecraft", "mods", "sodium.jar")); err != nil {
		t.Errorf("mods/sodium.jar не розпаковано: %v", err)
	}
}

// TestShaurmpackImportUserIconWins — явний вибір користувача (PNG чи пресет)
// має пріоритет над вбудованою іконкою архіву.
func TestShaurmpackImportUserIconWins(t *testing.T) {
	gameDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(gameDir, "mods"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(gameDir, "mods", "a.jar"), []byte("fake"), 0644); err != nil {
		t.Fatalf("write jar: %v", err)
	}
	iconFile := filepath.Join(t.TempDir(), "icon.png")
	if err := os.WriteFile(iconFile, []byte("\x89PNG\r\n\x1a\narchived-icon"), 0644); err != nil {
		t.Fatalf("write icon: %v", err)
	}
	archive := filepath.Join(t.TempDir(), "p.shaurmpack")
	if _, err := shaurmpack.Export(shaurmpack.ExportOptions{
		GameDir: gameDir, PackID: "p", Name: "P", VersionID: "1.0.0",
		GameVersion: "1.20.1", Loader: "fabric", LoaderVersion: "x",
		Color: "#ff0000", IconFile: iconFile, OutPath: archive,
	}); err != nil {
		t.Fatalf("Export: %v", err)
	}

	dir := t.TempDir()
	a, _ := newTestApp(t, dir, "seed", "1.20.1", struct{ Loader, Version string }{"fabric", "x"})

	// Пресет, обраний користувачем, перебиває вбудовану іконку архіву.
	pack, err := a.ImportShaurmpackFile(archive, "Preset Pack", "#10b981", "", "ti-castle")
	if err != nil {
		t.Fatalf("ImportShaurmpackFile: %v", err)
	}
	if pack.Icon != "ti-castle" {
		t.Errorf("Icon = %q, хотіли обраний пресет ti-castle", pack.Icon)
	}
	if pack.IconPath != "" {
		t.Errorf("IconPath = %q, хотіли порожній (пресет, не PNG)", pack.IconPath)
	}
	// Колір користувача теж у пріоритеті над маніфестним.
	if pack.Color != "#10b981" {
		t.Errorf("Color = %q, хотіли обраний #10b981", pack.Color)
	}
}
