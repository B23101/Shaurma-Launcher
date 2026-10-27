package custompack

import (
	"path/filepath"
	"testing"
)

// TestRemapAssetPaths перевіряє пере-маппінг абсолютних шляхів асетів після
// зміни папки даних (SetAppDir): IconPath/BackgroundPath/ArchivePath, що
// вказували всередину старої папки .shaurm, мають вказати на нову. Шляхи
// поза папкою даних (користувацький архів в іншій теці) не чіпаються.
func TestRemapAssetPaths(t *testing.T) {
	configDir := t.TempDir()
	oldBase := filepath.Join(configDir, "old-shurm")
	newBase := filepath.Join(configDir, "new-shurm")
	instDir := filepath.Join(oldBase, "installations")

	s := NewStore(filepath.Join(configDir, "instances"))
	packID := "test-pack-1"
	p := Pack{
		ID:             packID,
		Name:           "Тест",
		IconPath:       filepath.Join(instDir, packID, "icon.png"),
		BackgroundPath: filepath.Join(instDir, packID, "background.png"),
		ArchivePath:    filepath.Join(instDir, packID, "archive.mrpack"),
	}
	if err := s.Save(p); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if err := s.RemapAssetPaths(oldBase, newBase); err != nil {
		t.Fatalf("RemapAssetPaths: %v", err)
	}

	got, ok := s.Get(packID)
	if !ok {
		t.Fatalf("збірка зникла після ремапу")
	}
	wantIcon := filepath.Join(newBase, "installations", packID, "icon.png")
	if got.IconPath != wantIcon {
		t.Errorf("IconPath = %q, want %q", got.IconPath, wantIcon)
	}
	wantBg := filepath.Join(newBase, "installations", packID, "background.png")
	if got.BackgroundPath != wantBg {
		t.Errorf("BackgroundPath = %q, want %q", got.BackgroundPath, wantBg)
	}
	wantArc := filepath.Join(newBase, "installations", packID, "archive.mrpack")
	if got.ArchivePath != wantArc {
		t.Errorf("ArchivePath = %q, want %q", got.ArchivePath, wantArc)
	}
}

// TestRemapAssetPathsOutsideLeavesUntouched — шляхи поза папкою даних
// (напр. архів, що користувач тримає на іншому диску) лишаються як є.
func TestRemapAssetPathsOutsideLeavesUntouched(t *testing.T) {
	configDir := t.TempDir()
	oldBase := filepath.Join(configDir, "old-shurm")
	newBase := filepath.Join(configDir, "new-shurm")
	outside := filepath.Join(configDir, "outside", "my-archive.mrpack")

	s := NewStore(filepath.Join(configDir, "instances"))
	p := Pack{
		ID:          "test-pack-2",
		Name:        "З архівом",
		IconPath:    filepath.Join(oldBase, "installations", "test-pack-2", "icon.png"),
		ArchivePath: outside,
	}
	if err := s.Save(p); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if err := s.RemapAssetPaths(oldBase, newBase); err != nil {
		t.Fatalf("RemapAssetPaths: %v", err)
	}

	got, _ := s.Get("test-pack-2")
	wantIcon := filepath.Join(newBase, "installations", "test-pack-2", "icon.png")
	if got.IconPath != wantIcon {
		t.Errorf("IconPath = %q, want %q", got.IconPath, wantIcon)
	}
	if got.ArchivePath != outside {
		t.Errorf("ArchivePath = %q, want %q (поза папкою даних не чіпаємо)", got.ArchivePath, outside)
	}
}

// TestRemapAssetPathsSameBaseNoop — той самий baseDir: нічого не міняється.
func TestRemapAssetPathsSameBaseNoop(t *testing.T) {
	configDir := t.TempDir()
	base := filepath.Join(configDir, "shurm")

	s := NewStore(filepath.Join(configDir, "instances"))
	p := Pack{
		ID:       "test-pack-3",
		Name:     "Без змін",
		IconPath: filepath.Join(base, "installations", "test-pack-3", "icon.png"),
	}
	if err := s.Save(p); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if err := s.RemapAssetPaths(base, base); err != nil {
		t.Fatalf("RemapAssetPaths: %v", err)
	}

	// Файл фолбеку не повинен бути перезаписаний зайвий раз (але це не
	// критично) — головне, шлях лишився тим самим.
	got, _ := s.Get("test-pack-3")
	if got.IconPath != p.IconPath {
		t.Errorf("IconPath = %q, want %q", got.IconPath, p.IconPath)
	}
}

// TestRemapAssetPathsSQLite — той самий ремап, але коли Store працює на
// SQLite (cfg.Store() != nil): дані зберігаються в БД, не у файл.
func TestRemapAssetPathsFileBased(t *testing.T) {
	configDir := t.TempDir()

	oldBase := filepath.Join(configDir, "old-shurm")
	newBase := filepath.Join(configDir, "new-shurm")

	s := NewStore(filepath.Join(configDir, "instances"))

	packID := "test-1"
	if err := s.Save(Pack{
		ID:       packID,
		Name:     "Test",
		IconPath: filepath.Join(oldBase, "installations", packID, "icon.png"),
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if err := s.RemapAssetPaths(oldBase, newBase); err != nil {
		t.Fatalf("RemapAssetPaths: %v", err)
	}

	got, ok := s.Get(packID)
	if !ok {
		t.Fatal("Get: not found after remap")
	}
	want := filepath.Join(newBase, "installations", packID, "icon.png")
	if got.IconPath != want {
		t.Errorf("IconPath = %s, want %s", got.IconPath, want)
	}
}
