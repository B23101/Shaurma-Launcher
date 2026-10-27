package sync

// filehashcache.go — кеш "хеш файлу за (шлях, розмір, час модифікації)".
//
// ПРОБЛЕМА (звідки взялись довгі 1-2+ секунди на сканування 100+ модів):
// localHashMatches РАНІШЕ завжди відкривала jar і рахувала SHA1/SHA256 з
// нуля — навіть якщо файл не змінювався з минулого разу. При 100+ модах
// по кілька мегабайт кожен це сотні мегабайт реального читання диска +
// CPU на хешування ПРИ КОЖНОМУ відкритті менеджера модів чи запуску
// синхронізації, хоча реально нового на диску нічого не з'явилось.
//
// РІШЕННЯ (той самий принцип, що в референсних лаунчерах на Rust —
// ключ кешу "v2-{розмір}-{час_модифікації}-{шлях}"): якщо файл на диску
// має ТОЙ САМИЙ розмір і ТОЙ САМИЙ час модифікації, що й під час
// попереднього хешування — його вміст ФІЗИЧНО не міг змінитися без зміни
// хоча б одного з цих двох чисел (перезапис файлу завжди оновлює mtime).
// Тож хеш можна взяти з кешу, не відкриваючи jar взагалі. Файл змінився
// (оновлення мода, ручна заміна гравцем) → розмір або mtime інші → ключ
// інший → кеш-міс → хеш рахується як і раніше, і одразу зберігається.
//
// ДЕ ЛЕЖИТЬ ФАЙЛ КЕШУ (ВАЖЛИВО): НЕ всередині теки встановлення збірки
// (InstallDir) і НЕ у mods/ чи будь-де під .minecraft. Тека збірки — це
// простір гравця/збірки, а не сховище лаунчера: вона синхронізується з
// маніфестом, копіюється при дублюванні збірки, потрапляє в бекапи, і
// сторонній службовий файл там — це сміття, яке гравець ніколи не просив.
// Кеш живе в службовій теці лаунчера (<AppDir>/cache/mod-hash-cache/,
// та сама CacheDir(), де вже лежать інші кеші лаунчера — game_langs,
// AI-аналіз тощо) — по одному невеликому JSON-файлу на PackID, з іменем
// файлу, похідним від InstallDir (щоб та сама збірка після переїзду на
// новий шлях не плуталась із записами з інших збірок). Runner отримує
// цю теку через StateStore.DataDir() (state.go) — той самий корінь
// даних лаунчера, куди вже пишеться install-state/.
//
// РОЗМІР НА ДИСКУ: один запис кешу — це один рядок JSON приблизно
// 120-160 байт (шлях + розмір + mtime + два hex-хеші). Навіть для
// збірки з 500 модами файл кешу важить одиниці-десятки кілобайт —
// незрівнянно менше за самі jar-и, і жодним чином не впливає на розмір
// самого лаунчера (це runtime-дані користувача в службовій теці, не
// частина бінарника і не файл усередині збірки).

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"shaurma-launcher-backend/internal/atomicfile"
)

// fileHashCacheSubdir — підтека всередині службової CacheDir лаунчера,
// де лежать файли кешу хешів (по одному на збірку).
const fileHashCacheSubdir = "mod-hash-cache"

// fileHashCacheEntry — один запис кешу: хеш файлу, дійсний ЛИШЕ доки
// розмір і час модифікації файлу на диску збігаються з записаними тут.
type fileHashCacheEntry struct {
	Size    int64  `json:"size"`
	ModNano int64  `json:"modNano"` // ModTime().UnixNano() — точніше за секунди
	SHA1    string `json:"sha1,omitempty"`
	SHA256  string `json:"sha256,omitempty"`
}

// fileHashCacheData — формат файлу кешу. Ключ мапи — destRelPath
// (напр. "mods/somemod.jar") відносно InstallDir цієї збірки.
type fileHashCacheData struct {
	Entries map[string]fileHashCacheEntry `json:"entries"`
}

// fileHashCache — потокобезпечна обгортка над fileHashCacheData з
// лінивим завантаженням і атомарним збереженням. Один екземпляр — на
// одну збірку (Runner тримає свій у полі hashCache).
type fileHashCache struct {
	mu     sync.Mutex
	path   string
	data   fileHashCacheData
	loaded bool
	dirty  bool // чи є незбережені зміни (уникаємо зайвого запису на диск)
}

// cacheFilePathFor будує шлях до файлу кешу в службовій теці лаунчера
// (НЕ в теці збірки — див. коментар вище). launcherDataDir — корінь
// даних лаунчера (StateStore.DataDir()); packID + короткий хеш від
// installDir у імені файлу гарантують, що дублікати/перейменовані
// збірки з тим самим PackID, але різним фізичним шляхом, не плутають
// кеші один одного.
func cacheFilePathFor(launcherDataDir, packID, installDir string) string {
	h := sha1.Sum([]byte(filepath.Clean(installDir)))
	suffix := hex.EncodeToString(h[:])[:10]
	safeID := packID
	if safeID == "" {
		safeID = "unknown"
	}
	return filepath.Join(launcherDataDir, "cache", fileHashCacheSubdir, safeID+"-"+suffix+".json")
}

// newFileHashCache створює кеш для вказаної збірки. Сам файл ще не
// читається тут (лінива ініціалізація в load()) — щоб створення
// Runner/кешу ніколи не було блокуючою операцією диска.
func newFileHashCache(launcherDataDir, packID, installDir string) *fileHashCache {
	return &fileHashCache{path: cacheFilePathFor(launcherDataDir, packID, installDir)}
}

// load читає файл кешу з диска один раз за час життя fileHashCache.
// Відсутній/пошкоджений файл — не помилка: просто стартуємо з порожнім
// кешем (перше хешування кожного мода відбудеться як і раніше, з нуля).
func (c *fileHashCache) load() {
	if c.loaded {
		return
	}
	c.loaded = true
	c.data.Entries = map[string]fileHashCacheEntry{}

	raw, err := os.ReadFile(c.path)
	if err != nil {
		return // немає файлу — це нормально (перший запуск для цієї збірки)
	}
	var parsed fileHashCacheData
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return // пошкоджений JSON — ігноруємо, кеш просто "прогріється" заново
	}
	if parsed.Entries != nil {
		c.data.Entries = parsed.Entries
	}
}

// get повертає закешований хеш файлу, ЯКЩО розмір і mtime збігаються з
// тим, що записано в кеші (тобто файл гарантовано не змінювався з
// моменту, коли хеш рахували востаннє). ok=false — треба порахувати
// хеш із вмісту файлу, як робилось раніше.
func (c *fileHashCache) get(relPath string, size, modNano int64) (sha1hex, sha256hex string, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.load()

	e, exists := c.data.Entries[relPath]
	if !exists || e.Size != size || e.ModNano != modNano {
		return "", "", false
	}
	return e.SHA1, e.SHA256, true
}

// put зберігає щойно порахований хеш у кеш (у пам'яті) і позначає кеш
// "брудним" — реальний запис на диск відбувається пізніше, одним разом
// через flush(), а не на кожен окремий файл (інакше 100+ модів дали б
// 100+ окремих операцій запису замість однієї в кінці сканування).
func (c *fileHashCache) put(relPath string, size, modNano int64, sha1hex, sha256hex string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.load()

	e := c.data.Entries[relPath]
	e.Size = size
	e.ModNano = modNano
	if sha1hex != "" {
		e.SHA1 = sha1hex
	}
	if sha256hex != "" {
		e.SHA256 = sha256hex
	}
	c.data.Entries[relPath] = e
	c.dirty = true
}

// flush зберігає накопичені зміни на диск ОДНИМ атомарним записом
// (.tmp + rename — той самий гарантований механізм, що й для
// settings.json/shaurma.json, див. internal/atomicfile). Викликається
// в кінці сканування/синхронізації, а не на кожен put() — саме це
// перетворює "100+ дрібних записів" на "один запис для всієї збірки".
// Помилка запису навмисно НЕ зупиняє синхронізацію: кеш — це лише
// оптимізація швидкості, а не джерело істини (при відсутньому/битому
// файлі кешу лаунчер просто порахує хеші заново, як робив завжди).
func (c *fileHashCache) flush() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.dirty {
		return
	}
	if err := atomicfile.WriteJSONAtomic(c.path, c.data); err == nil {
		c.dirty = false
	}
}
