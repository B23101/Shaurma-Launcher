//go:build !linux

package main

// resolveWindowBackground — на не-Linux платформах (Windows) поведінка
// не змінюється: WebView2 + DWM надійно підтримують прозорість вікна,
// той клас проблем із композитором дисплей-сервера, що на Linux, тут
// не актуальний.
func resolveWindowBackground(background, backgroundHex string) (string, string) {
	return background, backgroundHex
}
