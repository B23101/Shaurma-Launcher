//go:build !windows

package main

import (
	"os"
	"syscall"
)

// isProcessAlive — не-Windows fallback: сигнал 0 до процесу. Якщо процес
// живий — сигнал «доставляється» без помилки; інакше повертається помилка.
func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

// killProcess — не-Windows fallback: SIGKILL процесу за PID. Помилки тихо
// ігноруються (процес міг уже завершитись).
func killProcess(pid int) {
	if pid <= 0 {
		return
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return
	}
	_ = proc.Kill()
}
