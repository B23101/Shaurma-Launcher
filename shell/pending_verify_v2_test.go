package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"shaurma-trust"
)

func TestPendingProofV2(t *testing.T) {
	rootPub, rootPriv, _ := ed25519.GenerateKey(rand.Reader)
	relPub, relPriv, _ := ed25519.GenerateKey(rand.Reader)
	prev := rootKeys
	rootKeys = base64.StdEncoding.EncodeToString(rootPub)
	defer func() { rootKeys = prev }()

	ks, _ := json.Marshal(trust.Keyset{V: 1, Version: 1, IssuedAt: time.Now(),
		Keys:    []trust.KeyEntry{{KID: "release-1", Role: trust.RoleRelease, Alg: trust.AlgEd25519, Pub: base64.StdEncoding.EncodeToString(relPub)}},
		Sources: trust.Sources{Repo: "B23101/Shaurma-Launcher"}})
	body := []byte("new updater bytes")
	sum := sha256.Sum256(body)
	m, _ := json.Marshal(trust.Manifest{V: 2, Channel: "stable", Version: "2.8.0", Tag: "v2.8.0",
		IssuedAt: time.Now().Add(-time.Hour), ExpiresAt: time.Now().Add(time.Hour), KeyID: "release-1",
		Files: []trust.File{{Name: "updater.dll", Platform: runtime.GOOS + "-" + runtime.GOARCH, Role: trust.FileUpdater, Path: "updater.dll", Size: int64(len(body)), SHA256: hex.EncodeToString(sum[:])}}})
	proof, _ := json.Marshal(map[string]any{
		"format": 2, "payload": m, "signature": trust.Sign(relPriv, trust.ContextManifest, m),
		"keyset": ks, "keysetSig": trust.Sign(rootPriv, trust.ContextKeyset, ks),
	})
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "updater.dll.new"), body, 0o644)
	os.WriteFile(filepath.Join(dir, "updater.dll.new.proof"), proof, 0o600)
	if err := verifyPendingComponent(dir, "updater.dll"); err != nil {
		t.Fatalf("valid v2 proof rejected: %v", err)
	}
	// Підмінений .new між запусками — не застосовується.
	os.WriteFile(filepath.Join(dir, "updater.dll.new"), []byte("evil bytes here!!"), 0o644)
	if err := verifyPendingComponent(dir, "updater.dll"); err == nil {
		t.Fatal("tampered pending file accepted")
	}
	// Keyset, підписаний чужим root, — теж ні.
	_, evilRoot, _ := ed25519.GenerateKey(rand.Reader)
	os.WriteFile(filepath.Join(dir, "updater.dll.new"), body, 0o644)
	bad, _ := json.Marshal(map[string]any{"format": 2, "payload": m, "signature": trust.Sign(relPriv, trust.ContextManifest, m), "keyset": ks, "keysetSig": trust.Sign(evilRoot, trust.ContextKeyset, ks)})
	os.WriteFile(filepath.Join(dir, "updater.dll.new.proof"), bad, 0o600)
	if err := verifyPendingComponent(dir, "updater.dll"); err == nil {
		t.Fatal("keyset from foreign root accepted")
	}
}
