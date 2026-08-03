package model

type Instance struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Loader        string `json:"loader"`
	MCVersion     string `json:"mcVersion"`
	LoaderVersion string `json:"loaderVersion"`
	Icon          string `json:"icon"`
	Color         string `json:"color"`
	Type          string `json:"type"`
	Status        string `json:"status"`
	Downloaded    int64  `json:"downloaded"`
	TotalBytes    int64  `json:"totalBytes"`
	IsShaurma     bool   `json:"isShaurma"`
	GameDir       string `json:"gameDir"`
}

type Account struct {
	ID            string `json:"id"`
	Username      string `json:"username"`
	Type          string `json:"type"`
	AccessToken   string `json:"accessToken"`
	MsAccessToken string `json:"msAccessToken"`
	RefreshToken  string `json:"refreshToken"`
	ExpiresAt     int64  `json:"expiresAt"`
	UUID          string `json:"uuid"`
	IsLicensed    bool   `json:"isLicensed"`
}

// BuildConsoleLine — рядок консолі з контекстом збірки/акаунта (подія
// buildConsole:line). Потрібен для «одна консоль на збірку з перемикачем
// акаунтів»: фронтенд фільтрує по buildID і групує по accountID.
type BuildConsoleLine struct {
	BuildID   string `json:"buildId"`
	AccountID string `json:"accountId"`
	Line      string `json:"line"`
}

// BuildStartEvent — контекст запуску збірки (подія build:started).
type BuildStartEvent struct {
	BuildID     string `json:"buildId"`
	AccountID   string `json:"accountId"`
	AccountName string `json:"accountName"`
}

// BuildExitEvent — контекст завершення збірки (подія build:exit).
type BuildExitEvent struct {
	BuildID   string `json:"buildId"`
	AccountID string `json:"accountId"`
	Code      int    `json:"code"`
}

// ConsoleAIEvent — новий AI-діагноз крашу для КОНКРЕТНОЇ збірки (подія
// console:ai). buildID дозволяє у режимі «кілька вікон консолі по збірках»
// реагувати лише консолям ЦІЄЇ збірки: вікно/вкладка іншої збірки не
// отримує чужий діагноз.
type ConsoleAIEvent struct {
	BuildID   string      `json:"buildId"`
	Diagnosis AIDiagnosis `json:"diagnosis"`
}

type Settings struct {
	Language      string `json:"language"`
	Accent        string `json:"accent"`
	AccentCustom  string `json:"accentCustom"` // hex-колір для акценту "custom" (пікер)
	Theme         string `json:"theme"`        // dark | light
	Font          string `json:"font"`         // system | inter | jetbrains | custom
	FontPath      string `json:"fontPath"`     // шлях до кастомного TTF (font=custom)
	AutoUpdate    bool   `json:"autoUpdate"`
	UpdateChannel string `json:"updateChannel"`
	CloseOnLaunch bool   `json:"closeOnLaunch"`
	ShowConsole   bool   `json:"showConsole"`
	SaveLogs      bool   `json:"saveLogs"`
	InstanceDir   string `json:"instanceDir"`
	JavaPath      string `json:"javaPath"`
	MaxRAM        int    `json:"maxRAM"`
	JavaArgs      string `json:"javaArgs"`
	// Активний (обраний для гри) акаунт. Зберігається в settings.json —
	// перемикання між акаунтами переживає перезапуск лаунчера.
	ActiveAccountID string `json:"activeAccountId"`

	// ── Консоль ──
	// Ліміт буфера — у РЯДКАХ (consoleMaxLines), не символах: користувачу
	// зрозуміліше, і Prism/термінали теж рахують рядками. Розмір консолі
	// (consoleWidth) прибрано: консоль розтягується по розміру вікна.
	ConsoleMaxLines   int    `json:"consoleMaxLines"`   // обмеження буфера консолі (рядків)
	ConsoleInfoColor  string `json:"consoleInfoColor"`  // hex кольору звичайних повідомлень
	ConsoleWarnColor  string `json:"consoleWarnColor"`  // hex кольору попереджень
	ConsoleErrorColor string `json:"consoleErrorColor"` // hex кольору помилок
	ConsoleWrap       bool   `json:"consoleWrap"`       // перенесення довгих рядків (як у Prism)
	ConsoleAI         bool   `json:"consoleAI"`         // AI-аналіз помилок крашу
	// Розмір і шрифт тексту консолі (дефолт 12px / моноширинний).
	ConsoleFontSize   int    `json:"consoleFontSize"`   // розмір шрифту консолі (px)
	ConsoleFontFamily string `json:"consoleFontFamily"` // mono | sans (шрифт для ВСІХ типів)
	// Шрифт консолі окремо для кожного типу повідомлення (ТЗ: «має бути
	// режим, де шрифт консолі різний для info/warn/error»).
	// ConsoleFontMode: "all" — один шрифт (consoleFontFamily) для всіх
	// типів; "perType" — кожен тип використовує свій (поля нижче).
	// Значення полів: "mono" | "sans" (ті самі опції, що consoleFontFamily).
	ConsoleFontMode     string `json:"consoleFontMode"`  // all | perType
	ConsoleInfoFont     string `json:"consoleInfoFont"`  // mono | sans (коли mode=perType)
	ConsoleWarnFont     string `json:"consoleWarnFont"`  // mono | sans
	ConsoleErrorFont    string `json:"consoleErrorFont"` // mono | sans
	ShowConsoleOnLaunch bool   `json:"showConsoleOnLaunch"`
	ShowConsoleOnCrash  bool   `json:"showConsoleOnCrash"`
	ShowConsoleOnClose  bool   `json:"showConsoleOnClose"`

	// ── Завдання (ліміти завантажень) ──
	MaxConcurrentDownloads int `json:"maxConcurrentDownloads"`
	MaxRetries             int `json:"maxRetries"`
	HTTPTimeoutSec         int `json:"httpTimeoutSec"`

	// ── Команди ──
	PreLaunchCommand string `json:"preLaunchCommand"` // виконується ДО запуску гри
	WrapperCommand   string `json:"wrapperCommand"`   // обгортка запуску (optirun тощо)
	PostExitCommand  string `json:"postExitCommand"`  // виконується ПІСЛЯ виходу з гри
	EnvVars          string `json:"envVars"`          // KEY=VALUE, по одному в рядку

	// ── Вікно гри ──
	Fullscreen      bool `json:"fullscreen"`
	WindowWidth     int  `json:"windowWidth"`
	WindowHeight    int  `json:"windowHeight"`
	HideOnGameOpen  bool `json:"hideOnGameOpen"`  // сховати лаунчер, коли гра відкрилась
	ExitOnGameClose bool `json:"exitOnGameClose"` // вийти з лаунчера, коли гра закрилась
	// ConfirmOnStop — показувати вікно підтвердження перед зупинкою
	// запущеної збірки (кнопка «Зупинити»). Вимкнено = зупиняти одразу.
	// За замовчуванням увімкнено (ТЗ: «вікно чи хочете зупинити збірку»).
	ConfirmOnStop bool `json:"confirmOnStop"`

	// ── Ігровий час ──
	ShowGameTime      bool `json:"showGameTime"`
	RecordGameTime    bool `json:"recordGameTime"`
	ShowTotalGameTime bool `json:"showTotalGameTime"`
	GameTimeInHours   bool `json:"gameTimeInHours"`
}

// SystemInfo — зведення про систему для кроку "Продуктивність" майстра
// та сторінки налаштувань (обчислюється на бекенді).
type SystemInfo struct {
	CPUCount   int   `json:"cpuCount"`
	TotalRAMMB int64 `json:"totalRAMMB"`
	FreeDiskMB int64 `json:"freeDiskMB"`
}

// JavaStatus — стан вбудованої Java лаунчера (для кроку «Теки та Java»
// майстра та сторінки налаштувань). Installed=false означає, що Java ще
// не розкладена у теку лаунчера і її треба встановити (EnsureJava).
// Dir — ПАПКА Java (та, що показується користувачу в полі), ExePath —
// виконуваний файл усередині неї для запуску гри.
type JavaStatus struct {
	Installed bool   `json:"installed"`
	Dir       string `json:"dir"`
	ExePath   string `json:"exePath"`
	Version   string `json:"version"`
}

// SettingOption — один варіант вибору для налаштування типу select/accent/language.
type SettingOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// SettingDef — декларативний опис одного налаштування (архітектура
// налаштувань). Фронтенд отримує повний список через GetSettingsSchema()
// і малює контроли сам: щоб додати нове налаштування, достатньо додати
// один рядок у config.SettingsSchema() + ключі в i18n — решта (збереження
// в settings.json, відображення, дефолти) підхоплюється автоматично.
type SettingDef struct {
	Key         string          `json:"key"`
	Type        string          `json:"type"` // bool | string | int | color | select | accent | language | ram
	Group       string          `json:"group"`
	Label       string          `json:"label"`
	Desc        string          `json:"desc"`
	Default     interface{}     `json:"default"`
	Options     []SettingOption `json:"options,omitempty"`
	Min         *int            `json:"min,omitempty"`
	Max         *int            `json:"max,omitempty"`
	Step        *int            `json:"step,omitempty"`
	Placeholder string          `json:"placeholder,omitempty"`
	Action      string          `json:"action,omitempty"`    // browse | detectJava — кнопка поруч із полем
	Multiline   bool            `json:"multiline,omitempty"` // багаторядкове поле (команди/env)
}

// FolderPaths — поточні шляхи тек лаунчера для вкладки «Теки та шляхи».
// BaseDir — папка даних лаунчера; Installations/Java/Cache/Logs — підтеки
// (кастомні значення з launcher-location.json або дефолти відносно BaseDir);
// Config — стабільна тека конфігурації (ніколи не змінюється).
type FolderPaths struct {
	BaseDir       string `json:"baseDir"`
	Installations string `json:"installations"`
	Java          string `json:"java"`
	Cache         string `json:"cache"`
	Logs          string `json:"logs"`
	Config        string `json:"config"`
	// Кастомні значення, збережені у launcher-location.json ("" = дефолт).
	InstallationsCustom string `json:"installationsCustom"`
	JavaCustom          string `json:"javaCustom"`
	CacheCustom         string `json:"cacheCustom"`
	LogsCustom          string `json:"logsCustom"`
	// DefaultBaseDir — системний дефолт папки даних (для кнопки
	// «За замовчуванням» на вкладці «Теки та шляхи»).
	DefaultBaseDir string `json:"defaultBaseDir"`
}

// StorageEntry — одна тека на вкладці «Пам'ять і кеш»: назва, шлях, розмір.
type StorageEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
}

// StorageInfo — зведення зайнятого місця для вкладки «Пам'ять і кеш».
type StorageInfo struct {
	Entries    []StorageEntry `json:"entries"`
	TotalBytes int64          `json:"totalBytes"`
	FreeDiskMB int64          `json:"freeDiskMB"`
}

// StoragePackEntry — одна ВСТАНОВЛЕНА збірка у списку «Пам'ять і кеш»
// (для швидкого видалення зайвого місця): id, назва, розмір теки.
// IconURL/Color додатково — щоб рядок у списку виглядав як картка
// у sidebar (іконка + колір збірки), а не голий текст.
type StoragePackEntry struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Bytes   int64  `json:"bytes"`
	IconURL string `json:"iconUrl"`
	Color   string `json:"color"`
}

// SetupProgress — чернетка майстра першого запуску (wizard). Пишеться на
// диск при КОЖНІЙ зміні кроку чи поля, а не лише в кінці — тому якщо
// користувач закриє лаунчер (або вкладку) на кроці 2, наступний запуск
// відновлює саме крок 2 з уже введеними даними, а не починає з кроку 1.
// Файл видаляється тільки після успішного CompleteSetup().
type SetupProgress struct {
	Step        int    `json:"step"`
	InstanceDir string `json:"instanceDir"`
	JavaPath    string `json:"javaPath"`
	Language    string `json:"language"`
	Accent      string `json:"accent"`
	MaxRAM      int    `json:"maxRAM"`
	JavaArgs    string `json:"javaArgs"`
}

type DownloadProgress struct {
	InstanceID string  `json:"instanceId"`
	FileName   string  `json:"fileName"`
	Downloaded int64   `json:"downloaded"`
	Total      int64   `json:"total"`
	Speed      float64 `json:"speed"`
	Percent    int     `json:"percent"`
}

// InstanceConfig — редаговані per-instance параметри збірки. Для збірок
// Шаурма НЕ можна міняти фон, лоадер чи версію гри (це фіксовано
// маніфестом збірки), тому цих полів тут навмисно немає — редагувати
// можна лише те, що нижче: RAM, аргументи JVM, вікно гри, команди,
// список закріплених серверів.
//
// Порожній рядок / нульове значення в *Override-полях означає
// "успадкувати з глобальних Settings" — так збірка одразу працює без
// збереженого файлу конфігурації, аж поки користувач щось не перевизначить.
type InstanceConfig struct {
	ID string `json:"id"`

	MaxRAMOverride int `json:"maxRamOverride"`
	// MinRAMOverride — стартовий розмір купи (-Xms) для ЦІЄЇ збірки. 0 =
	// успадкувати правило "глобальний Xms = MaxRAM/2" (стара поведінка).
	// Якщо задано (>0) — використовується саме це значення замість
	// автоматичного ram/2, як окреме поле "Мінімальна RAM" у Prism/старому
	// лаунчері.
	MinRAMOverride   int    `json:"minRamOverride"`
	JavaArgsOverride string `json:"javaArgsOverride"`

	FullscreenOverride   *bool `json:"fullscreenOverride,omitempty"`
	WindowWidthOverride  int   `json:"windowWidthOverride"`
	WindowHeightOverride int   `json:"windowHeightOverride"`

	PreLaunchCommandOverride string `json:"preLaunchCommandOverride"`
	WrapperCommandOverride   string `json:"wrapperCommandOverride"`
	PostExitCommandOverride  string `json:"postExitCommandOverride"`
	EnvVarsOverride          string `json:"envVarsOverride"`
	// UseCustomCommands — явний прапорець «використовувати свої команди».
	// Без нього тогл у «Продуктивності» не міг би вимкнутись, коли ВСІ
	// глобальні команди порожні (порожні override = «успадкувати»), тож
	// «свої» значення неможливо було б увімкнути для збірки. false =
	// глобальні команди лаунчера.
	UseCustomCommands bool `json:"useCustomCommands"`

	UseSeparateJava  bool   `json:"useSeparateJava"`
	JavaPathOverride string `json:"javaPathOverride"`

	// AccountIDOverride — якщо задано, ЦЯ збірка завжди запускається під
	// вказаним акаунтом, ігноруючи активний акаунт лаунчера (аналог
	// "Override Default Account" у Prism). Порожній рядок = використовувати
	// активний акаунт лаунчера, як і раніше.
	AccountIDOverride string `json:"accountIdOverride"`

	// ── Автоприєднання (Auto-join) ──
	// Аналог "Enable Auto-join" у Prism: одразу після завантаження світу
	// гра або заходить в обраний одиночний світ, або одразу конектиться на
	// вказаний сервер. AutoJoinType: "world" | "server".
	AutoJoinEnabled bool   `json:"autoJoinEnabled"`
	AutoJoinType    string `json:"autoJoinType"`
	// AutoJoinWorld — назва теки світу в saves/ (для типу "world").
	AutoJoinWorld string `json:"autoJoinWorld"`
	// AutoJoinServer — адреса сервера host[:port] (для типу "server").
	AutoJoinServer string `json:"autoJoinServer"`

	Servers []InstanceServer `json:"servers,omitempty"`
}

// InstanceServer — один сервер у списку "Сервери" на сторінці збірки.
type InstanceServer struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Address  string `json:"address"`
	Pinned   bool   `json:"pinned"`
	Official bool   `json:"official"`
}

// ModEntry — мод у папці модів збірки. Поле ID — modId з метаданих jar
// (fabric.mod.json / mods.toml), Name — людська назва, Version — версія,
// прочитана з jar (реальна, а не назва файлу). Решта полів заповнюється
// сканнером (sha1/іконка/автор/залежності) і перевіркою оновлень на
// Modrinth/CurseForge (hasUpdate/latestVersion/source/projectSlug).
type ModEntry struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Version   string `json:"version"`
	Loader    string `json:"loader"`
	Enabled   bool   `json:"enabled"`
	HasUpdate bool   `json:"hasUpdate"`
	Source    string `json:"source"`
	FileName  string `json:"fileName"`

	Author         string   `json:"author,omitempty"`
	Description    string   `json:"description,omitempty"`
	IconURL        string   `json:"iconUrl,omitempty"`  // віддалена іконка (Modrinth/CF)
	IconData       string   `json:"iconData,omitempty"` // data URL іконки з jar
	FileSize       int64    `json:"fileSize"`
	InstalledDate  string   `json:"installedDate"` // RFC3339 час створення файлу
	ProjectSlug    string   `json:"projectSlug,omitempty"`
	ProjectID      string   `json:"projectId,omitempty"`
	McVersion      string   `json:"mcVersion,omitempty"`
	LatestVersion  string   `json:"latestVersion,omitempty"`
	LatestURL      string   `json:"latestUrl,omitempty"`
	DependsOn      []string `json:"dependsOn,omitempty"` // id залежностей з метаданих
	HasMissingDeps bool     `json:"hasMissingDeps"`
	MissingDeps    []string `json:"missingDeps,omitempty"`
	Sha1           string   `json:"sha1,omitempty"`
	Status         string   `json:"status,omitempty"` // checking | done | error (перевірка оновлень)
	// IsDuplicate — той самий modId встановлено кількома файлами
	// одночасно (різні версії/копії одного мода). Рахується на бекенді
	// при скануванні (ScanModsFull), щоб список модів міг одразу
	// підсвітити такі рядки — раніше про дублікати можна було дізнатись
	// лише з окремої модалки CheckPreLaunchModIssues/FindDuplicateMods
	// перед запуском гри, а не безпосередньо в списку модів.
	IsDuplicate bool `json:"isDuplicate"`
	Error          string   `json:"error,omitempty"`
}

// ModVersionOption — варіант версії мода для модалки «Змінити версію».
type ModVersionOption struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	VersionNumber string `json:"versionNumber"`
	GameVersion   string `json:"gameVersion"`
	Loader        string `json:"loader"`
	URL           string `json:"url"`
	Filename      string `json:"filename"`
	Sha1          string `json:"sha1"`
	DatePublished string `json:"datePublished"`
	// IsCurrent — ця версія збігається з тією, що зараз встановлена
	// (визначається на бекенді за sha1/іменем файлу, а не порівнянням
	// versionNumber — воно ненадійне: mods.toml і Modrinth/CurseForge
	// часто форматують номер версії по-різному, напр. "6.0.8" проти
	// "mc1.20.1-6.0.8", тож рядки не збігаються навіть для тієї самої
	// версії). Фронтенд має покладатись саме на цей прапорець.
	IsCurrent bool `json:"isCurrent"`
}

// ModDependency — знайдена залежність мода (для модалки встановлення).
type ModDependency struct {
	ModID       string `json:"modId"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	DownloadURL string `json:"downloadUrl"`
	Filename    string `json:"filename"`
	IconURL     string `json:"iconUrl"`
	Resolved    bool   `json:"resolved"`
	Error       string `json:"error,omitempty"`
}

// ModBackupEntry — запис резервної копії мода (mods-backup/manifest.json).
type ModBackupEntry struct {
	ID         string `json:"id"`
	ModID      string `json:"modId"`
	Name       string `json:"name"`
	Version    string `json:"version"`
	FileName   string `json:"fileName"`   // оригінальне ім'я файлу в mods/
	BackupFile string `json:"backupFile"` // шлях відносно mods-backup/
	Date       string `json:"date"`
}

// DuplicateModGroup — група дублікатів (однаковий sha1, різні файли).
type DuplicateModGroup struct {
	Sha1 string     `json:"sha1"`
	Mods []ModEntry `json:"mods"`
}

// ModUpdateResult — підсумок масового оновлення модів.
type ModUpdateResult struct {
	Updated  []string `json:"updated"`
	Failed   []string `json:"failed"`
	BackedUp bool     `json:"backedUp"`
}

// PreLaunchModIssues — результат перевірки збірки перед запуском гри
// (дублікати + відсутні залежності) — модалка зі швидкими діями.
type PreLaunchModIssues struct {
	HasIssues   bool                `json:"hasIssues"`
	Duplicates  []DuplicateModGroup `json:"duplicates"`
	MissingDeps []string            `json:"missingDeps"`
}

type BrowserEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Version     string `json:"version"`
	Loader      string `json:"loader"`
	MCVersion   string `json:"mcVersion"`
	Description string `json:"description"`
	Downloads   int64  `json:"downloads"`
	Likes       int64  `json:"likes"`
	IconURL     string `json:"iconUrl"`
	Source      string `json:"source"`
}

type SkinEntry struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Variant string `json:"variant"`
	IsLocal bool   `json:"isLocal"`
}
