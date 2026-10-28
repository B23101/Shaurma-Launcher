package custompack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"shaurma-launcher-backend/internal/model"
)

// curseForgeAPIKey вшивається на етапі збірки через -ldflags
// (-X shaurma-launcher-backend/internal/custompack.curseForgeAPIKey=...),
// той самий підхід, що й tokenShaurma в internal/api/shaurma_packs_full.go.
// Без ключа офіційний CurseForge API (api.curseforge.com) відповідає 403 —
// це очікувано, доки ключ не додано; ImportCurseForgeZip у цьому випадку
// повертає зрозумілу помилку замість мовчазного пропуску модів.
//
// Ключ задається РОЗРОБНИКОМ при збірці (build.bat → -CFApiKey → -ldflags)
// і НЕ налаштовується користувачем у UI — як і інші ключі лаунчера.
// ОКРЕМО (баг 6): у dev/test-режимі без -ldflags ключ береться з env
// CF_API_KEY — щоб CurseForge працював у локальних збірках (міграція
// модів, імпорт .zip-модпаків) без перезбірки з вшитими значеннями.
var curseForgeAPIKey string

// userCurseForgeAPIKey — ВЛАСНИЙ ключ користувача з налаштувань
// (Settings.CurseForgeAPIKey), встановлюється через SetUserCurseForgeAPIKey.
// Має пріоритет над вбудованим ключем розробника: користувач може працювати
// зі СВОЇМ лімітом запитів CurseForge Core API, не чекаючи, поки загальний
// ключ розробника впреться в rate limit (50 запитів/10с на ключ).
var userCurseForgeAPIKey string

func init() {
	if curseForgeAPIKey == "" {
		curseForgeAPIKey = os.Getenv("CF_API_KEY")
	}
}

// effectiveAPIKey — ключ, який реально використовується: власний ключ
// користувача, якщо заданий, інакше вбудований/з env.
func effectiveAPIKey() string {
	if userCurseForgeAPIKey != "" {
		return userCurseForgeAPIKey
	}
	return curseForgeAPIKey
}

// SetUserCurseForgeAPIKey оновлює власний ключ користувача з налаштувань.
// Викликається з app.go (SaveSettings + при старті): порожній рядок =
// повернутись до вбудованого ключа розробника.
func SetUserCurseForgeAPIKey(key string) {
	userCurseForgeAPIKey = strings.TrimSpace(key)
}

// curseForgeAPIBase — base URL CurseForge Core API. var (не const): тест
// переписує на локальний сервер, щоб перевірити обрив мовчазних з'єднань.
var curseForgeAPIBase = cfDefaultAPIBase

const (
	cfDefaultAPIBase = "https://api.curseforge.com/v1"
	// cfMirrorBase — публічне дзеркало без ключа (так само, як CF_MIRROR у
	// Millida Launcher).
	cfMirrorBase = "https://api.curse.tools/v1"
	// CFUnreachableText — що показати, коли недоступні ОБИДВА шляхи.
	CFUnreachableText = "CurseForge не відповідає. Так буває, коли провайдер ріже доступ: увімкніть або вимкніть VPN і повторіть. Моди з Modrinth при цьому встановлюються як зазвичай."
)

// ── Fallback на Modrinth для ЗАБЛОКОВАНИХ модів (Prism FallbackMRBlockedMods) ─
// Коли автор CurseForge-мода увімкнув "3rd party download opt-out", прямий
// URL відсутній (CFFileInfo.Blocked=true). Якщо користувач увімкнув
// налаштування UseModrinthForBlocked — лаунчер шукає ТОЙ САМИЙ мод на
// Modrinth за sha1 і качає звідти. Це не „підміна“ мода: sha1 збігається
// побайтово, тож файл ідентичний (той самий підхід, що Prism Launcher
// робить у FileResolvingTask::netJobFinished).
//
// modrinthResolver — функція пошуку версії на Modrinth за sha1; nil =
// fallback вимкнено. Реалізація задається з app.go (там уже є ModrinthClient
// з GetVersionByHash), щоб custompack не залежав від internal/api.
var modrinthResolver func(ctx context.Context, sha1hex string) (*ModrinthMirror, error)

// ModrinthMirror — результат fallback-пошуку: прямий URL + ім'я файлу
// версії Modrinth, що відповідає sha1 заблокованого CurseForge-мода.
type ModrinthMirror struct {
	URL      string
	Filename string
}

// SetModrinthBlockedResolver реєструє fallback-резолвер (викликається з
// app.go при старті та при зміні налаштування UseModrinthForBlocked).
func SetModrinthBlockedResolver(fn func(ctx context.Context, sha1hex string) (*ModrinthMirror, error)) {
	modrinthResolver = fn
}

// CurseForgeClient — тонкий клієнт над офіційним CurseForge API,
// потрібним лише для одного виклику: резолв (projectID, fileID) → пряме
// посилання на завантаження файлу мода (той самий підхід, яким
// користуються MultiMC/Prism/GDLauncher при імпорті .zip-модпаків).
type CurseForgeClient struct {
	client *http.Client
}

// ── Глобальний rate limiter для CurseForge API ──────────────────────────
// CurseForge Core API має жорсткий ліміт безкоштовного тарифу: 50 запитів
// на 10 секунд на API-ключ. Паралельний резолв модів (resolveCFFiles,
// maxConcurrent воркерів) легко виходить за цей ліміт: після ~50 запитів
// API починає відповідати 429, а короткі фіксовані ретраї лише створюють
// «thundering herd» — і імпорт CurseForge-модпака «розхешує 2-30 модів і
// зависає» (реально: майже всі запити упираються в rate limit, а імпорт
// або валиться, або довго гриндить). Prism Launcher вирішує це глобальним
// спейсером запитів до API — тут той самий підхід: не більше cfRatePerSec
// запитів/сек на ВСІ воркери (4 req/s = 40 req/10s, із запасом під 50/10s).
const cfRatePerSec = 4

type cfRateLimiter struct {
	mu       sync.Mutex
	interval time.Duration
	last     time.Time
}

func newCFRateLimiter(perSec int) *cfRateLimiter {
	return &cfRateLimiter{interval: time.Second / time.Duration(perSec)}
}

// wait блокує до звільнення слота запиту. ctx.Done() розблоковує негайно
// (скасування імпорту не чекає своєї черги на запит).
func (l *cfRateLimiter) wait(ctx context.Context) error {
	for {
		l.mu.Lock()
		next := l.last.Add(l.interval)
		now := time.Now()
		if !now.Before(next) {
			l.last = now
			l.mu.Unlock()
			return nil
		}
		wait := next.Sub(now)
		l.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

var cfRateLimiterInst = newCFRateLimiter(cfRatePerSec)

// curseForgeAPIHeaderTimeout — скільки чекати перші ЗАГОЛОВКИ відповіді
// CurseForge API (резолв (projectID, fileID) → URL на фазі «розхешування
// модів» імпорту модпаку). Без цього мовчазне API-з'єднання (сервер
// прийняв TCP і мовчить) тримало б резолв-горутину весь Timeout клієнта
// (20с на кожен мод — на 200-модному модпаку це виглядало як вічне
// зависання «0.0 МБ/с», хоча й було обмежене). var (не const): тест може
// зменшити для швидкої перевірки.
var curseForgeAPIHeaderTimeout = 15 * time.Second

func NewCurseForgeClient() *CurseForgeClient {
	return &CurseForgeClient{client: &http.Client{
		Timeout: 20 * time.Second,
		// Той самий захист від мовчазних з'єднань, що й у downloadImportTasks
		// (downloadResponseHeaderTimeout) — на фазі отримання заголовків, а не
		// лише на тілі відповіді.
		Transport: &http.Transport{
			// Proxy — збереження поведінки дефолтного транспорту (явний
			// Transport його не успадковує; інакше запити йшли б в обхід
			// системного/корпоративного проксі і ламались би за ним).
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: curseForgeAPIHeaderTimeout,
			IdleConnTimeout:       90 * time.Second,
			ForceAttemptHTTP2:     true,
			// За замовчуванням MaxIdleConnsPerHost=2 — паралельні запити
			// (пошук + сторінка проєкту + файли) постійно відкривали нові
			// TLS-з'єднання до api.curseforge.com.
			MaxIdleConns:        64,
			MaxIdleConnsPerHost: 32,
		},
	}}
}

// HasAPIKey — чи вшито ключ у цю збірку лаунчера. UI показує попередження
// на вкладці "Імпортувати", якщо ключа немає (моди з CurseForge не
// зможуть автоматично довантажитись, лише overrides/локальні файли).
func (c *CurseForgeClient) HasAPIKey() bool { return effectiveAPIKey() != "" }

// HasCurseForgeAPIKey / CurseForgeAPIKeyValue — пакетні (не метод клієнта)
// хелпери для коду поза custompack (наприклад, downloadModFile у
// mods_api.go), якому треба лише перевірити наявність ключа й додати
// x-api-key на прямому HTTP-запиті до CDN, не створюючи CurseForgeClient.
func HasCurseForgeAPIKey() bool     { return effectiveAPIKey() != "" }

// CurseForgeAvailable — CurseForge доступний ЗАВЖДИ: без ключа (власного чи
// вбудованого) запити йдуть через публічне дзеркало api.curse.tools, як у
// Millida Launcher. Ключ лише пришвидшує/стабілізує прямий шлях.
func CurseForgeAvailable() bool { return true }
func CurseForgeAPIKeyValue() string { return curseForgeAPIKey }

// IsCurseForgeCDNURL — true, якщо посилання веде на CDN CurseForge
// (edge.forgecdn.net / media.forgecdn.net), а не на Modrinth чи інший
// хост. Використовується, щоб x-api-key додавався ЛИШЕ до запитів на
// CurseForge CDN — на інших хостах цей заголовок зайвий і нічого не значить.
func IsCurseForgeCDNURL(rawURL string) bool {
	return strings.Contains(rawURL, "forgecdn.net")
}

type cfFileResponse struct {
	Data struct {
		ID          int    `json:"id"`
		FileName    string `json:"fileName"`
		DownloadURL string `json:"downloadUrl"`
		FileLength  int64  `json:"fileLength"`
		// GameVersions — ЗМІШАНИЙ масив: і версії Minecraft, і назва
		// лоадера (напр. ["Fabric", "1.20", "1.20.1"]) — так реально
		// відповідає CurseForge API. Поля "modLoaders" на цьому рівні
		// відповіді НЕ ІСНУЄ (раніше тут стояв `json:"modLoaders"`, який
		// мовчки завжди десеріалізувався в порожній масив) — назва
		// лоадера дістається з GameVersions через cfLoaderNamesFrom.
		GameVersions []string `json:"gameVersions"`
		Hashes       []struct {
			Value string `json:"value"`
			Algo  int    `json:"algo"` // 1 = sha1, 2 = md5
		} `json:"hashes"`
	} `json:"data"`
}

// CFFileInfo — розв'язана інформація про файл мода з CurseForge.
type CFFileInfo struct {
	FileName     string
	DownloadURL  string
	FileLength   int64
	SHA1         string
	GameVersions []string
	ModLoaders   []string
	// Blocked — файл існує і його метадані (включно з SHA1) відомі, але
	// прямого посилання на завантаження немає: автор увімкнув
	// "3rd party download opt-out" і навіть /download-url ендпоінт не
	// віддає URL. Це НЕ те саме, що «файл не знайдено»: лаунчер може
	// спробувати знайти цей самий мод на Modrinth за SHA1 (fallback,
	// налаштування UseModrinthForBlocked — той самий підхід, що
	// FallbackMRBlockedMods у Prism Launcher).
	Blocked bool
}

// ── Браузер контенту (пошук по типах, деталі проєкту) ─────────────────
// CurseForge класифікує контент через classId. Ті самі значення, що
// CurseForge Core API /mods/search?classId=... використовує для фільтра
// типу контенту:
//
//	6    — моди
//	4471 — модпаки
//	12   — ресурспаки (texture packs)
//	6552 — шейдери
//	6945 — датапаки
func cfClassID(projectType string) int {
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
		return 6 // mod
	}
}

// CFSortField — поле сортування /mods/search (ті самі значення, що в
// офіційному клієнті CurseForge): 1=популярні, 2=завантаження,
// 3=оновлено, 4=створено.
func cfSortField(sort string) int {
	switch sort {
	case "downloads":
		return 2
	case "updated":
		return 3
	case "newest":
		return 4
	default:
		return 2 // як у Millida Launcher: за замовчуванням — за кількістю завантажень
	}
}

// CFProjectSummary — компактний опис проєкту для пошуку.
type CFProjectSummary struct {
	ID      int    `json:"id"`
	Slug    string `json:"slug"`
	Name    string `json:"name"`
	Summary string `json:"summary"`
	// Logo — лого проєкту. ВАЖЛИВО: CurseForge API v1 повертає лого у
	// НЕПРАВИЛЬНОМУ для нас форматі: вкладеного об'єкта logo:{url,...},
	// а НЕ рядка logoUrl. Раніше тут був `json:"logoUrl"` — тому картинки
	// збірок у пошуку НІКОЛИ не показувались (поле завжди порожнє).
	Logo struct {
		URL          string `json:"url"`
		ThumbnailURL string `json:"thumbnailUrl"`
	} `json:"logo"`
	// LogoURL — deprecated-фолбек: лишається порожнім, але зберігаємо
	// для сумісності з кодом, який ще читає це поле напряму.
	LogoURL       string   `json:"-"`
	DownloadCount int64    `json:"downloadCount"`
	DateModified  string   `json:"dateModified"`
	DateCreated   string   `json:"dateCreated"`
	GameVersions  []string `json:"gameVersions"`
	// ClassID — клас контенту (6=моди, 4471=модпаки, ...). Для сторінки
	// автора потрібен, щоб визначити тип кожного проєкту автора
	// (SearchProjectsByAuthor без classId повертає проєкти всіх типів).
	ClassID int `json:"classId"`
	// Authors — автори проєкту (id/name/url) — для сторінки автора
	// (CurseForge не має окремого endpoint-а профілю, тож ім'я автора
	// беремо з його проєктів).
	Authors []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
		URL  string `json:"url"`
	} `json:"authors"`
	// Categories — теги проєкту (для фільтра по категоріях і бейджів).
	Categories []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"categories"`
	Links struct {
		WebsiteURL string `json:"websiteUrl"`
	} `json:"links"`
	// Screenshots — галерея проєкту; перше зображення — банер картки.
	Screenshots []struct {
		URL          string `json:"url"`
		ThumbnailURL string `json:"thumbnailUrl"`
	} `json:"screenshots"`
}

// SearchProjects шукає контент будь-якого типу на CurseForge за назвою.
// classId фільтрує тип (моди/модпаки/ресурспаки/шейдери/датапаки),
// gameVersion/modLoaderType — за версією гри та лоадером (ті самі
// параметри, що в офіційному клієнті). Без API-ключа — зрозуміла помилка.
// logoURL — URL лого проєкту з вкладеного logo:{url} (реальний формат
// CurseForge API v1), з фолбеком на старе поле LogoURL.
func (h CFProjectSummary) logoURL() string {
	// Мініатюра (~256px) замість оригіналу: іконка картки — десятки
	// пікселів, а оригінальне лого CF буває 400–1000px.
	if h.Logo.ThumbnailURL != "" {
		return h.Logo.ThumbnailURL
	}
	if h.Logo.URL != "" {
		return h.Logo.URL
	}
	return h.LogoURL
}

func (c *CurseForgeClient) SearchProjects(query, projectType, gameVersion, loader string, sort string, limit int, categoryIDs []int) ([]CFProjectSummary, error) {
	hits, _, err := c.SearchProjectsPage(query, projectType, gameVersion, loader, sort, limit, 0, categoryIDs)
	return hits, err
}

// cfSearchMaxIndex — CurseForge /mods/search відмовляє, якщо index+pageSize
// більше за 10 000 (обмеження API), тож далі цієї межі сторінок немає.
const cfSearchMaxIndex = 10000

// SearchProjectsPage — те саме, що SearchProjects, але для конкретної
// сторінки видачі: offset іде в параметр index (зсув у результатах), а
// другим значенням повертається загальна кількість хітів
// (pagination.totalCount, обрізана межею API) — для пагінації браузера.
// Раніше index не передавався зовсім, і будь-яка сторінка CurseForge
// показувала ту саму першу сторінку.
func (c *CurseForgeClient) SearchProjectsPage(query, projectType, gameVersion, loader string, sort string, limit, offset int, categoryIDs []int) ([]CFProjectSummary, int, error) {
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	if offset+limit > cfSearchMaxIndex {
		return []CFProjectSummary{}, cfSearchMaxIndex, nil
	}
	u := fmt.Sprintf("%s/mods/search?searchFilter=%s&gameId=432&classId=%d&sortField=%d&sortOrder=desc&pageSize=%d&index=%d",
		curseForgeAPIBase, url.QueryEscape(query), cfClassID(projectType), cfSortField(sort), limit, offset)
	if gameVersion != "" {
		u += "&gameVersion=" + url.QueryEscape(gameVersion)
	}
	if loader != "" && strings.EqualFold(projectType, "mod") && cfLoaderTypeID(loader) > 0 {
		u += fmt.Sprintf("&modLoaderType=%d", cfLoaderTypeID(loader))
	}
	// Категорії фільтруються ПРЯМО в CurseForge Search API (categoryId для
	// одного тегу, categoryIds=[...] для декількох — офіційний параметр
	// /mods/search), а НЕ постфактум на клієнті: раніше фільтр застосовувався
	// вже ПІСЛЯ обрізання видачі до `limit` (типово 30) нефільтрованих
	// хітів, тож обраний тег часто відсіював майже все, хоча реально
	// сумісних проєктів під цим тегом могло бути значно більше — вони
	// просто не потрапляли у перші `limit` найпопулярніших хітів без
	// фільтра. З categoryId(s) у запиті CurseForge сам повертає лише
	// проєкти цього тегу, і `limit` обрізає вже ВІДФІЛЬТРОВАНУ видачу.
	if len(categoryIDs) == 1 {
		u += fmt.Sprintf("&categoryId=%d", categoryIDs[0])
	} else if len(categoryIDs) > 1 {
		parts := make([]string, len(categoryIDs))
		for i, id := range categoryIDs {
			parts[i] = strconv.Itoa(id)
		}
		u += "&categoryIds=" + url.QueryEscape("["+strings.Join(parts, ",")+"]")
	}
	// Кеш пошуку (5 хв) + дедуп одночасних однакових запитів: повторний
	// пошук/повернення на вкладку не б'є в rate limiter (4 req/s) і мережу.
	body, err := cfSearchCacheGet(u, func() ([]byte, error) {
		req, err := http.NewRequest(http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		resp, err := c.doRequest(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode == 403 || resp.StatusCode == 401 {
			return nil, fmt.Errorf("CurseForge API відхилив запит (%d) — перевірте API-ключ", resp.StatusCode)
		}
		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("CurseForge API: HTTP %d", resp.StatusCode)
		}
		return io.ReadAll(resp.Body)
	})
	if err != nil {
		return nil, 0, err
	}
	var parsed struct {
		Data       []CFProjectSummary `json:"data"`
		Pagination struct {
			TotalCount int `json:"totalCount"`
		} `json:"pagination"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, 0, err
	}
	total := parsed.Pagination.TotalCount
	if total > cfSearchMaxIndex {
		total = cfSearchMaxIndex
	}
	// ЗАХИСТ ВІД «ТИХ САМИХ ПРОЄКТІВ ПІД УСІМА ВКЛАДКАМИ»: якщо CurseForge
	// проігнорував classId (наприклад, разом із categoryId від ІНШОГО
	// типу контенту), у видачі опиняються проєкти чужого класу — моди під
	// вкладкою ресурспаків тощо. Відкидаємо все, чий classId відомий і не
	// збігається з запитаним типом.
	wantClass := cfClassID(projectType)
	filtered := parsed.Data[:0]
	for _, h := range parsed.Data {
		if h.ClassID != 0 && h.ClassID != wantClass {
			continue
		}
		filtered = append(filtered, h)
	}
	return filtered, total, nil
}

// CFProjectDetail — повний опис проєкту CurseForge (сторінка модпаку).
type CFProjectDetail struct {
	ID            int    `json:"id"`
	Slug          string `json:"slug"`
	Name          string `json:"name"`
	Summary       string `json:"summary"`
	Description   string `json:"description"` // HTML
	DownloadCount int64  `json:"downloadCount"`
	DateCreated   string `json:"dateCreated"`
	DateModified  string `json:"dateModified"`
	Links         struct {
		WebsiteURL string `json:"websiteUrl"`
		IssuesURL  string `json:"issuesUrl"`
		SourceURL  string `json:"sourceUrl"`
		WikiURL    string `json:"wikiUrl"`
		DiscordURL string `json:"discordUrl"`
	} `json:"links"`
	Logo struct {
		URL string `json:"url"`
	} `json:"logo"`
	Screenshots []struct {
		URL   string `json:"url"`
		Title string `json:"title"`
	} `json:"screenshots"`
	Categories []struct {
		Name string `json:"name"`
	} `json:"categories"`
	GameVersions []string `json:"gameVersions"`
	// ModLoaders: на рівні ПРОЄКТУ (Mod) офіційний CurseForge API теж не
	// має такого поля — лоадери видно лише per-файл через
	// latestFilesIndexes[].modLoader (число) чи latestFiles[].gameVersions
	// (див. коментар на CFFileHit/cfFileResponse нижче). Це поле лишене
	// як розрахункове (заповнюється вручну в GetProjectDetail з
	// latestFiles), а не як пряме дзеркало неіснуючого JSON-ключа.
	ModLoaders []string `json:"-"`
	ClassID    int      `json:"classId"`
	// Authors — автори проєкту (id/name/url). Для секції «Автор» і сторінки
	// автора: CurseForge не має endpoint-а профілю користувача, тож це
	// єдине джерело імені автора. Аватар з CurseForge отримати неможливо.
	Authors []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
		URL  string `json:"url"`
	} `json:"authors"`
}

// GetProjectDetail повертає повний опис проєкту CurseForge
// (/mods/{id}) з описом, скріншотами та посиланнями — аналог
// ModrinthClient.GetProjectDetail для сторінки модпаку в браузері.
func (c *CurseForgeClient) GetProjectDetail(projectID int) (*CFProjectDetail, error) {
	url := fmt.Sprintf("%s/mods/%d", curseForgeAPIBase, projectID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.doRequest(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("CurseForge API: HTTP %d (project %d)", resp.StatusCode, projectID)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Data CFProjectDetail `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	d := &parsed.Data
	// ModLoaders — розрахункове поле (JSON-тег "-", реального ключа
	// modLoaders у відповіді CurseForge немає): GameVersions на рівні
	// проєкту так само змішує версії гри й назви лоадерів, як і на
	// рівні файлу (див. cfExtractLoaderName) — розпізнаємо звідти.
	d.ModLoaders = cfLoaderNamesFrom(d.GameVersions)
	// Повний опис проєкту (баг 8): /mods/{id} у полі description часто
	// віддає ЛИШЕ короткий анонс (2 речення), а канонічне джерело повної
	// статті — GET /mods/{id}/description ({data: "<html>"}). Раніше
	// фолбек спрацьовував лише коли description був ПОРОЖНІМ — для
	// модпаків з коротким, але непорожнім description (напр. «Create -
	// Forge Frontier») сторінка контенту показувала 2 речення замість
	// цілої статті. Тепер повний опис завжди має пріоритет над полем
	// /mods/{id}, а коротке поле лишається лише фолбеком.
	if desc, derr := c.getProjectDescription(projectID); derr == nil && strings.TrimSpace(desc) != "" {
		d.Description = desc
	}
	return d, nil
}

// getProjectDescription отримує ПОВНИЙ HTML-опис проєкту з окремого
// ендпоінта /mods/{id}/description — фолбек, коли /mods/{id} віддав
// description порожнім (типово для багатьох модпаків CurseForge).
func (c *CurseForgeClient) getProjectDescription(projectID int) (string, error) {
	url := fmt.Sprintf("%s/mods/%d/description", curseForgeAPIBase, projectID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.doRequest(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("CurseForge API: HTTP %d (description %d)", resp.StatusCode, projectID)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var parsed struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", err
	}
	return parsed.Data, nil
}

// CFToBrowserEntry конвертує CFProjectSummary у model.BrowserEntry
// (спільний контракт браузера: source="curseforge", id=slug, projectId
// = числовий ID).
func CFToBrowserEntry(h CFProjectSummary, projectType string) model.BrowserEntry {
	entry := model.BrowserEntry{
		ID:           h.Slug,
		ProjectID:    fmt.Sprintf("%d", h.ID),
		Name:         h.Name,
		Type:         projectType,
		Description:  h.Summary,
		Downloads:    h.DownloadCount,
		IconURL:      h.logoURL(),
		Source:       "curseforge",
		DateCreated:  h.DateCreated,
		DateModified: h.DateModified,
	}
	if len(h.GameVersions) > 0 {
		entry.MCVersion = h.GameVersions[0]
	}
	for _, c := range h.Categories {
		entry.Categories = append(entry.Categories, c.Name)
	}
	if len(h.Screenshots) > 0 {
		// Мініатюра замість повного скриншота: картка показує банер ~340px,
		// а повнорозмірний (до 1920×1080) WebView2 декодує у ~8 МБ bitmap.
		entry.BannerURL = h.Screenshots[0].URL
		if h.Screenshots[0].ThumbnailURL != "" {
			entry.BannerURL = h.Screenshots[0].ThumbnailURL
		}
	}
	return entry
}

// ── Категорії CurseForge для фільтра браузера ──────────────────────────

// CFCategory — один тег-категорія CurseForge (вкладена категорія з класу
// контенту, напр. "Performance" у класі "Mods").
type CFCategory struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Slug     string `json:"slug"`
	IconURL  string `json:"iconUrl"`
	ClassID  int    `json:"classId"`
	ParentID int    `json:"parentCategoryId"`
	IsClass  bool   `json:"isClass"`
}

// GetCategories повертає категорії-теги CurseForge для гри Minecraft
// (gameId=432). Класи-контейнери (isClass=true) відсікаються — фільтр
// браузера показує лише вкладені теги (як у сайдбарі веб-апки Modrinth).
func (c *CurseForgeClient) GetCategories() ([]CFCategory, error) {
	u := curseForgeAPIBase + "/categories?gameId=432"
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.doRequest(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("CurseForge API: HTTP %d (categories)", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var all []CFCategory
	if err := json.Unmarshal(body, &all); err != nil {
		return nil, err
	}
	out := make([]CFCategory, 0, len(all))
	for _, c := range all {
		if !c.IsClass && c.ParentID > 0 {
			out = append(out, c)
		}
	}
	return out, nil
}

// ── Сторінка автора (CurseForge) ──────────────────────────────────────
// ВАЖЛИВО: CurseForge Core API НЕ має endpoint-а для профілю користувача
// (немає /users/{id} і немає /users/{id}/projects). Профіль автора (аватар,
// біо, дата реєстрації) з CurseForge отримати НЕ можна. Єдине, що API
// дозволяє — /mods/search з фільтром authorId / primaryAuthorId: список
// проєктів конкретного автора. Тому сторінка автора CurseForge показує
// ім'я (з проєкту) і сітку його проєктів, а аватар — запасний (літера).

// SearchProjectsByAuthor шукає проєкти конкретного автора за authorId
// (/mods/search?authorId=...) — єдиний спосіб отримати «сторінку автора»
// з CurseForge. Без classId (автор може мати проєкти різних типів).
func (c *CurseForgeClient) SearchProjectsByAuthor(authorID int, sort string, limit int) ([]CFProjectSummary, error) {
	if limit <= 0 {
		limit = 50
	}
	u := fmt.Sprintf("%s/mods/search?gameId=432&authorId=%d&sortField=%d&sortOrder=desc&pageSize=%d",
		curseForgeAPIBase, authorID, cfSortField(sort), limit)
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.doRequest(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 403 || resp.StatusCode == 401 {
		return nil, fmt.Errorf("CurseForge API відхилив запит (%d) — перевірте API-ключ", resp.StatusCode)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("CurseForge API: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Data []CFProjectSummary `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	return parsed.Data, nil
}

// ── Пошук модів / список файлів (менеджер модів, вкладка «Моди») ───────
// Використовується як fallback, коли мод не знайдено на Modrinth
// (більшість Forge/NeoForge модів є на обох платформах, але деякі старі
// живуть лише на CurseForge). Потребує API-ключ (див. curseForgeAPIKey).

// CFSearchHit — знайдений мод.
type CFSearchHit struct {
	ID      int    `json:"id"`
	Slug    string `json:"slug"`
	Name    string `json:"name"`
	Summary string `json:"summary"`
	// Logo — той самий фікс вкладеного об'єкта, що й у CFProjectSummary
	// (logo:{url}), бо CurseForge API v1 ніколи не віддає logoUrl.
	Logo struct {
		URL string `json:"url"`
	} `json:"logo"`
	LogoURL string `json:"-"`
	Links   struct {
		WebsiteURL string `json:"websiteUrl"`
	} `json:"links"`
}

// CFFileHit — файл (версія) мода CurseForge.
type CFFileHit struct {
	ID           int      `json:"id"`
	FileName     string   `json:"fileName"`
	DownloadURL  string   `json:"downloadUrl"`
	FileDate     string   `json:"fileDate"`
	GameVersions []string `json:"gameVersions"`
	ModLoaders   []string `json:"modLoaders"`
	// ReleaseType — канал релізу файлу CurseForge: 1=release, 2=beta,
	// 3=alpha (офіційне поле releaseType /mods/{id}/files). Раніше його
	// взагалі не читали: для CurseForge pickBestBrowserVersion бачив
	// порожній VersionType і завжди обирав найсвіжіший файл — а ним може
	// бути beta/alpha, випущена після останнього стабільного релізу
	// (реально: у JEI-файлів 1.19.2/1.21.1 на CurseForge перші 3-4 файли
	// за датою — beta, а стабільний release лежить нижче). Це і є
	// «сортування/вибір версії мода згідно збірки»: має братись останній
	// СТАБІЛЬНИЙ реліз, як у GDLauncher/Modrinth (release → beta → alpha).
	ReleaseType int `json:"releaseType"`
	Hashes      []struct {
		Value string `json:"value"`
		Algo  int    `json:"algo"`
	} `json:"hashes"`
}

// CFReleaseTypeName — канал релізу файлу CurseForge як назва, зрозуміла
// решті бекенду/UI (та сама шкала, що VersionType у Modrinth): 1=release,
// 2=beta, 3=alpha; усе інше (0/невідоме) = "" — щоб старий код, який не
// знає про канали, однаково отримував коректний фолбек.
func CFReleaseTypeName(releaseType int) string {
	switch releaseType {
	case 1:
		return "release"
	case 2:
		return "beta"
	case 3:
		return "alpha"
	default:
		return ""
	}
}

// CFLatestFile — «найкращий» файл з відсортованого GetModFiles списку
// (новіші-перші) за каналом релізу. Семантика — як LatestVersionFor для
// Modrinth (allowAlphaBeta):
//   - release — завжди дозволений, обирається найсвіжіший release;
//   - файли без releaseType (0/невідоме — старі файли до появи каналів)
//     вважаються стабільними і теж дозволені завжди (інакше для них
//     оновлення б «мовчазно зникло»);
//   - beta — лише коли allowBeta (і release/невідомого немає);
//   - alpha — лише коли allowBeta й allowAlpha;
//   - якщо дозволеного файлу немає взагалі — nil (викликач вирішує:
//     «немає новішої версії» замість встановлення небажаного каналу).
//
// Раніше «найсвіжіший» брався сліпо (files[0]) всюди поза браузером
// (оновлення модів, міграція) — найсвіжішим міг бути beta/alpha,
// випущена після останнього стабільного релізу.
func CFLatestFile(files []CFFileHit, allowBeta, allowAlpha bool) *CFFileHit {
	// Правило — «найсвіжіший ДОЗВОЛЕНИЙ»: список уже відсортований
	// новіші-перші, тож перший файл, чий канал дозволений, і є відповіддю.
	//   - release (1) і невідомий канал (0, легасі-файли) — завжди;
	//   - beta (2) — лише з дозволом прев'ю (allowBeta чи allowAlpha);
	//   - alpha (3) — лише з allowAlpha.
	// При дозволі прев'ю найсвіжішим може бути beta/alpha, випущена після
	// останнього стабільного релізу, — це і є «оновлення з прев'ю-каналом».
	// Без дозволу — найсвіжіший release/невідомий; коли таких немає, а є
	// лише beta/alpha — nil (немає дозволеної версії, а не помилка мережі).
	allowed := func(rt int) bool {
		switch rt {
		case 1, 0:
			return true
		case 2:
			return allowBeta || allowAlpha
		case 3:
			return allowAlpha
		default:
			return true
		}
	}
	for i := range files {
		if allowed(files[i].ReleaseType) {
			return &files[i]
		}
	}
	return nil
}

func (h *CFFileHit) SHA1() string {
	for _, x := range h.Hashes {
		if x.Algo == 1 {
			return x.Value
		}
	}
	return ""
}

// cfLoaderTypeID — числовий код лоадера для параметра modLoaderType
// CurseForge API. 0 = «Any» (офіційне значення CurseForge для «без
// фільтра за лоадером», те саме, що НЕ передавати modLoaderType
// узагалі) — повертається для порожнього/невідомого лоадера. Раніше тут
// був default-фолбек на 1 (Forge): невідома чи майбутня назва лоадера
// (регістр, нова назва на кшталт "legacyfabric", помилка передачі) тихо
// перетворювалась на «шукати ЛИШЕ Forge-файли», через що GetModFiles
// повертав порожній список і сумісна збірка помилково показувалась як
// непідтримувана, хоча насправді проєкт мав версію під її лоадер —
// просто не під Forge, на який мовчки підмінявся запит.
func cfLoaderTypeID(loader string) int {
	switch strings.ToLower(strings.TrimSpace(loader)) {
	case "forge":
		return 1
	case "fabric":
		return 4
	case "quilt":
		return 5
	case "neoforge":
		return 6
	default:
		return 0 // Any — не звужувати запит за лоадером
	}
}

// doRequest виконує запит до CurseForge API з ретраєм на 429 (rate limit) —
// той самий підхід, що й у ModrinthClient.fetch: без нього паралельне
// сканування великої збірки (modUpdateConcurrency воркерів одночасно)
// губило б моди при першому ж 429 замість того, щоб почекати й повторити.
func (c *CurseForgeClient) doRequest(req *http.Request) (*http.Response, error) {
	return c.doRequestCtx(context.Background(), req)
}

// doRequestCtx — той самий doRequest, але з контекстом: ретрай-пауза на 429
// поважає скасування (користувач натиснув «Скасувати» під час імпорту
// модпаку) і не тримає резолв-горутину на весь час сну.
func (c *CurseForgeClient) doRequestCtx(ctx context.Context, req *http.Request) (*http.Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	// Дзеркало можливе лише для GET на офіційний API (порт cf_get з Millida
	// Launcher: основний шлях → публічне дзеркало без ключа).
	reqURL := req.URL.String()
	canMirror := (req.Method == http.MethodGet || (req.Method == http.MethodPost && req.GetBody != nil)) &&
		curseForgeAPIBase == cfDefaultAPIBase &&
		strings.HasPrefix(reqURL, cfDefaultAPIBase)
	if !canMirror {
		return c.doRequestDirect(ctx, req, true)
	}
	if effectiveAPIKey() != "" {
		resp, err := c.doRequestDirect(ctx, req, true)
		if err == nil && resp.StatusCode != 401 && resp.StatusCode != 403 && resp.StatusCode < 500 {
			return resp, nil
		}
		if ctx.Err() != nil {
			if resp != nil {
				resp.Body.Close()
			}
			return nil, ctx.Err()
		}
		if resp != nil {
			resp.Body.Close()
		}
	}
	var mbody io.Reader
	if req.Method == http.MethodPost && req.GetBody != nil {
		if rc, gerr := req.GetBody(); gerr == nil {
			mbody = rc
		}
	}
	mreq, err := http.NewRequest(req.Method, cfMirrorBase+strings.TrimPrefix(reqURL, cfDefaultAPIBase), mbody)
	if err != nil {
		return nil, err
	}
	if ct := req.Header.Get("Content-Type"); ct != "" {
		mreq.Header.Set("Content-Type", ct)
	}
	resp, err := c.doRequestDirect(ctx, mreq, false)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%s", CFUnreachableText)
	}
	return resp, nil
}

// doRequestDirect — один запит із rate limiter-ом і ретраями на 429.
// withKey=false — без x-api-key (запит на публічне дзеркало).
func (c *CurseForgeClient) doRequestDirect(ctx context.Context, req *http.Request, withKey bool) (*http.Response, error) {
	if withKey && effectiveAPIKey() != "" {
		req.Header.Set("x-api-key", effectiveAPIKey())
	}
	req.Header.Set("Accept", "application/json")
	// CurseForge, як і Modrinth, радить унікальний User-Agent з контактом
	// замість дефолтного Go http.Client (порожній) — щоб трафік не виглядав
	// як анонімний бот і не потрапив під блокування без попередження.
	req.Header.Set("User-Agent", "shaurma-team/shaurma-launcher/2.0 (shaurmaofficial2210@gmail.com)")
	if ctx != nil {
		req = req.WithContext(ctx)
	}
	// Спейсер запитів: навіть 4 паралельні воркери не перевищать
	// cfRatePerSec запитів/сек — замість лавини 429 (див. cfRateLimiter).
	for attempt := 0; attempt < 5; attempt++ {
		if ctx != nil && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err := cfRateLimiterInst.wait(ctx); err != nil {
			return nil, err
		}
		resp, err := c.client.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			// Retry-After (якщо сервер дав) має пріоритет над фіксованим
			// бекофом; капуємо на 10с, щоб імпорт не завис надовго.
			sleep := time.Duration(attempt+1) * 700 * time.Millisecond
			if ra := resp.Header.Get("Retry-After"); ra != "" {
				if secs, perr := strconv.Atoi(ra); perr == nil && secs > 0 {
					if s := time.Duration(secs) * time.Second; s < 10*time.Second {
						sleep = s
					}
				}
			}
			resp.Body.Close()
			if ctx != nil {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(sleep):
				}
			} else {
				time.Sleep(sleep)
			}
			continue
		}
		return resp, nil
	}
	return nil, fmt.Errorf("CurseForge API: rate limit")
}

// SearchMods шукає моди на CurseForge за назвою (+ версія гри + лоадер).
// Без ключа — повертає зрозумілу помилку.
func (c *CurseForgeClient) SearchMods(query, gameVersion, loader string) ([]CFSearchHit, error) {
	u := fmt.Sprintf("%s/mods/search?searchFilter=%s&gameId=432&classId=6&sortField=1&sortOrder=desc&pageSize=20",
		curseForgeAPIBase, url.QueryEscape(query))
	if gameVersion != "" {
		u += "&gameVersion=" + url.QueryEscape(gameVersion)
	}
	if loader != "" {
		u += fmt.Sprintf("&modLoaderType=%d", cfLoaderTypeID(loader))
	}
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.doRequest(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 403 || resp.StatusCode == 401 {
		return nil, fmt.Errorf("CurseForge API відхилив запит (%d) — перевірте API-ключ", resp.StatusCode)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("CurseForge API: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Data []CFSearchHit `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	return parsed.Data, nil
}

// GetModFiles повертає файли (версії) мода проєкту, опційно відфільтровані
// за версією гри та лоадером.
//
// РЕГРЕСІЯ (виправлено): раніше запит брав ЛИШЕ перші 50 файлів
// (pageSize=50, без index — API повертає перший page) і БЕЗ
// gameVersion=/modLoaderType= у самому запиті — фільтрація йшла вручну
// вже після відповіді, локально, тільки серед цих 50. У проєкта з
// довгою історією версій (сотні файлів) потрібна версія майже напевно
// лежала за межами перших 50 — фільтр казав «немає сумісних файлів»,
// хоча підтримка насправді була. Це проявлялось як: (1) пошук показує
// мод «непідтримується» для версії/лоадера, які CurseForge насправді
// підтримує; (2) сторінка модпаку не знаходить жодної сумісної версії →
// кнопка «Встановити» видає «Спершу обери версію»; (3) те саме в
// модальному вікні встановлення модпаку.
//
// Виправлення: gameVersion=/modLoaderType= тепер ідуть ПРЯМО в запиті —
// CurseForge фільтрує на своєму боці (офіційно документований параметр
// /v1/mods/{modId}/files), а не лаунчер локально на обрізаному зрізі.
// Максимальний pageSize у CurseForge API — рівно 50 (документований
// ліміт, більше сервер не віддасть за раз) — тому для проєктів, де
// навіть після серверного фільтра файлів більше 50, потрібна
// пагінація через index: цикл нижче йде сторінками, поки
// pagination.totalCount не вичерпано, з розумною верхньою межею на
// кількість сторінок (не гнатись за 10000-лімітом API для одного
// проєкту — жоден реальний мод не має тисяч сумісних файлів під
// конкретну (mcVersion, loader) пару; 10 сторінок = 500 файлів більш
// ніж достатньо і захищає від патологічного проєкту, що поклав би
// цей виклик на десятки секунд).
func (c *CurseForgeClient) GetModFiles(projectID int, gameVersion, loader string) ([]CFFileHit, error) {
	// Порт підбору файлів з Millida Launcher (cf_files_for + cf_file_fits):
	// 1) просимо API відфільтрувати за gameVersion + modLoaderType;
	// 2) якщо порожньо — повторюємо БЕЗ modLoaderType: частина файлів
	//    CurseForge не має loader-тегу (або має нестандартний), і серверний
	//    фільтр їх відсікає — звідси «версія є, а лаунчер її не бачить»;
	// 3) клієнтська перевірка: файл без loader-тегу вважається сумісним
	//    (як у Millida: «без тегів — суддя лише версія гри»).
	files, err := c.getModFilesPages(projectID, gameVersion, loader, true)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 && loader != "" {
		files, err = c.getModFilesPages(projectID, gameVersion, loader, false)
		if err != nil {
			return nil, err
		}
	}
	return files, nil
}

func (c *CurseForgeClient) getModFilesPages(projectID int, gameVersion, loader string, sendLoaderParam bool) ([]CFFileHit, error) {
	const pageSize = 50 // максимум, який дозволяє CurseForge API
	const maxPages = 10 // safety net
	var out []CFFileHit
	for page := 0; page < maxPages; page++ {
		u := fmt.Sprintf("%s/mods/%d/files?pageSize=%d&index=%d", curseForgeAPIBase, projectID, pageSize, page*pageSize)
		if gameVersion != "" {
			u += "&gameVersion=" + url.QueryEscape(gameVersion)
		}
		if loader != "" && sendLoaderParam && cfLoaderTypeID(loader) > 0 {
			u += fmt.Sprintf("&modLoaderType=%d", cfLoaderTypeID(loader))
		}
		// Кеш (5 хв) + дедуп одночасних запитів: перевірка оновлень, модалка
		// версій і сторінка проєкту більше не б'ють в rate limiter 4/с по
		// кілька разів на той самий мод.
		body, err := cfSearchCacheGet(u, func() ([]byte, error) {
			req, err := http.NewRequest(http.MethodGet, u, nil)
			if err != nil {
				return nil, err
			}
			resp, err := c.doRequest(req)
			if err != nil {
				return nil, err
			}
			defer resp.Body.Close()
			if resp.StatusCode != 200 {
				return nil, fmt.Errorf("CurseForge API: HTTP %d (project %d)", resp.StatusCode, projectID)
			}
			return io.ReadAll(resp.Body)
		})
		if err != nil {
			return nil, err
		}
		var parsed struct {
			Data       []CFFileHit `json:"data"`
			Pagination struct {
				TotalCount int `json:"totalCount"`
			} `json:"pagination"`
		}
		if err := json.Unmarshal(body, &parsed); err != nil {
			return nil, err
		}
		for _, f := range parsed.Data {
			if gameVersion != "" && !containsStr(f.GameVersions, gameVersion) {
				continue
			}
			// Файл без loader-тегу — сумісний (суддя лише версія гри).
			if loader != "" {
				if tags := cfLoaderNamesFrom(f.GameVersions); len(tags) > 0 && !containsLoader(f.GameVersions, loader) {
					continue
				}
			}
			if ln := cfExtractLoaderName(f.GameVersions); ln != "" {
				f.ModLoaders = []string{ln}
			}
			// downloadUrl=null у списку файлів — будуємо CDN-посилання.
			if f.DownloadURL == "" {
				f.DownloadURL = cfCDNURL(f.ID, f.FileName)
			}
			out = append(out, f)
		}
		fetched := page*pageSize + len(parsed.Data)
		if len(parsed.Data) < pageSize || fetched >= parsed.Pagination.TotalCount {
			break
		}
	}
	// «Новіші перші» по ВСЬОМУ списку (вибір «останньої версії» на цьому
	// покладається).
	sort.SliceStable(out, func(i, j int) bool {
		return cfParseFileDate(out[i].FileDate).After(cfParseFileDate(out[j].FileDate))
	})
	return out, nil
}

// GetProjectsByIDs — пакетне отримання проєктів (POST /mods) для іконок/slug
// після fingerprint-розпізнавання (у Millida cf_projects, тут одним запитом).
func (c *CurseForgeClient) GetProjectsByIDs(ids []int) (map[int]CFProjectSummary, error) {
	out := map[int]CFProjectSummary{}
	for start := 0; start < len(ids); start += 100 {
		end := start + 100
		if end > len(ids) {
			end = len(ids)
		}
		body, _ := json.Marshal(map[string]any{"modIds": ids[start:end]})
		req, err := http.NewRequest(http.MethodPost, curseForgeAPIBase+"/mods", bytes.NewReader(body))
		if err != nil {
			return out, err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.doRequest(req)
		if err != nil {
			return out, err
		}
		raw, rerr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if rerr != nil || resp.StatusCode != 200 {
			continue
		}
		var parsed struct {
			Data []CFProjectSummary `json:"data"`
		}
		if json.Unmarshal(raw, &parsed) == nil {
			for _, p := range parsed.Data {
				out[p.ID] = p
			}
		}
	}
	return out, nil
}

// CFLogoURL — мініатюра лого проєкту (публічний доступ до logoURL()).
func (h CFProjectSummary) CFLogoURL() string { return h.logoURL() }

// cfParseFileDate — time.Time з FileDate CurseForge (RFC3339 з частковими
// секундами, напр. "2026-08-31T13:57:10.67Z"); невідомий формат = нульовий
// час (такі файли йдуть у кінець сортування).
func cfParseFileDate(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// cfCDNURL — пряме посилання CurseForge CDN за fileId і назвою файлу
// (https://edge.forgecdn.net/files/<id/1000>/<id%1000>/<fileName>).
// Порожній рядок, якщо немає даних для побудови.
func cfCDNURL(fileID int, fileName string) string {
	if fileID <= 0 || fileName == "" {
		return ""
	}
	return fmt.Sprintf("https://edge.forgecdn.net/files/%d/%d/%s",
		fileID/1000, fileID%1000, url.PathEscape(fileName))
}

// isForgeCDNURL — лише https і лише *.forgecdn.net (метадані можуть прийти з
// дзеркала, тож стороннім посиланням не віримо).
func isForgeCDNURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" {
		return false
	}
	return strings.HasSuffix(strings.ToLower(u.Hostname()), ".forgecdn.net")
}

// CurseForgeFileURLs — упорядкований список адрес файлу (порт cf_file_urls з
// Millida): downloadUrl з API (якщо веде на CDN CurseForge), потім
// edge.forgecdn.net і mediafilez.forgecdn.net — для частини файлів edge
// віддає 403, а mediafilez той самий шлях.
func CurseForgeFileURLs(downloadURL string, fileID int, fileName string) []string {
	var out []string
	if downloadURL != "" && isForgeCDNURL(downloadURL) {
		out = append(out, downloadURL)
	}
	if fileID > 0 && fileName != "" {
		enc := url.PathEscape(fileName)
		for _, host := range []string{"https://edge.forgecdn.net", "https://mediafilez.forgecdn.net"} {
			u := fmt.Sprintf("%s/files/%d/%d/%s", host, fileID/1000, fileID%1000, enc)
			dup := false
			for _, e := range out {
				if e == u {
					dup = true
				}
			}
			if !dup {
				out = append(out, u)
			}
		}
	}
	return out
}

// ResolveFileDownloadURL повертає посилання на завантаження файлу
// CurseForge: спершу офіційний /download-url, потім CDN-фолбек за
// fileId/fileName. Порожній рядок — посилання отримати неможливо.
func (c *CurseForgeClient) ResolveFileDownloadURL(projectID, fileID int, fileName string) string {
	if u, err := c.resolveDownloadURL(projectID, fileID); err == nil && u != "" {
		return u
	}
	return cfCDNURL(fileID, fileName)
}

// resolveDownloadURL — фолбек-запит до /mods/{id}/files/{id}/download-url,
// коли основна відповідь /files/{id} прийшла з порожнім downloadUrl.
// CurseForge повертає рядок з прямим посиланням у полі data навіть для
// файлів, для яких основний ендпоінт приховує downloadUrl.
func (c *CurseForgeClient) resolveDownloadURL(projectID, fileID int) (string, error) {
	return c.resolveDownloadURLCtx(context.Background(), projectID, fileID)
}

// resolveDownloadURLCtx — версія resolveDownloadURL з контекстом (скасування
// імпорту обриває запит, що висить).
func (c *CurseForgeClient) resolveDownloadURLCtx(ctx context.Context, projectID, fileID int) (string, error) {
	url := fmt.Sprintf("%s/mods/%d/files/%d/download-url", curseForgeAPIBase, projectID, fileID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.doRequestCtx(ctx, req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("CurseForge API: HTTP %d (download-url, project %d, file %d)", resp.StatusCode, projectID, fileID)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var parsed struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", err
	}
	if parsed.Data == "" {
		return "", fmt.Errorf("порожнє посилання на завантаження")
	}
	return parsed.Data, nil
}

// ── Fingerprint-резолв (експорт у CurseForge-формат) ────────────────────
// CurseForge ідентифікує файли модів за «fingerprint» — 32-бітним Murmur2
// хешем вмісту (алгоритм з Prism Launcher, реалізація в internal/export).
// Щоб зібрати manifest.json з files[] (projectID/fileID), хешуємо локальні
// моди, відправляємо список fingerprint-ів на POST /v1/fingerprints і
// отримуємо назад modId + fileId для кожного точного збігу.

// CFFingerprintMatch — файл мода, знайдений на CurseForge за fingerprint.
type CFFingerprintMatch struct {
	Fingerprint string // десятковий рядок fingerprint (ключ мапи)
	ProjectID   int    // modId (CurseForge project)
	FileID      int    // id файлу (CurseForge file)
	FileName    string // displayName з відповіді API
}

// ResolveFingerprints відправляє список fingerprint-ів на POST
// /v1/fingerprints і повертає мапу fingerprint → знайдений файл.
// Точні збіги (exactMatches) — це файли, які CurseForge розпізнав
// стовідсотково; partialMatches ігноруємо (як Prism FlameCheckUpdate).
// Якщо ключ не налаштований або API недоступний — повертається помилка
// (викликач вирішує: curseforge-експорт без резолву все одно валідний,
// всі файли підуть у overrides/).
func (c *CurseForgeClient) ResolveFingerprints(fingerprints []string) (map[string]CFFingerprintMatch, error) {
	if len(fingerprints) == 0 {
		return map[string]CFFingerprintMatch{}, nil
	}

	// API очікує числа (uint64), а не рядки.
	nums := make([]uint64, 0, len(fingerprints))
	for _, f := range fingerprints {
		if n, err := strconv.ParseUint(f, 10, 64); err == nil {
			nums = append(nums, n)
		}
	}
	if len(nums) == 0 {
		return map[string]CFFingerprintMatch{}, nil
	}

	body, err := json.Marshal(map[string]any{"fingerprints": nums})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, curseForgeAPIBase+"/fingerprints", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.doRequest(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 403 || resp.StatusCode == 401 {
		return nil, fmt.Errorf("CurseForge API відхилив запит (%d) — перевірте API-ключ", resp.StatusCode)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("CurseForge API: HTTP %d (fingerprints)", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Data struct {
			ExactMatches []struct {
				File struct {
					ID          int    `json:"id"`
					ModID       int    `json:"modId"`
					FileName    string `json:"displayName"`
					Fingerprint uint64 `json:"fileFingerprint"`
				} `json:"file"`
			} `json:"exactMatches"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	out := make(map[string]CFFingerprintMatch, len(parsed.Data.ExactMatches))
	for _, m := range parsed.Data.ExactMatches {
		if m.File.ID == 0 || m.File.ModID == 0 {
			continue
		}
		key := strconv.FormatUint(m.File.Fingerprint, 10)
		out[key] = CFFingerprintMatch{
			Fingerprint: key,
			ProjectID:   m.File.ModID,
			FileID:      m.File.ID,
			FileName:    m.File.FileName,
		}
	}
	return out, nil
}

// cfLoaderNamesFrom — усі розпізнані назви лоадерів серед елементів
// gameVersions (зазвичай один, але лишаємо можливість кількох про
// всяк випадок — CurseForge теоретично може позначити файл під
// декілька лоадерів одразу, хоча на практиці це рідкість).
func cfLoaderNamesFrom(gameVersions []string) []string {
	var out []string
	for _, v := range gameVersions {
		lv := strings.ToLower(strings.TrimSpace(v))
		if lv == "forge" || lv == "fabric" || lv == "quilt" || lv == "neoforge" || lv == "cauldron" || lv == "liteloader" {
			out = append(out, v)
		}
	}
	return out
}

// CFVersionsOnly — публічний хелпер (пакет custompack) для решти
// бекенду: версії гри з масиву CurseForge gameVersions БЕЗ домішаних
// туди назв лоадерів ("Fabric"/"Forge"/...) — див. cfKnownLoaderNames.
func CFVersionsOnly(gameVersions []string) []string {
	out := make([]string, 0, len(gameVersions))
	for _, v := range gameVersions {
		if !cfKnownLoaderNames[strings.ToLower(strings.TrimSpace(v))] {
			out = append(out, v)
		}
	}
	return out
}

// CFLoaderNamesFrom — публічний хелпер (пакет custompack) для решти
// бекенду: усі розпізнані назви лоадерів у масиві CurseForge
// gameVersions (див. cfLoaderNamesFrom).
func CFLoaderNamesFrom(gameVersions []string) []string {
	return cfLoaderNamesFrom(gameVersions)
}

func containsStr(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// cfKnownLoaderNames — назви модлоадерів, які CurseForge API підмішує
// прямо в масив gameVersions поруч із версіями Minecraft (підтверджено
// реальною відповіддю API, напр. файл AppleSkin-Fabric:
// "gameVersions": ["Fabric", "1.20", "1.20.1"] — окремого поля
// "modLoaders" у відповіді /mods/{id}/files НЕ ІСНУЄ, хоч яким логічним
// не виглядало б інше припущення; є лише числове mod_loader у
// sortableGameVersions/latestFilesIndexes, яке ми тут не запитуємо).
var cfKnownLoaderNames = map[string]bool{
	"forge":      true,
	"fabric":     true,
	"quilt":      true,
	"neoforge":   true,
	"cauldron":   true,
	"liteloader": true,
	"client":     true, // не лоадер, але теж трапляється в gameVersions поруч
	"server":     true, // з лоадером — фільтруємо як "не версія MC", не як збіг
}

// containsLoader — чи є серед РЯДКІВ gameVersions назва потрібного
// лоадера. ВАЖЛИВО: приймає САМЕ gameVersions (не окреме поле
// "modLoaders" — такого поля в відповіді CurseForge API немає, воно
// завжди приходить порожнім, і перевірка проти нього раніше ЗАВЖДИ
// відхиляла файл як "несумісний", навіть коли лоадер насправді
// підтримувався — саме це виглядало як "лаунчер не вміє шукати/качати
// з CurseForge, хоча підтримується"). Порівняння — точний збіг без
// урахування регістру (HasPrefix прибрано: раніше "forge" помилково
// збігався з версією гри, що починається на ту саму підрядку, і з
// назвою "Forge" для будь-якого mod_loader — обидва боки тепер повна,
// а не часткова назва).
func containsLoader(gameVersions []string, loader string) bool {
	l := strings.ToLower(strings.TrimSpace(loader))
	if l == "" {
		return true
	}
	for _, x := range gameVersions {
		if strings.ToLower(strings.TrimSpace(x)) == l {
			return true
		}
	}
	return false
}

// cfExtractMCVersion — версія Minecraft із масиву gameVersions, що
// змішує в собі і версії гри, і назву лоадера (див. коментар
// cfKnownLoaderNames). Раніше код брав GameVersions[0] напряму — коли
// CurseForge клав лоадер ПЕРШИМ елементом масиву (реальна поведінка
// API, підтверджено на живих файлах), це давало mcVersion="Fabric"
// замість справжньої версії гри. Тут беремо перший елемент, який НЕ є
// відомою назвою лоадера/client/server.
func cfExtractMCVersion(gameVersions []string) string {
	for _, v := range gameVersions {
		if !cfKnownLoaderNames[strings.ToLower(strings.TrimSpace(v))] {
			return v
		}
	}
	return ""
}

// cfExtractLoaderName — назва лоадера (як CurseForge пише її: "Forge",
// "Fabric", "Quilt", "NeoForge") із масиву gameVersions, або "" якщо
// жодної відомої назви лоадера серед елементів немає (напр. дуже старі
// файли до впровадження тегування лоадерів на CurseForge).
func cfExtractLoaderName(gameVersions []string) string {
	for _, v := range gameVersions {
		lv := strings.ToLower(strings.TrimSpace(v))
		if lv == "forge" || lv == "fabric" || lv == "quilt" || lv == "neoforge" || lv == "cauldron" || lv == "liteloader" {
			return v
		}
	}
	return ""
}

// GetProjectWebsiteURL повертає websiteUrl проєкту (links.websiteUrl,
// напр. "https://www.curseforge.com/minecraft/mc-mods/jei") — потрібен для
// формування правильного посилання ручного скачування ЗАБЛОКОВАНОГО файлу
// у Prism-стилі: {websiteUrl}/download/{fileId}. Порожній рядок, якщо запит
// не вдався (викликач падає на числовий mc-mods fallback).
func (c *CurseForgeClient) GetProjectWebsiteURL(ctx context.Context, projectID int) string {
	u := fmt.Sprintf("%s/mods/%d", curseForgeAPIBase, projectID)
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return ""
	}
	resp, err := c.doRequestCtx(ctx, req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return ""
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}
	var parsed struct {
		Data struct {
			Links struct {
				WebsiteURL string `json:"websiteUrl"`
			} `json:"links"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return ""
	}
	return strings.TrimRight(parsed.Data.Links.WebsiteURL, "/")
}

// GetModFile повертає інформацію про конкретний файл мода за
// (projectID, fileID) — саме ця пара зберігається в manifest.json
// CurseForge-модпаків (розділ files[]).
func (c *CurseForgeClient) GetModFile(projectID, fileID int) (*CFFileInfo, error) {
	return c.GetModFileCtx(context.Background(), projectID, fileID)
}

// GetModFileCtx — версія GetModFile з контекстом: скасування (кнопка
// «Скасувати» в імпорті модпаку) обриває HTTP-запит, що висить, замість
// того, щоб чекати повний 20-секундний таймаут клієнта.
func (c *CurseForgeClient) GetModFileCtx(ctx context.Context, projectID, fileID int) (*CFFileInfo, error) {
	url := fmt.Sprintf("%s/mods/%d/files/%d", curseForgeAPIBase, projectID, fileID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.doRequestCtx(ctx, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 403 || resp.StatusCode == 401 {
		return nil, fmt.Errorf("CurseForge API відхилив запит (%d) — перевірте API-ключ", resp.StatusCode)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("CurseForge API: HTTP %d (project %d, file %d)", resp.StatusCode, projectID, fileID)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var parsed cfFileResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	downloadURL := parsed.Data.DownloadURL
	info := &CFFileInfo{
		FileName:    parsed.Data.FileName,
		DownloadURL: downloadURL,
		FileLength:  parsed.Data.FileLength,
		// GameVersions/ModLoaders: розрізняємо змішаний масив gameVersions
		// з реальної відповіді CurseForge (лоадер лежить УСЕРЕДИНІ нього,
		// напр. ["Fabric", "1.20.1"] — окремого поля "modLoaders" у
		// /mods/{id}/files/{id} немає, воно завжди приходило порожнім).
		// GameVersions лишаємо як є (для сумісності з рештою коду, що
		// звіряється проти нього через containsStr), але для явного
		// ModLoaders дістаємо розпізнану назву окремо.
		GameVersions: parsed.Data.GameVersions,
		ModLoaders:   cfLoaderNamesFrom(parsed.Data.GameVersions),
	}
	for _, h := range parsed.Data.Hashes {
		if h.Algo == 1 {
			info.SHA1 = h.Value
		}
	}
	if downloadURL == "" {
		// CurseForge інколи приховує downloadUrl у відповіді /files/{id}
		// для модів/модпаків з увімкненим "3rd party download opt-out",
		// але сам файл усе одно доступний через окремий ендпоінт
		// /mods/{id}/files/{id}/download-url — саме так качають MultiMC/
		// Prism/GDLauncher у цьому випадку. Пробуємо його, перш ніж
		// здаватись. Якщо й він не віддає URL — файл ЗАБЛОКОВАНИЙ
		// (Blocked=true): повертаємо інфо з SHA1, щоб викликач міг
		// спробувати Modrinth fallback за хешем (див. resolveCFFiles).
		if resolved, err := c.resolveDownloadURLCtx(ctx, projectID, fileID); err == nil && resolved != "" {
			info.DownloadURL = resolved
		} else {
			info.Blocked = true
		}
	}
	return info, nil
}
