package main

// sysopen.go — відкриття теки/файлу/URL системним обробником.
//
// На Linux це xdg-open, і ЗАПУСКАТИ його з успадкованим os.Environ()
// не можна: у середовищі лаунчера лежать наші compat-змінні для
// WebKitGTK (LIBGL_ALWAYS_SOFTWARE=1 тощо) і, якщо лаунчер запущено з
// AppImage, підмінені LD_LIBRARY_PATH/GTK_PATH/GIO_MODULE_DIR, що ведуть
// усередину бандла. Файловий менеджер чи браузер, запущений з таким
// середовищем, або стартує кілька секунд, або падає мовчки (підхоплює
// НАШІ бібліотеки замість системних). internal/childenv прибирає рівно
// це — див. коментарі там.
//
// Додатково дочірній процес «пожинається» (cmd.Wait у горутині): раніше
// кожен виклик exec.Command(...).Start() лишав по зомбі-процесу, бо на
// Start ніхто не чекав.

import (
	"os/exec"

	"shaurma-launcher-backend/internal/childenv"
)

// xdgOpen відкриває шлях або URL системним обробником (Linux/BSD).
// Повертає помилку лише якщо сам xdg-open не вдалося запустити —
// результат роботи обробника асинхронний і викликачу не потрібен.
func xdgOpen(target string) error {
	if target == "" {
		return nil
	}
	cmd := exec.Command("xdg-open", target)
	cmd.Env = childenv.Clean()
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
