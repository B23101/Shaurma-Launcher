package main

// packindex_cache.go — швидкий старт: каталог Шаурма-збірок з ДИСКОВОГО кешу.
//
// ПРОБЛЕМА. GetBuilds() викликав a.packs.GetPackIndex() синхронно, а той:
//   1) для кожного акаунта шле POST /session/verify (послідовно, до 20 с кожен),
//   2) потім GET /packs-index.json (теж до 20 с).
// Кеш був лише в пам'яті на 15 с, тож КОЖЕН запуск лаунчера чекав мережу до
// воркера. А стартове завантаження UI (loadData → Promise.allSettled) чекає
// GetBuilds — отже весь екран («збірки», налаштування, тема) висів, поки
// workers.dev не відповість (з України він часто повільний або недоступний).
//
// РІШЕННЯ. Останній вдалий каталог лежить на диску. GetBuilds одразу віддає
// його, а оновлення йде у фоні; якщо каталог змінився — подія builds:changed
// (фронтенд її вже слухає і перечитує список). Лише на ПЕРШОМУ запуску без
// кешу чекаємо мережу, але не довше packIndexFirstWait.
//
// Безпека кешу: він прив'язаний до набору акаунтів (приватні збірки видно лише
// їм). Змінився набір акаунтів — кеш ігнорується й не показується.

import (
	"shaurma-launcher-backend/internal/crashguard"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"shaurma-launcher-backend/internal/api"
)

const (
	packIndexViewTTL   = 15 * time.Second // як часто оновлювати у фоні
	packIndexFirstWait = 4 * time.Second  // скільки чекаємо мережу, коли кешу немає зовсім
)

type packIndexDisk struct {
	Key     string               `json:"key"`
	SavedAt time.Time            `json:"savedAt"`
	Entries []api.PackIndexEntry `json:"entries"`
}

var packIdx struct {
	mu       sync.Mutex
	disk     bool // спробу прочитати диск уже зроблено
	entries  []api.PackIndexEntry
	have     bool
	key      string
	at       time.Time
	inflight chan struct{} // не nil, поки йде фонове оновлення
}

// packIndexKey — відбиток набору акаунтів (ID, відсортовані).
func (a *App) packIndexKey() string {
	var ids []string
	for _, acc := range a.cfg.GetAccounts() {
		ids = append(ids, acc.ID)
	}
	sort.Strings(ids)
	return strings.Join(ids, "|")
}

func (a *App) packIndexDiskPath() string {
	return filepath.Join(a.cfg.Dir(), "packs-index-cache.json")
}

func (a *App) writePackIndexDisk(key string, entries []api.PackIndexEntry) {
	data, err := json.Marshal(packIndexDisk{Key: key, SavedAt: time.Now(), Entries: entries})
	if err != nil {
		return
	}
	dst := a.packIndexDiskPath()
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
	}
}

// invalidatePackCache — замість прямого a.packs.InvalidatePackCache(): скидає
// і кеш клієнта, і наш «свіжий» штамп (дані лишаються для показу, але
// наступний GetBuilds обов'язково піде оновлюватись у фоні).
func (a *App) invalidatePackCache() {
	packIdx.mu.Lock()
	packIdx.at = time.Time{}
	packIdx.mu.Unlock()
	if a.packs != nil {
		a.packs.InvalidatePackCache()
	}
}

// packIndexForView — каталог для GetBuilds без блокування на мережі.
func (a *App) packIndexForView() []api.PackIndexEntry {
	if a.packs == nil {
		return nil
	}
	key := a.packIndexKey()

	packIdx.mu.Lock()
	if !packIdx.disk {
		packIdx.disk = true
		if data, err := os.ReadFile(a.packIndexDiskPath()); err == nil {
			var d packIndexDisk
			if json.Unmarshal(data, &d) == nil && d.Key == key && len(d.Entries) > 0 {
				packIdx.entries, packIdx.key, packIdx.have = d.Entries, d.Key, true
				// at лишається нульовим → вважається застарілим → піде фонове оновлення.
			}
		}
	}
	usable := packIdx.have && packIdx.key == key
	if usable {
		out := packIdx.entries
		stale := time.Since(packIdx.at) > packIndexViewTTL
		if stale && packIdx.inflight == nil {
			a.startPackIndexRefreshLocked(key)
		}
		packIdx.mu.Unlock()
		return out
	}
	// Кешу немає (перший запуск / змінився набір акаунтів): чекаємо мережу,
	// але обмежено. Решта доїде подією builds:changed.
	if packIdx.inflight == nil {
		a.startPackIndexRefreshLocked(key)
	}
	done := packIdx.inflight
	packIdx.mu.Unlock()

	select {
	case <-done:
	case <-time.After(packIndexFirstWait):
	}
	packIdx.mu.Lock()
	defer packIdx.mu.Unlock()
	if packIdx.have && packIdx.key == key {
		return packIdx.entries
	}
	return nil
}

// startPackIndexRefreshLocked запускає фонове оновлення (викликати під packIdx.mu).
func (a *App) startPackIndexRefreshLocked(key string) {
	done := make(chan struct{})
	packIdx.inflight = done
	go func() {
		defer crashguard.Recover("packindex_cache.go:149")
		list, err := a.packs.GetPackIndex()

		packIdx.mu.Lock()
		changed := false
		if err == nil {
			changed = !packIdx.have || packIdx.key != key || !reflect.DeepEqual(packIdx.entries, list)
			packIdx.entries, packIdx.key, packIdx.have, packIdx.at = list, key, true, time.Now()
		}
		packIdx.inflight = nil
		packIdx.mu.Unlock()
		close(done)

		if err != nil {
			// Без кешу UI мусить знати, чому каталог порожній (як і раніше).
			log.Printf("GetBuilds: GetPackIndex error: %v", err)
			a.emit("packs:index-error", err.Error())
			return
		}
		if changed {
			a.writePackIndexDisk(key, list)
			a.emit("builds:changed", "")
		}
	}()
}
