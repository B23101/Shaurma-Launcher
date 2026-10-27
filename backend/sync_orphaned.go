package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"time"

	"shaurma-launcher-backend/internal/builds"
	"shaurma-launcher-backend/internal/custompack"
)

// syncOrphanedToCustomPacks сканує buildsRegistry і реєструє в customPacks
// ВСІ збірки, яких там ще немає. Викликається при старті ПІСЛЯ buildsReg.Load()
// і ДО будь-яких API-викликів (launch, rename, configure тощо).
func (a *App) syncOrphanedToCustomPacks() {
	if a.customPacks == nil || a.buildsReg == nil {
		return
	}

	settings := a.cfg.GetSettings()
	if settings.InstanceDir == "" {
		return
	}

	registered := 0
	for _, id := range a.buildsReg.AllIDs() {
		// Пропускаємо якщо вже є в customPacks
		if _, exists := a.customPacks.Get(id); exists {
			continue
		}
		// Пропускаємо Shaurma-збірки — вони НІКОЛИ не стають кастомними.
		if entry, ok := a.buildsReg.Get(id); ok && entry.Kind == builds.KindShaurma {
			continue
		}
		// Пропускаємо якщо папка вже є в shaurma_packs/ — це Shaurma-збірка,
		// яка лишилась в installations/ після невдалої/незавершеної міграції.
		if _, err := os.Stat(filepath.Join(a.cfg.ShaurmaPacksDir(), id)); err == nil {
			continue
		}

		// Читаємо instance.json з дефолтної теки кастомних збірок
		instDir := filepath.Join(a.cfg.InstancesDir(), id)
		cfgPath := filepath.Join(instDir, "instance.json")
		raw, err := os.ReadFile(cfgPath)
		if err != nil {
			continue
		}

		// Читаємо ПОВНІСТЮ як Pack (а НЕ як InstanceConfig) щоб
		// не втратити унікальні поля (importState, blockedMods тощо).
		var pack custompack.Pack
		if json.Unmarshal(raw, &pack) != nil {
			continue
		}
		if pack.ID == "" {
			pack.ID = id
		}
		if pack.Name == "" {
			pack.Name = id
		}
		if pack.Loader == "" {
			pack.Loader = "vanilla"
		}
		if pack.CreatedAt.IsZero() {
			pack.CreatedAt = time.Now()
		}
		pack.UpdatedAt = time.Now()
		if err := a.customPacks.Save(pack); err != nil {
			log.Printf("sync: помилка реєстрації %s в customPacks: %v", id, err)
			continue
		}
		registered++
	}

	if registered > 0 {
		log.Printf("sync: зареєстровано %d orphaned збірок в customPacks", registered)
	}
}
