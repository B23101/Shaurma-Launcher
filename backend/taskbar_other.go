//go:build !windows

package main

// taskbar_other.go — прогрес у панелі завдань — підтримується лише на
// Windows (ITaskbarList3). На інших платформах — безпечний no-op (той
// самий підхід, що в notify.go: лаунчер Windows-only, але код збирається
// всюди).
//
// tbpNoProgress і друзі не оголошуються тут: константи живуть у
// taskbar_windows.go (build tag windows), а єдине спільне — сигнатура
// updateTaskbarProgress нижче, яку кличе app.go.

// updateTaskbarProgress — no-op на не-Windows платформах.
func updateTaskbarProgress(percent int, state taskbarState) {}
