// Пакет childenv — підготовка ЧИСТОГО середовища для дочірніх процесів
// (гра, xdg-open, файловий менеджер, pre/post-launch команди).
//
// ЧОМУ ЦЕ ПОТРІБНО (Linux):
//
//  1. shell при старті свідомо виставляє compat-змінні для WebKitGTK —
//     WEBKIT_DISABLE_COMPOSITING_MODE / WEBKIT_DISABLE_DMABUF_RENDERER /
//     LIBGL_ALWAYS_SOFTWARE (shell/webview_compat_linux.go). Вони потрібні
//     САМОМУ вікну лаунчера в оточеннях без робочого GPU — і категорично
//     НЕ потрібні дочірнім процесам. os.Environ() успадковує їх усі, тож:
//     - Minecraft стартував з LIBGL_ALWAYS_SOFTWARE=1, тобто гра
//     рендерилась програмним llvmpipe замість GPU (кілька FPS);
//     - системний браузер, відкритий для входу в Microsoft, стартував так
//     само на софтверному GL — звідси «браузер вантажиться вічність».
//     Список того, що shell реально виставив САМ, він публікує у змінній
//     SHAURMA_COMPAT_ENV (кома-розділені імена) — прибираємо рівно їх,
//     не чіпаючи те, що користувач експортував свідомо.
//
//  2. AppImage (linuxdeploy AppRun) підмінює LD_LIBRARY_PATH, GTK_PATH,
//     GIO_MODULE_DIR тощо на теки всередині бандла. Дочірній процес із
//     таким середовищем підхоплює НАШІ бібліотеки замість системних —
//     браузер/файловий менеджер або довго стартують, або падають мовчки.
//     Прибираємо змінні бандла (APPDIR-шляхи), відновлюючи оригінали з
//     APPIMAGE_ORIGINAL_* там, де AppRun їх зберіг.
//
// На Windows/macOS усе це — no-op: SHAURMA_COMPAT_ENV не виставлений,
// APPDIR не існує, жодна змінна зі списку не зустрічається.
package childenv

import (
	"os"
	"strings"
)

// CompatEnvMarker — ім'я змінної, у якій shell перелічує compat-змінні,
// що він виставив САМ (див. shell/webview_compat_linux.go). Рядок має
// бути ідентичним у shell — це контракт між двома модулями.
const CompatEnvMarker = "SHAURMA_COMPAT_ENV"

// bundleVars — змінні, які повністю належать AppImage/Flatpak-бандлу і
// не мають сенсу поза ним. Видаляються цілком (або відновлюються з
// APPIMAGE_ORIGINAL_*), якщо процес реально запущено з бандла.
var bundleVars = []string{
	"LD_LIBRARY_PATH",
	"LD_PRELOAD",
	"GTK_PATH",
	"GTK_EXE_PREFIX",
	"GTK_DATA_PREFIX",
	"GTK_IM_MODULE_FILE",
	"GDK_PIXBUF_MODULE_FILE",
	"GDK_PIXBUF_MODULEDIR",
	"GIO_MODULE_DIR",
	"GSETTINGS_SCHEMA_DIR",
	"GST_PLUGIN_SYSTEM_PATH",
	"GST_PLUGIN_SYSTEM_PATH_1_0",
	"GCONV_PATH",
	"LIBGL_DRIVERS_PATH",
	"QT_PLUGIN_PATH",
	"FONTCONFIG_FILE",
	"FONTCONFIG_PATH",
	"PYTHONHOME",
	"PYTHONPATH",
	"PERLLIB",
	// XDG_DATA_HOME свідомо НЕ чіпаємо: на Linux саме він визначає теку
	// даних лаунчера (exedir_linux.go) і теку даних дочірніх процесів —
	// AppRun його не підміняє, тож видалення лише зламало б шляхи.
}

// pathListVars — змінні-списки шляхів, які НЕ можна видаляти цілком
// (без них дочірній процес не знайде ні бінарників, ні desktop-баз), але
// з яких треба прибрати компоненти, що ведуть усередину бандла.
var pathListVars = []string{"PATH", "XDG_DATA_DIRS", "XDG_CONFIG_DIRS"}

// Clean повертає санітизовану копію поточного середовища процесу —
// зручний скорочений виклик для exec.Cmd.Env.
func Clean() []string {
	return Sanitized(os.Environ())
}

// Sanitized прибирає з переданого середовища compat-змінні лаунчера і
// сліди AppImage-бандла. Вхідний слайс не змінюється.
func Sanitized(environ []string) []string {
	drop := map[string]bool{}

	// 1. Те, що shell виставив сам (і сам маркер).
	for _, name := range strings.Split(getenvFrom(environ, CompatEnvMarker), ",") {
		if name = strings.TrimSpace(name); name != "" {
			drop[name] = true
		}
	}
	if len(drop) > 0 {
		drop[CompatEnvMarker] = true
	}

	// 2. Змінні AppImage-бандла — лише якщо ми справді всередині бандла
	// (APPDIR виставляє runtime AppImage). Поза бандлом ці змінні
	// належать користувачеві/дистрибутиву, і чіпати їх не можна.
	appDir := getenvFrom(environ, "APPDIR")
	restore := map[string]string{}
	if appDir != "" {
		for _, name := range bundleVars {
			if orig := getenvFrom(environ, "APPIMAGE_ORIGINAL_"+name); orig != "" {
				restore[name] = orig
				continue
			}
			drop[name] = true
		}
	}

	out := make([]string, 0, len(environ)+len(restore))
	for _, e := range environ {
		idx := strings.IndexByte(e, '=')
		if idx < 0 {
			out = append(out, e)
			continue
		}
		name, value := e[:idx], e[idx+1:]
		if drop[name] {
			continue
		}
		if strings.HasPrefix(name, "APPIMAGE_ORIGINAL_") {
			continue
		}
		if v, ok := restore[name]; ok {
			out = append(out, name+"="+v)
			delete(restore, name)
			continue
		}
		if appDir != "" && isPathListVar(name) {
			if cleaned := stripPrefixPaths(value, appDir); cleaned != "" {
				out = append(out, name+"="+cleaned)
			}
			continue
		}
		out = append(out, e)
	}
	// Оригінали, яких не було в самому середовищі (AppRun лишив тільки
	// APPIMAGE_ORIGINAL_*) — додаємо явно.
	for name, value := range restore {
		out = append(out, name+"="+value)
	}
	return out
}

func isPathListVar(name string) bool {
	for _, v := range pathListVars {
		if v == name {
			return true
		}
	}
	return false
}

// stripPrefixPaths прибирає зі списку шляхів (розділених ':') усі
// компоненти, що лежать усередині prefix (теки бандла).
func stripPrefixPaths(value, prefix string) string {
	parts := strings.Split(value, ":")
	kept := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		if p == prefix || strings.HasPrefix(p, prefix+"/") {
			continue
		}
		kept = append(kept, p)
	}
	return strings.Join(kept, ":")
}

func getenvFrom(environ []string, name string) string {
	prefix := name + "="
	for _, e := range environ {
		if strings.HasPrefix(e, prefix) {
			return e[len(prefix):]
		}
	}
	return ""
}
