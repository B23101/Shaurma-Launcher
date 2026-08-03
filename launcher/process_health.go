//go:build windows

package main

import (
	"golang.org/x/sys/windows"
)

// isProcessAlive перевіряє, чи процес із вказаним PID ще живий. На Windows
// процес «живий», якщо його можна відкрити з PROCESS_QUERY_LIMITED_INFORMATION
// і його код виходу досі STILL_ACTIVE. Використовується для відновлення
// зв'язку з грою після перезапуску лаунчера (tray-PID): якщо PID з
// running-game.json живий — гра все ще запущена, лаунчер не «забуває» її.
func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		// Немає доступу/процес не існує — вважаємо мертвим.
		return false
	}
	defer windows.CloseHandle(handle)
	var code uint32
	if err := windows.GetExitCodeProcess(handle, &code); err != nil {
		return false
	}
	// STILL_ACTIVE (259) = процес не завершився.
	return code == 259
}
