// Package ai — клієнт Google Gemini API для аналізу крашів гри.
//
// Архітектура: ВЕСЬ аналіз виконує виключно Gemini. Машинний/локальний
// детектор (crashdiag) видалено повністю — лаунчер лише збирає для ШІ
// дані (crash-report або останні значущі рядки консолі) і передає їх у
// промт. Тип проблеми (kind), факти, причину та рекомендовану дію модель
// визначає САМА з наданого логу.
//
// Моделі: спершу розумна й дешева gemini-2.5-flash (високі ліміти
// безкоштовного тарифу — не "з'їсть" ліміти за пару крашів), потім
// gemini-2.5-flash-lite (найдешевша), і лише потім gemini-2.5-pro.
// Fallback-ланцюжок виконується ПОСЛІДОВНО (не паралельно): якщо одна
// модель недоступна/перевищила ліміт — пробується наступна.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"shaurma-launcher-backend/internal/model"
)

const geminiEndpoint = "https://generativelanguage.googleapis.com/v1beta/models"

// DefaultModels — порядок спроб моделей (послідовно, перша успішна виграє).
// Лише БЕЗКОШТОВНІ/дешеві: gemini-2.5-pro тут НАВМИСНО немає — це платна
// модель, а консоль має працювати на безкоштовному тарифі (Groq першим,
// Gemini flash/lite фолбеком).
//
// ПОРЯДОК = за щедрістю безкоштовних лімітів (щоб рідше впиратись у
// rate limit): спершу Lite-моделі — у них значно більші квоти (напр. на
// безкоштовному тарифі gemini-3.x-flash-lite: 15 RPM / 500 запитів на
// день, тоді як звичайні flash: 5 RPM / 20 на день). Новіші flash-lite
// ставляться перед старішими — якість вища, ліміти ті самі. Звичайні
// flash лишаються фолбеком на випадок, якщо lite недоступна (інша
// версія API/регіон) — їх теж кілька (від 3.7 до 2.5), щоб збільшити
// шанс знайти вільну від rate limit модель у межах загального дедлайну.
//
// "gemini-3-flash" (без версійного суфікса) прибрано зі списку — це
// НІКОЛИ не було валідним id: реальна публічна модель називалась
// "gemini-3-flash-preview", і вона сама вже deprecated (офіційна
// міграція Google — "gemini-3-flash-preview → gemini-3.5-flash", який
// у списку вже є нижче). Бита строка не просто марна: у ланцюжку
// послідовних спроб (перша успішна виграє) вона гарантовано повертала
// 404 і зʼїдала один HTTP round-trip щоразу, коли до неї доходила черга
// — на повільному з'єднанні це відчутна затримка перед тим, як консоль
// нарешті дістається робочої моделі нижче. Якщо колись здасться, що
// "модель недоступна" при аналізі краша — це перша підозра: список
// нижче застарів відносно реального каталогу Gemini API, чинні id варто
// звірити на https://ai.google.dev/gemini-api/docs/changelog і
// https://ai.google.dev/gemini-api/docs/models перед тим, як просто
// додавати ще один fallback наосліп.
var DefaultModels = []string{
	"gemini-3.5-flash-lite", // 15 RPM / 500 RPD — найбільші ліміти
	"gemini-3.1-flash-lite", // 15 RPM / 500 RPD
	"gemini-2.5-flash-lite", // 10 RPM / 20 RPD (стара, але стабільна)
	"gemini-3.7-flash",      // 5 RPM / 20 RPD (найновіший flash)
	"gemini-3.6-flash",      // 5 RPM / 20 RPD
	"gemini-3.5-flash",      // 5 RPM / 20 RPD
	"gemini-2.5-flash",      // 5 RPM / 20 RPD (останній фолбек)
}

// DefaultAPIKey — ключ API за замовчуванням, вбудований у backend.dll на
// етапі збірки через -ldflags (-X ...internal/ai.DefaultAPIKey=...).
// ВИХІДНИЙ КОД НІКОЛИ НЕ МІСТИТЬ САМОГО КЛЮЧА (тільки параметр збірки
// build.ps1 -GeminiApiKey / env GEMINI_API_KEY) — щоб ключ не потрапив у
// публічний репозиторій. Завжди можна перевизначити через змінну
// середовища GEMINI_API_KEY або файл <configDir>/gemini.key.
var DefaultAPIKey string

// Client — HTTP-клієнт Gemini.
type Client struct {
	apiKey  string
	models  []string
	http    *http.Client
	timeout time.Duration
}

// NewClient створює клієнт. apiKey="" → брати з env GEMINI_API_KEY, потім
// з файлу <configDir>/gemini.key, потім DefaultAPIKey. models=nil →
// DefaultModels.
func NewClient(apiKey, configDir string, models []string) *Client {
	if apiKey == "" {
		apiKey = ResolveAPIKey(configDir)
	}
	if len(models) == 0 {
		models = DefaultModels
	}
	return &Client{
		apiKey:  apiKey,
		models:  models,
		http:    &http.Client{Timeout: 20 * time.Second},
		timeout: 15 * time.Second, // таймаут запиту (спінер у UI не висить вічно)
	}
}

// ResolveAPIKey визначає ключ: env GEMINI_API_KEY → файл gemini.key →
// вбудований дефолт.
func ResolveAPIKey(configDir string) string {
	if k := os.Getenv("GEMINI_API_KEY"); k != "" {
		return strings.TrimSpace(k)
	}
	if configDir != "" {
		if data, err := os.ReadFile(filepath.Join(configDir, "gemini.key")); err == nil {
			if k := strings.TrimSpace(string(data)); k != "" {
				return k
			}
		}
	}
	return DefaultAPIKey
}

// SaveAPIKey записує ключ у <configDir>/gemini.key (щоб не хардкодити у
// конфізі користувача і мати змогу його змінити).
func SaveAPIKey(configDir, key string) error {
	if configDir == "" || key == "" {
		return nil
	}
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(configDir, "gemini.key"), []byte(strings.TrimSpace(key)), 0600)
}

// orNone — плейсхолдер для порожніх полів контексту збірки.
func orNone(v string) string {
	if strings.TrimSpace(v) == "" {
		return "(не вказано)"
	}
	return v
}

// CrashContext — дані про збірку, які бекенд підкладає у промт ШІ разом
// із самим логом: версія гри, лоадер (+версія), список модів і типи
// кнопок-дій, доступних у консолі лаунчера. Модель має будувати діагноз
// (конфлікти, залежності, версійні несумісності), спираючись на реальні
// дані збірки, а рекомендовану дію обирати ЛИШЕ серед AvailableActions —
// інакше консоль не зможе виконати рекомендацію (ApplyAIFix розуміє лише
// increase_ram | disable_mod | download_dependency | repair).
type CrashContext struct {
	LogWindow        string   // crash-report АБО останні значущі рядки консолі
	MCVersion        string   // версія Minecraft, напр. "1.21.1"
	Loader           string   // fabric | forge | neoforge | quilt | vanilla
	LoaderVersion    string   // версія лоадера, напр. "0.16.9"
	Mods             []string // файли модів з теки mods/ збірки
	AvailableActions []string // типи кнопок-дій консолі (див. вище)
	// HasCrashReport — чи знайдено crash-report файл Minecraft (або маркер
	// "Game crashed!" у логу). Без нього ШІ НЕ має права стверджувати, що
	// гра технічно впала: вихід із ненульовим кодом буває і без крашу.
	HasCrashReport bool
	// HasErrorLines — чи є у логу консолі рядки рівня ERROR (exception /
	// stack trace тощо). Якщо немає — у логу ЛИШЕ попередження WARN, і це
	// НЕ привід звинувачувати моди.
	HasErrorLines bool
}

// AnalyzeCrash викликає Gemini з системним промтом, парсить структуровану
// JSON-відповідь (kind/cause/facts/recommendedAction модель визначає САМА
// з наданих даних). Проходить моделями по черзі; якщо всі недоступні —
// повертає помилку з поясненням (фронтенд покаже помилку з кнопкою
// «Повторити»).
func (c *Client) AnalyzeCrash(ctx context.Context, lang string, cc CrashContext) (model.AIResult, error) {
	if c.apiKey == "" {
		return model.AIResult{}, fmt.Errorf("Gemini API ключ не задано")
	}
	prompt := buildSystemPrompt(lang, cc)

	// Загальний дедлайн на ВЕСЬ ланцюжок фолбеків (8 моделей × 15с таймаут
	// кожної дали б до 120с очікування). 25с — щоб кнопка «Проаналізувати»
	// не висіла вічно: після цього фронтенд показує базовий локальний аналіз.
	// Ланцюжок упорядкований так, що найщедріші за лімітами моделі йдуть
	// першими, тож на практиці успіх зазвичай настає з першої-другої спроби.
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()

	var lastErr error
	for _, m := range c.models {
		res, err := c.generateContent(ctx, m, prompt)
		if err == nil {
			res.Model = m
			return res, nil
		}
		lastErr = err
	}
	return model.AIResult{}, fmt.Errorf("всі моделі Gemini недоступні: %w", lastErr)
}

// generateContent робить один запит generateContent до конкретної моделі.
func (c *Client) generateContent(ctx context.Context, modelName, prompt string) (model.AIResult, error) {
	reqCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	body := map[string]any{
		"contents": []map[string]any{
			{"parts": []map[string]any{{"text": prompt}}},
		},
		"generationConfig": map[string]any{
			"temperature":     0.2,
			"maxOutputTokens": 1024,
		},
	}
	payload, _ := json.Marshal(body)

	url := fmt.Sprintf("%s/%s:generateContent?key=%s", geminiEndpoint, modelName, c.apiKey)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return model.AIResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return model.AIResult{}, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		// 401 = ключ недійсний/відкликаний — розбірливе повідомлення
		// замість сирого JSON від Google (користувач бачив у консолі
		// «Request had invalid authentication credentials...» без жодної
		// підказки, що робити). 429 = ліміт, інші 4xx = модель недоступна
		// — тих це не стосується, лишаємо короткий технічний опис.
		if resp.StatusCode == http.StatusUnauthorized {
			return model.AIResult{}, fmt.Errorf("недійсний Gemini API-ключ (HTTP 401). Перевірте власний ключ у Налаштуваннях → Консоль (поле «Gemini API ключ») або файл gemini.key")
		}
		// 429 = ліміт, 4xx = модель недоступна/ключ — пробуємо наступну.
		return model.AIResult{}, fmt.Errorf("Gemini %s HTTP %d: %s", modelName, resp.StatusCode, truncate(string(data), 200))
	}

	var geminiResp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(data, &geminiResp); err != nil {
		return model.AIResult{}, fmt.Errorf("Gemini відповідь не JSON: %w", err)
	}
	if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return model.AIResult{}, fmt.Errorf("Gemini %s: порожня відповідь", modelName)
	}
	text := geminiResp.Candidates[0].Content.Parts[0].Text

	// Gemini іноді обгортає JSON у markdown-код-блок — стрипаємо.
	text = stripMarkdownFence(text)
	if text == "" {
		return model.AIResult{}, fmt.Errorf("Gemini %s: порожній текст відповіді", modelName)
	}

	var result model.AIResult
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return model.AIResult{}, fmt.Errorf("Gemini %s: не вдалось розпарсити JSON: %w", modelName, err)
	}
	return result, nil
}

// buildSystemPrompt будує промт: ШІ отримує сирі дані (crash-report АБО
// останні значущі рядки консолі) + контекст збірки (версія гри, лоадер,
// список модів, доступні кнопки-дії) і визначає ВСЕ САМ — тип проблеми,
// факти, причину, рекомендовану дію. Ніякого машинного аналізу на бекенді
// більше немає.
func buildSystemPrompt(lang string, cc CrashContext) string {
	languageName := map[string]string{
		"uk": "українська",
		"en": "англійська",
	}[lang]
	if languageName == "" {
		languageName = "українська"
	}
	logWindow := cc.LogWindow
	if logWindow == "" {
		logWindow = "(немає даних)"
	}
	// Контекст збірки для ШІ: версія гри/лоадера та список модів. Це
	// дає моделі реальні факти (конфлікти модів, залежності, версійна
	// сумісність), а не здогади з порожнього логу.
	ctxBlock := "Контекст збірки:"
	ctxBlock += "\n- Версія Minecraft: " + orNone(cc.MCVersion)
	ctxBlock += "\n- Завантажувач: " + orNone(strings.TrimSpace(cc.Loader+" "+cc.LoaderVersion))
	if len(cc.Mods) > 0 {
		ctxBlock += "\n- Встановлені моди (" + fmt.Sprintf("%d", len(cc.Mods)) + "):"
		for _, m := range cc.Mods {
			ctxBlock += "\n  - " + m
		}
	} else {
		ctxBlock += "\n- Встановлені моди: (не виявлені — можливо, тека mods порожня)"
	}
	// Доступні кнопки-дії: модель має обирати recommendedAction.type ЛИШЕ
	// серед них (решта значень бекенд не зможе виконати в консолі).
	actions := strings.TrimSpace(strings.Join(cc.AvailableActions, ", "))
	if actions == "" {
		actions = "none"
	}
	ctxBlock += "\n- Кнопки-дії, доступні у консолі лаунчера (обирай recommendedAction.type ЛИШЕ серед них): " + actions
	// Факти про «чи справді це краш» — головний захист від хибних діагнозів:
	// без crash-report і без ERROR-рядків у логу ШІ має повернути UNKNOWN,
	// а не вигадувати винного мода з WARN-попереджень.
	if cc.HasCrashReport {
		ctxBlock += "\n- Crash-report Minecraft: ЗНАЙДЕНО (у даних вище повний текст краш-репорту)"
	} else {
		ctxBlock += "\n- Crash-report Minecraft: НЕ ЗНАЙДЕНО (у даних вище лише рядки консолі, не crash-report)"
	}
	if cc.HasErrorLines {
		ctxBlock += "\n- Рядки ERROR у консолі: Є (у даних вище присутні помилки/винятки)"
	} else {
		ctxBlock += "\n- Рядки ERROR у консолі: НЕМАЄ (у даних вище лише INFO/WARN-попередження, жодного винятку)"
	}

	return fmt.Sprintf(`Ти — модуль діагностики крашів лаунчера Minecraft "Shaurma Launcher".
Відповідай ВИКЛЮЧНО валідним JSON без жодного тексту навколо, мовою: %s.
(Уся cause/explanation/facts/recommendedAction пишуться цією мовою, незалежно від мови вхідних даних логу.)

Визнач тип проблеми САМ за наведеними нижче даними:
- MISSING_DEPENDENCY — мод вимагає залежність, якої немає в грі
- MOD_CONFLICT — два або більше модів конфліктують між собою
- SINGLE_MOD_ERROR — помилка локалізована в одному моді
- ENV_CORRUPTION — пошкоджені/відсутні файли оточення (Java, нативки, асети, jar, бібліотеки)
- JAVA_VERSION — класи збірки скомпільовані для новішої Java, ніж запущена ("Unsupported class file major version" / UnsupportedClassVersionError)
- UNKNOWN — причину не вдалось визначити

Відомості про збірку (версія гри/лоадера, встановлені моди — це РЕАЛЬНІ факти, на них можна спиратись):
---
%s
---

Дані для аналізу (повний crash-report АБО останні значущі рядки консолі):
---
%s
---

Поверни JSON СУВОРО такої форми:
{
  "kind": "один із типів вище",
  "cause": "коротке речення, суть проблеми, без води",
  "explanation": "1-2 речення технічного пояснення чому це сталось",
  "facts": ["факт 1", "факт 2"],
  "actionable": true | false,
  "recommendedAction": {
    "type": "increase_ram" | "disable_mod" | "download_dependency" | "repair" | "none",
    "targetMod": "ім'я файлу мода (для disable_mod) або null",
    "dependencyName": "назва залежності (для download_dependency) або null",
    "suggestedRamMb": число або null
  },
  "copyText": "компактний одно-два-рядковий технічний текст без води, придатний для вставки в баг-репорт: тип винятку + ключовий файл/причина, БЕЗ пояснень і рекомендацій"
}

Правила:
- Якщо KIND == MISSING_DEPENDENCY: recommendedAction.type = "download_dependency", dependencyName = точна назва залежності з даних.
- Якщо KIND == MOD_CONFLICT: recommendedAction.type = "disable_mod", targetMod = один із конфліктуючих файлів (обери найімовірнішого винуватця).
- Якщо KIND == SINGLE_MOD_ERROR і причина схожа на нестачу пам'яті (OutOfMemoryError/"heap space"/GC overhead): recommendedAction.type = "increase_ram", suggestedRamMb = число (рекомендація), не менше 6144.
- Якщо KIND == ENV_CORRUPTION (пошкоджені/відсутні файли оточення: Java, нативки, асети, jar, бібліотеки): recommendedAction.type = "repair", actionable = true. У explanation ОБОВ'ЯЗКОВО напиши, що лаунчер має кнопку «Полагодити» (перевірка цілісності файлів гри та Java, докачування лише пошкодженого), і що цю кнопку можна натиснути ПРЯМО в консолі (кнопка нижче) або в розділі «Продуктивність» на сторінці збірки; світи, моди та налаштування не постраждають.
- Якщо KIND == JAVA_VERSION (застаріла Java: "Unsupported class file major version" або UnsupportedClassVersionError — класи збірки скомпільовані для новішої Java): actionable = false, recommendedAction.type = "none". У explanation поясни, що треба оновити/змінити Java на новішу (Налаштування лаунчера → Java), вказавши потрібну версію, якщо вона є в логу.
- Якщо KIND == UNKNOWN або жодна дія не підходить: actionable = false, recommendedAction.type = "none". У цьому разі explanation — теоретичне пояснення можливої причини БЕЗ вигаданих конкретних кроків.
- КРИТИЧНО: попередження (WARN) — це НЕ краш. Якщо crash-report НЕ ЗНАЙДЕНО І в логу немає жодного ERROR-рядка (лише INFO/WARN): гра завершилась без технічної аварії. Тоді kind = "UNKNOWN", actionable = false, recommendedAction.type = "none", targetMod/dependencyName = null, а в cause та explanation чесно напиши, що в логу лише попередження і явної причини крашу не видно. КАТЕГОРИЧНО заборонено у цьому випадку призначати винного мода чи будь-яку дію.
- Ніколи не звинувачуй мод (targetMod, cause, facts) лише тому, що він написав WARN-попередження або згаданий у звичайному логу. Щоб назвати мод винуватцем, у даних МАЄ бути явний exception або stack trace, що вказує на його клас/файл.
- copyText заповнюй завжди, незалежно від actionable.
- Не вигадуй назви модів, класів чи залежностей, яких немає в даних.`, languageName, ctxBlock, logWindow)
}

// stripMarkdownFence прибирає ```json ... ``` обгортку, якщо вона є.
func stripMarkdownFence(text string) string {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "```") {
		if idx := strings.Index(text, "\n"); idx >= 0 {
			text = text[idx+1:]
		}
		text = strings.TrimSuffix(text, "```")
	}
	return strings.TrimSpace(text)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
