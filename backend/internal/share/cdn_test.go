package share

import (
	"context"
	"crypto/sha1"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeCDN — TLS-сервер, на який клієнт «ходить» як на cdn.modrinth.com.
type fakeCDN struct {
	srv   *httptest.Server
	mu    sync.Mutex
	files map[string][]byte // шлях URL → вміст (nil → 404)
	hits  map[string]int
}

func newFakeCDN(t *testing.T) *fakeCDN {
	c := &fakeCDN{files: map[string][]byte{}, hits: map[string]int{}}
	c.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.mu.Lock()
		c.hits[r.URL.Path]++
		b, ok := c.files[r.URL.Path]
		c.mu.Unlock()
		if !ok || b == nil {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(b)
	}))
	t.Cleanup(c.srv.Close)
	return c
}

func (c *fakeCDN) client() *http.Client {
	addr := c.srv.Listener.Addr().String()
	return &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, addr)
		},
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // лише тест
	}}
}

func TestCDNDownloadsInParallelAndFallsBack(t *testing.T) {
	root := makeTree(t, 3<<20)
	m := buildM(t, root)
	cdn := newFakeCDN(t)
	var good, wrong, missing int
	for i := range m.Files {
		f := &m.Files[i]
		if !strings.HasPrefix(f.Path, "mods/") {
			continue
		}
		urlPath := "/data/p" + f.Path[len("mods/"):]
		f.Src = &FileSource{Modrinth: "AbCd1234", Version: "v1", URL: "https://" + CDNHost + urlPath}
		raw, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(f.Path)))
		switch {
		case wrong == 0 && len(raw) > 0:
			bad := append([]byte{}, raw...)
			bad[0] ^= 0xff
			cdn.files[urlPath] = bad // CDN віддає ІНШИЙ вміст → має піти через P2P
			wrong++
		case missing == 0:
			missing++ // 404 → P2P
		default:
			cdn.files[urlPath] = raw
			good++
		}
	}
	r := startHost(t, root, m, false)
	k, _ := r.host.AddRecipient("a")
	stage := t.TempDir()
	var last Progress
	var mu sync.Mutex
	d := NewDownloader(Options{Dial: r.dial(false), Key: k, StageDir: stage, CDN: cdn.client(), RetryBase: 5 * time.Millisecond,
		OnProgress: func(p Progress) { mu.Lock(); last = p; mu.Unlock() }})
	if err := d.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	compareTrees(t, m, root, FilesDir(stage))
	mu.Lock()
	defer mu.Unlock()
	if last.CDNFiles != good {
		t.Fatalf("з CDN мало прийти %d файлів, прийшло %d", good, last.CDNFiles)
	}
	// Відправник віддав лише те, чого нема на CDN (+ відкати), а не всю збірку.
	if sent := stateOf(r.host, "a").BytesSent; sent >= m.TotalBytes {
		t.Fatalf("P2P віддав %d з %d — CDN не використано", sent, m.TotalBytes)
	}
}

func TestNoCDNUsesOnlyP2P(t *testing.T) {
	root := makeTree(t, 1000)
	m := buildM(t, root)
	cdn := newFakeCDN(t)
	for i := range m.Files {
		if strings.HasPrefix(m.Files[i].Path, "mods/") {
			m.Files[i].Src = &FileSource{Modrinth: "AbCd1234", URL: "https://" + CDNHost + "/x/" + m.Files[i].Path}
		}
	}
	r := startHost(t, root, m, false)
	k, _ := r.host.AddRecipient("a")
	stage := t.TempDir()
	if err := NewDownloader(Options{Dial: r.dial(false), Key: k, StageDir: stage, CDN: cdn.client(), NoCDN: true}).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	compareTrees(t, m, root, FilesDir(stage))
	if len(cdn.hits) != 0 {
		t.Fatalf("NoCDN, а запити на CDN були: %v", cdn.hits)
	}
}

func TestManifestSourceValidation(t *testing.T) {
	root := makeTree(t, 1000)
	m := buildM(t, root)
	id := m.ID()
	m.Files[0].Src = &FileSource{Modrinth: "AbCd1234", URL: "https://cdn.modrinth.com/data/a.jar"}
	if err := m.Validate(); err != nil {
		t.Fatalf("коректне джерело: %v", err)
	}
	if m.ID() != id {
		t.Fatal("src не має впливати на ID вмісту")
	}
	for _, bad := range []FileSource{
		{Modrinth: "AbCd1234", URL: "https://evil.example/a.jar"},
		{Modrinth: "AbCd1234", URL: "http://cdn.modrinth.com/a.jar"},
		{Modrinth: "AbCd1234", URL: "https://cdn.modrinth.com:8443/a.jar"},
		{Modrinth: "AbCd1234", URL: "https://user@cdn.modrinth.com/a.jar"},
		{Modrinth: "../x", URL: "https://cdn.modrinth.com/a.jar"},
		{Modrinth: "AbCd1234", Version: "v/1", URL: "https://cdn.modrinth.com/a.jar"},
	} {
		b := bad
		m.Files[0].Src = &b
		if err := m.Validate(); err == nil {
			t.Errorf("мало відхилитись: %+v", bad)
		}
	}
}

func TestPreviewLeavesRecipientUntouched(t *testing.T) {
	root := makeTree(t, 1000)
	m := buildM(t, root)
	r := startHost(t, root, m, false)
	r.host.SetTimeouts(Timeouts{Accept: time.Minute, OfflineAfter: time.Second, Offline: time.Hour})
	k, _ := r.host.AddRecipient("a")
	pm, err := NewDownloader(Options{Dial: r.dial(false), Key: k, StageDir: t.TempDir(), ExpectManifestID: m.ID()}).Preview(context.Background())
	if err != nil || len(pm.Files) != len(m.Files) {
		t.Fatalf("preview: %v", err)
	}
	if s := stateOf(r.host, "a"); s.State != StateWaiting || s.LastSeenMs != 0 {
		t.Fatalf("перегляд не має змінювати стан: %+v", s)
	}
	r.host.Sweep(time.Now().Add(2 * time.Minute))
	if s := stateOf(r.host, "a"); s.State != StateTimeout || s.Reason != ReasonTimeout {
		t.Fatalf("тайм-аут прийняття має лишитись тайм-аутом прийняття: %+v", s)
	}
	// Відмовився → і перегляд закрито.
	r2 := startHost(t, root, m, false)
	k2, _ := r2.host.AddRecipient("b")
	r2.host.ApplyControl("b", CtlDeclined, "")
	if _, err := NewDownloader(Options{Dial: r2.dial(false), Key: k2, StageDir: t.TempDir()}).Preview(context.Background()); ReasonOf(err) != ReasonDeclined {
		t.Fatalf("після відмови: %v", err)
	}
}

func TestPreviewOldSender(t *testing.T) {
	old := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(old.Close)
	addr := old.Listener.Addr().String()
	dial := func(ctx context.Context) (net.Conn, error) { var d net.Dialer; return d.DialContext(ctx, "tcp", addr) }
	_, err := NewDownloader(Options{Dial: dial, Key: strings.Repeat("a", 32), StageDir: t.TempDir()}).Preview(context.Background())
	if !errors.Is(err, ErrPreviewUnsupported) {
		t.Fatalf("старий відправник: %v", err)
	}
}

func TestBuildManifestSHA1(t *testing.T) {
	root := t.TempDir()
	writeRand(t, filepath.Join(root, "mods", "a.jar"), 5000)
	writeRand(t, filepath.Join(root, "config", "b.toml"), 50)
	var mu sync.Mutex
	got := map[string]string{}
	_, _, err := BuildManifest(context.Background(), root, KindBuild, "x", nil, BuildOptions{
		WantSHA1: func(rel string) bool { return strings.HasPrefix(rel, "mods/") },
		OnSHA1:   func(rel, s string) { mu.Lock(); got[rel] = s; mu.Unlock() },
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(root, "mods", "a.jar"))
	want := sha1.Sum(raw)
	if len(got) != 1 || got["mods/a.jar"] != hex.EncodeToString(want[:]) {
		t.Fatalf("sha1: %v", got)
	}
}
