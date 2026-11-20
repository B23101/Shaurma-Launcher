package custompack

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCreateInCustomLocationAndDelete(t *testing.T) {
	root := t.TempDir()
	inst := filepath.Join(root, "installations")
	store := NewStore(inst)
	loc := filepath.Join(root, "D-drive", "my-pack")
	p, err := Create(store, inst, CreateParams{Name: "Far", MCVersion: "1.20.1", Loader: LoaderVanilla, Location: loc})
	if err != nil {
		t.Skipf("посилання недоступні: %v", err)
	}
	if p.Location == "" {
		t.Fatal("Location має зберігатись")
	}
	if _, err := os.Stat(filepath.Join(loc, ".minecraft")); err != nil {
		t.Fatal(".minecraft має бути у вибраній теці")
	}
	if got, ok := store.Get(p.ID); !ok || got.Location == "" {
		t.Fatal("збірка має читатись зі стору (через посилання)")
	}
	if err := Delete(store, inst, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(loc); !os.IsNotExist(err) {
		t.Fatal("вміст теки збірки має бути видалено")
	}
	// Непорожня тека відхиляється.
	os.MkdirAll(filepath.Join(root, "busy"), 0755)
	os.WriteFile(filepath.Join(root, "busy", "f"), []byte("x"), 0644)
	if _, err := Create(store, inst, CreateParams{Name: "Busy", MCVersion: "1.20.1", Loader: LoaderVanilla, Location: filepath.Join(root, "busy")}); err == nil {
		t.Fatal("непорожня тека має відхилятись")
	}
}
