package main

// webview_memory.go — політика «коли притиснути пам'ять WebView2».
//
// ПРОБЛЕМА. WebView2 під час роботи набирає ~400 МБ (кеші Chromium, рендерер,
// GPU-процес) і сам нічого не віддає.
//
// ПОЛІТИКА (одна точка рішень; механізм — у shell, webview_memory_windows.go):
//   - вікно втратило активацію → через webviewInactiveDelay (щоб Alt-Tab туди-
//     сюди не смикав WebView2) рівень Low + повернення сторінок ОС;
//   - вікно сховане/згорнуте → Low одразу (trimNowForHiddenWindow);
//   - вікно активне, але користувач бездіяльний webviewIdleAfter (немає вводу
//     в системі) → теж Low: лаунчер відкритий на столі, а пам'ять нікому не
//     потрібна;
//   - вікно знову активне / користувач щось натиснув → Normal одразу.
//
// Low не зупиняє скрипти (на відміну від TrySuspend), тож події game:exit,
// чат і сповіщення працюють і в «економному» стані. Сторінки, потрібні знову,
// підвантажуються самі — зі скидом кешів перший кадр після пробудження може
// бути на соті частки секунди повільніший, це свідома ціна.
//
// Старий shell.exe без підтримки операцій memory-* просто ігнорує їх; тоді
// лишається повернення робочих наборів (memtrim_windows.go).

import (
	"context"
	"sync"
	"time"

	"shaurma-launcher-backend/internal/crashguard"
)

var (
	// webviewInactiveDelay — скільки вікно має бути неактивним, перш ніж ми
	// його «притискаємо».
	webviewInactiveDelay = 10 * time.Second
	// webviewIdleAfter — скільки користувач має не торкатись ні миші, ні
	// клавіатури, щоб притиснути ВІДКРИТЕ активне вікно.
	webviewIdleAfter = 60 * time.Second
	// webviewIdlePoll — частота перевірки простою (GetLastInputInfo — це
	// мікросекунди, тож 10 с нічого не коштує).
	webviewIdlePoll = 10 * time.Second
	// webviewTrimSettle — пауза між виставленням Low і поверненням робочих
	// наборів: Chromium скидає кеші асинхронно, і EmptyWorkingSet одразу після
	// запиту забрав би ще не звільнене.
	webviewTrimSettle = 2 * time.Second
)

// Ін'єкція для тестів.
var (
	webviewMemOp = func(op string) {
		// Простій/трей: Go-збирач теж переходить на найекономніший профіль.
		setGCIdle(op == "memory-low")
		bridgeWindowOp("main", op)
	}
	webviewMemTrim = func() {
		releaseMemoryNow()
		trimProcessWorkingSets()
	}
	webviewIdleFn = systemIdleDuration
)

var wvMem = struct {
	sync.Mutex
	active  bool // головне вікно активне (останнє відоме з window:active)
	relaxed bool // WebView2 зараз на рівні Low
	timer   *time.Timer
	gen     uint64 // скасування відкладених дій
}{active: true}

// webviewMemoryWindowActive — подія window:active від shell.
func webviewMemoryWindowActive(active bool) {
	wvMem.Lock()
	wvMem.active = active
	wvMem.gen++
	gen := wvMem.gen
	if wvMem.timer != nil {
		wvMem.timer.Stop()
		wvMem.timer = nil
	}
	if active {
		restore := wvMem.relaxed
		wvMem.relaxed = false
		wvMem.Unlock()
		if restore {
			webviewMemOp("memory-normal")
		}
		return
	}
	wvMem.timer = time.AfterFunc(webviewInactiveDelay, func() {
		defer crashguard.Recover("webviewMemory.inactive")
		webviewMemoryRelaxIf(func() bool { return wvMem.gen == gen && !wvMem.active })
	})
	wvMem.Unlock()
}

// webviewMemoryHidden — вікно сховане в трей або згорнуте (memtrim.go):
// одразу рівень Low, без очікування.
func webviewMemoryHidden() {
	wvMem.Lock()
	wvMem.active = false
	wvMem.gen++
	if wvMem.timer != nil {
		wvMem.timer.Stop()
		wvMem.timer = nil
	}
	already := wvMem.relaxed
	wvMem.relaxed = true
	wvMem.Unlock()
	if !already {
		webviewMemOp("memory-low")
	}
}

// webviewMemoryRestore — вікно показано користувачу (revealMainWindow).
func webviewMemoryRestore() {
	wvMem.Lock()
	wvMem.gen++
	if wvMem.timer != nil {
		wvMem.timer.Stop()
		wvMem.timer = nil
	}
	restore := wvMem.relaxed
	wvMem.relaxed = false
	wvMem.Unlock()
	if restore {
		webviewMemOp("memory-normal")
	}
}

// webviewMemoryRelaxIf переводить WebView2 в Low, якщо cond (читається під
// м'ютексом) досі істинна, і після паузи повертає ОС сторінки.
func webviewMemoryRelaxIf(cond func() bool) {
	wvMem.Lock()
	if wvMem.relaxed || !cond() {
		wvMem.Unlock()
		return
	}
	wvMem.relaxed = true
	wvMem.Unlock()

	webviewMemOp("memory-low")
	time.Sleep(webviewTrimSettle)
	// Користувач міг повернутись за ці секунди — тоді сторінки не чіпаємо.
	wvMem.Lock()
	still := wvMem.relaxed
	wvMem.Unlock()
	if still {
		webviewMemTrim()
	}
}

// startWebviewIdleWatcher — фонова перевірка простою користувача, поки вікно
// активне. Без підтримки платформи (не Windows) одразу виходить.
func startWebviewIdleWatcher(ctx context.Context) {
	if _, ok := webviewIdleFn(); !ok {
		return
	}
	go func() {
		defer crashguard.Recover("webviewMemory.idleWatcher")
		t := time.NewTicker(webviewIdlePoll)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				webviewMemoryIdleTick()
			}
		}
	}()
}

// webviewMemoryIdleTick — один крок перевірки простою (окремо для тестів).
func webviewMemoryIdleTick() {
	idle, ok := webviewIdleFn()
	if !ok {
		return
	}
	wvMem.Lock()
	active, relaxed := wvMem.active, wvMem.relaxed
	wvMem.Unlock()
	switch {
	case active && !relaxed && idle >= webviewIdleAfter:
		webviewMemoryRelaxIf(func() bool { return wvMem.active })
	case active && relaxed && idle < webviewIdlePoll/2:
		// Ввід з'явився, поки вікно активне — користувач працює з лаунчером.
		wvMem.Lock()
		restore := wvMem.active && wvMem.relaxed
		if restore {
			wvMem.relaxed = false
		}
		wvMem.Unlock()
		if restore {
			webviewMemOp("memory-normal")
		}
	}
}
