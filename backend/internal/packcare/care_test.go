package packcare

import (
	"os"
	"path/filepath"
	"shaurma-launcher-backend/internal/mcoptions"
	"testing"
)

func TestPresetRestorePreservesUnrelatedChanges(t *testing.T) {
	dir := t.TempDir()
	path := mcoptions.OptionsFilePath(dir)
	if err := mcoptions.WriteFile(path, map[string]string{"renderDistance": "18", "lang": "uk_ua"}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"low", "balanced"} {
		if err := ApplyPreset(dir, p); err != nil {
			t.Fatal(err)
		}
	}
	opts, _ := mcoptions.ParseFile(path)
	opts["lang"] = "en_us"
	mcoptions.WriteFile(path, opts)
	if err := ApplyPreset(dir, "restore"); err != nil {
		t.Fatal(err)
	}
	opts, _ = mcoptions.ParseFile(path)
	if opts["renderDistance"] != "18" || opts["lang"] != "en_us" {
		t.Fatal(opts)
	}
	if _, ok := opts["simulationDistance"]; ok {
		t.Fatal("invented original option")
	}
}

func TestSnapshotRestoreNeverOverwritesWorld(t *testing.T) {
	dir := t.TempDir()
	world := filepath.Join(dir, ".minecraft", "saves", "my-world")
	os.MkdirAll(world, 0700)
	os.WriteFile(filepath.Join(world, "level.dat"), []byte("before"), 0600)
	name, err := Snapshot(dir)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(world, "level.dat"), []byte("after"), 0600)
	copies, err := Restore(dir, name)
	if err != nil {
		t.Fatal(err)
	}
	original, _ := os.ReadFile(filepath.Join(world, "level.dat"))
	restored, _ := os.ReadFile(filepath.Join(dir, ".minecraft", "saves", copies[0], "level.dat"))
	if string(original) != "after" || string(restored) != "before" {
		t.Fatal("world data changed")
	}
	if _, err := Restore(dir, "../outside"); err == nil {
		t.Fatal("accepted traversal")
	}
}

func TestSnapshotRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	saves := filepath.Join(dir, ".minecraft", "saves")
	os.MkdirAll(saves, 0700)
	if err := os.Symlink(t.TempDir(), filepath.Join(saves, "linked")); err != nil {
		t.Skip(err)
	}
	if _, err := Snapshot(dir); err == nil {
		t.Fatal("accepted symlink")
	}
	list, err := List(dir)
	if err != nil || len(list) != 0 {
		t.Fatal(list, err)
	}
}
