package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"shaurma-launcher-backend/internal/model"
)

// ── Пошук і завантаження залежностей (порт CrashResolverService.java) ─────
// Старий лаунчер шукав залежність спершу на Modrinth (без ключа), потім
// фолбек на CurseForge (потребує API-ключ). Тут та сама структура:
// searchMods → pickBestModrinthMatch → getLatestVersion → download напряму
// в modsDir. CurseForge-гілка залишена як структура (tryCurseForge) — не
// блокер MVP, але додається легко.

// mrDepSearchResult — структура відповіді Modrinth /search для резолвера
// залежностей (окремий тип, щоб не конфліктувати з mrHit з modrinth.go,
// який використовує браузер контенту).
type mrDepSearchResult struct {
	Hits []mrDepHit `json:"hits"`
}

// mrDepHit — елемент результатів пошуку Modrinth для резолвера залежностей.
type mrDepHit struct {
	ProjectID  string   `json:"project_id"`
	Slug       string   `json:"slug"`
	Title      string   `json:"title"`
	Author     string   `json:"author"`
	IconURL    string   `json:"icon_url"`
	Categories []string `json:"categories"`
	Versions   []string `json:"versions"`
}

// mrVersion — версія проєкту Modrinth.
//
// Dependencies — масив залежностей версії (порт Labrinth.Versions.v3.Version):
// project_id/version_id/dependency_type. Це те, з чого Modrinth app будує
// DownloadDependencies ("Встановити з залежностями"): required-залежності
// встановлюються разом з проєктом (рекурсивно), embedded уже всередині
// головного архіву, optional — лише за бажанням. Files[].FileType — мітка
// "required-resource-pack" / "optional-resource-pack": так Modrinth позначає
// companion-ресурспак, який датапаки вимагають/радять встановити разом.
type mrVersion struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	VersionNumber string   `json:"version_number"`
	VersionType   string   `json:"version_type"` // release | beta | alpha
	Changelog     string   `json:"changelog"`
	GameVersions  []string `json:"game_versions"`
	Loaders       []string `json:"loaders"`
	ProjectID     string   `json:"project_id"`
	DatePublished string   `json:"date_published"`
	Files         []struct {
		URL      string `json:"url"`
		Filename string `json:"filename"`
		Size     int64  `json:"size"`
		Primary  bool   `json:"primary"`
		FileType string `json:"file_type"` // required-resource-pack | optional-resource-pack | ""
		Hashes   struct {
			Sha1   string `json:"sha1"`
			Sha512 string `json:"sha512"`
		} `json:"hashes"`
	} `json:"files"`
	Dependencies []struct {
		VersionID      string `json:"version_id"`
		ProjectID      string `json:"project_id"`
		FileName       string `json:"file_name"`
		DependencyType string `json:"dependency_type"` // required | optional | incompatible | embedded
	} `json:"dependencies"`
}

// PrimaryFileIndex повертає індекс файлу, позначеного Modrinth-ом як
// "primary" (саме він — той архів/мод, що встановлюється), або 0, якщо
// жоден файл не позначено primary — раніше код усюди брав Files[0]
// напряму, а Modrinth не гарантує, що первинний файл іде першим у
// масиві (у версій з кількома файлами, напр. складні модпаки типу
// Cobblemon, це призводило до "не має посилання на архів модпаку",
// бо URL брався з побічного файлу, а не з головного архіву).
func (v mrVersion) PrimaryFileIndex() int {
	for i := range v.Files {
		if v.Files[i].Primary {
			return i
		}
	}
	return 0
}

// SearchMods шукає моди на Modrinth за назвою + версією гри + лоадером.
func (c *ModrinthClient) SearchMods(query, gameVersion, loader string, limit, offset int) ([]mrDepHit, error) {
	params := map[string]string{
		"query":  query,
		"limit":  fmt.Sprintf("%d", limit),
		"offset": fmt.Sprintf("%d", offset),
	}
	// Фасети: кожен в окремому внутрішньому масиві — Modrinth робить AND між ними.
	var facets [][]string
	if gameVersion != "" {
		facets = append(facets, []string{"versions:" + gameVersion})
	}
	if loader != "" {
		facets = append(facets, []string{"categories:" + strings.ToLower(loader)})
	}
	if len(facets) > 0 {
		fj, _ := json.Marshal(facets)
		params["facets"] = string(fj)
	}
	data, err := c.fetch("/search", params)
	if err != nil {
		return nil, err
	}
	var res mrDepSearchResult
	if err := json.Unmarshal(data, &res); err != nil {
		return nil, err
	}
	return res.Hits, nil
}

// GetLatestVersion повертає найсвіжішу версію проєкту під конкретну версію
// гри та лоадер. Повертає перший елемент списку версій (як у старому
// лаунчері).
func (c *ModrinthClient) GetLatestVersion(projectID, gameVersion, loader string) (*mrVersion, error) {
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
	if len(versions) == 0 {
		return nil, fmt.Errorf("немає версій для %s (%s, %s)", projectID, gameVersion, loader)
	}
	return &versions[0], nil
}

// FullVersion — повна версія проєкту Modrinth з усіма файлами і
// залежностями (для авто-встановлення залежностей браузером контенту).
// Окремий експортований тип: mrVersion неекспортований, а браузеру
// (пакет main) потрібні саме ці поля.
type FullVersion struct {
	ID            string
	VersionNumber string
	VersionType   string
	GameVersions  []string
	Loaders       []string
	ProjectID     string
	Files         []FullVersionFile
	Dependencies  []FullVersionDependency
}

// FullVersionFile — файл версії для встановлення (URL + ім'я + file_type).
type FullVersionFile struct {
	URL      string
	Filename string
	Size     int64
	Primary  bool
	FileType string
	Sha1     string
	Sha512   string
}

// FullVersionDependency — залежність версії (порт Labrinth Dependency).
// Type: required | optional | incompatible | embedded.
type FullVersionDependency struct {
	VersionID string
	ProjectID string
	FileName  string
	Type      string
}

// GetVersionFull повертає повну версію проєкту за її ID — те, що Modrinth
// app отримує запитом GET /version/{id}. Потрібно для встановлення
// залежностей: списки GetProjectVersions не несуть dependencies/files,
// а для коректного "Встановити з залежностями" треба бачити їх повністю.
func (c *ModrinthClient) GetVersionFull(versionID string) (*FullVersion, error) {
	data, err := c.fetch("/version/"+url.PathEscape(versionID), nil)
	if err != nil {
		return nil, err
	}
	var v mrVersion
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	return versionToFull(&v), nil
}

// versionToFull конвертує внутрішній mrVersion у експортований FullVersion
// (той самий маппінг, що був у GetVersionFull — винесений окремо, бо
// GetVersionsByHashes теж віддає FullVersion назовні пакету main).
func versionToFull(v *mrVersion) *FullVersion {
	if v == nil {
		return nil
	}
	out := &FullVersion{
		ID: v.ID, VersionNumber: v.VersionNumber, VersionType: v.VersionType,
		GameVersions: v.GameVersions, Loaders: v.Loaders, ProjectID: v.ProjectID,
	}
	for _, f := range v.Files {
		out.Files = append(out.Files, FullVersionFile{
			URL: f.URL, Filename: f.Filename, Size: f.Size, Primary: f.Primary,
			FileType: f.FileType, Sha1: f.Hashes.Sha1, Sha512: f.Hashes.Sha512,
		})
	}
	for _, d := range v.Dependencies {
		out.Dependencies = append(out.Dependencies, FullVersionDependency{
			VersionID: d.VersionID, ProjectID: d.ProjectID, FileName: d.FileName, Type: d.DependencyType,
		})
	}
	return out
}

// DownloadFile завантажує файл за URL у dest (без ключа, як у старого).
func (c *ModrinthClient) DownloadFile(fileURL, dest string) (string, error) {
	req, err := http.NewRequest("GET", fileURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "ShaurmaLauncher/2.0")
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Modrinth download error: %d", resp.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return "", err
	}
	out, err := os.Create(dest)
	if err != nil {
		return "", err
	}
	defer out.Close()
	if _, err := io.Copy(out, resp.Body); err != nil {
		os.Remove(dest)
		return "", err
	}
	return dest, nil
}

// ResolveDependency — повний пайплайн пошуку та завантаження залежності
// (порт CrashResolverService.downloadMissingDependency). Спочатку Modrinth;
// якщо нічого не знайдено/не вдалось — tryCurseForge (структура готова,
// MVP повертає зрозумілу помилку з посиланням на сторінку пошуку).
func (c *ModrinthClient) ResolveDependency(depName, gameVersion, loader, modsDir string) model.DependencyResult {
	hits, err := c.SearchMods(depName, gameVersion, loader, 5, 0)
	if err != nil {
		return c.tryCurseForge(depName, gameVersion, loader, modsDir, err)
	}
	best := pickBestModrinthMatch(hits, depName)
	if best == nil {
		return c.tryCurseForge(depName, gameVersion, loader, modsDir, fmt.Errorf("не знайдено на Modrinth"))
	}
	v, err := c.GetLatestVersion(best.ProjectID, gameVersion, loader)
	if err != nil || v == nil || len(v.Files) == 0 || v.Files[v.PrimaryFileIndex()].URL == "" {
		return c.tryCurseForge(depName, gameVersion, loader, modsDir, fmt.Errorf("немає файлу для завантаження"))
	}
	file := v.Files[v.PrimaryFileIndex()]
	filename := file.Filename
	if filename == "" {
		filename = best.Slug + ".jar"
	}
	dest := filepath.Join(modsDir, filename)
	if _, err := c.DownloadFile(file.URL, dest); err != nil {
		return model.DependencyResult{
			Success:    false,
			Source:     "modrinth",
			Message:    "Не вдалось завантажити \"" + best.Title + "\": " + rootMessage(err),
			ProjectURL: "https://modrinth.com/mod/" + best.Slug,
		}
	}
	return model.DependencyResult{
		Success:     true,
		Source:      "modrinth",
		Message:     "Залежність \"" + best.Title + "\" завантажено в папку модів.",
		InstalledTo: dest,
	}
}

// tryCurseForge — фолбек-гілка (як у Java: потребує API-ключ). У новому
// лаунчері ключа немає — повертаємо зрозумілу помилку з посиланням для
// ручного пошуку. Структура готова до додавання реального клієнта.
func (c *ModrinthClient) tryCurseForge(depName, gameVersion, loader, modsDir string, cause error) model.DependencyResult {
	return model.DependencyResult{
		Success: false,
		Message: "Залежність \"" + depName + "\" не знайдено на Modrinth: " + rootMessage(cause) +
			". Пошукайте її вручну на Modrinth чи CurseForge.",
		ProjectURL: "https://modrinth.com/search?q=" + url.QueryEscape(depName),
	}
}

// BestModrinthMatch — експортована обгортка pickBestModrinthMatch для
// менеджера модів (вкладка «Моди» в main-пакеті).
func BestModrinthMatch(hits []mrDepHit, depName string) *mrDepHit {
	return pickBestModrinthMatch(hits, depName)
}

// pickBestModrinthMatch — вибір найкращого збігу (порт Java-версії):
// спершу точний збіг slug/title (нормалізовані — lowercase, тільки
// [a-z0-9]), потім slug/title, що ПОЧИНАЄТЬСЯ з потрібної назви (короткий
// modId типу "flywheel" чи "jei" — саме такий пошуковий запит формується
// з modId у mods.toml, а не довільна фраза користувача). "Часткового
// входження підрядка" НЕДОСТАТНЬО як критерію — Modrinth-пошук за
// "flywheel" реально повертає аддони на кшталт "Iris & Oculus Flywheel
// Compat" чи "Flywheel Version Fix" вище за сам мод Flywheel, і раніше
// код мовчки брав перший-ліпший хіт (hits[0]) навіть коли жоден
// результат не відповідав шуканому modId — звідси в Create показувалась
// "залежність" на аддон, якого немає в mods.toml. Тепер: якщо немає
// збігу за префіксом — вважаємо, що на Modrinth нічого не знайдено
// (nil), і викликач падає у CurseForge-фолбек / повідомлення про ручний
// пошук, замість підсовувати випадковий мод як нібито знайдену залежність.
func pickBestModrinthMatch(hits []mrDepHit, depName string) *mrDepHit {
	if len(hits) == 0 {
		return nil
	}
	needle := normalizeDep(depName)
	if needle == "" {
		return nil
	}
	for i := range hits {
		if normalizeDep(hits[i].Slug) == needle || normalizeDep(hits[i].Title) == needle {
			return &hits[i]
		}
	}
	for i := range hits {
		if strings.HasPrefix(normalizeDep(hits[i].Slug), needle) || strings.HasPrefix(normalizeDep(hits[i].Title), needle) {
			return &hits[i]
		}
	}
	return nil
}

func normalizeDep(s string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// ResolvedModInfo — короткі дані про мод, знайдений на Modrinth за назвою/
// ID (для карток-рекомендацій, без завантаження файлу).
type ResolvedModInfo struct {
	ProjectID string
	Slug      string
	Title     string
	IconURL   string
}

// ResolveModInfo шукає мод на Modrinth за depName (fabric mod ID або
// назва) під конкретну версію гри/лоадер і повертає короткі дані про
// найкращий збіг — той самий підбір, що ResolveDependency, але без
// завантаження файлу (для показу картки-пропозиції користувачу).
func (c *ModrinthClient) ResolveModInfo(depName, gameVersion, loader string) *ResolvedModInfo {
	hits, err := c.SearchMods(depName, gameVersion, loader, 5, 0)
	if err != nil {
		return nil
	}
	best := pickBestModrinthMatch(hits, depName)
	if best == nil {
		return nil
	}
	return &ResolvedModInfo{ProjectID: best.ProjectID, Slug: best.Slug, Title: best.Title, IconURL: best.IconURL}
}

func rootMessage(err error) string {
	if err == nil {
		return "невідома помилка"
	}
	msg := err.Error()
	if strings.Contains(msg, "HTTP 4") || strings.Contains(msg, "HTTP 5") {
		return msg
	}
	return msg
}
