//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// detachProcess відв'язує дочірній процес від термінала батька (нова сесія).
func detachProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
