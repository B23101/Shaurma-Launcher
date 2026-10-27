//go:build windows

package main

import "golang.org/x/sys/windows"

// processAlive — чи процес із вказаним PID ще живий. Логіка та сама, що в
// backend/process_health.go: відкриваємо з PROCESS_QUERY_LIMITED_INFORMATION
// і перевіряємо, що код виходу ще STILL_ACTIVE (259). Помилка відкриття
// (немає доступу / процес не існує) трактується як «мертвий» — саме те,
// що потрібно для очікування звільнення single-instance локу.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == 259 // STILL_ACTIVE
}
