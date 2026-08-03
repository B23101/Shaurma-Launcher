//go:build shaurma

package api

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	stdsync "sync"
	"time"

	"shaurma-launcher-wails/internal/sync"
)

const cdnBaseURL = "https://shaurma-proxy.shaurmaofficial2210.workers.dev"

// tokenShaurma вшивається на етапі збірки через -ldflags
// (-X shaurma-launcher-wails/internal/api.tokenShaurma=...). Воркер без нього
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
		Timeout: 0,
		Transport: &http.Transport{
			MaxIdleConns:        20,
			MaxConnsPerHost:     10,
			IdleConnTimeout:     90 * time.Second,
			TLSHandshakeTimeout: 20 * time.Second,
		},
	}
}

func (c *ShaurmaClient) GetPackIndex() ([]PackIndexEntry, error) {
	// Захист від «тихого» регресу: якщо хтось зібрав shaurma-редакцію без
	// -ldflags, токен порожній → воркер віддає 401 → список збірок порожній,
	// і вкладка «Шаурма» зникає без жодної помилки. Робимо помилку явною.
	if tokenShaurma == "" {
		return nil, fmt.Errorf("tokenShaurma is empty — build shaurma with -ldflags -X shaurma-launcher-wails/internal/api.tokenShaurma=...")
	}

	idx, err := c.mc.FetchPacksIndex()
	if err != nil {
		return nil, fmt.Errorf("fetch packs-index.json: %w", err)
	}

	entries := make([]PackIndexEntry, 0, len(idx.Packs))
	for _, p := range idx.Packs {
		entries = append(entries, PackIndexEntry{
			ID:            p.ID,
			Name:          p.Name,
			Description:   p.Description,
			MCVersion:     p.MCVersion,
			LoaderType:    p.Loader,
			LoaderVersion: p.LoaderVersion,
			IconURL:       resolveAssetURL(p.IconPath),
			BackgroundURL: resolveAssetURL(p.BackgroundPath),
			Tags:          p.Tags,
			Color:         p.AccentColor,
			ServerIP:      p.ServerIP,
			UpdatedAt:     p.UpdatedAt,
			Version:       p.UpdatedAt,
			ManifestPath:  p.ManifestPath,
		})
	}
	return entries, nil
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
