package main

import (
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// ── Окремі вікна консолі гри (ПО ЗБІРКАХ) ────────────────────────────────
//
// КОЖНА збірка має ВЛАСНЕ незалежне вікно консолі: можна запустити 2, 3
// чи 5 версій гри одночасно — кожна отримує своє вікно зі своїм логом і
// своїм станом. Вікна:
//   - створюються ліниво при першому ShowConsoleWindowFor(<buildID>) (не
//     займають ресурси до того, як користувач їх відкриє);
//   - завантажують окрему HTML-сторінку /console.html?build=<id> (Vite
//     multi-page build, див. frontend/console.html), яка монтує
//     GameConsole.svelte, прив'язаний САМЕ до цієї збірки через ?build=;
//   - закриття вікна (кнопка X) ЛИШЕ ХОВАЄ його, а не знищує — буфер логу
//     живе на бекенді (по парі акаунт × збірка) і продовжує записуватись,
//     поки гра запущена; повторний показ повертає те саме вікно;
//   - коли головне вікно йде у трей (closeOnLaunch / hideOnGameOpen) —
//     вікна консолей ЛИШАЮТЬСЯ ЖИВИМИ. Процес завершується лише через
//     «Вийти» у треї.
//
// ВАЖЛИВО про потоки: операції з вікном (створення/Show/Hide/UnMinimise)
// у Wails v3 повинні виконуватись на головному потоці. Ці методи можуть
// викликатись з різних горутин (Wails-binding, game-monitor горутина на
// краш-події, UI-подія закриття), тому ВСЯ робота з вікном — включно зі
// створенням (NewWithOptions) — обгортається application.InvokeSync.
// М'ютекс consoleWinMu при цьому НЕ утримується під час InvokeSync (він
// звільняється до виклику), тож блокування головного потоку не створює
// deadlock.

var (
	consoleWinMu sync.Mutex // захищає consoleWindows (створення/перевірка)
	// consoleWindows — вікна консолі ПО ЗБІРКАХ: ключ — buildID, значення —
	// вікно. Кілька збірок = кілька незалежних вікон (кожне зі своїм логом).
	consoleWindows map[string]*application.WebviewWindow
	// consoleAppQuitting — прапорець реального завершення процесу
	// (application.Get().Quit() з трею або закриття головного вікна без
	// запущеної гри). Коли він виставлений — WindowClosing консолей НЕ
	// скасовується, щоб не блокувати штатний вихід з програми.
	consoleAppQuitting atomic.Bool
)

// ShowConsoleWindowFor показує окреме вікно консолі КОНКРЕТНОЇ збірки
// (створює при потребі). Повторний виклик для тієї самої збірки лише
// показує/фокусує вже створене вікно. Безпечно викликати з будь-якої
// горутини: створення, реєстрація хука, Show/UnMinimise/UnMaximise/Focus
// виконуються на головному потоці через InvokeSync.
func (a *App) ShowConsoleWindowFor(buildID string) {
	if buildID == "" {
		buildID = a.currentConsoleBuildID()
	}
	if buildID == "" {
		return
	}
	application.InvokeSync(func() {
		consoleWinMu.Lock()
		if consoleWindows == nil {
			consoleWindows = map[string]*application.WebviewWindow{}
		}
		win := consoleWindows[buildID]
		if win == nil {
			win = application.Get().Window.NewWithOptions(application.WebviewWindowOptions{
				Name:           "game-console-" + sanitizeWindowName(buildID),
				Title:          "Shaurma Launcher — Консоль гри",
				Width:          1100,
				Height:         760,
				MinWidth:       640,
				MinHeight:      420,
				Frameless:      true,
				BackgroundType: application.BackgroundTypeTransparent,
				// buildID у query: сторінка консолі прив'язується САМЕ до цієї
				// збірки (GameConsole читає ?build= у console.ts/GameConsole) —
				// вікно завжди показує лог і стан СВОЄЇ збірки, незалежно від
				// того, скільки інших вікон/збірок відкрито.
				URL: consoleWindowURL() + "?build=" + url.QueryEscape(buildID),
			})

			// Закриття вікна консолі = сховати, а не знищити: запис логу
			// продовжується на бекенді, поки гра активна. Під час реального
			// завершення програми (Quit) — не скасовуємо, інакше заблокуємо
			// вихід з процесу. Хук виконується на UI-потоці.
			win.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
				if consoleAppQuitting.Load() {
					return // справжній вихід з програми — даємо вікну закритись
				}
				event.Cancel()
				consoleWinMu.Lock()
				w := consoleWindows[buildID]
				consoleWinMu.Unlock()
				if w != nil {
					application.InvokeSync(func() { w.Hide() })
				}
			})
			consoleWindows[buildID] = win
		}
		consoleWinMu.Unlock()
		if win != nil {
			win.Show()
			win.UnMinimise()
			// Максимізований стан вікна консолі НЕ запам'ятовується між
			// відкриттями: розгорнуте на весь екран вікно може заважати
			// користувачу, тож при кожному показі повертаємо звичайний
			// розмір (сам користувач може розгорнути його знову кнопкою).
			win.UnMaximise()
			win.Focus()
		}
	})
	// Вікно перечитує знімок/контекст САМЕ цієї збірки (console:focus).
	a.emit("console:focus", buildID)
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

// sanitizeWindowName перетворює buildID на безпечне ім'я Wails-вікна
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
// випадок згортання у «шапку» (SetConsoleCollapsed). Під захистом
// consoleWinMu. Ключ — buildID.
var consoleNormalSize = map[string][2]int{}

// SetConsoleCollapsed згортає/розгортає ОКРЕМЕ вікно консолі до «шапки»:
// зменшує висоту вікна, щоб користувач бачив гру, лишаючи кнопки дій.
// Розгортання повертає збережений розмір. Вкладка консолі в головному
// вікні згортається на чистому CSS (там свого вікна немає).
func (a *App) SetConsoleCollapsed(buildID string, collapsed bool) {
	if buildID == "" {
		buildID = a.currentConsoleBuildID()
	}
	if buildID == "" {
		return
	}
	application.InvokeSync(func() {
		consoleWinMu.Lock()
		defer consoleWinMu.Unlock()
		win := consoleWindows[buildID]
		if win == nil {
			return
		}
		w, h := win.Size()
		const headerH = 56 // висота «шапки» консолі
		if collapsed {
			if _, ok := consoleNormalSize[buildID]; !ok {
				consoleNormalSize[buildID] = [2]int{w, h}
			}
			win.SetMinSize(640, headerH)
			win.SetSize(w, headerH)
		} else {
			if norm, ok := consoleNormalSize[buildID]; ok && norm[1] > headerH {
				win.SetSize(norm[0], norm[1])
			}
			delete(consoleNormalSize, buildID)
			win.SetMinSize(640, 420)
		}
	})
}

// ShowConsoleWindow показує вікно консолі для ПОТОЧНОЇ активної збірки
// (сумісність: старий виклик без buildID).
func (a *App) ShowConsoleWindow() {
	a.ShowConsoleWindowFor(a.currentConsoleBuildID())
}

// OpenPackConsole відкриває окреме вікно консолі КОНКРЕТНОЇ збірки
// (кнопка «Консоль» на сторінці збірки). Вікно прив'язується до збірки
// через ?build= у URL — поточний буфер/консоль інших вікон не зачіпається.
func (a *App) OpenPackConsole(packID string) {
	a.ShowConsoleWindowFor(packID)
}

// HideConsoleWindowFor ховає вікно консолі конкретної збірки (лог
// продовжує записуватись). Порожній buildID (старий виклик з вікна без
// ?build=) — ховаємо ЛИШЕ вікно поточної активної збірки, а не всі вікна:
// у режимі «кілька вікон консолі по збірках» закриття одного вікна не
// повинно знищувати консолі інших збірок.
func (a *App) HideConsoleWindowFor(buildID string) {
	if buildID == "" {
		buildID = a.currentConsoleBuildID()
	}
	if buildID == "" {
		return
	}
	application.InvokeSync(func() {
		consoleWinMu.Lock()
		win := consoleWindows[buildID]
		consoleWinMu.Unlock()
		if win != nil {
			win.Hide()
		}
	})
}

// HideConsoleWindow ховає ВСІ відкриті вікна консолей.
func (a *App) HideConsoleWindow() {
	application.InvokeSync(func() {
		consoleWinMu.Lock()
		defer consoleWinMu.Unlock()
		for _, win := range consoleWindows {
			if win != nil {
				win.Hide()
			}
		}
	})
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

// ToggleConsoleWindow — перемикач для поточної активної збірки
// (сумісність зі старим викликом).
func (a *App) ToggleConsoleWindow() {
	a.ToggleConsoleWindowFor("")
}

// IsConsoleWindowOpen повертає, чи відкрите зараз вікно консолі
// конкретної збірки (порожній buildID — для поточної активної).
func (a *App) IsConsoleWindowOpen(buildID string) bool {
	if buildID == "" {
		buildID = a.currentConsoleBuildID()
	}
	consoleWinMu.Lock()
	defer consoleWinMu.Unlock()
	if win, ok := consoleWindows[buildID]; ok && win != nil {
		return win.IsVisible()
	}
	return false
}

// maybeAutoShowConsole показує ОКРЕМЕ вікно консолі для КОНКРЕТНОЇ збірки
// згідно з налаштуваннями showConsoleOnLaunch / showConsoleOnCrash /
// showConsoleOnClose:
//   - trigger "launch" — гра запустилась (після game:started)
//   - trigger "crash"  — гра впала з помилкою (після game:exit, код != 0)
//   - trigger "close"  — гра завершилась штатно (після game:exit, код 0):
//     showConsoleOnClose — показати останні рядки логу після виходу з гри
//
// Окремого «режиму консолі» (consoleMode) більше НЕМАЄ: користувачу незрозуміло,
// навіщо він, коли є окремі перемикачі на кожен випадок.
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

// consoleWindowURL визначає URL сторінки консолі. У прод-збірці — відносний
// шлях /console.html зі вбудованих ассетів; у dev-режимі — адреса
// Vite dev-сервера (FRONTEND_DEVSERVER_URL ставить wails dev), щоб консоль
// теж отримувала live-reload. buildID додається окремо (ShowConsoleWindowFor).
func consoleWindowURL() string {
	if dev := os.Getenv("FRONTEND_DEVSERVER_URL"); dev != "" {
		return strings.TrimSuffix(dev, "/") + "/console.html"
	}
	return "/console.html"
}

// QuitLauncher — завершення процесу з фронтенду (кнопка X у топбарі
// головного вікна). Application.Quit() з фронтенду йде напряму в
// impl.destroy() і НЕ проходить через WindowClosing-хук головного вікна,
// тому ДУБЛЮЄМО ту саму логіку, що й у setupWindowCloseHook:
//
//   - гра запущена → ховаємось у трей (качки продовжуються у треї);
//   - синхронізація активна → попередження (фронт покаже діалог, і лише
//     після підтвердження ConfirmQuitWhileSyncing дозволяємо закриття);
//   - інакше — вихід по-справжньому, з явним consoleAppQuitting.Store(true),
//     щоб відкриті вікна консолей не заблокували вихід.
func (a *App) QuitLauncher() {
	if a.sessionsM != nil && a.sessionsM.AnyRunning() {
		a.HideToTray()
		return
	}
	if a.syncMgr != nil && a.syncMgr.HasActive() && !forceQuitConfirmed {
		a.emit("sync:quit-warning", a.syncMgr.ActivePackIDs())
		return
	}
	consoleAppQuitting.Store(true)
	if a.syncMgr != nil {
		a.syncMgr.PauseAll()
	}
	if app := application.Get(); app != nil {
		app.Quit()
	}
}
