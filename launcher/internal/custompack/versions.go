// Пакет custompack реалізує створення та імпорт КАСТОМНИХ (не-Shaurma)
// збірок гравця: вибір версії Minecraft/лоадера вручну (розділ "Нова
// збірка → Вручну") та імпорт готового модпаку .mrpack/.zip (розділ
// "Нова збірка → Імпортувати"), за зразком Prism Launcher.
//
// На відміну від Shaurma-збірок (packs.PackProvider), тут немає власного
// сервера-джерела правди: версії Minecraft/лоадерів беруться напряму з
// офіційних публічних API (Mojang, Fabric Meta, Forge/NeoForge Maven,
// Quilt Meta), а сама збірка — локальний запис у builds.Registry.
package custompack

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Loader — підтримувані завантажувачі модів кастомної збірки.
type Loader string

const (
	LoaderVanilla  Loader = "vanilla"
	LoaderFabric   Loader = "fabric"
	LoaderForge    Loader = "forge"
	LoaderNeoForge Loader = "neoforge"
	LoaderQuilt    Loader = "quilt"
)

// MCVersionEntry — одна версія Minecraft у списку вибору (спрощення
// minecraft.VersionManifest — тут нам потрібні лише id/type для UI).
type MCVersionEntry struct {
	ID          string `json:"id"`
	Type        string `json:"type"` // release | snapshot | old_beta | old_alpha
	ReleaseTime string `json:"releaseTime"`
}

// LoaderVersionEntry — одна версія конкретного лоадера.
type LoaderVersionEntry struct {
	Version       string `json:"version"`
	Stable        bool   `json:"stable"`
	Recommended   bool   `json:"recommended"`
}

// VersionsProvider якає й кешує списки версій із зовнішніх API. Кеш —
// щоб відкриття сторінки "Нова збірка" не било в мережу щоразу (версії
// оновлюються рідко), TTL 30 хв достатньо для сесії лаунчера.
type VersionsProvider struct {
	client *http.Client

	mcCache      []MCVersionEntry
	mcCacheAt    time.Time
	loaderCache  map[Loader]loaderCacheEntry
}

type loaderCacheEntry struct {
	versions map[string][]LoaderVersionEntry // key: mcVersion (порожньо = всі/незалежно від mcVersion)
	at       time.Time
}

const cacheTTL = 30 * time.Minute

func NewVersionsProvider() *VersionsProvider {
	return &VersionsProvider{
		client:      &http.Client{Timeout: 15 * time.Second},
		loaderCache: map[Loader]loaderCacheEntry{},
	}
}

func (p *VersionsProvider) get(url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "ShaurmaLauncher/2.0 (+custompack)")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
	}
	buf := make([]byte, 0, 65536)
	tmp := make([]byte, 65536)
	for {
		n, e := resp.Body.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if e != nil {
			break
		}
	}
	return buf, nil
}

// ── Minecraft (Mojang version_manifest_v2) ──────────────────────────────

type mojangManifest struct {
	Latest struct {
		Release  string `json:"release"`
		Snapshot string `json:"snapshot"`
	} `json:"latest"`
	Versions []struct {
		ID          string `json:"id"`
		Type        string `json:"type"`
		ReleaseTime string `json:"releaseTime"`
	} `json:"versions"`
}

// GetMCVersions повертає всі версії Minecraft (release+snapshot+старі),
// найновіші перші — так само як їх видає Mojang. includeSnapshots і
// includeOld фільтрують список для UI (перемикачі "Snapshot"/"Beta" на
// сторінці створення збірки, як у старому лаунчері).
func (p *VersionsProvider) GetMCVersions() ([]MCVersionEntry, error) {
	if p.mcCache != nil && time.Since(p.mcCacheAt) < cacheTTL {
		return p.mcCache, nil
	}
	data, err := p.get("https://launchermeta.mojang.com/mc/game/version_manifest_v2.json")
	if err != nil {
		if p.mcCache != nil {
			return p.mcCache, nil // деградуємо на застарілий кеш, а не падаємо
		}
		return nil, fmt.Errorf("не вдалося отримати список версій Minecraft: %w", err)
	}
	var m mojangManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	out := make([]MCVersionEntry, 0, len(m.Versions))
	for _, v := range m.Versions {
		out = append(out, MCVersionEntry{ID: v.ID, Type: v.Type, ReleaseTime: v.ReleaseTime})
	}
	p.mcCache = out
	p.mcCacheAt = time.Now()
	return out, nil
}

// LatestReleaseMCVersion — поточна стабільна версія (для дефолтного
// вибору у формі створення збірки).
func (p *VersionsProvider) LatestReleaseMCVersion() (string, error) {
	data, err := p.get("https://launchermeta.mojang.com/mc/game/version_manifest_v2.json")
	if err != nil {
		return "", err
	}
	var m mojangManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return "", err
	}
	return m.Latest.Release, nil
}

// ── Fabric (Fabric Meta API) ─────────────────────────────────────────────

type fabricLoaderMeta struct {
	Loader struct {
		Version string `json:"version"`
		Stable  bool   `json:"stable"`
	} `json:"loader"`
}

// GetFabricLoaderVersions повертає версії Fabric loader, сумісні з
// вказаною версією Minecraft (найновіша стабільна — Recommended=true,
// як у старому лаунчері "0.16.5 (рекомендовано)").
func (p *VersionsProvider) GetFabricLoaderVersions(mcVersion string) ([]LoaderVersionEntry, error) {
	if cached, ok := p.cachedLoader(LoaderFabric, mcVersion); ok {
		return cached, nil
	}
	url := "https://meta.fabricmc.net/v2/versions/loader/" + strings.TrimSpace(mcVersion)
	data, err := p.get(url)
	if err != nil {
		return nil, fmt.Errorf("Fabric Meta: %w", err)
	}
	var raw []fabricLoaderMeta
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	out := make([]LoaderVersionEntry, 0, len(raw))
	for i, r := range raw {
		out = append(out, LoaderVersionEntry{
			Version:     r.Loader.Version,
			Stable:      r.Loader.Stable,
			Recommended: i == 0 && r.Loader.Stable,
		})
	}
	p.storeLoader(LoaderFabric, mcVersion, out)
	return out, nil
}

// ── Quilt (Quilt Meta API, той самий протокол, що й Fabric) ─────────────

type quiltLoaderMeta struct {
	Loader struct {
		Version string `json:"version"`
	} `json:"loader"`
}

func (p *VersionsProvider) GetQuiltLoaderVersions(mcVersion string) ([]LoaderVersionEntry, error) {
	if cached, ok := p.cachedLoader(LoaderQuilt, mcVersion); ok {
		return cached, nil
	}
	url := "https://meta.quiltmc.org/v3/versions/loader/" + strings.TrimSpace(mcVersion)
	data, err := p.get(url)
	if err != nil {
		return nil, fmt.Errorf("Quilt Meta: %w", err)
	}
	var raw []quiltLoaderMeta
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	out := make([]LoaderVersionEntry, 0, len(raw))
	for i, r := range raw {
		out = append(out, LoaderVersionEntry{Version: r.Loader.Version, Stable: true, Recommended: i == 0})
	}
	p.storeLoader(LoaderQuilt, mcVersion, out)
	return out, nil
}

// ── Forge / NeoForge (maven-metadata.xml) ────────────────────────────────

type mavenMetadata struct {
	Versioning struct {
		Versions struct {
			Version []string `xml:"version"`
		} `xml:"versions"`
	} `xml:"versioning"`
}

// GetForgeLoaderVersions парсить maven-metadata.xml Forge і фільтрує
// версії формату "<mcVersion>-<forgeVersion>" під потрібну версію гри.
func (p *VersionsProvider) GetForgeLoaderVersions(mcVersion string) ([]LoaderVersionEntry, error) {
	if cached, ok := p.cachedLoader(LoaderForge, mcVersion); ok {
		return cached, nil
	}
	data, err := p.get("https://maven.minecraftforge.net/net/minecraftforge/forge/maven-metadata.xml")
	if err != nil {
		return nil, fmt.Errorf("Forge Maven: %w", err)
	}
	var meta mavenMetadata
	if err := xml.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	prefix := mcVersion + "-"
	out := make([]LoaderVersionEntry, 0, 16)
	for _, v := range meta.Versioning.Versions.Version {
		if !strings.HasPrefix(v, prefix) {
			continue
		}
		forgeVer := strings.TrimPrefix(v, prefix)
		// Відкидаємо суфікси на кшталт "-1.21.1" (старі branch-мітки).
		if idx := strings.Index(forgeVer, "-"); idx >= 0 {
			continue
		}
		out = append(out, LoaderVersionEntry{Version: forgeVer})
	}
	// Maven видає версії у порядку публікації (старі→нові) — розвертаємо,
	// щоб найновіша була першою й позначалась як рекомендована.
	reverseLoaderEntries(out)
	if len(out) > 0 {
		out[0].Recommended = true
		out[0].Stable = true
	}
	p.storeLoader(LoaderForge, mcVersion, out)
	return out, nil
}

// GetNeoForgeLoaderVersions — той самий maven-metadata.xml підхід, але
// NeoForge нумерує версії без префіксу mcVersion (формат "21.1.x" для
// Minecraft 1.21.1), тому фільтруємо за мажор.мінор гри.
func (p *VersionsProvider) GetNeoForgeLoaderVersions(mcVersion string) ([]LoaderVersionEntry, error) {
	if cached, ok := p.cachedLoader(LoaderNeoForge, mcVersion); ok {
		return cached, nil
	}
	data, err := p.get("https://maven.neoforged.net/releases/net/neoforged/neoforge/maven-metadata.xml")
	if err != nil {
		return nil, fmt.Errorf("NeoForge Maven: %w", err)
	}
	var meta mavenMetadata
	if err := xml.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	// "1.21.1" → шукаємо версії, що починаються з "21.1." (NeoForge:
	// відкидає провідний "1." від Mojang-нотації).
	parts := strings.SplitN(mcVersion, ".", 2)
	shortPrefix := mcVersion
	if len(parts) == 2 {
		shortPrefix = parts[1] + "."
	}
	out := make([]LoaderVersionEntry, 0, 16)
	for _, v := range meta.Versioning.Versions.Version {
		if strings.HasPrefix(v, shortPrefix) {
			out = append(out, LoaderVersionEntry{Version: v})
		}
	}
	reverseLoaderEntries(out)
	if len(out) > 0 {
		out[0].Recommended = true
		out[0].Stable = true
	}
	p.storeLoader(LoaderNeoForge, mcVersion, out)
	return out, nil
}

func reverseLoaderEntries(s []LoaderVersionEntry) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}

func (p *VersionsProvider) cachedLoader(l Loader, mcVersion string) ([]LoaderVersionEntry, bool) {
	entry, ok := p.loaderCache[l]
	if !ok || time.Since(entry.at) >= cacheTTL {
		return nil, false
	}
	v, ok := entry.versions[mcVersion]
	return v, ok
}

func (p *VersionsProvider) storeLoader(l Loader, mcVersion string, versions []LoaderVersionEntry) {
	entry, ok := p.loaderCache[l]
	if !ok || entry.versions == nil {
		entry = loaderCacheEntry{versions: map[string][]LoaderVersionEntry{}}
	}
	entry.versions[mcVersion] = versions
	entry.at = time.Now()
	p.loaderCache[l] = entry
}

// GetLoaderVersions — єдина точка входу для UI: обирає потрібний
// провайдер за типом лоадера. Vanilla повертає порожній список (немає
// версій лоадера — сама гра без модів).
func (p *VersionsProvider) GetLoaderVersions(loader Loader, mcVersion string) ([]LoaderVersionEntry, error) {
	switch loader {
	case LoaderFabric:
		return p.GetFabricLoaderVersions(mcVersion)
	case LoaderQuilt:
		return p.GetQuiltLoaderVersions(mcVersion)
	case LoaderForge:
		return p.GetForgeLoaderVersions(mcVersion)
	case LoaderNeoForge:
		return p.GetNeoForgeLoaderVersions(mcVersion)
	case LoaderVanilla:
		return nil, nil
	default:
		return nil, fmt.Errorf("невідомий лоадер: %s", loader)
	}
}
