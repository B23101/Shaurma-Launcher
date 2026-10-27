// Package autorepair — авто-починка збірки після краху або невдалого запуску.
//
// Порт логіки Nivora Launcher (auto_repair.py), а не коду: Diagnose збирає
// «улики» з трьох джерел — виводу гри (консольний буфер), свіжого звіту в
// crash-reports/ та вмісту mods/ (маніфести jar) — і повертає список проблем.
// У кожної проблеми є один чи кілька способів лагодження з людськими кроками;
// інтерфейс показує їх користувачу, а обране застосовує Apply.
//
// Принципи (збережені з Nivora):
//   - усі лагодження оборотні чи безпечні: моди НЕ видаляються (отримують
//     суфікс .disabled), зламані конфіги переносяться в резервну теку,
//     відновлення версії не чіпає mods/config/saves;
//   - параметри лагоджень приходять з інтерфейсу, тому Apply перевіряє їх
//     наново, а не довіряє;
//   - рушій чистий: нічого не знає про App/конфіг — усе потрібне передається
//     в Input, а дії виконує Executor. Тому він тестується без лаунчера.
//
// Відмінності від Nivora, зумовлені SHAURMA: немає пісочниці екземпляра
// (security_sandbox не переноситься), Java ставить сам лаунчер (замість
// пошуку по системі — дія install_java), графіка керується лише глобальним
// «дискретна відеокарта».
package autorepair

import "time"

// Account — мінімум відомостей про акаунт для діагностики входу.
type Account struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Licensed bool   `json:"licensed"` // true — Microsoft/ліцензійний
	Active   bool   `json:"active"`
}

// Input — усе, що рушій має знати про збірку й запуск.
type Input struct {
	// Lines — вивід гри останньої сесії (консольний буфер), Error — текст
	// помилки запуску (коли гра навіть не стартувала).
	Lines []string
	Error string
	// ExitCode — nil, якщо невідомий (ручна перевірка без запуску).
	ExitCode *int

	// GameDir — тека .minecraft збірки (mods/, config/, crash-reports/).
	GameDir string

	Loader           string // vanilla | fabric | forge | neoforge | quilt
	MinecraftVersion string
	LoaderVersion    string

	// Since — початок останньої сесії: звіти про краш старіші за нього ігноруються.
	Since time.Time
	// CrashReport — готовий текст звіту (тести); nil → читається з диска.
	CrashReport *string
	// LastSuccess — час останнього успішного запуску («нові» моди новіші за нього).
	LastSuccess time.Time

	EffectiveRAMMB   int
	TotalRAMMB       int
	RecommendedRAMMB int

	// JavaPath — ручна Java збірки ("" = автовибір).
	JavaPath string
	// CustomJVM — у збірці задано власні аргументи JVM (JVMArgs — для кроків).
	CustomJVM bool
	JVMArgs   string
	// DedicatedGPU — глобальне налаштування «дискретна відеокарта для гри».
	DedicatedGPU bool
	// CanUpdateLoader — версію лоадера можна змінити (кастомна збірка; в
	// Shaurma-збірок її задає автор у Pack Manager).
	CanUpdateLoader bool

	Accounts         []Account
	CurrentAccountID string

	// Muted — ID проблем, які користувач приховав.
	Muted []string
}

// Fix — один спосіб лагодження.
type Fix struct {
	ID          string         `json:"id"`
	Action      string         `json:"action"`
	Label       string         `json:"label"`
	Steps       []string       `json:"steps"`
	Params      map[string]any `json:"params"`
	Recommended bool           `json:"recommended"`
}

// Issue — одна знайдена проблема.
type Issue struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Detail   string   `json:"detail"`
	Evidence []string `json:"evidence"`
	Fixes    []Fix    `json:"fixes"`
	// Manual — що користувач має зробити сам, якщо автоматика не допоможе.
	Manual string `json:"manual"`
}

// Report — результат діагностики.
type Report struct {
	ExitCode *int    `json:"exitCode"`
	Issues   []Issue `json:"issues"`
	// MutedIssues — приховані користувачем види проблем, що знайшлися.
	MutedIssues []string `json:"mutedIssues"`
}

// FixResult — підсумок застосування одного лагодження.
type FixResult struct {
	ID      string `json:"id"`
	OK      bool   `json:"ok"`
	Message string `json:"message"`
	// NeedsLogin — лагодження потребує інтерактивного входу Microsoft: його
	// запускає інтерфейс після застосування (див. AutoRepairDialog).
	NeedsLogin bool `json:"needsLogin,omitempty"`
}
