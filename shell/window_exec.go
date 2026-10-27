package main

// window_exec.go — виконавча частина generic-вікон. Shell НЕ знає, що таке
// «консоль гри» чи «splash апдейтера»: він створює вікно за ID і набором
// опцій (window_create), виконує над ним операції (window_op) і шле події
// стану/закриття назад у backend через __dispatch. Уся семантика — в DLL.

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

var (
	winMu  sync.Mutex
	winMap = map[string]*application.WebviewWindow{}

	// mainWinActive — останній відомий стан активації головного вікна
	// (true = активне). Оновлюється хуками WindowFocus/WindowLostFocus;
	// на WindowRuntimeReady шлемо його фронтенду ще раз — ранні події
	// WM_ACTIVATE могли прийти, поки webview ще не підписався.
	mainWinActive = true

	// mainStateApplied — збережений стан головного вікна вже застосовано
	// (розмір задано ще при створенні вікна). mainRestoring — прапорець
	// «зараз триває відновлення»: доки він піднятий, події resize/move
	// НЕ йдуть у backend, інакше проміжна геометрія перезаписала б
	// збережений стан.
	//
	// ОКРЕМИЙ м'ютекс (НЕ winMu) — принципово. applyWindowState кличе
	// win.SetSize/Maximise, а ті на частині платформ доставляють хук
	// WindowDidResize СИНХРОННО, у тому самому потоці. Хук кличе
	// sendWindowState, який теж має прочитати цей прапорець — якби вони
	// ділили winMu (не реентрабельний sync.Mutex), отримали б дедлок
	// рівно в момент відновлення вікна. Тому прапорці мають власний
	// м'ютекс, і він НІКОЛИ не утримується під час викликів у вікно.
	mainStateMu      sync.Mutex
	mainStateApplied = false
	mainRestoring    = false

	// forceClose — вікна, які shell ЗНИЩУЄ програмно (window_op "destroy":
	// splash апдейтера, діагностичні вікна). Для них хук WindowClosing НЕ
	// запитує рішення у backend'а — інакше backend для будь-якого
	// не-головного вікна повертає {"action":"hide"} (логіка консолей гри),
	// закриття скасовується, і splash ніколи реально не закривається
	// (залишається прихованим «зомбі»-вікном, а головне вікно ніби «не
	// запускається»).
	forceClose = map[string]bool{}
)

func getWindow(id string) *application.WebviewWindow {
	winMu.Lock()
	defer winMu.Unlock()
	return winMap[id]
}

// winOptions — дзеркало options_json з bridge.h (window_create).
// Background: "transparent" (за замовчуванням, як основний лаунчер) |
// "solid" (суцільний фон — splash апдейтера, як у старому коді);
// BackgroundHex — колір для solid у форматі "#rrggbb".
type winOptions struct {
	Title         string `json:"title"`
	Width         int    `json:"width"`
	Height        int    `json:"height"`
	MinWidth      int    `json:"minWidth"`
	MinHeight     int    `json:"minHeight"`
	MaxWidth      int    `json:"maxWidth"`
	MaxHeight     int    `json:"maxHeight"`
	DisableResize bool   `json:"disableResize"`
	Frameless     bool   `json:"frameless"`
	URL           string `json:"url"`
	Background    string `json:"background,omitempty"`
	BackgroundHex string `json:"backgroundHex,omitempty"`
	// AlwaysOnTop — вікно поверх усіх інших (віджет сповіщень у трей).
	AlwaysOnTop bool `json:"alwaysOnTop,omitempty"`
	// HiddenOnTaskbar — НЕ показувати вікно в панелі завдань (WS_EX_TOOLWINDOW).
	HiddenOnTaskbar bool `json:"hiddenOnTaskbar,omitempty"`
	// Position — спеціальне позиціонування: "bottom-right" = правий нижній
	// кут робочої області основного монітора (з MarginX/MarginY відступом).
	// "" або інше → X/Y як звичайні координати.
	Position string `json:"position,omitempty"`
	X        int    `json:"x,omitempty"`
	Y        int    `json:"y,omitempty"`
	MarginX  int    `json:"marginX,omitempty"`
	MarginY  int    `json:"marginY,omitempty"`
	// Hidden — створити вікно НЕпоказаним. Головне вікно лаунчера прозоре
	// (заокруглені кути frameless-вікна), тож показане ДО першого малювання
	// сторінки воно «просвічує» робочий стіл — звідси «прозорий лаунчер при
	// старті». Показуємо його лише коли фронтенд змонтовано (App.UIReady).
	Hidden bool `json:"hidden,omitempty"`
}

// windowCreate створює вікно з ID (no-op, якщо вже існує). Викликається з
// UI-потоку (через InvokeAsync з колбека або напряму з main до Run).
func windowCreate(id, optsJSON string) {
	if id == "" {
		return
	}
	if getWindow(id) != nil {
		return
	}
	var o winOptions
	_ = json.Unmarshal([]byte(optsJSON), &o)
	if o.Width <= 0 {
		o.Width = 800
	}
	if o.Height <= 0 {
		o.Height = 600
	}
	url := o.URL
	if url == "" {
		url = "index.html"
	}

	app := application.Get()
	if app == nil {
		return
	}
	// Стиль старого апдейтера: суцільний фон #0f0f0f (splash) або прозорий
	// (основне вікно лаунчера, як у старому launcher/main.go). Прозорий фон
	// БЕЗ вказаного кольору — як раніше (BackgroundTypeTransparent).
	//
	// На Linux resolveWindowBackground (window_background_linux.go) форсує
	// solid ЗАМІСТЬ transparent — GTK4/WebKitGTK-прозорість вікна вимагає
	// робочого композитора дисплей-сервера (gdk_display_is_composited);
	// у типових Linux-оточеннях без композитора (WSLg/X11 без compositing
	// window manager, деякі headless/VNC-сетапи) вікно з transparent-фоном
	// рендериться СУЦІЛЬНО БІЛИМ замість прозорого — саме так проявлялась
	// "головне вікно лаунчера біле, а splash (уже solid) — ні": обидва
	// вікна показують той самий фронтенд-контент, різниця була лише в
	// Background. Фронтенд (frontend/src/style.css: body{background:
	// var(--bg-app)}) і так сам малює непрозорий фон поверх вікна, тому
	// втрата справжньої прозорості на Linux візуально непомітна — колір
	// фолбека нижче свідомо збігається з --bg-app: #0f0f0f. На Windows
	// (WebView2/DWM) ця функція — no-op, поведінка не змінюється.
	o.Background, o.BackgroundHex = resolveWindowBackground(o.Background, o.BackgroundHex)
	bgType := application.BackgroundTypeTransparent
	if o.Background == "solid" {
		bgType = application.BackgroundTypeSolid
	}
	// Позиція вікна. "bottom-right" обчислюється від робочої області
	// ОСНОВНОГО монітора (без панелі завдань) — інакше віджет сповіщень
	// виявився б «під» панеллю завдань або на краю за межами екрана.
	// Якщо монітор ще не відомий (рідко на старті) — лишаємо стандартне
	// розміщення; пізніше windowOp("anchor-bottom-right") переставить
	// вікно на місце після кожної зміни його розміру.
	var (
		posX, posY     = o.X, o.Y
		haveExplicitXY = false
	)
	if o.Position == "bottom-right" {
		if x, y, ok := computeBottomRightXY(o.Width, o.Height, o.MarginX, o.MarginY); ok {
			posX, posY, haveExplicitXY = x, y, true
		}
	}
	win := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:          o.Title,
		Width:          o.Width,
		Height:         o.Height,
		MinWidth:       o.MinWidth,
		MinHeight:      o.MinHeight,
		MaxWidth:       o.MaxWidth,
		MaxHeight:      o.MaxHeight,
		DisableResize:  o.DisableResize,
		Frameless:      o.Frameless,
		URL:            url,
		BackgroundType: bgType,
		Hidden:         o.Hidden,
		AlwaysOnTop:    o.AlwaysOnTop,
		// Експліцитні координати: WindowXY перемикає Wails у режим
		// "X/Y = відносно робочої області екрана" (без цього Wails
		// проігнорує X/Y і розмістить вікно за своїм дефолтом).
		InitialPosition: explicitPosition(haveExplicitXY),
		X:               posX,
		Y:               posY,
		// Windows-розширені стилі вікна: приховання з панелі завдань.
		//
		// НАВМИСНО НЕ задаємо Windows.ExStyle: за джерелом Wails він
		// ЗАМІНЮЄ усі обчислені стилі (exStyle = options.Windows.ExStyle),
		// тож підстава сюди власного WS_EX_NOACTIVATE стерла б
		// WS_EX_TOPMOST, WS_EX_TOOLWINDOW і — головне —
		// WS_EX_NOREDIRECTIONBITMAP, без якого не працює прозорість
		// вікна. Fallback від крадіжки фокуса реалізований окремою
		// операцією window_op "show-inactive" (див. window_show_*.go).
		Windows: application.WindowsWindow{
			HiddenOnTaskbar: o.HiddenOnTaskbar,
		},
		// Linux: GPU-політика webview. «Економний режим» вимикає апаратне
		// прискорення WebKitGTK (WebviewGpuPolicyNever) — разом з env-фіксами
		// з webview_compat_linux.go це головний важіль по памʼяті WebKit
		// (композитні буфери/допоміжні процеси). Поза Linux поле ігнорується
		// (Windows-фронтенд керується --disable-gpu з того самого ui.json).
		Linux: application.LinuxWindow{
			WebviewGpuPolicy: launcherWebviewGpuPolicy(),
		},
	})
	// Іконка вікна = іконка ЛАУНЧЕРА (з ресурсів exe), а не системна чи
	// іконка ярлика, яким запустили лаунчер (див. window_icon.go).
	//
	// ВАЖЛИВО: НЕ ставимо іконку тут одразу після NewWithOptions. У цей
	// момент HWND щойно створений і ще НЕ показаний (win.Show()
	// викликається окремо, пізніше, через windowOp("show") — часто вже
	// в наступному тіку event loop). WM_SETICON, надісланий до першого
	// показу вікна, губиться: коли Windows Shell реєструє taskbar-запис
	// при показі вікна, запущеного через .lnk, він повторно підхоплює
	// іконку джерела активації (іконку ярлика), затираючи те, що ми
	// встановили заздалегідь. Тому чіпляємось на WindowRuntimeReady —
	// вікно на цей момент уже показане і taskbar-запис уже існує,
	// WM_SETICON після цього не перезаписується.
	win.RegisterHook(events.Common.WindowRuntimeReady, func(*application.WindowEvent) {
		setLauncherWindowIcon(win)
	})
	if o.Background == "solid" && o.BackgroundHex != "" {
		if c, err := parseHexColor(o.BackgroundHex); err == nil {
			win.SetBackgroundColour(c)
		}
	}

	// Закриття БУДЬ-ЯКОГО вікна вирішує backend (generic contract):
	// шлемо "window:closing:<id>" і виконуємо повернутий action. Виняток —
	// програмне знищення (forceClose, див. window_op "destroy"): такі вікна
	// закриваються одразу, без запиту до backend'а.
	win.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		winMu.Lock()
		fc := forceClose[id]
		delete(forceClose, id)
		winMu.Unlock()
		if fc {
			winMu.Lock()
			delete(winMap, id)
			winMu.Unlock()
			return
		}
		action := backendDispatch("window:closing:"+id, "")
		if action == "hide" {
			e.Cancel()
			win.Hide()
			if id == "main" {
				setWindowHiddenMarker(true)
			}
			return
		}
		winMu.Lock()
		delete(winMap, id)
		winMu.Unlock()
	})

	// Персистенція стану — ЛИШЕ для головного вікна ("main"), яким володіє
	// backend (windowstate.go: хуки resize/move/maximise/fullscreen шлють
	// "window:state", а "window:ready:main" віддає збережений стан).
	if id == "main" {
		win.RegisterHook(events.Common.WindowDidResize, func(*application.WindowEvent) { sendWindowState(win) })
		win.RegisterHook(events.Common.WindowDidMove, func(*application.WindowEvent) { sendWindowState(win) })
		win.RegisterHook(events.Common.WindowMaximise, func(*application.WindowEvent) { sendWindowState(win) })
		win.RegisterHook(events.Common.WindowUnMaximise, func(*application.WindowEvent) { sendWindowState(win) })
		win.RegisterHook(events.Common.WindowFullscreen, func(*application.WindowEvent) { sendWindowState(win) })
		win.RegisterHook(events.Common.WindowUnFullscreen, func(*application.WindowEvent) { sendWindowState(win) })
		win.RegisterHook(events.Common.WindowRuntimeReady, func(*application.WindowEvent) {
			restoreWindowState(win)
			// Після завантаження webview шлемо поточний стан активності —
			// щоб фронтенд не залежав від document.hasFocus() (у WebView2
			// він не відображає деактивацію вікна).
			backendDispatch("window:active", fmt.Sprintf(`{"active":%v}`, mainWinActive))
		})
		// Активність головного вікна -> backend (правило «вікно неактивне →
		// анімації на паузу»). Wails шле WindowFocus/WindowLostFocus на
		// WM_ACTIVATE — надійно навіть при ПРОСТІЙ деактивації (клік по
		// іншому вікну, згортання), чого document.hasFocus() у WebView2 не
		// відображає (WebView тримає DOM-фокус і blur не приходить).
		win.RegisterHook(events.Common.WindowFocus, func(*application.WindowEvent) {
			mainWinActive = true
			backendDispatch("window:active", `{"active":true}`)
		})
		win.RegisterHook(events.Common.WindowLostFocus, func(*application.WindowEvent) {
			mainWinActive = false
			backendDispatch("window:active", `{"active":false}`)
		})
	} else {
		// Активність НЕ-головних вікон (консоль гри, splash апдейтера,
		// діагностика): скопована подія "window:active:<id>" напряму з
		// shell (app.Event.Emit, БЕЗ backend — splash показується ДО
		// завантаження backend.dll, тож каналу через backend там немає).
		// Фронтенд вікна підписується на подію СВОГО id: консоль слухає
		// "window:active:console-<build>", splash — "window:active:updater-splash".
		// Це той самий принцип «вікно неактивне → анімації на паузу», що й
		// для головного вікна, лише з адресацією по конкретному вікну.
		win.RegisterHook(events.Common.WindowFocus, func(*application.WindowEvent) {
			app.Event.Emit("window:active:"+id, json.RawMessage(`{"active":true}`))
		})
		win.RegisterHook(events.Common.WindowLostFocus, func(*application.WindowEvent) {
			app.Event.Emit("window:active:"+id, json.RawMessage(`{"active":false}`))
		})
	}

	winMu.Lock()
	winMap[id] = win
	winMu.Unlock()
}

// windowOp виконує generic-операцію над вікном ("show"|"hide"|"close"|...).
func windowOp(id, op string) {
	win := getWindow(id)
	if win == nil {
		return
	}
	// "anchor-bottom-right" (з необов'язковим ":<marginX>:<marginY>")
	// обробляється ДО switch, бо case у Go — це вираз, порівнюваний з op,
	// тож case strings.HasPrefix(op, ...) тут було б bool проти string.
	if strings.HasPrefix(op, "anchor-bottom-right") {
		anchorWindowBottomRight(win, strings.TrimPrefix(op, "anchor-bottom-right:"))
		return
	}
	switch op {
	case "show":
		win.Show()
		if id == "main" {
			setWindowHiddenMarker(false)
			// Splash апдейтера знімаємо лише коли головне вікно реально
			// показане — інакше між ними була б порожня/прозора пауза.
			onMainShown()
		}
	case "show-inactive":
		// Як "show", але БЕЗ викрадення фокуса: показуємо вікно і одразу
		// повертаємо foreground тому, хто мав його ДО показу. Потрібно
		// віджету сповіщень — тост не має переривати набір тексту.
		// Подробиці та чому саме так — window_show_*.go.
		showWindowInactive(win)
	case "hide":
		win.Hide()
		if id == "main" {
			setWindowHiddenMarker(true)
		}
	case "minimise":
		win.Minimise()
	case "unminimise":
		win.UnMinimise()
	case "maximise":
		win.Maximise()
	case "unmaximise":
		win.UnMaximise()
	case "focus":
		win.Focus()
	case "close":
		win.Close()
	case "destroy":
		// Програмне знищення: forceClose змушує хук WindowClosing закрити
		// вікно БЕЗ запиту до backend'а (той для splash повернув би "hide",
		// і splash лишився б прихованим, а не закритим).
		winMu.Lock()
		forceClose[id] = true
		delete(winMap, id)
		winMu.Unlock()
		win.Close()
	}
}

func windowSetSize(id string, w, h int) {
	if win := getWindow(id); win != nil {
		win.SetSize(w, h)
	}
}

func windowSetMinSize(id string, w, h int) {
	if win := getWindow(id); win != nil {
		win.SetMinSize(w, h)
	}
}

// parseHexColor розбирає "#rrggbb" (необов'язковий #) у application.RGBA
// з повною непрозорістю. Помилка формату повертається як error — викликач
// тихо ігнорує (залишається дефолтний фон вікна).
func parseHexColor(s string) (application.RGBA, error) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) != 6 {
		return application.RGBA{}, fmt.Errorf("invalid hex color: %s", s)
	}
	var out [3]uint8
	for i := 0; i < 3; i++ {
		v, err := strconv.ParseUint(s[i*2:i*2+2], 16, 8)
		if err != nil {
			return application.RGBA{}, err
		}
		out[i] = uint8(v)
	}
	return application.NewRGBA(out[0], out[1], out[2], 255), nil
}

// sendWindowState — зміна стану головного вікна -> backend (windowstate.go).
//
// ВАЖЛИВО (виправлення «на Linux головне вікно відкривається криво»):
//
//  1. Поле hasPos тепер шлеться ЯВНО. Раніше його тут не було, а backend
//     розбирає подію в ту саму структуру windowState і робить
//     winStateLast = st — тобто відсутнє поле ставало false і ЗАТИРАЛО
//     збережену ознаку «позиція відома». У результаті на диску лежала
//     позиція, яку ніхто не відновлював, і вікно щоразу опинялось там,
//     де його поставив WM.
//
//  2. Позиція вважається валідною лише там, де віконний менеджер узагалі
//     дозволяє клієнту знати/задавати координати (windowPositioningSupported:
//     на Wayland — ні, і це не баг, а дизайн протоколу). Під Wayland
//     Position() повертає нулі, і збереження їх як «реальної позиції»
//     давало відновлення вікна в лівий верхній кут екрана.
//
//  3. Під час відновлення стану (restoreWindowState) події resize/move
//     ігноруються — інакше проміжна геометрія (спершу дефолтна, потім
//     максимізація) перезаписала б щойно прочитаний із диска стан.
func sendWindowState(win *application.WebviewWindow) {
	if win == nil {
		return
	}
	mainStateMu.Lock()
	restoring := mainRestoring
	mainStateMu.Unlock()
	if restoring {
		return
	}

	w, h := win.Size()
	x, y := win.Position()
	hasPos := windowPositioningSupported() && isSanePosition(x, y)
	st, _ := json.Marshal(map[string]any{
		"width":      w,
		"height":     h,
		"x":          x,
		"y":          y,
		"hasPos":     hasPos,
		"maximised":  win.IsMaximised(),
		"fullscreen": win.IsFullscreen(),
	})
	backendDispatch("window:state", string(st))
}

// isSanePosition відсіює координати, які точно не є реальним місцем
// вікна: нулі (типова відповідь GTK, коли WM не повідомляє позицію) і
// від'ємні «за екраном» значення, які іноді приходять під час
// згортання/максимізації.
func isSanePosition(x, y int) bool {
	if x == 0 && y == 0 {
		return false
	}
	return x > -32000 && y > -32000 && x < 32000 && y < 32000
}

// savedWindowState — стан головного вікна, збережений backend'ом
// (window-state.json). Дзеркало структури windowState у backend.
type savedWindowState struct {
	Width      int  `json:"width"`
	Height     int  `json:"height"`
	X          int  `json:"x"`
	Y          int  `json:"y"`
	HasPos     bool `json:"hasPos"`
	Maximised  bool `json:"maximised"`
	Fullscreen bool `json:"fullscreen"`
}

// fetchSavedWindowState просить backend віддати збережений стан вікна.
// Викликається ДВІЧІ за різних обставин:
//   - до створення вікна (loadBackendAndShowMain) — щоб вікно одразу
//     народилось потрібного розміру;
//   - на WindowRuntimeReady — як фолбек, якщо перший виклик не вдався
//     (backend ще не був готовий).
func fetchSavedWindowState() (savedWindowState, bool) {
	var st savedWindowState
	stJSON := backendDispatch("window:ready:main", "")
	if stJSON == "" {
		return st, false
	}
	if err := json.Unmarshal([]byte(stJSON), &st); err != nil {
		return st, false
	}
	return st, true
}

// restoreWindowState — головне вікно готове (WindowRuntimeReady):
// застосовуємо збережений стан, якщо його ще не застосували при
// створенні вікна.
//
// ВАЖЛИВО (Linux): основний шлях відновлення розміру тепер — ПАРАМЕТРИ
// СТВОРЕННЯ вікна (див. loadBackendAndShowMain у updater_phase.go).
// Раніше вікно створювалось дефолтним 1280x820, показувалось, і лише
// після завантаження сторінки йому міняли розмір/позицію. На Windows це
// майже непомітно, а на GTK — видимий стрибок: вікно з'являється одного
// розміру, потім перескакує в інший, WebKit переверстує сторінку, і в
// частині WM/композиторів вікно лишається в поламаній геометрії (саме
// те «криво відображає головний екран»). Тут лишилась лише максимізація/
// повний екран і фолбек, якщо на етапі створення стан прочитати не
// вдалось.
func restoreWindowState(win *application.WebviewWindow) {
	if win == nil {
		return
	}
	mainStateMu.Lock()
	already := mainStateApplied
	mainStateMu.Unlock()

	st, ok := fetchSavedWindowState()
	if !ok {
		return
	}
	applyWindowState(win, st, !already)
}

// applyWindowState застосовує стан до вікна. withGeometry=false означає,
// що розмір/позицію вже застосували при створенні — лишається тільки
// розгорнути/розгорнути на весь екран.
func applyWindowState(win *application.WebviewWindow, st savedWindowState, withGeometry bool) {
	if win == nil {
		return
	}
	// Прапорці ставимо і знімаємо КОРОТКИМИ секціями, не утримуючи
	// м'ютекс під час викликів у вікно (див. коментар при оголошенні
	// mainStateMu — інакше синхронний хук resize дав би дедлок).
	mainStateMu.Lock()
	mainRestoring = true
	mainStateApplied = true
	mainStateMu.Unlock()
	defer func() {
		mainStateMu.Lock()
		mainRestoring = false
		mainStateMu.Unlock()
	}()

	if withGeometry {
		if w, h := sanitizeWindowSize(st.Width, st.Height); w > 0 {
			win.SetSize(w, h)
		}
		// Позицію відновлюємо лише там, де це взагалі має сенс: під
		// Wayland клієнт не може позиціонувати власне вікно, і спроба це
		// зробити або ігнорується, або дає вікно в куті екрана.
		if st.HasPos && windowPositioningSupported() && isSanePosition(st.X, st.Y) {
			win.SetPosition(st.X, st.Y)
		}
	}
	if st.Maximised {
		win.Maximise()
	}
	if st.Fullscreen {
		win.Fullscreen()
	}
}

// sanitizeWindowSize перевіряє збережений розмір на осмисленість.
//
// Раніше тут стояв жорсткий clamp 1920x1080. Це ламало Linux-користувачів
// з 2560x1440/4K-моніторами: вікно, розтягнуте на пів екрана 1440p,
// після перезапуску стискалось до 1920x1080, і разом із відновленою
// позицією виглядало саме як «лаунчер відкрився криво». Розмір узятий з
// РЕАЛЬНОГО вікна на цій же машині, тож обмежувати його чужим числом
// сенсу нема — відсікаємо лише явне сміття (нулі, від'ємні, абсурдно
// великі значення після зміни конфігурації моніторів).
func sanitizeWindowSize(w, h int) (int, int) {
	if w < winMinWidth || h < winMinHeight {
		return 0, 0
	}
	if w > 16000 || h > 16000 {
		return 0, 0
	}
	return w, h
}

// winMinWidth/winMinHeight мають збігатися з MinWidth/MinHeight головного
// вікна (updater_phase.go) і з winMinW/winMinH у backend/windowstate.go.
const (
	winMinWidth  = 960
	winMinHeight = 640
)

// ── Спеціальне позиціонування (віджет сповіщень у трей) ───────────────

// defaultAnchorMargin — відступ від країв робочої області, якщо backend
// не вказав власний MarginX/MarginY.
const defaultAnchorMargin = 18

// wsExNoActivate — розширений стиль Windows: вікно НЕ активізується при
// показі/кліку. Константа лишається тут як документація рішення: її НЕ МОЖНА
// віддати в WebviewWindowOptions.Windows.ExStyle (Wails там ЗАМІНЮЄ усі
// стилі — див. коментар у windowCreate), тож крадіжка фокуса вирішується
// операцією "show-inactive", а не стилем вікна.
const wsExNoActivate = 0x08000000

// explicitPosition переводить «є власні координати» в стартовий режим
// позиціонування Wails: WindowXY — «X/Y відносно робочої області екрана».
func explicitPosition(has bool) application.WindowStartPosition {
	if has {
		// WindowXY — «X/Y відносно робочої області екрана». Без цього Wails
		// проігнорував би наші координати (або інтерпретував нулі як (0,0)).
		return application.WindowXY
	}
	// WindowCentered — дефолтний стартовий стан Wails (значення 0).
	// Позицію НЕ задаємо: Wails сам вирішить, де розмістити вікно, як і
	// раніше (коли в opts її взагалі не було).
	return application.WindowCentered
}

// computeBottomRightXY рахує координати правого нижнього кута вікна в
// робочій області основного монітора. Повертає ok=false, якщо монітор
// поки недоступний — тоді позиціюю застосує пізніше windowOp
// ("anchor-bottom-right").
func computeBottomRightXY(w, h, marginX, marginY int) (int, int, bool) {
	sc := primaryScreen()
	if sc == nil {
		return 0, 0, false
	}
	if marginX <= 0 {
		marginX = defaultAnchorMargin
	}
	if marginY <= 0 {
		marginY = defaultAnchorMargin
	}
	wa := sc.WorkArea
	return wa.X + wa.Width - w - marginX, wa.Y + wa.Height - h - marginY, true
}

// primaryScreen — робоча область основного монітора або nil.
func primaryScreen() *application.Screen {
	app := application.Get()
	if app == nil || app.Screen == nil {
		return nil
	}
	return app.Screen.GetPrimary()
}

// anchorWindowBottomRight переставляє вікно у правий нижній кут
// робочої області основного монітора. args — ":<mx>:<my>" з операційного
// рядка або порожній рядок для значень за замовчуванням.
//
// Чому це викликається ЗНАНОВА після кожного SetSize: віджет сповіщень
// зростає вгору (нові картки додаються зверху стосу), тож точка прив'язки
// — НИЖНІЙ кут. Перерахунок після зміни розміру тримає його фіксованим.
func anchorWindowBottomRight(win *application.WebviewWindow, args string) {
	if win == nil {
		return
	}
	marginX, marginY := 0, 0
	if parts := strings.Split(strings.TrimPrefix(args, ":"), ":"); len(parts) >= 2 {
		marginX, _ = strconv.Atoi(parts[0])
		marginY, _ = strconv.Atoi(parts[1])
	}
	w, h := win.Size()
	x, y, ok := computeBottomRightXY(w, h, marginX, marginY)
	if !ok {
		return
	}
	win.SetPosition(x, y)
}
