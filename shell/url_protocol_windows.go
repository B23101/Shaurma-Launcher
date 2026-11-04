//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// Схема shaurma:// (запрошення в друзі з сайту) реєструється установлювачем
// (installer/installer.iss, [Registry]). Але лаунчер, який оновився сам через
// вбудоване оновлення, установлювач не запускає — у старих установках схеми
// немає, і браузер не пропонує «Відкрити Shaurma Launcher?». Тому під час
// запуску лаунчер відновлює ті самі ключі в HKCU (без прав адміністратора).
// Пишемо лише свої ключі й лише коли вони відсутні або ведуть на інший exe.

const urlProtocolKey = `Software\Classes\shaurma`

func urlProtocolCommand(exe string) string {
	return `"` + exe + `" "%1"`
}

// ensureURLProtocol повертає true, якщо ключі довелося записати.
func ensureURLProtocol() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	want := urlProtocolCommand(exe)
	if k, err := registry.OpenKey(registry.CURRENT_USER, urlProtocolKey+`\shell\open\command`, registry.QUERY_VALUE); err == nil {
		got, _, gerr := k.GetStringValue("")
		k.Close()
		if gerr == nil && strings.EqualFold(got, want) {
			return false
		}
	}
	set := func(path, name, value string) error {
		k, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.SET_VALUE)
		if err != nil {
			return err
		}
		defer k.Close()
		return k.SetStringValue(name, value)
	}
	if set(urlProtocolKey, "", "URL:Shaurma Launcher") != nil ||
		set(urlProtocolKey, "URL Protocol", "") != nil ||
		set(urlProtocolKey+`\DefaultIcon`, "", exe+",0") != nil ||
		set(urlProtocolKey+`\shell\open\command`, "", want) != nil {
		return false
	}
	return true
}
