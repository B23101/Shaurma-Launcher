package java

import (
	"crypto/sha1"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type runtimeTransport func(*http.Request) (*http.Response, error)

func (f runtimeTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func runtimeTestFile(data string) mojangPackageFile {
	var f mojangPackageFile
	f.Type = "file"
	f.Downloads.Raw.URL = "https://piston-data.mojang.com/test"
	h := sha1.Sum([]byte(data))
	f.Downloads.Raw.SHA1 = hex.EncodeToString(h[:])
	f.Downloads.Raw.Size = int64(len(data))
	return f
}
func runtimeTestInstaller(body string) *Installer {
	return &Installer{cancelRequested: &atomic.Bool{}, client: &http.Client{Transport: runtimeTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
	})}}
}

func TestRuntimeRejectsCorruptionWithoutReplacingExistingFile(t *testing.T) {
	for _, body := range []string{"tampered", "expected-extra", "short"} {
		dir := t.TempDir()
		os.MkdirAll(filepath.Join(dir, "bin"), 0755)
		dest := filepath.Join(dir, "bin", "java.exe")
		os.WriteFile(dest, []byte("original"), 0644)
		pkg := &mojangPackageManifest{Files: map[string]mojangPackageFile{"bin/java.exe": runtimeTestFile("expected")}}
		if err := runtimeTestInstaller(body).downloadPackageFiles(pkg, dir, 21); err == nil {
			t.Fatal("corrupt runtime accepted")
		}
		got, _ := os.ReadFile(dest)
		if string(got) != "original" {
			t.Fatalf("existing runtime replaced: %q", got)
		}
	}
}
func TestRuntimeVerifiedDownload(t *testing.T) {
	dir := t.TempDir()
	pkg := &mojangPackageManifest{Files: map[string]mojangPackageFile{"bin/java.exe": runtimeTestFile("expected")}}
	if err := runtimeTestInstaller("expected").downloadPackageFiles(pkg, dir, 21); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "bin", "java.exe"))
	if string(got) != "expected" {
		t.Fatalf("runtime bytes: %q", got)
	}
}
func TestRuntimeRejectsUnsafePathsBeforeWriting(t *testing.T) {
	for _, rel := range []string{"../escape", "bin/../../escape", "C:/escape", "bin\\..\\escape", "/escape"} {
		dir := t.TempDir()
		pkg := &mojangPackageManifest{Files: map[string]mojangPackageFile{rel: runtimeTestFile("data")}}
		if runtimeTestInstaller("data").downloadPackageFiles(pkg, dir, 21) == nil {
			t.Errorf("unsafe path accepted: %s", rel)
		}
		entries, _ := os.ReadDir(dir)
		if len(entries) != 0 {
			t.Fatal("wrote before validation")
		}
	}
}
func TestRuntimeRejectsEscapingLink(t *testing.T) {
	pkg := &mojangPackageManifest{Files: map[string]mojangPackageFile{"bin/java": {Type: "link", Target: "../../outside"}}}
	if validateRuntimePackage(pkg) == nil {
		t.Fatal("escaping link accepted")
	}
	pkg.Files["bin/java"] = mojangPackageFile{Type: "link", Target: "../lib/java"}
	if err := validateRuntimePackage(pkg); err != nil {
		t.Fatalf("internal relative link rejected: %v", err)
	}
}
func TestRuntimeOriginAndManifestIntegrity(t *testing.T) {
	for _, raw := range []string{"http://piston-data.mojang.com/file", "https://piston-data.mojang.com.evil.test/file", "https://piston-data.mojang.com@evil.test/file"} {
		if runtimeURL(raw) == nil {
			t.Errorf("untrusted origin: %s", raw)
		}
	}
	j := runtimeTestInstaller("tampered")
	h := sha1.Sum([]byte("expected"))
	if _, err := j.runtimeBytes("https://piston-meta.mojang.com/test", hex.EncodeToString(h[:]), 1024); err == nil {
		t.Fatal("tampered manifest accepted")
	}
	req, _ := http.NewRequest(http.MethodGet, "https://evil.test/file", nil)
	if j.runtimeClient().CheckRedirect(req, nil) == nil {
		t.Fatal("untrusted redirect accepted")
	}
}

// Explicit opt-in: default CI remains independent of external service availability.
func TestRuntimeLiveMetadata(t *testing.T) {
	if os.Getenv("SHAURMA_TEST_LIVE_JAVA") != "1" {
		t.Skip("set SHAURMA_TEST_LIVE_JAVA=1 to verify official Mojang metadata")
	}
	j := NewInstaller("")
	j.client.Timeout = 20 * time.Second
	for _, component := range []string{"jre-legacy", "java-runtime-gamma", "java-runtime-delta", "java-runtime-epsilon"} {
		t.Run(component, func(t *testing.T) {
			raw, hash, err := j.resolvePackageManifestURL(component)
			if err != nil {
				t.Fatal(err)
			}
			pkg, err := j.fetchPackageManifest(raw, hash)
			if err != nil {
				t.Fatal(err)
			}
			if err := validateRuntimePackage(pkg); err != nil {
				t.Fatal(err)
			}
			root, err := os.OpenRoot(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			for rel, f := range pkg.Files {
				if f.Type == "file" && f.Downloads.Raw.Size < 65536 {
					rel = filepath.FromSlash(rel)
					if err := root.MkdirAll(filepath.Dir(rel), 0755); err != nil {
						t.Fatal(err)
					}
					if err := j.downloadRuntimeFile(root, rel, f); err != nil {
						t.Fatal(err)
					}
					return
				}
			}
			t.Fatal("no small sample file in runtime")
		})
	}
}
