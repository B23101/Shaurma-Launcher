// Package trust — спільні криптоконтракти нової системи оновлень і доступу
// (план «Єдиний лаунчер», §7 і §13). Один код для оновлювача, shell, backend
// і Pack Manager, лише стандартна бібліотека.
//
// Модель ключів (TUF-подібна):
//
//	root    — офлайн. Підписує лише набір ключів (keyset) і master.cert.
//	release — Pack Manager. Підписує маніфести оновлень, замовлення збірки,
//	          підписані списки (ACL, маніфести паків).
//	server  — сервер доступу. Підписує відповіді активації й токени доступу.
//
// Кожен підпис має власний доменний префікс (Context*): підпис одного типу
// документа ніколи не годиться для іншого, навіть якщо ключ той самий.
// Підпис завжди над ТОЧНИМИ байтами документа (без канонікалізації JSON),
// тому формат однаково перевіряється в Go і в JS (worker).
package trust

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
)

// Доменні префікси підписів. ЗАМОРОЖЕНО (§13): зміна ламає всіх клієнтів.
const (
	ContextKeyset      = "shaurma-keyset-v1\n"
	ContextManifest    = "shaurma-launcher-manifest-v2\n"
	ContextCert        = "shaurma-master-cert-v1\n"
	ContextActivation  = "shaurma-activation-v1\n"
	ContextEntitlement = "shaurma-entitlement-v1\n"
	ContextOrder       = "shaurma-build-order-v1\n"
	ContextDeviceReq   = "shaurma-device-request-v1\n"
	ContextSignedList  = "shaurma-signed-list-v1\n"
	ContextReport      = "shaurma-release-report-v1\n"
	ContextSignal      = "shaurma-update-signal-v1\n"
)

// AlgEd25519 — єдиний алгоритм зараз; поле alg існує, щоб змінити його без
// нового формату.
const AlgEd25519 = "ed25519"

// Ролі ключів у наборі.
const (
	RoleRelease = "release"
	RoleServer  = "server"
)

var (
	ErrBadSignature = errors.New("підпис недійсний")
	ErrNoKey        = errors.New("немає чинного ключа для перевірки підпису")
)

// ParsePublicKey розбирає base64 (std або url, з/без padding) ключ Ed25519.
func ParsePublicKey(s string) (ed25519.PublicKey, error) {
	raw, err := decodeB64(s)
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return nil, errors.New("публічний ключ має бути base64 від 32 байтів")
	}
	return ed25519.PublicKey(raw), nil
}

// ParsePublicKeys розбирає список ключів через кому; некоректні пропускаються.
func ParsePublicKeys(list string) []ed25519.PublicKey {
	var out []ed25519.PublicKey
	for _, part := range strings.Split(list, ",") {
		if k, err := ParsePublicKey(part); err == nil {
			out = append(out, k)
		}
	}
	return out
}

// KeyID — короткий ідентифікатор публічного ключа: перші 16 hex SHA-256.
func KeyID(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:])[:16]
}

// Sign підписує payload з доменним префіксом і повертає base64 (std).
func Sign(priv ed25519.PrivateKey, context string, payload []byte) string {
	return base64.StdEncoding.EncodeToString(ed25519.Sign(priv, message(context, payload)))
}

// Verify перевіряє підпис (base64 std/url) будь-яким із ключів.
func Verify(keys []ed25519.PublicKey, context string, payload []byte, sigB64 string) error {
	sig, err := decodeB64(sigB64)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return ErrBadSignature
	}
	if len(keys) == 0 {
		return ErrNoKey
	}
	msg := message(context, payload)
	for _, k := range keys {
		if len(k) == ed25519.PublicKeySize && ed25519.Verify(k, msg, sig) {
			return nil
		}
	}
	return ErrBadSignature
}

func message(context string, payload []byte) []byte {
	msg := make([]byte, 0, len(context)+len(payload))
	msg = append(msg, context...)
	return append(msg, payload...)
}

func decodeB64(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(s); err == nil {
			return b, nil
		}
	}
	return nil, errors.New("некоректний base64")
}

// trimBOM прибирає UTF-8 BOM: PowerShell любить його дописувати.
func trimBOM(b []byte) []byte {
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		return b[3:]
	}
	return b
}
