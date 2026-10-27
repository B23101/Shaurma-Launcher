//go:build windows

package main

// taskbar_windows.go — прогрес завантаження/оновлення збірки у панелі
// завдань Windows (ITaskbarList3::SetProgressValue/SetProgressState).
//
// ЧОМУ ТАК (план C2): shell.exe за проєктом «назавжди незмінний» — його
// не перекомпілюємо. Але backend.dll завантажується В ТОЙ САМИЙ процес
// (shell.exe робить LoadLibrary), тож з backend.dll ми бачимо HWND
// головного вікна так само, як focus_windows.go (EnumWindows + PID) — і
// можемо напряму кликати ITaskbarList3 через COM. Жодних змін у shell.
//
// Сценарій: backend агрегує sync:progress (recordSyncProgress, ~300мс) —
// тут ми малюємо прогрес у taskbar: заповнення за % качки, «нескінченний»
// (indeterminate) поки % ще невідомий, жовтий на паузі, червоний при
// помилці, прибирання після завершення.

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

// TBPFLAG — стани прогрес-бара ITaskbarList3.
const (
	tbpNoProgress    = 0x0 // прибрати прогрес з кнопки
	tbpIndeterminate = 0x1 // «нескінченний» прогрес (заповнення біжить)
	tbpNormal        = 0x2 // звичайний детермінований прогрес
	tbpError         = 0x4 // червоний (завершилося помилкою)
	tbpPaused        = 0x8 // жовтий (пауза)
)

var (
	ole32dll                  = syscall.NewLazyDLL("ole32.dll")
	procCoCreate              = ole32dll.NewProc("CoCreateInstance")
	procCoInitialize          = ole32dll.NewProc("CoInitializeEx")
	procTaskbarCoUninitialize = ole32dll.NewProc("CoUninitialize")
)

// CLSID_TaskbarList {56FDF344-FD6D-11D0-958A-006097C9A090}
var (
	clsidTaskbarList = struct {
		Data1 uint32
		Data2 uint16
		Data3 uint16
		Data4 [8]byte
	}{0x56FDF344, 0xFD6D, 0x11D0, [8]byte{0x95, 0x8A, 0x00, 0x60, 0x97, 0xC9, 0xA0, 0x90}}
	// IID_ITaskbarList3 {EA1AFB91-9E28-4B86-90E9-9E9F8A5EEA72}
	iidTaskbarList3 = struct {
		Data1 uint32
		Data2 uint16
		Data3 uint16
		Data4 [8]byte
	}{0xEA1AFB91, 0x9E28, 0x4B86, [8]byte{0x90, 0xE9, 0x9E, 0x9F, 0x8A, 0x5E, 0xEA, 0x72}}
)

// itaskbarList3 — мінімальний vtable-скелет ITaskbarList3 (лише методи,
// які використовуємо). Layout стандартний COM: IUnknown(3) → ITaskbarList
// (HrInit,AddTab,DeleteTab,ActivateTab,SetActiveAlt) → ITaskbarList2
// (MarkFullscreenWindow) → ITaskbarList3 (SetProgressValue,SetProgressState
// і далі — нас не цікавлять).
type itaskbarList3 struct {
	lpVtbl *taskbarVtbl
}

type taskbarVtbl struct {
	// IUnknown
	queryInterface uintptr
	addRef         uintptr
	release        uintptr
	// ITaskbarList
	hrInit       uintptr
	addTab       uintptr
	deleteTab    uintptr
	activateTab  uintptr
	setActiveAlt uintptr
	// ITaskbarList2
	markFullscreenWindow uintptr
	// ITaskbarList3
	setProgressValue   uintptr
	setProgressState   uintptr
	registerTab        uintptr
	unregisterTab      uintptr
	setTabOrder        uintptr
	setTabActive       uintptr
	thumbBarAddButtons uintptr
}

func (t *itaskbarList3) hrInit() error {
	r, _, _ := syscall.SyscallN(t.lpVtbl.hrInit, uintptr(unsafe.Pointer(t)))
	return hresultErr(uint32(r))
}

func (t *itaskbarList3) setProgressValue(hwnd uintptr, completed, total uint64) error {
	r, _, _ := syscall.SyscallN(t.lpVtbl.setProgressValue, uintptr(unsafe.Pointer(t)), hwnd,
		uintptr(completed), uintptr(total))
	return hresultErr(uint32(r))
}

func (t *itaskbarList3) setProgressState(hwnd uintptr, state uint32) error {
	r, _, _ := syscall.SyscallN(t.lpVtbl.setProgressState, uintptr(unsafe.Pointer(t)), hwnd, uintptr(state))
	return hresultErr(uint32(r))
}

// hresultErr перетворює HRESULT на error (успіх = >= 0).
func hresultErr(hr uint32) error {
	if hr >= 0x80000000 {
		return fmt.Errorf("HRESULT 0x%08X", hr)
	}
	return nil
}

// newTaskbarIface створює СВІЖИЙ ITaskbarList3 (CoCreateInstance + HrInit)
// на поточному OS-потоці. Помилки тихі: без taskbar-прогресу лаунчер
// працює нормально (екзотичні середовища, старий Windows).
//
// ВАЖЛИВО: інтерфейс створюється І використовується В ОДНОМУ виклику
// (див. updateTaskbarProgress), тому завжди на одному потоці. Go-горутини
// вільно мігрують між OS-потоками, тож кешований sync.Once-інтерфейс міг
// бути створений на STA-потоці (перший виклик з будь-якої горутини, напр.
// головного потоку, де Wails/WebView2 уже ініціалізували COM як STA), а
// потім викликатись з інших потоків — такі COM-виклики мовчки не
// працювали, і прогрес Shaurma-синку в таскбарі не з'являвся, хоча
// імпорт модпаків (стабільна горутина) працював. Per-call створення це
// повністю усуває.
//
// Повертає coOwned=true, якщо COM-ініціалізацію зробили МИ: тоді викликач
// зобов'язаний закінчити її CoUninitialize на ТОМУ Ж потоці (див.
// updateTaskbarProgress, де потік закріплений через runtime.LockOSThread).
func newTaskbarIface() (iface *itaskbarList3, coOwned bool, err error) {
	// CoInitializeEx повертає RPC_E_CHANGED_MODE (0x80010106), якщо потік уже
	// ініціалізовано з іншою моделлю (STA) — це НЕ помилка для нас: об'єкт
	// все одно створюється і використовується на цьому ж потоці.
	hr, _, _ := procCoInitialize.Call(0, 0 /*COINIT_MULTITHREADED*/)
	coOwned = coInitializedByUs(uint32(hr))
	var ppv unsafe.Pointer
	r, _, _ := procCoCreate.Call(
		uintptr(unsafe.Pointer(&clsidTaskbarList)),
		0, // pUnkOuter
		1, // CLSCTX_INPROC_SERVER
		uintptr(unsafe.Pointer(&iidTaskbarList3)),
		uintptr(unsafe.Pointer(&ppv)),
	)
	if err := hresultErr(uint32(r)); err != nil {
		return nil, coOwned, err
	}
	if ppv == nil {
		return nil, coOwned, fmt.Errorf("CoCreateInstance повернув nil")
	}
	iface = (*itaskbarList3)(ppv)
	// HrInit обов'язковий перед першим використанням. При невдачі
	// звільняємо об'єкт через Release (vtable[2]) на тому ж поінтері.
	if err := iface.hrInit(); err != nil {
		syscall.SyscallN(iface.lpVtbl.release, uintptr(ppv))
		return nil, coOwned, err
	}
	return iface, coOwned, nil
}

// coInitializedByUs — HRESULT CoInitializeEx, після якого закінчити
// ініціалізацію CoUninitialize ЗОБОВ'ЯЗАНИЙ саме ми: S_OK (0) та S_FALSE (1)
// (потік уже ініціалізований з тією ж моделлю — лічильник усе одно зростає).
// RPC_E_CHANGED_MODE (0x80010106) та інші помилки — не наш потік, не чіпаємо.
func coInitializedByUs(hr uint32) bool { return hr == 0 || hr == 1 }

// mainWindowHwnd повертає HWND головного вікна лаунчера (перше вікно
// нашого процесу). Той самий підхід, що у focus_windows.go: backend.dll
// живе В ПРОЦЕСІ shell.exe, тож EnumWindows за PID нас знаходить — це
// надійно, бо WebView2 тримає окремі процеси з іншими PID. Кешуємо після
// першого знаходження; якщо вікно ще не створене (сигнал прийшов рано) —
// наступний виклик пересканує (мутекс, не Once).
var (
	mainHwndMu  sync.Mutex
	mainHwnd    uintptr
	mainHwndSet bool
)

func mainWindowHwnd() uintptr {
	mainHwndMu.Lock()
	defer mainHwndMu.Unlock()
	if mainHwndSet {
		return mainHwnd
	}
	pid := uint32(syscall.Getpid())
	user32 := syscall.NewLazyDLL("user32.dll")
	enumWindows := user32.NewProc("EnumWindows")
	enumProc := syscall.NewCallback(func(hwnd uintptr, lparam uintptr) uintptr {
		var wndPid uint32
		user32.NewProc("GetWindowThreadProcessId").Call(hwnd, uintptr(unsafe.Pointer(&wndPid)))
		if wndPid == pid {
			mainHwnd = hwnd
			mainHwndSet = true
			return 0
		}
		return 1
	})
	enumWindows.Call(enumProc, 0)
	return mainHwnd
}

// updateTaskbarProgress малює прогрес у панелі завдань.
//
//	percent: 0..100 (для taskbarNormal)
//	state:   taskbarNormal/Indeterminate/Paused/Error/None
func updateTaskbarProgress(percent int, state taskbarState) {
	// Безпечний режим (safe_mode.go): COM взагалі не чіпаємо — прогрес у
	// панелі завдань не та фіча, заради якої варто ризикувати стартом.
	if safeModeActive() {
		return
	}
	hwnd := mainWindowHwnd()
	if hwnd == 0 {
		return
	}
	// КРИТИЧНО: CoInitializeEx, CoCreateInstance, усі vtable-виклики й Release
	// мусять відбутись на ОДНОМУ OS-потоці. Без LockOSThread горутина Go
	// вільно мігрує (у т.ч. між CoInitializeEx і CoCreateInstance), і тоді COM
	// створює/використовує об'єкт у потоці, який ми не ініціалізували —
	// невизначена поведінка в COM, а не просто «прогрес не показується».
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	taskbarIface, coOwned, err := newTaskbarIface()
	if err != nil {
		uninitializeComIfOwned(coOwned)
		return
	}
	// Release після використання (vtable[2]) — той самий об'єкт, що й створили.
	defer func() {
		syscall.SyscallN(taskbarIface.lpVtbl.release, uintptr(unsafe.Pointer(taskbarIface)))
		uninitializeComIfOwned(coOwned)
	}()
	switch state {
	case taskbarNone:
		_ = taskbarIface.setProgressState(hwnd, tbpNoProgress)
	case taskbarPaused:
		_ = taskbarIface.setProgressState(hwnd, tbpPaused)
		_ = taskbarIface.setProgressValue(hwnd, uint64(clampPercent(percent)), 100)
	case taskbarError:
		_ = taskbarIface.setProgressState(hwnd, tbpError)
	case taskbarIndeterminate:
		_ = taskbarIface.setProgressState(hwnd, tbpIndeterminate)
	default: // taskbarNormal
		_ = taskbarIface.setProgressState(hwnd, tbpNormal)
		_ = taskbarIface.setProgressValue(hwnd, uint64(clampPercent(percent)), 100)
	}
}

// uninitializeComIfOwned — симетричний до CoInitializeEx виклик на тому ж
// потоці (updateTaskbarProgress тримає його LockOSThread до самого кінця).
func uninitializeComIfOwned(owned bool) {
	if owned {
		procTaskbarCoUninitialize.Call()
	}
}

func clampPercent(p int) int {
	if p < 0 {
		return 0
	}
	if p > 100 {
		return 100
	}
	return p
}
