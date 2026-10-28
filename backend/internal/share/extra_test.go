package share

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Файл поза root (іконка збірки) іде в передачу під службовим шляхом і доходить цілим.
func TestExtraFileOutsideRoot(t *testing.T) {
	root := t.TempDir()
	writeRand(t, filepath.Join(root, "mods", "a.jar"), 2048)
	iconDir := t.TempDir()
	icon := filepath.Join(iconDir, "icon.png")
	writeRand(t, icon, 777)
	rel := SharedIconDir + "/icon.png"
	extra := map[string]string{rel: icon, "../evil.png": icon}

	m, _, err := BuildManifest(context.Background(), root, KindBuild, "Тест", nil, BuildOptions{Extra: extra})
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Files) != 2 {
		t.Fatalf("файлів %d, очікували 2 (небезпечний шлях має відпасти): %+v", len(m.Files), m.Files)
	}
	r := startHost(t, root, m, false)
	r.host.SetExtraFiles(extra)
	key, err := r.host.AddRecipient("friend1")
	if err != nil {
		t.Fatal(err)
	}
	stage := t.TempDir()
	d := NewDownloader(Options{Dial: r.dial(false), Key: key, StageDir: stage, ExpectManifestID: m.ID()})
	if err := d.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	want, _ := os.ReadFile(icon)
	got, err := os.ReadFile(filepath.Join(FilesDir(stage), filepath.FromSlash(rel)))
	if err != nil || !bytes.Equal(want, got) {
		t.Fatalf("іконка не дійшла: %v", err)
	}
}
