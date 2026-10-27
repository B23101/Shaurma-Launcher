package sync

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateRelPath(t *testing.T) {
	bad := []string{
		"", "../evil.jar", "mods/../../evil.jar", "/etc/passwd", "C:/Windows/x.jar", "C:evil",
		`..\evil.jar`, `mods\..\..\evil`, "mods/a\x00.jar", "mods/evil.exe", "mods/Run.BAT",
		"scripts/x.ps1", "mods/evil.exe.", "mods/evil.exe ", "mods/a.jar:stream",
	}
	for _, p := range bad {
		if err := ValidateRelPath(p); err == nil {
			t.Errorf("шлях %q має бути відхилений", p)
		}
	}
	good := []string{"mods/a.jar", "config/sodium-options.json", "options.txt", "kubejs/server_scripts/x.js", "mods/mod..name.jar"}
	for _, p := range good {
		if err := ValidateRelPath(p); err != nil {
			t.Errorf("шлях %q має бути дозволений: %v", p, err)
		}
	}
}

func TestSafeJoinStaysInside(t *testing.T) {
	base := t.TempDir()
	p, err := SafeJoin(base, "mods/a.jar")
	if err != nil || !strings.HasPrefix(p, base) {
		t.Fatalf("SafeJoin: %q %v", p, err)
	}
	if _, err := SafeJoin(base, "../x.jar"); err == nil {
		t.Fatal("вихід за межі base не відхилено")
	}
	if _, err := safeJoinDir(base, "."); err == nil {
		t.Fatal("тека '.' (вся збірка) має бути відхилена")
	}
	if _, err := safeJoinDir(base, "a/.."); err == nil {
		t.Fatal("'a/..' має бути відхилено")
	}
	if _, err := safeJoinDir(base, "config"); err != nil {
		t.Fatalf("config дозволено: %v", err)
	}
}

func TestCheckDownloadURL(t *testing.T) {
	for _, u := range []string{"https://cdn.modrinth.com/x.jar", "http://127.0.0.1:1234/x", "http://localhost/x"} {
		if err := checkDownloadURL(u); err != nil {
			t.Errorf("%s має бути дозволений: %v", u, err)
		}
	}
	for _, u := range []string{"http://evil.example/x.jar", "ftp://a/b", "file:///etc/passwd", "javascript:1", ""} {
		if err := checkDownloadURL(u); err == nil {
			t.Errorf("%s має бути відхилений", u)
		}
	}
}

func writeZip(t *testing.T, dir, key string, entries map[string]string) {
	t.Helper()
	makeArchiveZips(t, dir, key, entries)
}

func TestExtractRejectsZipSlipAndKeepsOutsideIntact(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(filepath.Dir(dir), "outside-victim.txt")
	_ = os.WriteFile(outside, []byte("keep"), 0644)
	t.Cleanup(func() { os.Remove(outside) })
	writeZip(t, dir, "a.zip", map[string]string{"../outside-victim.txt": "PWNED", "ok.txt": "fine"})

	r := newRunnerForTest(dir)
	err := r.extractOverrideFolder(Task{ManifestKey: "a.zip", DestRelPath: "config", IsArchive: true})
	if err == nil {
		t.Fatal("zip-slip мав бути відхилений")
	}
	if b, _ := os.ReadFile(outside); string(b) != "keep" {
		t.Fatalf("файл поза текою змінено: %q", b)
	}
}

func TestExtractRejectsDangerousDestDir(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "saves", "world.dat")
	_ = os.MkdirAll(filepath.Dir(keep), 0755)
	_ = os.WriteFile(keep, []byte("world"), 0644)
	for _, dest := range []string{".", "..", "../other", "saves/.."} {
		writeZip(t, dir, "b.zip", map[string]string{"x.txt": "1"})
		r := newRunnerForTest(dir)
		if err := r.extractOverrideFolder(Task{ManifestKey: "b.zip", DestRelPath: dest, IsArchive: true}); err == nil {
			t.Errorf("DestRelPath %q мав бути відхилений", dest)
		}
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("дані збірки видалено: %v", err)
	}
}

func TestExtractSkipsExecutablesButInstallsRest(t *testing.T) {
	dir := t.TempDir()
	writeZip(t, dir, "c.zip", map[string]string{"options.txt": "ok", "run.bat": "calc", "tools/x.EXE": "MZ"})
	r := newRunnerForTest(dir)
	if err := r.extractOverrideFolder(Task{ManifestKey: "c.zip", DestRelPath: "config", IsArchive: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "config", "options.txt")); err != nil {
		t.Fatal("звичайний файл має бути розпакований")
	}
	for _, p := range []string{"run.bat", "tools/x.EXE"} {
		if _, err := os.Stat(filepath.Join(dir, "config", filepath.FromSlash(p))); err == nil {
			t.Errorf("%s не мав розпаковуватись", p)
		}
	}
}

func TestVetZipEntriesBomb(t *testing.T) {
	files := make([]*zip.File, maxExtractEntries+1)
	for i := range files {
		files[i] = &zip.File{FileHeader: zip.FileHeader{Name: fmt.Sprintf("f%d", i)}}
	}
	if err := vetZipEntries(files); err == nil {
		t.Fatal("забагато файлів має відхилятись")
	}
	big := []*zip.File{{FileHeader: zip.FileHeader{Name: "big.bin", UncompressedSize64: maxExtractFileBytes + 1}}}
	if err := vetZipEntries(big); err == nil {
		t.Fatal("завеликий файл має відхилятись")
	}
}

func TestDownloadRefusesPathTraversalAndMissingHash(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("data")) }))
	defer srv.Close()
	dir := t.TempDir()
	r := newTestRunner(t, dir, &http.Client{}, 1)
	sum := fmt.Sprintf("%x", sha256.Sum256([]byte("data")))

	if err := r.downloadTask(context.Background(), Task{URL: srv.URL, DestRelPath: "../evil.jar", SHA256: sum}); err == nil {
		t.Error("traversal у DestRelPath мав бути відхилений")
	}
	if err := r.downloadTask(context.Background(), Task{URL: srv.URL, DestRelPath: "mods/nohash.jar"}); err == nil {
		t.Error("файл без хешу мав бути відхилений")
	}
	if _, err := os.Stat(filepath.Join(dir, "mods", "nohash.jar")); err == nil {
		t.Error("файл без хешу з'явився на диску")
	}
	if err := r.downloadTask(context.Background(), Task{URL: srv.URL, DestRelPath: "mods/ok.jar", SHA256: sum, SizeBytes: 4}); err != nil {
		t.Errorf("коректний файл має завантажитись: %v", err)
	}
}

func TestDownloadRejectsOversizedBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("X", 5000)))
	}))
	defer srv.Close()
	dir := t.TempDir()
	r := newTestRunner(t, dir, &http.Client{}, 1)
	task := Task{URL: srv.URL, DestRelPath: "mods/big.jar", SHA256: strings.Repeat("a", 64), SizeBytes: 100}
	if err := r.downloadTask(context.Background(), task); err == nil {
		t.Fatal("тіло більше за заявлений розмір має відхилятись")
	}
	if _, err := os.Stat(filepath.Join(dir, "mods", "big.jar")); err == nil {
		t.Fatal("завеликий файл збережено")
	}
}

func TestRedirectDoesNotLeakToken(t *testing.T) {
	var gotToken string
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("X-Shaurma-Token")
		_, _ = w.Write([]byte("ok"))
	}))
	defer other.Close()
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL, http.StatusFound)
	}))
	defer first.Close()

	// 127.0.0.1:порт1 → 127.0.0.1:порт2 — різні Host, токен має зникнути.
	c := clientWithSafeRedirects(&http.Client{})
	req, _ := http.NewRequest(http.MethodGet, first.URL, nil)
	req.Header.Set("X-Shaurma-Token", "SECRET")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if gotToken != "" {
		t.Fatalf("токен витік на інший хост: %q", gotToken)
	}
}
