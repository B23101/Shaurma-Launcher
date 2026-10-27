//go:build linux

package main

// browser_open_linux.go — відкриття зовнішнього URL у системному браузері.
//
// Раніше це був один рядок `exec.Command("xdg-open", url).Start()`, і на
// Linux він давав дві реальні проблеми, найпомітніші саме при вході в
// Microsoft-акаунт («браузер вантажиться вічність / взагалі не
// відкривається»):
//
//  1. СЕРЕДОВИЩЕ. Дочірній процес успадковує os.Environ() лаунчера, а
//     там: наші compat-змінні для WebKitGTK (LIBGL_ALWAYS_SOFTWARE=1 і
//     компанія, див. webview_compat_linux.go) і, якщо лаунчер запущено
//     з AppImage, підмінені AppRun'ом LD_LIBRARY_PATH / GTK_PATH /
//     GIO_MODULE_DIR, що вказують усередину бандла. Тобто Firefox/Chrome
//     стартував з примусовим програмним GL і з НАШИМИ бібліотеками
//     замість системних — звідси і багатосекундний старт, і мовчазні
//     падіння. Тепер середовище санітизується (sanitizedChildEnv).
//
//  2. ЄДИНИЙ КАНДИДАТ. xdg-open є не скрізь (мінімальні образи, Steam
//     Deck у game mode, частина Flatpak-рантаймів). Якщо його нема або
//     він повернув помилку — раніше не відбувалось НІЧОГО і без жодного
//     сліду. Тепер це ланцюжок кандидатів із перевіркою коду виходу.
//
// Плюс дрібне, але важливе: дочірній процес реально «пожинається»
// (cmd.Wait у горутині) — інакше кожен вхід лишав зомбі-процес.

import (
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// openExternalURL відкриває URL у системному браузері користувача.
// Викликається з горутини (див. cgoBrowserOpenURL) — блокуватись тут
// безпечно, UI-потік не задіяний.
func openExternalURL(url string) {
	if url == "" {
		return
	}
	// Мінімальна перевірка схеми: у системний обробник не має потрапити
	// щось, що не є http(s)-посиланням (те саме правило, що в
	// App.OpenExternal на боці backend).
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return
	}

	env := sanitizedChildEnv()
	for _, c := range urlOpenerCandidates(url) {
		if runOpener(c.name, c.args, env) {
			return
		}
	}
}

type openerCandidate struct {
	name string
	args []string
}

// urlOpenerCandidates — порядок спроб відкрити URL. Спершу свідомий
// вибір користувача ($BROWSER), далі стандартні freedesktop-утиліти,
// і лише в кінці — конкретні браузери за іменем (щоб не нав'язувати
// браузер, коли система вміє вирішити це сама).
func urlOpenerCandidates(url string) []openerCandidate {
	var out []openerCandidate

	if b := strings.TrimSpace(os.Getenv("BROWSER")); b != "" {
		// $BROWSER може містити плейсхолдер %s (freedesktop-конвенція)
		// і аргументи: "flatpak run org.mozilla.firefox %s".
		fields := strings.Fields(b)
		if len(fields) > 0 {
			args := make([]string, 0, len(fields))
			replaced := false
			for _, f := range fields[1:] {
				if strings.Contains(f, "%s") {
					args = append(args, strings.ReplaceAll(f, "%s", url))
					replaced = true
					continue
				}
				args = append(args, f)
			}
			if !replaced {
				args = append(args, url)
			}
			out = append(out, openerCandidate{fields[0], args})
		}
	}

	out = append(out,
		openerCandidate{"xdg-open", []string{url}},
		openerCandidate{"gio", []string{"open", url}},
		openerCandidate{"gvfs-open", []string{url}},
		openerCandidate{"x-www-browser", []string{url}},
		openerCandidate{"sensible-browser", []string{url}},
	)

	// Усередині Flatpak xdg-open/gio самі йдуть через портал OpenURI (прав
	// у finish-args не треба). Вихід на хост через `flatpak-spawn --host`
	// навмисно НЕ робимо: він вимагає --talk-name=org.freedesktop.Flatpak,
	// тобто фактично виходу з пісочниці.

	out = append(out,
		openerCandidate{"firefox", []string{url}},
		openerCandidate{"chromium", []string{url}},
		openerCandidate{"chromium-browser", []string{url}},
		openerCandidate{"google-chrome", []string{url}},
		openerCandidate{"google-chrome-stable", []string{url}},
		openerCandidate{"brave-browser", []string{url}},
		openerCandidate{"microsoft-edge", []string{url}},
	)
	return out
}

// runOpener запускає одного кандидата і повідомляє, чи вийшло.
//
// Логіка очікування: xdg-open/gio завершуються швидко і віддають код
// виходу (0 — обробник знайдено і запущено). Конкретний браузер,
// навпаки, живе далі — тому якщо процес ще працює через
// openerSettleDelay, вважаємо запуск успішним.
func runOpener(name string, args []string, env []string) bool {
	path, err := exec.LookPath(name)
	if err != nil {
		return false
	}
	cmd := exec.Command(path, args...)
	cmd.Env = env
	// Домашня тека як робоча: поточною текою процесу може бути тека
	// всередині AppImage-маунта, який зникне після виходу лаунчера.
	if home, err := os.UserHomeDir(); err == nil {
		cmd.Dir = home
	}
	// Окрема сесія: браузер не має померти разом із лаунчером і не має
	// перехоплювати сигнали нашого термінала.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return false
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err == nil
	case <-time.After(openerSettleDelay):
		// Процес живий — це браузер, який лишається працювати.
		// Горутина вище дочекається його і звільнить дескриптори.
		return true
	}
}

const openerSettleDelay = 2 * time.Second

// sanitizedChildEnv — середовище для дочірніх процесів shell'а: без
// compat-змінних, які ми виставили для WebKitGTK, і без слідів
// AppImage-бандла.
//
// Дзеркало internal/childenv у backend.so (shell — окремий Go-модуль і
// не може імпортувати internal-пакети backend'а, тому логіка продубльована
// свідомо; змінювати треба ОБИДВА місця).
func sanitizedChildEnv() []string {
	drop := map[string]bool{}
	for _, name := range strings.Split(os.Getenv(compatEnvMarker), ",") {
		if name = strings.TrimSpace(name); name != "" {
			drop[name] = true
		}
	}
	if len(drop) > 0 {
		drop[compatEnvMarker] = true
	}

	appDir := os.Getenv("APPDIR")
	restore := map[string]string{}
	if appDir != "" {
		for _, name := range appImageBundleVars {
			if orig := os.Getenv("APPIMAGE_ORIGINAL_" + name); orig != "" {
				restore[name] = orig
				continue
			}
			drop[name] = true
		}
	}

	environ := os.Environ()
	out := make([]string, 0, len(environ)+len(restore))
	for _, e := range environ {
		idx := strings.IndexByte(e, '=')
		if idx < 0 {
			out = append(out, e)
			continue
		}
		name, value := e[:idx], e[idx+1:]
		if drop[name] || strings.HasPrefix(name, "APPIMAGE_ORIGINAL_") {
			continue
		}
		if v, ok := restore[name]; ok {
			out = append(out, name+"="+v)
			delete(restore, name)
			continue
		}
		// PATH/XDG_DATA_DIRS видаляти цілком не можна (без них xdg-open
		// не знайде ні бінарників, ні desktop-баз) — прибираємо лише
		// компоненти, що ведуть усередину бандла.
		if appDir != "" && (name == "PATH" || name == "XDG_DATA_DIRS" || name == "XDG_CONFIG_DIRS") {
			if cleaned := stripBundlePaths(value, appDir); cleaned != "" {
				out = append(out, name+"="+cleaned)
			}
			continue
		}
		out = append(out, e)
	}
	for name, value := range restore {
		out = append(out, name+"="+value)
	}
	return out
}

var appImageBundleVars = []string{
	"LD_LIBRARY_PATH", "LD_PRELOAD",
	"GTK_PATH", "GTK_EXE_PREFIX", "GTK_DATA_PREFIX", "GTK_IM_MODULE_FILE",
	"GDK_PIXBUF_MODULE_FILE", "GDK_PIXBUF_MODULEDIR",
	"GIO_MODULE_DIR", "GSETTINGS_SCHEMA_DIR",
	"GST_PLUGIN_SYSTEM_PATH", "GST_PLUGIN_SYSTEM_PATH_1_0",
	"GCONV_PATH", "LIBGL_DRIVERS_PATH", "QT_PLUGIN_PATH",
	"FONTCONFIG_FILE", "FONTCONFIG_PATH",
	"PYTHONHOME", "PYTHONPATH", "PERLLIB",
}

func stripBundlePaths(value, prefix string) string {
	parts := strings.Split(value, ":")
	kept := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" || p == prefix || strings.HasPrefix(p, prefix+"/") {
			continue
		}
		kept = append(kept, p)
	}
	return strings.Join(kept, ":")
}
