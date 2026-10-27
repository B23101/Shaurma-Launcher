//go:build windows

package main

import (
	"runtime"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/w32"
)

// setLauncherWindowIcon ставить на вікно іконку ЛАУНЧЕРА (завантажену з
// ресурсів exe, RT_ICON id=1), а не системну чи іконку ярлика.
//
// Без цього Wails реєструє клас вікна зі СИСТЕМНОЮ іконкою
// (IDI_APPLICATION), і Windows, коли лаунчер запущено через ярлик збірки
// (.lnk зі своєю іконкою збірки), показує у таскбарі/заголовку вікна
// іконку САМОГО ярлика — тобто іконку збірки замість власної іконки
// лаунчера (баг: «запускаю ярлик — лаунчер стартує з іконкою ярлика»).
// Явний WM_SETICON перебиває і системну іконку класу, і будь-яку
// прив'язку до .lnk.
func setLauncherWindowIcon(win *application.WebviewWindow) {
	if win == nil || runtime.GOOS != "windows" {
		return
	}
	hwnd := w32.HWND(win.NativeWindow())
	if hwnd == 0 {
		return
	}
	// Іконка вбудована в exe генератором syso (wails3 generate syso) з
	// ресурсним ID 1 (winres.SetIcon(RT_ICON)).
	icon, err := application.NewIconFromResource(w32.GetModuleHandle(""), 1)
	if err != nil || icon == 0 {
		return
	}
	// ICON_SMALL — іконка в заголовку вікна/таскбарі, ICON_BIG — велика
	// (Alt+Tab, панель завдань при високому DPI). Ставимо обидві.
	w32.SendMessage(hwnd, w32.WM_SETICON, w32.ICON_SMALL, uintptr(icon))
	w32.SendMessage(hwnd, w32.WM_SETICON, w32.ICON_BIG, uintptr(icon))
}
