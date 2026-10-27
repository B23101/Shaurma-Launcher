//go:build linux

package main

/*
#include <stdlib.h>
#if defined(__GLIBC__)
#include <malloc.h>
static void shaurma_malloc_trim(void) { malloc_trim(0); }
#else
// musl та інші libc не мають malloc_trim — там це просто no-op.
static void shaurma_malloc_trim(void) {}
#endif
*/
import "C"

import (
	"shaurma-launcher-backend/internal/crashguard"
	"runtime/debug"
	"sync"
	"time"
)

// memtrim_linux.go — примусове повернення пам'яті операційній системі
// після важких операцій (сканування сотень модів, перевірка оновлень).
//
// ЧОМУ ЦЕ ПОТРІБНО САМЕ НА LINUX. backend.so — це cgo-модуль усередині
// процесу shell, і пам'ять там звільняється двома різними аллокаторами:
//
//   - Go-купа: збирач сміття звільняє об'єкти, але віддає сторінки ядру
//     ліниво (за власним таймером, до кількох хвилин). Поки цього не
//     сталося, RSS процесу лишається на піку — користувач бачить у
//     системному моніторі «лаунчер тримає 2 ГБ» ще довго після того, як
//     сторінка модів закрита;
//   - glibc malloc: рядки, що перетинають межу C ABI (C.CString із
//     JSON-відповіддю на кожен backend_invoke), живуть в аренах glibc.
//     free() повертає блок у арену, але НЕ ядру — арена лишається за
//     процесом. malloc_trim(0) віддає вільні частини арен назад.
//
// На Windows цього файлу нема (memtrim_other.go — порожній no-op): там
// і поведінка аллокатора інша, і скарг на «процес тримає гігабайти» не
// було.
//
// Виклик дешевий, але не безкоштовний (FreeOSMemory — це повний цикл GC),
// тому він тротлиться: не частіше ніж раз на memReleaseInterval.

const memReleaseInterval = 30 * time.Second

var (
	memReleaseMu   sync.Mutex
	memReleaseLast time.Time
)

// maybeReleaseMemory повертає пам'ять ОС, але не частіше ніж раз на
// memReleaseInterval. Безпечно викликати після будь-якої важкої операції.
func maybeReleaseMemory() {
	memReleaseMu.Lock()
	if time.Since(memReleaseLast) < memReleaseInterval {
		memReleaseMu.Unlock()
		return
	}
	memReleaseLast = time.Now()
	memReleaseMu.Unlock()
	go func() {
		defer crashguard.Recover("releaseMemoryNow")
		releaseMemoryNow()
	}()
}

// releaseMemoryNow — негайне повернення пам'яті (без тротлінгу).
// Викликається з періодичної очистки кешів (pruneModsCaches).
func releaseMemoryNow() {
	debug.FreeOSMemory()
	C.shaurma_malloc_trim()
}
