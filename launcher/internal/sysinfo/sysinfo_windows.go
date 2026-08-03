//go:build windows

package sysinfo

import (
	"runtime"
	"unsafe"

	"shaurma-launcher-wails/internal/model"

	"golang.org/x/sys/windows"
)

type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

var (
	kernel32          = windows.NewLazySystemDLL("kernel32.dll")
	procGlobalMemory  = kernel32.NewProc("GlobalMemoryStatusEx")
	procDiskFreeSpace = kernel32.NewProc("GetDiskFreeSpaceExW")
)

func collect(dir string) model.SystemInfo {
	info := model.SystemInfo{CPUCount: runtime.NumCPU()}

	var ms memoryStatusEx
	ms.Length = uint32(unsafe.Sizeof(ms))
	if procGlobalMemory.Find() == nil {
		r1, _, _ := procGlobalMemory.Call(uintptr(unsafe.Pointer(&ms)))
		if r1 != 0 {
			info.TotalRAMMB = int64(ms.TotalPhys / 1024 / 1024)
		}
	}

	if dir != "" {
		dirPtr, err := windows.UTF16PtrFromString(dir)
		if err == nil && procDiskFreeSpace.Find() == nil {
			var freeAvail, total, free uint64
			r1, _, _ := procDiskFreeSpace.Call(
				uintptr(unsafe.Pointer(dirPtr)),
				uintptr(unsafe.Pointer(&freeAvail)),
				uintptr(unsafe.Pointer(&total)),
				uintptr(unsafe.Pointer(&free)),
			)
			if r1 != 0 {
				info.FreeDiskMB = int64(freeAvail / 1024 / 1024)
			}
		}
	}

	return info
}
