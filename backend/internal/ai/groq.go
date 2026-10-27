// Package ai — клієнти LLM для лаунчера.
//
// groq.go — клієнт Groq (api.groq.com, OpenAI-сумісний формат
// /chat/completions) для ШІ-функцій браузера контенту:
//   - переклад описів проєктів зі збереженням markdown/HTML розмітки
//     (кольори, посилання, код лишаються як в оригіналі — перекладається
//     лише текст);
//   - відгук про проєкт («чи вартий він уваги»): модель отримує те, що
//     бачить користувач (назва, опис, статистика, категорії) + результати
//     веб-пошуку і пише людською мовою розгорнутий відгук.
//
// Ключ: env GROQ_API_KEY → файл <configDir>/groq.key → вбудований дефолт
// (той самий підхід, що gemini.key для аналізу крашів).
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
	"regexp"
	"strconv"
	"strings"
	"time"

	"shaurma-launcher-backend/internal/model"
)

const groqEndpoint = "https://api.groq.com/openai/v1/chat/completions"

// DefaultGroqAPIKey — ключ за замовчуванням, вбудований у backend.dll на
// етапі збірки через -ldflags (-X ...internal/ai.DefaultGroqAPIKey=...).
// ВИХІДНИЙ КОД НІКОЛИ НЕ МІСТИТЬ САМОГО КЛЮЧА (тільки параметр збірки
// build.ps1 -GroqApiKey / env GROQ_API_KEY) — щоб ключ не потрапив у
// публічний репозиторій. Перевизначається також env GROQ_API_KEY або
// файлом groq.key.
var DefaultGroqAPIKey string

// DefaultGroqModels — моделі Groq для перекладу/відгуків, у порядку
// спроб (перша успішна виграє, той самий патерн, що DefaultModels у
// gemini.go). ОДНА жорстко закодована модель без fallback — крихко:
// Groq регулярно деактивує старі моделі (задокументована політика,
// https://console.groq.com/docs/deprecations), і виклик до знятої з
// продакшену моделі повертає HTTP 400 model_decommissioned одразу для
// КОЖНОГО запиту — саме так одного разу llama-3.3-70b-versatile (був тут
// єдиною жорстко закодованою моделлю) перестав працювати повністю: не
// rate limit, який ретраїться сам, а постійна відмова, що виглядає як
// «немає доступної моделі» на кожному виклику ШІ-функцій браузера.
// openai/gpt-oss-120b — офіційна рекомендована заміна llama-3.3-70b-
// versatile (Groq deprecation notice, червень 2026); qwen/qwen3.6-27b —
// другий офіційний варіант заміни, лишений тут другим фолбеком.
//
// Якщо знову побачиш "немає доступної моделі" від Groq — це перша
// підозра: звір актуальний список на
// https://console.groq.com/docs/models і
// https://console.groq.com/docs/deprecations, а не одразу дописуй ще
// одну модель наосліп.
var DefaultGroqModels = []string{
	"openai/gpt-oss-120b",
	"qwen/qwen3.6-27b",
}

// DefaultGroqModel лишено для сумісності (напр. якщо десь у коді
// звертались до самої константи напряму) — це перший елемент
// DefaultGroqModels.
const DefaultGroqModel = "openai/gpt-oss-120b"

// GroqClient — HTTP-клієнт Groq Chat Completions.
type GroqClient struct {
	apiKey  string
	models  []string
	http    *http.Client
	timeout time.Duration
}

// NewGroqClient створює клієнт. apiKey="" → ResolveGroqAPIKey, потім
// DefaultGroqAPIKey. models=nil → DefaultGroqModels.
func NewGroqClient(apiKey, configDir string, models []string) *GroqClient {
	if apiKey == "" {
		apiKey = ResolveGroqAPIKey(configDir)
	}
	if apiKey == "" {
		apiKey = DefaultGroqAPIKey
	}
	if len(models) == 0 {
		models = DefaultGroqModels
	}
	return &GroqClient{
		apiKey:  apiKey,
		models:  models,
		http:    &http.Client{Timeout: 60 * time.Second},
		timeout: 55 * time.Second,
	}
}

// ModelName повертає назву моделі, яку клієнт спробує першою (для показу
// в UI консолі: «Модель: groq (openai/gpt-oss-120b)»). Якщо перша модель
// у списку недоступна, реальний запит (chat) мовчки перейде на наступну
// — цей рядок лишається орієнтовним, не гарантією, що саме ця модель
// відповіла.
func (c *GroqClient) ModelName() string {
	if len(c.models) == 0 {
		return ""
	}
	return c.models[0]
}

// ResolveGroqAPIKey визначає ключ: env GROQ_API_KEY → файл groq.key →
// порожньо (тоді NewGroqClient підставить вбудований дефолт).
func ResolveGroqAPIKey(configDir string) string {
	if k := os.Getenv("GROQ_API_KEY"); k != "" {
		return strings.TrimSpace(k)
	}
	if configDir != "" {
		if data, err := os.ReadFile(filepath.Join(configDir, "groq.key")); err == nil {
			if k := strings.TrimSpace(string(data)); k != "" {
				return k
			}
		}
	}
	return ""
}

// retryAfterRe — «Please try again in 14.535s» у тілі 429-відповіді Groq
// (Retry-After-заголовок Groq іноді не надсилає, лише текст помилки).
var retryAfterRe = regexp.MustCompile(`try again in (\d+(?:\.\d+)?)\s*s`)

// chat робить запит chat/completions з ретраєм на 429 (rate limit) і
// повертає текст відповіді. jsonMode=true → response_format={"type":"json_object"}:
// модель ЗМУШЕНА повернути валідний JSON (Groq гарантує синтаксис) — для
// перекладу, де невалідний JSON перетворювався на сирий текст з дужками у
// тілі опису.
//
// Безкоштовний тариф Groq періодично кидає 429 з повідомленням
// «Please try again in Xs» — раніше функція падала з першого ж ліміту, і
// помилка йшла в журнал лаунчера. Тепер чекаємо вказаний час (Retry-After
// із заголовка або з тексту помилки, максимум 15с) і пробуємо ще раз — до
// 3 спроб загалом, з повагою до дедлайну контексту.
//
// Моделі з c.models перебираються ПОСЛІДОВНО (перша успішна виграє, той
// самий принцип, що Client.Complete у gemini.go) — якщо модель зняли з
// продакшену (model_decommissioned, HTTP 400), c.chatOnce повертає
// decommissioned=true і chat одразу переходить до наступної моделі БЕЗ
// витрачання спроб-ретраїв на неї (немає сенсу повторювати запит до
// моделі, яка ніколи не відповість успіхом).
func (c *GroqClient) chat(ctx context.Context, system, user string, maxTokens int, jsonMode bool) (string, error) {
	if c.apiKey == "" {
		return "", fmt.Errorf("Groq API ключ не задано")
	}
	if len(c.models) == 0 {
		return "", fmt.Errorf("Groq: список моделей порожній")
	}
	reqCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	var lastErr error
	for _, m := range c.models {
		body := map[string]any{
			"model": m,
			"messages": []map[string]string{
				{"role": "system", "content": system},
				{"role": "user", "content": user},
			},
			"temperature":           0.4,
			"max_completion_tokens": maxTokens,
			"stream":                false,
		}
		if jsonMode {
			body["response_format"] = map[string]string{"type": "json_object"}
		}
		payload, _ := json.Marshal(body)

		const maxAttempts = 3
		for attempt := 1; attempt <= maxAttempts; attempt++ {
			out, retryAfter, decommissioned, err := c.chatOnce(reqCtx, payload)
			if err == nil {
				return out, nil
			}
			lastErr = err
			if decommissioned {
				// Ця модель більше не обслуговується Groq — жодна
				// кількість ретраїв цього не змінить, переходимо до
				// наступної моделі зі списку негайно.
				break
			}
			// 429 — rate limit: чекаємо і повторюємо (крім останньої спроби).
			if retryAfter <= 0 || attempt == maxAttempts {
				break
			}
			wait := retryAfter
			if wait > 15*time.Second {
				wait = 15 * time.Second
			}
			select {
			case <-time.After(wait):
			case <-reqCtx.Done():
				return "", lastErr
			}
		}
	}
	return "", lastErr
}

// chatOnce виконує ОДИН запит chat/completions. При 429 повертає також
// час очікування з Retry-After (заголовок або «try again in Xs» з тіла)
// — нуль означає «ретраїти не можна». decommissioned=true — модель знята
// з продакшену Groq (код помилки model_decommissioned у тілі відповіді);
// у цьому разі retryAfter не має значення, chat() одразу переходить до
// наступної моделі зі списку.
func (c *GroqClient) chatOnce(reqCtx context.Context, payload []byte) (out string, retryAfter time.Duration, decommissioned bool, err error) {
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, groqEndpoint, bytes.NewReader(payload))
	if err != nil {
		return "", 0, false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("User-Agent", "shaurma-team/shaurma-launcher/2.0 (shaurmaofficial2210@gmail.com)")

	resp, doErr := c.http.Do(req)
	if doErr != nil {
		return "", 0, false, doErr
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusTooManyRequests {
		header := resp.Header.Get("Retry-After")
		return "", parseRetryAfter(header, string(data)), false, fmt.Errorf("Groq HTTP %d: %s", resp.StatusCode, truncate(string(data), 300))
	}
	if resp.StatusCode != http.StatusOK {
		// 401 = ключ недійсний/відкликаний — розбірливе повідомлення
		// замість сирого JSON (той самий кейс, що в Gemini-клієнті: без
		// підказки користувач не знає, що робити).
		if resp.StatusCode == http.StatusUnauthorized {
			return "", 0, false, fmt.Errorf("недійсний Groq API-ключ (HTTP 401). Перевірте власний ключ у Налаштуваннях → Консоль (поле «Gemini API ключ») або файл groq.key")
		}
		// 413 (Request Entity Too Large) — Groq-безкоштовний тариф має
		// жорсткий ліміт токенів на хвилину (TPM) НА ЗАПИТ, а не лише
		// сумарно; текст помилки від Groq — сирий JSON з назвою моделі,
		// ID організації і посиланням на платний тариф ("Upgrade to Dev
		// Tier") — усе це не має жодного сенсу для користувача лаунчера
		// і виглядає як «злам»/чужа реклама в інтерфейсі. Дані про опис
		// проєкту (переклад/туторіал з довгими веб-згадками) — головна
		// причина: контекст запиту завеликий для безкоштовного ліміту.
		if resp.StatusCode == http.StatusRequestEntityTooLarge {
			return "", 0, false, fmt.Errorf("опис проєкту завеликий для безкоштовного ліміту Groq — спробуйте пізніше або скоротіть джерело (менше веб-згадок)")
		}
		// model_decommissioned — Groq повертає це як код помилки в тілі
		// (HTTP 400), коли модель остаточно знята з обслуговування (не
		// rate limit, не тимчасова відмова). Перевіряємо підрядок у
		// сирому тілі замість повного JSON-парсингу схеми помилки —
		// формат тіла помилки Groq не задокументований настільки жорстко,
		// щоб покладатись на конкретні поля.
		if strings.Contains(string(data), "model_decommissioned") {
			return "", 0, true, fmt.Errorf("Groq HTTP %d (модель знята з обслуговування): %s", resp.StatusCode, truncate(string(data), 300))
		}
		return "", 0, false, fmt.Errorf("Groq HTTP %d: %s", resp.StatusCode, truncate(string(data), 300))
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return "", 0, false, fmt.Errorf("Groq відповідь не JSON: %w", err)
	}
	if len(parsed.Choices) == 0 || parsed.Choices[0].Message.Content == "" {
		return "", 0, false, fmt.Errorf("Groq: порожня відповідь")
	}
	return strings.TrimSpace(parsed.Choices[0].Message.Content), 0, false, nil
}

// parseRetryAfter визначає час очікування перед ретраєм: спершу заголовок
// Retry-After (цілі секунди), інакше — «try again in Xs» з тіла помилки.
// Повертає 0, якщо час визначити не вдалось (тоді не ретраїмо).
func parseRetryAfter(header, body string) time.Duration {
	if secs, err := strconv.Atoi(strings.TrimSpace(header)); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	if m := retryAfterRe.FindStringSubmatch(body); len(m) == 2 {
		if secs, err := strconv.ParseFloat(m[1], 64); err == nil && secs > 0 {
			return time.Duration(secs * float64(time.Second))
		}
	}
	return 0
}

// ── Переклад опису проєкту ───────────────────────────────────────────────
// Перекладається ЛИШЕ текст: markdown/HTML розмітка, URL посилань,
// код і зображення лишаються як в оригіналі — сторінка зберігає
// кольори/форматування/посилання у нормальному стані.

// TranslateDescription перекладає (shortDesc, body) проєкту на вказану
// мову. Повертає перекладені описи зі збереженою розміткою.
//
// Запит іде у json-режимі (response_format=json_object) — Groq гарантує
// валідний синтаксис JSON. Промт ЖОРСТКО вимагає: у body має бути ЛИШЕ
// перекладений повний опис, без дублювання короткого опису, без JSON-
// дужок, без додаткових заголовків/коментарів — інакше фінальний текст
// пахне «ШІ зробив дивно». Якщо навіть після цього JSON не розібрався —
// повертаємо ПОМИЛКУ, а не сирий текст: інакше фронт рендерив би дужки
// {"description":...} прямо в тілі опису.
func (c *GroqClient) TranslateDescription(ctx context.Context, lang, shortDesc, body string) (string, string, error) {
	// Безкоштовний тариф Groq має жорсткий ліміт ~8000 токенів НА ЗАПИТ
	// (сума system+user+відповідь), а maxTokens=8192 нижче — це ліміт лише
	// на ВІДПОВІДЬ. Дуже довгий опис проєкту (body з Modrinth/CurseForge
	// іноді на кілька тисяч слів з таблицями/галереями) сам по собі міг
	// перевищити ліміт ще до генерації відповіді → Groq повертає HTTP 413
	// ще на вхідних даних. Обрізаємо ВХІД заздалегідь до розумної межі
	// (симптоматично ~4 символи/токен) — половина туторіального опису
	// краще, ніж повна відмова перекладу через один завеликий проєкт.
	const maxBodyRunes = 12000
	if len([]rune(body)) > maxBodyRunes {
		r := []rune(body)
		body = string(r[:maxBodyRunes]) + "\n\n…(опис скорочено — джерело задовге для перекладу за один запит)"
	}
	codeMd := "`код`"
	system := fmt.Sprintf(
		`Ти — професійний перекладач описів Minecraft-контенту (модів, модпаків, ресурспаків) для лаунчера.
Перекладай текст опису мовою: %s.

ПРАВИЛА (важливо!):
1. Перекладай ЛИШЕ текст. ВСЯ розмітка markdown (##, **, *, #, - списки, > цитати, %s, кодові блоки) та HTML-теги (<p>, <b>, <a href>, <img src>, <iframe src>) мають лишитись БЕЗ ЗМІН.
2. URL посилань, шляхи до зображень, назви файлів, версії модів, команди в коді і значення src/атрибутів тегів — НЕ перекладай.
3. Не змінюй структуру: кількість абзаців, списків, заголовків, зображень, відео — та сама.
4. Поверни ВИКЛЮЧНО валідний JSON без тексту навколо, у такій формі:
{"description": "<перекладений короткий опис>", "body": "<перекладений повний опис зі збереженою розміткою>"}
5. У JSON-значеннях використовуй ЕКРАНОВАНІ переноси (\n), а не реальні переводи рядків. Екранування лапок і спецсимволів усередині рядків обробить JSON-режим сам — тобі достатньо писати звичайний текст.
6. КАТЕГОРИЧНО заборонено: у "body" НЕ додавай короткий опис окремо, НЕ вставляй JSON-дужки {}, НЕ додавай заголовків типу «Переклад:», «Опис:», НЕ коментуй і не пояснюй. body = ПЕРЕКЛАДЕНА КОПІЯ повного опису, і тільки вона.

Якщо короткий опис порожній — у "description" поверни порожній рядок.`, lang, codeMd)
	user := fmt.Sprintf("Короткий опис:\n---\n%s\n---\n\nПовний опис (markdown/HTML):\n---\n%s\n---\n\nПереклади та поверни JSON. Пам'ятай: body має бути чистим перекладом повного опису, без короткого опису в ньому.", shortDesc, body)
	out, err := c.chat(ctx, system, user, 4096, true)
	if err != nil {
		return "", "", err
	}
	var parsed struct {
		Description string `json:"description"`
		Body        string `json:"body"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err == nil && parsed.Body != "" {
		return parsed.Description, parsed.Body, nil
	}
	// Модель могла додати текст навколо JSON (рідко, але буває навіть у
	// json-режимі) — пробуємо витягти об'єкт із будь-якого місця відповіді.
	if idx := extractJSONObject(out); idx >= 0 {
		if err := json.Unmarshal([]byte(idxText(out, idx)), &parsed); err == nil && parsed.Body != "" {
			return parsed.Description, parsed.Body, nil
		}
	}
	// Нічого не вдалось розібрати — помилка, а не сирий текст з дужками.
	return "", "", fmt.Errorf("перекладач не повернув валідний JSON: %s", truncate(out, 200))
}

// ── Аналіз крашу (консоль) — БЕЗКОШТОВНА альтернатива Gemini ────────────
// Groq (llama-3.3-70b-versatile) доступний на безкоштовному тарифі і
// обробляє той самий структурований JSON-промт, що Gemini. Консоль
// використовує Groq ПЕРШИМ (бо безкоштовно і ключ уже є у користувача),
// а Gemini лишається фолбеком. json-режим гарантує валідний JSON.

// AnalyzeCrash викликає Groq з тим самим системним промтом, що Gemini
// (buildSystemPrompt), і парсить ту саму структуру model.AIResult.
func (c *GroqClient) AnalyzeCrash(ctx context.Context, lang string, cc CrashContext) (model.AIResult, error) {
	if c.apiKey == "" {
		return model.AIResult{}, fmt.Errorf("Groq API ключ не задано")
	}
	prompt := buildSystemPrompt(lang, cc)

	// Загальний дедлайн на весь ланцюжок (як у Gemini-клієнта) — щоб
	// кнопка «Проаналізувати» не висіла вічно.
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()

	out, err := c.chat(ctx, prompt, "Поверни JSON діагнозу суворо у формі, описаній у системному промті.", 1200, true)
	if err != nil {
		return model.AIResult{}, err
	}

	var result model.AIResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		// Модель могла додати текст навколо JSON — витягаємо об'єкт.
		if idx := extractJSONObject(out); idx >= 0 {
			if err2 := json.Unmarshal([]byte(idxText(out, idx)), &result); err2 == nil {
				return result, nil
			}
		}
		return model.AIResult{}, fmt.Errorf("Groq не повернув валідний JSON: %w", err)
	}
	return result, nil
}

// ── Відгук про проєкт ────────────────────────────────────────────────────
// Модель отримує те, що бачить користувач (деталі проєкту) + знайдені у
// вебі згадки і пише людською мовою відгук: чи вартий проєкт уваги.

// WriteProjectReview будує відгук про проєкт. contextData — підготовлений
// бекендом текст (деталі проєкту + веб-сніпети). Повертає markdown-відгук.
//
// Промт вимогливо налаштований на ТУТОРІАЛЬНИЙ формат: не шаблонний
// «відгук-огляд», а практичний гайд, який пояснює що це, як встановити,
// як налаштувати і як користуватись — зміст залежить від конкретного
// проєкту (команди, конфіги, скріншоти з опису, версії), а не від
// заготовки.
func (c *GroqClient) WriteProjectReview(ctx context.Context, lang, contextData string) (string, error) {
	system := fmt.Sprintf(
		`Ти — модуль лаунчера Minecraft "Shaurma Launcher", який пише докладні туторіали по Minecraft-контенту (модам, модпакам, ресурспакам, шейдерам, датапакам) ПРЯМО НА СТОРІНЦІ ПРОЄКТУ всередині цього лаунчера. Мова відповіді: %s.

Завдання — написати ПРАКТИЧНИЙ ТУТОРІАЛ, а не рекламу чи шаблонний огляд. Користувач читає його, щоб ЗРОЗУМІТИ ПРОЄКТ І ПОЧАТИ НИМ КОРИСТУВАТИСЬ.

ВАЖЛИВО ПРО ІНСТРУКЦІЇ ВСТАНОВЛЕННЯ: користувач ВЖЕ перебуває всередині Shaurma Launcher, дивлячись на цю сторінку проєкту. У Shaurma Launcher є ВЛАСНИЙ вбудований менеджер контенту (пошук/встановлення модів, ресурспаків, шейдерів, датапаків прямо з інтерфейсу лаунчера, кнопка «Встановити»/«Додати з браузера» на цій же сторінці) — НЕ пиши інструкції на кшталт «відкрийте лаунчер Modrinth або CurseForge», «скачайте вручну і покладіть у .minecraft/mods» тощо: це поради для ІНШИХ застосунків і вони лише заплутають користувача, який уже на потрібній сторінці в потрібному лаунчері. У розділі «Як встановити» пиши коротко: «Натисніть кнопку «Встановити» на цій сторінці — Shaurma Launcher сам підбере сумісну версію під вашу збірку і встановить файл у потрібну теку» (адаптуй формулювання під тип контенту — мод/ресурспак/шейдер/датапак — але БЕЗ згадок сторонніх лаунчерів чи ручного копіювання файлів, якщо це прямо не унікальна вимога САМЕ цього проєкту з опису).

СТРУКТУРА (обов'язково, але назви розділів можна адаптувати під проєкт):
## <назва проєкту>
**Що це і що робить:** розгорнутий опис СУТІ проєкту — які функції/механіки додає, як змінює геймплей. Будь конкретним: виписуй реальні функції, згадані в описі, з деталями (назви команд, механік, блоків, предметів, елементів UI).
**Сумісність:** версії Minecraft, лоадери (Fabric/Forge/NeoForge/Quilt), необхідні залежності або бібліотеки-супутники — лише те, що є в даних.
**Як встановити:** «Натисніть «Встановити» на цій сторінці» (див. правило вище). Якщо з даних видно особливості САМЕ ЦЬОГО проєкту (потрібен певний лоадер, конфлікти, обов'язкові додаткові файли/залежності) — обов'язково згадай ЦЕ, а не загальний спосіб встановлення.
**Як налаштувати і користуватись:** що робити ПІСЛЯ встановлення — конфіги, команди, меню, клавіші, взаємодія з іншими модами. Черпай деталі з опису (розділи про конфігурацію, команди, скріншоти). Якщо в описі їх немає — чесно напиши «докладних інструкцій в описі немає, базові кроки: …» і дай мінімальний безпечний гайд.
**Часті помилки / поради:** якщо в даних (опис, веб-згадки) є відомі проблеми, конфлікти, вимоги до версій — перелічи. Якщо ні — розділ можна опустити.
**Підсумок:** 1-2 речення — кому варто спробувати і чому.

ПРАВИЛА:
- Пиши ТІЛЬКИ на основі наданих даних. Не вигадуй функцій, команд чи конфігів, яких немає в описі. Якщо даних мало — так і напиши, і дай загальний безпечний гайд, не вигадуючи деталей.
- Кожен туторіал має бути УНІКАЛЬНИМ: структура одна, але зміст випливає з конкретного проєкту. НЕ використовуй фраз-заготовок («цей мод додає багато цікавого»), порожніх захватів, «неймовірно!», «must-have!».
- Якщо у веб-згадках є реальні відгуки/проблеми — використовуй їх у розділах «Часті помилки» і «Підсумок».
- Формат — markdown. Обсяг — 400-700 слів: детально, але без води.
- Відповідь — ЧИСТИЙ markdown-текст, без жодних обгорток. НЕ обгортай всю відповідь у потрійні зворотні лапки (код-блок) — лаунчер рендерить markdown напряму, і зайва обгортка показала б увесь туторіал одним нечитабельним блоком коду.`, lang)
	user := "Дані про проєкт та веб-згадки:\n---\n" + contextData + "\n---\n\nНапиши докладний туторіал: що це, як встановити (пам'ятай: користувач вже в Shaurma Launcher, встановлення — кнопкою на цій сторінці, БЕЗ сторонніх лаунчерів чи ручного копіювання файлів), як налаштувати і користуватись, часті помилки."
	out, err := c.chat(ctx, system, user, 2000, false)
	if err != nil {
		return "", err
	}
	// Захист від моделі, яка попри промт все одно обгорнула відповідь у
	// код-блок (```markdown ... ``` або просто ``` ... ```) — без цього
	// фронтенд (marked.parse) відрендерив би весь туторіал одним суцільним
	// нечитабельним блоком коду замість форматованого тексту.
	return stripFence(out), nil
}

// stripFence прибирає обгортку потрійними зворотними лапками навколо
// всього тексту (```markdown\n...\n``` або ```\n...\n```), якщо модель
// додала її попри промт. На відміну від stripMarkdownFence у gemini.go
// (яка ріже суфікс завжди), тут суфікс ``` знімається лише якщо він
// справді завершує текст — щоб не обрізати легітимний потрійний код-блок
// ВСЕРЕДИНІ відповіді (наприклад приклад команди в самому туторіалі).
func stripFence(text string) string {
	t := strings.TrimSpace(text)
	if !strings.HasPrefix(t, "```") {
		return text
	}
	firstNL := strings.Index(t, "\n")
	if firstNL < 0 {
		return text
	}
	// Перший рядок після ``` — необов'язкова мітка мови (markdown/md/…),
	// не сам вміст: завжди відкидаємо його разом з ```.
	rest := t[firstNL+1:]
	if strings.HasSuffix(strings.TrimRight(rest, "\n\r\t "), "```") {
		rest = strings.TrimRight(rest, "\n\r\t ")
		rest = strings.TrimSuffix(rest, "```")
	}
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return text
	}
	return rest
}

// ── Стиль збірки для міграції (пресет-іконка + колір) ──────────────────
// Коли перенесена збірка не має PNG-іконки, ШІ призначає їй один із
// пресетів лаунчера (Tabler-іконки з CreatePackPage) та підходящий колір
// картки — json-режим гарантує валідний JSON, який лаунчер легко розкодує.

// PackStyle — призначений ШІ стиль збірки: пресет-іконка + колір картки
// (#rrggbb). Icon — у форматі фронтенду, З префіксом "ti-" (напр.
// "ti-puzzle") — саме так рендерить <i class="ti ti-puzzle">. Модель у
// промті просять повернути ключ БЕЗ префікса, але приймаємо і з префіксом
// і нормалізуємо до "ti-" форми.
type PackStyle struct {
	Icon  string `json:"icon"`
	Color string `json:"color"`
}

// PresetIconKeys — пресети, з яких ШІ може обирати іконку (ті самі, що
// фронтенд показує у CreatePackPage; дублюється з shortcuts.PresetIconGlyphs,
// бо ai не залежить від shortcuts). КОЖЕН ключ перевірений: існує у
// tabler-icons.min.css — жодної «примарної» іконки, яку ШІ міг би
// запропонувати, а фронтенд показав би порожнім квадратом.
var PresetIconKeys = []string{
	"puzzle", "star", "heart", "diamond", "crown", "skull", "flame", "bolt",
	"sword", "shield", "axe", "target", "ghost", "alien", "robot", "brain",
	"cat", "dog", "bug", "rocket", "planet", "moon", "sun", "snowflake",
	// Істоти
	"dragon", "fish", "spider", "paw", "bone", "feather",
	// Природа
	"leaf", "tree", "mushroom", "flower", "cactus", "clover",
	// Їжа
	"apple", "cherry", "pizza", "coffee", "beer", "cake",
	"candy", "cookie", "ice-cream", "egg", "cheese", "salad",
	// Битва
	"bow", "bomb", "droplet", "helmet", "gavel", "swords",
	// Транспорт
	"plane", "car", "bike", "truck", "bus", "train", "ship", "anchor",
	// Світ / квести
	"map", "compass", "key", "lock", "tower",
	"trophy", "medal", "award", "flag", "chess", "dice",
	"pokeball", "wand", "meteor", "comet", "galaxy", "space",
	// Погода
	"cloud", "cloud-rain", "cloud-snow", "umbrella", "sunrise", "sunset", "moon-stars",
	// Дім / предмети
	"home", "building", "fence", "palette", "brush", "paint",
	"pencil", "ruler", "calculator", "cpu", "wallet", "coins",
	// Техніка
	"device-gamepad", "device-desktop", "device-laptop", "device-mobile",
}

// SuggestPackStyle призначає збірці пресет-іконку і колір за назвою/версією/
// лоадером. Якщо ключ не задано або модель не повернула валідний JSON —
// повертає порожній PackStyle (викликач застосовує детермінований фолбек).
func (c *GroqClient) SuggestPackStyle(ctx context.Context, name, mcVersion, loader string) PackStyle {
	if c.apiKey == "" {
		return PackStyle{}
	}
	icons := strings.Join(PresetIconKeys, ", ")
	system := fmt.Sprintf(`Ти — дизайнер іконок Minecraft-збірок для лаунчера.
Признач збірці ОДИН пресет-іконку та ОДИН колір картки, які найкраще пасують до її назви і тематики.

ДОЗВОЛЕНІ іконки (обирай ЛИШЕ з цього списку, ключ без префікса ti-): %s
Колір — hex у форматі #rrggbb (насичений, але не яскраво-кислотний; підходить для темного фону картки).

Поверни ВИКЛЮЧНО валідний JSON без тексту навколо:
{"icon": "<ключ іконки>", "color": "<#rrggbb>"}`, icons)
	user := fmt.Sprintf("Збірка: %s\nMinecraft: %s\nЛоадер: %s",
		firstNonEmptyAI(name, "невідома збірка"), firstNonEmptyAI(mcVersion, "невідома"), firstNonEmptyAI(loader, "vanilla"))

	out, err := c.chat(ctx, system, user, 200, true)
	if err != nil {
		return PackStyle{}
	}
	var s PackStyle
	if err := json.Unmarshal([]byte(out), &s); err != nil {
		if idx := extractJSONObject(out); idx >= 0 {
			if err2 := json.Unmarshal([]byte(idxText(out, idx)), &s); err2 != nil {
				return PackStyle{}
			}
		} else {
			return PackStyle{}
		}
	}
	// Валідація: іконка має бути у списку пресетів, колір — коректним hex.
	// Приймаємо ключ і з префіксом "ti-", і без (модель іноді додає префікс
	// попри промт) — нормалізуємо до форми з префіксом, яку рендерить
	// фронтенд (<i class="ti ti-puzzle">).
	icon := strings.TrimPrefix(s.Icon, "ti-")
	if !validPresetIcon(icon) || !validHexColor(s.Color) {
		return PackStyle{}
	}
	s.Icon = "ti-" + icon
	return s
}

func firstNonEmptyAI(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return strings.TrimSpace(v)
}

func validPresetIcon(icon string) bool {
	for _, k := range PresetIconKeys {
		if k == icon {
			return true
		}
	}
	return false
}

func validHexColor(s string) bool {
	if len(s) != 7 || s[0] != '#' {
		return false
	}
	for i := 1; i < 7; i++ {
		c := s[i]
		if !(c >= '0' && c <= '9') && !(c >= 'a' && c <= 'f') && !(c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

// ── Веб-пошук (для відгуку: «гуглить у інтернеті») ──────────────────────
// Без ключів: DuckDuckGo HTML-пошук (lite-версія). Якщо DDG недоступний —
// повертаємо порожній список, і модель пише відгук лише за даними проєкту
// (деградація, а не помилка).

var (
	ddgResultRe = regexp.MustCompile(`<a[^>]+class="result__a"[^>]*>(.*?)</a>`)
	ddgSnipRe   = regexp.MustCompile(`<a[^>]+class="result__snippet"[^>]*>(.*?)</a>`)
	stripTagsRe = regexp.MustCompile(`<[^>]+>`)
	stripEntRe  = regexp.MustCompile(`&[a-zA-Z#0-9]+;`)
)

// SearchWeb шукає в інтернеті запит і повертає до limit результатів у
// форматі "Назва — снипет" (текст без HTML). Помилки ігноруються:
// відгук має працювати навіть без веб-даних.
func SearchWeb(ctx context.Context, query string, limit int) []string {
	if limit <= 0 {
		limit = 5
	}
	reqCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	u := "https://lite.duckduckgo.com/lite/?q=" + strings.ReplaceAll(query, " ", "+")
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, u, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Safari/537.36")
	resp, err := (&http.Client{Timeout: 12 * time.Second}).Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil
	}
	data, _ := io.ReadAll(resp.Body)
	html := string(data)

	titles := ddgResultRe.FindAllStringSubmatch(html, -1)
	snips := ddgSnipRe.FindAllStringSubmatch(html, -1)

	var out []string
	for i, m := range titles {
		if len(out) >= limit {
			break
		}
		title := cleanWebText(m[1])
		snip := ""
		if i < len(snips) {
			snip = cleanWebText(snips[i][1])
		}
		if title != "" {
			if snip != "" {
				out = append(out, title+" — "+snip)
			} else {
				out = append(out, title)
			}
		}
	}
	return out
}

// cleanWebText прибирає HTML-теги та HTML-сутності з результату пошуку.
func cleanWebText(s string) string {
	s = stripTagsRe.ReplaceAllString(s, "")
	s = stripEntRe.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

// extractJSONObject знаходить позицію ПЕРШОГО '{' у тексті (початок JSON-
// об'єкта, який модель могла обгорнути поясненням навіть у json-режимі).
// Повертає -1, якщо '{' немає.
func extractJSONObject(s string) int {
	idx := strings.IndexByte(s, '{')
	return idx
}

// idxText повертає підрядок з idx до кінця (передається в json.Unmarshal,
// який ігнорує все після об'єкта).
func idxText(s string, idx int) string {
	if idx < 0 || idx >= len(s) {
		return s
	}
	return s[idx:]
}
