package main

// memreport.go — звіт пам'яті в launcher.log і знімок купи при розростанні.
//
// НАВІЩО. Скарги «лаунчер тримає сотні мегабайт» без цифр зводились до
// здогадів: незрозуміло, чи це Go-купа backend.dll (кеші, відповіді API,
// P2P-вузли), чи нативна частина процесу (WebView2, Wails), яку Go не бачить.
// Тепер раз на memReportInterval у лог пишеться один рядок: жива Go-купа,
// скільки рантайм тримає в ОС, стеки, горутини і приватна пам'ять усього
// процесу. Різниця «процес − Go» = нативна частина.
//
// Якщо жива Go-купа перевищує memProfileThreshold, у теку логів пишеться
// heap-*.pprof (не частіше memProfileEvery, тримаємо memProfileKeep останніх) —
// за ним `go tool pprof` показує, ЯКИЙ код тримає пам'ять.

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"sort"
	"time"

	"shaurma-launcher-backend/internal/launchlog"
)

const (
	memReportInterval   = 5 * time.Minute
	memProfileThreshold = 80 << 20
	memProfileEvery     = 30 * time.Minute
	memProfileKeep      = 3
)

var memProfileLast time.Time

// reportMemory пише рядок зі станом пам'яті і, якщо купа розрослась, знімок купи.
// Викликається з фонового прибиральника (memtrim.go).
func (a *App) reportMemory() {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	mb := func(v uint64) float64 { return float64(v) / (1 << 20) }
	private := processPrivateBytes()
	native := ""
	if private > 0 {
		goTotal := ms.Sys - ms.HeapReleased
		if private > goTotal {
			native = fmt.Sprintf(", поза Go %.0f МБ", mb(private-goTotal))
		}
	}
	launchlog.Info("memory", "процес %.0f МБ%s; Go-купа жива %.1f МБ, утримано в ОС %.1f МБ, стеки %.1f МБ, горутин %d, GC %d",
		mb(private), native, mb(ms.HeapAlloc), mb(ms.HeapSys-ms.HeapReleased), mb(ms.StackSys), runtime.NumGoroutine(), ms.NumGC)

	if ms.HeapAlloc < memProfileThreshold || time.Since(memProfileLast) < memProfileEvery {
		return
	}
	memProfileLast = time.Now()
	a.writeHeapProfile()
}

func (a *App) writeHeapProfile() {
	if a.cfg == nil {
		return
	}
	dir := a.cfg.LogsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	path := filepath.Join(dir, "heap-"+time.Now().Format("20060102-150405")+".pprof")
	f, err := os.Create(path)
	if err != nil {
		return
	}
	runtime.GC() // знімок живих об'єктів, а не сміття, що чекає збирача
	werr := pprof.WriteHeapProfile(f)
	_ = f.Close()
	if werr != nil {
		_ = os.Remove(path)
		return
	}
	launchlog.Warn("memory", "Go-купа понад %d МБ — знімок купи збережено: %s", memProfileThreshold>>20, path)

	old, _ := filepath.Glob(filepath.Join(dir, "heap-*.pprof"))
	sort.Strings(old) // ім'я = час, тож лексикографічно = хронологічно
	for len(old) > memProfileKeep {
		_ = os.Remove(old[0])
		old = old[1:]
	}
}
