//go:build windows

package main

// bridge_host_windows.go — завантаження backend.dll/updater.dll і виклики
// чотирьох експортованих функцій (bridge.h) через ЧИСТИЙ syscall, без cgo
// (shell.exe збирається з CGO_ENABLED=0, як і старий wails-білд).
//
// Модулі (backend.dll і updater.dll) реалізують ОДИН І ТОЙ САМИЙ контракт —
// тому shell завантажує будь-який з них через той самий dllModule і не знає,
// хто саме зараз підвантажений (Bridge.swap лише підміняє вказівники).

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// dllModule — завантажена DLL-модуль з адресами чотирьох функцій контракту.
type dllModule struct {
	handle   syscall.Handle
	init     uintptr // int  backend_init(char* configJSON, void* callbacks)
	invoke   uintptr // char* backend_invoke(char* method, char* argsJSON)
	shutdown uintptr // void  backend_shutdown()
	free     uintptr // void  backend_free(char* s)
}

// loadModule завантажує DLL і резолвить чотири обов'язкові символи.
func loadModule(path string) (*dllModule, error) {
	h, err := syscall.LoadLibrary(path)
	if err != nil {
		return nil, fmt.Errorf("LoadLibrary(%s): %w", path, err)
	}
	m := &dllModule{handle: h}
	for name, dst := range map[string]*uintptr{
		"backend_init":     &m.init,
		"backend_invoke":   &m.invoke,
		"backend_shutdown": &m.shutdown,
		"backend_free":     &m.free,
	} {
		p, err := syscall.GetProcAddress(h, name)
		if err != nil {
			syscall.FreeLibrary(h)
			return nil, fmt.Errorf("GetProcAddress(%s): %w", name, err)
		}
		*dst = p
	}
	return m, nil
}

// initModule кличе backend_init(configJSON, &callbacks). Повертає 0 при успіху.
func (m *dllModule) initModule(configJSON string, cbs *bridgeCallbacksC) int {
	cfg, cfgKeep := cstrPtr(configJSON)
	defer runtime.KeepAlive(cfgKeep)
	r, _, _ := syscall.SyscallN(m.init, uintptr(cfg), uintptr(unsafe.Pointer(cbs)))
	return int(r)
}

// invokeModule кличе backend_invoke(method, argsJSON) і повертає рядок-результат
// ({"ok":..,"result":..}|{"ok":..,"error":..}), звільняючи пам'ять DLL через
// backend_free. Порожній рядок — якщо DLL повернула NULL або не завантажена.
func (m *dllModule) invokeModule(method, argsJSON string) string {
	if m == nil || m.invoke == 0 {
		return `{"ok":false,"error":"модуль не завантажено"}`
	}
	mp, mKeep := cstrPtr(method)
	ap, aKeep := cstrPtr(argsJSON)
	r, _, _ := syscall.SyscallN(m.invoke, uintptr(mp), uintptr(ap))
	runtime.KeepAlive(mKeep)
	runtime.KeepAlive(aKeep)
	if r == 0 {
		return `{"ok":false,"error":"backend_invoke повернув NULL"}`
	}
	s := goStr(r)
	if m.free != 0 {
		syscall.SyscallN(m.free, r)
	}
	return s
}

// shutdownModule кличе backend_shutdown перед вивантаженням DLL.
func (m *dllModule) shutdownModule() {
	if m != nil && m.shutdown != 0 {
		syscall.SyscallN(m.shutdown)
	}
}

// allocCString виділяє пам'ять під NUL-завершений рядок (VirtualAlloc) для
// значень, які DLL звільняє через колбек free_string (bridge.h). Звільняється
// у cbFreeString через VirtualFree.
func allocCString(s string) uintptr {
	b := append([]byte(s), 0)
	mem, err := windows.VirtualAlloc(0, uintptr(len(b)), memCommit|memReserve, pageReadWrite)
	if err != nil {
		return 0
	}
	dst := unsafe.Slice((*byte)(unsafe.Pointer(mem)), len(b))
	copy(dst, b)
	return mem
}

// freeCString звільняє пам'ять, виділену allocCString (VirtualFree).
func freeCString(p uintptr) {
	if p != 0 {
		_ = windows.VirtualFree(p, 0, memRelease)
	}
}

// ---- bridgeCallbacksC: дзеркало BridgeCallbacks (bridge.h v2) ------------
//
// Layout ідентичний C-структурі на amd64: uint32 (struct_version) + 4 байти
// паддингу + по 8 байт на кожен вказівник функції. Кожне поле — код
// trampoline від syscall.NewCallback (працює без cgo на windows/amd64).
type bridgeCallbacksC struct {
	structVersion    uint32
	_pad             uint32
	emit             uintptr
	windowCreate     uintptr
	windowOp         uintptr
	windowSetSize    uintptr
	windowGetSize    uintptr
	windowIsVisible  uintptr
	traySet          uintptr
	trayDestroy      uintptr
	dialogOpenFile   uintptr
	browserOpenURL   uintptr
	appQuit          uintptr
	freeString       uintptr
	dialogOpenFiles  uintptr // v2
	dialogSaveFile   uintptr // v2
	windowSetMinSize uintptr // v2
	phaseComplete    uintptr // v2
	runUpdater       uintptr // v2
}

var bridgeCB bridgeCallbacksC

// initCallbacks заповнює структуру колбеків (викликається один раз у main).
func initCallbacks() {
	bridgeCB = bridgeCallbacksC{
		structVersion:    2,
		emit:             syscall.NewCallback(cbEmit),
		windowCreate:     syscall.NewCallback(cbWindowCreate),
		windowOp:         syscall.NewCallback(cbWindowOp),
		windowSetSize:    syscall.NewCallback(cbWindowSetSize),
		windowGetSize:    syscall.NewCallback(cbWindowGetSize),
		windowIsVisible:  syscall.NewCallback(cbWindowIsVisible),
		traySet:          syscall.NewCallback(cbTraySet),
		trayDestroy:      syscall.NewCallback(cbTrayDestroy),
		dialogOpenFile:   syscall.NewCallback(cbDialogOpenFile),
		browserOpenURL:   syscall.NewCallback(cbBrowserOpenURL),
		appQuit:          syscall.NewCallback(cbAppQuit),
		freeString:       syscall.NewCallback(cbFreeString),
		dialogOpenFiles:  syscall.NewCallback(cbDialogOpenFiles),
		dialogSaveFile:   syscall.NewCallback(cbDialogSaveFile),
		windowSetMinSize: syscall.NewCallback(cbWindowSetMinSize),
		phaseComplete:    syscall.NewCallback(cbPhaseComplete),
		runUpdater:       syscall.NewCallback(cbRunUpdater),
	}
}

// ---- Win32 константи для VirtualAlloc/VirtualFree ------------------------

const (
	memCommit     = 0x1000
	memReserve    = 0x2000
	memRelease    = 0x8000
	pageReadWrite = 0x04
)
