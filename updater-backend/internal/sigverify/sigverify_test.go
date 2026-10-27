package sigverify

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"
)

func TestVerify(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	trusted := base64.StdEncoding.EncodeToString(pub)
	payload := []byte(`{"version":"1.2.3"}`)
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, append([]byte(Context), payload...)))

	if err := VerifyWith(trusted, payload, sig); err != nil {
		t.Fatalf("валідний підпис відхилено: %v", err)
	}
	if err := VerifyWith(trusted, []byte(`{"version":"9.9.9"}`), sig); err == nil {
		t.Fatal("змінений маніфест прийнято")
	}
	otherPub, _, _ := ed25519.GenerateKey(rand.Reader)
	if err := VerifyWith(base64.StdEncoding.EncodeToString(otherPub), payload, sig); err == nil {
		t.Fatal("чужий ключ прийнято")
	}
	if err := VerifyWith(trusted, payload, "не-base64"); err == nil {
		t.Fatal("сміття замість підпису прийнято")
	}
}
