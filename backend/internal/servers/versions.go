// versions.go — версії Minecraft і версії ядра/лоадера для UI створення
// сервера. Для vanilla/fabric/forge/neoforge/quilt перевикористовує ТОЙ
// САМИЙ custompack.VersionsProvider, яким живиться "Нова збірка → Вручну"
// (щоб не тримати два незалежні джерела правди й два кеші для однієї й
// тієї ж інформації) — Paper/Purpur/Velocity додаються тут окремо, бо їх
// немає серед клієнтських лоадерів збірок.
package servers

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"shaurma-launcher-backend/internal/custompack"
	"shaurma-launcher-backend/internal/versions"
)

// ServerCoreVersionEntry — одна версія ядра сервера для селектора (Paper
// build, Purpur build, версія Forge/Fabric тощо) — дзеркалить
// custompack.LoaderVersionEntry, щоб фронтенд використовував ту саму форму
// даних, що й SelectField у створенні збірки.
type ServerCoreVersionEntry struct {
	Version     string `json:"version"`
	Stable      bool   `json:"stable"`
	Recommended bool   `json:"recommended"`
}

// GetServerMCVersions повертає версії Minecraft для селектора "Версія
// гри" у створенні сервера — той самий список і те саме кешування, що й
// у створенні збірки.
func (a *API) GetServerMCVersions() ([]custompack.MCVersionEntry, error) {
	if a.versions == nil {
		return nil, fmt.Errorf("провайдер версій недоступний")
	}
	return a.versions.GetMCVersions()
}

// GetServerDefaultMCVersion — поточна стабільна версія Minecraft (дефолт
// селектора при відкритті сторінки створення сервера).
func (a *API) GetServerDefaultMCVersion() (string, error) {
	if a.versions == nil {
		return "", fmt.Errorf("провайдер версій недоступний")
	}
	return a.versions.LatestReleaseMCVersion()
}

// GetServerCoreVersions повертає версії конкретного ядра, сумісні з
// обраною версією Minecraft, найновіші/рекомендовані першими:
//
//	vanilla|velocity  — версій немає (порожній список, поле не показується)
//	fabric|forge|neoforge|quilt — делегується в custompack.VersionsProvider
//	paper|purpur      — власні API (fill.papermc.io / purpurmc.org)
func (a *API) GetServerCoreVersions(core, mcVersion string) ([]ServerCoreVersionEntry, error) {
	switch core {
	case CoreVanilla:
		return nil, nil
	case CoreVelocity:
		// Velocity — це проксі, який НЕ прив'язаний до версії Minecraft:
		// версії, які показуємо в селекторі, — це версії самого проксі
		// (з api.papermc.io/v2/projects/velocity), а не "версія гри".
		// (Раніше тут повертався nil і користувач не міг обрати версію
		// проксі — install мовчки брав останню. Тепер UI може показати
		// повний список і користувач обирає явно, як у збірках.)
		return getVelocityVersions()
	case CorePaper:
		return getPaperBuilds(mcVersion)
	case CorePurpur:
		return getPurpurVersions(mcVersion)
	case CoreFabric, CoreForge, CoreNeoForge, CoreQuilt:
		if a.versions == nil {
			return nil, fmt.Errorf("провайдер версій недоступний")
		}
		list, err := a.versions.GetLoaderVersions(custompack.Loader(core), mcVersion)
		if err != nil {
			return nil, err
		}
		out := make([]ServerCoreVersionEntry, 0, len(list))
		for _, v := range list {
			out = append(out, ServerCoreVersionEntry{Version: v.Version, Stable: v.Stable, Recommended: v.Recommended})
		}
		return out, nil
	default:
		return nil, fmt.Errorf("невідоме ядро: %s", core)
	}
}

// GetServerAvailableCores повертає ядра, доступні для обраної версії
// Minecraft (Vanilla/Paper/Purpur/Velocity доступні завжди — вони не
// прив'язані до реєстру лоадерів збірок; Fabric/Forge/NeoForge/Quilt
// ховаються, якщо під цю MC немає жодної версії — той самий принцип, що
// GetAvailableLoaders для збірок).
//
// СПОЧАТКУ перевіряється статична межа мінімальної підтримуваної версії
// (minCoreMCVersion) — детермінований факт про сам лоадер, який не
// залежить від мережі. Це головне джерело правди: мережевий запит
// (custompack.GetAvailableLoaders) трактує СВОЮ помилку/таймаут як "лоадер
// лишається доступним" (щоб не ховати опцію через нестабільний інтернет),
// тому мережа сама по собі не може безпечно ВІДХИЛИТИ лоадер — лише
// статична межа може. Обидві перевірки застосовуються: лоадер показується,
// лише якщо він проходить і статичну межу, і (за наявності мережі) реальний
// список версій.
func (a *API) GetServerAvailableCores(mcVersion string) []string {
	available := []string{CoreVanilla, CorePaper, CorePurpur, CoreVelocity}
	candidates := []string{CoreFabric, CoreForge, CoreNeoForge, CoreQuilt}

	// Крок 1: статичний фільтр — межа, яку сам лоадер ніколи не перетне,
	// незалежно від мережі (Fabric/Quilt: пре-1.14 intermediary-мапінгів не
	// існує в природі; форк-лоадери мають свої історичні межі).
	staticOK := make([]string, 0, len(candidates))
	for _, c := range candidates {
		if mcVersion == "" || minCoreMCVersionOK(c, mcVersion) {
			staticOK = append(staticOK, c)
		}
	}
	if a.versions == nil || mcVersion == "" {
		return append(available, staticOK...)
	}

	// Крок 2: мережева звірка — звужує staticOK (не розширює): якщо
	// GetLoaderVersions дає ПОРОЖНІЙ список (а не помилку), це остаточне
	// підтвердження несумісності навіть у межах статичного порогу
	// (наприклад форк випустив підтримку із затримкою після релізу MC).
	networkAvail := map[string]bool{}
	for _, l := range a.versions.GetAvailableLoaders(mcVersion) {
		networkAvail[string(l)] = true
	}
	for _, c := range staticOK {
		if networkAvail[c] {
			available = append(available, c)
		}
	}
	return available
}

// minCoreMCVersionOK — статична, offline-детермінована перевірка: чи
// теоретично МІГ БИ цей лоадер підтримувати вказану версію Minecraft.
// Це факти про сам лоадер (коли з'явився, з якої версії MC працює), а не
// мережевий здогад — тому лишається коректним навіть без інтернету і не
// залежить від того, як конкретний meta-API відповідає на неіснуючу версію.
func minCoreMCVersionOK(core, mcVersion string) bool {
	var minVersion string
	switch core {
	case CoreFabric, CoreQuilt:
		minVersion = "1.14" // Fabric/Quilt intermediary-мапінги існують лише з 1.14
	case CoreNeoForge:
		minVersion = "1.20.1" // NeoForge — форк Forge, з'явився після розколу в 1.20.1
	case CoreForge:
		minVersion = "1.1" // Forge (Minecraft Forge) існує з дуже ранніх версій
	default:
		return true
	}
	return versions.Compare(mcVersion, minVersion) >= 0
}

// ── Velocity (fill.papermc.io v3) ───────────────────────────────────────
//
// PaperMC закрив свій старий v2 API (api.papermc.io/v2 тепер віддає
// 410 Gone) — увесь новий код ходить у v3 (fill.papermc.io). Velocity тут
// описується з java-вимогою кожної версії: у v3-відповіді є
// java.version.minimum (4.x потребує Java 25, 3.5.x — Java 21), і це
// єдине надійне джерело правди для автовибору Java проксі.

// velocityCandidate — одна версія Velocity з даними, потрібними для
// селектора UI, install-дефолту та автовибору Java.
type velocityCandidate struct {
	Version   string // "4.1.1" або "4.1.1-SNAPSHOT"
	JavaMajor int    // мінімальна Java з API (25/21)
	Builds    int    // кількість опублікованих білдів (0 = пусто)
	Supported bool   // status == "SUPPORTED"
}

var velocityCandMu sync.Mutex
var velocityCandCache []velocityCandidate
var velocityCandAt time.Time

// velocityCandidates повертає версії Velocity з fill.papermc.io v3,
// найновіші першими (так віддає API). Кешується на cacheTTLServers.
func velocityCandidates() ([]velocityCandidate, error) {
	velocityCandMu.Lock()
	defer velocityCandMu.Unlock()
	if len(velocityCandCache) > 0 && time.Since(velocityCandAt) < cacheTTLServers {
		return velocityCandCache, nil
	}

	type fillResp struct {
		Versions []struct {
			Version struct {
				ID   string `json:"id"`
				Java struct {
					Version struct {
						Minimum int `json:"minimum"`
					} `json:"version"`
				} `json:"java"`
			} `json:"version"`
			Support struct {
				Status string `json:"status"`
			} `json:"support"`
			Builds []int `json:"builds"`
		} `json:"versions"`
	}
	var r fillResp
	if err := fetchJSONWithUAReal("https://fill.papermc.io/v3/projects/velocity/versions", &r); err != nil {
		return nil, err
	}

	out := make([]velocityCandidate, 0, len(r.Versions))
	for _, v := range r.Versions {
		if v.Version.ID == "" {
			continue
		}
		jm := v.Version.Java.Version.Minimum
		if jm == 0 {
			jm = 21 // фолбек: невідомо — консервативний мінімум Velocity 3.x
		}
		out = append(out, velocityCandidate{
			Version:   v.Version.ID,
			JavaMajor: jm,
			Builds:    len(v.Builds),
			Supported: v.Support.Status == "SUPPORTED",
		})
	}
	velocityCandCache = out
	velocityCandAt = time.Now()
	return out, nil
}

// velocityStable — чи це стабільний реліз (не -SNAPSHOT) з білдами.
func (c velocityCandidate) velocityStable() bool {
	return !strings.HasSuffix(c.Version, "-SNAPSHOT") && c.Builds > 0
}

// velocityRecommended повертає найкращу версію за замовчуванням:
// найновіша підтримувана стабільна з білдами (4.1.1), якщо такої немає —
// найновіша стабільна, далі — просто найновіша. (cands йдуть від нових до
// старих, тому перший збіг і є найновішим.)
func velocityRecommended(cands []velocityCandidate) (velocityCandidate, bool) {
	for _, c := range cands {
		if c.velocityStable() && c.Supported {
			return c, true
		}
	}
	for _, c := range cands {
		if c.velocityStable() {
			return c, true
		}
	}
	if len(cands) > 0 {
		return cands[0], true
	}
	return velocityCandidate{}, false
}

// velocityJavaForVersion повертає мінімальну Java для конкретної версії
// проксі (за кешем), або Java 25 (вимога поточної 4.x) як безпечний
// дефолт — Java 25 запускає і 3.x (мінімум 21), і 4.x (мінімум 25).
func velocityJavaForVersion(loaderVer string) int {
	if loaderVer != "" {
		if cands, err := velocityCandidates(); err == nil {
			for _, c := range cands {
				if c.Version == loaderVer {
					return c.JavaMajor
				}
			}
		}
	}
	return 25
}

// getVelocityVersions повертає версії Velocity для селектора "Новий
// сервер → Проксі": версії САМОГО проксі ("4.1.1" тощо), не версії
// Minecraft — проксі байдуже, яка версія гри.
func getVelocityVersions() ([]ServerCoreVersionEntry, error) {
	cands, err := velocityCandidates()
	if err != nil {
		return nil, err
	}
	rec, _ := velocityRecommended(cands)
	out := make([]ServerCoreVersionEntry, 0, len(cands))
	for _, c := range cands {
		if c.Builds == 0 {
			continue // немає жодного білда — не показуємо в селекторі
		}
		out = append(out, ServerCoreVersionEntry{
			Version:     c.Version,
			Stable:      c.velocityStable(),
			Recommended: c.Version == rec.Version,
		})
	}
	return out, nil
}

// ── Paper (fill.papermc.io v3) ──────────────────────────────────────────

var paperCache = struct {
	mu        sync.Mutex
	byVersion map[string]paperCacheEntry
}{byVersion: map[string]paperCacheEntry{}}

type paperCacheEntry struct {
	builds []ServerCoreVersionEntry
	at     time.Time
}

func getPaperBuilds(mcVersion string) ([]ServerCoreVersionEntry, error) {
	if mcVersion == "" {
		return nil, nil
	}
	paperCache.mu.Lock()
	if e, ok := paperCache.byVersion[mcVersion]; ok && time.Since(e.at) < cacheTTLServers {
		paperCache.mu.Unlock()
		return e.builds, nil
	}
	paperCache.mu.Unlock()

	type paperBuild struct {
		ID      int    `json:"id"`
		Channel string `json:"channel"`
	}
	url := fmt.Sprintf("https://fill.papermc.io/v3/projects/paper/versions/%s/builds", mcVersion)
	var builds []paperBuild
	if err := fetchJSONWithUAReal(url, &builds); err != nil {
		return nil, err
	}

	// Найновіший білд першим; STABLE позначається рекомендованим.
	sort.Slice(builds, func(i, j int) bool { return builds[i].ID > builds[j].ID })
	out := make([]ServerCoreVersionEntry, 0, len(builds))
	recommendedSet := false
	for _, b := range builds {
		isStable := b.Channel == "STABLE"
		out = append(out, ServerCoreVersionEntry{
			Version:     fmt.Sprintf("%d", b.ID),
			Stable:      isStable,
			Recommended: isStable && !recommendedSet,
		})
		if isStable {
			recommendedSet = true
		}
	}
	paperCache.mu.Lock()
	paperCache.byVersion[mcVersion] = paperCacheEntry{builds: out, at: time.Now()}
	paperCache.mu.Unlock()
	return out, nil
}

// ── Purpur (api.purpurmc.org v2) ────────────────────────────────────────

var purpurCache = struct {
	mu        sync.Mutex
	byVersion map[string]paperCacheEntry
}{byVersion: map[string]paperCacheEntry{}}

func getPurpurVersions(mcVersion string) ([]ServerCoreVersionEntry, error) {
	if mcVersion == "" {
		return getPurpurMCVersions()
	}
	purpurCache.mu.Lock()
	if e, ok := purpurCache.byVersion[mcVersion]; ok && time.Since(e.at) < cacheTTLServers {
		purpurCache.mu.Unlock()
		return e.builds, nil
	}
	purpurCache.mu.Unlock()

	type purpurVersionInfo struct {
		Builds struct {
			Latest string   `json:"latest"`
			All    []string `json:"all"`
		} `json:"builds"`
	}
	url := fmt.Sprintf("https://api.purpurmc.org/v2/purpur/%s", mcVersion)
	var info purpurVersionInfo
	if err := fetchJSONWithUAReal(url, &info); err != nil {
		return nil, err
	}

	out := make([]ServerCoreVersionEntry, 0, len(info.Builds.All))
	// All йде від найстарішого до найновішого (Purpur API) — розвертаємо,
	// щоб найновіший білд був першим, як в решти селекторів.
	for i := len(info.Builds.All) - 1; i >= 0; i-- {
		b := info.Builds.All[i]
		out = append(out, ServerCoreVersionEntry{
			Version:     b,
			Stable:      true,
			Recommended: b == info.Builds.Latest,
		})
	}
	purpurCache.mu.Lock()
	purpurCache.byVersion[mcVersion] = paperCacheEntry{builds: out, at: time.Now()}
	purpurCache.mu.Unlock()
	return out, nil
}

// getPurpurMCVersions — версії Minecraft, для яких Purpur взагалі має
// білди (використовується лише для GetServerAvailableCores, якщо колись
// знадобиться фільтрація; наразі Purpur завжди в списку доступних).
func getPurpurMCVersions() ([]ServerCoreVersionEntry, error) {
	type purpurProject struct {
		Versions []string `json:"versions"`
	}
	var proj purpurProject
	if err := fetchJSONWithUAReal("https://api.purpurmc.org/v2/purpur", &proj); err != nil {
		return nil, err
	}
	out := make([]ServerCoreVersionEntry, 0, len(proj.Versions))
	for i := len(proj.Versions) - 1; i >= 0; i-- {
		out = append(out, ServerCoreVersionEntry{Version: proj.Versions[i]})
	}
	return out, nil
}

const cacheTTLServers = 30 * time.Minute
