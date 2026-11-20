//go:build windows

package java

import (
	"os/exec"
	"syscall"
)

// hideConsole не дає java.exe показати чорне вікно консолі під час перевірки.
func hideConsole(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}
