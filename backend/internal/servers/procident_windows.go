//go:build windows

package servers

import (
	"strings"
	"syscall"
	"unsafe"
)

// processSnapshot — ідентичність процесу на момент знімка.
//
// Нам потрібні рівно два факти, щоб БЕЗПЕЧНО впізнати «наш» java після
// краша лаунчера:
//   - Alive — процес узагалі існує;
//   - CreateNs — час створення процесу (не змінюється за все життя).
//
// PID сам по собі недостатній: Windows перевикористовує номери, тож
// «PID 1234 живий» могло б означати зовсім інший процес, і лаунчер
// привласнив би чужий. Час створення + PID разом дають практично
// неможливу колізію. Image — додаткова страховка (має бути java).
type processSnapshot struct {
	Alive    bool
	CreateNs int64
	Image    string
}

const processQueryLimitedInformation = 0x1000

// snapshotProcess читає ідентичність процесу через WinAPI (жодного
// дочірнього процесу: tasklist/wmic/powershell НЕ викликаються — саме
// тому цей же підхід уже використовується у stats_windows.go).
func snapshotProcess(pid int) processSnapshot {
	if pid <= 0 {
		return processSnapshot{}
	}
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	procOpenProcess := kernel32.NewProc("OpenProcess")
	procCloseHandle := kernel32.NewProc("CloseHandle")
	procGetProcessTimes := kernel32.NewProc("GetProcessTimes")
	procQueryImage := kernel32.NewProc("QueryFullProcessImageNameW")

	h, _, _ := procOpenProcess.Call(uintptr(processQueryLimitedInformation), 0, uintptr(pid))
	if h == 0 {
		return processSnapshot{}
	}
	defer procCloseHandle.Call(h)

	// Процес, який уже завершився, АЛЕ тримає відкриті хендли (напр. тест,
	// що ще не зібрав Wait()), лишає видимим для OpenProcess/GetProcessTimes.
	// GetExitCodeProcess повертає STILL_ACTIVE (259) ЛИШЕ для ЖИВОГО — без
	// цієї перевірки «усиновився» б і процес, що насправді помер.
	const stillActive = 259
	procGetExitCode := kernel32.NewProc("GetExitCodeProcess")
	var exitCode uint32
	if r, _, _ := procGetExitCode.Call(h, uintptr(unsafe.Pointer(&exitCode))); r == 0 || exitCode != stillActive {
		return processSnapshot{}
	}

	var creation, exit, kernelT, userT syscall.Filetime
	ret, _, _ := procGetProcessTimes.Call(
		h,
		uintptr(unsafe.Pointer(&creation)),
		uintptr(unsafe.Pointer(&exit)),
		uintptr(unsafe.Pointer(&kernelT)),
		uintptr(unsafe.Pointer(&userT)),
	)
	if ret == 0 {
		return processSnapshot{}
	}

	image := ""
	buf := make([]uint16, syscall.MAX_PATH)
	size := uint32(len(buf))
	if r, _, _ := procQueryImage.Call(h, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size))); r != 0 && size > 0 {
		image = syscall.UTF16ToString(buf[:size])
	}

	return processSnapshot{Alive: true, CreateNs: filetimeToUnixNs(creation), Image: image}
}

// filetimeToUnixNs конвертує FILETIME (100-нс інтервали від 1601-01-01) у
// unix-наносекунди. Використовується лише для порівняння «той самий
// процес чи ні», тож будь-яка монотонна конверсія підійшла б, але саме
// ця дає ще й людський час для логів.
func filetimeToUnixNs(ft syscall.Filetime) int64 {
	ticks := int64(ft.HighDateTime)<<32 | int64(ft.LowDateTime)
	// Лічимо ЦІЛИМ ЧИСЛОМ: 1601→1970 у наносекундах (1.16e19) не вміщується
	// в int64 (max 9.2e18), тож спершу секунди, потім нс у дробі.
	const filetimeEpochOffsetSec int64 = 11644473600 // 1601 → 1970, с
	sec := ticks/10000000 - filetimeEpochOffsetSec
	ns := (ticks % 10000000) * 100 // залишок тіків → наносекунди
	return sec*1000000000 + ns
}

// isJavaImage — чи виконуваний файл процесу — java (java.exe/javaw.exe).
// Регістр не має значення; шлях може бути будь-який (лаунчер ставить
// власну Java у папку даних).
func isJavaImage(image string) bool {
	if image == "" {
		return false
	}
	base := strings.ToLower(image)
	if i := strings.LastIndexAny(base, `\/`); i >= 0 {
		base = base[i+1:]
	}
	return base == "java.exe" || base == "javaw.exe" || base == "java"
}
