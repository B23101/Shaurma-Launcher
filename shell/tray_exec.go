package main

// tray_exec.go — виконавча частина трею. Shell отримує iconB64 + tooltip +
// menuJSON (бізнес-логіку формує backend у tray_menu.go) і будує реальний
// Wails SystemTray. На клік пункту шле "tray:click:<id>" назад у backend.
//
// ВИПРАВЛЕНО (збірка падала: «app.NewSystemTray undefined»):
//
// У wails v3, який пінить shell/go.mod (v3.0.0-alpha2.121, та сама
// версія у wailsCliVersion), конструктор трею — app.SystemTray.New():
// App має поле SystemTray *SystemTrayManager (application.go), а його
// метод New() реєструє трей (system_tray_manager.go). Методу
// App.NewSystemTray() немає ні в цій версії, ні в beta.14 — попередня
// правка файлу спиралась на неіснуючу сигнатуру і тому ламала
// `go build`; повертаємо робочу.
//
// SystemTrayManager.New() реєструє трей через app.runOrDeferToAppRun:
// якщо застосунок уже запущений — SystemTray.Run() викликається одразу,
// інакше він виконується на старті App.Run() (pendingRun). Тому ОКРЕМО
// кликати tray.Run() НЕ можна: повторний Run() створює другий
// platform-impl (s.impl = newSystemTrayImpl(s)) і в панелі лишається
// зайва іконка/пункт. Сеттери нижче або застосовуються до вже
// створеного impl (SetLabel/SetIcon/SetMenu викликають impl.setX), або
// лягають у поля SystemTray{}, з яких їх читає newSystemTrayImpl — обидва
// порядки коректні.
//
// SetLabel. На Linux Wails пише цей рядок у Title StatusNotifierItem, і
// саме Title показує панель як підказку (setTooltip на Linux — no-op:
// systemtray_linux.go). Без SetLabel підказка лишалась дефолтною «Wails»
// — саме її підставляє newSystemTrayImpl, коли label порожній. Беремо
// текст, який backend прислав як tooltip («Shaurma Launcher — гра
// запущена»), з фолбеком на назву редакції.

import (
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
)

var tray *application.SystemTray

// traySet створює/оновлює трей (повторний виклик перестворює меню).
func traySet(iconB64, tooltip, menuJSON string) {
	app := application.Get()
	if app == nil {
		return
	}
	if tray != nil {
		tray.Destroy()
		tray = nil
	}
	icon, err := base64.StdEncoding.DecodeString(iconB64)
	if err != nil {
		return
	}
	var items []struct {
		ID        string `json:"id"`
		Label     string `json:"label"`
		Separator bool   `json:"separator"`
	}
	_ = json.Unmarshal([]byte(menuJSON), &items)

	// app.SystemTray.New() — конструктор трею в цій версії Wails
	// (App.SystemTray *SystemTrayManager → system_tray_manager.go).
	tray = app.SystemTray.New()
	// Підпис (Title) — одразу після створення: якщо impl уже є, сигнал
	// піде живим (NewTitle), інакше значення прочитає newSystemTrayImpl.
	tray.SetLabel(trayLabel(tooltip))
	tray.SetIcon(icon)
	tray.SetTooltip(tooltip)

	menu := application.NewMenu()
	for _, it := range items {
		if it.Separator {
			menu.AddSeparator()
			continue
		}
		id := it.ID
		menu.Add(it.Label).OnClick(func(*application.Context) {
			backendDispatch("tray:click:"+id, "")
		})
	}
	tray.SetMenu(menu)
	// Клік по самій іконці = «показати лаунчер» (як tray.OnClick в оригіналі).
	tray.OnClick(func() {
		backendDispatch("tray:click:show", "")
	})
	// tray.Run() тут НЕ викликаємо: SystemTrayManager.New() уже
	// зареєстрував трей через runOrDeferToAppRun (одразу, якщо застосунок
	// запущений, інакше — на App.Run). Другий Run() створив би ще один
	// platform-impl і зайву іконку в панелі (див. коментар угорі файлу).
}

// trayLabel — підпис трею. Backend формує tooltip виду
// «Shaurma Launcher — гра запущена»; цей рядок — єдине, що на Linux
// взагалі видно як підказку панелі (Title SNI). Якщо backend прислав
// порожній рядок — беремо назву редакції, а не дефолт Wails.
func trayLabel(tooltip string) string {
	if s := strings.TrimSpace(tooltip); s != "" {
		return s
	}
	return launcherDisplayName()
}

func trayDestroy() {
	if tray != nil {
		tray.Destroy()
		tray = nil
	}
}
