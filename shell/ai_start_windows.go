//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// detachProcess відв'язує дочірній процес від консолі батька й без вікна (DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP).
func detachProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000008 | 0x00000200, HideWindow: true}
}
