package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"shaurma-launcher-backend/internal/model"
)

type ModrinthClient struct {
	baseURL string
	client  *http.Client
}

func NewModrinthClient() *ModrinthClient {
	return &ModrinthClient{
		baseURL: "https://api.modrinth.com/v2",
		client: &http.Client{
			Timeout: 30 * time.Second,
			// Власний Transport з пулом keep-alive з'єднань. Дефолтний
			// http.DefaultTransport тримає лише 2 idle-з'єднання на хост
			// (MaxIdleConnsPerHost=2), тож паралельні запити (пошук +
			// перевірка сумісності + версії) постійно відкривали НОВІ
			// TCP+TLS-з'єднання до api.modrinth.com (~150-400 мс кожне).
			// З пулом і HTTP/2 усі запити мультиплексуються в одне
			// тепле з'єднання — саме це робить GDLauncher швидким.
			Transport: &http.Transport{
				Proxy:                 http.ProxyFromEnvironment,
				ForceAttemptHTTP2:     true,
				MaxIdleConns:          64,
				MaxIdleConnsPerHost:   32,
				MaxConnsPerHost:       0,
				IdleConnTimeout:       120 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
				DialContext: (&net.Dialer{
					Timeout:   8 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
			},
		},
	}
}

func (c *ModrinthClient) fetch(path string, params map[string]string) ([]byte, error) {
	return c.fetchJSON("GET", path, params, nil)
}

// fetchV3 — GET до API v3 (організації та is_owner у командах є лише там;
// v2 на /organization/* відповідає 404).
func (c *ModrinthClient) fetchV3(path string, params map[string]string) ([]byte, error) {
	v3 := *c
	v3.baseURL = strings.TrimSuffix(c.baseURL, "/v2") + "/v3"
	return v3.fetchJSON("GET", path, params, nil)
}

// fetchJSON виконує HTTP-запит (GET/POST/...) до Modrinth API. Для POST
// body серіалізується в JSON і кладеться в тіло запиту; для GET
// використовуються params. 429 (rate limit) — повторюємо з паузою до 3
// спроб: менеджер модів шле багато паралельних запитів, і без ретраю
// частина модів «губилась» б.
func (c *ModrinthClient) fetchJSON(method, path string, params map[string]string, body any) ([]byte, error) {
	u, _ := url.Parse(c.baseURL + path)
	q := u.Query()
	for k, v := range params {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()

	// bodyBytes серіалізуємо ОДИН раз; на кожну спробу створюємо СВІЖИЙ
	// bytes.Reader — client.Do споживає тіло, тож повторне використання того
	// самого reader'а на 429-ретраї надіслало б ПОРОЖНЄ тіло (баг: batch
	// resolve /version_files тихо повертав би порожній результат).
	var bodyBytes []byte
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyBytes = payload
	}

	for attempt := 0; attempt < 3; attempt++ {
		var bodyReader io.Reader
		if bodyBytes != nil {
			bodyReader = bytes.NewReader(bodyBytes)
		}
		req, err := http.NewRequest(method, u.String(), bodyReader)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "shaurma-team/shaurma-launcher/2.0 (shaurmaofficial2210@gmail.com)")
		if bodyBytes != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := c.client.Do(req)
		if err != nil {
			// Мережевий збій (обрив, TCP reset, відмова, тимчасова помилка
			// DNS) — НЕ остаточний висновок: у лаунчера мережа «блимає»
			// (Wi-Fi, VPN, провайдер), і одна невдала спроба раніше валила
			// завантаження сторінки проєкту цілком — користувач бачив
			// «не вдалося завантажити проєкт» через один обірваний запит.
			// Повторюємо з короткою паузою.
			//
			// ВАЖЛИВО: таймаут НЕ повторюємо — якщо ми вже прочекали
			// 30 с, ще два кола (90 с) лише подовжать «сторінка вантажиться
			// вічно» замість того, щоб швидко показати помилку з кнопкою
			// повтору.
			var netErr net.Error
			if attempt == 2 || (errors.As(err, &netErr) && netErr.Timeout()) {
				return nil, err
			}
			time.Sleep(time.Duration(attempt+1) * 300 * time.Millisecond)
			continue
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			resp.Body.Close()
			time.Sleep(time.Duration(attempt+1) * 700 * time.Millisecond)
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("Modrinth API error: %d", resp.StatusCode)
		}
		return io.ReadAll(resp.Body)
	}
	return nil, fmt.Errorf("Modrinth API: rate limit")
}

// ── Міграція модів: пакетні (batch) запити ───────────────────────────────
// Аналіз міграції сканує ДО 500+ модів збірки-джерела. Поодинокий запит
// на кожен мод (GetVersionByHash) давав би стільки ж HTTP-запитів до
// Modrinth — дорого і повільно. Тому для міграції використовуємо пакетні
// endpoints Modrinth: POST /version_files (resolve багатьох sha1 одним
// запитом, до 1000 хешів) і GET /projects?ids= (інфо багатьох проєктів
// одним запитом, до 50 id).

// GetVersionsByHashes resolve декілька файлів за SHA1 ОДНИМ запитом
// (POST /version_files?algorithm=sha1). Повертає мапу sha1 → версія
// (повний експортований FullVersion — неекспортований mrVersion назовні
// не віддати). Незнайдені хеші просто відсутні в мапі. Це головна
// оптимізація для міграції 500 модів: замість 500 запитів
// GetVersionByHash — 1.
func (c *ModrinthClient) GetVersionsByHashes(hashes []string) (map[string]*FullVersion, error) {
	if len(hashes) == 0 {
		return map[string]*FullVersion{}, nil
	}
	out := map[string]*FullVersion{}
	// Modrinth дозволяє до 1000 хешів на один запит — але щоб не впертись
	// у ліміти тіла/таймауту, ділимо на порції по 200.
	const chunkSize = 200
	for start := 0; start < len(hashes); start += chunkSize {
		end := start + chunkSize
		if end > len(hashes) {
			end = len(hashes)
		}
		chunk := hashes[start:end]
		// ВАЖЛИВО: algorithm іде в JSON-ТІЛІ разом з hashes, а не в query-
		// параметрах — /version_files читає його саме звідти. Якщо передати
		// через params (query), Modrinth трактує запит як sha1 за
		// замовчуванням незалежно від того, що реально прислали в hashes:
		// нічого не збігається і versions завжди повертається порожнім
		// (мовчки — без HTTP-помилки), тож усі файли «губляться» в overrides.
		data, err := c.fetchJSON("POST", "/version_files", nil, map[string]any{"hashes": chunk, "algorithm": "sha1"})
		if err != nil {
			return nil, err
		}
		// ВАЖЛИВО: /version_files повертає ОБ'ЄКТ {hash: version}, а НЕ
		// масив — на відміну від /projects чи /search. Раніше тут парсили
		// як []mrVersion, json.Unmarshal падав на об'єкті з помилкою, і
		// виклик трактувався як "мережа недоступна": усі моди мовчки йшли
		// в overrides/bundled без резолву. Ключ мапи — це саме той хеш,
		// що ми питали, тож можна не покладатись на fv.Files[].Sha1.
		var versions map[string]mrVersion
		if err := json.Unmarshal(data, &versions); err != nil {
			return nil, err
		}
		for h, v := range versions {
			out[h] = versionToFull(&v)
		}
	}
	return out, nil
}

// GetLatestVersionsByHashes — batch-перевірка оновлень ОДНИМ запитом:
// POST /version_files/update (той самий endpoint, що використовує сам
// Modrinth App). Для кожного sha1 повертає НАЙНОВІШУ версію проєкту, що
// підтримує вказані loaders + game_versions (а не точний матч хеша, як
// GetVersionsByHashes). Це перетворює перевірку оновлень N модів з N×3
// поодиноких запитів (hash → project → latest) в ОДИН batch-запит.
//
// Returns: sha1 → найновіша сумісна версія проєкту; хеші без оновлення
// або без сумісної версії просто відсутні в мапі.
func (c *ModrinthClient) GetLatestVersionsByHashes(hashes, loaders, gameVersions []string) (map[string]*FullVersion, error) {
	return c.GetLatestVersionsByHashesFiltered(hashes, loaders, gameVersions, nil)
}

// GetLatestVersionsByHashesFiltered — те саме, але з фільтром каналу
// релізів (version_types). Це та сама логіка, що в Modrinth App:
//   - AllowAlphaBetaModUpdates=false → лише "release";
//   - true → "release", потім fallback "beta", потім "alpha" окремими
//     запитами лише для хешів, які ще не знайшлись.
//
// Без фільтра бекенд міг запропонувати alpha-версію гравцю, який її не
// вмикав, — саме цю розбіжність з Modrinth App і прибираємо.
func (c *ModrinthClient) GetLatestVersionsByHashesFiltered(hashes, loaders, gameVersions, versionTypes []string) (map[string]*FullVersion, error) {
	if len(hashes) == 0 {
		return map[string]*FullVersion{}, nil
	}
	out := map[string]*FullVersion{}
	// Modrinth приймає до 1000 хешів на запит. 500 — безпечний розмір,
	// який ще вкладається в таймаут на повільному з'єднанні.
	const chunkSize = 500
	for start := 0; start < len(hashes); start += chunkSize {
		end := start + chunkSize
		if end > len(hashes) {
			end = len(hashes)
		}
		chunk := hashes[start:end]
		body := map[string]any{
			"hashes":        chunk,
			"algorithm":     "sha1",
			"loaders":       loaders,
			"game_versions": gameVersions,
		}
		if len(versionTypes) > 0 {
			body["version_types"] = versionTypes
		}
		data, err := c.fetchJSON("POST", "/version_files/update", nil, body)
		if err != nil && len(versionTypes) > 0 && strings.Contains(err.Error(), "Modrinth API error: 4") {
			// Лише 4xx (старіший бекенд/проксі не знає version_types) —
			// повторюємо без фільтра. Мережевий таймаут НЕ дублюємо: інакше
			// одна невдала перевірка чекала б удвічі довше.
			delete(body, "version_types")
			data, err = c.fetchJSON("POST", "/version_files/update", nil, body)
		}
		if err != nil {
			return nil, err
		}
		// Відповідь — об'єкт {sha1: version}, а не масив (як і у
		// /version_files). Ключ — саме той хеш, що питали.
		var versions map[string]mrVersion
		if err := json.Unmarshal(data, &versions); err != nil {
			return nil, err
		}
		for h, v := range versions {
			out[h] = versionToFull(&v)
		}
	}
	return out, nil
}

// GetVersionsByHashes512 resolve декілька файлів за SHA512 ОДНИМ запитом
// (POST /version_files?algorithm=sha512) — той самий batch-підхід, що й
// GetVersionsByHashes, але для mrpack-експорту: специфікація modrinth.index
// вимагає у files[] саме пару (sha1, sha512) та посилання на завантаження.
// Повертає мапу sha512 → повна версія; незнайдені хеші просто відсутні.
func (c *ModrinthClient) GetVersionsByHashes512(hashes []string) (map[string]*FullVersion, error) {
	if len(hashes) == 0 {
		return map[string]*FullVersion{}, nil
	}
	out := map[string]*FullVersion{}
	const chunkSize = 200
	for start := 0; start < len(hashes); start += chunkSize {
		end := start + chunkSize
		if end > len(hashes) {
			end = len(hashes)
		}
		chunk := hashes[start:end]
		// Те саме зауваження щодо algorithm у тілі, а не в query — див.
		// коментар у GetVersionsByHashes вище. Без цього резолв mrpack
		// (sha512) мовчки повертав порожній результат і всі моди йшли
		// в overrides без files[] в index — саме тому mrpack виглядав
		// «пустим» (files: []).
		data, err := c.fetchJSON("POST", "/version_files", nil, map[string]any{"hashes": chunk, "algorithm": "sha512"})
		if err != nil {
			return nil, err
		}
		// Той самий момент, що й у GetVersionsByHashes: відповідь — ОБ'ЄКТ
		// {hash: version}, не масив. Парсинг як []mrVersion раніше тихо
		// провалював Unmarshal, resolveMods() трактував це як недоступність
		// мережі, і ВСІ моди йшли в overrides/mods без запису в files[]
		// modrinth.index.json (звідси порожній "files": [] у mrpack-індексі).
		var versions map[string]mrVersion
		if err := json.Unmarshal(data, &versions); err != nil {
			return nil, err
		}
		for h, v := range versions {
			out[h] = versionToFull(&v)
		}
	}
	return out, nil
}

// GetProjectsBatch повертає інформацію (назва/слаг/іконка) для кількох
// проєктів ОДНИМ запитом (GET /projects?ids=[...], до 50 id). Повертає
// мапу projectID → інфо. Це друга пакетна оптимізація міграції: іконки
// та слаги для сотні знайдених модів качаються парою запитів замість
// сотні окремих GetProject.
func (c *ModrinthClient) GetProjectsBatch(ids []string) (map[string]*ModrinthProjectInfo, error) {
	out := map[string]*ModrinthProjectInfo{}
	if len(ids) == 0 {
		return out, nil
	}
	const chunkSize = 50
	for start := 0; start < len(ids); start += chunkSize {
		end := start + chunkSize
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[start:end]
		idsJSON, _ := json.Marshal(chunk)
		data, err := c.fetch("/projects", map[string]string{"ids": string(idsJSON)})
		if err != nil {
			return nil, err
		}
		var raw []struct {
			ID         string `json:"id"`
			Slug       string `json:"slug"`
			Title      string `json:"title"`
			IconURL    string `json:"icon_url"`
			ClientSide string `json:"client_side"`
			ServerSide string `json:"server_side"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, err
		}
		for _, r := range raw {
			if r.ID == "" {
				continue
			}
			p := &ModrinthProjectInfo{ID: r.ID, Slug: r.Slug, Title: r.Title, IconURL: r.IconURL,
				ClientSide: r.ClientSide, ServerSide: r.ServerSide}
			out[r.ID] = p
			projectCacheMu.Lock()
			projectCache[r.ID] = p
			projectCacheMu.Unlock()
		}
	}
	return out, nil
}

type mrHit struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Slug        string   `json:"slug"`
	ProjectType string   `json:"project_type"`
	Versions    []string `json:"versions"`
	Downloads   int64    `json:"downloads"`
	IconURL     string   `json:"icon_url"`
	ClientSide  string   `json:"client_side"`
}

func (c *ModrinthClient) Search(query string, limit int) ([]model.BrowserEntry, error) {
	params := map[string]string{
		"query":  query,
		"limit":  fmt.Sprintf("%d", limit),
		"facets": `[["project_type:modpack","project_type:mod","project_type:shader","project_type:resourcepack"]]`,
	}
	data, err := c.fetch("/search", params)
	if err != nil {
		return nil, err
	}
	var result struct {
		Hits []mrHit `json:"hits"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	var entries []model.BrowserEntry
	for _, h := range result.Hits {
		entries = append(entries, model.BrowserEntry{
			ID:          h.Slug,
			Name:        h.Title,
			Type:        h.ProjectType,
			Description: h.Description,
			Downloads:   h.Downloads,
			IconURL:     h.IconURL,
			Source:      "modrinth",
		})
	}
	return entries, nil
}

// ── Проєкти/версії для менеджера модів ─────────────────────────────────

// ModrinthProjectInfo — коротка інформація про проєкт (для іконок/посилань).
type ModrinthProjectInfo struct {
	ID      string `json:"id"`
	Slug    string `json:"slug"`
	Title   string `json:"title"`
	IconURL string `json:"iconUrl"`
	// ClientSide/ServerSide — "required" | "optional" | "unsupported" |
	// "unknown".
	ClientSide string `json:"clientSide,omitempty"`
	ServerSide string `json:"serverSide,omitempty"`
}

// projectCache — in-memory кеш проєктів (іконки/слаги), щоб не бити API
// повторно для тих самих проєктів у межах сесії.
var (
	projectCacheMu sync.Mutex
	projectCache   = map[string]*ModrinthProjectInfo{}
)

// GetProject повертає інформацію про проєкт (з кешем).
func (c *ModrinthClient) GetProject(projectID string) (*ModrinthProjectInfo, error) {
	if projectID == "" {
		return nil, fmt.Errorf("порожній projectID")
	}
	projectCacheMu.Lock()
	if p, ok := projectCache[projectID]; ok {
		projectCacheMu.Unlock()
		return p, nil
	}
	projectCacheMu.Unlock()

	data, err := c.fetch("/project/"+url.PathEscape(projectID), nil)
	if err != nil {
		return nil, err
	}
	var raw struct {
		ID         string `json:"id"`
		Slug       string `json:"slug"`
		Title      string `json:"title"`
		IconURL    string `json:"icon_url"`
		ClientSide string `json:"client_side"`
		ServerSide string `json:"server_side"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	p := &ModrinthProjectInfo{ID: raw.ID, Slug: raw.Slug, Title: raw.Title, IconURL: raw.IconURL,
		ClientSide: raw.ClientSide, ServerSide: raw.ServerSide}
	projectCacheMu.Lock()
	projectCache[projectID] = p
	projectCacheMu.Unlock()
	return p, nil
}

// GetProjectVersions повертає список версій проєкту, опційно відфільтрований
// за версією гри та лоадером (порожні значення = без фільтра).
// Результат відсортований Modrinth-ем за датою (новіші перші).
func (c *ModrinthClient) GetProjectVersions(projectID, gameVersion, loader string) ([]mrVersion, error) {
	path := "/project/" + url.PathEscape(projectID) + "/version"
	params := map[string]string{}
	if gameVersion != "" {
		gv, _ := json.Marshal([]string{gameVersion})
		params["game_versions"] = string(gv)
	}
	if loader != "" {
		lj, _ := json.Marshal([]string{strings.ToLower(loader)})
		params["loaders"] = string(lj)
	}
	data, err := c.fetch(path, params)
	if err != nil {
		return nil, err
	}
	var versions []mrVersion
	if err := json.Unmarshal(data, &versions); err != nil {
		return nil, err
	}
	return versions, nil
}

// versionToOption конвертує mrVersion у model.ModVersionOption (менеджер модів).
func versionToOption(v mrVersion) *model.ModVersionOption {
	opt := &model.ModVersionOption{
		ID: v.ID, Name: v.Name, VersionNumber: v.VersionNumber,
		DatePublished: v.DatePublished, VersionType: v.VersionType, Changelog: v.Changelog,
	}
	if len(v.GameVersions) > 0 {
		opt.GameVersion = v.GameVersions[0]
	}
	if len(v.Loaders) > 0 {
		opt.Loader = v.Loaders[0]
	}
	if len(v.Files) > 0 {
		f := v.Files[v.PrimaryFileIndex()]
		opt.URL = f.URL
		opt.Filename = f.Filename
		opt.Sha1 = f.Hashes.Sha1
	}
	return opt
}

// LatestVersionFor повертає найсвіжішу версію проєкту під конкретну версію
// гри та лоадер (для перевірки оновлень).
//
// allowAlphaBeta=false (типовий випадок) — пріоритет release-версіям: якщо
// серед versions є хоч одна release, беремо найновішу release, ігноруючи
// alpha/beta, щоб автоматичне оновлення не підмінило робочий мод
// нестабільним білдом без відома користувача.
//
// Якщо ЖОДНОЇ release-версії немає (проєкт публікує лише alpha/beta —
// типово для дев-збірок, тестових портів на нову версію гри, деяких
// invidivual-мододелів), прапорець не повинен ховати оновлення взагалі:
// мод, який користувач уже встановив, і так alpha/beta — вимога "лише
// релізи" тут безглузда, бо релізу просто не існує. У цьому випадку
// падаємо на найновішу З НАЯВНИХ версій незалежно від allowAlphaBeta,
// інакше користувач з таким модом ніколи не побачить оновлення.
//
// allowAlphaBeta=true — найновіша серед усіх (Modrinth і так сортує за
// датою публікації, тож просто versions[0]).
//
// Обмеження: hasUpdate у виклику (mods_api.go) визначається порівнянням
// НОМЕРА версії (isSameModVersion), а не дат публікації — це властивість
// усього наявного пайплайна оновлень, не специфічна для alpha/beta.
// У вкрай рідкісному сценарії "проєкт має і release, і новішу за
// встановлену alpha, але СТАРІШУ за неї release" fallback тут поверне
// саме release (за дизайном — пріоритет стабільності), що теоретично
// може показати "оновлення" на об'єктивно старішу версію. Прийнятний
// компроміс: якщо користувач узагалі поставив собі alpha-версію мода,
// це вже нішевий випадок, а не типовий сценарій.
func (c *ModrinthClient) LatestVersionFor(projectID, gameVersion, loader string, allowAlphaBeta bool) (*model.ModVersionOption, error) {
	versions, err := c.GetProjectVersions(projectID, gameVersion, loader)
	if err != nil {
		return nil, err
	}
	if len(versions) == 0 {
		return nil, nil
	}
	if allowAlphaBeta {
		return versionToOption(versions[0]), nil
	}
	for _, v := range versions {
		if v.VersionType == "release" {
			return versionToOption(v), nil
		}
	}
	// Жодної release-версії у проєкта немає — прапорець "лише релізи"
	// нема до чого застосувати, повертаємо найновішу наявну (alpha/beta),
	// а не мовчки ховаємо оновлення для модів, які release не публікують.
	return versionToOption(versions[0]), nil
}

// AllVersionsFor повертає список версій проєкту (модалка «Змінити
// версію»), відфільтрований під версію гри та лоадер поточної збірки —
// показувати версії для інших луадерів/версій гри безглуздо, вони все
// одно не запустяться. Якщо gameVersion/loader порожні (виклик без
// контексту збірки), фільтр не застосовується.
func (c *ModrinthClient) AllVersionsFor(projectID, gameVersion, loader string) ([]model.ModVersionOption, error) {
	versions, err := c.GetProjectVersions(projectID, gameVersion, loader)
	if err != nil {
		return nil, err
	}
	out := make([]model.ModVersionOption, 0, len(versions))
	for _, v := range versions {
		if opt := versionToOption(v); opt != nil {
			out = append(out, *opt)
		}
	}
	return out, nil
}

// ── Браузер контенту ───────────────────────────────────────────────────
// Пошук з фасетами (project_type / versions / categories) і сортуванням —
// той самий контракт, що /search, який використовує веб-апка Modrinth
// (discover-сторінки). mrBrowserHit — розширений хіт з категоріями,
// лоадерами та project_id (для сторінки проєкту/встановлення потрібен
// саме project_id, а не лише slug).

type mrBrowserHit struct {
	ProjectID   string   `json:"project_id"`
	Title       string   `json:"title"`
	Slug        string   `json:"slug"`
	ProjectType string   `json:"project_type"`
	Description string   `json:"description"`
	Versions    []string `json:"versions"`
	Downloads   int64    `json:"downloads"`
	Followers   int64    `json:"follows"`
	IconURL     string   `json:"icon_url"`
	Categories  []string `json:"categories"`
	Loaders     []string `json:"loaders"`
	Author      string   `json:"author"`
	// Organization — назва організації-власника (порожньо, якщо проєкт особистий).
	Organization string `json:"organization"`
	DateCreated  string `json:"date_created"`
	DateModified string `json:"date_modified"`
	// featured_gallery — обране зображення галереї (може бути null),
	// gallery — усі зображення; беремо перше доступне як банер картки.
	FeaturedGallery string   `json:"featured_gallery"`
	Gallery         []string `json:"gallery"`
}

// BrowserSearchParams — параметри пошуку контенту (браузер). Facets
// збираються за тим самим принципом, що у веб-апці Modrinth: кожен
// вимір — окремий внутрішній масив (AND між вимірами, OR всередині).
type BrowserSearchParams struct {
	Query       string
	ProjectType string // mod | modpack | resourcepack | shader | datapack
	GameVersion string
	Loader      string
	// ExtraLoaders — додаткові лоадери, що потрапляють в той самий
	// OR-фасет, що й Loader (напр. коли в збірці стоїть Sinytra Connector
	// і Forge/NeoForge-збірка додатково приймає fabric/quilt-контент —
	// див. BrowserSearchModrinth). Порожньо для звичайного пошуку.
	ExtraLoaders []string
	Categories   []string // теги-категорії (OR усередині фасета)
	Sort         string   // relevance | downloads | follows | newest | updated | featured
	// Client/Server — фільтр середовища (Modrinth "Environment"): який бік
	// гри має підтримувати контент. OpenSource — фільтр "Open source"
	// (лицензія під відкритою ліцензією). Вони Modrinth-specific і будують
	// фасети так само, як createEnvironmentFacets у веб-апці Modrinth.
	Client     bool
	Server     bool
	OpenSource bool
	Limit      int
	Offset     int
}

// mrSearchResponse — відповідь /search (ті самі поля, що й у веб-апки).
type mrSearchResponse struct {
	Hits      []mrBrowserHit `json:"hits"`
	Offset    int            `json:"offset"`
	Limit     int            `json:"limit"`
	TotalHits int            `json:"total_hits"`
}

// SearchBrowser шукає контент за типом/версією/лоадером/сортуванням.
// Facets збираються аналогічно useSearch у веб-апці Modrinth:
//   - project_type — окремий фасет (AND з рештою);
//   - versions:MC — окремий фасет (OR по версіях всередині, у нас одна);
//   - categories:loader — окремий фасет ЛИШЕ для типів, де лоадер має
//     значення (mod/modpack). Для resourcepack/datapack лоадер виключається
//     повністю (Modrinth use-browse-search: excludeLoaders=true), а для
//     shader фасет лоадера має бути iris/optifine, а НЕ fabric/forge —
//     фільтр по лоадеру збірки тут безглуздий. РАНІШЕ фасет
//     categories:<loader> додавався для ВСІХ типів, через що пошук
//     ресурспаків/датапаків/шейдерів у збірці з фільтром лоадера (напр.
//     fabric) повертав ПОРОЖНІЙ список — «браузер не вміє шукати».
//
// Сортування через index=... (relevance | downloads | follows | newest |
// updated) — той самий список, що dropdown сортування на сторінці пошуку.
func (c *ModrinthClient) SearchBrowser(p BrowserSearchParams) ([]model.BrowserEntry, int, error) {
	if p.Limit <= 0 {
		p.Limit = 20
	}
	params := map[string]string{
		"query":  p.Query,
		"limit":  fmt.Sprintf("%d", p.Limit),
		"offset": fmt.Sprintf("%d", p.Offset),
	}
	// ВАЖЛИВО: публічний API Modrinth НЕ підтримує index=featured
	// (відповідь: invalid index 'featured') — це значення використовує
	// лише їхня веб-апка через власний проксі. Найближчий аналог для
	// «Рекомендовані» у публічному API — relevance (популярне першим).
	// Без цього маппінгу дефолтний сорт браузера падав би в порожній
	// список — «нічого не шукається».
	index := p.Sort
	if index == "" || index == "featured" {
		index = "relevance"
	}
	if index != "relevance" {
		params["index"] = index
	}
	var facets [][]string
	if p.ProjectType != "" {
		facets = append(facets, []string{"project_type:" + p.ProjectType})
	}
	if p.GameVersion != "" {
		facets = append(facets, []string{"versions:" + p.GameVersion})
	}
	// Лоадер-фасет — лише для модів/модпаків (Modrinth: loader-фільтр
	// підтримують тільки mod_loader/modpack_loader). Для ресурспаків,
	// датапаків і шейдерів фасет лоадера НЕ додаємо: ресурспаки/датапаки
	// лоадер-агностичні, а шейдери використовують iris/optifine (не
	// fabric/forge), тож фільтр по лоадеру збірки лише ховає весь контент.
	if p.Loader != "" && loaderFacetApplies(p.ProjectType) {
		loaderFacet := []string{"categories:" + strings.ToLower(p.Loader)}
		for _, extra := range p.ExtraLoaders {
			extra = strings.ToLower(strings.TrimSpace(extra))
			if extra != "" && extra != strings.ToLower(p.Loader) {
				loaderFacet = append(loaderFacet, "categories:"+extra)
			}
		}
		facets = append(facets, loaderFacet)
	}
	// Категорії-теги: окремий фасет, OR між вибраними (той самий
	// контракт, що категорії у SearchSidebarFilter веб-апки Modrinth).
	if len(p.Categories) > 0 {
		cats := make([]string, 0, len(p.Categories))
		for _, c := range p.Categories {
			cats = append(cats, "categories:"+c)
		}
		facets = append(facets, cats)
	}
	// Фільтр середовища (Client/Server) — той самий createEnvironmentFacets,
	// що у search.ts веб-апки Modrinth. Комбінація виборів визначає, які
	// фасети client_side/server_side додати (див. helper нижче).
	if p.Client || p.Server {
		facets = append(facets, createEnvironmentFacets(p.Client, p.Server)...)
	}
	// Фільтр "Open source" (Modrinth License) — жорстка AND-умова
	// open_source:true (метод "and" у веб-апці, окремий фасет).
	if p.OpenSource {
		facets = append(facets, []string{"open_source:true"})
	}
	if len(facets) > 0 {
		fj, _ := json.Marshal(facets)
		params["facets"] = string(fj)
	}
	// Кеш відповіді пошуку: повторний запит з тими самими параметрами
	// (повернення на вкладку, «назад» з проєкту, перемикання сторінки
	// туди-сюди) віддається з пам'яті миттєво, без мережі.
	cacheKey := params["query"] + "|" + params["limit"] + "|" + params["offset"] + "|" + params["index"] + "|" + params["facets"]
	data, err := searchCacheGet(cacheKey, func() ([]byte, error) {
		return c.fetch("/search", params)
	})
	if err != nil {
		return nil, 0, err
	}
	var res mrSearchResponse
	if err := json.Unmarshal(data, &res); err != nil {
		return nil, 0, err
	}
	out := make([]model.BrowserEntry, 0, len(res.Hits))
	for _, h := range res.Hits {
		entry := model.BrowserEntry{
			ID:           h.Slug,
			ProjectID:    h.ProjectID,
			Name:         h.Title,
			Type:         h.ProjectType,
			Description:  h.Description,
			Downloads:    h.Downloads,
			Followers:    h.Followers,
			IconURL:      h.IconURL,
			Source:       "modrinth",
			Author:       firstNonEmptyStr(h.Organization, h.Author),
			Categories:   h.Categories,
			Loaders:      h.Loaders,
			DateCreated:  h.DateCreated,
			DateModified: h.DateModified,
		}
		if len(h.Versions) > 0 {
			entry.MCVersion = h.Versions[0]
		}
		if len(h.Loaders) > 0 {
			entry.Loader = h.Loaders[0]
		}
		if h.FeaturedGallery != "" {
			entry.BannerURL = h.FeaturedGallery
		} else if len(h.Gallery) > 0 {
			entry.BannerURL = h.Gallery[0]
		}
		out = append(out, entry)
	}
	return out, res.TotalHits, nil
}

// loaderFacetApplies — чи має лоадер-фасет зміст для цього типу контенту.
// Порт excludeLoaders з use-browse-search веб-апки Modrinth: для
// resourcepack/datapack лоадери виключаються завжди; для shader вони є
// (iris/optifine), але це НЕ лоадери збірки, тож фасет тут не застосовуємо.
// "plugin" — лоадер тут paper/purpur/velocity (categories на Modrinth для
// плагінів), і фасет так само критичний, як для модів: без нього пошук
// плагінів повертав би вперемішку Paper-, Velocity- і Bukkit-плагіни під
// Fabric-фільтром сервера, який жодного стосунку до плагінів не має.
func loaderFacetApplies(projectType string) bool {
	switch strings.ToLower(projectType) {
	case "mod", "modpack", "plugin", "":
		return true
	default:
		return false
	}
}

// createEnvironmentFacets — 1-в-1 порт createEnvironmentFacets з
// packages/ui/src/utils/search.ts веб-апки Modrinth. Залежно від комбінації
// обраних середовищ (client/server) повертає фасети client_side/server_side,
// що відфільтровують лише сумісний контент (тобто точніше, ніж просте
// client_side:required для «обох»). Кожен внутрішній рядок — окремий вимір
// (AND), OR усередині виміру.
func createEnvironmentFacets(client, server bool) [][]string {
	var facets [][]string
	switch {
	case client && server:
		facets = append(facets, []string{"client_side:required"}, []string{"server_side:required"})
	case client:
		facets = append(facets,
			[]string{"client_side:optional", "client_side:required"},
			[]string{"server_side:optional", "server_side:unsupported"},
		)
	case server:
		facets = append(facets,
			[]string{"client_side:optional", "client_side:unsupported"},
			[]string{"server_side:optional", "server_side:required"},
		)
	}
	return facets
}

// ── Деталі проєкту для сторінки модпаку/контенту ───────────────────────

// ModrinthGalleryImage — один елемент gallery проєкту (скріншот).
// RawURL — оригінальне зображення (raw_url) для повноекранного лайтбокса.
type ModrinthGalleryImage struct {
	URL         string `json:"url"`
	RawURL      string `json:"raw_url"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Featured    bool   `json:"featured"`
}

// GetProjectAuthor повертає ім'я власника проєкту (team → власник), для
// секції «Автор» на сторінці проєкту (як ProjectSidebarCreators веб-апки).
// Помилки мережі ігноруються — секція просто порожня.
func (c *ModrinthClient) GetProjectAuthor(projectID string) (string, error) {
	a, err := c.GetProjectAuthorFull(projectID)
	if err != nil {
		return "", err
	}
	return a.Username, nil
}

// ModrinthAuthor — власник проєкту (з /project/{id}/members): ID + ім'я +
// аватар + роль. Потрібно для сторінки автора (клік по автору в сайдбарі
// проєкту відкриває його профіль з іконкою, як UserPage у Modrinth app).
type ModrinthAuthor struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	AvatarURL string `json:"avatar_url"`
	Role      string `json:"role"`
}

// GetProjectAuthorFull повертає повну інформацію про власника проєкту
// (перший учасник з роллю Owner, інакше перший учасник) — ID, username,
// аватар і роль. Мережеві помилки ігноруються тільки на рівні викликача.
func (c *ModrinthClient) GetProjectAuthorFull(projectID string) (*ModrinthAuthor, error) {
	if projectID == "" {
		return &ModrinthAuthor{}, nil
	}
	data, err := c.fetch("/project/"+url.PathEscape(projectID)+"/members", nil)
	if err != nil {
		return &ModrinthAuthor{}, err
	}
	var members []struct {
		User struct {
			ID        string `json:"id"`
			Username  string `json:"username"`
			AvatarURL string `json:"avatar_url"`
		} `json:"user"`
		Role string `json:"role"`
	}
	if err := json.Unmarshal(data, &members); err != nil {
		return &ModrinthAuthor{}, err
	}
	// Перший учасник з роллю Owner (або просто перший) — як у веб-апки.
	for _, m := range members {
		if m.Role == "Owner" && m.User.Username != "" {
			return &ModrinthAuthor{ID: m.User.ID, Username: m.User.Username, AvatarURL: m.User.AvatarURL, Role: m.Role}, nil
		}
	}
	if len(members) > 0 {
		m := members[0]
		return &ModrinthAuthor{ID: m.User.ID, Username: m.User.Username, AvatarURL: m.User.AvatarURL, Role: m.Role}, nil
	}
	return &ModrinthAuthor{}, nil
}

// ── Сторінка автора (UserPage / OrganizationPage, як Modrinth app) ──────
// Модель взята 1-в-1 з theseus: get_user_profile (GET /user/{id}),
// get_user_projects (GET /user/{id}/projects), get_user_organizations
// (GET /user/{id}/organizations), організація — GET /organization/{id} +
// /organization/{id}/projects.

// ModrinthUserProfile — профіль користувача (GET /user/{id}).
type ModrinthUserProfile struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
	Bio       string `json:"bio"`
	Role      string `json:"role"`
	Created   string `json:"created"`
}

// GetUserProfile повертає профіль користувача Modrinth за ID або username
// (GET /user/{id}) — для сторінки автора (аватар, біо, дата реєстрації).
func (c *ModrinthClient) GetUserProfile(userID string) (*ModrinthUserProfile, error) {
	if userID == "" {
		return nil, fmt.Errorf("порожній userID")
	}
	data, err := c.fetch("/user/"+url.PathEscape(userID), nil)
	if err != nil {
		return nil, err
	}
	var p ModrinthUserProfile
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// ModrinthUserProject — короткий опис проєкту користувача (з /user/{id}/
// projects) — ті самі поля, що у mrBrowserHit, але без facets-полів.
type ModrinthUserProject struct {
	ID          string   `json:"id"`
	Slug        string   `json:"slug"`
	ProjectType string   `json:"project_type"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Downloads   int64    `json:"downloads"`
	Follows     int64    `json:"follows"`
	IconURL     string   `json:"icon_url"`
	Author      string   `json:"author"`
	Versions    []string `json:"versions"`
}

// GetUserProjects повертає всі проєкти користувача (GET /user/{id}/projects)
// — той самий контракт, що get_user_projects у theseus/Modrinth app.
func (c *ModrinthClient) GetUserProjects(userID string) ([]ModrinthUserProject, error) {
	if userID == "" {
		return nil, fmt.Errorf("порожній userID")
	}
	data, err := c.fetch("/user/"+url.PathEscape(userID)+"/projects", nil)
	if err != nil {
		return nil, err
	}
	var out []ModrinthUserProject
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ModrinthOrganization — профіль організації (GET /organization/{id}, v3).
type ModrinthOrganization struct {
	ID          string               `json:"id"`
	Slug        string               `json:"slug"`
	Name        string               `json:"name"`
	Description string               `json:"description"`
	IconURL     string               `json:"icon_url"`
	Created     string               `json:"created"`
	Members     []ModrinthTeamMember `json:"members"`
}

// ModrinthTeamMember — учасник команди проєкту чи організації (v3).
type ModrinthTeamMember struct {
	User struct {
		ID        string `json:"id"`
		Username  string `json:"username"`
		AvatarURL string `json:"avatar_url"`
	} `json:"user"`
	Role     string `json:"role"`
	IsOwner  bool   `json:"is_owner"`
	Accepted *bool  `json:"accepted"`
	Ordering int64  `json:"ordering"`
}

// GetProjectMembers повертає всіх учасників команди проєкту
// (GET /v3/project/{id}/members) — як get_team у Modrinth app.
func (c *ModrinthClient) GetProjectMembers(projectID string) ([]ModrinthTeamMember, error) {
	if projectID == "" {
		return nil, fmt.Errorf("порожній projectID")
	}
	data, err := c.fetchV3("/project/"+url.PathEscape(projectID)+"/members", nil)
	if err != nil {
		return nil, err
	}
	var out []ModrinthTeamMember
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetOrganizationProfile повертає профіль організації (GET /organization/{id}).
func (c *ModrinthClient) GetOrganizationProfile(orgID string) (*ModrinthOrganization, error) {
	if orgID == "" {
		return nil, fmt.Errorf("порожній orgID")
	}
	data, err := c.fetchV3("/organization/"+url.PathEscape(orgID), nil)
	if err != nil {
		return nil, err
	}
	var o ModrinthOrganization
	if err := json.Unmarshal(data, &o); err != nil {
		return nil, err
	}
	return &o, nil
}

// GetOrganizationProjects повертає проєкти організації (GET /organization/
// {id}/projects, v3).
func (c *ModrinthClient) GetOrganizationProjects(orgID string) ([]ModrinthUserProject, error) {
	if orgID == "" {
		return nil, fmt.Errorf("порожній orgID")
	}
	// v3 віддає проєкти в іншому форматі (name/summary/project_types), тож
	// беремо лише ID і дочитуємо самі проєкти через v2 /projects?ids=.
	data, err := c.fetchV3("/organization/"+url.PathEscape(orgID)+"/projects", nil)
	if err != nil {
		return nil, err
	}
	var ids []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &ids); err != nil {
		return nil, err
	}
	out := []ModrinthUserProject{}
	for start := 0; start < len(ids); start += 100 {
		end := min(start+100, len(ids))
		list := make([]string, 0, end-start)
		for _, it := range ids[start:end] {
			list = append(list, it.ID)
		}
		raw, _ := json.Marshal(list)
		page, err := c.fetch("/projects", map[string]string{"ids": string(raw)})
		if err != nil {
			return nil, err
		}
		var part []struct {
			ModrinthUserProject
			Followers int64  `json:"followers"`
			Team      string `json:"team"`
		}
		if err := json.Unmarshal(page, &part); err != nil {
			return nil, err
		}
		for _, p := range part {
			if p.Follows == 0 {
				p.Follows = p.Followers
			}
			out = append(out, p.ModrinthUserProject)
		}
	}
	return out, nil
}

// ModrinthProjectDetail — повний опис проєкту для сторінки модпаку
// (як ProjectPage веб-апки Modrinth: заголовок, опис (markdown body),
// галерея, посилання, категорії).
type ModrinthProjectDetail struct {
	ID                   string                 `json:"id"`
	Slug                 string                 `json:"slug"`
	Title                string                 `json:"title"`
	Description          string                 `json:"description"`
	Body                 string                 `json:"body"`
	IconURL              string                 `json:"icon_url"`
	ProjectType          string                 `json:"project_type"`
	Downloads            int64                  `json:"downloads"`
	Followers            int64                  `json:"followers"`
	Published            string                 `json:"published"`
	Updated              string                 `json:"updated"`
	Categories           []string               `json:"categories"`
	AdditionalCategories []string               `json:"additional_categories"`
	Gallery              []ModrinthGalleryImage `json:"gallery"`
	// Organization — ID організації-власника (порожньо для особистих проєктів).
	Organization string          `json:"organization"`
	ClientSide   string          `json:"client_side"`
	ServerSide   string          `json:"server_side"`
	IssuesURL    string          `json:"issues_url"`
	SourceURL    string          `json:"source_url"`
	DiscordURL   string          `json:"discord_url"`
	WikiURL      string          `json:"wiki_url"`
	License      ModrinthLicense `json:"license"`
	// GameVersions/Loaders — версії гри та лоадери, під які проєкт має
	// версії (те саме, що ProjectPage веб-апки показує у сайдбарі
	// «Сумісність»). Раніше їх тут НЕ було — сторінка проєкту Modrinth
	// показувала порожній рядок сумісності, хоча API віддає ці поля.
	GameVersions []string `json:"game_versions"`
	Loaders      []string `json:"loaders"`
}

// ModrinthLicense — ліцензія проєкту (для секції «Деталі» на сторінці).
type ModrinthLicense struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

// ── Категорії-теги для фільтра браузера ────────────────────────────────

// ModrinthCategory — один тег-категорія з /tag/category/{type} (той самий
// список, що SearchSidebarFilter у веб-апці: назва + іконка).
type ModrinthCategory struct {
	Name        string `json:"name"`
	Icon        string `json:"icon"`
	ProjectType string `json:"project_type"`
	Header      string `json:"header"`
}

// GetCategories повертає теги-категорії для типу контенту (mod/modpack/
// resourcepack/shader/datapack) — з іконками для сайдбара браузера.
func (c *ModrinthClient) GetCategories(projectType string) ([]ModrinthCategory, error) {
	data, err := c.fetch("/tag/category/"+url.PathEscape(projectType), nil)
	if err != nil {
		return nil, err
	}
	var out []ModrinthCategory
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ── Версії гри та лоадери для фільтра браузера (як /tag у Modrinth app) ──

// ModrinthGameVersion — одна версія гри з /tag/game_version. Порядок від
// API вже-відсортований (найновіші спочатку, release до snapshot).
type ModrinthGameVersion struct {
	Version     string `json:"version"`
	VersionType string `json:"version_type"` // release | snapshot | beta | alpha
	Date        string `json:"date"`
	Major       bool   `json:"major"`
}

// GetGameVersionTags повертає глобальний список версій гри, для яких у
// Modrinth є контент (на відміну від повного Mojang-маніфесту — тут лише
// версії з актуальним контентом). Це джерело списку версій у фільтрі
// браузера, як у веб-апці/app Modrinth.
func (c *ModrinthClient) GetGameVersionTags() ([]ModrinthGameVersion, error) {
	data, err := c.fetch("/tag/game_version", nil)
	if err != nil {
		return nil, err
	}
	var out []ModrinthGameVersion
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ModrinthLoaderTag — один лоадер з /tag/loader.
type ModrinthLoaderTag struct {
	Icon                  string   `json:"icon"`
	Name                  string   `json:"name"`
	SupportedProjectTypes []string `json:"supported_project_types"`
}

// GetLoaderTags повертає глобальний список лоадерів з /tag/loader (зі
// значенням, для яких типів контенту лоадер підходить — щоб фільтр
// показував лише релевантні поточному типу контенту, як у Modrinth app).
func (c *ModrinthClient) GetLoaderTags() ([]ModrinthLoaderTag, error) {
	data, err := c.fetch("/tag/loader", nil)
	if err != nil {
		return nil, err
	}
	var out []ModrinthLoaderTag
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetProjectDetail повертає повний опис проєкту Modrinth (/project/{id})
// з галереєю і описом-розміткою — для сторінки модпаку в браузері.
func (c *ModrinthClient) GetProjectDetail(projectID string) (*ModrinthProjectDetail, error) {
	if projectID == "" {
		return nil, fmt.Errorf("порожній projectID")
	}
	data, err := c.fetch("/project/"+url.PathEscape(projectID), nil)
	if err != nil {
		return nil, err
	}
	var d ModrinthProjectDetail
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, err
	}
	if d.ID == "" {
		d.ID = projectID
	}
	return &d, nil
}

// ── Версії проєкту для сторінки модпаку ────────────────────────────────
// mrVersion вже є (див. resolver.go) — він містить ID/version_number/
// game_versions/loaders/date_published/version_type/changelog/files[].
// Для сторінки модпаку потрібні ще й game_versions/loaders повними
// списками — їх віддає GetProjectVersions (вже існує).

// GetVersionByHash шукає версію мода за SHA1 файлу (/version_file/{hash}).
// Повертає nil, якщо мод не знайдено (404) — без помилки.
func (c *ModrinthClient) GetVersionByHash(sha1hex string) (*mrVersion, error) {
	if sha1hex == "" {
		return nil, nil
	}
	data, err := c.fetch("/version_file/"+url.PathEscape(sha1hex)+"?algorithm=sha1", nil)
	if err != nil {
		if strings.Contains(err.Error(), "404") {
			return nil, nil
		}
		return nil, err
	}
	var v mrVersion
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// firstNonEmptyStr повертає перший непорожній рядок.
func firstNonEmptyStr(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
