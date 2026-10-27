//go:build windows

package minecraft

import (
	"os/exec"
	"syscall"
)

// hideConsoleWindow — Windows-реалізація: приховує консольне вікно, яке
// Windows створює для консольних дочірніх процесів (java.exe, cmd /C),
// запущених з GUI-лаунчера. Лаунчер зібрано з -H windowsgui — власної
// консолі не має, тому без цього прапора кожен запуск java.exe (гра,
// Forge processors) або pre/post-команди показував би порожнє вікно
// терміналу, яке блимає і закривається.
//
// ВАЖЛИВО: тут НЕ можна використовувати SysProcAttr.HideWindow (як у
// notify.go/shortcuts.go) — той прапорець ставить STARTF_USESHOWWINDOW +
// SW_HIDE у STARTUPINFO, і Windows застосовує SW_HIDE до ПЕРШОГО вікна
// процесу, що показується через ShowWindow(SW_SHOWDEFAULT). Для java.exe
// це ламає гру: старий LWJGL 2 (Minecraft ≤1.8.9) показує вікно гри саме
// через SW_SHOWDEFAULT, тож гра успішно завантажується (GL-контекст,
// текстури, звук — все ініціалізується), але вікно лишається НЕВИДИМИМ
// («запустив, а екрана гри немає»). Замість цього використовуємо
// CREATE_NO_WINDOW (0x08000000): процес створюється без консольного
// вікна, але GUI-вікна (вікно гри) цим прапорцем НЕ чіпаються — саме так
// запускають гру всі сучасні лаунчери (MultiMC/Prism).
func hideConsoleWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		// CREATE_NO_WINDOW = 0x08000000 (у syscall-пакеті константи немає).
		CreationFlags: 0x08000000,
	}
}
