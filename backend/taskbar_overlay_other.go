//go:build !windows

package main

// taskbar_overlay_other.go — оверлей-бейдж у панелі завдань є лише на Windows (ITaskbarList3).

// setTaskbarOverlay — no-op поза Windows; true = «нічого повторювати».
func setTaskbarOverlay(n int) bool { return true }
