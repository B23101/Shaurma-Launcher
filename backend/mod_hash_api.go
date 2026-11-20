package main

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"shaurma-launcher-backend/internal/modstore"
)

// «Хешування» модів старих збірок (налаштування hashOldPacks, за замовчуванням
// вимкнено). Див. internal/modstore: лише Modrinth-впізнані моди, вміст файлів
// не змінюється, решта лишається як є.

const hashOldPacksAge = 30 * 24 * time.Hour

func (a *App) modStoreDir() string { return filepath.Join(a.cfg.CacheDir(), "mod-store") }

// startModHashJob запускає фонову перевірку: через 3 хв після старту і далі раз на 12 год.
func (a *App) startModHashJob() {
	go func() {
		time.Sleep(3 * time.Minute)
		for {
			a.runModHashPass()
			time.Sleep(12 * time.Hour)
		}
	}()
}

// runModHashPass обробляє збірки, які не запускали понад місяць.
func (a *App) runModHashPass() {
	if !a.cfg.GetSettings().HashOldPacks || a.modrinth == nil {
		return
	}
	known := func(hashes []string) (map[string]bool, error) {
		m, err := a.modrinth.GetVersionsByHashes(hashes)
		if err != nil {
			return nil, err
		}
		out := make(map[string]bool, len(m))
		for h := range m {
			out[strings.ToLower(h)] = true
		}
		return out, nil
	}
	for _, root := range []string{a.cfg.InstancesDir(), a.cfg.ShaurmaPacksDir()} {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() || e.Name() == "shaurma_packs" {
				continue
			}
			gameDir := filepath.Join(root, e.Name(), ".minecraft")
			modsDir := filepath.Join(gameDir, "mods")
			if _, err := os.Stat(modsDir); err != nil {
				continue
			}
			if last := modstore.LastActivity(gameDir); last.IsZero() || time.Since(last) < hashOldPacksAge {
				continue
			}
			if !a.cfg.GetSettings().HashOldPacks {
				return
			}
			res, err := modstore.HashDir(a.modStoreDir(), modsDir, known)
			if err != nil {
				log.Printf("mod-hash %s: %v", e.Name(), err)
				return // мережа недоступна — спробуємо наступного разу
			}
			if res.Hashed > 0 {
				log.Printf("mod-hash %s: захешовано %d, заощаджено ~%d КБ", e.Name(), res.Hashed, res.Saved/1024)
			}
		}
	}
}

// GetPackHashedCount — скільки модів збірки захешовано (для бейджа «Захешовано»).
func (a *App) GetPackHashedCount(packID string) int {
	return modstore.CountHashed(a.instanceModsDir(packID))
}
