//go:build windows

package servers

import (
	"syscall"
	"time"
	"unsafe"
)

// getProcessRAM повертає RSS пам'яті процесу в МБ.
func getProcessRAM(pid int) int64 {
	return getProcessRAMWindows(pid)
}

// getProcessCPU повертає сукупний CPU time процесу.
func getProcessCPU(pid int) time.Duration {
	return getProcessCPUWindows(pid)
}

// processMemoryCounters — підмножина полів PROCESS_MEMORY_COUNTERS
// (psapi.h), нам потрібен лише WorkingSetSize (RSS).
type processMemoryCounters struct {
	cb                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
}

// getProcessRAMWindows повертає RSS (working set) процесу в МБ через
// прямий виклик WinAPI GetProcessMemoryInfo — БЕЗ спавну tasklist.exe.
//
// Раніше тут запускався `tasklist.exe` РАЗ НА СЕКУНДУ для кожного
// запущеного сервера (collectLoop — тікер 1с у stats.go). Кожен такий
// spawn — це нове консольне вікно (яке блимає, якщо не приховане) і
// зайві мілісекунди на створення процесу. GetProcessMemoryInfo — та сама
// інформація напряму з ядра, без жодного дочірнього процесу.
func getProcessRAMWindows(pid int) int64 {
	const processQueryLimitedInformation = 0x1000
	const processVMRead = 0x0010

	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	psapi := syscall.NewLazyDLL("psapi.dll")
	procOpenProcess := kernel32.NewProc("OpenProcess")
	procCloseHandle := kernel32.NewProc("CloseHandle")
	procGetProcessMemoryInfo := psapi.NewProc("GetProcessMemoryInfo")

	h, _, _ := procOpenProcess.Call(
		uintptr(processQueryLimitedInformation|processVMRead),
		0,
		uintptr(pid),
	)
	if h == 0 {
		return 0
	}
	defer procCloseHandle.Call(h)

	var counters processMemoryCounters
	counters.cb = uint32(unsafe.Sizeof(counters))
	ret, _, _ := procGetProcessMemoryInfo.Call(
		h,
		uintptr(unsafe.Pointer(&counters)),
		uintptr(counters.cb),
	)
	if ret == 0 {
		return 0
	}

	return int64(counters.WorkingSetSize) / 1024 / 1024 // байти → МБ
}

// getProcessCPUWindows повертає сукупний CPU time процесу через прямий
// виклик WinAPI GetProcessTimes — БЕЗ спавну жодного дочірнього процесу.
//
// Раніше тут запускався `powershell -Command (Get-Process ...)` РАЗ НА
// СЕКУНДУ для кожного запущеного сервера (collectLoop — тікер 1с). Старт
// PowerShell коштує 200–800 мс і без прапорця приховування відкривав
// власне консольне вікно, яке щосекунди блимало на екрані користувача —
// саме ці "консолі, що вилазять". GetProcessTimes — те саме, що робить
// PowerShell під капотом, але напряму через syscall, без вікна й майже
// миттєво (мікросекунди замість сотень мілісекунд).
func getProcessCPUWindows(pid int) time.Duration {
	const processQueryLimitedInformation = 0x1000

	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	procOpenProcess := kernel32.NewProc("OpenProcess")
	procGetProcessTimes := kernel32.NewProc("GetProcessTimes")
	procCloseHandle := kernel32.NewProc("CloseHandle")

	h, _, _ := procOpenProcess.Call(
		uintptr(processQueryLimitedInformation),
		0,
		uintptr(pid),
	)
	if h == 0 {
		return 0
	}
	defer procCloseHandle.Call(h)

	var creation, exit, kernelT, userT syscall.Filetime
	ret, _, _ := procGetProcessTimes.Call(
		h,
		uintptr(unsafe.Pointer(&creation)),
		uintptr(unsafe.Pointer(&exit)),
		uintptr(unsafe.Pointer(&kernelT)),
		uintptr(unsafe.Pointer(&userT)),
	)
	if ret == 0 {
		return 0
	}

	// FILETIME — 100-наносекундні інтервали.
	kernelTicks := int64(kernelT.HighDateTime)<<32 | int64(kernelT.LowDateTime)
	userTicks := int64(userT.HighDateTime)<<32 | int64(userT.LowDateTime)
	totalTicks := kernelTicks + userTicks

	return time.Duration(totalTicks * 100) // × 100 нс → наносекунди
}
