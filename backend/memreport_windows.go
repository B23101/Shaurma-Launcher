//go:build windows

package main

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var procMemCountersEx = windows.NewLazySystemDLL("psapi.dll").NewProc("GetProcessMemoryInfo")

// processMemoryCountersEx — PROCESS_MEMORY_COUNTERS_EX: PrivateUsage = те, що
// Диспетчер завдань показує як «пам'ять» процесу (commit charge).
type processMemoryCountersEx struct {
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
	PrivateUsage               uintptr
}

// processPrivateBytes — приватна пам'ять ВСЬОГО процесу лаунчера (Go + нативні DLL). 0 — невідомо.
func processPrivateBytes() uint64 {
	var c processMemoryCountersEx
	c.cb = uint32(unsafe.Sizeof(c))
	r, _, _ := procMemCountersEx.Call(uintptr(windows.CurrentProcess()), uintptr(unsafe.Pointer(&c)), uintptr(c.cb))
	if r == 0 {
		return 0
	}
	return uint64(c.PrivateUsage)
}
