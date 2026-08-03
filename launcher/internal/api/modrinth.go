package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"shaurma-launcher-wails/internal/model"
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
		},
	}
}

func (c *ModrinthClient) fetch(path string, params map[string]string) ([]byte, error) {
	u, _ := url.Parse(c.baseURL + path)
	q := u.Query()
	for k, v := range params {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()

	// 429 (rate limit) — повторюємо з паузою до 3 спроб; менеджер модів шле
	// багато паралельних запитів, і без ретраю частина модів «губилась» б.
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequest("GET", u.String(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "shaurma-team/shaurma-launcher/2.0 (shaurmaofficial2210@gmail.com)")

		resp, err := c.client.Do(req)
		if err != nil {
			return nil, err
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
		ID      string `json:"id"`
		Slug    string `json:"slug"`
		Title   string `json:"title"`
		IconURL string `json:"icon_url"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	p := &ModrinthProjectInfo{ID: raw.ID, Slug: raw.Slug, Title: raw.Title, IconURL: raw.IconURL}
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
		DatePublished: v.DatePublished,
	}
	if len(v.GameVersions) > 0 {
		opt.GameVersion = v.GameVersions[0]
	}
	if len(v.Loaders) > 0 {
		opt.Loader = v.Loaders[0]
	}
	if len(v.Files) > 0 {
		f := v.Files[0]
		opt.URL = f.URL
		opt.Filename = f.Filename
		opt.Sha1 = f.Hashes.Sha1
	}
	return opt
}

// LatestVersionFor повертає найсвіжішу версію проєкту під конкретну версію
// гри та лоадер (для перевірки оновлень).
func (c *ModrinthClient) LatestVersionFor(projectID, gameVersion, loader string) (*model.ModVersionOption, error) {
	versions, err := c.GetProjectVersions(projectID, gameVersion, loader)
	if err != nil {
		return nil, err
	}
	if len(versions) == 0 {
		return nil, nil
	}
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