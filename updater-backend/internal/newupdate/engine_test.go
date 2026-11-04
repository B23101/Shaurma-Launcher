package newupdate

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"shrm-updater-backend/internal/install"
	"shrm-updater-backend/internal/manifest"

	"shaurma-trust"
)

var testNow = time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)

type world struct {
	t        *testing.T
	rootPriv ed25519.PrivateKey
	relPriv  ed25519.PrivateKey
	assets   map[string][]byte // "<tag>/<name>" або "latest/<name>"
	signal   *trust.Signal
	signalUp bool
	redirect string // якщо задано — ассети перенаправляються на цей URL
	srv      *httptest.Server
	dir      string
	reports  []string
}

func key(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func newWorld(t *testing.T) *world {
	t.Helper()
	w := &world{t: t, assets: map[string][]byte{}, signalUp: true, dir: t.TempDir()}
	rootPub, rootPriv := key(t)
	relPub, relPriv := key(t)
	w.rootPriv, w.relPriv = rootPriv, relPriv

	w.srv = httptest.NewTLSServer(http.HandlerFunc(w.serve))
	t.Cleanup(w.srv.Close)
	host, _ := url.Parse(w.srv.URL)

	ks := trust.Keyset{V: 1, Version: 1, IssuedAt: testNow,
		Keys:    []trust.KeyEntry{{KID: "release-1", Role: trust.RoleRelease, Alg: trust.AlgEd25519, Pub: base64.StdEncoding.EncodeToString(relPub)}},
		Sources: trust.Sources{Repo: "B23101/Shaurma-Launcher", SignalURL: w.srv.URL, AssetHosts: []string{"release-assets.githubusercontent.com"}},
	}
	ksRaw, _ := json.Marshal(ks)

	oldRoot, oldKS, oldSig, oldNow, oldHook, oldCert := RootKeys, EmbeddedKeyset, EmbeddedKeysetSig, now, testSourceHook, EmbeddedCert
	t.Cleanup(func() {
		RootKeys, EmbeddedKeyset, EmbeddedKeysetSig, now, testSourceHook, EmbeddedCert = oldRoot, oldKS, oldSig, oldNow, oldHook, oldCert
	})
	RootKeys = base64.StdEncoding.EncodeToString(rootPub)
	EmbeddedKeyset = base64.StdEncoding.EncodeToString(ksRaw)
	EmbeddedKeysetSig = trust.Sign(rootPriv, trust.ContextKeyset, ksRaw)
	EmbeddedCert = ""
	now = func() time.Time { return testNow }
	testSourceHook = func(s *source) {
		s.githubBase = w.srv.URL
		testHost := host.Hostname()
		allow := s.allowHost
		s.allowHost = func(h string) bool { return h == testHost || allow(h) }
		c := s.httpClient(5 * time.Second)
		c.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} //nolint:gosec // лише фейковий сервер тесту
		s.client = c
	}
	return w
}

func (w *world) serve(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Date", testNow.Format(http.TimeFormat))
	switch {
	case r.URL.Path == "/v1/update/check":
		if !w.signalUp || w.signal == nil {
			http.Error(rw, "down", 503)
			return
		}
		json.NewEncoder(rw).Encode(w.signal)
		return
	case r.URL.Path == "/v1/update/report":
		w.reports = append(w.reports, r.URL.Path)
		return
	}
	prefix := "/B23101/Shaurma-Launcher/releases/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		http.NotFound(rw, r)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, prefix)
	rest = strings.TrimPrefix(rest, "download/")
	rest = strings.Replace(rest, "latest/download/", "latest/", 1)
	if w.redirect != "" {
		http.Redirect(rw, r, w.redirect, http.StatusFound)
		return
	}
	data, ok := w.assets[rest]
	if !ok {
		http.NotFound(rw, r)
		return
	}
	rw.Write(data)
}

// publish кладе в «реліз» маніфест (і в latest), підписаний release-ключем.
func (w *world) publish(version string, mut func(*trust.Manifest), signWith ed25519.PrivateKey) {
	tag := "v" + version
	m := trust.Manifest{V: 2, Channel: "stable", Version: version, Tag: tag,
		IssuedAt: testNow.Add(-time.Hour), ExpiresAt: testNow.Add(90 * 24 * time.Hour), KeyID: "release-1",
		Files: []trust.File{
			{Name: "ShaurmLauncher.exe", Platform: Platform(), Role: trust.FileShell, Path: "ShaurmLauncher.exe", Size: 3, SHA256: strings.Repeat("1", 64)},
			{Name: "backend.dll", Platform: Platform(), Role: trust.FileBackend, Path: "backend.dll", Size: 3, SHA256: strings.Repeat("2", 64)},
		}}
	if mut != nil {
		mut(&m)
	}
	if signWith == nil {
		signWith = w.relPriv
	}
	raw, _ := json.Marshal(m)
	sig := trust.Sign(signWith, trust.ContextManifest, raw)
	for _, p := range []string{tag, "latest"} {
		w.assets[p+"/"+trust.ManifestAsset] = raw
		w.assets[p+"/"+trust.ManifestAsset+".sig"] = []byte(sig)
	}
}

func (w *world) open(current string) *Engine {
	w.t.Helper()
	e, err := Open(Options{InstallDir: w.dir, Channel: "stable", Edition: "shaurma", ShellName: "ShaurmLauncherBase.exe", CurrentVersion: current})
	if err != nil {
		w.t.Fatal(err)
	}
	w.t.Cleanup(e.Close)
	return e
}

func TestSignalThenGitHub(t *testing.T) {
	w := newWorld(t)
	w.publish("2.8.0", nil, nil)
	w.signal = &trust.Signal{Version: "2.8.0", Tag: "v2.8.0", Rollout: "now"}
	p, err := w.open("2.7.27.1").Check(context.Background())
	if err != nil || p == nil {
		t.Fatalf("plan: %v %v", p, err)
	}
	if !strings.HasSuffix(p.BaseURL, "/B23101/Shaurma-Launcher/releases/download/v2.8.0") {
		t.Fatalf("base url %s", p.BaseURL)
	}
	// Роль shell ставиться під іменем поточного exe (стара Base-установка).
	if p.Remote.Components[0].Path != "ShaurmLauncherBase.exe" || p.Remote.Components[0].URL != "/ShaurmLauncher.exe" {
		t.Fatalf("shell mapping %+v", p.Remote.Components[0])
	}
	if p.Remote.Proof == nil || p.Remote.Proof.Format != 2 || len(p.Remote.Proof.Keyset) == 0 {
		t.Fatal("v2 proof missing")
	}
}

func TestWorkerDownFallsBackToGitHub(t *testing.T) {
	w := newWorld(t)
	w.publish("2.8.0", nil, nil)
	w.signalUp = false
	p, err := w.open("2.7.0").Check(context.Background())
	if err != nil || p == nil || p.Manifest.Version != "2.8.0" {
		t.Fatalf("fallback: %v %v", p, err)
	}
}

func TestRejections(t *testing.T) {
	_, evil := key(t)
	cases := map[string]struct {
		setup func(w *world)
		want  error
	}{
		"signal mismatch": {func(w *world) {
			w.publish("2.8.0", nil, nil)
			w.signal = &trust.Signal{Version: "2.9.0", Tag: "v2.8.0"}
		}, trust.ErrSignalMismatch},
		"forged": {func(w *world) { w.publish("2.8.0", nil, evil); w.signalUp = false }, trust.ErrBadSignature},
		"expired": {func(w *world) {
			w.publish("2.8.0", func(m *trust.Manifest) { m.ExpiresAt = testNow.Add(-time.Minute); m.IssuedAt = testNow.Add(-time.Hour) }, nil)
			w.signalUp = false
		}, trust.ErrExpired},
		"foreign redirect": {func(w *world) {
			w.publish("2.8.0", nil, nil)
			w.signalUp = false
			w.redirect = "https://evil.example.com/x"
		}, nil},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			c.setup(w)
			p, err := w.open("2.7.0").Check(context.Background())
			if p != nil {
				t.Fatalf("plan accepted: %+v", p.Manifest)
			}
			if err == nil {
				t.Fatal("no error")
			}
			if c.want != nil && !errors.Is(err, c.want) {
				t.Fatalf("got %v want %v", err, c.want)
			}
		})
	}
}

func TestAntiRollback(t *testing.T) {
	w := newWorld(t)
	w.publish("2.8.0", nil, nil)
	w.signalUp = false
	s := &State{HighestSeen: "2.9.0"}
	saveState(w.dir, s)
	if p, err := w.open("2.7.0").Check(context.Background()); p != nil || !errors.Is(err, trust.ErrRollback) {
		t.Fatalf("rollback accepted: %v %v", p, err)
	}
}

func TestRolloutLaterAndSecurity(t *testing.T) {
	w := newWorld(t)
	w.publish("2.8.0", nil, nil)
	w.signal = &trust.Signal{Version: "2.8.0", Tag: "v2.8.0", Rollout: "later"}
	if _, err := w.open("2.7.0").Check(context.Background()); !errors.Is(err, ErrDeferred) {
		t.Fatalf("later not deferred: %v", err)
	}
	w2 := newWorld(t)
	w2.publish("2.8.0", func(m *trust.Manifest) { m.Security = true }, nil)
	w2.signal = &trust.Signal{Version: "2.8.0", Tag: "v2.8.0", Rollout: "pause"}
	if p, err := w2.open("2.7.0").Check(context.Background()); err != nil || p == nil {
		t.Fatalf("security update blocked: %v", err)
	}
}

func TestKeysetRotation(t *testing.T) {
	w := newWorld(t)
	newPub, newPriv := key(t)
	ks := trust.Keyset{V: 1, Version: 2, IssuedAt: testNow,
		Keys:    []trust.KeyEntry{{KID: "release-2", Role: trust.RoleRelease, Alg: trust.AlgEd25519, Pub: base64.StdEncoding.EncodeToString(newPub)}},
		Revoked: []string{"release-1"},
		Sources: trust.Sources{Repo: "B23101/Shaurma-Launcher", SignalURL: w.srv.URL},
	}
	raw, _ := json.Marshal(ks)
	w.assets["latest/keyset.json"] = raw
	w.assets["latest/keyset.json.sig"] = []byte(trust.Sign(w.rootPriv, trust.ContextKeyset, raw))
	w.signalUp = false
	w.publish("2.8.0", func(m *trust.Manifest) { m.KeyID = "release-2" }, newPriv)
	p, err := w.open("2.7.0").Check(context.Background())
	if err != nil || p == nil {
		t.Fatalf("rotated key not accepted: %v", err)
	}
	if loadState(w.dir).KeysetVersion != 2 {
		t.Fatal("new keyset not persisted")
	}
	// Тепер старий release-1 відкликано: маніфест, підписаний ним, не пройде.
	w.publish("2.9.0", nil, nil)
	if p, err := w.open("2.7.0").Check(context.Background()); p != nil || err == nil {
		t.Fatal("revoked key still accepted")
	}
}

func TestCertGate(t *testing.T) {
	w := newWorld(t)
	cert, err := trust.IssueCert(w.rootPriv, trust.Cert{Epoch: 1, IssuedAt: testNow})
	if err != nil {
		t.Fatal(err)
	}
	EmbeddedCert = base64.StdEncoding.EncodeToString(cert)
	e := w.open("2.7.0")
	got, err := os.ReadFile(filepath.Join(w.dir, CertFileName))
	if err != nil || string(got) != string(cert) {
		t.Fatal("cert not unpacked next to components")
	}
	if !e.state.CertSecured || !e.certGateOpen() {
		t.Fatal("gate must open after cert is on disk")
	}
	// Base-редакція: сертифікат не розпаковується, ворота не потрібні.
	os.Remove(filepath.Join(w.dir, CertFileName))
	w2 := newWorld(t)
	EmbeddedCert = base64.StdEncoding.EncodeToString(cert)
	if _, err := Open(Options{InstallDir: w2.dir, Channel: "stable", Edition: "base", CurrentVersion: "2.7.0"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(w2.dir, CertFileName)); err == nil {
		t.Fatal("base edition must not get the cert")
	}
	// Підроблений вшитий cert: ворота закриті, нічого не качаємо.
	_, evil := key(t)
	forged, _ := trust.IssueCert(evil, trust.Cert{Epoch: 1, IssuedAt: testNow})
	w3 := newWorld(t)
	EmbeddedCert = base64.StdEncoding.EncodeToString(forged)
	w3.publish("2.8.0", nil, nil)
	if _, err := w3.open("2.7.0").Check(context.Background()); !errors.Is(err, ErrCertGate) {
		t.Fatalf("forged cert must close the gate: %v", err)
	}
}

func TestTwoPhaseCommitAndRollback(t *testing.T) {
	for _, healthy := range []bool{true, false} {
		w := newWorld(t)
		os.WriteFile(filepath.Join(w.dir, "launcher-version.json"), []byte(`{"version":"2.7.0"}`), 0o644)
		os.WriteFile(filepath.Join(w.dir, "backend.dll"), []byte("NEW"), 0o644)
		os.WriteFile(filepath.Join(w.dir, "backend.dll"+install.BackupSuffix), []byte("OLD"), 0o644)
		e := w.open("2.7.0")
		plan := &Plan{Manifest: &trust.Manifest{Version: "2.8.0"}}
		if err := e.BeforeApply(plan); err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(w.dir, "launcher-version.json"), []byte(`{"version":"2.8.0"}`), 0o644)
		e.AfterApply(plan, []manifest.Component{{Path: "backend.dll"}})
		e.Close()
		if healthy {
			MarkHealthy(w.dir, "2.8.0")
			w.open("2.8.0").Close()
			if loadState(w.dir).Phase != PhaseCommitted {
				t.Fatal("not committed")
			}
			if _, err := os.Stat(filepath.Join(w.dir, "backend.dll"+install.BackupSuffix)); err == nil {
				t.Fatal(".previous not cleaned after commit")
			}
			continue
		}
		for i := 0; i < maxHealthAttempts; i++ {
			w.open("2.8.0").Close()
		}
		b, _ := os.ReadFile(filepath.Join(w.dir, "backend.dll"))
		lv, _ := os.ReadFile(filepath.Join(w.dir, "launcher-version.json"))
		if string(b) != "OLD" || !strings.Contains(string(lv), "2.7.0") {
			t.Fatalf("not rolled back: %s %s", b, lv)
		}
		if loadState(w.dir).FailedVersion != "2.8.0" {
			t.Fatal("failed version not recorded")
		}
	}
}

func TestLock(t *testing.T) {
	w := newWorld(t)
	a, b := w.open("2.7.0"), w.open("2.7.0")
	if err := a.Lock(); err != nil {
		t.Fatal(err)
	}
	if err := b.Lock(); !errors.Is(err, ErrBusy) {
		t.Fatalf("second updater must wait: %v", err)
	}
	a.Close()
	if err := b.Lock(); err != nil {
		t.Fatalf("lock not released: %v", err)
	}
}
