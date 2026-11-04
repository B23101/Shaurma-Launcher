package access

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"shaurma-trust"
)

// fakeServer — мінімальна реалізація нового worker на Go (контракт той самий).
type fakeServer struct {
	signer   ed25519.PrivateKey // чим підписувати відповіді (може бути чужим ключем)
	status   int                // примусовий код відповіді активації (0 — звичайно)
	revoked  bool
	devices  map[string]bool
	approval bool
}

func (f *fakeServer) handler(t *testing.T) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/activate/nonce", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"nonce": "N-" + time.Now().Format("150405.000000")})
	})
	mux.HandleFunc("/v1/activate", func(w http.ResponseWriter, r *http.Request) {
		if f.status != 0 {
			w.WriteHeader(f.status)
			w.Write([]byte(`{"error":"x"}`))
			return
		}
		var req trust.ActivationRequest
		json.NewDecoder(r.Body).Decode(&req)
		pub, _ := trust.ParsePublicKey(req.DevicePub)
		if trust.Verify([]ed25519.PublicKey{pub}, trust.ContextActivation, trust.ActivationProofMessage(req.Nonce, req.DevicePub), req.NonceSig) != nil {
			w.WriteHeader(400)
			return
		}
		st := "active"
		code := 200
		if f.approval {
			st, code = "pending", 202
		}
		id := deviceID(pub)
		f.devices[id] = true
		payload, _ := json.Marshal(trust.ActivationResult{V: 1, Status: st, DeviceID: id, DevicePub: req.DevicePub, Nonce: req.Nonce, Epoch: 1, IssuedAt: time.Now().Unix()})
		w.WriteHeader(code)
		json.NewEncoder(w).Encode(map[string]string{"token": trust.SignCompact(f.signer, trust.ContextActivation, payload)})
	})
	mux.HandleFunc("/v1/entitlement", func(w http.ResponseWriter, r *http.Request) {
		if f.revoked {
			w.WriteHeader(403)
			w.Write([]byte(`{"error":"device_revoked"}`))
			return
		}
		if !f.devices[r.Header.Get("X-Shaurma-Device")] {
			w.WriteHeader(401)
			return
		}
		payload, _ := json.Marshal(trust.Entitlement{V: 1, DeviceID: r.Header.Get("X-Shaurma-Device"), Epoch: 1, Tier: "shaurma", IAT: time.Now().Unix(), EXP: time.Now().Add(time.Hour).Unix()})
		json.NewEncoder(w).Encode(map[string]string{"entitlement": trust.SignCompact(f.signer, trust.ContextEntitlement, payload)})
	})
	mux.HandleFunc("/v1/device/unlink", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"ok":true}`)) })
	return mux
}

type env struct {
	dir      string
	rootPriv ed25519.PrivateKey
	server   *fakeServer
	srvPriv  ed25519.PrivateKey
}

func setup(t *testing.T) *env {
	rootPub, rootPriv, _ := ed25519.GenerateKey(rand.Reader)
	srvPub, srvPriv, _ := ed25519.GenerateKey(rand.Reader)
	fs := &fakeServer{signer: srvPriv, devices: map[string]bool{}}
	srv := httptest.NewServer(fs.handler(t))
	t.Cleanup(srv.Close)
	ks, _ := json.Marshal(trust.Keyset{V: 1, Version: 1, IssuedAt: time.Now(),
		Keys:    []trust.KeyEntry{{KID: "server-1", Role: trust.RoleServer, Alg: trust.AlgEd25519, Pub: base64.StdEncoding.EncodeToString(srvPub)}},
		Sources: trust.Sources{Repo: "B23101/Shaurma-Launcher"}})
	prev := [3]string{RootKeys, EmbeddedKeyset, EmbeddedKeysetSig}
	t.Cleanup(func() { RootKeys, EmbeddedKeyset, EmbeddedKeysetSig = prev[0], prev[1], prev[2] })
	RootKeys = base64.StdEncoding.EncodeToString(rootPub)
	EmbeddedKeyset = base64.StdEncoding.EncodeToString(ks)
	EmbeddedKeysetSig = trust.Sign(rootPriv, trust.ContextKeyset, ks)
	e := &env{dir: t.TempDir(), rootPriv: rootPriv, server: fs, srvPriv: srvPriv}
	testServerURL = srv.URL
	t.Cleanup(func() { testServerURL = "" })
	return e
}

func (e *env) cert(t *testing.T, name string) string {
	c, _ := trust.IssueCert(e.rootPriv, trust.Cert{Epoch: 1, IssuedAt: time.Now()})
	p := filepath.Join(e.dir, name)
	os.WriteFile(p, c, 0o644)
	return p
}

func TestFindCertByContent(t *testing.T) {
	e := setup(t)
	os.WriteFile(filepath.Join(e.dir, "other.cert"), []byte("-----BEGIN CERTIFICATE-----"), 0o644)
	_, evil, _ := ed25519.GenerateKey(rand.Reader)
	forged, _ := trust.IssueCert(evil, trust.Cert{Epoch: 1, IssuedAt: time.Now()})
	os.WriteFile(filepath.Join(e.dir, "forged.cert"), forged, 0o644)
	if c, _ := New(e.dir, "2.8.0").FindCert(); c != nil {
		t.Fatal("foreign/garbage cert accepted")
	}
	e.cert(t, "master (1).cert")
	if c, paths := New(e.dir, "2.8.0").FindCert(); c == nil || len(paths) != 1 {
		t.Fatal("renamed cert not found by content")
	}
}

func TestActivationHappyPath(t *testing.T) {
	e := setup(t)
	p := e.cert(t, "master.cert")
	m := New(e.dir, "2.8.0")
	m.AutoActivate(context.Background())
	st := m.Status()
	if st.State != StateActive || !m.HasAccess() {
		t.Fatalf("not active: %+v", st)
	}
	if _, err := os.Stat(p); err == nil {
		t.Fatal("cert must be removed after successful activation")
	}
	if _, err := os.Stat(filepath.Join(e.dir, DeviceFile)); err != nil {
		t.Fatal("device record missing")
	}
	if m.Entitlement() == "" {
		t.Fatal("no entitlement after activation")
	}
	// Новий процес читає стан з диска.
	m2 := New(e.dir, "2.8.0")
	if !m2.HasAccess() || m2.Entitlement() == "" {
		t.Fatal("state not persisted")
	}
	// Відкликання на сервері → доступ зникає.
	e.server.revoked = true
	m3 := New(e.dir, "2.8.0")
	m3.refreshEntitlement(context.Background())
	if m3.HasAccess() || m3.Status().State == StateActive {
		t.Fatal("revoked device still has access")
	}
}

func TestCertKeptOnFailures(t *testing.T) {
	cases := map[string]func(e *env){
		"server down":      func(e *env) { e.server.status = 503 },
		"forged 200":       func(e *env) { _, evil, _ := ed25519.GenerateKey(rand.Reader); e.server.signer = evil },
		"closed epoch":     func(e *env) { e.server.status = 403 },
		"pending approval": func(e *env) { e.server.approval = true },
	}
	for name, mut := range cases {
		t.Run(name, func(t *testing.T) {
			e := setup(t)
			p := e.cert(t, "master.cert")
			mut(e)
			m := New(e.dir, "2.8.0")
			cert, paths := m.FindCert()
			_, err := m.Activate(context.Background(), cert, paths)
			if _, serr := os.Stat(p); serr != nil {
				t.Fatalf("cert removed on %s (err %v)", name, err)
			}
			if m.HasAccess() {
				t.Fatal("access granted without valid activation")
			}
			if name == "forged 200" && !errors.Is(err, ErrBadResponse) {
				t.Fatalf("forged response: %v", err)
			}
		})
	}
}

func TestUnlink(t *testing.T) {
	e := setup(t)
	e.cert(t, "master.cert")
	m := New(e.dir, "2.8.0")
	m.AutoActivate(context.Background())
	if _, err := m.Unlink(context.Background()); err != nil {
		t.Fatal(err)
	}
	if m.HasAccess() || m.Status().State != StateNone {
		t.Fatal("unlink kept access")
	}
	for _, f := range []string{DeviceFile, KeyFile} {
		if _, err := os.Stat(filepath.Join(e.dir, f)); err == nil {
			t.Fatalf("%s not removed", f)
		}
	}
}
