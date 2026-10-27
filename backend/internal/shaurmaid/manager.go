package shaurmaid

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// PowBits — складність proof-of-work при реєстрації (має збігатися з VPS).
const PowBits = 18

var (
	ErrNoCell      = errors.New("shaurmaid: для цього акаунта немає акаунта Шаурми")
	ErrCellExists  = errors.New("shaurmaid: акаунт Шаурми для цього акаунта вже створено")
	ErrWrongDevice = errors.New("shaurmaid: акаунт Шаурми прив'язаний до іншого ПК")
	ErrGone        = errors.New("shaurmaid: сервер не знає цього акаунта Шаурми")
	ErrNoNickname  = errors.New("shaurmaid: некоректний нік")
	ErrNickLocked  = errors.New("shaurmaid: нік ліцензійного акаунта береться лише з Mojang")
	ErrBadType     = errors.New("shaurmaid: некоректний тип акаунта або UUID")
	// ErrDeviceRevoked — цей пристрій вилучено з акаунта (з іншого пристрою або користувачем).
	// Локальні дані НЕ видаляються: користувач може увійти знову, і все повернеться.
	ErrDeviceRevoked = errors.New("shaurmaid: цей пристрій вилучено з акаунта")
	// ErrClockSkew — годинник цього ПК розходиться із сервером (stale_request).
	ErrClockSkew = errors.New("shaurmaid: неправильні дата й час на цьому ПК")
	// ErrNoAccount — для цієї особи (Google/Microsoft) акаунта Шаурми ще немає: треба створити.
	ErrNoAccount = errors.New("shaurmaid: акаунта Шаурми для цього входу ще немає")
)

// APIError — відповідь сервера з кодом (текст не показуємо — лише стабільний код).
type APIError struct {
	Status int
	Code   string
	// RetryAt — unix мс, коли можна повторити (handle_cooldown); 0 — невідомо.
	RetryAt int64
	// Max/Devices — для device_limit: ліміт і діючі пристрої (щоб вибрати, який вилучити).
	Max     int
	Devices []LimitDevice
}

func (e *APIError) Error() string { return fmt.Sprintf("shaurma api: %d %s", e.Status, e.Code) }

// Transport — як дістатись до VPS (у лаунчері це vpsPool із перемиканням вузлів).
// Транспорт додає User-Agent та X-Shaurma-Token; решту заголовків ставить Manager.
type Transport interface {
	Do(ctx context.Context, method, path string, headers map[string]string, body []byte) (status int, resp []byte, err error)
}

type Config struct {
	// ClientVersion / OSName / DeviceName — що показується в екрані «Пристрої» (необов'язкові).
	ClientVersion string
	OSName        string // "windows 11", "linux", "macos"
	DeviceName    func() string
	HWID          HWIDGenerator
	Crypto        CryptoService
	Storage       LocalSecureStorage
	Transport     Transport
	Now           func() time.Time
}

type Manager struct {
	c     Config
	mu    sync.Mutex
	locks map[string]*sync.Mutex
	sess  map[string]*Session // лише в пам'яті
}

func NewManager(c Config) *Manager {
	if c.Now == nil {
		c.Now = time.Now
	}
	return &Manager{c: c, locks: map[string]*sync.Mutex{}, sess: map[string]*Session{}}
}

var _ AuthManager = (*Manager)(nil)

type cellRecord struct {
	AccountID string `json:"accountId"`
	Nickname  string `json:"nickname"`
	Identity  string `json:"identity"` // Identity.Export()
	// Type/UUID/CreatedAt з'явились пізніше: у старих записах порожні → unlicensed.
	Type      string `json:"type,omitempty"`
	UUID      string `json:"uuid,omitempty"`
	CreatedAt int64  `json:"createdAt,omitempty"`
	// DeviceID — id ЦЬОГО пристрою на сервері (з'являється після оновлення; у старих записів порожній).
	DeviceID   string `json:"deviceId,omitempty"`
	DeviceName string `json:"deviceName,omitempty"`
}

func (r *cellRecord) cell() *Cell {
	t := r.Type
	if t != TypeLicensed {
		t = TypeUnlicensed
	}
	u := r.UUID
	if u == "" {
		u = OfflineUUID(r.Nickname)
	}
	return &Cell{AccountID: r.AccountID, Nickname: r.Nickname, Type: t, UUID: u, CreatedAt: r.CreatedAt}
}

func key(launcherID string) string { return "cell:" + launcherID }

func (m *Manager) lock(id string) func() {
	m.mu.Lock()
	l := m.locks[id]
	if l == nil {
		l = &sync.Mutex{}
		m.locks[id] = l
	}
	m.mu.Unlock()
	l.Lock()
	return l.Unlock
}

func (m *Manager) load(id string) (*cellRecord, *Identity, error) {
	raw, err := m.c.Storage.Get(key(id))
	if errors.Is(err, ErrNotFound) {
		return nil, nil, ErrNoCell
	}
	if err != nil {
		return nil, nil, err
	}
	var r cellRecord
	if json.Unmarshal(raw, &r) != nil {
		return nil, nil, ErrNoCell
	}
	ident, err := ParseIdentity([]byte(r.Identity))
	if err != nil {
		return nil, nil, ErrNoCell
	}
	return &r, ident, nil
}

func (m *Manager) save(id string, r *cellRecord) error {
	b, _ := json.Marshal(r)
	return m.c.Storage.Put(key(id), b)
}

func (m *Manager) Cell(id string) (*Cell, bool) {
	r, _, err := m.load(id)
	if err != nil {
		return nil, false
	}
	return r.cell(), true
}

func (m *Manager) call(ctx context.Context, method, path string, body any, hwid, bearer string, out any) error {
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	h := map[string]string{"X-Shaurma-HWID": hwid, "Accept": "application/json"}
	// Опис клієнта для екрана «Пристрої»: лише відображення, безпеки не стосується.
	if m.c.ClientVersion != "" {
		h["X-Shaurma-Client"] = "launcher/" + m.c.ClientVersion
	} else {
		h["X-Shaurma-Client"] = "launcher/0"
	}
	if m.c.OSName != "" {
		h["X-Shaurma-OS"] = m.c.OSName
	}
	if n := m.deviceName(); n != "" {
		h["X-Shaurma-Device-Name"] = base64.RawURLEncoding.EncodeToString([]byte(n))
	}
	if body != nil {
		h["Content-Type"] = "application/json"
	}
	if bearer != "" {
		h["Authorization"] = "Bearer " + bearer
	}
	st, resp, err := m.c.Transport.Do(ctx, method, path, h, raw)
	if err != nil {
		return err
	}
	if st/100 != 2 {
		var e struct {
			Error   string        `json:"error"`
			RetryAt int64         `json:"retryAt"`
			Max     int           `json:"max"`
			Devices []LimitDevice `json:"devices"`
		}
		_ = json.Unmarshal(resp, &e)
		return &APIError{Status: st, Code: e.Error, RetryAt: e.RetryAt, Max: e.Max, Devices: e.Devices}
	}
	if out != nil {
		return json.Unmarshal(resp, out)
	}
	return nil
}

type sessionResp struct {
	AccountID   string `json:"accountId"`
	Nickname    string `json:"nickname"`
	AccessToken string `json:"accessToken"`
	ExpiresInS  int64  `json:"expiresInS"`
	Type        string `json:"type"`
	UUID        string `json:"uuid"`
	DeviceID    string `json:"deviceId"`
	DeviceName  string `json:"deviceName"`
	MaxDevices  int    `json:"maxDevices"`
}

// Recovered — акаунт Шаурми, відновлений за OAuth-особою (новий ПК / перевстановлення).
// Сервер ДОДАВ цей пристрій до акаунта (решта пристроїв лишаються діючими); локально він з'явиться
// після Adopt.
type Recovered struct {
	rec  *cellRecord
	resp sessionResp
}

// Cell — публічний вигляд відновленого акаунта (щоб викликач вирішив, під яким лаунчер-акаунтом
// його зберегти).
func (r *Recovered) Cell() *Cell { return r.rec.cell() }

// DeviceLimitError — в акаунта вже максимум пристроїв. Клієнт показує список і просить обрати,
// який вилучити, після чого повторює вхід через RecoverReplacing.
type DeviceLimitError struct {
	Max     int
	Devices []LimitDevice
}

func (e *DeviceLimitError) Error() string {
	return fmt.Sprintf("shaurmaid: досягнуто ліміт пристроїв (%d)", e.Max)
}

// LimitDevice — пристрій у списку «кого вилучити» (без IP).
type LimitDevice struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	ClientName string `json:"clientName"`
	OS         string `json:"os"`
	LastSeenAt int64  `json:"lastSeenAt"`
	Country    string `json:"country"`
}

// Recover: якщо для цієї особи акаунт Шаурми вже є — ДОДАЄ цей пристрій до акаунта (ключ заготовки p)
// і повертає. Немає — ErrNoAccount (тоді викликач питає нік і робить RegisterWithToken з тим самим
// id_token: другого входу в браузері не потрібно). Ліміт пристроїв — *DeviceLimitError.
func (m *Manager) Recover(ctx context.Context, p *Pending, provider, idToken string) (*Recovered, error) {
	return m.RecoverReplacing(ctx, p, provider, idToken, "")
}

// RecoverReplacing — те саме, але якщо ліміт досягнуто, спершу вилучає пристрій revokeDeviceID
// (обраний користувачем у списку з DeviceLimitError) — одним атомарним кроком.
func (m *Manager) RecoverReplacing(ctx context.Context, p *Pending, provider, idToken, revokeDeviceID string) (*Recovered, error) {
	if p == nil || p.ident == nil || idToken == "" || (provider != "google" && provider != "microsoft") {
		return nil, ErrBadType
	}
	hwid, err := m.c.HWID.HWID()
	if err != nil {
		return nil, err
	}
	ident := p.ident
	ts := m.c.Now().UnixMilli()
	pow, err := m.c.Crypto.SolvePoW(ident.PublicKey, ts, PowBits)
	if err != nil {
		return nil, err
	}
	nonce, err := randomNonce()
	if err != nil {
		return nil, err
	}
	devName := m.deviceName()
	sig, err := m.c.Crypto.Sign(ident, recoverMessageV2(ident.PublicKey, ident.Slot, provider, idToken, ts, nonce, hwid, revokeDeviceID, devName))
	if err != nil {
		return nil, err
	}
	var resp sessionResp
	err = m.call(ctx, "POST", "/api/account/recover", map[string]any{
		"publicKey": ident.PublicKey, "slot": ident.Slot, "provider": provider,
		"idToken": idToken, "ts": ts, "nonce": nonce, "pow": pow, "sig": sig,
		"v": 2, "revokeDeviceId": revokeDeviceID, "deviceName": devName,
	}, hwid, "", &resp)
	if err != nil {
		var ae *APIError
		if errors.As(err, &ae) {
			switch {
			case ae.Status == 404 && ae.Code == "no_account":
				return nil, ErrNoAccount
			case ae.Status == 409 && ae.Code == "device_limit":
				return nil, &DeviceLimitError{Max: ae.Max, Devices: ae.Devices}
			case ae.Code == "stale_request":
				return nil, ErrClockSkew
			}
		}
		return nil, err
	}
	typ := resp.Type
	if typ != TypeLicensed {
		typ = TypeUnlicensed
	}
	uuid := NormalizeUUID(resp.UUID)
	if uuid == "" {
		uuid = OfflineUUID(resp.Nickname)
	}
	rec := &cellRecord{AccountID: resp.AccountID, Nickname: resp.Nickname, Identity: string(ident.Export()),
		Type: typ, UUID: uuid, CreatedAt: m.c.Now().UnixMilli(), DeviceID: resp.DeviceID, DeviceName: resp.DeviceName}
	return &Recovered{rec: rec, resp: resp}, nil
}

// deviceName — назва цього пристрою для сервера (≤ 40 символів; порожньо → сервер придумає сам).
func (m *Manager) deviceName() string {
	if m.c.DeviceName == nil {
		return ""
	}
	n := strings.TrimSpace(m.c.DeviceName())
	if r := []rune(n); len(r) > 40 {
		n = string(r[:40])
	}
	return n
}

// Adopt зберігає відновлений акаунт локально під ID лаунчер-акаунта. Стару комірку НЕ стирає
// мовчки: її копія лежить у "cell-bak:<id>" (ключ старого пристрою; сервер його міг уже
// вилучити, але «ніщо не зникає без архіву»).
func (m *Manager) Adopt(id string, r *Recovered) error {
	if r == nil || r.rec == nil {
		return ErrBadType
	}
	defer m.lock(id)()
	if old, err := m.c.Storage.Get(key(id)); err == nil && len(old) > 0 {
		if err := m.c.Storage.Put(bakKey(id), old); err != nil {
			return err // не вдалось заархівувати — не перезаписуємо
		}
	}
	if err := m.save(id, r.rec); err != nil {
		return err
	}
	m.remember(id, r.resp)
	return nil
}

func bakKey(id string) string { return "cell-bak:" + id }

// NickCheck — відповідь сервера на «чи можна взяти цей нік».
type NickCheck struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason"` // "" | invalid | taken | mojang
}

// NickAvailable питає сервер про нік БЕЗ автентифікації (для підказки під час вводу).
func (m *Manager) NickAvailable(ctx context.Context, nick string) (*NickCheck, error) {
	hwid, _ := m.c.HWID.HWID()
	var out NickCheck
	if err := m.call(ctx, "POST", "/api/account/nick-check", map[string]string{"nickname": strings.TrimSpace(nick)}, hwid, "", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (m *Manager) remember(id string, s sessionResp) *Session {
	ss := &Session{AccountID: s.AccountID, Nickname: s.Nickname, AccessToken: s.AccessToken, DeviceID: s.DeviceID,
		ExpiresAt: m.c.Now().Add(time.Duration(s.ExpiresInS) * time.Second)}
	m.mu.Lock()
	m.sess[id] = ss
	m.mu.Unlock()
	return ss
}

var nickOK = func(n string) bool {
	if n == "" || len(n) > 16 {
		return false
	}
	for _, c := range n {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}

// Register — окремий вхід: створює комірку, просить у src id_token із nonce цієї комірки
// (Google або Microsoft) і реєструє акаунт. Для входу в ліцензійний акаунт використовуйте
// Prepare + RegisterWithToken: id_token тоді приходить із того самого входу в Microsoft.
func (m *Manager) Register(ctx context.Context, id string, opts RegisterOpts, src IDTokenSource) (*Cell, error) {
	p, err := m.Prepare()
	if err != nil {
		return nil, err
	}
	// OAuth: лаунчер сам отримує id_token (scope openid) з nonce, прив'язаним до ключа комірки.
	idToken, err := src(ctx, opts.Provider, p.Nonce())
	if err != nil {
		return nil, err
	}
	return m.RegisterWithToken(ctx, id, opts, p, idToken)
}

// Prepare створює ключ комірки й nonce для OAuth. Нічого не пише ні на диск, ні на сервер.
func (m *Manager) Prepare() (*Pending, error) {
	ident, err := m.c.Crypto.NewIdentity()
	if err != nil {
		return nil, err
	}
	return &Pending{ident: ident, nonce: OIDCNonce(ident.PublicKey, ident.Slot)}, nil
}

// RegisterWithToken реєструє акаунт, використовуючи id_token, отриманий із nonce заготовки p.
func (m *Manager) RegisterWithToken(ctx context.Context, id string, opts RegisterOpts, p *Pending, idToken string) (*Cell, error) {
	defer m.lock(id)()
	if p == nil || p.ident == nil || idToken == "" {
		return nil, ErrBadType
	}
	nickname := strings.TrimSpace(opts.Nickname)
	if !nickOK(nickname) {
		return nil, ErrNoNickname
	}
	typ, uuid := opts.Type, ""
	switch typ {
	case TypeLicensed:
		if uuid = NormalizeUUID(opts.UUID); uuid == "" {
			return nil, ErrBadType
		}
	case TypeUnlicensed:
		uuid = OfflineUUID(nickname)
	default:
		return nil, ErrBadType
	}
	if _, _, err := m.load(id); err == nil {
		return nil, ErrCellExists
	}
	hwid, err := m.c.HWID.HWID()
	if err != nil {
		return nil, err
	}
	ident := p.ident
	ts := m.c.Now().UnixMilli()
	pow, err := m.c.Crypto.SolvePoW(ident.PublicKey, ts, PowBits)
	if err != nil {
		return nil, err
	}
	nonce, err := randomNonce()
	if err != nil {
		return nil, err
	}
	sig, err := m.c.Crypto.Sign(ident, registerMessage(nickname, ident.PublicKey, ident.Slot, opts.Provider, typ, uuid, idToken, ts, nonce, hwid))
	if err != nil {
		return nil, err
	}
	var resp sessionResp
	err = m.call(ctx, "POST", "/api/account/register", map[string]any{
		"nickname": nickname, "publicKey": ident.PublicKey, "slot": ident.Slot, "provider": opts.Provider,
		"type": typ, "uuid": uuid,
		"idToken": idToken, "ts": ts, "nonce": nonce, "pow": pow, "sig": sig,
	}, hwid, "", &resp)
	idToken = "" // id_token більше не потрібен і ніде не зберігається
	if err != nil {
		return nil, err
	}
	// Для ліцензійного нік у відповіді — з Mojang (сервер його перевірив), він і зберігається.
	rec := &cellRecord{AccountID: resp.AccountID, Nickname: resp.Nickname, Identity: string(ident.Export()),
		Type: typ, UUID: uuid, CreatedAt: m.c.Now().UnixMilli()}
	if err := m.save(id, rec); err != nil {
		// Акаунт на сервері вже є, а ключ зберегти не вдалось — прибираємо його, щоб не лишати сироту.
		_ = m.call(ctx, "POST", "/api/account/delete", nil, hwid, resp.AccessToken, nil)
		return nil, err
	}
	m.remember(id, resp)
	return rec.cell(), nil
}

func (m *Manager) Session(ctx context.Context, id string) (*Session, error) {
	defer m.lock(id)()
	m.mu.Lock()
	cur := m.sess[id]
	m.mu.Unlock()
	if cur != nil && cur.ExpiresAt.Sub(m.c.Now()) > 2*time.Minute {
		c := *cur
		return &c, nil
	}
	rec, ident, err := m.load(id)
	if err != nil {
		return nil, err
	}
	hwid, err := m.c.HWID.HWID()
	if err != nil {
		return nil, err
	}
	nonce, err := randomNonce()
	if err != nil {
		return nil, err
	}
	ts := m.c.Now().UnixMilli()
	sig, err := m.c.Crypto.Sign(ident, loginMessage(rec.AccountID, hwid, ts, nonce))
	if err != nil {
		return nil, err
	}
	var resp sessionResp
	loginBody := map[string]any{"accountId": rec.AccountID, "ts": ts, "nonce": nonce, "sig": sig}
	if rec.DeviceID != "" {
		loginBody["deviceId"] = rec.DeviceID
	}
	if err := m.call(ctx, "POST", "/api/account/login", loginBody, hwid, "", &resp); err != nil {
		var ae *APIError
		if errors.As(err, &ae) {
			switch {
			case ae.Status == 403 && ae.Code == "hwid_mismatch":
				return nil, ErrWrongDevice
			case ae.Status == 401 && ae.Code == "device_revoked":
				return nil, ErrDeviceRevoked
			case ae.Status == 401:
				return nil, ErrGone
			case ae.Code == "stale_request":
				return nil, ErrClockSkew
			}
		}
		return nil, err // мережа/5xx/ліміти: НЕ вихід — локальні дані й сесія лишаються
	}
	if resp.DeviceID != "" && (rec.DeviceID != resp.DeviceID || rec.DeviceName != resp.DeviceName) {
		rec.DeviceID, rec.DeviceName = resp.DeviceID, resp.DeviceName
		_ = m.save(id, rec)
	}
	if resp.Nickname != "" && resp.Nickname != rec.Nickname {
		rec.Nickname = resp.Nickname
		_ = m.save(id, rec)
	}
	c := *m.remember(id, resp)
	return &c, nil
}

func (m *Manager) SetNickname(ctx context.Context, id, nickname string) error {
	if c, ok := m.Cell(id); ok && c.Licensed() {
		return ErrNickLocked // нік ліцензійного — лише з Mojang (сервер теж відмовить)
	}
	nickname = strings.TrimSpace(nickname)
	if !nickOK(nickname) {
		return ErrNoNickname
	}
	s, err := m.Session(ctx, id)
	if err != nil {
		return err
	}
	hwid, _ := m.c.HWID.HWID()
	if err := m.call(ctx, "POST", "/api/account/nickname", map[string]string{"nickname": nickname}, hwid, s.AccessToken, nil); err != nil {
		return err
	}
	defer m.lock(id)()
	if rec, _, err := m.load(id); err == nil {
		rec.Nickname = nickname
		if rec.UUID != "" && rec.Type != TypeLicensed {
			rec.UUID = OfflineUUID(nickname) // UUID неліцензійного — функція ніка
		}
		_ = m.save(id, rec)
	}
	m.mu.Lock()
	if cur := m.sess[id]; cur != nil {
		cur.Nickname = nickname
	}
	m.mu.Unlock()
	return nil
}

// SetHandle — змінює @handle. Формат перевіряє сервер (VPS accounts); тут лише відсікаємо порожнє.
func (m *Manager) SetHandle(ctx context.Context, id, handle string) error {
	handle = strings.TrimSpace(handle)
	if handle == "" {
		return &APIError{Status: 400, Code: "invalid_handle"}
	}
	s, err := m.Session(ctx, id)
	if err != nil {
		return err
	}
	hwid, _ := m.c.HWID.HWID()
	return m.call(ctx, "POST", "/api/account/handle", map[string]string{"handle": handle}, hwid, s.AccessToken, nil)
}

// SetProfileStyle — зберігає біо й колір фону профілю. Обидва поля йдуть разом: так форма у
// лаунчері завжди відображає повний стан, а «очистити» — це просто порожній рядок.
func (m *Manager) SetProfileStyle(ctx context.Context, id, bio, color string) error {
	s, err := m.Session(ctx, id)
	if err != nil {
		return err
	}
	hwid, _ := m.c.HWID.HWID()
	return m.call(ctx, "POST", "/api/account/profile", map[string]string{"bio": bio, "color": color}, hwid, s.AccessToken, nil)
}

// SyncNickname — ліцензійний акаунт: сервер сам запитує Mojang за UUID і повертає актуальне
// ім'я (клієнт нікого не «призначає»). Неліцензійному повертає поточний нік без запитів.
func (m *Manager) SyncNickname(ctx context.Context, id string) (string, error) {
	c, ok := m.Cell(id)
	if !ok {
		return "", ErrNoCell
	}
	if !c.Licensed() {
		return c.Nickname, nil
	}
	s, err := m.Session(ctx, id)
	if err != nil {
		return "", err
	}
	hwid, _ := m.c.HWID.HWID()
	var out struct {
		Nickname string `json:"nickname"`
	}
	if err := m.call(ctx, "POST", "/api/account/nickname/sync", map[string]string{}, hwid, s.AccessToken, &out); err != nil {
		return "", err
	}
	if out.Nickname == "" {
		return c.Nickname, nil
	}
	defer m.lock(id)()
	if rec, _, err := m.load(id); err == nil && rec.Nickname != out.Nickname {
		rec.Nickname = out.Nickname
		_ = m.save(id, rec)
	}
	m.mu.Lock()
	if cur := m.sess[id]; cur != nil {
		cur.Nickname = out.Nickname
	}
	m.mu.Unlock()
	return out.Nickname, nil
}

// Profile — свіжий профіль з сервера (тип, UUID, залишок змін ніка, дата створення).
func (m *Manager) Profile(ctx context.Context, id string) (*Profile, error) {
	s, err := m.Session(ctx, id)
	if err != nil {
		return nil, err
	}
	hwid, _ := m.c.HWID.HWID()
	var p Profile
	if err := m.call(ctx, "GET", "/api/account/me", nil, hwid, s.AccessToken, &p); err != nil {
		return nil, err
	}
	// Нік на сервері міг змінитись (синхронізація з Mojang) — тримаємо локальну копію актуальною.
	if p.Nickname != "" {
		defer m.lock(id)()
		if rec, _, err := m.load(id); err == nil && rec.Nickname != p.Nickname {
			rec.Nickname = p.Nickname
			_ = m.save(id, rec)
		}
	}
	return &p, nil
}

func (m *Manager) Delete(ctx context.Context, id string) error {
	s, err := m.Session(ctx, id)
	if err != nil && !errors.Is(err, ErrGone) && !errors.Is(err, ErrNoCell) {
		return err // сервер недосяжний чи чужий ПК: нічого не стираємо мовчки
	}
	if s != nil {
		hwid, _ := m.c.HWID.HWID()
		if err := m.call(ctx, "POST", "/api/account/delete", nil, hwid, s.AccessToken, nil); err != nil {
			return err
		}
	}
	return m.Forget(id)
}

// Invalidate скидає кешовану сесію (сервер відповів 401): наступний Session зробить новий вхід підписом.
func (m *Manager) Invalidate(id string) {
	m.mu.Lock()
	delete(m.sess, id)
	m.mu.Unlock()
}

func (m *Manager) Forget(id string) error {
	defer m.lock(id)()
	m.mu.Lock()
	delete(m.sess, id)
	m.mu.Unlock()
	return m.c.Storage.Delete(key(id)) // cell-bak:<id> лишається навмисно
}
