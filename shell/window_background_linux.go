//go:build linux

package main

// window_background_linux.go — див. розлогий коментар у windowCreate
// (window_exec.go). Коротко: WebKitGTK4-прозорість вікна залежить від
// композитора дисплей-сервера, якого в типовому Linux-оточенні для
// розробки/CI (WSLg, X11 без compositing WM) може не бути — тоді вікно
// з transparent-фоном рендериться суцільно білим замість прозорого.
// Симптом, який це виправляє: "оновлювач (solid-фон) без білого екрана,
// а головний екран лаунчера (transparent-фон) — з білим".

// linuxFallbackBackgroundHex — колір фолбека, коли вікно просило
// "transparent" без явного BackgroundHex. Навмисно той самий, що
// frontend/src/style.css: --bg-app у темній темі (яка й активна за
// замовчуванням) — фронтенд однаково малює цей колір як фон body{},
// тому реальної прозорості вікно й так практично не потребує.
const linuxFallbackBackgroundHex = "#0f0f0f"

// resolveWindowBackground — на Linux "transparent" завжди підмінюється
// на "solid" з кольором fallbackHex (або вже заданим hex, якщо він був).
// "solid" з уже заданим hex не чіпається.
func resolveWindowBackground(background, backgroundHex string) (string, string) {
	if background == "solid" {
		return background, backgroundHex
	}
	if backgroundHex == "" {
		backgroundHex = linuxFallbackBackgroundHex
	}
	return "solid", backgroundHex
}
