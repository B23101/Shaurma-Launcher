package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"shaurma-launcher-backend/internal/atomicfile"
)

// ── Персистенція стану головного вікна ──────────────────────────────────
// Розмір, позиція, максимізація та повноекранний режим вікна лаунчера
// зберігаються у window-state.json (в теці конфіга) і відновлюються при
// наступному запуску.
//
// ПЕРЕНЕСЕННЯ (нова архітектура): хуки подій вікна (resize/move/maximise/
// fullscreen) тепер реєструє shell.exe на СВОЄМУ головному вікні, бо він
// ним володіє. Вони шлють події через __dispatch:
//   - "window:state"      — стан змінився, backend оновлює in-memory стан
//     і планує відкладений запис (debounce ~600мс);
//   - "window:ready:main" — webview готовий, backend повертає збережений
//     стан для відновлення (розмір/позиція/максимізація).
//
// SaveWindowPosition, як і раніше, викликається фронтендом через JS
// (window.screenX/Y) при кожному move.

type windowState struct {
	Width      int  `json:"width"`
	Height     int  `json:"height"`
	X          int  `json:"x"`
	Y          int  `json:"y"`
	HasPos     bool `json:"hasPos"` // чи є збережена позиція (false = центрувати)
	Maximised  bool `json:"maximised"`
	Fullscreen bool `json:"fullscreen"`
}

// Мінімальні розміри вікна (мають збігатися з WebviewWindowOptions.MinWidth
// /MinHeight у shell/main.go) — збережені розміри менші за них ігноруємо.
const winMinW, winMinH = 960, 640

var (
	winStateMu    sync.Mutex
	winStateLast  windowState
	winStateTimer *time.Timer
)

func (a *App) windowStatePath() string {
	// Вікно запускається ДО старту бази даних, тому стан вікна фізично не
	// може жити в SQLite — це єдиний виняток із правила «все в БД». Файл
	// лежить у теці ПРОГРАМИ (ConfigDir, біля exe), а не в папці даних
	// .shaurm: стан вікна — не «дані користувача», а налаштування самого
	// лаунчера, і він має переживати перенесення папки даних.
	return filepath.Join(a.cfg.ConfigDir(), "window-state.json")
}

func (a *App) loadWindowState() windowState {
	data, err := os.ReadFile(a.windowStatePath())
	if err != nil {
		return windowState{Width: 1024, Height: 640}
	}
	var st windowState
	if json.Unmarshal(data, &st) != nil || st.Width < winMinW || st.Height < winMinH {
		return windowState{Width: 1024, Height: 640}
	}
	// Верхня межа — лише захист від сміття у файлі (зміна конфігурації
	// моніторів, побитий JSON). Колишній clamp 1920x1080 прибрано: він
	// ламав користувачів 1440p/4K — вікно, розтягнуте на пів екрана,
	// після перезапуску стискалось до 1920x1080 і разом з відновленою
	// позицією виглядало як «лаунчер відкрився криво». Реальну верхню
	// межу і так накладає віконний менеджер.
	if st.Width > 16000 || st.Height > 16000 {
		return windowState{Width: 1024, Height: 640}
	}
	return st
}

// onWindowActive — подія "window:active" від shell: головне вікно
// отримало/втратило активацію (WM_ACTIVATE, див. shell/window_exec.go).
// Пересилаємо на фронтенд: там правило «вікно неактивне → анімації на
// паузу» спирається на цю НАТИВНУ подію, бо document.hasFocus() у
// WebView2 не стає false при деактивації вікна (WebView тримає фокус).
func (a *App) onWindowActive(dataJSON string) {
	var st struct {
		Active bool `json:"active"`
	}
	if err := json.Unmarshal([]byte(dataJSON), &st); err != nil {
		return
	}
	// Другий споживач цієї події — віджет сповіщень у трей
	// (notify_window.go): поки головне вікно активне, користувач і так бачить
	// застосунок, і тост був би лише шумом.
	setNotifyMainWindowActive(st.Active)
	// Третій споживач: статус друзям «відійшов», коли вікно довго неактивне.
	a.socialWindowActive(st.Active)
	a.emit("window:active", map[string]any{"active": st.Active})
}

// onShellWindowState — подія "window:state" від shell: розмір/позиція/
// стан вікна змінились. Зберігає значення в пам'ять і планує відкладений
// запис на диск (debounce — resize шле багато подій).
func (a *App) onShellWindowState(dataJSON string) {
	var st windowState
	if err := json.Unmarshal([]byte(dataJSON), &st); err != nil {
		return
	}
	winStateMu.Lock()
	// Розгорнуте або повноекранне вікно: зберігаємо ЛИШЕ сам стан
	// розгортання, а width/height (і позицію) лишаємо останні «віконні».
	// Коли вікно розгорнуте, win.Size() повертає розмір ЕКРАНА — якщо
	// його зберегти, після перезапуску «віконний» розмір зникне і вікно
	// відкриється на весь екран. Це різні величини: повний розмір ≠
	// розмір вікна. Реальний віконний розмір прийде окремою подією
	// WindowUnMaximise/WindowUnFullscreen з maximised=false.
	if st.Maximised || st.Fullscreen {
		winStateLast.Maximised = st.Maximised
		winStateLast.Fullscreen = st.Fullscreen
	} else {
		// hasPos приходить від shell (window_exec.go: sendWindowState) і
		// означає «віконний менеджер реально повідомив позицію». Якщо
		// його нема (Wayland — там клієнт не знає власних координат),
		// зберігаємо РОЗМІР, але лишаємо раніше відому позицію недоторканою:
		// сліпе присвоєння winStateLast = st колись затирало її нулями, і
		// вікно щоразу відновлювалось у лівому верхньому куті.
		if !st.HasPos {
			st.X, st.Y, st.HasPos = winStateLast.X, winStateLast.Y, winStateLast.HasPos
		}
		winStateLast = st
	}
	winStateMu.Unlock()
	a.scheduleWindowStateSave()
	a.emitWindowUIState()
}

// emitWindowUIState шле фронтенду поточний стан вікна (розгорнуте /
// повноекранне) подією "window:ui-state". Фронтенд оновлює за нею кнопку
// «На весь розмір» (іконка square/copy) — інакше вона показує стан НЕ
// розгорнутого вікна навіть коли вікно реально на весь екран (напр.
// після рестарту лаунчера з розгорнутою сесією або розгортання через
// заголовок вікна / двоклік).
func (a *App) emitWindowUIState() {
	winStateMu.Lock()
	st := winStateLast
	winStateMu.Unlock()
	a.emit("window:ui-state", map[string]bool{
		"maximised":  st.Maximised,
		"fullscreen": st.Fullscreen,
	})
}

// shellWindowReadyState — подія "window:ready:main" від shell: webview
// готовий, нативне вікно вже існує — повертаємо збережений стан, щоб
// shell застосував розмір/позицію/максимізацію/повноекранний режим.
// Додатково одразу шлемо фронтенду поточний стан: після відновлення
// розгорнутого вікна кнопка має одразу показувати «згорнути до вікна»,
// а не чекати наступної події resize.
func (a *App) shellWindowReadyState(id string) string {
	_ = id // наразі єдине головне вікно — "main"
	st := a.loadWindowState()
	a.emitWindowUIState()
	b, _ := json.Marshal(st)
	return string(b)
}

// GetWindowUIState повертає поточний стан вікна (розгорнуте/повноекранне)
// як {maximised, fullscreen}. Фронтенд опитує його при старті ПІСЛЯ
// підписки на події: подія "window:ui-state" могла прийти раніше за
// підписку (відновлення розгорнутого вікна shell робить на
// WindowRuntimeReady — до onMount фронтенду), і кнопка «На весь розмір»
// лишилась би з неправильним станом. Якщо in-memory стан ще не наповнений
// подіями shell — читаємо збережений стан з диска.
func (a *App) GetWindowUIState() map[string]bool {
	winStateMu.Lock()
	st := winStateLast
	winStateMu.Unlock()
	if st.Width == 0 && !st.HasPos {
		st = a.loadWindowState()
	}
	return map[string]bool{"maximised": st.Maximised, "fullscreen": st.Fullscreen}
}

// SaveWindowPosition — викликається фронтендом через JS, передає screenX/Y
// вікна браузера. Це надійніший спосіб отримати позицію вікна, ніж
// платформо-залежне window API.
func (a *App) SaveWindowPosition(x, y int) {
	// ЗАХИСТ (Linux/WebKitGTK): window.screenX/screenY там не дають
	// реальних екранних координат вікна і типово повертають 0. Фронтенд
	// уже не шле цей виклик на Linux (App.svelte), але приймати нулі як
	// «вікно стоїть у куті екрана» не можна в жодному разі — саме так
	// у window-state.json колись потрапляла позиція 0,0 з hasPos=true,
	// і вікно відновлювалось притиснутим до лівого верхнього кута.
	if x == 0 && y == 0 {
		return
	}
	winStateMu.Lock()
	winStateLast.X = x
	winStateLast.Y = y
	winStateLast.HasPos = true
	winStateMu.Unlock()
	a.scheduleWindowStateSave()
}

// saveWindowStateNow пише in-memory стан на диск (atomic tmp+rename).
func (a *App) saveWindowStateNow() {
	winStateMu.Lock()
	st := winStateLast
	winStateMu.Unlock()
	if st.Width == 0 {
		return
	}
	_ = atomicfile.WriteJSONAtomic(a.windowStatePath(), st)
}

// scheduleWindowStateSave — відкладений запис (resize шле багато подій).
func (a *App) scheduleWindowStateSave() {
	winStateMu.Lock()
	if winStateTimer != nil {
		winStateTimer.Stop()
	}
	winStateTimer = time.AfterFunc(600*time.Millisecond, func() {
		a.saveWindowStateNow()
	})
	winStateMu.Unlock()
}

// saveWindowStateOnExit — фінальне збереження перед завершенням процесу
// (backend_shutdown). Записує останній відомий стан (shell встиг надіслати
// "window:state" на останньому resize, тож дані свіжі).
func (a *App) saveWindowStateOnExit() {
	a.saveWindowStateNow()
}
