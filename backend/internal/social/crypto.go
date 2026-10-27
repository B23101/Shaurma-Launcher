package social

// crypto.go — наскрізне (E2E) шифрування чату.
//
// Модель довіри: НІ Worker, НІ D1, НІ VPS, НІ розробник не бачать відкритий
// текст повідомлення. Ключі живуть виключно на диску гравця; сервер отримує
// лише ciphertext + nonce (ті, що вже генерує Worker/friends.js). Тут — та
// сама схема, що в прикладі X3DH для двох сторін (план, розділ 1.3.1):
//
//	1. Кожен генерує пару Curve25519 (GenerateIdentity). Приватний ключ
//	   ніколи не покидає диск.
//	2. Публічні ключі публікуються через Worker (POST /friends/key).
//	3. Спільний секрет = ECDH(свій приватний, публічний друга) — Precompute.
//	   Він НІКОЛИ не передається мережею.
//	4. Повідомлення = NaCl box/XSalsa20-Poly1305 над спільним секретом,
//	   випадковий nonce на КОЖНЕ повідомлення.
//
// Реалізація — golang.org/x/crypto/nacl/box (той самий примітив, що вже
// стоїть у go.sum), а не саморобна криптографія. Формат — СТАНДАРТНИЙ base64
// (з padding): Worker перевіряє ciphertext/nonce регуляркою
// /^[A-Za-z0-9+/]+=*$/ (isLikelyBase64 у worker/friends.js), а не base64url.

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"

	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/nacl/box"
)

// Розміри, узгоджені з worker/friends.js.
const (
	PublicKeyBytes = 32 // Curve25519
	SecretKeyBytes = 32
	SharedKeyBytes = 32
	NonceBytes     = 24 // NaCl box nonce

	// BoxOverhead — MAC, який box додає до кожного повідомлення.
	BoxOverhead = box.Overhead

	// MaxPlaintextBytes — межа відкритого тексту, за якої base64(ciphertext)
	// гарантовано ≤ CIPHERTEXT_MAX_B64_LEN (2000) у worker/friends.js.
	// ciphertext = plaintext + 16 байтів MAC; base64(std) роздуває до
	// 4*ceil(N/3) символів, тож 1200 байт plaintext → (1216)·4/3 ≈ 1624
	// символи — із запасом під 2000.
	MaxPlaintextBytes = 1200

	// maxCiphertextB64Len — дзеркало CIPHERTEXT_MAX_B64_LEN. Ми НЕ декодуємо
	// більше, ніж Worker колись прийняв би (захист від «бомби» розміром).
	maxCiphertextB64Len = 2000
	// publicKeyB64Len — 32 байти → 44 символи з padding. Дозволяємо 40..48,
	// як і Worker (PUBLIC_KEY_B64_LEN ± 4).
	publicKeyB64MinLen = 40
	publicKeyB64MaxLen = 48
)

// Криптографічні ключі — фіксовані масиви (не слайси), щоб їх не можна було
// випадково «поділити» буфером із чимось іншим.
type (
	PublicKey [PublicKeyBytes]byte
	SecretKey [SecretKeyBytes]byte
	SharedKey [SharedKeyBytes]byte
	Nonce     [NonceBytes]byte
)

// Помилки крипто-шару — БЕЗ деталей (щоб жоден фрагмент ключа/тексту не
// потрапив у текст помилки, а отже й у лог/UI).
var (
	ErrCryptoInvalidKey  = errors.New("social:crypto_bad_key")
	ErrCryptoDecryptFail = errors.New("social:crypto_decrypt_failed")
	ErrCryptoTooLarge    = errors.New("social:crypto_too_large")
)

// GenerateIdentity створює нову пару ключів. Джерело випадковості — crypto/rand.
func GenerateIdentity() (PublicKey, SecretKey, error) {
	pub, priv, err := box.GenerateKey(rand.Reader)
	if err != nil {
		return PublicKey{}, SecretKey{}, fmt.Errorf("social:crypto_rand")
	}
	return PublicKey(*pub), SecretKey(*priv), nil
}

// Precompute обчислює спільний секрет ECDH(власний приватний, публічний друга).
// Симетричний: обидві сторони отримають однаковий ключ. Жодних мережевих
// передач секрету.
func Precompute(peer PublicKey, own SecretKey) SharedKey {
	var shared [SharedKeyBytes]byte
	var p, o [32]byte = peer, own
	box.Precompute(&shared, &p, &o)
	return SharedKey(shared)
}

// Encrypt шифрує plaintext спільним секретом і повертає (ciphertextB64, nonceB64).
// nonce — випадковий на КОЖНЕ повідомлення: повторне використання nonce з тим
// самим ключем ламає XSalsa20-Poly1305, тож воно неприпустиме.
func Encrypt(shared SharedKey, plaintext []byte) (ciphertextB64, nonceB64 string, err error) {
	if len(plaintext) == 0 {
		return "", "", ErrBadInput
	}
	if len(plaintext) > MaxPlaintextBytes {
		return "", "", ErrCryptoTooLarge
	}
	var nonce [NonceBytes]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", "", fmt.Errorf("social:crypto_rand")
	}
	sk := [SharedKeyBytes]byte(shared)
	sealed := box.SealAfterPrecomputation(nil, plaintext, &nonce, &sk)
	return base64.StdEncoding.EncodeToString(sealed), base64.StdEncoding.EncodeToString(nonce[:]), nil
}

// Decrypt розшифровує повідомлення. Будь-яка невідповідність (підміна,
// побиття, чужий ключ) → ErrCryptoDecryptFail, без жодних деталей.
func Decrypt(shared SharedKey, ciphertextB64, nonceB64 string) ([]byte, error) {
	if ciphertextB64 == "" || nonceB64 == "" || len(ciphertextB64) > maxCiphertextB64Len {
		return nil, ErrCryptoDecryptFail
	}
	ct, err := base64.StdEncoding.DecodeString(ciphertextB64)
	if err != nil || len(ct) <= BoxOverhead {
		return nil, ErrCryptoDecryptFail
	}
	nb, err := base64.StdEncoding.DecodeString(nonceB64)
	if err != nil || len(nb) != NonceBytes {
		return nil, ErrCryptoDecryptFail
	}
	var nonce [NonceBytes]byte
	copy(nonce[:], nb)
	sk := [SharedKeyBytes]byte(shared)
	out, ok := box.OpenAfterPrecomputation(nil, ct, &nonce, &sk)
	if !ok {
		return nil, ErrCryptoDecryptFail
	}
	return out, nil
}

// EncodePublicKey повертає стандартний base64 (44 символи) — рівно те, що
// приймає POST /friends/key.
func EncodePublicKey(pk PublicKey) string {
	return base64.StdEncoding.EncodeToString(pk[:])
}

// DecodePublicKey перевіряє довжину до декодування (щоб не алокувати під
// довільний рядок із мережі) і повертає рівно 32 байти.
func DecodePublicKey(s string) (PublicKey, error) {
	var pk PublicKey
	if len(s) < publicKeyB64MinLen || len(s) > publicKeyB64MaxLen {
		return pk, ErrCryptoInvalidKey
	}
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil || len(raw) != PublicKeyBytes {
		return pk, ErrCryptoInvalidKey
	}
	copy(pk[:], raw)
	return pk, nil
}

// publicFromSecret виводить публічний ключ із приватного (Curve25519,
// basepoint-multiplication). Потрібно лише для ПЕРЕВІРКИ цілісності файлу
// ключа (keystore.go): якщо збережений publicKey не відповідає secretKey —
// файл підмінено/пошкоджено, і мовчки ним користуватись не можна.
func publicFromSecret(sec SecretKey) (PublicKey, bool) {
	s := sec
	out, err := curve25519.X25519(s[:], curve25519.Basepoint)
	if err != nil || len(out) != PublicKeyBytes {
		return PublicKey{}, false
	}
	var pk PublicKey
	copy(pk[:], out)
	return pk, true
}
