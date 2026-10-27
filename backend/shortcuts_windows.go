//go:build windows

package main

// shortcuts_windows.go — три функції, винесені з shortcuts.go (Частина 8,
// крок 1 плану): вони єдині в файлі, що реально імпортують
// golang.org/x/sys/windows, а такий імпорт без build tag ламає
// `GOOS=linux go build` на етапі компіляції — раніше runtime.GOOS-перевірка
// всередині кожної функції рятувала лише від помилкового ВИКОРИСТАННЯ на
// не-Windows, але сам факт імпорту Windows-only пакета в спільному файлі
// не давав файлу взагалі скомпілюватись під Linux. Логіка тут — 1:1
// перенесення з shortcuts.go, без жодних змін.

import (
	"errors"
	"fmt"
	"runtime"

	"golang.org/x/sys/windows"
)

// desktopDir повертає абсолютний шлях до Desktop поточного користувача
// (FOLDERID_Desktop через KnownFolderPath — коректно працює з OneDrive-
// редіректом, коли Desktop реально лежить в іншій теці).
func desktopDir() (string, error) {
	if runtime.GOOS != "windows" {
		return "", errors.New("ярлики підтримуються лише на Windows")
	}
	return windows.KnownFolderPath(windows.FOLDERID_Desktop, 0)
}

// createLNK створює .lnk, що вказує на exePath з аргументом args, робочою
// текою workingDir і іконкою iconLocation ("" = іконка exe за замовчуванням).
// Нативний COM (IShellLinkW) — див. lnk_com_windows.go; прихований PowerShell
// більше не запускається (його антивіруси трактують як ознаку шкідника).
func createLNK(lnkPath, exePath, args, workingDir, iconLocation, description string) error {
	if runtime.GOOS != "windows" {
		return errors.New("ярлики підтримуються лише на Windows")
	}
	if err := createShellLink(lnkPath, exePath, args, workingDir, iconLocation, description); err != nil {
		return fmt.Errorf("createLNK (%s): %w", lnkPath, err)
	}
	return nil
}

// startMenuDir повертає теку меню «Пуск» → Програми поточного користувача
// (FOLDERID_Programs — %APPDATA%\Microsoft\Windows\Start Menu\Programs).
func startMenuDir() (string, error) {
	if runtime.GOOS != "windows" {
		return "", errors.New("ярлики підтримуються лише на Windows")
	}
	return windows.KnownFolderPath(windows.FOLDERID_Programs, 0)
}
