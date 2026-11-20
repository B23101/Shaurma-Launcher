package java

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRuntimeMarkerRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if _, ok := readRuntimeMarker(dir); ok {
		t.Fatal("мітки ще немає — старе встановлення має вважатись невідомим")
	}
	want := runtimeMarker{Component: "java-runtime-delta", Version: "21.0.7", SHA1: "abc", CheckedAt: time.Now().Truncate(time.Second)}
	if err := writeRuntimeMarker(dir, want); err != nil {
		t.Fatal(err)
	}
	got, ok := readRuntimeMarker(dir)
	if !ok || got.SHA1 != want.SHA1 || got.Version != want.Version || !got.CheckedAt.Equal(want.CheckedAt) {
		t.Fatalf("мітка не збереглась: %+v", got)
	}
}

func TestCheckDue(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name    string
		checked time.Time
		want    bool
	}{
		{"щойно", now.Add(-time.Minute), false},
		{"добу тому", now.Add(-25 * time.Hour), true},
		{"годинник у майбутньому", now.Add(48 * time.Hour), true},
	}
	for _, c := range cases {
		if got := checkDue(runtimeMarker{CheckedAt: c.checked}, now); got != c.want {
			t.Errorf("%s: checkDue=%v, want %v", c.name, got, c.want)
		}
	}
}

func TestSwapDirReplacesOldJava(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "java-21")
	staging := dir + ".update"
	os.MkdirAll(dir, 0755)
	os.MkdirAll(staging, 0755)
	os.WriteFile(filepath.Join(dir, "old.txt"), []byte("old"), 0644)
	os.WriteFile(filepath.Join(staging, "new.txt"), []byte("new"), 0644)

	if err := swapDir(dir, staging); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "new.txt")); err != nil {
		t.Fatal("нова Java не на місці")
	}
	for _, p := range []string{filepath.Join(dir, "old.txt"), staging, dir + ".old"} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%s мало зникнути", p)
		}
	}
}

func TestSwapDirIntoMissingDir(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "java-17")
	staging := dir + ".update"
	os.MkdirAll(staging, 0755)
	if err := swapDir(dir, staging); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatal("тека java-17 мала зʼявитись")
	}
}
