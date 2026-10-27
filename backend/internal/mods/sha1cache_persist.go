package mods

// sha1cache_persist.go — збереження modSha1Cache на диск між запусками
// лаунчера.
//
// ПРОБЛЕМА: modSha1Cache (manager.go) вже кешує SHA1 за (шлях, розмір,
// mtime) — правильна ідея, паралельний скан на 8 воркерів теж уже є.
// Але сам кеш — це проста мапа `var modSha1Cache = map[...]string{}` у
// пам'яті процесу: коли лаунчер закривається, кеш зникає разом з ним.
// Тому "перший раз довго" повторюється НЕ один раз за все життя збірки,
// а щоразу після перезапуску лаунчера — саме це й дає видиму різницю з
// референс-лаунчерами (там кеш хешів лежить у SQLite/на диску і
// переживає перезапуск: список 100+ модів після рестарту з'являється
// миттєво, бо жоден jar узагалі не відкривається повторно).
//
// ДЕ ЛЕЖИТЬ ФАЙЛ КЕШУ (ВАЖЛИВО): НЕ в mods/, НЕ будь-де під
// .minecraft/текою збірки. Тека модів належить гравцю й самій збірці —
// синхронізується з маніфестом, копіюється при дублюванні збірки,
// відкривається гравцем у провіднику — сторонній службовий файл лаунчера
// там неприпустимий. Кеш живе в СЛУЖБОВІЙ теці лаунчера:
// <CacheDir>/mod-sha1-cache/<hash-of-modsDir>.json — по одному невеликому
// файлу на кожну фізичну теку модів, з іменем, похідним від шляху (а не
// від, наприклад, PackID), щоб той самий фізичний mods/ (напр. спільний
// між збірками через симлінк чи override) завжди мапився на той самий
// файл кешу незалежно від того, з якої збірки до нього звернулись.
// Manager отримує CacheDir через SetCacheDir (викликається з App одразу
// після mods.NewManager() і повторно в rewireComponents при зміні теки
// даних лаунчера — manager.go/app.go).
//
// Якщо CacheDir ще не встановлено (порожній рядок — Manager створений
// без SetCacheDir, наприклад у тестах) — персистентність просто
// вимкнена: Sha1Of/ScanModsFull поводяться так само, як до цієї фічі
// (кеш лише в пам'яті процесу, нічого не пишеться на диск, нічого не
// ламається).
//
// РОЗМІР: один запис — ім'я jar-файлу + розмір/mtime/sha1 — це
// приблизно 100-140 байт у JSON. Навіть 300-400 модів в одній збірці —
// кілька десятків кілобайт у службовій теці лаунчера, на тлі самих
// jar-файлів (мегабайти кожен) абсолютно непомітно і жодним чином не
// впливає на розмір самого лаунчера (це runtime-дані в CacheDir
// користувача, не частина бінарника). Жодної нової залежності не
// додано — лише encoding/json, os, path/filepath, crypto/sha1,
// encoding/hex, які й так у стандартній бібліотеці.

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// sha1CacheSubdir — підтека всередині службової CacheDir лаунчера, де
// лежать файли кешу (по одному на фізичну теку модів).
const sha1CacheSubdir = "mod-sha1-cache"

// sha1CacheEntry — один запис: SHA1 дійсний ЛИШЕ доки розмір і mtime
// файлу на диску збігаються з тим, що тут записано.
type sha1CacheEntry struct {
	Size  int64  `json:"size"`
	Mtime int64  `json:"mtime"` // те саме значення, що jarCacheKey.mtime (Unix-секунди)
	SHA1  string `json:"sha1"`
}

// sha1CacheFile — формат файлу кешу. Ключ мапи — ім'я файлу
// (filepath.Base), а не повний шлях: файл кешу вже прив'язаний до
// конкретної теки модів своїм власним ім'ям (похідним від шляху теки),
// тож усередині достатньо відносного ключа.
type sha1CacheFile struct {
	Entries map[string]sha1CacheEntry `json:"entries"`
}

var (
	// diskCacheMu захищає доступ до diskCacheLoaded/diskCacheDirty —
	// окремий мутекс від modSha1CacheMu (manager.go), щоб довге
	// збереження на диск (flush) не тримало заблокованим гарячий шлях
	// читання кешу під час паралельного сканування модів.
	diskCacheMu     sync.Mutex
	diskCacheLoaded = map[string]bool{} // modsDir (Clean) → чи вже підвантажили файл кешу цієї теки в modSha1Cache
	diskCacheDirty  = map[string]bool{} // modsDir (Clean) → чи є незбережені зміни для цієї теки
)

// resetDiskSha1CacheLoadedFlags скидає позначки "вже підвантажено з
// диска" для ВСІХ тек модів — викликається з PruneModSha1Cache (manager.go)
// одразу після очищення переповненого modSha1Cache в пам'яті. Без цього
// скидання наступний Sha1Of для вже "забутого" ключа вважав би, що диск
// уже читали цієї сесії (diskCacheLoaded[modsDir]==true), і рахував би
// SHA1 з нуля — хоча готовий хеш весь цей час лежить у файлі на диску.
func resetDiskSha1CacheLoadedFlags() {
	diskCacheMu.Lock()
	diskCacheLoaded = map[string]bool{}
	diskCacheMu.Unlock()
}

// sha1CachePath повертає шлях до файлу кешу для вказаної теки модів,
// УСЕРЕДИНІ службової CacheDir лаунчера — НІКОЛИ всередині самої modsDir.
// Порожній cacheDir (SetCacheDir ще не викликали) — повертає "", і
// виклики нижче трактують це як "персистентність вимкнена".
func sha1CachePath(cacheDir, modsDir string) string {
	if cacheDir == "" {
		return ""
	}
	h := sha1.Sum([]byte(filepath.Clean(modsDir)))
	name := hex.EncodeToString(h[:]) + ".json"
	return filepath.Join(cacheDir, sha1CacheSubdir, name)
}

// loadDiskSha1Cache підвантажує збережений на диску кеш SHA1 для
// modsDir У modSha1Cache (у пам'яті), якщо ще не робили цього в межах
// поточної сесії лаунчера. Викликається лениво, перед скануванням теки
// (ScanModsFull/Sha1Of) — не додає жодного I/O для тек, які взагалі не
// сканують.
//
// ВАЖЛИВО: тут лише ДОПОВНЮЄМО modSha1Cache записами з диска — не
// перезаписуємо повністю. Якщо для когось з файлів кеш у пам'яті вже є
// (наприклад інша збірка посилається на той самий jar через спільну
// override-теку), свіжіший запис у пам'яті не губиться.
func (m *Manager) loadDiskSha1Cache(modsDir string) {
	if m.cacheDir == "" {
		return // персистентність вимкнена (SetCacheDir не викликали)
	}
	modsDir = filepath.Clean(modsDir)
	diskCacheMu.Lock()
	if diskCacheLoaded[modsDir] {
		diskCacheMu.Unlock()
		return
	}
	diskCacheLoaded[modsDir] = true
	diskCacheMu.Unlock()

	path := sha1CachePath(m.cacheDir, modsDir)
	raw, err := os.ReadFile(path)
	if err != nil {
		return // немає файлу кешу для цієї теки — нормально (перший скан)
	}
	var parsed sha1CacheFile
	if json.Unmarshal(raw, &parsed) != nil || parsed.Entries == nil {
		return // пошкоджений файл — ігноруємо, кеш "прогріється" заново
	}

	modSha1CacheMu.Lock()
	for name, e := range parsed.Entries {
		key := jarCacheKey{path: filepath.Join(modsDir, name), size: e.Size, mtime: e.Mtime}
		// Не затираємо запис, який паралельний скан уже встиг порахувати
		// заново (напр. файл змінився між збереженням кешу і зараз) —
		// пишемо лише якщо ключа ще немає в пам'яті.
		if _, exists := modSha1Cache[key]; !exists {
			modSha1Cache[key] = e.SHA1
		}
	}
	modSha1CacheMu.Unlock()
}

// markSha1CacheDirty позначає, що для modsDir з'явились нові хеші, які
// варто зберегти на диск (викликається з Sha1Of при кеш-міс).
func markSha1CacheDirty(modsDir string) {
	modsDir = filepath.Clean(modsDir)
	diskCacheMu.Lock()
	diskCacheDirty[modsDir] = true
	diskCacheMu.Unlock()
}

// FlushSha1Cache зберігає на диск усі записи modSha1Cache, що стосуються
// файлів у modsDir — ОДНИМ атомарним записом у службову CacheDir (не в
// саму modsDir, не на кожен окремий jar). Викликається після
// ScanModsFull (manager.go). Пропущений виклик не ламає коректність —
// це лише оптимізація швидкості: без збереження наступний запуск
// лаунчера просто порахує хеші заново, як робив завжди.
func (m *Manager) FlushSha1Cache(modsDir string) {
	if m.cacheDir == "" {
		return // персистентність вимкнена
	}
	modsDir = filepath.Clean(modsDir)
	diskCacheMu.Lock()
	dirty := diskCacheDirty[modsDir]
	if dirty {
		delete(diskCacheDirty, modsDir)
	}
	diskCacheMu.Unlock()
	if !dirty {
		return
	}

	prefix := modsDir + string(filepath.Separator)
	out := sha1CacheFile{Entries: map[string]sha1CacheEntry{}}

	modSha1CacheMu.Lock()
	for key, hash := range modSha1Cache {
		if !strings.HasPrefix(key.path, prefix) {
			continue
		}
		name := filepath.Base(key.path)
		out.Entries[name] = sha1CacheEntry{Size: key.size, Mtime: key.mtime, SHA1: hash}
	}
	modSha1CacheMu.Unlock()

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return
	}
	path := sha1CachePath(m.cacheDir, modsDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	tmp := path + ".tmp"
	if os.WriteFile(tmp, data, 0o644) == nil {
		os.Rename(tmp, path)
	}
}
