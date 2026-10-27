package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"shaurma-launcher-backend/internal/crashguard"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"shaurma-launcher-backend/internal/api"
	"shaurma-launcher-backend/internal/atomicfile"
	"shaurma-launcher-backend/internal/custompack"
	"shaurma-launcher-backend/internal/export"
	"shaurma-launcher-backend/internal/model"
	"shaurma-launcher-backend/internal/mods"
	"shaurma-launcher-backend/internal/versions"
)

// ── Менеджер модів збірки (вкладка «Моди») ──────────────────────────────
// Повний цикл роботи з модами збірки:
//   - сканування jar-файлів (метадані, іконки, sha1, залежності);
//   - перевірка оновлень: Modrinth (спершу, без ключа), CurseForge (fallback);
//   - зміна версії мода (модалка зі списком версій);
//   - пошук/встановлення залежностей;
//   - масове оновлення з резервними копіями та відновлення;
//   - дублікати (однаковий sha1), видалення, pre-launch перевірка.

const modUpdateConcurrency = 6

// safeModFileName — ім'я файлу мода без "%" (див. mods.SafeFileName).
func safeModFileName(name string) string { return mods.SafeFileName(name) }

// fixModFileNames перейменовує jar-и з "%" в імені у теці модів збірки.
// Викликається перед запуском гри: Forge/Fabric не завантажують такі моди.
func (a *App) fixModFileNames(packID string) {
	for _, r := range mods.FixUnsafeFileNames(a.instanceModsDir(packID)) {
		log.Printf("[mods] виправлено ім'я файлу: %q → %q", r[0], r[1])
	}
}

func (a *App) instanceModsDir(packID string) string {
	return filepath.Join(a.gameDir(packID), "mods")
}

func (a *App) modsBackupsDir(packID string) string {
	return filepath.Join(filepath.Dir(a.instanceModsDir(packID)), "mods-backup")
}

// OpenModsFolder відкриває теку mods/ збірки в системному файловому
// менеджері (кнопка «Відкрити теку модів» в UI). Створює теку, якщо її
// ще нема (наприклад, збірку щойно встановили і жоден мод не якано) —
// щоб кнопка не падала помилкою на щойно створеній збірці.
func (a *App) OpenModsFolder(packID string) error {
	if packID == "" {
		return fmt.Errorf("не вказано packID")
	}
	dir := a.instanceModsDir(packID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("не вдалось створити теку модів: %v", err)
	}
	if runtime.GOOS == "windows" {
		return exec.Command("explorer", dir).Start()
	}
	return xdgOpen(dir)
}

// RevealModFile відкриває Провідник із виділеним конкретним jar-файлом
// мода (кнопка «Показати у теці» на картці мода).
func (a *App) RevealModFile(packID, fileName string) error {
	if packID == "" || fileName == "" {
		return fmt.Errorf("не вказано packID/fileName")
	}
	abs := filepath.Join(a.instanceModsDir(packID), fileName)
	if _, err := os.Stat(abs); err != nil {
		return fmt.Errorf("файл мода не знайдено: %s", fileName)
	}
	if runtime.GOOS == "windows" {
		return exec.Command("explorer.exe", "/select,"+abs).Start()
	}
	return xdgOpen(filepath.Dir(abs))
}

// ── Сканування ──────────────────────────────────────────────────────────
func (a *App) ScanModsFull(packID string) []model.ModEntry {
	mods, err := a.modMgr.ScanModsFull(a.instanceModsDir(packID))
	if err != nil {
		return []model.ModEntry{}
	}
	// Захист на межі Go↔JS: навіть якщо Manager колись поверне nil-слайс
	// (напр. після майбутньої правки не помітили це місце знову), сюди
	// назовні йде гарантовано []model.ModEntry{}, а не nil — інакше Wails
	// серіалізує це в JSON як null, і фронтенд отримує list === null
	// замість порожнього масиву (баг "Сканування модів..." висить
	// назавжди на порожній теці, бо mods.length на null кидає TypeError
	// і Svelte не встигає прибрати попередню розмітку).
	if mods == nil {
		mods = []model.ModEntry{}
	}
	// SHAURMA: перевірка залежностей вимкнена для шаурма-збірок
	// a.attachMissingDeps(mods)
	a.attachDuplicateFlags(mods)
	a.annotatePackMods(packID, mods)
	// ПАМ'ЯТЬ: base64-іконки НЕ віддаємо у списку. Відкриття сторінки модів
	// робить кілька повних сканів (ScanModsWithCachedUpdates,
	// RefreshModUpdates, VerifyIncompatibleMods) — іконки всіх модів їхали
	// в UI кожного разу (+ у кожній події mods:update-one), лишаючи в
	// JS-купі кілька копій. Тепер у списку лише HasIcon, а самі іконки UI
	// просить пачками для ВИДИМИХ рядків (GetModIcons).
	stripModIcons(mods)
	return mods
}

// stripModIcons виносить іконки зі списку: IconData -> HasIcon.
func stripModIcons(mods []model.ModEntry) {
	for i := range mods {
		if mods[i].IconData != "" {
			mods[i].HasIcon = true
			mods[i].IconData = ""
		}
	}
}

// getModIconsMaxBatch — скільки іконок можна попросити за один виклик.
const getModIconsMaxBatch = 64

// GetModIcons повертає data-URL іконок для вказаних файлів модів збірки
// (fileName -> dataURL). Моди без іконки в результат не потрапляють.
// Іконки беруться з кешу метаданих jar (див. internal/mods), тож зазвичай
// диск не читається. Викликається UI лише для рядків, що потрапили у
// в'юпорт.
func (a *App) GetModIcons(packID string, fileNames []string) map[string]string {
	out := make(map[string]string, len(fileNames))
	if packID == "" || len(fileNames) == 0 {
		return out
	}
	if len(fileNames) > getModIconsMaxBatch {
		fileNames = fileNames[:getModIconsMaxBatch]
	}
	modsDir := a.instanceModsDir(packID)
	for _, fn := range fileNames {
		if fn == "" || fn != filepath.Base(fn) {
			continue // захист від шляхів виду ../
		}
		jar := a.modMgr.ResolveJarPath(modsDir, fn)
		if jar == "" {
			continue
		}
		if icon := a.modMgr.IconDataOf(jar); icon != "" {
			out[fn] = icon
		}
	}
	return out
}

// sinytraConnectorModID — modId мода Sinytra Connector (з [[mods]] у
// mods.toml / neoforge.mods.toml — той самий ID в обох гілках, і старій
// Forge-гілці під 1.20.1, і новій NeoForge-гілці 26.1.x). Connector —
// транслятор/шар сумісності, що дозволяє запускати Fabric-моди на
// Forge/NeoForge: фізично лежить у тій самій теці mods/, як звичайний
// jar, тож його присутність визначаємо тим самим скануванням, що й
// решту модів збірки.
const sinytraConnectorModID = "connector"

// hasSinytraConnector перевіряє, чи серед УВІМКНЕНИХ модів збірки є
// Sinytra Connector. Вимкнений (.disabled) Connector не рахуємо —
// якщо його не буде під час запуску гри, Fabric-моди справді не
// завантажаться, тож і в менеджері модів вони мають лишатись
// позначені несумісними.
func hasSinytraConnector(mods []model.ModEntry) bool {
	for i := range mods {
		if mods[i].ID == sinytraConnectorModID && mods[i].Enabled {
			return true
		}
	}
	return false
}

// extraLoadersForPack повертає додаткові лоадери, які збірка packID
// приймає ПОНАД свій основний лоадер — наразі лише "fabric" для
// Forge/NeoForge-збірок зі встановленим (увімкненим) Sinytra Connector
// (див. hasSinytraConnector). Використовується і браузером контенту
// (пошук на Modrinth/CurseForge — BrowserSearchModrinth/CurseForge), і
// перевіркою сумісності вже встановлених модів (attachLoaderCompat), щоб
// обидва місця погоджувались між собою: якщо мод-менеджер вважає
// Fabric-мод сумісним завдяки Connector, пошук так само має його
// знаходити, і навпаки. Порожній packID чи лоадер, що не Forge/NeoForge
// — nil (нема чого розширювати).
func (a *App) extraLoadersForPack(packID, loader string) []string {
	loader = strings.ToLower(loader)
	if packID == "" || (loader != "forge" && loader != "neoforge") {
		return nil
	}
	mods, err := a.modMgr.ScanModsFull(a.instanceModsDir(packID))
	if err != nil || !hasSinytraConnector(mods) {
		return nil
	}
	return []string{"fabric"}
}

// attachLoaderCompat позначає Incompatible=true для модів, скомпільованих
// під ІНШИЙ лоадер, ніж лоадер поточної збірки (напр. Forge-мод у
// Fabric-збірці) — такий мод фізично не завантажиться рушієм. loader
// передається з фронтенду (там, де відома версія/лоадер збірки — ті
// самі виклики, що вже носять mcVersion/loader для перевірки оновлень),
// а не читається з реєстру збірок, бо там ця інформація не зберігається.
// Порожній/невідомий лоадер збірки чи мода — нічого не позначаємо
// (недостатньо даних, аби звинувачувати мод помилково).
//
// Мультизавантажувальні jar-и (один файл з метаданими одразу під
// кілька лоадерів, напр. AmbientSounds — mods.toml + fabric.mod.json в
// одному jar) звіряються за ПОВНИМ списком mods[i].Loaders, а не лише
// за одним mods[i].Loader — досить перетину хоч з ОДНИМ із них, щоб
// вважати мод сумісним. Якщо Loaders порожній (старий кеш чи формат
// без множинного визначення), звіряємось по mods[i].Loader — так само,
// як і раніше.
//
// connectorPresent — у збірці встановлено (і увімкнено) Sinytra
// Connector (див. hasSinytraConnector), тож Fabric/Quilt-моди в цій
// самій теці більше не помилка: Connector якраз і є шаром, що робить
// їх запускними на Forge/NeoForge.
func attachLoaderCompat(mods []model.ModEntry, loader string, connectorPresent bool) {
	buildLoader := strings.ToLower(loader)
	if buildLoader == "" {
		return
	}
	for i := range mods {
		modLoaders := mods[i].Loaders
		if len(modLoaders) == 0 {
			if mods[i].Loader == "" || strings.ToLower(mods[i].Loader) == "unknown" {
				continue
			}
			modLoaders = []string{mods[i].Loader}
		}
		compatible := false
		for _, ml := range modLoaders {
			ml = strings.ToLower(ml)
			if ml == "" || ml == "unknown" {
				continue
			}
			if loadersCompatible(buildLoader, ml, connectorPresent) {
				compatible = true
				break
			}
		}
		if !compatible {
			mods[i].Incompatible = true
			mods[i].IncompatibleReason = strings.ToLower(mods[i].Loader)
		}
	}
}

// loadersCompatible — Fabric-моди зазвичай працюють і на Quilt (QSL-
// режим сумісності Quilt), тому цю пару не вважаємо конфліктом. Forge
// та NeoForge — різні рушії, попри спільне походження TOML-формату,
// тож НЕ сумісні між собою.
//
// connectorPresent — коли в збірці стоїть Sinytra Connector, Forge та
// NeoForge додатково приймають fabric/quilt-моди (Connector — саме
// транслятор, що це вмикає). Без Connector ця пара лишається
// несумісною, як і раніше.
func loadersCompatible(buildLoader, modLoader string, connectorPresent bool) bool {
	if buildLoader == modLoader {
		return true
	}
	if buildLoader == "quilt" && modLoader == "fabric" {
		return true
	}
	if connectorPresent && (buildLoader == "forge" || buildLoader == "neoforge") &&
		(modLoader == "fabric" || modLoader == "quilt") {
		return true
	}
	return false
}

// ── Онлайн-перевірка сумісності (Modrinth/CurseForge за хешем файлу) ────
//
// Локальний парсер (parseJar) читає лише файли метаданих, які реально
// лежать УСЕРЕДИНІ jar (mods.toml/fabric.mod.json/...). Якщо такий файл
// відсутній, пошкоджений, чи має нетиповий/застарілий формат (трапляється
// у старих чи криво зібраних jar) — attachLoaderCompat не має звідки
// взяти список лоадерів і мод хибно лишається позначеним «несумісний».
//
// Modrinth і CurseForge знають РЕАЛЬНИЙ список лоадерів кожного
// опублікованого файлу (незалежно від того, що написано в самому jar) —
// такий самий запит, що вже використовує перевірка оновлень модів
// (GetVersionByHash за sha1 / ResolveFingerprints за murmur2). Тому перед
// тим, як показати користувачу «мод несумісний», лаунчер додатково питає
// ці два джерела і знімає позначку, якщо там знайдеться сумісний лоадер.
//
// Кеш — той самий підхід, що modResolveCache: за sha1 файлу, спільний
// для всіх збірок, з TTL modsCacheTTL, щоб повторне відкриття сторінки
// модів не бомбардувало мережу заново.
type onlineLoaderCacheEntry struct {
	loaders []string // порожній слайс = мод НЕ знайдено (кешуємо і негативний результат)
	at      time.Time
}

// verifyIncompatibleModsOnline звіряє через мережу лише ТІ моди зі списку,
// які attachLoaderCompat вже позначив несумісними — решта (переважна
// більшість збірки) мережі не потребує взагалі. Мутує mods "на місці":
// знімає Incompatible/IncompatibleReason, якщо онлайн-джерело підтвердило
// сумісний лоадер. Мовчазний фейл мережі — мод лишається позначеним, як
// і раніше (краще хибне попередження, ніж тиха відсутність попередження
// при реально несумісному моді).
func (a *App) verifyIncompatibleModsOnline(packID string, mods []model.ModEntry, buildLoader string, connectorPresent bool) {
	buildLoader = strings.ToLower(buildLoader)
	if buildLoader == "" {
		return
	}
	type target struct {
		idx  int
		path string
	}
	var targets []target
	modsDir := a.instanceModsDir(packID)
	for i := range mods {
		if !mods[i].Incompatible {
			continue
		}
		targets = append(targets, target{idx: i, path: a.modMgr.ResolveJarPath(modsDir, mods[i].FileName)})
	}
	if len(targets) == 0 {
		return
	}

	for _, tg := range targets {
		loaders := a.onlineLoadersForJar(tg.path)
		if len(loaders) == 0 {
			continue // не знайдено на жодному джерелі — лишаємо позначку як є
		}
		for _, l := range loaders {
			if loadersCompatible(buildLoader, strings.ToLower(l), connectorPresent) {
				mods[tg.idx].Incompatible = false
				mods[tg.idx].IncompatibleReason = ""
				// Онлайн-джерело знає лоадери краще за локальний парсер —
				// підмішуємо їх у Loaders, щоб наступний локальний скан
				// (без мережі, ScanModsWithCachedUpdates) теж бачив повний
				// список і не позначав мод несумісним знову до зміни файлу.
				mods[tg.idx].Loaders = mergeLoaderLists(mods[tg.idx].Loaders, loaders)
				break
			}
		}
	}
}

// onlineLoadersForJar повертає список лоадерів файлу за даними Modrinth
// (спершу — не потребує ключа) з фолбеком на CurseForge (потребує
// налаштованого API-ключа; якщо його нема — крок просто пропускається).
// Порожній результат = жодне джерело не впізнало файл.
func (a *App) onlineLoadersForJar(jarPath string) []string {
	sha1hex, err := fileSHA1(jarPath)
	if err != nil || sha1hex == "" {
		return nil
	}
	sha1hex = strings.ToLower(sha1hex)

	a.onlineLoaderCacheMu.Lock()
	if a.onlineLoaderCache == nil {
		a.onlineLoaderCache = map[string]onlineLoaderCacheEntry{}
	}
	if c, ok := a.onlineLoaderCache[sha1hex]; ok && time.Since(c.at) < modsCacheTTL {
		a.onlineLoaderCacheMu.Unlock()
		return c.loaders
	}
	a.onlineLoaderCacheMu.Unlock()

	var loaders []string

	// 1) Modrinth за sha1 — швидко, без ключа.
	if a.modrinth != nil {
		if v, err := a.modrinth.GetVersionByHash(sha1hex); err == nil && v != nil {
			loaders = append(loaders, v.Loaders...)
		}
	}

	// 2) CurseForge за murmur2-fingerprint — лише якщо Modrinth не
	// відповів (не варто платити зайвим запитом, коли вже маємо відповідь)
	// і лише коли API-ключ налаштований (cfClient сам поверне помилку
	// інакше — просто пропускаємо крок мовчки).
	if len(loaders) == 0 {
		if cf := a.cfClient(); cf != nil && cf.HasAPIKey() {
			if fp, err := export.Murmur2Fingerprint(jarPath); err == nil {
				if matches, err := cf.ResolveFingerprints([]string{fp}); err == nil {
					if m, ok := matches[fp]; ok {
						if info, err := cf.GetModFile(m.ProjectID, m.FileID); err == nil && info != nil {
							loaders = append(loaders, info.ModLoaders...)
						}
					}
				}
			}
		}
	}

	a.onlineLoaderCacheMu.Lock()
	a.onlineLoaderCache[sha1hex] = onlineLoaderCacheEntry{loaders: loaders, at: time.Now()}
	a.onlineLoaderCacheMu.Unlock()

	return loaders
}

// mergeLoaderLists об'єднує два списки лоадерів без дублікатів
// (порівняння без урахування регістру, порядок першого списку зберігається).
func mergeLoaderLists(a, b []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(a)+len(b))
	for _, l := range a {
		key := strings.ToLower(l)
		if l == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, l)
	}
	for _, l := range b {
		key := strings.ToLower(l)
		if l == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, l)
	}
	return out
}

// attachDuplicateFlags позначає IsDuplicate для всіх модів, чий modId
// зустрічається в теці більше одного разу (різні файли/версії того
// самого мода встановлені одночасно) — та сама умова, що й
// FindDuplicateMods, але прикладена напряму до списку для списку модів,
// а не лише до окремої pre-launch перевірки.
func (a *App) attachDuplicateFlags(mods []model.ModEntry) {
	counts := map[string]int{}
	for i := range mods {
		if mods[i].ID != "" {
			counts[mods[i].ID]++
		}
	}
	for i := range mods {
		mods[i].IsDuplicate = mods[i].ID != "" && counts[mods[i].ID] > 1
	}
}

// modsCacheTTL — кеш результатів перевірки оновлень модів (modUpdateCache)
// і глобальний резолв (modResolveCache) живі НЕ довше цього часу: результати
// мережевих перевірок застарівають, а довга сесія лаунчера не має
// накопичувати сотні мегабайтів у пам'яті (див. startModsCachePruner).
const modsCacheTTL = 12 * time.Minute

// startModsCachePruner періодично вичищає застарілі записи з кешів модів
// (modUpdateCache / modResolveCache / jarMetaCache менеджера). Запускається
// при старті лаунчера і живе, поки живий контекст. Це частина захисту від
// «лаунчер жере 400 МБ після довгої роботи з багатьма збірками/модами»:
// кеші модів — найбільші необмежені сховища у пам'яті.
func (a *App) startModsCachePruner() {
	go func() {
		defer crashguard.Recover("mods_api.go:451")
		ticker := time.NewTicker(8 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-a.ctx.Done():
				return
			case <-ticker.C:
				a.pruneModsCaches()
			}
		}
	}()
}

// pruneModsCaches видаляє записи старші modsCacheTTL з кешів модів.
// Викликається періодично (startModsCachePruner) і одразу після старту.
func (a *App) pruneModsCaches() {
	cutoff := time.Now().Add(-modsCacheTTL)

	// modUpdateCache: тепер кожен запис має власний at — видаляємо тільки
	// ЗАСТАРІЛІ, а не весь кеш. Актуальні результати перевірок зберігаються.
	a.modUpdateCacheMu.Lock()
	for k, e := range a.modUpdateCache {
		if e.at.Before(cutoff) {
			delete(a.modUpdateCache, k)
		}
	}
	a.modUpdateCacheMu.Unlock()

	// modResolveCache: TTL уже зашитий у запис (at) — вичищаємо прострочені.
	a.modResolveCacheMu.Lock()
	for k, e := range a.modResolveCache {
		if e.at.Before(cutoff) {
			delete(a.modResolveCache, k)
		}
	}
	a.modResolveCacheMu.Unlock()

	// onlineLoaderCache: той самий принцип TTL-за-записом.
	a.onlineLoaderCacheMu.Lock()
	for k, e := range a.onlineLoaderCache {
		if e.at.Before(cutoff) {
			delete(a.onlineLoaderCache, k)
		}
	}
	a.onlineLoaderCacheMu.Unlock()

	// jarMetaCache + modSha1Cache менеджера модів: той самий підхід (вичистити застаріле).
	mods.PruneJarMetaCache(cutoff)
	mods.PruneModSha1Cache(cutoff)

	// Пам'ять, звільнену очисткою, віддаємо операційній системі одразу:
	// без цього Go-рантайм тримав би сторінки ще кілька хвилин, а арени
	// glibc (рядки, що перетинали межу C ABI) — до кінця сесії. Саме це
	// і виглядало як «лаунчер з'їв 2 ГБ і не віддає».
	releaseMemoryNow()
}

func modUpdateCacheKey(packID, mcVersion, loader string) string {
	return packID + "|" + mcVersion + "|" + strings.ToLower(loader)
}

// mergeCachedUpdateInfo підмішує в щойно відскановані моди (з диска)
// дані про оновлення з кешу останньої мережевої перевірки — щоб просте
// відкриття сторінки (ScanModsFull) одразу показувало відомий статус
// hasUpdate без повторного походу в мережу. Матчиться за FileName.
func (a *App) mergeCachedUpdateInfo(packID, mcVersion, loader string, mods []model.ModEntry) []model.ModEntry {
	if mcVersion == "" || loader == "" {
		return mods
	}
	a.modUpdateCacheMu.Lock()
	entry, ok := a.modUpdateCache[modUpdateCacheKey(packID, mcVersion, loader)]
	a.modUpdateCacheMu.Unlock()
	if !ok {
		return mods
	}
	for i := range mods {
		idx, found := entry.byFile[mods[i].FileName]
		if !found {
			continue
		}
		c := entry.mods[idx]
		mods[i].HasUpdate = c.HasUpdate
		mods[i].LatestVersion = c.LatestVersion
		mods[i].LatestURL = c.LatestURL
		mods[i].Source = c.Source
		mods[i].ProjectID = c.ProjectID
		mods[i].ProjectSlug = c.ProjectSlug
		if c.IconURL != "" {
			mods[i].IconURL = c.IconURL
		}
		mods[i].Status = "done"
	}
	return mods
}

// ScanModsWithCachedUpdates — те, що фронтенд викликає при відкритті
// сторінки збірки: скан з диска + вже відомий (закешований) статус
// оновлень, БЕЗ походу в мережу. Мережевий похід — лише через
// RefreshModUpdates (кнопка "Перевірити оновлення" в UI).
func (a *App) ScanModsWithCachedUpdates(packID, mcVersion, loader string) []model.ModEntry {
	mods := a.ScanModsFull(packID)
	mods = a.mergeCachedUpdateInfo(packID, mcVersion, loader, mods)
	attachLoaderCompat(mods, loader, hasSinytraConnector(mods))
	// Скан великої збірки — це піковий момент по пам'яті (розпаковані
	// метадані + іконки + JSON-серіалізація відповіді). Тротлінг усередині
	// не дасть цьому виклику стати дорогим при частому перемиканні збірок.
	maybeReleaseMemory()
	return mods
}

// VerifyIncompatibleMods — друга, МЕРЕЖЕВА хвиля перевірки сумісності:
// фронтенд викликає її окремо і асинхронно ПІСЛЯ ScanModsWithCachedUpdates
// (той самий принцип, що RefreshModUpdates для оновлень модів — швидкий
// локальний скан спочатку, важча мережева перевірка потім, у фоні, не
// блокуючи відкриття сторінки).
//
// Питає Modrinth/CurseForge за sha1/fingerprint лише для тих модів, які
// локальний парсер metadata (parseJar) уже позначив «несумісний» —
// зазвичай одна-дві штуки на збірку, а не всі моди. Знімає хибну позначку,
// якщо реальний файл на Modrinth/CurseForge заявлений під сумісний лоадер
// (буває, коли jar має нетиповий/пошкоджений/неповний файл метаданих, але
// сам мод насправді опубікований і під потрібний лоадер теж).
//
// Повертає лише ту частину списку, де щось змінилося (щоб фронт міг
// точково оновити картки, не перемальовуючи всю сторінку) — порожній
// слайс, якщо жодна позначка не знялась.
func (a *App) VerifyIncompatibleMods(packID, mcVersion, loader string) []model.ModEntry {
	mods := a.ScanModsFull(packID)
	connectorPresent := hasSinytraConnector(mods)
	attachLoaderCompat(mods, loader, connectorPresent)

	before := make(map[string]bool, len(mods))
	for i := range mods {
		before[mods[i].FileName] = mods[i].Incompatible
	}

	a.verifyIncompatibleModsOnline(packID, mods, loader, connectorPresent)

	changed := make([]model.ModEntry, 0)
	for i := range mods {
		if before[mods[i].FileName] != mods[i].Incompatible {
			changed = append(changed, mods[i])
		}
	}
	maybeReleaseMemory()
	return changed
}

// attachMissingDeps рахує відсутні залежності ЛОКАЛЬНО (без мережі):
// DependsOn мінус моди, що вже є в теці (увімкнені АБО вимкнені).
//
// Раніше рахувалось лише "мінус увімкнені моди" — тобто якщо гравець
// свідомо вимкнув супутній мод (типовий кейс: Flywheel/Ponder — вони
// mandatory=true в mods.toml у Create, бо технічно потрібні коду, але
// частина збірок кладе їх у моди вимкненими навмисно, чи гравець сам
// вирішує ставити їх окремо чи ні) — система все одно позначала їх як
// "відсутню залежність" і пропонувала примусово довстановити. Тепер
// "відсутньою" вважається лише те, чого в теці НЕМА ФІЗИЧНО взагалі —
// рішення "вмикати чи ні" лишається за гравцем, лаунчер туди не лізе.
func (a *App) attachMissingDeps(mods []model.ModEntry) {
	// present збирає не лише "головний" ID кожного jar, а й усі ID з
	// його "provides" (fabric.mod.json). Критично для Fabric API: це
	// один jar, що заявляє себе одразу десятками під-ID (networking-api,
	// lifecycle-events, item-api...) — реальні моди залежать саме від
	// цих під-ID, а не від "fabric-api" напряму. Без цього Fabric API,
	// навіть стоячи в теці, не покривав жодної залежності, і система
	// щоразу "рекомендувала" встановити те, що вже стоїть.
	present := map[string]bool{}
	for i := range mods {
		if mods[i].ID != "" {
			present[mods[i].ID] = true
		}
		for _, p := range mods[i].Provides {
			if p != "" {
				present[p] = true
			}
		}
	}
	for i := range mods {
		if !mods[i].Enabled {
			continue
		}
		var missing []string
		for _, d := range mods[i].DependsOn {
			if !present[d] {
				missing = append(missing, d)
			}
		}
		mods[i].MissingDeps = missing
		mods[i].HasMissingDeps = len(missing) > 0
	}
}

// GetSuggestedMods — рекомендовані моди для вкладки «Контент» збірки:
// жорсткі (hard) залежності вже встановлених модів, яких самих ще немає
// в збірці (напр. Create і Farmer's Delight обидва вимагають Fabric API —
// пропонуємо встановити його один раз). На відміну від "схожих модів",
// яких Modrinth API штатно не віддає, це рекомендація на основі РЕАЛЬНОГО
// складу збірки користувача, а не вгадування за категоріями.
//
// Поважає Settings.DisableModSuggestions — якщо вимкнено, повертає nil
// одразу без походу в мережу (фронт теж ховає блок, це подвійна
// підстраховка на випадок прямого виклику).
func (a *App) GetSuggestedMods(packID, mcVersion, loader string) []model.SuggestedMod {
	if a.cfg.GetSettings().DisableModSuggestions {
		return nil
	}
	if mcVersion == "" || loader == "" {
		return nil
	}
	mods := a.ScanModsFull(packID)
	attachLoaderCompat(mods, loader, hasSinytraConnector(mods))
	a.attachMissingDeps(mods)

	// depID -> назви модів, що його вимагають (для "Потрібен для: ...").
	requiredBy := map[string][]string{}
	for _, m := range mods {
		if !m.Enabled {
			continue
		}
		for _, dep := range m.MissingDeps {
			name := m.Name
			if name == "" {
				name = m.FileName
			}
			requiredBy[dep] = append(requiredBy[dep], name)
		}
	}
	if len(requiredBy) == 0 {
		return nil
	}

	// Стабільний порядок (map ітерується довільно) + обмеження на 6
	// карток, щоб блок не займав пів екрана при збірці з десятками модів.
	depIDs := make([]string, 0, len(requiredBy))
	for id := range requiredBy {
		depIDs = append(depIDs, id)
	}
	sort.Strings(depIDs)
	if len(depIDs) > 6 {
		depIDs = depIDs[:6]
	}

	loaderLower := strings.ToLower(loader)
	out := make([]model.SuggestedMod, 0, len(depIDs))
	for _, depID := range depIDs {
		// Глобальний кеш резолву (окремий від modResolveCache, той —
		// за sha1 встановленого файлу, тут — за текстовим depID).
		if cached, ok := a.getSuggestResolveCache(depID); ok {
			if cached != nil {
				cached.RequiredBy = requiredBy[depID]
				out = append(out, *cached)
			}
			continue
		}
		info := a.modrinth.ResolveModInfo(depID, mcVersion, loaderLower)
		if info == nil {
			a.setSuggestResolveCache(depID, nil)
			continue
		}
		sm := model.SuggestedMod{
			DepID: depID, ProjectID: info.ProjectID, Slug: info.Slug,
			Name: info.Title, IconURL: info.IconURL, RequiredBy: requiredBy[depID],
		}
		a.setSuggestResolveCache(depID, &sm)
		out = append(out, sm)
	}
	return out
}

// ── Перевірка оновлень ──────────────────────────────────────────────────
// RefreshModUpdates — примусова мережева перевірка оновлень (кнопка
// "Перевірити оновлення" в UI). Результат кешується під (packID,
// mcVersion, loader), тому наступні відкриття сторінки (ScanModsWithCachedUpdates)
// НЕ лізуть у мережу повторно — лише цей виклик оновлює кеш.
func (a *App) RefreshModUpdates(packID, mcVersion, loader string) []model.ModEntry {
	return a.CheckModUpdates(packID, mcVersion, loader)
}

// CheckModUpdates — перевірка оновлень модів збірки.
//
// Розумна стратегія (smart invalidation):
//  1. Завантажуємо поточний список файлів з диска.
//  2. Для модів, що вже є в кеші (той самий FileName) — беремо закешований
//     результат, не ходимо в мережу повторно.
//  3. Нові файли (яких не було в кеші) та ті, що не мають sha1-кешу —
//     перевіряємо мережево.
//  4. Результат зберігається назад у кеш.
//
// Таким чином додавання/видалення ОДНОГО мода не скасовує перевірку решти.
func (a *App) CheckModUpdates(packID, mcVersion, loader string) []model.ModEntry {
	mods := a.ScanModsFull(packID)
	attachLoaderCompat(mods, loader, hasSinytraConnector(mods))
	if mcVersion == "" || loader == "" {
		for i := range mods {
			mods[i].Status = "done"
		}
		return mods
	}
	loaderLow := strings.ToLower(loader)
	cacheKey := modUpdateCacheKey(packID, mcVersion, loaderLow)

	// Завантажуємо попередній кеш для smart-merge
	a.modUpdateCacheMu.Lock()
	prev, hasPrev := a.modUpdateCache[cacheKey]
	a.modUpdateCacheMu.Unlock()

	var mu sync.Mutex
	done := 0
	total := len(mods)
	emit := func() {
		mu.Lock()
		done++
		d := done
		mu.Unlock()
		// emitModsUpdateProgress (mods_batch.go) — батчинг: цей цикл
		// синхронний і для більшості модів (кеш валідний) миттєвий, тож
		// прямий a.emit тут раніше давав сотні окремих CGo-викликів/подій
		// підряд на збірках з великою кількістю модів — той самий клас
		// «зависання UI», що console_batch.go вже виправляв для логів гри.
		a.emitModsUpdateProgress(map[string]any{"packId": packID, "done": d, "total": total})
	}
	// Жива подія на ОДИН мод: UI патчить рядок одразу після перевірки
	// конкретного файлу (checking → done + hasUpdate), а не чекає, поки
	// перевіряться ВСІ моди (раніше стан змінювався лише в кінці).
	emitOne := func(m *model.ModEntry) {
		// Копія, а не вказівник: подію серіалізують пізніше (батчер), а
		// горутини перевірки тим часом змінюють той самий ModEntry.
		cp := *m
		a.emitModsUpdateOne(map[string]any{"packId": packID, "fileName": cp.FileName, "mod": &cp})
	}

	// ── Прохід 1: кеші (byFile smart-merge + глобальний sha1-кеш) ───────
	// Одразу застосовуємо вже відомі результати; у pending збираємо моди,
	// що реально потребують мережевої перевірки.
	type pendingMod struct {
		m *model.ModEntry
	}
	var pending []pendingMod
	for i := range mods {
		m := &mods[i]
		// ВИМКНЕНІ моди (.jar.disabled) перевіряємо так само, як увімкнені:
		// SHA1 і метадані в них ті самі. Раніше такі моди пропускались —
		// через це у вимкненого мода зникали іконка проєкту, посилання на
		// сторінку Modrinth/CurseForge і позначка оновлення.
		if hasPrev {
			if idx, ok := prev.byFile[m.FileName]; ok {
				c := prev.mods[idx]
				m.HasUpdate = c.HasUpdate
				m.LatestVersion = c.LatestVersion
				m.LatestURL = c.LatestURL
				m.Source = c.Source
				m.ProjectID = c.ProjectID
				m.ProjectSlug = c.ProjectSlug
				m.IconURL = c.IconURL
				m.Status = "done"
				emit()
				continue
			}
		}
		if m.Sha1 != "" {
			if cached, ok := a.getModResolveCache(m.Sha1); ok {
				applyModResolveEntry(m, cached)
				m.Status = "done"
				emit()
				continue
			}
		}
		m.Status = "checking"
		emitOne(m)
		pending = append(pending, pendingMod{m})
	}

	// ── Прохід 2: batch-resolve sha1 на Modrinth (ОДИН запит) ───────────
	// POST /version_files/update — той самий endpoint, що використовує сам
	// Modrinth App: для кожного sha1 повертає НАЙНОВІШУ версію проєкту під
	// вказані loaders+game_versions. Раніше на кожен мод йшло до 3 окремих
	// запитів (GetVersionByHash + GetProject + LatestVersionFor) — тепер
	// один batch на всі моди, а поодинокі запити — лише як fallback для
	// модів, не знайдених за хешем (curl/перейменовані jar тощо).
	var hashes []string
	seenHash := map[string]bool{}
	for _, p := range pending {
		h := p.m.Sha1
		if h != "" && !seenHash[h] {
			seenHash[h] = true
			hashes = append(hashes, h)
		}
	}
	// latestByHash: sha1 → найновіша сумісна версія. Якщо batch впав
	// (мережа), усі pending підуть у per-mod fallback нижче — той самий
	// шлях, що був раніше, тож поведінка деградує не в «помилку», а в
	// стару поодиноку перевірку.
	latestByHash := map[string]*api.FullVersion{}
	// batchOK — чи відповів Modrinth на пакетний запит. Якщо так, ця
	// відповідь остаточна: хеш, якого немає в мапі, = «сумісного оновлення
	// немає», і ми НЕ йдемо далі пошуком за назвою на кожен мод (саме цей
	// fallback робив перевірку на новій збірці в десятки разів повільнішою
	// за Modrinth App, який такого пошуку не робить взагалі).
	batchOK := true
	if len(hashes) > 0 {
		types := []string{"release", "beta", "alpha"}
		if !a.cfg.GetSettings().AllowAlphaBetaModUpdates {
			types = []string{"release"}
		}
		if res, err := a.modrinth.GetLatestVersionsByHashesFiltered(hashes, []string{loaderLow}, []string{mcVersion}, types); err == nil {
			latestByHash = res
		} else {
			// Мережа впала — лише тоді деградуємо до поодинокого резолву.
			batchOK = false
		}
	}
	// Інфо проєктів (іконка/slug) для знайдених — теж одним batch-запитом.
	projIDs := []string{}
	seenProj := map[string]bool{}
	for _, fv := range latestByHash {
		if fv != nil && fv.ProjectID != "" && !seenProj[fv.ProjectID] {
			seenProj[fv.ProjectID] = true
			projIDs = append(projIDs, fv.ProjectID)
		}
	}
	projects := map[string]*api.ModrinthProjectInfo{}
	if len(projIDs) > 0 {
		projects, _ = a.modrinth.GetProjectsBatch(projIDs)
	}

	// ── Прохід 3: застосовуємо batch-результати + fallback для решти ────
	resolved := make([]bool, len(pending)) // чи вже оброблено batch-результатом
	for i, p := range pending {
		m := p.m
		fv, ok := latestByHash[m.Sha1]
		if !ok || fv == nil || fv.ProjectID == "" {
			continue // цей мод не знайшовся за хешем — лишаємо на fallback
		}
		m.Source = "modrinth"
		m.ProjectID = fv.ProjectID
		if pr := projects[fv.ProjectID]; pr != nil {
			m.IconURL = pr.IconURL
			m.ProjectSlug = pr.Slug
		}
		// Порівнюємо встановлений файл з найновішою сумісною версією
		// (за ім'ям файлу насамперед, потім за нормалізованим номером).
		if m.Version != "" && fv.VersionNumber != "" &&
			!isSameModVersion(m.FileName, m.Version, primaryFile(fv).Filename, fv.VersionNumber) {
			m.HasUpdate = true
			m.LatestVersion = fv.VersionNumber
			m.LatestURL = primaryFile(fv).URL
		}
		if m.Sha1 != "" {
			a.setModResolveCache(m.Sha1, m)
		}
		m.Status = "done"
		emit()
		emitOne(m)
		resolved[i] = true
	}

	// Пакетна відповідь Modrinth — фінальна. Усе, чого в ній немає, не має
	// сумісної версії під ці loader/game_version (або взагалі не на
	// Modrinth) — позначаємо «перевірено, оновлень немає» і не робимо
	// жодних поодиноких пошуків за назвою. Це головна швидкісна різниця з
	// Modrinth App.
	if batchOK {
		for i, p := range pending {
			if resolved[i] {
				continue
			}
			p.m.Status = "done"
			emit()
			emitOne(p.m)
			resolved[i] = true
		}
	}

	// ── Прохід 3.5: CurseForge за fingerprint (порт Millida Launcher) ───
	// Не знайдене на Modrinth за sha1 розпізнаємо на CurseForge за його
	// власним відбитком (Murmur2) — ПАЧКАМИ по 100, а не пошуком за назвою
	// для кожного мода (повільно під rate limit 4/с і часто хибно). Лише
	// те, що й так не розпізнано, піде в старий пошук за назвою нижче.
	{
		var idx []int
		var items []*model.ModEntry
		for i, p := range pending {
			if !resolved[i] {
				idx = append(idx, i)
				items = append(items, p.m)
			}
		}
		if len(items) > 0 {
			got := a.identifyOnCurseForgeByFingerprint(packID, items, mcVersion, loaderLow)
			for k, ok := range got {
				if !ok {
					continue
				}
				m := items[k]
				if m.Sha1 != "" {
					a.setModResolveCache(m.Sha1, m)
				}
				m.Status = "done"
				emit()
				emitOne(m)
				resolved[idx[k]] = true
			}
		}
	}

	// ── Прохід 4: per-mod fallback для не знайдених за хешем ───────────
	// Обмежений паралелізм (той самий, що був у старій версії) — сюди
	// потрапляють лише моди, яких немає в batch-результаті Modrinth
	// (не на Modrinth або перейменований файл): пошук за назвою на
	// Modrinth, потім CurseForge.
	sem := make(chan struct{}, modUpdateConcurrency)
	var wg sync.WaitGroup
	for i, p := range pending {
		if resolved[i] {
			continue
		}
		m := p.m
		wg.Add(1)
		go func() {
			defer crashguard.Recover("mods_api.go:939")
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if projID, ok := a.findModOnModrinth(m, mcVersion, loaderLow); ok {
				m.Source = "modrinth"
				m.ProjectID = projID
				if opt := a.modrinthLatest(projID, mcVersion, loaderLow); opt != nil {
					if m.Version != "" && opt.VersionNumber != "" && !isSameModVersion(m.FileName, m.Version, opt.Filename, opt.VersionNumber) {
						m.HasUpdate = true
						m.LatestVersion = opt.VersionNumber
						m.LatestURL = opt.URL
					}
				}
				if p2, err := a.modrinth.GetProject(projID); err == nil && p2 != nil {
					m.IconURL = p2.IconURL
					m.ProjectSlug = p2.Slug
				}
			} else if cf, err := a.findModOnCurseForge(m, mcVersion, loaderLow); err == nil && cf != nil {
				m.Source = "curseforge"
				m.ProjectID = fmt.Sprintf("%d", cf.ID)
				m.IconURL = cf.LogoURL
				m.ProjectSlug = cf.Slug
				if m.Version != "" && cf.FileName != "" && !isSameModVersion(m.FileName, m.Version, cf.FileName, cf.FileName) {
					m.HasUpdate = true
					m.LatestVersion = cf.FileName
					m.LatestURL = cf.DownloadURL
				}
			}
			if m.Sha1 != "" {
				a.setModResolveCache(m.Sha1, m)
			}
			m.Status = "done"
			emit()
			emitOne(m)
		}()
	}
	wg.Wait()

	// Зберігаємо новий кеш з byFile-індексом і timestamp.
	//
	// ВАЖЛИВО (пам'ять): у кеш іде ЛЕГКА копія (updateCacheCopy), а не
	// сам слайс mods. Раніше сюди клався той самий слайс, який щойно
	// повернувся фронтенду — разом з IconData (base64-іконка КОЖНОГО
	// мода), Description, DependsOn тощо. Тобто після кожної перевірки
	// оновлень у пам'яті назавжди (до TTL) лишалась ПОВНА друга копія
	// всього списку модів збірки, помножена на кількість збірок, які
	// користувач відкривав за сесію. Кешу потрібні лише поля про
	// оновлення (їх і читають mergeCachedUpdateInfo та smart-merge на
	// початку цієї ж функції) — усе інше щоразу перечитується з диска.
	cached := updateCacheCopy(mods)
	byFile := make(map[string]int, len(cached))
	for i, m := range cached {
		if m.FileName != "" {
			byFile[m.FileName] = i
		}
	}
	a.modUpdateCacheMu.Lock()
	a.modUpdateCache[cacheKey] = modUpdateCacheEntry{
		mods:   cached,
		byFile: byFile,
		at:     time.Now(),
	}
	a.modUpdateCacheMu.Unlock()
	// Перевірка оновлень на великій збірці — найважча операція сторінки
	// модів (скан jar-ів + мережа + кілька копій списку). Просимо
	// повернути звільнену пам'ять ОС (на Linux ще й malloc_trim).
	maybeReleaseMemory()
	return mods
}

// identifyOnCurseForgeByFingerprint розпізнає моди на CurseForge за Murmur2
// fingerprint (як Millida identify_on_curseforge): відбитки рахуємо паралельно,
// шлемо пачками по 100, назви/іконки проєктів беремо одним POST /mods, а
// найновіший сумісний файл — по одному запиту на унікальний проєкт (кешуються).
// Заповнює Source/ProjectID/ProjectSlug/IconURL/HasUpdate; повертає, які
// елементи розпізнано.
func (a *App) identifyOnCurseForgeByFingerprint(packID string, items []*model.ModEntry, mcVersion, loaderLow string) []bool {
	res := make([]bool, len(items))
	if len(items) == 0 {
		return res
	}
	dir := a.instanceModsDir(packID)
	prints := make([]string, len(items))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for i, m := range items {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, name string) {
			defer crashguard.Recover("mods_api.go:1028")
			defer wg.Done()
			defer func() { <-sem }()
			if fp, err := export.Murmur2Fingerprint(filepath.Join(dir, name)); err == nil {
				prints[i] = fp
			}
		}(i, m.FileName)
	}
	wg.Wait()

	byPrint := map[string][]int{}
	var list []string
	for i, fp := range prints {
		if fp == "" {
			continue
		}
		if _, ok := byPrint[fp]; !ok {
			list = append(list, fp)
		}
		byPrint[fp] = append(byPrint[fp], i)
	}
	if len(list) == 0 {
		return res
	}
	client := custompack.NewCurseForgeClient()
	matches := map[string]custompack.CFFingerprintMatch{}
	for start := 0; start < len(list); start += 100 {
		end := start + 100
		if end > len(list) {
			end = len(list)
		}
		if ms, err := client.ResolveFingerprints(list[start:end]); err == nil {
			for k, v := range ms {
				matches[k] = v
			}
		}
	}
	if len(matches) == 0 {
		return res
	}
	idSet := map[int]bool{}
	var ids []int
	for _, mt := range matches {
		if !idSet[mt.ProjectID] {
			idSet[mt.ProjectID] = true
			ids = append(ids, mt.ProjectID)
		}
	}
	projects, _ := client.GetProjectsByIDs(ids)

	latest := map[int]*custompack.CFFileHit{}
	var lmu sync.Mutex
	lsem := make(chan struct{}, modUpdateConcurrency)
	var lwg sync.WaitGroup
	for _, id := range ids {
		lwg.Add(1)
		lsem <- struct{}{}
		go func(id int) {
			defer crashguard.Recover("mods_api.go:1085")
			defer lwg.Done()
			defer func() { <-lsem }()
			if f, err := a.findCurseForgeFile(client, id, mcVersion, loaderLow); err == nil && f != nil {
				lmu.Lock()
				latest[id] = f
				lmu.Unlock()
			}
		}(id)
	}
	lwg.Wait()

	for fp, mt := range matches {
		for _, i := range byPrint[fp] {
			m := items[i]
			m.Source = "curseforge"
			m.ProjectID = fmt.Sprintf("%d", mt.ProjectID)
			if p, ok := projects[mt.ProjectID]; ok {
				m.IconURL = p.CFLogoURL()
				m.ProjectSlug = p.Slug
			}
			// Оновлення — лише якщо новіший файл (id росте з часом), а не
			// просто інакше названий.
			if f := latest[mt.ProjectID]; f != nil && f.ID > mt.FileID {
				m.HasUpdate = true
				m.LatestVersion = f.FileName
				m.LatestURL = f.DownloadURL
			}
			res[i] = true
		}
	}
	return res
}

// cfProjectIDByFingerprint — точний projectId мода на CurseForge за відбитком
// (для модалки версій: без пошуку за назвою). 0 — не знайдено.
func (a *App) cfProjectIDByFingerprint(packID, fileName string) int {
	fp, err := export.Murmur2Fingerprint(filepath.Join(a.instanceModsDir(packID), fileName))
	if err != nil {
		return 0
	}
	ms, err := custompack.NewCurseForgeClient().ResolveFingerprints([]string{fp})
	if err != nil {
		return 0
	}
	if mt, ok := ms[fp]; ok {
		return mt.ProjectID
	}
	return 0
}

// updateCacheCopy — «худа» копія списку модів для modUpdateCache: лише
// поля, які кеш реально віддає назад (див. mergeCachedUpdateInfo і
// smart-merge у CheckModUpdates). Важкі поля (IconData, Description,
// DependsOn, Provides, Loaders) свідомо НЕ копіюються.
func updateCacheCopy(mods []model.ModEntry) []model.ModEntry {
	out := make([]model.ModEntry, len(mods))
	for i := range mods {
		out[i] = model.ModEntry{
			FileName:      mods[i].FileName,
			HasUpdate:     mods[i].HasUpdate,
			LatestVersion: mods[i].LatestVersion,
			LatestURL:     mods[i].LatestURL,
			Source:        mods[i].Source,
			ProjectID:     mods[i].ProjectID,
			ProjectSlug:   mods[i].ProjectSlug,
			IconURL:       mods[i].IconURL,
		}
	}
	return out
}

// primaryFile повертає головний файл версії Modrinth (primary=true, або
// перший, якщо primary не позначений) — для порівняння імені/URL оновлення.
func primaryFile(fv *api.FullVersion) api.FullVersionFile {
	if fv == nil {
		return api.FullVersionFile{}
	}
	for _, f := range fv.Files {
		if f.Primary {
			return f
		}
	}
	if len(fv.Files) > 0 {
		return fv.Files[0]
	}
	return api.FullVersionFile{}
}

// invalidateModUpdateCache — розумна інвалідація кешу оновлень.
//
// Якщо передано fileNames — видаляє з кешу ТІЛЬКИ ці файли (мод оновлено,
// видалено або доданий новий). Решта перевірених модів залишається в кеші
// і НЕ буде перевірятися мережево знову.
//
// Якщо fileNames порожній — скидає весь кеш для збірки (повна інвалідація,
// наприклад при зміні mcVersion або loader).
func (a *App) invalidateModUpdateCache(packID string, fileNames ...string) {
	a.modUpdateCacheMu.Lock()
	defer a.modUpdateCacheMu.Unlock()
	prefix := packID + "|"
	if len(fileNames) == 0 {
		// Повна інвалідація (стара поведінка) — скидаємо весь кеш збірки.
		for k := range a.modUpdateCache {
			if strings.HasPrefix(k, prefix) {
				delete(a.modUpdateCache, k)
			}
		}
		return
	}
	// Часткова інвалідація: видаляємо лише конкретні файли з кожного
	// кеш-запису збірки. Незмінені моди зберігають свій статус перевірки.
	toRemove := make(map[string]bool, len(fileNames))
	for _, f := range fileNames {
		toRemove[f] = true
	}
	for k, entry := range a.modUpdateCache {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		changed := false
		newMods := entry.mods[:0:len(entry.mods)]
		newMods = append(newMods, entry.mods...)
		newByFile := make(map[string]int, len(newMods))
		filtered := newMods[:0]
		for _, m := range newMods {
			if toRemove[m.FileName] {
				changed = true
				continue
			}
			filtered = append(filtered, m)
		}
		if !changed {
			continue
		}
		for i, m := range filtered {
			newByFile[m.FileName] = i
		}
		a.modUpdateCache[k] = modUpdateCacheEntry{
			mods:   filtered,
			byFile: newByFile,
			at:     entry.at, // зберігаємо час запису — TTL рахується від нього
		}
	}
}

// findModOnModrinth: спершу точний збіг за SHA1 файлу, інакше пошук за назвою.
// getModResolveCache читає глобальний (між усіма збірками) кеш
// результату мережевого резолву мода за sha1, з урахуванням TTL.
// invalidateModResolveCache повністю скидає ГЛОБАЛЬНИЙ кеш резолву модів
// (за sha1, спільний для всіх збірок) — викликається при зміні
// AllowAlphaBetaModUpdates, бо закешований "hasUpdate"/"latestVersion" міг
// бути порахований під СТАРЕ значення прапорця (release-only чи
// включно з alpha/beta) і більше не відповідає новому вибору користувача.
func (a *App) invalidateModResolveCache() {
	a.modResolveCacheMu.Lock()
	a.modResolveCache = map[string]modResolveEntry{}
	a.modResolveCacheMu.Unlock()
}

func (a *App) getModResolveCache(sha1 string) (modResolveEntry, bool) {
	a.modResolveCacheMu.Lock()
	defer a.modResolveCacheMu.Unlock()
	e, ok := a.modResolveCache[sha1]
	if !ok || time.Since(e.at) > modResolveCacheTTL {
		return modResolveEntry{}, false
	}
	return e, true
}

// setModResolveCache зберігає результат мережевого резолву мода під
// sha1 файлу — доступний одразу всім збіркам, що мають той самий мод.
func (a *App) setModResolveCache(sha1 string, m *model.ModEntry) {
	a.modResolveCacheMu.Lock()
	defer a.modResolveCacheMu.Unlock()
	a.modResolveCache[sha1] = modResolveEntry{
		source: m.Source, projectID: m.ProjectID, projectSlug: m.ProjectSlug,
		iconURL: m.IconURL, hasUpdate: m.HasUpdate,
		latestVersion: m.LatestVersion, latestURL: m.LatestURL, at: time.Now(),
	}
	if len(a.modResolveCache) > 5000 {
		a.modResolveCache = map[string]modResolveEntry{}
	}
}

// applyModResolveEntry переносить закешований результат резолву на
// конкретний ModEntry цієї збірки (той самий sha1 може відповідати
// файлу з іншим FileName в іншій збірці — усе інше однакове).
func applyModResolveEntry(m *model.ModEntry, e modResolveEntry) {
	m.Source = e.source
	m.ProjectID = e.projectID
	m.ProjectSlug = e.projectSlug
	if e.iconURL != "" {
		m.IconURL = e.iconURL
	}
	m.HasUpdate = e.hasUpdate
	m.LatestVersion = e.latestVersion
	m.LatestURL = e.latestURL
}

func (a *App) findModOnModrinth(m *model.ModEntry, mcVersion, loader string) (string, bool) {
	if m.Sha1 != "" {
		if v, err := a.modrinth.GetVersionByHash(m.Sha1); err == nil && v != nil && v.ProjectID != "" {
			return v.ProjectID, true
		}
	}
	if m.Name == "" {
		return "", false
	}
	// Пошук за назвою: спершу оригінальну назву, потім «чисту» (без
	// версійних суфіксів з jar-файлу, cleanModSearchName) — сирі назви на
	// кшталт "smoothchunk-1.20.1-4.1" у пошуку дають 0 результатів, хоча
	// сам мод на Modrinth/CurseForge є.
	for _, q := range []string{m.Name, cleanModSearchName(m.Name)} {
		if q == "" {
			continue
		}
		hits, err := a.modrinth.SearchMods(q, mcVersion, loader, 5, 0)
		if err != nil || len(hits) == 0 {
			continue
		}
		best := api.BestModrinthMatch(hits, q)
		if best != nil && best.ProjectID != "" {
			return best.ProjectID, true
		}
	}
	return "", false
}

func (a *App) modrinthLatest(projectID, mcVersion, loader string) *model.ModVersionOption {
	opt, err := a.modrinth.LatestVersionFor(projectID, mcVersion, loader, a.cfg.GetSettings().AllowAlphaBetaModUpdates)
	if err != nil || opt == nil {
		return nil
	}
	return opt
}

// cfModVersion — знайдений на CurseForge мод з його найсвіжішим файлом.
type cfModVersion struct {
	ID          int
	Name        string
	Slug        string
	LogoURL     string
	FileName    string
	DownloadURL string
}

// bestCurseForgeMatch — той самий принцип, що й pickBestModrinthMatch:
// точний нормалізований збіг slug/name, інакше префіксний збіг, інакше
// nil (не беремо перший-ліпший результат пошуку). Дублює normalizeDep з
// internal/api навмисно — різні пакети (main/api), тягнути окремий
// спільний пакет заради однієї функції на 6 рядків не виправдано.
func bestCurseForgeMatch(hits []custompack.CFSearchHit, name string) *custompack.CFSearchHit {
	if len(hits) == 0 {
		return nil
	}
	needle := normalizeDepName(name)
	if needle == "" {
		return nil
	}
	// 1) Точний збіг slug/назви (нормалізований — без регістру/розділювачів).
	for i := range hits {
		if normalizeDepName(hits[i].Slug) == needle || normalizeDepName(hits[i].Name) == needle {
			return &hits[i]
		}
	}
	// 2) Префіксний збіг.
	for i := range hits {
		if strings.HasPrefix(normalizeDepName(hits[i].Slug), needle) || strings.HasPrefix(normalizeDepName(hits[i].Name), needle) {
			return &hits[i]
		}
	}
	// 3) М'який фолбек: CONTAINS-збіг у будь-який бік. Назва мода на
	// CurseForge часто відрізняється від локальної (суфікси "-fabric",
	// "(Fabric)", різні переклади, версія в назві) — через це раніше
	// легальні CF-моди лишались "не знайденими" у міграції/перевірці
	// оновлень. Contains має коротший шлях до помилкового збігу, тому йде
	// ОСТАННІМ і лише коли жоден жорсткий варіант не спрацював.
	for i := range hits {
		hit := normalizeDepName(hits[i].Name)
		if hit != "" && (strings.Contains(hit, needle) || strings.Contains(needle, hit)) {
			return &hits[i]
		}
	}
	return nil
}

// strictCurseForgeMatch — СТРОГИЙ збіг для пошуку БЕЗ фільтрів версії
// гри/лоадера. Такий пошук повертає популярні проєкти з усього CurseForge,
// тож contains-фолбек тут небезпечний (може підхопити чужий мод із
// схожою підстрокою). Беремо лише точний або префіксний збіг назви/slug
// — мод існує, але ще не оновлений під поточну MC, майже завжди
// знаходиться цим способом.
func strictCurseForgeMatch(hits []custompack.CFSearchHit, name string) *custompack.CFSearchHit {
	if len(hits) == 0 {
		return nil
	}
	needle := normalizeDepName(name)
	if needle == "" {
		return nil
	}
	for i := range hits {
		if normalizeDepName(hits[i].Slug) == needle || normalizeDepName(hits[i].Name) == needle {
			return &hits[i]
		}
	}
	for i := range hits {
		if strings.HasPrefix(normalizeDepName(hits[i].Slug), needle) || strings.HasPrefix(normalizeDepName(hits[i].Name), needle) {
			return &hits[i]
		}
	}
	return nil
}

// isSameModVersion порівнює встановлений файл/версію мода з файлом/
// версією, знайденою на Modrinth/CurseForge — за іменем файлу насамперед
// (точне, не залежить від того, як автор проєкту оформив version_number),
// з фолбеком на толерантне порівняння номера версії (sameReleaseVersion).
// Заміняє крихке пряме порівняння рядків version_number, через яке
// HasUpdate міг помилково вилазити навіть коли встановлена версія й
// остання — та сама (просто по-різному підписана), і навпаки — не
// показувати оновлення там, де формат номера версії випадково збігся,
// а сама версія інша.
func isSameModVersion(installedFile, installedVersion, remoteFile, remoteVersion string) bool {
	instFile := strings.TrimSuffix(installedFile, ".disabled")
	if instFile != "" && remoteFile != "" && strings.EqualFold(decodeURLTokens(instFile), decodeURLTokens(remoteFile)) {
		return true
	}
	if installedVersion == "" || remoteVersion == "" {
		return false
	}
	return sameReleaseVersion(installedVersion, remoteVersion)
}

// decodeURLTokens повертає ім'я файлу/версію з розкодованими URL-токенами,
// які CurseForge/Modrinth CDN лишають у назвах файлів і посиланнях на
// завантаження: "+" у версіях на кшталт "1.5.5+1.20.4" передається як
// %2B, пробіл — %20, дужки — %5B/%5D/%28/%29, кома — %2C, не-ASCII —
// %C3%A9 тощо. Файл, збережений лаунчером із URL, може лежати на диску з
// літеральним "%XX" в імені, а віддалена назва містить сам символ — тоді
// точне порівняння імен завжди хибне, і лаунчер вважає мод неоновленим
// (пропонує «оновити» до тієї самої версії).
//
// Раніше розкодовувалось лише %2B і %20, тож будь-який інший токен (%5B,
// %28, %2C, %C3%A9 …) ламав порівняння. Тепер розкодовуємо ПОСЛІДОВНОСТІ
// валідних %XX, якщо вони складають коректний UTF-8; усе інше — одинокий
// "%" ("100%.jar"), обрізаний "%2", випадкові байти — лишається як є, тож
// легальний літерал "%" в іменах файлів не псується. Функція симетрична:
// її застосовують до обох боків порівняння.
var pctRunRe = regexp.MustCompile(`(?:%[0-9A-Fa-f]{2})+`)

func decodeURLTokens(s string) string {
	if !strings.Contains(s, "%") {
		return s
	}
	return pctRunRe.ReplaceAllStringFunc(s, func(run string) string {
		b, err := hex.DecodeString(strings.ReplaceAll(run, "%", ""))
		if err != nil || !utf8.Valid(b) {
			return run
		}
		return string(b)
	})
}

// versionLoaderSuffixes — текстові суфікси, які автори додають до НОМЕРА
// версії, щоб позначити лоадер. Modrinth дозволяє різним версіям одного
// релізу мати різні version_number під різні лоадери — типовий приклад:
// встановлений з CDN файл "...-1.5.5+1.20.4.jar" (мод збірки Forge,
// version у mods.toml = "1.5.5+1.20.4"), а на Modrinth та сама версія
// проєкту називається "1.5.5+1.20.4-forge". Без зрізання суфікса ці два
// рядки вважаються РІЗНИМИ версіями → хибне «є оновлення». (Fabric-версія
// того самого білда тоді називається "1.5.5+1.20.4-fabric".)
var versionLoaderSuffixes = []string{"-neoforge", "-forge", "-fabric", "-quilt", "-fml", "-legacyfabric"}

// sameReleaseVersion — чи два рядки версій позначають ТОЙ САМИЙ реліз
// мода, попри різне форматування в різних місцях:
//
//   - все до '+' ігнорується (FlexVer-семантика: "+" та appendix після
//     нього не впливають на порівняння — той самий підхід, що й у
//     internal/versions та у фронтендовому flexver.ts);
//   - з «ядра» зрізається хвостовий суфікс лоадера ("1.5.5-forge" →
//     "1.5.5") — теж чисто косметична різниця, не нова версія;
//   - регістр ігнорується (текстові секції порівнюються в lowercase).
//
// Порівнюємо за FlexVer, а не за normalizeDepName: "1.5.5" == "1.5.5",
// але "1.5.5" != "1.5.50" чи "1.5.5" != "1.5.5b" (справжні нові версії
// лишаються «оновленнями»).
func sameReleaseVersion(a, b string) bool {
	ca := versionCore(a)
	cb := versionCore(b)
	if ca == "" || cb == "" {
		return false
	}
	if ca == cb {
		return true
	}
	return versions.Compare(ca, cb) == 0
}

// versionCore — «ядро» версії для порівняння: без URL-токенів, без
// appendix (усе після '+') і без хвостового суфікса лоадера, у lowercase.
func versionCore(s string) string {
	s = decodeURLTokens(strings.TrimSpace(s))
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i]
	}
	low := strings.ToLower(s)
	for _, suf := range versionLoaderSuffixes {
		if strings.HasSuffix(low, suf) {
			low = low[:len(low)-len(suf)]
			break
		}
	}
	return low
}

func normalizeDepName(s string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// findCurseForgeProject знаходить САМ мод (проєкт CurseForge) за назвою.
// Порядок: СПЕРШУ ПОШУК БЕЗ ФІЛЬТРІВ (знайти мод), і лише якщо він не
// знайшовся — пошук з фільтрами версії гри/лоадера. Це принципово і так
// просить користувач: CurseForge search з gameVersion-параметром повертає
// ЛИШЕ проєкти, що вже мають файл саме під цю версію гри — якщо мод існує
// на CurseForge, але ще не оновлений під поточну MC (або назва jar
// відрізняється від slug проєкту, або у файлі метаданих версійний суфікс),
// такий пошук дає 0 результатів, хоча сам мод знайти можна. Версію/лоадер
// шукаємо ОКРЕМО серед файлів знайденого проєкту (findCurseForgeFile) —
// спочатку мод, потім його сумісна версія.
func (a *App) findCurseForgeProject(m *model.ModEntry, mcVersion, loader string) (*custompack.CFSearchHit, error) {
	client := custompack.NewCurseForgeClient()
	if !client.HasAPIKey() {
		return nil, fmt.Errorf("CurseForge API-ключ не налаштовано у цій збірці лаунчера")
	}
	query := m.Name
	if query == "" {
		return nil, fmt.Errorf("немає назви мода для пошуку на CurseForge")
	}
	// ── Порядок пошуку: СПОЧАТКУ ЗНАХОДИМО САМ МОД, ПОТІМ ВЕРСІЇ ──────
	// Раніше першим ішов пошук З ФІЛЬТРАМИ версії гри/лоадера
	// (gameVersion=1.20.1&modLoaderType=...). Проблема: CurseForge search
	// з gameVersion-параметром повертає ЛИШЕ проєкти, що вже мають файл
	// саме під цю версію — і тихо дає 0 результатів для легальних модів
	// (неспівпадіння формату версії в їхній базі, відкладений реліз під
	// нову MC тощо). Тому тепер навпаки: спершу шукаємо проєкт БЕЗ
	// фільтрів (знайти мод), а сумісну версію визначаємо окремо серед
	// файлів знайденого проєкту (findCurseForgeFile / GetModFiles).
	//
	// Друга проблема: назва з jar-файлу часто містить суфікс версії
	// (smoothchunk-1.20.1-4.1.jar → searchFilter "smoothchunk-1.20.1-4.1"
	// нічого не знайде). Пробуємо обидва варіанти: оригінальну назву і
	// «чисту» (без версійних суфіксів, cleanModSearchName).
	queries := []string{query}
	if clean := cleanModSearchName(query); clean != "" && clean != query {
		queries = append(queries, clean)
	}
	// 1) Unfiltered — знайти САМ мод (строгий збіг, щоб не підхопити
	// чужий популярний проєкт).
	for _, q := range queries {
		if hits, err := client.SearchMods(q, "", ""); err == nil {
			if hit := strictCurseForgeMatch(hits, q); hit != nil {
				return hit, nil
			}
		}
	}
	// 2) Filtered — фолбек для спільних назв, де unfiltered пошук дає
	// багато сторонніх проєктів: версія/лоадер звужують кандидатів, тож
	// можна дозволити м'якший contains-збіг.
	for _, q := range queries {
		if hits, err := client.SearchMods(q, mcVersion, loader); err == nil {
			if hit := bestCurseForgeMatch(hits, q); hit != nil {
				return hit, nil
			}
		}
	}
	return nil, fmt.Errorf("не знайдено на CurseForge")
}

// cleanModSearchName прибирає з назви jar-файлу версійні/складальні
// суфікси, щоб таку назву можна було шукати на Modrinth/CurseForge:
//
//	smoothchunk-1.20.1-4.1  →  smoothchunk
//	kotlinforforge-4.12.0-all → kotlinforforge
//	gpumemleakfix-1.20.1-1.8  →  gpumemleakfix
//
// Такі «сирі» назви приходять з jar, у якого немає метаданих (mods.toml/
// fabric.mod.json) або displayName у них порожній — тоді Name = ім'я файлу
// без .jar. Пошук із суфіксом версії завжди дає 0 результатів, хоча сам
// мод на CurseForge є (Cupboard, Smooth Chunk, GPU Memory Leak Fix...).
func cleanModSearchName(name string) string {
	s := name
	for {
		idx := strings.LastIndexByte(s, '-')
		if idx <= 0 {
			break
		}
		seg := s[idx+1:]
		if isModSearchNoise(seg) {
			s = s[:idx]
			continue
		}
		break
	}
	if s == "" {
		return name
	}
	return s
}

// isModSearchNoise — сегмент назви, який не є частиною «слагу» мода:
// номер версії (починається з цифри) або класифікатор збірки
// (all/universal/sources/dev/forge/fabric тощо).
func isModSearchNoise(seg string) bool {
	if seg == "" {
		return false
	}
	if seg[0] >= '0' && seg[0] <= '9' {
		return true
	}
	switch strings.ToLower(seg) {
	case "all", "universal", "sources", "dev", "src", "stable", "beta", "alpha", "latest",
		"forge", "fabric", "neoforge", "quilt":
		return true
	}
	return false
}

// findCurseForgeFile шукає СУМІСНИЙ файл (версію) знайденого проєкту
// CurseForge — лише ті, що підходять під цільову версію гри та лоадер.
// НЕ робимо fallback на всі файли проєкту: інакше міграція/оновлення
// взяли б файл для ІНШОЇ MC-версії чи лоадера (мод «знайдено», але
// качається несумісний jar, який зламає гру). Якщо сумісних немає —
// помилка; викликач (findModOnCurseForge) усе одно повертає проєкт без
// файлу, тож «мод знайдено, але версії під цю MC нема» обробляється
// коректно. УСІ файли (для модалки вибору версії) бере GetModVersions
// окремо — там показ повного списку і є сенс.
func (a *App) findCurseForgeFile(client *custompack.CurseForgeClient, projectID int, mcVersion, loader string) (*custompack.CFFileHit, error) {
	files, err := client.GetModFiles(projectID, mcVersion, loader)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("немає файлів, сумісних з цією версією гри/лоадером")
	}
	// Останній ДОЗВОЛЕНИЙ файл за каналом релізу (release завжди; beta/
	// alpha — лише з AllowAlphaBetaModUpdates). Раніше тут сліпо брався
	// files[0] — найсвіжіший за датою, навіть якщо то beta/alpha, випущена
	// після останнього стабільного релізу (напр. JEI: 3 свіжі beta над
	// останнім release), і налаштування AllowAlphaBetaModUpdates взагалі
	// не застосовувалось до CurseForge-шляху (лише Modrinth).
	settings := a.cfg.GetSettings()
	if best := custompack.CFLatestFile(files, settings.AllowAlphaBetaModUpdates, settings.AllowAlphaBetaModUpdates); best != nil {
		return best, nil
	}
	// Усі файли — лише beta/alpha, а користувач їх вимкнув: сумісної
	// версії «для оновлення» немає. Повертаємо nil (не помилку мережі):
	// викликачі трактують nil як «немає версії під цю збірку», а не як
	// збій запиту.
	return nil, nil
}

func (a *App) findModOnCurseForge(m *model.ModEntry, mcVersion, loader string) (*cfModVersion, error) {
	hit, err := a.findCurseForgeProject(m, mcVersion, loader)
	if err != nil {
		return nil, err
	}
	client := custompack.NewCurseForgeClient()
	f, ferr := a.findCurseForgeFile(client, hit.ID, mcVersion, loader)
	out := &cfModVersion{ID: hit.ID, Name: hit.Name, Slug: hit.Slug, LogoURL: hit.Logo.URL}
	if ferr == nil && f != nil {
		out.FileName = f.FileName
		out.DownloadURL = f.DownloadURL
	}
	// Мод знайдено (проєкт є), але сумісної версії нема — повертаємо
	// проєкт без файлу: викликач бачить Source/ProjectID/Slug (мод знайдено)
	// і не вважає відсутність версії під цю MC/лоадер помилкою пошуку.
	return out, nil
}

// ── Версії мода (модалка «Змінити версію») ──────────────────────────────
// mcVersion/loader — версія гри та лоадер ПОТОЧНОЇ збірки: список версій
// у модалці показує лише сумісні з нею білди мода, інші (для інших
// луадерів/версій гри) все одно б не запустилися і лише плутали б.
func (a *App) GetModVersions(packID, fileName, mcVersion, loader string) []model.ModVersionOption {
	mods := a.ScanModsFull(packID)
	var mod *model.ModEntry
	for i := range mods {
		if mods[i].FileName == fileName {
			mod = &mods[i]
			break
		}
	}
	if mod == nil {
		return nil
	}
	loader = strings.ToLower(loader)
	// Modrinth першим, CurseForge — fallback.
	if projID, ok := a.findModOnModrinth(mod, mcVersion, loader); ok {
		if opts, err := a.modrinth.AllVersionsFor(projID, mcVersion, loader); err == nil && len(opts) > 0 {
			markCurrentModVersion(opts, mod)
			return opts
		}
	}
	// CurseForge: спочатку знаходимо САМ мод (проєкт), потім його версії.
	// Якщо під поточну MC/лоадер файлів нема — повертаємо ВСІ файли проєкту
	// (користувач бачить список доступних версій мода, а не порожню модалку).
	var cfHit *custompack.CFSearchHit
	if id := a.cfProjectIDByFingerprint(packID, fileName); id > 0 {
		cfHit = &custompack.CFSearchHit{ID: id, Name: mod.Name}
	} else if h, err := a.findCurseForgeProject(mod, mcVersion, loader); err == nil {
		cfHit = h
	}
	if hit := cfHit; hit != nil {
		client := custompack.NewCurseForgeClient()
		files, ferr := client.GetModFiles(hit.ID, mcVersion, loader)
		if ferr != nil || len(files) == 0 {
			files, ferr = client.GetModFiles(hit.ID, "", "")
		}
		if ferr == nil && len(files) > 0 {
			opts := make([]model.ModVersionOption, 0, len(files))
			for _, f := range files {
				opts = append(opts, model.ModVersionOption{
					ID: fmt.Sprintf("%d", f.ID), Name: hit.Name, VersionNumber: f.FileName,
					URL: f.DownloadURL, Filename: f.FileName, DatePublished: f.FileDate, Sha1: f.SHA1(),
				})
			}
			markCurrentModVersion(opts, mod)
			return opts
		}
	}
	return nil
}

// markCurrentModVersion позначає IsCurrent=true на опції(ях), що
// відповідають фактично встановленому файлу мода. Порядок перевірки:
// 1) sha1 (найточніше — байт-у-байт той самий файл); 2) точна назва
// файлу; 3) нормалізований номер версії (fallback, коли автор проєкту
// перезалив той самий білд під новим іменем файлу). Раніше порівнювали
// голий versionNumber з версією з mods.toml — і для Create це майже
// завжди не збігалось через різне форматування ("6.0.8" на диску проти
// "mc1.20.1-6.0.8" на Modrinth), тож поточна версія в списку не
// підсвічувалась взагалі.
func markCurrentModVersion(opts []model.ModVersionOption, mod *model.ModEntry) {
	if mod == nil {
		return
	}
	for i := range opts {
		if mod.Sha1 != "" && opts[i].Sha1 != "" && strings.EqualFold(opts[i].Sha1, mod.Sha1) {
			opts[i].IsCurrent = true
			return
		}
	}
	if mod.FileName != "" {
		wantFile := decodeURLTokens(strings.TrimSuffix(mod.FileName, ".disabled"))
		for i := range opts {
			if opts[i].Filename != "" && strings.EqualFold(decodeURLTokens(opts[i].Filename), wantFile) {
				opts[i].IsCurrent = true
				return
			}
		}
	}
	if mod.Version != "" {
		for i := range opts {
			if opts[i].VersionNumber != "" && sameReleaseVersion(opts[i].VersionNumber, mod.Version) {
				opts[i].IsCurrent = true
				return
			}
		}
	}
}

// InstallModVersion завантажує обрану версію і замінює нею поточний файл
// (стан «вимкнено» зберігається).
func (a *App) InstallModVersion(packID, fileName string, opt model.ModVersionOption) error {
	if err := a.modsBusyErr(packID); err != nil {
		return err
	}
	if err := a.modsLockedErr(packID, fileName); err != nil {
		return err
	}
	modsDir := a.instanceModsDir(packID)
	if opt.URL == "" || opt.Filename == "" {
		return fmt.Errorf("немає посилання на файл версії")
	}
	opt.Filename = safeModFileName(opt.Filename) // без "%" в імені — інакше мод не запуститься
	tmp := filepath.Join(modsDir, "."+opt.Filename+".part")
	if err := a.downloadModFile(opt.URL, tmp); err != nil {
		return err
	}
	disabled := strings.HasSuffix(fileName, ".jar.disabled")
	_ = os.Remove(filepath.Join(modsDir, fileName))
	_ = os.Remove(strings.TrimSuffix(fileName, ".disabled"))
	newName := opt.Filename
	if disabled {
		// opt.Filename вже закінчується на ".jar" — просто додаємо
		// ".disabled" (те саме, що робить ToggleMod). Раніше тут було
		// opt.Filename + ".jar.disabled", що давало подвійне "mod.jar.jar.disabled".
		newName = opt.Filename + ".disabled"
	}
	if err := os.Rename(tmp, filepath.Join(modsDir, newName)); err != nil {
		os.Remove(tmp)
		return err
	}
	// Smart: видаляємо стару і нову назву (вони різні після зміни версії)
	a.invalidateModUpdateCache(packID, fileName, newName)
	return nil
}

// ── Посилання на сторінку мода (меню «⋯» → «Скопіювати посилання») ────
// Повертає URL сторінки мода на Modrinth/CurseForge, щоб користувач міг
// швидко відкрити його в браузері. Спершу береться вже відомий slug/
// projectId зі скана/кешу оновлень; якщо його нема — мод резолвиться на
// льоту тим самим шляхом, що й перевірка оновлень (Modrinth за sha1/
// назвою, фолбек CurseForge). Порожній рядок = мод знайти не вдалось.
func (a *App) GetModPageLink(packID, fileName, mcVersion, loader string) string {
	mods := a.ScanModsFull(packID)
	var m *model.ModEntry
	for i := range mods {
		if mods[i].FileName == fileName {
			m = &mods[i]
			break
		}
	}
	if m == nil {
		return ""
	}
	if m.ProjectSlug != "" || (m.ProjectID != "" && m.Source != "") {
		if url := modPageURL(m.Source, m.ProjectSlug, m.ProjectID); url != "" {
			return url
		}
	}
	// Слагу ще нема (перший скан без перевірки оновлень) — резолвимо мод
	// і качаємо slug проєкту на льоту.
	if projID, ok := a.findModOnModrinth(m, mcVersion, loader); ok {
		if p, err := a.modrinth.GetProject(projID); err == nil && p != nil && p.Slug != "" {
			return "https://modrinth.com/mod/" + p.Slug
		}
	}
	if cf, err := a.findModOnCurseForge(m, mcVersion, loader); err == nil && cf != nil {
		if cf.Slug != "" {
			return "https://www.curseforge.com/minecraft/mc-mods/" + cf.Slug
		}
		if cf.ID > 0 {
			return fmt.Sprintf("https://www.curseforge.com/minecraft/mc-mods/%d", cf.ID)
		}
	}
	return ""
}

// curseForgeTypePath повертає шлях CurseForge для типу контенту.
func curseForgeTypePath(projectType string) string {
	switch projectType {
	case "resourcepack":
		return "texture-packs"
	case "shader":
		return "shader-packs"
	case "datapack":
		return "data-packs"
	default:
		return "mc-mods"
	}
}

// modPageURL будує URL сторінки мода за джерелом/slug/projectId.
func modPageURL(source, slug, projectID string) string {
	return modPageURLWithType(source, slug, projectID, "mod")
}

// modPageURLWithType будує URL сторінки контенту з урахуванням типу
// (mod, resourcepack, shader, datapack) для правильних CurseForge посилань.
func modPageURLWithType(source, slug, projectID, projectType string) string {
	switch source {
	case "modrinth":
		if slug != "" {
			return "https://modrinth.com/mod/" + slug
		}
		if projectID != "" {
			return "https://modrinth.com/mod/" + projectID
		}
	case "curseforge":
		path := curseForgeTypePath(projectType)
		if slug != "" {
			return "https://www.curseforge.com/minecraft/" + path + "/" + slug
		}
		if projectID != "" {
			return "https://www.curseforge.com/minecraft/" + path + "/" + projectID
		}
	}
	return ""
}

// ── Масове оновлення + резервні копії ───────────────────────────────────
func (a *App) UpdateAllMods(packID, mcVersion, loader string, backup bool) model.ModUpdateResult {
	res := model.EmptyModUpdateResult()
	if err := a.modsBusyErr(packID); err != nil {
		res.Failed = append(res.Failed, err.Error())
		return res
	}
	// Використовуємо вже відому (закешовану) інформацію про оновлення —
	// користувач бачить кнопку "Оновити всі" саме тому, що перевірка вже
	// була виконана (RefreshModUpdates) і показала доступні оновлення;
	// повторний мережевий похід тут був би зайвим і сповільнював би клік.
	mods := a.ScanModsWithCachedUpdates(packID, mcVersion, loader)
	var toUpdate []model.ModEntry
	for _, m := range mods {
		// Моди з «єдиного джерела» оновлює джерело, а не Modrinth/CurseForge.
		if m.SourceKind != "" {
			continue
		}
		if m.Enabled && m.HasUpdate && m.LatestURL != "" {
			toUpdate = append(toUpdate, m)
		}
	}
	if len(toUpdate) == 0 {
		return res
	}
	backupsDir := a.modsBackupsDir(packID)
	if backup {
		stamp := time.Now().Format("20060102-150405")
		if err := os.MkdirAll(filepath.Join(backupsDir, stamp), 0755); err == nil {
			for _, m := range toUpdate {
				if data, err := os.ReadFile(filepath.Join(a.instanceModsDir(packID), m.FileName)); err == nil {
					bak := filepath.Join(stamp, m.FileName)
					if os.WriteFile(filepath.Join(backupsDir, bak), data, 0644) == nil {
						a.appendBackupEntry(packID, m, bak)
					}
				}
			}
			res.BackedUp = true
		}
	}
	// Прибираємо СТАРІ файли тих самих модів (іноді автор змінює ім'я jar)
	// одним проходом по вже відсканованому списку. Раніше це робив
	// installModUpdate через повторний ScanModsFull на КОЖЕН мод — на
	// «Оновити всі» збірка перечитувалась десятки разів, і оновлення
	// тривало хвилинами.
	for _, m := range toUpdate {
		if m.ID == "" {
			continue
		}
		for _, other := range mods {
			if other.ID == m.ID && other.FileName != m.FileName {
				removeModFileName(a.instanceModsDir(packID), other.FileName)
			}
		}
	}
	for i, m := range toUpdate {
		// emitModsUpdateProgress — той самий батчер, що вище в
		// CheckModUpdates (mods_batch.go): тут кожна подія природно
		// розділена мережевою/дисковою операцією installModUpdate, тож
		// сплеску подій тут самого по собі не буває, але батчер потрібен
		// для узгодженості формату на фронтенді — обидва виклики шлють
		// один логічний канал (PackModsSection слухає одну подію на
		// обидві фази "перевірка"/"встановлення").
		a.emitModsUpdateProgress(map[string]any{
			"packId": packID, "phase": "update", "current": i + 1, "total": len(toUpdate), "fileName": m.FileName,
		})
		if err := a.installModUpdate(packID, m); err != nil {
			res.Failed = append(res.Failed, fmt.Sprintf("%s: %v", m.Name, err))
		} else {
			res.Updated = append(res.Updated, m.Name)
		}
	}
	// Smart invalidation: видаляємо з кешу лише оновлені файли
	// (стара назва файлу мода) — решта зберігає свої перевірені стани.
	updatedFiles := make([]string, 0, len(toUpdate))
	for _, m := range toUpdate {
		updatedFiles = append(updatedFiles, m.FileName)
	}
	a.invalidateModUpdateCache(packID, updatedFiles...)
	return res
}

// installModUpdate замінює мод на нову версію (нова назва файлу з URL).
func (a *App) installModUpdate(packID string, m model.ModEntry) error {
	modsDir := a.instanceModsDir(packID)
	if m.LatestURL == "" || m.LatestVersion == "" {
		return fmt.Errorf("немає даних оновлення")
	}
	newName := urlLastSegment(m.LatestURL)
	if newName == "" || !strings.HasSuffix(strings.ToLower(newName), ".jar") {
		newName = sanitizeFileName(m.ProjectSlug) + "-" + sanitizeFileName(m.LatestVersion) + ".jar"
	}
	if newName == "" {
		newName = sanitizeFileName(m.Name) + ".jar"
	}
	// Видаляємо лише відомий файл (і його .disabled-двійника). Повний
	// повторний скан теки тут не потрібен: «Оновити всі» заздалегідь
	// прибирає перейменовані файли одним проходом, а одиночне оновлення
	// кличе removeModFilesByID перед входом сюди.
	removeModFileName(modsDir, m.FileName)
	tmp := filepath.Join(modsDir, "."+newName+".part")
	if err := a.downloadModFile(m.LatestURL, tmp); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(modsDir, newName))
}

// removeModFileName видаляє конкретний jar і його .disabled-двійника.
func removeModFileName(modsDir, fileName string) {
	if fileName == "" {
		return
	}
	_ = os.Remove(filepath.Join(modsDir, fileName))
	_ = os.Remove(filepath.Join(modsDir, strings.TrimSuffix(fileName, ".disabled")))
	_ = os.Remove(filepath.Join(modsDir, fileName+".disabled"))
}

// removeModFilesByID видаляє усі файли мода з цим modId (і явний fileName),
// щоб після оновлення не лишилося двох версій одного мода.
func (a *App) removeModFilesByID(modsDir, modID, fileName string) {
	if fileName != "" {
		_ = os.Remove(filepath.Join(modsDir, fileName))
		_ = os.Remove(strings.TrimSuffix(filepath.Join(modsDir, fileName), ".disabled"))
	}
	if modID == "" {
		return
	}
	// Знаходимо всі jar-файли з цим modId (оновлена версія могла змінити
	// ім'я файлу) і видаляємо їх разом із .disabled-двійниками.
	mods, err := a.modMgr.ScanModsFull(modsDir)
	if err != nil {
		return
	}
	for _, m := range mods {
		if m.ID == modID {
			_ = os.Remove(filepath.Join(modsDir, m.FileName))
			if strings.HasSuffix(m.FileName, ".jar") {
				_ = os.Remove(filepath.Join(modsDir, m.FileName+".disabled"))
			}
		}
	}
}

// UpdateOneModBackup — резервна копія перед оновленням ОДНОГО мода
// (кнопка «оновити» на картці мода в списку, а не масове «Оновити
// всі»). Той самий формат каталогу/manifest.json, що й UpdateAllMods,
// щоб RestoreModBackup працював однаково для обох сценаріїв.
func (a *App) UpdateOneModBackup(packID string, m model.ModEntry) error {
	backupsDir := a.modsBackupsDir(packID)
	stamp := time.Now().Format("20060102-150405")
	if err := os.MkdirAll(filepath.Join(backupsDir, stamp), 0755); err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(a.instanceModsDir(packID), m.FileName))
	if err != nil {
		return fmt.Errorf("не вдалось прочитати файл мода: %v", err)
	}
	bak := filepath.Join(stamp, m.FileName)
	if err := os.WriteFile(filepath.Join(backupsDir, bak), data, 0644); err != nil {
		return err
	}
	a.appendBackupEntry(packID, m, bak)
	return nil
}

// UpdateOneMod оновлює один мод до LatestVersion/LatestURL (вже відомих
// зі сканування/перевірки оновлень). backup=true — спершу створює
// резервну копію поточного файлу (UpdateOneModBackup), щоб можна було
// відкотитись через ListModBackups/RestoreModBackup.
func (a *App) UpdateOneMod(packID string, m model.ModEntry, backup bool) error {
	if err := a.modsBusyErr(packID); err != nil {
		return err
	}
	if err := a.modsLockedErr(packID, m.FileName); err != nil {
		return err
	}
	if backup {
		if err := a.UpdateOneModBackup(packID, m); err != nil {
			return err
		}
	}
	// Одиночне оновлення: прибираємо можливі старі файли того самого modId
	// (перейменований jar), перш ніж качати новий.
	a.removeModFilesByID(a.instanceModsDir(packID), m.ID, m.FileName)
	if err := a.installModUpdate(packID, m); err != nil {
		return err
	}
	// Видаляємо з кешу лише цей конкретний файл — решта залишається
	a.invalidateModUpdateCache(packID, m.FileName)
	return nil
}

// ── Резервні копії (відновлення) ────────────────────────────────────────
func (a *App) backupsManifestPath(packID string) string {
	return filepath.Join(a.modsBackupsDir(packID), "manifest.json")
}

func (a *App) readBackups(packID string) []model.ModBackupEntry {
	data, err := os.ReadFile(a.backupsManifestPath(packID))
	if err != nil {
		return nil
	}
	var list []model.ModBackupEntry
	if json.Unmarshal(data, &list) != nil {
		return nil
	}
	return list
}

// modSourceURL будує посилання на сторінку мода за відомим джерелом і
// слагом/id. Порожнє, якщо джерело невідоме — тоді єдиним джерелом
// відновлення лишається сама копія файлу у теці бек-апів (як і вимагає
// сценарій «немає посилання → кладемо стару копію»).
func modSourceURL(m model.ModEntry) string {
	slug := m.ProjectSlug
	if slug == "" {
		slug = m.ProjectID
	}
	if slug == "" {
		return ""
	}
	switch m.Source {
	case "modrinth":
		return "https://modrinth.com/mod/" + slug
	case "curseforge":
		return "https://www.curseforge.com/minecraft/mc-mods/" + slug
	}
	return ""
}

func (a *App) appendBackupEntry(packID string, m model.ModEntry, bak string) {
	list := a.readBackups(packID)
	list = append(list, model.ModBackupEntry{
		ID: fmt.Sprintf("bak-%d", time.Now().UnixNano()), ModID: m.ID, Name: m.Name,
		Version: m.Version, FileName: m.FileName, BackupFile: bak, Date: time.Now().Format(time.RFC3339),
		SourceURL: modSourceURL(m),
	})
	_ = atomicfile.WriteJSONAtomic(a.backupsManifestPath(packID), list)
}

func (a *App) ListModBackups(packID string) []model.ModBackupEntry {
	return a.readBackups(packID)
}

// RestoreModBackup повертає стару версію мода: видаляє поточні файли мода
// (оновлена версія могла змінити ім'я файлу) і кладе резервну копію назад.
// Після відновлення: видаляє запис резервної копії з manifest.json і
// інвалідує кеш перевірки оновлень ТІЛЬКИ для цього мода (не для решти).
func (a *App) RestoreModBackup(packID string, entry model.ModBackupEntry) error {
	if err := a.modsBusyErr(packID); err != nil {
		return err
	}
	if err := a.modsLockedErr(packID, ""); err != nil {
		return err
	}
	backupsDir := a.modsBackupsDir(packID)
	modsDir := a.instanceModsDir(packID)

	// Читаємо і кладемо файл резервної копії назад
	data, err := os.ReadFile(filepath.Join(backupsDir, entry.BackupFile))
	if err != nil {
		return fmt.Errorf("резервну копію не знайдено: %v", err)
	}
	a.removeModFilesByID(modsDir, entry.ModID, entry.FileName)
	if err := os.WriteFile(filepath.Join(modsDir, entry.FileName), data, 0644); err != nil {
		return err
	}

	// ── Баг 1: видаляємо запис з manifest після успішного відновлення ──
	// Раніше запис залишався — в UI список резервних копій не зменшувався.
	a.deleteBackupEntry(packID, entry.ID)

	// ── Баг 2: smart invalidation — тільки відновлений файл ────────────
	// Раніше invalidateModUpdateCache(packID) скидала ВСІ перевірки збірки,
	// тому після відновлення зникали всі значки «оновлення є» у решти модів.
	a.invalidateModUpdateCache(packID, entry.FileName)
	return nil
}

// deleteBackupEntry видаляє один запис резервної копії з manifest.json за ID.
func (a *App) deleteBackupEntry(packID, entryID string) {
	list := a.readBackups(packID)
	filtered := list[:0]
	for _, e := range list {
		if e.ID != entryID {
			filtered = append(filtered, e)
		}
	}
	if len(filtered) == len(list) {
		return // запис не знайдено — нічого не міняємо
	}
	_ = atomicfile.WriteJSONAtomic(a.backupsManifestPath(packID), filtered)
}

// ── Дублікати / видалення / pre-launch ──────────────────────────────────

// errGameRunningForMods — уніфіковане повідомлення, коли операція над
// файлами модів заблокована, бо збірка зараз запущена і Minecraft тримає
// jar-файли відкритими (видалення/заміна впаде з "used by another
// process" на Windows). Замість падіння з незрозумілою OS-помилкою
// просимо користувача спершу закрити гру.
func (a *App) modsBusyErr(packID string) error {
	if err := a.shareLockErr("build", packID); err != nil {
		return err // збірку зараз віддають друзям — файли мають лишатись незмінними
	}
	if s := a.sessionsM.RunningOnBuild(packID); s != nil {
		return fmt.Errorf("неможливо змінити моди: збірка запущена (акаунт %s). Закрийте гру і спробуйте ще раз", s.AccountName)
	}
	return nil
}

func (a *App) DeleteMod(packID, fileName string) error {
	if err := a.modsBusyErr(packID); err != nil {
		return err
	}
	if err := a.modsLockedErr(packID, fileName); err != nil {
		return err
	}
	err := a.modMgr.DeleteMod(a.instanceModsDir(packID), fileName)
	if err == nil {
		// Видаляємо з кешу лише цей файл — решта перевірених модів зберігає стан
		a.invalidateModUpdateCache(packID, fileName)
	}
	return err
}

// InstallSuggestedMod встановлює одну рекомендацію з блоку «Рекомендовані
// моди» (вкладка Контент) — той самий пайплайн пошуку/завантаження, що й
// автовстановлення відсутніх залежностей (ResolveDependency: Modrinth
// search за depID → найновіша версія під mcVersion/loader → файл у
// modsDir збірки). Скидає кеші оновлень і рекомендацій для збірки, щоб
// щойно встановлений мод одразу зник зі списку пропозицій.
func (a *App) InstallSuggestedMod(packID, depID, mcVersion, loader string) model.DependencyResult {
	if err := a.errPackRunning(packID); err != nil {
		return model.DependencyResult{Success: false, Message: err.Error()}
	}
	if err := a.modsLockedErr(packID, ""); err != nil {
		return model.DependencyResult{Success: false, Message: err.Error()}
	}
	res := a.modrinth.ResolveDependency(depID, mcVersion, strings.ToLower(loader), a.instanceModsDir(packID))
	if res.Success {
		// Новий мод — його FileName невідомий до завантаження, тому просто
		// залишаємо кеш як є: він не містить нового файлу, тому CheckModUpdates
		// автоматично перевірить лише його (smart invalidation у CheckModUpdates).
		// Повна інвалідація не потрібна — решта модів вже перевірена.
	}
	return res
}

// findDuplicateGroups — групи дублікатів за modId для ВЖЕ відсканованого
// списку модів (без повторного сканування jar-файлів). Швидке виявлення:
// той самий мод (з різними назвами файлів або версіями) встановлений
// двічі. Хешування jar НЕ виконуємо — воно читає гігабайти і гальмувало
// б запуск гри; modId унікальний у межах лоадера, тож цього достатньо для
// анти-дублікат системи.
func findDuplicateGroups(mods []model.ModEntry) []model.DuplicateModGroup {
	byID := map[string][]model.ModEntry{}
	for _, m := range mods {
		if m.ID == "" {
			continue
		}
		byID[m.ID] = append(byID[m.ID], m)
	}
	var out []model.DuplicateModGroup
	for id, group := range byID {
		if len(group) > 1 {
			out = append(out, model.DuplicateModGroup{Sha1: id, Mods: group})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Sha1 < out[j].Sha1 })
	return out
}

func (a *App) FindDuplicateMods(packID string) []model.DuplicateModGroup {
	return findDuplicateGroups(a.ScanModsFull(packID))
}

// CheckPreLaunchModIssues — перевірка дублікатів модів (без перевірки залежностей).
// Фронтенд показує модалку зі швидкими діями перед запуском гри.
// Сканування jar-файлів виконується РІВНО ОДИН РАЗ: його результат
// перевикористовується для перевірки дублікатів. Раніше
// FindDuplicateMods робила повний скан окремо, а потім ще один скан робився
// тут — на великих збірках кожен клік «Грати» парсив усі jar двічі, і
// кнопка без будь-якого індикатора «зависала» на час перевірки.
//
// ВАЖЛИВО (2026-08-17): використовуємо ЛЕГКИЙ скан (ListMods), а не
// ScanModsFull. Дублікати визначаються за modId, який читається з кешу
// метаданих jar — SHA1 тут НЕ потрібен. ScanModsFull рахує SHA1 КОЖНОГО
// jar-файла (перший запуск сесії на великій збірці — гігабайти читання,
// секунди очікування), а ця функція стоїть на гарячому шляху кліку
// «Грати» (і в RunPackHealthCheck, і в pre-launch перевірці).
func (a *App) CheckPreLaunchModIssues(packID string) model.PreLaunchModIssues {
	var out model.PreLaunchModIssues
	mods, _ := a.modMgr.ListMods(a.instanceModsDir(packID))
	if mods == nil {
		mods = []model.ModEntry{}
	}
	out.Duplicates = findDuplicateGroups(mods)
	// SHAURMA: перевірка залежностей вимкнена для шаурма-збірок
	// a.attachMissingDeps(mods)
	// seen := map[string]bool{}
	// for _, m := range mods {
	// 	for _, d := range m.MissingDeps {
	// 		if !seen[d] {
	// 			seen[d] = true
	// 			out.MissingDeps = append(out.MissingDeps, d)
	// 		}
	// 	}
	// }
	// Заблоковані моди (автор заборонив завантаження): показуємо попередження,
	// але НЕ блокуємо запуск — користувач може скачати їх вручну пізніше.
	if cp, ok := a.customPacks.Get(packID); ok {
		for _, bm := range cp.BlockedMods {
			if !bm.Downloaded {
				out.BlockedMods = append(out.BlockedMods, bm.FileName)
			}
		}
	}
	// ВАЖЛИВО: гарантуємо НЕ-nil слайси назовні.
	if out.Duplicates == nil {
		out.Duplicates = []model.DuplicateModGroup{}
	}
	out.MissingDeps = []string{} // Завжди порожній для шаурма-збірок
	if out.BlockedMods == nil {
		out.BlockedMods = []string{}
	}
	out.HasIssues = len(out.Duplicates) > 0 || len(out.BlockedMods) > 0
	return out
}

// ── Сервісні хелпери ────────────────────────────────────────────────────
// downloadModFile качає файл з ретраями і записує у dest (без атомарного
// rename — викликач перейменовує після успіху).
func (a *App) downloadModFile(url, dest string) error {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", "shaurma-team/shaurma-launcher/2.0 (shaurmaofficial2210@gmail.com)")
		// CurseForge CDN (edge.forgecdn.net) вимагає x-api-key на прямих
		// завантаженнях файлів — без нього запити повертають 401
		// Unauthorized (обов'язкова автентифікація для CDN-завантажень).
		// Modrinth-файли якаються з cdn.modrinth.com і ключа не потребують,
		// тож заголовок додаємо ЛИШЕ для CurseForge CDN.
		if custompack.IsCurseForgeCDNURL(url) && custompack.HasCurseForgeAPIKey() {
			req.Header.Set("x-api-key", custompack.CurseForgeAPIKeyValue())
		}
		client := &http.Client{Timeout: 5 * time.Minute}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(time.Duration(attempt+1) * 500 * time.Millisecond)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			resp.Body.Close()
			time.Sleep(time.Duration(attempt+1) * 500 * time.Millisecond)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			resp.Body.Close()
			return err
		}
		out, err := os.Create(dest)
		if err != nil {
			resp.Body.Close()
			return err
		}
		_, copyErr := io.Copy(out, resp.Body)
		out.Close()
		resp.Body.Close()
		if copyErr == nil {
			return nil
		}
		os.Remove(dest)
		lastErr = copyErr
		time.Sleep(time.Duration(attempt+1) * 500 * time.Millisecond)
	}
	return fmt.Errorf("не вдалось завантажити: %v", lastErr)
}

func urlLastSegment(rawURL string) string {
	u := rawURL
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		u = u[:i]
	}
	if i := strings.LastIndexAny(u, "/\\"); i >= 0 {
		u = u[i+1:]
	}
	return u
}

func sanitizeFileName(s string) string {
	var b strings.Builder
	for _, r := range s {
		// Раніше дозволяли ЛИШЕ ASCII (a-z0-9-_.): українська назва збірки
		// на кшталт "Моя збірка" перетворювалась на порожній рядок — і
		// .lnk у меню «Пуск» називався просто ".lnk" (баг «там пише .lnk,
		// а не назва ярлика»). Windows/NTFS повністю підтримують Unicode в
		// іменах файлів — лишаємо літери/цифри будь-якої мови, а розділювачі
		// і заборонені символи замінюємо на '-'
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	res := strings.Trim(b.String(), "-.")
	// Назва, що складається ЛИШЕ з розділювачів/заборонених символів,
	// після trim стає порожньою — тоді .lnk називався б просто ".lnk".
	// Фолбек на "pack" дає осмислене ім'я (викликач може підставити
	// packID поверх, якщо хоче точніше).
	if res == "" {
		return "pack"
	}
	return res
}
