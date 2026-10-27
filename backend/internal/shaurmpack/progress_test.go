package shaurmpack

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestExportProgressFirstAndLastEvent — експорт .shaurmpack мусить шляти
// ПЕРШУ подію прогресу (0/total) ще ДО першого файлу (резолв джерел іде по
// мережі — без цього модалка експорту весь цей час показує статичні 0%) і
// ФІНАЛЬНУ (total/total) після останнього файлу.
func TestExportProgressFirstAndLastEvent(t *testing.T) {
	gameDir := t.TempDir()
	modsDir := filepath.Join(gameDir, "mods")
	if err := os.MkdirAll(modsDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, n := range []string{"a.jar", "b.jar", "c.jar"} {
		if err := os.WriteFile(filepath.Join(modsDir, n), []byte(n), 0644); err != nil {
			t.Fatalf("write %s: %v", n, err)
		}
	}

	var events []struct{ d, total int }
	outPath := filepath.Join(t.TempDir(), "p.shaurmpack")
	if _, err := Export(ExportOptions{
		GameDir: gameDir, PackID: "p", Name: "P", VersionID: "1.0.0", OutPath: outPath,
		Progress: func(d, total int, fileName string) {
			events = append(events, struct{ d, total int }{d, total})
		},
	}); err != nil {
		t.Fatalf("Export: %v", err)
	}
	if len(events) == 0 {
		t.Fatalf("жодної події прогресу")
	}
	first, last := events[0], events[len(events)-1]
	if first.d != 0 || first.total == 0 {
		t.Errorf("перша подія = %d/%d, хотіли 0/%d (модалка 0%% без руху)", first.d, first.total, last.total)
	}
	if last.d != last.total || last.total == 0 {
		t.Errorf("остання подія = %d/%d, хотіли %d/%d", last.d, last.total, last.total, last.total)
	}
}

// TestBundleExportProgressBoundaries — те саме для експорту папки у
// .shaurmbundle: події 0/total на старті та total/total у кінці, а total —
// це кількість РЕАЛЬНО експортованих збірок (порожні пропускаються).
func TestBundleExportProgressBoundaries(t *testing.T) {
	dir := t.TempDir()
	mk := func(id string) (string, []byte) {
		gameDir := filepath.Join(dir, id, ".minecraft")
		if err := os.MkdirAll(filepath.Join(gameDir, "mods"), 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(gameDir, "mods", id+".jar"), []byte(id), 0644); err != nil {
			t.Fatalf("write: %v", err)
		}
		pd, _ := json.Marshal(map[string]string{"id": id, "name": id})
		return gameDir, pd
	}
	g1, p1 := mk("one")
	g2, p2 := mk("two")
	// «Порожня» збірка без PackData — має бути пропущена і не входити в total.
	outPath := filepath.Join(t.TempDir(), "b.shaurmbundle")
	var events []struct{ d, total int }
	if _, err := ExportBundle(BundleExportOptions{
		Packs: []BundlePackExport{
			{PackID: "one", PackData: p1, GameDir: g1},
			{PackID: "two", PackData: p2, GameDir: g2},
			{PackID: "skip", PackData: nil, GameDir: ""},
		},
		OutPath: outPath,
		Progress: func(d, total int, packID string) {
			events = append(events, struct{ d, total int }{d, total})
		},
	}); err != nil {
		t.Fatalf("ExportBundle: %v", err)
	}
	if len(events) == 0 {
		t.Fatalf("жодної події прогресу")
	}
	first, last := events[0], events[len(events)-1]
	if first.d != 0 || first.total != 2 {
		t.Errorf("перша подія = %d/%d, хотіли 0/2", first.d, first.total)
	}
	if last.d != 2 || last.total != 2 {
		t.Errorf("остання подія = %d/%d, хотіли 2/2", last.d, last.total)
	}
}
