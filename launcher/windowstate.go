package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"shaurma-launcher-wails/internal/atomicfile"
)

// ── Персистенція стану головного вікна ──────────────────────────────────
// Розмір, максимізація та повноекранний режим вікна лаунчера зберігаються
// у window-state.json (в теці конфіга) і відновлюються при наступному
// запуску — інакше щоразу доводиться налаштовувати вікно заново.
//
// Збереження:
//   - хуки подій вікна (resize/move/maximise/fullscreen) оновлюють
//     in-memory стан і планують відкладений запис (debounce ~600мс);
//   - при штатному завершенні (OnShutdown) стан записується ще раз
//     безумовно — навіть якщо останній resize не встиг зберегтись.
//
// Відновлення: main.go застосовує збережені розмір/стан одразу після
// створення вікна, ДО показу — без «стрибка» розміру на екрані.

type windowState struct {
	Width      int  `json:"width"`
	Height     int  `json:"height"`
	Maximised  bool `json:"maximised"`
	Fullscreen bool `json:"fullscreen"`
}

// Мінімальні розміри вікна (мають збігатися з WebviewWindowOptions.MinWidth
// /MinHeight у main.go) — збережені розміри менші за них ігноруємо, щоб
// не створювати вікно, менше за мінімум.
const winMinW, winMinH = 960, 640

var (
	winStateMu    sync.Mutex
	winStateLast  windowState
	winStateTimer *time.Timer
)

func (a *App) windowStatePath() string {
	return filepath.Join(a.cfg.Dir(), "window-state.json")
}

func (a *App) loadWindowState() windowState {
	data, err := os.ReadFile(a.windowStatePath())
	if err != nil {
		return windowState{Width: 1280, Height: 820}
	}
	var st windowState
	if json.Unmarshal(data, &st) != nil || st.Width < winMinW || st.Height < winMinH {
		return windowState{Width: 1280, Height: 820}
	}
	return st
}

// captureWindowState зчитує поточний стан вікна в пам'ять. Викликається з
// хук-ів подій (на UI-потоці) і з OnShutdown.
func (a *App) captureWindowState() {
	if a.window == nil {
		return
	}
	w, h := a.window.Size()
	winStateMu.Lock()
	winStateLast = windowState{
		Width:      w,
		Height:     h,
		Maximised:  a.window.IsMaximised(),
		Fullscreen: a.window.IsFullscreen(),
	}
	winStateMu.Unlock()
}

// saveWindowStateNow пише in-memory стан на диск (atomic tmp+rename).
func (a *App) saveWindowStateNow() {
	winStateMu.Lock()
	st := winStateLast
	winStateMu.Unlock()
	if st.Width == 0 {
		return
	}
	_ = atomicfile.WriteJSONAtomic(a.windowStatePath(), st)
}

// scheduleWindowStateSave — відкладений запис (resize шле багато подій).
func (a *App) scheduleWindowStateSave() {
	winStateMu.Lock()
	if winStateTimer != nil {
		winStateTimer.Stop()
	}
	winStateTimer = time.AfterFunc(600*time.Millisecond, func() {
		a.captureWindowState()
		a.saveWindowStateNow()
	})
	winStateMu.Unlock()
}

// setupWindowStatePersistence реєструє хуки подій вікна, що відстежують
// зміни розміру/позиції/стану. Викликається з main.go після створення вікна.
func (a *App) setupWindowStatePersistence(win *application.WebviewWindow) {
	if win == nil {
		return
	}
	hook := func(*application.WindowEvent) { a.scheduleWindowStateSave() }
	win.RegisterHook(events.Common.WindowDidResize, hook)
	win.RegisterHook(events.Common.WindowDidMove, hook)
	win.RegisterHook(events.Common.WindowMaximise, hook)
	win.RegisterHook(events.Common.WindowUnMaximise, hook)
	win.RegisterHook(events.Common.WindowFullscreen, hook)
	win.RegisterHook(events.Common.WindowUnFullscreen, hook)

	// Відновлення збереженого стану: коли webview готовий (нативне вікно вже
	// створено) — застосовуємо розмір/максимізацію/повноекранний режим.
	win.RegisterHook(events.Common.WindowRuntimeReady, func(*application.WindowEvent) {
		st := a.loadWindowState()
		if st.Width >= winMinW && st.Height >= winMinH {
			win.SetSize(st.Width, st.Height)
		}
		if st.Fullscreen {
			win.Fullscreen()
		} else if st.Maximised {
			win.Maximise()
		}
	})
}

// saveWindowStateOnExit — фінальне збереження перед завершенням процесу
// (OnShutdown). Вікно ще існує, тож читаємо реальні розміри напряму.
func (a *App) saveWindowStateOnExit() {
	a.captureWindowState()
	a.saveWindowStateNow()
}
