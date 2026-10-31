package sync

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type securityTransport func(*http.Request) (*http.Response, error)

func (f securityTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestManifestRedirectCredentials(t *testing.T) {
	for _, dest := range []string{"https://packs.test/final", "https://other.test/final", "http://packs.test/final"} {
		t.Run(dest, func(t *testing.T) {
			c := NewManifestClient("https://packs.test", "test-only-placeholder")
			c.SetAccountsProvider(func() []AccountCred { return []AccountCred{{Nick: "TestPlayer"}} })
			calls := 0
			c.Client.Transport = securityTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return &http.Response{StatusCode: 302, Header: http.Header{"Location": {dest}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
				}
				want := strings.HasPrefix(dest, "https://packs.test/")
				for _, key := range []string{"X-Shaurma-Token", "X-Shaurma-Auth"} {
					if (r.Header.Get(key) != "") != want {
						t.Errorf("unexpected %s forwarding", key)
					}
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"version":1,"packs":[]}`)), Request: r}, nil
			})
			_, err := c.FetchPacksIndex()
			if strings.HasPrefix(dest, "http:") {
				if err == nil || calls != 1 {
					t.Fatal("HTTPS downgrade accepted")
				}
			} else if err != nil || calls != 2 {
				t.Fatalf("normal redirect failed: %v", err)
			}
		})
	}
}

func linkDirectory(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err == nil {
		return
	}
	if runtime.GOOS == "windows" {
		// Junctions do not require Windows Developer Mode. Both paths belong
		// to this test's temporary directories; no launcher code runs a shell.
		if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
			t.Fatalf("create test junction: %v: %s", err, out)
		}
		return
	}
	t.Fatal("cannot create test symlink")
}

func TestSyncDoesNotWriteOrDeleteThroughExternalDirectory(t *testing.T) {
	dir, outside := t.TempDir(), t.TempDir()
	linkDirectory(t, outside, filepath.Join(dir, "mods"))
	sentinel := filepath.Join(outside, "keep.jar")
	if err := os.WriteFile(sentinel, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	r := newTestRunner(t, dir, &http.Client{}, 1)
	sum := fmt.Sprintf("%x", sha256.Sum256([]byte("replacement")))
	resp := &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("replacement"))}
	err := r.writeDownloadedBody(context.Background(), resp, filepath.Join(dir, "mods", "new.jar.part"), filepath.Join(dir, "mods", "new.jar"), Task{SHA256: sum, SizeBytes: 11}, 0)
	if err == nil {
		t.Fatal("write escaped installation")
	}
	r.trackPartial(filepath.Join(dir, "mods", "keep.jar"))
	r.cleanupPartials()
	r.removeOrphans([]string{"mods/keep.jar"})
	if b, err := os.ReadFile(sentinel); err != nil || string(b) != "original" {
		t.Fatalf("outside file changed: %q %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(outside, "new.jar.part")); !os.IsNotExist(err) {
		t.Fatal("outside partial created")
	}
	makeArchiveZips(t, dir, "test", map[string]string{"new.txt": "new"})
	if err := r.extractOverrideFolder(Task{DestRelPath: "mods/subdir", ManifestKey: "test"}); err == nil {
		t.Fatal("extraction escaped installation")
	}
	if _, err := os.Stat(filepath.Join(outside, "subdir")); !os.IsNotExist(err) {
		t.Fatal("outside directory created")
	}
}

func TestSyncResumeAndFreshInstall(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusPartialContent} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			dir := t.TempDir()
			r := newTestRunner(t, dir, &http.Client{}, 1)
			dest := filepath.Join(dir, "test.jar")
			if err := os.WriteFile(dest+".part", []byte("hello"), 0600); err != nil {
				t.Fatal(err)
			}
			body := "hello world"
			if status == http.StatusPartialContent {
				body = " world"
			}
			resp := &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}
			task := Task{SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("hello world"))), SizeBytes: 11}
			if err := r.writeDownloadedBody(context.Background(), resp, dest+".part", dest, task, 5); err != nil {
				t.Fatal(err)
			}
			if b, err := os.ReadFile(dest); err != nil || string(b) != "hello world" {
				t.Fatalf("bad resumed file: %q %v", b, err)
			}
		})
	}
	dir := filepath.Join(t.TempDir(), "new installation")
	client := &http.Client{Transport: securityTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ok")), Header: make(http.Header), Request: r}, nil
	})}
	r := newTestRunner(t, dir, client, 1)
	if err := r.downloadTask(context.Background(), Task{URL: "https://packs.test/file", DestRelPath: "mods/test.jar", SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("ok"))), SizeBytes: 2}); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "mods/test.jar")); err != nil || string(b) != "ok" {
		t.Fatalf("fresh installation failed: %v", err)
	}
}
