//go:build windows

package sysinfo

import (
	"runtime"
	"unsafe"

	"shaurma-launcher-backend/internal/model"

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

// osVersionInfoEx — OSVERSIONINFOEXW для RtlGetVersion (дозволяє дізнатись
// реальну версію Windows, яку GetVersionEx у маніфесті лаунчера ховає).
type osVersionInfoEx struct {
	OSVersionInfoSize      uint32
	MajorVersion           uint32
	MinorVersion           uint32
	BuildNumber            uint32
	PlatformID             uint32
	ServicePackMajor       uint16
	ServicePackMinor       uint16
	SuiteMask              uint16
	ProductType            byte
	Reserved               byte
}

var (
	kernel32          = windows.NewLazySystemDLL("kernel32.dll")
	procGlobalMemory  = kernel32.NewProc("GlobalMemoryStatusEx")
	procDiskFreeSpace = kernel32.NewProc("GetDiskFreeSpaceExW")
	ntdll             = windows.NewLazySystemDLL("ntdll.dll")
	procRtlGetVersion = ntdll.NewProc("RtlGetVersion")
)

// osName формує «людську» назву Windows за (major, minor, build):
// Windows 11 — 10.0.22000+, Windows 10 — 10.0, далі 8.1/8/7.
func osName(major, minor, build uint32) string {
	switch {
	case major == 10 && build >= 22000:
		return "Windows 11"
	case major == 10:
		return "Windows 10"
	case major == 6 && minor == 3:
		return "Windows 8.1"
	case major == 6 && minor == 2:
		return "Windows 8"
	case major == 6 && minor == 1:
		return "Windows 7"
	default:
		return "Windows"
	}
}

// archName перетворює runtime.GOARCH у зрозумілу архітектуру UI
// ("x64" / "x86" / "arm64") — так само називають її у SKlauncher.
func archName(goarch string) string {
	switch goarch {
	case "amd64":
		return "x64"
	case "386":
		return "x86"
	case "arm64":
		return "arm64"
	default:
		return goarch
	}
}

func collect(dir string) model.SystemInfo {
	info := model.SystemInfo{CPUCount: runtime.NumCPU()}

	// ОС/архітектура (вкладка «Про лаунчер»).
	info.Platform = runtime.GOOS
	info.Arch = archName(runtime.GOARCH)
	if procRtlGetVersion.Find() == nil {
		var vi osVersionInfoEx
		vi.OSVersionInfoSize = uint32(unsafe.Sizeof(vi))
		if r1, _, _ := procRtlGetVersion.Call(uintptr(unsafe.Pointer(&vi))); r1 == 0 {
			info.OS = osName(vi.MajorVersion, vi.MinorVersion, vi.BuildNumber)
		}
	}
	if info.OS == "" {
		info.OS = "Windows"
	}

	var ms memoryStatusEx
	ms.Length = uint32(unsafe.Sizeof(ms))
	if procGlobalMemory.Find() == nil {
		r1, _, _ := procGlobalMemory.Call(uintptr(unsafe.Pointer(&ms)))
		if r1 != 0 {
			info.TotalRAMMB = int64(ms.TotalPhys / 1024 / 1024)
			// AvailPhys — фізична пам'ять, вільна ПРЯМО ЗАРАЗ (з урахуванням
			// інших запущених програм). Раніше це поле читалось у ms, але
			// нікуди не записувалось — перевірка "мало вільної RAM" мовчки
			// звірялась із загальним обсягом пам'яті замість вільного і
			// тому не спрацьовувала, навіть коли вільної пам'яті лишалось
			// критично мало.
			info.FreeRAMMB = int64(ms.AvailPhys / 1024 / 1024)
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
