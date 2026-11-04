package trust

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

func pubSlice(k ed25519.PublicKey) []ed25519.PublicKey { return []ed25519.PublicKey{k} }

// ---- компактний підписаний токен ------------------------------------------
//
// Формат: base64url(payload) "." base64url(sig) — придатний для заголовка
// HTTP. Використовується для X-Shaurma-Entitlement і відповіді активації.

// SignCompact підписує JSON-payload і повертає компактний токен.
func SignCompact(priv ed25519.PrivateKey, context string, payload []byte) string {
	sig := ed25519.Sign(priv, message(context, payload))
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// VerifyCompact перевіряє токен і повертає payload.
func VerifyCompact(keys []ed25519.PublicKey, context, token string) ([]byte, error) {
	if len(token) > 8192 {
		return nil, ErrBadSignature
	}
	p, s, ok := strings.Cut(token, ".")
	if !ok {
		return nil, ErrBadSignature
	}
	payload, err := base64.RawURLEncoding.DecodeString(p)
	if err != nil {
		return nil, ErrBadSignature
	}
	if err := Verify(keys, context, payload, s); err != nil {
		return nil, err
	}
	return payload, nil
}

// Entitlement — короткоживучий токен доступу до приватних збірок (§4).
// Видає новий worker, перевіряє старий (stateless, за server-ключем).
type Entitlement struct {
	V        int    `json:"v"`
	DeviceID string `json:"device_id"`
	Epoch    int    `json:"epoch"`
	Tier     string `json:"tier"` // "shaurma"
	IAT      int64  `json:"iat"`
	EXP      int64  `json:"exp"`
	KID      string `json:"kid"`
}

// ParseEntitlement перевіряє підпис server-ключем і термін.
func ParseEntitlement(keys []ed25519.PublicKey, token string, now time.Time) (*Entitlement, error) {
	payload, err := VerifyCompact(keys, ContextEntitlement, token)
	if err != nil {
		return nil, err
	}
	var e Entitlement
	if err := json.Unmarshal(payload, &e); err != nil || e.V != 1 {
		return nil, errors.New("токен доступу: невідомий формат")
	}
	if now.Unix() >= e.EXP {
		return nil, errors.New("токен доступу прострочено")
	}
	return &e, nil
}

// ---- активація (§3.4) --------------------------------------------------------

// ActivationRequest — тіло POST /v1/activate.
type ActivationRequest struct {
	V           int    `json:"v"`
	Cert        string `json:"cert"`       // вміст master.cert як є
	DevicePub   string `json:"device_pub"` // base64 Ed25519
	Nonce       string `json:"nonce"`      // від сервера (GET /v1/activate/nonce)
	NonceSig    string `json:"nonce_sig"`  // підпис nonce ключем пристрою — доказ володіння
	Identity    string `json:"identity,omitempty"`
	LauncherVer string `json:"launcher_ver"`
	OS          string `json:"os"`
}

// ActivationProofMessage — що саме підписує пристрій для доказу володіння.
func ActivationProofMessage(nonce, devicePub string) []byte {
	return []byte("activate\n" + nonce + "\n" + devicePub)
}

// ActivationResult — підписаний сервером payload відповіді активації.
type ActivationResult struct {
	V         int    `json:"v"`
	Status    string `json:"status"` // "active" | "pending"
	DeviceID  string `json:"device_id"`
	DevicePub string `json:"device_pub"`
	Nonce     string `json:"nonce"`
	Epoch     int    `json:"epoch"`
	IssuedAt  int64  `json:"iat"`
}

// ParseActivation перевіряє підпис сервера і прив'язку до запиту: підроблене
// «200 OK» не повинно змусити лаунчер видалити сертифікат (§3.4а).
func ParseActivation(keys []ed25519.PublicKey, token, wantNonce, wantDevicePub string) (*ActivationResult, error) {
	payload, err := VerifyCompact(keys, ContextActivation, token)
	if err != nil {
		return nil, fmt.Errorf("відповідь активації: %w", err)
	}
	var r ActivationResult
	if err := json.Unmarshal(payload, &r); err != nil || r.V != 1 {
		return nil, errors.New("відповідь активації: невідомий формат")
	}
	if r.Nonce != wantNonce || r.DevicePub != wantDevicePub {
		return nil, errors.New("відповідь активації не відповідає запиту")
	}
	if r.Status != "active" && r.Status != "pending" {
		return nil, fmt.Errorf("відповідь активації: невідомий статус %q", r.Status)
	}
	return &r, nil
}

// ---- підписані запити пристрою (§3.5) ---------------------------------------

// DeviceRequestMessage — канонічний рядок, який підписує пристрій для кожного
// запиту: метод, шлях, час (unix), випадковий nonce і SHA-256 тіла.
func DeviceRequestMessage(method, path string, ts int64, nonce string, body []byte) []byte {
	sum := sha256.Sum256(body)
	return []byte(fmt.Sprintf("%s\n%s\n%d\n%s\n%s", strings.ToUpper(method), path, ts, nonce, hex.EncodeToString(sum[:])))
}

// ---- сигнал від worker (§5А) -------------------------------------------------

// Signal — відповідь /v1/update/check. Лише ім'я версії й темп, БЕЗ адрес.
type Signal struct {
	Version  string `json:"version"`
	Tag      string `json:"tag"`
	Rollout  string `json:"rollout,omitempty"` // now | later | pause
	Required bool   `json:"required,omitempty"`
}

// Validate відкидає сигнал із некоректними полями (клієнт тоді йде на GitHub).
func (s *Signal) Validate() error {
	if !ValidVersion(s.Version) || !tagRe.MatchString(s.Tag) {
		return errors.New("некоректний сигнал оновлення")
	}
	switch s.Rollout {
	case "", "now", "later", "pause":
	default:
		return errors.New("некоректний rollout")
	}
	return nil
}

// SignedSignal — те, що Pack Manager передає новому worker після публікації
// релізу (компактний токен, підписаний release-ключем, ContextSignal).
// Worker перевіряє підпис і лише тоді змінює відповідь /v1/update/check.
type SignedSignal struct {
	V        int    `json:"v"`
	Channel  string `json:"channel"`
	Version  string `json:"version"`
	Tag      string `json:"tag"`
	Rollout  string `json:"rollout,omitempty"`
	Required bool   `json:"required,omitempty"`
	IAT      int64  `json:"iat"`
}

// ---- замовлення збірки (§5.1) -----------------------------------------------

// BuildOrder — що Pack Manager замовляє зібрати на GitHub.
type BuildOrder struct {
	V       int    `json:"v"`
	Version string `json:"version"`
	SHA     string `json:"sha"`
	Nonce   string `json:"nonce"`
	Expires int64  `json:"expires"`
}

// ParseOrder перевіряє підписане замовлення (компактний токен).
func ParseOrder(keys []ed25519.PublicKey, token string, now time.Time) (*BuildOrder, error) {
	payload, err := VerifyCompact(keys, ContextOrder, token)
	if err != nil {
		return nil, err
	}
	var o BuildOrder
	if err := json.Unmarshal(payload, &o); err != nil || o.V != 1 {
		return nil, errors.New("замовлення: невідомий формат")
	}
	if !ValidVersion(o.Version) || !commitRe.MatchString(o.SHA) {
		return nil, errors.New("замовлення: некоректні version/sha")
	}
	if now.Unix() > o.Expires {
		return nil, errors.New("замовлення прострочене")
	}
	return &o, nil
}
