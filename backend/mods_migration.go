package main

import (
	"shaurma-launcher-backend/internal/crashguard"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"shaurma-launcher-backend/internal/api"
	"shaurma-launcher-backend/internal/model"
)

// ── Міграція модів між збірками ──────────────────────────────────────────
// Той самий сценарій, що ModMigrationScreen у старому лаунчері: вибираєш
// збірку-джерело і цільову збірку → «Аналіз» шукає для КОЖНОГО мода
// джерела сумісну версію для цільової (MC-версія + лоадер) на Modrinth,
// фолбек CurseForge → звіт (можна перенести / не знайдено / вже
// встановлено / треба оновити) → «Мігрувати» качає моди в цільову збірку.
//
// ОПТИМІЗАЦІЯ МЕРЕЖІ (500 модів без 500 запитів): аналіз не ходить в API
// по одному запиту на мод. Замість цього:
//   1) batch-resolve sha1 → POST /version_files (1 запит на 200 хешів);
//   2) batch-інфо проєктів → GET /projects?ids= (1 запит на 50 id) —
//      іконки/слаги всіх знайдених модів парою запитів;
//   3) пошук сумісної версії для цілі → лише для проєктів, чия знайдена
//      за sha1 версія НЕ підходить під цільову MC/лоадер (меншість);
//   4) пошук за назвою та CurseForge — ЛИШЕ для модів, яких немає за sha1.
// Плюс кеш проєктів (projectCache) і загальний кеш оновлень — наступні
// аналізи тієї ж сесії дешевші.

// GetMigrationPacks повертає список усіх збірок (Shaurma-каталог + кастомні),
// які ФІЗИЧНО встановлені і мають теки модів з .jar — для селекторів
// «Звідки/Куди» в діалозі міграції. excludePackID виключає поточну збірку
// зі списку джерел (мігрувати саму в себе безглуздо).
func (a *App) GetMigrationPacks(excludePackID string) []model.MigrationPack {
	out := []model.MigrationPack{}
	seen := map[string]bool{}
	add := func(id, name, mcVersion, loader, iconURL, icon, color string) {
		if id == "" || seen[id] || id == excludePackID {
			return
		}
		seen[id] = true
		count := a.countEnabledJars(id)
		out = append(out, model.MigrationPack{
			PackID: id, Name: name, MCVersion: mcVersion, Loader: loader,
			IconURL: iconURL, Icon: icon, Color: color, ModCount: count,
		})
	}

	// Shaurma-збірки з індексу (тільки встановлені — їх теки на диску).
	// Пресет-іконки в них немає (сервер віддає лише PNG у IconURL).
	if a.packs != nil {
		if list, err := a.packs.GetPackIndex(); err == nil {
			for _, e := range list {
				if _, inst := a.buildsReg.Get(e.ID); inst {
					add(e.ID, e.Name, e.MCVersion, e.LoaderType, e.IconURL, "", e.Color)
				}
			}
		}
	}
	// Кастомні збірки: іконка/колір зі збереженого Pack (ті самі, що на
	// картці збірки в sidebar) — раніше тут був порожній рядок, тож
	// селектор міграції показував кастомні збірки БЕЗ іконки і кольору.
	// Icon — пресет-іконка (Tabler), IconPath (PNG) має пріоритет у UI.
	if a.customPacks != nil {
		for _, cp := range a.customPacks.List() {
			iconURL := ""
			if cp.IconPath != "" {
				iconURL = a.packAssetURL(cp, cp.IconPath)
			}
			add(cp.ID, cp.Name, cp.MCVersion, cp.Loader, iconURL, cp.Icon, cp.Color)
		}
	}
	// Порядок — ТАКИЙ САМИЙ, як у sidebar: закріплені перші (packLayout.pinned
	// у їхньому порядку), далі незакріплені у збереженому порядку (packLayout.
	// order), нові — в кінці (як прийшли). Раніше тут було сортування за
	// назвою, і список міграції не збігався з порядком збірок у лаунчері.
	order := a.packLayoutOrder()
	pinned := a.packLayoutPinned()
	byID := map[string]model.MigrationPack{}
	for _, p := range out {
		byID[p.PackID] = p
	}
	pinnedSet := map[string]bool{}
	for _, id := range pinned {
		pinnedSet[id] = true
	}
	sorted := make([]model.MigrationPack, 0, len(out))
	for _, id := range pinned {
		if p, ok := byID[id]; ok {
			sorted = append(sorted, p)
		}
	}
	for _, id := range order {
		if pinnedSet[id] {
			continue
		}
		if p, ok := byID[id]; ok {
			sorted = append(sorted, p)
		}
	}
	for _, p := range out {
		if pinnedSet[p.PackID] {
			continue
		}
		known := false
		for _, id := range order {
			if id == p.PackID {
				known = true
				break
			}
		}
		if !known {
			sorted = append(sorted, p)
		}
	}
	// Страховка на випадок дублювання (одна збірка у кількох списках).
	if len(sorted) != len(out) {
		fallback := append([]model.MigrationPack{}, out...)
		sort.Slice(fallback, func(i, j int) bool {
			return strings.ToLower(fallback[i].Name) < strings.ToLower(fallback[j].Name)
		})
		return fallback
	}
	return sorted
}

// packLayoutOrder повертає збережений порядок збірок (settings.PackOrder).
func (a *App) packLayoutOrder() []string {
	s := a.cfg.GetSettings()
	if s.PackOrder == nil {
		return nil
	}
	return s.PackOrder
}

// packLayoutPinned повертає закріплені збірки (settings.PackPinned).
func (a *App) packLayoutPinned() []string {
	s := a.cfg.GetSettings()
	if s.PackPinned == nil {
		return nil
	}
	return s.PackPinned
}

// countEnabledJars — кількість увімкнених .jar у теці модів збірки (для
// підказки «можна перенести N модів» у селекторі джерела). Простий
// ReadDir без парсингу jar — це лише підказка, не аналіз.
func (a *App) countEnabledJars(packID string) int {
	dir := a.instanceModsDir(packID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, ".jar") && !strings.HasSuffix(name, ".disabled") {
			n++
		}
	}
	return n
}

// migrationStatus — статуси звіту міграції.
const (
	migrationStatusFound    = "found"     // знайдено сумісну версію — можна перенести
	migrationStatusNotFound = "not-found" // не знайдено сумісної версії
	migrationStatusAlready  = "already"   // та сама версія вже встановлена в цілі
	migrationStatusUpdate   = "update"    // встановлено, але знайдено новішу — буде оновлено
)

// AnalyzeModMigration будує звіт міграції модів зі збірки-джерела в
// цільову. targetMCVersion/targetLoader — версія гри та лоадер ЦІЛЬОВОЇ
// збірки (саме під них шукаються версії модів). targetPackID може бути
// порожнім (сценарій «створюю збірку» — цілі ще немає, тоді статусів
// already/update просто не буде).
func (a *App) AnalyzeModMigration(sourcePackID, targetPackID, targetMCVersion, targetLoader string) (model.ModMigrationReport, error) {
	var report model.ModMigrationReport
	srcMods := a.scanMigrationSource(sourcePackID)
	if len(srcMods) == 0 {
		report.Items = []model.MigrationItem{}
		report.TargetMCVersion = targetMCVersion
		report.TargetLoader = targetLoader
		if srcPack, ok := a.migrationPackByID(sourcePackID); ok {
			report.SourcePackName = srcPack.Name
		}
		return report, nil
	}

	// Скасування аналізу: контекст реєструється за targetPackID ('' для
	// міні-картки створення збірки). CancelModMigrationAnalysis викликає
	// cancel — горутини перестають обробляти нові моди, і метод повертає
	// частковий звіт (report.Cancelled=true), а не падає з помилкою.
	ctx, cancel := context.WithCancel(context.Background())
	a.migrateAnalyzeMu.Lock()
	if a.migrateAnalyzeCancel == nil {
		a.migrateAnalyzeCancel = map[string]context.CancelFunc{}
	}
	a.migrateAnalyzeCancel[targetPackID] = cancel
	a.migrateAnalyzeMu.Unlock()
	defer func() {
		a.migrateAnalyzeMu.Lock()
		delete(a.migrateAnalyzeCancel, targetPackID)
		a.migrateAnalyzeMu.Unlock()
		cancel()
	}()

	report.TargetMCVersion = targetMCVersion
	report.TargetLoader = targetLoader
	if srcPack, ok := a.migrationPackByID(sourcePackID); ok {
		report.SourcePackName = srcPack.Name
	}

	// Моди цілі: sha1-множина (already) + modId-множина (update).
	targetSha1s := map[string]bool{}
	targetModIDs := map[string]bool{}
	if targetPackID != "" {
		if tmods, err := a.modMgr.ScanModsFull(a.instanceModsDir(targetPackID)); err == nil {
			for i := range tmods {
				if tmods[i].Sha1 != "" {
					targetSha1s[strings.ToLower(tmods[i].Sha1)] = true
				}
				if tmods[i].ID != "" {
					targetModIDs[tmods[i].ID] = true
				}
			}
		}
	}

	// ── Короткий шлях: джерело і ціль на одному loader+MC-версії ───────
	// Якщо loader і MC-версія збірки-джерела збігаються з цільовими,
	// мережевий резолв (Modrinth batch + CurseForge fallback) не дає
	// нічого нового: jar вже доведено робочий саме під цю пару
	// loader+версія в джерелі, і саме туди ж і переноситься. Раніше
	// такі моди, якщо їх не було на Modrinth/CurseForge (приватні,
	// unlisted, самописні), падали в "not-found" і взагалі не
	// потрапляли в переліст, хоча насправді jar можна просто
	// скопіювати як є. Перевірку already/update (по sha1/modId з тим,
	// що вже стоїть у цілі) лишаємо — вона не залежить від мережі.
	if srcPack, ok := a.migrationPackByID(sourcePackID); ok &&
		srcPack.Loader != "" && targetLoader != "" &&
		strings.EqualFold(srcPack.Loader, targetLoader) &&
		srcPack.MCVersion != "" && srcPack.MCVersion == targetMCVersion {

		items := make([]model.MigrationItem, 0, len(srcMods))
		for _, m := range srcMods {
			item := model.MigrationItem{
				SourceName:       m.Name,
				SourceFileName:   m.FileName,
				SourceSha1:       m.Sha1,
				SourceModID:      m.ID,
				ResolvedName:     m.Name,
				ResolvedFilename: m.FileName,
				LocalCopy:        true,
				SourcePackID:     sourcePackID,
			}
			switch {
			case m.Sha1 != "" && targetSha1s[strings.ToLower(m.Sha1)]:
				item.Status = migrationStatusAlready
			case m.ID != "" && targetModIDs[m.ID]:
				item.Status = migrationStatusUpdate
				item.WillReplace = true
			default:
				item.Status = migrationStatusFound
			}
			items = append(items, item)
		}
		sort.SliceStable(items, func(i, j int) bool {
			return migrationStatusOrder(items[i].Status) < migrationStatusOrder(items[j].Status)
		})
		report.Items = items
		for _, it := range items {
			switch it.Status {
			case migrationStatusFound:
				report.Found++
			case migrationStatusAlready:
				report.Already++
			case migrationStatusUpdate:
				report.NeedsUpdate++
			}
		}
		return report, nil
	}

	// ── Крок 2: batch-resolve sha1 (головна оптимізація) ──────────────
	var hashes []string
	for _, m := range srcMods {
		if m.Sha1 != "" {
			hashes = append(hashes, strings.ToLower(m.Sha1))
		}
	}
	// ВАЖЛИВО: помилку batch-запиту НЕ ігноруємо. Якщо її з'їсти мовчки,
	// КОЖЕН мод провалився б у пошук за назвою/CurseForge — тобто замість
	// 1 запиту ми б зробили до 500+, рівно те навантаження, від якого
	// пакетна оптимізація створена. При збої мережі чесніше провалити
	// аналіз зі зрозумілою помилкою, ніж бити API сотнями поодиноких.
	versionsByHash, batchErr := a.modrinth.GetVersionsByHashes(hashes)
	if batchErr != nil {
		return report, fmt.Errorf("Modrinth batch resolve: %w", batchErr)
	}

	// ── Крок 3: batch-інфо проєктів для знайдених ─────────────────────
	projIDs := []string{}
	seenProj := map[string]bool{}
	for _, v := range versionsByHash {
		if v.ProjectID != "" && !seenProj[v.ProjectID] {
			seenProj[v.ProjectID] = true
			projIDs = append(projIDs, v.ProjectID)
		}
	}
	projects, _ := a.modrinth.GetProjectsBatch(projIDs)

	// ── Кроки 4-5: резолв + статуси (з обмеженим паралелізмом для
	// пошуку за назвою/CF — туди потрапляють лише незнайдені за sha1) ──
	items := make([]model.MigrationItem, 0, len(srcMods))
	var mu sync.Mutex
	sem := make(chan struct{}, 6)
	var wg sync.WaitGroup

	// Живий прогрес аналізу: загальна кількість одразу (фронтенд показує
	// «0 / N» ще до першого рядка), далі — по одній події на кожен мод.
	total := len(srcMods)
	done := 0
	a.emit("mods:migrate-analyze", map[string]any{
		"packId": targetPackID,
		"total":  total,
		"phase":  "start",
	})

	for i := range srcMods {
		m := &srcMods[i]
		wg.Add(1)
		go func() {
			defer crashguard.Recover("mods_migration.go:339")
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			// Скасування: моди, що ще не почали оброблятись, пропускаємо.
			select {
			case <-ctx.Done():
				return
			default:
			}

			item := model.MigrationItem{
				SourceName:     m.Name,
				SourceFileName: m.FileName,
				SourceSha1:     m.Sha1,
				SourceModID:    m.ID,
			}

			// 1) Точний збіг за sha1 (batch уже зробив роботу).
			if v, ok := versionsByHash[strings.ToLower(m.Sha1)]; ok {
				a.fillResolvedFromFull(&item, v, projects)
				// Знайдена версія може бути несумісна з ціллю (напр. інша MC
				// або інший лоадер) — тоді шукаємо сумісну саме для цілі.
				// Це єдиний per-project запит, і лише для несумісних.
				if !fullVersionCompatible(v, targetMCVersion, targetLoader) {
					if opt := a.modrinthLatest(item.ProjectID, targetMCVersion, targetLoader); opt != nil {
						item.ResolvedVersion = opt.VersionNumber
						item.ResolvedFilename = safeModFileName(opt.Filename)
						item.ResolvedURL = opt.URL
						item.ResolvedSha1 = opt.Sha1
					} else {
						// Сумісної версії для цілі НЕ знайшлось — обов'язково
						// чистимо дані несумісної версії: інакше status-логіка
						// побачила б непустий ResolvedURL і позначила б мод
						// «знайденим» з версією під ІНШУ MC/лоадер.
						item.ResolvedVersion = ""
						item.ResolvedFilename = ""
						item.ResolvedURL = ""
						item.ResolvedSha1 = ""
					}
				}
			} else {
				// 2) Пошук за назвою на Modrinth, фолбек CurseForge (лише
				// для незнайдених за sha1 — меншість у типових випадках).
				a.resolveMigrationByName(&item, m, targetMCVersion, targetLoader)
			}

			// Ще одна перевірка після резолву (потенційні мережеві запити):
			// якщо користувач натиснув «Скасувати», поки ми шукали сумісну
			// версію — результат цього мода не додаємо у звіт.
			select {
			case <-ctx.Done():
				return
			default:
			}

			if item.ProjectID == "" || item.ResolvedURL == "" {
				item.Status = migrationStatusNotFound
			} else if item.ResolvedSha1 != "" && targetSha1s[strings.ToLower(item.ResolvedSha1)] {
				item.Status = migrationStatusAlready
			} else if m.ID != "" && targetModIDs[m.ID] {
				item.Status = migrationStatusUpdate
				item.WillReplace = true
			} else {
				item.Status = migrationStatusFound
				// Файл з такою ж назвою вже лежить у цілі — буде замінений.
				if targetPackID != "" && item.ResolvedFilename != "" {
					if _, err := os.Stat(filepath.Join(a.instanceModsDir(targetPackID), item.ResolvedFilename)); err == nil {
						item.WillReplace = true
					}
				}
			}

			mu.Lock()
			items = append(items, item)
			done++
			cur, tot := done, total
			name, fileName, st := item.SourceName, item.SourceFileName, item.Status
			iconURL, src, resolvedName := item.IconURL, item.Source, item.ResolvedName
			mu.Unlock()

			// Подія «мод проаналізовано»: рядок звіту з'являється у діалозі
			// одразу після визначення статусу (а не після завершення всього
			// аналізу). Порядок подій може відрізнятись від порядку модів
			// (паралельний резолв) — це ок: UI оновлює рядки по sourceFileName.
			// IconURL/Source/ResolvedName — з batch-проєктів: іконка видна у
			// живому рядку одразу, а не лише у фінальному звіті.
			a.emit("mods:migrate-analyze", map[string]any{
				"packId":         targetPackID,
				"sourceName":     name,
				"sourceFileName": fileName,
				"status":         st,
				"current":        cur,
				"total":          tot,
				"phase":          "item",
				"iconUrl":        iconURL,
				"source":         src,
				"resolvedName":   resolvedName,
			})
		}()
	}
	wg.Wait()

	// Аналіз перервано кнопкою «Скасувати» — повертаємо частковий звіт
	// (те, що встигло розібратись), щоб UI показав результат без закриття
	// діалогу.
	report.Cancelled = ctx.Err() != nil

	// Звіт: спершу ті, що можна перенести (found/update), потім already,
	// потім not-found — як ModMigrationScreen (знайдені зверху).
	sort.SliceStable(items, func(i, j int) bool {
		return migrationStatusOrder(items[i].Status) < migrationStatusOrder(items[j].Status)
	})
	report.Items = items
	for _, it := range items {
		switch it.Status {
		case migrationStatusFound:
			report.Found++
		case migrationStatusNotFound:
			report.NotFound++
		case migrationStatusAlready:
			report.Already++
		case migrationStatusUpdate:
			report.NeedsUpdate++
		}
	}
	if report.Items == nil {
		report.Items = []model.MigrationItem{}
	}
	return report, nil
}

// CancelModMigrationAnalysis зупиняє активний аналіз міграції для вказаної
// цільової збірки (” — для міні-картки створення збірки). AnalyzeModMigration
// повертає частковий звіт з Cancelled=true — діалог не закривається,
// користувач бачить, що встигло розібратись.
func (a *App) CancelModMigrationAnalysis(packID string) {
	a.migrateAnalyzeMu.Lock()
	cancel := a.migrateAnalyzeCancel[packID]
	a.migrateAnalyzeMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// migrationStatusOrder — порядок сортування рядків звіту (знайдені зверху).
func migrationStatusOrder(s string) int {
	switch s {
	case migrationStatusFound:
		return 0
	case migrationStatusUpdate:
		return 1
	case migrationStatusAlready:
		return 2
	default:
		return 3
	}
}

// fullVersionCompatible — чи версія проєкту підходить під цільову MC і
// лоадер. Порожні списки game_versions/loaders = невідомо — не блокуємо.
// Завантажувальна сумісність: потрібен перетин хоча б з одним лоадером
// (мультизавантажувальні jar-и).
func fullVersionCompatible(v *api.FullVersion, mcVersion, loader string) bool {
	if v == nil {
		return false
	}
	// Лоадер збірки може бути "vanilla" (модів там немає взагалі) —
	// у такому разі не фільтруємо.
	if loader == "" || strings.EqualFold(loader, "vanilla") {
		// все одно перевіряємо MC
	} else if len(v.Loaders) > 0 {
		ok := false
		for _, l := range v.Loaders {
			if strings.EqualFold(l, loader) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	if mcVersion != "" && len(v.GameVersions) > 0 {
		for _, gv := range v.GameVersions {
			if gv == mcVersion {
				return true
			}
		}
		return false
	}
	return true
}

// fillResolvedFromFull заповнює item зі знайденої за sha1 версії + batch-інфо проєкту.
func (a *App) fillResolvedFromFull(item *model.MigrationItem, v *api.FullVersion, projects map[string]*api.ModrinthProjectInfo) {
	if v == nil || v.ProjectID == "" {
		return
	}
	item.ProjectID = v.ProjectID
	item.ResolvedVersion = v.VersionNumber
	item.Source = "modrinth"
	if p, ok := projects[v.ProjectID]; ok {
		item.ProjectSlug = p.Slug
		item.ResolvedName = p.Title
		if p.IconURL != "" {
			item.IconURL = p.IconURL
		}
	}
	// FullVersion не несе назви проєкту — підставляємо з кешу/batch-інфо;
	// якщо проєкт не прийшов, лишаємо назву джерела як фолбек.
	if item.ResolvedName == "" {
		item.ResolvedName = v.VersionNumber
	}
	// Primary-файл версії (той самий файл, який і був у джерелі — бо
	// знайшли саме цю версію за sha1 джерела).
	for _, f := range v.Files {
		if f.Primary || item.ResolvedURL == "" {
			item.ResolvedFilename = safeModFileName(f.Filename)
			item.ResolvedURL = f.URL
			item.ResolvedSha1 = f.Sha1
		}
		if f.Primary {
			break
		}
	}
}

// resolveMigrationByName шукає мод за назвою (Modrinth → CurseForge фолбек)
// і заповнює item найсвіжішою сумісною версією. Викликається ЛИШЕ для
// модів, не знайдених за sha1 — щоб не плодити запити.
func (a *App) resolveMigrationByName(item *model.MigrationItem, m *model.ModEntry, mcVersion, loader string) {
	if m.Name == "" {
		return
	}
	if projID, ok := a.findModOnModrinth(m, mcVersion, loader); ok {
		item.ProjectID = projID
		item.Source = "modrinth"
		if p, err := a.modrinth.GetProject(projID); err == nil && p != nil {
			item.ProjectSlug = p.Slug
			item.IconURL = p.IconURL
			item.ResolvedName = p.Title
		}
		if opt := a.modrinthLatest(projID, mcVersion, loader); opt != nil {
			item.ResolvedVersion = opt.VersionNumber
			item.ResolvedFilename = safeModFileName(opt.Filename)
			item.ResolvedURL = opt.URL
			item.ResolvedSha1 = opt.Sha1
		}
		return
	}
	if cf, err := a.findModOnCurseForge(m, mcVersion, loader); err == nil && cf != nil {
		item.ProjectID = fmt.Sprintf("%d", cf.ID)
		item.Source = "curseforge"
		item.ProjectSlug = cf.Slug
		item.IconURL = cf.LogoURL
		item.ResolvedName = cf.Name
		item.ResolvedVersion = cf.FileName
		item.ResolvedFilename = safeModFileName(cf.FileName)
		item.ResolvedURL = cf.DownloadURL
	}
}

// scanMigrationSource сканує теки модів джерела (увімкнені jar).
func (a *App) scanMigrationSource(packID string) []model.ModEntry {
	mods, err := a.modMgr.ScanModsFull(a.instanceModsDir(packID))
	if err != nil {
		return nil
	}
	out := make([]model.ModEntry, 0, len(mods))
	for _, m := range mods {
		if !m.Enabled {
			continue
		}
		out = append(out, m)
	}
	return out
}

// migrationPackByID повертає назву збірки за ID (для звіту).
func (a *App) migrationPackByID(packID string) (model.MigrationPack, bool) {
	for _, p := range a.GetMigrationPacks("") {
		if p.PackID == packID {
			return p, true
		}
	}
	return model.MigrationPack{}, false
}

// MigrateMods виконує міграцію СИНХРОННО (блокуючий виклик — для діалогу
// міграції у менеджері модів, який показує спінер на кнопці). Прогрес
// шлеться подією mods:migrate-progress. Для сценарію «створення збірки»
// використовуй StartModMigration (асинхронний — UI переходить на сторінку
// збірки одразу і дивиться прогрес живою стрічкою).
func (a *App) MigrateMods(targetPackID string, items []model.MigrationItem) model.ModMigrationResult {
	return a.migrateModsInternal(targetPackID, items)
}

// StartModMigration запускає міграцію модів АСИНХРОННО (сценарій
// «створення збірки»): метод повертається одразу, а сам перенос іде у
// горутині. Прогрес шлеться подією mods:migrate-progress — тепер з назвою
// та іконкою поточного мода (їх показує і картка збірки, і сторінка), а
// завершення — подією mods:migrate-done з підсумком. Це дозволяє UI
// перейти на сторінку щойно створеної збірки одразу і показати живий
// прогрес міграції замість тихого блокуючого виклику.
func (a *App) StartModMigration(targetPackID string, items []model.MigrationItem) string {
	go func() {
		defer crashguard.Recover("mods_migration.go:646")
		res := a.migrateModsInternal(targetPackID, items)
		// total обов'язково передається: фронтенд використовує його, щоб
		// зняти прогрес-банер з картки. Якщо міграція впала на самому старті
		// (жодного progress-івента не було) — total=0, і фронтенд має
		// надійний маркер, що прогрес треба прибрати (не зависати назавжди).
		// Обчислюємо як фактичну кількість опрацьованих файлів + пропущені.
		total := len(res.Migrated) + len(res.Failed) + res.Skipped
		a.emit("mods:migrate-done", map[string]any{
			"packId":   targetPackID,
			"migrated": res.Migrated,
			"failed":   res.Failed,
			"skipped":  res.Skipped,
			"total":    total,
			"hasError": len(res.Failed) > 0,
		})
	}()
	return "started"
}

// migrateModsInternal — тіло міграції (спільне для MigrateMods і
// StartModMigration): качає обрані моди (статус found/update) зі звіту
// аналізу в теки модів цільової збірки. items — рядки звіту, які
// користувач підтвердив (уже з резолвнутими URL). Повертає підсумок:
// скільки перенесено/провалено.
func (a *App) migrateModsInternal(targetPackID string, items []model.MigrationItem) model.ModMigrationResult {
	var res model.ModMigrationResult
	targetDir := a.instanceModsDir(targetPackID)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		res.Failed = append(res.Failed, fmt.Sprintf("не вдалось створити теку: %v", err))
		return res
	}

	// Вже встановлені (already) пропускаємо одразу. LocalCopy-рядки не
	// мають ResolvedURL (файл береться з диска джерела, а не мережею),
	// тому порожній URL сам по собі більше не відсіює рядок — важливо
	// саме LocalCopy, а не "found без URL" (це б означало биту аналітику).
	var todo []model.MigrationItem
	for _, it := range items {
		if it.Status == migrationStatusAlready {
			continue
		}
		if !it.LocalCopy && it.ResolvedURL == "" {
			continue
		}
		if it.Status == migrationStatusFound || it.Status == migrationStatusUpdate {
			todo = append(todo, it)
		}
	}

	// Резервна копія замінених файлів (та сама mods-backup-структура, що
	// для оновлень — RestoreModBackup працює з тим самим manifest.json).
	backupsDir := a.modsBackupsDir(targetPackID)
	stamp := ""
	if len(todo) > 0 {
		stamp = "migration-" + time.Now().Format("20060102-150405")
		os.MkdirAll(filepath.Join(backupsDir, stamp), 0755)
	}

	res.Skipped = len(items) - len(todo)
	for i, it := range todo {
		// Подія "старту" файлу: current=i (ще НЕ i+1) — показує, який файл
		// зараз качається, але НЕ рахує його завершеним. Раніше тут стояло
		// i+1, тож на останньому елементі progress=total одразу, а прогрес-бар
		// стрибав на 100% ще до того, як файл реально докачався/встановився —
		// виглядало як «зависання на 100%», хоча процес просто ще працював.
		a.emit("mods:migrate-progress", map[string]any{
			"packId":     targetPackID,
			"current":    i,
			"total":      len(todo),
			"fileName":   it.ResolvedFilename,
			"name":       firstNonEmpty(it.ResolvedName, it.SourceName),
			"iconUrl":    it.IconURL,
			"status":     it.Status,
			"sourceName": it.SourceName,
		})
		installErr := a.installMigratedMod(targetDir, backupsDir, stamp, it)
		if installErr != nil {
			res.Failed = append(res.Failed, fmt.Sprintf("%s: %v", it.SourceName, installErr))
		} else {
			// Якщо була зроблена резервна копія заміненого файлу — реєструємо її
			// в manifest.json, щоб вона з'явилась у розділі «Резервні копії» і
			// її можна було відновити (як після «Оновити всі»).
			if stamp != "" && it.WillReplace {
				if _, err := os.Stat(filepath.Join(backupsDir, stamp, it.ResolvedFilename)); err == nil {
					a.appendBackupEntry(targetPackID, model.ModEntry{
						ID: it.SourceModID, Name: it.SourceName, Version: it.ResolvedVersion, FileName: it.ResolvedFilename,
					}, filepath.Join(stamp, it.ResolvedFilename))
				}
			}
			res.Migrated = append(res.Migrated, it.SourceName)
		}
		// Подія "завершення" файлу: current=i+1 — прогрес і 100% з'являються
		// лише ПІСЛЯ того, як файл реально встановлено (або провалився), а не
		// перед цим. На останньому елементі total тепер досягається саме тут.
		a.emit("mods:migrate-progress", map[string]any{
			"packId":     targetPackID,
			"current":    i + 1,
			"total":      len(todo),
			"fileName":   it.ResolvedFilename,
			"name":       firstNonEmpty(it.ResolvedName, it.SourceName),
			"iconUrl":    it.IconURL,
			"status":     it.Status,
			"sourceName": it.SourceName,
		})
	}
	a.invalidateModUpdateCache(targetPackID)
	if res.Migrated == nil {
		res.Migrated = []string{}
	}
	if res.Failed == nil {
		res.Failed = []string{}
	}
	return res
}

// installMigratedMod качає мод у цільову збірку, попередньо прибравши
// старі файли того самого мода (за modId/назвою файлу), як ModMigrationScreen.
func (a *App) installMigratedMod(targetDir, backupsDir, stamp string, it model.MigrationItem) error {
	// Резервна копія старого файлу (якщо замінюємо).
	if stamp != "" && it.WillReplace && it.ResolvedFilename != "" {
		if old, err := os.ReadFile(filepath.Join(targetDir, it.ResolvedFilename)); err == nil {
			os.WriteFile(filepath.Join(backupsDir, stamp, it.ResolvedFilename), old, 0644)
		}
	}

	// Прибираємо старі версії цього мода: за modId з jar-метаданих (надійний
	// ключ з диска — той самий мод іншої версії міг мати інше ім'я файлу),
	// а якщо modId порожній — за projectID (менш надійно, але хоч щось).
	// Раніше матчили лише по ProjectID, який з'являється тільки після
	// мережевого резолву — на свіжому скані цілі він порожній, і старі
	// версії мода з іншими іменами лишалися б → дублікати.
	a.removeModFilesForMigration(targetDir, it.SourceModID, it.ProjectID)
	if it.ResolvedFilename != "" {
		_ = os.Remove(filepath.Join(targetDir, it.ResolvedFilename))
		_ = os.Remove(filepath.Join(targetDir, it.ResolvedFilename+".disabled"))
	}

	tmp := filepath.Join(targetDir, "."+it.ResolvedFilename+".part")

	// LocalCopy: джерело і ціль на однаковому loader+MC-версії — jar
	// копіюється з диска джерела, без мережевого запиту й без ResolvedURL
	// (могло взагалі не бути на Modrinth/CurseForge — приватний/unlisted
	// мод, самописний мод тощо).
	if it.LocalCopy {
		if it.SourcePackID == "" || it.SourceFileName == "" {
			return fmt.Errorf("немає джерела для локального копіювання")
		}
		srcPath := filepath.Join(a.instanceModsDir(it.SourcePackID), it.SourceFileName)
		src, err := os.Open(srcPath)
		if err != nil {
			return fmt.Errorf("не вдалося відкрити файл джерела: %w", err)
		}
		defer src.Close()
		dst, err := os.Create(tmp)
		if err != nil {
			return fmt.Errorf("не вдалося створити файл цілі: %w", err)
		}
		if _, err := io.Copy(dst, src); err != nil {
			dst.Close()
			os.Remove(tmp)
			return fmt.Errorf("копіювання не вдалося: %w", err)
		}
		if err := dst.Close(); err != nil {
			os.Remove(tmp)
			return err
		}
		return os.Rename(tmp, filepath.Join(targetDir, it.ResolvedFilename))
	}

	// Качаємо в .part, потім перейменовуємо (атомарно).
	if err := a.downloadModFile(it.ResolvedURL, tmp); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, filepath.Join(targetDir, it.ResolvedFilename))
}

// removeModFilesForMigration видаляє файли мода з цим modId (з jar-метаданих
// — читається з диска одразу при скануванні) або projectID — щоб після
// міграції не лишилось двох версій одного мода. modId перший: він точний
// і не залежить від мережі.
func (a *App) removeModFilesForMigration(modsDir, modID, projectID string) {
	if modID == "" && projectID == "" {
		return
	}
	mods, err := a.modMgr.ScanModsFull(modsDir)
	if err != nil {
		return
	}
	for _, m := range mods {
		match := (modID != "" && m.ID == modID) ||
			(modID == "" && (m.ProjectID == projectID || m.ProjectSlug == projectID))
		if match {
			_ = os.Remove(filepath.Join(modsDir, m.FileName))
			_ = os.Remove(filepath.Join(modsDir, m.FileName+".disabled"))
		}
	}
}
