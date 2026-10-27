package shaurmpack

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestExportEmbedsIconFile — експорт із IconFile кладе іконку збірки в
// КОРІНЬ .shaurmpack під фіксованим іменем icon.png (поза overrides/
// bundled — це персоналізація збірки, а не ігровий файл .minecraft/).
// Імпортер (ImportShaurmpackFile на рівні App) відновлює її як іконку
// збірки — без цього оригінальна іконка губилась при імпорті.
func TestExportEmbedsIconFile(t *testing.T) {
	gameDir := t.TempDir()
	modsDir := filepath.Join(gameDir, "mods")
	if err := os.MkdirAll(modsDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(modsDir, "sodium.jar"), []byte("fake sodium"), 0644); err != nil {
		t.Fatalf("write jar: %v", err)
	}

	iconBytes := []byte("\x89PNG\r\n\x1a\nfake-icon-content-for-test")
	iconFile := filepath.Join(t.TempDir(), "icon.png")
	if err := os.WriteFile(iconFile, iconBytes, 0644); err != nil {
		t.Fatalf("write icon: %v", err)
	}

	export := func() string {
		outPath := filepath.Join(t.TempDir(), "pack.shaurmpack")
		_, err := Export(ExportOptions{
			GameDir: gameDir, PackID: "p1", Name: "P1", VersionID: "1.0.0",
			Color: "#22cc88", IconFile: iconFile, OutPath: outPath,
		})
		if err != nil {
			t.Fatalf("Export: %v", err)
		}
		return outPath
	}

	// 1) Іконка є в корені архіву з точними байтами.
	outPath := export()
	r, err := zip.OpenReader(outPath)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	var got []byte
	for _, f := range r.File {
		if f.Name == "icon.png" {
			rc, err := f.Open()
			if err != nil {
				t.Fatalf("open icon entry: %v", err)
			}
			buf := new(bytes.Buffer)
			_, _ = buf.ReadFrom(rc)
			rc.Close()
			got = buf.Bytes()
		}
	}
	r.Close()
	if !bytes.Equal(got, iconBytes) {
		t.Fatalf("icon.png в архіві = %d байт, хотіли оригінальні %d байт", len(got), len(iconBytes))
	}

	// 2) Import (bundled, без мережі) НЕ розпаковує icon.png у .minecraft —
	// вона поза маніфестом, тож лишається лише в корені архіву.
	target := t.TempDir()
	if _, err := Import(ImportOptions{ShaurmPackPath: outPath, TargetGameDir: target}); err != nil {
		t.Fatalf("Import: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "icon.png")); err == nil {
		t.Fatalf("icon.png не має потрапляти в .minecraft при імпорті (вона не ігровий файл)")
	}
	if _, err := os.Stat(filepath.Join(target, "mods", "sodium.jar")); err != nil {
		t.Fatalf("mods/sodium.jar не розпаковано: %v", err)
	}

	// 3) Без IconFile іконки в архіві немає (стара поведінка — без регресу).
	outPath2 := exportWithoutIcon(t, gameDir)
	r2, err := zip.OpenReader(outPath2)
	if err != nil {
		t.Fatalf("open zip 2: %v", err)
	}
	defer r2.Close()
	for _, f := range r2.File {
		if f.Name == "icon.png" {
			t.Fatalf("icon.png не має бути в архіві без IconFile")
		}
	}
}

func exportWithoutIcon(t *testing.T, gameDir string) string {
	t.Helper()
	outPath := filepath.Join(t.TempDir(), "pack2.shaurmpack")
	_, err := Export(ExportOptions{
		GameDir: gameDir, PackID: "p2", Name: "P2", VersionID: "1.0.0", OutPath: outPath,
	})
	if err != nil {
		t.Fatalf("Export без іконки: %v", err)
	}
	return outPath
}
