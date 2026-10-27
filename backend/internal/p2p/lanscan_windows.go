//go:build windows

package p2p

// lanscan_windows.go — які TCP-порти СЛУХАЄ java на цьому ПК (GetExtendedTcpTable з PID власника).
// Це надійне джерело для світів «Відкрити для мережі»: порт випадковий, а процес — завжди java(w).exe.

import (
	"sort"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modIphlpapi           = windows.NewLazySystemDLL("iphlpapi.dll")
	procGetExtendedTcpTbl = modIphlpapi.NewProc("GetExtendedTcpTable")
)

const (
	afInet                   = 2
	tcpTableOwnerPidListener = 3 // TCP_TABLE_OWNER_PID_LISTENER
)

// mibTcpRowOwnerPid — MIB_TCPROW_OWNER_PID.
type mibTcpRowOwnerPid struct {
	State      uint32
	LocalAddr  uint32
	LocalPort  uint32
	RemoteAddr uint32
	RemotePort uint32
	OwningPid  uint32
}

func listeningJavaPorts() []int {
	var size uint32
	// Перший виклик — дізнатись розмір (повертає ERROR_INSUFFICIENT_BUFFER).
	procGetExtendedTcpTbl.Call(0, uintptr(unsafe.Pointer(&size)), 0, afInet, tcpTableOwnerPidListener, 0)
	if size == 0 || size > 8<<20 {
		return nil
	}
	buf := make([]byte, size)
	r, _, _ := procGetExtendedTcpTbl.Call(
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)), 0,
		afInet, tcpTableOwnerPidListener, 0)
	if r != 0 || len(buf) < 4 {
		return nil
	}
	n := *(*uint32)(unsafe.Pointer(&buf[0]))
	rowSize := unsafe.Sizeof(mibTcpRowOwnerPid{})
	if uintptr(len(buf)) < 4+uintptr(n)*rowSize {
		return nil
	}
	isJava := map[uint32]bool{} // кеш PID → чи java
	seen := map[int]bool{}
	for i := uint32(0); i < n; i++ {
		row := (*mibTcpRowOwnerPid)(unsafe.Pointer(&buf[4+uintptr(i)*rowSize]))
		// Порт у мережевому порядку байтів (у нижніх 16 бітах).
		port := int((row.LocalPort&0xff)<<8 | (row.LocalPort>>8)&0xff)
		if port < 1024 || seen[port] {
			continue
		}
		j, ok := isJava[row.OwningPid]
		if !ok {
			j = pidIsJava(row.OwningPid)
			isJava[row.OwningPid] = j
		}
		if j {
			seen[port] = true
		}
	}
	out := make([]int, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Ints(out)
	if len(out) > 64 {
		out = out[:64]
	}
	return out
}

func pidIsJava(pid uint32) bool {
	if pid == 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_PATH)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil || size == 0 {
		return false
	}
	img := strings.ToLower(windows.UTF16ToString(buf[:size]))
	if i := strings.LastIndexAny(img, `\/`); i >= 0 {
		img = img[i+1:]
	}
	return img == "java.exe" || img == "javaw.exe"
}
