//go:build !windows

package servers

import "os/exec"

// hideConsoleWindow — no-op на Linux/macOS: там немає концепції окремого
// консольного вікна для дочірніх процесів, приховувати нічого.
func hideConsoleWindow(cmd *exec.Cmd) {}
