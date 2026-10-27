//go:build !windows

package main

import (
	"os"
	"syscall"
)

// processAlive — не-Windows fallback (як backend/process_health_other.go):
// сигнал 0 до процесу. Якщо процес живий — сигнал «доставляється» без
// помилки; інакше os.FindProcess/Signal повертає помилку (ESRCH).
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}
