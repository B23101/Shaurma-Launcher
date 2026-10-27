//go:build shaurma

package main

import (
	"context"
	"errors"
	"log"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"shaurma-launcher-backend/internal/shaurmaid"
	"shaurma-launcher-backend/internal/social"
)

// Пристрої акаунта Шаурми: список, перейменування, вилучення, «Це не я», екстрена ротація, журнал;
// серверні ключі чату; вибір пристрою для вилучення при досягнутому ліміті.

func shaurmaOSName() string {
	switch runtime.GOOS {
	case "darwin":
		return "macos"
	case "windows", "linux":
		return runtime.GOOS
	}
	return runtime.GOOS
}

// shaurmaDeviceName — назва за замовчуванням: ім'я комп'ютера (користувач може змінити на екрані «Пристрої»).
func shaurmaDeviceName() string {
	h, err := os.Hostname()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(h)
}

// activeShaurmaID — лаунчер-акаунт активної комірки.
func (a *App) activeShaurmaID() (string, error) {
	id := a.currentSocialIdent()
	if id.key == "" {
		return "", errors.New("спершу створіть акаунт Шаурми")
	}
	if _, ok := a.shaurmaCell(id.launcherID); !ok {
		return "", errors.New("акаунт Шаурми не знайдено на цьому пристрої")
	}
	return id.launcherID, nil
}

func devCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 20*time.Second)
}

// GetShaurmaDevices — пристрої акаунта (назва, ОС, клієнт, IP, країна, остання активність, онлайн).
func (a *App) GetShaurmaDevices() (*shaurmaid.DeviceList, error) {
	id, err := a.activeShaurmaID()
	if err != nil {
		return nil, err
	}
	ctx, cancel := devCtx()
	defer cancel()
	l, err := a.shaurmaManager().Devices(ctx, id)
	if err != nil {
		return nil, a.shaurmaUserError(err)
	}
	return l, nil
}

func (a *App) RenameShaurmaDevice(deviceID, name string) error {
	id, err := a.activeShaurmaID()
	if err != nil {
		return err
	}
	ctx, cancel := devCtx()
	defer cancel()
	return a.shaurmaUserErrorOrNil(a.shaurmaManager().RenameDevice(ctx, id, deviceID, name))
}

func (a *App) shaurmaUserErrorOrNil(err error) error {
	if err == nil {
		return nil
	}
	return a.shaurmaUserError(err)
}

// RevokeShaurmaDevice вилучає пристрій. notMe=true («Це не я») — ще й ротує ключ чату.
func (a *App) RevokeShaurmaDevice(deviceID string, notMe bool) error {
	id, err := a.activeShaurmaID()
	if err != nil {
		return err
	}
	ctx, cancel := devCtx()
	defer cancel()
	kr, err := a.shaurmaManager().RevokeDevice(ctx, id, deviceID, notMe)
	if err != nil {
		return a.shaurmaUserError(err)
	}
	a.rotateChatKeyIf(kr)
	a.emit("shaurma-account:devices-changed", "")
	return nil
}

// RevokeOtherShaurmaDevices — «Вийти на всіх, крім цього» (10 хв діє «Повернути пристрої»).
func (a *App) RevokeOtherShaurmaDevices() (*shaurmaid.RevokeOthersResult, error) {
	id, err := a.activeShaurmaID()
	if err != nil {
		return nil, err
	}
	ctx, cancel := devCtx()
	defer cancel()
	r, err := a.shaurmaManager().RevokeOthers(ctx, id)
	if err != nil {
		return nil, a.shaurmaUserError(err)
	}
	a.emit("shaurma-account:devices-changed", "")
	return r, nil
}

func (a *App) RestoreShaurmaDevices() (int, error) {
	id, err := a.activeShaurmaID()
	if err != nil {
		return 0, err
	}
	ctx, cancel := devCtx()
	defer cancel()
	n, err := a.shaurmaManager().RestoreDevices(ctx, id)
	if err != nil {
		return 0, a.shaurmaUserError(err)
	}
	a.emit("shaurma-account:devices-changed", "")
	return n, nil
}

// ShaurmaEmergencyReset — «Мій акаунт скомпрометовано»: вилучає всі інші пристрої й створює нову
// версію ключа чату (стара історія лишається читабельною).
func (a *App) ShaurmaEmergencyReset() error {
	id, err := a.activeShaurmaID()
	if err != nil {
		return err
	}
	ctx, cancel := devCtx()
	defer cancel()
	kr, err := a.shaurmaManager().EmergencyReset(ctx, id)
	if err != nil {
		return a.shaurmaUserError(err)
	}
	a.rotateChatKeyIf(kr)
	a.emit("shaurma-account:devices-changed", "")
	return nil
}

func (a *App) GetShaurmaAudit(limit int) ([]shaurmaid.AuditEvent, error) {
	id, err := a.activeShaurmaID()
	if err != nil {
		return nil, err
	}
	ctx, cancel := devCtx()
	defer cancel()
	ev, err := a.shaurmaManager().Audit(ctx, id, limit)
	if err != nil {
		return nil, a.shaurmaUserError(err)
	}
	return ev, nil
}

// rotateChatKeyIf створює нову версію ключа чату, якщо сервер цього вимагає після небезпечної дії.
func (a *App) rotateChatKeyIf(kr *shaurmaid.KeyRotation) {
	if kr == nil || !kr.RotateKey {
		return
	}
	if svc := a.socialService(); svc != nil {
		if err := svc.RotateChatKey(kr.NextVersion); err != nil {
			log.Printf("ротація ключа чату: %v", err)
		}
	}
}

// GetChatKeyStatus — стан ключів чату: ready | pending | need_recovery.
func (a *App) GetChatKeyStatus() social.KeyStatus {
	if svc := a.socialService(); svc != nil {
		return svc.ChatKeyStatus()
	}
	return social.KeyStatus{State: social.KeysPending}
}

// ResetChatKeys — користувач погодився почати чат «з чистого аркуша» (ключ лишився на втраченому
// пристрої). Стара історія, зашифрована тим ключем, лишиться нечитабельною.
func (a *App) ResetChatKeys() error {
	svc := a.socialService()
	if svc == nil {
		return errors.New("соціальна система не запущена")
	}
	return svc.ResetChatKeys()
}

// ── серверні ключі чату: адаптер social.KeyServer ────────────────────────

type shaurmaKeyServer struct {
	a  *App
	id string // лаунчер-акаунт
}

func (a *App) chatKeyServerFor(id socialIdent) social.KeyServer {
	if id.launcherID == "" {
		return nil
	}
	return shaurmaKeyServer{a: a, id: id.launcherID}
}

func toRemote(k *shaurmaid.ChatKeys) *social.RemoteKeys {
	out := &social.RemoteKeys{Current: k.Current}
	for _, e := range k.Keys {
		out.Keys = append(out.Keys, social.RemoteKey{Version: e.Version, PublicB64: e.ChatPublicKey, SecretB64: e.SecretKey, CreatedAt: e.CreatedAt})
	}
	return out
}

func mapKeyErr(err error) error {
	switch {
	case errors.Is(err, shaurmaid.ErrNoChatKey):
		return social.ErrRemoteNoKey
	case errors.Is(err, shaurmaid.ErrChatKeyDisabled):
		return social.ErrRemoteDisabled
	case errors.Is(err, shaurmaid.ErrDeviceRevoked):
		return social.ErrDeviceRevoked
	}
	var cf *shaurmaid.ChatKeysConflict
	if errors.As(err, &cf) {
		return &social.RemoteConflict{Keys: toRemote(cf.Keys)}
	}
	// Усе інше (мережа, 5xx, key_unavailable, нема сесії) — «тимчасово недоступно»: клієнт НЕ створює
	// новий ключ, а чекає й повторює.
	return social.ErrRemoteUnavailable
}

func (k shaurmaKeyServer) GetKeys(ctx context.Context) (*social.RemoteKeys, error) {
	r, err := k.a.shaurmaManager().GetChatKeys(ctx, k.id)
	if err != nil {
		return nil, mapKeyErr(err)
	}
	return toRemote(r), nil
}

func (k shaurmaKeyServer) PutKey(ctx context.Context, version int, pub, sec string) (*social.RemoteKeys, error) {
	r, err := k.a.shaurmaManager().PutChatKey(ctx, k.id, version, pub, sec)
	if err != nil {
		return nil, mapKeyErr(err)
	}
	return toRemote(r), nil
}

// ── ліміт пристроїв: вибір, кого вилучити ────────────────────────────────

type limitPending struct {
	p        *shaurmaid.Pending
	provider string
	idToken  string
	cont     func(*shaurmaid.Recovered) (SignInResult, error)
	at       time.Time
}

var (
	limitMu   sync.Mutex
	limitWait *limitPending
)

const limitTTL = 8 * time.Minute

func isDeviceLimit(err error) bool {
	var le *shaurmaid.DeviceLimitError
	return errors.As(err, &le)
}

// deferForDeviceLimit запам'ятовує id_token цього входу й просить UI показати вибір пристрою.
func (a *App) deferForDeviceLimit(err error, p *shaurmaid.Pending, provider, idToken string,
	cont func(*shaurmaid.Recovered) (SignInResult, error)) (SignInResult, error) {
	var le *shaurmaid.DeviceLimitError
	if !errors.As(err, &le) {
		return SignInResult{}, err
	}
	limitMu.Lock()
	limitWait = &limitPending{p: p, provider: provider, idToken: idToken, cont: cont, at: time.Now()}
	limitMu.Unlock()
	res := SignInResult{Status: "device_limit", Provider: provider, MaxDevices: le.Max}
	for _, d := range le.Devices {
		res.Devices = append(res.Devices, DeviceChoice{ID: d.ID, Name: d.Name, ClientName: d.ClientName, OS: d.OS, LastSeenAt: d.LastSeenAt, Country: d.Country})
	}
	a.emit("shaurma-account:device-limit", res) // для входу, що йде в браузері (Microsoft)
	return res, nil
}

// ReplaceShaurmaDevice — користувач обрав, який пристрій вилучити, щоб увійти на цьому.
func (a *App) ReplaceShaurmaDevice(deviceID string) (SignInResult, error) {
	limitMu.Lock()
	lw := limitWait
	limitMu.Unlock()
	if lw == nil || time.Since(lw.at) > limitTTL {
		return SignInResult{}, errors.New("сесія входу завершилась — увійдіть ще раз")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	rec, err := a.shaurmaManager().RecoverReplacing(ctx, lw.p, lw.provider, lw.idToken, deviceID)
	if err != nil {
		if isDeviceLimit(err) {
			return a.deferForDeviceLimit(err, lw.p, lw.provider, lw.idToken, lw.cont)
		}
		return SignInResult{}, a.shaurmaUserError(err)
	}
	limitMu.Lock()
	limitWait = nil
	limitMu.Unlock()
	return lw.cont(rec)
}
