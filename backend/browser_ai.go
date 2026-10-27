package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"shaurma-launcher-backend/internal/ai"
	"shaurma-launcher-backend/internal/api"
	"shaurma-launcher-backend/internal/custompack"
	"shaurma-launcher-backend/internal/model"
)

// ── ШІ-функції браузера контенту (переклад + відгук) ───────────────────
// Обидві працюють через Groq (див. internal/ai/groq.go) і КЕШУЮТЬСЯ на
// диску: повторне відкриття сторінки проєкту не б'є в API знову, а читає
// вже згенерований результат.
//
// Кеш перекладу: browser-ai/translations/v2-<source>-<projectId>-<lang>.json
//   (v2-префікс — версія формату: старий кеш без нього міг зберегти
//   переклад з JSON-дужками прямо в тілі і більше не читається).
// Кеш відгуку:   browser-ai/reviews/v3-<source>-<projectId>-<lang>.json
//   (v3-префікс — версія формату: v1 лише хвалив, v2 — шаблонний огляд,
//   v3 — повноцінний туторіал. Старі кеші не читаються і генеруються
//   заново під новий промт).

// BrowserTranslation — результат ШІ-перекладу сторінки проєкту. Опис
// перекладається зі збереженням розмітки (markdown/HTML): кольори,
// посилання, код лишаються як в оригіналі.
type BrowserTranslation struct {
	Lang        string `json:"lang"`
	Description string `json:"description"`
	Body        string `json:"body"`
	// FromCache — true, якщо результат узято з дискового кешу (повторне
	// відкриття — без запиту до Groq).
	FromCache bool `json:"fromCache"`
	// Created — час генерації (RFC3339).
	Created string `json:"created"`
}

// BrowserReview — результат AI-відгуку про проєкт.
type BrowserReview struct {
	Lang string `json:"lang"`
	// Review — markdown-текст відгуку людською мовою.
	Review string `json:"review"`
	// Sources — сайти, які AI знайшов у веб-пошуку (для довіри).
	Sources []string `json:"sources"`
	// FromCache — true, якщо відгук узято з кешу (перегенерація заборонена).
	FromCache bool   `json:"fromCache"`
	Created   string `json:"created"`
}

// browserAIDir — ЛЕГАСІ-тека файлового кешу ШІ (browser-ai/ у теці
// конфігурації). Лишилась лише для одноразової міграції у БД (див.
// migration.go): тепер кеш живе в SQLite (таблиця browser_ai_cache).
func (a *App) browserAIDir() string {
	return filepath.Join(a.cfg.ConfigDir(), "browser-ai")
}

// aiCacheKind — типи кешованих ШІ-відповідей у БД (таблиця
// browser_ai_cache, колонка kind): переклад опису та відгук про проєкт.
// GC чистить кожен тип за власним TTL (див. maintenance.go).
const (
	aiCacheKindTranslation = "translation"
	aiCacheKindReview      = "review"
)

// groqClient — ліниво створюваний клієнт Groq (ключ: env GROQ_API_KEY →
// groq.key → вбудований дефолт). Перестворюється, якщо в налаштуваннях
// з'явився власний ключ (той самий підхід, що a.ai для Gemini).
func (a *App) groqClient() *ai.GroqClient {
	s := a.cfg.GetSettings()
	return ai.NewGroqClient(s.ConsoleAIKey, a.cfg.ConfigDir(), nil)
}

// groqAPIKeyResolved повертає КЛЮЧ, який реально використає Groq-клієнт
// (налаштування → env/file groq.key → вбудований у backend.dll дефолт),
// або "" — якщо ключа немає взагалі (збірка без -GroqApiKey і без
// groq.key). Перевірка потрібна, щоб показати зрозумілу помилку замість
// сирого HTTP 401 від Groq.
func (a *App) groqAPIKeyResolved() string {
	s := a.cfg.GetSettings()
	if k := strings.TrimSpace(s.ConsoleAIKey); k != "" {
		return k
	}
	if k := ai.ResolveGroqAPIKey(a.cfg.ConfigDir()); k != "" {
		return k
	}
	return ai.DefaultGroqAPIKey
}

// ── Переклад опису проєкту ──────────────────────────────────────────────

// TranslateBrowserProject перекладає опис проєкту (короткий + повний body)
// мовою lang (uk/en/...). Результат КЕШУЄТЬСЯ на диску: повторний виклик
// для того самого (source, projectId, lang) повертає закешований переклад
// без запиту до Groq. Викликається ЛИШЕ по кнопці «Перекласти» (не
// автоматично).
func (a *App) TranslateBrowserProject(source, projectID, lang string) (BrowserTranslation, error) {
	if lang == "" {
		lang = "uk"
	}
	// v2 у ключі: стара версія кешу (до json-режиму Groq) могла зберегти
	// переклад з JSON-дужками {"description":...} прямо в тілі — такий кеш
	// більше не читається, і переклад перегенерується чистим.
	cacheKey := fmt.Sprintf("v2-%s-%s-%s", cacheKeySafe(source), cacheKeySafe(projectID), cacheKeySafe(lang))

	// 1. Кеш (SQLite): вже перекладено — повертаємо без запиту.
	if st := a.cfg.Store(); st != nil {
		if e, ok, err := st.GetAICache(cacheKey); err == nil && ok {
			var cached BrowserTranslation
			if json.Unmarshal([]byte(e.Data), &cached) == nil && cached.Body != "" {
				cached.FromCache = true
				return cached, nil
			}
		}
	}

	// 2. Оригінальний опис з джерела.
	detail, err := a.GetBrowserProjectDetail(source, projectID)
	if err != nil {
		return BrowserTranslation{}, fmt.Errorf("не вдалося отримати проєкт для перекладу: %w", err)
	}

	// 3. Переклад через Groq (розмітка зберігається).
	if a.groqAPIKeyResolved() == "" {
		return BrowserTranslation{}, fmt.Errorf("ШІ-ключ (Groq) не вбудовано у цю збірку лаунчера — переклад недоступний. Додайте ключ у Налаштуваннях або передайте -GroqApiKey при збірці")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	desc, body, err := a.groqClient().TranslateDescription(ctx, langNameForAI(lang), detail.Description, detail.Body)
	if err != nil {
		return BrowserTranslation{}, fmt.Errorf("ШІ-переклад не вдався: %w", err)
	}
	if body == "" {
		body = detail.Body
	}
	if desc == "" {
		desc = detail.Description
	}

	res := BrowserTranslation{
		Lang:        lang,
		Description: desc,
		Body:        body,
		Created:     time.Now().Format(time.RFC3339),
	}

	// 4. Кешуємо (навіть порожній переклад — щоб не бити API повторно).
	if st := a.cfg.Store(); st != nil {
		if data, err := json.Marshal(res); err == nil {
			_ = st.SaveAICache(aiCacheKindTranslation, cacheKey, string(data))
		}
	} else {
		dir := filepath.Join(a.browserAIDir(), "translations")
		_ = os.MkdirAll(dir, 0755)
		if data, err := json.Marshal(res); err == nil {
			_ = os.WriteFile(filepath.Join(dir, cacheKey+".json"), data, 0644)
		}
	}
	return res, nil
}

// ── AI-відгук про проєкт ────────────────────────────────────────────────

// GetBrowserProjectReview генерує відгук про проєкт: AI отримує те, що
// бачить користувач (назва, опис, статистика, категорії, версії) +
// результати веб-пошуку і пише людською мовою розгорнутий відгук.
//
// ВАЖЛИВО: відгук генерується ОДИН раз. Якщо відгук уже закешований —
// повертається з кешу (FromCache=true) і НЕ перегенеровується. Повторна
// генерація можлива лише після помилки: помилкові відповіді не кешуються,
// тож кнопка «Спробувати ще раз» спрацює.
func (a *App) GetBrowserProjectReview(source, projectID, lang string) (BrowserReview, error) {
	if lang == "" {
		lang = "uk"
	}
	// v3 у ключі: версія промту. v1 — лише хвалив, v2 — описував мод
	// шаблонним оглядом; v3 — повноцінний ТУТОРІАЛ (встановлення,
	// налаштування, часті помилки). Старі кеші не читаються — відгуки
	// генеруються заново під новий формат.
	cacheKey := fmt.Sprintf("v3-%s-%s-%s", cacheKeySafe(source), cacheKeySafe(projectID), cacheKeySafe(lang))

	// 1. Кеш (SQLite): відгук уже є — перегенерація заборонена, повертаємо.
	if st := a.cfg.Store(); st != nil {
		if e, ok, err := st.GetAICache(cacheKey); err == nil && ok {
			var cached BrowserReview
			if json.Unmarshal([]byte(e.Data), &cached) == nil && cached.Review != "" {
				cached.FromCache = true
				return cached, nil
			}
		}
	}

	// 2. Збираємо контекст: що бачить користувач на сторінці проєкту.
	detail, err := a.GetBrowserProjectDetail(source, projectID)
	if err != nil {
		return BrowserReview{}, fmt.Errorf("не вдалося отримати проєкт для відгуку: %w", err)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Проєкт: %s (%s)\n", detail.Name, detail.ProjectType)
	fmt.Fprintf(&sb, "Джерело: %s\n", source)
	fmt.Fprintf(&sb, "Автор: %s\n", detail.Author)
	fmt.Fprintf(&sb, "Завантажень: %d, підписників: %d\n", detail.Downloads, detail.Followers)
	if len(detail.Categories) > 0 {
		fmt.Fprintf(&sb, "Категорії: %s\n", strings.Join(detail.Categories, ", "))
	}
	if len(detail.MCVersions) > 0 {
		// Обмежуємо, щоб не роздувати промт: перші 12 версій.
		vers := detail.MCVersions
		if len(vers) > 12 {
			vers = vers[:12]
		}
		fmt.Fprintf(&sb, "Версії Minecraft: %s\n", strings.Join(vers, ", "))
	}
	if len(detail.Loaders) > 0 {
		fmt.Fprintf(&sb, "Лоадери: %s\n", strings.Join(detail.Loaders, ", "))
	}
	if detail.Description != "" {
		// Опис може бути довгим — беремо перші ~1000 символів.
		desc := detail.Description
		if len(desc) > 1000 {
			desc = desc[:1000] + "…"
		}
		fmt.Fprintf(&sb, "Короткий опис: %s\n", desc)
	}
	if detail.Body != "" {
		// Повний опис (markdown/HTML) — головне джерело, щоб відгук ОПИСУВАВ
		// мод, а не хвалив наосліп. Обмежуємо розмір, щоб не роздувати промт.
		bodyTxt := stripMarkdownHTML(detail.Body)
		if len(bodyTxt) > 2500 {
			bodyTxt = bodyTxt[:2500] + "…"
		}
		fmt.Fprintf(&sb, "Повний опис:\n%s\n", bodyTxt)
	}
	// Дата публікації/оновлення.
	if detail.Published != "" {
		fmt.Fprintf(&sb, "Опубліковано: %s\n", detail.Published)
	}
	if detail.Updated != "" {
		fmt.Fprintf(&sb, "Оновлено: %s\n", detail.Updated)
	}

	// 3. Веб-пошук: «гуглить у інтернеті» згадки про проєкт.
	searchQuery := detail.Name + " minecraft " + detail.ProjectType + " review"
	sources := ai.SearchWeb(context.Background(), searchQuery, 5)
	if len(sources) > 0 {
		sb.WriteString("\nЗгадки у веб-пошуку:\n")
		for _, s := range sources {
			fmt.Fprintf(&sb, "- %s\n", s)
		}
	}

	// 4. Генеруємо відгук.
	if a.groqAPIKeyResolved() == "" {
		return BrowserReview{}, fmt.Errorf("ШІ-ключ (Groq) не вбудовано у цю збірку лаунчера — відгук недоступний. Додайте ключ у Налаштуваннях або передайте -GroqApiKey при збірці")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	review, err := a.groqClient().WriteProjectReview(ctx, langNameForAI(lang), sb.String())
	if err != nil {
		// Помилка НЕ кешується — користувач може спробувати ще раз.
		return BrowserReview{}, fmt.Errorf("AI-відгук не вдався: %w", err)
	}

	res := BrowserReview{
		Lang:    lang,
		Review:  review,
		Sources: sources,
		Created: time.Now().Format(time.RFC3339),
	}

	// 5. Кешуємо — наступного разу повернеться з кешу (без перегенерації).
	if st := a.cfg.Store(); st != nil {
		if data, err := json.Marshal(res); err == nil {
			_ = st.SaveAICache(aiCacheKindReview, cacheKey, string(data))
		}
	} else {
		dir := filepath.Join(a.browserAIDir(), "reviews")
		_ = os.MkdirAll(dir, 0755)
		if data, err := json.Marshal(res); err == nil {
			_ = os.WriteFile(filepath.Join(dir, cacheKey+".json"), data, 0644)
		}
	}
	return res, nil
}

// cleanupOldCacheFiles прибирає файли кешу старого формату (без заданого
// префікса) у вказаній теці — переклади/відгуки зі старих версій промтів,
// які більше не читаються і лише займають місце. Лишилась для одноразової
// міграції файлового кешу в БД (див. migration.go).
func (a *App) cleanupOldCacheFiles(dir, prefix string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() && strings.HasSuffix(name, ".json") && !strings.HasPrefix(name, prefix) {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
}

// cacheKeySafe — безпечний сегмент імені файлу кешу (ID проєкту/мова
// можуть містити символи, неприпустимі в іменах файлів Windows).
func cacheKeySafe(s string) string {
	replacer := strings.NewReplacer("\\", "_", "/", "_", ":", "_", "*", "_", "?", "_", "\"", "_", "<", "_", ">", "_", "|", "_", " ", "_")
	return replacer.Replace(s)
}

// stripMarkdownHTML прибирає markdown-розмітку та HTML-теги з тексту
// (повний опис проєкту) — щоб модель отримала ЧИСТИЙ текст для відгуку,
// а не сирий markdown/HTML з дужками, який засмічує промт.
func stripMarkdownHTML(s string) string {
	// HTML-теги (<...>) та markdown-розмітка (#, **, `, [](), ![][], тощо).
	// Послідовно, бо описи CurseForge — це HTML, а Modrinth — markdown.
	r := s
	r = strings.ReplaceAll(r, "<br>", "\n")
	r = strings.ReplaceAll(r, "<br/>", "\n")
	r = strings.ReplaceAll(r, "<br />", "\n")
	r = strings.ReplaceAll(r, "</p>", "\n")
	r = strings.ReplaceAll(r, "</h1>", "\n")
	r = strings.ReplaceAll(r, "</h2>", "\n")
	r = strings.ReplaceAll(r, "</h3>", "\n")
	r = strings.ReplaceAll(r, "</h4>", "\n")
	r = strings.ReplaceAll(r, "</li>", "\n")
	r = strings.ReplaceAll(r, "</ul>", "\n")
	r = strings.ReplaceAll(r, "</ol>", "\n")
	r = strings.ReplaceAll(r, "</tr>", "\n")
	r = strings.ReplaceAll(r, "</td>", " | ")
	r = strings.ReplaceAll(r, "</th>", " | ")
	// HTML-теги.
	r = smdTagsRe.ReplaceAllString(r, "")
	// HTML-сутності.
	r = smdEntRe.ReplaceAllString(r, " ")
	// Markdown: жирний/підкреслений/закреслений/код (до кінця рядка або
	// закриття), потім одинарний курсив *text* (тільки парні зірочки між
	// пробілами/початком рядка, щоб не з'їсти списки "* item").
	r = smdMDRe.ReplaceAllString(r, "")
	r = smdItalicRe.ReplaceAllString(r, "$1$2$3")
	r = strings.ReplaceAll(r, "###", "")
	r = strings.ReplaceAll(r, "##", "")
	r = strings.ReplaceAll(r, "#", "")
	// Посилання і зображення [text](url) / ![alt](url).
	r = smdImgRe.ReplaceAllString(r, " ")
	r = smdLinkRe.ReplaceAllString(r, " ")
	r = strings.ReplaceAll(r, "> ", "")
	r = strings.ReplaceAll(r, "* ", "• ")
	r = strings.ReplaceAll(r, "- ", "• ")
	// Стиснути порожні рядки.
	r = smdBlankRe.ReplaceAllString(r, "\n\n")
	return strings.TrimSpace(r)
}

// Package-level regex-и для stripMarkdownHTML (не компілюємо на кожен
// виклик — стиль як у groq.go: ddgResultRe тощо).
var (
	smdTagsRe = regexp.MustCompile(`<[^>]*>`)
	smdEntRe  = regexp.MustCompile(`&[a-zA-Z#0-9]+;`)
	smdMDRe   = regexp.MustCompile(`(?:\*\*|__|~~|` + "`" + `+)[^` + "`" + `\n]*`)
	// УВАГА: RE2 (Go regexp) НЕ підтримує lookahead (?=...) — тут класичний
	// варіант зі споживанням обмежувачів: "*text*" між пробілами/краями
	// рядка (щоб не з'їсти списки "* item"), зі збереженням обмежувачів у
	// заміні "$1$2$3".
	smdItalicRe = regexp.MustCompile(`(^|\s)\*([^*\n]+)\*(\s|$)`)
	smdImgRe    = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	smdLinkRe   = regexp.MustCompile(`\[[^\]]*\]\([^)]*\)`)
	smdBlankRe  = regexp.MustCompile(`\n{3,}`)
)

// langNameForAI — людська назва мови для промту Groq.
func langNameForAI(lang string) string {
	switch strings.ToLower(lang) {
	case "uk", "ua":
		return "українська"
	case "en":
		return "англійська"
	case "pl":
		return "польська"
	case "de":
		return "німецька"
	case "fr":
		return "французька"
	case "es":
		return "іспанська"
	case "ru":
		return "російська"
	default:
		return "українська"
	}
}

// ── Сторінка автора (Modrinth UserPage / OrganizationPage) ─────────────
// Модель — 1-в-1 з Modrinth app: профіль з аватаром + усі проєкти автора.
// CurseForge НЕ має endpoint-а профілю користувача (перевірено: немає
// /users/{id} і немає /users/{id}/projects) — єдине, що можна зробити —
// список проєктів через /mods/search?authorId=..., без аватара/біо.

// BrowserAuthor — профіль автора/організації для сторінки автора.
type BrowserAuthor struct {
	Source   string `json:"source"` // modrinth | curseforge
	AuthorID string `json:"authorId"`
	// Username — нікнейм/ім'я автора (для CurseForge — ім'я з проєкту).
	Username string `json:"username"`
	Name     string `json:"name"`
	Avatar   string `json:"avatar"`
	Bio      string `json:"bio"`
	Role     string `json:"role"`
	Created  string `json:"created"`
	// IsOrg — це організація (Modrinth org), а не користувач.
	IsOrg bool `json:"isOrg"`
	// HasProfile — чи API джерела віддає профіль автора. CurseForge — ні
	// (аватар/біо недоступні), Modrinth — так.
	HasProfile bool `json:"hasProfile"`
	// ProfileURL — посилання на сторінку автора на сайті джерела.
	ProfileURL string `json:"profileUrl"`
}

// GetBrowserAuthor повертає профіль автора. authorID — ID або username
// (Modrinth), числовий authorId (CurseForge). fallbackName — ім'я автора,
// відоме з поточної сторінки проєкту (для CurseForge, де профілю немає).
func (a *App) GetBrowserAuthor(source, authorID, fallbackName string) (BrowserAuthor, error) {
	switch source {
	case "modrinth":
		// Спершу — користувач; якщо це організація — /organization/{id}.
		if p, err := a.modrinth.GetUserProfile(authorID); err == nil && p.ID != "" {
			url := "https://modrinth.com/user/" + p.Username
			return BrowserAuthor{
				Source: source, AuthorID: p.ID, Username: p.Username,
				Name: firstNonEmpty(p.Name, p.Username), Avatar: p.AvatarURL,
				Bio: p.Bio, Role: p.Role, Created: p.Created,
				HasProfile: true, ProfileURL: url,
			}, nil
		}
		if o, err := a.modrinth.GetOrganizationProfile(authorID); err == nil && o.ID != "" {
			url := "https://modrinth.com/organization/" + o.Slug
			return BrowserAuthor{
				Source: source, AuthorID: o.ID, Username: o.Slug, Name: o.Name,
				Avatar: o.IconURL, Bio: o.Description, Created: o.Created,
				IsOrg: true, HasProfile: true, ProfileURL: url,
			}, nil
		}
		// Не знайшли — мінімальний профіль з fallback-імені (щоб клік по
		// автору не падав, а показав сторінку з поміткою).
		return BrowserAuthor{Source: source, AuthorID: authorID, Username: fallbackName, Name: fallbackName, HasProfile: false}, nil

	case "curseforge":
		// CurseForge не має профілю користувача (жодного /users endpoint).
		// Повертаємо мінімальний профіль: ім'я з проєкту + посилання на
		// профіль на сайті (якщо відоме з authors[].url).
		return BrowserAuthor{
			Source: source, AuthorID: authorID, Username: fallbackName,
			Name: fallbackName, HasProfile: false,
			ProfileURL: "https://www.curseforge.com/members/" + fallbackName + "/projects",
		}, nil

	default:
		return BrowserAuthor{}, fmt.Errorf("невідоме джерело: %s", source)
	}
}

// GetBrowserAuthorProjects повертає всі проєкти автора (для сторінки
// автора) у вигляді карток браузера.
func (a *App) GetBrowserAuthorProjects(source, authorID string) []model.BrowserEntry {
	switch source {
	case "modrinth":
		if projects, err := a.modrinth.GetUserProjects(authorID); err == nil && len(projects) > 0 {
			return modrinthUserProjectsToEntries(projects)
		}
		if projects, err := a.modrinth.GetOrganizationProjects(authorID); err == nil {
			return modrinthUserProjectsToEntries(projects)
		}
		return nil

	case "curseforge":
		if !custompack.CurseForgeAvailable() {
			return nil
		}
		id, err := strconv.Atoi(authorID)
		if err != nil {
			return nil
		}
		projects, err := a.cfClient().SearchProjectsByAuthor(id, "downloads", 50)
		if err != nil {
			return nil
		}
		out := make([]model.BrowserEntry, 0, len(projects))
		for _, p := range projects {
			entry := custompack.CFToBrowserEntry(p, projectTypeFromCFClass(p.ClassID))
			if len(p.Authors) > 0 {
				entry.Author = p.Authors[0].Name
			}
			out = append(out, entry)
		}
		return out

	default:
		return nil
	}
}

// modrinthUserProjectsToEntries конвертує проєкти автора у картки браузера
// (той самий формат, що пошук — фронт рендерить єдиною сіткою).
func modrinthUserProjectsToEntries(projects []api.ModrinthUserProject) []model.BrowserEntry {
	out := make([]model.BrowserEntry, 0, len(projects))
	for _, p := range projects {
		entry := model.BrowserEntry{
			ID:          p.Slug,
			ProjectID:   p.ID,
			Name:        p.Title,
			Type:        p.ProjectType,
			Description: p.Description,
			Downloads:   p.Downloads,
			Followers:   p.Follows,
			IconURL:     p.IconURL,
			Source:      "modrinth",
			Author:      p.Author,
		}
		if len(p.Versions) > 0 {
			entry.MCVersion = p.Versions[0]
		}
		out = append(out, entry)
	}
	return out
}
