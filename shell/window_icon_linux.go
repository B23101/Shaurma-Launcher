//go:build linux

package main

import (
	"github.com/wailsapp/wails/v3/pkg/application"
)

// setLauncherWindowIcon — на Linux немає прямого еквіваленту Win32
// WM_SETICON per-вікно. Іконка вікна/таскбару/пошуку застосунків на
// GTK-стеку (KDE Plasma/GNOME, включно з SteamOS/Deck) визначається
// ІНШИМ, ніж на Windows, механізмом — покритим поза цим файлом:
//   - трей: shell/tray_exec.go передає PNG (iconB64) напряму в
//     application.SystemTray, це не залежить ні від .desktop, ні від App;
//   - вікно/таскбар/пошук програм: на GTK4 (дефолтна збірка цього
//     проєкту — див. app_icon.go) немає жодного програмного API для
//     встановлення іконки вікна чи застосунку — gtk_window_set_icon
//     прибраний у GTK4, і App.SetIcon() у app_icon.go для цього шляху
//     є no-op. Оболонка (Wayland/X11) сама зіставляє вікно з
//     .desktop-файлом за application-id/WM_CLASS і бере Icon= ЗВІДТИ —
//     цей .desktop генерується launcherbuild під час пакування, а не
//     самим shell (shell/linux/desktop — лише шаблон; реальне ім'я
//     файлу рахує desktopid.go, воно ж = application-id з
//     app_identity.go). Тобто іконка таскбара на Linux залежить не від
//     жодного виклику в цьому процесі, а від того, чи application-id
//     запущеного вікна збігається з іменем встановленого .desktop.
//
// Функція лишається no-op (а не видаляється), щоб window_exec.go
// (спільний для всіх платформ файл) не потребував build tag навколо
// виклику setLauncherWindowIcon(win) — на Linux встановлювати
// per-вікно/per-застосунок іконку програмно взагалі нема чим.
func setLauncherWindowIcon(win *application.WebviewWindow) {
	_ = win
}
