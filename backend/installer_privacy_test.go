package main

import (
	"os"
	"path/filepath"
	"testing"

	"shaurma-launcher-backend/internal/config"
)

func TestInstallerSkinOptOutIsAppliedOnce(t *testing.T) {
	dir := t.TempDir()
	cfg := config.NewManager(dir)
	if err := cfg.SetAppDir(filepath.Join(dir, "data")); err != nil {
		t.Fatal(err)
	}
	a := &App{cfg: cfg}
	path := filepath.Join(a.cfg.InstallDir(), installerDisableSkinsMarker)
	if err := os.WriteFile(path, []byte("disabled"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.applyInstallerSkinPreference(); err != nil {
		t.Fatal(err)
	}
	if !a.cfg.GetSettings().DisableSkins {
		t.Fatal("installer opt-out was ignored")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("one-time installer preference was not consumed")
	}
	settings := a.cfg.GetSettings()
	settings.DisableSkins = false
	if err := a.cfg.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	if err := a.applyInstallerSkinPreference(); err != nil {
		t.Fatal(err)
	}
	if a.cfg.GetSettings().DisableSkins {
		t.Fatal("later user choice was overwritten")
	}
}
