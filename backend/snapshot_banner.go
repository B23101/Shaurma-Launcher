package main

// ── Банер авто-snapshot: картинка САМОГО снапшота, а не релізу ──────────
//
// ПРОБЛЕМА. Сторінка збірки auto-snapshot брала ту саму картинку, що й
// головний банер (GetNewsBanner → картинка останнього РЕЛІЗУ). Для
// снапшота має показуватись картинка з його власної статті на minecraft.net.
//
// ЗАКОНОМІРНІСТЬ (перевірено на 26.4 Snapshot 5):
//
//	версія в Mojang-маніфесті      slug статті на minecraft.net
//	26.4-snapshot-5            →   minecraft-26-4-snapshot-5
//	26.3-pre-2                 →   minecraft-26-3-pre-release-2
//	26.3-rc-2                  →   minecraft-26-3-release-candidate-2
//	25w51a (стара схема)       →   minecraft-snapshot-25w51a
//	1.21.5-pre1 (стара схема)  →   minecraft-1-21-5-pre-release-1
//
// Стаття: https://www.minecraft.net/en-us/article/<slug>
// Картинка статті = <meta property="og:image">. ЧАСТО це
// /content/dam/minecraftnet/article-asset/<рік>/<slug>/new-article-hero-image.jpg,
// але НЕ ЗАВЖДИ (стара назва файлу, інший каталог, .png) — тому шлях
// файла НІКОЛИ не вгадуємо першим, а читаємо з og:image самої статті.
// Вгаданий hero-шлях лишається тільки запасним варіантом, коли сторінку
// не вдалося завантажити.
//
// ЛАНЦЮЖОК ПОШУКУ для однієї версії (перший успіх виграє):
//  1. стаття minecraft.net за кожним кандидатом slug → og:image;
//  2. вгаданий hero-шлях article-asset/<рік>/<slug>/new-article-hero-image.*;
//  3. launchercontent.mojang.com/v2/javaPatchNotes.json (запис за version);
//  4. launchercontent.mojang.com/news.json (заголовок містить «26.4 Snapshot 5»).
//
// Якщо для найновішого снапшота картинки ще нема (Mojang виставляє версію
// в маніфест РАНІШЕ, ніж публікує статтю) — йдемо назад по історії
// снапшотів (до snapshotBannerMaxWalk штук), доки не знайдемо картинку.
// Картинку РЕЛІЗУ ніколи не підставляємо: нема знайденої — фронтенд
// лишає градієнт збірки.

import (
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	snapshotArticleBase = "https://www.minecraft.net/en-us/article/"
	snapshotAssetBase   = "https://www.minecraft.net/content/dam/minecraftnet/article-asset/"
	snapshotPatchNotes  = "https://launchercontent.mojang.com/v2/javaPatchNotes.json"
	// Скільки останніх снапшотів перебираємо назад, шукаючи картинку.
	snapshotBannerMaxWalk = 6
	// Позитивний результат живе довго (стаття не змінюється), негативний —
	// коротко: стаття може з'явитись через годину після появи версії.
	snapshotBannerOKTTL   = 6 * time.Hour
	snapshotBannerMissTTL = 20 * time.Minute
)

var snapshotBannerCache struct {
	mu       sync.Mutex
	at       time.Time
	version  string // версія, для якої шукали (latest.snapshot на той момент)
	imageURL string // "" + miss=true → «не знайшли»
	miss     bool
}

// ── Slug-и статей ───────────────────────────────────────────────────────

var (
	// 26.4-snapshot-5 / 1.21.6-snapshot-1
	reSnapNew = regexp.MustCompile(`^(\d+(?:\.\d+)*)-snapshot-(\d+)$`)
	// 26.3-pre-2 / 1.21.5-pre-2 / 1.21.5-pre2
	rePreAny = regexp.MustCompile(`^(\d+(?:\.\d+)*)-pre-?(\d+)$`)
	// 26.3-rc-2 / 1.21.5-rc-1 / 1.21.5-rc1
	reRCAny = regexp.MustCompile(`^(\d+(?:\.\d+)*)-rc-?(\d+)$`)
	// 25w51a, 24w14b
	reWeekly = regexp.MustCompile(`^(\d{2})w(\d{2})([a-z])$`)
	// 1.21.6-experimental-snapshot-1 та подібні
	reExperimental = regexp.MustCompile(`^(\d+(?:\.\d+)*)-experimental-snapshot-(\d+)$`)
)

func dashVersion(v string) string { return strings.ReplaceAll(v, ".", "-") }

// snapshotArticleSlugs повертає кандидатів slug-а статті minecraft.net для
// версії з Mojang-маніфесту — від найімовірнішого до найменш імовірного.
// Порожній результат = версію не розпізнано (шукаємо іншими джерелами).
func snapshotArticleSlugs(version string) []string {
	v := strings.ToLower(strings.TrimSpace(version))
	if v == "" {
		return nil
	}
	var out []string
	add := func(s string) {
		for _, e := range out {
			if e == s {
				return
			}
		}
		out = append(out, s)
	}
	switch {
	case reSnapNew.MatchString(v):
		m := reSnapNew.FindStringSubmatch(v)
		add("minecraft-" + dashVersion(m[1]) + "-snapshot-" + m[2])
	case rePreAny.MatchString(v):
		m := rePreAny.FindStringSubmatch(v)
		add("minecraft-" + dashVersion(m[1]) + "-pre-release-" + m[2])
		add("minecraft-" + dashVersion(m[1]) + "-pre-" + m[2])
	case reRCAny.MatchString(v):
		m := reRCAny.FindStringSubmatch(v)
		add("minecraft-" + dashVersion(m[1]) + "-release-candidate-" + m[2])
		add("minecraft-" + dashVersion(m[1]) + "-rc-" + m[2])
	case reWeekly.MatchString(v):
		add("minecraft-snapshot-" + v)
		add("minecraft-" + v)
	case reExperimental.MatchString(v):
		m := reExperimental.FindStringSubmatch(v)
		add("minecraft-" + dashVersion(m[1]) + "-experimental-snapshot-" + m[2])
	}
	// Загальний запасний варіант: усе не-буквено-цифрове → «-».
	generic := regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(v, "-")
	generic = strings.Trim(generic, "-")
	if generic != "" {
		add("minecraft-" + generic)
	}
	return out
}

// snapshotHumanTitle: «26.4-snapshot-5» → «26.4 Snapshot 5» (для пошуку в
// news.json за заголовком). Порожній — формат не розпізнано.
func snapshotHumanTitle(version string) string {
	v := strings.ToLower(strings.TrimSpace(version))
	switch {
	case reSnapNew.MatchString(v):
		m := reSnapNew.FindStringSubmatch(v)
		return m[1] + " Snapshot " + m[2]
	case rePreAny.MatchString(v):
		m := rePreAny.FindStringSubmatch(v)
		return m[1] + " Pre-Release " + m[2]
	case reRCAny.MatchString(v):
		m := reRCAny.FindStringSubmatch(v)
		return m[1] + " Release Candidate " + m[2]
	}
	return ""
}

// snapshotSlugYear — рік для вгаданого hero-шляху. Нові версії несуть рік у
// номері («26.4» → 2026); старі тижневі «25w51a» → 2025.
func snapshotSlugYear(version string) []int {
	v := strings.TrimSpace(version)
	var years []int
	if m := reWeekly.FindStringSubmatch(strings.ToLower(v)); m != nil {
		if yy, err := strconv.Atoi(m[1]); err == nil {
			years = append(years, 2000+yy)
		}
	} else if i := strings.IndexByte(v, '.'); i > 0 {
		if yy, err := strconv.Atoi(v[:i]); err == nil && yy >= 20 && yy < 100 {
			years = append(years, 2000+yy)
		}
	}
	cur := time.Now().Year()
	years = append(years, cur, cur-1)
	seen := map[int]bool{}
	uniq := years[:0]
	for _, y := range years {
		if !seen[y] {
			seen[y] = true
			uniq = append(uniq, y)
		}
	}
	return uniq
}

// ── Парсинг сторінки статті ─────────────────────────────────────────────

var (
	reMetaTag = regexp.MustCompile(`(?is)<meta\b[^>]*>`)
	reAttr    = regexp.MustCompile(`(?is)\b([a-z:_-]+)\s*=\s*("([^"]*)"|'([^']*)')`)
)

// parseArticleOGImage дістає абсолютний URL з <meta property="og:image"> (з
// фолбеком на twitter:image). Порядок атрибутів у <meta> довільний, тож
// розбираємо тег по атрибутах, а не одним регекспом. Логотипи/svg і
// загальносайтові картинки відкидаємо — це не картинка статті.
func parseArticleOGImage(html []byte) string {
	var twitter string
	for _, tag := range reMetaTag.FindAll(html, -1) {
		attrs := map[string]string{}
		for _, m := range reAttr.FindAllStringSubmatch(string(tag), -1) {
			val := m[3]
			if val == "" {
				val = m[4]
			}
			attrs[strings.ToLower(m[1])] = val
		}
		key := strings.ToLower(attrs["property"])
		if key == "" {
			key = strings.ToLower(attrs["name"])
		}
		content := strings.TrimSpace(strings.ReplaceAll(attrs["content"], "&amp;", "&"))
		if content == "" {
			continue
		}
		switch key {
		case "og:image":
			if u := cleanArticleImage(content); u != "" {
				return u
			}
		case "twitter:image":
			if twitter == "" {
				twitter = cleanArticleImage(content)
			}
		}
	}
	return twitter
}

func cleanArticleImage(raw string) string {
	u := resolveTimelineImageURL(raw) // відносний шлях → https://www.minecraft.net/...
	low := strings.ToLower(u)
	if u == "" || strings.HasSuffix(low, ".svg") || strings.Contains(low, "/logos/") {
		return ""
	}
	return u
}

// ── Окремі джерела ──────────────────────────────────────────────────────

// imageFromArticle: стаття minecraft.net → og:image.
func imageFromArticle(slug string) (string, error) {
	data, err := newsBannerGetUA(snapshotArticleBase+slug, newsBannerBrowserUA)
	if err != nil {
		return "", err
	}
	if u := parseArticleOGImage(data); u != "" {
		return u, nil
	}
	return "", fmt.Errorf("у статті %s немає og:image", slug)
}

// imageFromGuessedHero: article-asset/<рік>/<slug>/new-article-hero-image.<ext>.
// Перевіряємо, що за URL справді лежить картинка (магічні байти), а не
// HTML-заглушка 200 OK.
func imageFromGuessedHero(version, slug string) (string, error) {
	for _, year := range snapshotSlugYear(version) {
		for _, ext := range []string{"jpg", "png", "jpeg", "webp"} {
			u := fmt.Sprintf("%s%d/%s/new-article-hero-image.%s", snapshotAssetBase, year, slug, ext)
			data, err := newsBannerGetUA(u, newsBannerBrowserUA)
			if err != nil {
				continue
			}
			if ct := contentTypeForBytes(data); strings.HasPrefix(ct, "image/") {
				return u, nil
			}
		}
	}
	return "", fmt.Errorf("hero-картинку для %s не вгадано", slug)
}

type javaPatchNotesFeed struct {
	Entries []struct {
		Title   string `json:"title"`
		Type    string `json:"type"`
		Version string `json:"version"`
		Image   *struct {
			URL string `json:"url"`
		} `json:"image"`
	} `json:"entries"`
}

// imageFromPatchNotes: офіційний каталог патчноутів лаунчера Minecraft —
// там у кожного запису є version і image.url (відносний шлях).
func imageFromPatchNotes(version string) (string, error) {
	data, err := newsBannerGet(snapshotPatchNotes)
	if err != nil {
		return "", fmt.Errorf("javaPatchNotes: %w", err)
	}
	var feed javaPatchNotesFeed
	if err := json.Unmarshal(data, &feed); err != nil {
		return "", fmt.Errorf("javaPatchNotes: %w", err)
	}
	want := strings.ToLower(strings.TrimSpace(version))
	human := strings.ToLower(snapshotHumanTitle(version))
	for _, e := range feed.Entries {
		if e.Image == nil || strings.TrimSpace(e.Image.URL) == "" {
			continue
		}
		if strings.ToLower(strings.TrimSpace(e.Version)) == want ||
			(human != "" && strings.ToLower(strings.TrimSpace(e.Title)) == human) {
			return resolveNewsImageURL(e.Image.URL), nil
		}
	}
	return "", fmt.Errorf("у javaPatchNotes немає %s з картинкою", version)
}

// imageFromNewsFeed: news.json — заголовок має ПОВНІСТЮ містити «26.4 Snapshot 5»
// (а не лише «26.4», інакше зловили б будь-яку статтю цієї гілки).
func imageFromNewsFeed(version string) (string, error) {
	human := strings.ToLower(snapshotHumanTitle(version))
	if human == "" {
		return "", fmt.Errorf("версію %s не розпізнано для news.json", version)
	}
	data, err := newsBannerGet(newsBannerNewsURL)
	if err != nil {
		return "", fmt.Errorf("Mojang news: %w", err)
	}
	var feed newsFeed
	if err := json.Unmarshal(data, &feed); err != nil {
		return "", fmt.Errorf("Mojang news: %w", err)
	}
	for i := range feed.Entries {
		if strings.Contains(strings.ToLower(feed.Entries[i].Title), human) && feed.Entries[i].imageURL() != "" {
			return resolveNewsImageURL(feed.Entries[i].imageURL()), nil
		}
	}
	return "", fmt.Errorf("у news.json немає «%s» з картинкою", human)
}

// resolveSnapshotImageURL — повний ланцюжок для ОДНІЄЇ версії.
func resolveSnapshotImageURL(version string) (string, error) {
	var reasons []string
	slugs := snapshotArticleSlugs(version)
	for _, slug := range slugs {
		u, err := imageFromArticle(slug)
		if err == nil {
			return u, nil
		}
		reasons = append(reasons, err.Error())
	}
	// Вгадування hero-шляху — дорожче (кілька GET), тому лише для головного slug.
	if len(slugs) > 0 {
		if u, err := imageFromGuessedHero(version, slugs[0]); err == nil {
			return u, nil
		}
	}
	if u, err := imageFromPatchNotes(version); err == nil {
		return u, nil
	} else {
		reasons = append(reasons, err.Error())
	}
	if u, err := imageFromNewsFeed(version); err == nil {
		return u, nil
	} else {
		reasons = append(reasons, err.Error())
	}
	return "", fmt.Errorf("%s: %s", version, strings.Join(reasons, "; "))
}

// recentSnapshotIDs повертає до n найновіших версій типу snapshot (маніфест
// відсортований від новішого). Перший елемент — latest.snapshot, якщо він є.
func recentSnapshotIDs(n int) ([]string, error) {
	data, err := newsBannerGet(newsBannerManifestURL)
	if err != nil {
		return nil, err
	}
	var m autoVersionManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	var ids []string
	push := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		for _, e := range ids {
			if e == id {
				return
			}
		}
		ids = append(ids, id)
	}
	push(m.Latest.Snapshot)
	for _, v := range m.Versions {
		if v.Type == "snapshot" {
			push(v.ID)
		}
		if len(ids) >= n {
			break
		}
	}
	if len(ids) > n {
		ids = ids[:n]
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("у Mojang-маніфесті немає жодного снапшота")
	}
	return ids, nil
}

// resolveLatestSnapshotImageURL: найновіший снапшот → якщо картинки ще
// нема, йдемо назад по історії. Результат кешується (див. TTL вище).
func resolveLatestSnapshotImageURL() (version, imageURL string, err error) {
	snapshotBannerCache.mu.Lock()
	age := time.Since(snapshotBannerCache.at)
	if snapshotBannerCache.version != "" {
		if !snapshotBannerCache.miss && age < snapshotBannerOKTTL {
			v, u := snapshotBannerCache.version, snapshotBannerCache.imageURL
			snapshotBannerCache.mu.Unlock()
			return v, u, nil
		}
		if snapshotBannerCache.miss && age < snapshotBannerMissTTL {
			snapshotBannerCache.mu.Unlock()
			return "", "", fmt.Errorf("картинку снапшота не знайдено (повторна спроба пізніше)")
		}
	}
	snapshotBannerCache.mu.Unlock()

	ids, err := recentSnapshotIDs(snapshotBannerMaxWalk)
	if err != nil {
		return "", "", err
	}
	var lastErr error
	for _, id := range ids {
		u, rerr := resolveSnapshotImageURL(id)
		if rerr != nil {
			lastErr = rerr
			log.Printf("snapshotBanner: %v", rerr)
			continue
		}
		snapshotBannerCache.mu.Lock()
		snapshotBannerCache.at = time.Now()
		snapshotBannerCache.version, snapshotBannerCache.imageURL, snapshotBannerCache.miss = id, u, false
		snapshotBannerCache.mu.Unlock()
		return id, u, nil
	}
	snapshotBannerCache.mu.Lock()
	snapshotBannerCache.at = time.Now()
	snapshotBannerCache.version, snapshotBannerCache.imageURL, snapshotBannerCache.miss = ids[0], "", true
	snapshotBannerCache.mu.Unlock()
	return "", "", fmt.Errorf("картинку снапшота не знайдено: %v", lastErr)
}

// GetSnapshotBanner — data: URL картинки НАЙНОВІШОГО снапшота (стаття
// minecraft.net, з відкатом по історії снапшотів). Викликається з
// PackInstancePage для авто-збірки типу snapshot. Помилка = фронтенд лишає
// градієнт збірки (картинку релізу тут НЕ підставляємо навмисно).
func (a *App) GetSnapshotBanner() (string, error) {
	_, imageURL, err := resolveLatestSnapshotImageURL()
	if err != nil {
		return "", err
	}
	path := a.newsBannerCachePath(imageURL)
	data, lerr := loadNewsBannerFromDisk(path)
	if lerr != nil {
		log.Printf("snapshotBanner: кеш недоступний (%v), качаю заново", lerr)
		data = nil
	}
	if data == nil {
		data, err = downloadNewsBanner(imageURL, path)
		if err != nil {
			return "", err
		}
	}
	return localFileImageDataURL(data, path), nil
}
