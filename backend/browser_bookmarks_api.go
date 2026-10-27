package main

import (
	"fmt"
	"strings"
	"time"

	"shaurma-launcher-backend/internal/model"
	"shaurma-launcher-backend/internal/store"
)

// ── Закладки браузера контенту («зірочка») ──────────────────────────────
// Окремий розділ «Збережено» у браузері контенту (ContentBrowser.svelte):
// гравець ставить зірочку на будь-яку картку (мод/модпак/ресурспак/
// шейдер/датапак, Modrinth чи CurseForge) і потім знаходить її без
// повторного пошуку. Дані живуть у .bookmarks.json (internal/store),
// окремо від per-pack даних (закладка не прив'язана до конкретної
// збірки — той самий мод може стояти в кількох збірках одночасно).
//
// ── Групи (колекції) ─────────────────────────────────────────────────────
// Колекція — «тег-архів»: гравець складає пачку контенту один раз
// (напр. «Декорації»: набір з десятка ресурспаків/модів) і потім
// встановлює її ОДНІЄЮ кнопкою в будь-яку сумісну збірку. Записи
// колекції зберігаються тим самим типом store.Bookmark, що й звичайні
// закладки (лише з непорожнім CollectionID) — не дублюємо структуру/код.

func (a *App) bookmarksStore() *store.Store {
	if a.cfg == nil {
		return nil
	}
	return a.cfg.Store()
}

// projectTypeToContentDir — зворотний мапінг до contentDirToProjectType
// (browser_api.go) і contentDirFor (ContentBrowser.svelte): з типу
// проєкту в назву теки, куди встановлюється файл. Тримаємо в одному
// місці, щоб груповий інсталятор колекції не розходився з одиночним
// встановленням (InstallBrowserContentVersion).
func projectTypeToContentDir(projectType string) string {
	switch projectType {
	case "resourcepack":
		return "resourcepacks"
	case "shader":
		return "shaderpacks"
	case "datapack":
		return "datapacks"
	default:
		return "mods"
	}
}

// bookmarkToBrowserEntry — конвертація збереженої закладки назад у
// BrowserEntry для рендеру карткою в розділі «Збережено» — без
// повторного мережевого запиту деталей проєкту (назва/іконка/автор уже
// закешовані на момент додавання закладки, див. ToggleBookmark).
func bookmarkToBrowserEntry(b store.Bookmark) model.BrowserEntry {
	return model.BrowserEntry{
		ID:        b.ProjectID,
		Name:      b.Name,
		Type:      b.ProjectType,
		IconURL:   b.IconURL,
		Source:    b.Source,
		ProjectID: b.ProjectID,
		Author:    b.Author,
	}
}

// ── Звичайні закладки («зірочка») ────────────────────────────────────────

// IsBookmarked — чи стоїть зірочка на цьому проєкті. Використовується
// карткою браузера контенту одразу після рендеру списку (без окремого
// запиту на кожну картку — фронтенд викликає GetBookmarkedKeys один раз
// на весь видимий список, див. нижче).
func (a *App) IsBookmarked(source, projectID string) bool {
	st := a.bookmarksStore()
	if st == nil {
		return false
	}
	return st.IsBookmarked(source, projectID)
}

// GetBookmarkedKeys повертає МНОЖИНУ ключів "source|projectId" усіх
// закладених проєктів одним викликом — картки браузера контенту
// звіряються з нею локально (Set.has), замість окремого IPC-виклику на
// кожну з 30 карток видачі пошуку (це були б десятки зайвих round-trip'ів
// на кожен рендер сторінки результатів).
func (a *App) GetBookmarkedKeys() []string {
	st := a.bookmarksStore()
	if st == nil {
		return []string{}
	}
	d := st.GetBookmarksData()
	out := make([]string, 0, len(d.Bookmarks))
	for _, b := range d.Bookmarks {
		if b.CollectionID == "" {
			out = append(out, b.Source+"|"+b.ProjectID)
		}
	}
	return out
}

// ToggleBookmark додає закладку, якщо її ще немає, або прибирає, якщо
// вона вже стоїть — одна кнопка-зірочка на картці викликає саме цей
// метод і не мусить окремо перевіряти стан перед кожним кліком.
// Повертає новий стан (true = закладка щойно ДОДАНА).
//
// ВИПРАВЛЕНО: раніше вимагався СТРОГО непорожній entry.ProjectID —
// якщо картка (з якогось нестандартного джерела чи застарілого кешу)
// мала заповнений лише entry.ID (slug), а ProjectID порожній, метод
// мовчки повертав помилку "некоректний проєкт", і на фронтенді (там,
// де ключ картки рахується як projectId||id, той самий фолбек, що
// й скрізь у ContentBrowser.svelte/ModpackPage.svelte) зірочка
// залишалась незапаленою — виглядало так, ніби заклада «не існує»,
// хоча користувач щойно на неї натиснув. Тепер приймаємо slug (ID) як
// фолбек ідентифікатор, коли ProjectID не передано, — той самий
// принцип «projectId АБО id», що й на фронтенді.
func (a *App) ToggleBookmark(entry model.BrowserEntry) (bool, error) {
	st := a.bookmarksStore()
	if st == nil {
		return false, fmt.Errorf("сховище не ініціалізовано")
	}
	projectID := entry.ProjectID
	if projectID == "" {
		projectID = entry.ID
	}
	if projectID == "" || entry.Source == "" {
		return false, fmt.Errorf("некоректний проєкт для закладки")
	}
	if st.IsBookmarked(entry.Source, projectID) {
		if err := st.RemoveBookmark(entry.Source, projectID, ""); err != nil {
			return false, err
		}
		return false, nil
	}
	b := store.Bookmark{
		Source:      entry.Source,
		ProjectID:   projectID,
		ProjectType: entry.Type,
		Name:        entry.Name,
		IconURL:     entry.IconURL,
		Author:      entry.Author,
		AddedAt:     time.Now(),
	}
	if err := st.AddBookmark(b); err != nil {
		return false, err
	}
	return true, nil
}

// GetBookmarkedEntries повертає всі закладені проєкти як BrowserEntry —
// для рендеру розділу «Збережено» ТИМ САМИМ гріднм компонентом карток,
// що й звичайні результати пошуку (ContentBrowser.svelte: один шаблон
// картки для обох випадків). Сортування — найновіші закладки зверху.
func (a *App) GetBookmarkedEntries() []model.BrowserEntry {
	st := a.bookmarksStore()
	if st == nil {
		return []model.BrowserEntry{}
	}
	d := st.GetBookmarksData()
	out := make([]model.BrowserEntry, 0, len(d.Bookmarks))
	type row struct {
		e  model.BrowserEntry
		at time.Time
	}
	rows := make([]row, 0, len(d.Bookmarks))
	for _, b := range d.Bookmarks {
		if b.CollectionID != "" {
			continue
		}
		rows = append(rows, row{e: bookmarkToBrowserEntry(b), at: b.AddedAt})
	}
	// Найновіші зверху (просте вставлення в сортовану позицію — списки
	// закладок типово невеликі, десятки-сотні записів, тож O(n²) вставки
	// тут не проблема продуктивності).
	for i := 0; i < len(rows); i++ {
		for j := i; j > 0 && rows[j].at.After(rows[j-1].at); j-- {
			rows[j], rows[j-1] = rows[j-1], rows[j]
		}
	}
	for _, r := range rows {
		out = append(out, r.e)
	}
	return out
}

// ── Колекції (групи контенту) ────────────────────────────────────────────

// BrowserCollection — колекція для фронтенду: метадані + кількість
// записів (щоб список колекцій показував "Декорації (12)" без окремого
// запиту вмісту кожної).
type BrowserCollection struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Icon        string `json:"icon,omitempty"`
	Color       string `json:"color,omitempty"`
	ItemCount   int    `json:"itemCount"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
}

func toBrowserCollection(c store.Collection, itemCount int) BrowserCollection {
	return BrowserCollection{
		ID:          c.ID,
		Name:        c.Name,
		Description: c.Description,
		Icon:        c.Icon,
		Color:       c.Color,
		ItemCount:   itemCount,
		CreatedAt:   c.CreatedAt.Format(time.RFC3339),
		UpdatedAt:   c.UpdatedAt.Format(time.RFC3339),
	}
}

// GetCollections повертає всі колекції користувача з кількістю записів
// у кожній — для розділу «Збережено» → вкладка «Колекції».
func (a *App) GetCollections() []BrowserCollection {
	st := a.bookmarksStore()
	if st == nil {
		return []BrowserCollection{}
	}
	d := st.GetBookmarksData()
	counts := map[string]int{}
	for _, b := range d.Bookmarks {
		if b.CollectionID != "" {
			counts[b.CollectionID]++
		}
	}
	out := make([]BrowserCollection, 0, len(d.Collections))
	for _, c := range d.Collections {
		out = append(out, toBrowserCollection(c, counts[c.ID]))
	}
	return out
}

// GetEntryCollectionIDs повертає ID усіх колекцій, у яких УЖЕ є цей
// проєкт (source+projectID) — меню «Додати в колекцію» показує це
// позначкою на кожному пункті, замість дозволяти клікати «додати» в
// колекцію, де запис і так є (AddToCollection у такому випадку мовчки
// перезаписує наявний запис тим самим, тож повторний клік не мав
// видимого ефекту, і користувач не розумів, чи він щось зробив).
func (a *App) GetEntryCollectionIDs(source, projectID string) []string {
	st := a.bookmarksStore()
	if st == nil || projectID == "" || source == "" {
		return []string{}
	}
	d := st.GetBookmarksData()
	out := make([]string, 0, 4)
	for _, b := range d.Bookmarks {
		if b.CollectionID != "" && b.Source == source && b.ProjectID == projectID {
			out = append(out, b.CollectionID)
		}
	}
	return out
}

// CreateCollection створює нову порожню колекцію («Нова колекція» →
// модалка перейменування на фронтенді, як створення нової збірки).
func (a *App) CreateCollection(name, description, icon, color string) (BrowserCollection, error) {
	st := a.bookmarksStore()
	if st == nil {
		return BrowserCollection{}, fmt.Errorf("сховище не ініціалізовано")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Нова колекція"
	}
	c, err := st.SaveCollection(store.Collection{Name: name, Description: description, Icon: icon, Color: color})
	if err != nil {
		return BrowserCollection{}, err
	}
	return toBrowserCollection(c, 0), nil
}

// RenameCollection оновлює назву/опис/іконку/колір існуючої колекції.
func (a *App) RenameCollection(id, name, description, icon, color string) (BrowserCollection, error) {
	st := a.bookmarksStore()
	if st == nil {
		return BrowserCollection{}, fmt.Errorf("сховище не ініціалізовано")
	}
	d := st.GetBookmarksData()
	var found *store.Collection
	for i := range d.Collections {
		if d.Collections[i].ID == id {
			found = &d.Collections[i]
			break
		}
	}
	if found == nil {
		return BrowserCollection{}, fmt.Errorf("колекцію не знайдено: %s", id)
	}
	found.Name = strings.TrimSpace(name)
	if found.Name == "" {
		found.Name = "Нова колекція"
	}
	found.Description = description
	found.Icon = icon
	found.Color = color
	c, err := st.SaveCollection(*found)
	if err != nil {
		return BrowserCollection{}, err
	}
	counts := 0
	for _, b := range d.Bookmarks {
		if b.CollectionID == id {
			counts++
		}
	}
	return toBrowserCollection(c, counts), nil
}

// DeleteCollection видаляє колекцію і всі її записи (не сам встановлений
// контент у збірках — лише сам «архів»/групу, контент лишається як був).
func (a *App) DeleteCollection(id string) error {
	st := a.bookmarksStore()
	if st == nil {
		return fmt.Errorf("сховище не ініціалізовано")
	}
	return st.DeleteCollection(id)
}

// GetCollectionItems повертає вміст колекції як BrowserEntry (для
// рендеру карткою) — versionId/versionNumber кожного запису колекції
// доступні окремо через GetCollectionVersionInfo (мапа projectID →
// версія), бо BrowserEntry не несе версію (вона завжди "найновіша на
// момент показу" для звичайного пошуку, а тут — саме ЗАКРІПЛЕНА версія
// колекції).
func (a *App) GetCollectionItems(collectionID string) []model.BrowserEntry {
	st := a.bookmarksStore()
	if st == nil {
		return []model.BrowserEntry{}
	}
	d := st.GetBookmarksData()
	out := make([]model.BrowserEntry, 0, len(d.Bookmarks))
	for _, b := range d.Bookmarks {
		if b.CollectionID == collectionID {
			out = append(out, bookmarkToBrowserEntry(b))
		}
	}
	return out
}

// CollectionItemVersion — закріплена версія одного запису колекції,
// індексована source+"|"+projectId (той самий ключ, що entryKey(entry) у
// ContentBrowser.svelte) — фронтенд звіряється з мапою по картці, не
// перебираючи список лінійно.
type CollectionItemVersion struct {
	VersionID     string `json:"versionId,omitempty"`
	VersionNumber string `json:"versionNumber,omitempty"`
}

// GetCollectionVersionInfo повертає мапу "source|projectId" → закріплена
// версія для всіх записів колекції одним викликом — використовується
// поруч з GetCollectionItems, щоб картка кожного запису могла показати
// свою версію без окремого запиту на кожен елемент.
func (a *App) GetCollectionVersionInfo(collectionID string) map[string]CollectionItemVersion {
	st := a.bookmarksStore()
	if st == nil {
		return map[string]CollectionItemVersion{}
	}
	d := st.GetBookmarksData()
	out := map[string]CollectionItemVersion{}
	for _, b := range d.Bookmarks {
		if b.CollectionID != collectionID {
			continue
		}
		out[b.Source+"|"+b.ProjectID] = CollectionItemVersion{
			VersionID:     b.VersionID,
			VersionNumber: b.VersionNumber,
		}
	}
	return out
}

// AddToCollection додає запис (мод/ресурспак/шейдер/датапак з конкретною
// версією) до колекції. versionID/versionNumber — версія, ЗАКРІПЛЕНА за
// колекцією (не "завжди найновіша"): коли гравець складає перевірену
// пачку, він хоче, щоб групове встановлення поставило саме ту версію,
// яку він перевірив, а не найсвіжішу на момент майбутнього встановлення
// в іншу збірку (яка могла зламати сумісність).
func (a *App) AddToCollection(collectionID string, entry model.BrowserEntry, versionID, versionNumber string) error {
	st := a.bookmarksStore()
	if st == nil {
		return fmt.Errorf("сховище не ініціалізовано")
	}
	if collectionID == "" {
		return fmt.Errorf("не вказано колекцію")
	}
	if entry.ProjectID == "" || entry.Source == "" {
		return fmt.Errorf("некоректний проєкт для колекції")
	}
	// Модпаки НЕ додаються в колекції: колекція — це набір контенту для
	// ШВИДКОГО встановлення ВСЕРЕДИНУ вже наявної збірки (моди/ресурспаки/
	// шейдери/датапаки), а модпак — ціла окрема збірка, яку встановлюють
	// як нову картку, а не «в» якусь збірку. Групове встановлення колекції
	// (InstallCollectionToPack) у будь-якому разі не вміє встановити модпак
	// у збірку — тож забороняємо додавання ще на вході, а не мовчки
	// створюємо запис, який потім ніколи не встановиться. UI теж ховає
	// кнопку «+ колекція» на картках модпаків — ця перевірка страхує від
	// прямих викликів/старих клієнтів.
	if entry.Type == "modpack" {
		return fmt.Errorf("модпаки не можна додавати в колекції — колекція це набір контенту для збірки, а не сам пак")
	}
	b := store.Bookmark{
		Source:        entry.Source,
		ProjectID:     entry.ProjectID,
		ProjectType:   entry.Type,
		Name:          entry.Name,
		IconURL:       entry.IconURL,
		Author:        entry.Author,
		CollectionID:  collectionID,
		VersionID:     versionID,
		VersionNumber: versionNumber,
		AddedAt:       time.Now(),
	}
	return st.AddBookmark(b)
}

// RemoveFromCollection прибирає один запис із колекції (не всю колекцію).
func (a *App) RemoveFromCollection(collectionID, source, projectID string) error {
	st := a.bookmarksStore()
	if st == nil {
		return fmt.Errorf("сховище не ініціалізовано")
	}
	return st.RemoveBookmark(source, projectID, collectionID)
}

// ── Групове встановлення колекції в збірку ──────────────────────────────

// CollectionInstallPlanItem — один запис плану встановлення колекції:
// що з ним станеться і чому. Фронтенд показує це ПЕРЕД встановленням
// («луанчер буде писати навіть не підтримується такий-то мод у цій
// збірці — він може скасувати чи продовжити») — той самий принцип, що
// й у Prism/CurseForge app при імпорті модпаку: спершу план, потім дія.
type CollectionInstallPlanItem struct {
	Source        string `json:"source"`
	ProjectID     string `json:"projectId"`
	Name          string `json:"name"`
	ProjectType   string `json:"projectType"`
	VersionID     string `json:"versionId,omitempty"`
	VersionNumber string `json:"versionNumber,omitempty"`
	// Status: "install" (нового немає — поставимо), "update" (вже стоїть
	// СТАРІША версія — оновимо), "already" (вже стоїть ТА САМА версія —
	// пропустимо), "incompatible" (проєкт не має версії під (mc, loader)
	// цільової збірки — луанчер попереджає й дає скасувати або
	// продовжити БЕЗ цього запису).
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// PlanCollectionInstall аналізує колекцію ПРОТИ конкретної збірки:
// що вже встановлено (і якої версії), що потребує оновлення, що
// несумісне з (mcVersion, loader) цієї збірки. Викликається ПЕРЕД
// InstallCollectionToPack, щоб показати план і дати підтвердити/
// скасувати — сам процес встановлення нічого не змінює.
func (a *App) PlanCollectionInstall(collectionID, packID string) ([]CollectionInstallPlanItem, error) {
	st := a.bookmarksStore()
	if st == nil {
		return nil, fmt.Errorf("сховище не ініціалізовано")
	}
	pack, found := a.customPacks.Get(packID)
	if !found {
		return nil, fmt.Errorf("збірку не знайдено: %s", packID)
	}
	d := st.GetBookmarksData()
	var items []store.Bookmark
	for _, b := range d.Bookmarks {
		if b.CollectionID == collectionID {
			items = append(items, b)
		}
	}
	if len(items) == 0 {
		return []CollectionInstallPlanItem{}, nil
	}

	// Один виклик на всю колекцію, не на кожен запис — GetBrowserInstalledMap
	// сканує ВСІ файли контенту у збірці (читає метадані з диска), тож
	// виклик усередині циклу по N записах колекції означав би N повних
	// пересканувань теки збірки замість одного.
	installedMap := a.GetBrowserInstalledMap(packID)

	plan := make([]CollectionInstallPlanItem, 0, len(items))
	for _, b := range items {
		installed, alreadyHere := installedMap[b.ProjectID]

		item := CollectionInstallPlanItem{
			Source:        b.Source,
			ProjectID:     b.ProjectID,
			Name:          b.Name,
			ProjectType:   b.ProjectType,
			VersionID:     b.VersionID,
			VersionNumber: b.VersionNumber,
		}

		// Сумісність: чи взагалі є версія цього проєкту під (mc, loader)
		// цільової збірки (той самий метод, що й одиночний браузер
		// контенту використовує для фільтра — узгоджена логіка).
		effLoader := pack.Loader
		if !contentLoaderApplies(b.ProjectType) {
			effLoader = ""
		}
		extraLoaders := a.extraLoadersForPack(packID, effLoader)
		compatible, confirmed := a.browserEntryCompatibilityCached(model.BrowserEntry{
			Source:    b.Source,
			ProjectID: b.ProjectID,
		}, pack.MCVersion, effLoader, extraLoaders)
		if confirmed && !compatible {
			loaderNote := ""
			if effLoader != "" {
				loaderNote = " (" + effLoader + ")"
			}
			item.Status = "incompatible"
			item.Message = fmt.Sprintf("немає версії під %s%s", pack.MCVersion, loaderNote)
			plan = append(plan, item)
			continue
		}

		if alreadyHere && installed.ProjectID != "" {
			if b.VersionID != "" && installed.VersionID == b.VersionID {
				item.Status = "already"
				item.Message = "вже встановлено цю версію"
			} else {
				item.Status = "update"
				item.Message = "встановлено іншу версію — буде оновлено"
			}
		} else {
			item.Status = "install"
		}
		plan = append(plan, item)
	}
	return plan, nil
}

// InstallCollectionToPack встановлює всі записи колекції у вказану
// збірку ОДНІЄЮ дією (кнопка «Встановити колекцію»). skipProjectIDs —
// записи, які гравець явно попросив ПРОПУСТИТИ після перегляду плану
// (напр. позначені "incompatible", і він обрав "Продовжити без них"
// замість "Скасувати" повністю). Кожен запис встановлюється через той
// самий InstallBrowserContentVersion, що й одиночне встановлення з
// картки/сторінки проєкту — жодної окремої, розхідної логіки
// завантаження файлу чи резолву залежностей.
func (a *App) InstallCollectionToPack(collectionID, packID string, skipProjectIDs []string) ([]BrowserInstallResult, error) {
	st := a.bookmarksStore()
	if st == nil {
		return nil, fmt.Errorf("сховище не ініціалізовано")
	}
	d := st.GetBookmarksData()
	skip := map[string]bool{}
	for _, id := range skipProjectIDs {
		skip[id] = true
	}
	var results []BrowserInstallResult
	for _, b := range d.Bookmarks {
		if b.CollectionID != collectionID {
			continue
		}
		if skip[b.ProjectID] {
			continue
		}
		contentDir := projectTypeToContentDir(b.ProjectType)
		res, err := a.InstallBrowserContentVersion(packID, b.Source, b.ProjectID, b.VersionID, contentDir, true)
		if err != nil {
			results = append(results, BrowserInstallResult{
				PackID:  packID,
				Message: fmt.Sprintf("%s: %v", b.Name, err),
			})
			continue
		}
		results = append(results, res)
	}
	return results, nil
}

// ── Групове встановлення колекції НА СЕРВЕР ─────────────────────────────
// Колекція може містити і моди, і плагіни, і ресурспаки/шейдери/датапаки.
// Сервер тримає лише моди (мод-ядра) та плагіни (плагін-ядра), тож для
// сервера сумісність визначається насамперед ЯДРОМ (serverBrowserTarget):
// запис, який ядро не підтримує, отримує статус "incompatible" з поясненням
// (наприклад, шейдер або мод для Paper-сервера) — без запиту в мережу.

// serverKindForProjectType — тип проєкту → "mods"/"plugins" сервера
// ("" = сервер такий контент не тримає).
func serverKindForProjectType(projectType string) string {
	switch projectType {
	case "mod":
		return "mods"
	case "plugin":
		return "plugins"
	}
	return ""
}

// PlanCollectionInstallToServer — аналог PlanCollectionInstall для сервера.
func (a *App) PlanCollectionInstallToServer(collectionID, serverID string) ([]CollectionInstallPlanItem, error) {
	st := a.bookmarksStore()
	if st == nil {
		return nil, fmt.Errorf("сховище не ініціалізовано")
	}
	srv, err := a.srvAPI.GetServer(serverID)
	if err != nil {
		return nil, fmt.Errorf("сервер не знайдено: %s", serverID)
	}
	d := st.GetBookmarksData()
	var items []store.Bookmark
	for _, b := range d.Bookmarks {
		if b.CollectionID == collectionID {
			items = append(items, b)
		}
	}
	if len(items) == 0 {
		return []CollectionInstallPlanItem{}, nil
	}

	installedMap := a.GetServerBrowserInstalledMap(serverID)

	plan := make([]CollectionInstallPlanItem, 0, len(items))
	for _, b := range items {
		item := CollectionInstallPlanItem{
			Source:        b.Source,
			ProjectID:     b.ProjectID,
			Name:          b.Name,
			ProjectType:   b.ProjectType,
			VersionID:     b.VersionID,
			VersionNumber: b.VersionNumber,
		}
		kind := serverKindForProjectType(b.ProjectType)
		if kind == "" {
			item.Status = "incompatible"
			item.Message = "сервер не тримає цей тип контенту (лише моди та плагіни)"
			plan = append(plan, item)
			continue
		}
		_, loader, _, terr := serverBrowserTarget(srv.Core, kind)
		if terr != nil {
			item.Status = "incompatible"
			item.Message = terr.Error()
			plan = append(plan, item)
			continue
		}
		compatible, confirmed := a.browserEntryCompatibilityCached(model.BrowserEntry{
			Source:    b.Source,
			ProjectID: b.ProjectID,
		}, srv.MCVersion, loader, nil)
		if confirmed && !compatible {
			item.Status = "incompatible"
			item.Message = fmt.Sprintf("немає версії під %s (%s)", srv.MCVersion, loader)
			plan = append(plan, item)
			continue
		}
		if installed, here := installedMap[b.ProjectID]; here && installed.ProjectID != "" {
			if b.VersionID != "" && installed.VersionID == b.VersionID {
				item.Status = "already"
				item.Message = "вже встановлено цю версію"
			} else {
				item.Status = "update"
				item.Message = "встановлено іншу версію — буде оновлено"
			}
		} else {
			item.Status = "install"
		}
		plan = append(plan, item)
	}
	return plan, nil
}

// InstallCollectionToServer — аналог InstallCollectionToPack для сервера.
// Кожен запис іде тим самим InstallServerBrowserVersion, що й одиночне
// встановлення; записи, яких сервер не тримає, пропускаються з поясненням.
func (a *App) InstallCollectionToServer(collectionID, serverID string, skipProjectIDs []string) ([]BrowserInstallResult, error) {
	st := a.bookmarksStore()
	if st == nil {
		return nil, fmt.Errorf("сховище не ініціалізовано")
	}
	d := st.GetBookmarksData()
	skip := map[string]bool{}
	for _, id := range skipProjectIDs {
		skip[id] = true
	}
	var results []BrowserInstallResult
	for _, b := range d.Bookmarks {
		if b.CollectionID != collectionID || skip[b.ProjectID] {
			continue
		}
		kind := serverKindForProjectType(b.ProjectType)
		if kind == "" {
			results = append(results, BrowserInstallResult{
				PackID:  serverID,
				Message: fmt.Sprintf("%s: сервер не тримає цей тип контенту", b.Name),
			})
			continue
		}
		res, err := a.InstallServerBrowserVersion(serverID, b.Source, b.ProjectID, b.VersionID, kind, true)
		if err != nil {
			results = append(results, BrowserInstallResult{
				PackID:  serverID,
				Message: fmt.Sprintf("%s: %v", b.Name, err),
			})
			continue
		}
		results = append(results, res)
	}
	return results, nil
}
