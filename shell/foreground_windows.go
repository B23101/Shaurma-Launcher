//go:build windows

package main

// foreground_windows.go — повернення головного вікна на передній план при
// повторному запуску лаунчера (клік по ярлику/іконці Пуск, коли він уже
// працює у треї чи у фоні).
//
// Проблема: Windows забороняє процесу, який не є foreground, забирати фокус
// (foreground lock). Перший екземпляр показував вікно через Show()+Focus(),
// але фокус система не віддавала — вікно з'являлось позаду інших вікон або
// лише блимало на панелі завдань, і користувачу здавалось, що «інтерфейс не
// відкривається». Право на foreground має лише НОВИЙ процес (його щойно
// запустив користувач), тому:
//   1. allowAnyForeground() — новий процес (і наглядач, і дочірній) дозволяє
//      іншим процесам перехопити фокус (ASFW_ANY) ще до передачі даних
//      першому екземпляру;
//   2. forceForeground() — перший екземпляр додатково піднімає вікно
//      стандартним обходом (AttachThreadInput до потоку активного вікна).

import (
	"syscall"

	"github.com/wailsapp/wails/v3/pkg/application"
)

var (
	fgUser32                     = syscall.NewLazyDLL("user32.dll")
	fgKernel32                   = syscall.NewLazyDLL("kernel32.dll")
	procAllowSetForegroundWindow = fgUser32.NewProc("AllowSetForegroundWindow")
	procAttachThreadInput        = fgUser32.NewProc("AttachThreadInput")
	procBringWindowToTop         = fgUser32.NewProc("BringWindowToTop")
	procGetForegroundWindow      = fgUser32.NewProc("GetForegroundWindow")
	procGetWindowThreadProcessId = fgUser32.NewProc("GetWindowThreadProcessId")
	procIsIconic                 = fgUser32.NewProc("IsIconic")
	procSetForegroundWindow      = fgUser32.NewProc("SetForegroundWindow")
	procShowWindowFg             = fgUser32.NewProc("ShowWindow")
	procGetCurrentThreadId       = fgKernel32.NewProc("GetCurrentThreadId")
)

const (
	asfwAny   = 0xFFFFFFFF // ASFW_ANY: (DWORD)-1
	swRestore = 9          // SW_RESTORE
)

// allowAnyForeground дозволяє будь-якому процесу вивести своє вікно на
// передній план. Викликати з процесу, якого щойно запустив користувач.
func allowAnyForeground() {
	procAllowSetForegroundWindow.Call(uintptr(asfwAny))
}

// forceForeground піднімає вікно на передній план і віддає йому фокус,
// обходячи foreground lock. Викликати з UI-потоку. Невдача не фатальна.
func forceForeground(win *application.WebviewWindow) {
	if win == nil {
		return
	}
	hwnd := uintptr(win.NativeWindow())
	if hwnd == 0 {
		return
	}
	if r, _, _ := procIsIconic.Call(hwnd); r != 0 {
		procShowWindowFg.Call(hwnd, uintptr(swRestore))
	}
	fg, _, _ := procGetForegroundWindow.Call()
	var fgThread uintptr
	if fg != 0 {
		fgThread, _, _ = procGetWindowThreadProcessId.Call(fg, 0)
	}
	cur, _, _ := procGetCurrentThreadId.Call()
	attached := false
	if fgThread != 0 && fgThread != cur {
		r, _, _ := procAttachThreadInput.Call(cur, fgThread, 1)
		attached = r != 0
	}
	procBringWindowToTop.Call(hwnd)
	procSetForegroundWindow.Call(hwnd)
	if attached {
		procAttachThreadInput.Call(cur, fgThread, 0)
	}
}
