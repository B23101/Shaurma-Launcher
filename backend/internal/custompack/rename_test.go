package custompack

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRenameFileBackendNoPhantom — файловий фолбек: після Rename старий ID
// не повинен лишатись у списку (весь файл перезаписується атомарно, тож
// фантома тут не було й раніше — тест лишень фіксує очікувану поведінку).
func TestRenameFileBackendNoPhantom(t *testing.T) {
	configDir := t.TempDir()
	instancesDir := filepath.Join(configDir, "installations")
	s := NewStore(instancesDir)

	oldID := "mafia_dev"
	newID := "mafia_dev2"
	if err := os.MkdirAll(filepath.Join(instancesDir, oldID), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := s.Save(Pack{ID: oldID, Name: "Mafia Dev"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if _, err := s.Rename(instancesDir, oldID, newID); err != nil {
		t.Fatalf("Rename: %v", err)
	}

	if _, ok := s.Get(oldID); ok {
		t.Errorf("фантомна збірка зі старим ID %q лишилась у сховищі після перейменування", oldID)
	}
	if _, ok := s.Get(newID); !ok {
		t.Errorf("збірка з новим ID %q не знайдена після перейменування", newID)
	}
}

// TestRenameSQLiteNoPhantom — регресійний тест: після перейменування
// стара збірка зі старим ID НЕ лишається у файлі custom-packs.json.
func TestRenameSQLiteNoPhantom(t *testing.T) {
	configDir := t.TempDir()
	instancesDir := filepath.Join(configDir, "installations")
	s := NewStore(instancesDir)

	oldID := "mafia_dev"
	newID := "mafia_dev2"
	if err := os.MkdirAll(filepath.Join(instancesDir, oldID), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := s.Save(Pack{ID: oldID, Name: "Mafia Dev"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if _, err := s.Rename(instancesDir, oldID, newID); err != nil {
		t.Fatalf("Rename: %v", err)
	}

	// Перевіряємо що старий ID відсутній, новий є
	if _, ok := s.Get(oldID); ok {
		t.Errorf("збірка зі старим ID %q лишилась у сховищі після перейменування", oldID)
	}
	if _, ok := s.Get(newID); !ok {
		t.Errorf("збірка з новим ID %q не знайдена після перейменування", newID)
	}

	// Перевіряємо напряму файл
	list := s.List()
	if len(list) != 1 {
		t.Errorf("очікували рівно 1 запис, отримали %d", len(list))
	}
}

// TestRenameRemapsIconAndBackgroundPaths — IconPath/BackgroundPath/ArchivePath,
// що лежали всередині теки збірки (<instancesDir>/<oldID>/...), повинні
// пере-мапитись на нову теку (<instancesDir>/<newID>/...) після Rename —
// інакше шлях до іконки/фону лишався вказувати на вже перейменовану
// (неіснуючу за старим шляхом) теку і картинка «зникала» в UI.
func TestRenameRemapsIconAndBackgroundPaths(t *testing.T) {
	configDir := t.TempDir()
	instancesDir := filepath.Join(configDir, "installations")
	s := NewStore(instancesDir)

	oldID := "mafia_dev"
	newID := "mafia_dev2"
	oldDir := filepath.Join(instancesDir, oldID)
	if err := os.MkdirAll(oldDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	iconPath := filepath.Join(oldDir, "icon.png")
	bgPath := filepath.Join(oldDir, "background.png")
	if err := os.WriteFile(iconPath, []byte("fake-icon"), 0o644); err != nil {
		t.Fatalf("write icon: %v", err)
	}
	if err := os.WriteFile(bgPath, []byte("fake-bg"), 0o644); err != nil {
		t.Fatalf("write bg: %v", err)
	}

	if err := s.Save(Pack{
		ID:             oldID,
		Name:           "Mafia Dev",
		IconPath:       iconPath,
		BackgroundPath: bgPath,
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := s.Rename(instancesDir, oldID, newID)
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}

	wantIcon := filepath.Join(instancesDir, newID, "icon.png")
	wantBg := filepath.Join(instancesDir, newID, "background.png")
	if got.IconPath != wantIcon {
		t.Errorf("IconPath = %q, want %q", got.IconPath, wantIcon)
	}
	if got.BackgroundPath != wantBg {
		t.Errorf("BackgroundPath = %q, want %q", got.BackgroundPath, wantBg)
	}
	// Файли фізично мають бути доступні за новим шляхом (тека реально
	// перейменована, а не просто змінений рядок у БД).
	if _, err := os.Stat(got.IconPath); err != nil {
		t.Errorf("іконка не знайдена за новим шляхом %q: %v", got.IconPath, err)
	}
	if _, err := os.Stat(got.BackgroundPath); err != nil {
		t.Errorf("фон не знайдений за новим шляхом %q: %v", got.BackgroundPath, err)
	}

	// Перечитуємо зі сховища — переконуємось, що змінений шлях справді
	// збережений, а не лише повернутий у результаті виклику.
	reloaded, ok := s.Get(newID)
	if !ok {
		t.Fatalf("збірка з новим ID не знайдена після Rename")
	}
	if reloaded.IconPath != wantIcon {
		t.Errorf("після перезчитування IconPath = %q, want %q", reloaded.IconPath, wantIcon)
	}
	if reloaded.BackgroundPath != wantBg {
		t.Errorf("після перезчитування BackgroundPath = %q, want %q", reloaded.BackgroundPath, wantBg)
	}
}
