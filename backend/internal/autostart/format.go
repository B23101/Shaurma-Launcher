package autostart

// format.go — платформо-нейтральне форматування командного рядка
// автозапуску (щоб його можна було тестувати на будь-якій ОС).

// quotedCommand — значення Run-ключа Windows: шлях у лапках (потрібно,
// якщо в шляху є пробіли, напр. "C:\\Program Files\\...") + аргументи.
func quotedCommand(exePath string, args []string) string {
	cmd := `"` + exePath + `"`
	for _, a := range args {
		cmd += " " + a
	}
	return cmd
}

// execLine — значення Exec= у XDG .desktop: шлях без лапок + аргументи.
func execLine(exePath string, args []string) string {
	line := exePath
	for _, a := range args {
		line += " " + a
	}
	return line
}
