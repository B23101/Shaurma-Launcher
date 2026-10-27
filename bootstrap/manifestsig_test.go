package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"
)

func signFor(priv ed25519.PrivateKey, payload []byte) string {
	return base64.StdEncoding.EncodeToString(ed25519.Sign(priv, append([]byte(manifestSigContext), payload...)))
}

func TestVerifyManifestSig(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	pub2, _, _ := ed25519.GenerateKey(rand.Reader)
	keys := base64.StdEncoding.EncodeToString(pub2) + "," + base64.StdEncoding.EncodeToString(pub) // ротація: другий — справжній
	payload := []byte(`{"version":"1.2.3"}`)
	sig := signFor(priv, payload)

	if err := verifyManifestSigWith(keys, payload, sig); err != nil {
		t.Fatalf("валідний підпис відхилено: %v", err)
	}
	if err := verifyManifestSigWith(keys, []byte(`{"version":"9.9.9"}`), sig); err == nil {
		t.Fatal("змінений маніфест пройшов перевірку")
	}
	if err := verifyManifestSigWith(base64.StdEncoding.EncodeToString(pub2), payload, sig); err == nil {
		t.Fatal("підпис чужим ключем пройшов")
	}
	for _, bad := range []string{"", "not-base64!!", base64.StdEncoding.EncodeToString([]byte("short"))} {
		if err := verifyManifestSigWith(keys, payload, bad); err == nil {
			t.Fatalf("некоректний підпис %q пройшов", bad)
		}
	}
	// Підпис без доменного префікса (наприклад, підпис інсталятора) не підходить.
	raw := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, payload))
	if err := verifyManifestSigWith(keys, payload, raw); err == nil {
		t.Fatal("підпис без доменного префікса пройшов")
	}
	if manifestSigEnabledWith("") || !manifestSigEnabledWith(keys) {
		t.Fatal("manifestSigEnabled працює неправильно")
	}
}
