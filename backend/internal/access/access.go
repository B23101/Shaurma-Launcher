// Package access — активація приватного доступу єдиного лаунчера (план
// «Єдиний лаунчер», §3).
//
//  1. master.cert шукається біля лаунчера ЗА ВМІСТОМ (не за ім'ям) і
//     перевіряється root-ключем. Чужий/битий файл на сервер не йде.
//  2. Лаунчер генерує ключ пристрою Ed25519 (приватна частина — DPAPI).
//  3. Активація: nonce від сервера → підпис nonce ключем пристрою (доказ
//     володіння) → POST /v1/activate → відповідь, ПІДПИСАНА server-ключем із
//     keyset (підроблене «200 OK» не пройде).
//  4. Лише після збереженого й перечитаного access-device.json сертифікат
//     видаляється з теки лаунчера. Мережа/5xx — cert лишається, повтор пізніше.
//  5. Далі кожен запит до сервера підписується ключем пристрою; сервер видає
//     короткоживучий токен доступу (X-Shaurma-Entitlement) для старого worker.
package access

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"shaurma-trust"
)

// Вшиваються при збірці (-X shaurma-launcher-backend/internal/access.<Var>=...).
var (
	RootKeys          = ""
	EmbeddedKeyset    = ""
	EmbeddedKeysetSig = ""
)

// DefaultServer — новий worker; keyset.Sources.SignalURL має пріоритет.
const DefaultServer = "https://api.shaurma-launcher.org"

// Файли в теці лаунчера. access-device.json — контракт з оновлювачем
// (newupdate.AccessFileName): його наявність = активацію завершено.
const (
	DeviceFile = "access-device.json"
	KeyFile    = "access-key.bin"
	CertName   = "master.cert"
)

// OfflineTTL — скільки показувати приватний розділ без зв'язку з сервером.
const OfflineTTL = 7 * 24 * time.Hour

// Стани для UI.
const (
	StateNone    = "none"
	StateActive  = "active"
	StatePending = "pending"
	StateRevoked = "revoked"
)

// Status — те, що бачить користувач у Налаштуваннях → Доступ.
type Status struct {
	State       string `json:"state"`
	DeviceID    string `json:"deviceId,omitempty"` // короткий, щоб людина могла назвати його розробнику
	ActivatedAt string `json:"activatedAt,omitempty"`
	Epoch       int    `json:"epoch,omitempty"`
	CertFound   bool   `json:"certFound"`
	WeakStorage bool   `json:"weakStorage,omitempty"`
	Message     string `json:"message,omitempty"`
	Enabled     bool   `json:"enabled"` // збірка має root-ключ (нова система)
}

type deviceRecord struct {
	V           int       `json:"v"`
	DeviceID    string    `json:"device_id"`
	DevicePub   string    `json:"device_pub"`
	Status      string    `json:"status"`
	Epoch       int       `json:"epoch"`
	ActivatedAt time.Time `json:"activated_at"`
	LastOK      time.Time `json:"last_ok"`
	Weak        bool      `json:"weak,omitempty"`
}

// Manager — один на процес backend.
type Manager struct {
	dir     string
	extra   []string // інші теки пошуку cert (біля exe)
	client  *http.Client
	version string

	mu       sync.Mutex
	dev      *deviceRecord
	priv     ed25519.PrivateKey
	ent      string
	entExp   time.Time
	message  string
	onChange func(Status)
	backoff  time.Duration
}

// New готує менеджер для теки лаунчера.
func New(installDir, launcherVersion string, extraDirs ...string) *Manager {
	m := &Manager{dir: installDir, extra: extraDirs, version: launcherVersion,
		client: &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("редиректи сервера доступу заборонені")
		}}}
	m.dev = m.loadDevice()
	return m
}

// OnChange — колбек для події access:changed у frontend.
func (m *Manager) OnChange(f func(Status)) { m.mu.Lock(); m.onChange = f; m.mu.Unlock() }

// Enabled — чи вшито root-ключ (інакше активація неможлива — dev-збірка).
func Enabled() bool { return len(trust.ParsePublicKeys(RootKeys)) > 0 }

func (m *Manager) notify() {
	m.mu.Lock()
	f := m.onChange
	m.mu.Unlock()
	if f != nil {
		f(m.Status())
	}
}

// ---- keyset і сервер ---------------------------------------------------------

// keyset — найновіший чинний: вшитий або той, що прийняв оновлювач
// (update-state.json; файлу не довіряємо — перевіряємо root).
func (m *Manager) keyset() (*trust.Keyset, error) {
	roots := trust.ParsePublicKeys(RootKeys)
	var best *trust.Keyset
	try := func(raw []byte, sig string) {
		if ks, err := trust.ParseKeyset(roots, raw, sig); err == nil && (best == nil || ks.Version > best.Version) {
			best = ks
		}
	}
	if raw, err := base64.StdEncoding.DecodeString(EmbeddedKeyset); err == nil && len(raw) > 0 {
		try(raw, EmbeddedKeysetSig)
	}
	if data, err := os.ReadFile(filepath.Join(m.dir, "update-state.json")); err == nil {
		var st struct {
			Keyset    []byte `json:"keyset"`
			KeysetSig string `json:"keyset_sig"`
		}
		if json.Unmarshal(data, &st) == nil && len(st.Keyset) > 0 {
			try(st.Keyset, st.KeysetSig)
		}
	}
	if best == nil {
		return nil, errors.New("немає чинного набору ключів")
	}
	return best, nil
}

// testServerURL — лише для тестів (локальний фейковий сервер).
var testServerURL = ""

func (m *Manager) server(ks *trust.Keyset) string {
	if testServerURL != "" {
		return testServerURL
	}
	if ks != nil && ks.Sources.SignalURL != "" {
		return strings.TrimRight(ks.Sources.SignalURL, "/")
	}
	return DefaultServer
}

// ---- сертифікат ---------------------------------------------------------------

// FindCert шукає сертифікат ЗА ВМІСТОМ: будь-який *.cert біля лаунчера, що
// має маркер і підпис root («master (1).cert» теж знайдеться).
func (m *Manager) FindCert() ([]byte, []string) {
	roots := trust.ParsePublicKeys(RootKeys)
	var found []byte
	var paths []string
	for _, dir := range append([]string{m.dir}, m.extra...) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".cert") {
				continue
			}
			p := filepath.Join(dir, e.Name())
			if fi, err := e.Info(); err != nil || fi.Size() > trust.MaxCertSize {
				continue
			}
			data, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			if _, err := trust.ParseCert(roots, data); err != nil {
				continue
			}
			if found == nil {
				found = data
			}
			paths = append(paths, p)
		}
	}
	return found, paths
}

// ---- ключ пристрою -------------------------------------------------------------

func (m *Manager) loadDevice() *deviceRecord {
	data, err := os.ReadFile(filepath.Join(m.dir, DeviceFile))
	if err != nil {
		return nil
	}
	var d deviceRecord
	if json.Unmarshal(data, &d) != nil || d.V != 1 || d.DeviceID == "" {
		return nil
	}
	return &d
}

func (m *Manager) loadKey() (ed25519.PrivateKey, bool, error) {
	sealed, err := os.ReadFile(filepath.Join(m.dir, KeyFile))
	if err != nil {
		return nil, false, err
	}
	seed, err := unprotect(sealed)
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, false, errors.New("ключ пристрою не читається (новий ПК чи скинутий профіль) — активуйте ще раз")
	}
	_, weak, _ := protect(nil)
	return ed25519.NewKeyFromSeed(seed), weak, nil
}

// keyForActivation — наявний ключ або новий (зберігається ДО запиту).
func (m *Manager) keyForActivation() (ed25519.PrivateKey, bool, error) {
	if k, weak, err := m.loadKey(); err == nil {
		return k, weak, nil
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, false, err
	}
	sealed, weak, err := protect(priv.Seed())
	if err != nil {
		return nil, false, err
	}
	if err := writeAtomic(filepath.Join(m.dir, KeyFile), sealed, 0o600); err != nil {
		return nil, false, err
	}
	back, _, err := m.loadKey()
	if err != nil || !bytes.Equal(back, priv) {
		return nil, false, errors.New("не вдалося зберегти ключ пристрою")
	}
	return priv, weak, nil
}

func deviceID(pub ed25519.PublicKey) string {
	s := sha256.Sum256(pub)
	return hex.EncodeToString(s[:])[:32]
}

// ---- статус ---------------------------------------------------------------------

// Status — поточний стан для UI.
func (m *Manager) Status() Status {
	cert, _ := m.FindCert()
	m.mu.Lock()
	defer m.mu.Unlock()
	s := Status{State: StateNone, CertFound: cert != nil, Message: m.message, Enabled: Enabled()}
	if d := m.dev; d != nil {
		s.State = d.Status
		s.DeviceID = d.DeviceID[:8]
		s.Epoch = d.Epoch
		s.WeakStorage = d.Weak
		if !d.ActivatedAt.IsZero() {
			s.ActivatedAt = d.ActivatedAt.Format(time.RFC3339)
		}
	}
	return s
}

// HasAccess — чи показувати розділ приватних збірок: пристрій активний і або
// є чинний токен, або сервер підтверджував доступ не давніше OfflineTTL
// (немає мережі ≠ немає доступу).
func (m *Manager) HasAccess() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.dev == nil || m.dev.Status != StateActive {
		return false
	}
	if m.ent != "" && time.Now().Before(m.entExp) {
		return true
	}
	return time.Since(m.dev.LastOK) < OfflineTTL
}

// ---- активація (§3.4) -------------------------------------------------------------

// Результати, на які UI показує різні повідомлення.
var (
	ErrNotEnabled  = errors.New("ця збірка лаунчера не підтримує активацію")
	ErrBadCert     = errors.New("файл не є дійсним сертифікатом ШАУРМА")
	ErrEpochClosed = errors.New("цей сертифікат більше не приймається — отримайте новий")
	ErrRevoked     = errors.New("доступ для цього пристрою відкликано")
	ErrTemporary   = errors.New("сервер доступу тимчасово недоступний — спробуємо пізніше, сертифікат збережено")
	ErrBadResponse = errors.New("відповідь сервера не пройшла перевірку підпису — сертифікат НЕ видалено")
)

// Activate — активація цим сертифікатом. certPaths — файли, які можна
// видалити після успіху (копії біля лаунчера).
func (m *Manager) Activate(ctx context.Context, cert []byte, certPaths []string) (Status, error) {
	if !Enabled() {
		return m.Status(), ErrNotEnabled
	}
	if _, err := trust.ParseCert(trust.ParsePublicKeys(RootKeys), cert); err != nil {
		return m.Status(), ErrBadCert
	}
	ks, err := m.keyset()
	if err != nil {
		return m.Status(), err
	}
	priv, weak, err := m.keyForActivation()
	if err != nil {
		return m.Status(), err
	}
	pub := priv.Public().(ed25519.PublicKey)
	pubB64 := base64.StdEncoding.EncodeToString(pub)
	base := m.server(ks)

	var nonceResp struct {
		Nonce string `json:"nonce"`
	}
	if _, err := m.do(ctx, http.MethodGet, base+"/v1/activate/nonce", nil, nil, &nonceResp); err != nil || nonceResp.Nonce == "" {
		return m.setMessage(ErrTemporary)
	}
	req := trust.ActivationRequest{V: 1, Cert: string(cert), DevicePub: pubB64, Nonce: nonceResp.Nonce,
		NonceSig:    trust.Sign(priv, trust.ContextActivation, trust.ActivationProofMessage(nonceResp.Nonce, pubB64)),
		LauncherVer: m.version, OS: runtime.GOOS}
	body, _ := json.Marshal(req)
	var out struct {
		Token string `json:"token"`
		Error string `json:"error"`
	}
	code, err := m.do(ctx, http.MethodPost, base+"/v1/activate", body, nil, &out)
	switch {
	case err != nil || code == 429 || code >= 500:
		return m.setMessage(ErrTemporary)
	case code == 400:
		return m.setMessage(ErrBadCert)
	case code == 403 && out.Error == "device_revoked":
		return m.setMessage(ErrRevoked)
	case code == 403:
		return m.setMessage(ErrEpochClosed)
	case code != 200 && code != 202:
		return m.setMessage(ErrTemporary)
	}
	res, err := trust.ParseActivation(ks.RoleKeys(trust.RoleServer, time.Now()), out.Token, nonceResp.Nonce, pubB64)
	if err != nil || res.DeviceID != deviceID(pub) {
		return m.setMessage(ErrBadResponse)
	}
	now := time.Now().UTC()
	rec := &deviceRecord{V: 1, DeviceID: res.DeviceID, DevicePub: pubB64, Status: res.Status, Epoch: res.Epoch, ActivatedAt: now, LastOK: now, Weak: weak}
	if res.Status != StateActive {
		// Очікує схвалення: cert лишається, запис не створює «активований» стан.
		m.mu.Lock()
		m.message = "Запит на доступ очікує схвалення"
		m.priv = priv
		m.mu.Unlock()
		m.notify()
		st := m.Status()
		st.State = StatePending
		return st, nil
	}
	data, _ := json.MarshalIndent(rec, "", "  ")
	if err := writeAtomic(filepath.Join(m.dir, DeviceFile), data, 0o644); err != nil {
		return m.setMessage(err)
	}
	// Критичне правило §3.4: cert видаляється ЛИШЕ після збереженого й
	// перечитаного запису та читабельного ключа.
	if back := m.loadDevice(); back == nil || back.DeviceID != rec.DeviceID {
		return m.setMessage(errors.New("не вдалося зберегти дані активації"))
	}
	if k, _, err := m.loadKey(); err != nil || !bytes.Equal(k, priv) {
		return m.setMessage(errors.New("ключ пристрою не читається після збереження"))
	}
	for _, p := range certPaths {
		_ = os.Remove(p)
	}
	m.mu.Lock()
	m.dev, m.priv, m.message, m.ent = rec, priv, "", ""
	m.mu.Unlock()
	go m.refreshEntitlement(context.Background())
	m.notify()
	return m.Status(), nil
}

func (m *Manager) setMessage(err error) (Status, error) {
	m.mu.Lock()
	m.message = err.Error()
	m.mu.Unlock()
	m.notify()
	return m.Status(), err
}

// ActivateFromText — вставлений текстом сертифікат (§3.7).
func (m *Manager) ActivateFromText(ctx context.Context, text string) (Status, error) {
	return m.Activate(ctx, []byte(strings.TrimSpace(text)), nil)
}

// ActivateFromFile — вибраний у діалозі файл. Вихідний файл користувача не
// чіпаємо (його можуть пересилати далі); видаляються лише копії біля лаунчера.
func (m *Manager) ActivateFromFile(ctx context.Context, path string) (Status, error) {
	fi, err := os.Stat(path)
	if err != nil || fi.Size() > trust.MaxCertSize {
		return m.Status(), ErrBadCert
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return m.Status(), err
	}
	_, local := m.FindCert()
	return m.Activate(ctx, data, local)
}

// AutoActivate — старт лаунчера: cert біля exe / у теці (його поклав setup
// чи розпакував міст R1) → активація з наростаючою паузою між спробами.
func (m *Manager) AutoActivate(ctx context.Context) {
	if !Enabled() {
		return
	}
	for {
		m.mu.Lock()
		active := m.dev != nil && m.dev.Status == StateActive
		m.mu.Unlock()
		if active {
			m.refreshEntitlement(ctx)
			return
		}
		cert, paths := m.FindCert()
		if cert == nil {
			return
		}
		_, err := m.Activate(ctx, cert, paths)
		if !errors.Is(err, ErrTemporary) {
			return
		}
		m.backoff = min(max(m.backoff*2, time.Minute), time.Hour)
		select {
		case <-ctx.Done():
			return
		case <-time.After(m.backoff):
		}
	}
}

// ---- токен доступу (§4) ----------------------------------------------------------------

// Entitlement — чинний токен для X-Shaurma-Entitlement або "". Якщо до кінця
// терміну менше 5 хвилин — оновлюється у фоні.
func (m *Manager) Entitlement() string {
	m.mu.Lock()
	tok, exp, active := m.ent, m.entExp, m.dev != nil && m.dev.Status == StateActive
	m.mu.Unlock()
	if !active {
		return ""
	}
	if tok == "" || time.Until(exp) < 5*time.Minute {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		m.refreshEntitlement(ctx)
		m.mu.Lock()
		tok, exp = m.ent, m.entExp
		m.mu.Unlock()
	}
	if time.Now().After(exp) {
		return ""
	}
	return tok
}

var refreshMu sync.Mutex

func (m *Manager) refreshEntitlement(ctx context.Context) {
	refreshMu.Lock()
	defer refreshMu.Unlock()
	m.mu.Lock()
	if m.ent != "" && time.Until(m.entExp) > 5*time.Minute {
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()
	ks, err := m.keyset()
	if err != nil {
		return
	}
	var out struct {
		Entitlement string `json:"entitlement"`
		Error       string `json:"error"`
	}
	code, err := m.signedPost(ctx, m.server(ks), "/v1/entitlement", nil, &out)
	if err != nil || code >= 500 || code == 429 {
		return // мережа: лишаємо кешований стан (OfflineTTL)
	}
	if code == 401 || code == 403 {
		if strings.HasPrefix(out.Error, "device_") || out.Error == "unknown_device" || out.Error == "epoch_closed" {
			m.markRevoked()
		}
		return
	}
	e, err := trust.ParseEntitlement(ks.RoleKeys(trust.RoleServer, time.Now()), out.Entitlement, time.Now())
	if err != nil {
		return
	}
	m.mu.Lock()
	if m.dev == nil || m.dev.Status != StateActive {
		m.mu.Unlock()
		return // за час запиту пристрій відв'язали чи відкликали
	}
	m.ent, m.entExp = out.Entitlement, time.Unix(e.EXP, 0)
	{
		m.dev.LastOK = time.Now().UTC()
		if data, err := json.MarshalIndent(m.dev, "", "  "); err == nil {
			_ = writeAtomic(filepath.Join(m.dir, DeviceFile), data, 0o644)
		}
	}
	m.mu.Unlock()
}

func (m *Manager) markRevoked() {
	m.mu.Lock()
	if m.dev != nil {
		m.dev.Status = StateRevoked
	}
	m.ent, m.message = "", ErrRevoked.Error()
	_ = os.Remove(filepath.Join(m.dir, DeviceFile))
	m.mu.Unlock()
	m.notify()
}

// Unlink — «Відв'язати цей пристрій»: сервер відкликає ключ, локальний стирається.
func (m *Manager) Unlink(ctx context.Context) (Status, error) {
	ks, err := m.keyset()
	if err == nil {
		code, err := m.signedPost(ctx, m.server(ks), "/v1/device/unlink", nil, nil)
		if err != nil || (code != 200 && code != 401) {
			return m.setMessage(errors.New("не вдалося зв'язатися з сервером — спробуйте пізніше"))
		}
	}
	// Спершу стан у пам'яті (паралельне оновлення токена вже не перепише файл),
	// потім файли.
	m.mu.Lock()
	m.dev, m.priv, m.ent, m.message = nil, nil, "", ""
	_ = os.Remove(filepath.Join(m.dir, DeviceFile))
	_ = os.Remove(filepath.Join(m.dir, KeyFile))
	m.mu.Unlock()
	m.notify()
	return m.Status(), nil
}

// ---- HTTP ------------------------------------------------------------------------------

func (m *Manager) signedPost(ctx context.Context, base, path string, body []byte, out any) (int, error) {
	m.mu.Lock()
	dev, priv := m.dev, m.priv
	m.mu.Unlock()
	if dev == nil {
		return 0, errors.New("пристрій не активовано")
	}
	if priv == nil {
		k, _, err := m.loadKey()
		if err != nil {
			return 0, err
		}
		priv = k
		m.mu.Lock()
		m.priv = k
		m.mu.Unlock()
	}
	nb := make([]byte, 16)
	rand.Read(nb)
	nonce := base64.RawURLEncoding.EncodeToString(nb)
	ts := time.Now().Unix()
	sig := trust.Sign(priv, trust.ContextDeviceReq, trust.DeviceRequestMessage(http.MethodPost, path, ts, nonce, body))
	hdr := map[string]string{"X-Shaurma-Device": dev.DeviceID, "X-Shaurma-Ts": fmt.Sprint(ts), "X-Shaurma-Nonce": nonce, "X-Shaurma-Sig": sig}
	return m.do(ctx, http.MethodPost, base+path, body, hdr, out)
}

func (m *Manager) do(ctx context.Context, method, url string, body []byte, hdr map[string]string, out any) (int, error) {
	if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://127.0.0.1") {
		return 0, errors.New("сервер доступу лише по https")
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "ShaurmaLauncher/"+m.version)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return resp.StatusCode, err
	}
	if out != nil {
		_ = json.Unmarshal(data, out)
	}
	return resp.StatusCode, nil
}

func writeAtomic(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
