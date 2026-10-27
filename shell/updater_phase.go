package main

// updater_phase.go — життєвий цикл модулів shell'а.
//
// Один і той самий shell.exe вміє працювати з ДВОМА DLL, які реалізують
// один контракт (bridge.h):
//
//	updater.dll — стартова фаза «перевірка/встановлення оновлень»;
//	backend.dll — основний UI лаунчера.
//
// Переходи: shell завантажує updater.dll -> та показує вікно "updater-splash";
// коли апдейтер завершує роботу (колбек phase_complete), shell викликає
// його graceful shutdown, підміняє міст на backend.dll і показує головне
// вікно замість splash. ОБИДВІ DLL лишаються завантаженими на весь час
// життя процесу (FreeLibrary на c-shared Go-бібліотеці — crash-ризик,
// див. transitionToBackend).
//
// РОЛІ (лаунчер НЕ оновлює нічого):
//   - updater.dll оновлює ВСЕ, включно з самим собою: нову версію себе
//     качає як updater.dll.new (Windows не дає перезаписати завантажену
//     DLL), а цей shell при НАСТУПНОМУ старті застосовує її
//     (applyPendingUpdates нижче, ДО LoadLibrary).
//   - backend.dll (лаунчер) лише читає версію (CheckUpdate) — жодних
//     качань компонентів.
//   - shell.exe при ПЕРШОМУ запуску після чистого встановлення (коли
//     updater.dll на диску немає — setup.exe ставить лише exe) сам качає
//     всі компоненти з CDN у splash-вікні (bootstrap.go) і лише потім
//     вантажить апдейтер.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

const splashWindowID = "updater-splash"

var (
	current       *dllModule // активний модуль (updater АБО backend)
	backendModule *dllModule
	updaterModule *dllModule
)

// exeDir — записувана installDir: місце, куди updater кладе
// backend.so/backend.dll, launcher-version.json і куди Apply() пише
// оновлення. На Windows це тека, де фізично лежить .exe (там і
// встановлює звичайний інсталятор). На Linux — НЕ тека AppImage:
// AppImage монтується FUSE'ом як read-only (/tmp/.mount_XXXXXX/usr/bin/),
// писати туди неможливо в принципі, звідси й "read-only file system"
// при спробі покласти backend.so.part поруч з бінарником. Платформо-
// залежна реалізація — exeDir_linux.go / exeDir_windows.go.

// ── Self-update власного exe (shell) ──────────────────────────────────────
// Windows НЕ дає перезаписати/видалити запущений exe, але ДОЗВОЛЯЄ
// ПЕРЕЙМЕНУВАТИ його (змінюється лише запис каталогу, а не образ). Тому
// оновлення самого shell виконується за схемою користувача «~»:
//
//	ShaurmLauncher.exe      -> ShaurmLauncher.exe~   (активний, ще працює)
//	ShaurmLauncher.exe.new  -> ShaurmLauncher.exe    (новий, на його місці)
//
// Після swap os.Executable() все одно повертає ОРИГІНАЛЬНИЙ шлях
// (GetModuleFileName бере шлях запуску, який Windows не оновлює при
// rename), а на цьому шляху тепер лежить НОВИЙ файл — тож relaunchSelf()
// стартує вже нову версію, а старий процес (образ з «~») завершується.

// shellOldSuffix — суфікс, під яким лишається старий активний exe під час
// self-update («~»). Видаляється при наступному старті, коли попередній
// процес уже завершився і файл розблокувався.
const shellOldSuffix = "~"

// looksLikeExecutable — дешевий саніті-чек перед swap: файл має бути
// достатньо великим і починатись з правильної магії заголовка. Захист
// від того, що .new виявиться битим/чужим (CDN віддав помилку, обірвана
// качка).
//
// БАГ (виправлено): перевірялась ЛИШЕ MZ-сигнатура (PE/Windows-заголовок).
// На Linux self-update-бінарник shell — ELF (сигнатура 0x7F 'E' 'L' 'F'),
// а не MZ, тож ця перевірка завжди повертала false для КОЖНОГО валідного
// Linux .new — applyShellSelfUpdate() ніколи не проходив далі swap, і сам
// shell (той самий бінарник, що показує версію в UI) ніколи не оновлювався
// на Linux: апдейтер справно качав нову версію, install.Apply клав її як
// shell.new, а тут файл щоразу відбраковувався як "не схожий на exe" —
// звідси "лаунчер пише стару версію і не бачить оновлення" саме на Linux
// (на Windows баг непомітний, бо там і .new, і магія — PE/MZ).
func looksLikeExecutable(path string) bool {
	fi, err := os.Stat(path)
	if err != nil || fi.Size() < 1024*1024 {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var magic [4]byte
	if _, err := f.Read(magic[:]); err != nil {
		return false
	}
	if runtime.GOOS == "windows" {
		return magic[0] == 'M' && magic[1] == 'Z'
	}
	// ELF-заголовок: 0x7F 'E' 'L' 'F' — стандарт для Linux (і взагалі
	// будь-якого не-Windows виконуваного файлу цієї збірки).
	return magic[0] == 0x7F && magic[1] == 'E' && magic[2] == 'L' && magic[3] == 'F'
}

// applyShellSelfUpdate — застосовує <selfExe>.new до власного exe за
// схемою «~». Повертає (true, selfPath), якщо swap виконано: selfPath —
// ОРИГІНАЛЬНИЙ шлях exe, на якому тепер лежить НОВИЙ файл (його і треба
// запустити через relaunchExe). Повертає (false, "") якщо .new немає,
// невалідний або swap не вдався (тоді старий exe лишається на місці).
func applyShellSelfUpdate() (bool, string) {
	exe, err := os.Executable()
	if err != nil {
		return false, ""
	}
	dir := filepath.Dir(exe)
	base := filepath.Base(exe)
	newPath := filepath.Join(dir, base+".new")
	if _, err := os.Stat(newPath); err != nil {
		return false, "" // нової версії немає
	}
	if !looksLikeExecutable(newPath) {
		println("shell self-update: .new не схожий на exe, пропускаю")
		return false, ""
	}
	oldPath := filepath.Join(dir, base+shellOldSuffix)
	// Прибираємо застарілий ~ від минулого разу (якщо ще не заблокований
	// попереднім процесом) — інакше rename на місце ~ впаде.
	_ = os.Remove(oldPath)
	// Крок 1: активний exe -> ~ (rename ДОЗВОЛЕНО для запущеного exe).
	// ВІКНО КРАХУ: якщо процес впаде рівно МІЖ кроком 1 і кроком 2, на
	// оригінальному шляху на мить немає exe (лишились ~ і .new). Поточний
	// процес при цьому продовжує працювати (образ замаплений), а наступний
	// запуск через ярлик не знайде файл — відновлюється перевстановленням
	// або ручним rename (~ -> exe). Вікно мікросекундне і невід'ємне від
	// схеми «~» (Windows не дає поставити .new на місце, поки exe існує).
	if err := os.Rename(exe, oldPath); err != nil {
		println("shell self-update: не вдалось перейменувати активний exe:", err.Error())
		return false, ""
	}
	// Крок 2: новий exe на оригінальне місце. Якщо не вдалось — відкочуємо.
	if err := os.Rename(newPath, exe); err != nil {
		_ = os.Rename(oldPath, exe)
		println("shell self-update: не вдалось поставити новий exe:", err.Error())
		return false, ""
	}
	println("shell self-update: новий exe на місці, потрібен рестарт")
	return true, exe
}

// applyPendingUpdates — довстановлення .new-компонентів, залишених
// апдейтером з минулого запуску: updater.dll.new -> updater.dll і
// backend.dll.new -> backend.dll (updater не міг замінити їх, бо сам
// був завантажений / у фазі оновлення). ПЛЮС self-update власного exe
// (shell): .new застосовується через rename активного вбік «~», після
// чого процес перезапускається з нової версії. Викликається ПЕРШИМ у
// main (до LoadLibrary, до створення вікон — тож os.Exit тут безпечний).
func applyPendingUpdates() {
	dir := exeDir()
	for _, name := range []string{updaterLibraryName(), backendLibraryName()} {
		newPath := filepath.Join(dir, name+".new")
		if _, err := os.Stat(newPath); err != nil {
			continue
		}
		_ = os.Remove(filepath.Join(dir, name))
		if err := os.Rename(newPath, filepath.Join(dir, name)); err != nil {
			println("не вдалось довстановити", name, ":", err.Error())
		}
	}

	// Self-update власного exe. Якщо .new був — новий файл уже на місці,
	// запускаємо його (selfPath тепер вказує на НОВУ версію) і завершуємось.
	selfApplied, selfPath := applyShellSelfUpdate()
	if !selfApplied {
		// Немає .new — просто прибираємо застарілий ~ від попереднього
		// self-update (той процес уже завершився, файл розблокований).
		if exe, err := os.Executable(); err == nil {
			os.Remove(filepath.Join(filepath.Dir(exe), filepath.Base(exe)+shellOldSuffix))
		}
		return
	}
	if err := relaunchExe(selfPath); err != nil {
		// Не вдалось запустити новий — продовжуємо в старому (образ «~»
		// ще виконується, лаунчер працює); новий підхопиться наступного
		// запуску, бо файл на оригінальному шляху вже новий.
		println("shell self-update: рестарт не вдався:", err.Error())
		return
	}
	os.Exit(0)
}

// launcherIncomplete повертає true, якщо на диску бракує ОДНОГО з
// обов'язкових компонентів лаунчера: updater.dll, backend.dll або
// розпакованого frontend (index.html). Раніше перевірявся лише updater.dll,
// тому коли є updater, але зник фасад/backend (або качка не встигла) —
// лаунчер лишався без дизайну. Тепер брак компонента веде до повної
// bootstrap-докачки (delete-old + install-new) ще до завантаження апдейтера.
func launcherIncomplete() bool {
	dir := exeDir()
	for _, name := range []string{updaterLibraryName(), backendLibraryName()} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			return true
		}
	}
	// Розпакований фасад: frontend/index.html — вхідна точка UI.
	if _, err := os.Stat(filepath.Join(dir, "frontend", "index.html")); err != nil {
		return true
	}
	return false
}

// editionChannel — "shaurma" або "base" (яка редакція). За замовчуванням
// вгадується з імені exe (ShaurmLauncherBase.exe = base); build.ps1 може
// перевизначити через -X shaurma-shell.editionChannel=...
var editionChannel = ""

// updaterFinalize — РУЧНЕ оновлення (кнопка «Оновити» в налаштуваннях
// backend'а): ставиться в transitionToUpdater перед завантаженням
// updater.dll і передається в його config як "finalize":true. Апдейтер
// тоді встановлює оновлення БЕЗУМОВНО (не поважає вимкнене автооновлення)
// — це саме той випадок, коли користувач явно натиснув «Оновити». При
// звичайному старті застосунку лишається false — апдейтер читає
// settings.AutoUpdate сам.
var updaterFinalize = false

// setManualUpdaterFinalize ставить прапорець ручного оновлення і
// повертає попереднє значення (щоб transitionToUpdater міг його
// скинути після переходу).
func setManualUpdaterFinalize(on bool) bool {
	prev := updaterFinalize
	updaterFinalize = on
	return prev
}

func currentChannel() string {
	if editionChannel != "" {
		return editionChannel
	}
	if exe, err := os.Executable(); err == nil {
		if strings.Contains(strings.ToLower(filepath.Base(exe)), "base") {
			return "base"
		}
	}
	return "shaurma"
}

// moduleConfigJSON — конфіг, який shell передає у backend_init/updater_init
// ({"installDir":..,"lang":"uk","channel":..,"finalize":..}).
// finalize береться з updaterFinalize (ручне оновлення) — при звичайному
// старті false.
func moduleConfigJSON() string {
	cfg := map[string]any{
		"installDir": exeDir(),
		"lang":       "uk",
		"channel":    currentChannel(),
		"finalize":   updaterFinalize,
		// Фоновий старт: backend не показує вікно на UIReady (ui_ready.go).
		"background": startBackground,
	}
	b, _ := json.Marshal(cfg)
	return string(b)
}

// loadBackendAndShowMain — завантажує backend.dll, ініціалізує його і
// показує головне вікно (замість splash, якщо той був). Ідемпотентно:
// повторний виклик (оновлення → знову запуск) не створює другий модуль
// чи друге вікно.
func loadBackendAndShowMain() {
	if backendModule == nil {
		mod, err := loadModule(filepath.Join(exeDir(), backendLibraryName()))
		if err != nil {
			// Без backend немає UI. Раніше тут був лише println у консоль
			// (невидиму в GUI-застосунку) і return — користувач бачив, як
			// splash зникає, а головного вікна НЕ з'являється («лаунчер
			// зник»). Тепер: показуємо зрозуміле вікно помилки. ВАЖЛИВО:
			// splash знищуємо ЛИШЕ ПІСЛЯ показу вікна помилки (інакше splash
			// — останнє вікно, і Wails завершує застосунок до показу нового).
			println(backendLibraryName()+" load error:", err.Error())
			showFatalError(backendLibraryName(), err.Error())
			if getWindow(splashWindowID) != nil {
				windowOp(splashWindowID, "destroy")
			}
			return
		}
		backendModule = mod
	}
	bridgeSvc.swap(backendModule)
	// Повторний init після циклу «оновлення» створює СВІЖИЙ App (backend.dll
	// тримає власний initMu і перестворює appInstance) — саме те, що треба
	// після shutdown попередньої сесії.
	//
	// ВАЖЛИВО: код повернення ТРЕБА перевіряти. backend_init повертає 1,
	// якщо ServiceStartup() повернув error АБО (після відповідного патча
	// в backend/main.go) якщо ServiceStartup() запанікував — recover() там
	// ловить паніку й повертає 1 замість того, щоб убити весь процес.
	// Раніше код повернення ігнорувався (_ =), тож і охайна помилка init,
	// і зловлена паніка проходили непоміченими: головне вікно все одно
	// створювалось і показувалось — фронтенд лишався битим/порожнім, а
	// причина була видна лише в backend:ready події, яку фронт міг ще не
	// встигнути підписати. Тепер помилка init зупиняє показ головного вікна
	// і одразу показує нативне showFatalError (як для помилки LoadLibrary
	// вище) — той самий детальний текст паніки/стека потрапляє користувачу
	// й у лог, а не губиться мовчки.
	if code := backendModule.initModule(moduleConfigJSON(), &bridgeCB); code != 0 {
		println("backend_init error, code:", code)
		showFatalError(backendLibraryName(), fmt.Sprintf("backend_init повернув код помилки %d — дивись логи лаунчера (%s)", code, logsDirHint()))
		if getWindow(splashWindowID) != nil {
			windowOp(splashWindowID, "destroy")
		}
		return
	}
	current = backendModule

	// Головне вікно — створюється лише коли backend готовий (його перший
	// виклик Bridge.Invoke вже знайде ініціалізовану DLL). windowCreate —
	// no-op, якщо вікно вже існує (повторний перехід з оновлення).
	opts := winOptions{
		Title:      "Shaurma Launcher",
		Width:      1280,
		Height:     820,
		MinWidth:   winMinWidth,
		MinHeight:  winMinHeight,
		Frameless:  true,
		URL:        "index.html",
		Background: "transparent",
		// Не показуємо, доки сторінка не готова (див. armMainReveal).
		Hidden: true,
	}
	mainAlreadyExisted := getWindow("main") != nil
	// Збережений розмір вікна читаємо ДО створення вікна — backend уже
	// ініціалізований, тож "window:ready:main" відповість одразу.
	//
	// ЧОМУ САМЕ ТАК (виправлення «на Linux головне вікно відкривається
	// криво»): раніше вікно створювалось дефолтним 1280x820, одразу
	// показувалось, і тільки після завантаження сторінки (хук
	// WindowRuntimeReady) йому міняли розмір/позицію/максимізацію. GTK це
	// відпрацьовує як повноцінний цикл resize уже ПОКАЗАНОГО вікна:
	// видимий стрибок геометрії, повна переверстка WebKit, а в частині
	// композиторів — вікно, що лишилось у проміжному стані. Тепер вікно
	// одразу народжується потрібного розміру, а на runtime-ready лишається
	// хіба що максимізація.
	savedState, haveSaved := fetchSavedWindowState()
	if haveSaved {
		if w, h := sanitizeWindowSize(savedState.Width, savedState.Height); w > 0 {
			opts.Width, opts.Height = w, h
		}
	}
	b, _ := json.Marshal(opts)
	windowCreate("main", string(b))
	if haveSaved {
		// Позиція/максимізація — ДО show: показане вікно вже не смикається.
		if win := getWindow("main"); win != nil {
			applyWindowState(win, savedState, true)
		}
	}
	if startBackground && !mainAlreadyExisted {
		// Фоновий старт: вікно лишається схованим (воно створене
		// Hidden:true), запасний таймер показу НЕ заводимо. Splash
		// знімаємо тут — головне вікно вже існує, тож застосунок не
		// завершиться як «останнє вікно закрито».
		onMainShown()
	} else if mainAlreadyExisted {
		// Повторний перехід (вікно вже було показане) — чекати нічого.
		windowOp("main", "show")
	} else {
		armMainReveal()
	}

	// Ярлик збірки на робочому столі (--launch <packID>) і файл збірки
	// (.shaurmpack/.shaurmbundle, відкритий через Windows-асоціацію):
	// backend тепер завантажено і підключено — передаємо дані, фронтенд
	// відкриє сторінку збірки/імпорт. Викликається з UI-потоку (ця
	// функція завжди йде через safeInvokeAsync/InvokeAsync).
	dispatchLaunchToBackend()
	dispatchPackFileOpenToBackend()
	dispatchFriendLinkToBackend()

	// Splash апдейтера (якщо був) лишається до показу головного вікна —
	// його знімає onMainShown (windowOp "show" для "main").
}

// mainRevealFallback — скільки максимум чекати сигналу App.UIReady від
// фронтенду, перш ніж показати головне вікно все одно (щоб лаунчер ніколи
// не лишився невидимим, якщо сторінка не завантажилась).
const mainRevealFallback = 6 * time.Second

var mainShownOnce sync.Once

// armMainReveal запускає запасний таймер показу головного вікна. Штатно
// вікно показує backend за сигналом App.UIReady (фронтенд змонтовано).
func armMainReveal() {
	time.AfterFunc(mainRevealFallback, func() {
		application.InvokeAsync(func() { windowOp("main", "show") })
	})
}

// onMainShown — головне вікно показане: знімаємо splash апдейтера (один раз).
func onMainShown() {
	mainShownOnce.Do(func() {
		if getWindow(splashWindowID) != nil {
			windowOp(splashWindowID, "destroy")
		}
	})
}

// showUpdaterSplash — створює і показує splash-вікно оновлення (без
// завантаження апдейтера — для bootstrap-качки updater.dll). Опції вікна —
// ЯК У СТАРОМУ апдейтера (updater/main.go): фіксований розмір 280×340 без
// ресайзу, frameless, суцільний фон #0f0f0f (стиль старого splash).
func showUpdaterSplash() {
	opts := winOptions{
		Title:         "Shaurma Launcher — оновлення",
		Width:         280,
		Height:        340,
		MinWidth:      280,
		MinHeight:     340,
		MaxWidth:      280,
		MaxHeight:     340,
		DisableResize: true,
		Frameless:     true,
		Background:    "solid",
		BackgroundHex: "#0f0f0f",
		URL:           "updater-splash.html",
		Hidden:        startBackground,
	}
	b, _ := json.Marshal(opts)
	windowCreate(splashWindowID, string(b))
	// Фоновий автозапуск: splash лишається схованим — користувач не
	// чекав на лаунчер і не повинен бачити вікно оновлення при вході в ОС.
	if !startBackground {
		windowOp(splashWindowID, "show")
	}
}

// loadUpdaterAndShowSplash — завантажує updater.dll, ініціалізує його і
// показує splash-вікно оновлення (якщо ще не показане bootstrap-качкою).
func loadUpdaterAndShowSplash() {
	if updaterModule == nil {
		mod, err := loadModule(filepath.Join(exeDir(), updaterLibraryName()))
		if err != nil {
			// Немає апдейтера — одразу основний UI (не критично).
			loadBackendAndShowMain()
			return
		}
		updaterModule = mod
	}
	bridgeSvc.swap(updaterModule)
	_ = updaterModule.initModule(moduleConfigJSON(), &bridgeCB)
	current = updaterModule

	showUpdaterSplash()
}

// bootstrapAndLoadUpdater — ПЕРШИЙ запуск після чистого встановлення
// (setup.exe ставить ЛИШЕ shell.exe; updater.dll на диску немає). Shell
// показує splash-вікно і сам качає updater.dll з CDN (bootstrap.go) з
// прогресом, потім завантажує апдейтер як зазвичай — той качає решту
// компонентів (backend.dll, frontend.zip...). При невдачі: якщо backend.dll
// уже є (напр. видалений вручну апдейтер) — запускаємо основний UI,
// інакше — зрозуміле вікно помилки (showFatalError).
// bootstrapAndLoadUpdater — перший запуск або некомплектна установка
// (setup.exe ставить ЛИШЕ shell.exe; бракує updater.dll / backend.dll /
// розпакованого frontend). Shell показує splash-вікно і сам качає ВСІ
// компоненти з CDN (bootstrap.go) з прогресом, потім завантажує апдейтер
// як зазвичай. При невдачі: якщо backend.dll уже є — запускаємо основний UI,
// інакше — зрозуміле вікно помилки (showFatalError).
func bootstrapAndLoadUpdater() {
	bootstrapLog(exeDir(), "bootstrapAndLoadUpdater: старт (компоненти неповні)")
	showUpdaterSplash()
	go func() {
		bootstrapLog(exeDir(), "горутина bootstrap: запущена, чекаю 700мс")
		// Даємо WebView час завантажити splash і підписатись на події
		// (updater:status / updater:progress), інакше перші події качки
		// пройдуть повз сторінку.
		time.Sleep(700 * time.Millisecond)
		bootstrapLog(exeDir(), "горутина bootstrap: викликаю bootstrapUpdater")
		if err := bootstrapUpdater(); err != nil {
			bootstrapLog(exeDir(), "ПОМИЛКА bootstrapUpdater: %v", err)
			println("bootstrap updater.dll:", err.Error())
			safeInvokeAsync(func() {
				bootstrapLog(exeDir(), "InvokeAsync: показую помилку/UI")
				if app := application.Get(); app == nil {
					bootstrapLog(exeDir(), "application.Get() == nil — InvokeAsync не має UI!")
				}
				// ВАЖЛИВО (порядок): спочатку показуємо ВІКНО ПОМИЛКИ (або
				// основний UI), і ЛИШЕ потім знищуємо splash. Якщо зробити
				// навпаки — splash виявляється ОСТАННІМ відкритим вікном, і
				// Wails завершує застосунок (quit on last window closed)
				// РАНІШЕ, ніж встигне з'явитись нове вікно: користувач бачив
				// «лаунчер миттєво зник після спроби завантажити компоненти».
				if _, statErr := os.Stat(filepath.Join(exeDir(), backendLibraryName())); statErr == nil {
					// backend-модуль уже є? Тоді лаунчер може працювати без
					// апдейтера (він перевірятиме версію лише в налаштуваннях).
					loadBackendAndShowMain()
				} else {
					showFatalError(updaterLibraryName(), err.Error())
				}
				if getWindow(splashWindowID) != nil {
					windowOp(splashWindowID, "destroy")
				}
			})
			return
		}
		safeInvokeAsync(func() {
			// ВСІ компоненти (backend.dll, updater.dll, frontend) завантажені
			// на диск. Але цей процес вже міг завантажити старі DLL-модулі в
			// пам'ять (Go c-shared модулі не вивантажуються), а WebView2 тримає
			// профіль — тому свіжий фронт/DLL не підхопляться без рестарту
			// ("метод не знайдено", старий дизайн). Перезапускаємо shell сам.
			// Це де саме те, чого хотів користувач: без ручного перезапуску.
			if getWindow(splashWindowID) != nil {
				windowOp(splashWindowID, "destroy")
			}
			if err := relaunchSelf(); err != nil {
				println("рестарт після bootstrap не вдався, продовжую в цьому процесі:", err.Error())
				loadUpdaterAndShowSplash()
				return
			}
			if app := application.Get(); app != nil {
				app.Quit()
			} else {
				os.Exit(0)
			}
		})
	}()
}

// relaunchExe запускає новий процес з вказаного шляху (той самий набір
// аргументів) і повертає nil за успіху — поточний процес має завершитись
// (quit). Використовується і для звичайного relaunchSelf, і для self-update
// shell (після swap selfPath вказує на НОВИЙ файл). На Windows
// Process.Release «відпускає» хендел — дочірній живе незалежно після
// виходу батька.
func relaunchExe(exe string) error {
	// Прибираємо успадкований --wait-pid (від попередньої ланки ланцюжка) і
	// додаємо СВІЙ PID: новий процес дочекається нашого реального
	// завершення, перш ніж створювати Wails-застосунок (application.New
	// захоплює single-instance мутекс). Без цього новий процес, стартувавши
	// швидше, ніж ми звільнимо мутекс у App.cleanup() під час shutdown
	// backend'а, одразу вийде через os.Exit(ExitCode) — і лаунчер безслідно
	// зникне («запустився і закрився», порожні логи/краші). Деталі —
	// relaunch_wait.go.
	// --background НЕ успадковується сліпо (див. relaunchArgs): якщо вікно
	// зараз видиме, новий процес мусить його показати — інакше оновлення
	// з автозапуску виглядало як «лаунчер зник у трей після оновлення».
	args := relaunchArgs(os.Args[1:], windowHiddenMarkerSet(), os.Getpid())
	cmd := exec.Command(exe, args...)
	cmd.Dir = exeDir()
	// Новий екземпляр має сам стати наглядачем (supervisor.go), а не успадкувати маркер дочірнього.
	cmd.Env = withoutEnv(os.Environ(), supEnv)
	if err := cmd.Start(); err != nil {
		return err
	}
	_ = cmd.Process.Release()
	return nil
}

// relaunchSelf запускає новий процес цього ж exe (той самий набір аргументів).
// Новий процес побачить launcherIncomplete()==false і піде звичайним шляхом:
// апдейтер -> backend, усе з узгодженого стану на диску.
func relaunchSelf() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return relaunchExe(exe)
}

// preUpdateManifestHash — SHA256 локального launcher-version.json на момент
// входу в ручний цикл оновлення (transitionToUpdater). У transitionToBackend
// порівнюємо з поточним: якщо файл змінився — апдейтер реально встановив
// нові компоненти, і процес треба перезапустити (див. transitionToBackend).
//
// ЧОМУ САМЕ МАНІФЕСТ, а не наявність .new: при ручному оновленні
// backend.dll замінюється апдейтером НАПРЯМУ (вона не входить у
// IsLockedComponent — locked лише завантажена updater.dll і виконуваний
// exe), тож .new-файлів для неї може не бути, але локальний маніфест
// install.Apply переписує завжди (нова версія/компоненти).
var preUpdateManifestHash string

// localManifestHash повертає SHA256 installDir/launcher-version.json
// (порожній рядок, якщо файлу немає або він не читається).
func localManifestHash() string {
	return manifestHashOf(exeDir())
}

// manifestHashOf — чиста версія localManifestHash для тестів: SHA256 файлу
// launcher-version.json у вказаній теці.
func manifestHashOf(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "launcher-version.json"))
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// showFatalError — показує просте вікно помилки, коли критичний компонент
// (backend.dll/backend.so) не вдалось завантажити. Раніше це був невидимий
// println, і користувач бачив лише «лаунчер зник». Вікно використовує
// віртуальну сторінку fatal-error.html (assetfs.go) і не залежить від
// фронтенду.
//
// component/detail передаються через query-параметри URL (сама сторінка —
// статичний HTML/JS, без текстової підстановки на боці Go) — раніше
// fatal-error.html мала захардкоджений текст "backend.dll"/"setup.exe",
// тому на Linux (де компонент — backend.so, а setup.exe взагалі не існує)
// повідомлення вводило користувача в оману щодо реальної причини й кроків
// відновлення.
func showFatalError(component, detail string) {
	q := url.Values{}
	q.Set("component", component)
	q.Set("detail", detail)
	q.Set("installer", installerName())
	opts := winOptions{
		Title:         "Shaurma Launcher — помилка",
		Width:         520,
		Height:        300,
		Frameless:     false,
		Background:    "solid",
		BackgroundHex: "#14181d",
		URL:           "fatal-error.html?" + q.Encode(),
	}
	b, _ := json.Marshal(opts)
	windowCreate("fatal-error", string(b))
	windowOp("fatal-error", "show")
	println("fatal:", component, detail)
}

// transitionToBackend — колбек phase_complete: апдейтер завершив роботу,
// переходимо на backend.dll. Виконується на UI-потоці (InvokeAsync).
//
// ВАЖЛИВО (виправлення «зникання» лаунчера): DLL БІЛЬШЕ НЕ ВИВАНТАЖУЄМО
// (FreeLibrary). Це Go c-shared бібліотеки — Go Runtime НЕ підтримує
// вивантаження таких DLL: її фонові потоки (sysmon, GC, горутина, що щойно
// повернулась з bridgePhaseComplete) можуть усе ще виконуватись у момент
// FreeLibrary, і ОС розмапує код бібліотеки просто під ними — процес падає
// саме в момент переходу updater→backend, тобто «після перевірки оновлень
// лаунчер зникає, а головне вікно не з'являється». Тому модулі лишаються
// завантаженими на весь час життя процесу (пам'ять невелика: ~25-30 МБ
// кожен), а перемикання робиться ЛИШЕ підміною вказівника в мості
// (bridgeSvc.swap) + graceful shutdown попереднього модуля.
//
// Ідемпотентність: повторний phase_complete (гонка «кнопка Закрити» ↔
// природне завершення runUpdate) не повинен переініціалізовувати backend
// вдруге (backend_init створив би НОВИЙ App і повторив ServiceStartup —
// rpc.Start, restoreRunningGame тощо).
func transitionToBackend() {
	if backendModule != nil && current == backendModule {
		return // уже на backend — ігноруємо дубль phase_complete
	}

	// SELF-UPDATE shell: апдейтер щойно міг поставити нову версію нашого ж
	// exe поруч як <name>.new (install.Apply — IsLockedComponent). Замість
	// того, щоб чекати наступного запуску, застосовуємо swap ЗАРАЗ:
	// активний exe -> ~ (rename запущеного exe Windows дозволяє), .new на
	// його місце, і перезапускаємо самі себе — новий процес уже нова версія
	// (як і просив користувач: «при завершенні оновлення закрити все і
	// перезапустити лаунчер, і це вже буде новий exe»).
	if selfApplied, selfPath := applyShellSelfUpdate(); selfApplied {
		if err := relaunchExe(selfPath); err == nil {
			// Знімаємо updater з мосту ДО виходу (як у звичайному переході
			// нижче) — щоб жоден Invoke/backendDispatch у проміжку до
			// фактичного завершення процесу не потрапив у модуль, що вже
			// відпрацював phase_complete.
			bridgeSvc.swap(nil)
			// Коректний вихід: OnShutdown (shutdownShell) зупинить активний
			// модуль (updater), процес завершиться, а новий процес вже
			// стартував з НОВОГО файлу на оригінальному шляху.
			if app := application.Get(); app != nil {
				app.Quit()
			} else {
				os.Exit(0)
			}
			return
		}
		// Рестарт не вдався (рідко): swap уже виконано — новий файл лежить
		// на оригінальному шляху, тож підхопиться наступного запуску.
		// Продовжуємо в цьому (старому, з «~») процесі, лаунчер працює.
		println("shell self-update: рестарт після оновлення не вдався, продовжую в цьому процесі")
	}

	// РУЧНЕ оновлення (кнопка «Оновити» в налаштуваннях): backend.dll була
	// завантажена ДО циклу оновлення (головне вікно працювало), і
	// вивантажити її неможливо — Go c-shared DLL не підтримує FreeLibrary
	// (crash-ризик, див. коментар вище). Тож навіть якщо апдейтер замінив
	// backend.dll на диску, loadBackendAndShowMain перевикористав би СТАРИЙ
	// модуль — користувач бачив би стару версію («після оновлення
	// відкривається стара версія, поки не перезапустиш лаунчер вручну»).
	// Якщо локальний маніфест змінився за час циклу (оновлення реально
	// встановилось) — перезапускаємо процес: новий старт застосує .new
	// (applyPendingUpdates) і завантажить свіжу backend.dll з диска.
	if backendModule != nil && localManifestHash() != preUpdateManifestHash {
		if err := relaunchSelf(); err == nil {
			// Знімаємо updater з мосту ДО виходу — щоб жоден Invoke у
			// проміжку до фактичного завершення процесу не потрапив у
			// модуль, що вже відпрацював phase_complete.
			bridgeSvc.swap(nil)
			if app := application.Get(); app != nil {
				app.Quit()
			} else {
				os.Exit(0)
			}
			return
		}
		// Рестарт не вдався (рідко): лишаємось на старому модулі, нову
		// версію підхопить наступний запуск.
		println("оновлення: рестарт для застосування нової версії не вдався, продовжую в цьому процесі")
	}

	if updaterModule != nil {
		// Спершу знімаємо старий модуль з мосту (Invoke/backendDispatch у
		// проміжку отримають безпечну відповідь «модуль не завантажено»),
		// потім graceful shutdown. Без FreeLibrary — див. коментар вище.
		bridgeSvc.swap(nil)
		updaterModule.shutdownModule()
	}
	loadBackendAndShowMain()
}

// transitionToUpdater — колбек run_updater: backend просить оновитись,
// переходимо на updater.dll. Виконується на UI-потоці (InvokeAsync).
func transitionToUpdater() {
	if updaterModule != nil && current == updaterModule {
		return // уже на updater — ігноруємо дубль run_updater
	}
	// РУЧНЕ оновлення (кнопка «Оновити» в налаштуваннях): апдейтер має
	// встановити нову версію безумовно, навіть якщо автооновлення
	// вимкнене — finalize=true у конфігу updater.dll.
	setManualUpdaterFinalize(true)
	defer setManualUpdaterFinalize(false)
	if backendModule != nil {
		bridgeSvc.swap(nil)
		backendModule.shutdownModule()
	}
	// БАГ (виправлено): головне вікно "main" тут ніколи не приховувалось —
	// loadUpdaterAndShowSplash() лише СТВОРЮЄ/ПОКАЗУЄ splash поверх нього,
	// тому при ручному оновленні (кнопка в налаштуваннях) користувач бачив,
	// як лаунчер "не закривається": за новим splash-вікном далі висіло
	// старе головне вікно, і жодного видимого переходу в апдейтер не було.
	// Симетрично до того, як loadBackendAndShowMain() ховає splash при
	// зворотному переході (updater -> backend).
	if getWindow("main") != nil {
		// Це ПРОГРАМНЕ приховування на час оновлення, а не «користувач згорнув
		// у трей»: windowOp("hide") ставить маркер window_hidden.flag, і
		// relaunchArgs після оновлення додавав би --background — лаунчер
		// «йшов у трей». Тому якщо вікно було видиме — маркер лишаємо знятим.
		wasHidden := windowHiddenMarkerSet()
		windowOp("main", "hide")
		if !wasHidden {
			setWindowHiddenMarker(false)
		}
	}
	// Фіксуємо стан маніфесту ДО запуску апдейтера: якщо після
	// phase_complete він змінився — оновлення реально встановилось, і
	// процес треба перезапустити, щоб підхопити нову backend.dll
	// (стара досі завантажена в пам'ять, див. transitionToBackend).
	preUpdateManifestHash = localManifestHash()
	loadUpdaterAndShowSplash()
}
