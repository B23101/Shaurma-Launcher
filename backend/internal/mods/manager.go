package mods

import (
	"archive/zip"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"shaurma-launcher-backend/internal/crashguard"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"shaurma-launcher-backend/internal/model"
)

// ── Менеджер модів збірки ───────────────────────────────────────────────
// Повний сканер jar-файлів: читає метадані (fabric.mod.json / quilt.mod.json
// / META-INF/mods.toml / META-INF/neoforge.mods.toml / mcmod.info), іконки,
// SHA1 та залежності. Формат вимкненого мода — суфікс ".jar.disabled"
// (узгоджено зі старим JavaFX-лаунчером).

const disabledSuffix = ".jar.disabled"
const legacyDisabledPrefix = ".disabled."

type Manager struct {
	// cacheDir — службова тека лаунчера для персистентного кешу хешів
	// (sha1cache_persist.go), напр. <AppDir>/cache/mod-sha1-cache/.
	// НІКОЛИ не тека самої збірки/mods/.minecraft — та належить гравцю,
	// а не лаунчеру, і службові файли там неприпустимі. Порожній рядок
	// (Manager, створений без SetCacheDir чи старим NewManager()) —
	// персистентний кеш просто вимкнений, поведінка як до цієї фічі
	// (хешування щоразу з нуля в межах сесії процесу, як і раніше).
	cacheDir string
}

func NewManager() *Manager {
	return &Manager{}
}

// SetCacheDir підключає службову теку лаунчера для персистентного кешу
// хешів SHA1 (sha1cache_persist.go) — викликається одразу після
// mods.NewManager() у App (app.go), поки cfg.CacheDir() уже відомий.
// Без цього виклику Manager поводиться так само, як і раніше: кеш живе
// лише в пам'яті процесу (modSha1Cache), нічого не зберігається на диск.
func (m *Manager) SetCacheDir(dir string) {
	m.cacheDir = dir
}

// ── Кеш метаданих jar (path+size+mtime → метадані) ──────────────────────
// Повторні скани (після вимкнення/увімкнення мода, оновлення) НЕ читають
// zip заново для незмінених файлів — це те, що в старому лаунчері
// «підвішувало» завантаження сторінки на сотнях модів.

type jarCacheKey struct {
	path  string
	size  int64
	mtime int64
}

type jarMeta struct {
	ID          string
	Name        string
	Version     string
	at          time.Time
	Description string
	Author      string
	LoaderType  string // fabric | quilt | forge | neoforge | unknown
	Loaders     []string
	IconData    string // data URL (base64) або ""
	IconPath    string // шлях іконки ВСЕРЕДИНІ jar, з метаданих (logoFile/icon) — заповнюється парсером, IconData рахує attachIcon
	Depends     []string
	Provides    []string // додаткові mod-ID, які цей jar теж закриває (fabric.mod.json "provides")
	// Environment — сторона, на якій працює мод: "client" | "server" | "*"
	// (обидві) | "" (невідомо — лоадер не декларує це статично, напр.
	// Forge/NeoForge). Читається лише з fabric.mod.json/quilt.mod.json
	// поля "environment" — це єдиний формат зі СТРУКТУРОВАНОЮ декларацією
	// сторони; Forge визначає клієнт-серверність через рантайм
	// DistExecutor/анотації, які статично з jar не читаються.
	Environment string
}

var (
	jarMetaCacheMu sync.Mutex
	jarMetaCache   = map[jarCacheKey]*jarMeta{}

	// modSha1Cache — кеш SHA1 файлів модів за (шлях, розмір, mtime): той
	// самий ключ, що й jarMetaCache. SHA1 потрібен для точного матчингу
	// на Modrinth (/version_files, batch), але рахувати його на КОЖНОМУ
	// скануванні — це читання всього файлу (гігабайти на великих збірках),
	// тож він лічиться ліниво (Sha1Of) і кешується доки файл не змінився.
	modSha1CacheMu sync.Mutex
	modSha1Cache   = map[jarCacheKey]string{}
)

// scanWorkers — скільки jar-ів обробляти паралельно під час скану (SHA1 +
// розбір метаданих). Modrinth App хешує по 64 файли одночасно; 32 дає
// близький до нього час на типовій SSD-збірці й лишає запас на HDD.
var scanWorkers = func() int {
	n := runtime.NumCPU()
	if n < 4 {
		n = 4
	}
	if n > 8 {
		n = 8
	}
	return n
}()

func cacheKey(path string) (jarCacheKey, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return jarCacheKey{}, false
	}
	return jarCacheKey{path: path, size: info.Size(), mtime: info.ModTime().Unix()}, true
}

// ── ListMods: легкий список (для сумісності з консоллю/старими викликами) ──
func (m *Manager) ListMods(modsDir string) ([]model.ModEntry, error) {
	if err := os.MkdirAll(modsDir, 0755); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(modsDir)
	if err != nil {
		return nil, err
	}
	migrateLegacyDisabled(modsDir, entries)
	// Імена з "%" (%2B, %20 …) ламають запуск мода — виправляємо на місці.
	FixUnsafeFileNames(modsDir)
	entries, _ = os.ReadDir(modsDir)

	// 0-довжини, а не nil-слайс: nil серіалізується в JSON як null, і
	// на порожній теці фронтенд (Wails call) отримує null замість []
	// (див. коментар у ScanModsFull нижче — та сама причина).
	mods := make([]model.ModEntry, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		enabled := true
		fileName := name
		if strings.HasSuffix(name, disabledSuffix) {
			enabled = false
			fileName = strings.TrimSuffix(name, ".disabled")
		} else if !strings.HasSuffix(name, ".jar") {
			continue
		}
		mod := model.ModEntry{
			FileName: name,
			Name:     strings.TrimSuffix(fileName, ".jar"),
			Enabled:  enabled,
		}
		if meta := m.readJarCached(filepath.Join(modsDir, name)); meta != nil {
			mod.ID = meta.ID
			mod.Name = meta.Name
			mod.Version = meta.Version
		}
		mods = append(mods, mod)
	}
	return mods, nil
}

// ── ScanModsFull: повне сканування з метаданими/іконками/SHA1 ───────────
func (m *Manager) ScanModsFull(modsDir string) ([]model.ModEntry, error) {
	// Сторінка модів запускає кілька сканів майже одночасно (список,
	// оновлення, сумісність). Раніше кожен ганяв повний парсинг усіх jar
	// заново (кеш ще холодний) — це кратно навантажувало CPU/диск і
	// «вішало» лаунчер. Тепер одночасні скани тієї самої теки ділять
	// один результат (кожен викликач отримує власну копію — вони її мутують).
	key := filepath.Clean(modsDir)
	scanFlightMu.Lock()
	if c, ok := scanFlight[key]; ok {
		scanFlightMu.Unlock()
		<-c.done
		if c.err != nil {
			return nil, c.err
		}
		out := make([]model.ModEntry, len(c.res))
		copy(out, c.res)
		return out, nil
	}
	c := &scanCall{done: make(chan struct{})}
	scanFlight[key] = c
	scanFlightMu.Unlock()
	func() {
		defer func() {
			if r := recover(); r != nil {
				c.err = fmt.Errorf("scan mods: %v", r)
			}
			scanFlightMu.Lock()
			delete(scanFlight, key)
			scanFlightMu.Unlock()
			close(c.done)
		}()
		c.res, c.err = m.scanModsFull(modsDir)
	}()
	if c.err != nil {
		return nil, c.err
	}
	out := make([]model.ModEntry, len(c.res))
	copy(out, c.res)
	return out, nil
}

type scanCall struct {
	done chan struct{}
	res  []model.ModEntry
	err  error
}

var (
	scanFlightMu sync.Mutex
	scanFlight   = map[string]*scanCall{}
)

func (m *Manager) scanModsFull(modsDir string) ([]model.ModEntry, error) {
	if err := os.MkdirAll(modsDir, 0755); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(modsDir)
	if err != nil {
		return nil, err
	}
	migrateLegacyDisabled(modsDir, entries)
	// Імена з "%" (%2B, %20 …) ламають запуск мода — виправляємо на місці.
	FixUnsafeFileNames(modsDir)
	entries, _ = os.ReadDir(modsDir)

	// ВАЖЛИВО: 0-довжини слайс, а не nil. `var mods []model.ModEntry`
	// на порожній теці лишається nil, а Go's encoding/json серіалізує
	// nil-слайс у JSON як `null`, а не `[]`. У Wails-виклику з фронтенда
	// (mods_api.go) це прилітає в UI як `list === null`. Далі
	// PackModsSection.svelte робить `mods = list` (mods стає null) і
	// будь-яке подальше `mods.length` (у шаблоні чи $derived) кидає
	// TypeError — рендер шаблону вибухає ще до того, як Svelte встигає
	// прибрати попередню розмітку "Сканування модів...", тому вона
	// лишається висіти на екрані НАЗАВЖДИ, попри те що scanning вже
	// коректно виставлено у false в finally. Саме тому баг проявлявся
	// і при відкритті сторінки з порожньою текою, і при видаленні всіх
	// модів прямо на відкритій сторінці — в обох випадках бекенд
	// повертав null замість [].
	// Файли збираємо у плаский список, а важку роботу по кожному (SHA1 +
	// розбір jar-метаданих/іконки) виконуємо ПАРАЛЕЛЬНО обмеженим пулом
	// воркерів. Раніше це був послідовний цикл: на збірці зі сотнями модів
	// перше відкриття сторінки читало й хешувало кожен jar ОДИН ЗА ОДНИМ
	// (SHA1 — це повне читання файлу), і список з'являвся помітно
	// повільніше за «миттєвий» список в інших лаунчерах. Кеші
	// jarMetaCache/modSha1Cache потокобезпечні, файли незалежні —
	// паралельність тут коректна.
	type scanJob struct {
		name     string
		full     string
		enabled  bool
		fileName string
	}
	jobs := make([]scanJob, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		enabled := true
		fileName := name
		if strings.HasSuffix(name, disabledSuffix) {
			enabled = false
			fileName = strings.TrimSuffix(name, ".disabled")
		} else if !strings.HasSuffix(name, ".jar") {
			continue
		}
		jobs = append(jobs, scanJob{name: name, full: filepath.Join(modsDir, name), enabled: enabled, fileName: fileName})
	}

	mods := make([]model.ModEntry, len(jobs))
	for i := range jobs {
		j := jobs[i]
		mods[i] = model.ModEntry{
			FileName: j.name,
			Name:     strings.TrimSuffix(j.fileName, ".jar"),
			Enabled:  j.enabled,
		}
		// info перевіряємо на nil ПЕРЕД зверненням: os.Stat міг не вдатись
		// (файл щойно зник або заблокований антивірусом), а info.ModTime()
		// без перевірки — це паніка на nil-вказівнику.
		if info, err := os.Stat(j.full); err == nil {
			mods[i].InstalledDate = info.ModTime().Format(time.RFC3339)
			mods[i].FileSize = info.Size()
		}
	}

	workers := scanWorkers
	if workers > len(jobs) {
		workers = len(jobs)
	}
	if workers > 0 {
		var wg sync.WaitGroup
		sem := make(chan struct{}, workers)
		for i := range jobs {
			j := jobs[i]
			wg.Add(1)
			sem <- struct{}{}
			go func(idx int, job scanJob) {
				defer crashguard.Recover("manager.go:242")
				defer wg.Done()
				defer func() { <-sem }()
				// SHA1 потрібен для точного batch-матчингу на Modrinth
				// (/version_files) при перевірці оновлень і міграції.
				sha := m.Sha1Of(job.full)
				meta := m.readJarCached(job.full)
				if meta != nil {
					mods[idx].ID = meta.ID
					mods[idx].Name = meta.Name
					mods[idx].Version = meta.Version
					mods[idx].Description = meta.Description
					mods[idx].Author = meta.Author
					mods[idx].Loader = meta.LoaderType
					mods[idx].Loaders = meta.Loaders
					mods[idx].IconData = meta.IconData
					mods[idx].DependsOn = meta.Depends
					mods[idx].Provides = meta.Provides
					mods[idx].Environment = meta.Environment
				}
				mods[idx].Sha1 = sha
			}(i, j)
		}
		wg.Wait()
	}
	// Зберігаємо накопичені хеші на диск ОДНИМ записом для всієї теки —
	// не на кожен окремий jar (100+ модів дали б 100+ записів файлу
	// поспіль). Саме це дозволяє наступному запуску лаунчера (і навіть
	// повторному відкриттю сторінки в межах цієї ж сесії) пропустити
	// перехешування файлів, які не змінювались (sha1cache_persist.go).
	// Файл кешу лежить у службовій CacheDir лаунчера, НЕ в modsDir.
	m.FlushSha1Cache(modsDir)
	sort.Slice(mods, func(i, j int) bool {
		return mods[i].InstalledDate < mods[j].InstalledDate
	})
	return mods, nil
}

// IconDataOf повертає data-URL вбудованої іконки jar (з кешу метаданих,
// без повторного читання, якщо файл не змінювався). Порожній рядок —
// іконки нема. Використовується для ЛІНИВОЇ віддачі іконок у UI.
func (m *Manager) IconDataOf(jarPath string) string {
	meta := m.readJarCached(jarPath)
	if meta == nil {
		return ""
	}
	return meta.IconData
}

// readJarCached повертає метадані jar (з кешу, якщо файл не змінювався).
func (m *Manager) readJarCached(path string) *jarMeta {
	key, ok := cacheKey(path)
	if !ok {
		return nil
	}
	jarMetaCacheMu.Lock()
	if c, hit := jarMetaCache[key]; hit {
		jarMetaCacheMu.Unlock()
		return c
	}
	jarMetaCacheMu.Unlock()

	meta := parseJar(path)
	jarMetaCacheMu.Lock()
	// ВАЖЛИВО: зберігаємо ПОВНИЙ meta (з Loaders/Depends/IconData), а не
	// зрізану копію лише з ID/Name/Version/LoaderType. Раніше в кеш клали
	// неповний об'єкт, і повторні скани (другий заход на сторінку збірки,
	// перемикання між збірками) отримували з кешу jarMeta БЕЗ Loaders —
	// через це мультизавантажувальні моди (WorldEdit тощо) на другому
	// скані раптом ставали «несумісними»: attachLoaderCompat не бачив
	// повного списку лоадерів і судив лише за LoaderType primary-мета
	// (напр. neoforge для jar з neoforge.mods.toml + fabric.mod.json) —
	// такий jar хибно позначався несумісним у Fabric-збірці, хоча він
	// повністю підтримує Fabric.
	jarMetaCache[key] = meta
	meta.at = time.Now()
	// Ріст кешу контрольований: коли записів багато, старі (> 30 хв — моди
	// не змінюються часто) вичищаємо, залишаючи лише свіжі. Це тримає
	// пам'ять на рівні «зараз активно скануються збірки», а не «все за всю
	// сесію» (довга сесія з десятками збірок по сотні модів — раніше кеш
	// міг тримати все у пам'яті).
	pruneJarMetaCacheLocked(time.Now().Add(-30*time.Minute), 512)
	jarMetaCacheMu.Unlock()
	return meta
}

// ModIdentity — mod-id мода з jar-файлу для звірки з білим списком
// (internal/whitelist): ID — основний modid з метаданих
// (fabric.mod.json/mods.toml/neoforge.mods.toml/quilt.mod.json/mcmod.info),
// Provides — додаткові modid, які той самий jar теж закриває
// (fabric.mod.json "provides" — один jar фізично являє собою кілька
// модів). Порівнювати треба ЯКРАЗ по ID (і Provides), а не по імені
// файлу: той самий мод різних версій (оновлення автором, форк,
// перезбирання) має РІЗНЕ ім'я jar, але ОДИН modid — саме тому білий
// список зберігає id, а не ім'я файлу.
type ModIdentity struct {
	ID       string
	Provides []string
}

// ModIdentityOf повертає mod-id локального jar-файлу (з кешу метаданих —
// повторний виклик для того самого файлу не читає zip заново). Порожній
// ID при помилці читання jar (файл пошкоджений/недоступний) — викликач
// сам вирішує, як трактувати такий файл.
func (m *Manager) ModIdentityOf(jarPath string) ModIdentity {
	meta := m.readJarCached(jarPath)
	if meta == nil {
		return ModIdentity{}
	}
	return ModIdentity{ID: meta.ID, Provides: meta.Provides}
}

// Sha1Of рахує SHA1 файлу мода з кешем за (розмір, mtime) — порожній
// рядок, якщо файл не читається. SHA1 потрібен для точного матчингу на
// Modrinth (GetVersionByHash / batch /version_files) — без нього кожен мод
// резолвився лише пошуком за назвою, а Modrinth-аналіз "як у самому
// Modrinth" (хеш замість пошуку) неможливий.
func (m *Manager) Sha1Of(path string) string {
	key, ok := cacheKey(path)
	if !ok {
		return ""
	}
	// Персистентний кеш (sha1cache_persist.go): підвантажуємо збережені з
	// минулого запуску лаунчера хеші для теки цього jar-а — лише один раз
	// за сесію (loadDiskSha1Cache сама пильнує дублікати). Це і є те, що
	// прибирає "перше відкриття після кожного рестарту довге": якщо файл
	// не змінювався, хеш прийде з диска нижче ще до наступної перевірки.
	// Файл кешу лежить у службовій CacheDir лаунчера, НЕ в теці модів.
	m.loadDiskSha1Cache(filepath.Dir(path))

	modSha1CacheMu.Lock()
	if h, hit := modSha1Cache[key]; hit {
		modSha1CacheMu.Unlock()
		return h
	}
	modSha1CacheMu.Unlock()

	h, err := sha1File(path)
	if err != nil {
		return ""
	}
	modSha1CacheMu.Lock()
	modSha1Cache[key] = h
	modSha1CacheMu.Unlock()
	markSha1CacheDirty(filepath.Dir(path))
	return h
}

// sha1File рахує SHA1 файлу (стандартна бібліотека).
func sha1File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// PruneModSha1Cache вичищає записи старші cutoff — викликається з фонових
// очисток лаунчера разом із PruneJarMetaCache.
func PruneModSha1Cache(cutoff time.Time) {
	modSha1CacheMu.Lock()
	// Ключ (path,size,mtime) не несе часу — тож чистимо кеш цілком, якщо
	// він розрісся.
	pruned := len(modSha1Cache) > 4096
	if pruned {
		modSha1Cache = map[jarCacheKey]string{}
	}
	modSha1CacheMu.Unlock()

	// ВАЖЛИВО (узгодження з персистентним кешем, sha1cache_persist.go):
	// раніше коментар тут казав "sha1 дешевше перерахувати, ніж тримати
	// сотні мегабайтів хешів" — це було правдою, ПОКИ кеш жив лише в
	// пам'яті: перерахунок був неминучим злом раз на довгу сесію. Тепер,
	// коли повний кеш лежить на диску (у службовій CacheDir лаунчера, по
	// одному файлу на кожну фізичну теку модів), очищення мапи в пам'яті
	// НЕ повинно означати "рахуй усе заново з jar-ів": скидаємо
	// diskCacheLoaded, щоб наступний Sha1Of перечитав готові хеші з диска
	// (дешева операція — один невеликий JSON-файл), а не запускав повне
	// перехешування сотень jar-ів по новій.
	if pruned {
		resetDiskSha1CacheLoadedFlags()
	}
}

// PruneJarMetaCache вичищає записи старші cutoff — викликається з фонових
// очисток лаунчера (див. startModsCachePruner).
func PruneJarMetaCache(cutoff time.Time) {
	jarMetaCacheMu.Lock()
	defer jarMetaCacheMu.Unlock()
	for k, e := range jarMetaCache {
		if e.at.Before(cutoff) {
			delete(jarMetaCache, k)
		}
	}
}

// jarMetaCacheMaxBytes — бюджет пам'яті кешу метаданих за ВАГОЮ, а не за
// кількістю записів. Ліміт «512 записів» сам по собі нічого не гарантує:
// вага запису — це майже виключно IconData, і 512 записів можуть бути як
// 2 МБ, так і (до масштабування іконок, див. icon.go) сотні мегабайтів.
// Після масштабування типовий запис важить 2-4 КБ, тож 8 МБ — це кілька
// тисяч модів у кеші, і при цьому жорстка стеля.
const jarMetaCacheMaxBytes = 8 << 20

// pruneJarMetaCacheLocked тримає кеш компактним: спершу видаляє записи
// старші cutoff (поки не лишиться minKeep), потім — якщо кеш усе ще
// важить більше за jarMetaCacheMaxBytes — викидає найстаріші, доки вага
// не влізе в бюджет. Викликається з readJarCached ПІД утримуваним локом.
func pruneJarMetaCacheLocked(cutoff time.Time, minKeep int) {
	if len(jarMetaCache) > minKeep {
		for k, e := range jarMetaCache {
			if e.at.Before(cutoff) {
				delete(jarMetaCache, k)
			}
			if len(jarMetaCache) <= minKeep {
				break
			}
		}
	}

	total := 0
	for _, e := range jarMetaCache {
		total += jarMetaSize(e)
	}
	if total <= jarMetaCacheMaxBytes {
		return
	}

	type aged struct {
		key jarCacheKey
		at  time.Time
	}
	order := make([]aged, 0, len(jarMetaCache))
	for k, e := range jarMetaCache {
		order = append(order, aged{k, e.at})
	}
	sort.Slice(order, func(i, j int) bool { return order[i].at.Before(order[j].at) })
	for _, o := range order {
		if total <= jarMetaCacheMaxBytes {
			return
		}
		if e, ok := jarMetaCache[o.key]; ok {
			total -= jarMetaSize(e)
			delete(jarMetaCache, o.key)
		}
	}
}

// jarMetaSize — приблизна вага запису кешу в байтах. Рахуємо те, що
// реально важить: іконку (base64-рядок) і текстові поля.
func jarMetaSize(m *jarMeta) int {
	if m == nil {
		return 0
	}
	n := len(m.IconData) + len(m.Description) + len(m.Name) + len(m.ID) +
		len(m.Version) + len(m.Author) + len(m.IconPath)
	for _, d := range m.Depends {
		n += len(d)
	}
	for _, p := range m.Provides {
		n += len(p)
	}
	return n + 128 // накладні витрати структури/мапи
}

func migrateLegacyDisabled(modsDir string, entries []os.DirEntry) {
	// Проходимо двічі: спершу лікуємо ПООДИНОКІ файли (легасі-префікс
	// ".disabled.", подвійний суфікс), і лише потім — колізії, для яких треба
	// знати, які цільові імена вже зайняті (див. нижче).
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, legacyDisabledPrefix) && strings.HasSuffix(name, ".jar") {
			newName := strings.TrimPrefix(name, legacyDisabledPrefix)
			if strings.HasSuffix(newName, ".jar") {
				newName = newName[:len(newName)-len(".jar")] + disabledSuffix
			}
			if newName != name {
				_ = os.Rename(filepath.Join(modsDir, name), filepath.Join(modsDir, newName))
			}
		}
	}

	// Дубльований суфікс ".disabled": канонічний вимкнений файл — це
	// "X.jar.disabled"; "X.jar.disabled.disabled" з'являвся через баг
	// ToggleMod/ToggleServerMod, який приймав уже вимкнений файл за
	// увімкнений і перейменовував його ЩЕ РАЗ. Такий файл ламав усе:
	// UI показував ім'я "X.jar.disabled" (суфікс зрізався лише раз),
	// після чого toggle не знаходив ні "X.jar", ні "X.jar.disabled" —
	// і мод неможливо було ані увімкнути, ані вимкнути. Складаємо хвости
	// до ОДНОГО суфікса (якщо канонічне ім'я вже зайняте — лишаємо файл
	// як є, щоб нічого не перезаписати).
	fresh, err := os.ReadDir(modsDir)
	if err != nil {
		return
	}
	for _, e := range fresh {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, disabledSuffix+disabledSuffix[len(".jar"):]) {
			// Швидка відмова: не закінчується на ".disabled.disabled".
			continue
		}
		base := name
		for strings.HasSuffix(base, ".disabled") {
			base = strings.TrimSuffix(base, ".disabled")
		}
		if !strings.HasSuffix(base, ".jar") {
			continue
		}
		target := base + ".disabled"
		if _, err := os.Stat(filepath.Join(modsDir, target)); err == nil {
			continue // канонічне ім'я вже зайняте — не чіпаємо
		}
		_ = os.Rename(filepath.Join(modsDir, name), filepath.Join(modsDir, target))
	}
}

// ── Парсинг jar ─────────────────────────────────────────────────────────
// УВАГА: SHA1 НЕ рахується під час сканування — це читання всього файлу
// (гігабайти на великих збірках). Хеш потрібен лише для точного матчингу
// на Modrinth за /version_file/{hash}, але пошук за назвою з фасетами
// (версія+лоадер) дає той самий результат без важкого I/O. Дублікати
// виявляються за modId (швидко, без хешування).
//
// МУЛЬТИЗАВАНТАЖУВАЛЬНІ jar-и (2+ файли метаданих В ОДНОМУ jar):
// деякі моди (типово CreativeMD — AmbientSounds/EnhancedVisuals/
// CreativeCore, побудовані на спільній Architectury-кодовій базі)
// фізично містять ОБИДВА файли метаданих в одному jar — і mods.toml
// (Forge), і fabric.mod.json (Fabric) — бо вихідники спільні для обох
// платформ, навіть коли jar зібраний під конкретну одну (напр. jar з
// назвою "..._FORGE_...jar" все одно містить fabric.mod.json як
// артефакт спільної кодової бази).
//
// РАНІШЕ код перевіряв формати ПО ЧЕРЗІ й повертав ПЕРШИЙ знайдений —
// спершу пробували fabric.mod.json (і такий jar хибно ставав "fabric",
// підсвічуючись несумісним у Forge-збірці), потім спробували "TOML
// першим" — але це так само неповне рішення: ламається у зворотному
// напрямку, коли supplementary mods.toml неповний чи є артефактом
// спільних вихідників, а РЕАЛЬНО робочий формат — саме fabric.mod.json
// (jar фактично Fabric-only, попри наявність TOML-заглушки). Обидва
// варіанти "перший попавшийся" неправильно позначають сумісний мод
// як несумісний залежно від того, ЯКИЙ формат випадково опинився
// "першим" за чергою перевірки.
//
// ПРАВИЛЬНЕ рішення: НЕ зупинятись на першому знайденому форматі —
// розпарсити УСІ присутні формати метаданих і зібрати ПОВНИЙ список
// лоадерів, під які jar реально придатний (jarMeta.Loaders). Сумісність
// зі збіркою тоді перевіряється як "чи є перетин" між лоадером збірки
// і УСІМА знайденими лоадерами мода — а не лише з одним, довільно
// обраним форматом. Основні відображувані поля (назва/версія/опис/
// іконка) беремо з ПРІОРИТЕТНОГО формату (TOML, якщо є — він
// специфічніший, бо компілюється саме під Forge/NeoForge), проте це
// впливає лише на те, ЩО показати в картці, а не на перевірку
// сумісності.
func parseJar(jarPath string) *jarMeta {
	zr, err := zip.OpenReader(jarPath)
	if err != nil {
		return &jarMeta{ID: baseNameNoExt(jarPath), Name: baseNameNoExt(jarPath), Version: "unknown", LoaderType: "unknown"}
	}
	defer zr.Close()

	// Розпарсити ВСІ присутні формати метаданих (не зупинятись на
	// першому знайденому) — кожен успішно розпарсений формат додає свій
	// лоадер(и) у loaderSet. primary — перший УСПІШНО розпарсений мета-
	// об'єкт за пріоритетом TOML > Fabric > Quilt > mcmod.info; саме з
	// нього беремо назву/версію/опис/іконку для відображення.
	var primary *jarMeta
	loaderSet := map[string]bool{}
	var loaderOrder []string
	addLoaders := func(ls []string) {
		for _, l := range ls {
			if l != "" && !loaderSet[l] {
				loaderSet[l] = true
				loaderOrder = append(loaderOrder, l)
			}
		}
	}
	setPrimary := func(meta *jarMeta) {
		if meta == nil {
			return
		}
		addLoaders(meta.Loaders)
		if primary == nil {
			primary = meta
		}
	}

	for _, f := range zr.File {
		if f.Name == "META-INF/neoforge.mods.toml" || f.Name == "META-INF/mods.toml" {
			setPrimary(parseModsToml(f, jarPath))
		}
	}
	for _, f := range zr.File {
		if f.Name == "fabric.mod.json" {
			setPrimary(parseFabricJSON(f, jarPath))
		}
	}
	for _, f := range zr.File {
		if f.Name == "quilt.mod.json" {
			setPrimary(parseQuiltJSON(f, jarPath))
		}
	}
	for _, f := range zr.File {
		if f.Name == "mcmod.info" {
			setPrimary(parseMcmodInfo(f, jarPath))
		}
	}

	if primary == nil {
		return &jarMeta{ID: baseNameNoExt(jarPath), Name: baseNameNoExt(jarPath), Version: "unknown", LoaderType: "unknown"}
	}
	// LoaderType лишається основним (з primary) — використовується лише
	// для відображення бейджа лоадера на картці мода. Loaders — повний
	// список, за яким рахується РЕАЛЬНА сумісність (attachLoaderCompat).
	primary.Loaders = loaderOrder
	attachIcon(zr, primary)
	return primary
}

func baseNameNoExt(path string) string {
	name := path
	if idx := strings.LastIndexAny(path, `/\\`); idx >= 0 {
		name = path[idx+1:]
	}
	return strings.TrimSuffix(name, ".jar")
}

// ── fabric.mod.json / quilt.mod.json ────────────────────────────────────
// Виключаємо лише те, що ЗАВЖДИ присутнє за визначенням лоадера/рушія
// і ніколи не встановлюється як окремий jar у mods/ — сам лоадер, гру,
// Java. "fabric-api" (і Quilt-аналог "qsl"/"quilted_fabric_api") НЕ
// виключаємо: це звичайний мод, який треба поставити окремо, і якщо
// його немає — мод не запуститься, тому це має лишатись справжньою
// залежністю для аналізу/встановлення.
var excludedDeps = map[string]bool{
	"minecraft": true, "fabricloader": true,
	"java": true, "quilt_loader": true,
}

type fabricModJSON struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Version     string            `json:"version"`
	Description string            `json:"description"`
	Icon        json.RawMessage   `json:"icon"`
	Authors     []json.RawMessage `json:"authors"`
	Depends     map[string]any    `json:"depends"`
	// Environment — "*" (за замовчуванням, обидві сторони), "client" або
	// "server". Стандартне поле специфікації fabric.mod.json.
	Environment string `json:"environment"`
	// Provides — додаткові мод-ID, які цей jar теж "закриває" собою.
	// Критично для Fabric API: це один jar, але fabric.mod.json кожного
	// суб-модуля (networking, lifecycle-events, item-api...) в реальності
	// об'єднаний в один "fabric-api" jar, і САМЕ через "provides" він
	// заявляє себе як усі ці під-ID одночасно. Без цього поля мод, що
	// залежить від конкретного під-ID (напр. "fabric-networking-api-v1"),
	// не бачив Fabric API як задоволену залежність, навіть коли той уже
	// стояв у теці — і система рекомендувала встановити те, що вже є.
	Provides []string `json:"provides"`
}

func parseFabricJSON(f *zip.File, jarPath string) *jarMeta {
	rc, err := f.Open()
	if err != nil {
		return nil
	}
	defer rc.Close()
	var meta fabricModJSON
	if json.NewDecoder(rc).Decode(&meta) != nil {
		return nil
	}
	name := meta.Name
	if name == "" {
		name = meta.ID
	}
	author := ""
	if len(meta.Authors) > 0 {
		var s string
		if json.Unmarshal(meta.Authors[0], &s) == nil {
			author = s
		} else {
			var obj struct {
				Name string `json:"name"`
			}
			if json.Unmarshal(meta.Authors[0], &obj) == nil {
				author = obj.Name
			}
		}
	}
	id := meta.ID
	if id == "" {
		id = baseNameNoExt(jarPath)
	}
	// "depends" у fabric.mod.json — це саме ОБОВ'ЯЗКОВІ (hard) залежності
	// специфікації Fabric (на відміну від "recommends"/"suggests"/
	// "conflicts"/"breaks", які тут навмисно не читаються — це не
	// обов'язкові вимоги, встановлювати їх примусово не треба).
	// Виключаємо власний ID мода (де-не-де трапляється в кривих jar) і
	// завжди-присутні псевдо-залежності лоадера/рушія.
	var deps []string
	for key := range meta.Depends {
		if key == id || excludedDeps[key] {
			continue
		}
		deps = append(deps, key)
	}
	version := meta.Version
	if version == "" {
		version = "unknown"
	}
	loaderType := "fabric"
	loaders := []string{"fabric"}
	if strings.HasPrefix(id, "quilt") {
		loaderType, loaders = "quilt", []string{"quilt"}
	}
	env := strings.ToLower(strings.TrimSpace(meta.Environment))
	return &jarMeta{
		ID: id, Name: name, Version: version, Description: meta.Description,
		Author: author, LoaderType: loaderType, Loaders: loaders, Depends: deps,
		IconPath: fabricIconPath(meta.Icon), Provides: meta.Provides,
		Environment: env,
	}
}

// fabricIconPath дістає шлях іконки з поля "icon" fabric.mod.json — воно
// буває або простим рядком-шляхом ("assets/modid/icon.png"), або мапою
// розмір→шлях ({"16": "...", "32": "...", "128": "..."}), у такому разі
// беремо найбільший (краще підходить для показу в UI). Раніше поле
// "icon" декодувалось у struct, але ніде не використовувалось — тому
// іконка мода бралась лише за жорстко зашитими іменами pack.png/icon.png
// у КОРЕНІ jar, а більшість fabric-модів кладуть іконку в assets/<id>/...
// і взагалі не мають файлу з такою назвою в корені — іконка не
// показувалась, хоча в jar вона фактично є.
func fabricIconPath(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil && s != "" {
		return s
	}
	var sizes map[string]string
	if json.Unmarshal(raw, &sizes) == nil && len(sizes) > 0 {
		bestSize, bestPath := -1, ""
		for k, v := range sizes {
			n, err := strconv.Atoi(k)
			if err == nil && n > bestSize {
				bestSize, bestPath = n, v
			}
		}
		return bestPath
	}
	return ""
}

// ── quilt.mod.json ───────────────────────────────────────────────────────
// Формат Quilt відрізняється від Fabric: усе (id/version/depends/метадані)
// вкладено в об'єкт "quilt_loader", а "depends" — це МАСИВ (не мапа, як у
// Fabric), елементи якого — або рядок-ID мода (коротка форма, завжди
// обов'язкова), або об'єкт {"id": "...", "optional": true/false, ...}.
// Через цю відмінність структури парсинг через fabricModJSON (map)
// раніше просто нічого не знаходив на реальних Quilt-native модах.
type quiltDependEntry struct {
	// раз перевірка на "чистий рядок" (short-form), раз на об'єкт —
	// json.RawMessage дозволяє розрізнити обидва варіанти нижче.
	raw json.RawMessage
}

func (q *quiltDependEntry) UnmarshalJSON(b []byte) error {
	q.raw = append([]byte(nil), b...)
	return nil
}

type quiltModJSON struct {
	QuiltLoader struct {
		ID       string `json:"id"`
		Version  string `json:"version"`
		Metadata struct {
			Name         string          `json:"name"`
			Description  string          `json:"description"`
			Icon         json.RawMessage `json:"icon"`
			Contributors json.RawMessage `json:"contributors"`
		} `json:"metadata"`
		Depends []quiltDependEntry `json:"depends"`
	} `json:"quilt_loader"`
}

// excludedQuiltDeps — псевдо-залежності, завжди присутні за визначенням
// (сам лоадер, гра, Java). "fabric" (Quilt QSL-режим сумісності) НЕ
// виключаємо навмисно — якщо мод явно вимагає Fabric API/QSL, це
// реальна залежність, яку треба показати й дозволити встановити.
var excludedQuiltDeps = map[string]bool{
	"quilt_loader": true, "minecraft": true, "java": true,
}

func parseQuiltJSON(f *zip.File, jarPath string) *jarMeta {
	rc, err := f.Open()
	if err != nil {
		return nil
	}
	defer rc.Close()
	var meta quiltModJSON
	if json.NewDecoder(rc).Decode(&meta) != nil {
		return nil
	}
	id := meta.QuiltLoader.ID
	if id == "" {
		id = baseNameNoExt(jarPath)
	}
	name := meta.QuiltLoader.Metadata.Name
	if name == "" {
		name = id
	}
	version := meta.QuiltLoader.Version
	if version == "" {
		version = "unknown"
	}
	var deps []string
	for _, entry := range meta.QuiltLoader.Depends {
		var depID string
		var optional bool
		// Коротка форма: сам рядок — ID мода.
		var asString string
		if json.Unmarshal(entry.raw, &asString) == nil {
			depID = asString
		} else {
			var asObj struct {
				ID       string `json:"id"`
				Optional bool   `json:"optional"`
			}
			if json.Unmarshal(entry.raw, &asObj) == nil {
				depID = asObj.ID
				optional = asObj.Optional
			}
		}
		if depID == "" || optional || depID == id || excludedQuiltDeps[depID] {
			continue
		}
		deps = append(deps, depID)
	}
	return &jarMeta{
		ID: id, Name: name, Version: version, Description: meta.QuiltLoader.Metadata.Description,
		LoaderType: "quilt", Loaders: []string{"quilt"}, Depends: deps,
		IconPath: fabricIconPath(meta.QuiltLoader.Metadata.Icon), // формат "icon" однаковий у Fabric і Quilt
	}
}

// ── META-INF/mods.toml (Forge/NeoForge) — міні-парсер ───────────────────
// Формат TOML для Forge/NeoForge:
//
//	[[mods]]
//	  modId="mymod"          ← ID/назва/версія САМОГО мода
//	  version="1.0"
//	[[dependencies.mymod]]   ← "mymod" тут — це modId мода, ЩО ОГОЛОШУЄ
//	  modId="forge"          ←   залежність (той самий, що й вище!), а
//	  mandatory=true         ←   РЕАЛЬНА залежність — це modId ВСЕРЕДИНІ
//	                             блоку, не в заголовку [[dependencies.X]].
//
// Раніше код помилково брав X із заголовка [[dependencies.X]] як ID
// залежності — а це ID самого мода (він завжди дорівнює modId з [[mods]]),
// тому мод фактично "залежав сам від себе", а справжні залежності
// (forge/minecraft/інші моди, вказані через modId= у тілі блоку)
// повністю ігнорувались. Тепер: (1) метадані мода читаємо ТІЛЬКИ з
// блоку [[mods]], а не з усього файлу (інакше modId="forge" з блоку
// залежностей міг перезаписати ID самого мода); (2) залежність — це
// modId=, mandatory= З ТІЛА блоку [[dependencies.*]], а не заголовок.
//
// Go-регулярки (RE2) не підтримують зворотні посилання (\2) і lookahead
// (?=...), тому лапки (одинарні/подвійні) і межі блоків обробляються
// окремими регулярками/вручну по індексах — без цих конструкцій.
var tomlKeyReDouble = regexp.MustCompile(`(?m)^\s*([a-zA-Z0-9_]+)\s*=\s*"(.*?)"`)
var tomlKeyReSingle = regexp.MustCompile(`(?m)^\s*([a-zA-Z0-9_]+)\s*=\s*'(.*?)'`)
var tomlBoolReDouble = regexp.MustCompile(`(?m)^\s*([a-zA-Z0-9_]+)\s*=\s*(true|false)\s*$`)

// tomlBlockHeaderRe знаходить заголовок БУДЬ-якого TOML-блоку виду
// [[щось]] або [щось] (не лише dependencies) — потрібен, щоб коректно
// визначати межі кожного блоку (тіло = до наступного будь-якого
// заголовка, а не лише наступного [[dependencies.*]]).
var tomlBlockHeaderRe = regexp.MustCompile(`(?m)^\s*\[\[?([^\]]+)\]\]?\s*$`)

// tomlKeyValues парсить прості "key = value" (рядки/булі) у межах [start,end)
// рядка s — і double-quoted, і single-quoted, і булеві значення.
func tomlKeyValues(s string, start, end int) map[string]string {
	body := s[start:end]
	vals := map[string]string{}
	for _, m := range tomlKeyReDouble.FindAllStringSubmatch(body, -1) {
		if _, ok := vals[m[1]]; !ok {
			vals[m[1]] = m[2]
		}
	}
	for _, m := range tomlKeyReSingle.FindAllStringSubmatch(body, -1) {
		if _, ok := vals[m[1]]; !ok {
			vals[m[1]] = m[2]
		}
	}
	for _, m := range tomlBoolReDouble.FindAllStringSubmatch(body, -1) {
		if _, ok := vals[m[1]]; !ok {
			vals[m[1]] = m[2]
		}
	}
	return vals
}

func parseModsToml(f *zip.File, jarPath string) *jarMeta {
	rc, err := f.Open()
	if err != nil {
		return nil
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return nil
	}
	s := string(data)

	// Знаходимо межі КОЖНОГО блоку [[...]]/[...] у файлі — потрібно, щоб
	// не змішувати поля різних блоків (мод сам може мати кілька
	// [[mods]] у модпаках, і кожен [[dependencies.X]] — окремий блок).
	headers := tomlBlockHeaderRe.FindAllStringSubmatchIndex(s, -1)
	type block struct {
		name      string // вміст дужок, напр. "mods" або "dependencies.mymod"
		bodyStart int
		bodyEnd   int
	}
	blocks := make([]block, 0, len(headers))
	for i, h := range headers {
		name := s[h[2]:h[3]]
		bodyStart := h[1]
		bodyEnd := len(s)
		if i+1 < len(headers) {
			bodyEnd = headers[i+1][0]
		}
		blocks = append(blocks, block{name: name, bodyStart: bodyStart, bodyEnd: bodyEnd})
	}

	// Метадані самого мода — ЛИШЕ з першого блоку [[mods]] (не з усього
	// файлу), щоб поля з блоків залежностей (теж modId=, version=) не
	// підмінили дані мода.
	var vals map[string]string
	for _, b := range blocks {
		if b.name == "mods" {
			vals = tomlKeyValues(s, b.bodyStart, b.bodyEnd)
			break
		}
	}
	if vals == nil {
		vals = map[string]string{}
	}

	name := vals["displayName"]
	if name == "" {
		name = vals["name"]
	}
	if name == "" {
		name = vals["modId"]
	}
	if name == "" {
		name = baseNameNoExt(jarPath)
	}

	selfID := vals["modId"]

	// Реальні залежності: для кожного блоку [[dependencies.<будь-що>]]
	// беремо modId= З ТІЛА блоку (не з заголовка!) — це і є ID мода, від
	// якого залежимо. mandatory=true (Forge) — обов'язкова залежність;
	// NeoForge з часом перейшов на type="required", тому перевіряємо
	// обидва варіанти. Власний ID мода (форвардна самозалежність,
	// зустрічається в деяких погано згенерованих mods.toml) і
	// службові псевдо-моди (forge/neoforge/minecraft — вони завжди
	// присутні за визначенням лоадера) виключаємо зі списку залежностей.
	depsExcluded := map[string]bool{"forge": true, "neoforge": true, "minecraft": true}
	if selfID != "" {
		depsExcluded[selfID] = true
	}
	seenDeps := map[string]bool{}
	var deps []string
	for _, b := range blocks {
		if !strings.HasPrefix(b.name, "dependencies.") {
			continue
		}
		bvals := tomlKeyValues(s, b.bodyStart, b.bodyEnd)
		depID := bvals["modId"]
		if depID == "" || depsExcluded[depID] || seenDeps[depID] {
			continue
		}
		mandatory := bvals["mandatory"] == "true" || bvals["type"] == "required"
		if mandatory {
			deps = append(deps, depID)
			seenDeps[depID] = true
		}
	}

	version := vals["version"]
	if version == "" {
		version = "unknown"
	}
	loaderType := "forge"
	if strings.Contains(f.Name, "neoforge") {
		loaderType = "neoforge"
	}
	return &jarMeta{
		ID: selfID, Name: name, Version: version,
		Description: vals["description"], Author: vals["authors"],
		LoaderType: loaderType, Loaders: []string{loaderType}, Depends: deps,
		IconPath: vals["logoFile"],
	}
}

// ── mcmod.info (старий Forge, MC 1.7–1.12) ──────────────────────────────
func parseMcmodInfo(f *zip.File, jarPath string) *jarMeta {
	rc, err := f.Open()
	if err != nil {
		return nil
	}
	defer rc.Close()
	var list []struct {
		ModID        string   `json:"modid"`
		Name         string   `json:"name"`
		Version      string   `json:"version"`
		Description  string   `json:"description"`
		AuthorList   []string `json:"authorList"`
		RequiredMods []string `json:"requiredMods"` // напр. "forge@[14.23,)" або просто "somemod"
	}
	if json.NewDecoder(rc).Decode(&list) != nil || len(list) == 0 {
		return nil
	}
	m := list[0]
	author := ""
	if len(m.AuthorList) > 0 {
		author = m.AuthorList[0]
	}
	name := m.Name
	if name == "" {
		name = m.ModID
	}
	version := m.Version
	if version == "" {
		version = "unknown"
	}
	// requiredMods містить modId (іноді з версійним обмеженням через "@",
	// напр. "forge@[14.23.5.2860,)") — залежність без псевдо-модів
	// лоадера/гри, які завжди присутні за визначенням.
	depsExcluded := map[string]bool{"forge": true, "minecraft": true, "mcp": true, "fml": true}
	if m.ModID != "" {
		depsExcluded[m.ModID] = true
	}
	var deps []string
	for _, raw := range m.RequiredMods {
		depID := raw
		if i := strings.IndexByte(depID, '@'); i >= 0 {
			depID = depID[:i]
		}
		depID = strings.TrimSpace(depID)
		if depID == "" || depsExcluded[depID] {
			continue
		}
		deps = append(deps, depID)
	}
	return &jarMeta{
		ID: m.ModID, Name: name, Version: version, Description: m.Description,
		Author: author, LoaderType: "forge", Loaders: []string{"forge"}, Depends: deps,
	}
}

// attachIcon шукає іконку мода в zip і кодує в data URL (base64).
// Спершу — за шляхом з метаданих самого мода (logoFile у mods.toml для
// Forge/NeoForge, icon у fabric.mod.json/quilt.mod.json) — це ЄДИНИЙ
// надійний спосіб, бо автори кладуть іконку куди завгодно (типово
// assets/<modid>/icon.png, а не в корінь jar). Раніше код шукав лише
// файли з жорстко зашитими іменами pack.png/icon.png у КОРЕНІ архіву —
// спрацьовувало випадково (лише коли автор випадково поклав файл саме
// туди й так назвав), а для більшості модів (зокрема типових fabric-
// модів з іконкою в assets/) не знаходило нічого, хоча іконка в jar
// фізично була. Шлях з мета-даних (logoFile/icon) пробуємо і як є, і
// без ведучого "/" (деякі автори пишуть з ним, zip-записи — без).
func attachIcon(zr *zip.ReadCloser, meta *jarMeta) {
	if meta.IconPath != "" {
		want := strings.TrimPrefix(meta.IconPath, "/")
		// 1) Точний шлях (як у метаданих, і без ведучого "/").
		for _, f := range zr.File {
			if f.Name == want || f.Name == meta.IconPath {
				if data := readZipFileDataURL(f); data != "" {
					meta.IconData = data
					return
				}
			}
		}
		// 2) Той самий шлях без урахування регістру (трапляється розбіжність
		// між "icon.PNG"/"Icon.png" у файловій системі автора і полем у toml).
		wantLower := strings.ToLower(want)
		for _, f := range zr.File {
			if strings.ToLower(f.Name) == wantLower {
				if data := readZipFileDataURL(f); data != "" {
					meta.IconData = data
					return
				}
			}
		}
		// 3) logoFile часто вказують без шляху (просто "icon.png"), а автор-
		// генератор (типово MCreator) фактично кладе файл у assets/<modid>/...
		// — шукаємо файл з такою самою НАЗВОЮ будь-де в архіві.
		if base := filepath.Base(want); base != "" && !strings.Contains(want, "/") {
			baseLower := strings.ToLower(base)
			for _, f := range zr.File {
				if strings.ToLower(filepath.Base(f.Name)) == baseLower {
					if data := readZipFileDataURL(f); data != "" {
						meta.IconData = data
						return
					}
				}
			}
		}
	}
	for _, f := range zr.File {
		if f.Name == "pack.png" || f.Name == "icon.png" {
			if data := readZipFileDataURL(f); data != "" {
				meta.IconData = data
				return
			}
		}
	}
	// Останній фолбек: типовий шаблон assets/<modid>/icon.png — навіть
	// коли logoFile у метаданих порожній/невірний, файл сам за собою часто
	// лежить саме там (Forge-конвенція для іконок ресурспаку мода).
	if meta.ID != "" {
		want := "assets/" + strings.ToLower(meta.ID) + "/icon.png"
		for _, f := range zr.File {
			if strings.ToLower(f.Name) == want {
				if data := readZipFileDataURL(f); data != "" {
					meta.IconData = data
					return
				}
			}
		}
	}
}

// ── ToggleMod / DisableMod / EnableMod / ResolveJarPath ─────────────────
// ToggleMod вмикає/вимикає мод у теці mods/.
//
// Стан береться з РЕАЛЬНОГО імені на диску, а не з рядка, що передав
// виклик, і всі хвостові ".disabled" складаються до одного — інакше
// вимкнений файл (у UI він ходить як "X.jar.disabled") міг бути
// перейменований ще раз, у "X.jar.disabled.disabled", і мод ставав
// некерованим (ані увімкнути, ні вимкнути). Саме так ламались моди
// СЕРВЕРІВ (див. internal/servers/mods.go) — тут та сама страховка
// для збірок і будь-яких уже зіпсованих імен.
func (m *Manager) ToggleMod(modsDir, fileName string, enabled bool) error {
	base := fileName
	for strings.HasSuffix(base, ".disabled") {
		base = strings.TrimSuffix(base, ".disabled")
	}

	// Яке з імен реально існує: "X.jar", "X.jar.disabled" чи пошкоджене
	// "X.jar.disabled.disabled".
	cur := ""
	curEnabled := false
	candidates := []struct {
		name    string
		enabled bool
	}{
		{base, true},
		{base + ".disabled", false},
	}
	for _, c := range candidates {
		if _, err := os.Stat(filepath.Join(modsDir, c.name)); err == nil {
			cur, curEnabled = c.name, c.enabled
			break
		}
	}
	if cur == "" {
		for _, tail := range []string{".disabled.disabled", ".disabled.disabled.disabled"} {
			if _, err := os.Stat(filepath.Join(modsDir, base+tail)); err == nil {
				cur, curEnabled = base+tail, false
				break
			}
		}
	}
	if cur == "" {
		return os.ErrNotExist
	}
	// Ідемпотентно: уже в потрібному стані — нічого не робимо.
	if curEnabled == enabled {
		return nil
	}
	target := base + ".disabled"
	if enabled {
		target = base
	}
	return os.Rename(filepath.Join(modsDir, cur), filepath.Join(modsDir, target))
}

func (m *Manager) DisableMod(modsDir, fileName string) error {
	return m.ToggleMod(modsDir, fileName, false)
}

func (m *Manager) EnableMod(modsDir, fileName string) error {
	return m.ToggleMod(modsDir, fileName, true)
}

func (m *Manager) ResolveJarPath(modsDir, fileName string) string {
	if strings.HasSuffix(fileName, disabledSuffix) {
		return filepath.Join(modsDir, fileName)
	}
	if strings.HasSuffix(fileName, ".jar") {
		return filepath.Join(modsDir, fileName)
	}
	return filepath.Join(modsDir, fileName+".jar")
}

// DeleteMod видаляє файл мода (і його .disabled-двійник, якщо є).
func (m *Manager) DeleteMod(modsDir, fileName string) error {
	paths := []string{filepath.Join(modsDir, fileName)}
	if strings.HasSuffix(fileName, ".jar") {
		paths = append(paths, filepath.Join(modsDir, fileName+".disabled"))
	} else if strings.HasSuffix(fileName, disabledSuffix) {
		paths = append(paths, filepath.Join(modsDir, strings.TrimSuffix(fileName, ".disabled")))
	}
	var firstErr error
	for _, p := range paths {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// ── KeyBindLabels: справжні людяні назви біндів клавіш модів ────────────
// options.txt зберігає бінди модів як key_key.<modid>.<name> — сам файл
// НЕ містить перекладу, лише технічний ідентифікатор. Раніше фронтенд
// «вгадував» назву з ідентифікатора (camelCase → слова), що давало
// приблизний, а не точний текст.
//
// Тут — справжній переклад: для кожного modid із переданих ключів шукаємо
// його jar у теці mods/, читаємо звідти assets/<modid>/lang/<lang>.json
// (мова гри збірки; якщо немає — fallback en_us.json) і дістаємо звідти
// підпис ключа за тим самим ідентифікатором, що лежить у key_*
// (Minecraft традиційно кладе назви біндів під ключ "key.<modid>.<name>",
// а категорію — під "key.categories.<modid>" або "itemGroup.<modid>").
//
// Якщо для modid із ключа НЕМАЄ встановленого мода в mods/ (мод видалили/
// вимкнули видаленням файлу) — бінд у результат не включається взагалі:
// показувати «вгадану» назву неіснуючого мода немає сенсу, а сам бінд
// у грі однаково ні на що не впливає.
type KeyBindLabel struct {
	Bind     string `json:"bind"`               // напр. "key.iris.reload"
	ModID    string `json:"modId"`              // напр. "iris"
	ModName  string `json:"modName"`            // людяна назва мода (з jarMeta.Name) — для сортування "як в Minecraft"
	Label    string `json:"label"`              // перекладений підпис самого біндингу (без назви мода)
	Category string `json:"category,omitempty"` // перекладена назва категорії біндів мода, якщо є в lang-файлі
}

// KeyBindLabels повертає справжні перекладені підписи для переданих ключів
// options.txt (без префікса "key_", тобто у форматі "key.<modid>.<name>").
// lang — бажана мова гри збірки у форматі Minecraft ("uk_ua", "en_us"...);
// порожній рядок або відсутність перекладу → fallback на "en_us".
func (m *Manager) KeyBindLabels(modsDir string, bindKeys []string, lang string) map[string]KeyBindLabel {
	lang = strings.ToLower(strings.TrimSpace(lang))
	if lang == "" {
		lang = "en_us"
	}

	// modid -> ключі options.txt, що йому належать.
	byMod := map[string][]string{}
	for _, k := range bindKeys {
		id := modIDFromBindKey(k)
		if id == "" {
			continue
		}
		byMod[id] = append(byMod[id], k)
	}
	if len(byMod) == 0 {
		return map[string]KeyBindLabel{}
	}

	// modid -> шлях jar (знаходимо один раз, скануючи теку mods/).
	jarByModID := m.findModJarsByID(modsDir, byMod)

	out := map[string]KeyBindLabel{}
	for modID, keys := range byMod {
		jarPath, ok := jarByModID[modID]
		if !ok {
			continue // мода немає в mods/ — ключі цього modid не додаємо взагалі
		}
		meta := m.readJarCached(jarPath)
		modName := modID
		if meta != nil && meta.Name != "" {
			modName = meta.Name
		}
		langMap := readJarLangEntries(jarPath, modID, lang)
		category := langMap["key.categories."+modID]

		for _, bind := range keys {
			label := langMap[bind]
			if label == "" {
				label = fallbackBindLabel(bind, modID)
			}
			out[bind] = KeyBindLabel{
				Bind:     bind,
				ModID:    modID,
				ModName:  modName,
				Label:    label,
				Category: category,
			}
		}
	}
	return out
}

// modIDFromBindKey дістає modid з ключа "key.<modid>.<name...>".
// Повертає "", якщо формат не схожий на бінд мода (менше 3 сегментів).
func modIDFromBindKey(bind string) string {
	parts := strings.Split(bind, ".")
	if len(parts) < 3 || parts[0] != "key" {
		return ""
	}
	return parts[1]
}

// findModJarsByID сканує modsDir (враховуючи вимкнені ".jar.disabled") і
// повертає modid -> абсолютний шлях jar, але лише для modid з wanted.
func (m *Manager) findModJarsByID(modsDir string, wanted map[string][]string) map[string]string {
	result := map[string]string{}
	entries, err := os.ReadDir(modsDir)
	if err != nil {
		return result
	}
	for _, e := range entries {
		if e.IsDir() || len(result) == len(wanted) {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".jar") && !strings.HasSuffix(name, disabledSuffix) {
			continue
		}
		full := filepath.Join(modsDir, name)
		meta := m.readJarCached(full)
		if meta == nil {
			continue
		}
		if _, need := wanted[meta.ID]; need {
			if _, already := result[meta.ID]; !already {
				result[meta.ID] = full
			}
		}
	}
	return result
}

// readJarLangEntries читає assets/<modid>/lang/<lang>.json з jar, з
// fallback на en_us.json, якщо бажаної мови немає або запису в ній немає.
// Повертає об'єднану мапу: en_us як база, поверх — записи бажаної мови
// (той самий fallback-принцип, що й у UI-словниках лаунчера).
func readJarLangEntries(jarPath, modID, lang string) map[string]string {
	zr, err := zip.OpenReader(jarPath)
	if err != nil {
		return nil
	}
	defer zr.Close()

	files := map[string]*zip.File{}
	prefix := "assets/" + modID + "/lang/"
	for _, f := range zr.File {
		lower := strings.ToLower(f.Name)
		if strings.HasPrefix(lower, prefix) {
			files[filepath.Base(lower)] = f
		}
	}
	if len(files) == 0 {
		return nil
	}

	result := map[string]string{}
	if f, ok := files["en_us.json"]; ok {
		mergeLangJSON(result, f)
	}
	if lang != "en_us" {
		if f, ok := files[lang+".json"]; ok {
			mergeLangJSON(result, f)
		}
	}
	return result
}

func mergeLangJSON(dst map[string]string, f *zip.File) {
	rc, err := f.Open()
	if err != nil {
		return
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return
	}
	var raw map[string]string
	if err := json.Unmarshal(data, &raw); err != nil {
		return
	}
	for k, v := range raw {
		dst[k] = v
	}
}

// fallbackBindLabel — запасний варіант, коли lang-файл мода не містить
// підпису цього конкретного ключа (буває — деякі моди покладаються на
// автогенеровану назву в грі). Той самий "camelCase → слова", що раніше
// був єдиним варіантом, лишається як крайній fallback, а не основний шлях.
func fallbackBindLabel(bind, modID string) string {
	parts := strings.Split(bind, ".")
	rest := parts[2:]
	if len(rest) == 0 {
		return bind
	}
	joined := strings.Join(rest, " ")
	joined = camelSplit.ReplaceAllString(joined, "$1 $2")
	joined = strings.ReplaceAll(joined, "_", " ")
	joined = strings.ReplaceAll(joined, "-", " ")
	words := strings.Fields(joined)
	for i, w := range words {
		if w == "" {
			continue
		}
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

var camelSplit = regexp.MustCompile(`([a-z0-9])([A-Z])`)

// ReadModInfo — сумісний легкий читач метаданих (використовується
// AI-діагностикою консолі).
func (m *Manager) ReadModInfo(jarPath string) (modInfo, error) {
	meta := m.readJarCached(jarPath)
	if meta == nil {
		return modInfo{}, fmt.Errorf("no mod metadata found")
	}
	return modInfo{Name: meta.Name, Version: meta.Version}, nil
}

type modInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}
