// Package sigverify перевіряє Ed25519-підпис маніфесту оновлень.
//
// МОДЕЛЬ: у лаунчер вшито ЛИШЕ публічні ключі (TrustedKeys, задаються
// при збірці: -ldflags "-X shrm-updater-backend/internal/sigverify.TrustedKeys=<b64>[,<b64>]").
// Приватний ключ живе тільки в pack-manager. Той, хто контролює CDN, але
// не має приватного ключа, не може підсунути свій маніфест — а отже й
// чужі sha256 файлів.
//
// Якщо TrustedKeys порожній (збірка без ключа), перевірка вимкнена —
// це дозволяє поступовий перехід: спершу випускається клієнт із ключем,
// потім pack-manager починає публікувати <manifest>.sig.
package sigverify

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"strings"
)

// TrustedKeys — base64 (raw, 32 байти) публічні ключі через кому.
var TrustedKeys = ""

// Context — доменний префікс підпису (відокремлює підписи маніфестів
// лаунчера від підписів інсталяторів та будь-чого іншого).
const Context = "shaurma-launcher-manifest-v1\n"

// ErrBadSignature — підпис відсутній або недійсний.
var ErrBadSignature = errors.New("підпис маніфесту оновлення недійсний")

// Enabled — чи вшито хоч один довірений ключ.
func Enabled() bool { return len(keys(TrustedKeys)) > 0 }

func keys(list string) []ed25519.PublicKey {
	var out []ed25519.PublicKey
	for _, part := range strings.Split(list, ",") {
		b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(part))
		if err == nil && len(b) == ed25519.PublicKeySize {
			out = append(out, ed25519.PublicKey(b))
		}
	}
	return out
}

// Verify перевіряє підпис точних байтів маніфесту довіреними ключами.
func Verify(payload []byte, sigB64 string) error {
	return VerifyWith(TrustedKeys, payload, sigB64)
}

// VerifyWith — те саме з явним списком ключів (для тестів).
func VerifyWith(trusted string, payload []byte, sigB64 string) error {
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(sigB64))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return ErrBadSignature
	}
	msg := append([]byte(Context), payload...)
	for _, k := range keys(trusted) {
		if ed25519.Verify(k, msg, sig) {
			return nil
		}
	}
	return ErrBadSignature
}
