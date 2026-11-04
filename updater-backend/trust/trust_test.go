package trust

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

type fixture struct {
	rootPub  ed25519.PublicKey
	rootPriv ed25519.PrivateKey
	relPriv  ed25519.PrivateKey
	srvPriv  ed25519.PrivateKey
	ks       *Keyset
	ksBytes  []byte
	ksSig    string
	now      time.Time
}

func newKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func b64(k ed25519.PublicKey) string { return base64.StdEncoding.EncodeToString(k) }

func setup(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{now: time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)}
	f.rootPub, f.rootPriv = newKey(t)
	relPub, relPriv := newKey(t)
	srvPub, srvPriv := newKey(t)
	f.relPriv, f.srvPriv = relPriv, srvPriv
	ks := Keyset{
		V: 1, Version: 1, IssuedAt: f.now,
		Keys: []KeyEntry{
			{KID: "release-1", Role: RoleRelease, Alg: AlgEd25519, Pub: b64(relPub)},
			{KID: "server-1", Role: RoleServer, Alg: AlgEd25519, Pub: b64(srvPub)},
		},
		Sources: Sources{Repo: "B23101/Shaurma-Launcher", SignalURL: "https://api.shaurma-launcher.org", AssetHosts: []string{"release-assets.githubusercontent.com"}},
	}
	f.ksBytes, _ = json.Marshal(ks)
	f.ksSig = Sign(f.rootPriv, ContextKeyset, f.ksBytes)
	parsed, err := ParseKeyset([]ed25519.PublicKey{f.rootPub}, f.ksBytes, f.ksSig)
	if err != nil {
		t.Fatal(err)
	}
	f.ks = parsed
	return f
}

func (f *fixture) manifest(mut func(*Manifest)) ([]byte, string) {
	m := Manifest{
		V: 2, Channel: "stable", Version: "2.8.0", Tag: "v2.8.0",
		IssuedAt: f.now.Add(-time.Hour), ExpiresAt: f.now.Add(90 * 24 * time.Hour),
		Files: []File{{Name: "backend.dll", Platform: "windows-amd64", Role: FileBackend, Path: "backend.dll", Size: 10, SHA256: strings.Repeat("a", 64)}},
		KeyID: "release-1",
	}
	if mut != nil {
		mut(&m)
	}
	b, _ := json.Marshal(m)
	return b, Sign(f.relPriv, ContextManifest, b)
}

func TestKeysetRejectsForeignRoot(t *testing.T) {
	f := setup(t)
	other, _ := newKey(t)
	if _, err := ParseKeyset([]ed25519.PublicKey{other}, f.ksBytes, f.ksSig); err == nil {
		t.Fatal("keyset signed by another root accepted")
	}
	tampered := append([]byte(nil), f.ksBytes...)
	tampered[len(tampered)-2] ^= 1
	if _, err := ParseKeyset([]ed25519.PublicKey{f.rootPub}, tampered, f.ksSig); err == nil {
		t.Fatal("tampered keyset accepted")
	}
}

func TestManifestSignatureAndPolicy(t *testing.T) {
	f := setup(t)
	b, sig := f.manifest(nil)
	m, err := ParseManifest(f.ks, b, sig, f.now)
	if err != nil {
		t.Fatal(err)
	}
	pol := UpdatePolicy{Channel: "stable", CurrentVersion: "2.7.27.1", HighestSeen: "2.7.27.1", Now: f.now}
	if err := CheckUpdate(m, f.ks, pol); err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		p    func(*UpdatePolicy)
		want error
	}{
		"not newer": {func(p *UpdatePolicy) { p.CurrentVersion = "2.8.0" }, ErrNotNewer},
		"rollback":  {func(p *UpdatePolicy) { p.CurrentVersion = "2.7.0"; p.HighestSeen = "2.9.0" }, ErrRollback},
		"expired":   {func(p *UpdatePolicy) { p.Now = f.now.Add(100 * 24 * time.Hour) }, ErrExpired},
		"future":    {func(p *UpdatePolicy) { p.Now = f.now.Add(-48 * time.Hour) }, ErrNotYetValid},
		"channel":   {func(p *UpdatePolicy) { p.Channel = "beta" }, ErrWrongChannel},
		"signal":    {func(p *UpdatePolicy) { p.SignaledVersion = "2.9.0" }, ErrSignalMismatch},
	}
	for name, c := range cases {
		p := pol
		c.p(&p)
		if err := CheckUpdate(m, f.ks, p); !errors.Is(err, c.want) {
			t.Errorf("%s: got %v want %v", name, err, c.want)
		}
	}
}

func TestManifestRejectsForgeryAndWrongRole(t *testing.T) {
	f := setup(t)
	b, _ := f.manifest(nil)
	_, evil := newKey(t)
	if _, err := ParseManifest(f.ks, b, Sign(evil, ContextManifest, b), f.now); err == nil {
		t.Fatal("foreign signature accepted")
	}
	// Server-ключ не вміє підписувати оновлення.
	b2, _ := f.manifest(func(m *Manifest) { m.KeyID = "server-1" })
	if _, err := ParseManifest(f.ks, b2, Sign(f.srvPriv, ContextManifest, b2), f.now); err == nil {
		t.Fatal("server key accepted for manifest")
	}
	// Підпис іншого контексту (напр. keyset) не годиться для маніфесту.
	if _, err := ParseManifest(f.ks, b, Sign(f.relPriv, ContextKeyset, b), f.now); err == nil {
		t.Fatal("cross-context signature accepted")
	}
}

func TestManifestStructure(t *testing.T) {
	f := setup(t)
	bad := map[string]func(*Manifest){
		"traversal":  func(m *Manifest) { m.Files[0].Path = "../evil.dll" },
		"abs":        func(m *Manifest) { m.Files[0].Path = `C:\Windows\x.dll` },
		"bad sha":    func(m *Manifest) { m.Files[0].SHA256 = "zz" },
		"zero size":  func(m *Manifest) { m.Files[0].Size = 0 },
		"bad name":   func(m *Manifest) { m.Files[0].Name = "../x" },
		"format v3":  func(m *Manifest) { m.V = 3 },
		"bad tag":    func(m *Manifest) { m.Tag = "main" },
		"bad authcd": func(m *Manifest) { m.Authenticode = "maybe" },
	}
	for name, mut := range bad {
		b, sig := f.manifest(mut)
		if _, err := ParseManifest(f.ks, b, sig, f.now); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestKeyRotationAndRevocation(t *testing.T) {
	f := setup(t)
	b, sig := f.manifest(nil)
	f.ks.Revoked = []string{"release-1"}
	if _, err := ParseManifest(f.ks, b, sig, f.now); err == nil {
		t.Fatal("revoked key accepted")
	}
	f.ks.Revoked = nil
	past := f.now.Add(-time.Minute)
	f.ks.Keys[0].ValidUntil = &past
	if _, err := ParseManifest(f.ks, b, sig, f.now); err == nil {
		t.Fatal("expired key accepted")
	}
}

func TestRollout(t *testing.T) {
	f := setup(t)
	b, sig := f.manifest(nil)
	m, _ := ParseManifest(f.ks, b, sig, f.now)
	if RolloutAllows(m, "later", "2.7.0", f.now) {
		t.Fatal("later must defer a normal update")
	}
	if !RolloutAllows(m, "later", "2.7.0", f.now.Add(8*24*time.Hour)) {
		t.Fatal("later must not defer beyond 7 days")
	}
	m.Security = true
	if !RolloutAllows(m, "pause", "2.7.0", f.now) {
		t.Fatal("security update must ignore pause")
	}
	m.Security = false
	m.MinVersion = "2.7.5"
	if !RolloutAllows(m, "pause", "2.7.0", f.now) {
		t.Fatal("min_version must ignore pause")
	}
}

func TestCert(t *testing.T) {
	f := setup(t)
	data, err := IssueCert(f.rootPriv, Cert{Epoch: 1, IssuedAt: f.now, Note: "test"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := ParseCert([]ed25519.PublicKey{f.rootPub}, data)
	if err != nil || c.Epoch != 1 {
		t.Fatalf("cert: %v %+v", err, c)
	}
	_, evil := newKey(t)
	forged, _ := IssueCert(evil, Cert{Epoch: 1, IssuedAt: f.now})
	if _, err := ParseCert([]ed25519.PublicKey{f.rootPub}, forged); err == nil {
		t.Fatal("foreign cert accepted")
	}
	if _, err := ParseCert([]ed25519.PublicKey{f.rootPub}, []byte("-----BEGIN CERTIFICATE-----")); !errors.Is(err, ErrNotCert) {
		t.Fatal("non-shaurma cert must be ErrNotCert")
	}
}

func TestEntitlementAndActivation(t *testing.T) {
	f := setup(t)
	srv := f.ks.RoleKeys(RoleServer, f.now)
	ent, _ := json.Marshal(Entitlement{V: 1, DeviceID: "d1", Epoch: 1, Tier: "shaurma", IAT: f.now.Unix(), EXP: f.now.Add(time.Hour).Unix()})
	tok := SignCompact(f.srvPriv, ContextEntitlement, ent)
	if _, err := ParseEntitlement(srv, tok, f.now); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseEntitlement(srv, tok, f.now.Add(2*time.Hour)); err == nil {
		t.Fatal("expired entitlement accepted")
	}
	// Release-ключ не видає токенів доступу.
	if _, err := ParseEntitlement(srv, SignCompact(f.relPriv, ContextEntitlement, ent), f.now); err == nil {
		t.Fatal("release key accepted for entitlement")
	}
	res, _ := json.Marshal(ActivationResult{V: 1, Status: "active", DeviceID: "d1", DevicePub: "PUB", Nonce: "N1"})
	at := SignCompact(f.srvPriv, ContextActivation, res)
	if _, err := ParseActivation(srv, at, "N1", "PUB"); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseActivation(srv, at, "N2", "PUB"); err == nil {
		t.Fatal("activation for another nonce accepted")
	}
	if _, err := ParseActivation(srv, "eyJ2IjoxfQ.AAAA", "N1", "PUB"); err == nil {
		t.Fatal("unsigned activation accepted")
	}
}
