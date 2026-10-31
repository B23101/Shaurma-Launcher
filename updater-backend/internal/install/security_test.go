package install

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"shrm-updater-backend/internal/download"
	"shrm-updater-backend/internal/manifest"
	"testing"
)

func TestApplyPreflightKeepsExistingVersion(t *testing.T) {
	dir, tmp := t.TempDir(), t.TempDir()
	first := manifest.Component{Path: "backend.dll", URL: "/backend", Size: 3, SHA256: hexSHA([]byte("new"))}
	second := manifest.Component{Path: "second.dll", URL: "/second", Size: 3, SHA256: hexSHA([]byte("new"))}
	for name, data := range map[string]string{"backend.dll": "new", "second.dll": "bad"} {
		if err := os.WriteFile(filepath.Join(tmp, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, first.Path), []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	err := Apply(context.Background(), dir, []download.DownloadedComponent{{Component: first, TempPath: filepath.Join(tmp, first.Path)}, {Component: second, TempPath: filepath.Join(tmp, second.Path)}}, &manifest.Manifest{Version: "3", Components: []manifest.Component{first, second}}, "en", nil)
	if err == nil {
		t.Fatal("corrupt batch accepted")
	}
	if b, e := os.ReadFile(filepath.Join(dir, first.Path)); e != nil || string(b) != "old" {
		t.Fatalf("old version modified: %q %v", b, e)
	}
}
func TestApplyDoesNotFollowExternalDirectory(t *testing.T) {
	dir, outside, tmp := t.TempDir(), t.TempDir(), t.TempDir()
	link := filepath.Join(dir, "external")
	if err := os.Symlink(outside, link); err != nil {
		if runtime.GOOS != "windows" {
			t.Fatal(err)
		}
		if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, outside).CombinedOutput(); err != nil {
			t.Fatalf("junction fixture: %v %s", err, out)
		}
	}
	c := manifest.Component{Path: "external/test.dll", URL: "/test", Size: 3, SHA256: hexSHA([]byte("new"))}
	src := filepath.Join(tmp, "test.dll")
	if err := os.WriteFile(src, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	err := Apply(context.Background(), dir, []download.DownloadedComponent{{Component: c, TempPath: src}}, &manifest.Manifest{Version: "3", Components: []manifest.Component{c}}, "en", nil)
	if err == nil {
		t.Fatal("external destination accepted")
	}
	if _, err := os.Stat(filepath.Join(outside, "test.dll")); !os.IsNotExist(err) {
		t.Fatal("wrote outside installation")
	}
}
func TestApplySavesPendingProof(t *testing.T) {
	dir, tmp := t.TempDir(), t.TempDir()
	c := manifest.Component{Path: manifest.UpdaterLibraryName(), URL: "/updater", Size: 3, SHA256: hexSHA([]byte("new"))}
	src := filepath.Join(tmp, c.Path)
	if err := os.WriteFile(src, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	m := &manifest.Manifest{Version: "3", Components: []manifest.Component{c}, Proof: &manifest.UpdateProof{Payload: []byte(`{"version":"3"}`), Signature: "test-only"}}
	if err := Apply(context.Background(), dir, []download.DownloadedComponent{{Component: c, TempPath: src}}, m, "en", nil); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, c.Path+".new")); err != nil || string(b) != "new" {
		t.Fatalf("pending component missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, c.Path+".new.proof")); err != nil {
		t.Fatal(err)
	}
}
