//go:build !windows

package main

// window_show_other.go — фолбек show-inactive поза Windows.
//
// Лінукс-стек (GTK4/WebKitGTK) не має прямого аналога SW_SHOW і нашого
// «показати → повернути foreground» контракту: там WM сам вирішує, чи
// забирати фокус при Map(), і звичний для нас X11/Wayland-патерн — не
// викрадати фокус узагалі. Просто показуємо вікно: це вже не гірше за
// звичайний window_op "show", який до цього використовувався скрізь.
//
// Окремий файл з тегом потрібен тому, що показ «показати + віддати фокус»
// у windows-файлі імпортує pkg/w32, який поза Windows не компілюється.

import (
	"github.com/wailsapp/wails/v3/pkg/application"
)

func showWindowInactive(win *application.WebviewWindow) {
	if win == nil {
		return
	}
	win.Show()
}
