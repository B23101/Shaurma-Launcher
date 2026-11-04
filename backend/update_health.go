package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"shaurma-launcher-backend/internal/crashguard"
)

// updateHealthFile — контракт з оновлювачем (updater-backend/internal/newupdate:
// HealthFileName): {"version": "...", "at": "..."}.
const updateHealthFile = "update-health.json"

// updateHealthDelay — скільки backend має пропрацювати без краху, щоб версія
// вважалась робочою.
const updateHealthDelay = 20 * time.Second

func markUpdateHealthy(installDir, version string) {
	defer crashguard.Recover("update_health.go:markUpdateHealthy")
	if installDir == "" || version == "" {
		return
	}
	time.Sleep(updateHealthDelay)
	writeUpdateHealth(installDir, version, time.Now().UTC())
}

func writeUpdateHealth(installDir, version string, at time.Time) {
	data, err := json.Marshal(struct {
		Version string    `json:"version"`
		At      time.Time `json:"at"`
	}{version, at})
	if err != nil {
		return
	}
	path := filepath.Join(installDir, updateHealthFile)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}
