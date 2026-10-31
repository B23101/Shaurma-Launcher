package servers

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestUnzipRejectsTraversalBeforeWriting(t *testing.T) {
	for _, name := range []string{"../escape.txt", "a/../../escape.txt", "C:/escape.txt", "a\\..\\escape.txt"} {
		t.Run(name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "backup.zip")
			f, err := os.Create(file)
			if err != nil {
				t.Fatal(err)
			}
			z := zip.NewWriter(f)
			for _, n := range []string{"ok.txt", name} {
				w, err := z.Create(n)
				if err != nil {
					t.Fatal(err)
				}
				w.Write([]byte("data"))
			}
			z.Close()
			f.Close()
			dir := t.TempDir()
			if unzipTo(file, dir) == nil {
				t.Fatal("unsafe backup accepted")
			}
			entries, _ := os.ReadDir(dir)
			if len(entries) != 0 {
				t.Fatal("wrote before validation")
			}
		})
	}
}
func TestExportBackupRejectsTraversal(t *testing.T) {
	if ExportBackup(t.TempDir(), "../outside", t.TempDir()) == nil {
		t.Fatal("unsafe backup ID accepted")
	}
}

func TestUnzipValidBackup(t *testing.T) {
	file := filepath.Join(t.TempDir(), "backup.zip")
	f, err := os.Create(file)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	w, err := z.Create("world/level.dat")
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("world-data"))
	z.Close()
	f.Close()
	dir := t.TempDir()
	if err := unzipTo(file, dir); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "world", "level.dat"))
	if err != nil || string(b) != "world-data" {
		t.Fatalf("backup restore: %q %v", b, err)
	}
}

func TestRestoreRejectsForeignFilesWithoutChangingWorld(t *testing.T) {
	for _, name := range []string{"server.properties", "../outside.txt", "other-world/level.dat"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			world := filepath.Join(dir, "world")
			if err := os.MkdirAll(world, 0755); err != nil {
				t.Fatal(err)
			}
			original := filepath.Join(world, "level.dat")
			os.WriteFile(original, []byte("original"), 0644)
			backups := filepath.Join(dir, "backups")
			os.MkdirAll(backups, 0755)
			f, err := os.Create(filepath.Join(backups, "bad.zip"))
			if err != nil {
				t.Fatal(err)
			}
			z := zip.NewWriter(f)
			w, err := z.Create(name)
			if err != nil {
				t.Fatal(err)
			}
			w.Write([]byte("bad"))
			z.Close()
			f.Close()
			if err := RestoreBackup(dir, "world", backups, "bad"); err == nil {
				t.Fatal("foreign backup accepted")
			}
			b, err := os.ReadFile(original)
			if err != nil || string(b) != "original" {
				t.Fatalf("original world damaged: %q %v", b, err)
			}
			if _, err := os.Stat(filepath.Join(dir, "server.properties")); !os.IsNotExist(err) {
				t.Fatal("overwrote server configuration")
			}
		})
	}
}

func TestRestoreRejectsParentWorld(t *testing.T) {
	dir := t.TempDir()
	backups := filepath.Join(dir, "backups")
	os.MkdirAll(backups, 0755)
	os.WriteFile(filepath.Join(backups, "test.zip"), nil, 0644)
	if RestoreBackup(dir, "..", backups, "test") == nil {
		t.Fatal("parent world accepted")
	}
}
