//go:build !windows

package servers

import "os/exec"

// setNewProcessGroup — no-op на Linux/macOS: CREATE_NEW_PROCESS_GROUP —
// суто Windows-концепція (потрібна лише для того, щоб пізніше можна було
// надіслати CTRL_BREAK_EVENT конкретно дочірньому java-процесу, не
// зачепивши сам лаунчер). На Unix graceful stop і так іде через stdin
// ("stop\n"), а kill — через process group Unix-way, окремого прапорця
// при створенні процесу не потрібно.
func setNewProcessGroup(cmd *exec.Cmd) {}
