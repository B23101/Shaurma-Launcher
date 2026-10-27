//go:build !windows

package main

// No-op версія для не-Windows: пріоритет-класи процесів існують лише на
// Windows, тож тут просто нічого не робимо (сигнатури збігаються з
// priority_windows.go, щоб App міг викликати їх без розгалуження по GOOS).

// guardGamePriority — no-op на не-Windows.
func guardGamePriority() {}

// restoreGamePriority — no-op на не-Windows.
func restoreGamePriority() {}
