package main

import (
	"shaurma-launcher-backend/internal/crashguard"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"log"
	"time"

	"shaurma-launcher-backend/internal/imageconvert"
	"shaurma-launcher-backend/internal/winbehavior"
)

// ── Трей + "закрити насправді чи сховати" (БІЗНЕС-ЛОГІКА) ────────────────
//
// РОЗБИТТЯ tray.go (нова архітектура):
//   - Бізнес-логіка (яка іконка, який текст пунктів меню, що робить кожен
//     пункт, коли ховати головне вікно у трей) — ТУТ, у backend.dll;
//   - Виконавча частина (реальне application.SystemTray.New(), Menu.Add(),
//     OnClick) — у shell/tray_exec.go (generic: приймає icon+tooltip+
//     menuJSON, на клік шле "__dispatch" подію "tray:click:<id>" назад у
//     backend).
//
// Shell не знає слова "трей лаунчера" — лише iconB64+menuJSON. Це дає
// змогу міняти меню трею (додавати/прибирати пункти) БЕЗ перекомпіляції
// shell.exe.

//go:embed build/appicon.png
var iconPNG []byte

var trayRunning bool

// forceQuitConfirmed — виставляється ConfirmQuitWhileSyncing(), коли
// користувач підтвердив закриття лаунчера попри активну синхронізацію
// збірок.
var forceQuitConfirmed bool

// trayMenuJSON — пункти меню трею (формат bridge.h menuJson).
func trayMenuJSON() string {
	items := []map[string]any{
		{"id": "show", "label": "Показати лаунчер"},
		{"id": "quit", "label": "Вийти", "separator": true},
	}
	b, _ := json.Marshal(items)
	return string(b)
}

// trayIconSide — сторона іконки трею в пікселях. Панелі малюють іконку
// 16–32 px; 64 дає запас на HiDPI, але не тягне за собою мегабайти в
// D-Bus (див. imageconvert.ResizePNGToFit).
const trayIconSide = 64

// ensureTray піднімає системний трей один раз (повторні виклики — no-op).
// Формує menuJSON і віддає shell'у через generic bridgeTraySet — сам
// трей створює shell (tray_exec.go).
//
// Іконка попередньо масштабується до trayIconSide: вшитий appicon.png
// 883×883 у форматі ARGB — це ~3 МБ на кожне відправлення через D-Bus,
// тоді як реально видно 16–32 px. Підпис трею shell формує з tooltip
// (на Linux показується саме він) — тут лишається людський текст із
// назвою редакції, яка реально запущена.
func (a *App) ensureTray() {
	if trayRunning {
		return
	}
	trayRunning = true
	b64 := base64.StdEncoding.EncodeToString(imageconvert.ResizePNGToFit(iconPNG, trayIconSide))
	// Підпис БЕЗ «— гра запущена»: трей тепер живе й у фоновому режимі
	// (startupMode=background, closeAction=tray), коли гри може не бути.
	bridgeTraySet(b64, launcherDisplayName(), trayMenuJSON())
}

// launcherDisplayName — людяна назва для підпису трею/вікон. ОДНАКОВА для
// усіх редакцій (shaurma/base) — редакція є суто внутрішнім технічним
// каналом збірки/оновлень і не повинна бути видимою користувачу ніде: ні в
// треї, ні в заголовку вікна, ні в диспетчері задач/taskbar/меню "Пуск".
// Раніше тут поверталось "Shaurma Base" для base-редакції — саме це
// просочувалось у підпис трею користувачу; це й був баг.
func launcherDisplayName() string {
	return "Shaurma Launcher"
}

// onTrayClick — клік по пункту меню трею (подія "tray:click:<id>" від
// shell через __dispatch). ІДЕНТИЧНА логіка OnClick-ів з оригінального
// tray.go:
//   - "show" — розгорнути головне вікно;
//   - "quit" — закрити лаунчер повністю (зупинить гру).
func (a *App) onTrayClick(id string) {
	switch id {
	case "show":
		a.showWindow()
	case "quit":
		a.quitNow()
	}
}

// showWindow розгортає головне вікно лаунчера назад з трею (generic
// bridge-команда над вікном "main", яке створює shell при старті).
func (a *App) showWindow() {
	a.revealMainWindow()
}

// ConfirmQuitWhileSyncing викликається фронтендом, коли користувач у
// діалозі попередження явно підтвердив "закрити попри активну качку".
// Активні синхронізації переводяться в Pause (прогрес не пропадає), а
// закриття вікна ініціюється повторно.
func (a *App) ConfirmQuitWhileSyncing() {
	forceQuitConfirmed = true
	if a.syncMgr != nil {
		a.syncMgr.PauseAll()
	}
	bridgeWindowOp("main", "close")
}

// shutdown викликається один раз, коли процес реально завершується
// (через backend_shutdown C ABI — shell кличе її ПЕРЕД FreeLibrary).
func (a *App) shutdown() {
	// Останній шанс зберегти розмір/стан вікна (якщо фінальний resize
	// не встиг записатись у фоновому debounce).
	a.saveWindowStateOnExit()
	// Локальний loopback-сервер іконок модів (modicon_server.go).
	stopModIconServer()
	// Журнал помилок: лише локальне збереження, у мережу нічого не йде.
	a.AutoSaveLauncherErrorLog()
	// Discord Rich Presence: завершуємо serve-цикл.
	if a.rpc != nil {
		a.rpc.Stop()
	}
	// Передачі збірок: зупиняємо (журнал одержувача вже на диску).
	a.shareShutdown()
	// P2P ("LAN через інтернет"): гасимо tsnet-ноди акуратно — Ephemeral
	// і так прибрав би їх з Headscale-мережі сам за кілька хвилин
	// мовчання, але явне закриття звільняє одразу, без затримки.
	// StopAllP2PHosts — а не StopP2PHost для однієї адреси: мультихостинг
	// дозволяє кілька активних сесій одночасно, і при виході з лаунчера
	// мають погаснути ВСІ, а не лише якась одна конкретна.
	if a.p2pAPI != nil {
		if err := a.p2pAPI.StopAllP2PHosts(); err != nil {
			log.Printf("p2p host shutdown: %v", err)
		}
		// Тимчасові адреси запрошень не переживають лаунчер.
		a.purgeTempP2PAddresses(4 * time.Second)
		if err := a.p2pAPI.StopP2PGuestService(); err != nil {
			log.Printf("p2p guest shutdown: %v", err)
		}
	}
	bridgeTrayDestroy()
	// Штатний вихід: прибираємо маркер session.running. Якщо до цього рядка
	// не дійшли — наступний запуск напише в лог, що сесія завершилась аварійно.
	crashguard.MarkCleanExit()
}

// onMainWindowClosing — рішення при закритті ГОЛОВНОГО вікна (подія
// "window:closing:main" від shell через __dispatch). ІДЕНТИЧНА логіка
// setupWindowCloseHook з оригінального tray.go, але повертає shell'у
// рішення action JSON замість того, щоб напряму кликати event.Cancel():
//   - {"action":"hide"}  — сховати вікно у трей (процес живе далі);
//   - {"action":"close"} — дозволити закриття (процес завершується).
func (a *App) onMainWindowClosing() string {
	// Гра запущена, йде P2P-сесія АБО користувач обрав «× ховає в трей» →
	// ховаємось у трей ПЕРШИМ ділом, НЕЗАЛЕЖНО від активних качок:
	// синхронізація просто продовжує йти у треї (рішення і причина —
	// winbehavior.CloseTrayReason).
	closeCtx := winbehavior.CloseContext{
		GameRunning: a.sessionsM != nil && a.sessionsM.AnyRunning(),
		P2PActive:   a.p2pAPI != nil && a.p2pAPI.HasActiveSessions(),
	}
	if reason := winbehavior.CloseTrayReason(a.windowSettings(), closeCtx); reason != winbehavior.TrayReasonNone {
		logHideToTray("window-close:" + reason)
		a.ensureTray()
		setNotifyMainWindowActive(false)
		maybeReleaseMemory()
		a.notifyTrayContinues(a.windowSettings(), closeCtx)
		return `{"action":"hide"}`
	}

	// Гра не запущена, але йде синхронізація збірок: попереджаємо — фронт
	// покаже діалог "закриття може скасувати завантаження", і тільки після
	// явного підтвердження (ConfirmQuitWhileSyncing) дозволяємо закриття.
	if a.syncMgr != nil && a.syncMgr.HasActive() && !forceQuitConfirmed {
		logHideToTray("window-close:sync-active")
		a.ensureTray()
		a.emit("sync:quit-warning", a.syncMgr.ActivePackIDs())
		return `{"action":"hide"}`
	}

	// Йде передача/приймання збірки друзям: закриття її обірве (прийняте лишиться на
	// диску, але відправник закриє доступ). Питаємо підтвердження, як для качки.
	if a.ShareBusy() && !forceQuitConfirmed {
		logHideToTray("window-close:share-active")
		a.ensureTray()
		a.emit("share:quit-warning")
		return `{"action":"hide"}`
	}

	// Ні гри, ні качок — закриття головного вікна завершує процес
	// по-справжньому. Даємо знати вікнам консолі (якщо відкриті), щоб
	// вони не скасували своє закриття і не заблокували вихід.
	consoleAppQuitting.Store(true)
	if a.syncMgr != nil {
		a.syncMgr.PauseAll()
	}
	return `{"action":"close"}`
}

// HideToTray ховає головне вікно у трей (викликається фронтендом через
// App.HideToTray та з QuitLauncher при запущеній грі).
// (Реалізація в app.go — тут лише документуємо зв'язок.)
