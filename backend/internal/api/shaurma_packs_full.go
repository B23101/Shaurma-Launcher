//go:build shaurma

package api

import (
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"strings"
	stdsync "sync"
	"time"

	"shaurma-launcher-backend/internal/sync"
)

const cdnBaseURL = "https://shaurma-proxy.shaurmaofficial2210.workers.dev"

// tokenShaurma вшивається на етапі збірки через -ldflags
// (-X shaurma-launcher-backend/internal/api.tokenShaurma=...). Воркер без нього
// віддає 401 навіть на існуючий packs-index.json, тож без токена список
// Шаурма-збірок не завантажиться взагалі (див. worker.js: X-Shaurma-Token).
var tokenShaurma string

// ShaurmaClient V2 (DOWNLOAD_SYNC_DESIGN_V2.md) більше НЕ якає .mrpack
// цілим файлом: список збірок читається з packs-index.json, повний опис
// однієї збірки — з packs/<id>/manifest.json (обидва — легкі JSON,
// самі файли якаються окремо новим двочерговим sync.Runner'ом).
type ShaurmaClient struct {
	mc *sync.ManifestClient

	// assetsCache — in-memory кеш data-URL асетів (іконки/фони) за
	// оригінальним URL: файли невеликі, стабільні, а фронтенд може
	// перемальовуватись часто — повторно не тягнемо з CDN.
	assetsCache stdsync.Map // url -> string (data URL)

	// packsIndexMu/packsIndexAt/packsIndexCache — короткий TTL-кеш
	// packs-index.json (60 с). Список збірок міняється на worker лише при
	// публікації оновлення, а читається ЧАСТО: LaunchInstance робить
	// GetPackByID при КОЖНОМУ запуску гри, і без кешу кожен клік «Грати»
	// тягнув би index.json з CDN ще раз — зайва мережева затримка на
	// гарячому шляху. При збої запиту кеш НЕ оновлюється (старий результат
	// лишається дійсним до закінчення TTL).
	packsIndexMu    stdsync.Mutex
	packsIndexAt    time.Time
	packsIndexCache []PackIndexEntry
}

func NewShaurmaClient() *ShaurmaClient {
	return &ShaurmaClient{
		mc: sync.NewManifestClient(cdnBaseURL, tokenShaurma),
	}
}

// AssetDataURL повертає data URL асета збірки (іконка/фон) для фронтенду.
// Прямі <img src>/background-image не працюють: Worker віддає файли лише з
// токеном у заголовках (розділ 6 worker.js), які браузер додати не може, —
// тому картинки качаються через бекенд і повертаються як data: URL (той
// самий підхід, що в LoadFontFile/GetWardrobeTextureDataURL).
func (c *ShaurmaClient) AssetDataURL(assetURL string) (string, error) {
	if assetURL == "" {
		return "", nil
	}
	if v, ok := c.assetsCache.Load(assetURL); ok {
		return v.(string), nil
	}
	// Захист від довільних URL: дозволяємо лише асети власного CDN.
	if !strings.HasPrefix(assetURL, cdnBaseURL+"/") {
		return "", fmt.Errorf("asset URL поза CDN збірок: %s", assetURL)
	}
	path := strings.TrimPrefix(assetURL, cdnBaseURL+"/")
	data, ct, err := c.mc.FetchAsset(path)
	if err != nil {
		return "", err
	}
	dataURL := "data:" + ct + ";base64," + base64.StdEncoding.EncodeToString(data)
	c.assetsCache.Store(assetURL, dataURL)
	return dataURL, nil
}

// GetPackWiki повертає вікі збірки (packs/<id>/wiki.json) з CDN. nil, nil —
// вікі ще не створено (404): фронтенд не показує вкладку «Вікі».
func (c *ShaurmaClient) GetPackWiki(packID string) (*sync.PackWikiDoc, error) {
	if packID == "" {
		return nil, nil
	}
	return c.mc.FetchPackWiki(packID)
}

// BaseURL і Token — потрібні app_shaurma_full.go для побудови sync.Runner
// (Worker-запити воркер-файлів ідуть з тим самим базовим URL і токеном).
func (c *ShaurmaClient) BaseURL() string { return cdnBaseURL }
func (c *ShaurmaClient) Token() string   { return tokenShaurma }

// HTTPClientForSync повертає http.Client для sync.Runner: БЕЗ загального
// Timeout (він рахувався б для читання всього тіла — вбивав би якання
// великих файлів), з розумним keep-alive для повторних запитів у межах
// однієї черги.
func (c *ShaurmaClient) HTTPClientForSync() *http.Client {
	return &http.Client{
		Timeout: 0, // без загального Timeout — великі файли мають качатись довго
		Transport: &http.Transport{
			// Proxy — збереження поведінки дефолтного транспорту (явний
			// Transport не успадковує ProxyFromEnvironment; без нього запити
			// йшли б в обхід системного/корпоративного проксі).
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   20 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			MaxIdleConns:        20,
			MaxConnsPerHost:     10,
			IdleConnTimeout:     90 * time.Second,
			TLSHandshakeTimeout: 20 * time.Second,
			// Мовчазний сервер (прийняв TCP і не шле заголовки) обривається
			// тут, а не висить весь syncStallTimeout — «0.0 МБ/с без кінця».
			ResponseHeaderTimeout: 45 * time.Second,
			ForceAttemptHTTP2:     true,
		},
	}
}

// SetAccountsProvider підключає джерело акаунтів користувача — усі акаунти
// (не лише активний), щоб worker міг перевірити приватний доступ по
// БУДЬ-ЯКОМУ з них (виправляє «додав нового гравця в приватну групу, а
// активний акаунт лаунчера інший — доступу нема», див. sync.ManifestClient).
func (c *ShaurmaClient) SetAccountsProvider(p sync.AuthHeaderProvider) {
	c.mc.SetAccountsProvider(p)
}

func (c *ShaurmaClient) GetPackIndex() ([]PackIndexEntry, error) {
	// TTL-кеш: повертаємо свіжий результат, не торкаючись мережі.
	c.packsIndexMu.Lock()
	if c.packsIndexCache != nil && time.Since(c.packsIndexAt) < packsIndexTTL {
		out := c.packsIndexCache
		c.packsIndexMu.Unlock()
		return out, nil
	}
	c.packsIndexMu.Unlock()

	// Захист від «тихого» регресу: якщо хтось зібрав shaurma-редакцію без
	// -ldflags, токен порожній → воркер віддає 401 → список збірок порожній,
	// і вкладка «Шаурма» зникає без жодної помилки. Робимо помилку явною.
	if tokenShaurma == "" {
		return nil, fmt.Errorf("tokenShaurma is empty — build shaurma with -ldflags -X shaurma-launcher-backend/internal/api.tokenShaurma=...")
	}

	idx, err := c.mc.FetchPacksIndex()
	if err != nil {
		return nil, fmt.Errorf("fetch packs-index.json: %w", err)
	}

	entries := make([]PackIndexEntry, 0, len(idx.Packs))
	for _, p := range idx.Packs {
		entries = append(entries, PackIndexEntry{
			ID:                 p.ID,
			Name:               p.Name,
			Description:        p.Description,
			MCVersion:          p.MCVersion,
			LoaderType:         p.Loader,
			LoaderVersion:      p.LoaderVersion,
			IconURL:            resolveAssetURL(p.IconPath),
			BackgroundURL:      resolveAssetURL(p.BackgroundPath),
			Tags:               p.Tags,
			Color:              p.AccentColor,
			ServerIP:           p.ServerIP,
			OfficialServerSync: p.OfficialServerSync,
			UpdatedAt:          p.UpdatedAt,
			Version:            p.UpdatedAt,
			ManifestPath:       p.ManifestPath,
			LaunchArgs:         p.LaunchArgs,
			RecommendedRAMMB:   p.RecommendedRAMMB,
			FolderID:           p.FolderID,
			FolderName:         p.FolderName,
			FolderColor:        p.FolderColor,
			FolderOrder:        p.FolderOrder,
			FolderPosition:     p.FolderPosition,
		})
	}
	// Кешуємо лише успішний результат.
	c.packsIndexMu.Lock()
	c.packsIndexCache = entries
	c.packsIndexAt = time.Now()
	c.packsIndexMu.Unlock()
	return entries, nil
}

// packsIndexTTL — термін життя кешу packs-index.json на боці лаунчера.
// Раніше 60с і без жодного способу примусово оновити раніше — саме тому
// нова приватна збірка (щойно гравця додали в acl.json на сервері) не
// з'являлась у списку без перезапуску лаунчера: GetBuilds викликається
// при кожному відкритті вкладки, але сам він теж завжди бив у цей самий
// кеш. 15с — достатньо коротко, щоб короткий поллінг фронтенду (App.svelte
// refreshShaurmaCatalog, кожні ~20с) реально бачив зміну без рестарту, і
// достатньо довго, щоб не бити CDN на кожен дрібний ререндер UI.
const packsIndexTTL = 15 * time.Second

// InvalidatePackIndexCache скидає TTL-кеш негайно — викликається після дій,
// що напевно міняють видимість збірок (додали/перемкнули акаунт), щоб
// наступний GetPackIndex одразу пішов у мережу, а не чекав packsIndexTTL.
func (c *ShaurmaClient) InvalidatePackIndexCache() {
	c.packsIndexMu.Lock()
	c.packsIndexCache = nil
	c.packsIndexMu.Unlock()
}

func resolveAssetURL(path string) string {
	if path == "" {
		return ""
	}
	return cdnBaseURL + "/" + path
}

func (c *ShaurmaClient) GetPackByID(id string) (*PackIndexEntry, error) {
	entries, err := c.GetPackIndex()
	if err != nil {
		return nil, err
	}
	for i := range entries {
		if entries[i].ID == id {
			return &entries[i], nil
		}
	}
	return nil, fmt.Errorf("pack %s not found", id)
}

// GetPackManifest якає повний packs/<id>/manifest.json для конкретної
// збірки — це вхід для sync.BuildPlan/sync.Runner (заміна старого
// GetMrpackFileTasks, який якав .mrpack цілим файлом).
func (c *ShaurmaClient) GetPackManifest(packID string) (*sync.PackManifest, error) {
	entry, err := c.GetPackByID(packID)
	if err != nil {
		return nil, err
	}
	if entry.ManifestPath == "" {
		return nil, fmt.Errorf("pack %s: порожній manifestPath у packs-index.json", packID)
	}
	return c.mc.FetchPackManifest(entry.ManifestPath)
}

// CheckWorkerHealth — легкий health-check без токена (розрізняє "немає
// інтернету" від "worker тимчасово недоступний").
func (c *ShaurmaClient) CheckWorkerHealth() error {
	return c.mc.CheckHealth()
}

// GetEffectiveWhitelist якає глобальний (global-whitelist.json) і
// per-pack (packs/<id>/whitelist.json) білі списки та об'єднує їх у
// один набір mod-id (case-insensitive, lower-case ключі). ТЗ: «білий
// список працює окремо на збірку а є загальний білий список який
// додається з загальним» — тому лаунчер завжди перевіряє ОБИДВА разом.
//
// Мережева помилка одного з двох файлів НЕ провалює виклик повністю:
// повертає те, що вдалось завантажити, і best-effort лог через err
// (викликач може продовжити синк навіть без свіжого білого списку —
// краще залишити збірку "як була", ніж провалити весь Run через дрібний
// допоміжний файл).
//
// Гарячий шлях (кожен файл у mods/ під час перевірки цілісності —
// app_shaurma_full.go runner.IsWhitelisted): свідомо повертає лише
// map[string]bool, БЕЗ метаданих (Name/Platform/IconURL тощо) — їх
// парсити й тримати в пам'яті тут не потрібно, це просто зайва робота
// на гарячому шляху. Показ картки мода в UI (іконка, посилання на
// сторінку) — окремий метод нижче, GetEffectiveWhitelistDetailed,
// викликається лише коли людина відкриває сторінку білого списку
// збірки, не на кожен файл при синку.
func (c *ShaurmaClient) GetEffectiveWhitelist(packID string) (map[string]bool, error) {
	out := map[string]bool{}
	var firstErr error

	global, err := c.mc.FetchWhitelist("global-whitelist.json")
	if err != nil {
		firstErr = err
	} else {
		for _, e := range global.Entries {
			if e.ModID != "" {
				out[strings.ToLower(e.ModID)] = true
			}
		}
	}

	perPack, err := c.mc.FetchWhitelist("packs/" + packID + "/whitelist.json")
	if err != nil {
		if firstErr == nil {
			firstErr = err
		}
	} else {
		for _, e := range perPack.Entries {
			if e.ModID != "" {
				out[strings.ToLower(e.ModID)] = true
			}
		}
	}

	return out, firstErr
}

// GetEffectiveWhitelistDetailed — те саме об'єднання (глобальний +
// per-pack), але повертає ПОВНІ записи (Name/Source/Platform/ProjectID/
// IconURL/ProjectURL), а не голий набір id. Для UI: сторінка збірки
// показує, які моди дозволені понад маніфест, з іконкою мода і
// посиланням на його сторінку Modrinth/CurseForge, коли ці поля
// заповнені (запис додано за посиланням на проєкт у Pack Manager) —
// для записів "jar"/"manual" без прив'язки до платформи UI показує
// звичайну картку без іконки (fallback), як і раніше.
//
// Дедуплікація за ModID (case-insensitive): якщо той самий мод є і в
// глобальному, і в per-pack списку — per-pack запис виграє (свіжіший
// намір автора саме цієї збірки).
func (c *ShaurmaClient) GetEffectiveWhitelistDetailed(packID string) ([]sync.WhitelistEntry, error) {
	byID := map[string]sync.WhitelistEntry{}
	var firstErr error

	global, err := c.mc.FetchWhitelist("global-whitelist.json")
	if err != nil {
		firstErr = err
	} else {
		for _, e := range global.Entries {
			if e.ModID != "" {
				byID[strings.ToLower(e.ModID)] = e
			}
		}
	}

	perPack, err := c.mc.FetchWhitelist("packs/" + packID + "/whitelist.json")
	if err != nil {
		if firstErr == nil {
			firstErr = err
		}
	} else {
		for _, e := range perPack.Entries {
			if e.ModID != "" {
				byID[strings.ToLower(e.ModID)] = e // per-pack перекриває global
			}
		}
	}

	out := make([]sync.WhitelistEntry, 0, len(byID))
	for _, e := range byID {
		out = append(out, e)
	}
	return out, firstErr
}
