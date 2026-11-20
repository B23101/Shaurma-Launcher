package main

// social_idle_shaurma.go — статус «відійшов» за простоєм ПК, а не за вікном лаунчера.
//
// «Відійшов» = у системі 5 хвилин немає вводу (миша/клавіатура), тобто людина не за комп'ютером.
// Поки вона користується ПК (будь-яка програма чи гра), друзі бачать «в мережі», навіть якщо вікно
// лаунчера неактивне. Простій міряє systemIdleDuration (Windows, GetLastInputInfo). Де його не
// визначити (не Windows) — лишається попередня поведінка за неактивним вікном (socialWindowActive).

import (
	"context"
	"time"

	"shaurma-launcher-backend/internal/crashguard"
)

const (
	// socialAwayAfter — скільки ПК має бути без вводу, щоб друзі побачили «відійшов».
	socialAwayAfter = 5 * time.Minute
	// socialIdlePoll — частота перевірки. GetLastInputInfo коштує мікросекунди; 5 с — це і є
	// найбільша затримка повернення статусу «в мережі» після першого руху мишею.
	socialIdlePoll = 5 * time.Second
)

// socialIdleFn — джерело простою (підміняється в тестах).
var socialIdleFn = systemIdleDuration

// socialIdleSupported — чи вміє ця платформа міряти простій ПК.
func socialIdleSupported() bool {
	_, ok := socialIdleFn()
	return ok
}

// startSocialIdleWatcher запускає фонову перевірку простою до завершення контексту застосунку.
// Без підтримки платформи одразу виходить.
func (a *App) startSocialIdleWatcher() {
	if !socialIdleSupported() {
		return
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	go func() {
		defer crashguard.Recover("socialIdleWatcher")
		t := time.NewTicker(socialIdlePoll)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				a.socialIdleTick()
			}
		}
	}()
}

// socialIdleTick — один крок перевірки (окремо для тестів).
func (a *App) socialIdleTick() {
	idle, ok := socialIdleFn()
	if !ok {
		return
	}
	if svc := currentSocialService(); svc != nil {
		svc.SetUserIdle(idle >= socialAwayAfter)
	}
}
