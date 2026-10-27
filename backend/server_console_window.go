package main

import (
	"encoding/json"
	"net/url"
	"sync"
)

// ── Окреме вікно консолі СЕРВЕРА ─────────────────────────────────────────
//
// 1:1 та сама схема, що для консолей збірок (console_window.go): backend
// лише ВИРІШУЄ «яке вікно для якого сервера», а саме вікно створює shell
// за стабільним рядковим ID (bridgeWindowCreate) і він же шле подію
// "window:closing:<id>", за якою backend відповідає hide/close
// (onShellWindowClosing — там уже generic-логіка для ВСІХ не-головних
// вікон, тож тут нічого додавати не треба).
//
// Навіщо окреме вікно: консоль сервера всередині вкладки ділить екран із
// банерами/вкладками, а тривалий лог хочеться бачити на весь екран (і на
// другому моніторі) — рівно як у збірках, де є кнопка «відкрити у вікні».
// Сторінка вікна — той самий console.html, лише з ?server=<id> замість
// ?build=<id>: console.ts монтує відповідний standalone-компонент.

var (
	srvConsoleWinMu sync.Mutex
	// serverConsoleWindowIDs — serverID, для яких уже створене вікно.
	serverConsoleWindowIDs map[string]bool
)

// serverConsoleWindowID — стабільний ID вікна для bridge (shell тримає
// handle під цим рядком). Префікс "server-console-" не перетинається з
// "console-<buildID>" у вікон збірок.
func serverConsoleWindowID(serverID string) string {
	return "server-console-" + sanitizeWindowName(serverID)
}

// ShowServerConsoleWindowFor показує окреме вікно консолі КОНКРЕТНОГО
// сервера (створює при потребі). Повторний виклик лише показує/фокусує
// вже створене вікно — лог при цьому писався весь час, бо вікно на
// закриття ховається, а не знищується.
func (a *App) ShowServerConsoleWindowFor(serverID string) {
	if serverID == "" {
		return
	}
	id := serverConsoleWindowID(serverID)

	srvConsoleWinMu.Lock()
	if serverConsoleWindowIDs == nil {
		serverConsoleWindowIDs = map[string]bool{}
	}
	exists := serverConsoleWindowIDs[id]
	serverConsoleWindowIDs[id] = true
	srvConsoleWinMu.Unlock()

	if !exists {
		opts := windowOptions{
			Title:     "Shaurma Launcher — Консоль сервера",
			Width:     1100,
			Height:    760,
			MinWidth:  640,
			MinHeight: 420,
			Frameless: true,
			// server — id сервера, якого показує вікно; win — ТОЧНИЙ id
			// вікна (фронтенд НЕ перекалькулює його сам: JS-репліка
			// sanitizeWindowName не збігається для не-ASCII id, і
			// скопійована подія window:active:<id> ніколи б не співпала
			// зі слухачем).
			URL: consoleWindowURL() + "?server=" + url.QueryEscape(serverID) + "&win=" + url.QueryEscape(id),
		}
		optsJSON, _ := json.Marshal(opts)
		bridgeWindowCreate(id, string(optsJSON))
	}

	bridgeWindowOp(id, "show")
	bridgeWindowOp(id, "unminimise")
	bridgeWindowOp(id, "unmaximise")
	bridgeWindowOp(id, "focus")
}

// OpenServerConsole — кнопка «відкрити у вікні» у вкладці консолі сервера.
func (a *App) OpenServerConsole(serverID string) {
	a.ShowServerConsoleWindowFor(serverID)
}

// HideServerConsoleWindowFor ховає вікно консолі сервера (лог продовжує
// записуватись).
func (a *App) HideServerConsoleWindowFor(serverID string) {
	if serverID == "" {
		return
	}
	bridgeWindowOp(serverConsoleWindowID(serverID), "hide")
}

// ToggleServerConsoleWindow — перемикач для кнопки у вкладці: показує
// вікно, якщо його не видно, і ховає, якщо воно вже на екрані.
func (a *App) ToggleServerConsoleWindow(serverID string) {
	if serverID == "" {
		return
	}
	if a.IsServerConsoleWindowOpen(serverID) {
		a.HideServerConsoleWindowFor(serverID)
	} else {
		a.ShowServerConsoleWindowFor(serverID)
	}
}

// IsServerConsoleWindowOpen повертає, чи видно зараз вікно консолі цього
// сервера (використовується кнопкою-перемикачем).
func (a *App) IsServerConsoleWindowOpen(serverID string) bool {
	if serverID == "" {
		return false
	}
	return bridgeWindowIsVisible(serverConsoleWindowID(serverID))
}

// hideAllServerConsoleWindows ховає ВСІ відкриті вікна консолей серверів
// (виклик при реальному виході з лаунчера — разом із HideConsoleWindow
// для консолей збірок).
func (a *App) hideAllServerConsoleWindows() {
	srvConsoleWinMu.Lock()
	ids := make([]string, 0, len(serverConsoleWindowIDs))
	for id := range serverConsoleWindowIDs {
		ids = append(ids, id)
	}
	srvConsoleWinMu.Unlock()
	for _, id := range ids {
		bridgeWindowOp(id, "hide")
	}
}
