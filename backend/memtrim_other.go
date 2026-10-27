//go:build !linux

package main

import (
	"shaurma-launcher-backend/internal/crashguard"
	"runtime/debug"
	"sync"
	"time"
)

// memtrim_other.go — повернення пам'яті ОС на Windows/macOS.
//
// РАНІШЕ тут були порожні no-op з припущенням «на Windows це не потрібно».
// Це хибно: сторінка модів і браузер контенту дають короткі піки (скан
// jar-ів, JSON-відповіді, розпарсені відповіді Modrinth/CurseForge), після
// яких Go-купа лишається роздутою — рантайм віддає сторінки ОС ліниво, за
// власним scavenger-таймером. У Диспетчері завдань це виглядало як
// «лаунчер 30 МБ → 120 МБ і не зменшується».
//
// debug.FreeOSMemory() = повний GC + негайне повернення вільних сторінок.
// Дорого робити часто, тому тротлимо (раз на memReleaseInterval).

const memReleaseInterval = 15 * time.Second

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
func releaseMemoryNow() {
	debug.FreeOSMemory()
}
