//go:build windows

package main

import (
	"os"
	"path/filepath"
)

// exeDir — на Windows installDir завжди лишається текою, де фізично
// лежить ShaurmLauncher.exe: саме туди кладе файли інсталятор
// (installer.iss), і саме там exe має право на запис (звичайна тека
// користувача чи Program Files з відповідними правами). Поведінка НЕ
// змінена відносно попередньої спільної реалізації.
func exeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(exe)
}
