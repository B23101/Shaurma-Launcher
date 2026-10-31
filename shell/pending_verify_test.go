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
	"testing"
)

func TestPendingUpdateSignedProof(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	oldKeys, oldCDN := trustedKeys, cdnBase
	trustedKeys, cdnBase = base64.StdEncoding.EncodeToString(pub), "https://updates.example.test"
	defer func() { trustedKeys, cdnBase = oldKeys, oldCDN }()
	dir := t.TempDir()
	name := updaterLibraryName()
	content := []byte("verified module")
	sum := sha256.Sum256(content)
	m := bootstrapRemoteManifest{Version: "3.0.0", Components: []bootstrapComponent{{Path: name, URL: "/launcher/" + name, Size: int64(len(content)), SHA256: hex.EncodeToString(sum[:])}}}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	signature := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, append([]byte(manifestSigContext), raw...)))
	proof := struct {
		Payload   []byte `json:"payload"`
		Signature string `json:"signature"`
	}{raw, signature}
	proofData, err := json.Marshal(proof)
	if err != nil {
		t.Fatal(err)
	}
	pending := filepath.Join(dir, name+".new")
	if err := os.WriteFile(pending, content, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pending+".proof", proofData, 0600); err != nil {
		t.Fatal(err)
	}
	if err := verifyPendingComponent(dir, name); err != nil {
		t.Fatalf("valid offline pending update rejected: %v", err)
	}
	if err := os.WriteFile(pending, []byte("tampered module"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := verifyPendingComponent(dir, name); err == nil {
		t.Fatal("tampered pending file accepted")
	}
	if err := os.WriteFile(pending, content, 0600); err != nil {
		t.Fatal(err)
	}
	proof.Payload = append(proof.Payload, ' ')
	proofData, _ = json.Marshal(proof)
	if err := os.WriteFile(pending+".proof", proofData, 0600); err != nil {
		t.Fatal(err)
	}
	if err := verifyPendingComponent(dir, name); err == nil {
		t.Fatal("modified signed payload accepted")
	}
}
