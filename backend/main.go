// backend.dll — весь бекенд лаунчера як C ABI-модуль.
//
// Контракт: bridge.h (v2). Експортує рівно 4 символи, які shell.exe
// шукає через GetProcAddress після LoadLibrary:
//
//	backend_init(config_json, callbacks) -> 0 при успіху
//	backend_invoke(method, args_json)     -> {"ok":true,"result":...}|{"ok":false,"error":...}
//	backend_shutdown()                     — graceful shutdown перед FreeLibrary
//	backend_free(s)                        — звільнити рядок, виділений DLL
//
// Диспетчер працює через reflect: App-структура реєструється один раз у
// backend_init, backend_invoke шукає метод по імені рядком і викликає його
// через reflect.Value.Call, розпаковуючи argsJson в аргументи методу і
// пакуючи результат назад у JSON. Той самий принцип, що Wails Binding —
// тільки диспетчеризація тут, всередині dll, а shell.exe ніколи не знає
// сигнатур методів (і не потребує перекомпіляції при появі нових).
package main

/*
#include <stdlib.h>
#include <string.h>
#include "bridge.h"
*/
import "C"

import (
	"encoding/json"
	"fmt"
	"log"
	"reflect"
	"runtime/debug"
	"strings"
	"sync"
	"unsafe"

	"shaurma-launcher-backend/internal/bridgeinvoke"
	"shaurma-launcher-backend/internal/crashguard"
)

var (
	// appInstance — єдиний екземпляр App (аналог NewApp() у launcher/main.go).
	appInstance *App
	// appValue — reflect.Value для диспетчера.
	appValue reflect.Value
	// initMu захищає backend_init від повторних викликів (shell може
	// перезавантажити dll після оновлення — тоді створюється НОВИЙ
	// екземпляр App, старий відкидається).
	initMu sync.Mutex
)

// initConfig — config_json, який shell передає в backend_init
// ({"installDir":..,"lang":"uk","channel":"shaurma",...}).
type initConfig struct {
	InstallDir string `json:"installDir"`
	Lang       string `json:"lang"`
	Channel    string `json:"channel"`
	// Background — shell запущено з --background (автозапуск «у фоні»):
	// головне вікно не показується, див. ui_ready.go.
	Background bool `json:"background"`
}

// backendInitConfig — збережений конфіг старту (потрібен пізніше,
// напр. GetInstallDir для запуску апдейтера).
var backendInitConfig initConfig

//export backend_init
func backend_init(configJSON *C.char, callbacks unsafe.Pointer) (result C.int) {
	initMu.Lock()
	defer initMu.Unlock()

	// ДІАГНОСТИЧНИЙ ЗАХИСТ: без цього recover() будь-яка паніка всередині
	// ServiceStartup() (чи в будь-якому виклику, що він робить — cfg.Load(),
	// SQLite-міграції, buildsReg.Load(), autostart.Set(), Discord RPC,
	// restoreRunningGame(), syncFolderSettings() тощо) пробиває межу cgo/C
	// ABI і миттєво вбиває ВЕСЬ процес shell.exe — без вікна, без Event Viewer
	// запису, без жодного сліду. Симптом саме такий: "лаунчер зникає одразу
	// після запуску, тиша". Тепер паніка ловиться тут, пишеться в лог і
	// повертається як звичайна помилка init (код 1) — shell.exe вже вміє
	// показати showFatalError з цим текстом (код повернення тепер
	// перевіряється на боці shell — див. shell/updater_phase.go).
	defer func() {
		if r := recover(); r != nil {
			stack := string(debug.Stack())
			msg := fmt.Sprintf("паніка в ServiceStartup: %v\n%s", r, stack)
			log.Print(msg)
			crashguard.Report("backend_init", r, []byte(stack))
			// НЕ emit "backend:ready" тут: emit → cbEmit → InvokeAsync → падає якщо app.Run() ще не працює.
			// Shell перевіряє code і показує showFatalError самостійно.
			result = 1
		}
	}()

	_ = json.Unmarshal([]byte(C.GoString(configJSON)), &backendInitConfig)
	if backendInitConfig.Background {
		markBackgroundStart()
	}

	// Зберігаємо вказівник на структуру колбеків — shell передає адресу
	// BridgeCallbacks, що живе в його пам'яті на весь час роботи процесу.
	setBridgeCallbacks(callbacks)

	// Створюємо App і виконуємо ServiceStartup (логіка, що раніше була
	// в Wails-сервісі launcher/main.go).
	appInstance = NewApp()
	appValue = reflect.ValueOf(appInstance)
	if err := appInstance.ServiceStartup(); err != nil {
		appInstance.emit("backend:ready", map[string]any{"ok": false, "error": err.Error()})
		return 1
	}
	appInstance.emit("backend:ready", map[string]any{"ok": true, "version": readLauncherVersion(backendInitConfig.InstallDir)})
	return 0
}

//export backend_invoke
func backend_invoke(method *C.char, argsJSON *C.char) *C.char {
	res := dispatch(C.GoString(method), C.GoString(argsJSON))
	out, err := json.Marshal(res)
	if err != nil {
		out, _ = json.Marshal(invokeResult{OK: false, Error: "внутрішня помилка серіалізації"})
	}
	return C.CString(string(out))
}

//export backend_shutdown
func backend_shutdown() {
	if appInstance != nil {
		appInstance.shutdown()
	}
}

//export backend_free
func backend_free(s *C.char) {
	C.free(unsafe.Pointer(s))
}

// invokeResult — формат відповіді backend_invoke (дзеркало bridge.h).
//
// ВАЖЛИВО (виправлення «фронт отримує undefined замість значення»): поле
// Result БЕЗ omitempty. Раніше порожнє значення ("", [], false, nil)
// зникало з JSON — фронт отримував {ok:true} БЕЗ result, тобто undefined
// замість ""/[]/null. Це ламало:
//   - StartMSLogin → фронт бачив undefined → OpenExternal(null) → Провідник C:;
//   - методи, що повертають порожні списки → undefined.map(...) падав;
//   - GetPackAsset при порожньому результаті → undefined замість "".
//
// Тепер result присутній завжди (nil → null) — фронт сам вирішує, як
// трактувати null/undefined (всюди є ?? [] / ?? null).
type invokeResult struct {
	OK     bool   `json:"ok"`
	Result any    `json:"result"`
	Error  string `json:"error,omitempty"`
}

// quoteJSON екранує рядок як JSON-літерал (для вбудовування в emit-дані).
func quoteJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// Діагностика «метод не знайдено» (запис кожного виклику у
// %TEMP%\shaurm-invoke.log) видалена: причина була на межі C ABI —
// shell передавав не-NUL-завершені рядки, а DLL читала їх через
// C.GoString зі сміттям з купи (див. shell/bridge_host.go: cstrPtr).

// dispatch виконує реальний виклик методу через reflect: розпаковує
// JSON-масив аргументів у типи параметрів методу, кличе метод, пакує
// результат(и) назад у JSON.
//
// РЕЗЕРВОВАНІ методи (тільки для shell, див. bridge.h):
//   - "__dispatch" — внутрішній канал подій shell->backend (закриття
//     вікон, стан вікна, кліки трею). Обробляється окремо, бо не є
//     експортованим методом App.
func dispatch(method string, argsJSON string) (res invokeResult) {
	// Паніка всередині БУДЬ-ЯКОГО методу App не повинна валити лаунчер.
	// dispatch викликається з C (backend_invoke), тож паніка на межі cgo
	// або вбиває весь процес, або (залежно від збірки) лишає виклик
	// фронтенду БЕЗ ВІДПОВІДІ — а це і є «сторінка вічно завантажується»
	// замість повідомлення про помилку. Ловимо її, пишемо в журнал
	// лаунчера зі стеком і віддаємо звичайну помилку фронту — так само,
	// як це робить Wails для паніки в прив'язаному методі.
	defer func() {
		if r := recover(); r != nil {
			stack := string(debug.Stack())
			res = invokeResult{OK: false, Error: fmt.Sprintf("%s: паніка: %v", method, r)}
			if appInstance != nil {
				appInstance.logErrorDetail("bridge."+method, fmt.Sprintf("паніка: %v", r), stack)
			}
			log.Printf("backend_invoke: паніка в %s: %v\n%s", method, r, stack)
			crashguard.Report("invoke."+method, r, []byte(stack))
		}
	}()

	if appValue.Kind() == reflect.Invalid || appInstance == nil {
		return invokeResult{OK: false, Error: "backend не ініціалізований (backend_init не викликано)"}
	}

	// Внутрішній канал подій shell->backend.
	if method == "__dispatch" {
		var args []string
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return invokeResult{OK: false, Error: "__dispatch: " + err.Error()}
		}
		if len(args) < 2 {
			return invokeResult{OK: false, Error: "__dispatch: очікує [event, dataJson]"}
		}
		action := appInstance.handleShellEvent(args[0], args[1])
		return invokeResult{OK: true, Result: action}
	}

	m := appValue.MethodByName(method)
	if !m.IsValid() {
		return invokeResult{OK: false, Error: fmt.Sprintf("метод не знайдено: %s", method)}
	}

	var rawArgs []json.RawMessage
	if argsJSON != "" {
		if err := json.Unmarshal([]byte(argsJSON), &rawArgs); err != nil {
			return invokeResult{OK: false, Error: fmt.Sprintf("некоректний argsJson: %v", err)}
		}
	}

	mt := m.Type()
	if mt.NumIn() != len(rawArgs) {
		return invokeResult{OK: false, Error: fmt.Sprintf(
			"%s: очікує %d аргумент(ів), отримано %d", method, mt.NumIn(), len(rawArgs))}
	}

	callArgs := make([]reflect.Value, mt.NumIn())
	for i := 0; i < mt.NumIn(); i++ {
		paramPtr := reflect.New(mt.In(i))
		if err := json.Unmarshal(rawArgs[i], paramPtr.Interface()); err != nil {
			return invokeResult{OK: false, Error: fmt.Sprintf(
				"%s: аргумент %d не вдалось розпакувати: %v", method, i, err)}
		}
		callArgs[i] = paramPtr.Elem()
	}

	out := m.Call(callArgs)

	// Конвенція повернених значень: (T) | (T, error) | (error) | ().
	// Розпакування винесено в internal/bridgeinvoke.ExtractResults (чистий
	// пакет без cgo, покритий регресійним тестом): error-значення
	// розпізнаються за reflect-типом, а не через type assertion
	// v.Interface().(error) — для nil-error assertion провалюється, і
	// старий код затирав справжній результат значенням nil (StartMSLogin
	// → null, GetPackAsset → null, GetCustomPackMCVersions → null, ...).
	result, errVal := bridgeinvoke.ExtractResults(out)

	if errVal != nil {
		return invokeResult{OK: false, Error: errVal.Error()}
	}
	// Список модів для UI худне на межі (mods_slim.go): без DependsOn/
	// Provides/Sha1/повного опису, яких рядок списку не показує.
	result = slimBridgeResult(method, result)
	return invokeResult{OK: true, Result: result}
}

// handleShellEvent — внутрішній канал подій shell->backend (метод
// "__dispatch" у bridge.h). Повертає action JSON для подій, що вимагають
// відповіді від backend (закриття вікна), інакше "".
func (a *App) handleShellEvent(event, dataJSON string) string {
	switch {
	case strings.HasPrefix(event, "window:closing:"):
		id := strings.TrimPrefix(event, "window:closing:")
		if id == "main" {
			// Головне вікно: рішення «сховати у трей / дозволити закриття»
			// з повною логікою (гра запущена, активні качки, підтвердження).
			return a.onMainWindowClosing()
		}
		return a.onShellWindowClosing(id)
	case event == "window:state":
		a.onShellWindowState(dataJSON)
		return ""
	case event == "window:reveal":
		// Повторний запуск лаунчера (shell: revealFromSecondInstance):
		// скидаємо фоновий старт, щоб UIReady не лишив вікно схованим.
		startBackground.Store(false)
		return ""
	case event == "window:active":
		// Активність головного вікна (WM_ACTIVATE від shell) — нативне
		// джерело правила «вікно неактивне → анімації на паузу».
		a.onWindowActive(dataJSON)
		return ""
	case strings.HasPrefix(event, "window:ready:"):
		return a.shellWindowReadyState(strings.TrimPrefix(event, "window:ready:"))
	case strings.HasPrefix(event, "tray:click:"):
		a.onTrayClick(strings.TrimPrefix(event, "tray:click:"))
		return ""
	case event == "shortcut:launch":
		// Лаунчер запущено з ярлика на робочому столі (shell передав
		// "--launch <packID> [--account <id>] [--server <host>]", див.
		// shell/main.go). Зберігаємо дані як pending (фронтенд підхопить
		// на маунті через TakePendingShortcutLaunch) І одразу емітимо
		// подію — на випадок, коли лаунчер уже запущений (другий клік по
		// ярлику), фронтенд підписаний на "shortcut:launch" і запустить
		// збірку негайно (з персональними опціями ярлика).
		//
		// Payload від shell: JSON {packId, accountId?, autoJoinServer?}.
		// Старі shell (без опцій) слали голий рядок packID — приймаємо
		// обидва формати.
		p := PendingShortcutLaunch{}
		if err := json.Unmarshal([]byte(dataJSON), &p); err != nil || p.PackID == "" {
			var pid string
			if err := json.Unmarshal([]byte(dataJSON), &pid); err == nil && pid != "" {
				p.PackID = pid
			}
		}
		if p.PackID != "" {
			setPendingShortcutLaunch(p)
			a.emit("shortcut:launch", p)
		}
		return ""
	case event == "packfile:open":
		// Лаунчер запущено з файлу збірки — .shaurmpack/.shaurmbundle через
		// Windows-асоціацію («Open with», двічі клік у Провіднику). Shell
		// передав шлях як аргумент (див. shell/main.go: parsePackFilePathArg /
		// dispatchPackFileOpenToBackend). Як і для shortcut:launch: зберігаємо
		// pending (фронтенд підхопить на маунті через
		// TakePendingPackImportFile) І одразу емітимо — на випадок, коли
		// лаунчер уже запущений (другий клік по файлу), фронтенд підписаний
		// на "packfile:open" і відкриє імпорт негайно.
		//
		// Payload від shell: JSON {path}. Приймаємо також голий рядок шляху
		// (сумісність).
		path := ""
		var pfo struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal([]byte(dataJSON), &pfo); err == nil && pfo.Path != "" {
			path = pfo.Path
		} else {
			var s string
			if err := json.Unmarshal([]byte(dataJSON), &s); err == nil && s != "" {
				path = s
			}
		}
		if path != "" {
			setPendingPackImportFile(path)
			a.emit("packfile:open", path)
		}
		return ""
	case event == "friend:open":
		// Лаунчер відкрито з браузера через shaurma://friend/@handle?n=Nick
		// (сторінка-посередник сайту /friend/…). Shell нормалізував його до
		// https-посилання лаунчера. Як і packfile:open: pending (фронтенд
		// забирає на маунті через TakePendingFriendLink) + одразу емітимо.
		var fo struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal([]byte(dataJSON), &fo); err == nil && fo.URL != "" {
			setPendingFriendLink(fo.URL)
			a.emit("friend:open", fo.URL)
			a.FocusMainWindow()
		}
		return ""
	default:
		return ""
	}
}

func main() {}
