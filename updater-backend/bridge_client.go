// bridge_client.go — ЄДИНЕ місце в backend.dll, де є cgo-виклики
// колбеків shell'а. Решта коду (console_window.go, tray_menu.go і всі
// інші перенесені файли) кличуть прості Go-функції звідси
// (bridgeWindowCreate, bridgeWindowOp, ...) і НЕ знають про cgo/C
// взагалі — так само, як зараз вони не знають про internal влаштування
// application.Get().
//
// Кожна публічна функція тут — дзеркало одного поля BridgeCallbacks
// з bridge.h (v2). Компілюється лише в межах backend.dll (той самий
// принцип, що invoke_emit у прототипі: cgo не викликає Go function
// pointer напряму, тому C-хелпер у преамбулі).
package main

/*
#include <stdlib.h>
#include <string.h>
#include "bridge.h"

static void call_window_create(const BridgeCallbacks* cb, const char* id, const char* opts) {
    if (cb && cb->window_create) cb->window_create(id, opts);
}
static void call_window_op(const BridgeCallbacks* cb, const char* id, const char* op) {
    if (cb && cb->window_op) cb->window_op(id, op);
}
static void call_window_set_size(const BridgeCallbacks* cb, const char* id, int w, int h) {
    if (cb && cb->window_set_size) cb->window_set_size(id, w, h);
}
static void call_window_set_min_size(const BridgeCallbacks* cb, const char* id, int w, int h) {
    if (cb && cb->window_set_min_size) cb->window_set_min_size(id, w, h);
}
static char* call_window_get_size(const BridgeCallbacks* cb, const char* id) {
    if (cb && cb->window_get_size) return cb->window_get_size(id);
    return NULL;
}
static int call_window_is_visible(const BridgeCallbacks* cb, const char* id) {
    if (cb && cb->window_is_visible) return cb->window_is_visible(id);
    return 0;
}
static void call_tray_set(const BridgeCallbacks* cb, const char* icon, const char* tooltip, const char* menu) {
    if (cb && cb->tray_set) cb->tray_set(icon, tooltip, menu);
}
static void call_tray_destroy(const BridgeCallbacks* cb) {
    if (cb && cb->tray_destroy) cb->tray_destroy();
}
static char* call_dialog_open_file(const BridgeCallbacks* cb, const char* options) {
    if (cb && cb->dialog_open_file) return cb->dialog_open_file(options);
    return NULL;
}
static char* call_dialog_open_files(const BridgeCallbacks* cb, const char* options) {
    if (cb && cb->dialog_open_files) return cb->dialog_open_files(options);
    return NULL;
}
static char* call_dialog_save_file(const BridgeCallbacks* cb, const char* options) {
    if (cb && cb->dialog_save_file) return cb->dialog_save_file(options);
    return NULL;
}
static void call_browser_open_url(const BridgeCallbacks* cb, const char* url) {
    if (cb && cb->browser_open_url) cb->browser_open_url(url);
}
static void call_app_quit(const BridgeCallbacks* cb) {
    if (cb && cb->app_quit) cb->app_quit();
}
static void call_emit(const BridgeCallbacks* cb, const char* event, const char* data) {
    if (cb && cb->emit) cb->emit(event, data);
}
static void call_free_string(const BridgeCallbacks* cb, char* s) {
    if (cb && cb->free_string && s) cb->free_string(s);
}
static void call_phase_complete(const BridgeCallbacks* cb) {
    if (cb && cb->phase_complete) cb->phase_complete();
}
static void call_run_updater(const BridgeCallbacks* cb) {
    if (cb && cb->run_updater) cb->run_updater();
}
*/
import "C"

import (
	"encoding/json"
	"unsafe"
)

// bridgeCallbacks — вказівник, збережений у backend_init (main.go).
var bridgeCallbacks *C.BridgeCallbacks

func setBridgeCallbacks(cb unsafe.Pointer) {
	bridgeCallbacks = (*C.BridgeCallbacks)(cb)
}

// bridgeCallbacksVersion — версія структури колбеків, яку передав shell
// (щоб dll знала, які поля безпечно читати).
func bridgeCallbacksVersion() int {
	if bridgeCallbacks == nil {
		return 0
	}
	return int(bridgeCallbacks.struct_version)
}

func cstr(s string) *C.char { return C.CString(s) }

func freeCstr(s *C.char) { C.free(unsafe.Pointer(s)) }

// ---- Generic window API ---------------------------------------------

type windowSize struct {
	W int `json:"w"`
	H int `json:"h"`
}

func bridgeWindowCreate(id, optionsJSON string) {
	if bridgeCallbacks == nil {
		return
	}
	cid, copts := cstr(id), cstr(optionsJSON)
	defer freeCstr(cid)
	defer freeCstr(copts)
	C.call_window_create(bridgeCallbacks, cid, copts)
}

func bridgeWindowOp(id, op string) {
	if bridgeCallbacks == nil {
		return
	}
	cid, cop := cstr(id), cstr(op)
	defer freeCstr(cid)
	defer freeCstr(cop)
	C.call_window_op(bridgeCallbacks, cid, cop)
}

func bridgeWindowSetSize(id string, w, h int) {
	if bridgeCallbacks == nil {
		return
	}
	cid := cstr(id)
	defer freeCstr(cid)
	C.call_window_set_size(bridgeCallbacks, cid, C.int(w), C.int(h))
}

func bridgeWindowSetMinSize(id string, w, h int) {
	if bridgeCallbacks == nil || bridgeCallbacksVersion() < 2 {
		return
	}
	cid := cstr(id)
	defer freeCstr(cid)
	C.call_window_set_min_size(bridgeCallbacks, cid, C.int(w), C.int(h))
}

func bridgeWindowGetSize(id string) (w, h int) {
	if bridgeCallbacks == nil {
		return 0, 0
	}
	cid := cstr(id)
	defer freeCstr(cid)
	cres := C.call_window_get_size(bridgeCallbacks, cid)
	if cres == nil {
		return 0, 0
	}
	defer C.call_free_string(bridgeCallbacks, cres)
	var s windowSize
	if err := json.Unmarshal([]byte(C.GoString(cres)), &s); err != nil {
		return 0, 0
	}
	return s.W, s.H
}

func bridgeWindowIsVisible(id string) bool {
	if bridgeCallbacks == nil {
		return false
	}
	cid := cstr(id)
	defer freeCstr(cid)
	return C.call_window_is_visible(bridgeCallbacks, cid) != 0
}

// ---- Tray -------------------------------------------------------------

func bridgeTraySet(iconPNGBase64, tooltip, menuJSON string) {
	if bridgeCallbacks == nil {
		return
	}
	cicon, ctip, cmenu := cstr(iconPNGBase64), cstr(tooltip), cstr(menuJSON)
	defer freeCstr(cicon)
	defer freeCstr(ctip)
	defer freeCstr(cmenu)
	C.call_tray_set(bridgeCallbacks, cicon, ctip, cmenu)
}

func bridgeTrayDestroy() {
	if bridgeCallbacks == nil {
		return
	}
	C.call_tray_destroy(bridgeCallbacks)
}

// ---- Dialogs / browser / quit ----------------------------------------

// dialogOptions — формат options_json для діалогів (див. bridge.h).
type dialogOptions struct {
	Title       string `json:"title"`
	Directories bool   `json:"directories"`
	DefaultName string `json:"defaultName"`
	Filters     []struct {
		Name       string   `json:"name"`
		Extensions []string `json:"extensions"`
	} `json:"filters"`
}

// bridgeDialogOpenFile відкриває діалог вибору ОДНОГО файлу/теки.
// Повертає шлях або "" при скасуванні.
func bridgeDialogOpenFile(title string, directories bool, filters map[string][]string) string {
	if bridgeCallbacks == nil {
		return ""
	}
	opts := dialogOptions{Title: title, Directories: directories}
	for name, exts := range filters {
		opts.Filters = append(opts.Filters, struct {
			Name       string   `json:"name"`
			Extensions []string `json:"extensions"`
		}{Name: name, Extensions: exts})
	}
	b, _ := json.Marshal(opts)
	copts := cstr(string(b))
	defer freeCstr(copts)
	cres := C.call_dialog_open_file(bridgeCallbacks, copts)
	if cres == nil {
		return ""
	}
	defer C.call_free_string(bridgeCallbacks, cres)
	return C.GoString(cres)
}

// bridgeDialogOpenFiles відкриває діалог вибору КІЛЬКОХ файлів.
// Повертає список шляхів (може бути порожнім при скасуванні).
func bridgeDialogOpenFiles(title string, filters map[string][]string) []string {
	if bridgeCallbacks == nil || bridgeCallbacksVersion() < 2 {
		return nil
	}
	opts := dialogOptions{Title: title}
	for name, exts := range filters {
		opts.Filters = append(opts.Filters, struct {
			Name       string   `json:"name"`
			Extensions []string `json:"extensions"`
		}{Name: name, Extensions: exts})
	}
	b, _ := json.Marshal(opts)
	copts := cstr(string(b))
	defer freeCstr(copts)
	cres := C.call_dialog_open_files(bridgeCallbacks, copts)
	if cres == nil {
		return nil
	}
	defer C.call_free_string(bridgeCallbacks, cres)
	var paths []string
	_ = json.Unmarshal([]byte(C.GoString(cres)), &paths)
	return paths
}

// bridgeDialogSaveFile відкриває діалог збереження файлу.
// Повертає шлях або "" при скасуванні.
func bridgeDialogSaveFile(title, defaultName string, filters map[string][]string) string {
	if bridgeCallbacks == nil || bridgeCallbacksVersion() < 2 {
		return ""
	}
	opts := dialogOptions{Title: title, DefaultName: defaultName}
	for name, exts := range filters {
		opts.Filters = append(opts.Filters, struct {
			Name       string   `json:"name"`
			Extensions []string `json:"extensions"`
		}{Name: name, Extensions: exts})
	}
	b, _ := json.Marshal(opts)
	copts := cstr(string(b))
	defer freeCstr(copts)
	cres := C.call_dialog_save_file(bridgeCallbacks, copts)
	if cres == nil {
		return ""
	}
	defer C.call_free_string(bridgeCallbacks, cres)
	return C.GoString(cres)
}

func bridgeBrowserOpenURL(url string) {
	if bridgeCallbacks == nil {
		return
	}
	curl := cstr(url)
	defer freeCstr(curl)
	C.call_browser_open_url(bridgeCallbacks, curl)
}

func bridgeAppQuit() {
	if bridgeCallbacks == nil {
		return
	}
	C.call_app_quit(bridgeCallbacks)
}

// bridgePhaseComplete — updater.dll: роботу завершено, shell може
// переходити до backend.dll (див. bridge.h v2).
func bridgePhaseComplete() {
	if bridgeCallbacks == nil || bridgeCallbacksVersion() < 2 {
		return
	}
	C.call_phase_complete(bridgeCallbacks)
}

// bridgeRunUpdater — backend.dll просить shell перейти у фазу оновлення.
func bridgeRunUpdater() {
	if bridgeCallbacks == nil || bridgeCallbacksVersion() < 2 {
		return
	}
	C.call_run_updater(bridgeCallbacks)
}

// ---- Events (той самий канал, що зараз a.emit) ------------------------

func bridgeEmit(event string, dataJSON string) {
	if bridgeCallbacks == nil {
		return
	}
	cevent, cdata := cstr(event), cstr(dataJSON)
	defer freeCstr(cevent)
	defer freeCstr(cdata)
	C.call_emit(bridgeCallbacks, cevent, cdata)
}
