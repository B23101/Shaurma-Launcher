//go:build windows

package main

// crash_dialog_windows.go — showStartupCrashDialog для Windows: нативний
// MessageBox через golang.org/x/sys/windows. НЕ залежить від WebView2 чи
// Wails runtime — критично, бо паніка в main() може статись ДО того, як
// той runtime встиг піднятись, тобто showFatalError (яка створює
// Wails-вікно) тут ще не спрацює.

import (
	"fmt"

	"golang.org/x/sys/windows"
)

func showStartupCrashDialog(stack string) {
	msg, _ := windows.UTF16PtrFromString(fmt.Sprintf(
		"Shaurm Launcher crashed during startup:\n\n%v\n\nCheck logs: %%APPDATA%%\\.shaurm\\logs", stack))
	title, _ := windows.UTF16PtrFromString("Shaurm Launcher — error")
	windows.MessageBox(0, msg, title, windows.MB_OK|windows.MB_ICONERROR)
}
