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
	ManifestPath   string   `json:"manifestPath"`
	ManifestEtag   string   `json:"manifestEtag,omitempty"`
	UpdatedAt      string   `json:"updatedAt"`
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
	ServerIP       string          `json:"serverIp,omitempty"`
	AccentColor    string          `json:"accentColor,omitempty"`
}

// HashMod — мод із зовнішнім URL (Modrinth/CurseForge CDN), Черга А.
type HashMod struct {
	FileName    string `json:"fileName"`
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

// ── Завантаження маніфестів через Worker ────────────────────────────────

// ManifestClient якає packs-index.json і per-pack manifest.json з Worker.
type ManifestClient struct {
	BaseURL string
	Token   string
	Client  *http.Client
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

func (c *ManifestClient) newRequest(path string) (*http.Request, error) {
	req, err := http.NewRequest(http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "ShaurmaLauncher/2.0")
	req.Header.Set("X-Shaurma-Token", c.Token)
	return req, nil
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
