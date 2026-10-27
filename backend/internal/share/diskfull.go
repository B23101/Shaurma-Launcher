package share

import (
	"errors"
	"runtime"
	"syscall"
)

// isDiskFull — чи помилка запису означає «на диску закінчилось місце».
// Windows: ERROR_DISK_FULL (112) / ERROR_HANDLE_DISK_FULL (39); інші ОС — ENOSPC.
func isDiskFull(err error) bool {
	var en syscall.Errno
	if !errors.As(err, &en) {
		return false
	}
	if runtime.GOOS == "windows" {
		return en == 112 || en == 39
	}
	return en == syscall.ENOSPC
}
