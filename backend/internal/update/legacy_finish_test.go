package update

import (
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
	"testing"
)

type legacyFixture struct {
	dir, exe string
	newDLL   []byte
	newExe   []byte
}

func setupLegacy(t *testing.T, signer ed25519.PrivateKey, trusted ed25519.PublicKey, tamper bool) legacyFixture {
	t.Helper()
	oldTok, oldBase, oldKeys := token, legacyFinishBase, legacyManifestKeys
	t.Cleanup(func() { token, legacyFinishBase, legacyManifestKeys = oldTok, oldBase, oldKeys })
	token = "test-token-0123456789abcdef"
	legacyManifestKeys = base64.StdEncoding.EncodeToString(trusted)

	f := legacyFixture{dir: t.TempDir(), newDLL: []byte("NEW UPDATER"), newExe: []byte("NEW SHELL")}
	f.exe = filepath.Join(f.dir, "ShaurmLauncher.exe")
	lib := filepath.Join(f.dir, updaterLibName())
	os.WriteFile(lib, []byte("OLD UPDATER"), 0o644)
	os.WriteFile(f.exe, []byte("OLD SHELL"), 0o755)
	pendingDLL := f.newDLL
	if tamper {
		pendingDLL = []byte("EVIL UPDATER")
	}
	os.WriteFile(lib+".new", pendingDLL, 0o644)
	os.WriteFile(f.exe+".new", f.newExe, 0o755)

	sum := func(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
	manifest, _ := json.Marshal(map[string]any{"version": "2.7.29", "components": []map[string]any{
		{"path": updaterLibName(), "sha256": sum(f.newDLL), "size": len(f.newDLL)},
		{"path": "ShaurmLauncher.exe", "sha256": sum(f.newExe), "size": len(f.newExe)},
	}})
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(signer, append([]byte(legacyManifestContext), manifest...)))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Shaurma-Token") == "" {
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case "/launcher/" + platformSeg() + "launcher-version.json":
			w.Write(manifest)
		case "/launcher/" + platformSeg() + "launcher-version.json.sig":
			w.Write([]byte(sig))
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	legacyFinishBase = srv.URL
	return f
}

func read(t *testing.T, p string) string { b, _ := os.ReadFile(p); return string(b) }

func TestFinishLegacyPendingAppliesSignedComponents(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	f := setupLegacy(t, priv, pub, false)
	res, err := FinishLegacyPending(f.dir, f.exe)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Applied) != 2 {
		t.Fatalf("applied %v rejected %v", res.Applied, res.Rejected)
	}
	lib := filepath.Join(f.dir, updaterLibName())
	if read(t, lib) != "NEW UPDATER" || read(t, lib+".previous") != "OLD UPDATER" {
		t.Fatal("updater not swapped")
	}
	if read(t, f.exe) != "NEW SHELL" || read(t, f.exe+"~") != "OLD SHELL" {
		t.Fatal("shell not swapped")
	}
	if _, err := os.Stat(lib + ".new"); !os.IsNotExist(err) {
		t.Fatal(".new must be consumed")
	}
}

func TestFinishLegacyPendingRejectsWrongKeyAndTampering(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	otherPub, _, _ := ed25519.GenerateKey(rand.Reader)

	f := setupLegacy(t, priv, otherPub, false)
	if _, err := FinishLegacyPending(f.dir, f.exe); err == nil {
		t.Fatal("manifest signed by an untrusted key must be refused")
	}
	if read(t, f.exe) != "OLD SHELL" || read(t, filepath.Join(f.dir, updaterLibName())) != "OLD UPDATER" {
		t.Fatal("nothing may change without a trusted signature")
	}

	f = setupLegacy(t, priv, pub, true)
	res, err := FinishLegacyPending(f.dir, f.exe)
	if err != nil {
		t.Fatal(err)
	}
	if _, bad := res.Rejected[updaterLibName()]; !bad || read(t, filepath.Join(f.dir, updaterLibName())) != "OLD UPDATER" {
		t.Fatalf("tampered updater must stay pending: %+v", res)
	}
	if read(t, f.exe) != "NEW SHELL" {
		t.Fatal("valid shell should still be applied")
	}
}

func TestFinishLegacyPendingNoTokenOrNothingPending(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	f := setupLegacy(t, priv, pub, false)
	token = ""
	if res, _ := FinishLegacyPending(f.dir, f.exe); len(res.Applied) != 0 || read(t, f.exe) != "OLD SHELL" {
		t.Fatal("single launcher (no token) must not touch files")
	}
	token = "test-token-0123456789abcdef"
	os.Remove(f.exe + ".new")
	os.Remove(filepath.Join(f.dir, updaterLibName()+".new"))
	if res, err := FinishLegacyPending(f.dir, f.exe); err != nil || len(res.Applied) != 0 {
		t.Fatalf("nothing pending: %v %v", res, err)
	}
}

// Справжній ключ підпису Pack Manager підтверджує вже опублікований маніфест
// (резервна копія 2.7.27.1): вшитий ключ — правильний.
func TestLegacyKeyMatchesPublishedManifest(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "releases-frozen", "pre-migration-current", "app-shaurma")
	payload, err := os.ReadFile(filepath.Join(dir, "launcher-version.publish.json"))
	if err != nil {
		t.Skip("резервної копії немає (CI)")
	}
	sig, _ := os.ReadFile(filepath.Join(dir, "launcher-version.publish.json.sig"))
	if err := verifyLegacySig(payload, string(sig)); err != nil {
		t.Fatal(err)
	}
}
