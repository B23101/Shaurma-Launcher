package trust

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// KeysetFormat — версія формату keyset.json, яку розуміє цей клієнт.
const KeysetFormat = 1

// MaxDocSize — межа розміру будь-якого підписаного документа (keyset,
// маніфест, cert): захист від «нескінченної» відповіді сервера.
const MaxDocSize = 1 << 20

// Keyset — набір ключів, підписаний root (keyset.json + keyset.json.sig).
// Клієнт приймає лише набір із версією, не меншою за найвищу бачену.
type Keyset struct {
	V        int        `json:"v"`
	Version  int64      `json:"version"`
	IssuedAt time.Time  `json:"issued_at"`
	Keys     []KeyEntry `json:"keys"`
	Revoked  []string   `json:"revoked,omitempty"`
	Sources  Sources    `json:"sources"`
	// AuthenticodeSigners — прийнятні видавці Authenticode (§5.6): новий
	// сертифікат додається сюди заздалегідь, із перекриттям.
	AuthenticodeSigners []AuthenticodeSigner `json:"authenticode_signers,omitempty"`
	// RevokedVersions — версії лаунчера, на які оновлюватись не можна.
	RevokedVersions []string `json:"revoked_versions,omitempty"`
}

type KeyEntry struct {
	KID        string     `json:"kid"`
	Role       string     `json:"role"`
	Alg        string     `json:"alg"`
	Pub        string     `json:"pub"`
	ValidUntil *time.Time `json:"valid_until,omitempty"`
}

// Sources — звідки клієнт бере оновлення. Змінюється лише новим набором
// ключів, підписаним root (переїзд репозиторію чи домену без перевипуску).
type Sources struct {
	Repo       string   `json:"repo"`        // OWNER/REPO на GitHub
	SignalURL  string   `json:"signal_url"`  // https://api.shaurma-launcher.org
	AssetHosts []string `json:"asset_hosts"` // дозволені хости редиректів ассетів GitHub
}

type AuthenticodeSigner struct {
	Subject     string   `json:"subject"`               // CN видавця, напр. "SignPath Foundation"
	Thumbprints []string `json:"thumbprints,omitempty"` // SHA-1 hex листового сертифіката
}

var (
	repoRe = regexp.MustCompile(`^[A-Za-z0-9-]{1,39}/[A-Za-z0-9._-]{1,100}$`)
	hostRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)
)

// ParseKeyset перевіряє підпис root над ТОЧНИМИ байтами і розбирає набір.
func ParseKeyset(rootKeys []ed25519.PublicKey, payload []byte, sigB64 string) (*Keyset, error) {
	if len(payload) > MaxDocSize {
		return nil, errors.New("keyset завеликий")
	}
	if err := Verify(rootKeys, ContextKeyset, payload, sigB64); err != nil {
		return nil, fmt.Errorf("keyset: %w", err)
	}
	var ks Keyset
	if err := json.Unmarshal(trimBOM(payload), &ks); err != nil {
		return nil, fmt.Errorf("keyset: %w", err)
	}
	if ks.V != KeysetFormat {
		return nil, fmt.Errorf("keyset: невідомий формат v=%d (оновіть лаунчер вручну)", ks.V)
	}
	if err := ks.Validate(); err != nil {
		return nil, err
	}
	return &ks, nil
}

// Validate — структурна перевірка (джерела, ключі).
func (ks *Keyset) Validate() error {
	if ks.Version <= 0 {
		return errors.New("keyset: version має бути > 0")
	}
	if !repoRe.MatchString(ks.Sources.Repo) {
		return fmt.Errorf("keyset: некоректний repo %q", ks.Sources.Repo)
	}
	if ks.Sources.SignalURL != "" && !strings.HasPrefix(ks.Sources.SignalURL, "https://") {
		return errors.New("keyset: signal_url має бути https")
	}
	for _, h := range ks.Sources.AssetHosts {
		if !hostRe.MatchString(h) {
			return fmt.Errorf("keyset: некоректний asset host %q", h)
		}
	}
	seen := map[string]bool{}
	for _, k := range ks.Keys {
		if k.KID == "" || seen[k.KID] {
			return fmt.Errorf("keyset: порожній або повторний kid %q", k.KID)
		}
		seen[k.KID] = true
		if k.Alg != AlgEd25519 {
			return fmt.Errorf("keyset: ключ %s має невідомий alg %q", k.KID, k.Alg)
		}
		if k.Role != RoleRelease && k.Role != RoleServer {
			return fmt.Errorf("keyset: ключ %s має невідому роль %q", k.KID, k.Role)
		}
		if _, err := ParsePublicKey(k.Pub); err != nil {
			return fmt.Errorf("keyset: ключ %s: %w", k.KID, err)
		}
	}
	return nil
}

// Key повертає чинний ключ ролі за kid (не відкликаний і не прострочений).
func (ks *Keyset) Key(role, kid string, now time.Time) (ed25519.PublicKey, error) {
	for _, r := range ks.Revoked {
		if r == kid {
			return nil, fmt.Errorf("ключ %s відкликано", kid)
		}
	}
	for _, k := range ks.Keys {
		if k.KID != kid || k.Role != role {
			continue
		}
		if k.ValidUntil != nil && now.After(*k.ValidUntil) {
			return nil, fmt.Errorf("ключ %s прострочено", kid)
		}
		return ParsePublicKey(k.Pub)
	}
	return nil, fmt.Errorf("%w: роль %s, kid %q", ErrNoKey, role, kid)
}

// RoleKeys — усі чинні ключі ролі (для токенів, де kid лише підказка).
func (ks *Keyset) RoleKeys(role string, now time.Time) []ed25519.PublicKey {
	var out []ed25519.PublicKey
	for _, k := range ks.Keys {
		if k.Role != role {
			continue
		}
		if pub, err := ks.Key(role, k.KID, now); err == nil {
			out = append(out, pub)
		}
	}
	return out
}

// IsVersionRevoked — чи заборонена ця версія лаунчера.
func (ks *Keyset) IsVersionRevoked(v string) bool {
	for _, r := range ks.RevokedVersions {
		if CompareVersions(r, v) == 0 {
			return true
		}
	}
	return false
}

// AssetHostAllowed — чи дозволено редирект завантаження на цей хост.
func (ks *Keyset) AssetHostAllowed(host string) bool {
	host = strings.ToLower(host)
	if host == "github.com" {
		return true
	}
	for _, h := range ks.Sources.AssetHosts {
		if host == h {
			return true
		}
	}
	return false
}
