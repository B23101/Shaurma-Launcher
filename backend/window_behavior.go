package main

import (
	"log"
	"os"
	"sync/atomic"
	"time"

	"shaurma-launcher-backend/internal/autostart"
	"shaurma-launcher-backend/internal/config"
	"shaurma-launcher-backend/internal/winbehavior"
)

// window_behavior.go — застосування рішень internal/winbehavior до
// реального вікна/процесу лаунчера. САМІ рішення («що робити») — у чистому
// пакеті winbehavior (з тестами); тут лише виконання: bridgeWindowOp,
// трей, вихід. Єдине місце, де вікно ховається/повертається через
// налаштування, — раніше це було розкидано між App.svelte (hide на
// запуску гри, quit/restore на виході) і кількома Go-файлами.

// startBackground — лаунчер запущено з прапорцем --background (автозапуск
// «у фоні»): головне вікно НЕ показується, поки користувач сам його не
// відкриє (трей / повторний запуск ярлика). Скидається першим показом.
var startBackground atomic.Bool

// markBackgroundStart викликається з backend_init, коли shell повідомив
// background:true у конфігу ініціалізації.
func markBackgroundStart() {
	startBackground.Store(true)
	// Вікно ніколи не було активним → тости чатів мають показуватись.
	// Без цього notifyMainActive лишається true (дефолт «вікно активне»)
	// і ВСІ сповіщення мовчки гаснуть, поки користувач не відкриє й не
	// згорне вікно.
	setNotifyMainWindowActive(false)
}

// consumeBackgroundStart — true рівно один раз (перший UIReady) для
// фонового старту; далі вікно поводиться як зазвичай.
func consumeBackgroundStart() bool {
	return startBackground.Swap(false)
}

// windowSettings — поточні налаштування вікна (нормалізовані).
func (a *App) windowSettings() winbehavior.Settings {
	return winbehavior.Normalize(config.WindowSettings(a.cfg.GetSettings()))
}

// syncAutostart приводить автозавантаження ОС у відповідність до режиму
// startupMode: реєструє/знімає запис і передає потрібні прапорці
// (--background тощо). Викликається при старті й при кожному збереженні
// налаштувань — запис перезаписується, тож перемикання «з вікном» ↔
// «у фоні» діє одразу.
func (a *App) syncAutostart() {
	w := a.windowSettings()
	enabled := winbehavior.AutostartEnabled(w.StartupMode)
	if err := autostart.Set(enabled, execPath(), winbehavior.AutostartArgs(w.StartupMode)...); err != nil {
		log.Printf("autostart sync (%s): %v", w.StartupMode, err)
	}
}

// logHideToTray — один рядок у лозі на кожне приховування вікна в трей.
func logHideToTray(reason string) {
	log.Printf("window: hide to tray, reason=%s", reason)
}

// notifyTrayContinues — × примусово сховало вікно (гра / P2P при closeAction=quit):
// користувач має дізнатись, куди зник лаунчер. Вікно вже сховане, тож
// показуємо ОС-тост (діалог у схованому вікні ніхто б не побачив).
func (a *App) notifyTrayContinues(s winbehavior.Settings, c winbehavior.CloseContext) {
	if !winbehavior.CloseTrayForced(s, c) {
		return
	}
	_ = a.NotifyOS("Shaurma Launcher", "Лаунчер продовжить працювати в треї")
}

// notifyRestartedAfterCrash — shell-нагляд перезапустив лаунчер після збою
// (змінна SHAURMA_RESTARTED=1). Показуємо тост один раз за процес.
func (a *App) notifyRestartedAfterCrash() {
	if os.Getenv("SHAURMA_RESTARTED") == "" {
		return
	}
	_ = os.Unsetenv("SHAURMA_RESTARTED")

	// Безпечний режим (safe_mode.go) — нагляд перезапустив нас після КІЛЬКОХ
	// збоїв підряд і вимкнув ризикові нативні шляхи. Користувач має знати,
	// чому раптом немає Discord-присутності/прогресу в таскбарі.
	msg := "Перезапущено після збою"
	if safeModeActive() {
		msg = "Перезапущено після збою (безпечний режим: без GPU, Discord і прогресу в таскбарі)"
	}
	_ = a.NotifyOS("Shaurma Launcher", msg)
}

// revealMainWindow показує головне вікно (з трею/фону/згорнутого стану).
// Скидає прапорець фонового старту, щоб запізнілий UIReady не сховав вікно.
func (a *App) revealMainWindow() {
	startBackground.Store(false)
	bridgeWindowOp("main", "show")
	bridgeWindowOp("main", "unminimise")
}

// MinimizeLauncher — кнопка «Згорнути» (–) у топбарі: на панель завдань або
// в трей — за налаштуванням minimizeAction.
func (a *App) MinimizeLauncher() {
	if a.windowSettings().MinimizeAction == winbehavior.MinimizeTray {
		a.hideToTray("minimize-action-tray")
		return
	}
	bridgeWindowOp("main", "minimise")
	// Згорнуте вікно користувач теж не бачить — повертаємо ОС сторінки зараз,
	// а не тримаємо їх до наступного розгортання. При показі вони підвантажуються
	// назад (memtrim.go: trimNowForHiddenWindow, memtrim_windows.go).
	trimNowForHiddenWindow()
}

// quitNow — повний вихід із лаунчера (та сама підготовка, що й у пункті
// «Вийти» трею): консолі не блокують закриття, синхронізації ставляться
// на паузу, прогрес не втрачається.
func (a *App) quitNow() {
	consoleAppQuitting.Store(true)
	if a.syncMgr != nil {
		a.syncMgr.PauseAll()
	}
	bridgeAppQuit()
}

// quitBlocked — чи небезпечно виходити з лаунчера просто зараз: запущені
// сервери (їх процеси веде лаунчер) або йде синхронізація збірок.
func (a *App) quitBlocked() bool {
	if a.srvAPI != nil && a.srvAPI.HasRunningServers() {
		return true
	}
	if a.syncMgr != nil && a.syncMgr.HasActive() && !forceQuitConfirmed {
		return true
	}
	return false
}

// applyWindowAction виконує рішення winbehavior.
func (a *App) applyWindowAction(act winbehavior.WindowAction) {
	switch act {
	case winbehavior.WindowMinimize:
		bridgeWindowOp("main", "minimise")
	case winbehavior.WindowHide:
		a.hideToTray("game-launch-action")
	case winbehavior.WindowShow:
		a.revealMainWindow()
	case winbehavior.WindowQuit:
		a.quitNow()
	}
}

// applyGameLaunchWindow — гру успішно запущено: згорнути/сховати вікно за
// gameLaunchAction (з необов'язковою затримкою gameLaunchDelaySec, щоб
// користувач встиг побачити прогрес або помилку запуску).
func (a *App) applyGameLaunchWindow() {
	plan := winbehavior.PlanGameLaunch(a.windowSettings())
	if plan.Action == winbehavior.WindowNone {
		return
	}
	if plan.Delay <= 0 {
		a.applyWindowAction(plan.Action)
		return
	}
	time.AfterFunc(plan.Delay, func() {
		// За час очікування гра могла вже закритись — тоді ховати вікно
		// нема сенсу (повертати його вже нікому).
		if a.sessionsM != nil && a.sessionsM.AnyRunning() {
			a.applyWindowAction(plan.Action)
		}
	})
}

// applyGameExitWindow — гра завершилась: повернути вікно / вийти / не
// чіпати за gameExitAction. Викликається ПІСЛЯ sessionsM.Remove, тож
// AnyRunning відображає лише решту паралельних сесій.
func (a *App) applyGameExitWindow(code int, wasUserStop bool) {
	act := winbehavior.PlanGameExit(a.windowSettings(), winbehavior.ExitContext{
		OtherSessionsRunning: a.sessionsM != nil && a.sessionsM.AnyRunning(),
		Crashed:              code != 0 && !wasUserStop,
		QuitBlocked:          a.quitBlocked(),
	})
	a.applyWindowAction(act)
}
