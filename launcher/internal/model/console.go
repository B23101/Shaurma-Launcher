package model

import "shaurma-launcher-wails/internal/builds"

// ── Консоль гри ────────────────────────────────────────────────────────────
// Структури для розумної консолі: рядок логу, контекст запущеної збірки,
// AI-діагноз крашу та результат застосування рекомендованої дії.

// ConsoleLine — один рядок логу гри, збережений у кільцевому буфері консолі.
type ConsoleLine struct {
	ID    uint64 `json:"id"`
	Time  string `json:"time"`  // HH:MM:SS
	Level string `json:"level"` // info | warn | error | system
	Text  string `json:"text"`
}

// ConsoleAccountInfo — акаунт у перемикачі консолі (хто має лог/запуск
// для конкретної збірки і чи запущена в нього гра).
type ConsoleAccountInfo struct {
	AccountID   string `json:"accountId"`
	AccountName string `json:"accountName"`
	// Uuid/Licensed — для голови скіна (PNG-іконки) у перемикачі акаунтів
	// консолі: фронтенд робить App.GetAccountHead(uuid, name, licensed).
	Uuid     string `json:"uuid"`
	Licensed bool   `json:"licensed"`
	Running  bool   `json:"running"`
}

// ConsoleContext — відомості про збірку, до якої прив'язана консоль:
// запущена вона чи ні, яка збірка (для заголовка консолі та панелі
// запуску), чий акаунт показується зараз (AccountID/AccountName) і список
// акаунтів з логом цієї збірки (Accounts — перемикач консолі).
// Status — РЕАЛЬНИЙ стан збірки (той самий, що в sidebar/«Мої збірки»):
// not-installed | needs-update | ready | downloading | updating | running |
// error. Панель запуску консолі показує правильну дію (Завантажити /
// Оновити / Запустити) за ним, а не фіктивну.
type ConsoleContext struct {
	InstanceID string `json:"instanceId"`
	Name       string `json:"name"`
	MCVersion  string `json:"mcVersion"`
	Loader     string `json:"loader"`
	Running    bool   `json:"running"`
	RAMMB      int    `json:"ramMb"`
	// Active — чи ця збірка є АКТИВНОЮ у головному вікні (вибрана/відкрита
	// на сторінці деталей) або запущена зараз. Окреме вікно консолі НЕ
	// показує кнопку «Запустити» для неактивної збірки — інакше клік по
	// ній запускав би застарілу збірку з минулого запуску (баг «запустилась
	// інша збірка»: консоль була прив'язана до BlockFront із старої сесії).
	Active bool `json:"active"`
	// Status — стан файлів збірки (див. вище). StatusRunning збігається з
	// Running=true; для станів downloading/updating Progress містить %.
	Status   string          `json:"status,omitempty"`
	Progress *builds.Progress `json:"progress,omitempty"`
	// AccountID/AccountName — акаунт, ЧИЮ консоль показуємо (після
	// перемикання може відрізнятись від активного акаунта).
	AccountID   string `json:"accountId,omitempty"`
	AccountName string `json:"accountName,omitempty"`
	Accounts    []ConsoleAccountInfo `json:"accounts,omitempty"`
	// Launching — активна стадія запуску збірки (checking/worker_update/
	// java/loader) з останньої події launch:progress. Консоль показує
	// реальний стан, навіть якщо її відкрили посеред запуску (події до
	// цього вже пройшли повз неї).
	Launching     bool   `json:"launching,omitempty"`
	LaunchStage   string `json:"launchStage,omitempty"`
	LaunchMessage string `json:"launchMessage,omitempty"`
	LaunchPercent int    `json:"launchPercent,omitempty"`

	// Crashed/LastExitCode — останній вихід гри для цієї збірки: консоль
	// відновлює стан крашу (AI-панель) навіть якщо вікно/вкладку відкрили
	// після того, як гра впала. Diagnosis — останній збережений AI-діагноз
	// (той самий для всіх копій консолі — вікна і вкладки, щоб відповіді
	// ШІ не розходилися між ними).
	Crashed     bool          `json:"crashed,omitempty"`
	LastExitCode int          `json:"lastExitCode,omitempty"`
	Diagnosis   *AIDiagnosis  `json:"diagnosis,omitempty"`
}

// AIRecommendedAction — структурована рекомендація дії від AI-аналізу крашу.
// Type: increase_ram | disable_mod | download_dependency | repair | none.
// repair = «Полагодити»: перевірка цілісності файлів збірки/Java (для
// Kind=ENV_CORRUPTION) — викликає той самий RepairPack, що й кнопка у
// розділі «Продуктивність» на сторінці збірки.
type AIRecommendedAction struct {
	Type           string `json:"type"`
	TargetMod      string `json:"targetMod"`      // для disable_mod — ім'я файлу мода
	DependencyName string `json:"dependencyName"` // для download_dependency
	SuggestedRamMB int    `json:"suggestedRamMb"` // для increase_ram
}

// AIResult — відповідь Gemini (вже розпарсена, без markdown-обгортки).
type AIResult struct {
	Cause             string              `json:"cause"`
	Explanation       string              `json:"explanation"`
	Facts             []string            `json:"facts"`
	Actionable        bool                `json:"actionable"`
	RecommendedAction AIRecommendedAction `json:"recommendedAction"`
	CopyText          string              `json:"copyText"`
	Model             string              `json:"model"`
}

// AIDiagnosis — результат аналізу крашу: локальний детектор + (опційно) AI.
// AIEnabled=false → блок AI не рендериться (налаштування consoleAI вимкнене).
// AIAvailable=false → Gemini не відповів, показати лише локальний аналіз.
type AIDiagnosis struct {
	Kind            string   `json:"kind"` // MISSING_DEPENDENCY | MOD_CONFLICT | SINGLE_MOD_ERROR | ENV_CORRUPTION | UNKNOWN
	Summary         string   `json:"summary"`
	Facts           []string `json:"facts"`
	CulpritLines    []string `json:"culpritLines"` // точні рядки логу, що вказують на проблему
	RawExcerpt      string   `json:"rawExcerpt"`
	CrashReportFile string   `json:"crashReportFile"`
	CurrentRAMMB    int      `json:"currentRamMb"`

	// RepairSuggested — локальний детектор знайшов пошкодження файлів
	// оточення (Java/нативки/асети/jar), які виправляються кнопкою
	// «Полагодити». RepairHint — куди піти (розділ «Продуктивність» на
	// сторінці збірки) + що буде зроблено.
	RepairSuggested bool   `json:"repairSuggested"`
	RepairHint      string `json:"repairHint,omitempty"`

	AIEnabled   bool      `json:"aiEnabled"`
	AIAvailable bool      `json:"aiAvailable"`
	AIError     string    `json:"aiError,omitempty"`
	AIResult    *AIResult `json:"aiResult,omitempty"`
}

// AIFixResult — результат застосування рекомендованої AI дії (ApplyAIFix).
type AIFixResult struct {
	Success     bool   `json:"success"`
	Message     string `json:"message"`
	ProjectURL  string `json:"projectUrl,omitempty"`
	Source      string `json:"source,omitempty"` // modrinth | curseforge
	InstalledTo string `json:"installedTo,omitempty"`
}

// DependencyResult — результат пошуку/завантаження залежності (Modrinth).
type DependencyResult struct {
	Success     bool   `json:"success"`
	Source      string `json:"source"` // modrinth | curseforge
	Message     string `json:"message"`
	ProjectURL  string `json:"projectUrl,omitempty"`
	InstalledTo string `json:"installedTo,omitempty"`
}
