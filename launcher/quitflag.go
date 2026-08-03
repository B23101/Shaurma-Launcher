package main

import (
	"os"
	"path/filepath"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// quitFlagName — файл-прапорець м'якого завершення: shrm-updater створює
// його в теці конфігурації (%LOCALAPPDATA%\ShaurmLauncher — це та сама
// тека, де стоїть launcher.exe) перед тим, як оновити лаунчер.
//
// Раніше (аудит, п. 5): апдейтер робив taskkill /F — безумовний форс-кілл,
// який не дає процесу жодного шансу виконати PauseAll/WaitAll синхронізацій:
// .part-файли обривались посеред чанка, стан не зберігався. Тепер лаунчер
// сам помічає прапорець і виконує той самий м'який вихід, що й «Вийти» в
// треї.
const quitFlagName = "shrm-quit.flag"

// quitFlagFreshness — прапорець вважається «свіжим» лише 10 секунд.
// Старий залишок від краху (лаунчер впав після запису, але до видалення)
// не має зупиняти наступний запуск.
const quitFlagFreshness = 10 * time.Second

// startQuitFlagWatcher запускає полінг прапорця м'якого завершення.
// Викликається з ServiceStartup.
func (a *App) startQuitFlagWatcher() {
	path := filepath.Join(a.cfg.ConfigDir(), quitFlagName)
	// Прапорець, написаний ДО старту цього процесу, — завжди залишок:
	// апдейтер пише його лише коли лаунчер УЖЕ працює. Перевірка за mtime
	// обов'язкова: цикл «записав → почекав 6с → taskkill → встановив нову
	// версію → запустив» може вкластися у 10 секунд, і щойно запущений
	// лаунчер міг би прийняти старий прапорець за запит на вихід і закритись
	// одразу після оновлення.
	startTime := time.Now()
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			fi, err := os.Stat(path)
			if err != nil {
				continue
			}
			os.Remove(path)
			// Прапорець, старіший за запуск цього процесу або за 10с —
			// залишок від краху/попередньої версії, ігноруємо.
			if fi.ModTime().Before(startTime) || time.Since(fi.ModTime()) > quitFlagFreshness {
				continue
			}
			// М'який вихід: НЕ a.window.Close() (той проходить через
			// WindowClosing-hook і при запущеній грі лише ховає вікно в
			// трей), а прямий Quit — апдейтер чекає саме завершення процесу.
			consoleAppQuitting.Store(true)
			if a.syncMgr != nil {
				a.syncMgr.PauseAll()
			}
			if app := application.Get(); app != nil {
				app.Quit()
			}
			return
		}
	}()
}
