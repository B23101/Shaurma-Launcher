//go:build windows

package main

// Зниження пріоритету процесу лаунчера під час гри (план A оптимізації запуску).
//
// Ідея з Modrinth App: поки гра запущена, лаунчер не має конкурувати з нею
// за CPU. На Windows пріоритет-клас процесу визначає, скільки квантів часу
// ОС виділяє потокам процесу відносно інших. Опускаючи сам лаунчер
// (launcher.exe) НИЖЧЕ за звичайний (BELOW_NORMAL), ми лишаємо більше CPU грі,
// не чіпаючи саму JVM гри — вона окремий процес і лишається на NORMAL.
//
// Торкаємося ЛИШЕ свого процесу та його WebView2-дітей (msedgewebview2.exe):
// вони рендерять фронтенд, який не потрібен, поки гра у фокусі. Сторонні
// процеси (зокрема JVM гри) не змінюються — гра має працювати на повному
// пріоритеті. Файл лише для Windows (за _windows.go, як process_health.go);
// на інших ОС компілюється no-op версія priority_other.go.

import (
	"runtime"
	"syscall"
	"unsafe"
)

const (
	// BELOW_NORMAL_PRIORITY_CLASS (WinNT.h)
	priorityClassBelowNormal = 0x00004000
	// NORMAL_PRIORITY_CLASS (WinNT.h)
	priorityClassNormal = 0x00000020
	// PROCESS_SET_INFORMATION — права для SetPriorityClass на чужий процес.
	processSetInformation = 0x0200
	// TH32CS_SNAPPROCESS — знімок усіх процесів системи.
	th32csSnapProcess = 0x00000002
)

// guardGamePriority знижує пріоритет свого процесу та його WebView2-дітей.
// Викликається, коли гра переходить у стан «запущена» (sessions callback).
func guardGamePriority() {
	if runtime.GOOS != "windows" {
		return
	}
	setProcessClass(0, priorityClassBelowNormal)
	setChildWebViewPriority(priorityClassBelowNormal)
}

// restoreGamePriority повертає NORMAL пріоритет своєму процесу і WebView-дітям.
// Викликається, коли остання гра завершилась (sessions callback → false).
func restoreGamePriority() {
	if runtime.GOOS != "windows" {
		return
	}
	setProcessClass(0, priorityClassNormal)
	setChildWebViewPriority(priorityClassNormal)
}

// setProcessClass встановлює пріоритет-клас процесу. pid == 0 → власний процес
// (через GetCurrentProcess, псевдо-хендл — без OpenProcess/CloseHandle).
func setProcessClass(pid uint32, class uint32) {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	setPrio := kernel32.NewProc("SetPriorityClass")
	var handle uintptr
	if pid == 0 {
		h, _, _ := kernel32.NewProc("GetCurrentProcess").Call() // pseudo-handle
		handle = h
		if handle == 0 {
			return
		}
	} else {
		openProc := kernel32.NewProc("OpenProcess")
		h, _, _ := openProc.Call(uintptr(processSetInformation), 0, uintptr(pid))
		if h == 0 {
			return // процес міг завершитись або немає прав — просто пропускаємо
		}
		defer kernel32.NewProc("CloseHandle").Call(h)
		handle = h
	}
	setPrio.Call(handle, uintptr(class))
}

// setChildWebViewPriority знаходить серед прямих нащадків нашого процесу
// WebView2 (msedgewebview2.exe) і встановлює їм пріоритет-клас class.
func setChildWebViewPriority(class uint32) {
	for _, pid := range webViewChildPIDs() {
		setProcessClass(pid, class)
	}
}

// webViewChildPIDs повертає PIDs живих прямих дітей нашого процесу, чия назва
// модуля — msedgewebview2.exe (фронтенд). Використовує Toolhelp snapshot.
func webViewChildPIDs() []uint32 {
	var out []uint32
	myPID := uint32(syscall.Getpid())

	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	h, _, _ := kernel32.NewProc("CreateToolhelp32Snapshot").Call(uintptr(th32csSnapProcess), 0)
	if h == uintptr(syscall.InvalidHandle) || h == 0 {
		return out
	}
	defer kernel32.NewProc("CloseHandle").Call(h)

	// PROCESSENTRY32W — структура, сумісна з x64.
	type processEntry32W struct {
		Size            uint32
		Usage           uint32
		ProcessID       uint32
		DefaultHeapID   uintptr
		ModuleID        uint32
		Threads         uint32
		ParentProcessID uint32
		PriClassBase    int32
		Flags           uint32
		ExeFile         [260]uint16
	}

	first := kernel32.NewProc("Process32FirstW")
	next := kernel32.NewProc("Process32NextW")

	var e processEntry32W
	e.Size = uint32(unsafe.Sizeof(e))
	ok, _, _ := first.Call(h, uintptr(unsafe.Pointer(&e)))
	for ok != 0 {
		if e.ParentProcessID == myPID && exeNameEquals(&e.ExeFile, "msedgewebview2.exe") {
			out = append(out, e.ProcessID)
		}
		e.Size = uint32(unsafe.Sizeof(e))
		ok, _, _ = next.Call(h, uintptr(unsafe.Pointer(&e)))
	}
	return out
}

// exeNameEquals порівнює UTF-16LE (NUL-terminated) ім'я модуля з target
// (порівняння case-insensitive). maxLen — розмір буфера ExeFile.
func exeNameEquals(name *[260]uint16, target string) bool {
	// Зіставляємо символ за символом, щоб не будувати тимчасову стрічку.
	var i int
	for ; i < 260; i++ {
		ch := name[i]
		if ch == 0 {
			break
		}
		if i >= len(target) {
			return false
		}
		want := uint16(target[i])
		// ASCII lowercasing для порівняння.
		if ch >= 'A' && ch <= 'Z' {
			ch += 32
		}
		if want >= 'A' && want <= 'Z' {
			want += 32
		}
		if ch != want {
			return false
		}
	}
	return i == len(target)
}
