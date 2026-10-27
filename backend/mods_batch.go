package main

import (
	"sync"
	"time"
)

// ── Батчування подій прогресу перевірки модів ────────────────────────────
//
// ПРОБЛЕМА (лаунчер «зависає» — особливо при перемиканні вкладок, коли
// вікно вже було приховане/неактивне): CheckModUpdates у mods_api.go
// раніше слав a.emit("mods:update-progress", ...) і a.emit("mods:update-one",
// ...) НА КОЖЕН мод, прямо в синхронному циклі по всьому списку — для
// збірки на кілька сотень модів, коли більшість результатів уже в кеші
// (типовий випадок — кеш валідний, мережа не потрібна), це сотні emit()
// підряд практично без затримки між ними. Той самий механізм, що описаний
// у console_batch.go: a.emit → bridgeEmit → CGo-виклик у shell, який
// ставить подію в чергу UI-потоку через application.InvokeAsync
// (неблокуюче саме по собі, але черга нічим не обмежена). Поки WebView
// не встигає розібрати чергу — інтерфейс лаунчера не реагує. Це особливо
// помітно, коли вікно було приховане/неактивне (згорнуте в трей під час
// гри, або просто не в фокусі): WebKitGTK притлумлює обробку JS у фоновій
// вкладці (сторінковий Page Visibility throttling — стандартна поведінка
// браузерного рушія, не наш код), тож черга подій росте ще швидше, а
// «розморожування» при поверненні фокусу/перемиканні вкладок виглядає
// саме як зависання на секунду-дві.
//
// ВИПРАВЛЕННЯ: той самий рецепт, що для console:line — події не шлються
// по одній, а складаються в невеликий буфер і скидаються ПАЧКОЮ раз на
// modsBatchInterval (або одразу, якщо пачка вже велика).
const (
	modsBatchInterval = 40 * time.Millisecond
	modsBatchFlushAt  = 200
)

// modsEventBatcher — той самий алгоритм, що consoleLineBatcher
// (console_batch.go), але окремий тип: події тут — це map[string]any
// довільної форми (progress/update-one мають різні поля), не варто
// штучно узагальнювати з рядками консолі заради спільного коду.
type modsEventBatcher struct {
	mu      sync.Mutex
	pending []any
	timer   *time.Timer
	flush   func(batch []any)
}

func newModsEventBatcher(flush func(batch []any)) *modsEventBatcher {
	return &modsEventBatcher{flush: flush}
}

func (b *modsEventBatcher) add(item any) {
	b.mu.Lock()
	b.pending = append(b.pending, item)
	shouldFlushNow := len(b.pending) >= modsBatchFlushAt
	startTimer := len(b.pending) == 1 && !shouldFlushNow
	var timer *time.Timer
	if startTimer {
		timer = time.AfterFunc(modsBatchInterval, b.flushNow)
		b.timer = timer
	}
	var batch []any
	if shouldFlushNow {
		batch = b.pending
		b.pending = nil
		if b.timer != nil {
			b.timer.Stop()
			b.timer = nil
		}
	}
	b.mu.Unlock()
	if shouldFlushNow {
		b.flush(batch)
	}
}

func (b *modsEventBatcher) flushNow() {
	b.mu.Lock()
	batch := b.pending
	b.pending = nil
	b.timer = nil
	b.mu.Unlock()
	if len(batch) > 0 {
		b.flush(batch)
	}
}

// modsBatchState — стан батчерів App для подій модів. Ініціалізується
// лениво при першому виклику, щоб не чіпати всі місця створення App.
// Два окремі батчери (progress/one) — так само, як console/buildConsole
// у console_batch.go: пачка одного потоку подій не має затримувати іншу.
type modsBatchState struct {
	once      sync.Once
	progressB *modsEventBatcher
	oneB      *modsEventBatcher
}

func (a *App) ensureModsBatchers() {
	a.modsBatch.once.Do(func() {
		a.modsBatch.progressB = newModsEventBatcher(func(batch []any) {
			a.emit("mods:update-progress:batch", batch)
		})
		a.modsBatch.oneB = newModsEventBatcher(func(batch []any) {
			a.emit("mods:update-one:batch", batch)
		})
	})
}

// emitModsUpdateProgress ставить подію прогресу перевірки модів у чергу
// батчера замість негайного emit. payload — те саме, що раніше йшло
// напряму в a.emit("mods:update-progress", payload).
func (a *App) emitModsUpdateProgress(payload map[string]any) {
	a.ensureModsBatchers()
	a.modsBatch.progressB.add(payload)
}

// emitModsUpdateOne ставить подію оновлення ОДНОГО мода у чергу батчера
// замість негайного emit.
func (a *App) emitModsUpdateOne(payload map[string]any) {
	a.ensureModsBatchers()
	a.modsBatch.oneB.add(payload)
}
