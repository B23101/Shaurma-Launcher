//go:build windows

package servers

import (
	"os/exec"
	"syscall"
)

// hideConsoleWindow приховує консольне вікно для допоміжних процесів, які
// запускає менеджер серверів (netstat, tasklist, taskkill, powershell,
// сам java-процес сервера). Лаунчер зібраний з -H windowsgui і власної
// консолі не має — без цього прапорця КОЖЕН такий виклик (а деякі
// повторюються раз на секунду, напр. збір CPU-статистики) відкривав би
// власне видиме консольне вікно, яке блимає на екрані користувача.
//
// CREATE_NO_WINDOW (0x08000000) — процес створюється без консольного
// вікна взагалі. На відміну від SysProcAttr.HideWindow (SW_HIDE), це
// безпечно й для java.exe самого сервера: сервер запускається з --nogui
// і жодного GUI-вікна не показує, тож ефекту «запустив, а вікна немає»
// (як буває з клієнтською грою) тут не існує.
func hideConsoleWindow(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	// CREATE_NO_WINDOW = 0x08000000 (у пакеті syscall константи немає).
	cmd.SysProcAttr.CreationFlags |= 0x08000000
}
