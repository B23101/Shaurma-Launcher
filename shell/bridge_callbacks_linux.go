//go:build linux

package main

// bridge_callbacks_linux.go — 16 колбеків DLL -> shell (BridgeCallbacks у
// bridge.h), Linux-варіант. На Windows той самий контракт реалізовано
// через syscall.NewCallback-трамполіни (callbacks.go); на Linux
// еквівалента немає, тому кожен колбек — окрема "//export"-функція з
// точною C-сигнатурою з bridge.h, а адреса цієї C-функції (не Go-функції
// напряму — так cgo вимагає) підставляється в BridgeCallbacks у
// initCallbacks() (bridge_host_linux.go).
//
// ПРАВИЛО БЕЗПЕКИ ВІД ДЕДЛОКІВ — те саме, що на Windows: колбек,
// викликаний із UI-потоку (через backend_invoke усередині хука вікна),
// НЕ повинен блокувати UI-потік (InvokeSync). Тому всі «запустив і
// забув» команди виконуються через safeInvokeAsync; синхронними
// можуть бути ЛИШЕ window_get_size/window_is_visible/діалоги — вони
// викликаються тільки з потоків, що НЕ є UI-потоком.
//
// safeInvokeAsync/safeInvokeSync (main_thread_ready.go) — тонкі обгортки
// над application.InvokeAsync/InvokeSync з бар'єром готовності головного
// потоку: на Linux перший колбек із щойно ініціалізованого .so може
// прийти РАНІШЕ, ніж app.Run() встигне підняти внутрішній диспетчер —
// прямий виклик application.InvokeAsync у цьому вікні падає з SIGSEGV
// (див. розлогий коментар у main.go).

/*
#include <stdlib.h>
#include "bridge.h"

// Прототипи в C-просторі — потрібні, щоб узяти адресу //export-функції
// нижче (cgoEmit і т.д.) як значення типу bridge_emit_fn/... сумісне з
// полями BridgeCallbacks. cgo автоматично генерує C-обгортку з таким же
// іменем для кожної "//export" Go-функції; тут лише декларуємо сигнатуру
// так, щоб вона збігалась байт-в-байт з typedef у bridge.h.
//
// БЕЗ const: cgo завжди генерує _cgo_export.c з "char*" (не "const
// char*") для будь-якого параметра *C.char у Go-сигнатурі "//export"-
// функції — навіть якщо сама функція не змінює даних. "const" тут
// викликав би "conflicting types" між цим ручним прототипом і
// автогенерованим (перевірено реальною компіляцією GOOS=linux).
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
*/
import "C"

import (
	"encoding/json"
	"strconv"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// ---- Події на фронтенд -----------------------------------------------

//export cgoEmit
func cgoEmit(eventC, dataC *C.char) {
	defer recoverCb("cgoEmit")
	event := C.GoString(eventC)
	data := C.GoString(dataC)
	if app := application.Get(); app != nil {
		d := make([]byte, len(data))
		copy(d, data)
		safeInvokeAsync(func() {
			app.Event.Emit(event, json.RawMessage(d))
		})
	}
}

// ---- Вікна --------------------------------------------------------------

//export cgoWindowCreate
func cgoWindowCreate(idC, optsC *C.char) {
	defer recoverCb("cgoWindowCreate")
	id := C.GoString(idC)
	optsJSON := C.GoString(optsC)
	safeInvokeAsync(func() { windowCreate(id, optsJSON) })
}

//export cgoWindowOp
func cgoWindowOp(idC, opC *C.char) {
	defer recoverCb("cgoWindowOp")
	id := C.GoString(idC)
	op := C.GoString(opC)
	safeInvokeAsync(func() { windowOp(id, op) })
}

//export cgoWindowSetSize
func cgoWindowSetSize(idC *C.char, w, h C.int) {
	defer recoverCb("cgoWindowSetSize")
	id := C.GoString(idC)
	safeInvokeAsync(func() { windowSetSize(id, int(w), int(h)) })
}

//export cgoWindowSetMinSize
func cgoWindowSetMinSize(idC *C.char, w, h C.int) {
	defer recoverCb("cgoWindowSetMinSize")
	id := C.GoString(idC)
	safeInvokeAsync(func() { windowSetMinSize(id, int(w), int(h)) })
}

// cgoWindowGetSize — синхронний запит розміру вікна. Викликається лише з
// не-UI потоків (диспетчер/горутини модуля) — InvokeSync тут безпечний.
// Повертає malloc-рядок (backend.so звільняє його через free_string).
//
//export cgoWindowGetSize
func cgoWindowGetSize(idC *C.char) *C.char {
	defer recoverCb("cgoWindowGetSize")
	id := C.GoString(idC)
	var w, h int
	safeInvokeSync(func() {
		if win := getWindow(id); win != nil {
			w, h = win.Size()
		}
	})
	return C.CString(`{"w":` + strconv.Itoa(w) + `,"h":` + strconv.Itoa(h) + `}`)
}

//export cgoWindowIsVisible
func cgoWindowIsVisible(idC *C.char) C.int {
	defer recoverCb("cgoWindowIsVisible")
	id := C.GoString(idC)
	vis := false
	safeInvokeSync(func() {
		if win := getWindow(id); win != nil {
			vis = win.IsVisible()
		}
	})
	if vis {
		return 1
	}
	return 0
}

// ---- Трей -----------------------------------------------------------

//export cgoTraySet
func cgoTraySet(iconC, tipC, menuC *C.char) {
	defer recoverCb("cgoTraySet")
	iconB64 := C.GoString(iconC)
	tooltip := C.GoString(tipC)
	menuJSON := C.GoString(menuC)
	safeInvokeAsync(func() { traySet(iconB64, tooltip, menuJSON) })
}

//export cgoTrayDestroy
func cgoTrayDestroy() {
	defer recoverCb("cgoTrayDestroy")
	safeInvokeAsync(func() { trayDestroy() })
}

// ---- Діалоги (синхронні, повертають char*) ---------------------------

//export cgoDialogOpenFile
func cgoDialogOpenFile(optsC *C.char) *C.char {
	defer recoverCb("cgoDialogOpenFile")
	return C.CString(dialogOpenFile(C.GoString(optsC)))
}

//export cgoDialogOpenFiles
func cgoDialogOpenFiles(optsC *C.char) *C.char {
	defer recoverCb("cgoDialogOpenFiles")
	return C.CString(dialogOpenFiles(C.GoString(optsC)))
}

//export cgoDialogSaveFile
func cgoDialogSaveFile(optsC *C.char) *C.char {
	defer recoverCb("cgoDialogSaveFile")
	return C.CString(dialogSaveFile(C.GoString(optsC)))
}

// ---- Браузер / вихід / фази -------------------------------------------

// openExternalURL живе в browser_open_linux.go — там ланцюжок
// xdg-open/gio/$BROWSER, санітизація середовища (без AppImage-бібліотек
// і без наших WebKit/Mesa compat-змінних) і коректне «пожинання»
// дочірнього процесу.

//export cgoBrowserOpenURL
func cgoBrowserOpenURL(urlC *C.char) {
	defer recoverCb("cgoBrowserOpenURL")
	u := C.GoString(urlC)
	// ВАЖЛИВО: НЕ safeInvokeAsync. Запуск браузера — це fork/exec плюс
	// xdg-open, який сканує freedesktop MIME-бази; на холодному кеші це
	// секунди. На UI-потоці GTK це рівно стільки ж часу замороженого
	// вікна лаунчера. Операція нічого не робить з GTK-об'єктами, тож
	// їй головний потік не потрібен — виконуємо у звичайній горутині.
	go openExternalURL(u)
}

// cgoAppQuit — повне завершення процесу (трей «Вийти» тощо).
//
//export cgoAppQuit
func cgoAppQuit() {
	defer recoverCb("cgoAppQuit")
	safeInvokeAsync(func() {
		if app := application.Get(); app != nil {
			app.Quit()
		}
	})
}

// cgoFreeString — модуль звільняє рядок, виділений ШЕЛЛОМ (allocCString,
// malloc). Дзеркало cbFreeString на Windows (там — VirtualFree).
//
//export cgoFreeString
func cgoFreeString(p *C.char) {
	defer recoverCb("cgoFreeString")
	if p != nil {
		C.free(unsafe.Pointer(p))
	}
}

// cgoPhaseComplete — updater.so завершив роботу: перемикаємось на backend.so.
//
//export cgoPhaseComplete
func cgoPhaseComplete() {
	defer recoverCb("cgoPhaseComplete")
	safeInvokeAsync(transitionToBackend)
}

// cgoRunUpdater — backend.so просить перейти у фазу оновлення.
//
//export cgoRunUpdater
func cgoRunUpdater() {
	defer recoverCb("cgoRunUpdater")
	safeInvokeAsync(transitionToUpdater)
}
