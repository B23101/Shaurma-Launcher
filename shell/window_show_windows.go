//go:build windows

package main

// window_show_windows.go — показ вікна БЕЗ викрадення фокуса.
//
// Віджет сповіщень у трей має зʼявитись у правому нижньому куті, не
// перериваючи те, що користувач робить в іншому застосунку (набір тексту в
// чаті, гра на повний екран). Звичайний Show() на Windows виконує
// ShowWindow(hwnd, SW_SHOW) — це активізує вікно і фокус їде до нього.
//
// Чому не WS_EX_NOACTIVATE: за джерелом Wails (webview_window_windows.go,
// коментар біля обчислення exStyle) цей стиль прибрано саме тому, що він
// блокує фокус і ламає поведінку після Win+D/Alt+Tab. А віддати його в
// WebviewWindowOptions.Windows.ExStyle не можна ВЗАГАЛІ: Wails там не OR-ить,
// а ЗАМІНЮЄ увесь обчислений стиль (exStyle = options.Windows.ExStyle) — разом
// з WS_EX_TOPMOST, WS_EX_TOOLWINDOW і WS_EX_NOREDIRECTIONBITMAP, без якого не
// працює прозорість вікна.
//
// Рішення: показувати як звичайно (щоб Wails виставив внутрішні прапорці
// windowShown/showRequested і показав сам webview через chromium.Show()), а
// ПОТІМ повернути foreground тому, хто мав його до показу. Працює тому, що
// після ShowWindow наш процес УЖЕ має право на foreground — SetForegroundWindow
// тоді не відхиляється системою.
//
// Невдача SetForegroundWindow не є фатальною: worst case фокус опиниться у
// віджеті на час його показу (до 7 с), тобто поведінка не гірша за Show().

import (
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/w32"
)

// showWindowInactive показує вікно і негайно повертає фокус попередньому
// власнику. prev == 0 (нічого не було в фокусі) — просто показуємо.
func showWindowInactive(win *application.WebviewWindow) {
	if win == nil {
		return
	}
	prev := w32.GetForegroundWindow()
	win.Show()
	if prev == 0 {
		return
	}
	w32.SetForegroundWindow(prev)
}
