//go:build linux

package main

import (
	"os"
	"strings"
)

// window_pos_linux.go — чи можна на цьому дисплей-сервері читати/задавати
// абсолютну позицію власного вікна.
//
// Wayland цього НЕ дозволяє принципово (це не обмеження GTK, а дизайн
// протоколу: розміщенням вікон керує виключно композитор). Через це:
//   - win.Position() повертає нулі;
//   - win.SetPosition() нічого не робить.
//
// Під XWayland (GDK_BACKEND=x11, виставлений DISPLAY) працює класичний
// X11-шлях, тому там позиціонування лишається ввімкненим.
func windowPositioningSupported() bool {
	if strings.EqualFold(os.Getenv("GDK_BACKEND"), "x11") {
		return true
	}
	if strings.EqualFold(os.Getenv("XDG_SESSION_TYPE"), "wayland") {
		return false
	}
	return os.Getenv("WAYLAND_DISPLAY") == ""
}
