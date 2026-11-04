package safedel

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestProtectedRoots(t *testing.T) {
	base := filepath.Join(t.TempDir(), ".shaurm")
	inst := filepath.Join(base, "installations")
	pack := filepath.Join(inst, "mypack")
	os.MkdirAll(pack, 0o755)
	os.WriteFile(filepath.Join(pack, "x"), []byte("1"), 0o644)
	SetResolver(func() []string { return []string{base, inst} })
	defer SetResolver(nil)

	for _, p := range []string{base, inst, filepath.Dir(base), "", string(filepath.Separator)} {
		if err := RemoveAll(p); !errors.Is(err, ErrProtected) {
			t.Fatalf("%q має бути захищено, err=%v", p, err)
		}
	}
	if _, err := os.Stat(pack); err != nil {
		t.Fatal("збірка зникла")
	}
	if err := RemoveAll(pack); err != nil {
		t.Fatal(err)
	}
	if err := RemoveIfEmpty(inst); err != nil { // порожня → можна, не RemoveAll
		t.Fatal(err)
	}
}

func TestRemoveIfEmptyKeepsData(t *testing.T) {
	d := t.TempDir()
	os.WriteFile(filepath.Join(d, "keep"), []byte("1"), 0o644)
	_ = RemoveIfEmpty(d)
	if _, err := os.Stat(filepath.Join(d, "keep")); err != nil {
		t.Fatal("дані видалено")
	}
}
