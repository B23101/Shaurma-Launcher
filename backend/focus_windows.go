package main

import (
	"syscall"
	"time"
	"unsafe"
)

// bringWindowToFront піднімає вікно лаунчера у фокус після старту.
//
// Причина: launcher.exe майже завжди запускається з shrm-updater.exe
// (ярлик у меню Пуск). Updater — foreground-процес: він показує своє
// вікно оновлення, потім запускає launcher.exe і одразу виходить
// через runtime.Quit. Windows тоді НЕ віддає фокус новому вікну
// launcher (воно відкривається позаду інших вікон), і користувач бачить
// "вікно оновлення блимнуло і більше нічого" — хоча лаунчер насправді
// працює.
//
// Рішення: знаходимо HWND нашого головного вікна за PID процесу
// (EnumWindows + GetWindowThreadProcessId) і примусово викликаємо
// SetForegroundWindow на ньому. Перед цим робимо AttachThreadInput нашого
// потоку до потоку поточного активного вікна — це стандартний обхід
// Windows foreground-lock (система забороняє процесу без права
// перехоплення фокусу "вкрасти" його, а AttachThreadInput це право дає).
func bringWindowToFront() {
	// Даємо Wails час підняти вікно (ServiceStartup спрацьовує до того, як
	// вікно повністю з'явилось) — інакше SetForegroundWindow влучить у
	// порожній момент.
	time.Sleep(800 * time.Millisecond)

	pid := uint32(syscall.Getpid())

	var ourHwnd uintptr
	user32 := syscall.NewLazyDLL("user32.dll")
	enumWindows := user32.NewProc("EnumWindows")
	enumProc := syscall.NewCallback(func(hwnd uintptr, lparam uintptr) uintptr {
		var wndPid uint32
		user32.NewProc("GetWindowThreadProcessId").Call(hwnd, uintptr(unsafe.Pointer(&wndPid)))
		if wndPid == pid {
			// Перше знайдене вікно нашого процесу — зазвичай і є
			// головним вікном лаунчера (WebView2 має окремі процеси
			// зі своїми PID, тому не переплутаємось).
			ourHwnd = hwnd
			return 0 // зупиняємо перебір
		}
		return 1
	})
	enumWindows.Call(enumProc, 0)
	if ourHwnd == 0 {
		return
	}

	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getThread := kernel32.NewProc("GetCurrentThreadId")
	attach := user32.NewProc("AttachThreadInput")
	detach := user32.NewProc("AttachThreadInput")
	getWindowThread := user32.NewProc("GetWindowThreadProcessId")
	setForeground := user32.NewProc("SetForegroundWindow")
	bringToTop := user32.NewProc("BringWindowToTop")

	activeHwnd, _, _ := user32.NewProc("GetForegroundWindow").Call()
	ourThread, _, _ := getThread.Call()
	var activeThread uintptr
	if activeHwnd != 0 {
		activeThread, _, _ = getWindowThread.Call(activeHwnd, 0)
	}

	if activeThread != 0 && ourThread != activeThread {
		attach.Call(ourThread, activeThread, 1) // TRUE
	}
	setForeground.Call(ourHwnd)
	bringToTop.Call(ourHwnd)
	if activeThread != 0 && ourThread != activeThread {
		detach.Call(ourThread, activeThread, 0) // FALSE
	}
}
