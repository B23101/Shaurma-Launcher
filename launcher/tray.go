package main

import (
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// ── Трей + "закрити насправді чи сховати" ───────────────────────────────
//
// Раніше (баг): закриття вікна завжди вбивало процес — разом з ним
// пропадала запущена гра (game-monitor логіка жила в тому ж процесі),
// і будь-який незбережений прогрес майстра першого запуску governance
// зникав, бо currentStep ніде не писався на диск (див. step.go).
//
// Тепер: setupWindowCloseHook реєструє HOOK на подію WindowClosing.
// Hooks виконуються РАНІШЕ за listeners у HandleWindowEvent, тому коли
// гра запущена — ми скасовуємо закриття (event.Cancel) до того, як
// внутрішній listener Wails встановить прапорець «закривати безумовно»,
// ховаємо вікно і показуємо трей-іконку. Процес продовжує жити у фоні:
// game-monitor-горутина (internal/minecraft.Launcher) як і раніше стежить
// за PID, а трей дозволяє користувачу розгорнути вікно назад або вийти
// насправді через пункт меню.

var trayRunning bool

// tray — створюється ліниво при першому хованні у трей (ensureTray).
var tray *application.SystemTray

// forceQuitConfirmed — виставляється ConfirmQuitWhileSyncing(), коли
// користувач підтвердив закриття лаунчера попри активну синхронізацію
// збірок (розділ "попередження при закритті" — файли якаються атомарно
// по чанках, тож різкий вихід не пошкоджує вже записані байти, але
// перерве .part на середині чанка, тому попередження все одно доречне).
var forceQuitConfirmed bool

// setupWindowCloseHook підвішує обробник закриття вікна. Викликається
// з main() після створення вікна.
func (a *App) setupWindowCloseHook(w *application.WebviewWindow) {
	w.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		// Гра запущена → ховаємось у трей ПЕРШИМ ділом, НЕЗАЛЕЖНО від активних
		// качок: користувач попросив «якщо запустили збірку і лаунчер пішов у
		// трей — збірка і далі має качатися, але вже в треї». Процес лишається
		// живим у фоні, тож синхронізація просто продовжує йти.
		if a.sessionsM != nil && a.sessionsM.AnyRunning() {
			event.Cancel()
			w.Hide()
			a.ensureTray()
			return
		}

		// Гра не запущена, але йде синхронізація збірок Шаурма: попереджаємо —
		// фронт покаже діалог "закриття може скасувати завантаження", і тільки
		// після явного підтвердження користувача (ConfirmQuitWhileSyncing)
		// дозволяємо закриття пройти цей hook вдруге.
		if a.syncMgr != nil && a.syncMgr.HasActive() && !forceQuitConfirmed {
			event.Cancel()
			a.emit("sync:quit-warning", a.syncMgr.ActivePackIDs())
			return
		}

		// Ні гри, ні качок — закриття головного вікна завершує процес
		// по-справжньому. Даємо знати вікну консолі (якщо відкрите),
		// щоб воно не скасувало свій WindowClosing і не заблокувало вихід.
		consoleAppQuitting.Store(true)
		// Активні синхронізації переводимо в Pause (НЕ Cancel) — той
		// самий шлях, що й при краху процесу: прогрес зберігається,
		// користувач після наступного запуску побачить "Продовжити".
		if a.syncMgr != nil {
			a.syncMgr.PauseAll()
		}
	})
}

// ConfirmQuitWhileSyncing викликається фронтендом, коли користувач у
// діалозі попередження явно підтвердив "закрити попри активну качку".
// Активні синхронізації переводяться в Pause (не Cancel — прогрес не
// пропадає), а закриття вікна ініціюється повторно.
func (a *App) ConfirmQuitWhileSyncing() {
	forceQuitConfirmed = true
	if a.syncMgr != nil {
		a.syncMgr.PauseAll()
	}
	if a.window != nil {
		a.window.Close()
	}
}

// shutdown викликається один раз, коли процес реально завершується
// (гра не запущена, або користувач обрав "Вийти" в треї).
func (a *App) shutdown() {
	// Останній шанс зберегти розмір/стан вікна (якщо фінальний resize
	// не встиг записатись у фоновому debounce).
	a.saveWindowStateOnExit()
	if tray != nil {
		tray.Destroy()
	}
}

// ensureTray піднімає системний трей один раз (повторні виклики — no-op).
// На відміну від energye/systray, v3-native SystemTray не потребує
// окремого OS-потоку (runtime.LockOSThread): уся Win32-робота ведеться
// на внутрішньому головному потоці Wails через InvokeSync.
func (a *App) ensureTray() {
	if trayRunning {
		return
	}
	trayRunning = true

	app := application.Get()
	if app == nil {
		return
	}
	tray = app.SystemTray.New()
	tray.SetIcon(iconPNG)
	tray.SetTooltip("Shaurma Launcher — гра запущена")
	tray.SetMenu(a.trayMenu())
	tray.OnClick(func() {
		a.showWindow()
	})
	tray.Run()
}

func (a *App) trayMenu() *application.Menu {
	menu := application.NewMenu()

	mShow := menu.Add("Показати лаунчер")
	mShow.SetTooltip("Розгорнути вікно")
	mShow.OnClick(func(*application.Context) {
		a.showWindow()
	})

	menu.AddSeparator()

	mQuit := menu.Add("Вийти")
	mQuit.SetTooltip("Закрити лаунчер повністю (зупинить гру)")
	mQuit.OnClick(func(*application.Context) {
		// Явний вихід користувача через трей: закриваємо процес
		// по-справжньому, незалежно від того, чи гра ще йде.
		consoleAppQuitting.Store(true)
		// Качки м'яко зупиняємо (прогрес зберігається на диску — .part
		// лишається, користувач продовжить після наступного запуску).
		if a.syncMgr != nil {
			a.syncMgr.PauseAll()
		}
		application.Get().Quit()
	})

	return menu
}

// showWindow розгортає головне вікно лаунчера назад з трею.
func (a *App) showWindow() {
	if a.window != nil {
		a.window.Show()
		a.window.UnMinimise()
	}
}
