package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

// appicon.png вшивається напряму в бінарник через embed — це іконка
// трею (tray.SetIcon у tray.go). Іконка самого вікна/панелі задач на
// Windows береться з .ico, вшитого в EXE-ресурси на етапі збірки
// (build/windows/icon.ico через `wails3 generate syso`) — тому
// build/windows/icon.ico обов'язково має існувати, інакше вікно отримає
// системну заглушку.
//
//go:embed build/appicon.png
var iconPNG []byte

// appVersion — версія лаунчера (фолбек, коли launcher-version.json поруч
// з exe недоступний — напр. dev-збірка в build\bin). Основний шлях —
// читати версію з launcher-version.json при старті (build.ps1 кладе його
// поруч з launcher.exe), щоб версіонована тека WebView2-профілю не
// вимагала ручної синхронізації з -Version у build.ps1.
const appVersion = "2.0.0"

// readLauncherVersion читає version з launcher-version.json, що лежить
// поруч з виконуваним файлом (build.ps1 кладе його в staging і
// installer — в installDir). При відсутності/помилці повертає фолбек
// appVersion — так версіонований WebView2-профіль завжди свіжий після
// кожного оновлення без ручного супроводу.
func readLauncherVersion() string {
	exe, err := os.Executable()
	if err != nil {
		return appVersion
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(exe), "launcher-version.json"))
	if err != nil {
		return appVersion
	}
	// build.ps1/installer пишуть файл з UTF-8 BOM (U+FEFF), а Go
	// json.Unmarshal на BOM падає — прибираємо префікс до парсингу,
	// інакше версія завжди йшла б у фолбек і профіль не оновлювався б.
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	var m struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(data, &m) != nil || m.Version == "" {
		return appVersion
	}
	return m.Version
}

// webviewUserDataPath — тека профілю WebView2 лаунчера. Замість дефолту
// Wails (%APPDATA%\<exe-ім'я>\EBWebView), який НЕ змінюється між версіями
// і тримає закешований старий index.html попередніх білдів (звідси
// «порожні» розділи налаштувань і зниклі іконки акаунтів після
// оновлення), — використовуємо стабільну теку
// %LOCALAPPDATA%\<InstallDir>\webview2\v<версія>. <InstallDir> береться з
// імені теки, де лежить exe (ShaurmLauncher чи ShaurmLauncherBase), щоб
// дві версії лаунчера (shaurma/base) не ділили один браузерний профіль.
// Версія в шляху гарантує чистий профіль після кожного оновлення.
func webviewUserDataPath() string {
	installName := "ShaurmLauncher"
	if exe, err := os.Executable(); err == nil {
		if base := filepath.Base(filepath.Dir(exe)); base != "." && base != string(filepath.Separator) && base != "" {
			installName = base
		}
	}
	version := readLauncherVersion()
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		return filepath.Join(local, installName, "webview2", "v"+version)
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, installName+"-webview2", "v"+version)
}

func main() {
	app := NewApp()

	launcherApp := application.New(application.Options{
		Name:        "Shaurma Launcher",
		Description: "Shaurma Launcher",
		Services: []application.Service{
			application.NewService(app),
		},
		// Іконка вікна/панелі задач під час роботи на Windows береться
		// з EXE-ресурсу (build/windows/icon.ico, вшивається на етапі
		// збірки через `wails3 generate syso`).
		Assets: application.AssetOptions{
			Handler: application.BundledAssetFileServer(assets),
			// НЕ кешувати вбудований фронтенд взагалі: WebView2 має кожен
			// запуск віддавати саме ті файли, що вшиті в поточний
			// бінарник. Без цього заголовка WebView2 міг віддавати старий
			// index.html, який посилається на старі hashed-ассети, яких
			// уже немає — а це і є «побитий» UI після оновлення.
			Middleware: func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
					next.ServeHTTP(w, r)
				})
			},
		},
		Windows: application.WindowsOptions{
			// Явна версіонована тека профілю WebView2 (див.
			// webviewUserDataPath). Дефолт Wails (%APPDATA%\<exe-ім'я>)
			// кешує старий фронтенд попередніх версій, і після оновлення
			// лаунчер показував закешований старий index.html. Тепер кожна
			// версія отримує свіжий профіль і це неможливо.
			WebviewUserDataPath: webviewUserDataPath(),
		},
		// OnShutdown: коли процес реально завершується (гра не запущена,
		// або користувач обрав «Вийти» в треї) — прибираємо трей.
		OnShutdown: app.shutdown,
	})

	app.window = launcherApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:          "Shaurma Launcher",
		Width:          1280,
		Height:         820,
		MinWidth:       960,
		MinHeight:      640,
		Frameless:      true,
		BackgroundType: application.BackgroundTypeTransparent,
	})

	// Закриття вікна «у трей»: замість стандартного закриття (яке вбиває
	// процес і будь-яку активну гру разом з ним) перевіряємо, чи гра
	// запущена. Якщо так — скасовуємо закриття (event.Cancel), ховаємо
	// вікно і лишаємо процес живим у треї. Якщо гра не запущена —
	// закриваємось як завжди.
	app.setupWindowCloseHook(app.window)

	// Розмір/стан вікна (розмір, максимізація, повноекранний режим)
	// зберігаються у window-state.json і відновлюються при наступному
	// запуску. Застосування виконується по події WindowRuntimeReady (коли
	// webview вже створено, але ще не показано контент) — на цьому етапі
	// нативне вікно вже існує, тож SetSize/Maximise гарантовано спрацюють.
	app.setupWindowStatePersistence(app.window)

	if err := launcherApp.Run(); err != nil {
		println("Error:", err.Error())
	}
}
