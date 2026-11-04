package core

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"shrm-updater-backend/internal/manifest"
	"shrm-updater-backend/internal/newupdate"

	"shaurma-trust"
)

// Наскрізний прогін нової системи: фейковий GitHub (TLS) → перевірка підпису →
// завантаження → Authenticode → install.Apply → стан swapped.
func TestRunNewEndToEnd(t *testing.T) {
	for _, authenticode := range []string{"waived", "required"} {
		t.Run(authenticode, func(t *testing.T) { runE2E(t, authenticode) })
	}
}

func runE2E(t *testing.T, authenticode string) {
	now := time.Now().UTC().Truncate(time.Second)
	rootPub, rootPriv, _ := ed25519.GenerateKey(rand.Reader)
	relPub, relPriv, _ := ed25519.GenerateKey(rand.Reader)

	backend := []byte("NEW BACKEND " + strings.Repeat("x", 100))
	var zbuf bytes.Buffer
	zw := zip.NewWriter(&zbuf)
	w, _ := zw.Create("index.html")
	w.Write([]byte("<html>v2.8.0</html>"))
	zw.Close()
	frontend := zbuf.Bytes()
	sum := func(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
	platform := runtime.GOOS + "-" + runtime.GOARCH
	backendName := backendLibraryName()

	assets := map[string][]byte{"backend.bin": backend, "frontend.zip": frontend}
	m := trust.Manifest{V: 2, Channel: "stable", Version: "2.8.0", Tag: "v2.8.0", Authenticode: authenticode,
		IssuedAt: now.Add(-time.Hour), ExpiresAt: now.Add(30 * 24 * time.Hour), KeyID: "release-1",
		Files: []trust.File{
			{Name: "backend.bin", Platform: platform, Role: trust.FileBackend, Path: backendName, Size: int64(len(backend)), SHA256: sum(backend)},
			{Name: "frontend.zip", Platform: platform, Role: trust.FileFrontend, Path: "frontend.zip", Kind: "archive", ExtractTo: "frontend", Size: int64(len(frontend)), SHA256: sum(frontend)},
		}}
	mraw, _ := json.Marshal(m)
	assets["launcher-manifest.json"] = mraw
	assets["launcher-manifest.json.sig"] = []byte(trust.Sign(relPriv, trust.ContextManifest, mraw))

	srv := httptest.NewTLSServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Date", now.Format(http.TimeFormat))
		if r.URL.Path == "/v1/update/check" {
			json.NewEncoder(rw).Encode(trust.Signal{Version: "2.8.0", Tag: "v2.8.0", Rollout: "now"})
			return
		}
		name := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		if !strings.Contains(r.URL.Path, "/B23101/Shaurma-Launcher/releases/") {
			http.NotFound(rw, r)
			return
		}
		if b, ok := assets[name]; ok {
			rw.Write(b)
			return
		}
		http.NotFound(rw, r)
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)

	ks, _ := json.Marshal(trust.Keyset{V: 1, Version: 1, IssuedAt: now,
		Keys:    []trust.KeyEntry{{KID: "release-1", Role: trust.RoleRelease, Alg: trust.AlgEd25519, Pub: base64.StdEncoding.EncodeToString(relPub)}},
		Sources: trust.Sources{Repo: "B23101/Shaurma-Launcher", SignalURL: srv.URL}})
	prevRoot, prevKS, prevSig := newupdate.RootKeys, newupdate.EmbeddedKeyset, newupdate.EmbeddedKeysetSig
	newupdate.RootKeys = base64.StdEncoding.EncodeToString(rootPub)
	newupdate.EmbeddedKeyset = base64.StdEncoding.EncodeToString(ks)
	newupdate.EmbeddedKeysetSig = trust.Sign(rootPriv, trust.ContextKeyset, ks)
	defer func() { newupdate.RootKeys, newupdate.EmbeddedKeyset, newupdate.EmbeddedKeysetSig = prevRoot, prevKS, prevSig }()
	defer newupdate.SetClockForTest(now)()
	insecure := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} //nolint:gosec // лише тестовий сервер
	defer newupdate.UseTestEndpoints(srv.URL, u.Hostname(), func(c *http.Client) { c.Transport = insecure })()
	prevDT := http.DefaultTransport
	http.DefaultTransport = insecure // download.DownloadWith бере стандартний транспорт
	defer func() { http.DefaultTransport = prevDT }()

	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, launcherExeName(ChannelShaurma)), []byte("shell"), 0o644)
	os.WriteFile(filepath.Join(dir, backendName), []byte("OLD BACKEND"), 0o644)
	manifest.Save(filepath.Join(dir, "launcher-version.json"), &manifest.Manifest{Version: "2.7.27.1"})

	var statuses []string
	err := Run(context.Background(), dir, ChannelShaurma, "stable", true, Callbacks{OnStatus: func(s Status, msg string) { statuses = append(statuses, msg) }})
	if err != nil {
		t.Fatalf("Run: %v (%v)", err, statuses)
	}
	got, _ := os.ReadFile(filepath.Join(dir, backendName))
	local, _ := manifest.Load(filepath.Join(dir, "launcher-version.json"))

	if authenticode == "required" && runtime.GOOS == "windows" {
		// Непідписаний backend.dll не проходить Authenticode — лишаємось на старій версії.
		if string(got) != "OLD BACKEND" || local.Version != "2.7.27.1" {
			t.Fatalf("unsigned update installed: %q %s", got, local.Version)
		}
		if !strings.Contains(strings.Join(statuses, "|"), "підпис") {
			t.Fatalf("no signature message: %v", statuses)
		}
		return
	}
	if !bytes.Equal(got, backend) || local.Version != "2.8.0" {
		t.Fatalf("update not installed: %q %s %v", got, local.Version, statuses)
	}
	if html, _ := os.ReadFile(filepath.Join(dir, "frontend", "index.html")); !strings.Contains(string(html), "v2.8.0") {
		t.Fatal("frontend archive not extracted")
	}
	if prev, _ := os.ReadFile(filepath.Join(dir, backendName+".previous")); string(prev) != "OLD BACKEND" {
		t.Fatal("previous version not kept for rollback")
	}
	var st newupdate.State
	data, _ := os.ReadFile(filepath.Join(dir, newupdate.StateFileName))
	json.Unmarshal(data, &st)
	if st.Phase != newupdate.PhaseSwapped || st.PendingVersion != "2.8.0" || st.HighestSeen != "2.8.0" {
		t.Fatalf("state: %+v", st)
	}
	if _, err := os.Stat(filepath.Join(dir, ".update.lock")); err == nil {
		t.Fatal("lock not released")
	}
}
