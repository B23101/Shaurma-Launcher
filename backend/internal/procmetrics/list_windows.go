//go:build windows

package procmetrics

import (
	"syscall"
	"unsafe"
)

var (
	psapi                = syscall.NewLazyDLL("psapi.dll")
	procGetProcessMemory = psapi.NewProc("GetProcessMemoryInfo")
)

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

const processQueryLimitedInformation = 0x1000

func ft(f syscall.Filetime) uint64 { return uint64(f.HighDateTime)<<32 | uint64(f.LowDateTime) }

func listProcs() []procInfo {
	snap, err := syscall.CreateToolhelp32Snapshot(syscall.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer syscall.CloseHandle(snap)
	var e syscall.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	if syscall.Process32First(snap, &e) != nil {
		return nil
	}
	var out []procInfo
	for {
		p := procInfo{pid: int(e.ProcessID), ppid: int(e.ParentProcessID)}
		if h, err := syscall.OpenProcess(processQueryLimitedInformation, false, e.ProcessID); err == nil {
			var c, x, k, u syscall.Filetime
			if syscall.GetProcessTimes(h, &c, &x, &k, &u) == nil {
				p.cpuNs = (ft(k) + ft(u)) * 100 // одиниці по 100 нс
			}
			var m processMemoryCounters
			m.cb = uint32(unsafe.Sizeof(m))
			if r, _, _ := procGetProcessMemory.Call(uintptr(h), uintptr(unsafe.Pointer(&m)), uintptr(m.cb)); r != 0 {
				p.rss = uint64(m.WorkingSetSize)
			}
			syscall.CloseHandle(h)
		}
		out = append(out, p)
		if syscall.Process32Next(snap, &e) != nil {
			break
		}
	}
	return out
}
