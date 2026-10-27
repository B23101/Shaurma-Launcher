//go:build linux

package main

// bridge_host_linux.go — завантаження backend.so/updater.so і виклики
// чотирьох експортованих функцій (bridge.h) через cgo dlopen/dlsym.
//
// На Windows той самий контракт реалізовано чистим syscall
// (LoadLibrary/GetProcAddress + syscall.NewCallback-трамполіни, без cgo).
// На Linux немає еквівалента syscall.NewCallback — Go-функція, яку
// викличе C-код (backend.so через колбек), повинна бути експортована
// через "//export" і мати відповідний C ABI. Тому тут два cgo-файли:
//   - цей файл: dlopen(backend.so) + виклик 4 функцій контракту
//   - bridge_callbacks_linux.go: 16 "//export"-функцій (DLL -> shell),
//     що заповнюють BridgeCallbacks
//
// Механіка вже доведена робочим прототипом
// (shell-bridge-prototype/shell/host.c, живий запуск на Linux) — тут
// та сама логіка перенесена з C у Go-файл під cgo.

/*
#cgo LDFLAGS: -ldl
#include <dlfcn.h>
#include <stdlib.h>
#include "bridge.h"

// Прототипи //export-функцій з bridge_callbacks_linux.go — потрібні тут
// окремо, бо cgo компілює C-преамбулу КОЖНОГО файлу ізольовано: extern-
// декларації з преамбули bridge_callbacks_linux.go не видимі звідси,
// хоча символи (лінкер) ті самі. Без цього повторення initCallbacks()
// нижче не зміг би узяти адресу C.cgoEmit і т.д. — "could not determine
// what C.cgoXxx refers to". Сигнатури мають збігатись байт-в-байт з
// оригінальними extern-рядками в bridge_callbacks_linux.go — БЕЗ const
// (cgo генерує "char*", не "const char*", для *C.char-параметрів
// "//export"-функцій; перевірено реальною компіляцією GOOS=linux).
extern void cgoEmit(char* event, char* data_json);
extern void cgoWindowCreate(char* id, char* options_json);
extern void cgoWindowOp(char* id, char* op);
extern void cgoWindowSetSize(char* id, int w, int h);
extern void cgoWindowSetMinSize(char* id, int w, int h);
extern char* cgoWindowGetSize(char* id);
extern int cgoWindowIsVisible(char* id);
extern void cgoTraySet(char* icon_png_base64, char* tooltip, char* menu_json);
extern void cgoTrayDestroy(void);
extern char* cgoDialogOpenFile(char* options_json);
extern char* cgoDialogOpenFiles(char* options_json);
extern char* cgoDialogSaveFile(char* options_json);
extern void cgoBrowserOpenURL(char* url);
extern void cgoAppQuit(void);
extern void cgoFreeString(char* s);
extern void cgoPhaseComplete(void);
extern void cgoRunUpdater(void);

// Типізовані обгортки над dlsym-адресами — cgo не вміє викликати
// void* напряму як функцію, тому кожен символ приводимо до потрібного
// типу функції з bridge.h перед викликом.
static int call_backend_init(void *fn, const char *cfg, const BridgeCallbacks *cbs) {
    backend_init_fn f = (backend_init_fn)fn;
    return f(cfg, cbs);
}
static char* call_backend_invoke(void *fn, const char *method, const char *args) {
    backend_invoke_fn f = (backend_invoke_fn)fn;
    return f(method, args);
}
static void call_backend_shutdown(void *fn) {
    backend_shutdown_fn f = (backend_shutdown_fn)fn;
    f();
}
static void call_backend_free(void *fn, char *s) {
    backend_free_fn f = (backend_free_fn)fn;
    f(s);
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// dllModule — завантажений .so-модуль з адресами чотирьох функцій контракту.
// Назва типу й набір методів (initModule/invokeModule/shutdownModule)
// навмисно ідентичні bridge_host_windows.go — bridge_service.go і
// updater_phase.go використовують *dllModule без урахування платформи.
type dllModule struct {
	handle   unsafe.Pointer // dlopen() handle
	init     unsafe.Pointer // backend_init
	invoke   unsafe.Pointer // backend_invoke
	shutdown unsafe.Pointer // backend_shutdown
	free     unsafe.Pointer // backend_free
}

// loadModule відкриває backend.so/updater.so (dlopen) і резолвить чотири
// обов'язкові символи (dlsym). RTLD_NOW — усі символи модуля резолвляться
// одразу при завантаженні (не лениво), щоб зіпсований .so впав тут, а не
// посеред роботи лаунчера.
//
// ВАЖЛИВО (дзеркало Windows-коментаря в updater_phase.go): dlclose()
// нижче викликається ЛИШЕ у шляху помилки резолву символів — модуль ще
// не встиг ініціалізуватись (backend_init не викликаний), тож вивантажити
// його безпечно. Після успішного loadModule дана бібліотека НІКОЛИ не
// вивантажується через dlclose протягом життя процесу — той самий
// crash-ризик, що й з FreeLibrary на Windows (Go runtime не підтримує
// вивантаження c-shared бібліотек, код лишається замаплений під активними
// горутинами). shutdownModule() виконує лише graceful backend_shutdown.
func loadModule(path string) (*dllModule, error) {
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))

	const rtldNow = 2
	h := C.dlopen(cPath, rtldNow)
	if h == nil {
		return nil, fmt.Errorf("dlopen(%s): %s", path, C.GoString(C.dlerror()))
	}

	m := &dllModule{handle: h}
	syms := map[string]*unsafe.Pointer{
		"backend_init":     &m.init,
		"backend_invoke":   &m.invoke,
		"backend_shutdown": &m.shutdown,
		"backend_free":     &m.free,
	}
	for name, dst := range syms {
		cName := C.CString(name)
		p := C.dlsym(h, cName)
		C.free(unsafe.Pointer(cName))
		if p == nil {
			C.dlclose(h)
			return nil, fmt.Errorf("dlsym(%s): %s", name, C.GoString(C.dlerror()))
		}
		*dst = p
	}
	return m, nil
}

// initModule кличе backend_init(configJSON, &callbacks). Повертає 0 при успіху.
//
// ВАЖЛИВО (фікс "cgo argument has Go pointer to unpinned Go pointer"):
// cbs — це *bridgeCallbacksC, вказівник на Go-структуру. Раніше її поля
// мали тип unsafe.Pointer (адреси C-функцій-колбеків), і саме структура
// з такими полями передавалась у C.call_backend_init як
// (*C.BridgeCallbacks)(unsafe.Pointer(cbs)). Для cgo checker'а (cgocheck,
// увімкнений за замовчуванням у не-release збірках) це виглядає як
// "Go-вказівник (cbs), що вказує на Go-пам'ять, яка сама містить Go-
// вказівники (unsafe.Pointer-поля), і жоден з них не запінений через
// runtime.Pinner" — звідси паніка одразу при першому виклику
// initModule (старт лаунчера).
//
// Замість пінінгу (що ускладнило б код і мусило б жити рівно стільки,
// скільки й bridgeCB) переносимо С-структуру BridgeCallbacks у ЧИСТУ
// С-пам'ять (malloc), заповнену РІВНО ОДИН РАЗ у initCallbacks(). Сюди
// (initModule) відтепер передається лише вказівник на цю C-пам'ять —
// жодного Go-вказівника всередині переданого блоку немає, тож
// cgocheck не спрацьовує. Це дзеркалить bridge_host_windows.go, де
// поля bridgeCallbacksC — uintptr (не Go-вказівник з точки зору
// рантайму), а не unsafe.Pointer.
func (m *dllModule) initModule(configJSON string, cbs *bridgeCallbacksC) int {
	cCfg := C.CString(configJSON)
	defer C.free(unsafe.Pointer(cCfg))
	r := C.call_backend_init(m.init, cCfg, (*C.BridgeCallbacks)(bridgeCBNative))
	return int(r)
}

// invokeModule кличе backend_invoke(method, argsJSON) і повертає рядок-результат,
// звільняючи пам'ять модуля через backend_free. Порожній рядок — якщо модуль
// повернув NULL або не завантажений.
func (m *dllModule) invokeModule(method, argsJSON string) string {
	if m == nil || m.invoke == nil {
		return `{"ok":false,"error":"модуль не завантажено"}`
	}
	cMethod := C.CString(method)
	cArgs := C.CString(argsJSON)
	defer C.free(unsafe.Pointer(cMethod))
	defer C.free(unsafe.Pointer(cArgs))

	r := C.call_backend_invoke(m.invoke, cMethod, cArgs)
	if r == nil {
		return `{"ok":false,"error":"backend_invoke повернув NULL"}`
	}
	s := C.GoString(r)
	if m.free != nil {
		C.call_backend_free(m.free, r)
	}
	return s
}

// shutdownModule кличе backend_shutdown перед вивантаженням .so.
func (m *dllModule) shutdownModule() {
	if m != nil && m.shutdown != nil {
		C.call_backend_shutdown(m.shutdown)
	}
}

// allocCString виділяє C-пам'ять (malloc) під NUL-завершений рядок для
// значень, які модуль звільняє через колбек free_string (bridge.h).
// Аналог windows.VirtualAlloc з bridge_host_windows.go, тільки malloc —
// той самий аллокатор, який backend.so очікує через backend_free-подібний
// виклик free_string.
func allocCString(s string) uintptr {
	p := C.CString(s) // malloc-копія з NUL-термінацією
	return uintptr(unsafe.Pointer(p))
}

// freeCString звільняє пам'ять, виділену allocCString (free).
func freeCString(p uintptr) {
	if p != 0 {
		C.free(unsafe.Pointer(p))
	}
}

// ---- bridgeCallbacksC: дзеркало BridgeCallbacks (bridge.h v2) ------------
//
// На Linux структура заповнюється РЕАЛЬНИМИ адресами C-функцій
// (bridge_callbacks_linux.go, кожна — "//export"-обгортка над Go-логікою
// з callbacks.go), а не трамполінами syscall.NewCallback (яких на цій
// платформі не існує). Layout — той самий C ABI (bridge.h), тому назва
// типу і порядок полів ідентичні bridge_host_windows.go, лише тип поля —
// unsafe.Pointer (C function pointer) замість uintptr(trampoline).
type bridgeCallbacksC struct {
	structVersion    uint32
	_pad             uint32
	emit             unsafe.Pointer
	windowCreate     unsafe.Pointer
	windowOp         unsafe.Pointer
	windowSetSize    unsafe.Pointer
	windowGetSize    unsafe.Pointer
	windowIsVisible  unsafe.Pointer
	traySet          unsafe.Pointer
	trayDestroy      unsafe.Pointer
	dialogOpenFile   unsafe.Pointer
	browserOpenURL   unsafe.Pointer
	appQuit          unsafe.Pointer
	freeString       unsafe.Pointer
	dialogOpenFiles  unsafe.Pointer // v2
	dialogSaveFile   unsafe.Pointer // v2
	windowSetMinSize unsafe.Pointer // v2
	phaseComplete    unsafe.Pointer // v2
	runUpdater       unsafe.Pointer // v2
}

// bridgeCB лишається лише для сумісності сигнатур з bridge_service.go /
// updater_phase.go (обидві платформи приймають *bridgeCallbacksC) — на
// Linux вона більше НЕ передається в cgo напряму (див. initModule).
// Поля не заповнюються і не читаються.
var bridgeCB bridgeCallbacksC

// bridgeCBNative — вказівник на BridgeCallbacks, виділену в C-купі
// (malloc), а не в Go-пам'яті. Заповнюється один раз у initCallbacks і
// живе до кінця процесу (як і сам bridgeCB на Windows), тому окремого
// free для неї не передбачено.
var bridgeCBNative unsafe.Pointer

// initCallbacks виділяє C-пам'ять під BridgeCallbacks і заповнює її
// адресами C-обгорток над //export Go-функціями
// (bridge_callbacks_linux.go). Викликається один раз у main, як і на
// Windows.
//
// ЧОМУ malloc, а не Go-структура: див. розлогий коментар над
// initModule. Коротко — cgocheck забороняє передавати в C Go-вказівник,
// що вказує на пам'ять із НЕЗАПІНЕНИМИ Go-вказівниками всередині; поля
// цієї структури (адреси C-функцій-колбеків) технічно є Go-значеннями
// типу unsafe.Pointer, тому Go-структура з такими полями під цю
// заборону підпадає. C-пам'ять (malloc) Go-рантайму не належить, тож
// cgocheck її не інспектує.
func initCallbacks() {
	p := C.malloc(C.size_t(unsafe.Sizeof(C.BridgeCallbacks{})))
	cb := (*C.BridgeCallbacks)(p)

	// C.cgoXxx (без викличних дужок) — це значення типу "вказівник на
	// C-функцію" (cgo генерує для кожного extern-символу тип *[0]byte).
	// Присвоєння напряму в поля *C.BridgeCallbacks (замість проміжної
	// Go-структури bridgeCallbacksC) — ключова відмінність від
	// попередньої версії.
	// unsafe.Pointer як проміжний каст (а не C.bridge_emit_fn(C.cgoEmit)
	// напряму) — надійніший шлях між різними версіями cgo для
	// присвоєння "вказівник на функцію" в поле структури; сама структура
	// (cb) — валідна C-пам'ять, тому запис у неї Go unsafe.Pointer-значень
	// безпечний (це не Go-пам'ять, яку інспектує cgocheck).
	cb.struct_version = 2
	*(*unsafe.Pointer)(unsafe.Pointer(&cb.emit)) = unsafe.Pointer(C.cgoEmit)
	*(*unsafe.Pointer)(unsafe.Pointer(&cb.window_create)) = unsafe.Pointer(C.cgoWindowCreate)
	*(*unsafe.Pointer)(unsafe.Pointer(&cb.window_op)) = unsafe.Pointer(C.cgoWindowOp)
	*(*unsafe.Pointer)(unsafe.Pointer(&cb.window_set_size)) = unsafe.Pointer(C.cgoWindowSetSize)
	*(*unsafe.Pointer)(unsafe.Pointer(&cb.window_get_size)) = unsafe.Pointer(C.cgoWindowGetSize)
	*(*unsafe.Pointer)(unsafe.Pointer(&cb.window_is_visible)) = unsafe.Pointer(C.cgoWindowIsVisible)
	*(*unsafe.Pointer)(unsafe.Pointer(&cb.tray_set)) = unsafe.Pointer(C.cgoTraySet)
	*(*unsafe.Pointer)(unsafe.Pointer(&cb.tray_destroy)) = unsafe.Pointer(C.cgoTrayDestroy)
	*(*unsafe.Pointer)(unsafe.Pointer(&cb.dialog_open_file)) = unsafe.Pointer(C.cgoDialogOpenFile)
	*(*unsafe.Pointer)(unsafe.Pointer(&cb.browser_open_url)) = unsafe.Pointer(C.cgoBrowserOpenURL)
	*(*unsafe.Pointer)(unsafe.Pointer(&cb.app_quit)) = unsafe.Pointer(C.cgoAppQuit)
	*(*unsafe.Pointer)(unsafe.Pointer(&cb.free_string)) = unsafe.Pointer(C.cgoFreeString)
	*(*unsafe.Pointer)(unsafe.Pointer(&cb.dialog_open_files)) = unsafe.Pointer(C.cgoDialogOpenFiles)
	*(*unsafe.Pointer)(unsafe.Pointer(&cb.dialog_save_file)) = unsafe.Pointer(C.cgoDialogSaveFile)
	*(*unsafe.Pointer)(unsafe.Pointer(&cb.window_set_min_size)) = unsafe.Pointer(C.cgoWindowSetMinSize)
	*(*unsafe.Pointer)(unsafe.Pointer(&cb.phase_complete)) = unsafe.Pointer(C.cgoPhaseComplete)
	*(*unsafe.Pointer)(unsafe.Pointer(&cb.run_updater)) = unsafe.Pointer(C.cgoRunUpdater)

	bridgeCBNative = p
}
