package mods

// metacache_persist.go — збереження jarMetaCache (розібрані метадані jar:
// назва, версія, лоадери, залежності, зменшена іконка) на диск між
// запусками лаунчера.
//
// Той самий принцип, що sha1cache_persist.go і кеш Modrinth App (там
// список вмісту збірки береться з SQLite-кешу за шляхом+розміром+mtime, і
// незмінені файли взагалі не відкриваються). До цього файлу на диску
// жили лише SHA1, а метадані — тільки в пам'яті процесу: після кожного
// перезапуску лаунчер знову відкривав КОЖЕН jar, розбирав toml/json і
// декодував/зменшував іконку. Тепер незмінений jar після рестарту не
// читається зовсім — скан теки з сотнями модів займає частки секунди.
//
// Файл лежить у службовій CacheDir лаунчера (НЕ в теці модів):
// <CacheDir>/mod-meta-cache/<hash-of-modsDir>.json. Порожній cacheDir —
// персистентність вимкнена, усе працює як раніше (лише кеш у пам'яті).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const metaCacheSubdir = "mod-meta-cache"

// metaCacheVersion — підняти, якщо змінюється розбір jar (parseJar) або
// формат jarMeta: старі файли кешу тоді ігноруються і прогріваються заново.
const metaCacheVersion = 3 // 2: метадані плагінів серверів (plugin_meta.go); 3: вкладений мод з Embedded-Dependencies-Mod

type metaCacheEntry struct {
	Size  int64    `json:"size"`
	Mtime int64    `json:"mtime"` // те саме значення, що jarCacheKey.mtime
	Meta  *jarMeta `json:"meta"`
}

type metaCacheFile struct {
	Version int                       `json:"version"`
	Entries map[string]metaCacheEntry `json:"entries"`
}

var (
	metaDiskMu     sync.Mutex
	metaDiskLoaded = map[string]bool{} // modsDir (Clean) → файл кешу вже підвантажено в jarMetaCache
	metaDiskDirty  = map[string]bool{} // modsDir (Clean) → є нові розібрані jar, які варто зберегти
)

// resetDiskMetaCacheLoadedFlags — після чистки jarMetaCache наступний скан
// має знову підтягнути записи з диска, а не розбирати jar з нуля.
func resetDiskMetaCacheLoadedFlags() {
	metaDiskMu.Lock()
	metaDiskLoaded = map[string]bool{}
	metaDiskMu.Unlock()
}

func metaCachePath(cacheDir, modsDir string) string {
	if cacheDir == "" {
		return ""
	}
	return filepath.Join(cacheDir, metaCacheSubdir, filepath.Base(sha1CachePath(cacheDir, modsDir)))
}

// loadDiskMetaCache доповнює jarMetaCache записами з диска для modsDir
// (один раз за сесію, або після чистки кешу). Наявні в пам'яті записи не
// затираються.
func (m *Manager) loadDiskMetaCache(modsDir string) {
	if m.cacheDir == "" {
		return
	}
	modsDir = filepath.Clean(modsDir)
	metaDiskMu.Lock()
	if metaDiskLoaded[modsDir] {
		metaDiskMu.Unlock()
		return
	}
	metaDiskLoaded[modsDir] = true
	metaDiskMu.Unlock()

	raw, err := os.ReadFile(metaCachePath(m.cacheDir, modsDir))
	if err != nil {
		return
	}
	var parsed metaCacheFile
	if json.Unmarshal(raw, &parsed) != nil || parsed.Version != metaCacheVersion || parsed.Entries == nil {
		return // старий формат або пошкоджений файл — прогріється заново
	}
	now := time.Now()
	jarMetaCacheMu.Lock()
	for name, e := range parsed.Entries {
		if e.Meta == nil {
			continue
		}
		key := jarCacheKey{path: filepath.Join(modsDir, name), size: e.Size, mtime: e.Mtime}
		if _, exists := jarMetaCache[key]; !exists {
			e.Meta.at = now
			jarMetaCache[key] = e.Meta
		}
	}
	// Бюджет пам'яті кешу діє і тут: раніше записи з диска вливались без
	// обрізання, і після перегляду сотні збірок у пам'яті лежали метадані
	// (з іконками) всіх їхніх модів. Першими йдуть записи давніше
	// відкритих тек — їх at старший за щойно підвантажені.
	pruneJarMetaCacheLocked(now.Add(-30*time.Minute), 512)
	jarMetaCacheMu.Unlock()
}

func markMetaCacheDirty(modsDir string) {
	modsDir = filepath.Clean(modsDir)
	metaDiskMu.Lock()
	metaDiskDirty[modsDir] = true
	metaDiskMu.Unlock()
}

// FlushMetaCache зберігає на диск метадані всіх jar з modsDir, що є в
// jarMetaCache, одним атомарним записом. Викликається після скану теки;
// нічого не пише, якщо нових розборів не було.
func (m *Manager) FlushMetaCache(modsDir string) {
	if m.cacheDir == "" {
		return
	}
	modsDir = filepath.Clean(modsDir)
	metaDiskMu.Lock()
	dirty := metaDiskDirty[modsDir]
	delete(metaDiskDirty, modsDir)
	metaDiskMu.Unlock()
	if !dirty {
		return
	}

	prefix := modsDir + string(filepath.Separator)
	out := metaCacheFile{Version: metaCacheVersion, Entries: map[string]metaCacheEntry{}}
	jarMetaCacheMu.Lock()
	for key, meta := range jarMetaCache {
		if meta == nil || !strings.HasPrefix(key.path, prefix) || strings.ContainsRune(key.path[len(prefix):], filepath.Separator) {
			continue
		}
		out.Entries[filepath.Base(key.path)] = metaCacheEntry{Size: key.size, Mtime: key.mtime, Meta: meta}
	}
	data, err := json.Marshal(out)
	jarMetaCacheMu.Unlock()
	if err != nil {
		return
	}
	path := metaCachePath(m.cacheDir, modsDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	tmp := path + ".tmp"
	if os.WriteFile(tmp, data, 0o644) == nil {
		os.Rename(tmp, path)
	}
}
