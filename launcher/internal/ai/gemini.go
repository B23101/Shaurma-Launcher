// Package ai — клієнт Google Gemini API для аналізу крашів гри.
//
// Архітектура (як у старому лаунчері): ЛОКАЛЬНИЙ детектор (crashdiag)
// визначає тип проблеми і витягує факти — Gemini лише пише людське
// пояснення обраною мовою та уточнює структуровані поля для UI. Тому тут
// немає "вигадування причини з нуля" — модель отримує готові факти.
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

	"shaurma-launcher-wails/internal/model"
)

const geminiEndpoint = "https://generativelanguage.googleapis.com/v1beta/models"

// DefaultModels — порядок спроб моделей (послідовно, перша успішна виграє).
var DefaultModels = []string{
	"gemini-2.5-flash",
	"gemini-2.5-flash-lite",
	"gemini-2.5-pro",
}

// DefaultAPIKey — ключ API за замовчуванням (вбудований у збірку). Завжди
// можна перевизначити через змінну середовища GEMINI_API_KEY або файл
// <configDir>/gemini.key.
const DefaultAPIKey = "AQ.Ab8RN6LEjOac3Aaw9wxH8QziZ0DbRZT2COJ57HcrBR7lx-kGLg"

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

// AnalyzeCrash викликає Gemini з системним промтом (див. ТЗ §2.2), парсить
// структуровану JSON-відповідь. Проходить моделями по черзі; якщо всі
// недоступні — повертає помилку з поясненням (фронтенд покаже базовий
// аналіз від локального детектора).
func (c *Client) AnalyzeCrash(ctx context.Context, lang string, kind string, factsJSON string, logWindow string) (model.AIResult, error) {
	if c.apiKey == "" {
		return model.AIResult{}, fmt.Errorf("Gemini API ключ не задано")
	}
	prompt := buildSystemPrompt(lang, kind, factsJSON, logWindow)

	// Загальний дедлайн на ВЕСЬ ланцюжок фолбеків (3 моделі × 15с таймаут
	// кожної дали б до 45с очікування). 25с — щоб кнопка «Проаналізувати»
	// не висіла вічно: після цього фронтенд показує базовий локальний аналіз.
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

// buildSystemPrompt будує промт за шаблоном ТЗ §2.2.
func buildSystemPrompt(lang, kind, factsJSON, logWindow string) string {
	languageName := map[string]string{
		"uk": "українська",
		"en": "англійська",
	}[lang]
	if languageName == "" {
		languageName = "українська"
	}
	if logWindow == "" {
		logWindow = "(немає даних)"
	}
	if factsJSON == "" {
		factsJSON = "{}"
	}
	return fmt.Sprintf(`Ти — модуль діагностики крашів лаунчера Minecraft "Shaurma Launcher".
Відповідай ВИКЛЮЧНО валідним JSON без жодного тексту навколо, мовою: %s.
(Уся cause/explanation/facts/recommendedAction пишуться цією мовою, незалежно від мови вхідних даних логу.)

Локальний аналізатор лаунчера вже визначив тип проблеми: %s
(один з: MISSING_DEPENDENCY, MOD_CONFLICT, SINGLE_MOD_ERROR, ENV_CORRUPTION, UNKNOWN)

Вже витягнуті факти від аналізатора (довіряй їм, не суперечь):
%s

Повний crash-report АБО останні рядки консолі (якщо звіту немає):
---
%s
---

Поверни JSON СУВОРО такої форми:
{
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
- Якщо KIND == MISSING_DEPENDENCY: recommendedAction.type = "download_dependency", dependencyName = точна назва з фактів аналізатора.
- Якщо KIND == MOD_CONFLICT: recommendedAction.type = "disable_mod", targetMod = один із конфліктуючих файлів (обери найімовірнішого винуватця).
- Якщо KIND == SINGLE_MOD_ERROR і причина схожа на нестачу пам'яті (OutOfMemoryError/"heap space"/GC overhead): recommendedAction.type = "increase_ram", suggestedRamMb = число (рекомендація), не менше 6144.
- Якщо KIND == ENV_CORRUPTION (пошкоджені/відсутні файли оточення: Java, нативки, асети, jar, бібліотеки): recommendedAction.type = "repair", actionable = true. У explanation ОБОВ'ЯЗКОВО напиши, що лаунчер має кнопку «Полагодити» (перевірка цілісності файлів гри та Java, докачування лише пошкодженого), і що цю кнопку можна натиснути ПРЯМО в консолі (кнопка нижче) або в розділі «Продуктивність» на сторінці збірки; світи, моди та налаштування не постраждають.
- Якщо KIND == UNKNOWN або жодна дія не підходить: actionable = false, recommendedAction.type = "none". У цьому разі explanation — теоретичне пояснення можливої причини БЕЗ вигаданих конкретних кроків.
- copyText заповнюй завжди, незалежно від actionable.
- Не вигадуй назви модів чи залежностей, яких немає у фактах чи в логу.`, languageName, kind, factsJSON, logWindow)
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
