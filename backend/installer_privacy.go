package main

import (
	"errors"
	"os"
	"path/filepath"
)

const installerDisableSkinsMarker = "disable-skins-once"

// Consume the installer's opt-out before account or game services start.
// Later changes in launcher settings remain under the user's control.
func (a *App) applyInstallerSkinPreference() error {
	if a.cfg.InstallDir() == "" {
		return nil // Development/test configurations have no installer directory.
	}
	path := filepath.Join(a.cfg.InstallDir(), installerDisableSkinsMarker)
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	settings := a.cfg.GetSettings()
	settings.DisableSkins = true
	if err := a.cfg.UpdateSettings(settings); err != nil {
		return err
	}
	return os.Remove(path)
}
