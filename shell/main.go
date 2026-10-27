package main

// main.go — ShaurmLauncher.exe / ShaurmLauncherBase.exe (shell).
//
// НОВА АРХІТЕКТУРА: shell.exe — тонкий, НАЗАВЖДИ незмінний Wails-застосунок,
// який НЕ містить жодної бізнес-логіки лаунчера. Він:
//   1. реєструє ЄДИНИЙ Wails-сервіс Bridge{Invoke(method, argsJson)} — фронт
//      спілкується з ним, а він просто проксирує рядки у підвантажену DLL;
//   2. завантажує backend.dll (або updater.dll) через LoadLibrary і викликає
//      чотири експортовані функції (bridge.h): backend_init / backend_invoke /
//      backend_shutdown / backend_free;
//   3. виконує generic-команди DLL -> shell через 16 колбеків (вікна, трей,
//      діалоги, події) — ЖОДНОГО спеціалізованого поняття ("консоль",
//      "buildID") він не знає.
//
// Будь-яка нова фіча лаунчера реалізується ВСЕРЕДИНІ backend.dll і НЕ вимагає
// перекомпіляції цього файлу (не кажучи вже про перекомпіляцію exe).
//
// Збірка: CGO_ENABLED=0 (wails v3 на Windows не потребує cgo — тільки
// syscall), `go build` з wails3 generate syso для іконки/маніфесту.
//
// ГОНКА СТАРТУ (Linux, знайдено емпірично — panic у dispatchOnMainThread):
// updaterModule.initModule()/backendModule.initModule() викликаються ще ДО
// app.Run(). Якщо .so синхронно (чи з власної горутини, майже одразу)
// шле колбек назад у shell (cgoEmit і т.п.), той колбек кличе
// application.InvokeAsync -> dispatchOnMainThread, а внутрішня черга
// головного потоку в GTK-варіанті Wails v3 (Linux) ініціалізується лише
// ВСЕРЕДИНІ app.Run(), не в application.New(). application.Get() до
// цього моменту вже повертає НЕ nil (об'єкт створений), тож перевірка
// "app != nil" у колбеках не рятує — падає рівно в dispatchOnMainThread
// на недоініціалізованому полі. На Windows та сама послідовність,
// вочевидь, не проявляється (native message queue живе одразу після
// створення вікна, до явного "run loop"), тому раніше це не було
// помітно. Захист — mainThreadReady (main_thread_ready.go): усі
// колбеки, які роблять InvokeAsync/InvokeSync, чекають на нього перед
// першим викликом; закривається одноразово одразу після старту app.Run().

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// startupGraceDelay — мінімальна пауза перед першим initModule (updater.so
// чи backend.so), щоб дати app.Run() встигнути ініціалізувати внутрішній
// диспетчер головного потоку (див. коментар про гонку старту вище). Те ж
// значення, що вже емпірично використовується в bootstrapAndLoadUpdater
// для аналогічної паузи (700мс з UI встигає стартувати splash перед
// якісним завантаженням) — тут той самий порядок величини навмисно,
// а не довільне число.
const startupGraceDelay = 700 * time.Millisecond

// webviewUserDataPath — тека профілю WebView2 лаунчера. ОДИН ФІКСОВАНИЙ
// шлях: <тека exe>\webview2\current — ТАМ, ДЕ ЛЕЖИТЬ exe (installDir),
// без хешів і без версійного номера в шляху (вимога ТЗ). Чи треба
// перестворювати профіль після оновлення, вирішує маркер вмісту
// (content_marker.go, syncWebViewProfile), а не ім'я теки: тека лишається
// недоторканою, поки frontend/backend реально не змінились — так
// логіни/cookies користувача переживають оновлення, але закешований
// старий фронт не залипає.
//
// Раніше профіль жорстко сидів у %LOCALAPPDATA%\<InstallDir>\webview2 і
// лишався там, навіть якщо користувач обрав власну теку встановлення.
// Тепер профіль завжди поруч з exe — куди б лаунчер не встановили.
// installDirName — ім'я теки, де лежить exe (ShaurmLauncher чи
// ShaurmLauncherBase). Використовується, щоб дві редакції лаунчера не
// ділили SingleInstance-lock.
func installDirName() string {
	installName := "ShaurmLauncher"
	if exe, err := os.Executable(); err == nil {
		if base := filepath.Base(filepath.Dir(exe)); base != "." && base != string(filepath.Separator) && base != "" {
			installName = base
		}
	}
	return installName
}

func webviewUserDataPath() string {
	return filepath.Join(exeDir(), "webview2", "current")
}

// main — життєвий цикл:
//
//  1. applyPendingUpdaterSwap: застосовує updater.dll.new -> updater.dll;
//  2. створює Wails-застосунок з сервісом Bridge (фронтенд бачить лише його);
//  3. стартова фаза: якщо поруч є updater.dll — спочатку вікно
//     "updater-splash" і оновлення, потім (по phase_complete) перемикання на
//     backend.dll; інакше — одразу backend.dll + головне вікно.
//
// launchPackID — packID з CLI-аргумента "--launch <packID>" (ярлик на
// робочому столі, див. backend/shortcuts.go). Зберігається в package-level
// змінній: значення потрібне не одразу в main(), а ПІСЛЯ завантаження
// backend.dll (тоді подія shortcut:launch поїде у backend, а він — у
// фронтенд).
var launchPackID string
var launchAccountID string
var launchAutoJoinServer string

// openPackFilePath — шлях до файлу збірки (.shaurmpack/.shaurmbundle),
// відкритого через Windows-асоціацію («Open with», двічі клік у Провіднику
// або «Відкрити за допомогою → Shaurma Launcher»). Windows передає шлях
// як ПЕРШИЙ позиційний аргумент командного рядка. Зберігається в
// package-level змінній (як launchPackID): значення потрібне ПІСЛЯ
// завантаження backend.dll — тоді подія packfile:open поїде у backend, а
// він — у фронтенд (авто-імпорт збірки).
var openPackFilePath string

// openFriendLink — посилання-запрошення в друзі, передане ОС через
// custom-схему shaurma://friend/@handle?n=Nick (сайт: /friend/… →
// friend.html → shaurma://). Windows/Linux запускають exe з URL як
// аргументом. Зберігаємо так само, як openPackFilePath: диспатч у backend
// (подія friend:open) — після завантаження backend.dll.
var openFriendLink string

// friendLinkScheme — префікс схеми запрошення в друзі (порівняння без регістру).
const friendLinkScheme = "shaurma://friend/"

// parseFriendLinkArg шукає серед аргументів URL shaurma://friend/… і
// повертає його нормалізованим до https-форми лаунчера
// (https://shaurma-launcher.org/friend/…), яку розуміє friendLink.ts. Інакше "".
func parseFriendLinkArg(args []string) string {
	for i := 1; i < len(args); i++ {
		a := strings.TrimSpace(args[i])
		if len(a) <= len(friendLinkScheme) || !strings.EqualFold(a[:len(friendLinkScheme)], friendLinkScheme) {
			continue
		}
		rest := strings.TrimRight(a[len(friendLinkScheme):], "/")
		if rest == "" {
			continue
		}
		// ?n=… лишається в кінці; слеш перед "?" (shaurma://friend/@h/?n=x) прибираємо.
		rest = strings.Replace(rest, "/?", "?", 1)
		return "https://shaurma-launcher.org/friend/" + rest
	}
	return ""
}

// dispatchFriendLinkToBackend передає посилання-запрошення у backend (подія
// friend:open). Лише з UI-потоку, коли backend.dll вже підключено.
func dispatchFriendLinkToBackend() {
	if openFriendLink == "" {
		return
	}
	payload := struct {
		URL string `json:"url"`
	}{URL: openFriendLink}
	openFriendLink = ""
	b, _ := json.Marshal(payload)
	backendDispatch("friend:open", string(b))
}

// revealFromSecondInstance — користувач запустив лаунчер ще раз, поки
// працює перший екземпляр («у фоні»/в трей). Має завжди бути видимий
// результат, на якому б етапі перший не був:
//   - головне вікно вже є (сховане/згорнуте) → показати й сфокусувати,
//     а backend повідомити, щоб він не сховав його знову (прапорець
//     фонового старту) і не вважав вікно неактивним;
//   - вікна ще немає (триває оновлення/завантаження) → скинути
//     фоновий режим, щоб вікно з'явилось одразу після готовності, і
//     показати splash, щоб було видно: лаунчер запускається.
//
// Викликати з UI-потоку.
func revealFromSecondInstance() {
	startBackground = false
	if getWindow("main") != nil {
		windowOp("main", "show")
		windowOp("main", "unminimise")
		windowOp("main", "focus")
		backendDispatch("window:reveal", "")
		return
	}
	if getWindow(splashWindowID) != nil {
		windowOp(splashWindowID, "show")
	}
	// Backend міг уже стартувати з background:true — скидаємо й там.
	backendDispatch("window:reveal", "")
}

// startBackground — лаунчер стартував з --background (див. main()).
var startBackground bool

// hasArg — чи є точний прапорець серед аргументів.
func hasArg(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

// parseLaunchArg витягує значення --launch / --account / --server з os.Args
// (або з args другого інстансу при повторному кліку по ярлику, коли
// лаунчер уже запущений).
//
// --account / --server — персональні опції ЯРЛИКА (як у Prism):
// перекривають акаунт/автоприєднання збірки рівно для запуску з цього
// ярлика. Решта параметрів запуску — як при звичайному запуску.
func parseLaunchArg(args []string) string {
	for i := 0; i < len(args)-1; i++ {
		switch args[i] {
		case "--launch":
			if args[i+1] != "" {
				launchPackID = args[i+1]
			}
		case "--account":
			if args[i+1] != "" {
				launchAccountID = args[i+1]
			}
		case "--server":
			if args[i+1] != "" {
				launchAutoJoinServer = args[i+1]
			}
		}
	}
	return launchPackID
}

// packFileExts — розширення файлів збірок, які лаунчер розпізнає при
// запуску з файлу (Windows-асоціація). Реєстрація самих асоціацій — в
// installer.iss (.shaurmpack/.shaurmbundle -> цей exe).
var packFileExts = []string{".shaurmpack", ".shaurmbundle"}

// isPackFileArg повертає true, якщо args[i] — шлях до файлу збірки
// (розширення .shaurmpack/.shaurmbundle) і файл реально існує.
func isPackFileArg(arg string) bool {
	if arg == "" || strings.HasPrefix(arg, "-") {
		return false
	}
	ext := strings.ToLower(filepath.Ext(arg))
	for _, e := range packFileExts {
		if ext == e {
			if _, err := os.Stat(arg); err == nil {
				return true
			}
			return false
		}
	}
	return false
}

// parsePackFilePathArg витягує з os.Args (або з args другого інстансу)
// шлях до відкритого файлу збірки. Windows-асоціація передає шлях як
// позиційний аргумент; для сумісності приймаємо також явний
// "--open <path>". Аргументи-значення відомих прапорців (--launch/
// --account/--server) пропускаються — packID з ярлика файлом не є.
// Повертає абсолютний шлях або "".
func parsePackFilePathArg(args []string) string {
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--launch", "--account", "--server", "--open", waitPIDFlag:
			i++ // пропускаємо значення прапорця
		default:
			if !isPackFileArg(args[i]) {
				continue
			}
			if abs, err := filepath.Abs(args[i]); err == nil {
				return abs
			}
			return args[i]
		}
	}
	return ""
}

// dispatchPackFileOpenToBackend передає шлях відкритого файлу збірки у
// backend (подія packfile:open). Викликається лише з UI-потоку (після
// loadBackendAndShowMain), коли backend.dll гарантовано завантажено і
// підключено.
func dispatchPackFileOpenToBackend() {
	if openPackFilePath == "" {
		return
	}
	payload := struct {
		Path string `json:"path"`
	}{
		Path: openPackFilePath,
	}
	openPackFilePath = ""
	b, _ := json.Marshal(payload)
	backendDispatch("packfile:open", string(b))
}

// dispatchLaunchToBackend передає дані запуску з ярлика (packID + опції)
// у backend (подія shortcut:launch). Викликається лише з UI-потоку (після
// loadBackendAndShowMain), коли backend.dll гарантовано завантажено і
// підключено.
func dispatchLaunchToBackend() {
	if launchPackID == "" {
		return
	}
	payload := struct {
		PackID         string `json:"packId"`
		AccountID      string `json:"accountId,omitempty"`
		AutoJoinServer string `json:"autoJoinServer,omitempty"`
	}{
		PackID: launchPackID, AccountID: launchAccountID, AutoJoinServer: launchAutoJoinServer,
	}
	launchPackID = ""
	launchAccountID = ""
	launchAutoJoinServer = ""
	b, _ := json.Marshal(payload)
	backendDispatch("shortcut:launch", string(b))
}

func main() {
	// Краш-лог shell-процесу — ПЕРШЕ, до будь-якої іншої роботи (crashlog.go).
	initCrashLog()

	// Безпечний режим (supervisor.go: після повторюваних аварій). Пишемо в
	// bootstrap.log Одразу: якщо користувач питає «чому немає Discord/GPU» —
	// відповідь мусить бути в лозі, а не тільки в тості.
	if safeModeRequested() {
		bootstrapLog(exeDir(), "shell: БЕЗПЕЧНИЙ РЕЖИМ (SHAURMA_SAFE_MODE=1) — без GPU, "+
			"без автоперевірки буфера обміну, без Discord RPC і прогресу в таскбарі")
	}

	// Самоперезапуск (оновлення/self-update): якщо нас запущено з
	// --wait-pid, чекаємо завершення процесу-попередника, перш ніж чіпати
	// UI/single-instance. Без цього новий процес міг побачити ще зайнятий
	// single-instance мутекс (старий звільняє його лише під час shutdown) і
	// безслідно вийти — «лаунчер запустився і зник». Деталі — relaunch_wait.go.
	waitForPreviousInstance()

	// Нагляд: батьківський процес лише стежить і перезапускає лаунчер після аварійного виходу (supervisor.go).
	superviseIfParent()

	// Env-фікси для WebKitGTK/Mesa/EGL — ОБОВ'ЯЗКОВО перше, до будь-якого
	// GTK/WebKit виклику (application.New, syncWebViewProfile тощо), бо
	// WebKit читає ці змінні лише при власній ініціалізації. No-op на
	// Windows. Виправляє "вікно повністю біле" (див. коментар у
	// webview_compat_linux.go) — типово через libEGL/MESA ZINK помилки
	// в оточеннях без прокинутого GPU (WSL2, headless, старі драйвери).
	applyLinuxWebViewCompatEnv()

	// ГЛОБАЛЬНИЙ ЗАХИСТ: будь-яка паніка в main goroutine НІКОЛИ не зникне
	// мовчки. Ми ловимо її ТУТ і показуємо нативне вікно помилки Windows
	// (MessageBox), що НЕ залежить від WebView2 чи Wails. Без цього
	// recover'у паніка вбиває процес, користувач бачить «нічого не
	// відбувається» — жодного вікна, жодного повідомлення.
	defer func() {
		if r := recover(); r != nil {
			stack := fmt.Sprintf("%v", r)
			logShellPanic("main", r, debug.Stack())
			// showStartupCrashDialog — платформозалежна реалізація
			// (crash_dialog_windows.go / crash_dialog_linux.go): на
			// Windows це нативний MessageBox, на Linux — GTK4-діалог
			// через той самий механізм, що й showFatalError. ОБОВ'ЯЗКОВО
			// не WebView2/Wails — паніка тут означає, що весь UI-стек ще
			// міг не піднятись.
			showStartupCrashDialog(stack)
			os.Exit(1)
		}
	}()

	// GPU-supervisor (Linux): перехоплює panic ДО стандартного обробника
	// вище (дефери LIFO) — на пробному GPU-старті замість crash-діалогу
	// тихо перезапускає себе у безпечному режимі. SEGFAULT у cgo сюди не
	// потрапляє (процес вмирає), але crash-маркер на диску переводить
	// наступний запуск у safe mode — див. gpu_recovery_linux.go.
	defer recoverGPUStartupRecover()

	// GPU-supervisor (Linux): рішення про режим рендерингу (пробний запуск
	// зі звичайним прискоренням чи безпечний режим після крашу) — ДО
	// будь-якого GTK/WebKit виклику. No-op поза Linux.
	recoverGPUStartup()

	// CLI-аргументи: "--launch <packID>" (ярлик на робочому столі) та
	// файл збірки (.shaurmpack/.shaurmbundle через Windows-асоціацію).
	// Запам'ятовуємо заздалегідь — диспатч у backend відбудеться після
	// завантаження backend.dll.
	launchPackID = parseLaunchArg(os.Args)
	openPackFilePath = parsePackFilePathArg(os.Args)
	openFriendLink = parseFriendLinkArg(os.Args)
	// --background: автозапуск «у фоні» (startupMode=background) — головне
	// вікно не показується, лише трей. Лише для ХОЛОДНОГО старту.
	startBackground = hasArg(os.Args[1:], "--background")
	setWindowHiddenMarker(startBackground)

	// Self-update компонентів: .new-файли з минулого запуску (updater.dll.new,
	// backend.dll.new) застосовуємо ДО LoadLibrary — Windows тримає лок на
	// завантажену DLL, тож замінити її можна лише поки вона не завантажена.
	applyPendingUpdates()

	// Старий WebView2-профіль з %LOCALAPPDATA% переносимо у теку біля exe
	// (новий дім профілю) — поки профіль ніхто не відкрив.
	migrateLegacyLocalAppDataProfile()

	// Маркер вмісту WebView2-профілю: якщо frontend/backend/updater реально
	// змінились (sha256), стара тека webview2\current видаляється і створюється
	// заново; якщо контент той самий — профіль з логінами/cookies лишається.
	// ВАЖЛИВО: ДО application.New і ДО створення будь-якого вікна WebView2 —
	// видаляти теку, яку WebView2 вже відкрив, Windows не дозволить.
	syncWebViewProfile()

	// Нативний перехоплювач аварій (Windows): аварія на рівні ОС —
	// пошкодження купи (0xC0000374), access violation у WebView2/драйвері —
	// вбиває процес БЕЗ сліду в launcher.log і crash.log, і користувач
	// бачить лише «дочірній процес завершився аварійно: exit status
	// 0xc0000374» від наглядача. Ставимо фільтр ДО створення вікон/WebView2,
	// щоб наступна така аварія лишила звіт з модулем і міні-дампом
	// (logs/crash-native.log + logs/crash-*.dmp). No-op поза Windows і в
	// батьківському процесі-наглядачі (він виходить раніше).
	installNativeCrashReporter()

	initCallbacks()
	bridgeSvc = newBridgeService()

	app := application.New(application.Options{
		// На Linux Options.Name — це НЕ лише підпис: Wails робить з нього
		// GTK application-id (`org.wails.<sanitizeAppName(Name)>`), а той
		// визначає, який .desktop-файл оболонка (GNOME/KDE/SteamOS)
		// зіставить з нашим вікном — і звідки візьме іконку для
		// таскбара/доку та таск-менеджера. Тому на Linux appOptionsName()
		// повертає ТЕХНІЧНИЙ токен редакції (не людяний текст — сам
		// Options.Name ніде не рендериться напряму) — інакше обидві
		// редакції ділили б один application-id і один .desktop-файл.
		// Деталі та контракт із пакувальниками — app_identity.go,
		// app_identity_linux.go, pack-manager/internal/launcherbuild/
		// paths.go (linuxAppIDTokens).
		//
		// Description — ЛЮДЯНА назва, яку реально може побачити
		// користувач (деякі DE підтягують її як tooltip/тему вікна).
		// Однакова на ВСІХ редакціях (launcherDisplayName()) — жодної
		// згадки "Base"/"повна" тут бути не повинно.
		Name:        appOptionsName(),
		Description: launcherDisplayName(),
		// Linux: WM_CLASS (g_get_prgname) має дорівнювати StartupWMClass
		// у .desktop-файлах пакетів (== ім'я бінарника), інакше на X11
		// оболонка не знайде застосунок для вікна і покаже узагальнену
		// іконку. На GLib це один виклик g_set_prgname до старту GTK.
		Linux: application.LinuxOptions{
			ProgramName: exeBaseName(),
		},
		Services: []application.Service{
			application.NewService(bridgeSvc),
		},
		// SingleInstance: клік по ярлику збірки на робочому столі (або
		// повторний запуск exe), коли лаунчер УЖЕ запущений — не створює
		// другий процес, а передає аргументи цьому. Далі ми розбираємо
		// --launch <packID> і форвардимо у backend (подія shortcut:launch) —
		// фронтенд запускає потрібну збірку. Без цього ярлики працювали б
		// лише з холодного старту лаунчера.
		SingleInstance: &application.SingleInstanceOptions{
			// UniqueID прив'язаний до редакції (ShaurmLauncher / ShaurmLauncherBase)
			// через ім'я теки встановлення — щоб дві редакції не блокували
			// одна одну (це НЕ один і той самий застосунок).
			UniqueID: "com.shaurma.launcher.single-instance." + installDirName(),
			OnSecondInstanceLaunch: func(data application.SecondInstanceData) {
				pid := parseLaunchArg(data.Args)
				pfp := parsePackFilePathArg(data.Args)
				if pid != "" {
					launchPackID = pid
				}
				if pfp != "" {
					openPackFilePath = pfp
				}
				if fl := parseFriendLinkArg(data.Args); fl != "" {
					openFriendLink = fl
				}
				// Повторний запуск exe/ярлика, коли лаунчер уже працює (у т.ч.
				// сховано в трей у фоновому режимі) = «покажи мене». Виняток —
				// другий автозапуск ОС (--autostart), який вікно не відкриває.
				// Раніше без --launch/файлу/посилання повторний запуск не робив нічого.
				if pid == "" && pfp == "" && openFriendLink == "" && !hasArg(data.Args, "--autostart") {
					application.InvokeAsync(revealFromSecondInstance)
				}
				if pid != "" || pfp != "" || openFriendLink != "" {
					// Показуємо і фокусуємо головне вікно (воно могло бути
					// згорнуте в трей) і передаємо дані у backend. Усе через
					// UI-потік — backendDispatch так і має викликатись.
					application.InvokeAsync(func() {
						windowOp("main", "show")
						windowOp("main", "focus")
						dispatchLaunchToBackend()
						dispatchPackFileOpenToBackend()
						dispatchFriendLinkToBackend()
					})
				}
			},
		},
		// Іконка вікна/панелі задач на Windows береться з EXE-ресурсу
		// (build/windows/icon.ico через `wails3 generate syso`).
		Assets: application.AssetOptions{
			// Диск-перший asset-сервер: файли фронтенду читаються з
			// installDir/frontend/** (або frontend/dist поруч з exe), а НЕ
			// вшиваються в бінарник — тому exe ніколи не змінюється, коли
			// змінюється фронтенд (саме для цього існує frontend.zip).
			Handler: assetHandler(),
			// НЕ кешувати фронтенд взагалі: WebView2 має кожен запуск
			// віддавати саме ті файли, що лежать на диску (інакше після
			// оновлення залишиться старий index.html, що посилається на
			// старі hashed-ассети — «побитий» UI).
			Middleware: func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
					next.ServeHTTP(w, r)
				})
			},
		},
		Windows: application.WindowsOptions{
			WebviewUserDataPath: webviewUserDataPath(),
			// GPU-прискорення фронтенду: коли користувач вимкнув його в
			// налаштуваннях (backend пише ui.json поруч з exe при зміні),
			// усі WebView2-вікна отримують --disable-gpu. Це працює лише
			// через options (env WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS Wails
			// сам обнуляє), і МАЄ бути задано до першого вікна — тут і є те
			// місце. Діє глобально на всі вікна (спільне середовище WebView2).
			AdditionalBrowserArgs: launcherBrowserArgs(),
		},
		// Коли процес реально завершується — коректно зупиняємо DLL
		// (backend_shutdown: БД, фонові задачі) ПЕРЕД виходом.
		OnShutdown: shutdownShell,
	})

	// Іконка ЗАСТОСУНКУ (app_icon.go). ВАЖЛИВО: на Linux (GTK4, дефолтна
	// збірка цього проєкту) цей виклик — no-op: GTK4 прибрав усі програмні
	// API іконок, і App.SetIcon у wails v3 для GTK4 просто нічого не
	// робить (linux_cgo.go: setIcon). На Linux іконка в таскбарі/доку/
	// таск-менеджері береться ВИКЛЮЧНО з .desktop-файлу, знайденого за
	// application-id (Wayland) або WM_CLASS (X11) — саме тому ім'я
	// застосунку (appOptionsName) і .desktop-файли в пакетах мусять
	// збігатися (див. app_identity.go, desktopid.go, і докладніше —
	// app_icon.go та window_icon_linux.go). Виклик лишається для macOS
	// та Windows, де SetIcon реально працює.
	applyAppIcon(app)

	// Стартова фаза: з апдейтером чи без — уся механіка в updater_phase.go.
	// Якщо всі компоненти (updater.dll, backend.dll, frontend/index.html) на
	// диску — просто вантажимо апдейтер (він оновить блок за версією). Якщо
	// хоч ОДНОГО бракує (перший запуск після чистого встановлення — setup.exe
	// ставить лише shell.exe; або втрачено фасад/backend) — качаємо ВСІ
	// компоненти з CDN самі (bootstrap.go, splash з прогресом), і лише потім
	// вантажимо апдейтер.
	//
	// ВАЖЛИВО (Linux race, див. коментар вгорі файлу): initModule() всередині
	// loadUpdaterAndShowSplash()/bootstrapAndLoadUpdater() може синхронно чи
	// майже одразу спричинити перший колбек із .so (cgoEmit і т.п.), який
	// робить application.InvokeAsync. Якщо це станеться РАНІШЕ, ніж
	// application.Get() внутрішній диспетчер головного потоку встигне
	// ініціалізуватись (це відбувається ВСЕРЕДИНІ app.Run(), не в
	// application.New()) — падіння з SIGSEGV у dispatchOnMainThread. Тому
	// сам виклик відкладено в окрему горутину, а колбеки (cgoEmit і решта в
	// bridge_callbacks_linux.go / callbacks_windows.go) додатково чекають
	// на markMainThreadReady() перед першим InvokeAsync/InvokeSync —
	// markMainThreadReady() виставляється тут-таки, одразу як стартувала
	// горутина, що чекає перед стартом застосунку. Двошаровий захист:
	// затримка перед initModule (як і в bootstrapAndLoadUpdater — 700мс
	// вже емпірично працює на Windows) + жорсткий бар'єр у самих колбеках
	// на випадок, якщо конкретне .so встигне відповісти швидше.
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logShellPanic("startup goroutine", r, debug.Stack())
			}
		}()
		time.Sleep(startupGraceDelay)
		markMainThreadReady()
		application.InvokeAsync(func() {
			// Flatpak-збірка: якщо компоненти ще не на диску, спершу
			// пробуємо скопіювати їх із запеченого /app/shaurma-seed
			// (без мережі). На всіх інших способах доставки (AppImage/
			// DEB/RPM/Windows) ця функція миттєво виходить, нічого не
			// змінюючи — /.flatpak-info там просто не існує.
			seedFromFlatpakIfNeeded()
			if !launcherIncomplete() {
				loadUpdaterAndShowSplash()
			} else {
				bootstrapAndLoadUpdater()
			}
		})
	}()

	if err := app.Run(); err != nil {
		println("Error:", err.Error())
	}

	// Штатне завершення (у тому числі з помилкою app.Run): прибираємо
	// crash-маркер, щоб наступний запуск знову пробував апаратне
	// прискорення. No-op поза Linux. (os.Exit у crash-обробниках його не
	// кличе — там маркер навмисно лишається, щоб наступний старт пішов у
	// safe mode.)
	removeGPUCrashMarker()
}

// shutdownShell — OnShutdown: зупинка активного модуля (backend або updater).
func shutdownShell() {
	if current != nil {
		current.shutdownModule()
	}
	trayDestroy()
}
