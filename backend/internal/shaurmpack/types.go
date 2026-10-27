package shaurmpack

// ─────────────────────────────────────────────────────────────────────────────
// .shaurmpack — власний формат модпаків Shaurma Launcher
//
// Формат: ZIP-архів з manifest.json та bundled файлами.
// Подібний до .mrpack (Modrinth), але з розширеними можливостями:
//   - Підтримка CurseForge + Modrinth джерел одночасно
//   - bundled/ — моди/ресурси, які не мають онлайн-джерела
//   - metadata/ — налаштування персоналізації, пресети гардеробу
//   - Хешування SHA-1 для всіх файлів
//   - Версію формату для backward/forward compatibility
//
// Структура архіву:
//   manifest.json          — головний маніфест
//   overrides/             — файли, що розпаковуються в .minecraft/
//   bundled/               — моди/ресурси без онлайн-джерела (вбудовані в архів)
//   metadata/personalization.json — налаштування персоналізації збірки
// ─────────────────────────────────────────────────────────────────────────────

const (
	// FormatVersion — поточна версія формату .shaurmpack.
	FormatVersion = 1

	// FileExtension — розширення файлу.
	FileExtension = ".shaurmpack"

	// ManifestFile — шлях до маніфесту в архіві.
	ManifestFile = "manifest.json"

	// OverridesDir — тека з overrides (розпаковується в .minecraft/).
	OverridesDir = "overrides/"

	// BundledDir — тека з bundled файлами (моди без джерела).
	BundledDir = "bundled/"

	// MetadataDir — тека з метаданими (персоналізація тощо).
	MetadataDir = "metadata/"
)

// Manifest — головний маніфест .shaurmpack файлу.
type Manifest struct {
	// Version — версія формату маніфесту ( FormatVersion ).
	Version int `json:"version"`

	// ── Метадані збірки ──
	PackID      string   `json:"packId"`               // унікальний ID збірки
	Name        string   `json:"name"`                 // назва збірки
	Description string   `json:"description,omitempty"`// опис
	VersionID   string   `json:"versionId"`            // версія збірки (semver або date-based)
	Authors     []string `json:"authors,omitempty"`    // автори
	License     string   `json:"license,omitempty"`    // ліцензія
	Changelog   string   `json:"changelog,omitempty"`  // що змінилось у цій версії
	ProjectURL  string   `json:"projectUrl,omitempty"` // посилання на проєкт
	Icon        string   `json:"icon,omitempty"`       // відносний шлях до іконки в overrides/

	// ── Minecraft ──
	GameVersion  string `json:"gameVersion"`            // MC version (напр. "1.20.1")
	Loader       string `json:"loader"`                  // vanilla|fabric|forge|neoforge|quilt
	LoaderVersion string `json:"loaderVersion,omitempty"` // версія лоадера

	// ── Колір/стиль ──
	Color string `json:"color,omitempty"` // hex-колір збірки (#ff0000)

	// ── Файли ──
	Files    []PackFile     `json:"files"`              // список файлів
	OverrideCount int      `json:"overrideCount"`      // кількість override файлів
	BundledCount  int      `json:"bundledCount"`       // кількість bundled файлів
	// ResolvedCount — файли з онлайн-джерелом (НЕ лежать в архіві; качаються
	// при імпорті). У старих паків відсутнє (=0) — рахувати з Files.
	ResolvedCount int      `json:"resolvedCount,omitempty"`

	// ── Метадані лаунчера ──
	Launcher    LauncherMeta  `json:"launcher"`
	CreatedAt   string        `json:"createdAt"`        // ISO 8601
}

// LauncherMeta — інформація про лаунчер що створив пак.
type LauncherMeta struct {
	Name    string `json:"name"`              // "Shaurma Launcher"
	Version string `json:"version"`           // версія лаунчера
	Build   int    `json:"build,omitempty"`   // номер збірки
}

// PackFile — опис одного файлу в збірці.
type PackFile struct {
	// Path — відносний шлях файлу (Unix '/') від кореня .minecraft/
	// Наприклад: "mods/fabric-api-0.87.0+1.20.1.jar"
	Path string `json:"path"`

	// Hash — SHA-1 хеш файлу (hex)
	Hash string `json:"hash"`

	// Size — розмір файлу в байтах
	Size int64 `json:"size"`

	// ── Джерело файлу (одне з трьох) ──
	// Порядок резолву в Export(): СПЕРШУ Modrinth, ПОТІМ CurseForge (див.
	// коментар над Export у pack.go). Причина порядку: Modrinth ніколи не
	// має "заблокованих авторами" файлів (opt-out на рівні API відсутній),
	// тож перевірка Modrinth першою одразу закриває найбільшу частку модів
	// без ризику наштовхнутись на Blocked; CurseForge лишається фолбеком
	// саме для того, чого немає на Modrinth.

	// CurseForge — якщо мод знайдений на CurseForge. Якщо знайдений, але
	// заблокований автором для стороннього завантаження — Blocked=true
	// (див. CurseForgeSource), і файл однаково піде в bundled/.
	CurseForge *CurseForgeSource `json:"curseforge,omitempty"`

	// Modrinth — якщо мод знайдений на Modrinth
	Modrinth *ModrinthSource `json:"modrinth,omitempty"`

	// Bundled — якщо файл вбудований в архів у bundled/ (джерело можливе, але
	// не знайдено/заблоковано). Файли без джерела й без Bundled лежать в
	// overrides/ (config тощо); файли з робочим джерелом в архіві НЕ лежать
	// (у старих паках — лежали в overrides/ як дублікат).
	Bundled bool `json:"bundled,omitempty"`

	// ── Тип файлу ──
	FileType FileType `json:"fileType"` // mod | resourcepack | shaderpack | config | other
}

// FileType — тип файлу для правильного розпаковування.
type FileType string

const (
	FileTypeMod         FileType = "mod"
	FileTypeResourcePack FileType = "resourcepack"
	FileTypeShaderPack  FileType = "shaderpack"
	FileTypeConfig      FileType = "config"
	FileTypeOther       FileType = "other"
)

// CurseForgeSource — джерело файлу на CurseForge.
type CurseForgeSource struct {
	ProjectID   int    `json:"projectId"`   // CurseForge project ID (числовий)
	FileID      int    `json:"fileId"`      // CurseForge file ID (числовий)
	FileName    string `json:"fileName"`    // назва файлу на CF
	DownloadURL string `json:"downloadUrl,omitempty"` // пряме посилання; ПОРОЖНЄ якщо Blocked=true
	Required    bool   `json:"required"`    // обов'язковий мод?

	// Blocked — автор мода увімкнув "3rd party download opt-out" на
	// CurseForge (те саме, що CFFileInfo.Blocked в internal/custompack).
	// Пряме посилання для завантаження відсутнє — файл ІДЕНТИФІКОВАНО
	// (projectId/fileId відомі, вони пишуться в маніфест для довідки), але
	// НЕ резолвиться як онлайн-джерело: Export() в цьому випадку трактує
	// файл так само, як "джерело не знайдено", і вшиває його в bundled/,
	// а Import() при зустрічі Blocked=true НАВІТЬ НЕ намагається йти по
	// DownloadURL (він порожній і завжди буде 403/404) — одразу бере файл
	// з bundled/ в архіві. Це навмисно: спроба довантажити заблокований
	// автором файл ніколи не спрацює, і не повинна витрачати час/трафік
	// чи показувати користувачу помилку мережі там, де насправді немає
	// нічого, крім бекапу в самому архіві.
	Blocked bool `json:"blocked,omitempty"`
}

// ModrinthSource — джерело файлу на Modrinth.
type ModrinthSource struct {
	ProjectID   string `json:"projectId"`   // Modrinth project ID (hex)
	VersionID   string `json:"versionId"`   // Modrinth version ID
	FileName    string `json:"fileName"`
	DownloadURL string `json:"downloadUrl,omitempty"`
	Required    bool   `json:"required"`
}

// Personalization — налаштування персоналізації збірки (зберігається в
// metadata/personalization.json всередині архіву).
type Personalization struct {
	// WelcomeMessage — текст привітання на сторінці збірки
	WelcomeMessage string `json:"welcomeMessage,omitempty"`

	// BannerURL — URL банера збірки
	BannerURL string `json:"bannerUrl,omitempty"`

	// Tags — теги для пошуку
	Tags []string `json:"tags,omitempty"`

	// MinLauncherVersion — мінімальна версія лаунчера для цієї збірки
	MinLauncherVersion string `json:"minLauncherVersion,omitempty"`
}

// InstalledInfo — зберігається в instance.json для відстеження
// зв'язку між встановленою збіркою та .shaurmpack файлом.
// Використовується для системи оновлення.
type InstalledInfo struct {
	SourcePackID  string `json:"sourcePackId"`  // ID оригінального .shaurmpack
	SourceVersion string `json:"sourceVersion"` // версія на момент встановлення
	InstalledAt   string `json:"installedAt"`   // коли встановлено
	SourceHash    string `json:"sourceHash"`    // SHA-1 хеш .shaurmpack файлу
}
