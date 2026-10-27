//go:build linux

// Package autostart (Linux) — «Запускати при старті системи» через
// freedesktop.org XDG Autostart: .desktop-файл у ~/.config/autostart/.
// Це прямий аналог Windows Run-ключа (HKCU\...\Run) з Частини 2.4 плану
// — той самий механізм, яким користуються Discord/Steam на Linux: жодного
// systemd user-сервісу чи root-прав не потрібно, файл діє лише для
// поточного користувача і читається сесією стільниці (GNOME/KDE/...) при
// вході.
package autostart

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// desktopFileName — стабільне ім'я файлу, під яким лаунчер себе реєструє.
// Не залежить від мови/локалізації, щоб Enable/Disable завжди знаходили
// саме свій запис і не плодили дублікати (аналог valueName на Windows).
const desktopFileName = "shaurma-launcher-autostart.desktop"

// insideFlatpak — чи процес працює в Flatpak-пісочниці. Маркер
// /.flatpak-info створює сам flatpak (той самий детект, що в shell:
// flatpak_seed_linux.go:runningInsideFlatpak), FLATPAK_ID — офіційна
// змінна середовища застосунку.
func insideFlatpak() bool {
	if _, err := os.Stat("/.flatpak-info"); err == nil {
		return true
	}
	return os.Getenv("FLATPAK_ID") != ""
}

// flatpakAppID повертає app-id поточного Flatpak-застосунку
// (com.shaurma.Launcher / com.shaurma.LauncherBase) або "", якщо процес
// запущено не через Flatpak. Потрібен, щоб записати в ~/.config/autostart
// хоста команду `flatpak run <app-id>`: шлях /app/bin/ShaurmLauncher
// існує ЛИШЕ всередині пісочниці й на хості не запуститься.
func flatpakAppID() string {
	if id := os.Getenv("FLATPAK_ID"); id != "" {
		return id
	}
	// Фолбек: ім'я застосунку лежить у самому /.flatpak-info
	// ([Application] name=com.shaurma.Launcher).
	data, err := os.ReadFile("/.flatpak-info")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if v, ok := strings.CutPrefix(line, "name="); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// autostartDir повертає ~/.config/autostart, з урахуванням XDG_CONFIG_HOME
// якщо він явно заданий (стандартна XDG Base Directory поведінка).
//
// ВАЖЛИВО (Flatpak): у пісочниці XDG_CONFIG_HOME примусово вказує на
// ~/.var/app/<app-id>/config, і записаний туди .desktop хост НЕ читає —
// перемикач «Запускати при старті» нічого не робив. Тому всередині
// Flatpak XDG_CONFIG_HOME свідомо ІГНОРУЄТЬСЯ і використовується справжній
// ~/.config/autostart хоста — він доступний завдяки дозволу
// --filesystem=xdg-config/autostart:create у flatpak/com.shaurma.*.yml.
func autostartDir() (string, error) {
	if !insideFlatpak() {
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			return filepath.Join(xdg, "autostart"), nil
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "autostart"), nil
}

// Set вмикає або вимикає автозапуск лаунчера з Linux-сесії. exePath —
// повний шлях до виконуваного файлу shell (ELF без розширення, аналог
// shell.exe). Порожній exePath — тиха no-op (немає що реєструвати),
// той самий контракт, що й у Windows-версії.
//
// args — аргументи автозапуску (напр. --background: старт без вікна).
// Файл ПЕРЕЗАПИСУЄТЬСЯ при кожному виклику, тож зміна режиму одразу
// потрапляє в .desktop.
func Set(enabled bool, exePath string, args ...string) error {
	if exePath == "" {
		return nil
	}
	dir, err := autostartDir()
	if err != nil {
		return fmt.Errorf("autostart dir: %w", err)
	}
	path := filepath.Join(dir, desktopFileName)

	if !enabled {
		// Видалення відсутнього файлу — не помилка для викликача (той
		// самий контракт, що й DeleteValue на відсутньому значенні
		// реєстру у Windows-версії: вимикання того, чого й так не було,
		// не повинне падати).
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("autostart remove: %w", err)
		}
		return nil
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("autostart mkdir: %w", err)
	}

	// Exec без лапок навколо шляху: .desktop Exec= не потребує лапок для
	// пробілів так, як Windows CLI (парситься через g_shell_parse_argv,
	// пробіли в самому шляху — рідкісний edge-case на Linux, де шляхи
	// зазвичай без пробілів; додавання лапок тут ризикованіше, бо деякі
	// DE неправильно їх розбирають). %U лишається порожнім — лаунчер не
	// приймає файлові аргументи при автозапуску.
	//
	// У Flatpak-пісочниці exePath (напр. /app/bin/ShaurmLauncher) на хості
	// НЕ існує, тому замість нього пишемо `flatpak run <app-id>` — інакше
	// автозапуск на хості падав би з «файл не знайдено».
	exec := execLine(exePath, args)
	if appID := flatpakAppID(); appID != "" {
		exec = execLine("flatpak", append([]string{"run", appID}, args...))
	}
	content := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=Shaurma Launcher
Exec=%s
X-GNOME-Autostart-enabled=true
NoDisplay=true
Terminal=false
`, exec)

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("autostart write: %w", err)
	}
	return nil
}

// IsEnabled повертає поточний стан автозапуску — перевіряється сама
// наявність файлу в ~/.config/autostart/ (той самий принцип звірки з
// фактичним станом системи, а не лише з settings.json, що й на Windows:
// якщо користувач вручну прибрав файл, лаунчер не повинен брехати, що
// автозапуск усе ще активний).
func IsEnabled() bool {
	dir, err := autostartDir()
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(dir, desktopFileName))
	return err == nil
}
