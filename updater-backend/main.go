// updater.dll — апдейтер як C ABI-модуль (той самий контракт, що backend.dll).
//
// Раніше апдейтер був окремим Wails-застосунком (shrm-updater.exe), який
// сам запускав launcher.exe окремим процесом і вигасав. Тепер:
//   - updater.dll завантажує той самий shell.exe через LoadLibrary;
//   - роботу він виконує в межах процесу shell'а (вікно "updater-splash"
//     створює shell за generic window_create, UI-події йдуть через bridgeEmit);
//   - коли апдейтер завершує перевірку/встановлення — він кличе
//     bridgePhaseComplete(), і shell ВИВАНТАЖУЄ updater.dll, завантажує
//     backend.dll і показує основний UI. Жодного запуску окремих exe.
//
// Self-update updater.dll: нова версія качається як updater.dll.new
// (install.Apply ставить завантажені DLL/exe під .new — Windows тримає лок
// на завантажену DLL), застосовується при НАСТУПНОМУ старті shell'а.
package main

/*
#include <stdlib.h>
#include "bridge.h"
*/
import "C"

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"sync"
	"time"
	"unsafe"

	"shrm-updater-backend/internal/bridgeinvoke"
	"shrm-updater-backend/internal/core"
	"shrm-updater-backend/internal/download"
	"shrm-updater-backend/internal/manifest"
	"shrm-updater-backend/internal/prefs"
)

// App — тонкий шар над internal/core: прокидує прогрес у splash через
// bridgeEmit і на завершенні кличе bridgePhaseComplete (замість того, щоб
// запускати launcher.exe і виходити, як старий окремий exe).
type App struct {
	installDir string
	channel    core.Channel
	// finalize — true, коли updater запущений shell'ом за запитом backend
	// (кнопка «Оновити» / автооновлення): backend уже прийняв рішення
	// оновлюватись, тож апдейтер ПЕРЕВІРЯЄ І СТАВИТЬ оновлення безумовно.
	// false (звичайний старт застосунку) — апдейтер сам читає
	// settings.AutoUpdate з БД лаунчера.
	finalize bool
	lang     string

	ctx    context.Context
	cancel context.CancelFunc
}

var (
	appMu       sync.Mutex
	appInstance *App
	appValue    reflect.Value
)

// ---- C ABI (bridge.h: ті самі 4 символи, що й у backend.dll) -------------

//export backend_init
func backend_init(configJSON *C.char, callbacks unsafe.Pointer) C.int {
	appMu.Lock()
	defer appMu.Unlock()

	setBridgeCallbacks(callbacks)

	var cfg struct {
		InstallDir string `json:"installDir"`
		Lang       string `json:"lang"`
		Channel    string `json:"channel"`
		Finalize   bool   `json:"finalize"`
	}
	_ = json.Unmarshal([]byte(C.GoString(configJSON)), &cfg)

	channel := core.ChannelShaurma
	if cfg.Channel == "base" {
		channel = core.ChannelBase
	}
	lang := cfg.Lang
	if lang != "en" {
		lang = "uk"
	}

	app := &App{
		installDir: cfg.InstallDir,
		channel:    channel,
		finalize:   cfg.Finalize,
		lang:       lang,
	}
	appInstance = app
	appValue = reflect.ValueOf(app)

	go app.runUpdate()
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
	appMu.Lock()
	defer appMu.Unlock()
	if appInstance != nil && appInstance.cancel != nil {
		appInstance.cancel()
	}
}

//export backend_free
func backend_free(s *C.char) {
	C.free(unsafe.Pointer(s))
}

// invokeResult — формат відповіді backend_invoke (той самий контракт, що
// у backend.dll). Result БЕЗ omitempty: порожнє значення ("", nil, [])
// має приходити у фронт як "result":null, а не зникати з JSON (інакше
// фронт отримає undefined і впаде на .map/.trim).
type invokeResult struct {
	OK     bool   `json:"ok"`
	Result any    `json:"result"`
	Error  string `json:"error,omitempty"`
}

// dispatch — простий reflect-диспетчер по методах App (Retry, CloseUpdater,
// GetPrefs, GetLanguage) + внутрішній __dispatch (канал подій shell->dll).
func dispatch(method, argsJSON string) invokeResult {
	if appInstance == nil || appValue.Kind() == reflect.Invalid {
		return invokeResult{OK: false, Error: "updater не ініціалізований"}
	}

	// Канал подій shell->updater: splash-вікно закривається без питань
	// (апдейтер продовжує роботу у фоні, результат — phase_complete).
	if method == "__dispatch" {
		var args []string
		if err := json.Unmarshal([]byte(argsJSON), &args); err == nil && len(args) >= 1 {
			switch {
			case len(args) >= 1 && len(args[0]) > len("window:closing:") &&
				args[0][:len("window:closing:")] == "window:closing:":
				return invokeResult{OK: true, Result: `{"action":"close"}`}
			}
		}
		return invokeResult{OK: true, Result: ""}
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
		return invokeResult{OK: false, Error: fmt.Sprintf("%s: очікує %d аргументів, отримано %d", method, mt.NumIn(), len(rawArgs))}
	}
	callArgs := make([]reflect.Value, mt.NumIn())
	for i := 0; i < mt.NumIn(); i++ {
		paramPtr := reflect.New(mt.In(i))
		if err := json.Unmarshal(rawArgs[i], paramPtr.Interface()); err != nil {
			return invokeResult{OK: false, Error: fmt.Sprintf("%s: аргумент %d: %v", method, i, err)}
		}
		callArgs[i] = paramPtr.Elem()
	}
	out := m.Call(callArgs)

	// Розпакування винесено в bridgeinvoke.ExtractResults: error-значення
	// розпізнаються за reflect-типом, а не через type assertion — для
	// nil-error v.Interface().(error) провалюється, і старий код затирав
	// справжній результат (рядок/слайс/булеан) значенням nil.
	result, errVal := bridgeinvoke.ExtractResults(out)
	if errVal != nil {
		return invokeResult{OK: false, Error: errVal.Error()}
	}
	return invokeResult{OK: true, Result: result}
}

// ---- App-методи (викликаються фронтендом через Bridge.Invoke) -----------

// GetPrefs — налаштування персоналізації лаунчера для splash (мова,
// акцент, тема) з settings.json, щоб апдейтер виглядав в унісон.
//
// ВАЖЛИВО ПРО КАСТОМНИЙ АКЦЕНТ: у лаунчері користувач може обрати
// accent="custom" і задати ВЛАСНИЙ hex-колір (accentCustom, пікер у
// «Вигляд»). Апдейтер має показати саме той колір, а не дефолтний
// помаранч — тому сюди повертається і accentCustom: фронтенд splash
// використовує його, коли accent == "custom".
func (a *App) GetPrefs() map[string]string {
	return map[string]string{
		"lang":         a.GetLanguage(),
		"accent":       readPref(a.installDir, "accent", "orange"),
		"accentCustom": readPref(a.installDir, "accentCustom", "#ff8a00"),
		"theme":        readPref(a.installDir, "theme", "dark"),
	}
}

// GetLanguage — мова інтерфейсу (з БД лаунчера, дефолт "uk").
func (a *App) GetLanguage() string {
	if a.lang != "" {
		return a.lang
	}
	return detectLanguage(a.installDir)
}

// Retry — кнопка «Повторити» у splash-вікні: перезапускає весь runUpdate.
func (a *App) Retry() {
	go a.runUpdate()
}

// CloseUpdater — кнопка «Закрити» у splash-вікні при помилці: переходимо
// до backend.dll як є (shell покаже основний UI; якщо лаунчер не
// встановлений — splash-помилка покаже шлях).
func (a *App) CloseUpdater() {
	bridgePhaseComplete()
}

func (a *App) emit(name string, data any) {
	b, err := json.Marshal(data)
	if err != nil {
		bridgeEmit(name, "null")
		return
	}
	bridgeEmit(name, string(b))
}

// runUpdate — головний потік апдейтера. На УСПІШНОМУ завершенні (з
// оновленням чи без) кличе bridgePhaseComplete() — shell вивантажує цю DLL
// і переходить до backend.dll. При ПОМИЛЦІ — лишаємо splash з карткою
// помилки (кнопки Retry/Close), процес продовжує жити.
func (a *App) runUpdate() {
	ctx, cancel := context.WithCancel(context.Background())
	a.ctx = ctx
	a.cancel = cancel
	defer cancel()

	if a.lang == "" {
		a.lang = detectLanguage(a.installDir)
	}

	cb := core.Callbacks{
		Lang: a.lang,
		OnStatus: func(s core.Status, msg string) {
			a.emit("updater:status", map[string]any{
				"status":  statusToString(s),
				"message": msg,
			})
		},
		OnProgress: func(p download.Progress) {
			a.emit("updater:progress", map[string]any{
				"fileIndex":  p.FileIndex,
				"fileTotal":  p.FileTotal,
				"fileName":   p.FileName,
				"bytesDone":  p.BytesDone,
				"bytesTotal": p.BytesTotal,
				// Спільний прогрес по ВСІХ файлах (для одного прогресс-бара):
				// назви компонентів фронтенд не показує — лише загальний %.
				"totalBytesDone":  p.TotalBytesDone,
				"totalBytesTotal": p.TotalBytesTotal,
			})
		},
		OnUpdateAvailable: func(version string, bytesTotal int64, fileCount int) {
			a.emit("updater:update", map[string]any{
				"version":    version,
				"bytesTotal": bytesTotal,
				"fileCount":  fileCount,
			})
		},
	}

	// Поточна версія для напису "vX.Y.Z" (ядро теж читає маніфест —
	// невелике дублювання свідоме, щоб не ускладнювати сигнатуру core.Run).
	if local, err := manifest.Load(filepath.Join(a.installDir, "launcher-version.json")); err == nil {
		a.emit("updater:version", map[string]any{"current": local.Version})
	}

	p := prefs.Load(a.installDir)
	installUpdates := a.finalize || p.AutoUpdate

	if err := core.Run(ctx, a.installDir, a.channel, p.UpdateChannel, installUpdates, cb); err != nil {
		a.emit("updater:status", map[string]any{
			"status":  "error",
			"message": err.Error(),
		})
		// Не виходимо автоматично — користувач бачить картку помилки з
		// кнопками «Повторити»/«Закрити» і сам вирішує.
		return
	}

	// Успіх: переходимо до backend.dll (phase_complete). Невелика затримка,
	// щоб останній статус встиг відрендеритись на splash.
	time.Sleep(250 * time.Millisecond)
	bridgePhaseComplete()
}

func statusToString(s core.Status) string {
	switch s {
	case core.StatusChecking:
		return "checking"
	case core.StatusUpToDate:
		return "up_to_date"
	case core.StatusUpdateAvailable:
		return "update_available"
	case core.StatusDownloading:
		return "downloading"
	case core.StatusInstalling:
		return "installing"
	case core.StatusLaunching:
		return "launching"
	default:
		return "error"
	}
}

// ---- prefs-читання (аналог старого updater/main.go) ----------------------

func readPref(installDir, key, def string) string {
	raw, ok := prefs.Raw(installDir)
	if !ok {
		return def
	}
	var s map[string]any
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return def
	}
	if v, ok := s[key].(string); ok && v != "" {
		return v
	}
	return def
}

func detectLanguage(installDir string) string {
	raw, ok := prefs.Raw(installDir)
	if !ok {
		return "uk"
	}
	var s struct {
		Language string `json:"language"`
	}
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return "uk"
	}
	if s.Language == "en" || s.Language == "uk" {
		return s.Language
	}
	return "uk"
}

func main() {}
