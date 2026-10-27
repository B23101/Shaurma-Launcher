package main

// notify_window.go — віджет сповіщень у трей (стиль Steam, правий нижній кут).
//
// Це ОКРЕМЕ Wails-вікно (id "notify-toast"), а не частина головного вікна:
//   - frameless + transparent → вікно майже невидиме, контент малює сам;
//   - AlwaysOnTop → тост поверх інших застосунків;
//   - HiddenOnTaskbar → не зʼявляється в панелі завдань;
//   - показ через window_op "show-inactive" → фокус лишається в того
//     застосунку, що мав його до появи тоста (shell/window_show_*.go), тож
//     користувач може продовжувати набір тексту під час сповіщення.
//
// ВАЖЛИВО: НЕ передавати Windows.ExStyle у створення цього вікна — Wails
// там ЗАМІНЮє обчислений стиль цілком і це стирає WS_EX_TOPMOST /
// WS_EX_TOOLWINDOW / WS_EX_NOREDIRECTIONBITMAP (прозорість).
//
// Вікно росте ВГОРУ: нові картки стають зверху стосу, нижній кут лишається
// на місці. Розмір і позицію задає фронтенд цього вікна через
// SetNotifyPopupSize (він знає фактичну висоту свого контенту), а shell
// після кожного SetSize викликає window_op "anchor-bottom-right" — саме
// тому привʼязка до кута НЕ зсуває тост при зміні кількості карток.
//
// СКІЛЬКИ КАРТОК: не більше notifyMaxStack. Черга тримається в памʼяті
// (payload) і віддається вікну при його монтуванні через TakeNotifyPopup —
// вікно створюється асинхронно, тож подія могла б прийти «до реєстрації».
// Дедуплікація — за ID; на фронтенді картки з однаковим accountId
// оновлюються, а не дублюються (спам повідомленнями → одна картка з
// останнім текстом).

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// notifyWindowID — стабільний ID вікна у shell (window_create/window_op).
	notifyWindowID = "notify-toast"
	// notifyMaxStack — максимум одночасно показаних карток (стос росте
	// вгору; далі нова витісняє найстарішу). Збігається з MAX у
	// NotifyPopup.svelte.
	notifyMaxStack = 3
	// notifyWidth — ширина вікна (px, DIP).
	notifyWidth = 360
	// notifyInitialH — стартова висота (одна картка), щоб перше показання
	// вікна не було «широкою порожнечею», поки контент ще не встиг виміряти
	// свою висоту.
	notifyInitialH = 96
	// notifyMarginX/MarginY — відступ від правого та нижнього країв екрана.
	notifyMarginX = 18
	notifyMarginY = 18
)

// NotifyPayload — одна картка сповіщення. Серіалізується у подію
// "notify:push" і віддається вікну через TakeNotifyPopup.
type NotifyPayload struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`      // "chat" | "invite"
	Title     string `json:"title"`     // нік відправника (SANITIZED на бекенді)
	Body      string `json:"body"`      // превʼю тексту (SANITIZED, обрізане)
	AccountID string `json:"accountId"` // ціль кліку: відкрити чат із цим другом
	TS        int64  `json:"ts"`        // unix ms
	// DurationMs — час показу САМЕ цієї картки (налаштування «Час показу»).
	// Заповнюється на бекенді при постановці в чергу, а не читається
	// вікном із налаштувань окремо: так час показу завжди свіжий (користувач
	// міг змінити його секунду тому) і не треба додатковий місток
	// вікно→бекенд на кожне сповіщення.
	DurationMs int `json:"durationMs,omitempty"`
}

// notifyDurationBounds — межі часу показу картки (мс). Дзеркалять
// notifyDurationRange у internal/config (там — секунди): значення вже
// нормалізовано при збереженні, тут просто захист від сміття.
const (
	notifyDurationMinMs = 3000
	notifyDurationMaxMs = 60000
	notifyDurationDefMs = 7000
)

var (
	notifyWinMu sync.Mutex
	// notifyPending — черга сповіщень, які ще не отримані вікном.
	notifyPending []NotifyPayload
	// notifyCreated — вікно вже створене (create виконано).
	notifyCreated bool

	// notifyMainActive — чи головне вікно зараз активне. Оновлюється з
	// події "window:active" (windowstate.go). За замовчуванням true, бо
	// при старті лаунчера головне вікно показане — інакше під час першого
	// запуску сповіщення лінувались би показуватись взагалі.
	notifyMainActive atomic.Bool
)

func init() {
	notifyMainActive.Store(true)
}

// notificationsAllowed — чи взагалі показувати тости ЗАРАЗ.
//
// Два незалежних вимикачі з Налаштування → «Сповіщення» (schema.go,
// група notifications):
//   - NotifyEnabled  — головний: вимкнено → тост не створюється взагалі,
//     вікно віджета навіть не породжується (економія памʼяті + тиші);
//   - NotifyDnd      — «Не турбувати»: швидкий перемикач на час, який НЕ
//     змінює налаштування вище — користувач вмикає його назад і все
//     лишається так, як він налаштував.
//
// Окремо від цього (див. notifyFromSocial) діє правило «активне головне
// вікно → не показувати»: застосунок і так на очах.
func (a *App) notificationsAllowed() bool {
	s := a.cfg.GetSettings()
	return s.NotifyEnabled && !s.NotifyDnd
}

// notifyDurationMs — час показу однієї картки в мілісекундах. Нормалізація
// (3..60 с) робиться в config.normalize при збереженні, тут — конвертація з
// повторним стисненням на випадок нульового/зіпсованого значення.
func (a *App) notifyDurationMs() int {
	sec := a.cfg.GetSettings().NotifyDurationSec
	if sec <= 0 {
		sec = notifyDurationDefMs / 1000
	}
	ms := sec * 1000
	if ms < notifyDurationMinMs {
		ms = notifyDurationMinMs
	}
	if ms > notifyDurationMaxMs {
		ms = notifyDurationMaxMs
	}
	return ms
}

// notifyWindowURL — URL сторінки віджета (аналог consoleWindowURL).
func notifyWindowURL() string {
	if dev := os.Getenv("FRONTEND_DEVSERVER_URL"); dev != "" {
		return strings.TrimSuffix(dev, "/") + "/notify.html"
	}
	return "/notify.html"
}

// setNotifyMainWindowActive викликається з onWindowActive (windowstate.go).
func setNotifyMainWindowActive(active bool) {
	notifyMainActive.Store(active)
}

// notifyHideIfDisabled — викликається ПІСЛЯ збереження налаштувань: якщо
// користувач щойно вимкнув сповіщення (або увімкнув «Не турбувати»), вже
// показані картки мають зникнути одразу, а не висіти до кінця свого TTL —
// інакше вимкнення виглядало б так, ніби не спрацювало.
func (a *App) notifyHideIfDisabled() {
	if !a.notificationsAllowed() {
		a.hideNotifyPopup()
	}
}

// ── Черга та створення вікна ──────────────────────────────────────────────

// pushNotify додає сповіщення в чергу, створює вікно (одного разу) і
// шле подію "notify:push" усім вікнам. Вікно може ще не бути готовим —
// тоді payload забере його фронтенд через TakeNotifyPopup.
//
// Викликати ЛИШЕ коли сповіщення доречне (mainActive == false і т.п.) —
// фільтр стоїть у викликачі, щоб тут не ховати бізнес-логіку.
func (a *App) pushNotify(p NotifyPayload) {
	if p.Kind == "" {
		p.Kind = "chat"
	}
	if p.AccountID == "" && p.Title == "" {
		return
	}
	if p.TS == 0 {
		p.TS = time.Now().UnixMilli()
	}
	// Час показу — з налаштувань У МОМЕНТ постановки в чергу (див. DurationMs).
	p.DurationMs = a.notifyDurationMs()
	if p.ID == "" {
		p.ID = fmt.Sprintf("%s-%s-%d", p.Kind, p.AccountID, p.TS)
	}
	// Обрізання превʼю: бекенд не має віддавати в UI необмежений текст
	// (та й картка фізично вміщує ~2 рядки).
	p.Body = truncateNotifyRunes(p.Body, 140)
	p.Title = truncateNotifyRunes(p.Title, 32)

	notifyWinMu.Lock()
	for _, q := range notifyPending {
		if q.ID == p.ID {
			notifyWinMu.Unlock()
			return // вже в черзі (ретрай/повторна подія)
		}
	}
	notifyPending = append(notifyPending, p)
	if len(notifyPending) > notifyMaxStack {
		notifyPending = notifyPending[len(notifyPending)-notifyMaxStack:]
	}
	created := notifyCreated
	notifyWinMu.Unlock()

	if !created {
		a.ensureNotifyWindow()
	}
	a.emit("notify:push", p)
}

// ensureNotifyWindow створює вікно віджета ОДНОГО разу (no-op, якщо вже
// існує). Вікно створюється ПРИХОВАНИМ: показ відбувається в
// SetNotifyPopupSize, коли фронтенд відомі фактичний розмір і наявні картки.
// Так ми уникаємо «порожнього» тоста, якщо контент ще не завантажився.
func (a *App) ensureNotifyWindow() {
	notifyWinMu.Lock()
	if notifyCreated {
		notifyWinMu.Unlock()
		return
	}
	notifyCreated = true
	notifyWinMu.Unlock()

	opts := windowOptions{
		Title:     "Shaurma Launcher",
		Width:     notifyWidth,
		Height:    notifyInitialH,
		Frameless: true,
		// Порожній Background → transparent за замовчуванням у shell
		// (resolveWindowBackground лишає transparent на Windows; на Linux
		// форсує solid — там прозорість GTK залежить від композитора).
		URL: notifyWindowURL(),
		// Вікно СТВОРЮЄТЬСЯ НЕпоказаним — показ лише коли відома висота.
		Hidden: true,
		// Не зʼявляється в панелі завдань (тост — не «вікно»).
		HiddenOnTaskbar: true,
		// Поверх усіх застосунків.
		AlwaysOnTop: true,
		// Правий нижній кут робочої області (без панелі завдань).
		Position: "bottom-right",
		MarginX:  notifyMarginX,
		MarginY:  notifyMarginY,
	}
	if b, err := json.Marshal(opts); err == nil {
		bridgeWindowCreate(notifyWindowID, string(b))
	}
}

// SetNotifyPopupSize викликається ВІКНОМ сповіщень після кожної зміни
// стосу карток: встановлює розмір вікна і привʼязує його до правого
// нижнього кута. h <= 0 → стос порожній → ховаємо вікно (але не знищуємо,
// щоб наступне сповіщення не вантажило webview заново).
func (a *App) SetNotifyPopupSize(w, h int) {
	if w <= 0 {
		w = notifyWidth
	}
	// Захист від сміття (спотворений DOM/божевільний вимір).
	if h > 1200 {
		h = 1200
	}
	if h <= 0 {
		bridgeWindowOp(notifyWindowID, "hide")
		return
	}
	bridgeWindowSetSize(notifyWindowID, w, h)
	// Привʼязка ДОДАТКОВО: розмір через bridge асинхронний, тож shell
	// читає вже оновлений у своєму потоці (обидва виклики — UI-потік
	// shell'а, ідуть послідовно).
	bridgeWindowOp(notifyWindowID, "anchor-bottom-right")
	// "show-inactive", а не "show": тост не має забирати фокус у того, хто
	// зараз друкує/грає в іншому застосунку (див. shell/window_show_*.go).
	bridgeWindowOp(notifyWindowID, "show-inactive")
}

// TakeNotifyPopup віддає накопичені сповіщення вікну при його монтуванні
// і чистить чергу. Виклик ЛИШЕ одного разу на життя вікна (фронтенд у
// onMount), тож події не губляться між create і реєстрацією слухача.
func (a *App) TakeNotifyPopup() []NotifyPayload {
	notifyWinMu.Lock()
	out := notifyPending
	notifyPending = nil
	notifyWinMu.Unlock()
	if len(out) == 0 {
		// Порожній slice, а не nil — фронт отримує [] і .map() не падає.
		return []NotifyPayload{}
	}
	return out
}

// hideNotifyPopup — сховати вікно (викликається після дії користувача).
func (a *App) hideNotifyPopup() {
	bridgeWindowOp(notifyWindowID, "hide")
}

// OpenChatFromNotification — клік по картці сповіщення: показує/активізує
// головне вікно, шле йому подію відкрити діалог і ховає тост.
//
// peerAccountID може бути порожнім (запит у друзі — у того ще немає
// accountId для чату): тоді шухляда «Друзі» просто відкриється без
// конкретного діалогу, і це теж правильний результат.
func (a *App) OpenChatFromNotification(peerAccountID string) error {
	// revealMainWindow (а не голі show/unminimise): скидає прапорець
	// фонового старту й однаково працює з трею/згорнутого стану. Клік —
	// дія користувача, тож додатково фокусуємо вікно.
	a.revealMainWindow()
	bridgeWindowOp("main", "focus")
	// Головне вікно стане активним (WM_ACTIVATE) — оновлюємо флаг, щоб
	// наступні сповіщення знову пригнічувались, поки користувач у застосунку.
	setNotifyMainWindowActive(true)
	// Подія йде у ВСІ вікна; реагує лише головне (App.svelte → відкрити
	// шухляду «Друзі» і конкретний діалог).
	a.emit("social:open-chat", peerAccountID)
	a.hideNotifyPopup()
	return nil
}

// CloseNotifyWindow — програмне знищення вікна (напр. перед виходом).
func (a *App) CloseNotifyWindow() {
	notifyWinMu.Lock()
	notifyCreated = false
	notifyPending = nil
	notifyWinMu.Unlock()
	bridgeWindowOp(notifyWindowID, "destroy")
}

// ── Дрібне ────────────────────────────────────────────────────────────────

// truncateNotifyRunes обрізає рядок до n рун і додає «…», якщо зрізався.
// Превʼю картки коротше за текст системного повідомлення, а функція чиста.
func truncateNotifyRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
