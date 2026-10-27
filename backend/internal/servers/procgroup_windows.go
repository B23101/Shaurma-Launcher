//go:build windows

package servers

import (
	"os/exec"
	"syscall"
)

// setNewProcessGroup ставить CREATE_NEW_PROCESS_GROUP для дочірнього
// java-процесу сервера. Винесено в окремий platform-split файл за тим
// самим принципом, що вже використовує hideConsoleWindow
// (hide_window_windows.go/hide_window_other.go) — syscall.CREATE_NEW_
// PROCESS_GROUP існує лише в windows-варіанті пакета syscall, тож пряме
// використання в process.go (як було раніше, без build-тегів) не
// компілюється на Linux/macOS. Це не змінює жодної повʼязаної з розділами
// 1-3 логіки — виправлення того, що інакше ламало б збірку на не-Windows
// платформах.
func setNewProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= syscall.CREATE_NEW_PROCESS_GROUP
}
