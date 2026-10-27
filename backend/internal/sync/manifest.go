// Пакет sync реалізує нову схему синхронізації збірок Шаурма
// (DOWNLOAD_SYNC_DESIGN_V2.md): .mrpack більше НЕ якається лаунчером як
// цілий файл — Pack Manager розпаковує його на сервері під час deploy,
// Worker роздає вже готові адресовані частини (окремі hash-моди, окремі
// worker-файли, override-теки як zip-архіви), а лаунчер синхронізує їх
// двома незалежними паралельними чергами.
package sync

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

// PacksIndex — packs-index.json, легкий корінний файл (розділ 2.1 V2).
type PacksIndex struct {
	Version     int              `json:"version"`
	GeneratedAt string           `json:"generatedAt"`
	Packs       []PacksIndexPack `json:"packs"`
}

type PacksIndexPack struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	MCVersion      string   `json:"mcVersion"`
	Loader         string   `json:"loader"`
	LoaderVersion  string   `json:"loaderVersion"`
	IconPath       string   `json:"iconPath,omitempty"`
	IconEtag       string   `json:"iconEtag,omitempty"`
	BackgroundPath string   `json:"backgroundPath,omitempty"`
	BackgroundEtag string   `json:"backgroundEtag,omitempty"`
	Tags           []string `json:"tags,omitempty"`
	AccentColor    string   `json:"accentColor,omitempty"`
	ServerIP       string   `json:"serverIp,omitempty"`
	// OfficialServerSync — «офіційний сервер завжди в servers.dat»: якщо true,
	// лаунчер при кожному запуску збірки ставить/оновлює в servers.dat запис
	// із ServerIP (актуальний IP із CDN). Пише Pack Manager.
	OfficialServerSync bool   `json:"officialServerSync,omitempty"`
	ManifestPath       string `json:"manifestPath"`
	ManifestEtag       string `json:"manifestEtag,omitempty"`
	UpdatedAt          string `json:"updatedAt"`
	// LaunchArgs — аргументи запуску збірки (пише Pack Manager; лаунчер
	// додає їх до команди запуску гри). RecommendedRAMMB — рекомендована
	// оперативна пам'ять (МБ): якщо користувач виділив менше — перед
	// запуском показується попередження.
	LaunchArgs       []string `json:"launchArgs,omitempty"`
	RecommendedRAMMB int      `json:"recommendedRamMb,omitempty"`
	// ── Групування по серверній папці (ТЗ: «групування по кастомній
	// папці... викласти на сервер тоді лаунчер буде показувати папку з
	// тими збірками з її назвою і кольором») ──
	// FolderID порожній — збірка НЕ в жодній серверній папці (як зараз).
	// Заданий — усі збірки з однаковим FolderID лаунчер групує в ОДНУ
	// папку sidebar (FolderName/FolderColor беруться з першого запису —
	// Pack Manager пише їх однаково в усіх збірках однієї папки).
	// FolderOrder — позиція ЦІЄЇ збірки всередині папки (сортування, яке
	// задає розробник у Pack Manager); FolderPosition — позиція самої
	// папки серед інших папок/збірок верхнього рівня. Те, відкрита папка
	// чи згорнута, і чи користувач вручну переставив саму папку (не
	// вміст!) — ЛОКАЛЬНИЙ стан лаунчера (packLayout, головний
	// settings.json), сервер цього не знає і не диктує.
	FolderID       string `json:"folderId,omitempty"`
	FolderName     string `json:"folderName,omitempty"`
	FolderColor    string `json:"folderColor,omitempty"`
	FolderOrder    int    `json:"folderOrder,omitempty"`
	FolderPosition int    `json:"folderPosition,omitempty"`
}

// PackManifest — packs/<id>/manifest.json, повний маніфест ОДНІЄЇ збірки
// (розділ 2.2 V2).
type PackManifest struct {
	ID             string          `json:"id"`
	SchemaVersion  int             `json:"schemaVersion"`
	MCVersion      string          `json:"mcVersion"`
	Loader         string          `json:"loader"`
	LoaderVersion  string          `json:"loaderVersion"`
	UpdatedAt      string          `json:"updatedAt"`
	ContentHash    string          `json:"contentHash"`
	HashMods       []HashMod       `json:"hashMods"`
	WorkerFiles    []WorkerFile    `json:"workerFiles"`
	OnMissingFiles []OnMissingFile `json:"onMissingFiles"`
	// OnMissingFolders — override-теки з правилом on_missing/first_install_only
	// (наприклад "config/", "resourcepacks/", "shaderpacks/"): архів лежить на
	// R2, але розпаковується ЛИШЕ якщо теки ще НЕМАЄ на диску, і ніколи
	// більше не перезаписується (на відміну від WorkerFile{Kind:
	// KindOverrideFolder}, яка перезаписується щоразу при зміні контенту).
	// Пише Pack Manager (manifest.OnMissingFolders); лаунчер читає і
	// довстановлює у фазі finalizing. РЕГРЕСІЯ 2026-08-09: поле було відсутнє
	// в лаунчері → теки config/ тощо ніколи не встановлювались.
	OnMissingFolders []OnMissingFolder `json:"onMissingFolders,omitempty"`
	ServerIP         string            `json:"serverIp,omitempty"`
	// OfficialServerSync — дубль прапорця з packs-index (див. PacksIndexPack).
	OfficialServerSync bool   `json:"officialServerSync,omitempty"`
	AccentColor        string `json:"accentColor,omitempty"`
	// LaunchArgs / RecommendedRAMMB — ті самі поля, що в packs-index:
	// дублюються у маніфесті для повноти, але лаунчер читає їх з індексу
	// (без зайвого fetch маніфесту перед запуском).
	LaunchArgs       []string `json:"launchArgs,omitempty"`
	RecommendedRAMMB int      `json:"recommendedRamMb,omitempty"`
}

// HashMod — мод із зовнішнім URL (Modrinth/CurseForge CDN), Черга А.
type HashMod struct {
	FileName string `json:"fileName"`
	// DestPath — відносний шлях призначення в .minecraft/ (наприклад
	// "shaderpacks/x.zip"). Порожній (звичайні моди, старі маніфести) —
	// файл кладеться за замовчуванням у "mods/"+FileName, як завжди.
	// ЗАДАНИЙ — файл кладеться саме за цим шляхом (шейдерпаки/ресурспаки
	// з modrinth.index.json теж мають прямий CDN-URL і тому потрапляють
	// у Чергу А, але не є модами — без DestPath вони помилково опинялись
	// у mods/ поряд зі звичайними модами).
	DestPath    string `json:"destPath,omitempty"`
	SHA1        string `json:"sha1,omitempty"`
	SHA512      string `json:"sha512,omitempty"`
	FileSize    int64  `json:"fileSize"`
	DownloadURL string `json:"downloadUrl"`
	UpdatedAt   string `json:"updatedAt"`
}

// WorkerFileKind — тип файлу, що якається з Worker (Черга B).
type WorkerFileKind string

const (
	KindBundledMod     WorkerFileKind = "bundled-mod"
	KindOverrideFile   WorkerFileKind = "override-file"
	KindOverrideFolder WorkerFileKind = "override-folder"
)

// WorkerFile — bundled-мод / override-файл / override-архів (Черга B).
type WorkerFile struct {
	Kind        WorkerFileKind `json:"kind"`
	FileName    string         `json:"fileName,omitempty"`  // bundled-mod
	LocalPath   string         `json:"localPath,omitempty"` // override-file/-folder
	Path        string         `json:"path"`                // R2-ключ (шлях на Worker)
	SHA256      string         `json:"sha256"`
	FileSize    int64          `json:"fileSize"`
	ArchiveSize int64          `json:"archiveSize,omitempty"`
	FileCount   int            `json:"fileCount,omitempty"`
	UpdatedAt   string         `json:"updatedAt"`
}

// DestPath повертає ВІДНОСНИЙ (до кореня установки збірки) шлях, куди
// файл має бути записаний на диску гравця.
func (w WorkerFile) DestPath() string {
	switch w.Kind {
	case KindBundledMod:
		return "mods/" + w.FileName
	case KindOverrideFile:
		return w.LocalPath
	case KindOverrideFolder:
		return w.LocalPath // сама тека; вміст архіву розпаковується СЮДИ
	default:
		return w.LocalPath
	}
}

// OnMissingFile — файл, що довстановлюється лише якщо гравець його видалив.
type OnMissingFile struct {
	LocalPath string `json:"localPath"`
	Path      string `json:"path"`
	SHA256    string `json:"sha256"`
}

// OnMissingFolder — тека (наприклад "config/"), позначена правилом
// on_missing / first_install_only: архів лежить на R2, але лаунчер
// розпаковує його ЛИШЕ якщо теки ще немає на диску, і ніколи більше не
// перезаписує — на відміну від WorkerFile{Kind: KindOverrideFolder}, яка
// перезаписується щоразу, коли контент архіву змінюється.
type OnMissingFolder struct {
	LocalPath string `json:"localPath"`
	Path      string `json:"path"`
	SHA256    string `json:"sha256"`
	FileSize  int64  `json:"fileSize"`
	FileCount int    `json:"fileCount"`
}

// ── Білий список модів ───────────────────────────────────────────────────
// Точна копія форми pack-manager/internal/model.Whitelist — Pack Manager
// публікує ЦЕЙ JSON окремим файлом на CDN (global-whitelist.json у корені
// бакета, packs/<id>/whitelist.json для конкретної збірки), незалежно
// від manifest.json самої збірки. Це навмисно: додавання/видалення мода з
// білого списку не мусить чекати повного деплою збірки — лаунчер
// перечитує ці два файли окремо (короткий TTL, як packs-index.json) і
// звіряє JAR-и в mods/ за ModID (з fabric.mod.json/mods.toml/mcmod.info),
// а НЕ за іменем файлу: той самий мод різних версій має різне ім'я jar,
// але сталий id.
type Whitelist struct {
	Version   int              `json:"version"`
	UpdatedAt string           `json:"updatedAt"`
	Entries   []WhitelistEntry `json:"entries"`
}

type WhitelistEntry struct {
	ModID     string `json:"modId"`
	Name      string `json:"name,omitempty"`
	Source    string `json:"source,omitempty"`
	SourceRef string `json:"sourceRef,omitempty"`
	// Platform/ProjectID/IconURL/ProjectURL — прив'язка до Modrinth/
	// CurseForge (заповнено, коли Source — "modrinth"/"curseforge": автор
	// збірки додав запис вибором проєкту за посиланням на його сторінку в
	// Pack Manager, а не через локальний jar). Дають змогу UI лаунчера
	// показати картку мода з іконкою і посиланням на сторінку, а не голий
	// ModID — див. GetEffectiveWhitelistDetailed нижче.
	Platform   string `json:"platform,omitempty"`
	ProjectID  string `json:"projectId,omitempty"`
	IconURL    string `json:"iconUrl,omitempty"`
	ProjectURL string `json:"projectUrl,omitempty"`
	AddedAt    string `json:"addedAt,omitempty"`
}

// ── Завантаження маніфестів через Worker ────────────────────────────────

// AccountCred — одна ідентичність для worker'а (worker/acl.js:
// resolveIdentities). Microsoft/Minecraft-токени на сервер НЕ передаються
// взагалі: підтверджена особа — це лише сесія Google-акаунта Shaurma.
//   - Token — access-JWT Google-сесії Shaurma (підтверджена особа);
//   - Nick  — офлайн-нік (непідтверджений, годиться лише для звичайних
//     правил доступу за ніком, не для licensedOnly).
type AccountCred struct {
	Token string
	Nick  string
}

// AuthHeaderProvider повертає ПОТОЧНИЙ список акаунтів користувача, які
// потрібно передати worker'у в X-Shaurma-Auth. Викликається окремо на
// кожен запит (не кешується на боці ManifestClient) — це те, що виправляє
// «додав нового гравця в приватну групу, а активний акаунт лаунчера
// інший» без рестарту: якщо гравець додав/перемкнув акаунт, наступний же
// запит списку збірок уже понесе оновлений набір. Nil означає «акаунти
// не налаштовані» — тоді запити йдуть анонімно (як і раніше).
type AuthHeaderProvider func() []AccountCred

// ManifestClient якає packs-index.json і per-pack manifest.json з Worker.
type ManifestClient struct {
	BaseURL string
	Token   string
	Client  *http.Client

	// Accounts — джерело акаунтів користувача для X-Shaurma-Auth. Задає
	// виклик SetAccountsProvider (з App, де живуть model.Account). Якщо
	// nil, запити йдуть без ідентичності (тільки публічні збірки).
	Accounts AuthHeaderProvider
}

func NewManifestClient(baseURL, token string) *ManifestClient {
	return &ManifestClient{
		BaseURL: baseURL,
		Token:   token,
		Client: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

// SetAccountsProvider підключає джерело акаунтів користувача (викликає
// App при старті shaurmaProvider — див. backend/app_shaurma_full.go).
func (c *ManifestClient) SetAccountsProvider(p AuthHeaderProvider) {
	c.Accounts = p
}

func (c *ManifestClient) newRequest(path string) (*http.Request, error) {
	req, err := http.NewRequest(http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "ShaurmaLauncher/2.0")
	req.Header.Set("X-Shaurma-Token", c.Token)
	c.applyIdentityHeaders(req)
	return req, nil
}

// applyIdentityHeaders додає X-Shaurma-Auth (масив акаунтів — worker/acl.js
// parseAuthHeader: [{"token":"…"} | {"nick":"…"}]) до запиту, якщо
// провайдер акаунтів підключений і в лаунчері є хоч один акаунт.
// Ідентичність потрібна ЛИШЕ для читання приватних збірок — публічні
// збірки/файли worker віддає і без неї. Жодних мережевих викликів тут
// немає: токен уже готовий (Google-сесія), тож це не може блокувати запит.
func (c *ManifestClient) applyIdentityHeaders(req *http.Request) {
	if c.Accounts == nil {
		return
	}
	creds := c.Accounts()
	if len(creds) == 0 {
		return
	}
	type authEntry struct {
		Token string `json:"token,omitempty"`
		Nick  string `json:"nick,omitempty"`
	}
	entries := make([]authEntry, 0, len(creds))
	for _, cr := range creds {
		if cr.Token != "" {
			entries = append(entries, authEntry{Token: cr.Token})
		} else if cr.Nick != "" {
			entries = append(entries, authEntry{Nick: cr.Nick})
		}
	}
	if len(entries) == 0 {
		return
	}
	data, err := json.Marshal(entries)
	if err != nil {
		return
	}
	req.Header.Set("X-Shaurma-Auth", string(data))
}

// FetchPacksIndex якає легкий кореневий packs-index.json.
func (c *ManifestClient) FetchPacksIndex() (*PacksIndex, error) {
	req, err := c.newRequest("/packs-index.json")
	if err != nil {
		return nil, err
	}
	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, classifyNetErr(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("packs-index.json: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var idx PacksIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("parse packs-index.json: %w", err)
	}
	return &idx, nil
}

// FetchPackManifest якає повний manifest.json ОДНІЄЇ збірки за шляхом з
// packs-index.json (entry.ManifestPath).
func (c *ManifestClient) FetchPackManifest(manifestPath string) (*PackManifest, error) {
	if manifestPath == "" {
		return nil, fmt.Errorf("порожній manifestPath")
	}
	req, err := c.newRequest("/" + manifestPath)
	if err != nil {
		return nil, err
	}
	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, classifyNetErr(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", manifestPath, resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var pm PackManifest
	if err := json.Unmarshal(data, &pm); err != nil {
		return nil, fmt.Errorf("parse %s: %w", manifestPath, err)
	}
	return &pm, nil
}

// CheckHealth перевіряє /health — легкий ендпоінт без токена. Повертає nil,
// якщо worker відповідає (незалежно від токена/R2-стану), інакше —
// класифіковану мережеву помилку (розділ 3.6/4 V2-документа: "немає
// інтернету" відрізняється тут від "worker недоступний", бо DialContext сам
// провалиться на рівні transport, а не отримає HTTP-статус).
func (c *ManifestClient) CheckHealth() error {
	req, err := http.NewRequest(http.MethodGet, c.BaseURL+"/health", nil)
	if err != nil {
		return err
	}
	resp, err := c.Client.Do(req)
	if err != nil {
		return classifyNetErr(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("worker health: HTTP %d", resp.StatusCode)
	}
	return nil
}

// FetchAsset якає асет збірки (іконка/фон) з Worker і повертає тіло файлу
// та Content-Type. Потрібно для data-URL на фронтенді: <img>/background-image
// не можуть додати X-Shaurma-Token до запиту (Worker віддає файли лише з
// токеном — розділ 6 worker.js), тому асети тягнуться через бекенд, який
// вже має токен і правильний User-Agent.
func (c *ManifestClient) FetchAsset(path string) ([]byte, string, error) {
	if path == "" {
		return nil, "", fmt.Errorf("порожній шлях асета")
	}
	req, err := c.newRequest("/" + strings.TrimPrefix(path, "/"))
	if err != nil {
		return nil, "", err
	}
	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, "", classifyNetErr(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("asset %s: HTTP %d", path, resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	ct := resp.Header.Get("Content-Type")
	// Worker для .jpg/.webp/.gif віддає application/octet-stream (його
	// guessMime знає лише .png/.json/.exe/.zip/.jar/.mrpack) — такий data URL
	// CSS background-image може відхилити. Якщо заголовок не image/* —
	// беремо MIME за розширенням.
	if !strings.HasPrefix(ct, "image/") {
		ct = guessAssetMime(path)
	}
	return data, ct, nil
}

// guessAssetMime — дефолтний Content-Type за розширенням (якщо Worker не
// віддав заголовок): .png → image/png, .jpg/.jpeg → image/jpeg тощо.
func guessAssetMime(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	default:
		return "application/octet-stream"
	}
}

// FetchWhitelist якає один білий список за шляхом ("global-whitelist.json"
// для глобального, "packs/<id>/whitelist.json" для конкретної збірки).
// 404 — НЕ помилка: список ще ніколи не публікувався (типовий стан для
// більшості збірок) — повертає порожній Whitelist, nil. Викликач
// об'єднує глобальний і per-pack результати перед звіркою з локальними
// jar-файлами.
func (c *ManifestClient) FetchWhitelist(path string) (*Whitelist, error) {
	req, err := c.newRequest("/" + strings.TrimPrefix(path, "/"))
	if err != nil {
		return nil, err
	}
	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, classifyNetErr(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return &Whitelist{}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", path, resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var wl Whitelist
	if err := json.Unmarshal(data, &wl); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &wl, nil
}

// ── Вікі збірки (packs/<id>/wiki.json) ──────────────────────────────────
//
// Дзеркало model.WikiDoc з Pack Manager. Лаунчер лише ЧИТАЄ документ:
// розділи з картинкою зліва та HTML/Markdown-описом справа, які адмін
// створив у Pack Manager. Формат навмисно без окремих файлів картинок:
// Image — вбудований data URL, тому worker/авторизація не потрібні.
type PackWikiSection struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Icon   string `json:"icon,omitempty"`
	Image  string `json:"image,omitempty"`
	Body   string `json:"body,omitempty"`
	Format string `json:"format,omitempty"`
}

type PackWikiDoc struct {
	Enabled     bool              `json:"enabled"`
	Title       string            `json:"title,omitempty"`
	Description string            `json:"description,omitempty"`
	UpdatedAt   string            `json:"updatedAt,omitempty"`
	Sections    []PackWikiSection `json:"sections"`
}

// FetchPackWiki якає packs/<id>/wiki.json. 404 — НЕ помилка: вікі ще не
// створено (типовий стан) — повертає nil, nil, і фронтенд просто не
// показує вкладку «Вікі».
func (c *ManifestClient) FetchPackWiki(packID string) (*PackWikiDoc, error) {
	path := "packs/" + packID + "/wiki.json"
	req, err := c.newRequest("/" + path)
	if err != nil {
		return nil, err
	}
	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, classifyNetErr(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", path, resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var doc PackWikiDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &doc, nil
}
