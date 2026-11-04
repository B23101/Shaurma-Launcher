// Package newupdate — нова система оновлень лаунчера (план §5, §5А, §8).
//
// Звідки береться оновлення:
//
//  1. Сигнал від нового worker (SignalURL/v1/update/check) — лише ім'я
//     версії, тег і темп роздачі. Жодних адрес.
//  2. Якщо worker мовчить — GitHub напряму:
//     https://github.com/<repo>/releases/latest/download/launcher-manifest.json
//  3. Файли — лише з того самого релізу GitHub; адресу будує клієнт сам із
//     Sources.Repo і тега.
//
// Довіра: root-ключ вшито в клієнт; keyset (підписаний root) називає
// release-ключі; маніфест підписує release. Будь-яка невдача перевірки
// означає «не оновлюватись», без фолбеку на непідписане.
package newupdate

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"shaurma-trust"
)

// Вшиваються при збірці (-ldflags -X shrm-updater-backend/internal/newupdate.<Var>=...).
var (
	// RootKeys — публічні root-ключі (base64 через кому). Порожньо — нова
	// система вимкнена (dev-збірка), працює лише старий шлях.
	RootKeys = ""
	// EmbeddedKeyset / EmbeddedKeysetSig — початковий набір ключів
	// (base64 від keyset.json) і його підпис root.
	EmbeddedKeyset    = ""
	EmbeddedKeysetSig = ""
	// EmbeddedCert — base64 від master.cert. Лише в повній збірці мосту R1:
	// оновлювач розпаковує його в теку лаунчера ДО будь-якого оновлення
	// (ворота сертифіката, §14 етап B), бо наступна версія backend/updater
	// вже не міститиме вшитого сертифіката.
	EmbeddedCert = ""
)

// Початкові джерела, якщо keyset їх не задає (той самий репозиторій і домен
// нового worker; змінюються лише новим keyset, підписаним root).
const (
	DefaultRepo      = "B23101/Shaurma-Launcher"
	DefaultSignalURL = "https://api.shaurma-launcher.org"
)

// DefaultAssetHosts — куди GitHub перенаправляє завантаження ассетів релізу.
var DefaultAssetHosts = []string{"release-assets.githubusercontent.com", "objects.githubusercontent.com"}

// CertFileName — куди розпаковується сертифікат (тека лаунчера, §3.2).
const CertFileName = "master.cert"

// StateFileName — стан нової системи в теці лаунчера (§13 п.7).
const StateFileName = "update-state.json"

// HealthFileName — backend пише сюди версію після успішного старту (§8.3).
const HealthFileName = "update-health.json"

// AccessFileName — backend пише сюди дані активованого пристрою (без
// приватного ключа). Його наявність = активація завершена, cert більше не
// потрібен.
const AccessFileName = "access-device.json"

// Enabled — чи зібрано клієнт із root-ключем (нова система активна).
func Enabled() bool { return len(rootKeys()) > 0 }

func rootKeys() []ed25519.PublicKey { return trust.ParsePublicKeys(RootKeys) }

func embeddedKeyset() ([]byte, string, error) {
	if EmbeddedKeyset == "" || EmbeddedKeysetSig == "" {
		return nil, "", errors.New("вшитого keyset немає")
	}
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(EmbeddedKeyset))
	if err != nil {
		return nil, "", err
	}
	return b, EmbeddedKeysetSig, nil
}

func embeddedCert() []byte {
	if EmbeddedCert == "" {
		return nil
	}
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(EmbeddedCert))
	if err != nil {
		return nil
	}
	return b
}

// now — підміняється в тестах.
var now = func() time.Time { return time.Now().UTC() }
