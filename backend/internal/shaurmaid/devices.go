package shaurmaid

import (
	"context"
	"errors"
	"net/url"
	"strconv"
)

// Пристрої акаунта й ключі чату (серверні конверти). Усе — через автентифікований виклик;
// 401 device_revoked перетворюється на ErrDeviceRevoked (локальні дані при цьому НЕ чіпаються).

var (
	// ErrNoChatKey — на сервері ще немає ключа чату (лише тоді клієнт створює перший).
	ErrNoChatKey = errors.New("shaurmaid: ключа чату на сервері ще немає")
	// ErrChatKeyDisabled — сервер вимкнув серверні конверти (старий сервер / не налаштовано).
	// Клієнт працює зі своїм локальним ключем, як раніше.
	ErrChatKeyDisabled = errors.New("shaurmaid: серверні ключі чату недоступні")
	// ErrChatKeyUnavailable — конверт є, але сервер тимчасово не може його прочитати. НІКОЛИ не
	// привід створювати новий ключ: це затерло б історію.
	ErrChatKeyUnavailable = errors.New("shaurmaid: сервер тимчасово не може видати ключ чату")
)

// Device — пристрій акаунта (екран «Пристрої»). Поля збігаються з відповіддю VPS.
type Device struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	ClientKind     string `json:"clientKind"` // launcher | browser | unknown
	ClientName     string `json:"clientName"` // «Shaurma Launcher», «Chrome 126»
	ClientVersion  string `json:"clientVersion"`
	OS             string `json:"os"`
	CreatedAt      int64  `json:"createdAt"`
	LastSeenAt     int64  `json:"lastSeenAt"`
	CreatedIP      string `json:"createdIp"`
	LastIP         string `json:"lastIp"`
	CreatedCountry string `json:"createdCountry"`
	LastCountry    string `json:"lastCountry"`
	Status         string `json:"status"` // active | revoked
	RevokedAt      int64  `json:"revokedAt"`
	RevokeReason   string `json:"revokeReason"`
	Current        bool   `json:"current"`
	Online         bool   `json:"online"`
}

// DeviceList — відповідь екрана «Пристрої».
type DeviceList struct {
	Devices      []Device `json:"devices"`
	Current      string   `json:"current"`
	Max          int      `json:"max"`
	RestoreUntil int64    `json:"restoreUntil"` // до коли діє «Повернути пристрої» (0 — нічого)
}

// KeyRotation — після «Це не я»/екстреної ротації: клієнт має створити нову версію ключа чату.
type KeyRotation struct {
	RotateKey   bool `json:"rotateKey"`
	NextVersion int  `json:"nextVersion"`
	Revoked     int  `json:"revoked"`
}

// RevokeOthersResult — відповідь «Вийти на всіх, крім цього».
type RevokeOthersResult struct {
	Revoked      int   `json:"revoked"`
	RestoreUntil int64 `json:"restoreUntil"`
}

// AuditEvent — подія журналу акаунта.
type AuditEvent struct {
	ID       int64  `json:"id"`
	DeviceID string `json:"deviceId"`
	ActorID  string `json:"actorId"`
	Event    string `json:"event"`
	Detail   string `json:"detail"`
	IP       string `json:"ip"`
	Country  string `json:"country"`
	At       int64  `json:"at"`
}

// authCall — автентифікований запит із перетворенням помилок пристрою.
func (m *Manager) authCall(ctx context.Context, id, method, path string, body, out any) error {
	s, err := m.Session(ctx, id)
	if err != nil {
		return err
	}
	hwid, _ := m.c.HWID.HWID()
	err = m.call(ctx, method, path, body, hwid, s.AccessToken, out)
	var ae *APIError
	if errors.As(err, &ae) && ae.Status == 401 && ae.Code == "device_revoked" {
		m.Invalidate(id)
		return ErrDeviceRevoked
	}
	return err
}

// Devices — список пристроїв акаунта.
func (m *Manager) Devices(ctx context.Context, id string) (*DeviceList, error) {
	var out DeviceList
	if err := m.authCall(ctx, id, "GET", "/api/account/devices", nil, &out); err != nil {
		return nil, err
	}
	if out.Devices == nil {
		out.Devices = []Device{}
	}
	return &out, nil
}

func (m *Manager) RenameDevice(ctx context.Context, id, deviceID, name string) error {
	return m.authCall(ctx, id, "POST", "/api/account/devices/rename", map[string]string{"deviceId": deviceID, "name": name}, nil)
}

// RevokeDevice вилучає пристрій. notMe=true («Це не я») → відповідь радить ротацію ключа чату.
func (m *Manager) RevokeDevice(ctx context.Context, id, deviceID string, notMe bool) (*KeyRotation, error) {
	path := "/api/account/devices/revoke"
	if notMe {
		path = "/api/account/devices/not-me"
	}
	var out KeyRotation
	if err := m.authCall(ctx, id, "POST", path, map[string]string{"deviceId": deviceID}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RevokeOthers — «Вийти на всіх, крім цього». Діє вікно «Повернути пристрої» (RestoreUntil).
func (m *Manager) RevokeOthers(ctx context.Context, id string) (*RevokeOthersResult, error) {
	var out RevokeOthersResult
	if err := m.authCall(ctx, id, "POST", "/api/account/devices/revoke-others", map[string]string{}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RestoreDevices — «Повернути пристрої» (10 хв після RevokeOthers). Повертає, скільки повернуто.
func (m *Manager) RestoreDevices(ctx context.Context, id string) (int, error) {
	var out struct {
		Restored int `json:"restored"`
	}
	if err := m.authCall(ctx, id, "POST", "/api/account/devices/restore", map[string]string{}, &out); err != nil {
		return 0, err
	}
	return out.Restored, nil
}

// EmergencyReset — «Мій акаунт скомпрометовано»: сервер вилучає всі інші пристрої; клієнт далі
// створює нову версію ключа чату (KeyRotation.RotateKey).
func (m *Manager) EmergencyReset(ctx context.Context, id string) (*KeyRotation, error) {
	var out KeyRotation
	if err := m.authCall(ctx, id, "POST", "/api/account/emergency-reset", map[string]string{}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Audit — журнал подій акаунта (новіші першими).
func (m *Manager) Audit(ctx context.Context, id string, limit int) ([]AuditEvent, error) {
	q := url.Values{}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	path := "/api/account/audit"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	var out struct {
		Events []AuditEvent `json:"events"`
	}
	if err := m.authCall(ctx, id, "GET", path, nil, &out); err != nil {
		return nil, err
	}
	if out.Events == nil {
		out.Events = []AuditEvent{}
	}
	return out.Events, nil
}

// DeviceID — id цього пристрою (з локального запису; "" — ще невідомий).
func (m *Manager) DeviceID(id string) string {
	if rec, _, err := m.load(id); err == nil {
		return rec.DeviceID
	}
	return ""
}

// ── Ключі чату ────────────────────────────────────────────────────────────

// ChatKey — одна версія ключа чату. SecretKey — стандартний base64 (32 байти). Приватний ключ
// не логується й не показується в UI.
type ChatKey struct {
	Version       int    `json:"version"`
	ChatPublicKey string `json:"chatPublicKey"`
	SecretKey     string `json:"secretKey"`
	CreatedAt     int64  `json:"createdAt"`
	Current       bool   `json:"current"`
}

// ChatKeys — усі версії ключа чату акаунта (keyring).
type ChatKeys struct {
	Keys    []ChatKey `json:"keys"`
	Current int       `json:"current"`
}

// ChatKeysConflict — інший пристрій уже створив цю версію ключа («перший виграє»): усиновіть Keys.
type ChatKeysConflict struct{ Keys *ChatKeys }

func (e *ChatKeysConflict) Error() string {
	return "shaurmaid: ключ чату вже створено іншим пристроєм"
}

func mapChatKeyErr(err error) error {
	var ae *APIError
	if !errors.As(err, &ae) {
		return err
	}
	switch {
	case ae.Status == 404 && ae.Code == "no_chat_key":
		return ErrNoChatKey
	case ae.Status == 503 && ae.Code == "chat_key_disabled":
		return ErrChatKeyDisabled
	case ae.Status == 503 && ae.Code == "key_unavailable":
		return ErrChatKeyUnavailable
	case ae.Status == 404 || ae.Status == 405:
		return ErrChatKeyDisabled // старий сервер без цього ендпоінта
	}
	return err
}

// GetChatKeys — ключ(і) чату акаунта з сервера. ErrNoChatKey — ключа ще нема (створюйте перший);
// ErrChatKeyDisabled/ErrChatKeyUnavailable — НЕ створюйте новий: працюйте з локальним.
func (m *Manager) GetChatKeys(ctx context.Context, id string) (*ChatKeys, error) {
	var out ChatKeys
	if err := m.authCall(ctx, id, "GET", "/api/account/chat-key", nil, &out); err != nil {
		return nil, mapChatKeyErr(err)
	}
	return &out, nil
}

// PutChatKey зберігає нову версію ключа. Інший пристрій випередив — *ChatKeysConflict з його ключами.
func (m *Manager) PutChatKey(ctx context.Context, id string, version int, chatPublicKey, secretKey string) (*ChatKeys, error) {
	var out ChatKeys
	err := m.authCall(ctx, id, "POST", "/api/account/chat-key",
		map[string]any{"version": version, "chatPublicKey": chatPublicKey, "secretKey": secretKey}, &out)
	if err == nil {
		return &out, nil
	}
	var ae *APIError
	if errors.As(err, &ae) && ae.Status == 409 && ae.Code == "key_exists" {
		// Тіло 409 несе актуальні ключі; call() його не розбирає — перечитаємо GET'ом.
		keys, gerr := m.GetChatKeys(ctx, id)
		if gerr != nil {
			return nil, gerr
		}
		return nil, &ChatKeysConflict{Keys: keys}
	}
	return nil, mapChatKeyErr(err)
}
