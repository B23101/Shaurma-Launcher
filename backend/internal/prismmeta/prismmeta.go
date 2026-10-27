// Пакет prismmeta — клієнт мета-сервера Prism Launcher
// (https://meta.prismlauncher.org/v1/<uid>/index.json) — ТОГО САМОГО
// джерела даних, з якого Prism бере версії лоадерів для своєї форми
// створення інстансу.
//
// На відміну від сирих Maven/мета-API, Prism-meta індекс вже містить:
//   - requires: [{uid: "net.minecraft", equals: "<mc>"}] — сумісність
//     версії з конкретною версією Minecraft (для Forge/NeoForge це
//     авторитетний спосіб фільтрації, без хевристик на кшталт
//     "mcVersion-префікс" чи "короткий префікс 21.1." — саме префіксна
//     хевристика втрачала NeoForge 1.20.1, де версії нумеруються 47.1.x);
//   - recommended: true/false — прапорець рекомендованої версії (Prism
//     RecommendedRole). Якщо для MC-версії прапорець не заданий — Prism
//     обирає найновішу (VersionProxyModel::getRecommended повертає перший
//     рядок = найновіший за часом);
//   - releaseTime — час випуску (Prism сортує списки за ним, SortRole =
//     rawTime, DescendingOrder).
//
// Фільтрація за MC і вибір рекомендованої відтворюють поведінку Prism
// 1:1: CustomPage::loaderFilterChanged (exact-filter за ParentVersionRole =
// minecraftVersion) + VersionSelectWidget::selectRecommended →
// VersionProxyModel::getRecommended.
package prismmeta

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"shaurma-launcher-backend/internal/versions"
)

// UID компонентів, які цей клієнт використовує (ті самі, що в Prism:
// Component.cpp — net.minecraftforge / net.neoforged).
const (
	UIDForge    = "net.minecraftforge"
	UIDNeoForge = "net.neoforged"

	baseURL = "https://meta.prismlauncher.org/v1/"
)

// Require — один запис requires у індексі (Prism: uid + equals).
type Require struct {
	UID    string `json:"uid"`
	Equals string `json:"equals"`
}

// IndexEntry — одна версія в index.json.
type IndexEntry struct {
	Version     string    `json:"version"`
	Recommended bool      `json:"recommended"`
	ReleaseTime string    `json:"releaseTime"`
	Requires    []Require `json:"requires"`
}

type index struct {
	FormatVersion int          `json:"formatVersion"`
	Name          string       `json:"name"`
	UID           string       `json:"uid"`
	Versions      []IndexEntry `json:"versions"`
}

// Client — клієнт meta.prismlauncher.org з кешем індексів (TTL 30 хв,
// як кеш VersionsProvider — версії оновлюються рідко).
type Client struct {
	httpClient *http.Client
	base       string

	mu    sync.Mutex
	cache map[string]cachedIndex
}

type cachedIndex struct {
	entries []IndexEntry
	at      time.Time
}

const cacheTTL = 30 * time.Minute

func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 15 * time.Second},
		base:       baseURL,
		cache:      map[string]cachedIndex{},
	}
}

// Index повертає список версій компонента (uid) з meta.prismlauncher.org,
// ВПОРЯДКОВАНИЙ ЗА СПАДАННЯМ часу випуску (найновіші перші) — так само,
// як Prism VersionList::setVersions() (sort by rawTime, DescendingOrder).
func (c *Client) Index(uid string) ([]IndexEntry, error) {
	c.mu.Lock()
	if e, ok := c.cache[uid]; ok && time.Since(e.at) < cacheTTL {
		entries := make([]IndexEntry, len(e.entries))
		copy(entries, e.entries) // копія — кеш не можна мутувати ззовні
		c.mu.Unlock()
		return entries, nil
	}
	c.mu.Unlock()

	req, err := http.NewRequest(http.MethodGet, c.base+uid+"/index.json", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "ShaurmaLauncher/2.0 (+prismmeta)")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("meta.prismlauncher.org/%s: HTTP %d", uid, resp.StatusCode)
	}
	var idx index
	if err := json.NewDecoder(resp.Body).Decode(&idx); err != nil {
		return nil, fmt.Errorf("парсинг індексу %s: %w", uid, err)
	}

	SortByReleaseTimeDesc(idx.Versions)

	c.mu.Lock()
	c.cache[uid] = cachedIndex{entries: idx.Versions, at: time.Now()}
	c.mu.Unlock()

	// Повертаємо копію — кешований зріз залишається приватним для клієнта.
	out := make([]IndexEntry, len(idx.Versions))
	copy(out, idx.Versions)
	return out, nil
}

// FilterByMinecraft повертає версії, сумісні з вказаною версією Minecraft:
// requires містить {uid: "net.minecraft", equals: mcVersion}. Це точний
// аналог exact-фільтра Prism за ParentVersionRole у
// CustomPage::loaderFilterChanged. Порядок зберігається (час спадання).
func FilterByMinecraft(entries []IndexEntry, mcVersion string) []IndexEntry {
	var out []IndexEntry
	for _, e := range entries {
		for _, r := range e.Requires {
			if r.UID == "net.minecraft" && r.Equals == mcVersion {
				out = append(out, e)
				break
			}
		}
	}
	return out
}

// RecommendedVersion — порт VersionProxyModel::getRecommended(): серед
// відсортованого (час спадання) списку повертає ОСТАННІЙ рядок з
// recommended=true; якщо таких немає — перший рядок (найновіший). Саме
// так Prism обирає версію, що буде виділена у списку лоадера.
func RecommendedVersion(entries []IndexEntry) string {
	if len(entries) == 0 {
		return ""
	}
	rec := ""
	for _, e := range entries {
		if e.Recommended {
			rec = e.Version
		}
	}
	if rec == "" {
		rec = entries[0].Version
	}
	return rec
}

// SortByReleaseTimeDesc сортує за спаданням часу випуску (найновіші
// перші), як Prism VersionList::setVersions (SortRole = rawTime).
// Рівні/невалідні часи долаються FlexVer-порівнянням (новіша версія
// першою) — той самий алгоритм, що в internal/versions.
func SortByReleaseTimeDesc(entries []IndexEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		ti := parseTime(entries[i].ReleaseTime)
		tj := parseTime(entries[j].ReleaseTime)
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		return versions.Compare(entries[i].Version, entries[j].Version) > 0
	})
}

func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// IsStableVersion — чи версія стабільна (не пре-реліз). Prism-meta
// позначає пре-релізи суфіксами "-beta"/"-alpha"/"-rc" тощо; стабільність
// використовується фронтендом як підказка (LoaderVersionEntry.Stable).
func IsStableVersion(v string) bool {
	lv := strings.ToLower(v)
	for _, m := range []string{"-beta", "-alpha", "-rc", "-pre", "-snapshot", "-experiment", " pre-release"} {
		if strings.Contains(lv, m) {
			return false
		}
	}
	return true
}
