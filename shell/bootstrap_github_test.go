package main

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"shaurma-trust"
)

func TestBootstrapFromGitHubRelease(t *testing.T) {
	for _, forged := range []bool{false, true} {
		rootPub, rootPriv, _ := ed25519.GenerateKey(rand.Reader)
		relPub, relPriv, _ := ed25519.GenerateKey(rand.Reader)
		_, evil, _ := ed25519.GenerateKey(rand.Reader)

		var zbuf bytes.Buffer
		zw := zip.NewWriter(&zbuf)
		w, _ := zw.Create("index.html")
		w.Write([]byte("ui"))
		zw.Close()
		assets := map[string][]byte{"backend.bin": []byte("BACKEND"), "updater.bin": []byte("UPDATER"), "frontend.zip": zbuf.Bytes()}
		sum := func(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
		plat := runtime.GOOS + "-" + runtime.GOARCH
		m := trust.Manifest{V: 2, Channel: "stable", Version: "2.8.0", Tag: "v2.8.0", KeyID: "release-1",
			IssuedAt: time.Now().Add(-time.Hour), ExpiresAt: time.Now().Add(time.Hour),
			Files: []trust.File{
				{Name: "backend.bin", Platform: plat, Role: trust.FileBackend, Path: backendLibraryName(), Size: 7, SHA256: sum(assets["backend.bin"])},
				{Name: "updater.bin", Platform: plat, Role: trust.FileUpdater, Path: updaterLibraryName(), Size: 7, SHA256: sum(assets["updater.bin"])},
				{Name: "frontend.zip", Platform: plat, Role: trust.FileFrontend, Path: "frontend.zip", Kind: "archive", ExtractTo: "frontend", Size: int64(zbuf.Len()), SHA256: sum(assets["frontend.zip"])},
			}}
		raw, _ := json.Marshal(m)
		signer := relPriv
		if forged {
			signer = evil
		}
		ks, _ := json.Marshal(trust.Keyset{V: 1, Version: 1, IssuedAt: time.Now(),
			Keys:    []trust.KeyEntry{{KID: "release-1", Role: trust.RoleRelease, Alg: trust.AlgEd25519, Pub: base64.StdEncoding.EncodeToString(relPub)}},
			Sources: trust.Sources{Repo: "B23101/Shaurma-Launcher"}})

		srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			name := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
			switch {
			case strings.HasSuffix(r.URL.Path, "/latest/download/launcher-manifest.json"):
				rw.Write(raw)
			case strings.HasSuffix(r.URL.Path, "/latest/download/launcher-manifest.json.sig"):
				rw.Write([]byte(trust.Sign(signer, trust.ContextManifest, raw)))
			case strings.Contains(r.URL.Path, "/releases/download/v2.8.0/") && assets[name] != nil:
				rw.Write(assets[name])
			default:
				http.NotFound(rw, r)
			}
		}))
		prev := [5]string{rootKeys, embeddedKeyset, embeddedKeysetSig, githubBase, ghReleaseBase}
		rootKeys = base64.StdEncoding.EncodeToString(rootPub)
		embeddedKeyset = base64.StdEncoding.EncodeToString(ks)
		embeddedKeysetSig = trust.Sign(rootPriv, trust.ContextKeyset, ks)
		githubBase, ghReleaseBase = srv.URL, ""
		bootstrapRetryDelay = 0

		got, err := githubManifest()
		if forged {
			if err == nil {
				t.Fatal("forged manifest accepted")
			}
		} else {
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			if err := bootstrapAll(dir, got); err != nil {
				t.Fatal(err)
			}
			if b, _ := os.ReadFile(filepath.Join(dir, backendLibraryName())); string(b) != "BACKEND" {
				t.Fatal("backend not installed")
			}
			if _, err := os.Stat(filepath.Join(dir, "frontend", "index.html")); err != nil {
				t.Fatal("frontend not extracted")
			}
			// Підмінений файл на «GitHub» — відмова встановлення.
			assets["backend.bin"] = []byte("EVILXXX")
			if err := bootstrapAll(t.TempDir(), got); err == nil {
				t.Fatal("tampered component installed")
			}
		}
		srv.Close()
		rootKeys, embeddedKeyset, embeddedKeysetSig, githubBase, ghReleaseBase = prev[0], prev[1], prev[2], prev[3], prev[4]
	}
}
