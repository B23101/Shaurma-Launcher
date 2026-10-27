package main

import (
	"shaurma-launcher-backend/internal/crashguard"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"shaurma-launcher-backend/internal/minecraft"
)

// ── Швидкий підрахунок розмірів (паралельний, з кешем) ────────────────────
// Старий dirSize обходив теки ОДНИМ потоком — на багатогігабайтних теках
// (збірки, libraries) «Пам'ять і кеш» рахувалась десятки секунд. Новий:
// filepath.WalkDir + пул воркерів (runtime.NumCPU()*2, але не менше 4).
// Розмір ВСІЇХ тек рахується паралельно (GetStorageUsage шле підтеки по
// воркерах), а результат кешується на 45 секунд — поки щось не зміниться,
// повторне відкриття вкладки миттєве.

const storageSizeCacheTTL = 45 * time.Second

type storageSizeCacheEntry struct {
	bytes int64
	at    time.Time
}

var (
	storageSizeCache   = map[string]storageSizeCacheEntry{}
	storageSizeCacheMu sync.Mutex
)

// dirScanSem обмежує СУМАРНУ кількість одночасних обходів тек по всьому
// лаунчеру. Аналізи вкладки «Пам'ять і кеш» (GetStorageUsage,
// GetStoragePacks, AnalyzeUnusedJava, AnalyzeStorageVersions) можуть
// запускатись одночасно при вході у вкладку; без спільного ліміту кожен
// спавнить до 16 воркерів і десятки паралельних WalkDir молотять диск —
// через це весь UI лаунчера (WebView2) «засинав» на десятки секунд:
// інші розділи налаштувань переставали вантажитись, а аналіз версій
// виглядав безкінечним.
var dirScanSem = make(chan struct{}, 3)

func cachedDirSize(dir string) int64 {
	key := filepath.Clean(dir)
	storageSizeCacheMu.Lock()
	if e, ok := storageSizeCache[key]; ok && time.Since(e.at) < storageSizeCacheTTL {
		storageSizeCacheMu.Unlock()
		return e.bytes
	}
	storageSizeCacheMu.Unlock()

	size := dirSizeParallel(dir)

	storageSizeCacheMu.Lock()
	storageSizeCache[key] = storageSizeCacheEntry{bytes: size, at: time.Now()}
	// Тримаємо кеш компактним: з десятками тек/збірок і TTL 45с це просто
	// захист від безмежного росту.
	if len(storageSizeCache) > 512 {
		for k := range storageSizeCache {
			delete(storageSizeCache, k)
			if len(storageSizeCache) <= 256 {
				break
			}
		}
	}
	storageSizeCacheMu.Unlock()
	return size
}

// invalidateStorageSizes скидає кеш розмірів (видалення збірки, зміна теки
// даних/кешу, очищення кешу/логів). Викликається з усіх місць, що змінюють
// дані на диску.
func invalidateStorageSizes() {
	storageSizeCacheMu.Lock()
	storageSizeCache = map[string]storageSizeCacheEntry{}
	storageSizeCacheMu.Unlock()
}

// dirSizeParallel — швидкий паралельний підрахунок розміру теки. WalkDir
// роздає файли по воркерах; кожен воркер сумує розміри у свій акумулятор
// (без атоміків на кожен файл — мінімум контеншну). Обхід репортує прогрес
// через onProg (див. GetStorageUsage/GetStoragePacks).
func dirSizeParallel(dir string, onProg ...func(done, total int)) int64 {
	// Спільний ліміт паралельних обходів (див. dirScanSem): аналіз однієї
	// теки НЕ повинен влаштовувати дисковий шторм, який «морозить» увесь
	// лаунчер. Один обхід чекає своєї черги, поки інші завершуються.
	dirScanSem <- struct{}{}
	defer func() { <-dirScanSem }()

	var acc [64]int64
	workers := 4
	if n := runtime.NumCPU() * 2; n > workers {
		workers = n
	}
	if workers > 8 {
		workers = 8
	}
	type job struct {
		info os.FileInfo
	}
	jobs := make(chan job, 4096)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(idx int) {
			defer crashguard.Recover("storage_cleanup.go:108")
			defer wg.Done()
			for j := range jobs {
				if j.info != nil && !j.info.IsDir() {
					acc[idx] += j.info.Size()
				}
			}
		}(w)
	}
	_ = filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err != nil || d == nil {
			return nil
		}
		info, ierr := d.Info()
		if ierr == nil && !d.IsDir() {
			jobs <- job{info: info}
		}
		return nil
	})
	close(jobs)
	wg.Wait()
	var total int64
	for _, a := range acc {
		total += a
	}
	return total
}

// ── Аналіз встановлених версій Minecraft ─────────────────────────────────

// StorageVersionEntry — одна версія у теці minecraft/versions: id, розмір,
// тип (release/snapshot/loader-профіль) і чи використовується вона хоч
// однією ВСТАНОВЛЕНОЮ збіркою (безпосередньо або через InheritsFrom-ланцюг).
type StorageVersionEntry struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Bytes   int64  `json:"bytes"`
	Used    bool   `json:"used"`
	Inherit string `json:"inherit,omitempty"`
}

// StorageVersionsInfo — результат аналізу версій: повний список + ті, що
// можна безпечно видалити, + сумарний обсяг звільнення.
type StorageVersionsInfo struct {
	Installed   []StorageVersionEntry `json:"installed"`
	Unused      []string              `json:"unused"`
	TotalBytes  int64                 `json:"totalBytes"`
	AutoDelete  bool                  `json:"autoDelete"`
	LibrariesMB int64                 `json:"librariesMB"`
	AssetsMB    int64                 `json:"assetsMB"`
}

// installedBuildMCVersions повертає (MCVersion, loader, loaderVersion) всіх
// ВСТАНОВЛЕНИХ збірок — це «корені» для визначення використаних версій.
// LoaderVersion потрібен, щоб точно впізнати профіль лоадера на диску
// (напр. «neoforge-26.2.0.48-beta» за LV «26.2.0.48-beta»), а не гадати
// за назвою.
func (a *App) installedBuildMCVersions() []struct{ MC, Loader, LV string } {
	out := []struct{ MC, Loader, LV string }{}
	for _, v := range a.GetBuilds() {
		if !v.Installed {
			continue
		}
		lb := strings.ToLower(v.Build.Loader)
		if lb == "" || lb == "vanilla" {
			lb = "vanilla"
		}
		out = append(out, struct{ MC, Loader, LV string }{
			MC: strings.TrimSpace(v.Build.MCVersion), Loader: lb, LV: v.Build.LoaderVersion,
		})
	}
	return out
}

// loadVersionJSON читає <versions>/<id>/<id>.json (без мережі).
func loadVersionJSON(baseDir, id string) (*minecraft.VersionJSON, error) {
	path := filepath.Join(baseDir, "versions", id, id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var v minecraft.VersionJSON
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	if v.ID == "" {
		v.ID = id
	}
	return &v, nil
}

// collectUsedVersions будує множину ID версій, які використовує хоч одна
// встановлена збірка (прямо або через InheritsFrom). Версії-профілі лоадерів
// збігаються із збіркою за префіксом/суфіксом MC-версії в ID — той самий
// трюк, що в Prism («1.20.1-forge-47.4.10», «fabric-loader-0.15.11-1.20.1»).
func (a *App) collectUsedVersions(versionsDir string, installed []StorageVersionEntry) (map[string]bool, map[string]*minecraft.VersionJSON) {
	used := map[string]bool{}
	loaded := map[string]*minecraft.VersionJSON{}
	byID := map[string]StorageVersionEntry{}
	for _, v := range installed {
		byID[v.ID] = v
	}
	// loadVersionJSON приймає БАЗУ (minecraft), а не versions/ — додаємо
	// «versions» всередині сам. Тут у нас уже versionsDir, тож передаємо
	// батьківську теку.
	mcBase := filepath.Dir(versionsDir)
	// Щоб не перечитувати JSON по сто разів — читаємо кожну версію один раз.
	for _, v := range installed {
		if j, err := loadVersionJSON(mcBase, v.ID); err == nil {
			loaded[v.ID] = j
		}
	}

	loadWithParents := func(id string) []string {
		chain := []string{}
		seen := map[string]bool{}
		cur := id
		for cur != "" && !seen[cur] {
			seen[cur] = true
			chain = append(chain, cur)
			j := loaded[cur]
			if j == nil {
				if nj, err := loadVersionJSON(mcBase, cur); err == nil {
					j = nj
					loaded[cur] = j
				}
			}
			if j == nil || j.InheritsFrom == "" {
				break
			}
			cur = j.InheritsFrom
		}
		return chain
	}

	// Знайдені «корені» (ванілла) для кожної MC-версії: профілі лоадерів
	// мають InheritsFrom → батько, а отже їхній ланцюг сходиться до ванільної
	// версії. Відмічаємо і корінь, і весь ланцюг, і сам профіль.
	markRoot := func(root string) {
		if root == "" {
			return
		}
		for _, id := range loadWithParents(root) {
			used[id] = true
		}
	}
	for _, b := range a.installedBuildMCVersions() {
		mc := b.MC
		if mc == "" {
			continue
		}
		loader := b.Loader
		lv := strings.TrimSpace(b.LV)

		// 1) Профіль лоадера збірки: шукаємо встановлену версію, чий ланцюг
		//    InheritsFrom сходиться до MC-версії збірки. Це покриває ВСІ
		//    формати профілів: 1.20.1-forge-47.4.10, fabric-loader-0.19.3-1.20.1,
		//    neoforge-26.2.0.48-beta, neoforge-21.11.45 (де MC-версія ВЗАГАЛІ
		//    не читається з імені). Раніше профілі NeoForge рахувались
		//    «невикористаними», бо ім'я не містило MC як чистий префікс/суфікс.
		for id := range byID {
			if used[id] {
				continue
			}
			if !loaderProfileMatches(id, loader, lv, mc) {
				continue
			}
			j := loaded[id]
			if j == nil {
				if nj, err := loadVersionJSON(mcBase, id); err == nil {
					j = nj
					loaded[id] = j
				}
			}
			// Ланцюг має сходитись до mc (або бути ванільною версією mc).
			ok := false
			if j != nil && j.ID == mc {
				ok = true
			} else if j != nil && j.InheritsFrom != "" {
				for _, p := range loadWithParents(id) {
					if p == mc {
						ok = true
						break
					}
				}
			}
			if ok {
				for _, p := range loadWithParents(id) {
					used[p] = true
				}
			}
		}
		// 2) Ванільний корінь для MC-версії (профіль знайдено або ні — корінь
		//    потрібен в обох випадках, якщо його тека є).
		if _, ok := byID[mc]; ok {
			markRoot(mc)
		}
	}
	// 3) Фолбек: ланцюги, що не зійшлися жодною збіркою, але сходяться до
	//    вже позначеного кореня (напр. друга версія лоадера тієї самої MC,
	//    встановлена раніше, але без збірки).
	if len(used) > 0 {
		for id := range byID {
			if used[id] {
				continue
			}
			for _, p := range loadWithParents(id) {
				if used[p] {
					for _, c := range loadWithParents(id) {
						used[c] = true
					}
					break
				}
			}
		}
	}
	return used, loaded
}

// loaderProfileMatches — чи версія з ID відповідає (loader, loaderVersion)
// збірки. Усі варіанти іменування профілів:
//
//	1.20.1-forge-47.4.10            → loader «forge»,   LV «47.4.10»
//	fabric-loader-0.19.3-1.20.1     → loader «fabric»,  LV «0.19.3»
//	neoforge-21.11.45               → loader «neoforge»,LV «21.11.45»
//	neoforge-26.2.0.48-beta         → loader «neoforge»,LV «26.2.0.48-beta»
//	quilt-loader-0.20.0-beta.9-1.20.1 → loader «quilt», LV «0.20.0-beta.9»
//
// Якщо LV порожня (Shaurma-збірка не знає версії лоадера) — падаємо на
// MC-версію в імені.
func loaderProfileMatches(id, loader, lv, mc string) bool {
	if id == "" || id == mc {
		return false
	}
	if loader != "" && loader != "vanilla" {
		// Ім'я починається з імені лоадера або містить його слово.
		nameOK := strings.HasPrefix(id, loader+"-") || strings.HasPrefix(id, loader+"loader-")
		if lv != "" && nameOK && strings.Contains(id, lv) {
			return true
		}
		if lv != "" && strings.Contains(id, "-"+loader+"-") {
			return strings.Contains(id, lv)
		}
	}
	// MC-версія в імені як чистий префікс/суфікс/середина.
	if mc != "" {
		if strings.HasPrefix(id, mc+"-") || strings.HasSuffix(id, "-"+mc) || strings.Contains(id, "-"+mc+"-") {
			return true
		}
	}
	return false
}

// AnalyzeStorageVersions сканує minecraft/versions, рахує розмір кожної
// версії (паралельно, з кешем) і визначає невикористовувані. Працює
// швидко — розміри окремих версій малі (один jar + json), основний час
// йде на libraries/assets (рахуються разом).
func (a *App) AnalyzeStorageVersions() StorageVersionsInfo {
	mcBase := filepath.Join(a.cfg.Dir(), "minecraft")
	versionsDir := filepath.Join(mcBase, "versions")
	info := StorageVersionsInfo{}
	// Гарантуємо НЕ-nil слайси назовні: Go серіалізує nil-слайс у JSON як
	// null, а фронтенд у вкладці «Пам'ять і кеш» робить
	// storageVersions.installed.length / storageVersions.unused.length
	// прямо в шаблоні — null там кидав би TypeError "Cannot read properties
	// of null (reading 'length')" при відкритті розділу. Це і був краш
	// налаштувань, що з'явився після перенесення даних у БД (раніше
	// код повертав не-nil, але після рефакторингу гілки з помилкою
	// лишали слайси nil).
	info.Installed = []StorageVersionEntry{}
	info.Unused = []string{}

	entries, err := os.ReadDir(versionsDir)
	if err != nil {
		info.LibrariesMB = cachedDirSize(filepath.Join(mcBase, "libraries")) / (1024 * 1024)
		info.AssetsMB = cachedDirSize(filepath.Join(mcBase, "assets")) / (1024 * 1024)
		return info
	}

	// Спершу список, потім розміри паралельно (кожна версія — своя маленька
	// тека; навіть послідовно це швидко, але паралельністю дешево виграємо).
	installed := []StorageVersionEntry{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		id := e.Name()
		v := StorageVersionEntry{ID: id}
		if j, err := loadVersionJSON(mcBase, id); err == nil {
			v.Type = j.Type
			v.Inherit = j.InheritsFrom
		}
		installed = append(installed, v)
	}
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i := range installed {
		wg.Add(1)
		go func(idx int) {
			defer crashguard.Recover("storage_cleanup.go:406")
			defer wg.Done()
			sem <- struct{}{}
			installed[idx].Bytes = cachedDirSize(filepath.Join(versionsDir, installed[idx].ID))
			<-sem
		}(i)
	}
	wg.Wait()
	// Сортування: спершу великі, потім за алфавітом — у списку одразу видно
	// головних «пожирачів» місця.
	sortStorageVersions(installed)

	used, _ := a.collectUsedVersions(versionsDir, installed)

	for i := range installed {
		if used[installed[i].ID] {
			installed[i].Used = true
		} else {
			info.Unused = append(info.Unused, installed[i].ID)
			info.TotalBytes += installed[i].Bytes
		}
	}
	info.Installed = installed
	info.AutoDelete = a.cfg.GetSettings().CleanupAfterPackDelete
	info.LibrariesMB = cachedDirSize(filepath.Join(mcBase, "libraries")) / (1024 * 1024)
	info.AssetsMB = cachedDirSize(filepath.Join(mcBase, "assets")) / (1024 * 1024)
	return info
}

func sortStorageVersions(list []StorageVersionEntry) {
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && list[j].Bytes > list[j-1].Bytes; j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
}

// CleanupUnusedVersions видаляє теки версій зі списку unused. Потрібні
// версії перевстановляться автоматично при наступному запуску збірки.
// Після видалення версій варто переаналізувати libraries/assets (збірка
// «прибрала» свої залежності).
func (a *App) CleanupUnusedVersions(ids []string) error {
	mcBase := filepath.Join(a.cfg.Dir(), "minecraft")
	for _, id := range ids {
		if id == "" || strings.ContainsAny(id, `\/:*?"<>|`) {
			continue
		}
		dir := filepath.Join(mcBase, "versions", id)
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("видалення версії %s: %w", id, err)
		}
	}
	invalidateStorageSizes()
	return nil
}

// ── Очищення зайвих бібліотек і асетів ───────────────────────────────────
// libraries/ та assets/ — СПІЛЬНІ для всіх збірок теки. Безпечно видаляти
// можна лише те, що не згадує жодна ВИКОРИСТОВУВАНА версія. Аналіз будує
// множину використаних шляхів з version.json всіх використовуваних версій
// (включно з батьками InheritsFrom) і шукає зайве на диску.

type StorageLibrariesInfo struct {
	UsedPaths    int    `json:"usedPaths"`
	OrphanFiles  int    `json:"orphanFiles"`
	OrphanBytes  int64  `json:"orphanBytes"`
	UsedBytes    int64  `json:"usedBytes"`
	ScanDone     bool   `json:"scanDone"`
	Error        string `json:"error,omitempty"`
	RemovedFiles int    `json:"removedFiles"`
	RemovedBytes int64  `json:"removedBytes"`
}

// collectUsedLibraryPaths збирає всі шляхи бібліотек, які згадує ланцюг
// версій, що використовуються встановленими збірками.
func (a *App) collectUsedLibraryPaths() (map[string]bool, error) {
	mcBase := filepath.Join(a.cfg.Dir(), "minecraft")
	versionsDir := filepath.Join(mcBase, "versions")
	// Перевикористовуємо AnalyzeStorageVersions без зайвого — нам потрібен
	// лише список встановлених ID, а потім ланцюги.
	entries, err := os.ReadDir(versionsDir)
	if err != nil {
		return nil, err
	}
	installed := []StorageVersionEntry{}
	for _, e := range entries {
		if e.IsDir() {
			installed = append(installed, StorageVersionEntry{ID: e.Name()})
		}
	}
	used, loaded := a.collectUsedVersions(versionsDir, installed)

	paths := map[string]bool{}
	for id := range used {
		j := loaded[id]
		if j == nil {
			if nj, err := loadVersionJSON(filepath.Dir(versionsDir), id); err == nil {
				j = nj
			}
		}
		if j == nil {
			continue
		}
		for _, lib := range j.Libraries {
			// Зберігаємо і artifact.path, і класифікатори (natives тощо).
			if lib.Downloads.Artifact.Path != "" {
				paths[filepath.ToSlash(lib.Downloads.Artifact.Path)] = true
			}
			for _, c := range lib.Downloads.Classifiers {
				if c.Path != "" {
					paths[filepath.ToSlash(c.Path)] = true
				}
			}
		}
	}
	return paths, nil
}

// AnalyzeOrphanLibraries рахує, скільки файлів у minecraft/libraries не
// згадується жодною використовуваною версією, і сумарний їх розмір.
// САМЕ ВИДАЛЕННЯ не виконує — лише аналіз (щоб користувач бачив, скільки
// звільниться).
func (a *App) AnalyzeOrphanLibraries() StorageLibrariesInfo {
	out := StorageLibrariesInfo{}
	used, err := a.collectUsedLibraryPaths()
	if err != nil {
		out.Error = err.Error()
		return out
	}
	out.UsedPaths = len(used)
	libsDir := filepath.Join(a.cfg.Dir(), "minecraft", "libraries")
	// Обходимо дерево і збираємо ФАЙЛИ (листки), що не в used-множині.
	var orphanBytes int64
	orphanCount := 0
	var mu sync.Mutex
	_ = filepath.WalkDir(libsDir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(libsDir, p)
		if rerr != nil {
			return nil
		}
		if used[filepath.ToSlash(rel)] {
			out.UsedBytes += dInfoSize(d)
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		mu.Lock()
		orphanBytes += info.Size()
		orphanCount++
		mu.Unlock()
		return nil
	})
	out.OrphanFiles = orphanCount
	out.OrphanBytes = orphanBytes
	out.ScanDone = true
	return out
}

func dInfoSize(d os.DirEntry) int64 {
	info, err := d.Info()
	if err != nil {
		return 0
	}
	return info.Size()
}

// CleanupOrphanLibraries видаляє файли бібліотек, не використовувані
// жодною встановленою збіркою. Порожні теки, що лишилися, прибираються.
func (a *App) CleanupOrphanLibraries() StorageLibrariesInfo {
	out := a.AnalyzeOrphanLibraries()
	if !out.ScanDone {
		return out
	}
	used := map[string]bool{}
	if p, err := a.collectUsedLibraryPaths(); err == nil {
		used = p
	}
	// ЗАХИСТ (як у асетах): якщо використовуваних шляхів немає взагалі —
	// ми «не знаємо», що потрібно, і масове видалення всіх бібліотек може
	// зламати гру. Краще відмовитись, ніж знести libraries/ цілком.
	if len(used) == 0 {
		out.ScanDone = false
		out.Error = "немає жодної використовуваної версії — очищення бібліотек відкладено"
		return out
	}
	libsDir := filepath.Join(a.cfg.Dir(), "minecraft", "libraries")
	var removedBytes int64
	removed := 0
	_ = filepath.WalkDir(libsDir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(libsDir, p)
		if rerr != nil {
			return nil
		}
		if used[filepath.ToSlash(rel)] {
			return nil
		}
		info, ierr := d.Info()
		size := int64(0)
		if ierr == nil {
			size = info.Size()
		}
		if os.Remove(p) == nil {
			removedBytes += size
			removed++
		}
		return nil
	})
	out.RemovedFiles = removed
	out.RemovedBytes = removedBytes
	out.OrphanBytes = removedBytes
	invalidateStorageSizes()
	// Прибираємо порожні теки знизу вгору.
	pruneEmptyDirs(libsDir)
	return out
}

func pruneEmptyDirs(root string) {
	// Обхід знизу вгору: спершу вкладені, потім батьківські.
	var dirs []string
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			dirs = append(dirs, p)
		}
		return nil
	})
	for i := len(dirs) - 1; i >= 0; i-- {
		if dirs[i] == root {
			continue
		}
		entries, err := os.ReadDir(dirs[i])
		if err == nil && len(entries) == 0 {
			_ = os.Remove(dirs[i])
		}
	}
}

// AnalyzeOrphanAssets рахує об'єкти асетів (assets/objects), що не згадує
// жоден використовуваний asset index. ОБЕРЕЖНО: indexes лишаємо завжди —
// вони маленькі, і їх видалення небезпечне (гра падає, якщо індекс є,
// а об'єктів нема).
type StorageAssetsInfo struct {
	UsedObjects   int    `json:"usedObjects"`
	OrphanObjects int    `json:"orphanObjects"`
	OrphanBytes   int64  `json:"orphanBytes"`
	Removed       int    `json:"removed"`
	RemovedBytes  int64  `json:"removedBytes"`
	ScanDone      bool   `json:"scanDone"`
	Error         string `json:"error,omitempty"`
}

func (a *App) collectUsedAssetHashes() (map[string]bool, error) {
	mcBase := filepath.Join(a.cfg.Dir(), "minecraft")
	versionsDir := filepath.Join(mcBase, "versions")
	entries, err := os.ReadDir(versionsDir)
	if err != nil {
		return nil, err
	}
	installed := []StorageVersionEntry{}
	for _, e := range entries {
		if e.IsDir() {
			installed = append(installed, StorageVersionEntry{ID: e.Name()})
		}
	}
	used, loaded := a.collectUsedVersions(versionsDir, installed)

	hashes := map[string]bool{}
	for id := range used {
		j := loaded[id]
		if j == nil {
			if nj, err := loadVersionJSON(filepath.Dir(versionsDir), id); err == nil {
				j = nj
			}
		}
		if j == nil {
			continue
		}
		// У версії може бути власний assetIndex.ID — шукаємо його і читаємо.
		if j.AssetIndex.ID == "" {
			continue
		}
		idxPath := filepath.Join(mcBase, "assets", "indexes", j.AssetIndex.ID+".json")
		data, rerr := os.ReadFile(idxPath)
		if rerr != nil {
			continue
		}
		var idx struct {
			Objects map[string]struct {
				Hash string `json:"hash"`
			} `json:"objects"`
		}
		if json.Unmarshal(data, &idx) == nil {
			for _, o := range idx.Objects {
				if o.Hash != "" {
					hashes[o.Hash] = true
				}
			}
		}
	}
	return hashes, nil
}

func (a *App) analyzeOrphanAssets(delete bool) StorageAssetsInfo {
	out := StorageAssetsInfo{}
	hashes, err := a.collectUsedAssetHashes()
	if err != nil {
		out.Error = err.Error()
		return out
	}
	out.UsedObjects = len(hashes)
	// ЗАХИСТ: якщо немає жодної використовуваної версії (напр. тека
	// versions порожня чи збірки не встановлені) — «використані хеші»
	// порожні, і звичайний алгоритм видалив би ВСІ асети. Так робити не
	// можна: у такому стані ми просто не знаємо, що потрібно, тож аналіз
	// повертає помилку замість масового видалення.
	if len(hashes) == 0 {
		out.Error = "немає жодної використовуваної версії — аналіз асетів відкладено"
		out.ScanDone = false
		return out
	}
	objectsDir := filepath.Join(a.cfg.Dir(), "minecraft", "assets", "objects")
	var orphanBytes int64
	_ = filepath.WalkDir(objectsDir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(objectsDir, p)
		if rerr != nil {
			return nil
		}
		// Шлях objects/<hash-prefix>/<hash>. Перші 2 символи = префікс.
		name := filepath.Base(rel)
		if hashes[name] {
			return nil
		}
		out.OrphanObjects++
		if info, ierr := d.Info(); ierr == nil {
			orphanBytes += info.Size()
		}
		if delete {
			if os.Remove(p) == nil {
				out.Removed++
				if info, ierr := d.Info(); ierr == nil {
					out.RemovedBytes += info.Size()
				}
			}
		}
		return nil
	})
	out.OrphanBytes = orphanBytes
	out.ScanDone = true
	if delete {
		invalidateStorageSizes()
	}
	return out
}

func (a *App) AnalyzeOrphanAssets() StorageAssetsInfo { return a.analyzeOrphanAssets(false) }
func (a *App) CleanupOrphanAssets() StorageAssetsInfo { return a.analyzeOrphanAssets(true) }

// ── Рекомендації: рідко використовувані збірки ───────────────────────────
// Збірки, які займають багато місця, але майже не запускаються (за даними
// playtime.json), — найкращі кандидати на видалення.

type StorageSuggestion struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	IconURL string `json:"iconUrl"`
	// Icon — пресет-іконка збірки (Tabler); PNG (IconURL) має пріоритет.
	Icon        string `json:"icon"`
	Color       string `json:"color"`
	Bytes       int64  `json:"bytes"`
	PlaySeconds int64  `json:"playSeconds"`
	Recommended bool   `json:"recommended"`
}

// GetStorageSuggestions повертає збірки з розмірами і часом гри. Збірка
// «рекомендована до видалення», якщо вона велика (≥ 300 МБ) і в неї
// практично немає ігрового часу (< 30 хв за весь час) — «займає місце,
// але не граєш». Сортування: спершу кандидати, всередині — за розміром.
func (a *App) GetStorageSuggestions() []StorageSuggestion {
	playtime := a.cfg.GetPlaytime()
	out := []StorageSuggestion{}
	for _, v := range a.GetBuilds() {
		if !v.Installed {
			continue
		}
		bytes := cachedDirSize(filepath.Join(a.cfg.GetSettings().InstanceDir, v.Build.ID))
		secs := playtime[v.Build.ID]
		sug := StorageSuggestion{
			ID:          v.Build.ID,
			Name:        v.Build.Name,
			IconURL:     v.Build.IconURL,
			Icon:        v.Build.Icon,
			Color:       v.Build.Color,
			Bytes:       bytes,
			PlaySeconds: secs,
		}
		// «Велика і не граєш»: ≥ 300 МБ і ≤ 30 хв за весь час.
		sug.Recommended = bytes >= 300*1024*1024 && secs <= 30*60
		out = append(out, sug)
	}
	sortStorageSuggestions(out)
	return out
}

func sortStorageSuggestions(list []StorageSuggestion) {
	for i := 1; i < len(list); i++ {
		for j := i; j > 0; j-- {
			a, b := list[j], list[j-1]
			better := false
			if a.Recommended && !b.Recommended {
				better = true
			} else if a.Recommended == b.Recommended {
				better = a.Bytes > b.Bytes
			}
			if !better {
				break
			}
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
}

// ── Видалення версій після видалення збірки (auto-clean) ────────────────
// CleanupVersionsAfterPackDelete видаляє теки версій, які після видалення
// збірки не використовує НІХТО. Викликається з DeletePack, якщо увімкнено
// налаштування CleanupAfterPackDelete. Версії, потрібні іншим збіркам,
// не чіпаються.

func (a *App) CleanupVersionsAfterPackDelete() (removed []string) {
	info := a.AnalyzeStorageVersions()
	if len(info.Unused) == 0 {
		return nil
	}
	for _, id := range info.Unused {
		dir := filepath.Join(a.cfg.Dir(), "minecraft", "versions", id)
		if os.RemoveAll(dir) == nil {
			removed = append(removed, id)
		}
	}
	invalidateStorageSizes()
	return removed
}
