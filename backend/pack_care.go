package main

import (
	"fmt"
	"os"
	"path/filepath"
	"shaurma-launcher-backend/internal/packcare"
	"strings"
	"sync"
)

var packCareMu sync.RWMutex

func (a *App) careDir(id string) (string, error) {
	if id == "" || filepath.Base(id) != id || id == "." || id == ".." || strings.ContainsAny(id, `\/:`) {
		return "", fmt.Errorf("некоректна збірка")
	}
	kind, err := a.packKind(id)
	if err != nil {
		return "", err
	}
	for _, s := range a.sessionsM.Snapshot() {
		if s.BuildID == id {
			return "", fmt.Errorf("спочатку закрийте гру")
		}
	}
	for _, active := range a.GetActiveSyncPackIDs() {
		if active == id {
			return "", fmt.Errorf("дочекайтеся завершення завантаження")
		}
	}
	return a.packDir(id, kind), nil
}

func (a *App) ApplyPerformancePreset(id, preset string) error {
	packCareMu.Lock()
	defer packCareMu.Unlock()
	dir, err := a.careDir(id)
	if err != nil {
		return err
	}
	// Never replace managed mods, loader versions or author JVM flags.
	return packcare.ApplyPreset(dir, preset)
}

func (a *App) GetPerformancePreset(id string) (bool, error) {
	dir, err := a.careDir(id)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(filepath.Join(dir, "performance-original.json"))
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}

func (a *App) CreatePackWorldSnapshot(id string) (string, error) {
	packCareMu.Lock()
	defer packCareMu.Unlock()
	dir, err := a.careDir(id)
	if err != nil {
		return "", err
	}
	return packcare.Snapshot(dir)
}

func (a *App) ListPackWorldSnapshots(id string) ([]string, error) {
	dir, err := a.careDir(id)
	if err != nil {
		return nil, err
	}
	return packcare.List(dir)
}

func (a *App) RestorePackWorldSnapshot(id, snapshot string) ([]string, error) {
	packCareMu.Lock()
	defer packCareMu.Unlock()
	dir, err := a.careDir(id)
	if err != nil {
		return nil, err
	}
	return packcare.Restore(dir, snapshot)
}
