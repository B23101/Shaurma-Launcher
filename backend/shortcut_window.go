package main

// shortcut_window.go — запуск гри з ЯРЛИКА збірки на робочому столі.
//
//  1. Лаунчер ЩЕ НЕ БУВ відкритий (або сидів у треї): головне вікно НЕ
//     показується. Замість нього — окреме мале вікно запуску ярлика
//     (shortcut.html, id "shortcut-launch") з іконкою/назвою збірки,
//     прогресом стадій і кнопкою «Скасувати».
//  2. Гра стартувала → вікно запуску зникає, лаунчер лишається в треї.
//     Що буде після закриття гри (повернути головний екран чи ні), вирішують
//     налаштування користувача (gameExitAction) — як і для звичайного запуску.
//  3. Помилка запуску / крах гри під час завантаження / потрібна дія
//     користувача (RAM-попередження тощо) → вікно запуску закривається і
//     відкривається головний екран з усім, що потрібно (тост, діалог краху).
//  4. Головне вікно БУЛО відкрите до кліку по ярлику → жодного окремого
//     вікна: гра запускається так само, як після кнопки «Грати».
//
// Вся ця логіка — тут і у ui_ready.go/main.go; фронтенд лише показує
// сторінку вікна (frontend/shortcut.html) і запускає саму гру у приховане
// головне вікно (App.svelte: launchFromShortcut).

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// shortcutWindowID — стабільний ID вікна у shell.
	shortcutWindowID = "shortcut-launch"
	// shortcutWindowW/H — розмір вікна запуску ярлика (DIP).
	shortcutWindowW = 360
	shortcutWindowH = 420
	// shortcutLoadingGrace — скільки після старту гри вважається
	// «завантаженням»: краш у цей час точно не планований вихід, тож
	// головний екран відкривається незалежно від restoreOnCrash.
	shortcutLoadingGrace = 2 * time.Minute
	// shortcutSafetyReveal — запобіжник холодного старту з ярлика: якщо за
	// цей час не з'явилось ні вікно запуску, ні гра — показуємо головне
	// вікно, щоб лаунчер ніколи не лишався невидимим.
	shortcutSafetyReveal = 25 * time.Second

	shortcutModeWindow = "window"
	shortcutModePlain  = "plain"
)

// ShortcutWindowInfo — що показує вікно запуску ярлика. Назву/іконку/колір
// надсилає головне (приховане) вікно, яке вже знає збірку; до цього вікно
// показує нейтральний вигляд.
type ShortcutWindowInfo struct {
	PackID     string `json:"packId"`
	Name       string `json:"name"`
	IconURL    string `json:"iconUrl"`
	PresetIcon string `json:"presetIcon"`
	Color      string `json:"color"`
}

var (
	shortcutMu sync.Mutex
	// shortcutOpen — вікно запуску ярлика створене й ще не закрите.
	shortcutOpen bool
	shortcutInfo ShortcutWindowInfo
	// shortcutSessions — збірки, запущені через вікно ярлика, → час старту
	// гри. Потрібно, щоб відрізнити «крах під час завантаження».
	shortcutSessions = map[string]time.Time{}
	// shortcutColdStart — лаунчер стартував заради ярлика (shell: "shortcut").
	shortcutColdStart atomic.Bool
)

// markShortcutStart викликається з backend_init, коли shell повідомив
// shortcut:true: головне вікно не показується (як у фоновому старті), а
// замість нього backend відкриє вікно запуску ярлика.
func markShortcutStart() {
	shortcutColdStart.Store(true)
	markBackgroundStart()
}

// shortcutWindowURL — URL сторінки вікна (аналог notifyWindowURL).
func shortcutWindowURL() string {
	if dev := os.Getenv("FRONTEND_DEVSERVER_URL"); dev != "" {
		return strings.TrimSuffix(dev, "/") + "/shortcut.html"
	}
	return "/shortcut.html"
}

// decideShortcutMode — вікно запуску чи звичайний запуск «як кнопка Грати».
// Головне вікно видиме (відкритий головний екран) → plain. Сховане в трей,
// ще не створене, холодний старт з ярлика → window.
func (a *App) decideShortcutMode() string {
	if !shortcutColdStart.Load() && bridgeWindowIsVisible("main") {
		return shortcutModePlain
	}
	return shortcutModeWindow
}

// openShortcutWindow створює вікно запуску ярлика (або перемикає вже
// відкрите на іншу збірку). Безпечно викликати з будь-якої горутини.
func (a *App) openShortcutWindow(packID string) {
	shortcutMu.Lock()
	already := shortcutOpen
	shortcutOpen = true
	shortcutInfo = ShortcutWindowInfo{PackID: packID}
	shortcutMu.Unlock()

	if already {
		// Вікно вже є — просто просимо сторінку скинути вигляд під нову збірку.
		a.emit("shortcut-window:info", ShortcutWindowInfo{PackID: packID})
		bridgeWindowOp(shortcutWindowID, "show")
		return
	}

	a.ensureTray()
	opts := windowOptions{
		Title:     "Shaurma Launcher",
		Width:     shortcutWindowW,
		Height:    shortcutWindowH,
		MinWidth:  shortcutWindowW,
		MinHeight: shortcutWindowH,
		Frameless: true,
		URL:       shortcutWindowURL(),
		// Суцільний фон як у фронтенду: показ до першого малювання
		// сторінки не блимає білим. Саме ж показ — по ShortcutWindowReady.
		Background:    "solid",
		BackgroundHex: "#0a0a0f",
		Hidden:        true,
	}
	if b, err := json.Marshal(opts); err == nil {
		bridgeWindowCreate(shortcutWindowID, string(b))
	}
	// Запасний показ: якщо сторінка не змогла викликати ShortcutWindowReady.
	time.AfterFunc(3*time.Second, func() {
		shortcutMu.Lock()
		open := shortcutOpen
		shortcutMu.Unlock()
		if open {
			bridgeWindowOp(shortcutWindowID, "show")
		}
	})
}

// closeShortcutWindow знищує вікно запуску ярлика (no-op, якщо його немає).
// Повертає true, якщо вікно справді було відкрите.
func (a *App) closeShortcutWindow() bool {
	shortcutMu.Lock()
	was := shortcutOpen
	shortcutOpen = false
	shortcutMu.Unlock()
	if was {
		bridgeWindowOp(shortcutWindowID, "destroy")
	}
	return was
}

// escalateToMainWindow закриває вікно запуску й відкриває головний екран.
// Використовується, коли лаунчеру потрібен користувач: помилка, діалог,
// краш.
func (a *App) escalateToMainWindow() {
	a.closeShortcutWindow()
	a.revealMainWindow()
	bridgeWindowOp("main", "focus")
}

// ── Хуки життєвого циклу гри ────────────────────────────────────────────

// shortcutWindowGameStarted — гра стартувала. Якщо запуск ішов через вікно
// ярлика: закриваємо його, гарантуємо трей і запам'ятовуємо час старту.
// Повертає true, коли запуск був «ярликовий» — тоді звичайна реакція вікна
// на запуск гри (applyGameLaunchWindow: згорнути/сховати) не потрібна:
// головне вікно й так сховане, а minimise на сховане вікно показав би його
// згорнутим у панелі завдань.
func (a *App) shortcutWindowGameStarted(buildID string) bool {
	shortcutMu.Lock()
	match := shortcutOpen && shortcutInfo.PackID == buildID
	if match {
		shortcutSessions[buildID] = time.Now()
	}
	shortcutMu.Unlock()
	if !match {
		return false
	}
	a.closeShortcutWindow()
	a.ensureTray()
	return true
}

// shortcutWindowLaunchFailed — запуск збірки закінчився помилкою (до старту
// гри): закриваємо вікно ярлика й відкриваємо головний екран, де користувач
// побачить тост/діалог помилки.
func (a *App) shortcutWindowLaunchFailed(buildID string) {
	shortcutMu.Lock()
	match := shortcutOpen && shortcutInfo.PackID == buildID
	shortcutMu.Unlock()
	if match {
		a.escalateToMainWindow()
	}
}

// shortcutWindowGameExited — гра завершилась. Повертає true, якщо це був
// КРАШ під час завантаження гри, запущеної з ярлика: головне вікно
// відкрито (діалог краху/Pack Doctor покаже фронтенд) і додаткових дій за
// налаштуваннями не потрібно. Усе інше (штатний вихід, ручна зупинка, пізній
// краш) вирішують налаштування користувача — applyGameExitWindow.
func (a *App) shortcutWindowGameExited(buildID string, code int, wasUserStop bool) bool {
	shortcutMu.Lock()
	started, ok := shortcutSessions[buildID]
	delete(shortcutSessions, buildID)
	shortcutMu.Unlock()
	if !ok || wasUserStop || code == 0 {
		return false
	}
	if time.Since(started) > shortcutLoadingGrace {
		return false
	}
	a.revealMainWindow()
	bridgeWindowOp("main", "focus")
	return true
}

// shortcutSafetyNet — запобіжник холодного старту з ярлика (див.
// shortcutSafetyReveal). Викликається з UIReady.
func (a *App) shortcutSafetyNet() {
	time.AfterFunc(shortcutSafetyReveal, func() {
		shortcutMu.Lock()
		open := shortcutOpen
		busy := len(shortcutSessions) > 0
		shortcutMu.Unlock()
		if open || busy {
			return
		}
		if a.sessionsM != nil && a.sessionsM.AnyRunning() {
			return
		}
		if bridgeWindowIsVisible("main") {
			return
		}
		a.revealMainWindow()
	})
}

// ── Біндінги для фронтенду ──────────────────────────────────────────────

// TakeShortcutLaunchInfo — стан вікна запуску при монтуванні його сторінки
// (подія shortcut-window:info могла прийти ще до підписки).
func (a *App) TakeShortcutLaunchInfo() ShortcutWindowInfo {
	shortcutMu.Lock()
	defer shortcutMu.Unlock()
	return shortcutInfo
}

// ShortcutWindowReady — сторінка вікна змонтована: показуємо вікно.
func (a *App) ShortcutWindowReady() {
	shortcutMu.Lock()
	open := shortcutOpen
	shortcutMu.Unlock()
	if !open {
		return
	}
	bridgeWindowOp(shortcutWindowID, "show")
	bridgeWindowOp(shortcutWindowID, "focus")
}

// SetShortcutWindowInfo — приховане головне вікно повідомляє назву, іконку
// й колір збірки для вікна запуску.
func (a *App) SetShortcutWindowInfo(info ShortcutWindowInfo) {
	shortcutMu.Lock()
	if !shortcutOpen || shortcutInfo.PackID != info.PackID {
		shortcutMu.Unlock()
		return
	}
	shortcutInfo = info
	shortcutMu.Unlock()
	a.emit("shortcut-window:info", info)
}

// ShortcutWindowEscalate — головному вікну для запуску потрібен користувач
// (RAM-попередження, проблеми з модами, помилка): відкрити головний екран.
func (a *App) ShortcutWindowEscalate() {
	a.escalateToMainWindow()
}

// ShortcutWindowCancel — кнопка «Скасувати» у вікні запуску (або закриття
// вікна): зупиняємо запуск і повертаємо звичайний головний екран.
func (a *App) ShortcutWindowCancel() {
	shortcutMu.Lock()
	packID := shortcutInfo.PackID
	shortcutMu.Unlock()
	a.closeShortcutWindow()
	if packID != "" {
		// М'яко скасовуємо качку компонентів; якщо гра встигла стартувати —
		// зупиняємо її як звичайно (помилки ігноруються: нічого не запущено).
		a.CancelGameInstall(packID)
		_ = a.StopInstance(packID)
	}
	a.revealMainWindow()
	bridgeWindowOp("main", "focus")
}
