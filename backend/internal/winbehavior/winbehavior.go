// Package winbehavior — ЄДИНЕ місце, де описано, як вікно лаунчера поводиться
// на трьох етапах життя: запуск із системою, закриття вікна (×) та гра.
//
// Раніше ця поведінка була розсипана по трьох незв'язаних булевих
// перемикачах (openOnStartup, closeOnLaunch, exitOnGameClose) і одному
// «привиду» hideOnGameOpen, що дублював closeOnLaunch. Комбінації булів
// давали незрозумілі стани («закривати лаунчер» насправді згортало в трей;
// «вийти» працювало лише коли гра остання тощо).
//
// Тепер кожен етап — ОДИН параметр з іменованими режимами, а рішення
// «що робити з вікном» — чисті функції без залежностей від UI/shell, тож
// їх покрито тестами (winbehavior_test.go) окремо від решти лаунчера.
package winbehavior

import (
	"encoding/json"
	"time"
)

// ── Запуск лаунчера разом із системою ───────────────────────────────────

const (
	// StartupNever — лаунчер не додається в автозавантаження ОС.
	StartupNever = "never"
	// StartupWindow — запускається з системою й одразу показує вікно.
	StartupWindow = "window"
	// StartupBackground — запускається з системою без вікна, лише трей:
	// чати, друзі й сповіщення працюють, а вікно відкривається за кліком.
	StartupBackground = "background"
)

// ── Закриття вікна (кнопка ×, Alt+F4) ───────────────────────────────────

const (
	// CloseQuit — × завершує лаунчер (стандартна поведінка).
	CloseQuit = "quit"
	// CloseTray — × ховає вікно в трей, лаунчер лишається у фоні.
	CloseTray = "tray"
)

// ── Кнопка «Згорнути» (–) ───────────────────────────────────────────────

const (
	// MinimizeTaskbar — звичайне згортання на панель завдань.
	MinimizeTaskbar = "taskbar"
	// MinimizeTray — згортання одразу в трей (без кнопки на панелі).
	MinimizeTray = "tray"
)

// ── Вікно лаунчера під час гри ──────────────────────────────────────────

const (
	// LaunchKeep — вікно лаунчера не чіпати.
	LaunchKeep = "keep"
	// LaunchMinimize — згорнути на панель завдань.
	LaunchMinimize = "minimize"
	// LaunchTray — сховати в трей.
	LaunchTray = "tray"
)

const (
	// ExitRestore — повернути вікно, коли гра закрилась (якщо воно було
	// сховане/згорнуте; видиме вікно просто лишається).
	ExitRestore = "restore"
	// ExitStay — нічого не робити: що було зі вікном, те й лишається
	// (зручно разом з автозапуском у фоні).
	ExitStay = "stay"
	// ExitQuit — закрити лаунчер повністю.
	ExitQuit = "quit"
)

// Межі затримки згортання після старту гри (секунди).
const (
	MaxLaunchDelaySec = 120
)

// Settings — підмножина налаштувань лаунчера, яка стосується вікна.
type Settings struct {
	StartupMode        string
	CloseAction        string
	MinimizeAction     string
	GameLaunchAction   string
	GameLaunchDelaySec int
	GameExitAction     string
	RestoreOnCrash     bool
}

// Defaults — значення за замовчуванням для НОВОЇ установки.
//
// Автозапуск «з вікном»: лаунчер не має ховатися в трей без явного вибору
// користувача («У фоні» вмикається в налаштуваннях). Наявні користувачі
// цей дефолт НЕ отримують — див. MigrateLegacy.
func Defaults() Settings {
	return Settings{
		StartupMode:        StartupWindow,
		CloseAction:        CloseQuit,
		MinimizeAction:     MinimizeTaskbar,
		GameLaunchAction:   LaunchKeep,
		GameLaunchDelaySec: 0,
		GameExitAction:     ExitRestore,
		RestoreOnCrash:     true,
	}
}

func oneOf(v string, allowed ...string) bool {
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
}

// Normalize приводить значення до допустимих: невідомий/порожній режим
// замінюється дефолтом, затримка стискається до 0..MaxLaunchDelaySec.
// RestoreOnCrash — bool, тут не чіпається (його відсутність у старому
// файлі обробляє MigrateLegacy).
func Normalize(s Settings) Settings {
	d := Defaults()
	if !oneOf(s.StartupMode, StartupNever, StartupWindow, StartupBackground) {
		s.StartupMode = d.StartupMode
	}
	if !oneOf(s.CloseAction, CloseQuit, CloseTray) {
		s.CloseAction = d.CloseAction
	}
	if !oneOf(s.MinimizeAction, MinimizeTaskbar, MinimizeTray) {
		s.MinimizeAction = d.MinimizeAction
	}
	if !oneOf(s.GameLaunchAction, LaunchKeep, LaunchMinimize, LaunchTray) {
		s.GameLaunchAction = d.GameLaunchAction
	}
	if !oneOf(s.GameExitAction, ExitRestore, ExitStay, ExitQuit) {
		s.GameExitAction = d.GameExitAction
	}
	if s.GameLaunchDelaySec < 0 {
		s.GameLaunchDelaySec = 0
	}
	if s.GameLaunchDelaySec > MaxLaunchDelaySec {
		s.GameLaunchDelaySec = MaxLaunchDelaySec
	}
	return s
}

// ── Міграція зі старих булевих налаштувань ──────────────────────────────

func rawBool(raw map[string]json.RawMessage, key string) (val, present bool) {
	b, ok := raw[key]
	if !ok {
		return false, false
	}
	var v bool
	if json.Unmarshal(b, &v) != nil {
		return false, true
	}
	return v, true
}

// MigrateLegacy переносить старі ключі settings.json у нові режими.
// raw — розібраний settings.json (ключ → сирий JSON). cur — поточні
// (вже десеріалізовані) значення; функція змінює лише ті поля, яких у
// файлі ще немає, тож вже збережений вибір користувача ніколи не
// перезаписується.
//
//	openOnStartup  true  → startupMode = window   (було «відкривати вікно»)
//	openOnStartup  false → startupMode = never
//	closeOnLaunch  true  → gameLaunchAction = tray (старий «hide to tray»)
//	exitOnGameClose true → gameExitAction = quit
//
// ВАЖЛИВО: наявний файл БЕЗ startupMode трактується як «автозапуск не
// було» (never), а не як новий дефолт «у фоні» — оновлення лаунчера не
// має самовільно додавати його в автозавантаження ОС.
func MigrateLegacy(raw map[string]json.RawMessage, cur Settings) Settings {
	if _, ok := raw["startupMode"]; !ok {
		if on, _ := rawBool(raw, "openOnStartup"); on {
			cur.StartupMode = StartupWindow
		} else {
			cur.StartupMode = StartupNever
		}
	}
	if _, ok := raw["gameLaunchAction"]; !ok {
		if on, _ := rawBool(raw, "closeOnLaunch"); on {
			cur.GameLaunchAction = LaunchTray
		} else {
			cur.GameLaunchAction = LaunchKeep
		}
	}
	if _, ok := raw["gameExitAction"]; !ok {
		if on, _ := rawBool(raw, "exitOnGameClose"); on {
			cur.GameExitAction = ExitQuit
		} else {
			cur.GameExitAction = ExitRestore
		}
	}
	if _, ok := raw["closeAction"]; !ok {
		cur.CloseAction = CloseQuit
	}
	if _, ok := raw["minimizeAction"]; !ok {
		cur.MinimizeAction = MinimizeTaskbar
	}
	// Нове поле, дефолт true: нульовий Go false вимкнув би його всім.
	if _, ok := raw["restoreOnCrash"]; !ok {
		cur.RestoreOnCrash = true
	}
	return Normalize(cur)
}

// ── Автозапуск ──────────────────────────────────────────────────────────

// Прапорці командного рядка, з якими ОС запускає лаунчер при вході.
const (
	// FlagBackground — стартувати без вікна (лише трей).
	FlagBackground = "--background"
	// FlagAutostart — запуск ініційовано автозавантаженням ОС (а не
	// користувачем): shell не повинен красти фокус чи показувати splash.
	FlagAutostart = "--autostart"
)

// AutostartEnabled — чи треба реєструвати лаунчер в автозавантаженні.
func AutostartEnabled(mode string) bool {
	return mode == StartupWindow || mode == StartupBackground
}

// AutostartArgs — аргументи, з якими ОС запускатиме лаунчер.
func AutostartArgs(mode string) []string {
	switch mode {
	case StartupBackground:
		return []string{FlagAutostart, FlagBackground}
	case StartupWindow:
		return []string{FlagAutostart}
	}
	return nil
}

// HasFlag — чи є прапорець серед аргументів (для shell/second-instance).
func HasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

// ── Рішення про вікно ───────────────────────────────────────────────────

// WindowAction — що зробити з головним вікном.
type WindowAction int

const (
	WindowNone     WindowAction = iota // нічого не робити
	WindowMinimize                     // згорнути на панель завдань
	WindowHide                         // сховати в трей
	WindowShow                         // показати/розгорнути й повернути
	WindowQuit                         // завершити лаунчер
)

func (a WindowAction) String() string {
	switch a {
	case WindowMinimize:
		return "minimize"
	case WindowHide:
		return "hide"
	case WindowShow:
		return "show"
	case WindowQuit:
		return "quit"
	}
	return "none"
}

// LaunchPlan — що зробити з вікном після старту гри і коли.
type LaunchPlan struct {
	Action WindowAction
	Delay  time.Duration
}

// PlanGameLaunch — рішення про вікно в момент успішного старту гри.
func PlanGameLaunch(s Settings) LaunchPlan {
	s = Normalize(s)
	var act WindowAction
	switch s.GameLaunchAction {
	case LaunchMinimize:
		act = WindowMinimize
	case LaunchTray:
		act = WindowHide
	default:
		return LaunchPlan{Action: WindowNone}
	}
	return LaunchPlan{Action: act, Delay: time.Duration(s.GameLaunchDelaySec) * time.Second}
}

// ExitContext — обставини завершення гри.
type ExitContext struct {
	// OtherSessionsRunning — ще йде хоча б одна гра (паралельна сесія).
	OtherSessionsRunning bool
	// Crashed — гра завершилась аварійно (код ≠ 0 і не ручна зупинка).
	Crashed bool
	// QuitBlocked — повний вихід небезпечний: запущені сервери або йде
	// синхронізація збірок. Тоді замість виходу вікно повертається.
	QuitBlocked bool
}

// PlanGameExit — рішення про вікно, коли гра закрилась.
//
// Правила (від найважливішого):
//  1. Поки йде ще одна гра — вікно НЕ чіпаємо (вихід однієї сесії не
//     повинен ні закрити лаунчер, ні висмикнути вікно з-під іншої гри).
//  2. Краш + RestoreOnCrash — завжди повертаємо вікно, навіть при
//     «вийти»/«нічого не робити»: користувач має побачити, що сталось.
//  3. Далі — за налаштуванням GameExitAction; «вийти» не виконується,
//     якщо QuitBlocked (замість цього вікно повертається).
func PlanGameExit(s Settings, c ExitContext) WindowAction {
	s = Normalize(s)
	if c.OtherSessionsRunning {
		return WindowNone
	}
	if c.Crashed && s.RestoreOnCrash {
		return WindowShow
	}
	switch s.GameExitAction {
	case ExitQuit:
		if c.QuitBlocked {
			return WindowShow
		}
		return WindowQuit
	case ExitStay:
		return WindowNone
	default:
		return WindowShow
	}
}

// CloseContext — обставини закриття головного вікна.
type CloseContext struct {
	GameRunning bool
	// P2PActive — лаунчер хостить P2P-кімнати або тримає гостьові: процес має жити в треї.
	P2PActive bool
}

// CloseOutcome — що робити з запитом на закриття вікна.
type CloseOutcome int

const (
	// CloseProceed — закрити лаунчер (подальші перевірки — серверів/
	// синхронізації — робить викликач).
	CloseProceed CloseOutcome = iota
	// CloseToTray — сховати вікно в трей, процес живе далі.
	CloseToTray
)

// Причини, з яких × ховає вікно в трей замість виходу (для логу й сповіщення).
const (
	TrayReasonNone        = ""
	TrayReasonGame        = "game-running"
	TrayReasonP2P         = "p2p-active"
	TrayReasonCloseAction = "close-action-tray"
)

// CloseTrayReason — чому × має сховати вікно в трей («» — не ховати).
// Йде гра → лаунчер веде сесію; активна P2P-сесія → процес має жити;
// інакше — користувач сам обрав CloseAction=tray.
func CloseTrayReason(s Settings, c CloseContext) string {
	s = Normalize(s)
	switch {
	case c.GameRunning:
		return TrayReasonGame
	case c.P2PActive:
		return TrayReasonP2P
	case s.CloseAction == CloseTray:
		return TrayReasonCloseAction
	}
	return TrayReasonNone
}

// CloseTrayForced — трей не за вибором користувача (гра/P2P при
// CloseAction=quit): про це треба повідомити, а не ховати мовчки.
func CloseTrayForced(s Settings, c CloseContext) bool {
	r := CloseTrayReason(s, c)
	return r == TrayReasonGame || r == TrayReasonP2P
}

// PlanClose — рішення при ×. Йде гра або активна P2P-сесія → в трей (лаунчер
// веде сесію, закриття вікна не повинно її обривати); інакше за CloseAction.
func PlanClose(s Settings, c CloseContext) CloseOutcome {
	if CloseTrayReason(s, c) != TrayReasonNone {
		return CloseToTray
	}
	return CloseProceed
}
