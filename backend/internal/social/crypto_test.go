package social

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCryptoRoundTrip(t *testing.T) {
	aPub, aSec, err := GenerateIdentity()
	if err != nil {
		t.Fatalf("GenerateIdentity A: %v", err)
	}
	bPub, bSec, err := GenerateIdentity()
	if err != nil {
		t.Fatalf("GenerateIdentity B: %v", err)
	}

	// Спільний секрет мусить збігатись з обох боків (ECDH симетричний).
	ab := Precompute(bPub, aSec)
	ba := Precompute(aPub, bSec)
	if ab != ba {
		t.Fatal("спільний секрет не симетричний")
	}

	secretText := "привіт, це секретне повідомлення з UTF-8: 🔐"
	ct, nonce, err := Encrypt(ab, []byte(secretText))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if ct == "" || nonce == "" {
		t.Fatal("Encrypt повернув порожнє")
	}
	if strings.Contains(ct, secretText) {
		t.Fatal("ciphertext містить відкритий текст")
	}

	got, err := Decrypt(ba, ct, nonce)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if string(got) != secretText {
		t.Fatalf("розшифровано %q, очікували %q", got, secretText)
	}
}

func TestCryptoNonceUniquePerMessage(t *testing.T) {
	aPub, aSec, _ := GenerateIdentity()
	bPub, _, _ := GenerateIdentity()
	shared := Precompute(bPub, aSec)
	_, n1, _ := Encrypt(shared, []byte("те саме"))
	_, n2, _ := Encrypt(shared, []byte("те саме"))
	if n1 == n2 {
		t.Fatal("nonce повторюється — це ламає XSalsa20-Poly1305")
	}
	_ = aPub
}

func TestCryptoTamperAndWrongKey(t *testing.T) {
	aPub, aSec, _ := GenerateIdentity()
	bPub, _, _ := GenerateIdentity()
	shared := Precompute(bPub, aSec)
	ct, nonce, _ := Encrypt(shared, []byte("дані"))

	// Побитий ciphertext мусить не пройти автентифікацію.
	if _, err := Decrypt(shared, ct+"AA", nonce); err == nil {
		t.Fatal("побитий ciphertext прийнято")
	}
	// Чужий ключ — не розшифровує.
	cPub, cSec, _ := GenerateIdentity()
	other := Precompute(cPub, cSec)
	if _, err := Decrypt(other, ct, nonce); err == nil {
		t.Fatal("чужий ключ розшифрував повідомлення")
	}
	_ = aPub
}

func TestCryptoLimits(t *testing.T) {
	_, aSec, _ := GenerateIdentity()
	bPub, _, _ := GenerateIdentity()
	shared := Precompute(bPub, aSec)

	if _, _, err := Encrypt(shared, nil); !errors.Is(err, ErrBadInput) {
		t.Fatalf("порожній plaintext: got %v", err)
	}
	tooBig := make([]byte, MaxPlaintextBytes+1)
	if _, _, err := Encrypt(shared, tooBig); !errors.Is(err, ErrCryptoTooLarge) {
		t.Fatalf("завеликий plaintext: got %v", err)
	}
	// Максимальний дозволений розмір мусить влазити в ліміт Worker (2000 символів base64).
	ct, _, err := Encrypt(shared, make([]byte, MaxPlaintextBytes))
	if err != nil {
		t.Fatalf("Encrypt на межі: %v", err)
	}
	if len(ct) > maxCiphertextB64Len {
		t.Fatalf("ciphertext %d символів > ліміту Worker %d", len(ct), maxCiphertextB64Len)
	}
}

func TestPublicKeyEncoding(t *testing.T) {
	pub, _, _ := GenerateIdentity()
	enc := EncodePublicKey(pub)
	if len(enc) != 44 { // 32 байти base64 з padding
		t.Fatalf("довжина ключа %d, очікували 44", len(enc))
	}
	back, err := DecodePublicKey(enc)
	if err != nil || back != pub {
		t.Fatalf("DecodePublicKey: %v, рівність=%v", err, back == pub)
	}
	for _, bad := range []string{"", "!!!!", strings.Repeat("A", 20), strings.Repeat("A", 60), "не-base64"} {
		if _, err := DecodePublicKey(bad); err == nil {
			t.Fatalf("прийнято невалідний ключ %q", bad)
		}
	}
}

func TestKeyStoreLoadOrCreateAndPersist(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "keys")
	ks := NewKeyStore(dir)

	pub1, sec1, err := ks.LoadOrCreate()
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	// Повторний виклик — той самий ключ (ідемпотентно).
	pub2, sec2, err := ks.LoadOrCreate()
	if err != nil {
		t.Fatalf("LoadOrCreate повторно: %v", err)
	}
	if pub1 != pub2 || sec1 != sec2 {
		t.Fatal("друговий виклик створив інший ключ")
	}
	if !ks.Exists() {
		t.Fatal("Exists() == false після створення")
	}

	// Права файлу 0600 — лише на Unix (Windows не зберігає POSIX-права).
	if runtime.GOOS != "windows" {
		if st, err := os.Stat(filepath.Join(dir, identityFileName)); err == nil {
			if perm := st.Mode().Perm(); perm != 0o600 {
				t.Fatalf("права файлу ключа %o, очікували 600", perm)
			}
		}
	}
}

func TestKeyStoreCorruptDoesNotRegenerate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "keys")
	ks := NewKeyStore(dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// Пошкоджений (але наявний) файл — НЕ можна тихо перезаписувати новим
	// ключем, інакше стара переписка стане нерозшифровною.
	if err := os.WriteFile(filepath.Join(dir, identityFileName), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ks.LoadOrCreate(); err == nil {
		t.Fatal("пошкоджений ключ мовчки перегенеровано")
	}
}

func TestKeyStoreMismatchedPublic(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "keys")
	ks := NewKeyStore(dir)
	pub, sec, err := GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	// Підміняємо публічний ключ на чужий — файл неконсистентний.
	other, _, _ := GenerateIdentity()
	if err := ks.save(other, sec); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ks.Load(); err == nil {
		t.Fatal("прийнято файл із чужим публічним ключем")
	}
	_ = pub
}
