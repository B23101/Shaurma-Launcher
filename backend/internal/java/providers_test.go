package java

import (
	"archive/zip"
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArchivePathRejectsTraversal(t *testing.T) {
	bad := []string{"../evil", "a/../../evil", "/abs", "C:/win", "a\\..\\..\\x", "dir/ trailing. /x", "a\x00b", ""}
	for _, n := range bad {
		if _, ok := archivePath(n); ok {
			t.Errorf("archivePath(%q) accepted", n)
		}
	}
	good := map[string]string{"jdk-21/bin/java.exe": "jdk-21/bin/java.exe", "./jre/lib/x": "jre/lib/x"}
	for in, want := range good {
		if got, ok := archivePath(in); !ok || got != want {
			t.Errorf("archivePath(%q) = %q,%v want %q", in, got, ok, want)
		}
	}
}

func writeZip(t *testing.T, p string, files map[string]string) {
	t.Helper()
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	zw.Close()
	f.Close()
}

func TestExtractZipSafeBlocksZipSlip(t *testing.T) {
	dir := t.TempDir()
	zp := filepath.Join(dir, "a.zip")
	writeZip(t, zp, map[string]string{"../../escape.txt": "x"})
	dst := filepath.Join(dir, "out")
	os.MkdirAll(dst, 0755)
	if err := extractZipSafe(zp, dst, func() bool { return false }); err == nil {
		t.Fatal("zip slip not blocked")
	}
	if _, err := os.Stat(filepath.Join(dir, "escape.txt")); err == nil {
		t.Fatal("file escaped the target dir")
	}
}

func TestExtractZipAndFindHome(t *testing.T) {
	dir := t.TempDir()
	zp := filepath.Join(dir, "a.zip")
	writeZip(t, zp, map[string]string{"jdk-21.0.5+11-jre/bin/" + exeName(): "bin", "jdk-21.0.5+11-jre/release": "JAVA_VERSION=\"21\""})
	dst := filepath.Join(dir, "out")
	os.MkdirAll(dst, 0755)
	if err := extractZipSafe(zp, dst, func() bool { return false }); err != nil {
		t.Fatal(err)
	}
	home, err := findJavaHome(dst)
	if err != nil || filepath.Base(home) != "jdk-21.0.5+11-jre" {
		t.Fatalf("home=%q err=%v", home, err)
	}
}

func TestDownloadVendorChecksumMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("tampered")) }))
	defer srv.Close()
	j := NewInstaller(t.TempDir())
	j.client = srv.Client()
	good := sha256.Sum256([]byte("original"))
	err := j.downloadVendor(vendorPackage{URL: srv.URL}, good[:], filepath.Join(t.TempDir(), "x"), 21, "test")
	if err == nil || !strings.Contains(err.Error(), "контрольна сума") {
		t.Fatalf("expected checksum error, got %v", err)
	}
}

func TestVendorURLAllowlist(t *testing.T) {
	for _, u := range []string{"http://api.adoptium.net/x", "https://evil.com/x", "https://user@cdn.azul.com/x", "https://cdn.azul.com:8443/x"} {
		if vendorURL(u) == nil {
			t.Errorf("vendorURL(%q) accepted", u)
		}
	}
	for _, u := range []string{"https://api.adoptium.net/v3/x", "https://cdn.azul.com/zulu/bin/x.zip", "https://github.com/adoptium/x"} {
		if err := vendorURL(u); err != nil {
			t.Errorf("vendorURL(%q): %v", u, err)
		}
	}
}

func TestMajorFromVersionAndCompat(t *testing.T) {
	cases := map[string]int{"1.8.0_402": 8, "17.0.10": 17, "21": 21, "25-ea": 25, "": 0, "abc": 0}
	for v, want := range cases {
		if got := majorFromVersion(v); got != want {
			t.Errorf("majorFromVersion(%q)=%d want %d", v, got, want)
		}
	}
	if Compat(17, 21) != CompatOld || Compat(21, 21) != CompatOK || Compat(21, 17) != CompatNewer || Compat(17, 8) != CompatLegacy {
		t.Error("Compat rules broken")
	}
}

func TestParseProps(t *testing.T) {
	out := "Property settings:\n    java.version = 21.0.5\n    java.vendor = Eclipse Adoptium\n    os.arch = amd64\n    sun.arch.data.model = 64\n\nopenjdk version \"21.0.5\""
	p := parseProps(out)
	if p["java.version"] != "21.0.5" || p["java.vendor"] != "Eclipse Adoptium" || p["sun.arch.data.model"] != "64" {
		t.Fatalf("props: %#v", p)
	}
}

func TestInspectRejectsNonJava(t *testing.T) {
	p := filepath.Join(t.TempDir(), "notepad.exe")
	os.WriteFile(p, []byte("x"), 0755)
	if _, err := Inspect(p); err == nil {
		t.Fatal("non-java file accepted")
	}
}
