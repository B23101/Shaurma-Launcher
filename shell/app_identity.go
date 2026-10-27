package main

// app_identity.go — «особа» застосунку: людяна назва редакції та (для
// Linux) GTK application-id, який водночас є id .desktop-файлу.
//
// НАВІЩО. На Windows іконку процесу визначає exe (ресурс RT_ICON + маніфест),
// тому там достатньо window_icon.go/`wails3 generate syso`. На Linux
// (GTK-стек) усе інакше: GTK4 прибрав per-віконні іконки
// (gtk_window_set_icon/logpixmap видалені, App.SetIcon на GTK4 — no-op,
// див. wails pkg/application/linux_cgo.go: setIcon), і оболонка (GNOME/KDE/
// SteamOS) зіставляє ВІКНО з .desktop-файлом:
//   - Wayland: за application-id (xdg_toplevel.app_id);
//   - X11: за WM_CLASS (матично — StartupWMClass у .desktop).
// Іконку в таскбарі/доку та в таск-менеджері (gnome-system-monitor тощо)
// оболонка бере САМЕ з Icon= знайденого .desktop. Не зіставилось — малюється
// узагальнена «іконка вікна» (той самий баг, який ми виправляємо).
//
// ЩО ДАЄ ЦЕЙ ФАЙЛ. Wails v3 створює GTK-застосунок як
// `org.wails.<sanitizeAppName(Options.Name)>` (application_linux.go: appNew
// + sanitizeAppName) — іншої ручки для application-id у Wails v3 немає:
// префікс "org.wails." і sanitizeAppName зашиті в бібліотеку. Отже єдиний
// спосіб керувати application-id — керувати Options.Name. Тому:
//   1. на Linux Options.Name (appOptionsName() в app_identity_linux.go)
//      залежить від РЕДАКЦІЇ — але це ТЕХНІЧНИЙ токен (linuxAppIDToken*
//      нижче), а НЕ людяна назва: сам Options.Name ніде не рендериться як
//      текст користувачу (людяну назву користувач бачить із Options.
//      Description і з Name= у .desktop-файлі — обидва завжди
//      launcherDisplayName(), однакові для всіх редакцій). Якби Options.
//      Name був спільним, обидві редакції мали б один application-id,
//      базова зіставлялась би з .desktop повної редакції, а два пакети не
//      могли б стояти поруч (один і той самий шлях
//      /usr/share/applications/<id>.desktop) — БЕЗ цієї технічної різниці
//      сценарій із багу («base зіставляється не з тим .desktop») стався б
//      знову, лиш іншим шляхом;
//   2. linuxAppID() нижче рахує ТУ САМУ формулу, що Wails, тож пакувальники
//      (або сам користувач у конфліктному випадку) можуть порахувати,
//      як САМЕ має називатись .desktop-файл.
//
// Контракт із пакувальниками: pack-manager/internal/launcherbuild/paths.go
// (linuxAppIDTokens) має містити ті самі рядки. Зміниш тут — онови там
// (тест shell/app_identity_test.go фіксує очікувані значення, щоб це не
// розповзлося тихо).

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	// launcherDisplayNameShaurma — ЄДИНА людяна назва застосунку, однакова
	// для ВСІХ редакцій. Редакція (shaurma/base) — суто внутрішній канал
	// збірки/оновлень і НЕ повинна бути видимою користувачу ніде: ні в
	// заголовку вікна, ні в треї, ні в диспетчері задач/taskbar/меню
	// "Пуск" Windows чи в аналогах на Linux. Раніше тут існувала окрема
	// launcherDisplayNameBase = "Shaurma Base", яка просочувалась саме
	// туди (через Options.Description і через трей —
	// backend/tray_menu.go) — це й було багом.
	launcherDisplayNameShaurma = "Shaurma Launcher"

	// linuxAppIDTokenShaurma / linuxAppIDTokenBase — НЕ людяні назви, а
	// суто технічні токени для формування GTK application-id. На Linux
	// application-id обов'язково має відрізнятись між редакціями (інакше
	// дві редакції ділили б один .desktop-файл і оболонка плутала б їхні
	// іконки/групування вікон) — але сам токен ніде не показується
	// користувачу як текст, тож він не порушує вимогу «без згадки base/
	// повна в UI».
	linuxAppIDTokenShaurma = "Shaurma Launcher"
	linuxAppIDTokenBase    = "Shaurma Launcher Alt"
)

// launcherDisplayName — людяна назва застосунку, яку бачить користувач
// (заголовок вікна, трей, підписи). Однакова незалежно від редакції — див.
// коментар вище.
func launcherDisplayName() string {
	return launcherDisplayNameShaurma
}

// linuxAppID — GTK application-id цього застосунку; він же — ім'я
// .desktop-файлу без розширення в DEB/RPM/AppImage/Flatpak. Рахується з
// технічного токена редакції (linuxAppIDToken*), а НЕ з людяної назви —
// токен ніде не рендериться як текст, тому дві редакції можуть мати різний
// application-id (потрібно для співіснування на диску), не показуючи
// користувачу жодної згадки "base"/"повна".
func linuxAppID() string {
	token := linuxAppIDTokenShaurma
	if currentChannel() == "base" {
		token = linuxAppIDTokenBase
	}
	return "org.wails." + wailsSanitizeAppName(token)
}

// wailsSanitizeAppName — ТОЧНА копія sanitizeAppName() з wails v3
// (pkg/application/application_linux.go і ..._gtk3.go — обидві гілки
// ідентичні): заміна [^a-zA-Z0-9_-] на "_", префікс "_" для імені, що
// починається з цифри, згортання подвійних "_", обрізання "_" по краях,
// порожній результат → "wailsapp", усе в нижній регістр.
//
// Дублювання свідоме: підняти приватну функцію чужої бібліотеки
// неможливо, а помилитись тут = отримати «іконку вікна» замість іконки
// лаунчера. Тест (app_identity_test.go) тримає формулу прив'язаною до
// очікуваних id.
func wailsSanitizeAppName(name string) string {
	name = wailsInvalidAppNameChars.ReplaceAllString(name, "_")
	name = wailsLeadingDigits.ReplaceAllString(name, "_$0")
	for strings.Contains(name, "__") {
		name = strings.ReplaceAll(name, "__", "_")
	}
	name = strings.Trim(name, "_")
	if name == "" {
		name = "wailsapp"
	}
	return strings.ToLower(name)
}

var (
	wailsInvalidAppNameChars = regexp.MustCompile(`[^a-zA-Z0-9_-]`)
	wailsLeadingDigits       = regexp.MustCompile(`^[0-9]+`)
)

// exeBaseName — ім'я самого бінарника (ShaurmLauncher / ShaurmLauncherBase /
// shaurma-shell). Використовується як Linux.ProgramName: GTK виставляє
// WM_CLASS саме з g_get_prgname(), а .desktop-файли пакетів мають
// StartupWMClass=<ім'я бінарника> — тобто ProgramName і StartupWMClass
// мають лишатись однаковими, інакше на X11 оболонка знову не знайде
// застосунок для вікна. Порожній рядок (не вдалось дізнатись) безпечний:
// Wails тоді не викликає g_set_prgname і GTK сам бере basename argv[0],
// що для всіх наших пакувань той самий рядок.
func exeBaseName() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	base := filepath.Base(exe)
	if base == "." || base == string(filepath.Separator) || base == "" {
		return ""
	}
	return base
}
