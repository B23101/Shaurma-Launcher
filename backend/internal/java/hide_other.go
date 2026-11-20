//go:build !windows

package java

import "os/exec"

func hideConsole(*exec.Cmd) {}
