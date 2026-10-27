package main

// manifestsig.go — перевірка Ed25519-підпису launcher-version.json у
// bootstrap (Shell A). Той самий контракт, що в updater-backend/internal/
// sigverify і shell/manifestsig.go: підпис над ТОЧНИМИ байтами маніфесту з
// доменним префіксом manifestSigContext. Файл навмисно лише зі stdlib і без
// WinAPI — тестується на будь-якій ОС.
//
// ЧОМУ ЦЕ КРИТИЧНО: bootstrap завантажує і ЗАПУСКАЄ виконуваний файл, чий
// sha256 береться з маніфесту. Без підпису маніфесту sha256 нічого не
// захищає — хто підмінив маніфест на CDN, той підмінив і хеш. Приватний
// ключ є лише в pack-manager.

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"strings"
)

// trustedKeys — публічні ключі Ed25519 (base64 raw, 32 байти) через кому.
// Вшивається на етапі збірки: -X main.trustedKeys=<b64>[,<b64>].
// Кілька ключів = ротація без розриву оновлень.
var trustedKeys = ""

// manifestSigContext ПОВИНЕН збігатися з sigverify.Context в updater-backend
// і installers.LauncherManifestContext у pack-manager.
const manifestSigContext = "shaurma-launcher-manifest-v1\n"

var errBadManifestSig = errors.New("підпис маніфесту недійсний або відсутній")

func parseTrustedKeys(list string) []ed25519.PublicKey {
	var out []ed25519.PublicKey
	for _, part := range strings.Split(list, ",") {
		b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(part))
		if err == nil && len(b) == ed25519.PublicKeySize {
			out = append(out, ed25519.PublicKey(b))
		}
	}
	return out
}

// manifestSigEnabled — чи вшито хоч один довірений ключ (інакше збірка
// без перевірки підпису: лише для розробки).
func manifestSigEnabled() bool { return manifestSigEnabledWith(trustedKeys) }

func manifestSigEnabledWith(keys string) bool { return len(parseTrustedKeys(keys)) > 0 }

func verifyManifestSigWith(keys string, payload []byte, sigB64 string) error {
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(sigB64))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return errBadManifestSig
	}
	msg := append([]byte(manifestSigContext), payload...)
	for _, k := range parseTrustedKeys(keys) {
		if ed25519.Verify(k, msg, sig) {
			return nil
		}
	}
	return errBadManifestSig
}

// verifyManifestSig перевіряє підпис довіреними (вшитими) ключами.
func verifyManifestSig(payload []byte, sigB64 string) error {
	return verifyManifestSigWith(trustedKeys, payload, sigB64)
}
