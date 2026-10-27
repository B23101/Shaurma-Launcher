package main

import (
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"shaurma-launcher-backend/internal/winbehavior"
)

// ── Окремі вікна консолі гри (ПО ЗБІРКАХ) ────────────────────────────────
//
// ПЕРЕНЕСЕННЯ: вся бізнес-логіка ("яке вікно для якої збірки", "коли
// ховати а не знищувати", "згортання до шапки") — ІДЕНТИЧНА оригіналу.
// Змінились ЛИШЕ виклики: замість application.Get().Window.NewWithOptions
// — bridgeWindowCreate(id, optionsJSON); замість win.Show() —
// bridgeWindowOp(id, "show"); і так для кожного виклику application.*.
// Саме вікно тепер живе в shell.exe під стабільним рядковим ID, backend
// тримає лише факт "існує" (consoleWindowIDs).
//
// Закриття вікна консолі (кнопка X) вирішує backend через централізований
// канал подій shell->backend ("window:closing:<id>" — див. handleShellEvent
// у main.go): сховати, а не знищити. Під час реального завершення
// програми (consoleAppQuitting) — дозволяємо закриття, щоб не блокувати
// вихід з процесу.

var (
	consoleWinMu sync.Mutex // захищає consoleWindowIDs (створення/перевірка)
	// consoleWindowIDs — buildID, для яких уже створене вікно консолі
	// (сам handle тепер живе в shell під id "console-<buildID>").
	consoleWindowIDs map[string]bool
	// consoleAppQuitting — прапорець реального завершення процесу
	// (bridgeAppQuit() з трею або закриття головного вікна без запущеної
	// гри). Коли він виставлений — закриття консолей НЕ скасовується.
	consoleAppQuitting atomic.Bool
)

// windowOptions — дзеркалить bridge.h options_json для window_create.
//
// Розширені поля (alwaysOnTop / hiddenOnTaskbar / noActivate / position /
// x / y / marginX / marginY) додані для віджета сповіщень у трей
// (notify_window.go). Старі поля без змін — нові відповідають значенням
// за замовчуванням (false / "" / 0), тож створення консолей не змінюється.
type windowOptions struct {
	Title     string `json:"title"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	MinWidth  int    `json:"minWidth"`
	MinHeight int    `json:"minHeight"`
	Frameless bool   `json:"frameless"`
	URL       string `json:"url"`
	// Hidden — створити вікно НЕпоказаним (див. опис у shell/window_exec.go).
	Hidden bool `json:"hidden,omitempty"`

	// AlwaysOnTop — вікно поверх усіх інших.
	AlwaysOnTop bool `json:"alwaysOnTop,omitempty"`
	// HiddenOnTaskbar — не показувати вікно в панелі завдань.
	HiddenOnTaskbar bool `json:"hiddenOnTaskbar,omitempty"`
	// NoActivate — показ вікна не викрадає фокус у головного вікна.
	NoActivate bool `json:"noActivate,omitempty"`
	// Position — "bottom-right": правий нижній кут робочої області.
	Position string `json:"position,omitempty"`
	X        int    `json:"x,omitempty"`
	Y        int    `json:"y,omitempty"`
	MarginX  int    `json:"marginX,omitempty"`
	MarginY  int    `json:"marginY,omitempty"`
}

// consoleWindowID — стабільний, generic ID вікна для bridge (замінює
// колишнє Name: "game-console-"+sanitize + мапу *application.WebviewWindow —
// тепер shell сам зберігає handle під цим рядком-ключем).
func consoleWindowID(buildID string) string {
	return "console-" + sanitizeWindowName(buildID)
}

// ShowConsoleWindowFor показує окреме вікно консолі КОНКРЕТНОЇ збірки
// (створює при потребі). Повторний виклик для тієї самої збірки лише
// показує/фокусує вже створене вікно. Безпечно викликати з будь-якої
// горутини: bridge-команди виконуються на UI-потоці shell'а.
func (a *App) ShowConsoleWindowFor(buildID string) {
	if buildID == "" {
		buildID = a.currentConsoleBuildID()
	}
	if buildID == "" {
		return
	}
	id := consoleWindowID(buildID)

	consoleWinMu.Lock()
	if consoleWindowIDs == nil {
		consoleWindowIDs = map[string]bool{}
	}
	exists := consoleWindowIDs[id]
	if !exists {
		consoleWindowIDs[id] = true
	}
	consoleWinMu.Unlock()

	if !exists {
		opts := windowOptions{
			Title:     "Shaurma Launcher — Консоль гри",
			Width:     1100,
			Height:    760,
			MinWidth:  640,
			MinHeight: 420,
			Frameless: true,
			// buildID у query: сторінка консолі прив'язується САМЕ до цієї
			// збірки (GameConsole читає ?build= у console.ts/GameConsole).
			// win — ТОЧНИЙ id вікна консолі (consoleWindowID), передається
			// явно, щоб фронтенд не перекалькулював його сам: JS-репліка
			// sanitizeWindowName не збігалась би для не-ASCII id (Cyrillic =
			// 2 UTF-16 code units проти 1 rune у Go), і скопована подія
			// window:active:<id> від shell ніколи б не співпала зі слухачем
			// (console.ts: initWindowActive('window:active:' + win)).
			URL: consoleWindowURL() + "?build=" + url.QueryEscape(buildID) + "&win=" + url.QueryEscape(id),
		}
		optsJSON, _ := json.Marshal(opts)
		bridgeWindowCreate(id, string(optsJSON))
		// Хук закриття реалізує shell ГЕНЕРИКОМ: він завжди шле подію
		// "window:closing:<id>" через __dispatch, а backend вирішує —
		// сховати чи дозволити закриття (onShellWindowClosing у
		// tray_menu.go/console_window.go).
	}

	bridgeWindowOp(id, "show")
	bridgeWindowOp(id, "unminimise")
	// Максимізований стан вікна консолі НЕ запам'ятовується між
	// відкриттями: розгорнуте на весь екран вікно може заважати.
	bridgeWindowOp(id, "unmaximise")
	bridgeWindowOp(id, "focus")

	// Вікно перечитує знімок/контекст САМЕ цієї збірки (console:focus).
	a.emit("console:focus", buildID)
}

// onShellWindowClosing — викликається з handleShellEvent (main.go), коли
// shell шле "window:closing:<id>". Повертає action JSON:
//   - "hide"  — сховати вікно замість знищення (лог продовжує писатись);
//   - "close" — дозволити закриття (реальний вихід з програми).
//
// ІДЕНТИЧНА логіка оригінального хука RegisterHook(WindowClosing) у
// console_window.go: якщо процес не завершується насправді — відхиляємо
// закриття, інакше дозволяємо.
func (a *App) onShellWindowClosing(id string) string {
	if consoleAppQuitting.Load() {
		return `{"action":"close"}`
	}
	return `{"action":"hide"}`
}

// currentConsoleBuildID — збірка, для якої показується «основне» вікно
// консолі (виклик без явного buildID): вибрана зараз у головному вікні,
// інакше — остання запущена.
func (a *App) currentConsoleBuildID() string {
	if a.activeInstanceID != "" {
		return a.activeInstanceID
	}
	return a.lastInstanceID
}

// sanitizeWindowName перетворює buildID на безпечний ідентифікатор
// (лише [A-Za-z0-9-_], решта — дефіси).
func sanitizeWindowName(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	if b.Len() == 0 {
		return "console"
	}
	return b.String()
}

// consoleNormalSize — збережені «звичайні» розміри вікна консолі на
// випадок згортання у «шапку» (SetConsoleCollapsed). Ключ — buildID.
var consoleNormalSize = map[string][2]int{}

// SetConsoleCollapsed згортає/розгортає ОКРЕМЕ вікно консолі до «шапки»:
// зменшує висоту вікна, щоб користувач бачив гру, лишаючи кнопки дій.
func (a *App) SetConsoleCollapsed(buildID string, collapsed bool) {
	if buildID == "" {
		buildID = a.currentConsoleBuildID()
	}
	if buildID == "" {
		return
	}
	id := consoleWindowID(buildID)

	w, h := bridgeWindowGetSize(id)
	const headerH = 56 // висота «шапки» консолі
	if collapsed {
		if _, ok := consoleNormalSize[buildID]; !ok {
			consoleNormalSize[buildID] = [2]int{w, h}
		}
		bridgeWindowSetMinSize(id, 640, headerH)
		bridgeWindowSetSize(id, w, headerH)
	} else {
		if norm, ok := consoleNormalSize[buildID]; ok && norm[1] > headerH {
			bridgeWindowSetSize(id, norm[0], norm[1])
		}
		delete(consoleNormalSize, buildID)
		bridgeWindowSetMinSize(id, 640, 420)
	}
}

// ShowConsoleWindow показує вікно консолі для ПОТОЧНОЇ активної збірки
// (сумісність: старий виклик без buildID).
func (a *App) ShowConsoleWindow() {
	a.ShowConsoleWindowFor(a.currentConsoleBuildID())
}

// OpenPackConsole відкриває окреме вікно консолі КОНКРЕТНОЇ збірки
// (кнопка «Консоль» на сторінці збірки).
func (a *App) OpenPackConsole(packID string) {
	a.ShowConsoleWindowFor(packID)
}

// HideConsoleWindowFor ховає вікно консолі конкретної збірки (лог
// продовжує записуватись).
func (a *App) HideConsoleWindowFor(buildID string) {
	if buildID == "" {
		buildID = a.currentConsoleBuildID()
	}
	if buildID == "" {
		return
	}
	bridgeWindowOp(consoleWindowID(buildID), "hide")
}

// HideConsoleWindow ховає ВСІ відкриті вікна консолей (збірок і серверів —
// консолі серверів мають власний реєстр, див. server_console_window.go).
func (a *App) HideConsoleWindow() {
	consoleWinMu.Lock()
	ids := make([]string, 0, len(consoleWindowIDs))
	for id := range consoleWindowIDs {
		ids = append(ids, id)
	}
	consoleWinMu.Unlock()
	for _, id := range ids {
		bridgeWindowOp(id, "hide")
	}
	a.hideAllServerConsoleWindows()
}

// ToggleConsoleWindowFor — зручний перемикач для кнопки «відкрити вікно»
// у консолі: показує/ховає вікно конкретної збірки.
func (a *App) ToggleConsoleWindowFor(buildID string) {
	if buildID == "" {
		buildID = a.currentConsoleBuildID()
	}
	if buildID == "" {
		return
	}
	if a.IsConsoleWindowOpen(buildID) {
		a.HideConsoleWindowFor(buildID)
	} else {
		a.ShowConsoleWindowFor(buildID)
	}
}

// ToggleConsoleWindow — перемикач для поточної активної збірки.
func (a *App) ToggleConsoleWindow() {
	a.ToggleConsoleWindowFor("")
}

// IsConsoleWindowOpen повертає, чи відкрите зараз вікно консолі
// конкретної збірки.
func (a *App) IsConsoleWindowOpen(buildID string) bool {
	if buildID == "" {
		buildID = a.currentConsoleBuildID()
	}
	return bridgeWindowIsVisible(consoleWindowID(buildID))
}

// maybeAutoShowConsole показує ОКРЕМЕ вікно консолі для КОНКРЕТНОЇ збірки
// згідно з налаштуваннями showConsoleOnLaunch / showConsoleOnCrash /
// showConsoleOnClose. (Без змін порівняно з оригіналом.)
func (a *App) maybeAutoShowConsole(trigger, buildID string) {
	s := a.cfg.GetSettings()
	show := false
	if trigger == "launch" && s.ShowConsoleOnLaunch {
		show = true
	}
	if trigger == "crash" && s.ShowConsoleOnCrash {
		show = true
	}
	if trigger == "close" && s.ShowConsoleOnClose {
		show = true
	}
	if show {
		a.ShowConsoleWindowFor(buildID)
	}
}

// consoleWindowURL визначає URL сторінки консолі. У прод-збірці —
// відносний шлях /console.html; у dev-режимі — адреса Vite dev-сервера.
func consoleWindowURL() string {
	if dev := os.Getenv("FRONTEND_DEVSERVER_URL"); dev != "" {
		return strings.TrimSuffix(dev, "/") + "/console.html"
	}
	return "/console.html"
}

// QuitLauncher — завершення процесу з фронтенду (кнопка X у топбарі
// головного вікна). ДУБЛЮЄ логіку закриття головного вікна (див.
// handleShellEvent -> onMainWindowClosing у tray_menu.go), бо
// Application.Quit() з фронтенду йде напряму в impl.destroy() і НЕ
// проходить через WindowClosing-хук головного вікна.
//
// ІДЕНТИЧНА логіка оригіналу, лише application.Get().Quit() замінено на
// bridgeAppQuit().
func (a *App) QuitLauncher() {
	// Йде гра або обрано «× ховає в трей» → ховаємо, а не виходимо
	// (те саме рішення, що й у onMainWindowClosing).
	closeCtx := winbehavior.CloseContext{
		GameRunning: a.sessionsM != nil && a.sessionsM.AnyRunning(),
		P2PActive:   a.p2pAPI != nil && a.p2pAPI.HasActiveSessions(),
	}
	if reason := winbehavior.CloseTrayReason(a.windowSettings(), closeCtx); reason != winbehavior.TrayReasonNone {
		a.hideToTray("quit-launcher:" + reason)
		a.notifyTrayContinues(a.windowSettings(), closeCtx)
		return
	}
	// Якщо запущено сервери — попереджаємо.
	if a.srvAPI != nil && a.srvAPI.HasRunningServers() {
		a.emit("server:quit-warning", nil)
		return
	}
	if a.syncMgr != nil && a.syncMgr.HasActive() && !forceQuitConfirmed {
		a.emit("sync:quit-warning", a.syncMgr.ActivePackIDs())
		return
	}
	if a.ShareBusy() && !forceQuitConfirmed {
		a.emit("share:quit-warning")
		return
	}
	consoleAppQuitting.Store(true)
	if a.syncMgr != nil {
		a.syncMgr.PauseAll()
	}
	bridgeAppQuit()
}
