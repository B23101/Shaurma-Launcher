package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"shaurma-launcher-backend/internal/console"
	"shaurma-launcher-backend/internal/model"
	"shaurma-launcher-backend/internal/social"
	"shaurma-launcher-backend/internal/update"
)

// ── HTTP-клієнт до worker'а скін-системи ──────────────────────────────
// Той самий стек заголовків, що й інші запити лаунчера: User-Agent +
// X-Shaurma-Token (у shaurma-збірці це TOKEN_SHAURMA).
func (a *App) skinHTTPClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second}
}

func (a *App) skinRequest(method, path string, body []byte) (*http.Response, error) {
	return vpsPool().Do(nodeKeyFor(path, ""), func(base string) (*http.Request, error) {
		var reader io.Reader
		if body != nil {
			reader = bytes.NewReader(body)
		}
		req, err := http.NewRequest(method, base+path, reader)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "ShaurmaLauncher/2.0")
		req.Header.Set("X-Shaurma-Token", update.Token())
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		return req, nil
	})
}

// ═══════════════════════════════════════════════════════════════════════
// Launch hook — викликається з LaunchInstance ПЕРЕД стартом JVM
// ═══════════════════════════════════════════════════════════════════════
// У base-збірці ця функція — порожній no-op (offline_account_base.go),
// тож сам виклик у спільному app.go безпечний для обох редакцій.
func (a *App) prepareOfflineAccount(account model.Account, buildID, gameDir string) []string {
	_ = gameDir // у теку гри нічого не пишемо: скін віддає Yggdrasil-API в рантаймі

	// ЄДИНИЙ вимикач скін-системи — налаштування «Скіни» (DisableSkins).
	//
	// Система скінів одна — Шаурма (authlib-injector) — і для піратських, і
	// для ліцензійних акаунтів (account.IsLicensed або Type == "microsoft"):
	// гравець бачить скіни піратів, а свій скін видно й на неліцензійних
	// серверах. Вхід на ліцензійні сервери при цьому працює: join/hasJoined
	// ліцензійних ніків наш Yggdrasil-API форвардить на справжній
	// sessionserver Mojang (vps/internal/skins/ygg.go). Перемикача системи
	// скінів більше немає.
	licensed := account.IsLicensed || account.Type == "microsoft"
	if a.cfg.GetSettings().DisableSkins {
		if buf := a.consoleBufFor(account.ID, buildID); buf != nil {
			buf.AppendLevel(console.LevelSystem, "Скіни вимкнено в налаштуваннях — запуск без скін-системи")
		}
		return nil
	}

	args, err := a.authlibJVMArgsOpt(account, licensed)
	if err != nil {
		// Не критично: гра стартує як завжди (буде дефолтний Steve), але
		// причину показуємо в консолі збірки, а не мовчки.
		if buf := a.consoleBufFor(account.ID, buildID); buf != nil {
			buf.AppendLevel(console.LevelWarn, "Скіни: інжектор не підготовлено — "+err.Error())
		}
		return nil
	}
	if buf := a.consoleBufFor(account.ID, buildID); buf != nil {
		buf.AppendLevel(console.LevelSystem, skinSourcesNote())
	}
	return args
}

// ═══════════════════════════════════════════════════════════════════════
// Google OAuth flow (лаунчер-частина)
// ═══════════════════════════════════════════════════════════════════════
// 1. StartGoogleSkinLogin: локальний callback-сервер (29121-29125),
//    POST /api/auth/google/login { redirect: http://localhost:<port>/shaurma-auth }
//    → worker повертає URL Google. Фронтенд відкриває його в браузері.
// 2. Користувач авторизується → Google → worker callback → worker
//    редиректить на http://localhost:<port>/shaurma-auth?token=JWT.
// 3. AwaitGoogleSkinLogin ловить цей запит, зберігає JWT у skin-auth.json.

// ═══════════════════════════════════════════════════════════════════════
// Статус сесії / вихід / нік / завантаження скіна
// ═══════════════════════════════════════════════════════════════════════

// licensedAccountWithNick — чи є в лаунчері ЛІЦЕНЗІЙНИЙ (Microsoft) акаунт
// із таким самим ніком. Умова пріоритету ліцензійного (ТЗ: «якщо є
// ліцензійний акаунт із таким ніком — редагувати скін треба на ньому, а
// не на Шаурма-акаунті»).
//
// Перевіряємо саме список акаунтів ЛАУНЧЕРА, а не реєстр Mojang: Mojang
// не знає, чи це справді акаунт цього користувача. Серверний бік
// (worker/skin.js, worker/yggdrasil.js) додатково тримає той самий
// пріоритет за реєстром Mojang — обидві умови працюють разом.
func (a *App) licensedAccountWithNick(nick string) (model.Account, bool) {
	nick = strings.TrimSpace(nick)
	if nick == "" {
		return model.Account{}, false
	}
	for _, acc := range a.cfg.GetAccounts() {
		if acc.IsLicensed && strings.EqualFold(acc.Username, nick) {
			return acc, true
		}
	}
	return model.Account{}, false
}

// fetchSkinServerLook забирає поточний PNG скіна гравця з сервера скінів
// Shaurma (GET /api/skin/by-nick/{нік}) і повертає його разом із моделлю
// рук (classic/slim, з заголовка X-Skin-Model). found=false, якщо на
// сервері немає запису для цього ніка (гравець ще не завантажував скін) —
// у грі в цьому випадку показується дефолтний Steve, і гардероб має
// поводитись так само, а не мовчки показувати Mojang-скін.
func (a *App) fetchSkinServerLook(nickname string) (png []byte, slim bool, found bool, err error) {
	if a.cfg.GetSettings().DisableSkins {
		return nil, false, false, nil
	}
	resp, err := a.skinRequest(http.MethodGet, "/api/skin/by-nick/"+url.PathEscape(nickname), nil)
	if err != nil {
		return nil, false, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, false, false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, false, skinAPIErr(resp)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return nil, false, false, err
	}
	slim = resp.Header.Get("X-Skin-Model") == "slim"
	return data, slim, true, nil
}

// activeLookFromSkinServer — хук для App.GetWardrobeActiveLook. Для
// НЕліцензійного (шаурма) акаунта поточний вигляд береться не з Mojang
// (у такого акаунта там нічого немає), а з нашого сервера скінів під
// ніком акаунта — тим самим шляхом, яким скін роздається у грі
// (authlib-injector → Yggdrasil-API → цей самий by-nick). Плащів на
// сервері скінів немає — CapeDataURL завжди порожній.
//
// Повертає handled=true, якщо акаунт офлайн і хук взяв справу на себе
// (навіть якщо на сервері ще немає скіна — тоді look=nil, як і для
// ліцензійного акаунта без власного скіна); handled=false — акаунт
// ліцензійний, потрібен звичайний шлях через Mojang.
func (a *App) activeLookFromSkinServer(accountID string) (look *model.ActiveLook, handled bool, err error) {
	acc := a.accountByID(accountID)
	if acc.ID == "" || acc.IsLicensed {
		return nil, false, nil
	}
	png, slim, found, ferr := a.fetchSkinServerLook(acc.Username)
	if ferr != nil {
		return nil, true, ferr
	}
	if !found {
		return nil, true, nil
	}
	return &model.ActiveLook{
		SkinDataURL: "data:image/png;base64," + base64.StdEncoding.EncodeToString(png),
		SlimArms:    slim,
	}, true, nil
}

// GetFriendLook повертає вигляд (скін) довільного гравця Шаурми за ніком —
// для 3D-моделі у вікні профілю друга. Береться з нашого сервера скінів
// (там живуть скіни Shaurma/офлайн-акаунтів, яких Mojang не знає — раніше
// профіль друга шукав скін лише на Mojang, тому для таких друзів
// 3D-модель не вантажилась). nil, nil — на сервері запису немає: фронтенд
// тоді пробує Mojang (ліцензійний друг).
func (a *App) GetFriendLook(nickname string) (*model.ActiveLook, error) {
	nickname = strings.TrimSpace(nickname)
	if nickname == "" {
		return nil, nil
	}
	png, slim, found, err := a.fetchSkinServerLook(nickname)
	if err != nil || !found {
		return nil, err
	}
	return &model.ActiveLook{
		SkinDataURL: "data:image/png;base64," + base64.StdEncoding.EncodeToString(png),
		SlimArms:    slim,
	}, nil
}

// activeMatchingPresetFromSkinServer — хук для App.GetWardrobeActivePresetID.
// Той самий принцип побайтового порівняння, що й ActiveMatchingPresetID
// у internal/wardrobe/service.go (Mojang-шлях), але джерело — сервер
// скінів Shaurma замість Mojang, і без порівняння плаща (плащів у
// шаурма-скін-системі немає).
func (a *App) activeMatchingPresetFromSkinServer(accountID string) (id string, handled bool, err error) {
	acc := a.accountByID(accountID)
	if acc.ID == "" || acc.IsLicensed {
		return "", false, nil
	}
	png, _, found, ferr := a.fetchSkinServerLook(acc.Username)
	if ferr != nil {
		return "", true, ferr
	}
	if !found {
		return "", true, nil
	}
	remoteHash := sha256.Sum256(png)
	for _, p := range a.wardrobe.ListPresets(acc.Username) {
		if p.CapePath != "" {
			continue // на сервері скінів плащів немає — пресет із плащем точно не збігається
		}
		localSkin, rerr := os.ReadFile(p.SkinPath)
		if rerr != nil {
			continue
		}
		if sha256.Sum256(localSkin) == remoteHash {
			return p.ID, true, nil
		}
	}
	return "", true, nil
}

// ═══════════════════════════════════════════════════════════════════════
// Хуки для App.RenameOfflineAccount / App.RemoveAccount (app.go)
// ═══════════════════════════════════════════════════════════════════════

// wireShaurmaAvatarResolver підключає avatar.Service.Head() до сервера
// скінів Shaurma для НЕліцензійних акаунтів (виклик з app.go одразу
// після avatar.NewService — там же, де і сам сервіс створюється, щоб не
// було вікна, коли Head() встигне відпрацювати без резолвера). Реальний
// мережевий запит — GET /api/skin/by-nick/{нік}, публічний (у межах
// авторизації токеном продукту, як і решта скін-API), не потребує JWT
// конкретної сесії: показ іконки чужого гравця з тим же роздачі скінів,
// що й у грі, має працювати незалежно від того, залогінений САМ
// переглядач у скін-систему чи ні.
func (a *App) wireShaurmaAvatarResolver() {
	if a.avatars == nil {
		return
	}
	a.avatars.SetShaurmaSkinResolver(func(username string) ([]byte, bool, error) {
		if a.cfg.GetSettings().DisableSkins {
			return nil, false, nil
		}
		resp, err := a.skinRequest(http.MethodGet, "/api/skin/by-nick/"+url.PathEscape(username), nil)
		if err != nil {
			return nil, false, err
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound {
			return nil, false, nil // нема запису на сервері — не помилка, фолбек на Mojang
		}
		if resp.StatusCode != http.StatusOK {
			return nil, false, fmt.Errorf("skin by-nick: %s", resp.Status)
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
		if err != nil {
			return nil, false, err
		}
		return data, true, nil
	})
}

func skinAPIErr(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil && e.Error != "" {
		return fmt.Errorf("сервер скінів (%d): %s", resp.StatusCode, e.Error)
	}
	return fmt.Errorf("сервер скінів: status %d", resp.StatusCode)
}

// ═══════════════════════════════════════════════════════════════════════
// Скіни Шаурми — під акаунтом Шаурми (КОМІРКОЮ) конкретного акаунта лаунчера
// ═══════════════════════════════════════════════════════════════════════
// Вхід, нік, видалення — shaurma_account_shaurma.go (VPS). Тут лише скіни. Скін може змінити
// тільки акаунт, що має власну комірку: акаунт «лише з ніком» її не має, тож скінів не отримує.
// Скін роздається за ніком комірки (так грі його віддає Yggdrasil), тому нік комірки має
// збігатись із ніком офлайн-акаунта в лаунчері.

// applyPresetToSkinServer — хук для ApplyWardrobePreset (спільний app.go). Для ОФЛАЙН-акаунта
// пресет не можна вивантажити в Mojang (потрібен Microsoft-токен, а його ми нікуди не передаємо),
// тож скін іде на сервер Шаурми під коміркою ЦЬОГО акаунта.
//
// handled=true — офлайн-акаунт, хук взяв справу на себе (навіть з помилкою); handled=false —
// ліцензійний, застосовується звичайний шлях через Mojang.
func (a *App) applyPresetToSkinServer(accountID, presetID string) (bool, error) {
	acc := a.accountByID(accountID)
	if acc.ID == "" || acc.IsLicensed {
		return false, nil
	}
	cell, ok := a.shaurmaCell(acc.ID)
	if !ok {
		return true, fmt.Errorf("цей акаунт без акаунта Шаурми: створіть його (Друзі → Акаунт Шаурми), щоб застосовувати скіни")
	}
	if !strings.EqualFold(cell.Nickname, acc.Username) {
		return true, fmt.Errorf("нік акаунта Шаурми «%s» відрізняється від ніка акаунта «%s»: скін у грі не з'явиться. Змініть нік у акаунті Шаурми", cell.Nickname, acc.Username)
	}
	// Скін за ТИПОМ акаунта Шаурми: ліцензійний бере нік і скін з Mojang.
	if cell.Licensed() {
		return true, fmt.Errorf("акаунт Шаурми ліцензійного типу — нік і скін беруться з Mojang. Застосуйте пресет на ліцензійному акаунті (Minecraft → Скіни)")
	}

	var preset *model.SkinPreset
	for _, p := range a.wardrobe.ListPresets(acc.Username) {
		if p.ID == presetID {
			pp := p
			preset = &pp
			break
		}
	}
	if preset == nil {
		return true, fmt.Errorf("пресет не знайдено")
	}
	data, err := os.ReadFile(preset.SkinPath)
	if err != nil {
		return true, err
	}
	skinModel := "classic"
	if preset.SlimArms {
		skinModel = "slim"
	}
	return true, a.uploadSkinFor(acc.ID, base64.StdEncoding.EncodeToString(data), skinModel)
}

// markShaurmaSkinAccounts — хук для App.GetAccounts. Офлайн-акаунт із власною коміркою Шаурми
// позначається «Шаурма-акаунт»; без комірки лишається «Не ліцензійний» (лише нік).
func (a *App) markShaurmaSkinAccounts(accounts []model.Account) {
	for i := range accounts {
		if accounts[i].IsLicensed {
			continue
		}
		if _, ok := a.licensedAccountWithNick(accounts[i].Username); ok {
			accounts[i].LicensedNickMine = true // скіном під цим ніком керує ліцензійний акаунт
		}
		if _, ok := a.shaurmaCell(accounts[i].ID); ok {
			accounts[i].IsShaurmaSkinAccount = true
		}
	}
}

// renameSkinNicknameIfMatches — з App.RenameOfflineAccount. Якщо нік комірки цього акаунта
// збігався зі старим ніком — переназиваємо й комірку (скін лишається під новим ніком).
// Помилки не блокують локальне перейменування (App.RenameOfflineAccount повідомить про них).
func (a *App) renameSkinNicknameIfMatches(oldUsername, newUsername string) error {
	for _, acc := range a.cfg.GetAccounts() {
		if acc.IsLicensed || !strings.EqualFold(acc.Username, oldUsername) && !strings.EqualFold(acc.Username, newUsername) {
			continue
		}
		cell, ok := a.shaurmaCell(acc.ID)
		if !ok || !strings.EqualFold(cell.Nickname, oldUsername) {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		return a.shaurmaManager().SetNickname(ctx, acc.ID, newUsername)
	}
	return nil
}

// UploadSkin завантажує PNG (base64) як скін АКТИВНОГО акаунта лаунчера.
func (a *App) UploadSkin(pngBase64, model string) error {
	return a.uploadSkinFor(a.cfg.ActiveAccount().ID, pngBase64, model)
}

func (a *App) uploadSkinFor(launcherID, pngBase64, model string) error {
	cell, ok := a.shaurmaCell(launcherID)
	if !ok {
		return fmt.Errorf("створіть акаунт Шаурми для цього акаунта, щоб завантажувати скін")
	}
	// Скін за ТИПОМ акаунта Шаурми: ліцензійний бере нік і скін з Mojang (у грі — скін
	// з Mojang). Кажемо одразу, до мережі.
	if cell.Licensed() {
		return fmt.Errorf("акаунт Шаурми ліцензійного типу: нік і скін беруться з Mojang, тут скін не змінити. Змініть скін на ліцензійному акаунті (Minecraft → Скіни)")
	}
	body, _ := json.Marshal(map[string]string{"png": pngBase64, "model": model})
	resp, err := a.shaurmaRequest(launcherID, http.MethodPost, "/api/skin", body)
	if err != nil {
		if errors.Is(err, social.ErrNotLoggedIn) {
			return fmt.Errorf("акаунт Шаурми недоступний на цьому комп'ютері")
		}
		return fmt.Errorf("немає зв'язку з сервером скінів")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return fmt.Errorf("скіни можна міняти не частіше ніж раз на 10 секунд")
	}
	if resp.StatusCode != http.StatusOK {
		return skinAPIErr(resp)
	}
	// Кеш іконки акаунта ключується за ніком — скидаємо, щоб нова іконка з'явилась одразу.
	if a.avatars != nil && cell.Nickname != "" {
		a.avatars.Invalidate("", cell.Nickname)
	}
	a.emit("avatar:changed", "")
	return nil
}

// skinSourcesNote — рядок для консолі збірки: які джерела скінів діють.
func skinSourcesNote() string {
	skinProxyMu.Lock()
	active := theSkinProxy != nil
	skinProxyMu.Unlock()
	if active {
		return "Скіни: локальний injector активний (джерела: Shaurma → Mojang за ніком)"
	}
	return "Скіни: authlib-injector активний (скіни з сервера Shaurma)"
}
