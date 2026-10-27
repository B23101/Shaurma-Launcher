//go:build shaurma

package main

// Акаунти Шаурми на боці лаунчера (нова система).
//
// Два світи не перетинаються: локальний акаунт гри (Minecraft/Mojang) лишається на цьому ПК
// і сюди не потрапляє; акаунт Шаурми — це комірка (internal/shaurmaid): власний ключ Ed25519,
// власний accountId, власні друзі/чат/P2P на КОЖЕН лаунчер-акаунт. Акаунт «лише з ніком»
// комірки не має — отже, ні друзів, ні чату, ні P2P, ні скінів.
//
// Усі запити йдуть на VPS (vpsPool). Worker акаунтів не веде і токенів не бачить.

import (
	"shaurma-launcher-backend/internal/crashguard"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"shaurma-launcher-backend/internal/auth"
	"shaurma-launcher-backend/internal/model"
	"shaurma-launcher-backend/internal/shaurmaid"
	"shaurma-launcher-backend/internal/social"
	"shaurma-launcher-backend/internal/update"
)

var (
	shaurmaMgrOnce sync.Once
	shaurmaMgr     *shaurmaid.Manager
	shaurmaHW      = shaurmaid.NewHWIDGenerator()

	// shaurmaPending — заготовка комірки (ключ + nonce) останнього входу Microsoft.
	// Завдяки їй id_token того самого входу реєструє акаунт Шаурми — один вхід замість двох.
	shaurmaPendingMu sync.Mutex
	shaurmaPending   *shaurmaid.Pending
)

// shaurmaHWID — апаратний хеш цього ПК ("" — не вдалося обчислити).
func shaurmaHWID() string {
	h, err := shaurmaHW.HWID()
	if err != nil {
		return ""
	}
	return h
}

// vpsTransport — shaurmaid.Transport поверх пулу VPS-вузлів лаунчера.
type vpsTransport struct{}

func (vpsTransport) Do(ctx context.Context, method, path string, headers map[string]string, body []byte) (int, []byte, error) {
	bearer := strings.TrimPrefix(headers["Authorization"], "Bearer ")
	resp, err := vpsPool().Do(nodeKeyFor(path, bearer), func(base string) (*http.Request, error) {
		var rd io.Reader
		if body != nil {
			rd = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, base+path, rd)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "ShaurmaLauncher/2.0")
		req.Header.Set("X-Shaurma-Token", update.Token())
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		return req, nil
	})
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	return resp.StatusCode, b, err
}

// shaurmaManager — менеджер комірок (ключі лежать у захищеному сховищі ОС: DPAPI на Windows).
func (a *App) shaurmaManager() *shaurmaid.Manager {
	shaurmaMgrOnce.Do(func() {
		st, err := shaurmaid.NewSecureStorage(filepath.Join(a.cfg.Dir(), ".shaurma", "vault"), shaurmaHW)
		if err != nil {
			// Без сховища ключів акаунтів Шаурми немає: менеджер із заглушкою, що завжди відмовляє.
			st = deadStorage{}
		}
		shaurmaMgr = shaurmaid.NewManager(shaurmaid.Config{
			HWID: shaurmaHW, Crypto: shaurmaid.NewCrypto(), Storage: st, Transport: vpsTransport{},
			// Опис пристрою для екрана «Пристрої» (лише відображення; безпеки не стосується).
			ClientVersion: appVersion, OSName: shaurmaOSName(), DeviceName: shaurmaDeviceName,
		})
	})
	return shaurmaMgr
}

type deadStorage struct{}

func (deadStorage) Put(string, []byte) error {
	return errors.New("захищене сховище недоступне")
}
func (deadStorage) Get(string) ([]byte, error) { return nil, shaurmaid.ErrNotFound }
func (deadStorage) Delete(string) error        { return nil }

func (a *App) shaurmaCell(launcherID string) (*shaurmaid.Cell, bool) {
	if launcherID == "" {
		return nil, false
	}
	return a.shaurmaManager().Cell(launcherID)
}

// shaurmaRequest — запит до VPS від імені комірки лаунчер-акаунта: JWT + HWID. Якщо сервер
// відповів 401 — один раз перелогінюємось підписом і повторюємо.
func (a *App) shaurmaRequest(launcherID, method, path string, body []byte) (*http.Response, error) {
	hwid := shaurmaHWID()
	if hwid == "" {
		return nil, social.ErrNotLoggedIn
	}
	do := func(token string) (*http.Response, error) {
		return vpsPool().Do(nodeKeyFor(path, token), func(base string) (*http.Request, error) {
			var rd io.Reader
			if body != nil {
				rd = bytes.NewReader(body)
			}
			req, err := http.NewRequest(method, base+path, rd)
			if err != nil {
				return nil, err
			}
			req.Header.Set("User-Agent", "ShaurmaLauncher/2.0")
			req.Header.Set("X-Shaurma-Token", update.Token())
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("X-Shaurma-HWID", hwid)
			if body != nil {
				req.Header.Set("Content-Type", "application/json")
			}
			return req, nil
		})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	sess, err := a.shaurmaManager().Session(ctx, launcherID)
	if err != nil {
		return nil, shaurmaSessionErr(err)
	}
	resp, err := do(sess.AccessToken)
	if err != nil {
		return nil, errors.New("social:network")
	}
	if resp.StatusCode != http.StatusUnauthorized {
		return resp, nil
	}
	resp.Body.Close()
	a.shaurmaManager().Invalidate(launcherID)
	sess, err = a.shaurmaManager().Session(ctx, launcherID)
	if err != nil {
		return nil, shaurmaSessionErr(err)
	}
	resp, err = do(sess.AccessToken)
	if err != nil {
		return nil, errors.New("social:network")
	}
	return resp, nil
}

func shaurmaSessionErr(err error) error {
	switch {
	case errors.Is(err, shaurmaid.ErrDeviceRevoked):
		// Окрема причина: «цей пристрій вилучено», а не безликий «потрібен вхід». Локальні дані лишаються.
		return social.ErrDeviceRevoked
	case errors.Is(err, shaurmaid.ErrClockSkew):
		return social.ErrClockSkew
	case errors.Is(err, shaurmaid.ErrNoCell), errors.Is(err, shaurmaid.ErrGone), errors.Is(err, shaurmaid.ErrWrongDevice):
		return social.ErrNotLoggedIn
	}
	return errors.New("social:network") // сирий текст мережевої помилки в UI не віддаємо
}

func (a *App) shaurmaToken(launcherID string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	s, err := a.shaurmaManager().Session(ctx, launcherID)
	if err != nil {
		return "", shaurmaSessionErr(err)
	}
	return s.AccessToken, nil
}

// ── Теки/ідентифікатори ──────────────────────────────────────────────────

// sanitizeIdentityID — ID лаунчер-акаунта придатний для імені теки ("" — ні).
func sanitizeIdentityID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" || len(id) > 64 {
		return ""
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return ""
		}
	}
	return id
}

// ── Стан для UI ──────────────────────────────────────────────────────────

func (a *App) GetShaurmaAccount() ShaurmaAccountInfo {
	acc := a.cfg.ActiveAccount()
	info := ShaurmaAccountInfo{LauncherAccountID: acc.ID, LauncherNick: acc.Username, IsLicensed: acc.IsLicensed}
	if acc.ID == "" {
		info.Reason = "no_launcher_account"
		return info
	}
	if shaurmaHWID() == "" {
		info.Reason = "no_hwid"
		return info
	}
	if c, ok := a.shaurmaCell(acc.ID); ok {
		info.HasAccount = true
		info.AccountID, info.Nickname = c.AccountID, c.Nickname
		info.Type, info.UUID, info.CreatedAt = c.Type, c.UUID, c.CreatedAt
		info.NickLocked = c.Licensed()
		info.CanUpgrade = acc.IsLicensed && !c.Licensed()
	}
	return info
}

// CreateShaurmaAccount — створити акаунт Шаурми для АКТИВНОГО лаунчер-акаунта. Лаунчер сам
// підставляє все для реєстрації; користувач лише підтверджує особу в Google/Microsoft
// (OAuth, scope openid: підписаний «паспорт», без доступу до його акаунта). provider:
// "google" | "microsoft". Блокує до завершення входу в браузері (до 3 хв) — див. CancelShaurmaLogin.
func (a *App) CreateShaurmaAccount(nickname, provider string) error {
	acc := a.cfg.ActiveAccount()
	if acc.ID == "" {
		return errors.New("спершу додайте акаунт у лаунчері")
	}
	nickname = strings.TrimSpace(nickname)
	if nickname == "" {
		nickname = strings.TrimSpace(acc.Username)
	}
	return a.registerShaurma(acc, nickname, provider)
}

// shaurmaRegisterOpts — тип і UUID акаунта Шаурми з даних акаунта ЛАУНЧЕРА:
// ліцензійний → type=licensed + UUID профілю Mojang (нік сервер бере сам з Mojang);
// офлайн → type=unlicensed (UUID сервер обчислює з ніка й звіряє).
func (a *App) shaurmaRegisterOpts(acc model.Account, nickname, provider string) shaurmaid.RegisterOpts {
	opts := shaurmaid.RegisterOpts{Nickname: nickname, Provider: provider, Type: shaurmaid.TypeUnlicensed}
	if acc.IsLicensed {
		opts.Type = shaurmaid.TypeLicensed
		opts.UUID = acc.UUID
	}
	return opts
}

// registerShaurma — спільна частина ручного (CreateShaurmaAccount) створення акаунта Шаурми
// для КОНКРЕТНОГО лаунчер-акаунта: окремий OAuth-вхід (Google/Microsoft), id_token бере менеджер.
func (a *App) registerShaurma(acc model.Account, nickname, provider string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	oidcMu.Lock()
	if oidcCancel != nil {
		oidcCancel()
	}
	oidcCancel = cancel
	oidcMu.Unlock()
	defer cancel()

	// Сторінка в браузері (спільний callback-сервер) показує підсумок: беремо її сесію з oidcIDToken.
	var sess *auth.Session
	src := func(c context.Context, prov, nonce string) (string, error) {
		tok, s, err := a.oidcIDTokenSession(c, prov, nonce)
		sess = s
		return tok, err
	}
	_, err := a.shaurmaManager().Register(ctx, acc.ID, a.shaurmaRegisterOpts(acc, nickname, provider), src)
	if err != nil {
		uerr := a.shaurmaUserError(err)
		finishSignInPage(sess, SignInResult{}, uerr)
		return uerr
	}
	sess.Finish(auth.Outcome{Phase: "done", Provider: provider, Registered: true,
		Licensed: acc.IsLicensed, Nickname: nickname})
	a.emit("shaurma-account:changed", "")
	a.socialOnAccountChanged()
	return nil
}

// registerShaurmaWithToken — реєстрація з id_token, який уже прийшов із входу Microsoft у гру
// (nonce підготовлено в shaurmaLoginPrepare). Вхід у Microsoft для користувача — ОДИН.
func (a *App) registerShaurmaWithToken(acc model.Account, nickname, provider string, p *shaurmaid.Pending, idToken string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	_, err := a.shaurmaManager().RegisterWithToken(ctx, acc.ID, a.shaurmaRegisterOpts(acc, nickname, provider), p, idToken)
	if err != nil {
		return a.shaurmaUserError(err)
	}
	a.emit("shaurma-account:changed", "")
	a.socialOnAccountChanged()
	return nil
}

// shaurmaLoginPrepare — готує комірку й повертає nonce для входу Microsoft. nonce кладеться
// прямо в OAuth-запит гри (StartMSLogin → StartLoginWithNonce), тож id_token ЦЬОГО Ж входу
// реєструє акаунт Шаурми — жодного другого вікна браузера. Порожній рядок: base-збірка / без HWID.
func (a *App) shaurmaLoginPrepare() string {
	if shaurmaHWID() == "" {
		return ""
	}
	p, err := a.shaurmaManager().Prepare()
	if err != nil {
		return ""
	}
	shaurmaPendingMu.Lock()
	shaurmaPending = p
	shaurmaPendingMu.Unlock()
	return p.Nonce()
}

// takeShaurmaPending — одноразове читання заготовки (вона належить рівно одному входу).
func (a *App) takeShaurmaPending() *shaurmaid.Pending {
	shaurmaPendingMu.Lock()
	p := shaurmaPending
	shaurmaPending = nil
	shaurmaPendingMu.Unlock()
	return p
}

func (a *App) CancelShaurmaLogin() {
	oidcMu.Lock()
	if oidcCancel != nil {
		oidcCancel()
	}
	oidcMu.Unlock()
}

// SetShaurmaNickname — змінити ВІДОБРАЖУВАНИЙ нік (ID не змінюється; нік не унікальний).
func (a *App) SetShaurmaNickname(nickname string) error {
	acc := a.cfg.ActiveAccount()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := a.shaurmaManager().SetNickname(ctx, acc.ID, nickname); err != nil {
		return a.shaurmaUserError(err)
	}
	a.emit("shaurma-account:changed", "")
	a.socialOnAccountChanged()
	return nil
}

// SetShaurmaHandle — змінити @handle (публічний унікальний ідентифікатор для пошуку друзів).
// ID акаунта не змінюється; старі друзі й чати не зачіпаються.
func (a *App) SetShaurmaHandle(handle string) error {
	acc := a.cfg.ActiveAccount()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := a.shaurmaManager().SetHandle(ctx, acc.ID, handle); err != nil {
		return a.shaurmaUserError(err)
	}
	a.emit("shaurma-account:changed", "")
	a.socialOnAccountChanged()
	return nil
}

// SetShaurmaProfileStyle — зберегти біо (≤ 120 символів) і колір фону профілю (#RRGGBB або "" — стандартний).
// Дані перевіряються тут ще ДО мережі (щоб не палити ліміт змін на очевидне сміття), але остаточне слово — за
// сервером: він чистить біо від прихованих/bidi-символів і не дозволяє змінювати частіше ніж раз на кілька секунд.
func (a *App) SetShaurmaProfileStyle(bio, color string) error {
	cleanBio, ok := social.NormalizeBio(bio)
	if !ok {
		return fmt.Errorf("біо: не більше %d символів", social.MaxBioRunes)
	}
	color = strings.TrimSpace(color)
	if color != "" {
		if color = social.SanitizeColor(color); color == "" {
			return errors.New("колір має бути у форматі #RRGGBB")
		}
	}
	acc := a.cfg.ActiveAccount()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := a.shaurmaManager().SetProfileStyle(ctx, acc.ID, cleanBio, color); err != nil {
		return a.shaurmaUserError(err)
	}
	a.emit("shaurma-account:changed", "")
	return nil
}

// DeleteShaurmaAccount — видалити акаунт Шаурми активного лаунчер-акаунта (на сервері й локально).
func (a *App) DeleteShaurmaAccount() error {
	acc := a.cfg.ActiveAccount()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	a.stopSocial()
	if err := a.shaurmaManager().Delete(ctx, acc.ID); err != nil {
		return a.shaurmaUserError(err)
	}
	_ = os.RemoveAll(a.socialDirFor(acc.ID)) // ключі E2E цієї комірки
	a.emit("shaurma-account:changed", "")
	a.socialOnAccountChanged()
	return nil
}

func (a *App) shaurmaUserError(err error) error {
	var ae *shaurmaid.APIError
	var le *oidcLocalError
	switch {
	case errors.As(err, &le):
		return errors.New(le.msg)
	case errors.Is(err, shaurmaid.ErrCellExists):
		return errors.New("для цього акаунта вже є акаунт Шаурми")
	case errors.Is(err, shaurmaid.ErrNoNickname):
		return errors.New("нік: 1–16 символів, лише літери, цифри та _")
	case errors.Is(err, shaurmaid.ErrNickLocked):
		return errors.New("нік ліцензійного акаунта береться лише з Mojang — тут його не змінити")
	case errors.Is(err, shaurmaid.ErrBadType):
		return errors.New("не вдалося визначити тип акаунта (перевірте UUID профілю)")
	case errors.Is(err, shaurmaid.ErrWrongDevice):
		return errors.New("цей акаунт Шаурми прив'язаний до іншого комп'ютера")
	case errors.Is(err, shaurmaid.ErrDeviceRevoked):
		return errors.New("цей пристрій вилучено з акаунта. Увійдіть знову — ваші друзі, нік, скіни й чати повернуться")
	case errors.Is(err, shaurmaid.ErrClockSkew):
		return errors.New("годинник на комп'ютері йде неправильно — виправте дату й час")
	case errors.Is(err, context.Canceled):
		return errors.New("вхід скасовано")
	case errors.Is(err, context.DeadlineExceeded):
		return errors.New("час очікування входу минув")
	case errors.As(err, &ae):
		switch ae.Code {
		case "rate_limited":
			return errors.New("забагато спроб — спробуйте пізніше")
		case "hwid_limit", "identity_limit":
			return errors.New("досягнуто ліміт акаунтів Шаурми для цього комп'ютера або входу")
		case "bad_identity":
			return errors.New("не вдалось підтвердити вхід у Google/Microsoft")
		case "nick_taken":
			return errors.New("цей нік уже зайнятий іншим акаунтом Шаурми")
		case "nick_mojang":
			return errors.New("цей нік належить гравцю Minecraft з ліцензією — оберіть інший")
		case "account_exists":
			return errors.New("для цього входу вже є акаунт Шаурми — увійдіть через «Додати акаунт»")
		case "no_account":
			return errors.New("для цього входу ще немає акаунта Шаурми")
		case "cell_exists":
			return errors.New("для цього акаунта вже є акаунт Шаурми")
		case "invalid_nickname":
			return errors.New("нік: 1–16 символів, лише літери, цифри та _")
		case "invalid_handle":
			return errors.New("ідентифікатор: 3–32 символи, лише a–z, 0–9, _ та крапка; без крапки чи _ на краях і без «..» та «__»")
		case "handle_taken":
			return errors.New("цей ідентифікатор уже зайнятий")
		case "handle_reserved":
			return errors.New("цей ідентифікатор зарезервований або недавно звільнений — оберіть інший")
		case "handle_cooldown":
			if ae.RetryAt > 0 {
				return fmt.Errorf("ідентифікатор можна змінити знову %s", time.UnixMilli(ae.RetryAt).Format("02.01.2006"))
			}
			return errors.New("ідентифікатор можна змінювати не частіше разу на 7 днів")
		case "invalid_bio":
			return fmt.Errorf("біо: не більше %d символів, без прихованих і керуючих символів", social.MaxBioRunes)
		case "invalid_color":
			return errors.New("колір має бути у форматі #RRGGBB")
		case "profile_cooldown":
			return errors.New("занадто часто — зачекайте кілька секунд і збережіть ще раз")
		case "nick_locked":
			return errors.New("нік ліцензійного акаунта береться лише з Mojang — тут його не змінити")
		case "bad_type":
			return errors.New("не вдалося визначити тип акаунта (перевірте UUID профілю)")
		case "no_such_profile":
			return errors.New("Mojang не знайшов ліцензійний профіль з таким UUID")
		case "mojang_unavailable":
			return errors.New("сервер Шаурми не зміг звернутись до Mojang для перевірки ліцензії — спробуйте за хвилину")
		case "device_limit":
			return errors.New("в акаунті вже максимум пристроїв — вилучіть один у списку пристроїв")
		case "new_device_rate":
			return errors.New("забагато нових пристроїв за добу — спробуйте завтра або вилучіть зайві")
		case "device_revoked":
			return errors.New("цей пристрій вилучено з акаунта. Увійдіть знову")
		case "stale_request":
			return errors.New("годинник на комп'ютері йде неправильно — виправте дату й час")
		case "replay":
			return errors.New("повторний запит — спробуйте ще раз")
		case "storage_error", "internal":
			return fmt.Errorf("помилка бази даних на сервері Шаурми (%s)", ae.Code)
		}
		// Невідомий код (або відповідь не від сервісу акаунтів — шлюз/Cloudflare без JSON): показуємо
		// HTTP-статус і код, щоб причину було видно одразу, а не лише «відхилив».
		if ae.Code == "" {
			return fmt.Errorf("сервер відхилив запит (HTTP %d, без коду)", ae.Status)
		}
		return fmt.Errorf("сервер відхилив запит (HTTP %d, %s)", ae.Status, ae.Code)
	}
	return fmt.Errorf("немає зв'язку з сервером Шаурми: %v", err)
}

// socialDirFor — тека даних комірки (ключі E2E чату).
func (a *App) socialDirFor(launcherID string) string {
	id := sanitizeIdentityID(launcherID)
	if id == "" {
		return ""
	}
	return filepath.Join(a.cfg.Dir(), ".shaurma", "social", "cell-"+id)
}

// socialForgetMS — лаунчер-акаунт видалено з ЛАУНЧЕРА: прибираємо ЛИШЕ локальні дані комірки
// (ключ акаунта й ключі E2E чату). Акаунт Шаурми НА СЕРВЕРІ (друзі, чат, скін, ID) НЕ
// чіпаємо: повторний вхід через Google/Microsoft знаходить його за особою (Recover) і
// прив'язує до цього ПК — так само, як на новому комп'ютері. Раніше тут викликався
// Manager.Delete (POST /api/account/delete), і «видалити акаунт у лаунчері» мовчки знищувало
// акаунт Шаурми назавжди; тепер серверний акаунт видаляється ЛИШЕ явно —
// DeleteShaurmaAccount (Налаштування → Акаунт → «Видалити акаунт Шаурми»). Інші акаунти не зачіпаються.
func (a *App) socialForgetMS(acc model.Account) {
	id := acc.ID
	if id == "" {
		return
	}
	go func() {
		defer crashguard.Recover("shaurma_account_shaurma.go:517")
		_ = a.shaurmaManager().Forget(id) // лише локально
		if d := a.socialDirFor(id); d != "" {
			_ = os.RemoveAll(d)
		}
	}()
}

// shaurmaAutoCreateEvent — подія "shaurma-account:autocreate" для тостів фронтенду.
type shaurmaAutoCreateEvent struct {
	Status  string `json:"status"` // "start" | "ok" | "error"
	Message string `json:"message,omitempty"`
}

// afterMicrosoftLogin — вхід у Microsoft = і вхід у Шаурму, як у випадку Google: якщо для цього
// лаунчер-акаунта ще немає акаунта Шаурми, створюємо його через Microsoft OIDC (id_token зі
// scope «openid» — лише «паспорт» особи). Ліцензія, токени Xbox/Minecraft та UUID профілю на
// сервери Шаурми НЕ передаються. Якщо акаунт уже є (повторний вхід) — нічого не робимо.
// Best-effort: збій не валить вхід у гру, користувач може створити акаунт у «Акаунт».
//
// idToken — id_token ЦЬОГО входу (nonce уже вшито через shaurmaLoginPrepare): реєстрація йде
// без другого вікна браузера. Якщо заготовки немає (вхід стався до Shaurma-редакції) —
// фолбек на окремий OAuth.
func (a *App) afterMicrosoftLogin(account model.Account, idToken string, sess *auth.Session) {
	if account.ID == "" || shaurmaHWID() == "" {
		sess.Finish(auth.Outcome{Phase: "done", Provider: "microsoft", Licensed: account.IsLicensed, Nickname: account.Username})
		return
	}
	pending := a.takeShaurmaPending()
	if cell, ok := a.shaurmaCell(account.ID); ok {
		// Акаунт Шаурми вже є — для сторінки в браузері це просто вхід (не реєстрація).
		sess.Finish(auth.Outcome{Phase: "done", Provider: "microsoft", Licensed: account.IsLicensed, Nickname: account.Username})
		// Ліцензійному підтягуємо нік з Mojang; якщо лаунчер-акаунт
		// став ліцензійним, а комірка ще неліцензійна — пропонуємо переоформити тип.
		if account.IsLicensed && !cell.Licensed() {
			a.emit("shaurma-account:upgrade-available", map[string]any{
				"message": "У цього акаунта з'явилася ліцензія. Переоформіть акаунт Шаурми як ліцензійний, щоб нік і скін бралися з Mojang.",
			})
		} else if cell.Licensed() {
			go func() {
				defer crashguard.Recover("shaurma_account_shaurma.go:556")
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				if _, err := a.shaurmaManager().SyncNickname(ctx, account.ID); err == nil {
					a.emit("shaurma-account:changed", "")
				}
			}()
		}
		return
	}
	nick := strings.TrimSpace(account.Username)
	go func() {
		defer crashguard.Recover("shaurma_account_shaurma.go:567")
		a.emit("shaurma-account:autocreate", shaurmaAutoCreateEvent{Status: "start"})
		registered, pageSess, err := a.recoverOrRegisterMicrosoft(account, nick, pending, idToken, sess)
		if err != nil {
			// Вхід у гру вдався, а акаунт Шаурми — ні: сторінка каже про це чесно.
			pageSess.Finish(auth.Outcome{Phase: "done", Provider: "microsoft", Licensed: account.IsLicensed,
				Nickname: account.Username,
				Warning:  "Акаунт Шаурми створити не вдалося: " + err.Error() + " Спробуйте ще раз у розділі «Акаунт» лаунчера."})
			a.emit("shaurma-account:autocreate", shaurmaAutoCreateEvent{Status: "error", Message: err.Error()})
			return
		}
		pageSess.Finish(auth.Outcome{Phase: "done", Provider: "microsoft", Registered: registered,
			Licensed: account.IsLicensed, Nickname: account.Username})
		a.emit("shaurma-account:autocreate", shaurmaAutoCreateEvent{Status: "ok"})
	}()
}

// recoverOrRegisterMicrosoft: для Microsoft-особи акаунт Шаурми вже є (інший ПК / перевстановлення)
// → відновлюємо його на цьому ПК; немає → створюємо ліцензійний (нік і UUID з Mojang).
// Без заготовки (вхід стався до Shaurma-редакції) — окремий OAuth з тією ж логікою.
func (a *App) recoverOrRegisterMicrosoft(account model.Account, nick string, pending *shaurmaid.Pending, idToken string, sess *auth.Session) (registered bool, pageSess *auth.Session, err error) {
	pageSess = sess
	if pending == nil || idToken == "" {
		// Другий вхід у браузері: сторінка першого входу вже може показати «вхід виконано».
		sess.Finish(auth.Outcome{Phase: "done", Provider: "microsoft", Licensed: account.IsLicensed, Nickname: account.Username})
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
		oidcMu.Lock()
		if oidcCancel != nil {
			oidcCancel()
		}
		oidcCancel = cancel
		oidcMu.Unlock()
		defer cancel()
		p, perr := a.shaurmaManager().Prepare()
		if perr != nil {
			return false, pageSess, a.shaurmaUserError(perr)
		}
		tok, s2, terr := a.oidcIDTokenSession(ctx, "microsoft", p.Nonce())
		pageSess = s2
		if terr != nil {
			return false, pageSess, a.shaurmaUserError(terr)
		}
		pending, idToken = p, tok
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	rec, rerr := a.shaurmaManager().Recover(ctx, pending, "microsoft", idToken)
	switch {
	case rerr == nil:
		if err := a.finishRecoveredMicrosoft(account, rec); err != nil {
			return false, pageSess, err
		}
		return false, pageSess, nil // акаунт Шаурми цієї особи вже був — відновлено, не створено
	case isDeviceLimit(rerr):
		_, derr := a.deferForDeviceLimit(rerr, pending, "microsoft", idToken, func(r *shaurmaid.Recovered) (SignInResult, error) {
			if err := a.finishRecoveredMicrosoft(account, r); err != nil {
				return SignInResult{}, err
			}
			return SignInResult{Status: "signed_in", Provider: "microsoft", Nickname: account.Username, Existing: true}, nil
		})
		if derr != nil {
			return false, pageSess, derr
		}
		return false, pageSess, errors.New("в акаунті вже максимум пристроїв — оберіть у лаунчері, який вилучити")
	case errors.Is(rerr, shaurmaid.ErrNoAccount):
		if err := a.registerShaurmaWithToken(account, nick, "microsoft", pending, idToken); err != nil {
			return false, pageSess, err
		}
		return true, pageSess, nil
	}
	return false, pageSess, a.shaurmaUserError(rerr)
}

// finishRecoveredMicrosoft — зберегти відновлений (пристрій додано до існуючого акаунта) локально й
// оновити UI.
func (a *App) finishRecoveredMicrosoft(account model.Account, rec *shaurmaid.Recovered) error {
	if err := a.shaurmaManager().Adopt(account.ID, rec); err != nil {
		return a.shaurmaUserError(err)
	}
	a.emit("shaurma-account:changed", "")
	a.socialOnAccountChanged()
	if c := rec.Cell(); account.IsLicensed && !c.Licensed() {
		a.emit("shaurma-account:upgrade-available", map[string]any{
			"message": "У цього акаунта з'явилася ліцензія. Переоформіть акаунт Шаурми як ліцензійний, щоб нік і скін бралися з Mojang.",
		})
	} else if c.Licensed() {
		go func() {
			defer crashguard.Recover("shaurma_account_shaurma.go:653")
			sctx, scancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer scancel()
			if _, err := a.shaurmaManager().SyncNickname(sctx, account.ID); err == nil {
				a.emit("shaurma-account:changed", "")
			}
		}()
	}
	return nil
}

// SyncShaurmaNickname — ліцензійний акаунт: підтягнути актуальний нік з Mojang (через сервер).
// Неліцензійному нічого не робить (нік обирає гравець).
func (a *App) SyncShaurmaNickname() (string, error) {
	acc := a.cfg.ActiveAccount()
	if acc.ID == "" {
		return "", errors.New("спершу додайте акаунт у лаунчері")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	nick, err := a.shaurmaManager().SyncNickname(ctx, acc.ID)
	if err != nil {
		return "", a.shaurmaUserError(err)
	}
	a.emit("shaurma-account:changed", "")
	a.socialOnAccountChanged()
	return nick, nil
}

// GetShaurmaProfile — повний профіль акаунта Шаурми активного акаунта лаунчера для сторінки
// «Акаунт»: дані з сервера (тип/UUID/дата/ліміт змін ніка) + локальна статистика лаунчера.
// Скіна тут НЕМАЄ: ним керує гардероб.
func (a *App) GetShaurmaProfile() ShaurmaProfile {
	acc := a.cfg.ActiveAccount()
	p := ShaurmaProfile{LauncherAccountID: acc.ID, LauncherNick: acc.Username}
	if acc.ID == "" {
		p.Reason = "no_launcher_account"
		return p
	}
	if shaurmaHWID() == "" {
		p.Reason = "no_hwid"
		return p
	}
	cell, ok := a.shaurmaCell(acc.ID)
	if !ok {
		p.Reason = "no_shaurma_account"
		return p
	}
	p.HasAccount = true
	p.AccountID, p.Nickname, p.Type, p.UUID, p.CreatedAt = cell.AccountID, cell.Nickname, cell.Type, cell.UUID, cell.CreatedAt
	p.Licensed, p.NickLocked = cell.Licensed(), cell.Licensed()
	p.CanUpgrade = acc.IsLicensed && !cell.Licensed()

	// Статистика лаунчера — локальна й дешева.
	playtime := a.cfg.GetPlaytime()
	for _, secs := range playtime {
		p.GameSeconds += secs
	}
	p.BuildsCount = len(playtime)
	if svc := a.socialService(); svc != nil && svc.Running() {
		p.Friends = len(svc.Snapshot().Friends)
	}

	// Свіжі дані з сервера (best-effort: збій мережі лишає локальний знімок).
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if prof, err := a.shaurmaManager().Profile(ctx, acc.ID); err == nil && prof != nil {
		if prof.Nickname != "" {
			p.Nickname = prof.Nickname
		}
		if prof.Type != "" {
			p.Type = prof.Type
			p.Licensed = prof.Type == shaurmaid.TypeLicensed
			p.NickLocked = p.Licensed
		}
		if prof.UUID != "" {
			p.UUID = prof.UUID
		}
		if prof.CreatedAt != 0 {
			p.CreatedAt = prof.CreatedAt
		}
		p.NickChangesLeft, p.NickResetAt = prof.NickChangesLeft, prof.NickResetAt
		p.Handle, p.HandleNextChangeAt = prof.Handle, prof.HandleNextChangeAt
		p.Bio, p.Color = social.SanitizeBio(prof.Bio), social.SanitizeColor(prof.Color)
	}
	return p
}

var legacyPurgeOnce sync.Once

// purgeLegacyAccountData прибирає залишки СТАРОЇ системи (спільна Google-сесія, ms-сесії, спільні
// ключі E2E): refresh-токени на диску більше не потрібні й небезпечні.
func (a *App) purgeLegacyAccountData() {
	legacyPurgeOnce.Do(func() {
		base := filepath.Join(a.cfg.Dir(), ".shaurma")
		_ = os.Remove(filepath.Join(a.cfg.Dir(), "skin-auth.json"))
		_ = os.RemoveAll(filepath.Join(base, "social-auth"))
		_ = os.RemoveAll(filepath.Join(base, "keys"))
		if ents, err := os.ReadDir(filepath.Join(base, "social")); err == nil {
			for _, e := range ents {
				n := e.Name()
				if strings.HasPrefix(n, "google-") || strings.HasPrefix(n, "ms-") {
					_ = os.RemoveAll(filepath.Join(base, "social", n))
				}
			}
		}
	})
}

// ── Тікети ACL для приватних збірок ──────────────────────────────────────

var (
	ticketMu    sync.Mutex
	ticketCache = map[string]struct {
		tok string
		exp time.Time
	}{}
)

// shaurmaACLTicket — підписаний VPS-тікет (10 хв) для ACL збірок. Worker перевіряє лише підпис.
func (a *App) shaurmaACLTicket(launcherID string) string {
	ticketMu.Lock()
	if c, ok := ticketCache[launcherID]; ok && time.Until(c.exp) > time.Minute {
		ticketMu.Unlock()
		return c.tok
	}
	ticketMu.Unlock()
	resp, err := a.shaurmaRequest(launcherID, http.MethodPost, "/api/account/ticket", []byte("{}"))
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	var out struct {
		Ticket     string `json:"ticket"`
		ExpiresInS int    `json:"expiresInS"`
	}
	if err := decodeJSONLimited(resp.Body, &out); err != nil || out.Ticket == "" {
		return ""
	}
	ticketMu.Lock()
	ticketCache[launcherID] = struct {
		tok string
		exp time.Time
	}{out.Ticket, time.Now().Add(time.Duration(out.ExpiresInS) * time.Second)}
	ticketMu.Unlock()
	return out.Ticket
}
