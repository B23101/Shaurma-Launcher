//go:build windows

package main

// memtrim_windows.go — повернення робочих наборів ОС на Windows.
//
// НАВІЩО ОКРЕМО ВІД memtrim_other.go. `debug.FreeOSMemory()` повертає ОС
// лише сторінки **Go-купи**. Але лаунчер на Windows — це ОДИН процес
// (`ShaurmLauncher.exe`), у якому живуть:
//   - shell (Wails + Go),
//   - backend.dll (Go),
//   - нативний `EmbeddedBrowserWebView.dll` (Chromium рантайму WebView2, §8.1),
// плюс окремі дочірні процеси `msedgewebview2.exe` (браузерний, рендерер, GPU,
// службові). Коли вікно сховано в трей, усі вони продовжують тримати сторінки
// в пам'яті — саме це користувач бачить у Диспетчері завдань як «лаунчер
// тримає сотні мегабайт у треї».
//
// `EmptyWorkingSet` віддає ОС сторінки робочого набору процесу: вони
// переміщуються в standby/paging file і повертаються при першому зверненні
// (сторінкові переходи замість стіни «з'їденої» пам'яті). Дані НЕ втрачаються,
// стан процесу не змінюється — тому це безпечно саме тоді, коли вікно
// приховане і користувач не взаємодіє з UI.
//
// ВАЖЛИВО: викликаємо це ЛИШЕ на приховування в трей / перед сном UI
// (app.go: hideToTray). Періодично «підчищати» робочий набір видимого вікна
// не можна — кожне повернення сторінки коштує дискового I/O і дає ривки UI.

import (
	"log"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var procEmptyWorkingSet = syscall.NewLazyDLL("psapi.dll").NewProc("EmptyWorkingSet")

// trimTreeLimit — скільки нащадків максимум чіпаємо (WebView2 тримає 5–10
// процесів; 32 — з запасом, щоб ніколи не «гуляти» по всьому дереву ОС).
const trimTreeLimit = 32

// trimProcessWorkingSets віддає ОС робочі набори нашого процесу і всіх
// нащадків (WebView2). Повертає кількість процесів, які вдалось обробити.
//
// Ніколи не панікує і не повертає помилку: без цієї оптимізації лаунчер
// працює як раніше, тож будь-яка відмова (права, закритий хендл) — тиха.
func trimProcessWorkingSets() (trimmed int) {
	// Власний процес — без OpenProcess (GetCurrentProcess завжди валідний).
	if r, _, _ := procEmptyWorkingSet.Call(uintptr(windows.CurrentProcess())); r != 0 {
		trimmed++
	}
	pids := descendantPIDs(windows.GetCurrentProcessId(), trimTreeLimit)
	for _, pid := range pids {
		h, err := windows.OpenProcess(
			windows.PROCESS_SET_QUOTA|windows.PROCESS_QUERY_INFORMATION, false, pid)
		if err != nil {
			// WebView2-процеси іноді захищені/вже завершились — це нормально.
			continue
		}
		if r, _, _ := procEmptyWorkingSet.Call(uintptr(h)); r != 0 {
			trimmed++
		}
		windows.CloseHandle(h)
	}
	return trimmed
}

// descendantPIDs повертає PID усіх нащадків root (рекурсивно), не більше
// limit штук. Один знімок процесів через Toolhelp32 — без WMI (WMI на цьому
// шляху коштував би сотні мілісекунд і власний COM-апартамент).
func descendantPIDs(root uint32, limit int) []uint32 {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)

	parentOf := make(map[uint32]uint32, 512)
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	if err := windows.Process32First(snap, &e); err != nil {
		return nil
	}
	for {
		parentOf[e.ProcessID] = e.ParentProcessID
		if err := windows.Process32Next(snap, &e); err != nil {
			break
		}
	}

	seen := map[uint32]bool{root: true}
	out := make([]uint32, 0, 16)
	// Кілька проходів, поки дерево не стабілізується: батько міг з'явитись у
	// списку пізніше за дитину (порядок у знімку не гарантований).
	for grow := true; grow && len(out) < limit; {
		grow = false
		for pid, ppid := range parentOf {
			if seen[pid] || !seen[ppid] {
				continue
			}
			seen[pid] = true
			out = append(out, pid)
			grow = true
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}

// logWorkingSetTrim — один рядок у лог на приховування (не частіше, ніж
// приховують вікно). Потрібен, щоб «лаунчер у треї їсть RAM» можна було
// перевірити за логом, а не на око.
func logWorkingSetTrim(trimmed int, heapInUse uint64) {
	if trimmed <= 0 {
		return
	}
	log.Printf("memory: робочі набори віддано ОС (%d процесів: лаунчер + WebView2), Go-купа в роботі: %.1f МБ",
		trimmed, float64(heapInUse)/(1<<20))
}
