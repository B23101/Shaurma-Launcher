package main

// ── Динамічний банер головного екрана («останнє офіційне оновлення») ────
//
// Як це працює:
//
//  1. version_manifest_v2.json (Mojang) → latest.release (напр. «26.3»).
//  2. launchercontent.mojang.com/news.json → шукаємо в entries новину, у
//     title якої згадується номер цієї версії.
//  3. Витягуємо playPageImage.url (а якщо його немає — newsPageImage.url;
//     відносний шлях додаємо до домену).
//     ЗАПАСНИЙ ШЛЯХ: якщо в news.json немає запису з картинкою для цієї
//     версії (Mojang часто називає новини за назвою дропу, а не за номером,
//     напр. «Wilderness Bound», а не «26.3») — беремо першу картинку каруселі
//     зі сторінки minecraft.net/en-us/updates/minecraft-updates-timeline-and-evolution:
//     там картинки найновішого дропу завжди зверху.
//  4. Качаємо картинку в кеш <CacheDir>/banners/ — якщо файл із хешем
//     URL уже лежить на диску, повторно НЕ качається.
//  5. App.GetNewsBanner повертає фронтенду картинку як data: URL (той самий
//     контракт, що GetPackAsset для іконок/фонів збірок: WebView не може
//     читати довільні local-шляхи, а data: URL працює завжди).
//
// Фронтенд викликає GetNewsBanner у onMount компонента NewsBanner.svelte
// (головний екран «Мої збірки»), а бекенд ДОДАТКОВО сам перевіряє банер
// один раз при старті лаунчера (ensureNewsBannerAtStartup, виклик з
// ServiceStartup — той самий патерн, що ensureAutoVersionsAtStartup для
// авто-збірок) і шле подію news:banner, коли картинка готова/оновилась.
// Так банер з'являється навіть без дій користувача і підхоплює нову
// версію гри відразу після виходу офіційного оновлення.

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"shaurma-launcher-backend/internal/crashguard"
	"strings"
	"sync"
	"time"
)

const (
	// Той самий маніфест, яким користуються авто-збірки (autopacks.go),
	// лише версію беремо саме з latest.release.
	newsBannerManifestURL = "https://launchermeta.mojang.com/mc/game/version_manifest_v2.json"
	// Офіційний контент-канал лаунчера Minecraft (новини, патчноути).
	newsBannerNewsURL = "https://launchercontent.mojang.com/news.json"
	// Домен для відносних шляхів у playPageImage.url.
	newsBannerContentBase = "https://launchercontent.mojang.com"
	// Запасне джерело: офіційна сторінка з історією оновлень. Mojang не завжди
	// кладе в news.json запис із номером версії («26.3») у заголовку, а от
	// timeline на minecraft.net завжди містить картинки найновішого дропу.
	newsBannerTimelineURL = "https://www.minecraft.net/en-us/updates/minecraft-updates-timeline-and-evolution"
	newsBannerSiteBase    = "https://www.minecraft.net"
	// minecraft.net інколи віддає 403 нестандартному User-Agent — для
	// запитів до сайту (сторінка + картинки з /content/dam/) представляємось
	// звичайним браузером.
	newsBannerBrowserUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36"
	// Кеш картинки живе в <CacheDir>/banners/ (шлях дає config.Manager).
	newsBannerDirName = "banners"
	// TTL мережевих даних у пам'яті: щоб фоновий виклик при старті і
	// onMount-виклик фронтенду не смикали news.json двічі підряд.
	newsBannerTTL = 4 * time.Hour
	// Ліміт розміру відповіді: скріншоти новин важать кілька МБ, ліміт
	// захищає від аномально великих/битих відповідей.
	newsBannerMaxBytes = 25 << 20 // 25 MiB
	// Таймаут усіх мережевих запитів фічі.
	newsBannerHTTPTimeout = 20 * time.Second
)

// newsBannerState — спільний стан фічі. refreshMu серіалізує ПОВНІ цикли
// оновлення (фоновий стартовий і фронтендний виклики не качають feed
// паралельно — другий дочекается і забере готовий результат), mu захищає
// поля, які читаються швидким шляхом без повного циклу.
var newsBannerState struct {
	refreshMu sync.Mutex
	mu        sync.Mutex
	at        time.Time
	imageURL  string // абсолютний URL картинки останнього знайденого банера
	cachePath string // шлях до неї в локальному кеші
}

// ── JSON-структури відповідей Mojang ────────────────────────────────────

// newsBannerManifest — тільки те, що читаємо з version_manifest_v2.json.
type newsBannerManifest struct {
	Latest struct {
		Release string `json:"release"`
	} `json:"latest"`
}

// newsEntryImage — обгортка поля playPageImage (нас цікавить лише url).
type newsEntryImage struct {
	URL string `json:"url"`
}

// newsEntry — один запис масиву entries з news.json. Нас цікавлять лише
// title (пошук за номером версії) та playPageImage.url.
type newsEntry struct {
	Title         string          `json:"title"`
	PlayPageImage *newsEntryImage `json:"playPageImage"`
	NewsPageImage *newsEntryImage `json:"newsPageImage"`
}

// imageURL повертає першу непорожню картинку запису: playPageImage
// (банер лаунчера), а якщо її немає — newsPageImage (картинка статті).
func (e *newsEntry) imageURL() string {
	if e == nil {
		return ""
	}
	if e.PlayPageImage != nil && strings.TrimSpace(e.PlayPageImage.URL) != "" {
		return e.PlayPageImage.URL
	}
	if e.NewsPageImage != nil && strings.TrimSpace(e.NewsPageImage.URL) != "" {
		return e.NewsPageImage.URL
	}
	return ""
}

// newsFeed — верхньорівнева структура news.json.
type newsFeed struct {
	Entries []newsEntry `json:"entries"`
}

// ── Мережеві хелпери ────────────────────────────────────────────────────

// newsBannerGet виконує GET із User-Agent (piston-meta/launchercontent
// часом віддають 403 без нього) і повертає тіло відповіді.
func newsBannerGet(url string) ([]byte, error) {
	return newsBannerGetUA(url, "shaurma-team/shaurma-launcher/2.0 (news-banner)")
}

// newsBannerGetUA — те саме, але з довільним User-Agent.
func newsBannerGetUA(url, userAgent string) ([]byte, error) {
	client := &http.Client{Timeout: newsBannerHTTPTimeout}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d від %s", resp.StatusCode, url)
	}
	return io.ReadAll(io.LimitReader(resp.Body, newsBannerMaxBytes))
}

// fetchLatestRelease повертає latest.release з Mojang-маніфесту.
func fetchLatestRelease() (string, error) {
	data, err := newsBannerGet(newsBannerManifestURL)
	if err != nil {
		return "", err
	}
	var m newsBannerManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return "", err
	}
	if strings.TrimSpace(m.Latest.Release) == "" {
		return "", fmt.Errorf("у Mojang-маніфесті порожній latest.release")
	}
	return m.Latest.Release, nil
}

// findNewsForVersion шукає в news.json новину, у title якої згадується
// номер версії І є картинка (перший збіг перемагає — feed відсортований від
// найновіших). Записи без картинки пропускаємо: раніше перший збіг без
// playPageImage ламав пошук, хоча далі міг бути придатний запис.
func findNewsForVersion(feed *newsFeed, version string) *newsEntry {
	if feed == nil || version == "" {
		return nil
	}
	for i := range feed.Entries {
		if newsTitleMentions(feed.Entries[i].Title, version) && feed.Entries[i].imageURL() != "" {
			return &feed.Entries[i]
		}
	}
	return nil
}

// newsTitleMentions — чи згадується номер версії в заголовку новини.
// Порівнюємо входження ПО СЛОВНІЙ МЕЖІ: «26.3» має знайтись у
// «Minecraft Java 26.3 вийшла!», але НЕ має ловитись на «26.35» чи
// «1.26.35» — тому простий strings.Contains недостатній.
func newsTitleMentions(title, version string) bool {
	if title == "" || version == "" {
		return false
	}
	idx := 0
	for {
		pos := strings.Index(title[idx:], version)
		if pos < 0 {
			return false
		}
		pos += idx
		before := byte(' ') // початок/кінець рядка вважаємо словесною межею
		if pos > 0 {
			before = title[pos-1]
		}
		after := byte(' ')
		if pos+len(version) < len(title) {
			after = title[pos+len(version)]
		}
		// Сусіди не цифра і не крапка (інакше «26.3» матчилось би всередині
		// «26.35» або «1.26.3»); літери теж виключаємо — на випадок схем
		// типу «26.3a».
		if !isVersionNeighborByte(before) && !isVersionNeighborByte(after) {
			return true
		}
		idx = pos + 1
		if idx >= len(title) {
			return false
		}
	}
}

func isVersionNeighborByte(b byte) bool {
	return (b >= '0' && b <= '9') || b == '.'
}

// resolveNewsImageURL перетворює playPageImage.url на абсолютний URL:
// відносні шляхи доповнюються доменом контент-сервера Mojang.
func resolveNewsImageURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw
	}
	if !strings.HasPrefix(raw, "/") {
		raw = "/" + raw
	}
	return newsBannerContentBase + raw
}

// ── Запасне джерело: timeline на minecraft.net ──────────────────────────

// newsTimelineImageRe — картинки каруселі timeline. На сторінці вони йдуть
// від найновішого дропу до найстаршого, тож ПЕРШИЙ збіг = актуальне
// оновлення (напр. MC-Updates_Carousel-N-0_2026_FGD-26_01_1000x563.png).
var newsTimelineImageRe = regexp.MustCompile(
	`(?i)(?:https?://www\.minecraft\.net)?/content/dam/minecraftnet/games/minecraft/[A-Za-z0-9_\-./%]*MC-Updates_Carousel[A-Za-z0-9_\-./%]*\.(?:png|jpe?g|webp)`)

// parseTimelineBannerURL дістає з HTML сторінки timeline першу картинку
// каруселі й повертає її абсолютний URL ("" — не знайдено).
func parseTimelineBannerURL(html []byte) string {
	m := newsTimelineImageRe.Find(html)
	if m == nil {
		return ""
	}
	return resolveTimelineImageURL(string(m))
}

func resolveTimelineImageURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw
	}
	if !strings.HasPrefix(raw, "/") {
		raw = "/" + raw
	}
	return newsBannerSiteBase + raw
}

// fetchTimelineBannerURL качає сторінку timeline і повертає URL картинки
// найновішого оновлення гри.
func fetchTimelineBannerURL() (string, error) {
	data, err := newsBannerGetUA(newsBannerTimelineURL, newsBannerBrowserUA)
	if err != nil {
		return "", fmt.Errorf("minecraft.net timeline: %w", err)
	}
	u := parseTimelineBannerURL(data)
	if u == "" {
		return "", fmt.Errorf("minecraft.net timeline: на сторінці немає картинок каруселі")
	}
	return u, nil
}

// newsImageFromFeed — основний шлях: news.json → новина з номером версії.
func newsImageFromFeed(version string) (string, error) {
	data, err := newsBannerGet(newsBannerNewsURL)
	if err != nil {
		return "", fmt.Errorf("Mojang news: %w", err)
	}
	var feed newsFeed
	if err := json.Unmarshal(data, &feed); err != nil {
		return "", fmt.Errorf("Mojang news: %w", err)
	}
	entry := findNewsForVersion(&feed, version)
	if entry == nil {
		return "", fmt.Errorf("у новинах Mojang немає запису з картинкою для версії %s", version)
	}
	u := resolveNewsImageURL(entry.imageURL())
	if u == "" {
		return "", fmt.Errorf("у новини %q порожній url картинки", entry.Title)
	}
	return u, nil
}

// resolveNewsBannerImageURL шукає URL картинки банера: спершу news.json
// (за номером версії), якщо там порожньо — timeline на minecraft.net.
func resolveNewsBannerImageURL() (string, error) {
	var reasons []string
	if version, err := fetchLatestRelease(); err != nil {
		reasons = append(reasons, fmt.Sprintf("Mojang-маніфест: %v", err))
	} else if u, err := newsImageFromFeed(version); err == nil {
		return u, nil
	} else {
		reasons = append(reasons, err.Error())
	}
	u, err := fetchTimelineBannerURL()
	if err == nil {
		log.Printf("newsBanner: %s → беру картинку з timeline minecraft.net", strings.Join(reasons, "; "))
		return u, nil
	}
	reasons = append(reasons, err.Error())
	return "", fmt.Errorf("%s", strings.Join(reasons, "; "))
}

// ── Кеш на диску ────────────────────────────────────────────────────────

// newsBannerCachePath — <CacheDir>/banners/<sha256(url)>.img. Ім'я за хешем
// URL: нова версія гри = нова URL картинки = новий файл; стара картинка
// лишається в кеші (її підбере загальний бот-прибиральник кешу).
func (a *App) newsBannerCachePath(imageURL string) string {
	sum := sha256.Sum256([]byte(imageURL))
	name := hex.EncodeToString(sum[:16]) + ".img"
	return filepath.Join(a.cfg.CacheDir(), newsBannerDirName, name)
}

// loadNewsBannerFromDisk повертає байти з кешу (nil, nil — файлу немає,
// це «ще не качали», а не помилка).
func loadNewsBannerFromDisk(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil // порожній файл = битий кеш, качаємо заново
	}
	return data, nil
}

// downloadNewsBanner качає картинку в кеш через tmp-файл і rename — щоб у
// кеш не потрапила обрізана пів-картинки при обриві мережі (той самий
// атомарний принцип, що atomicfile для JSON-станів лаунчера).
func downloadNewsBanner(imageURL, path string) ([]byte, error) {
	ua := "shaurma-team/shaurma-launcher/2.0 (news-banner)"
	if strings.HasPrefix(imageURL, newsBannerSiteBase) {
		ua = newsBannerBrowserUA
	}
	data, err := newsBannerGetUA(imageURL, ua)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("порожня відповідь від %s", imageURL)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return nil, err
	}
	return data, nil
}

// ── Основна логіка ──────────────────────────────────────────────────────

// refreshNewsBannerOnce виконує повний цикл фічі і повертає data: URL
// картинки банера. Швидкий шлях (TTL гарячий + файл у кеші) не робить
// жодного мережевого запиту.
func (a *App) refreshNewsBannerOnce() (string, error) {
	// Швидкий шлях: уже знаємо URL і файл лежить на диску.
	if data, path, ok := a.cachedNewsBanner(); ok {
		return localFileImageDataURL(data, path), nil
	}

	// Повний цикл: серіалізуємо конкурентні виклики (стартовий фоновий +
	// onMount фронтенду) — news.json качається лише один раз.
	newsBannerState.refreshMu.Lock()
	defer newsBannerState.refreshMu.Unlock()

	// Повторна перевірка після очікування: поки ми стояли в черзі, перший
	// виклик міг уже все принести.
	if data, path, ok := a.cachedNewsBanner(); ok {
		return localFileImageDataURL(data, path), nil
	}

	// Кроки 1-3: URL картинки — news.json за версією, інакше timeline сайту.
	imageURL, err := resolveNewsBannerImageURL()
	if err != nil {
		return "", err
	}

	// Крок 4: качаємо картинку або беремо її з дискового кешу.
	path := a.newsBannerCachePath(imageURL)
	bytes, err := loadNewsBannerFromDisk(path)
	if err != nil {
		log.Printf("newsBanner: кеш недоступний (%v), качаю заново", err)
		bytes = nil
	}
	if bytes == nil {
		bytes, err = downloadNewsBanner(imageURL, path)
		if err != nil {
			return "", err
		}
	}

	newsBannerState.mu.Lock()
	newsBannerState.at = time.Now()
	newsBannerState.imageURL = imageURL
	newsBannerState.cachePath = path
	newsBannerState.mu.Unlock()
	return localFileImageDataURL(bytes, path), nil
}

// cachedNewsBanner — швидкий шлях без мережі: TTL гарячий і файл у кеші
// на місці. Повертає (байти, шлях, true) або (nil, "", false).
func (a *App) cachedNewsBanner() ([]byte, string, bool) {
	newsBannerState.mu.Lock()
	hot := time.Since(newsBannerState.at) < newsBannerTTL && newsBannerState.imageURL != ""
	path := newsBannerState.cachePath
	newsBannerState.mu.Unlock()
	if !hot || path == "" {
		return nil, "", false
	}
	data, err := loadNewsBannerFromDisk(path)
	if err != nil || data == nil {
		return nil, "", false
	}
	return data, path, true
}

// GetNewsBanner — експортований для фронтенду метод (викликається через
// Bridge.Invoke з NewsBanner.svelte в onMount). Повертає data: URL
// картинки останнього офіційного оновлення або помилку (фронтенд тоді
// просто не показує банер — це компактно і без зайвих станів UI).
func (a *App) GetNewsBanner() (string, error) {
	return a.refreshNewsBannerOnce()
}

// ensureNewsBannerAtStartup — фонова перевірка банера РІВНО раз при старті
// лаунчера (той самий патерн, що ensureAutoVersionsAtStartup для авто-
// збірок). Результат шлється подією news:banner: фронтенд підписується на
// неї в onMount, тож банер з'являється, навіть якщо користувач нічого не
// натискав, а новий реліз Mojang підхоплюється при кожному запуску.
func (a *App) ensureNewsBannerAtStartup() {
	go func() {
		defer crashguard.Recover("news_banner.go:358")
		dataURL, err := a.refreshNewsBannerOnce()
		if err != nil {
			// Тихо: немає мережі / Mojang недоступний — банер просто не
			// показується цього сеансу, ламати через нього старт не треба.
			log.Printf("newsBanner: %v", err)
			return
		}
		a.emit("news:banner", map[string]any{"dataUrl": dataURL})
	}()
}

// localFileImageDataURL — MIME-обгортка над байтами картинки кешу.
// contentTypeForBytes сніффить вміст (PNG/JPEG/WebP/GIF) за магічними
// байтами — той самий хелпер, що для іконок/фонів збірок (app.go).
func localFileImageDataURL(data []byte, path string) string {
	ct := contentTypeForBytes(data)
	if ct == "" || !strings.HasPrefix(ct, "image/") {
		// Фолбек на розширення кеш-файла (магічних байтів немає — несподівано,
		// але краще віддати щось, ніж банер «не прийшов» через MIME).
		ct = "image/png"
		switch strings.ToLower(filepath.Ext(path)) {
		case ".jpg", ".jpeg":
			ct = "image/jpeg"
		case ".webp":
			ct = "image/webp"
		case ".gif":
			ct = "image/gif"
		}
	}
	return "data:" + ct + ";base64," + base64.StdEncoding.EncodeToString(data)
}
