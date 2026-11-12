//go:build windows

package main

import (
	"syscall"
	"time"
	"unsafe"
)

var (
	procGetLastInputInfo = syscall.NewLazyDLL("user32.dll").NewProc("GetLastInputInfo")
	procGetTickCount     = syscall.NewLazyDLL("kernel32.dll").NewProc("GetTickCount")
)

type lastInputInfo struct {
	cbSize uint32
	dwTime uint32
}

// systemIdleDuration — скільки часу в поточному сеансі не було ні миші, ні
// клавіатури (GetLastInputInfo). false — не вдалось визначити.
func systemIdleDuration() (time.Duration, bool) {
	li := lastInputInfo{cbSize: uint32(unsafe.Sizeof(lastInputInfo{}))}
	if r, _, _ := procGetLastInputInfo.Call(uintptr(unsafe.Pointer(&li))); r == 0 {
		return 0, false
	}
	tick, _, _ := procGetTickCount.Call()
	// GetTickCount — 32 біти й переповнюється раз на ~49 діб; віднімання в
	// uint32 коректне і через переповнення.
	return time.Duration(uint32(tick)-li.dwTime) * time.Millisecond, true
}
