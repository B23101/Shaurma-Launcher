package main

import (
	"shaurma-launcher-backend/internal/crashguard"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"shaurma-launcher-backend/internal/api"
	"shaurma-launcher-backend/internal/browsersearch"
	"shaurma-launcher-backend/internal/builds"
	"shaurma-launcher-backend/internal/custompack"
	"shaurma-launcher-backend/internal/imageconvert"
	"shaurma-launcher-backend/internal/model"
	"shaurma-launcher-backend/internal/neterr"
	syncengine "shaurma-launcher-backend/internal/sync"
)

// ── Браузер контенту: методи App над ModrinthClient + CurseForgeClient ──
// Архітектура повторює веб-апку Modrinth (modrinth/code): пошук з
// фасетами (тип/версія/лоадер) і сортуванням, сторінка проєкту з
// описом/галереєю/версіями, встановлення контенту у вибрану збірку.
// Результати пошуку рендеряться окремо по джерелах (Modrinth/CurseForge),
// як просив ужиток — «показувати окремо з того окремо стого».

// BrowserProjectDetail — уніфікований опис проєкту для сторінки модпаку
// (незалежно від джерела: Modrinth чи CurseForge). Body — markdown
// (Modrinth) або HTML (CurseForge), фронтенд рендерить у «Описі».
type BrowserProjectDetail struct {
	Source      string   `json:"source"` // modrinth | curseforge
	ProjectID   string   `json:"projectId"`
	Slug        string   `json:"slug"`
	ProjectType string   `json:"projectType"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Body        string   `json:"body"`
	IconURL     string   `json:"iconUrl"`
	Downloads   int64    `json:"downloads"`
	Followers   int64    `json:"followers"`
	Categories  []string `json:"categories"`
	// MCVersions/Loaders — версії гри та лоадери, під які є версії проєкту.
	MCVersions []string `json:"mcVersions"`
	Loaders    []string `json:"loaders"`
	// Gallery — скріншоти (Modrinth gallery / CurseForge screenshots).
	Gallery []BrowserGalleryImage `json:"gallery"`
	// ProjectURL — посилання на сторінку проєкту в сервісі.
	ProjectURL string `json:"projectUrl"`
	// Published/Updated — дати публікації та останнього оновлення
	// (секція «Деталі» на сторінці, як ProjectSidebarDetails веб-апки).
	Published string `json:"published"`
	Updated   string `json:"updated"`
	// License — ліцензія проєкту (для секції «Деталі»).
	LicenseID   string `json:"licenseId"`
	LicenseName string `json:"licenseName"`
	LicenseURL  string `json:"licenseUrl"`
	// ClientSide/ServerSide — середовища виконання (client_side/server_side)
	// для секції «Сумісність» (ProjectSidebarCompatibility веб-апки).
	ClientSide string `json:"clientSide"`
	ServerSide string `json:"serverSide"`
	// Посилання проєкту для секції «Посилання» (ProjectSidebarLinks).
	IssuesURL  string `json:"issuesUrl"`
	SourceURL  string `json:"sourceUrl"`
	DiscordURL string `json:"discordUrl"`
	WikiURL    string `json:"wikiUrl"`
	// Author — автор/команда проєкту (для секції «Автор»).
	Author string `json:"author"`
	// AuthorID/AuthorAvatar/AuthorURL — ідентичність автора для сторінки
	// автора (клік по автору в сайдбарі → профіль з аватаром і всіма
	// проєктами, як UserPage у Modrinth app). AuthorID — Modrinth user/org
	// ID або CurseForge authorId; AuthorAvatar — посилання на аватар
	// (CurseForge не віддає аватар — порожньо, фронт покаже літеру).
	AuthorID     string `json:"authorId"`
	AuthorAvatar string `json:"authorAvatar"`
	AuthorURL    string `json:"authorUrl"`
	// Creators — усі автори проєкту, як ProjectSidebarCreators у Modrinth
	// app: спершу організація-власник (якщо є), далі учасники команди
	// (власник першим, решта за роллю й ім'ям). Для CurseForge — authors[].
	Creators []BrowserCreator `json:"creators"`
}

// BrowserCreator — один автор проєкту (організація або учасник команди).
type BrowserCreator struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Avatar  string `json:"avatar"`
	Role    string `json:"role"`
	IsOwner bool   `json:"isOwner"`
	IsOrg   bool   `json:"isOrg"`
	URL     string `json:"url"`
}

// modrinthCreators складає список авторів так само, як sortedMembers у
// ProjectSidebarCreators Modrinth app: лише прийняті учасники; власник —
// учасник, що є власником організації (для проєктів організації) або з
// is_owner; решта — за роллю, потім за ім'ям.
func modrinthCreators(org *api.ModrinthOrganization, members []api.ModrinthTeamMember) []BrowserCreator {
	out := []BrowserCreator{}
	if org != nil && org.ID != "" {
		out = append(out, BrowserCreator{
			ID: org.ID, Name: org.Name, Avatar: org.IconURL, IsOrg: true,
			URL: "https://modrinth.com/organization/" + org.Slug,
		})
	}
	accepted := make([]api.ModrinthTeamMember, 0, len(members))
	for _, m := range members {
		if (m.Accepted == nil || *m.Accepted) && m.User.Username != "" {
			accepted = append(accepted, m)
		}
	}
	isOwner := func(m api.ModrinthTeamMember) bool {
		if org != nil && org.ID != "" {
			for _, om := range org.Members {
				if om.User.ID == m.User.ID && om.IsOwner {
					return true
				}
			}
			return false
		}
		return m.IsOwner
	}
	ownerIdx := -1
	for i, m := range accepted {
		if isOwner(m) {
			ownerIdx = i
			break
		}
	}
	var owner *api.ModrinthTeamMember
	rest := make([]api.ModrinthTeamMember, 0, len(accepted))
	for i := range accepted {
		if i == ownerIdx {
			owner = &accepted[i]
			continue
		}
		rest = append(rest, accepted[i])
	}
	sort.SliceStable(rest, func(i, j int) bool {
		if rest[i].Role == rest[j].Role {
			return rest[i].User.Username < rest[j].User.Username
		}
		return rest[i].Role < rest[j].Role
	})
	toCreator := func(m api.ModrinthTeamMember, own bool) BrowserCreator {
		return BrowserCreator{
			ID: m.User.ID, Name: m.User.Username, Avatar: m.User.AvatarURL, Role: m.Role, IsOwner: own,
			URL: "https://modrinth.com/user/" + m.User.Username,
		}
	}
	if owner != nil {
		out = append(out, toCreator(*owner, true))
	}
	for _, m := range rest {
		out = append(out, toCreator(m, false))
	}
	return out
}

// BrowserGalleryImage — один скріншот у галереї сторінки модпаку.
// Featured — позначене автором «головне» зображення проєкту (Modrinth
// gallery.featured); використовується як фон картки/банера збірки,
// створеної з браузера контенту (CreateBrowserModpack), коли featured
// немає — беремо перше зображення галереї як запасний варіант.
// RawURL — оригінальне (не стиснуте CDN-ом) зображення для лайтбокса
// (Modrinth gallery.raw_url) — те саме, що показує expanded-image-modal
// у Modrinth app.
type BrowserGalleryImage struct {
	URL      string `json:"url"`
	RawURL   string `json:"rawUrl,omitempty"`
	Title    string `json:"title"`
	Featured bool   `json:"featured"`
}

// BrowserVersionView — уніфікована версія проєкту для сторінки модпаку /
// списку «Версії» (і для діалогу «Оновити з джерела»).
type BrowserVersionView struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	VersionNumber string   `json:"versionNumber"`
	VersionType   string   `json:"versionType"` // release | beta | alpha
	GameVersions  []string `json:"gameVersions"`
	Loaders       []string `json:"loaders"`
	DatePublished string   `json:"datePublished"`
	Changelog     string   `json:"changelog"`
	FileName      string   `json:"fileName"`
	FileURL       string   `json:"fileUrl"`
	DownloadURL   string   `json:"downloadUrl"`
	FileSize      int64    `json:"fileSize"`
	SHA1          string   `json:"sha1"`
}

// BrowserSearchResult — результат пошуку, згрупований по джерелах.
// Секції відображаються окремо (картки з бейджем джерела).
type BrowserSearchResult struct {
	// Modrinth — результати Modrinth (порожні, якщо джерело не вибрано).
	Modrinth []model.BrowserEntry `json:"modrinth"`
	// CurseForge — результати CurseForge (порожні, якщо ключ не налаштовано).
	CurseForge []model.BrowserEntry `json:"curseforge"`
	// CFDisabled — CurseForge не налаштовано (немає API-ключа).
	CFDisabled bool `json:"cfDisabled"`
	// CFError — помилка запиту CurseForge (якщо ключ є, але API відмовив).
	CFError string `json:"cfError,omitempty"`
	// ModrinthError — помилка запиту Modrinth (мережа/API). Раніше помилка
	// ковталась, і фронтенд показував порожній список без пояснення;
	// тепер UI бачить «Немає інтернету» замість тиші (офлайн-режим).
	ModrinthError string `json:"modrinthError,omitempty"`
	// TotalHits — загальна кількість результатів пошуку (пагінація).
	TotalHits int `json:"totalHits"`
	// HasMore — чи є ще сторінки після поточної (offset+limit < total).
	HasMore bool `json:"hasMore"`
	// CFTotalHits / CFHasMore — те саме для CurseForge (окремо, бо джерела
	// шукаються незалежними запитами і мають різну кількість результатів).
	CFTotalHits int  `json:"cfTotalHits"`
	CFHasMore   bool `json:"cfHasMore"`
	// SmartSearchTerms — непорожньо, якщо запит розпізнано словником
	// «розумного пошуку» (browsersearch.Expand) як тему (напр. «меблі»)
	// і пошук був розширений цими англійськими термінами. UI показує
	// підказку «Показано результати за темою: furniture, decoration» —
	// гравець бачить, ЧОМУ видача не збігається дослівно з тим, що він
	// ввів, замість мовчазної підміни запиту.
	SmartSearchTerms []string `json:"smartSearchTerms,omitempty"`
}

// BrowserCompatiblePack — одна кастомна збірка, у яку МОЖНА встановити
// знайдений контент (є версія під (mcVersion, loader) збірки).
type BrowserCompatiblePack struct {
	PackID  string `json:"packId"`
	Name    string `json:"name"`
	IconURL string `json:"iconUrl"`
	// Icon — пресет-іконка (Tabler, напр. "ti-puzzle") збірки; IconURL
	// (PNG) має пріоритет у UI, коли є.
	Icon      string `json:"icon,omitempty"`
	Color     string `json:"color"`
	MCVersion string `json:"mcVersion"`
	Loader    string `json:"loader"`
	// AlreadyInstalled — контент цього проєкту вже є у збірці.
	AlreadyInstalled  bool   `json:"alreadyInstalled"`
	InstalledFileName string `json:"installedFileName,omitempty"`
	// Whitelisted — це шаурма-збірка (kind=="shaurma"), а не кастомна, і
	// автор збірки явно ДОЗВОЛИВ саме цей проєкт через Pack Manager →
	// Білий список (internal/api.GetEffectiveWhitelistDetailed за
	// Platform+ProjectID). Шаурма-збірки, у яких мод НЕ у білому списку,
	// у цей список кандидатів взагалі не потрапляють — синк лаунчера
	// прибрав би такий файл при наступній перевірці цілісності. Фронтенд
	// показує ці збірки ОКРЕМОЮ групою від кастомних (не змішує в
	// одному невиразному списку — саме шаурма-збірка тут виняток, а не
	// правило).
	Whitelisted bool `json:"whitelisted,omitempty"`
	// Unconfirmed — сумісність НЕ вдалось перевірити мережевим запитом
	// (CurseForge: відсутній API-ключ, таймаут, rate limit тощо), а не
	// «підтверджено несумісна». Раніше при err != nil збірка просто
	// пропускалась (continue) нарівні з реальною несумісністю — при
	// нестабільному CurseForge (не завантажився список версій мода)
	// діалог встановлення показував «немає сумісної збірки» навіть коли
	// збірка (напр. fabric 1.20.1 під мод fabric 1.20.1) насправді
	// підходила. Тепер такі збірки лишаються у списку з цим прапорцем —
	// той самий fail-open принцип, що вже застосований у
	// browserEntryCompatibility для сторінки/картки контенту.
	Unconfirmed bool `json:"unconfirmed,omitempty"`
}

// BrowserInstallResult — результат встановлення контенту у збірку.
type BrowserInstallResult struct {
	PackID      string `json:"packId,omitempty"`
	FileName    string `json:"fileName,omitempty"`
	InstalledTo string `json:"installedTo,omitempty"`
	Message     string `json:"message"`
}

// cfClient повертає CurseForge-клієнт (клієнт без стану, лише HTTP).
func (a *App) cfClient() *custompack.CurseForgeClient {
	return custompack.NewCurseForgeClient()
}

// ── Пошук контенту ──────────────────────────────────────────────────────

// BrowserSearch шукає контент на Modrinth (і CurseForge, якщо ключ є) і
// повертає результати, згруповані по джерелах. Той самий контракт, що
// discover-сторінка веб-апки Modrinth: query + facets (project_type,
// versions, categories, loaders) + index (сортування).
//
// categories — кома-розділений список тегів-категорій (порожній =
// без фільтра). Для Modrinth ідуть у facets (OR усередині фасета); для
// CurseForge результати відфільтровуються по тегах проєктів.
//
// offset — зміщення для пагінації («перемикання між сторінками контенту
// як у Modrinth»): кожна сторінка запитує наступний блок по 30.
//
// ЗАСТАРІЛЕ для основного екрану браузера контенту: цей метод чекає ОБИДВА
// джерела разом (wg.Wait нижче), тож коли CurseForge гальмує (rate limit,
// повільна мережа), фронтенд не бачить навіть давно готові результати
// Modrinth — спінер «Пошук…» висить, поки не відповість повільніше з двох
// джерел. Метод лишається для місць, де окремий результат на джерело не
// потрібен (напр. глобальний Ctrl+K пошук у App.svelte, де обидва списки
// одразу змішуються в один короткий preview). Для екрану браузера контенту
// використовуються BrowserSearchModrinth і BrowserSearchCurseForge —
// кожен повертає свій результат одразу по готовності, незалежно від
// іншого (див. ContentBrowser.svelte doSearch).
func (a *App) BrowserSearch(query, projectType, source, mcVersion, loader, categories, sort string, client, server, openSource bool, limit, offset int) (res BrowserSearchResult) {
	defer func() {
		if r := recover(); r != nil {
			res.CFError = fmt.Sprintf("BrowserSearch panic: %v\n%s", r, debug.Stack())
		}
	}()
	res.Modrinth = []model.BrowserEntry{}
	res.CurseForge = []model.BrowserEntry{}

	var wg sync.WaitGroup
	if source == "" || source == "all" || source == "modrinth" {
		wg.Add(1)
		go func() {
			defer crashguard.Recover("browser_api.go:237")
			defer wg.Done()
			mr := a.BrowserSearchModrinth(query, projectType, mcVersion, loader, categories, sort, client, server, openSource, limit, offset, "")
			res.Modrinth = mr.Modrinth
			res.ModrinthError = mr.ModrinthError
			res.TotalHits = mr.TotalHits
			res.HasMore = mr.HasMore
		}()
	}
	// CurseForge пропускаємо для "plugin" (Bukkit/Paper/Velocity-плагіни):
	// на відміну від модів/ресурспаків/шейдерів/датапаків, у CurseForge
	// немає окремого класу для цього типу контенту, зіставленого з нашою
	// схемою — cfClassIDFor повернув би classId модів (6), і пошук плагінів
	// на CurseForge насправді видавав би ЗВИЧАЙНІ FORGE/FABRIC-МОДИ під
	// виглядом плагінів. Modrinth — єдине надійне джерело для плагінів тут.
	if projectType != "plugin" && (source == "" || source == "all" || source == "curseforge") {
		wg.Add(1)
		go func() {
			defer crashguard.Recover("browser_api.go:254")
			defer wg.Done()
			cf := a.BrowserSearchCurseForge(query, projectType, mcVersion, loader, categories, sort, limit, offset, "")
			res.CurseForge = cf.CurseForge
			res.CFDisabled = cf.CFDisabled
			res.CFError = cf.CFError
		}()
	}
	wg.Wait()

	if len(res.CurseForge) > 0 && len(res.Modrinth) > 0 {
		filtered := res.CurseForge[:0]
		for _, entry := range res.CurseForge {
			if browserEntryDuplicateName(entry.Name, res.Modrinth) {
				continue
			}
			filtered = append(filtered, entry)
		}
		res.CurseForge = filtered
	}
	return res
}

// parseBrowserCategories розбирає кома-розділений список тегів-категорій
// («меблі,декор» → ["меблі", "декор"]) — Modrinth-категорії фільтруються
// за назвою/slug (Modrinth /search facets так влаштовані офіційно).
func parseBrowserCategories(categories string) []string {
	var cats []string
	for _, c := range strings.Split(categories, ",") {
		c = strings.TrimSpace(c)
		if c != "" {
			cats = append(cats, c)
		}
	}
	return cats
}

// parseBrowserCategoryIDs розбирає кома-розділений список ЧИСЛОВИХ ID
// категорій CurseForge («123,456» → [123, 456]) — на відміну від Modrinth,
// CurseForge Search API фільтрує за категорією виключно за числовим
// categoryId(s), не за назвою тегу (див. BrowserSearchCurseForge). Нечислові
// або порожні шматки мовчки пропускаються (захист від застарілого стану
// фронтенду, що ще міг передати назву замість ID).
func parseBrowserCategoryIDs(categoryIds string) []int {
	var ids []int
	for _, c := range strings.Split(categoryIds, ",") {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if id, err := strconv.Atoi(c); err == nil && id > 0 {
			ids = append(ids, id)
		}
	}
	return ids
}

// BrowserSearchModrinth — пошук ЛИШЕ на Modrinth, незалежно від CurseForge.
// Повертає результат одразу по готовності Modrinth-запиту, не чекаючи на
// CurseForge (див. ContentBrowser.svelte: викликається паралельно з
// BrowserSearchCurseForge, кожен рендериться на екрані щойно готовий).
//
// Якщо задано mcVersion — фільтр сумісності НЕ блокує видачу: перевірка
// сумісності запускається у фоні й дошльоє результат подіями
// "browser:compat" (див. filterCompatibleEntriesAsync), а тут повертаються
// ВСІ хіти одразу з CompatibilityUnconfirmed=true, щоб список був видно
// негайно замість порожнього екрана на секунди перевірки.
func (a *App) BrowserSearchModrinth(query, projectType, mcVersion, loader, categories, sort string, client, server, openSource bool, limit, offset int, packID string) (res BrowserSearchResult) {
	defer func() {
		if r := recover(); r != nil {
			res.ModrinthError = fmt.Sprintf("BrowserSearchModrinth panic: %v\n%s", r, debug.Stack())
		}
	}()
	res.Modrinth = []model.BrowserEntry{}
	if limit <= 0 {
		limit = 30
	}
	if offset < 0 {
		offset = 0
	}
	cats := parseBrowserCategories(categories)
	// extraLoaders — напр. "fabric" для Forge/NeoForge-збірки зі
	// встановленим Sinytra Connector (див. extraLoadersForPack): такі
	// збірки реально можуть запускати Fabric-моди, тож пошук має їх
	// показувати поряд зі "своїм" контентом, а не ховати повністю.
	extraLoaders := a.extraLoadersForPack(packID, loader)

	// Розумний пошук («меблі» → додає furniture/decoration до запиту) —
	// лише на ПЕРШІЙ сторінці (offset=0): на сторінці 2+ гравець уже
	// бачив і прийняв розширений запит (той самий query приходить знову
	// з фронтенду при пагінації), повторне розширення того самого рядка
	// нешкідливе, але тут явно з міркувань чистоти — Expand ідемпотентний
	// для вже розширеного рядка (жодне ключове слово словника вдруге не
	// збігається з "меблі furniture decoration decor" як ЦІЛОЮ фразою).
	smartQuery, smartTerms := query, []string(nil)
	if expanded, matched, terms := browsersearch.Expand(query); matched {
		smartQuery, smartTerms = expanded, terms
	}

	entries, total, err := a.modrinth.SearchBrowser(api.BrowserSearchParams{
		Query:        smartQuery,
		ProjectType:  projectType,
		GameVersion:  mcVersion,
		Loader:       loader,
		ExtraLoaders: extraLoaders,
		Categories:   cats,
		Sort:         sort,
		Client:       client,
		Server:       server,
		OpenSource:   openSource,
		Limit:        limit,
		Offset:       offset,
	})
	if err != nil {
		// Мережева помилка (немає інтернету/DNS/тайм-аут) — людською мовою,
		// щоб браузер контенту показував зрозуміле повідомлення, а не
		// порожній список чи спінер, що крутиться вічно.
		res.ModrinthError = neterr.Friendly(err, "завантажити список контенту").Error()
		return res
	}
	// Каскад: розширений запит (оригінал + терміни) дав 0 результатів —
	// оригінальне слово (напр. неанглійське, якого немає в жодному описі)
	// могло "засмітити" повнотекстовий пошук лише шумом. Пробуємо ще раз
	// ЛИШЕ англійськими термінами словника, без оригінального слова.
	if total == 0 && len(entries) == 0 && len(smartTerms) > 0 && smartQuery != query {
		fallbackQuery := strings.Join(smartTerms, " ")
		if fbEntries, fbTotal, fbErr := a.modrinth.SearchBrowser(api.BrowserSearchParams{
			Query:        fallbackQuery,
			ProjectType:  projectType,
			GameVersion:  mcVersion,
			Loader:       loader,
			ExtraLoaders: extraLoaders,
			Categories:   cats,
			Sort:         sort,
			Client:       client,
			Server:       server,
			OpenSource:   openSource,
			Limit:        limit,
			Offset:       offset,
		}); fbErr == nil && len(fbEntries) > 0 {
			entries, total = fbEntries, fbTotal
		}
	}
	if smartTerms != nil {
		res.SmartSearchTerms = smartTerms
	}
	res.TotalHits = total
	res.HasMore = offset+len(entries) < total

	if mcVersion != "" {
		// Modrinth /search facets вже фільтрують за (versions, loaders) на
		// рівні API — additional post-filter лишається як гарантія для
		// мультизавантажувальних проєктів (див. filterCompatibleEntries),
		// але позначає елементи як «підтверджується» і не блокує видачу:
		// повертаємо ввесь список одразу, помічений unconfirmed, і
		// перевірку сумісності досилаємо подіями у фоні.
		// ШВИДКІСТЬ: Modrinth /search вже відфільтрував видачу фасетами
		// versions:MC + categories:loader (сервер гарантує, що в кожного
		// хіта є версія під цю пару). Додаткова перевірка по запиту на
		// КОЖЕН хіт (30 запитів на сторінку) давала лише рідкісні
		// збіги для мультилоадерних проєктів, але забивала пул з'єднань
		// і rate-limit Modrinth (300/хв), через що решта запитів
		// (картинки, версії, наступна сторінка) гальмувала. Так само
		// робить GDLauncher — довіряє фасетам. Тож для Modrinth
		// повертаємо список як є, без «перевіряється» і без фону.
		// Явна перевірка все одно відбувається при відкритті сторінки
		// проєкту / натисканні «Встановити».
		res.Modrinth = entries
	} else {
		res.Modrinth = entries
	}
	return res
}

// BrowserSearchCurseForge — пошук ЛИШЕ на CurseForge, незалежно від
// Modrinth. Повертає результат щойно CurseForge відповість (rate-limited
// до 4 запитів/сек — див. cfRateLimiter), не чекаючи на Modrinth і не
// блокуючись на перевірці сумісності кожного хіту (та сама логіка
// "покажи зараз, підтверди у фоні", що й BrowserSearchModrinth).
func (a *App) BrowserSearchCurseForge(query, projectType, mcVersion, loader, categoryIds, sort string, limit, offset int, packID string) (res BrowserSearchResult) {
	defer func() {
		if r := recover(); r != nil {
			res.CFError = fmt.Sprintf("BrowserSearchCurseForge panic: %v\n%s", r, debug.Stack())
		}
	}()
	res.CurseForge = []model.BrowserEntry{}
	if limit <= 0 {
		limit = 30
	}
	if !custompack.CurseForgeAvailable() {
		res.CFDisabled = true
		return res
	}
	// ВАЖЛИВО (баг фільтрації категорій CurseForge): раніше сюди приходив
	// той самий спільний рядок НАЗВ категорій, що й для Modrinth
	// (selectedCategories.join(',') на фронтенді), і категорія
	// відсіювалась ПІСЛЯ пошуку, порівнянням назви (cfHasAnyCategory) —
	// вже на видачі, обрізаній до `limit` (типово 30) НЕФІЛЬТРОВАНИХ
	// найпопулярніших хітів. Якщо обраний тег не входив у ці 30 —
	// результат ставав порожнім, хоча реально під тегом могло бути багато
	// сумісних проєктів. Тепер фронтенд шле окремі ЧИСЛОВІ ID категорій
	// CurseForge (BrowserCategory.cfId), і вони йдуть прямо у
	// categoryId(s) офіційного /mods/search — сервер сам повертає лише
	// проєкти цього тегу, і `limit` ріже вже відфільтровану видачу.
	cfCategoryIDs := parseBrowserCategoryIDs(categoryIds)

	// Розумний пошук — той самий словник, що для Modrinth (browsersearch
	// пакет спільний для обох джерел, тема «меблі» означає те саме
	// незалежно від того, де саме шукаємо).
	smartQuery, smartTerms := query, []string(nil)
	if expanded, matched, terms := browsersearch.Expand(query); matched {
		smartQuery, smartTerms = expanded, terms
	}

	// Той самий тип-залежний фільтр лоадера, що й для Modrinth:
	// ресурспаки/датапаки/шейдери не фільтруються за лоадером збірки.
	// ВАЖЛИВО: передаємо gameVersion+modLoaderType прямо у facet-параметри
	// CurseForge search API (SearchProjects) — раніше сумісність
	// перевірялась ЛИШЕ пост-фактум окремим запитом GetModFiles на КОЖЕН
	// хіт (filterCompatibleEntries), що для 30 хітів означало ще ~30
	// додаткових запитів крізь rate limiter 4/с (~7-8с лише на фільтр,
	// і мережевий збій одного запиту прибирав хіт зі списку — звідси
	// "постійно немає сумісних"). Тепер API одразу повертає лише
	// сумісні за версією/лоадером результати, і додаткова перевірка
	// (нижче) лишається дешевою підстраховкою для мультизавантажувальних
	// модів, а не основним фільтром.
	cfLoader := effectiveLoaderForType(projectType, loader)
	// extraLoaders — "fabric" для Forge/NeoForge-збірки зі встановленим
	// Sinytra Connector (див. extraLoadersForPack). На відміну від
	// Modrinth, CurseForge Search API приймає лише ОДИН modLoaderType за
	// запит (немає OR-фасету) — тож для кожного додаткового лоадера
	// робимо ОКРЕМИЙ паралельний запит і мерджимо унікальні результати
	// (за ProjectID) з основним, замість того щоб ховати весь Fabric-
	// контент від збірки, яка насправді може його запускати.
	extraLoaders := a.extraLoadersForPack(packID, loader)
	entries, total, err := a.cfClient().SearchProjectsPage(smartQuery, projectType, mcVersion, cfLoader, sort, limit, offset, cfCategoryIDs)
	if err != nil {
		res.CFError = err.Error()
		return res
	}
	// Той самий каскадний fallback, що для Modrinth: розширений запит не
	// дав нічого — пробуємо ЛИШЕ англійськими термінами словника.
	if len(entries) == 0 && len(smartTerms) > 0 && smartQuery != query {
		fallbackQuery := strings.Join(smartTerms, " ")
		if fbEntries, fbTotal, fbErr := a.cfClient().SearchProjectsPage(fallbackQuery, projectType, mcVersion, cfLoader, sort, limit, offset, cfCategoryIDs); fbErr == nil && len(fbEntries) > 0 {
			entries, total = fbEntries, fbTotal
		}
	}
	if smartTerms != nil {
		res.SmartSearchTerms = smartTerms
	}
	seenIDs := make(map[int]bool, len(entries))
	for _, h := range entries {
		seenIDs[h.ID] = true
	}
	for _, extra := range extraLoaders {
		extraQuery := smartQuery
		if smartQuery == "" {
			extraQuery = query
		}
		extraEntries, extraTotal, extraErr := a.cfClient().SearchProjectsPage(extraQuery, projectType, mcVersion, extra, sort, limit, offset, cfCategoryIDs)
		if extraErr != nil {
			continue // додаткове джерело другорядне — мережева помилка не має гасити основний пошук
		}
		if extraTotal > total {
			total = extraTotal
		}
		for _, h := range extraEntries {
			if seenIDs[h.ID] {
				continue
			}
			seenIDs[h.ID] = true
			entries = append(entries, h)
		}
	}
	cfEntries := make([]model.BrowserEntry, 0, len(entries))
	for _, h := range entries {
		cfEntries = append(cfEntries, custompack.CFToBrowserEntry(h, projectType))
	}

	// ШВИДКІСТЬ: gameVersion+modLoaderType уже передані прямо в CurseForge
	// /mods/search (див. вище) — сервер повертає лише сумісні проєкти.
	// Пост-перевірка кожного хіта окремим запитом GetModFiles упиралась у
	// rate limiter 4 req/s (30 хітів ≈ 8 с фонового трафіку, який ще й
	// блокував решту запитів). Довіряємо серверному фільтру; точна
	// перевірка відбувається на сторінці проєкту / при встановленні.
	res.CurseForge = cfEntries
	res.CFTotalHits = total
	res.CFHasMore = offset+limit < total
	return res
}

// filterCompatibleEntries перевіряє кожен хіт пошуку на реальну сумісність
// з (mcVersion, loader) — чи має проєкт хоч одну ВЕРСІЮ під цю пару
// (а не лише якусь версію цієї MC під іншим лоадером). Лоадер застосовується
// лише для типів, де він має зміст (mod/modpack); для ресурспаків/шейдерів/
// датапаків — лише версія (loader порожній, див. effectiveLoaderForType).
// Перевірки йдуть паралельно (пул з 6 воркерів) — додатковий час пошуку з
// фільтром збірки лишається в межах ~1 с. Мережева помилка перевірки
// НЕ викидає хіт (не можемо підтвердити — не видаляємо); викидає лише
// підтверджена несумісність (порожній список сумісних версій).
func (a *App) filterCompatibleEntries(entries []model.BrowserEntry, projectType, mcVersion, loader string, extraLoaders []string) []model.BrowserEntry {
	if len(entries) == 0 {
		return entries
	}
	if loader != "" && !contentLoaderApplies(projectType) {
		loader = "" // ресурспак/шейдер/датапак — лоадер неважливий
		extraLoaders = nil
	}
	// ВАНІЛЬНА ЗБІРКА НЕ ПРИЙМАЄ МОДИ: для mod/modpack vanilla-лоадер
	// лишається як є — фільтр `browserEntryCompatibility` перевірить чи є
	// у проєкту версії під vanilla (немає → мод відсіюється). Раніше
	// vanilla завжди очищувався до "" (theseus instance_matches_targets),
	// через що будь-який мод вважався «сумісним» з ванільною збіркою.
	loader = strings.ToLower(loader)

	keep := make([]bool, len(entries))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 6)
	for i, e := range entries {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, e model.BrowserEntry) {
			defer crashguard.Recover("browser_api.go:571")
			defer wg.Done()
			defer func() { <-sem }()
			ok, _ := a.browserEntryCompatibility(e, mcVersion, loader, extraLoaders)
			keep[i] = ok
		}(i, e)
	}
	wg.Wait()

	out := make([]model.BrowserEntry, 0, len(entries))
	for i, e := range entries {
		if keep[i] {
			e.CompatibilityUnconfirmed = false
			out = append(out, e)
		}
	}
	return out
}

// ── Асинхронна перевірка сумісності («не чекати, показати зараз») ──────
// filterCompatibleEntriesAsync — та сама перевірка, що filterCompatibleEntries,
// але НЕ блокує видачу результатів пошуку. Пошук (BrowserSearchModrinth /
// BrowserSearchCurseForge) повертає ВЕСЬ список хітів одразу, позначений
// CompatibilityUnconfirmed=true; ця функція запускається у фоні (виклик не
// чекає на неї) і шле подію "browser:compat" на кожен хіт по готовності —
// картка в ContentBrowser.svelte або знімає позначку "перевіряється", або
// ховається (якщо підтверджено несумісний). Раніше фронтенд бачив список
// лише ПІСЛЯ того, як усі N хітів пройшли перевірку (по 6 паралельно,
// впираючись у rate limiter CurseForge 4 запити/с) — тепер список видно
// одразу, а статус сумісності «дотягується».
func (a *App) filterCompatibleEntriesAsync(entries []model.BrowserEntry, projectType, mcVersion, loader string, extraLoaders []string) {
	if len(entries) == 0 || mcVersion == "" {
		return
	}
	if loader != "" && !contentLoaderApplies(projectType) {
		loader = ""
		extraLoaders = nil
	}
	loader = strings.ToLower(loader)
	go func() {
		defer crashguard.Recover("browser_api.go:610")
		sem := make(chan struct{}, 6)
		var wg sync.WaitGroup
		for _, e := range entries {
			wg.Add(1)
			sem <- struct{}{}
			go func(e model.BrowserEntry) {
				defer crashguard.Recover("browser_api.go:616")
				defer wg.Done()
				defer func() { <-sem }()
				ok, confirmed := a.browserEntryCompatibilityCached(e, mcVersion, loader, extraLoaders)
				a.emit("browser:compat", map[string]interface{}{
					"source":      e.Source,
					"projectId":   e.ProjectID,
					"compatible":  ok,
					"unconfirmed": !confirmed,
				})
			}(e)
		}
		wg.Wait()
	}()
}

// ── Кеш перевірки сумісності (projectID+mcVersion+loader → результат) ──
// Той самий проєкт часто зустрічається повторно: різні сторінки пагінації
// одного пошуку, повторний запит після зміни сортування, або той самий
// мод у видачі і "Рекомендованих", і звичайного пошуку. Без кешу кожне
// таке повторення — новий мережевий запит крізь rate limiter CurseForge
// (4/с), хоча відповідь для (projectID, mcVersion, loader) не змінюється
// за час однієї сесії браузера. TTL — 10 хвилин: досить, щоб покрити
// типову сесію перегляду, і достатньо коротко, щоб не показувати
// застарілу сумісність, якщо автор проєкту щойно викотив нову версію.
type compatCacheEntry struct {
	ok, confirmed bool
	at            time.Time
}

var (
	compatCacheMu sync.Mutex
	compatCache   = map[string]compatCacheEntry{}
)

const compatCacheTTL = 10 * time.Minute

func compatCacheKey(source, projectID, mcVersion, loader string, extraLoaders []string) string {
	key := source + "|" + projectID + "|" + mcVersion + "|" + loader
	if len(extraLoaders) > 0 {
		key += "|+" + strings.Join(extraLoaders, ",")
	}
	return key
}

func (a *App) browserEntryCompatibilityCached(e model.BrowserEntry, mcVersion, loader string, extraLoaders []string) (ok bool, confirmed bool) {
	key := compatCacheKey(e.Source, e.ProjectID, mcVersion, loader, extraLoaders)
	compatCacheMu.Lock()
	if c, found := compatCache[key]; found && time.Since(c.at) < compatCacheTTL {
		compatCacheMu.Unlock()
		return c.ok, c.confirmed
	}
	compatCacheMu.Unlock()

	ok, confirmed = a.browserEntryCompatibility(e, mcVersion, loader, extraLoaders)

	// Не кешуємо непідтверджені результати (мережевий збій) — інакше
	// тимчасовий таймаут «застряг» би в кеші на 10 хвилин, і наступний
	// (можливо вже успішний) запит користувача отримав би ту саму
	// непевну відповідь замість шансу перевірити ще раз.
	if confirmed {
		compatCacheMu.Lock()
		compatCache[key] = compatCacheEntry{ok: ok, confirmed: confirmed, at: time.Now()}
		compatCacheMu.Unlock()
	}
	return ok, confirmed
}

// browserEntryCompatible — чи має проєкт хоч одну версію під (mcVersion,
// loader). Modrinth: /project/{id}/version?loaders=[...]&game_versions=[...]
// (сервер фільтрує версії одразу). CurseForge: /mods/{id}/files з локальним
// фільтром за gameVersion+modLoader (GetModFiles робить саме це).
func (a *App) browserEntryCompatible(e model.BrowserEntry, mcVersion, loader string, extraLoaders []string) bool {
	ok, _ := a.browserEntryCompatibility(e, mcVersion, loader, extraLoaders)
	return ok
}

// browserEntryCompatibility — те саме, що browserEntryCompatible, але
// друге повернене значення відрізняє "підтверджено сумісний" від "мережева
// помилка перевірки — залишаємо fail-open, але позначаємо як
// непідтверджений" (див. BrowserEntry.CompatibilityUnconfirmed). Без цього
// розрізнення елемент, чию сумісність не вдалось перевірити через
// нестабільну мережу, у списку виглядав БУКВАЛЬНО так само, як гарантовано
// сумісний — кнопка "Встановити" активна без жодного попередження, і про
// реальну несумісність (якщо вона таки є) користувач дізнавався лише після
// кліку.
//
// extraLoaders — додаткові лоадери, під які проєкт теж вважається
// сумісним (напр. "fabric", коли в Forge/NeoForge-збірці стоїть Sinytra
// Connector — див. BrowserSearchModrinth/CurseForge). Перевіряються ЛИШЕ
// якщо основний loader не дав жодної сумісної версії — типовий випадок
// (проєкт під власний лоадер збірки) лишається одним мережевим запитом,
// без подвоєння трафіку.
func (a *App) browserEntryCompatibility(e model.BrowserEntry, mcVersion, loader string, extraLoaders []string) (ok bool, confirmed bool) {
	ok, confirmed = a.browserEntryCompatibilitySingle(e, mcVersion, loader)
	if ok || !confirmed {
		return ok, confirmed
	}
	for _, extra := range extraLoaders {
		extra = strings.TrimSpace(extra)
		if extra == "" || strings.EqualFold(extra, loader) {
			continue
		}
		extraOk, extraConfirmed := a.browserEntryCompatibilitySingle(e, mcVersion, extra)
		if extraOk {
			return true, true
		}
		if !extraConfirmed {
			// Мережева помилка на альтернативному лоадері — не можемо
			// підтвердити фінальний "несумісний" з упевненістю.
			confirmed = false
		}
	}
	return false, confirmed
}

// browserEntryCompatibilitySingle перевіряє сумісність з ОДНИМ конкретним
// лоадером (без урахування extraLoaders) — базова перевірка, якою
// послідовно користується browserEntryCompatibility.
func (a *App) browserEntryCompatibilitySingle(e model.BrowserEntry, mcVersion, loader string) (ok bool, confirmed bool) {
	// «Не підтверджено» більше не існує: кожен виклик повторюється до
	// 4 разів із наростаючою паузою (мережевий збій/таймаут/429 — це
	// тимчасово), а якщо відповіді таки нема — рішення приймається за
	// даними самого хіта пошуку (див. compatFromEntryMeta), а не «не
	// знаю». Тож результат завжди «сумісний» або «несумісний».
	const attempts = 4
	switch e.Source {
	case "modrinth":
		var lastErr error
		for i := 0; i < attempts; i++ {
			vs, err := a.modrinth.GetProjectVersions(e.ProjectID, mcVersion, loader)
			if err == nil {
				return len(vs) > 0, true
			}
			lastErr = err
			time.Sleep(time.Duration(i+1) * 400 * time.Millisecond)
		}
		_ = lastErr
		return compatFromEntryMeta(e, mcVersion, loader), true
	case "curseforge":
		id, err := strconv.Atoi(e.ProjectID)
		if err != nil {
			return false, true
		}
		for i := 0; i < attempts; i++ {
			files, err := a.cfClient().GetModFiles(id, mcVersion, loader)
			if err == nil {
				return len(files) > 0, true
			}
			// Немає API-ключа — повтор нічого не змінить.
			if strings.Contains(err.Error(), "API-ключ") {
				break
			}
			time.Sleep(time.Duration(i+1) * 400 * time.Millisecond)
		}
		return compatFromEntryMeta(e, mcVersion, loader), true
	default:
		return true, true
	}
}

// compatFromEntryMeta — рішення про сумісність за даними, які вже є у
// хіті пошуку (Modrinth/CurseForge повертають лоадери проєкту), коли
// мережева перевірка версій недоступна. Пошук і так пройшов фасети
// (versions:MC + categories:loader) на боці API, тож хіт, який дійшов
// сюди, для Modrinth уже відфільтрований за версією; лишається звірити
// лоадер. Порожній список лоадерів = ресурс лоадер-агностичний.
func compatFromEntryMeta(e model.BrowserEntry, mcVersion, loader string) bool {
	if loader == "" || len(e.Loaders) == 0 {
		return true
	}
	for _, l := range e.Loaders {
		if strings.EqualFold(l, loader) {
			return true
		}
	}
	return false
}

// browserEntryDuplicateName — чи є в списку проєкт з такою ж нормалізованою
// назвою (треілінгові пробіли/регістр ігноруються).
func browserEntryDuplicateName(name string, list []model.BrowserEntry) bool {
	if name == "" {
		return false
	}
	target := strings.ToLower(strings.TrimSpace(name))
	for _, e := range list {
		if strings.ToLower(strings.TrimSpace(e.Name)) == target {
			return true
		}
	}
	return false
}

// BrowserCategory — один тег-категорія у фільтрі браузера (джерело +
// назва + іконка), щоб показувати теги окремо по Modrinth/CurseForge
// з іконками, як SearchSidebarFilter у веб-апці Modrinth.
type BrowserCategory struct {
	Source string `json:"source"` // modrinth | curseforge
	Name   string `json:"name"`
	Icon   string `json:"icon"`
	// Header — група категорії (як header у Modrinth /tag/category/{type}:
	// "category" | "feature" | "performance-impact" | "resolutions" тощо).
	// Для CurseForge визначається евристичним мапінгом назви (cfCategoryHeader),
	// щоб «Оптимізація» з обох джерел потрапляла в ОДНУ групу фільтра,
	// незважаючи на те, що офіційних headers у CurseForge немає.
	Header string `json:"header"`
	// CFID — числовий ID категорії CurseForge (0 для Modrinth-категорій).
	// Фронтенд передає його назад окремо від Modrinth-категорій (див.
	// BrowserSearchCurseForge/curseforgeCategoryIds) — CurseForge Search
	// API фільтрує за categoryId(s), не за назвою тегу.
	CFID int `json:"cfId"`
}

// cfCategoryToHeader — евристичний мапінг НАЗВИ категорії CurseForge на
// header-групу Modrinth. CurseForge не має офіційного групування тегів
// (усі категорії плоскі), тож відповідність будуємо по семантиці назви:
// «Optimization» / «Performance» у CurseForge = «performance-impact» у
// Modrinth, «Adventure and RPG» = «category», «Server Utility» = «feature»
// тощо. Невідомі категорії лишаються в загальній групі "category".
func cfCategoryToHeader(name string) string {
	n := strings.ToLower(name)
	switch {
	case strings.Contains(n, "optimiz"), strings.Contains(n, "performance"), strings.Contains(n, "lightweight"):
		return "performance-impact"
	case strings.Contains(n, "api"), strings.Contains(n, "library"), strings.Contains(n, "utility"),
		strings.Contains(n, "management"), strings.Contains(n, "server utility"),
		strings.Contains(n, "world gen"), strings.Contains(n, "worldgen"),
		strings.Contains(n, "storage"), strings.Contains(n, "food"), strings.Contains(n, "farming"):
		return "feature"
	case strings.Contains(n, "resolution"), strings.Contains(n, "8x"), strings.Contains(n, "16x"),
		strings.Contains(n, "32x"), strings.Contains(n, "64x"), strings.Contains(n, "128x"),
		strings.Contains(n, "256x"), strings.Contains(n, "512x"):
		return "resolutions"
	default:
		return "category"
	}
}

// GetBrowserCategories повертає теги-категорії для типу контенту з обох
// джерел (Modrinth /tag/category/{type} + CurseForge /categories),
// відфільтровані до підтримуваного класу (для CurseForge), і згруповані
// по Header — фронтенд рендерить groups-акордіони (Категорія/Функція/
// Вплив на продуктивність/Роздільна здатність), як SearchSidebarFilter
// у веб-апці Modrinth, де ОДНА і та сама «Оптимізація» видно як одну
// групу незалежно від того, з якого сервісу вона прийшла.
func (a *App) GetBrowserCategories(projectType string) []BrowserCategory {
	out := []BrowserCategory{}

	if cats, err := a.modrinth.GetCategories(projectType); err == nil {
		for _, c := range cats {
			if c.Name == "" {
				continue
			}
			header := c.Header
			if header == "" {
				header = "category"
			}
			out = append(out, BrowserCategory{Source: "modrinth", Name: c.Name, Icon: c.Icon, Header: header})
		}
	}

	// "plugin" (Bukkit/Paper/Velocity) пропускає CurseForge зовсім: там
	// немає окремого classId для цього типу контенту в нашій схемі —
	// cfClassIDFor мовчки впав би на classId модів (6), показавши
	// категорії ЗВИЧАЙНИХ МОДІВ під вкладкою «Плагіни» (та сама причина,
	// що й у BrowserSearch вище).
	if custompack.CurseForgeAvailable() && projectType != "plugin" {
		if cats, err := a.cfClient().GetCategories(); err == nil {
			for _, c := range cats {
				// Вкладені категорії конкретного класу контенту (classId =
				// той самий, що cfClassID для типу).
				if c.ClassID == cfClassIDFor(projectType) {
					out = append(out, BrowserCategory{
						Source: "curseforge", Name: c.Name, Icon: c.IconURL,
						Header: cfCategoryToHeader(c.Name), CFID: c.ID,
					})
				}
			}
		}
	}
	return out
}

// BrowserGameVersion — версія гри для фільтра браузера (з /tag/game_version
// Modrinth або fallback з Mojang-маніфесту).
type BrowserGameVersion struct {
	ID          string `json:"id"`
	VersionType string `json:"versionType"`
	Date        string `json:"date"`
	Major       bool   `json:"major"`
}

// GetBrowserGameVersions повертає список версій гри для фільтра браузера.
// Пріоритет — теги Modrinth /tag/game_version (лише версії з контентом,
// вже в Modrinth-порядку: найновіші спочатку, "major" й release окремо).
// Fallback — повний Mojang-маніфест (як було раніше), якщо теги недоступні.
func (a *App) GetBrowserGameVersions() []BrowserGameVersion {
	if gv, err := a.modrinth.GetGameVersionTags(); err == nil && len(gv) > 0 {
		out := make([]BrowserGameVersion, 0, len(gv))
		for _, g := range gv {
			if g.Version == "" {
				continue
			}
			out = append(out, BrowserGameVersion{ID: g.Version, VersionType: g.VersionType, Date: g.Date, Major: g.Major})
		}
		return out
	}
	// Fallback: Mojang release-маніфест (всі типи — release/snapshot/...) —
	// більше версій, порядок новіші-спочатку.
	if list, err := a.versionsProv.GetMCVersions(); err == nil {
		out := make([]BrowserGameVersion, 0, len(list))
		for _, v := range list {
			out = append(out, BrowserGameVersion{ID: v.ID, VersionType: v.Type, Date: v.ReleaseTime})
		}
		return out
	}
	return []BrowserGameVersion{}
}

// BrowserLoaderTag — лоадер для фільтра браузера (з /tag/loader Modrinth).
type BrowserLoaderTag struct {
	Name  string   `json:"name"`
	Icon  string   `json:"icon"`
	Types []string `json:"types"`
}

// supportedBrowserLoaders — лоадери, які ЛАУНЧЕР ФІЗИЧНО ВМІЄ встановлювати
// (той самий канонічний перелік, що MOD_CORES/PLUGIN_CORES на фронтенді:
// frontend/src/lib/servers/constants.ts — синхронізувати обидва місця при
// зміні). Modrinth /tag/loader повертає ВСІ лоадери, що є на маркетплейсі
// (litemod, rift, canvas, folia, sponge, cauldron і т.д.) — без цього
// фільтра юзер бачив у списку «Лоадер» варіанти, обравши які, застрягав
// би з порожньою видачею або встановленням, яке лаунчер не вміє провести.
// resourcepack/shader/datapack — лоадер там не про мод/плагін-лоадер, а
// про сумісність (vanilla/iris/optifine), тому не звужуємо, лише mod/plugin.
var supportedBrowserLoaders = map[string]bool{
	// mod (MOD_CORES)
	"fabric": true, "forge": true, "neoforge": true, "quilt": true,
	// plugin (PLUGIN_CORES)
	"paper": true, "purpur": true, "velocity": true,
	// без прив'язки до конкретного типу — сумісність, не мод/плагін-лоадер
	"vanilla": true, "iris": true, "optifine": true,
}

// GetBrowserLoaders повертає список лоадерів для фільтра браузера з
// /tag/loader Modrinth (з SupportedProjectTypes), щоб фільтр показував лише
// лоадери релевантні поточному типу контенту, як у Modrinth app. Додатково
// відсіює лоадери типу mod/plugin, які лаунчер не вміє встановлювати (див.
// supportedBrowserLoaders) — для resourcepack/shader/datapack обмеження не
// застосовується, там типи лоадера інші. Fallback — статичний набір тих,
// що лаунчер гарантовано знає.
func (a *App) GetBrowserLoaders() []BrowserLoaderTag {
	if ls, err := a.modrinth.GetLoaderTags(); err == nil && len(ls) > 0 {
		out := make([]BrowserLoaderTag, 0, len(ls))
		for _, l := range ls {
			if l.Name == "" {
				continue
			}
			isModOrPlugin := false
			for _, tp := range l.SupportedProjectTypes {
				if tp == "mod" || tp == "plugin" {
					isModOrPlugin = true
					break
				}
			}
			if isModOrPlugin && !supportedBrowserLoaders[strings.ToLower(l.Name)] {
				continue
			}
			out = append(out, BrowserLoaderTag{Name: l.Name, Icon: l.Icon, Types: l.SupportedProjectTypes})
		}
		return out
	}
	return []BrowserLoaderTag{
		{Name: "fabric", Types: []string{"mod"}},
		{Name: "forge", Types: []string{"mod"}},
		{Name: "neoforge", Types: []string{"mod"}},
		{Name: "quilt", Types: []string{"mod"}},
		{Name: "vanilla", Types: []string{"mod", "shader", "datapack"}},
		{Name: "iris", Types: []string{"shader"}},
		{Name: "optifine", Types: []string{"shader"}},
		{Name: "paper", Types: []string{"plugin"}},
		{Name: "purpur", Types: []string{"plugin"}},
		{Name: "velocity", Types: []string{"plugin"}},
	}
}

// versionDownloadURLs — впорядкований список адрес файлу версії. Modrinth —
// одна адреса. CurseForge (порт cf_file_urls з Millida Launcher): downloadUrl з
// API (лише *.forgecdn.net), далі edge.forgecdn.net і mediafilez.forgecdn.net;
// downloadUrl=null (бан сторонніх завантажень / дірка у відповіді) більше не
// дає «версія X не має посилання на завантаження».
func (a *App) versionDownloadURLs(source, projectID string, v *BrowserVersionView) []string {
	if source == "curseforge" {
		fid, err := strconv.Atoi(v.ID)
		if err != nil {
			return nil
		}
		name := v.FileName
		if name == "" {
			name = v.VersionNumber
		}
		first := v.FileURL
		if first == "" {
			first = v.DownloadURL
		}
		return custompack.CurseForgeFileURLs(first, fid, name)
	}
	if v.FileURL != "" {
		return []string{v.FileURL}
	}
	if v.DownloadURL != "" {
		return []string{v.DownloadURL}
	}
	return nil
}

// downloadContentFileAny пробує адреси по черзі, доки одна не скачається.
func (a *App) downloadContentFileAny(urls []string, dest, projectID, packID, fileName string) error {
	var last error
	for _, u := range urls {
		if err := a.downloadContentFile(u, dest, projectID, packID, fileName); err != nil {
			last = err
			continue
		}
		return nil
	}
	if last == nil {
		last = fmt.Errorf("у файла немає жодного посилання на завантаження")
	}
	return last
}

// cfClassIDFor — той самий мапінг типу → CurseForge classId, що й
// cfClassID у custompack (дублюємо тут, щоб не експортувати).
func cfClassIDFor(projectType string) int {
	switch strings.ToLower(projectType) {
	case "modpack":
		return 4471
	case "resourcepack":
		return 12
	case "shader":
		return 6552
	case "datapack":
		return 6945
	default:
		return 6
	}
}

// ── Сторінка проєкту (модпаку) ──────────────────────────────────────────

// GetBrowserProjectDetail повертає повний опис проєкту для сторінки
// модпаку в браузері. source: modrinth | curseforge.
func (a *App) GetBrowserProjectDetail(source, projectID string) (*BrowserProjectDetail, error) {
	switch source {
	case "modrinth":
		d, err := a.modrinth.GetProjectDetail(projectID)
		if err != nil {
			return nil, err
		}
		// Автор проєкту: ID + ім'я + аватар + роль (щоб клік по автору
		// відкривав його профіль, як Modrinth UserPage).
		//
		// ВАЖЛИВО: раніше тут було ДВА виклики — GetProjectAuthor(d.ID) і
		// GetProjectAuthorFull(d.ID), а перший усередині просто кличе
		// другий. Тобто сторінка проєкту робила той самий запит
		// /project/{id}/members ДВІЧІ — зайві 150–200 мс і ще одна точка
		// відмови (мережа «блимає» → «не вдалося завантажити проєкт»).
		//
		// Учасники команди й організація — паралельно, як у Modrinth app
		// (get_team + get_organization): обидва потрібні для секції авторів.
		var (
			members []api.ModrinthTeamMember
			org     *api.ModrinthOrganization
			wg      sync.WaitGroup
		)
		wg.Add(1)
		go func() {
			defer wg.Done()
			members, _ = a.modrinth.GetProjectMembers(d.ID)
		}()
		if d.Organization != "" {
			wg.Add(1)
			go func() {
				defer wg.Done()
				org, _ = a.modrinth.GetOrganizationProfile(d.Organization)
			}()
		}
		wg.Wait()
		creators := modrinthCreators(org, members)
		// Головний автор (картки, «від …»): організація, інакше власник.
		var author, authorID, authorAvatar, authorURL string
		if len(creators) > 0 {
			c := creators[0]
			author, authorID, authorAvatar, authorURL = c.Name, c.ID, c.Avatar, c.URL
		} else if af, err := a.modrinth.GetProjectAuthorFull(d.ID); err == nil {
			author, authorID, authorAvatar, authorURL = af.Username, af.ID, af.AvatarURL, "https://modrinth.com/user/"+af.Username
		}
		out := &BrowserProjectDetail{
			Source:       "modrinth",
			ProjectID:    d.ID,
			Slug:         d.Slug,
			ProjectType:  d.ProjectType,
			Name:         d.Title,
			Description:  d.Description,
			Body:         d.Body,
			IconURL:      d.IconURL,
			Downloads:    d.Downloads,
			Followers:    d.Followers,
			Categories:   append(append([]string{}, d.Categories...), d.AdditionalCategories...),
			ProjectURL:   "https://modrinth.com/" + d.ProjectType + "/" + d.Slug,
			Published:    d.Published,
			Updated:      d.Updated,
			LicenseID:    d.License.ID,
			LicenseName:  d.License.Name,
			LicenseURL:   d.License.URL,
			ClientSide:   d.ClientSide,
			ServerSide:   d.ServerSide,
			IssuesURL:    d.IssuesURL,
			SourceURL:    d.SourceURL,
			DiscordURL:   d.DiscordURL,
			WikiURL:      d.WikiURL,
			Author:       author,
			AuthorID:     authorID,
			AuthorAvatar: authorAvatar,
			AuthorURL:    authorURL,
			Creators:     creators,
			// GameVersions/Loaders — тепер з /project (раніше лишались
			// порожніми для Modrinth, і сайдбар сумісності був порожнім).
			MCVersions: append([]string{}, d.GameVersions...),
			Loaders:    append([]string{}, d.Loaders...),
		}
		for _, g := range d.Gallery {
			out.Gallery = append(out.Gallery, BrowserGalleryImage{URL: g.URL, RawURL: g.RawURL, Title: g.Title, Featured: g.Featured})
		}
		return out, nil

	case "curseforge":
		if !custompack.CurseForgeAvailable() {
			return nil, fmt.Errorf("CurseForge API-ключ не налаштовано у цій збірці лаунчера")
		}
		id, err := strconv.Atoi(projectID)
		if err != nil {
			return nil, fmt.Errorf("CurseForge projectID має бути числом: %s", projectID)
		}
		d, err := a.cfClient().GetProjectDetail(id)
		if err != nil {
			return nil, err
		}
		// Опис тіла: якщо CurseForge віддає порожній довгий опис
		// (description) для проєкту — фолбек на короткий summary, щоб
		// сторінка контенту ніколи не лишалась без тексту.
		body := d.Description
		if strings.TrimSpace(body) == "" {
			body = d.Summary
		}
		out := &BrowserProjectDetail{
			Source:      "curseforge",
			ProjectID:   projectID,
			Slug:        d.Slug,
			ProjectType: projectTypeFromCFClass(d.ClassID),
			Name:        d.Name,
			Description: d.Summary,
			Body:        body,
			Downloads:   d.DownloadCount,
			ProjectURL:  d.Links.WebsiteURL,
			// MCVersions: d.GameVersions ЗМІШУЄ версії гри й назви лоадерів
			// (реальна поведінка CurseForge API — див. коментар на
			// cfExtractLoaderName) — тут потрібні САМЕ версії гри для
			// показу списку на сторінці проєкту, тож лоадери відфільтровано.
			MCVersions: custompack.CFVersionsOnly(d.GameVersions),
			Loaders:    d.ModLoaders,
			Published:  d.DateCreated,
			Updated:    d.DateModified,
			IssuesURL:  d.Links.IssuesURL,
			SourceURL:  d.Links.SourceURL,
			DiscordURL: d.Links.DiscordURL,
			WikiURL:    d.Links.WikiURL,
		}
		if d.Logo.URL != "" {
			out.IconURL = d.Logo.URL
		}
		for _, s := range d.Screenshots {
			out.Gallery = append(out.Gallery, BrowserGalleryImage{URL: s.URL, Title: s.Title})
		}
		for _, c := range d.Categories {
			out.Categories = append(out.Categories, c.Name)
		}
		// Автор: CurseForge не має endpoint-а профілю користувача, тож
		// беремо першого автора з authors[] (id + ім'я + url). Аватар з
		// CurseForge отримати неможливо — фронт покаже запасну літеру.
		if len(d.Authors) > 0 {
			out.Author = d.Authors[0].Name
			out.AuthorID = strconv.Itoa(d.Authors[0].ID)
			out.AuthorURL = d.Authors[0].URL
		}
		out.Creators = []BrowserCreator{}
		for _, au := range d.Authors {
			out.Creators = append(out.Creators, BrowserCreator{ID: strconv.Itoa(au.ID), Name: au.Name, URL: au.URL})
		}
		return out, nil

	default:
		return nil, fmt.Errorf("невідоме джерело: %s", source)
	}
}

func projectTypeFromCFClass(classID int) string {
	switch classID {
	case 4471:
		return "modpack"
	case 12:
		return "resourcepack"
	case 6552:
		return "shader"
	case 6945:
		return "datapack"
	default:
		return "mod"
	}
}

// GetBrowserProjectVersions повертає версії проєкту (сторінка модпаку /
// діалог «Оновити з джерела»), відфільтровані за версією гри та лоадером
// (порожні значення = без фільтра). Сортовані новішими першими.
// GetBrowserProjectVersions повертає версії проєкту. projectType — тип
// контенту (mod/modpack/resourcepack/shader/datapack): від нього залежить,
// чи застосовувати фільтр лоадера. ВАЖЛИВО: для ресурспаків/шейдерів/
// датапаків лоадер збірки (fabric/forge) НЕ передається в API — такі
// проєкти не мають лоадерів збірки (ресурспак однаково працює на будь-
// якому завантажувачі), і фільтр за ним повернув би порожній список.
func (a *App) GetBrowserProjectVersions(source, projectID, projectType, mcVersion, loader string) ([]BrowserVersionView, error) {
	loader = effectiveLoaderForType(projectType, loader)
	switch source {
	case "modrinth":
		versions, err := a.modrinth.GetProjectVersions(projectID, mcVersion, loader)
		if err != nil {
			return nil, err
		}
		out := make([]BrowserVersionView, 0, len(versions))
		for _, v := range versions {
			view := BrowserVersionView{
				ID:            v.ID,
				Name:          v.Name,
				VersionNumber: v.VersionNumber,
				VersionType:   v.VersionType,
				GameVersions:  v.GameVersions,
				Loaders:       v.Loaders,
				DatePublished: v.DatePublished,
				Changelog:     v.Changelog,
			}
			if len(v.Files) > 0 {
				// PrimaryFileIndex — Modrinth не гарантує, що позначений
				// "primary" файл (той самий архів/мод, що встановлюється)
				// іде першим у масиві. Раніше тут бралося Files[0] напряму,
				// через що у версій з кількома файлами (напр. складні
				// модпаки) можна було отримати URL побічного файлу і
				// зловити RuntimeError "не має посилання на архів модпаку".
				pf := v.Files[v.PrimaryFileIndex()]
				view.FileName = pf.Filename
				view.FileURL = pf.URL
				view.FileSize = pf.Size
				view.SHA1 = pf.Hashes.Sha1
			}
			out = append(out, view)
		}
		return out, nil

	case "curseforge":
		if !custompack.CurseForgeAvailable() {
			return nil, fmt.Errorf("CurseForge API-ключ не налаштовано у цій збірці лаунчера")
		}
		id, err := strconv.Atoi(projectID)
		if err != nil {
			return nil, fmt.Errorf("CurseForge projectID має бути числом: %s", projectID)
		}
		files, err := a.cfClient().GetModFiles(id, mcVersion, loader)
		if err != nil {
			return nil, err
		}
		out := make([]BrowserVersionView, 0, len(files))
		for _, f := range files {
			view := BrowserVersionView{
				ID:            strconv.Itoa(f.ID),
				Name:          f.FileName,
				VersionNumber: f.FileName,
				// VersionType з РЕАЛЬНОГО каналу CurseForge (releaseType:
				// 1=release/2=beta/3=alpha). Раніше лишався порожнім для всіх
				// CurseForge-файлів, через що pickBestBrowserVersion не міг
				// віддати перевагу стабільному релізу й брав найсвіжіший файл
				// за датою — включно з beta/alpha, випущеними після останнього
				// стабільного (реально: JEI 1.19.2/1.21.1 — перші 3-4 файли
				// beta, release нижче). Тепер список версій і авто-вибір
				// «останній сумісний» працюють за тим самим правилом
				// release → beta → alpha, що й для Modrinth.
				VersionType: custompack.CFReleaseTypeName(f.ReleaseType),
				// f.GameVersions з CurseForge змішує версії гри й назву
				// лоадера — тут потрібні лише версії гри для бейджа на
				// UI, f.ModLoaders (заповнений у GetModFiles) несе лоадер.
				GameVersions:  custompack.CFVersionsOnly(f.GameVersions),
				Loaders:       f.ModLoaders,
				DatePublished: f.FileDate,
				FileName:      f.FileName,
				DownloadURL:   f.DownloadURL,
				SHA1:          f.SHA1(),
			}
			out = append(out, view)
		}
		return out, nil

	default:
		return nil, fmt.Errorf("невідоме джерело: %s", source)
	}
}

// ── Сумісні збірки для встановлення контенту ────────────────────────────

// contentLoaderApplies — чи фільтрувати версії контенту за лоадером збірки.
// Порт instance_matches_targets з theseus (install_candidate.rs):
// contentLoaderApplies — чи перевіряти сумісність версії за ЛОАДЕРОМ:
//   - моди та модпаки: версія має підтримувати лоадер збірки;
//   - плагіни: версія має підтримувати ядро сервера (paper/purpur/velocity —
//     на Modrinth ці «лоадери» плагінів позначаються так само, як лоадери
//     модів, тому та сама перевірка застосовна без змін);
//   - ресурспаки / шейдери / датапаки: ЛОАДЕР ІГНОРУЄТЬСЯ — має збігатись
//     лише версія гри (ресурспаки лоадер-агностичні, шейдери живуть під
//     iris/optifine, датапаки — vanilla).
//
// РАНІШЕ усі типи фільтрувались за (mc, loader) збірки, через що
// ресурспак/датапак/шейдер у Fabric-збірці показувались як «несумісні»
// і зникали з діалогу встановлення — головний баг «перевірка сумісності
// неправильна».
func contentLoaderApplies(projectType string) bool {
	switch strings.ToLower(projectType) {
	case "mod", "modpack", "plugin":
		return true
	default:
		return false
	}
}

// effectiveLoaderForType — лоадер для фільтрації версій за типом контенту:
// для mod/modpack — як задано; для інших типів — порожній (без фільтра).
//
// ВАЖЛИВО (порт theseus instance_matches_targets): збірка з vanilla-лоадером
// приймає БУДЬ-ЯКИЙ мод. У theseus цей випадок описується як target.loader
// == "datapack" (ванильна збірка мітить лоадер datapack); у лаунчері —
// loader "vanilla". Передавати "vanilla" як фасет Modrinth-пошуку безглуздо
// (модуля з лоадером "vanilla" не існує) — версії поверталися б порожні,
// і жоден мод не був би "сумісний" з ваниллю.
func effectiveLoaderForType(projectType, loader string) string {
	if !contentLoaderApplies(projectType) {
		return ""
	}
	if loader == "vanilla" || loader == "datapack" {
		return ""
	}
	return loader
}

// GetBrowserCompatiblePacks повертає кастомні збірки, у які МОЖНА
// встановити контент проєкту. projectType — тип контенту (mod/modpack/
// resourcepack/shader/datapack): від нього залежить, чи фільтруємо за
// лоадером збірки (див. contentLoaderApplies). contentDir — цільова тека
// встановлення (mods / resourcepacks / shaderpacks / datapacks), за якою
// перевіряється «вже встановлено» (датапаки — ГЛОБАЛЬНО .minecraft/
// datapacks/, як Modrinth app, а не у світах).
//
// versionID — конкретна версія контенту, під яку фільтруються збірки
// (необов'язково, "" = будь-яка сумісна): діалог встановлення показує
// версію контенту, і збірки-кандидати мають підтримувати САМЕ її
// (gameVersions/loaders цієї версії), а не просто мати якусь сумісну.
// Це відповідь на запит «у діалозі встановлення обираєш версію контенту,
// і там видно лише збірки, що підтримують саме оцю версію».
func (a *App) GetBrowserCompatiblePacks(source, projectID, projectType, contentDir, versionID string) []BrowserCompatiblePack {
	packs := a.customPacks.List()
	if len(packs) == 0 {
		return nil
	}
	// Версія-фільтр (якщо задана): її gameVersions/loaders визначають
	// сумісність збірок. Дістаємо ОДИН раз для всього списку збірок.
	var version *BrowserVersionView
	if versionID != "" {
		if all, err := a.GetBrowserProjectVersions(source, projectID, projectType, "", ""); err == nil {
			for i := range all {
				if all[i].ID == versionID {
					version = &all[i]
					break
				}
			}
		}
		if version == nil {
			// Версію не знайшли — жодна збірка несумісна з нею.
			return nil
		}
	}

	// Шаурма-збірки з білого списку перевіряються паралельно з кастомними.
	var whitelisted []BrowserCompatiblePack
	var wlDone chan struct{}
	if projectType == "mod" {
		wlDone = make(chan struct{})
		go func() {
			defer close(wlDone)
			whitelisted = a.whitelistedShaurmaPacks(source, projectID, contentDir)
		}()
	}

	// Відбір збірок локально (без мережі), далі — ОДИН запит версій на
	// кожну унікальну пару (mc, loader), паралельно. Раніше на кожну збірку
	// йшло два послідовні запити (projectHasVersionFor + browserLatestFileName),
	// тож 10 збірок означали 20 запитів поспіль — модалка «довго думала».
	type candidate struct {
		pack   int
		loader string
		key    string
	}
	var cands []candidate
	for i, p := range packs {
		if p.MCVersion == "" {
			continue
		}
		// ВАЖЛИВО (запит «ванільні збірки фізично не підтримують модінг»):
		// для МОДІВ збірка з лоадером vanilla/datapack (або без лоадера)
		// НЕ може бути сумісною — гра без мод-лоадера (Fabric/Forge/Quilt/
		// NeoForge) фізично не запустить мод. Для ресурспаків/датапаків/
		// шейдерів ванільні збірки лишаються (лоадер-агностичні).
		if contentLoaderApplies(projectType) {
			pl := strings.ToLower(p.Loader)
			if pl == "" || pl == "vanilla" || pl == "datapack" {
				continue
			}
		}
		loader := effectiveLoaderForType(projectType, p.Loader)

		// Фільтр за конкретною версією контенту: збірка сумісна, якщо її
		// mcVersion є серед game_versions версії, а для модів/модпаків ще й
		// лоадер збірки — серед loaders версії (ванильна збірка приймає все).
		if version != nil {
			if !containsStr(version.GameVersions, p.MCVersion) {
				continue
			}
			if contentLoaderApplies(projectType) && p.Loader != "" && p.Loader != "vanilla" && p.Loader != "datapack" {
				if !containsStrFold(version.Loaders, p.Loader) {
					continue
				}
			}
		}
		// Збірка, чиї моди повністю дзеркалять теку-джерело іншої збірки, —
		// не ціль для встановлення модів (дзеркало зітре встановлене).
		if contentDir == "mods" && a.modsLockedErr(p.ID, "") != nil {
			continue
		}
		cands = append(cands, candidate{pack: i, loader: loader, key: p.MCVersion + "|" + strings.ToLower(loader)})
	}

	results := versionsForTargets(a, source, projectID, projectType, cands, func(c candidate) (string, string, string) {
		return c.key, packs[c.pack].MCVersion, c.loader
	})

	out := make([]BrowserCompatiblePack, 0, len(cands))
	for _, c := range cands {
		p := packs[c.pack]
		r := results[c.key]
		// err != nil — ЗАПИТ провалився (мережа, немає ключа CurseForge,
		// rate limit), а не «сумісної версії немає»: збірку лишаємо
		// (встановлення перевірить ще раз і поверне зрозумілу помилку).
		// Підтверджена несумісність (err == nil, версій нема) — ховаємо.
		if r.err == nil && len(r.versions) == 0 {
			continue
		}
		cp := BrowserCompatiblePack{
			PackID:      p.ID,
			Name:        p.Name,
			IconURL:     a.packAssetURL(p, p.IconPath),
			Icon:        p.Icon,
			Color:       p.Color,
			MCVersion:   p.MCVersion,
			Loader:      p.Loader,
			Unconfirmed: r.err != nil,
		}
		if contentDir != "" && len(r.versions) > 0 {
			if fileName := r.versions[0].FileName; fileName != "" {
				abs := filepath.Join(a.gameDir(p.ID), contentDir, fileName)
				if st, err := os.Stat(abs); err == nil && !st.IsDir() {
					cp.AlreadyInstalled = true
					cp.InstalledFileName = fileName
				}
			}
		}
		out = append(out, cp)
	}

	// Шаурма-збірки, де ЦЕЙ САМЕ проєкт (source+projectID) явно дозволений
	// автором через Pack Manager → Білий список: додаємо кандидатів лише
	// для типу "mod" (whitelist — суто мод-концепція). Мод, якого НЕМАЄ у
	// білому списку жодної шаурма-збірки, тут ніяк не з'явиться.
	if wlDone != nil {
		<-wlDone
		out = append(out, whitelisted...)
	}
	return out
}

// targetVersions — версії проєкту під одну пару (mc, loader) цілі.
type targetVersions struct {
	versions []BrowserVersionView
	err      error
}

// browserCompatWorkers — скільки пар (mc, loader) перевіряємо одночасно.
const browserCompatWorkers = 6

// versionsForTargets робить ОДИН запит версій на кожну унікальну пару
// (mc, loader) цілей, паралельно (до browserCompatWorkers одночасно), з
// коротким повтором на мережевій помилці. keyOf повертає ключ пари, mc і
// лоадер для цілі.
func versionsForTargets[T any](a *App, source, projectID, projectType string, targets []T, keyOf func(T) (string, string, string)) map[string]targetVersions {
	type job struct{ key, mc, loader string }
	seen := map[string]bool{}
	var jobs []job
	for _, t := range targets {
		key, mc, loader := keyOf(t)
		if seen[key] {
			continue
		}
		seen[key] = true
		jobs = append(jobs, job{key, mc, loader})
	}
	out := make(map[string]targetVersions, len(jobs))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, browserCompatWorkers)
	for _, j := range jobs {
		wg.Add(1)
		sem <- struct{}{}
		go func(j job) {
			defer wg.Done()
			defer func() { <-sem }()
			versions, err := a.GetBrowserProjectVersions(source, projectID, projectType, j.mc, j.loader)
			for retry := 1; err != nil && retry < 3 && !strings.Contains(err.Error(), "API-ключ"); retry++ {
				time.Sleep(time.Duration(retry) * 300 * time.Millisecond)
				versions, err = a.GetBrowserProjectVersions(source, projectID, projectType, j.mc, j.loader)
			}
			mu.Lock()
			out[j.key] = targetVersions{versions: versions, err: err}
			mu.Unlock()
		}(j)
	}
	wg.Wait()
	return out
}

// whitelistedShaurmaPacks повертає шаурма-збірки, у яких мод (source+
// projectID) присутній у ефективному білому списку (global+per-pack,
// див. internal/api.GetEffectiveWhitelistDetailed) — тобто автор збірки
// явно дозволив цей мод понад офіційний маніфест. Мережева помилка
// отримання індексу/whitelist конкретної збірки просто пропускає її
// (best-effort — одна недоступна збірка не повинна ховати решту).
func (a *App) whitelistedShaurmaPacks(source, projectID, contentDir string) []BrowserCompatiblePack {
	if a.packs == nil {
		// Немає провайдера шаурма-збірок (тестовий App без повної
		// ініціалізації або base-збірка без відповідної частини) — білий
		// список порожній, кандидатів немає. Раніше nil-deref падав тут
		// у будь-якому виклику GetBrowserCompatiblePacks для типу "mod"
		// без встановленого a.packs (виявив живий тест).
		return nil
	}
	index, err := a.packs.GetPackIndex()
	if err != nil || len(index) == 0 {
		return nil
	}
	out := make([]BrowserCompatiblePack, 0, 4)
	for _, p := range index {
		entries, err := a.packs.GetPackWhitelist(p.ID)
		if err != nil {
			continue
		}
		var matched *syncengine.WhitelistEntry
		for i := range entries {
			e := &entries[i]
			if e.Platform == source && e.ProjectID == projectID {
				matched = e
				break
			}
		}
		if matched == nil {
			continue
		}
		cp := BrowserCompatiblePack{
			PackID:      p.ID,
			Name:        p.Name,
			IconURL:     p.IconURL,
			Color:       p.Color,
			MCVersion:   p.MCVersion,
			Loader:      p.LoaderType,
			Whitelisted: true,
		}
		if contentDir != "" {
			loader := effectiveLoaderForType("mod", p.LoaderType)
			fileName := a.browserLatestFileName(source, projectID, "mod", p.MCVersion, loader)
			if fileName != "" {
				abs := filepath.Join(a.gameDir(p.ID), contentDir, fileName)
				if st, err := os.Stat(abs); err == nil && !st.IsDir() {
					cp.AlreadyInstalled = true
					cp.InstalledFileName = fileName
				}
			}
		}
		out = append(out, cp)
	}
	return out
}

// containsStr — чи є s у списку.
func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// containsStrFold — чутливий до регістру пошук (лоадери з API можуть
// приходити в різних регістрах: "Fabric" vs "fabric").
func containsStrFold(list []string, s string) bool {
	for _, v := range list {
		if strings.EqualFold(v, s) {
			return true
		}
	}
	return false
}

// projectHasVersionFor — чи є у проєкта версія під конкретну (mc, loader)
// для даного типу контенту (лоадер враховується лише для mod/modpack).
func (a *App) projectHasVersionFor(source, projectID, projectType, mcVersion, loader string) (bool, error) {
	switch source {
	case "modrinth":
		list, err := a.modrinth.GetProjectVersions(projectID, mcVersion, loader)
		if err != nil {
			return false, err
		}
		return len(list) > 0, nil
	case "curseforge":
		if !custompack.CurseForgeAvailable() {
			return false, fmt.Errorf("CurseForge API-ключ не налаштовано")
		}
		id, err := strconv.Atoi(projectID)
		if err != nil {
			return false, err
		}
		files, err := a.cfClient().GetModFiles(id, mcVersion, loader)
		if err != nil {
			return false, err
		}
		return len(files) > 0, nil
	default:
		return false, fmt.Errorf("невідоме джерело: %s", source)
	}
}

// browserLatestFileName — ім'я файлу останньої версії проєкту під (mc, loader)
// для даного типу контенту.
func (a *App) browserLatestFileName(source, projectID, projectType, mcVersion, loader string) string {
	versions, err := a.GetBrowserProjectVersions(source, projectID, projectType, mcVersion, loader)
	if err != nil || len(versions) == 0 {
		return ""
	}
	return versions[0].FileName
}

// ── Встановлення модпаку як «інтернет-збірки» ───────────────────────────

// BrowserModpackAlreadyInstalledError — повертається CreateBrowserModpack,
// коли модпак (source, projectID) уже є в «Моїх збірках», а force=false.
// Wails серіалізує помилку в .Error() рядок, тож фронтенд розпізнає цей
// випадок за префіксом "already-installed:" і показує вибір користувачу
// (перейти до наявної збірки / створити копію (2) / скасувати), а не
// показує це як звичайну помилку RuntimeError.
type BrowserModpackAlreadyInstalledError struct {
	PackID   string
	PackName string
}

func (e *BrowserModpackAlreadyInstalledError) Error() string {
	return fmt.Sprintf("already-installed:%s:%s", e.PackID, e.PackName)
}

// BrowserModpackInstallCheck — результат CheckBrowserModpackInstalled:
// чи вже є в «Моїх збірках» збірка з того самого проєкту (Source +
// SourceProjectID). Фронтенд показує попередження ПЕРЕД створенням нової
// картки замість того, щоб мовчки плодити копії з суфіксом id -2/-3.
type BrowserModpackInstallCheck struct {
	AlreadyInstalled bool   `json:"alreadyInstalled"`
	ExistingPackID   string `json:"existingPackId,omitempty"`
	ExistingPackName string `json:"existingPackName,omitempty"`
}

// CheckBrowserModpackInstalled перевіряє, чи модпак (source, projectID) уже
// встановлений/доданий у «Моїх збірках», перш ніж CreateBrowserModpack
// створить нову картку. Викликається фронтендом одразу перед кліком
// «Встановити» на сторінці модпаку в браузері контенту.
func (a *App) CheckBrowserModpackInstalled(source, projectID string) BrowserModpackInstallCheck {
	existing, ok := a.customPacks.FindBySource(source, projectID)
	if !ok {
		return BrowserModpackInstallCheck{}
	}
	return BrowserModpackInstallCheck{
		AlreadyInstalled: true,
		ExistingPackID:   existing.ID,
		ExistingPackName: existing.Name,
	}
}

// CreateBrowserModpack створює з модпаку «інтернет-збірку» в стані
// «НЕ ЗАВАНТАЖЕНО» — точно так, як Шаурма-збірки чекають кліку
// «Завантажити»: картка з іконкою/кольором з'являється у «Моїх збірках»,
// прямий mrpack-URL з сервісу зберігається в Pack.SourceArchiveURL, а
// файлів гри ще немає. Клік «Завантажити» на картці (DownloadBrowserModpack)
// качає архів за збереженим URL і імпортує в ТОЙ САМИЙ ID.
//
// Так зараз влаштовано сторінку модпаку: «Завантажити» більше НЕ качає
// одразу — спершу створює картку «не завантажено», далі встановлення —
// з картки (як просив ужиток: «модпак створюється зі станом не завантажений
// з вбудованим прямим посиланням на mrpack, потім користувач коли клікне
// встановити воно має встановлюватись»).
//
// force=false і модпак уже встановлений (див. CheckBrowserModpackInstalled)
// → повертаємо помилку з ID наявної збірки замість мовчазного дубля;
// фронтенд показує діалог «Збірку вже додано — перейти до неї чи створити
// копію?» і за потреби повторює виклик з force=true.
func (a *App) CreateBrowserModpack(source, projectID, versionID string, force bool) (custompack.Pack, error) {
	if !force {
		if existing, ok := a.customPacks.FindBySource(source, projectID); ok {
			return custompack.Pack{}, &BrowserModpackAlreadyInstalledError{PackID: existing.ID, PackName: existing.Name}
		}
	}
	detail, err := a.GetBrowserProjectDetail(source, projectID)
	if err != nil {
		return custompack.Pack{}, err
	}

	// Знаходимо URL архіву вибраної версії — він і буде прямим посиланням.
	// Помилку показуємо як є (замість узагальненої) — щоб було зрозуміло,
	// чому саме: немає API-ключа CurseForge, автор заборонив завантаження,
	// версію не знайдено тощо. Картку збірки в цьому разі НЕ створюємо —
	// краще явна помилка одразу, ніж «зомбі»-картка без посилання на архів,
	// яка потім падає з RuntimeError при спробі встановити.
	archiveURL, fileName, mcVersion, loader, err := a.browserVersionArchiveInfo(source, projectID, versionID)
	if err != nil {
		return custompack.Pack{}, fmt.Errorf("не вдалося визначити посилання на архів модпаку: %w", err)
	}
	if archiveURL == "" {
		return custompack.Pack{}, fmt.Errorf("не вдалося визначити посилання на архів модпаку")
	}

	// Унікальний ID/тека ЗБІРКИ — так само, як CreateCustomPack/Import:
	// без цього дві встановлені копії того самого модпаку (напр. другий
	// раз з іншою версією) писались під тим самим порожнім ID і друга
	// затирала першу в Store. Тека створюється одразу (постійна, НЕ tmp),
	// щоб іконка/фон, завантажені нижче, лишились на диску після виходу
	// з функції — раніше вони писались у os.MkdirTemp з defer RemoveAll,
	// тож зникали одразу після створення картки.
	//
	// UniqueIDAndName замість самого UniqueID: при колізії id отримує
	// суфікс -2/-3/..., і назва тепер отримує ТОЙ САМИЙ суфікс у форматі
	// «(2)», «(3)», щоб дві картки того самого модпаку не виглядали
	// однаково (раніше id ставав "cobblemon-official-modpack-2", а назва
	// лишалась просто "Cobblemon Official Modpack" — не відрізнити).
	id, packName := a.customPacks.UniqueIDAndName(detail.Name)
	packDir := filepath.Join(a.cfg.InstancesDir(), id) // custom pack → installations/<id>/
	if err := os.MkdirAll(packDir, 0755); err != nil {
		return custompack.Pack{}, fmt.Errorf("не вдалося створити теку збірки: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(packDir, ".minecraft"), 0755); err != nil {
		return custompack.Pack{}, fmt.Errorf("не вдалося створити .minecraft: %w", err)
	}

	// Іконка: webp→png + домінантний колір (той самий конвеєр, що раніше).
	iconPath, dominantColor := imageconvert.FetchIconToPNG(detail.IconURL, packDir)

	// Фон (банер картки/сторінки збірки) — featured-зображення галереї
	// проєкту, якщо автор його позначив, інакше перше зображення галереї
	// (Modrinth gallery / CurseForge screenshots). Раніше фон узагалі не
	// завантажувався для модпаків з браузера — pack.BackgroundPath
	// лишався порожнім, навіть коли в галереї проєкту скріншоти є.
	backgroundPath := ""
	if bgURL := browserBackgroundURL(detail); bgURL != "" {
		backgroundPath = imageconvert.FetchImageToPNG(bgURL, packDir, "background")
	}

	// Створюємо запис «не завантажено»: іконка/фон/колір одразу, файлів
	// гри ще немає.
	pack := custompack.Pack{
		ID:                id,
		Name:              packName,
		Color:             dominantColor,
		MCVersion:         mcVersion,
		Loader:            loader,
		Source:            detail.Source,
		SourceProjectID:   detail.ProjectID,
		SourceVersionID:   versionID,
		SourceProjectSlug: detail.Slug,
		SourceArchiveURL:  archiveURL,
		ImportFile:        fileName, // ім'я архіву для DownloadBrowserModpack
	}
	if iconPath != "" {
		// Перетворюємо абсолютний шлях на відносний (лише ім'я файлу).
		if filepath.IsAbs(iconPath) {
			pack.IconPath = filepath.Base(iconPath)
		} else {
			pack.IconPath = iconPath
		}
	}
	if backgroundPath != "" {
		if filepath.IsAbs(backgroundPath) {
			pack.BackgroundPath = filepath.Base(backgroundPath)
		} else {
			pack.BackgroundPath = backgroundPath
		}
	}

	// Реєструємо в Store — це і є «створення збірки». Повертаємо ID одразу.
	if err := a.customPacks.Save(pack); err != nil {
		return custompack.Pack{}, fmt.Errorf("не вдалося створити збірку: %w", err)
	}
	a.emit("builds:changed", pack.ID)
	return pack, nil
}

// browserBackgroundURL обирає зображення галереї проєкту для фону збірки:
// featured-зображення (Modrinth), якщо є, інакше перше в списку.
func browserBackgroundURL(detail *BrowserProjectDetail) string {
	if detail == nil || len(detail.Gallery) == 0 {
		return ""
	}
	for _, g := range detail.Gallery {
		if g.Featured {
			return g.URL
		}
	}
	return detail.Gallery[0].URL
}

// browserVersionArchiveInfo повертає (archiveURL, fileName, mcVersion, loader)
// для конкретної версії модпаку з джерела.
func (a *App) browserVersionArchiveInfo(source, projectID, versionID string) (string, string, string, string, error) {
	var (
		archiveURL, fileName string
		mcVersions           []string
		loaders              []string
	)
	switch source {
	case "modrinth":
		versions, err := a.GetBrowserProjectVersions(source, projectID, "modpack", "", "")
		if err != nil {
			return "", "", "", "", fmt.Errorf("не вдалося отримати версії з Modrinth: %w", err)
		}
		found := false
		for i := range versions {
			if versions[i].ID == versionID {
				archiveURL = versions[i].FileURL
				fileName = versions[i].FileName
				mcVersions = versions[i].GameVersions
				loaders = versions[i].Loaders
				found = true
				break
			}
		}
		if !found {
			return "", "", "", "", fmt.Errorf("версію %s не знайдено серед версій Modrinth-проєкту %s", versionID, projectID)
		}
		if archiveURL == "" {
			return "", "", "", "", fmt.Errorf("версія %s не має файлу для завантаження (Modrinth)", versionID)
		}
	case "curseforge":
		if !custompack.CurseForgeAvailable() {
			return "", "", "", "", fmt.Errorf("CurseForge API-ключ не налаштовано у цій збірці лаунчера — встановлення модпаків з CurseForge недоступне")
		}
		fileID, err := strconv.Atoi(versionID)
		if err != nil {
			return "", "", "", "", fmt.Errorf("некоректний ID файлу CurseForge: %s", versionID)
		}
		info, err := a.cfClient().GetModFile(atoiOr0(projectID), fileID)
		if err != nil {
			return "", "", "", "", err
		}
		archiveURL = info.DownloadURL
		fileName = info.FileName
		// info.GameVersions (з CurseForge) змішує версії гри й назву
		// лоадера в одному масиві — info.ModLoaders вже розпізнаний
		// окремо (див. GetModFileCtx), а mcVersions тут відфільтровуємо
		// тим самим правилом, щоб не отримати mc="Fabric" замість
		// реальної версії гри.
		mcVersions = custompack.CFVersionsOnly(info.GameVersions)
		loaders = info.ModLoaders
	default:
		return "", "", "", "", fmt.Errorf("невідоме джерело контенту: %s", source)
	}
	mc := ""
	if len(mcVersions) > 0 {
		mc = mcVersions[0]
	}
	ld := ""
	if len(loaders) > 0 {
		ld = strings.ToLower(loaders[0])
	}
	return archiveURL, fileName, mc, ld, nil
}

// DownloadBrowserModpack встановлює «не завантажений» модпак (створений
// CreateBrowserModpack): качає архів за збереженим прямим посиланням і
// імпортує в ТОЙ САМИЙ pack ID (ImportParams.ExistingID), щоб картка в
// «Моїх збірках» не дублювалась, а наповнилась файлами.
//
// АСИНХРОННИЙ: повертає pack ID одразу (як ImportModpackArchive), а сам
// конвеєр (архів → розпаковка → моди) іде у фоні з прогресом у
// sync:progress — той самий екран, що для Шаурма-збірок. Це виправляє
// мертву кнопку «Завантажити»: раніше весь шлях був СИНХРОННИМ у межах
// Wails-виклику, тож на великих модпаках кнопка висіла без жодного
// прогресу (а на деяких архівах — і взагалі не запускалась, бо архів
// качався без прогрес-подій, а фронт чекав на результат).
func (a *App) DownloadBrowserModpack(packID string) (custompack.Pack, error) {
	pack, ok := a.customPacks.Get(packID)
	if !ok {
		return custompack.Pack{}, fmt.Errorf("збірку не знайдено: %s", packID)
	}
	if pack.SourceArchiveURL == "" {
		return custompack.Pack{}, fmt.Errorf("збірка %s не має посилання на архів модпаку", packID)
	}

	ctx, cancel := context.WithCancel(context.Background())
	// Архів качаємо у постійну теку збірки (не в tmp — щоб пауза/крах
	// не губили .part-прогрес, а повторний запуск продовжував з місця).
	packDir := a.packDir(packID, "")
	dlDir := filepath.Join(packDir, ".download")
	if err := os.MkdirAll(dlDir, 0755); err != nil {
		cancel()
		return custompack.Pack{}, err
	}
	archivePath := filepath.Join(dlDir, safeFileName(pack.ImportFile, "modpack.mrpack"))

	// Реєструємо cancel-механізм: PauseDownload/CancelDownload з app.go
	// викликають CancelModpackImport для цього ID (той самий шлях, що для
	// ImportModpackArchive), тож кнопки «Зупинити»/«Скасувати» працюють.
	a.customImportMu.Lock()
	a.customImportCancel[packID] = cancel
	a.customImportMu.Unlock()

	packName := pack.Name

	go func() {
		defer crashguard.Recover("browser_api.go:1879")
		defer func() {
			cancel()
			a.customImportMu.Lock()
			delete(a.customImportCancel, packID)
			a.customImportMu.Unlock()
		}()

		// 1) Архів з мережі (з Range-резюме .part та прогресом).
		reportArchive := func(done, total int64) {
			pct := 0
			if total > 0 {
				pct = int(done * 100 / total)
			}
			a.recordCustomImportProgress(custompack.ImportProgress{
				PackID:          packID,
				BytesTotal:      total,
				BytesDownloaded: done,
				SpeedBPS:        0,
				Stage:           custompack.StageDownloading,
				CurrentFile:     "модпак " + filepath.Base(archivePath),
			})
			a.emit("sync:progress", customSyncProgressPayload{
				PackID:          packID,
				PackName:        packName,
				Mode:            "running",
				BytesTotal:      total,
				BytesDownloaded: done,
				OverallPercent:  pct,
				CurrentFile:     "модпак " + filepath.Base(archivePath),
				Stage:           custompack.StageDownloading,
			})
		}
		if err := downloadFileToResumable(ctx, pack.SourceArchiveURL, archivePath, reportArchive); err != nil {
			if ctx.Err() == nil {
				// Помилка качання архіву — персистентний стан імпорту прибираємо,
				// щоб картка не показувала застарілу «Паузу»/%.
				a.clearCustomImportState(packID)
				a.emit("download:error", map[string]string{"packId": packID, "message": fmt.Sprintf("не вдалося завантажити модпак: %v", err)})
			}
			return
		}

		// 2) Розпізнати формат і імпортувати в той самий pack ID.
		detected, err := custompack.DetectArchive(archivePath)
		if err != nil {
			a.emit("download:error", map[string]string{"packId": packID, "message": err.Error()})
			return
		}
		res, err := a.importBrowserArchiveInto(ctx, archivePath, detected, pack, pack.SourceVersionID)
		if err != nil {
			// Пауза/скасування користувачем — НЕ помилка (тост не показуємо):
			// стан «Пауза»/«Скасовано» емітиться PauseModpackImport/
			// CancelModpackImport, а тут лишається лише дійсна помилка.
			if ctx.Err() == nil {
				// Справжня помилка імпорту — прибираємо персистентний стан.
				a.clearCustomImportState(packID)
				a.emit("download:error", map[string]string{"packId": packID, "message": err.Error()})
			} else {
				// Фінальне слово паузи: пізній тік прогресу не має затерти стан.
				a.finalizeCustomPaused(packID)
			}
			return
		}
		// Архів і .part більше не потрібні — прибираємо, щоб не лежали
		// в теці збірки назавжди.
		os.RemoveAll(dlDir)
		ensureBuildsRegistered(a, res.ID)
		a.emit("builds:changed", res.ID)
	}()

	// Завжди повертаємо ID одразу (async): прогрес, помилки та завершення
	// йдуть подіями sync:progress / download:error / builds:changed. Фронт
	// не чекає Wails-виклик, а слухає події — кнопка «Завантажити» миттєво
	// переходить у стан «качається» і показує реальний прогрес.
	return custompack.Pack{ID: packID, Name: packName}, nil
}

// importBrowserArchiveInto — синхронний імпорт архіву браузером у pack ID
// (новий, якщо pack.ID порожній, або існуючий «не завантажений» модпак).
// Виконує custompack.ImportArchive у ПОТОЧНІЙ горутині; прогрес
// транслюється у sync:progress. ctx керує скасуванням (кнопка
// «Зупинити»). Викликачі (DownloadBrowserModpack/importBrowserArchive)
// самі вирішують, чи запускати це в фоні.
func (a *App) importBrowserArchiveInto(ctx context.Context, archivePath string, detected *custompack.DetectedPack, pack custompack.Pack, versionID string) (custompack.Pack, error) {
	settings := a.cfg.GetSettings()
	pack, err := custompack.ImportArchive(
		ctx, a.customPacks, a.cfg.InstancesDir(), archivePath, detected,
		custompack.ImportParams{
			Name:              pack.Name,
			Color:             pack.Color,
			IconSrcPath:       pack.IconPath,
			BackgroundSrcPath: pack.BackgroundPath,
			Source:            pack.Source,
			SourceProjectID:   pack.SourceProjectID,
			SourceVersionID:   versionID,
			SourceProjectSlug: pack.SourceProjectSlug,
			SourceArchiveURL:  pack.SourceArchiveURL,
			ExistingID:        pack.ID,
			// Заблоковані автором моди: діалог рішення (як у Prism).
			BlockedHandler:        a.blockedModsHandler(pack.ID),
			UseModrinthForBlocked: settings.UseModrinthForBlocked,
		},
		settings.MaxConcurrentDownloads,
		func(ip custompack.ImportProgress) {
			// Після паузи/скасування не пишемо прогрес (див. той самий
			// ctx-гейт в ImportModpackArchive): стан «Пауза», виставлений
			// PauseModpackImport, не має перезаписуватись пізнім тіком.
			if ctx.Err() != nil {
				return
			}
			a.recordCustomImportProgress(ip)
			payload := customImportProgressToSyncPayload(ip, pack.Name)
			if cp, ok := a.customPacks.Get(ip.PackID); ok && cp.IconPath != "" {
				payload.IconURL = a.packAssetURL(cp, cp.IconPath)
			}
			a.emit("sync:progress", payload)
		},
	)
	if err != nil {
		// Помилка імпорту в існуючу збірку — прибираємо персистентний стан,
		// щоб картка не лишалась у «Паузі» після падіння конвеєра.
		a.clearCustomImportState(pack.ID)
		return custompack.Pack{}, err
	}
	ensureBuildsRegistered(a, pack.ID)
	a.emit("builds:changed", pack.ID)
	return pack, nil
}

// ── Оновлення «інтернет-збірки» з джерела ───────────────────────────────

// GetPackSourceVersions повертає версії модпаку для збірки-«інтернет-збірки»
// (кнопка «Оновити з джерела»): джерело і проєкт беруться з даних збірки.
// currentVersionID — ID поточної встановленої версії, щоб фронтенд
// позначив її бейджем «Поточна».
func (a *App) GetPackSourceVersions(packID string) ([]BrowserVersionView, error) {
	pack, ok := a.customPacks.Get(packID)
	if !ok {
		return nil, fmt.Errorf("збірку не знайдено: %s", packID)
	}
	if pack.Source != "modrinth" && pack.Source != "curseforge" {
		return nil, fmt.Errorf("збірка %s не є інтернет-збіркою (джерело: %s)", packID, pack.Source)
	}
	versions, err := a.GetBrowserProjectVersions(pack.Source, pack.SourceProjectID, "modpack", "", "")
	if err != nil {
		return nil, err
	}
	return versions, nil
}

// UpdatePackFromSource оновлює «інтернет-збірку» до вказаної версії:
// качає новий архів, робить diff проти старого маніфесту (моди/конфіги,
// що зникли — видаляються; користувацькі доповнення лишаються), оновлює
// версію гри/лоадер з НОВОЇ версії (як просив ужиток). Прогрес — у
// sync:progress. Повертає оновлену Pack (для оновлення картки).
func (a *App) UpdatePackFromSource(packID, versionID string) (custompack.Pack, error) {
	pack, ok := a.customPacks.Get(packID)
	if !ok {
		return custompack.Pack{}, fmt.Errorf("збірку не знайдено: %s", packID)
	}
	// Оновлення джерела змінює файли в .minecraft/ — блокуємо
	if err := a.errPackRunning(packID); err != nil {
		return custompack.Pack{}, err
	}
	if pack.Source != "modrinth" && pack.Source != "curseforge" {
		return custompack.Pack{}, fmt.Errorf("збірка %s не є інтернет-збіркою", packID)
	}
	if pack.SourceVersionID == versionID {
		return pack, nil // та сама версія — нічого робити
	}

	// Джерело і проєкт — з даних збірки; знаходимо URL архіву нової версії.
	var archiveURL, fileName string
	switch pack.Source {
	case "modrinth":
		versions, err := a.GetBrowserProjectVersions(pack.Source, pack.SourceProjectID, "modpack", "", "")
		if err != nil {
			return custompack.Pack{}, err
		}
		found := false
		for i := range versions {
			if versions[i].ID == versionID {
				archiveURL = versions[i].FileURL
				fileName = versions[i].FileName
				found = true
				break
			}
		}
		if !found {
			return custompack.Pack{}, fmt.Errorf("версію %s не знайдено в проєкті %s", versionID, pack.SourceProjectID)
		}
	case "curseforge":
		if !custompack.CurseForgeAvailable() {
			return custompack.Pack{}, fmt.Errorf("CurseForge API-ключ не налаштовано у цій збірці лаунчера")
		}
		fileID, err := strconv.Atoi(versionID)
		if err != nil {
			return custompack.Pack{}, err
		}
		info, err := a.cfClient().GetModFile(atoiOr0(pack.SourceProjectID), fileID)
		if err != nil {
			return custompack.Pack{}, err
		}
		archiveURL = info.DownloadURL
		fileName = info.FileName
	}

	tmpDir, err := os.MkdirTemp("", "shaurma-update-*")
	if err != nil {
		return custompack.Pack{}, err
	}
	defer os.RemoveAll(tmpDir)
	archivePath := filepath.Join(tmpDir, safeFileName(fileName, "modpack.mrpack"))
	if err := downloadFileTo(archiveURL, archivePath); err != nil {
		return custompack.Pack{}, fmt.Errorf("не вдалося завантажити нову версію: %w", err)
	}
	detected, err := custompack.DetectArchive(archivePath)
	if err != nil {
		return custompack.Pack{}, err
	}

	settings := a.cfg.GetSettings()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	resultCh := make(chan struct {
		pack custompack.Pack
		err  error
	}, 1)
	go func() {
		defer crashguard.Recover("browser_api.go:2108")
		updated, err := custompack.UpdatePackFromSource(
			ctx, a.customPacks, a.cfg.InstancesDir(), pack, archivePath, detected, versionID,
			settings.MaxConcurrentDownloads,
			// Заблоковані автором моди при оновленні: той самий діалог рішення,
			// що й при першому встановленні (Modrinth / пропустити / скасувати).
			a.blockedModsHandler(packID),
			settings.UseModrinthForBlocked,
			func(ip custompack.ImportProgress) {
				a.recordCustomImportProgress(ip)
				// Фінальний ImportProgress (Stage: StageComplete) несе
				// SkippedPaths — файли гравця (config/, options.txt тощо),
				// які extractOverrides свідомо НЕ перезаписала цього разу.
				// Друкуємо в консоль встановлення тим самим стилем, що й
				// офіційні Shaurma-збірки (emitSyncReport).
				a.emitSkippedProtectedFiles(ip.SkippedPaths)
				select {
				case <-ctx.Done():
				default:
					payload := customImportProgressToSyncPayload(ip, pack.Name)
					if cp, ok := a.customPacks.Get(ip.PackID); ok && cp.IconPath != "" {
						payload.IconURL = a.packAssetURL(cp, cp.IconPath)
					}
					a.emit("sync:progress", payload)
				}
			},
		)
		resultCh <- struct {
			pack custompack.Pack
			err  error
		}{updated, err}
	}()
	res := <-resultCh
	if res.err != nil {
		a.emit("download:error", map[string]string{"packId": packID, "message": res.err.Error()})
		return custompack.Pack{}, res.err
	}
	ensureBuildsRegistered(a, res.pack.ID)
	// НЕ застосовуємо дефолти тут: applyMinecraftDefaultsNow (ApplyNow)
	// перезаписує options.txt лише при ПЕРШОМУ встановленні (перевіряє
	// instance.json.MinecraftDefaultsApplied). При оновленні вже
	// встановленої збірки (цей шлях — UpdatePackFromSource) виклик тут
	// затирав би налаштування, які гравець змінив поверх дефолтів між
	// оновленнями, ігноруючи правило «Оновити при старті». Перед
	// наступним запуском гри дефолти й так застосує
	// applyMinecraftDefaultsBeforeLaunch → ApplyBeforeLaunch, яка теж
	// звіряється з instance.json.MinecraftDefaultsApplied і чіпає файл
	// гравця лише одноразово за все життя збірки.
	a.emit("builds:changed", res.pack.ID)
	return res.pack, nil
}

// ── Аналіз «вже встановлено» (як Modrinth app: за sha1 файлів) ────────

// BrowserInstalledItem — один встановлений файл контенту у збірці, який
// вдалося зіставити з проєктом (за sha1 через Modrinth /version_file).
// Використовується фронтендом для бейджа «Встановлено» на картках і для
// позначки встановленої версії на сторінці проєкту.
type BrowserInstalledItem struct {
	ProjectID     string `json:"projectId"`
	ProjectSlug   string `json:"projectSlug"`
	VersionID     string `json:"versionId"`
	VersionNumber string `json:"versionNumber"`
	FileName      string `json:"fileName"`
	Dir           string `json:"dir"` // mods | resourcepacks | shaderpacks
	Source        string `json:"source"`
}

// browserInstalledCache — кеш аналізу «вже встановлено» по збірці (TTL
// 60с): хешування сотень файлів і N запитів у Modrinth при кожному
// відкритті браузера з фільтром збірки надто дорого. Між зверненнями
// (картки + сторінка проєкту) результат перевикористовується.
var browserInstalledCache = struct {
	sync.Mutex
	m map[string]browserInstalledCacheEntry
}{m: map[string]browserInstalledCacheEntry{}}

type browserInstalledCacheEntry struct {
	items map[string]BrowserInstalledItem
	at    time.Time
}

const browserInstalledCacheTTL = 60 * time.Second

// GetBrowserInstalledMap аналізує, які проєкти вже встановлено у збірці:
// сканує теки mods/resourcepacks/shaderpacks, рахує sha1 файлів і зіставляє
// з проєктами через Modrinth /version_files (ОДИН batch-запит). Повертає
// мапу projectId → інформація про встановлений файл.
//
// ОПТИМІЗАЦІЯ ШВИДКОСТІ (фільтр «ця збірка» довго аналізував):
//  1. ПЕРСИСТЕНТНИЙ кеш на диску (кеш-тека лаунчера, browser-installed/
//     <packID>.json) з підписом теки (імена+розміри+mtime файлів). Поки
//     файли збірки не змінились — результат береться з диска ЗА МС,
//     без жодного sha1 і без мережі.
//  2. Якщо контент змінився — sha1 рахуються для всіх файлів і
//     резолвляться ОДНИМ POST /version_files (раніше було N окремих
//     /version_file запитів на кожен файл).
//  3. In-memory кеш з коротким TTL — захист від повторних викликів у межах
//     сесії (картки + сторінка проєкту).
func (a *App) GetBrowserInstalledMap(packID string) map[string]BrowserInstalledItem {
	return a.browserInstalledMapFor(packID, a.gameDir(packID))
}

// GetServerBrowserInstalledMap — те саме, що GetBrowserInstalledMap, але
// для СЕРВЕРА: аналізує mods/ та plugins/ у теці сервера (не збірки),
// щоб сторінка проєкту в браузері могла показати бейдж «Встановлено» /
// «Оновити», коли її відкрито з менеджера модів/плагінів сервера
// (ContentBrowser з serverContext — див. ModpackPage.svelte,
// packContext.isServer). Кеш-ключ префіксується "srv-", щоб файл
// дискового кешу не міг випадково збігтися з кешем збірки при однаковому
// id (малоймовірно, але id — довільний UUID з того самого простору).
func (a *App) GetServerBrowserInstalledMap(serverID string) map[string]BrowserInstalledItem {
	srv, err := a.srvAPI.GetServer(serverID)
	if err != nil {
		return map[string]BrowserInstalledItem{}
	}
	return a.browserInstalledMapFor("srv-"+serverID, srv.Dir)
}

// browserInstalledMapFor — спільне ядро GetBrowserInstalledMap /
// GetServerBrowserInstalledMap: cacheKey — ключ in-memory/дискового кешу
// (packID або "srv-"+serverID), base — коренева тека, у якій шукати
// mods/resourcepacks/shaderpacks/datapacks (тека збірки або тека сервера).
func (a *App) browserInstalledMapFor(cacheKey, base string) map[string]BrowserInstalledItem {
	browserInstalledCache.Lock()
	if c, ok := browserInstalledCache.m[cacheKey]; ok && time.Since(c.at) < browserInstalledCacheTTL {
		browserInstalledCache.Unlock()
		return c.items
	}
	browserInstalledCache.Unlock()

	// Збираємо файли контенту збірки (шлях + dir) БЕЗ хешування — це
	// швидко, і з цього будується підпис для дискового кешу.
	files := a.collectInstalledFiles(base)
	sig := installedSignature(files)

	// Дисковий кеш: підпис той самий → дані актуальні, sha1/мережу не чіпаємо.
	if items, ok := a.readInstalledDiskCache(cacheKey, sig); ok {
		browserInstalledCache.Lock()
		browserInstalledCache.m[cacheKey] = browserInstalledCacheEntry{items: items, at: time.Now()}
		browserInstalledCache.Unlock()
		return items
	}

	// Рахуємо sha1 усіх файлів (тільки для файлів до 256МБ — гігантські
	// шейдерпаки/ресурспаки однаково не зіставляються, лише гальмують).
	type hashedFile struct {
		full, dir string
		sha1      string
	}
	var toResolve []hashedFile
	for _, f := range files {
		if f.size > 256*1024*1024 {
			continue
		}
		h, err := fileSHA1(f.full)
		if err != nil {
			continue
		}
		toResolve = append(toResolve, hashedFile{full: f.full, dir: f.dir, sha1: h})
	}

	// Один batch-запит на всі хеші (до 200 у порції — сам клієнт ділить).
	hashes := make([]string, 0, len(toResolve))
	for _, f := range toResolve {
		hashes = append(hashes, f.sha1)
	}
	byHash, err := a.modrinth.GetVersionsByHashes(hashes)
	if err != nil {
		// Мережева помилка — повертаємо те, що є (можливо порожньо), не
		// блокуємо браузер; кеш не пишемо, щоб не закріпити порожній стан.
		return map[string]BrowserInstalledItem{}
	}

	out := map[string]BrowserInstalledItem{}
	for _, f := range toResolve {
		v := byHash[f.sha1]
		if v == nil || v.ProjectID == "" {
			continue
		}
		out[v.ProjectID] = BrowserInstalledItem{
			ProjectID:     v.ProjectID,
			VersionID:     v.ID,
			VersionNumber: v.VersionNumber,
			Source:        "modrinth",
			FileName:      filepath.Base(f.full),
			Dir:           f.dir,
		}
	}

	// Slug-fallback для файлів, чий sha1 не резолвився (перейменований jar,
	// стара версія вже зникла з Modrinth, встановлено з CurseForge тощо):
	// slug з імені файлу → GET /project/{slug} (з кешем) → projectId. Це те,
	// чого не вистачало для бейджа «Оновити/Встановлено» на картці, коли у
	// збірці лежить СТАРА версія мода з іншим ім'ям файлу. Обмежуємо кількість
	// додаткових запитів (pathological-збірки з десятками не-модринт-файлів не
	// мають гальмувати браузер) і не перезаписуємо вже знайдені проєкти.
	//
	// РЕГРЕСІЯ (виправлено): цикл раніше йшов ПОСЛІДОВНО — до 30 окремих
	// HTTP round-trip'ів один за одним. Для збірки з великою кількістю
	// CurseForge-контенту (Modrinth-хеш його не впізнає) це були реально
	// відчутні секунди на саму лише перевірку «встановлено/оновити». Пул
	// воркерів (той самий принцип, що filterCompatibleEntries нижче) —
	// час перевірки обмежений найповільнішим запитом, а не сумою всіх.
	if len(toResolve) > 0 {
		var unresolved []hashedFile
		for _, f := range toResolve {
			if byHash[f.sha1] == nil {
				unresolved = append(unresolved, f)
			}
		}
		if len(unresolved) > 30 {
			unresolved = unresolved[:30]
		}
		var outMu sync.Mutex
		var wg sync.WaitGroup
		sem := make(chan struct{}, 6)
		for _, f := range unresolved {
			slug := slugFromFileName(filepath.Base(f.full))
			if slug == "" {
				continue
			}
			wg.Add(1)
			sem <- struct{}{}
			go func(f hashedFile, slug string) {
				defer crashguard.Recover("browser_api.go:2332")
				defer wg.Done()
				defer func() { <-sem }()
				p, err := a.modrinth.GetProject(slug)
				if err != nil || p == nil || p.ID == "" {
					return
				}
				// VersionNumber — найкраща оцінка з імені файлу (частина після
				// slug-префікса), для повідомлення «оновити з X до Y».
				ver := strings.TrimPrefix(strings.TrimSuffix(filepath.Base(f.full), filepath.Ext(f.full)), slug+"-")
				outMu.Lock()
				if _, exists := out[p.ID]; !exists {
					out[p.ID] = BrowserInstalledItem{
						ProjectID:     p.ID,
						VersionID:     "",
						VersionNumber: ver,
						Source:        "modrinth",
						FileName:      filepath.Base(f.full),
						Dir:           f.dir,
					}
				}
				outMu.Unlock()
			}(f, slug)
		}
		wg.Wait()
	}

	a.writeInstalledDiskCache(cacheKey, sig, out)
	browserInstalledCache.Lock()
	browserInstalledCache.m[cacheKey] = browserInstalledCacheEntry{items: out, at: time.Now()}
	browserInstalledCache.Unlock()
	return out
}

// BrowserInstalledInPack — одна збірка, у якій знайдено проєкт (для
// сторінки контенту: «Встановлено у ...»).
type BrowserInstalledInPack struct {
	PackID        string `json:"packId"`
	PackName      string `json:"packName"`
	VersionNumber string `json:"versionNumber"`
}

// GetBrowserProjectInstalledPacks повертає список збірок користувача, у
// яких уже встановлено вказаний проєкт (projectID) — для бейджа «Також
// встановлено у: ...» на сторінці контенту. На відміну від
// GetBrowserInstalledMap (одна збірка → мапа всіх проєктів), тут навпаки:
// один проєкт → усі збірки.
//
// Перевіряються ЛИШЕ збірки, файли яких вже завантажені (Installed=true) —
// немає сенсу сканувати теку, якої ще не існує. Кожна збірка йде через той
// самий GetBrowserInstalledMap, тож повторний виклик для іншого проєкту
// (наступний клік по картці) читає вже готовий кеш (диск/пам'ять) майже
// миттєво — мережевий запит буде лише для збірок, які реально змінились
// з минулого разу.
func (a *App) GetBrowserProjectInstalledPacks(projectID string) []BrowserInstalledInPack {
	if projectID == "" {
		return nil
	}
	views := a.GetBuilds()
	out := make([]BrowserInstalledInPack, 0, 4)
	for _, v := range views {
		if !v.Installed {
			continue
		}
		items := a.GetBrowserInstalledMap(v.Build.ID)
		item, ok := items[projectID]
		if !ok {
			continue
		}
		out = append(out, BrowserInstalledInPack{
			PackID:        v.Build.ID,
			PackName:      v.Build.Name,
			VersionNumber: item.VersionNumber,
		})
	}
	return out
}

// installedFile — один файл контенту збірки для аналізу.
type installedFile struct {
	full, dir string
	size      int64
}

// collectInstalledFiles збирає файли з усіх тек контенту збірки АБО
// сервера: mods/resourcepacks/shaderpacks/datapacks/plugins (глобально) +
// saves/*/datapacks (світи, куди датапак міг бути встановлений старим
// способом). plugins/ для звичайної збірки просто ніколи не існує —
// os.ReadDir поверне помилку, listContentFiles тихо поверне nil (те саме,
// що для будь-якої іншої відсутньої теки), тож додавання цього пункту в
// спільний список безпечне для обох викликів (GetBrowserInstalledMap /
// GetServerBrowserInstalledMap) і не потребує розгалуження на base-тип.
func (a *App) collectInstalledFiles(base string) []installedFile {
	var out []installedFile
	for _, dir := range []string{"mods", "resourcepacks", "shaderpacks", "datapacks", "plugins"} {
		out = append(out, listContentFiles(filepath.Join(base, dir), dir)...)
	}
	savesDir := filepath.Join(base, "saves")
	if entries, err := os.ReadDir(savesDir); err == nil {
		for _, w := range entries {
			if !w.IsDir() {
				continue
			}
			out = append(out, listContentFiles(filepath.Join(savesDir, w.Name(), "datapacks"), "datapacks")...)
		}
	}
	return out
}

// listContentFiles читає одну теку і повертає файли (без підтек).
func listContentFiles(dir, contentDir string) []installedFile {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []installedFile
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		full := filepath.Join(dir, e.Name())
		if st, err := os.Stat(full); err == nil {
			out = append(out, installedFile{full: full, dir: contentDir, size: st.Size()})
		}
	}
	return out
}

// installedSignature — підпис теки контенту (детермінований): імена файлів
// + розміри + mtime. Якщо підпис не змінився — вміст той самий, дисковий
// кеш аналізу можна використати без перерахування sha1.
func installedSignature(files []installedFile) string {
	if len(files) == 0 {
		return "empty"
	}
	lines := make([]string, 0, len(files))
	for _, f := range files {
		if st, err := os.Stat(f.full); err == nil {
			lines = append(lines, fmt.Sprintf("%s|%d|%d", filepath.Base(f.full), f.size, st.ModTime().UnixNano()))
		} else {
			lines = append(lines, filepath.Base(f.full)+"|missing")
		}
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:])
}

// browserInstalledDiskDir — тека персистентного кешу аналізу (кеш лаунчера).
func (a *App) browserInstalledDiskDir() string {
	return filepath.Join(a.cfg.CacheDir(), "browser-installed")
}

// readInstalledDiskCache читає дисковий кеш аналізу для збірки, якщо
// підпис збігається. Повертає (items, true) при влученні.
func (a *App) readInstalledDiskCache(packID, sig string) (map[string]BrowserInstalledItem, bool) {
	data, err := os.ReadFile(filepath.Join(a.browserInstalledDiskDir(), packID+".json"))
	if err != nil {
		return nil, false
	}
	var cached struct {
		Sig   string                          `json:"sig"`
		Items map[string]BrowserInstalledItem `json:"items"`
	}
	if json.Unmarshal(data, &cached) != nil || cached.Sig != sig {
		return nil, false
	}
	return cached.Items, true
}

// writeInstalledDiskCache пише дисковий кеш аналізу (підпис + результат).
func (a *App) writeInstalledDiskCache(packID, sig string, items map[string]BrowserInstalledItem) {
	dir := a.browserInstalledDiskDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	data, err := json.Marshal(struct {
		Sig   string                          `json:"sig"`
		Items map[string]BrowserInstalledItem `json:"items"`
	}{sig, items})
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(dir, packID+".json"), data, 0o644)
}

// InvalidateBrowserInstalledCache скидає кеш аналізу для збірки (після
// встановлення/оновлення контенту): і in-memory, і ДИСКОВИЙ (наступний
// аналіз перерахує sha1 і запише свіжий кеш). Інакше після встановлення
// мода з браузера бейдж «Встановлено» показувався б лише після зміни
// mtime/розміру файлів збірки.
func (a *App) InvalidateBrowserInstalledCache(packID string) {
	browserInstalledCache.Lock()
	delete(browserInstalledCache.m, packID)
	browserInstalledCache.Unlock()
	_ = os.Remove(filepath.Join(a.browserInstalledDiskDir(), packID+".json"))
}

// fileSHA1 рахує sha1 файлу (стандартна бібліотека, без залежностей).
func fileSHA1(path string) (string, error) {
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

// slugFromFileName витягує slug проєкту з імені файлу виду
// "<slug>-<version>.<ext>" (конвенція іменування Modrinth і CurseForge).
// Префікс — до ПЕРШОГО дефіса, після якого йде цифра (початок версії):
//
//	"sodium-extra-0.5.0.jar"         → "sodium-extra"
//	"fabric-api-0.92.0+1.20.1.jar"   → "fabric-api"
//	"jei-1.20.1-forge-15.2.0.27.jar" → "jei"
//
// Якщо такого дефіса нема (нестандартне ім'я, напр. OptiFine_1.20.1...) —
// повертає "", тобто файл не намагаємось зіставляти (ніяких хибних
// спрацювань на несхожих іменах).
func slugFromFileName(name string) string {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	for i := 0; i < len(base); i++ {
		if base[i] == '-' && i+1 < len(base) && base[i+1] >= '0' && base[i+1] <= '9' {
			slug := base[:i]
			if slug == "" || len(slug) > 64 {
				return ""
			}
			// Modrinth-слаги: малі літери, цифри, дефіс, підкреслення.
			for j := 0; j < len(slug); j++ {
				c := slug[j]
				if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
					return ""
				}
			}
			return slug
		}
	}
	return ""
}

// removeOldProjectFiles видаляє з теки destDir старі файли того самого
// проєкту (у збірці вже стоїть стара версія мода/ресурспака, а нова має
// ІНШЕ ім'я файлу — інакше обидві версії лежатимуть поруч). Зіставлення:
//  1. sha1 файлу → Modrinth projectId (той самий batch, що й
//     GetBrowserInstalledMap) — надійно, коли версія ще є на Modrinth;
//  2. slug з імені файлу (slugFromFileName) — для перейменованих jar-ів,
//     старих версій, що вже зникли з Modrinth, і контенту з CurseForge.
//
// Файл, який зараз завантажується (newFileName), не чіпаємо.
func (a *App) removeOldProjectFiles(destDir, projectID, newFileName string) {
	entries, err := os.ReadDir(destDir)
	if err != nil || len(entries) == 0 {
		return
	}
	newSlug := slugFromFileName(newFileName)

	// 1) sha1-прохід: batch-резолв хешів через Modrinth.
	type cand struct {
		full string
		sha1 string
	}
	var cands []cand
	for _, e := range entries {
		if e.IsDir() || e.Name() == newFileName {
			continue
		}
		full := filepath.Join(destDir, e.Name())
		st, err := e.Info()
		if err != nil || st.Size() > 256*1024*1024 {
			continue
		}
		h, err := fileSHA1(full)
		if err != nil {
			continue
		}
		cands = append(cands, cand{full: full, sha1: h})
	}
	if len(cands) == 0 {
		return
	}
	hashes := make([]string, 0, len(cands))
	for _, c := range cands {
		hashes = append(hashes, c.sha1)
	}
	byHash, hashErr := a.modrinth.GetVersionsByHashes(hashes)

	for _, c := range cands {
		if hashErr == nil {
			if v, ok := byHash[c.sha1]; ok && v.ProjectID != "" && v.ProjectID == projectID {
				_ = os.Remove(c.full)
				continue
			}
		}
		// 2) slug-fallback: той самий slug в імені файлу = той самий проєкт.
		if newSlug != "" {
			if slug := slugFromFileName(filepath.Base(c.full)); slug != "" && slug == newSlug {
				_ = os.Remove(c.full)
			}
		}
	}
}

// ── Встановлення модів / ресурспаків / шейдерів у збірку ────────────────

// InstallBrowserContent встановлює контент (мод/ресурспак/шейдер) у
// вибрану збірку: знаходить файл останньої версії під (mc, loader) збірки
// і качає у відповідну теку .minecraft/. replace=true — перезаписати
// наявний файл (попередження про заміну вже показав фронтенд).
func (a *App) InstallBrowserContent(packID, source, projectID, contentDir string, replace bool) (BrowserInstallResult, error) {
	return a.InstallBrowserContentVersion(packID, source, projectID, "", contentDir, replace)
}

// contentDirToProjectType — зворотний мапінг теки встановлення → тип
// контенту (для типу-залежної фільтрації версій у встановленні).
func contentDirToProjectType(contentDir string) string {
	switch contentDir {
	case "resourcepacks":
		return "resourcepack"
	case "shaderpacks":
		return "shader"
	case "datapacks":
		return "datapack"
	default:
		return "mod"
	}
}

// InstallBrowserContentVersion встановлює КОНКРЕТНУ версію контенту у
// збірку (кнопка на сторінці проєкту). versionID="" = найсвіжіша під
// (mc, loader) збірки. Це і є відповідь на «фільтр по збірці не означає,
// що не можна встановити будь-яку версію»: фільтр визначає ЦІЛЬОВУ збірку,
// а версію користувач обирає сам зі списку на сторінці проєкту.
func (a *App) InstallBrowserContentVersion(packID, source, projectID, versionID, contentDir string, replace bool) (BrowserInstallResult, error) {
	// Джерело збірки: кастомна (customPacks, як і раніше) АБО шаурма —
	// але для шаурма-збірки встановлення дозволене ЛИШЕ якщо САМЕ ЦЕЙ
	// проєкт (source+projectID) є в її ефективному білому списку
	// (internal/api.GetEffectiveWhitelistDetailed). Перевірка тут, на
	// бекенді, а НЕ лише на фронтенді (де вона потрібна для UI-групування
	// кандидатів) — інакше будь-хто міг би обійти захист теки, що
	// керується синком, підставивши шаурма packID напряму.
	pack, ok := a.customPacks.Get(packID)
	var mcVersion, loaderType, packName string
	// Збірка, чиї моди повністю беруться з теки-джерела іншої збірки, — не
	// ціль для встановлення модів: усе, що туди покласти, дзеркало зітре.
	if contentDirToProjectType(contentDir) == "mod" {
		if err := a.modsLockedErr(packID, ""); err != nil {
			return BrowserInstallResult{}, err
		}
	}
	if ok {
		mcVersion, loaderType, packName = pack.MCVersion, pack.Loader, pack.Name
	} else {
		sp, err := a.packs.GetPackByID(packID)
		if err != nil {
			return BrowserInstallResult{}, fmt.Errorf("збірку не знайдено: %s", packID)
		}
		if contentDirToProjectType(contentDir) != "mod" {
			// Білий список — суто мод-концепція (див. BrowserCompatiblePack.
			// Whitelisted); для інших типів контенту шаурма-збірки взагалі
			// не потрапляють у список кандидатів на фронтенді, тож сюди
			// дійти без ручного виклику API неможливо — все одно блокуємо.
			return BrowserInstallResult{}, fmt.Errorf("у шаурма-збірку можна встановлювати лише дозволені моди")
		}
		entries, err := a.packs.GetPackWhitelist(packID)
		if err != nil {
			return BrowserInstallResult{}, fmt.Errorf("не вдалось перевірити білий список збірки: %w", err)
		}
		allowed := false
		for i := range entries {
			if entries[i].Platform == source && entries[i].ProjectID == projectID {
				allowed = true
				break
			}
		}
		if !allowed {
			return BrowserInstallResult{}, fmt.Errorf("цей мод не дозволений білим списком збірки «%s»", sp.Name)
		}
		mcVersion, loaderType, packName = sp.MCVersion, sp.LoaderType, sp.Name
	}
	// Блокуємо тільки РЕАЛЬНУ заміну — файл цього проєкту вже лежить на
	// диску, а не будь-яке встановлення. Новий контент, якого в збірці ще
	// не було, можна докинути в mods//resourcepacks/... без ризику для
	// активної сесії: Minecraft підхопить його лише при наступному
	// запуску, живому JVM-процесу він не заважає.
	//
	// Перевіряємо ФАКТИЧНУ наявність (GetBrowserInstalledMap), а не лише
	// прапорець replace з фронтенду: replace=false все одно може призвести
	// до видалення старого файлу нижче (removeOldProjectFiles спрацьовує
	// незалежно від replace, коли в теці вже лежить файл ЦЬОГО Ж проєкту)
	// — покладатись тільки на прапорець означало б пропустити саме той
	// випадок, який і треба заблокувати.
	//
	// РЕГРЕСІЯ (виправлено): раніше a.errPackRunning(packID) викликався
	// БЕЗУМОВНО для будь-якого встановлення — «Закрийте гру» показувалось
	// навіть для щойно знайденого в браузері мода, якого у збірці не було
	// взагалі, хоча реального ризику для запущеної сесії в цьому випадку
	// немає.
	if replace || a.GetBrowserInstalledMap(packID)[projectID].ProjectID != "" {
		if err := a.errPackRunning(packID); err != nil {
			return BrowserInstallResult{}, err
		}
	}
	// Тип-залежний фільтр лоадера: для ресурспаків/шейдерів/датапаків
	// лоадер збірки не застосовується (contentLoaderApplies) — інакше
	// встановлення ресурспака в Fabric-збірку було б неможливим.
	versions, err := a.GetBrowserProjectVersions(source, projectID, contentDirToProjectType(contentDir), mcVersion, loaderType)
	if err != nil {
		return BrowserInstallResult{}, err
	}
	var v *BrowserVersionView
	if versionID != "" {
		// Конкретна версія: шукаємо серед сумісних зі збіркою (завантажувати
		// несумісну з (mc, loader) збірки безглуздо — не запуститься).
		for i := range versions {
			if versions[i].ID == versionID {
				v = &versions[i]
				break
			}
		}
		if v == nil {
			return BrowserInstallResult{}, fmt.Errorf("версія %s несумісна з Minecraft %s / %s збірки «%s»",
				versionID, mcVersion, loaderType, packName)
		}
	} else if len(versions) > 0 {
		// Найсвіжіша сумісна: вибираємо за Modrinth-правилом «release → beta
		// → alpha» (найновіша стабільна, як CompatibleVersionCard у Modrinth
		// app), а не просто versions[0] — той може бути alpha-прев'ю.
		v = pickBestBrowserVersion(versions)
	}
	if v == nil {
		return BrowserInstallResult{}, fmt.Errorf("немає версій %s під Minecraft %s / %s для збірки «%s»",
			projectID, mcVersion, loaderType, packName)
	}
	fileURLs := a.versionDownloadURLs(source, projectID, v)
	if len(fileURLs) == 0 {
		return BrowserInstallResult{}, fmt.Errorf("версія %s не має посилання на завантаження", v.VersionNumber)
	}

	destDir := filepath.Join(a.gameDir(packID), contentDir)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return BrowserInstallResult{}, err
	}
	// Фолбек-розширення залежить від типу: датапаки/ресурспаки — .zip,
	// моди/шейдери — .jar (FileName зазвичай є, але при порожньому імені
	// неправильне розширення зламало б запуск).
	fallbackExt := ".jar"
	if contentDir == "datapacks" || contentDir == "resourcepacks" {
		fallbackExt = ".zip"
	}
	destFile := filepath.Join(destDir, safeFileName(v.FileName, v.VersionNumber+fallbackExt))
	if !replace {
		if _, err := os.Stat(destFile); err == nil {
			return BrowserInstallResult{}, fmt.Errorf("файл %s уже існує у збірці — оберіть «Замінити»", v.FileName)
		}
	}
	// Видаляємо СТАРІ файли того самого проєкту (у збірці вже є цей мод/
	// ресурспак, але старішої версії) — щоб після встановлення нової версії
	// не лишилося двох версій одного контенту (дублікат у mods/ ламає
	// запуск гри, у resourcepacks/ — плутає гру). Раніше стара версія з
	// ІНШИМ іменем файлу лишалась лежати поруч з новою. Пошук іде за sha1
	// (Modrinth batch) + fallback за slug з імені файлу.
	a.removeOldProjectFiles(destDir, projectID, filepath.Base(destFile))
	// Прогрес встановлення — у подію browser:progress (показуємо на
	// сторінці проєкту/картці, як індикатор завантаження у Modrinth app).
	if err := a.downloadContentFileAny(fileURLs, destFile, projectID, packID, v.FileName); err != nil {
		return BrowserInstallResult{}, fmt.Errorf("не вдалося завантажити: %w", err)
	}
	// Авто-встановлення залежностей (порт Modrinth app "Встановити з
	// залежностями" + theseus apply_content_install): required-залежності
	// версії качаються разом з проєктом (рекурсивно), required-resource-pack
	// companion-файли тієї ж версії — у resourcepacks/. embedded-залежності
	// вже всередині головного архіву, optional — не встановлюються.
	// Помилка залежності НЕ вбиває встановлення головного файлу (воно вже
	// пройшло) — лише логується, щоб користувач знав, чого бракує.
	depsInstalled, depsErr := a.installBrowserVersionDeps(packID, source, v.ID, contentDirToProjectType(contentDir), mcVersion, loaderType, contentDir)
	if depsErr != nil {
		a.logBrowserDepsErr(packID, projectID, v.VersionNumber, depsErr)
	}
	// Скасування кешу оновлень модів — менеджер модів пересканує і покаже
	// новий мод з його проєктом (іконка/оновлення) одразу. Також скидаємо
	// кеш аналізу «вже встановлено», щоб бейджі оновились.
	a.invalidateModUpdateCache(packID)
	a.InvalidateBrowserInstalledCache(packID)
	a.emit("mods:changed", packID)
	msg := "Встановлено"
	if len(depsInstalled) > 0 {
		msg += " + " + strings.Join(depsInstalled, ", ")
	}
	return BrowserInstallResult{
		PackID:      packID,
		FileName:    v.FileName,
		InstalledTo: contentDir + "/" + v.FileName,
		Message:     msg,
	}, nil
}

// installBrowserVersionDeps встановлює разом з головним файлом версії:
//
//  1. required-resource-pack companion-файли ТІЄЇ САМОЇ версії (файли з
//     міткою file_type="required-resource-pack" — Modrinth так позначає
//     текстури, які датапак вимагає встановити разом) → у resourcepacks/;
//  2. required-залежності версії (dependencies[dependency_type=required]) →
//     у mods/ (залежності майже завжди моди), РЕКУРСИВНО (їх власні
//     required-залежності теж). Це порт theseus apply_content_install:
//     Required встановлюється обов'язково, Embedded уже в архіві,
//     Optional — лише за окремим запитом.
//
// Повертає імена встановлених файлів (для повідомлення "Встановлено +
// deps") і помилку лише якщо впав основний резолв (не знайдено версію
// залежності), — проблеми з окремими залежностями логуються, але не
// скасовують інші.
func (a *App) installBrowserVersionDeps(packID, source, versionID, projectType, mcVersion, loader, contentDir string) ([]string, error) {
	if source != "modrinth" {
		// CurseForge не віддає структуровані залежності через цей шлях —
		// залишаємо як є (головний файл встановлюється завжди).
		return nil, nil
	}
	full, err := a.modrinth.GetVersionFull(versionID)
	if err != nil {
		return nil, fmt.Errorf("не вдалося отримати залежності версії %s: %w", versionID, err)
	}
	base := a.gameDir(packID)
	var installed []string
	seen := map[string]bool{versionID: true}

	var walk func(fv *api.FullVersion) error
	walk = func(fv *api.FullVersion) error {
		// 1. Companion-ресурспаки цієї версії (required-resource-pack).
		for _, f := range fv.Files {
			if f.FileType != "required-resource-pack" {
				continue
			}
			destDir := filepath.Join(base, "resourcepacks")
			if err := os.MkdirAll(destDir, 0755); err != nil {
				return err
			}
			dest := filepath.Join(destDir, safeFileName(f.Filename, "resourcepack.zip"))
			if _, err := os.Stat(dest); err == nil {
				continue // уже є — не перезаписуємо
			}
			if err := a.downloadContentFile(f.URL, dest, fv.ProjectID, packID, f.Filename); err != nil {
				a.logBrowserDepsErr(packID, fv.ProjectID, fv.VersionNumber, fmt.Errorf("companion-ресурспак %s: %w", f.Filename, err))
				continue
			}
			installed = append(installed, "resourcepacks/"+f.Filename)
		}
		// 2. Required-залежності (рекурсивно).
		for _, dep := range fv.Dependencies {
			if dep.Type != "required" {
				continue
			}
			depFull, depVersion, err := a.resolveBrowserDependency(packID, projectType, mcVersion, loader, dep)
			if err != nil {
				a.logBrowserDepsErr(packID, fv.ProjectID, fv.VersionNumber, err)
				continue
			}
			if depFull == nil || seen[depVersion] {
				continue
			}
			seen[depVersion] = true
			file := browserPrimaryFile(depFull)
			if file == nil || file.URL == "" {
				continue
			}
			destDir := filepath.Join(base, "mods")
			if err := os.MkdirAll(destDir, 0755); err != nil {
				return err
			}
			dest := filepath.Join(destDir, safeFileName(file.Filename, depVersion+".jar"))
			// Файл уже є — качати не треба, АЛЕ в його власні залежності все
			// одно рекурсуємо (їх може бракувати навіть якщо сам мод присутній
			// — напр. встановлений вручну без deps). Рекурсію гейтимо лише
			// через seen, а не через наявність файла.
			if _, err := os.Stat(dest); err != nil {
				if err := a.downloadContentFile(file.URL, dest, depFull.ProjectID, packID, file.Filename); err != nil {
					a.logBrowserDepsErr(packID, depFull.ProjectID, depFull.VersionNumber, err)
					continue
				}
				installed = append(installed, file.Filename)
			}
			if err := walk(depFull); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(full); err != nil {
		return installed, err
	}
	return installed, nil
}

// resolveBrowserDependency знаходить повну версію required-залежності:
//   - version_id заданий → беремо її напряму (GET /version/{id});
//   - лише project_id → найкраща версія під (mc, loader) збірки (release →
//     beta → alpha, pickBestBrowserVersion — як CompatibleVersionCard).
//
// Портує theseus get_install_candidates: для залежності береться версія,
// сумісна з інстансом, а не перша-ліпша з мети.
func (a *App) resolveBrowserDependency(packID, projectType, mcVersion, loader string, dep api.FullVersionDependency) (*api.FullVersion, string, error) {
	if dep.VersionID != "" {
		v, err := a.modrinth.GetVersionFull(dep.VersionID)
		if err != nil {
			return nil, dep.VersionID, fmt.Errorf("залежність %s: %w", dep.VersionID, err)
		}
		return v, dep.VersionID, nil
	}
	if dep.ProjectID == "" {
		return nil, "", nil
	}
	versions, err := a.GetBrowserProjectVersions("modrinth", dep.ProjectID, projectType, mcVersion, loader)
	if err != nil || len(versions) == 0 {
		return nil, "", fmt.Errorf("залежність %s: немає версій під Minecraft %s / %s збірки «%s»", dep.ProjectID, mcVersion, loader, packID)
	}
	best := pickBestBrowserVersion(versions)
	if best == nil {
		best = &versions[0]
	}
	v, err := a.modrinth.GetVersionFull(best.ID)
	if err != nil {
		return nil, best.ID, fmt.Errorf("залежність %s: %w", dep.ProjectID, err)
	}
	return v, best.ID, nil
}

// browserPrimaryFile — головний файл версії (порт PrimaryFileIndex).
func browserPrimaryFile(fv *api.FullVersion) *api.FullVersionFile {
	if fv == nil {
		return nil
	}
	for i := range fv.Files {
		if fv.Files[i].Primary {
			return &fv.Files[i]
		}
	}
	if len(fv.Files) > 0 {
		return &fv.Files[0]
	}
	return nil
}

// InstallDatapackToWorlds встановлює датапак в ОДИН АБО КІЛЬКА світів
// збірки одразу: .minecraft/saves/<world>/datapacks/<file> для кожного
// вибраного світу. Так користувач обирає світи в модалці (мультивибір:
// один датапак — у кілька світів за один клік; датапаки застосовуються
// лише до вибраних світів, а не до всіх). Файл качається з мережі ОДИН
// раз (у тимчасову теку), після чого копіюється в кожен світ — при
// багатьох світах це не множить трафік.
//
// Companion-ресурспаки версії (file_type="required-resource-pack") — у
// .minecraft/resourcepacks/ збірки (як в InstallBrowserContentVersion).
//
// versionID="" → найсвіжіша сумісна версія (release → beta → alpha) під
// версію гри збірки, той самий pickBestBrowserVersion, що в
// InstallBrowserContentVersion. Прогрес — у подію browser:progress.
func (a *App) InstallDatapackToWorlds(packID string, worlds []string, source, projectID, versionID string) (BrowserInstallResult, error) {
	// Дедуп: той самий світ, вибраний двічі, не має оброблятись двічі.
	uniq := make([]string, 0, len(worlds))
	seen := map[string]bool{}
	for _, w := range worlds {
		w = strings.TrimSpace(w)
		if w == "" || seen[w] {
			continue
		}
		// Захист від path traversal: назва світу підставляється в шлях
		// saves/<world>/datapacks/. Дозволяємо лише просту назву теки (без
		// сепараторів і "..") — інакше можна було б писати поза saves/.
		if w == "." || w == ".." || strings.ContainsAny(w, "/\\") {
			return BrowserInstallResult{}, fmt.Errorf("некоректна назва світу: %q", w)
		}
		seen[w] = true
		uniq = append(uniq, w)
	}
	if packID == "" || len(uniq) == 0 {
		return BrowserInstallResult{}, fmt.Errorf("не вказано збірку або жоден світ")
	}
	pack, ok := a.customPacks.Get(packID)
	if !ok {
		return BrowserInstallResult{}, fmt.Errorf("збірку не знайдено: %s", packID)
	}
	// Тип-залежний фільтр лоадера: для датапаків лоадер збірки не
	// застосовується (contentLoaderApplies) — інакше встановлення датапака
	// в Forge-збірку було б неможливим.
	versions, err := a.GetBrowserProjectVersions(source, projectID, "datapack", pack.MCVersion, "")
	if err != nil {
		return BrowserInstallResult{}, err
	}
	var v *BrowserVersionView
	if versionID != "" {
		for i := range versions {
			if versions[i].ID == versionID {
				v = &versions[i]
				break
			}
		}
		if v == nil {
			return BrowserInstallResult{}, fmt.Errorf("версія %s несумісна з Minecraft %s збірки «%s»",
				versionID, pack.MCVersion, pack.Name)
		}
	} else if len(versions) > 0 {
		v = pickBestBrowserVersion(versions)
	}
	if v == nil {
		return BrowserInstallResult{}, fmt.Errorf("немає версій датапака під Minecraft %s для збірки «%s»",
			pack.MCVersion, pack.Name)
	}
	fileURLs := a.versionDownloadURLs(source, projectID, v)
	if len(fileURLs) == 0 {
		return BrowserInstallResult{}, fmt.Errorf("версія %s не має посилання на завантаження", v.VersionNumber)
	}

	// 1. Качаємо архів ОДИН раз у tmp-файл.
	tmp, err := os.CreateTemp("", "shaurma-datapack-*.zip")
	if err != nil {
		return BrowserInstallResult{}, err
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath)
	if err := a.downloadContentFileAny(fileURLs, tmpPath, projectID, packID, v.FileName); err != nil {
		return BrowserInstallResult{}, fmt.Errorf("не вдалося завантажити: %w", err)
	}

	// 2. Копіюємо архів у кожен вибраний світ. Файл качається один раз —
	// світи лише копіюють локальний архів. Якщо копіювання в якийсь світ
	// впало, повідомляємо, в які світи вже встигли записати (частковий
	// успіх), щоб користувач не перевстановлював усе заново.
	base := a.gameDir(packID)
	fileName := safeFileName(v.FileName, v.VersionNumber+".zip")
	installedTo := make([]string, 0, len(uniq))
	for _, w := range uniq {
		worldDP := filepath.Join(base, "saves", w, "datapacks")
		if err := os.MkdirAll(worldDP, 0755); err != nil {
			if len(installedTo) > 0 {
				return BrowserInstallResult{}, fmt.Errorf("не вдалося створити теку світу «%s»: %w (вже встановлено у: %s)", w, err, strings.Join(installedTo, "; "))
			}
			return BrowserInstallResult{}, fmt.Errorf("не вдалося створити теку datapacks світу «%s»: %w", w, err)
		}
		destFile := filepath.Join(worldDP, fileName)
		in, err := os.Open(tmpPath)
		if err != nil {
			return BrowserInstallResult{}, err
		}
		out, err := os.Create(destFile)
		if err != nil {
			in.Close()
			if len(installedTo) > 0 {
				return BrowserInstallResult{}, fmt.Errorf("не вдалося створити файл у світі «%s»: %w (вже встановлено у: %s)", w, err, strings.Join(installedTo, "; "))
			}
			return BrowserInstallResult{}, err
		}
		if _, err := io.Copy(out, in); err != nil {
			in.Close()
			out.Close()
			os.Remove(destFile)
			if len(installedTo) > 0 {
				return BrowserInstallResult{}, fmt.Errorf("не вдалося скопіювати у світ «%s»: %w (вже встановлено у: %s)", w, err, strings.Join(installedTo, "; "))
			}
			return BrowserInstallResult{}, fmt.Errorf("не вдалося скопіювати у світ «%s»: %w", w, err)
		}
		// Помилка запису може спливти лише при Close (напр. диск повний під
		// час скидання буфера) — перевіряємо і прибираємо битий файл.
		if cerr := out.Close(); cerr != nil {
			in.Close()
			os.Remove(destFile)
			return BrowserInstallResult{}, fmt.Errorf("не вдалося завершити запис у світ «%s»: %w", w, cerr)
		}
		in.Close()
		installedTo = append(installedTo, "saves/"+w+"/datapacks/"+fileName)
	}

	// 3. Companion-ресурспаки цієї версії (required-resource-pack) — у
	// resourcepacks/ збірки; required-залежності (майже завжди моди) — у
	// mods/, рекурсивно. Помилка залежності не вбиває сам датапак.
	depsInstalled, depsErr := a.installBrowserVersionDeps(packID, source, v.ID, "datapack", pack.MCVersion, "", "datapacks")
	if depsErr != nil {
		a.logBrowserDepsErr(packID, projectID, v.VersionNumber, depsErr)
	}
	a.invalidateModUpdateCache(packID)
	a.InvalidateBrowserInstalledCache(packID)
	a.emit("mods:changed", packID)
	var msg string
	if len(uniq) == 1 {
		msg = "Встановлено у світ «" + uniq[0] + "»"
	} else {
		msg = "Встановлено у світи: " + strings.Join(uniq, ", ")
	}
	if len(depsInstalled) > 0 {
		msg += " + " + strings.Join(depsInstalled, ", ")
	}
	return BrowserInstallResult{
		PackID:      packID,
		FileName:    v.FileName,
		InstalledTo: strings.Join(installedTo, "; "),
		Message:     msg,
	}, nil
}

// logBrowserDepsErr логує помилку встановлення залежності/companion-файла
// (не критично — головний файл уже встановлено).
func (a *App) logBrowserDepsErr(packID, projectID, versionNumber string, err error) {
	if err == nil {
		return
	}
	fmt.Printf("[browser] збірка %s: залежність %s (%s) не встановлена: %v\n", packID, projectID, versionNumber, err)
}

// GetBrowserRecommendedVersion повертає «найкращу» сумісну версію проєкту
// під (mcVersion, loader) для даного типу контенту — та сама, яку Modrinth
// app авто-обирає при встановленні у збірку (остання release → beta →
// alpha, CompatibleVersionCard). Лоадер враховується лише для mod/modpack
// (contentLoaderApplies); для ресурспаків/шейдерів/датапаків — лише
// версія гри. Вертає nil, якщо сумісних версій немає.
//
// Це і є відповідь на «нащо іншим типам контенту вибір версії»: коли
// обрано збірку, версія підбирається АВТОМАТИЧНО оцим методом (як
// Modrinth app з інстанс-контекстом); ручний вибір лишається лише для
// пошуку БЕЗ збірки.
func (a *App) GetBrowserRecommendedVersion(source, projectID, projectType, mcVersion, loader string) (*BrowserVersionView, error) {
	versions, err := a.GetBrowserProjectVersions(source, projectID, projectType, mcVersion, loader)
	if err != nil {
		return nil, err
	}
	best := pickBestBrowserVersion(versions)
	if best == nil && len(versions) > 0 {
		best = &versions[0]
	}
	return best, nil
}

// pickBestBrowserVersion обирає «найкращу» версію з відсортованого
// (новіші перші) списку за правилом Modrinth DownloadProject.vue:
// остання release, інакше остання beta, інакше остання alpha. Версії у
// списку вже новіші-перші (API сортує за датою), тож перша знайдена
// версія потрібного каналу і є найновішою.
//
// ВАЖЛИВО (CurseForge): у CurseForge-версій VersionType НЕ заповнюється
// (там немає каналів release/beta/alpha — є лише файли з датами). Раніше
// для будь-якого CurseForge-списку цикл не знаходив жодного каналу і
// повертав nil — «найкращу версію» не вдавалось обрати НІКОЛИ, хоча
// список був непорожній. Це проявлялось як «Спершу обери версію» при
// встановленні мода з CurseForge у збірку (InstallBrowserContentVersion
// з versionID=” не міг підібрати версію автоматично) і як порожній
// вибір версії у модалках — тоді як Modrinth (з каналами) працював.
// Фолбек: якщо жоден канал не знайдено, беремо найсвіжіший файл
// (versions[0] — API віддає новіші перші).
func pickBestBrowserVersion(versions []BrowserVersionView) *BrowserVersionView {
	var beta, alpha *BrowserVersionView
	for i := range versions {
		switch versions[i].VersionType {
		case "release":
			return &versions[i]
		case "beta":
			if beta == nil {
				beta = &versions[i]
			}
		case "alpha":
			if alpha == nil {
				alpha = &versions[i]
			}
		}
	}
	if beta != nil {
		return beta
	}
	if alpha != nil {
		return alpha
	}
	if len(versions) > 0 {
		return &versions[0]
	}
	return nil
}

// downloadContentFile качає файл контенту з прогресом у подію
// browser:progress {projectId, packId, fileName, downloaded, total, percent}.
// Фронтенд підписується через on('browser:progress') і малює прогрес-бар.
func (a *App) downloadContentFile(url, dest, projectID, packID, fileName string) error {
	client := &http.Client{Timeout: 10 * time.Minute}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", "shaurma-team/shaurma-launcher/2.0 (browser-content; shaurmaofficial2210@gmail.com)")
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(time.Duration(attempt+1) * 500 * time.Millisecond)
			continue
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			resp.Body.Close()
			time.Sleep(time.Duration(attempt+1) * 700 * time.Millisecond)
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			return err
		}
		out, err := os.Create(dest)
		if err != nil {
			return err
		}
		total := resp.ContentLength
		var downloaded int64
		buf := make([]byte, 64*1024)
		var copyErr error
		for {
			n, rerr := resp.Body.Read(buf)
			if n > 0 {
				if _, werr := out.Write(buf[:n]); werr != nil {
					copyErr = werr
					break
				}
				downloaded += int64(n)
				// Емітимо прогрес не частіше ніж раз на ~256КБ або на фініші.
				if downloaded%(256*1024) < int64(n) || (rerr == io.EOF && n > 0) {
					a.emitBrowserProgress(projectID, packID, fileName, downloaded, total)
				}
			}
			if rerr == io.EOF {
				break
			}
			if rerr != nil {
				copyErr = rerr
				break
			}
		}
		closeErr := out.Close()
		resp.Body.Close()
		if copyErr != nil {
			os.Remove(dest)
			return copyErr
		}
		if closeErr != nil {
			os.Remove(dest)
			return closeErr
		}
		a.emitBrowserProgress(projectID, packID, fileName, downloaded, total)
		return nil
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("завантаження не вдалось")
}

// emitBrowserProgress шле подію прогресу встановлення контенту.
func (a *App) emitBrowserProgress(projectID, packID, fileName string, downloaded, total int64) {
	percent := 0
	if total > 0 {
		percent = int(downloaded * 100 / total)
		if percent > 100 {
			percent = 100
		}
	}
	a.emit("browser:progress", map[string]interface{}{
		"projectId":  projectID,
		"packId":     packID,
		"fileName":   fileName,
		"downloaded": downloaded,
		"total":      total,
		"percent":    percent,
	})
}

// ── Хелпери ─────────────────────────────────────────────────────────────

func atoiOr0(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// safeFileName прибирає небезпечні символи з імені файлу (шляхи з URL).
func safeFileName(name, fallback string) string {
	name = safeModFileName(name) // %2B → +, одинокий % → _ (інакше мод не запуститься)
	name = strings.ReplaceAll(name, "\\", "_")
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return fallback
	}
	return name
}

// downloadFileTo качає файл за URL у dest (з ретраєм на 429 і перевіркою
// HTTP-статусу) — той самий стиль, що решта лаунчера.
func downloadFileTo(url, dest string) error {
	return downloadFileToResumable(context.Background(), url, dest, nil)
}

// downloadFileToResumable качає файл за URL у dest з підтримкою Range:
// якщо .part вже існує (пауза/крах), докачує з місця зупинки, а не з
// нуля. onBytes отримує кожен прочитаний блок (для плавного прогресу).
// Використовується «Завантажити» модпака з браузера: архів великий, і
// пауза/відновлення без втрати прогресу критичні.
func downloadFileToResumable(ctx context.Context, url, dest string, onBytes func(done, total int64)) error {
	client := &http.Client{Timeout: 5 * time.Minute}
	tmp := dest + ".part"
	var resumeFrom int64
	// Пріоритет завершеному файлу: після краху одразу після rename архів
	// вже повний — перекачувати його з нуля марно.
	if fi, err := os.Stat(dest); err == nil && fi.Size() > 0 {
		return nil
	}
	if fi, err := os.Stat(tmp); err == nil {
		resumeFrom = fi.Size()
	}

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", "shaurma-team/shaurma-launcher/2.0 (browser-content; shaurmaofficial2210@gmail.com)")
		if resumeFrom > 0 {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", resumeFrom))
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(time.Duration(attempt+1) * 500 * time.Millisecond)
			continue
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			resp.Body.Close()
			time.Sleep(time.Duration(attempt+1) * 700 * time.Millisecond)
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
			return fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		// Сервер не підтримує Range — починаємо з нуля. O_TRUNC при
		// resumeFrom==0: старий .part не має лишити хвіст у фінальному файлі.
		if resp.StatusCode == http.StatusOK {
			resumeFrom = 0
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			return err
		}
		flags := os.O_CREATE | os.O_WRONLY
		if resumeFrom == 0 {
			flags |= os.O_TRUNC
		}
		out, err := os.OpenFile(tmp, flags, 0644)
		if err != nil {
			return err
		}
		if resumeFrom > 0 {
			if _, err := out.Seek(resumeFrom, io.SeekStart); err != nil {
				out.Close()
				return err
			}
		}

		total := resp.ContentLength
		if total > 0 && resumeFrom > 0 {
			total += resumeFrom
		}
		var done int64 = resumeFrom
		buf := make([]byte, 64*1024)
		var copyErr error
		for {
			n, rerr := resp.Body.Read(buf)
			if n > 0 {
				if _, werr := out.Write(buf[:n]); werr != nil {
					copyErr = werr
					break
				}
				done += int64(n)
				if onBytes != nil {
					onBytes(done, total)
				}
			}
			if rerr == io.EOF {
				break
			}
			if rerr != nil {
				copyErr = rerr
				break
			}
		}
		closeErr := out.Close()
		if copyErr == nil {
			copyErr = closeErr
		}
		if copyErr != nil {
			// Часткове завантаження: .part лишаємо на диску (резюме), не
			// видаляємо — повторний запуск продовжить з місця зупинки.
			if ctx.Err() != nil {
				return ctx.Err()
			}
			lastErr = copyErr
			time.Sleep(time.Duration(attempt+1) * 500 * time.Millisecond)
			continue
		}
		if err := os.Rename(tmp, dest); err != nil {
			return err
		}
		return nil
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("завантаження не вдалось")
}

func ioCopy(dst *os.File, src interface{ Read([]byte) (int, error) }) (int64, error) {
	buf := make([]byte, 64*1024)
	var total int64
	for {
		n, rerr := src.Read(buf)
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return total, werr
			}
			total += int64(n)
		}
		if rerr != nil {
			if rerr.Error() == "EOF" {
				return total, nil
			}
			return total, rerr
		}
	}
}

// ensureBuildsRegistered — позначає збірку як встановлену після імпорту
// (той самий шлях, що recordCustomImportProgress при Done).
func ensureBuildsRegistered(a *App, packID string) {
	if a.buildsReg != nil {
		a.buildsReg.SetInstalled(packID, builds.KindCustom, "")
	}
}
