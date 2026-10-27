package main

import (
	"shaurma-launcher-backend/internal/crashguard"
	"net/http"
	"time"
)

// ── Статус мережі (network:status) ───────────────────────────────────────
// Фоновий пінгер, який кожні networkStatusInterval секунд робить легкий
// запит до надійного хоста і шле фронтенду подію network:status
// {"online": true|false}. Фронтенд показує позначку «офлайн» під назвою
// лаунчера на головній сторінці, коли інтернету немає — той самий підхід,
// що Net::Mode у Prism (один спільний сигнал замість того, щоб кожне
// мережеве місце лаунчера окремо вгадувало, чому впало).
//
// Точка перевірки — Modrinth API (api.modrinth.com): вона не вимагає
// токенів, має високий аптайм і при цьому потрібна лаунчеру для модів.
// Mojang launchermeta теж варіант, але Modrinth стабільніше тримає HEAD.

const (
	networkStatusInterval = 20 * time.Second
	networkCheckTimeout   = 4 * time.Second
	networkCheckURL       = "https://api.modrinth.com/v2"
)

// checkNetwork робить один легкий HEAD-запит і повертає, чи інтернет є.
// Помилки DNS/тайм-ауту/з'єднання → false; будь-який HTTP-відгук (навіть
// 4xx/5xx) → true, бо сервер досяжний — це не «немає мережі».
func checkNetwork() bool {
	client := &http.Client{Timeout: networkCheckTimeout}
	req, err := http.NewRequest(http.MethodHead, networkCheckURL, nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", "ShaurmaLauncher/2.0 (+netcheck)")
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return true
}

// startNetworkStatusLoop запускає фоновий пінгер, що шле network:status при
// кожній ЗМІНІ стану (а не на кожен тік — щоб не спамити фронтенд) і один
// раз одразу при старті. Викликається з ServiceStartup.
func (a *App) startNetworkStatusLoop() {
	go func() {
		defer crashguard.Recover("network_status.go:48")
		// Перший тік одразу: фронтенд підписується на подію пізніше за нас.
		last := -1 // -1 = стану ще не було, шлемо завжди
		check := func() {
			online := checkNetwork()
			cur := 0
			if online {
				cur = 1
			}
			if cur != last {
				last = cur
				a.emit("network:status", map[string]bool{"online": online})
			}
		}
		check()
		ticker := time.NewTicker(networkStatusInterval)
		defer ticker.Stop()
		for range ticker.C {
			check()
		}
	}()
}

// CheckNetwork — одноразова перевірка стану мережі на вимогу (фронтенд
// може перепитати після відновлення підключення, не чекаючи наступного
// тіку пінгера).
func (a *App) CheckNetwork() bool {
	return checkNetwork()
}
