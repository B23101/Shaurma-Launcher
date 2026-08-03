package custompack

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// curseForgeAPIKey вшивається на етапі збірки через -ldflags
// (-X shaurma-launcher-wails/internal/custompack.curseForgeAPIKey=...),
// той самий підхід, що й tokenShaurma в internal/api/shaurma_packs_full.go.
// Без ключа офіційний CurseForge API (api.curseforge.com) відповідає 403 —
// це очікувано, доки ключ не додано; ImportCurseForgeZip у цьому випадку
// повертає зрозумілу помилку замість мовчазного пропуску модів.
var curseForgeAPIKey string

const curseForgeAPIBase = "https://api.curseforge.com/v1"

// CurseForgeClient — тонкий клієнт над офіційним CurseForge API,
// потрібним лише для одного виклику: резолв (projectID, fileID) → пряме
// посилання на завантаження файлу мода (той самий підхід, яким
// користуються MultiMC/Prism/GDLauncher при імпорті .zip-модпаків).
type CurseForgeClient struct {
	client *http.Client
}

func NewCurseForgeClient() *CurseForgeClient {
	return &CurseForgeClient{client: &http.Client{Timeout: 20 * time.Second}}
}

// HasAPIKey — чи вшито ключ у цю збірку лаунчера. UI показує попередження
// на вкладці "Імпортувати", якщо ключа немає (моди з CurseForge не
// зможуть автоматично довантажитись, лише overrides/локальні файли).
func (c *CurseForgeClient) HasAPIKey() bool { return curseForgeAPIKey != "" }

// HasCurseForgeAPIKey / CurseForgeAPIKeyValue — пакетні (не метод клієнта)
// хелпери для коду поза custompack (наприклад, downloadModFile у
// mods_api.go), якому треба лише перевірити наявність ключа й додати
// x-api-key на прямому HTTP-запиті до CDN, не створюючи CurseForgeClient.
func HasCurseForgeAPIKey() bool { return curseForgeAPIKey != "" }
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
		Hashes      []struct {
			Value string `json:"value"`
			Algo  int    `json:"algo"` // 1 = sha1, 2 = md5
		} `json:"hashes"`
	} `json:"data"`
}

// CFFileInfo — розв'язана інформація про файл мода з CurseForge.
type CFFileInfo struct {
	FileName    string
	DownloadURL string
	FileLength  int64
	SHA1        string
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
	LogoURL string `json:"logoUrl"`
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
	Hashes       []struct {
		Value string `json:"value"`
		Algo  int    `json:"algo"`
	} `json:"hashes"`
}

func (h *CFFileHit) SHA1() string {
	for _, x := range h.Hashes {
		if x.Algo == 1 {
			return x.Value
		}
	}
	return ""
}

func cfLoaderTypeID(loader string) int {
	switch strings.ToLower(loader) {
	case "forge":
		return 1
	case "fabric":
		return 4
	case "quilt":
		return 5
	case "neoforge":
		return 6
	default:
		return 1
	}
}

// doRequest виконує запит до CurseForge API з ретраєм на 429 (rate limit) —
// той самий підхід, що й у ModrinthClient.fetch: без нього паралельне
// сканування великої збірки (modUpdateConcurrency воркерів одночасно)
// губило б моди при першому ж 429 замість того, щоб почекати й повторити.
func (c *CurseForgeClient) doRequest(req *http.Request) (*http.Response, error) {
	req.Header.Set("x-api-key", curseForgeAPIKey)
	req.Header.Set("Accept", "application/json")
	// CurseForge, як і Modrinth, радить унікальний User-Agent з контактом
	// замість дефолтного Go http.Client (порожній) — щоб трафік не виглядав
	// як анонімний бот і не потрапив під блокування без попередження.
	req.Header.Set("User-Agent", "shaurma-team/shaurma-launcher/2.0 (shaurmaofficial2210@gmail.com)")
	for attempt := 0; attempt < 3; attempt++ {
		resp, err := c.client.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			resp.Body.Close()
			time.Sleep(time.Duration(attempt+1) * 700 * time.Millisecond)
			continue
		}
		return resp, nil
	}
	return nil, fmt.Errorf("CurseForge API: rate limit")
}

// SearchMods шукає моди на CurseForge за назвою (+ версія гри + лоадер).
// Без ключа — повертає зрозумілу помилку.
func (c *CurseForgeClient) SearchMods(query, gameVersion, loader string) ([]CFSearchHit, error) {
	if curseForgeAPIKey == "" {
		return nil, fmt.Errorf("CurseForge API-ключ не налаштовано у цій збірці лаунчера")
	}
	u := fmt.Sprintf("%s/mods/search?searchFilter=%s&gameId=432&classId=6&sortField=1&sortOrder=desc&pageSize=10",
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
func (c *CurseForgeClient) GetModFiles(projectID int, gameVersion, loader string) ([]CFFileHit, error) {
	if curseForgeAPIKey == "" {
		return nil, fmt.Errorf("CurseForge API-ключ не налаштовано у цій збірці лаунчера")
	}
	u := fmt.Sprintf("%s/mods/%d/files?pageSize=50", curseForgeAPIBase, projectID)
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
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Data []CFFileHit `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	var out []CFFileHit
	for _, f := range parsed.Data {
		if gameVersion != "" && !containsStr(f.GameVersions, gameVersion) {
			continue
		}
		if loader != "" && !containsLoader(f.ModLoaders, loader) {
			continue
		}
		out = append(out, f)
	}
	return out, nil
}

func containsStr(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func containsLoader(list []string, loader string) bool {
	l := strings.ToLower(loader)
	for _, x := range list {
		xl := strings.ToLower(x)
		if xl == l || strings.HasPrefix(xl, l) {
			return true
		}
	}
	return false
}

// GetModFile повертає інформацію про конкретний файл мода за
// (projectID, fileID) — саме ця пара зберігається в manifest.json
// CurseForge-модпаків (розділ files[]).
func (c *CurseForgeClient) GetModFile(projectID, fileID int) (*CFFileInfo, error) {
	if curseForgeAPIKey == "" {
		return nil, fmt.Errorf("CurseForge API-ключ не налаштовано у цій збірці лаунчера")
	}
	url := fmt.Sprintf("%s/mods/%d/files/%d", curseForgeAPIBase, projectID, fileID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
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
	if parsed.Data.DownloadURL == "" {
		// CurseForge інколи приховує downloadUrl для модів, що заборонили
		// сторонні лаунчери якати напряму — довелось би йти через
		// /mods/{id}/files/{id}/download-url. Явна помилка краще за
		// мовчазний порожній файл.
		return nil, fmt.Errorf("файл %d (проєкт %d) не має прямого посилання на завантаження — автор заборонив сторонні завантаження", fileID, projectID)
	}
	info := &CFFileInfo{
		FileName:    parsed.Data.FileName,
		DownloadURL: parsed.Data.DownloadURL,
		FileLength:  parsed.Data.FileLength,
	}
	for _, h := range parsed.Data.Hashes {
		if h.Algo == 1 {
			info.SHA1 = h.Value
		}
	}
	return info, nil
}
