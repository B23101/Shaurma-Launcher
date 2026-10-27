package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBackupCreatedOnWrite(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir, "")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := st.SetMeta("test", "1"); err != nil {
		t.Fatal(err)
	}
	st.Close()

	backupPath := filepath.Join(dir, FileName+BackupSuffix)
	if _, err := os.Stat(backupPath); err != nil {
		t.Fatalf("резервну копію не створено: %v", err)
	}
}

func TestMarkErasedFlag(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.MarkDBErased(); err != nil {
		t.Fatal(err)
	}
	st.Close()

	st2, err := Open(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	h := st2.Health()
	if !h.ErasedIntentionally {
		t.Errorf("ErasedIntentionally = false, очікували true")
	}
	if h.ErasedAt == "" {
		t.Errorf("ErasedAt порожній")
	}
}

func TestRestoreFromCorrupt(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	// Записуємо дані для backup
	st.SaveSettingsRaw(`{"language":"uk"}`)
	st.Close()

	// Псуємо JSON файл
	jsonPath := filepath.Join(dir, FileName)
	if err := os.WriteFile(jsonPath, []byte("GARBAGE NOT JSON {{{{"), 0644); err != nil {
		t.Fatal(err)
	}

	st2, err := Open(dir, "")
	if err != nil {
		t.Fatalf("Open після пошкодження: %v", err)
	}
	defer st2.Close()
	h := st2.Health()
	if !h.RestoredFromBackup {
		t.Errorf("RestoredFromBackup = false, очікували true")
	}
	if err := st2.SetMeta("after-restore", "1"); err != nil {
		t.Errorf("store не працює після відновлення: %v", err)
	}
}
