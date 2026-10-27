//go:build !shaurma

package main

import (
	"errors"
	"fmt"
	"time"

	"shaurma-launcher-backend/internal/api"
	"shaurma-launcher-backend/internal/auth"
	"shaurma-launcher-backend/internal/model"
	"shaurma-launcher-backend/internal/shaurmaid"
	"shaurma-launcher-backend/internal/social"
	"shaurma-launcher-backend/internal/sync"
	"shaurma-launcher-backend/internal/update"
)

// p2pLobbyBase — у base-збірці кластера вузлів немає (cluster_nodes.go лише для
// shaurma-редакції), тож база лобі P2P — просто CDN, як запасний варіант там.
func p2pLobbyBase() string { return update.CDNBaseURL() }

func newPackProvider(a *App) PackProvider {
	return &stubProvider{}
}

// IsShaurmaEdition повертає false у base-збірці: фронтенд приховує
// вкладку збірок Шаурма, бо відповідні бекенд-методи тут не
// скомпільовані взагалі (їх просто немає в цьому бінарнику).
func (a *App) IsShaurmaEdition() bool { return false }

// launcherEditionName — назва редакції для аналітики та install-ping.
func launcherEditionName() string { return "base" }

// SyncTempPackServers — у base-збірці немає ні P2P, ні соцмережі, тож
// тимчасових серверів (спільні P2P-кімнати) тут не буває: метод лишається
// лише щоб фронтенд мав той самий набір викликів в обох збірках.
func (a *App) SyncTempPackServers() []string { return nil }

type stubProvider struct{}

func (p *stubProvider) GetPackIndex() ([]api.PackIndexEntry, error) {
	return []api.PackIndexEntry{}, nil
}

func (p *stubProvider) GetPackByID(id string) (*api.PackIndexEntry, error) {
	return nil, fmt.Errorf("pack %s not found: launcher built without Shaurma pack support", id)
}

func (p *stubProvider) DownloadPack(packID string, a *App) {}

// PauseDownload — no-op у base-збірці: завантажень Шаурма тут немає,
// метод доданий лише для реалізації інтерфейсу PackProvider.
func (p *stubProvider) PauseDownload(packID string) {}

func (p *stubProvider) CancelDownload(packID string) {}

// AssetDataURL — у base-збірці Шаурма-асетів (іконки/фони) немає; метод
// доданий лише для реалізації інтерфейсу PackProvider.
func (p *stubProvider) AssetDataURL(assetURL string) (string, error) {
	return "", fmt.Errorf("shaurma assets not available in base build")
}

// GetPackWhitelist — у base-збірці білого списку Шаурма-збірок немає;
// метод доданий лише для реалізації інтерфейсу PackProvider.
func (p *stubProvider) GetPackWhitelist(packID string) ([]sync.WhitelistEntry, error) {
	return nil, fmt.Errorf("shaurma whitelist not available in base build")
}

// GetPackWiki — у base-збірці Шаурма-вікі немає; повертаємо nil, nil
// (нічого не показувати), щоб не кидати помилку у фронтенд.
func (p *stubProvider) GetPackWiki(packID string) (*sync.PackWikiDoc, error) {
	return nil, nil
}

// InvalidatePackCache — no-op у base-збірці: немає ні кешу, ні Шаурма-збірок.
func (p *stubProvider) InvalidatePackCache() {}

// Друзі Shaurma — лише Shaurma-редакція (потрібна скін-сесія з JWT). У base-
// збірці хуки з app.go — порожні заглушки.
func (a *App) socialGameStarted(id, name, color string)  {}
func (a *App) socialGameExited(id string)                {}
func (a *App) startSocialIfLoggedIn()                    {}
func (a *App) scheduleCloudPlaytimeSync(time.Duration)   {} // хмарного ігрового часу в base-збірці нема
func (a *App) stopSocial()                               {}
func (a *App) socialOnAccountChanged()                   {}
func (a *App) socialWindowActive(active bool)            {}
func (a *App) socialForgetMS(acc model.Account)          {}
// У base-збірці акаунтів Шаурми нема — сторінці в браузері просто повідомляємо «вхід виконано».
func (a *App) afterMicrosoftLogin(account model.Account, idToken string, sess *auth.Session) {
	sess.Finish(auth.Outcome{Phase: "done", Provider: "microsoft", Licensed: true, Nickname: account.Username})
}

// Microsoft без ліцензії: у base-збірці акаунтів Шаурми нема — це просто помилка входу.
func (a *App) signInMicrosoftWithoutLicense(nl *auth.NoLicenseError, sess *auth.Session) (SignInResult, error) {
	return SignInResult{}, nl
}

func (a *App) SignInGoogle() (SignInResult, error) {
	return SignInResult{}, errors.New("акаунт Shaurma недоступний у цій збірці")
}
func (a *App) CompleteSignIn(nickname string) (SignInResult, error) {
	return SignInResult{}, errors.New("акаунт Shaurma недоступний у цій збірці")
}
func (a *App) CancelSignIn() {}
func (a *App) CheckShaurmaNick(nickname string) (NickCheckResult, error) {
	return NickCheckResult{}, errors.New("акаунт Shaurma недоступний у цій збірці")
}
func (a *App) shaurmaLoginPrepare() string                              { return "" }

// Акаунти Шаурми — лише Shaurma-редакція. У base-збірці їх немає: методи — заглушки з
// тим самим контрактом, щоб фронтенд не розгалужувався за бекендом.
func (a *App) GetShaurmaAccount() ShaurmaAccountInfo { return ShaurmaAccountInfo{} }
func (a *App) CreateShaurmaAccount(nickname, provider string) error {
	return errors.New("акаунт Shaurma недоступний у цій збірці")
}
func (a *App) CancelShaurmaLogin() {}
func (a *App) SetShaurmaNickname(nickname string) error {
	return errors.New("акаунт Shaurma недоступний у цій збірці")
}
func (a *App) SetShaurmaHandle(handle string) error {
	return errors.New("акаунт Shaurma недоступний у цій збірці")
}
func (a *App) SetShaurmaProfileStyle(bio, color string) error {
	return errors.New("акаунт Shaurma недоступний у цій збірці")
}
func (a *App) DeleteShaurmaAccount() error {
	return errors.New("акаунт Shaurma недоступний у цій збірці")
}
func (a *App) SyncShaurmaNickname() (string, error) {
	return "", errors.New("акаунт Shaurma недоступний у цій збірці")
}
func (a *App) GetShaurmaProfile() ShaurmaProfile { return ShaurmaProfile{} }

// p2pIdentity — base-збірка не має Google-акаунта Shaurma: P2P, як і раніше,
// прив'язується до локального акаунта лаунчера.
func (a *App) p2pIdentity() (string, string, error) {
	acc := a.cfg.ActiveAccount()
	if acc.ID == "" {
		return "", "", errors.New("P2P потребує входу в акаунт (Налаштування → Акаунти)")
	}
	if !acc.IsLicensed {
		return "", "", errors.New("P2P недоступний з офлайн-акаунтом (адреси прив'язуються до акаунта)")
	}
	ownerName := acc.Username
	if ownerName == "" {
		ownerName = acc.Gamertag
	}
	return acc.ID, ownerName, nil
}

// Пристрої та ключі чату — лише Shaurma-редакція; у base-збірці заглушки з тим самим контрактом.
func (a *App) GetShaurmaDevices() (*shaurmaid.DeviceList, error) {
	return nil, errors.New("акаунт Shaurma недоступний у цій збірці")
}
func (a *App) RenameShaurmaDevice(deviceID, name string) error {
	return errors.New("акаунт Shaurma недоступний у цій збірці")
}
func (a *App) RevokeShaurmaDevice(deviceID string, notMe bool) error {
	return errors.New("акаунт Shaurma недоступний у цій збірці")
}
func (a *App) RevokeOtherShaurmaDevices() (*shaurmaid.RevokeOthersResult, error) {
	return nil, errors.New("акаунт Shaurma недоступний у цій збірці")
}
func (a *App) RestoreShaurmaDevices() (int, error) {
	return 0, errors.New("акаунт Shaurma недоступний у цій збірці")
}
func (a *App) ShaurmaEmergencyReset() error {
	return errors.New("акаунт Shaurma недоступний у цій збірці")
}
func (a *App) GetShaurmaAudit(limit int) ([]shaurmaid.AuditEvent, error) {
	return nil, errors.New("акаунт Shaurma недоступний у цій збірці")
}
func (a *App) GetChatKeyStatus() social.KeyStatus { return social.KeyStatus{State: social.KeysReady} }
func (a *App) ResetChatKeys() error {
	return errors.New("акаунт Shaurma недоступний у цій збірці")
}
func (a *App) ReplaceShaurmaDevice(deviceID string) (SignInResult, error) {
	return SignInResult{}, errors.New("акаунт Shaurma недоступний у цій збірці")
}

// p2pAuth — base-збірка не має JWT Шаурми: запити лобі анонімні.
func (a *App) p2pAuth() (string, string) { return "", "" }
