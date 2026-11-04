package trust

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// ManifestFormat — версія формату launcher-manifest.json (§5.3).
const ManifestFormat = 2

// Імена ассетів релізу GitHub (§13 п.9). Канал beta — окремий файл.
const (
	ManifestAsset     = "launcher-manifest.json"
	ManifestBetaAsset = "launcher-manifest-beta.json"
	KeysetAsset       = "keyset.json"
	SigSuffix         = ".sig"
)

// ManifestAssetFor — ім'я маніфесту каналу.
func ManifestAssetFor(channel string) string {
	if channel == "beta" {
		return ManifestBetaAsset
	}
	return ManifestAsset
}

// Ролі файлів маніфесту. Роль shell клієнт ставить під ІМ'ЯМ СВОГО exe
// (ShaurmLauncher.exe чи ShaurmLauncherBase.exe) — так старі Base-установки
// отримують єдиний лаунчер без перейменування exe й ярликів.
const (
	FileShell           = "shell"
	FileBackend         = "backend"
	FileUpdater         = "updater"
	FileFrontend        = "frontend"
	FileUpdaterFrontend = "updater-frontend"
	FileBootstrap       = "bootstrap"
	FileOther           = "other"
)

// Manifest — підписаний маніфест оновлення. У ньому НЕМАЄ жодних URL:
// адресу клієнт будує сам із Sources.Repo і Tag (§5.4).
type Manifest struct {
	V                  int       `json:"v"`
	Channel            string    `json:"channel"`
	Version            string    `json:"version"`
	MinVersion         string    `json:"min_version,omitempty"`
	Security           bool      `json:"security,omitempty"`
	IssuedAt           time.Time `json:"issued_at"`
	ExpiresAt          time.Time `json:"expires_at"`
	Tag                string    `json:"tag"`
	Commit             string    `json:"commit,omitempty"`
	RunURL             string    `json:"run_url,omitempty"`
	Authenticode       string    `json:"authenticode,omitempty"` // "required" (default) | "waived"
	AuthenticodeReason string    `json:"authenticode_reason,omitempty"`
	Notes              string    `json:"notes,omitempty"`
	Files              []File    `json:"files"`
	KeyID              string    `json:"key_id"`
}

type File struct {
	Name      string `json:"name"`     // ім'я ассета в релізі GitHub
	Platform  string `json:"platform"` // windows-amd64 | linux-amd64
	Role      string `json:"role"`
	Path      string `json:"path"` // відносний шлях у теці лаунчера
	Kind      string `json:"kind,omitempty"`
	ExtractTo string `json:"extract_to,omitempty"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
}

var (
	assetNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	tagRe       = regexp.MustCompile(`^v?[0-9]{1,6}(\.[0-9]{1,6}){1,3}$`)
	platformRe  = regexp.MustCompile(`^(windows|linux)-(amd64|arm64)$`)
	commitRe    = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

// ErrUnknownFormat — критичне поле невідомої версії: не оновлюємось і
// повідомляємо (а не мовчки ігноруємо).
var ErrUnknownFormat = errors.New("невідомий формат маніфесту — оновіть лаунчер вручну")

// ParseManifest перевіряє підпис release-ключем із набору і структуру.
func ParseManifest(ks *Keyset, payload []byte, sigB64 string, now time.Time) (*Manifest, error) {
	if len(payload) > MaxDocSize {
		return nil, errors.New("маніфест завеликий")
	}
	var m Manifest
	if err := json.Unmarshal(trimBOM(payload), &m); err != nil {
		return nil, fmt.Errorf("маніфест: %w", err)
	}
	// Ключ шукаємо за kid з маніфесту, але підпис перевіряється ДО
	// використання будь-якого іншого поля.
	pub, err := ks.Key(RoleRelease, m.KeyID, now)
	if err != nil {
		return nil, fmt.Errorf("маніфест: %w", err)
	}
	if err := Verify(pubSlice(pub), ContextManifest, payload, sigB64); err != nil {
		return nil, fmt.Errorf("маніфест: %w", err)
	}
	if m.V != ManifestFormat {
		return nil, ErrUnknownFormat
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

// Validate — структурна перевірка (захист у глибину поверх підпису).
func (m *Manifest) Validate() error {
	if !ValidVersion(m.Version) {
		return fmt.Errorf("маніфест: некоректна версія %q", m.Version)
	}
	if m.MinVersion != "" && !ValidVersion(m.MinVersion) {
		return fmt.Errorf("маніфест: некоректна min_version %q", m.MinVersion)
	}
	if m.Channel != "stable" && m.Channel != "beta" {
		return fmt.Errorf("маніфест: невідомий канал %q", m.Channel)
	}
	if !tagRe.MatchString(m.Tag) {
		return fmt.Errorf("маніфест: некоректний тег %q", m.Tag)
	}
	if m.Commit != "" && !commitRe.MatchString(m.Commit) {
		return fmt.Errorf("маніфест: некоректний commit")
	}
	switch m.Authenticode {
	case "", "required", "waived":
	default:
		return fmt.Errorf("маніфест: невідоме значення authenticode %q", m.Authenticode)
	}
	if m.IssuedAt.IsZero() || m.ExpiresAt.IsZero() || !m.ExpiresAt.After(m.IssuedAt) {
		return errors.New("маніфест: некоректні issued_at/expires_at")
	}
	if len(m.Files) == 0 {
		return errors.New("маніфест: немає файлів")
	}
	names := map[string]bool{}
	paths := map[string]bool{}
	for _, f := range m.Files {
		if !assetNameRe.MatchString(f.Name) || names[f.Name] {
			return fmt.Errorf("маніфест: некоректне або повторне ім'я файлу %q", f.Name)
		}
		names[f.Name] = true
		if !platformRe.MatchString(f.Platform) {
			return fmt.Errorf("маніфест: %s: невідома платформа %q", f.Name, f.Platform)
		}
		switch f.Role {
		case FileShell, FileBackend, FileUpdater, FileFrontend, FileUpdaterFrontend, FileBootstrap, FileOther:
		default:
			return fmt.Errorf("маніфест: %s: невідома роль %q", f.Name, f.Role)
		}
		if !SafeRelPath(f.Path) {
			return fmt.Errorf("маніфест: %s: небезпечний шлях %q", f.Name, f.Path)
		}
		key := f.Platform + "|" + strings.ToLower(strings.ReplaceAll(f.Path, `\`, "/"))
		if paths[key] {
			return fmt.Errorf("маніфест: повторний шлях %q", f.Path)
		}
		paths[key] = true
		switch f.Kind {
		case "", "file":
		case "archive":
			if !SafeRelPath(f.ExtractTo) {
				return fmt.Errorf("маніфест: %s: небезпечна тека розпакування", f.Name)
			}
		default:
			return fmt.Errorf("маніфест: %s: невідомий kind %q", f.Name, f.Kind)
		}
		if f.Size <= 0 || f.Size > 2<<30 {
			return fmt.Errorf("маніфест: %s: некоректний розмір", f.Name)
		}
		if len(f.SHA256) != 64 {
			return fmt.Errorf("маніфест: %s: sha256 має бути 64 hex", f.Name)
		}
		if _, err := hex.DecodeString(f.SHA256); err != nil {
			return fmt.Errorf("маніфест: %s: sha256 не hex", f.Name)
		}
	}
	return nil
}

// FilesFor — файли однієї платформи.
func (m *Manifest) FilesFor(platform string) []File {
	var out []File
	for _, f := range m.Files {
		if f.Platform == platform {
			out = append(out, f)
		}
	}
	return out
}

// AuthenticodeRequired — за замовчуванням підпис Authenticode обов'язковий.
func (m *Manifest) AuthenticodeRequired() bool { return m.Authenticode != "waived" }

// SafeRelPath — шлях ВСЕРЕДИНІ теки лаунчера (без абсолютних, дисків, «..»).
func SafeRelPath(p string) bool {
	if p == "" || len(p) > 260 || strings.ContainsAny(p, ":\x00") || strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`) {
		return false
	}
	for _, seg := range strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' }) {
		if seg == ".." || seg == "." {
			return false
		}
	}
	return true
}

// UpdatePolicy — стан клієнта, проти якого перевіряється маніфест (§5.4).
type UpdatePolicy struct {
	Channel         string
	CurrentVersion  string
	HighestSeen     string // найвища версія, яку клієнт будь-коли приймав
	SignaledVersion string // версія, яку назвав worker (порожньо — питали GitHub напряму)
	Now             time.Time
}

// Типові причини відмови — окремі, щоб UI пояснював їх по-людськи.
var (
	ErrNotNewer       = errors.New("маніфест не новіший за встановлену версію")
	ErrRollback       = errors.New("спроба відкату на старішу версію відхилена")
	ErrExpired        = errors.New("термін дії маніфесту минув")
	ErrNotYetValid    = errors.New("маніфест видано в майбутньому — перевірте системний час")
	ErrWrongChannel   = errors.New("маніфест іншого каналу")
	ErrSignalMismatch = errors.New("версія в маніфесті не збігається з названою сервером")
	ErrRevoked        = errors.New("версію відкликано")
)

// CheckUpdate — чи можна прийняти цей (уже перевірений підписом) маніфест.
func CheckUpdate(m *Manifest, ks *Keyset, p UpdatePolicy) error {
	if m.Channel != p.Channel {
		return ErrWrongChannel
	}
	if p.SignaledVersion != "" && CompareVersions(m.Version, p.SignaledVersion) != 0 {
		return ErrSignalMismatch
	}
	if p.CurrentVersion != "" && CompareVersions(m.Version, p.CurrentVersion) <= 0 {
		return ErrNotNewer
	}
	if p.HighestSeen != "" && CompareVersions(m.Version, p.HighestSeen) < 0 {
		return ErrRollback
	}
	if p.Now.After(m.ExpiresAt) {
		return ErrExpired
	}
	if m.IssuedAt.After(p.Now.Add(24 * time.Hour)) {
		return ErrNotYetValid
	}
	if ks != nil && ks.IsVersionRevoked(m.Version) {
		return ErrRevoked
	}
	return nil
}

// Required — оновлення обов'язкове: критичне виправлення або поточна версія
// нижча за min_version. Такі оновлення сервер відкласти не може.
func (m *Manifest) Required(current string) bool {
	return m.Security || (m.MinVersion != "" && current != "" && CompareVersions(current, m.MinVersion) < 0)
}

// MaxRolloutDelay — сервер може казати «пізніше» не довше цього від issued_at.
const MaxRolloutDelay = 7 * 24 * time.Hour

// RolloutAllows — чи ставити оновлення зараз з огляду на темп від сервера.
// rollout: "now" | "later" | "pause" (порожньо — як "now").
func RolloutAllows(m *Manifest, rollout, current string, now time.Time) bool {
	if rollout == "" || rollout == "now" {
		return true
	}
	if m.Required(current) {
		return true
	}
	return now.After(m.IssuedAt.Add(MaxRolloutDelay))
}
