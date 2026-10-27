//go:build !windows

package main

// shortcuts_other.go — Linux/інші-не-Windows реалізація трьох функцій,
// винесених з shortcuts.go. Поведінка НЕ змінена порівняно з тим, що
// було раніше (runtime.GOOS != "windows" повертав ту саму помилку) —
// просто тепер це живе у власному build-tag файлі, а не поруч з
// golang.org/x/sys/windows.
//
// Свідомо НЕ додано .desktop-based реалізацію ярликів на робочому столі:
// це самостійна фіча (потребує рішення, який CLI-виклик генерує сам
// .desktop, куди саме на Deck/GNOME/KDE він має лягти — ~/.local/share/
// applications/ чи ~/Desktop/, чи обидва — і чи взагалі показувати цю
// кнопку в UI на Linux), якої немає в плані підтримки Linux. Публічні
// App-методи (CreateDesktopShortcut тощо, в shortcuts.go) лишаються
// доступні фронтенду на всіх платформах і повертають цю ж помилку.
import "errors"

var errShortcutsWindowsOnly = errors.New("ярлики підтримуються лише на Windows")

// desktopDir — на Linux ярлики робочого столу поки не реалізовані.
func desktopDir() (string, error) {
	return "", errShortcutsWindowsOnly
}

// createLNK — на Linux немає .lnk; еквівалент був би .desktop-файл,
// поки не реалізовано (див. коментар вище).
func createLNK(lnkPath, exePath, args, workingDir, iconLocation, description string) error {
	return errShortcutsWindowsOnly
}

// startMenuDir — на Linux немає єдиного "меню Пуск"; DE-специфічні
// способи (~/.local/share/applications) поки не реалізовані.
func startMenuDir() (string, error) {
	return "", errShortcutsWindowsOnly
}
