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
	"sort"
	"strings"
	"time"

	"shaurma-launcher-backend/internal/prismmeta"
	"shaurma-launcher-backend/internal/versions"
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
	Version     string `json:"version"`
	Stable      bool   `json:"stable"`
	Recommended bool   `json:"recommended"`
}

// VersionsProvider якає й кешує списки версій із зовнішніх API. Кеш —
// щоб відкриття сторінки "Нова збірка" не било в мережу щоразу (версії
// оновлюються рідко), TTL 30 хв достатньо для сесії лаунчера.
//
// ОНОВЛЕННЯ 2026-08-04: Додано валідацію існування версій через внутрішній
// кеш (перевірка перед створенням збірки, аналог Prism Launcher).
type VersionsProvider struct {
	client *http.Client
	prism  *prismmeta.Client

	mcCache     []MCVersionEntry
	mcCacheAt   time.Time
	loaderCache map[Loader]loaderCacheEntry
}

type loaderCacheEntry struct {
	versions map[string][]LoaderVersionEntry // key: mcVersion (порожньо = всі/незалежно від mcVersion)
	at       time.Time
}

const cacheTTL = 30 * time.Minute

func NewVersionsProvider() *VersionsProvider {
	return &VersionsProvider{
		client:      &http.Client{Timeout: 15 * time.Second},
		prism:       prismmeta.NewClient(),
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
// вказаною версією Minecraft. Сортування — за FlexVer (як Prism
// VersionFilterModel: найновіша перша), рекомендована = найновіша
// стабільна (як Prism VersionList::getLatestForParent + getBetterVersion:
// серед стабільних перемагає новіша; 'release' тип має пріоритет).
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
	for _, r := range raw {
		out = append(out, LoaderVersionEntry{
			Version: r.Loader.Version,
			Stable:  r.Loader.Stable,
		})
	}
	prismSortAndRecommend(out)
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
	for _, r := range raw {
		out = append(out, LoaderVersionEntry{Version: r.Loader.Version, Stable: true})
	}
	prismSortAndRecommend(out)
	p.storeLoader(LoaderQuilt, mcVersion, out)
	return out, nil
}

// ── Forge / NeoForge (Prism-meta, фолбек maven-metadata.xml) ────────────

type mavenMetadata struct {
	Versioning struct {
		Versions struct {
			Version []string `xml:"version"`
		} `xml:"versions"`
	} `xml:"versioning"`
}

// GetForgeLoaderVersions повертає версії Forge, сумісні з вказаною версією
// Minecraft. Джерело даних — мета-сервер Prism Launcher (компонент
// net.minecraftforge, requires.equals = MC-версія), те саме, з якого Prism
// будує свою форму. Фолбек на maven-metadata.xml Forge, якщо Prism-meta
// недоступний.
//
// Це ВИПРАВЛЯЄ префіксну хевристику "<mc>-<ver>": Prism-meta підтримує
// точну фільтрацію за requires і не втрачає версії з нестандартними
// суфіксами (напр. "1.21.1-1.21.1.121" — ветка, якої немає в maven-префіксі).
func (p *VersionsProvider) GetForgeLoaderVersions(mcVersion string) ([]LoaderVersionEntry, error) {
	if cached, ok := p.cachedLoader(LoaderForge, mcVersion); ok {
		return cached, nil
	}
	mcVersion = strings.TrimSpace(mcVersion)

	list, err := p.prism.Index(prismmeta.UIDForge)
	if err == nil {
		// Порожній список (не помилка!): Forge справді не підтримує цю MC.
		// Це важливо для GetAvailableLoaders — воно ховає лоадер лише при
		// гарантовано порожньому списку, а не при мережевій помилці.
		out := prismEntriesToLoaderVersions(prismmeta.FilterByMinecraft(list, mcVersion))
		p.storeLoader(LoaderForge, mcVersion, out)
		return out, nil
	}
	prismErr := err

	// Фолбек: maven-metadata.xml (старий шлях).
	out, mavenErr := p.getForgeLoaderVersionsMaven(mcVersion)
	if mavenErr != nil {
		return nil, fmt.Errorf("Forge: Prism-meta: %v; Maven: %v", prismErr, mavenErr)
	}
	p.storeLoader(LoaderForge, mcVersion, out)
	return out, nil
}

// getForgeLoaderVersionsMaven — колишній шлях через maven-metadata.xml
// Forge: фільтрація версій формату "<mcVersion>-<forgeVersion>".
func (p *VersionsProvider) getForgeLoaderVersionsMaven(mcVersion string) ([]LoaderVersionEntry, error) {
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
		out = append(out, LoaderVersionEntry{Version: forgeVer, Stable: true})
	}
	// Maven видає версії у порядку публікації (старі→нові) — сортуємо за
	// FlexVer, щоб найновіша була першою й позначалась як рекомендована
	// (Prism: усі Forge-версії 'release', рекомендується найновіша).
	prismSortAndRecommend(out)
	return out, nil
}

// GetNeoForgeLoaderVersions повертає версії NeoForge для вказаної версії
// Minecraft з мета-сервера Prism (компонент net.neoforged). Це виправляє
// критичний баг префіксної хевристики: NeoForge для 1.20.1 нумерується
// 47.1.x, і старе правило "шукати 20.1.*" повертало ПОРОЖНІЙ список —
// тому NeoForge просто не встановлювався на 1.20.1. Prism-meta вирішує
// сумісність через requires.equals.
//
// Фолбек на maven-metadata.xml NeoForge, якщо Prism-meta недоступний.
func (p *VersionsProvider) GetNeoForgeLoaderVersions(mcVersion string) ([]LoaderVersionEntry, error) {
	if cached, ok := p.cachedLoader(LoaderNeoForge, mcVersion); ok {
		return cached, nil
	}
	mcVersion = strings.TrimSpace(mcVersion)

	list, err := p.prism.Index(prismmeta.UIDNeoForge)
	if err == nil {
		// Порожній список (не помилка!): NeoForge справді не підтримує цю
		// MC — GetAvailableLoaders ховає лоадер саме за цією ознакою.
		out := prismEntriesToLoaderVersions(prismmeta.FilterByMinecraft(list, mcVersion))
		p.storeLoader(LoaderNeoForge, mcVersion, out)
		return out, nil
	}
	prismErr := err

	out, mavenErr := p.getNeoForgeLoaderVersionsMaven(mcVersion)
	if mavenErr != nil {
		return nil, fmt.Errorf("NeoForge: Prism-meta: %v; Maven: %v", prismErr, mavenErr)
	}
	p.storeLoader(LoaderNeoForge, mcVersion, out)
	return out, nil
}

// getNeoForgeLoaderVersionsMaven — колишній шлях через maven-metadata.xml
// NeoForge. У NeoForge ДВІ лінійки публікації:
//
//	· 1.20.1 (47.x) — артефакт "forge" з mc-префіксованими версіями
//	  (1.20.1-47.1.106): net/neoforged/forge/maven-metadata.xml;
//	· 1.20.2+ (20.2.x/21.x) — артефакт "neoforge" без префікса
//	  (21.1.95): net/neoforged/neoforge/maven-metadata.xml.
//
// Версія в обох випадках повертається БЕЗ mc-префікса (47.1.106) — так
// само, як її віддає Prism-meta (основний шлях), щоб дані збірки не
// залежали від джерела списку.
func (p *VersionsProvider) getNeoForgeLoaderVersionsMaven(mcVersion string) ([]LoaderVersionEntry, error) {
	mcVersion = strings.TrimSpace(mcVersion)
	// NeoForge 1.20.1 (версії 47.x) — єдина версія MC, чиї NeoForge-версії
	// живуть під артефактом "forge" з mc-префіксом (1.20.1-47.1.106).
	legacy := mcVersion == "1.20.1"

	metaURL := "https://maven.neoforged.net/releases/net/neoforged/neoforge/maven-metadata.xml"
	prefix := mcVersion
	parts := strings.SplitN(mcVersion, ".", 2)
	if len(parts) == 2 {
		prefix = parts[1] + "." // "1.21.1" → "21.1." (NeoForge відкидає "1.")
	}
	if legacy {
		// NeoForge 1.20.1 живе під артефактом "forge" з mc-префіксом версії.
		metaURL = "https://maven.neoforged.net/releases/net/neoforged/forge/maven-metadata.xml"
		prefix = mcVersion + "-"
	}

	data, err := p.get(metaURL)
	if err != nil {
		return nil, fmt.Errorf("NeoForge Maven: %w", err)
	}
	var meta mavenMetadata
	if err := xml.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	out := make([]LoaderVersionEntry, 0, 16)
	for _, v := range meta.Versioning.Versions.Version {
		if !strings.HasPrefix(v, prefix) {
			continue
		}
		// Прибираємо mc-префікс у forge-артефакті (1.20.1-47.1.106 → 47.1.106),
		// щоб список був ідентичний Prism-meta формату.
		ver := strings.TrimPrefix(v, prefix)
		if ver == "" {
			continue
		}
		out = append(out, LoaderVersionEntry{Version: ver, Stable: true})
	}
	prismSortAndRecommend(out)
	return out, nil
}

// prismEntriesToLoaderVersions конвертує відсортований список Prism-meta
// у LoaderVersionEntry. recommended виставляється за RecommendedVersion
// (порт VersionProxyModel::getRecommended: прапорець recommended, інакше
// найновіша). Список ВЖЕ відсортований за часом випуску спаданням —
// найновіша перша, як Prism VersionSelectWidget.
func prismEntriesToLoaderVersions(entries []prismmeta.IndexEntry) []LoaderVersionEntry {
	out := make([]LoaderVersionEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, LoaderVersionEntry{
			Version: e.Version,
			Stable:  prismmeta.IsStableVersion(e.Version),
		})
	}
	rec := prismmeta.RecommendedVersion(entries)
	for i := range out {
		out[i].Recommended = out[i].Version == rec
	}
	return out
}

// prismSortAndRecommend — порт логіки Prism для списку версій лоадера:
//
//  1. SortDesc — FlexVer-сортування за спаданням (найновіші перші), те
//     саме, що VersionFilterModel у Prism (SortRole, DescendingOrder).
//  2. Recommended — порт VersionList::getLatestForParent() + getBetterVersion():
//     серед стабільних ('release' тип) перемагає НАЙНОВІША; якщо стабільних
//     немає — найновіша загалом. Далі RecommendedRole=true на цій версії,
//     щоб фронтенд показав "(рекомендовано)", як Prism показує зірку.
func prismSortAndRecommend(list []LoaderVersionEntry) {
	sort.SliceStable(list, func(i, j int) bool {
		return versions.Compare(list[i].Version, list[j].Version) > 0
	})
	rec := pickRecommendedLoaderVersion(list)
	for i := range list {
		list[i].Recommended = list[i].Version == rec
	}
}

// pickRecommendedLoaderVersion — порт Prism getBetterVersion(): серед
// сумісних версій перемагає найновіша стабільна; якщо стабільних немає —
// найновіша загалом. Порівняння за FlexVer.
func pickRecommendedLoaderVersion(list []LoaderVersionEntry) string {
	best := ""
	bestStable := false
	for _, e := range list {
		if e.Version == "" {
			continue
		}
		if best == "" {
			best, bestStable = e.Version, e.Stable
			continue
		}
		if e.Stable == bestStable {
			if versions.Compare(e.Version, best) > 0 {
				best = e.Version
			}
		} else if e.Stable {
			// 'release' тип має пріоритет над 'snapshot'/'beta'
			best, bestStable = e.Version, true
		}
	}
	return best
}

// GetAvailableLoaders повертає лоадери, для яких існують версії під
// вказану версію Minecraft — порт поведінки CustomPage::loaderFilterChanged()
// з Prism:
//
//   - Fabric/Quilt: підтримуються лише з Minecraft 1.14 (хардкод-правило
//     Prism: Version(minecraftVersion) >= Version("1.14"));
//   - Forge/NeoForge: лише якщо у Maven є версії під цю MC-версію;
//   - Vanilla доступна завжди.
//
// Мережева помилка отримання списку НЕ ховає лоадер (невідомо ≠ "версій
// немає") — ховаємо лише при гарантовано порожньому списку.
func (p *VersionsProvider) GetAvailableLoaders(mcVersion string) []Loader {
	available := []Loader{LoaderVanilla}
	candidates := []Loader{LoaderFabric, LoaderQuilt, LoaderForge, LoaderNeoForge}
	for _, l := range candidates {
		// Prism: Fabric/Quilt працюють лише з MC 1.14+ (CustomPage.cpp:
		// "FIXME: dirty hack because the launcher is unaware of Fabric's
		// dependencies").
		if (l == LoaderFabric || l == LoaderQuilt) && versions.Compare(mcVersion, "1.14") < 0 {
			continue
		}
		list, err := p.GetLoaderVersions(l, mcVersion)
		if err != nil {
			// Мережева помилка ≠ "версій немає" — лоадер лишаємо доступним
			// (невідомо), ховаємо лише при гарантовано порожньому списку.
			available = append(available, l)
			continue
		}
		if len(list) > 0 {
			available = append(available, l)
		}
	}
	return available
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

// ValidateLoaderVersion перевіряє, чи існує вказана версія лоадера для
// вказаної версії Minecraft у закешованих даних. Це швидка локальна
// перевірка перед створенням збірки (UI-рівень валідації).
//
// Використовується для перевірки введених користувачем даних (напр. при
// імпорті модпаку з застарілою версією лоадера). Якщо версія не знайдена —
// повертається список доступних версій для заміни.
func (p *VersionsProvider) ValidateLoaderVersion(loader Loader, mcVersion, loaderVersion string) (bool, []string, error) {
	if loader == LoaderVanilla || loaderVersion == "" || loaderVersion == "recommended" || loaderVersion == "latest" {
		return true, nil, nil
	}

	// Завантажуємо список версій (з кешу або API)
	versions, err := p.GetLoaderVersions(loader, mcVersion)
	if err != nil {
		return false, nil, fmt.Errorf("не вдалося отримати список версій: %w", err)
	}

	if len(versions) == 0 {
		return false, nil, fmt.Errorf("%s не підтримує Minecraft %s", loader, mcVersion)
	}

	// Перевіряємо існування версії
	for _, v := range versions {
		if v.Version == loaderVersion {
			return true, nil, nil
		}
	}

	// Версія не знайдена — повертаємо список доступних (перші 10)
	available := make([]string, 0, 10)
	for i, v := range versions {
		if i >= 10 {
			break
		}
		if v.Recommended {
			available = append(available, v.Version+" (рекомендовано)")
		} else {
			available = append(available, v.Version)
		}
	}

	return false, available, nil
}
