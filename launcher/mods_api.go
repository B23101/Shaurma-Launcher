package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"shaurma-launcher-wails/internal/api"
	"shaurma-launcher-wails/internal/atomicfile"
	"shaurma-launcher-wails/internal/custompack"
	"shaurma-launcher-wails/internal/model"
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

func (a *App) instanceModsDir(packID string) string {
	settings := a.cfg.GetSettings()
	return filepath.Join(settings.InstanceDir, packID, ".minecraft", "mods")
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
	return exec.Command("xdg-open", dir).Start()
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
	return exec.Command("xdg-open", filepath.Dir(abs)).Start()
}

// ── Сканування ──────────────────────────────────────────────────────────
func (a *App) ScanModsFull(packID string) []model.ModEntry {
	mods, err := a.modMgr.ScanModsFull(a.instanceModsDir(packID))
	if err != nil {
		return []model.ModEntry{}
	}
	a.attachMissingDeps(mods)
	a.attachDuplicateFlags(mods)
	return mods
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

// modUpdateCacheKey — ключ кешу перевірки оновлень: окремий кеш на
// кожну пару (версія гри, лоадер), бо доступні оновлення різні для
// різних версій/лоадерів однієї збірки.
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
	cached, ok := a.modUpdateCache[modUpdateCacheKey(packID, mcVersion, loader)]
	a.modUpdateCacheMu.Unlock()
	if !ok {
		return mods
	}
	byFile := make(map[string]model.ModEntry, len(cached))
	for _, c := range cached {
		byFile[c.FileName] = c
	}
	for i := range mods {
		if c, ok := byFile[mods[i].FileName]; ok {
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
	}
	return mods
}

// ScanModsWithCachedUpdates — те, що фронтенд викликає при відкритті
// сторінки збірки: скан з диска + вже відомий (закешований) статус
// оновлень, БЕЗ походу в мережу. Мережевий похід — лише через
// RefreshModUpdates (кнопка "Перевірити оновлення" в UI).
func (a *App) ScanModsWithCachedUpdates(packID, mcVersion, loader string) []model.ModEntry {
	mods := a.ScanModsFull(packID)
	return a.mergeCachedUpdateInfo(packID, mcVersion, loader, mods)
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
	present := map[string]bool{}
	for i := range mods {
		if mods[i].ID != "" {
			present[mods[i].ID] = true
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

// ── Перевірка оновлень ──────────────────────────────────────────────────
// RefreshModUpdates — примусова мережева перевірка оновлень (кнопка
// "Перевірити оновлення" в UI). Результат кешується під (packID,
// mcVersion, loader), тому наступні відкриття сторінки (ScanModsWithCachedUpdates)
// НЕ лізуть у мережу повторно — лише цей виклик оновлює кеш.
func (a *App) RefreshModUpdates(packID, mcVersion, loader string) []model.ModEntry {
	return a.CheckModUpdates(packID, mcVersion, loader)
}

// Паралельно (до 6 потоків) для кожного увімкненого мода: Modrinth за SHA1
// файлу (точний збіг), інакше пошук за назвою; якщо Modrinth порожній —
// CurseForge (потребує API-ключ). Прогрес шлеться подією mods:update-progress.
// Кешує результат — див. mergeCachedUpdateInfo/ScanModsWithCachedUpdates.
func (a *App) CheckModUpdates(packID, mcVersion, loader string) []model.ModEntry {
	mods := a.ScanModsFull(packID)
	if mcVersion == "" || loader == "" {
		for i := range mods {
			mods[i].Status = "done"
		}
		return mods
	}
	loader = strings.ToLower(loader)
	sem := make(chan struct{}, modUpdateConcurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	done := 0
	emit := func() {
		mu.Lock()
		done++
		d := done
		mu.Unlock()
		a.emit("mods:update-progress", map[string]any{"packId": packID, "done": d, "total": len(mods)})
	}
	for i := range mods {
		m := &mods[i]
		if !m.Enabled {
			m.Status = "done"
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			m.Status = "checking"
			if projID, ok := a.findModOnModrinth(m, mcVersion, loader); ok {
				m.Source = "modrinth"
				m.ProjectID = projID
				if opt := a.modrinthLatest(projID, mcVersion, loader); opt != nil {
					if m.Version != "" && opt.VersionNumber != "" && !isSameModVersion(m.FileName, m.Version, opt.Filename, opt.VersionNumber) {
						m.HasUpdate = true
						m.LatestVersion = opt.VersionNumber
						m.LatestURL = opt.URL
					}
					// Іконка/слаг проєкту — лише для модів з оновленням
					// (щоб не бити API додатковим запитом на всіх).
					if m.HasUpdate {
						if p, err := a.modrinth.GetProject(projID); err == nil && p != nil {
							m.IconURL = p.IconURL
							m.ProjectSlug = p.Slug
						}
					}
				}
			} else if cf, err := a.findModOnCurseForge(m, mcVersion, loader); err == nil && cf != nil {
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
			m.Status = "done"
			emit()
		}()
	}
	wg.Wait()
	a.modUpdateCacheMu.Lock()
	a.modUpdateCache[modUpdateCacheKey(packID, mcVersion, loader)] = mods
	a.modUpdateCacheMu.Unlock()
	return mods
}

// invalidateModUpdateCache скидає кеш перевірки оновлень для збірки —
// викликається після будь-якої дії, що змінює склад/версії модів
// (оновлення, зміна версії, видалення, встановлення залежностей), щоб
// застарілий "hasUpdate" не показувався після того, як мод уже оновлено.
func (a *App) invalidateModUpdateCache(packID string) {
	a.modUpdateCacheMu.Lock()
	defer a.modUpdateCacheMu.Unlock()
	prefix := packID + "|"
	for k := range a.modUpdateCache {
		if strings.HasPrefix(k, prefix) {
			delete(a.modUpdateCache, k)
		}
	}
}

// findModOnModrinth: спершу точний збіг за SHA1 файлу, інакше пошук за назвою.
func (a *App) findModOnModrinth(m *model.ModEntry, mcVersion, loader string) (string, bool) {
	if m.Sha1 != "" {
		if v, err := a.modrinth.GetVersionByHash(m.Sha1); err == nil && v != nil && v.ProjectID != "" {
			return v.ProjectID, true
		}
	}
	if m.Name == "" {
		return "", false
	}
	hits, err := a.modrinth.SearchMods(m.Name, mcVersion, loader, 5, 0)
	if err != nil || len(hits) == 0 {
		return "", false
	}
	best := api.BestModrinthMatch(hits, m.Name)
	if best == nil || best.ProjectID == "" {
		return "", false
	}
	return best.ProjectID, true
}

func (a *App) modrinthLatest(projectID, mcVersion, loader string) *model.ModVersionOption {
	opt, err := a.modrinth.LatestVersionFor(projectID, mcVersion, loader)
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
// з фолбеком на нормалізований номер версії. Заміняє крихке пряме
// порівняння рядків version_number, через яке HasUpdate міг помилково
// вилазити навіть коли встановлена версія й остання — та сама (просто
// по-різному підписана), і навпаки — не показувати оновлення там, де
// формат номера версії випадково збігся, а сама версія інша.
func isSameModVersion(installedFile, installedVersion, remoteFile, remoteVersion string) bool {
	instFile := strings.TrimSuffix(installedFile, ".disabled")
	if instFile != "" && remoteFile != "" && strings.EqualFold(instFile, remoteFile) {
		return true
	}
	if installedVersion == "" || remoteVersion == "" {
		return false
	}
	return normalizeDepName(installedVersion) == normalizeDepName(remoteVersion)
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

func (a *App) findModOnCurseForge(m *model.ModEntry, mcVersion, loader string) (*cfModVersion, error) {
	client := custompack.NewCurseForgeClient()
	if !client.HasAPIKey() {
		return nil, fmt.Errorf("CurseForge API-ключ не налаштовано у цій збірці лаунчера")
	}
	hits, err := client.SearchMods(m.Name, mcVersion, loader)
	if err != nil || len(hits) == 0 {
		return nil, fmt.Errorf("не знайдено на CurseForge")
	}
	hit := bestCurseForgeMatch(hits, m.Name)
	if hit == nil {
		return nil, fmt.Errorf("не знайдено на CurseForge")
	}
	files, err := client.GetModFiles(hit.ID, mcVersion, loader)
	if err != nil || len(files) == 0 {
		return nil, fmt.Errorf("немає файлів для версії/лоадера")
	}
	f := files[0]
	return &cfModVersion{ID: hit.ID, Name: hit.Name, Slug: hit.Slug, LogoURL: hit.LogoURL, FileName: f.FileName, DownloadURL: f.DownloadURL}, nil
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
	if cf, err := a.findModOnCurseForge(mod, mcVersion, loader); err == nil {
		client := custompack.NewCurseForgeClient()
		if files, err := client.GetModFiles(cf.ID, mcVersion, loader); err == nil && len(files) > 0 {
			opts := make([]model.ModVersionOption, 0, len(files))
			for _, f := range files {
				opts = append(opts, model.ModVersionOption{
					ID: fmt.Sprintf("%d", f.ID), Name: cf.Name, VersionNumber: f.FileName,
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
		wantFile := strings.TrimSuffix(strings.TrimSuffix(mod.FileName, ".disabled"), "")
		for i := range opts {
			if opts[i].Filename != "" && strings.EqualFold(opts[i].Filename, wantFile) {
				opts[i].IsCurrent = true
				return
			}
		}
	}
	if mod.Version != "" {
		wantVer := normalizeDepName(mod.Version)
		for i := range opts {
			if normalizeDepName(opts[i].VersionNumber) == wantVer {
				opts[i].IsCurrent = true
				return
			}
		}
	}
}

// InstallModVersion завантажує обрану версію і замінює нею поточний файл
// (стан «вимкнено» зберігається).
func (a *App) InstallModVersion(packID, fileName string, opt model.ModVersionOption) error {
	modsDir := a.instanceModsDir(packID)
	if opt.URL == "" || opt.Filename == "" {
		return fmt.Errorf("немає посилання на файл версії")
	}
	tmp := filepath.Join(modsDir, "."+opt.Filename+".part")
	if err := a.downloadModFile(opt.URL, tmp); err != nil {
		return err
	}
	disabled := strings.HasSuffix(fileName, ".jar.disabled")
	_ = os.Remove(filepath.Join(modsDir, fileName))
	_ = os.Remove(strings.TrimSuffix(fileName, ".disabled"))
	newName := opt.Filename
	if disabled {
		newName = opt.Filename + ".jar.disabled"
	}
	if err := os.Rename(tmp, filepath.Join(modsDir, newName)); err != nil {
		os.Remove(tmp)
		return err
	}
	a.invalidateModUpdateCache(packID)
	return nil
}

// ── Залежності ──────────────────────────────────────────────────────────
// mcVersion/loader — версія гри та лоадер ПОТОЧНОЇ збірки: без них пошук
// залежності на Modrinth/CurseForge міг би знайти версію мода для іншого
// лоадера/версії гри — вона встановилась би, але не запрацювала б разом
// з рештою збірки (саме на це скаржились: "має орієнтуватися який
// луадер у збірці і версія гри").
//
// fileName — опційний: якщо порожній, повертаються ВСІ відсутні залежності
// збірки (кнопка «Залежності» у тулбарі); якщо переданий — лише залежності
// КОНКРЕТНОГО мода з цим fileName (клік на бейдж «залежність» на картці
// мода). Раніше бейдж на картці викликав ту саму функцію без параметра —
// і завжди відкривав глобальний список усіх нестанов­лених залежностей
// всієї збірки, а не саме для мода, на якому клікнули.
func (a *App) ResolveModDependencies(packID, mcVersion, loader, fileName string) []model.ModDependency {
	mods := a.ScanModsFull(packID)
	// present — так само, як в attachMissingDeps: мод вважається наявним,
	// якщо він фізично лежить у теці (увімкнений чи ні) — гравець сам
	// вирішує, чи вмикати вимкнений мод, лаунчер туди не втручається.
	present := map[string]bool{}
	for i := range mods {
		if mods[i].ID != "" {
			present[mods[i].ID] = true
		}
	}
	var missing []string
	seen := map[string]bool{}
	for i := range mods {
		if !mods[i].Enabled {
			continue
		}
		if fileName != "" && mods[i].FileName != fileName {
			continue
		}
		for _, d := range mods[i].DependsOn {
			if !present[d] && !seen[d] {
				seen[d] = true
				missing = append(missing, d)
			}
		}
	}
	if len(missing) == 0 {
		return nil
	}
	out := make([]model.ModDependency, len(missing))
	loader = strings.ToLower(loader)
	sem := make(chan struct{}, modUpdateConcurrency)
	var wg sync.WaitGroup
	for i, name := range missing {
		wg.Add(1)
		go func(idx int, dep string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			out[idx] = a.resolveOneDependency(dep, mcVersion, loader)
		}(i, name)
	}
	wg.Wait()
	return out
}

// resolveOneDependency шукає modId залежності на Modrinth (спершу) і
// CurseForge (fallback), ФІЛЬТРУЮЧИ під версію гри/лоадер поточної
// збірки — щоб знайдена версія дійсно підходила саме туди, куди її
// збираються встановити, а не була випадковою найновішою версією для
// іншого лоадера чи версії гри.
func (a *App) resolveOneDependency(dep, mcVersion, loader string) model.ModDependency {
	if hits, err := a.modrinth.SearchMods(dep, mcVersion, loader, 5, 0); err == nil && len(hits) > 0 {
		if best := api.BestModrinthMatch(hits, dep); best != nil && best.ProjectID != "" {
			if opt, err := a.modrinth.LatestVersionFor(best.ProjectID, mcVersion, loader); err == nil && opt != nil && opt.URL != "" {
				icon := ""
				if p, err := a.modrinth.GetProject(best.ProjectID); err == nil && p != nil {
					icon = p.IconURL
				}
				return model.ModDependency{
					ModID: best.ProjectID, Name: best.Title, Version: opt.VersionNumber,
					DownloadURL: opt.URL, Filename: opt.Filename, IconURL: icon, Resolved: true,
				}
			}
		}
	}
	if cf, err := a.findModOnCurseForge(&model.ModEntry{Name: dep}, mcVersion, loader); err == nil && cf != nil {
		return model.ModDependency{
			ModID: fmt.Sprintf("%d", cf.ID), Name: cf.Name, Version: cf.FileName,
			DownloadURL: cf.DownloadURL, Filename: cf.FileName, Resolved: true,
		}
	}
	return model.ModDependency{ModID: dep, Name: dep, Resolved: false, Error: "не знайдено на Modrinth/CurseForge"}
}

// InstallModDependencies завантажує знайдені залежності в папку модів
// (вже встановлені за modId пропускаються).
func (a *App) InstallModDependencies(packID string, deps []model.ModDependency) error {
	modsDir := a.instanceModsDir(packID)
	existing := map[string]bool{}
	if mods, err := a.modMgr.ScanModsFull(modsDir); err == nil {
		for _, m := range mods {
			if m.ID != "" {
				existing[m.ID] = true
			}
		}
	}
	var errs []string
	for _, d := range deps {
		if !d.Resolved || d.DownloadURL == "" {
			continue
		}
		if d.ModID != "" && existing[d.ModID] {
			continue
		}
		dest := filepath.Join(modsDir, d.Filename)
		if err := a.downloadModFile(d.DownloadURL, dest); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", d.Name, err))
			continue
		}
		if d.ModID != "" {
			existing[d.ModID] = true
		}
	}
	a.invalidateModUpdateCache(packID)
	if len(errs) > 0 {
		return fmt.Errorf("не вдалось встановити: %s", strings.Join(errs, "; "))
	}
	return nil
}

// ── Масове оновлення + резервні копії ───────────────────────────────────
func (a *App) UpdateAllMods(packID, mcVersion, loader string, backup bool) model.ModUpdateResult {
	var res model.ModUpdateResult
	// Використовуємо вже відому (закешовану) інформацію про оновлення —
	// користувач бачить кнопку "Оновити всі" саме тому, що перевірка вже
	// була виконана (RefreshModUpdates) і показала доступні оновлення;
	// повторний мережевий похід тут був би зайвим і сповільнював би клік.
	mods := a.ScanModsWithCachedUpdates(packID, mcVersion, loader)
	var toUpdate []model.ModEntry
	for _, m := range mods {
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
	for i, m := range toUpdate {
		a.emit("mods:update-progress", map[string]any{
			"packId": packID, "phase": "update", "current": i + 1, "total": len(toUpdate), "fileName": m.FileName,
		})
		if err := a.installModUpdate(packID, m); err != nil {
			res.Failed = append(res.Failed, fmt.Sprintf("%s: %v", m.Name, err))
		} else {
			res.Updated = append(res.Updated, m.Name)
		}
	}
	a.invalidateModUpdateCache(packID)
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
	a.removeModFilesByID(modsDir, m.ID, m.FileName)
	tmp := filepath.Join(modsDir, "."+newName+".part")
	if err := a.downloadModFile(m.LatestURL, tmp); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(modsDir, newName))
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

func (a *App) appendBackupEntry(packID string, m model.ModEntry, bak string) {
	list := a.readBackups(packID)
	list = append(list, model.ModBackupEntry{
		ID: fmt.Sprintf("bak-%d", time.Now().UnixNano()), ModID: m.ID, Name: m.Name,
		Version: m.Version, FileName: m.FileName, BackupFile: bak, Date: time.Now().Format(time.RFC3339),
	})
	_ = atomicfile.WriteJSONAtomic(a.backupsManifestPath(packID), list)
}

func (a *App) ListModBackups(packID string) []model.ModBackupEntry {
	return a.readBackups(packID)
}

// RestoreModBackup повертає стару версію мода: видаляє поточні файли мода
// (оновлена версія могла змінити ім'я файлу) і кладе резервну копію назад.
func (a *App) RestoreModBackup(packID string, entry model.ModBackupEntry) error {
	backupsDir := a.modsBackupsDir(packID)
	modsDir := a.instanceModsDir(packID)
	data, err := os.ReadFile(filepath.Join(backupsDir, entry.BackupFile))
	if err != nil {
		return fmt.Errorf("резервну копію не знайдено: %v", err)
	}
	a.removeModFilesByID(modsDir, entry.ModID, entry.FileName)
	if err := os.WriteFile(filepath.Join(modsDir, entry.FileName), data, 0644); err != nil {
		return err
	}
	a.invalidateModUpdateCache(packID)
	return nil
}

// ── Дублікати / видалення / pre-launch ──────────────────────────────────
func (a *App) DeleteMod(packID, fileName string) error {
	err := a.modMgr.DeleteMod(a.instanceModsDir(packID), fileName)
	if err == nil {
		a.invalidateModUpdateCache(packID)
	}
	return err
}

func (a *App) FindDuplicateMods(packID string) []model.DuplicateModGroup {
	mods := a.ScanModsFull(packID)
	// Швидке виявлення за modId: той самий мод (з різними назвами файлів
	// або версіями) встановлений двічі. Хешування jar НЕ виконуємо — воно
	// читає гігабайти і гальмувало б запуск гри; modId унікальний у межах
	// лоадера, тож цього достатньо для анти-дублікат системи.
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

// CheckPreLaunchModIssues — дублікати + відсутні залежності (без мережі).
// Фронтенд показує модалку зі швидкими діями перед запуском гри.
func (a *App) CheckPreLaunchModIssues(packID string) model.PreLaunchModIssues {
	var out model.PreLaunchModIssues
	out.Duplicates = a.FindDuplicateMods(packID)
	mods := a.ScanModsFull(packID)
	a.attachMissingDeps(mods)
	seen := map[string]bool{}
	for _, m := range mods {
		for _, d := range m.MissingDeps {
			if !seen[d] {
				seen[d] = true
				out.MissingDeps = append(out.MissingDeps, d)
			}
		}
	}
	out.HasIssues = len(out.Duplicates) > 0 || len(out.MissingDeps) > 0
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
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-.")
}