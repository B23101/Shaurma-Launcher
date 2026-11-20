package modstore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHashDirDedupesKnownOnly(t *testing.T) {
	root := t.TempDir()
	store := filepath.Join(root, "store")
	mk := func(pack, name, body string) string {
		d := filepath.Join(root, pack, ".minecraft", "mods")
		os.MkdirAll(d, 0755)
		p := filepath.Join(d, name)
		os.WriteFile(p, []byte(body), 0644)
		return d
	}
	a := mk("a", "x.jar", "same-mod")
	b := mk("b", "x.jar", "same-mod")
	mk("b", "own.jar", "not-on-modrinth")

	known := func(hs []string) (map[string]bool, error) {
		out := map[string]bool{}
		for _, h := range hs {
			s, _ := fileSHA1(filepath.Join(b, "x.jar"))
			if h == s {
				out[h] = true
			}
		}
		return out, nil
	}
	if _, err := HashDir(store, a, known); err != nil {
		t.Fatal(err)
	}
	r, err := HashDir(store, b, known)
	if err != nil {
		t.Fatal(err)
	}
	if r.Hashed != 1 || r.Skipped != 1 || r.Saved == 0 {
		t.Fatalf("unexpected result %+v", r)
	}
	fa, _ := os.Stat(filepath.Join(a, "x.jar"))
	fb, _ := os.Stat(filepath.Join(b, "x.jar"))
	if !os.SameFile(fa, fb) {
		t.Fatal("expected hardlinked files")
	}
	if got, _ := os.ReadFile(filepath.Join(b, "own.jar")); string(got) != "not-on-modrinth" {
		t.Fatal("unknown mod must stay untouched")
	}
	if CountHashed(b) != 1 || !HashedSet(b)["x.jar"] || HashedSet(b)["own.jar"] {
		t.Fatal("marker mismatch")
	}
}
