//go:build windows

// Package autostart — «Запускати при старті системи» (налаштування
// лаунчера). Реєструє/знімає shell.exe у стандартному Run-ключі поточного
// користувача: HKCU\Software\Microsoft\Windows\CurrentVersion\Run.
//
// Той самий механізм, яким користуються десятки Windows-застосунків
// (Discord, Steam тощо) для "Start with Windows" — жодного окремого
// сервісу чи задачі Планувальника не потрібно, HKCU не вимагає прав
// адміністратора, і запис діє лише для поточного користувача.
package autostart

import (
	"fmt"

	"golang.org/x/sys/windows/registry"
)

const (
	runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`
	// valueName — ім'я значення в реєстрі під яким лаунчер себе реєструє.
	// Стабільне ім'я (не залежить від мови/локалізації), щоб Enable/
	// Disable завжди знаходили саме свій запис і не плодили дублікати.
	valueName = "ShaurmLauncher"
)

// Set вмикає або вимикає автозапуск лаунчера з Windows. exePath — повний
// шлях до shell.exe (головний виконуваний файл, а не backend.dll).
// args — аргументи командного рядка автозапуску (напр. --background:
// старт без вікна). Запис ПЕРЕЗАПИСУЄТЬСЯ при кожному виклику, тож зміна
// режиму («з вікном» ↔ «у фоні») одразу потрапляє в реєстр.
// Порожній exePath — тиха no-op (немає що реєструвати).
func Set(enabled bool, exePath string, args ...string) error {
	if exePath == "" {
		return nil
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return fmt.Errorf("autostart key: %w", err)
	}
	defer k.Close()

	if !enabled {
		// DeleteValue на відсутньому значенні повертає помилку —
		// ігноруємо (вимикання автозапуску, якого й так не було, не має
		// бути помилкою для викликача).
		_ = k.DeleteValue(valueName)
		return nil
	}
	// Значення в лапках — обов'язково, якщо шлях містить пробіли (типова
	// ситуація: "C:\Program Files\..."), інакше Windows розбиває команду
	// по пробілах при старті сесії.
	if err := k.SetStringValue(valueName, quotedCommand(exePath, args)); err != nil {
		return fmt.Errorf("autostart value: %w", err)
	}
	return nil
}

// IsEnabled повертає поточний стан запису в реєстрі — використовується
// при старті лаунчера для звірки з збереженим у settings.json значенням
// (наприклад, якщо користувач вручну прибрав запис через msconfig/
// Диспетчер задач, налаштування лаунчера не повинне брехати, що
// автозапуск усе ще активний).
func IsEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue(valueName)
	return err == nil
}
