package main

import (
	"fmt"
	"os"
)

// OpenComponentsFolder opens the installed shell's directory, not game data.
func (a *App) OpenComponentsFolder() error {
	dir := backendInitConfig.InstallDir
	if dir == "" {
		dir = a.cfg.ConfigDir()
	}
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("теку компонентів не знайдено")
	}
	return openPathInExplorer(dir)
}
