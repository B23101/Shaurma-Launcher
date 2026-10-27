//go:build !windows

package minecraft

import "os/exec"

// hideConsoleWindow — no-op на Linux/macOS: там консольні вікна не
// створюються системою для дочірніх процесів (немає концепції
// «GUI-підсистема без консолі»), тож приховувати нічого.
func hideConsoleWindow(cmd *exec.Cmd) {}
