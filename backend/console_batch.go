package main

import (
	"sync"
	"time"

	"shaurma-launcher-backend/internal/model"
)

// ── Батчування подій консолі ─────────────────────────────────────────────
//
// Рядки логу гри не шлються на фронтенд по одному: вони складаються в
// невеликий буфер і скидаються ПАЧКОЮ раз на consoleBatchInterval (або одразу,
// якщо пачка вже велика). Інакше при тисячах рядків/сек (спам у консоль, стек-
// трейс на кожен тік) черга UI-потоку (a.emit → bridgeEmit → CGo →
// InvokeAsync, серіалізація події у WebView) росте необмежено, і весь
// інтерфейс лаунчера перестає реагувати. Це узгоджується з RAF-батчингом на
// фронтенді (GameConsole.svelte addLine/flushPendingLines); читання stdout не
// блокується на IPC.
const (
	consoleBatchInterval = 40 * time.Millisecond
	// consoleBatchFlushAt — примусовий скид, якщо пачка встигла нарости до
	// цього розміру ще ДО спрацювання таймера (краш-дамп: тисячі рядків за
	// один момент) — щоб не тримати в пам'яті необмежено багато рядків між
	// тіками таймера.
	consoleBatchFlushAt = 500
)

// consoleLineBatcher акумулює рядки для однієї події (console:line або
// buildConsole:line) і зливає їх пачкою. Один інстанс на App — обидва
// потоки (активний буфер + buildConsole) мають власний батчер, щоб пачки
// однієї не затримували іншу.
//
// ⚠ КОНТРАКТ З ФРОНТЕНДОМ: події йдуть з НАЗВОМИ-УМНОЖНОЮ ФОРМОЮ —
// «console:lines» і «buildConsole:lines» (масив рядків/об'єктів),
// одиночних «console:line»/«buildConsole:line» цей бекенд БІЛЬШЕ не шле.
// Споживач — frontend/src/lib/GameConsole.svelte (вкладка «Консоль» на
// сторінці збірки і окреме вікно). Саме «миготіння назви події» без
// оновлення підписок свого часу зробило вкладку консолі мертвою: бекенд
// перейшов на пачки, а фронтенд лишився на одиночних — і поки вкладка
// була відкрита, вона не отримувала ЖОДНОГО живого рядка (контент
// з'являвся лише після перечитування знімка при поверненні в розділ).
type consoleLineBatcher struct {
	mu      sync.Mutex
	pending []any
	timer   *time.Timer
	flush   func(batch []any)
}

func newConsoleLineBatcher(flush func(batch []any)) *consoleLineBatcher {
	return &consoleLineBatcher{flush: flush}
}

// add додає елемент у пачку. Перший елемент після порожньої пачки заводить
// таймер скидання; якщо пачка вже завелика — скидається негайно (без
// очікування таймера), щоб один величезний сплеск логів не тримав усе в
// пам'яті до наступного тіку.
func (b *consoleLineBatcher) add(item any) {
	b.mu.Lock()
	b.pending = append(b.pending, item)
	shouldFlushNow := len(b.pending) >= consoleBatchFlushAt
	startTimer := len(b.pending) == 1 && !shouldFlushNow
	var timer *time.Timer
	if startTimer {
		timer = time.AfterFunc(consoleBatchInterval, b.flushNow)
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

// flushNow — колбек таймера: віддає накопичену пачку (якщо вона не встигла
// спорожніти через consoleBatchFlushAt-скид) і зупиняє посилання на таймер.
func (b *consoleLineBatcher) flushNow() {
	b.mu.Lock()
	batch := b.pending
	b.pending = nil
	b.timer = nil
	b.mu.Unlock()
	if len(batch) > 0 {
		b.flush(batch)
	}
}

// consoleLineBatchers — стан батчерів App. Ініціалізується лениво при
// першому виклику emitConsoleLine/emitBuildConsoleLine, щоб не чіпати всі
// місця створення App.
type consoleBatchState struct {
	once     sync.Once
	consoleB *consoleLineBatcher
	buildB   *consoleLineBatcher
}

func (a *App) ensureConsoleBatchers() {
	a.consoleBatch.once.Do(func() {
		a.consoleBatch.consoleB = newConsoleLineBatcher(func(batch []any) {
			a.emit("console:lines", batch)
		})
		a.consoleBatch.buildB = newConsoleLineBatcher(func(batch []any) {
			a.emit("buildConsole:lines", batch)
		})
	})
}

// emitConsoleLine ставить рядок живого потоку консолі (console:line, для
// поточного активного буфера) у чергу батчера замість негайного emit.
func (a *App) emitConsoleLine(line string) {
	a.ensureConsoleBatchers()
	a.consoleBatch.consoleB.add(line)
}

// emitBuildConsoleLine ставить скоупований по збірці/акаунту рядок
// (buildConsole:line) у чергу батчера замість негайного emit.
func (a *App) emitBuildConsoleLine(l model.BuildConsoleLine) {
	a.ensureConsoleBatchers()
	a.consoleBatch.buildB.add(l)
}
