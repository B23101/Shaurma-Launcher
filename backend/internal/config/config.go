package config

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"shaurma-launcher-backend/internal/model"
	"shaurma-launcher-backend/internal/store"
	"shaurma-launcher-backend/internal/winbehavior"
)

var DefaultSettings = model.Settings{
	Language:     "uk",
	Accent:       "orange",
	AccentCustom: "#ff8a00",
	// Другий колір градієнта для кастомного акценту — дефолт narrower:
	// той самий тон, що заводський "orange" (deep-точка), щоб перший
	// показ пікера відразу давав видимий, а не випадковий градієнт.
	AccentCustom2:     "#ff5e00",
	AccentCustomMode:  "flat",
	Theme:             "dark",
	AnimationsEnabled: true,
	Font:              "inter",
	AutoUpdate:        true,
	UpdateChannel:     "stable",
	LastUpdateCheck:   "",
	ShowConsole:       false,
	SaveLogs:      true,
	ConfirmOnStop: true,
	MaxRAM:        4096,

	ConsoleMaxLines:        10000,
	ConsoleInfoColor:       "#8fd3ff",
	ConsoleWarnColor:       "#ffd166",
	ConsoleErrorColor:      "#ff6b6b",
	ConsoleWrap:            false,
	ConsoleAI:              true,
	ConsoleAIKey:           "",
	ConsoleFontSize:        12,
	ConsoleFontFamily:      "mono",
	ConsoleFontMode:        "all",
	ConsoleInfoFont:        "mono",
	ConsoleWarnFont:        "mono",
	ConsoleErrorFont:       "mono",
	ConsoleFontPath:        "",
	ShowConsoleOnCrash:     true,
	MaxConcurrentDownloads: 10,
	MaxRetries:             6,
	HTTPTimeoutSec:         60,
	WindowWidth:            854,
	WindowHeight:           480,
	RecordGameTime:         true,
	ShowRamWarnings:        true,
	// Розумний аналізатор запуску СЕРВЕРА: увімкнено за замовчуванням
	// (порт/RAM/Java/EULA/диск). Окремі «ігнорувати» — вимкнені.
	ShowServerWarnings: true,
	ShowSessionTime:        true,
	ShowGameTimeOnPage:     true,
	// Тимчасові моди: вимикати (не видаляти) за замовчуванням при
	// спрацюванні дати — безпечніший вибір (мод не губиться безповоротно
	// при помилково поставленій даті).
	TempModsDeleteByDefault: false,
	// Discord Rich Presence увімкнено за замовчуванням (як у старого
	// лаунчера); тексти — дефолтні шаблони з плейсхолдерами.
	DiscordRPCEnabled:      true,
	DiscordRPCDetails:      "{pack}",
	CleanupAfterPackDelete: true,
	DiscordRPCState:        "{list}",
	// Сповіщення про онлайн друзів Xbox — увімкнено за замовчуванням
	// (налаштовується у шухляді «Друзі» на віджеті акаунта).
	XboxOnlineNotify: true,

	// Sidebar: ширина 220px (стандарт), масштаб 1.0, «лише закріплені» — вимкнено.
	SidebarWidth:      220,
	SidebarScale:      1.0,
	SidebarOnlyPinned: false,
	// Кастомна тема: за замовчуванням — темні кольори лаунчера.
	ThemeCustomBg:   "#0f0f0f",
	ThemeCustomCard: "#161616",
	ThemeCustomText: "#ffffff",

	// Власний API-ключ CurseForge: порожній = використовується вбудований
	// ключ розробника (з -ldflags / env CF_API_KEY).
	CurseForgeAPIKey: "",
	// Заблоковані автором CurseForge-моди брати з Modrinth (як Prism
	// FallbackMRBlockedMods) — увімкнено за замовчуванням.
	UseModrinthForBlocked: true,
	// GPU-прискорення WebView2-фронтенду — увімкнено (вимикання потребує
	// перезапуску лаунчера).
	HardwareAcceleration: true,
	// Запускати гру на дискретній графіці (ноутбуки з двома GPU).
	// Дефолт — УВІМКНЕНО завжди: на одно-GPU системах цей запис у реєстрі
	// просто не матиме ефекту (шкоди нуль), а на ноутбуках із двома GPU
	// (typовий кейс — Intel iGPU + NVIDIA/AMD dGPU) з вимкненим за
	// замовчуванням перемикачем гра стартує на слабкій вбудованій графіці,
	// і перше враження від лаунчера — низький FPS. Тому не морочимось із
	// детекцією кількості GPU (WMI/реєстр — зайва крихкість і затримка на
	// старті) і просто вмикаємо одразу.
	UseDedicatedGPU: true,
	// Режим продуктивності (blur/анімації/фонові ефекти лаунчера) —
	// вимкнено за замовчуванням: більшість користувачів мають достатньо
	// потужні машини, і різкий UI без blur — свідомий вибір, не дефолт.
	PerformanceMode: false,
	// «Економний режим» (менша памʼять лаунчера+WebKit на слабких
	// машинах) — вимкнено за замовчуванням: він помітно знижує якість
	// рендерингу UI (без композитингу), тож це свідомий вибір користувача.
	LowMemoryMode: false,
	// Поведінка вікна (запуск із системою / × / згортання / гра) — єдине
	// джерело дефолтів у winbehavior.Defaults(). Для НОВОЇ установки
	// автозапуск — «з вікном» (НЕ у фоні: «У фоні» користувач вмикає
	// сам у налаштуваннях); наявні користувачі отримують «ніколи»/їхній
	// старий вибір — див. winbehavior.MigrateLegacy.
	StartupMode:        winbehavior.Defaults().StartupMode,
	CloseAction:        winbehavior.Defaults().CloseAction,
	MinimizeAction:     winbehavior.Defaults().MinimizeAction,
	GameLaunchAction:   winbehavior.Defaults().GameLaunchAction,
	GameLaunchDelaySec: winbehavior.Defaults().GameLaunchDelaySec,
	GameExitAction:     winbehavior.Defaults().GameExitAction,
	RestoreOnCrash:     winbehavior.Defaults().RestoreOnCrash,
	// Рекомендації модів на вкладці «Контент» — показуються за
	// замовчуванням (корисна фіча для більшості).
	DisableModSuggestions: false,
	// Alpha/beta оновлення модів — вимкнено за замовчуванням, щоб
	// нестабільні білди не підмінювали робочі release-версії тихцем.
	AllowAlphaBetaModUpdates: false,

	// ── Сповіщення (віджет у трей) ──
	// Головний вимикач УВІМКНЕНО, «не турбувати» ВИМКНЕНО, час показу 7 с
	// (компроміс: вистачає, щоб прочитати коротке повідомлення, і не
	// затримує екран). Значення продубльовані в SettingsSchema — див.
	// коментар там про синхронність двох джерел дефолтів.
	NotifyEnabled:     true,
	NotifyDurationSec: 7,
	NotifyDnd:         false,
	ChatMuteAll:       false,
	ChatMuteSound:     false,
	MutedChats:        []string{},
}

type Manager struct {
	mu       sync.RWMutex
	settings model.Settings
	accounts []model.Account
	st       *store.Store // SQLite-сховище (nil = працюємо в пам'яті)
	dir      string
	// installDir — РЕАЛЬНА тека встановлення лаунчера (та, куди користувач
	// поставив exe; передається з shell через backend_init {"installDir":..}).
	// Біля неї живуть стабільна тека конфігурації (launcher-location.json,
	// shaurma.db) — програма завжди розуміє свою справжню теку, хоч би куди
	// її встановили. Порожнє (dev-збірка/тести) = фолбек на %LOCALAPPDATA%
	// \ShaurmLauncher.
	installDir       string
	instDirOverride  string // кастомна тека збірок (installations) з launcher-location.json
	javaDirOverride  string // кастомна тека Java з launcher-location.json
	cacheDirOverride string // кастомна тека кешу з launcher-location.json
	logsDirOverride  string // кастомна тека логів з launcher-location.json
}

// defaultBaseDir — системний дефолт папки даних лаунчера. Як у старого
// лаунчера: на Windows це %APPDATA%\.shaurm (C:\Users\<user>\AppData\
// Roaming\.shaurm), на macOS — ~/Library/Application Support/.shaurm,
// на Linux — ~/.shaurm.
func defaultBaseDir() string {
	var dir string
	if os.Getenv("OS") == "Windows_NT" {
		if appdata := os.Getenv("APPDATA"); appdata != "" {
			dir = filepath.Join(appdata, ".shaurm")
		}
	}
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".shaurm")
	}
	return dir
}

// NewManager визначає папку даних лаунчера. Якщо у launcher-location.json
// (стабільна тека конфігурації) збережено кастомний baseDir — беремо його.
// installDir — реальна тека встановлення лаунчера (біля exe); порожнє =
// фолбек на %LOCALAPPDATA%\ShaurmLauncher (dev/тести).
func NewManager(installDir string) *Manager {
	m := &Manager{dir: defaultBaseDir(), settings: DefaultSettings, installDir: installDir}
	m.loadLocationConfig()
	return m
}

// LocationConfigFile — файл, де зберігається ПАПКА ДАНИХ лаунчера (baseDir)
// і кастомні шляхи підтек (installations/java). Лежить у РЕАЛЬНІЙ теці
// встановлення лаунчера (installDir — туди, куди користувач поставив exe;
// той самий installDir, що shell передає в backend_init), а НЕ в самій
// папці даних: якщо користувач перенесе/видалить папку даних, лаунчер
// завжди знає, де його справжні теки, і шляхи не гуляються.
//
// Фолбек (installDir порожній — dev-збірка, тести): %LOCALAPPDATA%\
// ShaurmLauncher, як раніше.
func (m *Manager) LocationConfigFile() string {
	if m.installDir != "" {
		return filepath.Join(m.installDir, "launcher-location.json")
	}
	if os.Getenv("OS") == "Windows_NT" {
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			return filepath.Join(local, "ShaurmLauncher", "launcher-location.json")
		}
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "shaurm-launcher", "launcher-location.json")
}

// legacyLocationConfigFile — старий шлях launcher-location.json (версія
// ≤2.0.x): він ЗАВЖДИ лежав у %LOCALAPPDATA%\ShaurmLauncher незалежно від
// edition (shaurma/base) і обраної теки встановлення. Повертає "" на
// не-Windows або якщо LOCALAPPDATA не заданий.
func (m *Manager) legacyLocationConfigFile() string {
	if os.Getenv("OS") == "Windows_NT" {
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			return filepath.Join(local, "ShaurmLauncher", "launcher-location.json")
		}
	}
	return ""
}

func (m *Manager) loadLocationConfig() {
	path := m.LocationConfigFile()
	data, err := os.ReadFile(path)
	if err != nil {
		// Міграція зі старої версії (≤2.0.x): launcher-location.json лежав у
		// %LOCALAPPDATA%\ShaurmLauncher для ОБОХ редакцій і будь-якої теки
		// встановлення. Після переходу «конфіг біля exe» base-редакція та
		// кастомні теки шукають його в іншому місці — щоб збережені
		// користувачем шляхи (baseDir/installations/java) не «зникли» після
		// оновлення, читаємо старий файл і переносимо його в нове місце
		// (одноразово, ідемпотентно).
		legacy := m.legacyLocationConfigFile()
		if legacy == "" || legacy == path {
			return
		}
		data, err = os.ReadFile(legacy)
		if err != nil {
			return
		}
		// Переносимо в нове місце (атомарно): наступний запуск прочитає
		// вже звідси. При невдачі запису лишаємо прочитані дані в пам'яті
		// — шляхи все одно застосуються цього разу.
		if tmp := path + ".tmp"; os.MkdirAll(filepath.Dir(path), 0755) == nil {
			if os.WriteFile(tmp, data, 0644) == nil {
				_ = os.Rename(tmp, path)
			}
		}
	}
	var loc struct {
		BaseDir       string `json:"baseDir"`
		Installations string `json:"installations"`
		Java          string `json:"java"`
		Cache         string `json:"cache"`
		Logs          string `json:"logs"`
	}
	if err := json.Unmarshal(data, &loc); err != nil {
		return
	}
	if loc.BaseDir != "" {
		m.dir = loc.BaseDir
	}
	m.instDirOverride = loc.Installations
	m.javaDirOverride = loc.Java
	m.cacheDirOverride = loc.Cache
	m.logsDirOverride = loc.Logs
}

// SaveLocationConfig пише baseDir + кастомні підтеки у launcher-location.json.
// Атомарний запис (.tmp + rename) — щоб обрив процесу не лишив битий файл,
// через який лаунчер перестав би знаходити свою папку даних.
func (m *Manager) SaveLocationConfig(baseDir, installations, javaDir, cacheDir, logsDir string) error {
	if baseDir == "" {
		baseDir = m.dir
	}
	data, err := json.MarshalIndent(map[string]string{
		"baseDir":       baseDir,
		"installations": installations,
		"java":          javaDir,
		"cache":         cacheDir,
		"logs":          logsDir,
	}, "", "  ")
	if err != nil {
		return err
	}
	path := m.LocationConfigFile()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (m *Manager) Dir() string { return m.dir }

// InstallDir повертає РЕАЛЬНУ теку встановлення лаунчера (той самий
// installDir, що shell передає в backend_init) — де лежить сам виконуваний
// файл і launcher-version.json. НЕ плутати з Dir() — та повертає теку
// ДАНИХ (%APPDATA%\.shaurm / ~/.shaurm), яка на Linux (AppImage/XDG) є
// зовсім іншим шляхом, ніж installDir. Порожній рядок — якщо installDir
// не передали (dev-збірка/тести); викликач має свій фолбек на цей випадок.
func (m *Manager) InstallDir() string { return m.installDir }

// Store повертає SQLite-сховище (nil, якщо базу не вдалось відкрити).
// Реєстри збірок/ярликів/галереї читають через нього: джерело правди —
// одна база даних, а не розкидані JSON-файли. nil-значення означає, що
// App працює у файловому фолбеку (dev/тести/пошкоджена БД).
func (m *Manager) Store() *store.Store { return m.st }

// DefaultBaseDir — системний дефолт папки даних лаунчера (для кнопки
// «За замовчуванням» на вкладці «Теки та шляхи»).
func (m *Manager) DefaultBaseDir() string { return defaultBaseDir() }

// FolderPaths повертає поточні шляхи всіх тек лаунчера для вкладки
// «Теки та шляхи»: папка даних, installations, java, cache, logs, config.
// Показуються ЕФЕКТИВНІ теки (з урахуванням кастомних шляхів користувача),
// а Custom-поля позначають, які з них задані вручну ("" = дефолтна похідна
// baseDir). Це фікс «settings показують .shaurm, а збірки у D:\ShaurmLauncher»:
// вкладка відображає реальні теки, а не лише похідні папки даних.
func (m *Manager) FolderPaths() model.FolderPaths {
	return model.FolderPaths{
		BaseDir:             m.dir,
		Installations:       m.InstancesDir(),
		Java:                m.JavaDir(),
		Cache:               m.CacheDir(),
		Logs:                m.LogsDir(),
		Config:              m.ConfigDir(),
		InstallationsCustom: m.customDirFor("installations"),
		JavaCustom:          m.customDirFor("java"),
		CacheCustom:         m.customDirFor("cache"),
		LogsCustom:          m.customDirFor("logs"),
		DefaultBaseDir:      m.DefaultBaseDir(),
	}
}

// SetAppDir змінює папку даних лаунчера на вибрану користувачем теку і
// зберігає її у launcher-location.json. При зміні ВСІ дані (збірки, Java,
// кеш, логи) перемігруються у нову теку на боці App (див. App.SetAppDir).
func (m *Manager) SetAppDir(newDir string) error {
	if newDir == "" {
		return fmt.Errorf("порожня папка даних")
	}
	oldDir := m.dir
	m.dir = newDir
	if err := m.EnsureDirs(); err != nil {
		return err
	}
	// Тримаємо settings.InstanceDir/JavaPath синхронними з новою папкою
	// даних У ПАМ'ЯТІ (без запису в БД — майстер пише settings лише на
	// finish, щоб не зламати IsFirstRun). Кастомні шляхи користувача
	// (відмінні від похідної СТАРОЇ папки даних) НЕ затираємо — вони
	// лишаються як є і продовжують діяти (див. customDirFor).
	oldInstDef := filepath.Join(oldDir, "installations")
	if samePathCleaned(m.settings.InstanceDir, oldInstDef) {
		m.settings.InstanceDir = filepath.Join(newDir, "installations")
	}
	oldJavaDef := filepath.Join(oldDir, "java")
	if samePathCleaned(m.settings.JavaPath, oldJavaDef) {
		m.settings.JavaPath = filepath.Join(newDir, "java")
	}
	// Зберігаємо кастомні override-шляхи (installations/java/cache/logs),
	// щоб зміна папки даних їх не стерла.
	return m.SaveLocationConfig(newDir, m.instDirOverride, m.javaDirOverride, m.cacheDirOverride, m.logsDirOverride)
}

// SetFolderPaths збережено лише для сумісності зі старим wizard-флоу
// (finish() викликав його з instanceDir/javaPath). Тепер ці теки завжди
// деривативи baseDir — окремі шляхи не зберігаються, метод нічого не
// робить і повертає nil (щоб старий виклик не впав).
func (m *Manager) SetFolderPaths(installations, javaDir string) error {
	return nil
}

// SetFolderPath змінює одну теку лаунчера за іменем (kind). Модель: УСІ
// дані живуть в одній теці .shaurm (baseDir), яку можна змінити цілком
// (SetAppDir) — окремі кастомні підтеки (installations/java/cache/logs)
// не налаштовуються, вони завжди деривативи baseDir.
func (m *Manager) SetFolderPath(kind, path string) error {
	switch kind {
	case "baseDir":
		return m.SetAppDir(path)
	case "installations", "java", "cache", "logs":
		return fmt.Errorf("%s завжди всередині папки даних лаунчера — змінюється лише сама папка даних (.shaurm)", kind)
	default:
		return fmt.Errorf("невідома тека: %s", kind)
	}
}

// samePathCleaned порівнює два шляхи нормалізовано: filepath.Clean + ігнор
// регістру на Windows ("D:\Mine" і "d:\mine\" — один і той самий шлях).
func samePathCleaned(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// customDirFor повертає КАСТОМНУ теку підтеки ("" = дефолтна похідна
// baseDir). Джерела кастомної теки, у порядку пріоритету:
//  1. override з launcher-location.json (користувач явно обрав теку);
//  2. збережене значення settings (InstanceDir/JavaPath) — якщо воно
//     відрізняється від похідної baseDir (легасі-вибір користувача зі
//     старого лаунчера, напр. D:\ShaurmLauncher замість .shaurm).
//
// МОДЕЛЬ: усе живе в одній теці .shaurm (baseDir) — кастомна тека діє
// ЛИШЕ поки реально існує на диску (fail-safe для легасі-даних, які
// одноразова консолідація reconcileLegacyFolders ще не перенесла
// всередину .shaurm). Як тільки дані перенесені й стара тека зникла —
// повертається "", і ефективною знову стає похідна baseDir. Це усуває
// дисинхронізацію «settings показують D:\ShaurmLauncher, а збірки у
// .shaurm»: відображення і фактичне розташування завжди збігаються.
func (m *Manager) customDirFor(kind string) string {
	var override, s string
	switch kind {
	case "installations":
		override, s = m.instDirOverride, m.settings.InstanceDir
	case "java":
		override, s = m.javaDirOverride, m.settings.JavaPath
	case "cache":
		override = m.cacheDirOverride
		// "logs" навмисно відсутній: тека логів завжди біля exe
		// (ConfigDir/logs, див. LogsDir), кастомних тек логів більше немає.
	}
	var custom string
	if override != "" {
		custom = override
	} else if s != "" {
		def := filepath.Join(m.dir, kind)
		if !samePathCleaned(s, def) {
			custom = s
		}
	}
	// Кастомна тека діє, лише якщо реально існує на диску. Якщо шляху
	// немає (застаріле значення, легасі-шлях уже перенесений у .shaurm,
	// або override вказує на неіснуючу теку) — ефективною є похідна
	// baseDir: лаунчер ніколи не «гуляє» між двома джерелами правди.
	if custom != "" && dirExists(custom) {
		return custom
	}
	return ""
}

// dirExists — чи існує шлях на диску (для fail-safe кастомних тек).
func dirExists(p string) bool {
	if p == "" {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}

// CustomInstancesDir / CustomJavaDir — кастомні теки збірок/Java, якщо вони
// задані (override або легасі settings). Потрібні одноразовій консолідації
// (App.reconcileLegacyFolders): все, що колись лежало окремо, переноситься
// ВСЕРЕДИНІ папки даних .shaurm, після чого кастомних тек більше немає.
func (m *Manager) CustomInstancesDir() string { return m.customDirFor("installations") }
func (m *Manager) CustomJavaDir() string      { return m.customDirFor("java") }

// ClearCustomDirs прибирає кастомні override-шляхи з launcher-location.json
// (після успішної консолідації легасі-тек усередині .shaurm) — щоб
// кастомні шляхи не «воскресали» при наступних запусках.
func (m *Manager) ClearCustomDirs() error {
	m.instDirOverride = ""
	m.javaDirOverride = ""
	m.cacheDirOverride = ""
	m.logsDirOverride = ""
	return m.SaveLocationConfig(m.dir, "", "", "", "")
}

// FolderDir повертає ЕФЕКТИВНУ теку за іменем (kind): з урахуванням
// кастомних шляхів користувача. Для kind == "baseDir" — саму папку даних.
func (m *Manager) FolderDir(kind string) string {
	switch kind {
	case "baseDir":
		return m.dir
	case "installations":
		return m.InstancesDir()
	case "java":
		return m.JavaDir()
	case "cache":
		return m.CacheDir()
	case "logs":
		return m.LogsDir()
	}
	return ""
}

func (m *Manager) CacheDir() string {
	if c := m.customDirFor("cache"); c != "" {
		return c
	}
	return filepath.Join(m.dir, "cache")
}

// LogsDir — тека логів лаунчера (launcher.log, launcher-errors.json).
// Живе У ТЕЦІ ВСТАНОВЛЕННЯ (ConfigDir/logs), а НЕ в папці даних .shaurm:
// файл логу ВІДКРИТИЙ процесом увесь час роботи лаунчера, і якщо він
// лежить у папці даних — зміна папки даних (SetAppDir/migrateDataDir)
// падала на "rename ...launcher.log: file is being used by another process"
// (Windows забороняє перейменовувати відкриті файли). Лог — технічний
// файл програми, він має бути біля exe і не залежати від того, куди
// користувач переніс свої дані.
func (m *Manager) LogsDir() string {
	return filepath.Join(m.ConfigDir(), "logs")
}

// ConfigDir — стабільна тека конфігурації лаунчера (та сама, де лежить
// launcher-location.json — реальна тека встановлення). Сюди пишуться
// НАЛАШТУВАННЯ, чернетка майстра та акаунти — речі, потрібні лаунчеру для
// запуску. На відміну від папки даних (baseDir) ця тека НІКОЛИ не змінюється
// сама по собі: перенесення чи видалення папки даних не може загубити
// конфігурацію.
func (m *Manager) ConfigDir() string {
	return filepath.Dir(m.LocationConfigFile())
}

// SettingsPath / AccountsPath / SetupProgressPath — шляхи до ЛЕГАСОВИХ
// JSON-файлів (лежали у стабільній теці конфігурації до переходу на
// SQLite). Тепер дані зберігаються в БД (ConfigDir/shaurma.db); ці файли
// лишились лише для одноразового інструмента міграції (cmd/migrate) та
// фолбеку IsFirstRun, коли базу не вдалось відкрити.
func (m *Manager) SettingsPath() string { return filepath.Join(m.ConfigDir(), "settings.json") }
func (m *Manager) AccountsPath() string { return filepath.Join(m.ConfigDir(), "accounts.json") }
func (m *Manager) PlaytimePath() string { return filepath.Join(m.ConfigDir(), "playtime.json") }
func (m *Manager) InstanceConfigsPath() string {
	return filepath.Join(m.ConfigDir(), "instance-configs.json")
}
func (m *Manager) SetupProgressPath() string {
	return filepath.Join(m.ConfigDir(), "setup-progress.json")
}

// InstancesDir — тека збірок лаунчера. За замовчуванням — baseDir/
// installations, але якщо користувач тримає збірки у ВЛАСНІЙ теці
// (кастомний override або легасі settings.InstanceDir, напр.
// D:\ShaurmLauncher) — використовується саме вона: папка даних не
// прив'язана намертво до %APPDATA%, і лаунчер підпорядковується шляху
// користувача (див. customDirFor).
func (m *Manager) InstancesDir() string {
	if c := m.customDirFor("installations"); c != "" {
		return c
	}
	return filepath.Join(m.dir, "installations")
}

// ShaurmaPacksDir — тека Shaurma-збірок (окремо від кастомних).
// custom packs: installations/<id>/
// shaurma packs: installations/shaurma_packs/<id>/
func (m *Manager) ShaurmaPacksDir() string {
	return filepath.Join(m.InstancesDir(), "shaurma_packs")
}

// JavaDir — власна тека вбудованої Java лаунчера. Лаунчер сам
// встановлює Java сюди при першому запуску, якщо її ще немає
// (це за ТЗ: "лаунчер має власну теку з java, він сам її поставить").
// За замовчуванням — baseDir/java; кастомна тека користувача
// (override / легасі settings.JavaPath) має пріоритет.
func (m *Manager) JavaDir() string {
	if c := m.customDirFor("java"); c != "" {
		return c
	}
	return filepath.Join(m.dir, "java")
}

// JavaExePath — шлях до виконуваного файлу вбудованої Java (Windows).
func (m *Manager) JavaExePath() string {
	return filepath.Join(m.JavaDir(), "bin", "javaw.exe")
}

func (m *Manager) EnsureDirs() error {
	for _, d := range []string{m.dir, m.InstancesDir(), m.CacheDir(), m.LogsDir(), m.JavaDir(), m.ConfigDir()} {
		if err := os.MkdirAll(d, 0755); err != nil {
			return err
		}
		// Windows: теки даних лаунчера не індексуються Windows Search
		// (атрибут NOT_CONTENT_INDEXED) — інакше під час встановлення
		// збірки, коли створюються тисячі файлів, індексатор і провідник
		// гальмують (користувач скаржився на лаги Explorer при інсталяції).
		MarkNotIndexed(d)
	}
	return nil
}

// migrateConfigFiles переносить конфігурацію зі старої папки даних
// (baseDir), де вона лежала раніше, у стабільну теку конфігурації —
// щоб користувач не втратив налаштування, прогрес майстра та акаунти
// після оновлення. Якщо у новому місці файл уже є — старий зайвий.
func (m *Manager) migrateConfigFiles() {
	names := []string{"settings.json", "setup-progress.json", "accounts.json"}
	for _, name := range names {
		old := filepath.Join(m.dir, name)
		if _, err := os.Stat(old); err != nil {
			continue
		}
		if err := os.MkdirAll(m.ConfigDir(), 0755); err != nil {
			continue
		}
		dest := filepath.Join(m.ConfigDir(), name)
		if _, err := os.Stat(dest); err == nil {
			os.Remove(old)
			continue
		}
		os.Rename(old, dest)
	}

	// Міграція зі старого місця конфігурації (версія ≤2.0.x): раніше
	// launcher-location.json і shaurma.db ЗАВЖДИ лежали в
	// %LOCALAPPDATA%\ShaurmLauncher (для обох редакцій). Тепер вони живуть
	// біля exe (installDir). Якщо в новому місці файлу немає, а у старому
	// є — переносимо, щоб збережені налаштування й акаунти не «зникли»
	// після оновлення (base-редакція та кастомні теки встановлення).
	legacy := m.legacyLocationConfigFile()
	if legacy != "" && legacy != m.LocationConfigFile() {
		cfgDest := m.LocationConfigFile()
		if _, err := os.Stat(cfgDest); err != nil && m.installDir != "" {
			if data, err := os.ReadFile(legacy); err == nil {
				if os.MkdirAll(filepath.Dir(cfgDest), 0755) == nil {
					_ = os.WriteFile(cfgDest, data, 0644)
				}
			}
		}
		// Міграція старого shaurma.db: переносимо з legacy-місця в ConfigDir,
		// щоб store.MigrateFromSQLiteIfAvailable міг його знайти.
		legacyDB := filepath.Join(filepath.Dir(legacy), "shaurma.db")
		dbDest := filepath.Join(m.ConfigDir(), "shaurma.db")
		if _, err := os.Stat(dbDest); err != nil && m.installDir != "" {
			if _, err := os.Stat(legacyDB); err == nil {
				if os.MkdirAll(m.ConfigDir(), 0755) == nil {
					_ = os.Rename(legacyDB, dbDest)
				}
			}
		}
	}
}

// ResolveJavaExe перетворює збережений javaPath у шлях до javaw.exe для
// запуску гри. javaPath тепер — ПАПКА Java (дика, де лаунчер тримає версії),
// тому виконуваний файл шукається всередині неї як bin\javaw.exe. Якщо
// значення вже вказує на файл — повертається як є.
func (m *Manager) ResolveJavaExe(path string) string {
	if path == "" {
		return m.JavaExePath()
	}
	if strings.HasSuffix(strings.ToLower(path), "javaw.exe") {
		return path
	}
	return filepath.Join(path, "bin", "javaw.exe")
}

// openStore відкриває SQLite-базу в стабільній теці конфігурації
// (ConfigDir/shaurma.db), застосовуючи версіоновані міграції схеми. При
// помилці (пошкоджена/зайнята база) лаунчер НЕ падає — менеджер лишається
// у пам'яті з дефолтами (як раніше при відсутньому settings.json), а
// методи збереження тихо стають no-op.
func (m *Manager) openStore() {
	st, err := store.Open(m.ConfigDir(), m.InstancesDir())
	if err != nil {
		log.Printf("store: не вдалось відкрити JSON store: %v — лаунчер працює у пам'яті без збереження", err)
		m.st = nil
		return
	}
	// Друга тека збірок (офіційні Shaurma) — БЕЗ цього виклику
	// GetInstanceConfig/SaveInstanceConfig для shaurma-збірок писали й
	// читали override-и (RAM/Java/Fullscreen/AccountID) з installations/
	// (custom-теки), хоча сама збірка фізично лежить у ShaurmaPacksDir() —
	// дані для офіційних збірок опинялись у сирітському файлі не там, де
	// решта instance.json цієї ж збірки.
	st.SetShaurmaInstancesDir(m.ShaurmaPacksDir())
	// 1. Конвертація зі старого SQLite: якщо є shaurma.db — переносимо все в JSON.
	st.MigrateFromSQLiteIfAvailable(m.ConfigDir())
	// 2. Переносимо customPacks з shaurma.json → instance.json (одноразово).
	st.MigrateCustomPacksToInstance()
	// 2b. Переносимо ігрові сесії зі спільного масиву shaurma.json →
	// playtimeSessions кожного instance.json (одноразово, ПІСЛЯ 2, щоб
	// теки custom-збірок з попереднього кроку вже існували).
	st.MigratePlaytimeSessionsToInstances()
	// Видаляємо застарілі поля (customPacks, installState) з shaurma.json
	st.CleanupStaleFields()
	// 3. Оновлюємо кеш Shaurma ID з вже мігрованого registry
	// (після міграції BuildsRegistry може містити shaurma-збірки).
	st.RefreshKnownShaurmaIDs()
	// 4. Авто-виявлення збірок: скануємо installations на .minecraft папки
	// і реєструємо знайдені збірки.
	st.ScanAndRegisterInstances()
	m.st = st
}

func (m *Manager) Load() error {
	m.migrateConfigFiles()
	m.EnsureDirs()
	m.openStore()
	if err := m.loadSettings(); err != nil {
		m.settings = DefaultSettings
	}
	m.normalize()
	m.loadAccounts()
	return nil
}

// importLegacyIntoStore переносить дані зі старої JSON-системи збереження
// у SQLite. Кожна сутність імпортується лише один раз — тільки коли в БД
// для неї ще НІЧОГО немає (перший запуск після переходу на хранище). Після
// успішного переносу відповідний легасі-JSON видаляється, щоб уникнути
// повторного/дублюючого імпорту на наступних запусках.
// metaLegacyImported — прапорець «легасі-JSON (settings/accounts/playtime/
// instance-configs/setup-progress) уже імпортовано» в app_meta. БЕЗ цього
// прапорця importLegacyIntoStore вирішувала «чи мігрувати» лише за
// len(x) == 0, а це легітимний стан і після успішної міграції (акаунт
// вилогинений, ігровий час обнулено вручну тощо) — тож функція намагалась
// імпортувати легасі-JSON ЗНОВУ при кожному запуску, де таблиця виявлялась
// порожньою. Якщо стара версія файлу з якоїсь причини не видалилась
// (os.Remove мовчки не спрацював — файл заблокований антивірусом/іншим
// процесом, немає прав) — це виглядало як «дані самі скидаються»: щойно
// користувач легітимно спорожнював якусь сутність, наступний запуск міг
// підмінити її застарілим станом зі старого JSON. Прапорець (як і
// files_migrated у migration.go) робить імпорт рівно одноразовим.
// importLegacyIntoStore — прибрано. Тепер дані зберігаються в JSON, а
// міграція з SQLite відбувається в store.MigrateFromSQLiteIfAvailable().

func (m *Manager) loadSettings() error {
	if m.st == nil {
		return fmt.Errorf("SQLite-сховище недоступне")
	}
	raw, ok, err := m.st.GetSettingsRaw()
	if err != nil {
		return err
	}
	if !ok {
		return os.ErrNotExist
	}
	// Міграція нових bool-налаштувань: `false` відрізнити від «не задано»
	// неможливо, тож перевіряємо наявність ключів у JSON-блобі і для
	// відсутніх виставляємо значення за замовчуванням (ShowSessionTime /
	// ShowGameTimeOnPage / Discord RPC / авто-очищення за замовчуванням
	// увімкнені).
	var rawMap map[string]json.RawMessage
	if json.Unmarshal([]byte(raw), &rawMap) == nil {
		if _, ok := rawMap["showSessionTime"]; !ok {
			m.settings.ShowSessionTime = DefaultSettings.ShowSessionTime
		}
		if _, ok := rawMap["showGameTimeOnPage"]; !ok {
			m.settings.ShowGameTimeOnPage = DefaultSettings.ShowGameTimeOnPage
		}
		if _, ok := rawMap["discordRpcEnabled"]; !ok {
			m.settings.DiscordRPCEnabled = DefaultSettings.DiscordRPCEnabled
		}
		if _, ok := rawMap["cleanupAfterPackDelete"]; !ok {
			m.settings.CleanupAfterPackDelete = DefaultSettings.CleanupAfterPackDelete
		}
		// animationsEnabled — те саме: старий settings.json без цього
		// ключа не повинен вимкнути анімації всім наявним користувачам
		// (за замовчуванням увімкнено, вимикає лише свідомий вибір у
		// «Вигляд»).
		if _, ok := rawMap["animationsEnabled"]; !ok {
			m.settings.AnimationsEnabled = DefaultSettings.AnimationsEnabled
		}
		// xboxOnlineNotify — старий settings.json без ключа: дефолт увімкнено.
		if _, ok := rawMap["xboxOnlineNotify"]; !ok {
			m.settings.XboxOnlineNotify = DefaultSettings.XboxOnlineNotify
		}
		// useDedicatedGPU — старий settings.json без ключа (лаунчер
		// оновився з версії, де цього перемикача ще не було): дефолт
		// тепер УВІМКНЕНО, щоб наявні користувачі з двома GPU одразу
		// отримали продуктивний запуск, а не мовчазний false.
		if _, ok := rawMap["useDedicatedGPU"]; !ok {
			m.settings.UseDedicatedGPU = DefaultSettings.UseDedicatedGPU
		}
		// Нові перемикачі (Performance Mode / автозапуск / рекомендації
		// модів / alpha-beta оновлення) — при міграції зі старого
		// settings.json без цих ключів підставляємо дефолти, а не
		// нульовий Go false (для performanceMode false — це і так дефолт,
		// але явна гілка лишена для симетрії й на випадок майбутньої
		// зміни дефолту).
		if _, ok := rawMap["performanceMode"]; !ok {
			m.settings.PerformanceMode = DefaultSettings.PerformanceMode
		}
		if _, ok := rawMap["disableModSuggestions"]; !ok {
			m.settings.DisableModSuggestions = DefaultSettings.DisableModSuggestions
		}
		if _, ok := rawMap["allowAlphaBetaModUpdates"]; !ok {
			m.settings.AllowAlphaBetaModUpdates = DefaultSettings.AllowAlphaBetaModUpdates
		}
		// showServerWarnings — головний вимикач аналізатора запуску
		// СЕРВЕРА: у старому settings.json ключа немає, а значення за
		// замовчуванням — увімкнено. Без цієї гілки наявні користувачі
		// отримали б мовчазний false і жодного попередження ніколи.
		if _, ok := rawMap["showServerWarnings"]; !ok {
			m.settings.ShowServerWarnings = DefaultSettings.ShowServerWarnings
		}
		// notifyEnabled — віджет сповіщень у трей: нове налаштування, у
		// старому settings.json ключа немає, а дефолт — увімкнено. Без цієї
		// гілки кожен наявний користувач після оновлення отримав би мовчазний
		// false і ніколи не побачив би тостів, хоча фіча нова й очікувана.
		if _, ok := rawMap["notifyEnabled"]; !ok {
			m.settings.NotifyEnabled = DefaultSettings.NotifyEnabled
		}
		// notifyDurationSec — та сама історія для INT: відсутність ключа дає
		// 0, а 0 — не валідний час показу (див. normalize, який докладає
		// дефолт і стискає до 3..60).
		if _, ok := rawMap["notifyDurationSec"]; !ok {
			m.settings.NotifyDurationSec = DefaultSettings.NotifyDurationSec
		}
		// notifyDnd дефолт false = «сповіщення показувати», тож відсутність
		// ключа вже дає правильне значення. Гілка лишена за симетрією (той
		// самий патерн, що й performanceMode вище): якщо колись дефолт
		// зміниться на true, старі користувачі не отримають сюрпризу.
		if _, ok := rawMap["notifyDnd"]; !ok {
			m.settings.NotifyDnd = DefaultSettings.NotifyDnd
		}
		// Мути чатів: ті самі «відсутні = дефолт» міграції, що й вище —
		// старий settings.json без цих ключів не ламає нормалізацію.
		if _, ok := rawMap["chatMuteAll"]; !ok {
			m.settings.ChatMuteAll = DefaultSettings.ChatMuteAll
		}
		if _, ok := rawMap["chatMuteSound"]; !ok {
			m.settings.ChatMuteSound = DefaultSettings.ChatMuteSound
		}
		if m.settings.MutedChats == nil {
			m.settings.MutedChats = []string{}
		}
	}
	if err := json.Unmarshal([]byte(raw), &m.settings); err != nil {
		return err
	}
	// Поведінка вікна: старі булі (openOnStartup/closeOnLaunch/
	// exitOnGameClose) → нові режими. Робимо ПІСЛЯ Unmarshal, щоб вже
	// збережений вибір у новому форматі не перезаписувався.
	if rawMap != nil {
		migrateWindowBehavior(&m.settings, rawMap)
	}
	return nil
}

// migrateWindowBehavior — обгортка над winbehavior.MigrateLegacy для
// model.Settings (зв'язує поля моделі з чистим пакетом рішень).
func migrateWindowBehavior(s *model.Settings, raw map[string]json.RawMessage) {
	w := winbehavior.MigrateLegacy(raw, windowSettingsOf(*s))
	applyWindowSettings(s, w)
}

func windowSettingsOf(s model.Settings) winbehavior.Settings {
	return winbehavior.Settings{
		StartupMode:        s.StartupMode,
		CloseAction:        s.CloseAction,
		MinimizeAction:     s.MinimizeAction,
		GameLaunchAction:   s.GameLaunchAction,
		GameLaunchDelaySec: s.GameLaunchDelaySec,
		GameExitAction:     s.GameExitAction,
		RestoreOnCrash:     s.RestoreOnCrash,
	}
}

func applyWindowSettings(s *model.Settings, w winbehavior.Settings) {
	s.StartupMode = w.StartupMode
	s.CloseAction = w.CloseAction
	s.MinimizeAction = w.MinimizeAction
	s.GameLaunchAction = w.GameLaunchAction
	s.GameLaunchDelaySec = w.GameLaunchDelaySec
	s.GameExitAction = w.GameExitAction
	s.RestoreOnCrash = w.RestoreOnCrash
}

// WindowSettings повертає налаштування вікна для чистих функцій рішень
// (internal/winbehavior) — єдина точка зв'язку Settings → winbehavior.
func WindowSettings(s model.Settings) winbehavior.Settings { return windowSettingsOf(s) }

// normalize доповнює налаштування дефолтними значеннями для полів, яких
// бракує в settings.json (новий або застарілий файл). Без цього
// параметри з майстра "існували б, але не працювали" — тепер будь-яке
// поле, яке користувач не вказував, отримує робочий дефолт одразу.
func (m *Manager) normalize() {
	s := &m.settings
	if s.Language == "" {
		s.Language = DefaultSettings.Language
	}
	if s.Accent == "" {
		s.Accent = DefaultSettings.Accent
	}
	if s.Theme == "" {
		s.Theme = DefaultSettings.Theme
	}
	if s.Font == "" {
		s.Font = DefaultSettings.Font
	}
	if s.Font == "custom" && s.FontPath == "" {
		s.Font = DefaultSettings.Font
	}
	if s.InstanceDir == "" {
		s.InstanceDir = m.InstancesDir()
	}
	if s.JavaPath == "" {
		// За замовчуванням — власна вбудована Java лаунчера, а не
		// команда "java" з PATH. Це ПАПКА (m.JavaDir()), а не шлях до
		// файлу: користувач бачить теку, де лаунчер тримає всі версії
		// Java. Якщо її ще немає на диску — лаунчер встановить її сам
		// при першому запуску гри.
		s.JavaPath = m.JavaDir()
	}
	// Старий формат settings.json зберігав javaPath як шлях до файлу
	// (...\java\bin\javaw.exe). Тепер це ПАПКА Java — приводимо старі
	// значення до нового вигляду, щоб поле не показувало шлях до файлу.
	if strings.HasSuffix(s.JavaPath, "javaw.exe") {
		s.JavaPath = filepath.Dir(filepath.Dir(s.JavaPath))
	}
	if s.MaxRAM <= 0 {
		s.MaxRAM = DefaultSettings.MaxRAM
	}
	if s.UpdateChannel == "" {
		s.UpdateChannel = DefaultSettings.UpdateChannel
	}
	// Поведінка вікна: невідомі/порожні режими → дефолти, затримка 0..120.
	// RestoreOnCrash тут не чіпаємо (bool; його відсутність у старому
	// файлі вирішує MigrateLegacy, а збережене false має зберігатись).
	applyWindowSettings(s, winbehavior.Normalize(windowSettingsOf(*s)))
	// Sidebar: ширину/масштаб треба нормалізувати до дозволених меж —
	// запис у БД може містити старі (застарілі) значення, і без цього
	// сайдбар міг би вийти за екран або стати крихітним.
	if s.SidebarWidth < 220 {
		s.SidebarWidth = DefaultSettings.SidebarWidth
	}
	if s.SidebarWidth > 400 {
		s.SidebarWidth = 400
	}
	if s.SidebarScale <= 0 {
		s.SidebarScale = DefaultSettings.SidebarScale
	}
	if s.SidebarScale > 1.5 {
		s.SidebarScale = 1.5
	}
	// Кастомна тема: порожні кольори → дефолти темної теми.
	if s.ThemeCustomBg == "" {
		s.ThemeCustomBg = DefaultSettings.ThemeCustomBg
	}
	if s.ThemeCustomCard == "" {
		s.ThemeCustomCard = DefaultSettings.ThemeCustomCard
	}
	if s.ThemeCustomText == "" {
		s.ThemeCustomText = DefaultSettings.ThemeCustomText
	}
	if s.AccentCustom == "" {
		s.AccentCustom = DefaultSettings.AccentCustom
	}
	if s.AccentCustom2 == "" {
		s.AccentCustom2 = DefaultSettings.AccentCustom2
	}
	if s.AccentCustomMode != "gradient" && s.AccentCustomMode != "flat" {
		s.AccentCustomMode = DefaultSettings.AccentCustomMode
	}
	if s.ConsoleMaxLines <= 0 {
		s.ConsoleMaxLines = DefaultSettings.ConsoleMaxLines
	}
	if s.ConsoleInfoColor == "" {
		s.ConsoleInfoColor = DefaultSettings.ConsoleInfoColor
	}
	if s.ConsoleWarnColor == "" {
		s.ConsoleWarnColor = DefaultSettings.ConsoleWarnColor
	}
	if s.ConsoleErrorColor == "" {
		s.ConsoleErrorColor = DefaultSettings.ConsoleErrorColor
	}
	if s.ConsoleFontSize <= 0 {
		s.ConsoleFontSize = DefaultSettings.ConsoleFontSize
	}
	if s.ConsoleFontFamily == "" {
		s.ConsoleFontFamily = DefaultSettings.ConsoleFontFamily
	}
	if s.ConsoleFontMode == "" {
		s.ConsoleFontMode = DefaultSettings.ConsoleFontMode
	}
	// Сповіщення у трей: час показу картки. 0 (файл без ключа або ручне
	// правкування) → дефолт; далі обов'язково стискаємо до 3..60 с —
	// інакше користувач міг би записати через UI чи руками значення, яке
	// або не встигнеш прочитати (1 с), або зависне на екрані на хвилини.
	// Межі живуть у notifyDurationRange (та сама пара використовується
	// в SettingsSchema, тож UI і нормалізація ніколи не розійдуться).
	if s.NotifyDurationSec <= 0 {
		s.NotifyDurationSec = DefaultSettings.NotifyDurationSec
	}
	if s.NotifyDurationSec < notifyDurationRange.min {
		s.NotifyDurationSec = notifyDurationRange.min
	}
	if s.NotifyDurationSec > notifyDurationRange.max {
		s.NotifyDurationSec = notifyDurationRange.max
	}
	// Discord RPC підв'язаний до перемикача в налаштуваннях; дефолт
	// (увімкнено) ставився в loadSettings для старих settings.json без
	// ключа, а тут НЕ чіпаємо — щоб збережене `false` зберігалося.
	// Тексти Discord-присутності: порожні значення → дефолтні шаблони
	// (старий settings.json без цих полів отримує робочі дефолти одразу).
	if s.DiscordRPCDetails == "" {
		s.DiscordRPCDetails = DefaultSettings.DiscordRPCDetails
	}
	if s.DiscordRPCState == "" {
		s.DiscordRPCState = DefaultSettings.DiscordRPCState
	}
	if s.ConsoleInfoFont == "" {
		s.ConsoleInfoFont = DefaultSettings.ConsoleInfoFont
	}
	if s.ConsoleWarnFont == "" {
		s.ConsoleWarnFont = DefaultSettings.ConsoleWarnFont
	}
	if s.ConsoleErrorFont == "" {
		s.ConsoleErrorFont = DefaultSettings.ConsoleErrorFont
	}
	if s.MaxConcurrentDownloads <= 0 {
		s.MaxConcurrentDownloads = DefaultSettings.MaxConcurrentDownloads
	}
	if s.MaxRetries <= 0 {
		s.MaxRetries = DefaultSettings.MaxRetries
	}
	if s.HTTPTimeoutSec <= 0 {
		s.HTTPTimeoutSec = DefaultSettings.HTTPTimeoutSec
	}
	if s.WindowWidth <= 0 {
		s.WindowWidth = DefaultSettings.WindowWidth
	}
	if s.WindowHeight <= 0 {
		s.WindowHeight = DefaultSettings.WindowHeight
	}
}

func (m *Manager) SaveSettings() error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.st == nil {
		return nil
	}
	data, err := json.Marshal(m.settings)
	if err != nil {
		return err
	}
	return m.st.SaveSettingsRaw(string(data))
}

func (m *Manager) GetSettings() model.Settings {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.settings
}

func (m *Manager) UpdateSettings(s model.Settings) error {
	m.mu.Lock()
	m.settings = s
	m.normalize()
	m.mu.Unlock()
	return m.SaveSettings()
}

// ── Налаштування гри «за замовчуванням» (minecraft_defaults) ──────────

// GetMinecraftDefaults повертає збережені дефолтні налаштування гри.
// Якщо запису ще немає (або сховище недоступне) — дефолти за замовчуванням
// (Enabled=false, тобто функцію вимкнено).
func (m *Manager) GetMinecraftDefaults() model.MinecraftDefaults {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.st == nil {
		return model.DefaultMinecraftDefaults()
	}
	raw, ok, err := m.st.GetMinecraftDefaultsRaw()
	if err != nil || !ok {
		return model.DefaultMinecraftDefaults()
	}
	var d model.MinecraftDefaults
	if json.Unmarshal([]byte(raw), &d) != nil {
		return model.DefaultMinecraftDefaults()
	}
	model.NormalizeMinecraftDefaults(&d)
	return d
}

// SaveMinecraftDefaults зберігає дефолтні налаштування гри у SQLite.
func (m *Manager) SaveMinecraftDefaults(d model.MinecraftDefaults) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.st == nil {
		return nil
	}
	model.NormalizeMinecraftDefaults(&d)
	data, err := json.Marshal(d)
	if err != nil {
		return err
	}
	return m.st.SaveMinecraftDefaultsRaw(string(data))
}

func (m *Manager) loadAccounts() {
	if m.st == nil {
		m.accounts = []model.Account{}
		return
	}
	accounts, err := m.st.LoadAccounts()
	if err != nil {
		m.accounts = []model.Account{}
		return
	}
	m.accounts = accounts
}

func (m *Manager) SaveAccounts() error {
	if m.st == nil {
		return nil
	}
	return m.st.SaveAccounts(m.accounts)
}

func (m *Manager) GetAccounts() []model.Account {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.accounts == nil {
		return []model.Account{}
	}
	return m.accounts
}

// GetAccountByID повертає акаунт за ID (для відновлення сесії гри після
// перезапуску лаунчера: restoreRunningGame перевіряє, що акаунт, під яким
// йшла гра, ще існує). Другим значенням — чи знайдено.
func (m *Manager) GetAccountByID(id string) (model.Account, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, a := range m.accounts {
		if a.ID == id {
			return a, true
		}
	}
	return model.Account{}, false
}

// AddAccount додає акаунт або, якщо акаунт з таким ID вже є в списку
// (повторний вхід під тим самим офлайн-ніком чи той самий Microsoft-
// акаунт), ОНОВЛЮЄ наявний запис на місці — раніше тут завжди робився
// append, тому повторний офлайн-вхід під тим самим ніком (наприклад,
// автоматичний довхід після Google-логіну в скін-систему) створював у
// accounts.json ДРУГИЙ запис з однаковим ID. Далі GetAccountByID/
// ActiveAccount повертали перший знайдений — непередбачувано, який саме —
// і в UI могло виглядати, ніби зміни (новий нік) не застосувались.
func (m *Manager) AddAccount(a model.Account) error {
	m.mu.Lock()
	replaced := false
	for i := range m.accounts {
		if m.accounts[i].ID == a.ID {
			m.accounts[i] = a
			replaced = true
			break
		}
	}
	if !replaced {
		m.accounts = append(m.accounts, a)
	}
	// Щойно доданий акаунт стає активним за замовчуванням.
	m.settings.ActiveAccountID = a.ID
	m.mu.Unlock()
	if err := m.SaveAccounts(); err != nil {
		return err
	}
	return m.SaveSettings()
}

// ActiveAccount повертає обраний для гри акаунт. Якщо ActiveAccountID не
// заданий або вказує на видалений акаунт — перший у списку.
func (m *Manager) ActiveAccount() model.Account {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.accounts) == 0 {
		return model.Account{}
	}
	for _, a := range m.accounts {
		if a.ID == m.settings.ActiveAccountID {
			return a
		}
	}
	return m.accounts[0]
}

// SetActiveAccountID зберігає обраний акаунт у settings.json (переживає
// перезапуск). Порожній id = жоден не вибраний (візьметься перший).
func (m *Manager) SetActiveAccountID(id string) error {
	m.mu.Lock()
	m.settings.ActiveAccountID = id
	m.mu.Unlock()
	return m.SaveSettings()
}

// UpdateAccount замінює акаунт з тим самим ID (напр. після refresh токена).
func (m *Manager) UpdateAccount(a model.Account) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.accounts {
		if m.accounts[i].ID == a.ID {
			m.accounts[i] = a
			return m.SaveAccounts()
		}
	}
	return nil
}

// ReplaceAccountID замінює акаунт зі старим ID на новий запис (новий ID,
// можливо новий Username/UUID) НА ТОМУ Ж МІСЦІ у списку — використовується
// при перейменуванні офлайн-акаунта (App.RenameOfflineAccount), щоб
// зберегти позицію в sidebar замість видалення+додавання в кінець.
// Якщо активним був старий акаунт — активним стає новий. Помилка, якщо
// oldID не знайдено.
func (m *Manager) ReplaceAccountID(oldID string, newAccount model.Account) error {
	m.mu.Lock()
	found := false
	for i := range m.accounts {
		if m.accounts[i].ID == oldID {
			m.accounts[i] = newAccount
			found = true
			break
		}
	}
	if found && m.settings.ActiveAccountID == oldID {
		m.settings.ActiveAccountID = newAccount.ID
	}
	m.mu.Unlock()
	if !found {
		return fmt.Errorf("акаунт %s не знайдено", oldID)
	}
	if err := m.SaveAccounts(); err != nil {
		return err
	}
	return m.SaveSettings()
}

func (m *Manager) RemoveAccount(id string) error {
	m.mu.Lock()
	for i, a := range m.accounts {
		if a.ID == id {
			m.accounts = append(m.accounts[:i], m.accounts[i+1:]...)
			break
		}
	}
	// Видалили активний акаунт — скидаємо вибір, візьметься перший із
	// лишившихся (або ніхто, якщо акаунтів не стало).
	if m.settings.ActiveAccountID == id {
		m.settings.ActiveAccountID = ""
	}
	m.mu.Unlock()
	if err := m.SaveAccounts(); err != nil {
		return err
	}
	return m.SaveSettings()
}

// ReorderAccounts змінює порядок акаунтів відповідно до ids (наприклад,
// кнопки «вгору/вниз» у налаштуваннях акаунтів). Активний акаунт не
// змінюється. Невідомі ids пропускаються, а не-перелічені акаунти
// дописуються в кінець — порядок ніколи не втрачає акаунтів.
func (m *Manager) ReorderAccounts(ids []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	byID := make(map[string]model.Account, len(m.accounts))
	for _, a := range m.accounts {
		byID[a.ID] = a
	}
	ordered := make([]model.Account, 0, len(m.accounts))
	seen := make(map[string]bool, len(m.accounts))
	for _, id := range ids {
		a, ok := byID[id]
		if !ok || seen[id] {
			continue
		}
		seen[id] = true
		ordered = append(ordered, a)
	}
	for _, a := range m.accounts {
		if !seen[a.ID] {
			ordered = append(ordered, a)
		}
	}
	m.accounts = ordered
	return m.SaveAccounts()
}

func (m *Manager) IsFirstRun() bool {
	if m.st != nil {
		_, ok, err := m.st.GetSettingsRaw()
		if err == nil {
			return !ok
		}
	}
	// Сховище недоступне — фолбек на стару поведінку (немає settings.json).
	_, err := os.Stat(m.SettingsPath())
	return os.IsNotExist(err)
}

func (m *Manager) SetupComplete(s model.Settings) error {
	// Теки installations/java ЗАВЖДИ деривативи папки даних (baseDir) —
	// окремих кастомних тек більше немає. Примусово підставляємо актуальні
	// значення з cfg, а не те, що надіслав майстер: якщо на фронтенді
	// GetFolderPaths впав, fallback міг порахувати шлях БЕЗ .shaurm
	// (напр. D:\java замість D:\\Games\\.shaurm\\java) і записати його
	// в settings — потім запуск гри падав «java not found at D:\java».
	s.InstanceDir = m.InstancesDir()
	s.JavaPath = m.JavaDir()
	m.mu.Lock()
	m.settings = s
	m.normalize()
	m.mu.Unlock()
	if err := m.SaveSettings(); err != nil {
		return err
	}
	// Майстер успішно завершено — чернетка кроків більше не потрібна.
	// Ігноруємо помилку видалення (запису могло ще не бути).
	if m.st != nil {
		_ = m.st.DeleteSetupProgress()
	}
	return nil
}

// SaveSetupProgress пишеться при КОЖНІЙ зміні кроку/поля майстра.
// Раніше — атомарний запис у JSON-файл (через .tmp + rename); тепер —
// UPSERT у SQLite (єдиний рядок), що так само не може лишити биту
// чернетку, через яку майстер не зміг би відновитись.
func (m *Manager) SaveSetupProgress(p model.SetupProgress) error {
	if m.st == nil {
		return nil
	}
	return m.st.SaveSetupProgress(p)
}

// LoadSetupProgress повертає збережений крок майстра. Якщо запису немає
// (перший запуск взагалі, або майстер вже колись завершено і чернетку
// прибрали) — повертає прогрес з Step=0 і nil-помилку: виклик має
// трактувати Step==0 як "почати з кроку 1", а не як помилку.
func (m *Manager) LoadSetupProgress() (model.SetupProgress, error) {
	var p model.SetupProgress
	if m.st == nil {
		return p, nil
	}
	p, ok, err := m.st.LoadSetupProgress()
	if err != nil || !ok {
		return model.SetupProgress{}, nil
	}
	return p, nil
}

// ── Ігровий час (playtime у SQLite) ────────────────────────────────────
// Записується час, проведений у кожній збірці, якщо увімкнено
// RecordGameTime. Храниться в БД у стабільній теці конфігурації
// (як і settings), щоб переживати перенесення папки даних.
func (m *Manager) GetPlaytime() map[string]int64 {
	if m.st == nil {
		return map[string]int64{}
	}
	pt, err := m.st.LoadPlaytime()
	if err != nil || pt == nil {
		return map[string]int64{}
	}
	return pt
}

// AddPlaytime додає секунди до збірки з вказаним ID.
func (m *Manager) AddPlaytime(instanceID string, seconds int64) error {
	if m.st == nil {
		return nil
	}
	return m.st.AddPlaytime(instanceID, seconds)
}

// AddPlaytimeSession записує ОДНУ завершену ігрову сесію (для аналітики:
// розподіл часу по днях/тижнях/місяцях/роках, середня сесія, топ збірок).
// Сесія прив'язується до НІКА гравця (accountName) — аналітика окремо по
// гравцях, і дані переживають вихід з акаунта.
func (m *Manager) AddPlaytimeSession(instanceID, accountID, accountName string, seconds int64, startedAt time.Time) error {
	if m.st == nil || seconds <= 0 {
		return nil
	}
	return m.st.AddPlaytimeSession(instanceID, accountID, accountName, seconds, startedAt)
}

// CompactPlaytime згортає сесії старші за store.PlaytimeCompactAfter у
// місячні підсумки (див. store.CompactPlaytimeSessions). Аналітика від цього
// не ламається: GetAnalytics бачить підсумки як звичайні записи.
func (m *Manager) CompactPlaytime() (int, error) {
	if m.st == nil {
		return 0, nil
	}
	return m.st.CompactPlaytimeSessions(time.Now().Add(-store.PlaytimeCompactAfter))
}

// LoadAllPlaytimeSessions повертає ВСІ ігрові сесії (усі збірки, усі ніки).
// Потрібно аналітиці ВИКОРИСТАННЯ (backend/analytics.go), яка рахує підсумки
// для звіту на worker. GetAnalytics() тут не підходить: він віддає вже
// згрупований знімок під графіки UI (бакети тижнів/місяців/років), а для
// звіту потрібні саме сирі сесії — сума, кількість і «сьогодні».
func (m *Manager) LoadAllPlaytimeSessions() ([]store.PlaytimeSession, error) {
	if m.st == nil {
		return nil, nil
	}
	return m.st.LoadPlaytimeSessions("")
}

// GetAnalytics рахує повний знімок аналітики ігрового часу з таблиці
// сесій: підсумки, три серії стовпчиків (12 тижнів / 12 місяців /
// 8 років) і топ збірок. accountName == "" — сумарно по ВСІХ гравцях;
// інакше — лише сесії цього гравця (аналітика окремо по кожному
// ніку; нік переживає вихід з акаунта). Порожні дані повертають нульову
// структуру з порожніми, але відкаліброваними бакетами — фронтенд малює
// графік одразу, без окремого стану «немає даних».
func (m *Manager) GetAnalytics(accountName string) (model.AnalyticsSummary, error) {
	var out model.AnalyticsSummary
	if m.st == nil {
		return out, nil
	}
	recs, err := m.st.LoadPlaytimeSessions(accountName)
	if err != nil {
		return out, err
	}

	now := time.Now()

	// Бакети: дні (7, локальний початок доби), тижні (12, ISO з понеділка),
	// місяці (12, 1-ше число) і роки (8, 1 січня) — найстаріший перший.
	days := make([]time.Time, 7)
	for i := range days {
		d := now.AddDate(0, 0, i-6)
		days[i] = time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, d.Location())
	}
	weeks := make([]time.Time, 12)
	for i := range weeks {
		w := now.AddDate(0, 0, (i-11)*7)
		// ISO-тиждень починається з понеділка (weekday: Mon=1..Sun=7).
		wd := int(w.Weekday())
		if wd == 0 {
			wd = 7
		}
		start := w.AddDate(0, 0, -(wd - 1))
		weeks[i] = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, start.Location())
	}
	months := make([]time.Time, 12)
	for i := range months {
		months[i] = time.Date(now.Year(), now.Month()-time.Month(11-i), 1, 0, 0, 0, 0, now.Location())
	}
	years := make([]time.Time, 8)
	for i := range years {
		years[i] = time.Date(now.Year()-7+i, 1, 1, 0, 0, 0, 0, now.Location())
	}

	// bucketOf повертає індекс бакета для моменту початку сесії (останній
	// бакет, що почався НЕ пізніше моменту). Сесія, що перетинає межу доби,
	// зараховується повністю в бакет свого початку — для тривалостей у
	// хвилини/години це несуттєва похибка.
	// -1, якщо момент раніше за найстаріший бакет: такі сесії НЕ зараховуємо
	// у цю серію (раніше вони всі «злипались» у крайній лівий стовпчик, і
	// «7 днів» показувало б і сесії двомісячної давності). У підсумки та топ
	// збірок вони однаково входять — вони рахуються окремо від бакетів.
	bucketOf := func(starts []time.Time, t time.Time) int {
		if t.Before(starts[0]) {
			return -1
		}
		idx := 0
		for i, s := range starts {
			if !t.Before(s) {
				idx = i
			}
		}
		return idx
	}

	daySec := make([]int64, len(days))
	weekSec := make([]int64, len(weeks))
	monthSec := make([]int64, len(months))
	yearSec := make([]int64, len(years))

	byInstance := map[string]*model.AnalyticsInstance{}
	for _, r := range recs {
		// Sessions > 1 — місячний підсумок стиснутих старих сесій
		// (store.CompactPlaytimeSessions); 0 — звичайна одна сесія.
		n := r.Sessions
		if n <= 0 {
			n = 1
		}
		out.TotalSeconds += r.Seconds
		out.TotalSessions += n
		if i := bucketOf(days, r.StartedAt); i >= 0 {
			daySec[i] += r.Seconds
		}
		if i := bucketOf(weeks, r.StartedAt); i >= 0 {
			weekSec[i] += r.Seconds
		}
		if i := bucketOf(months, r.StartedAt); i >= 0 {
			monthSec[i] += r.Seconds
		}
		if i := bucketOf(years, r.StartedAt); i >= 0 {
			yearSec[i] += r.Seconds
		}

		ai := byInstance[r.InstanceID]
		if ai == nil {
			ai = &model.AnalyticsInstance{InstanceID: r.InstanceID}
			byInstance[r.InstanceID] = ai
		}
		ai.Seconds += r.Seconds
		ai.Sessions += n
	}

	out.Last7Days = make([]model.AnalyticsPoint, len(days))
	for i, d := range days {
		out.Last7Days[i] = model.AnalyticsPoint{Start: d.Format("2006-01-02"), Seconds: daySec[i]}
	}
	out.Weeks = make([]model.AnalyticsPoint, len(weeks))
	for i, w := range weeks {
		out.Weeks[i] = model.AnalyticsPoint{Start: w.Format("2006-01-02"), Seconds: weekSec[i]}
	}
	out.Months = make([]model.AnalyticsPoint, len(months))
	for i, mt := range months {
		out.Months[i] = model.AnalyticsPoint{Start: mt.Format("2006-01-02"), Seconds: monthSec[i]}
	}
	out.Years = make([]model.AnalyticsPoint, len(years))
	for i, y := range years {
		out.Years[i] = model.AnalyticsPoint{Start: y.Format("2006"), Seconds: yearSec[i]}
	}

	// Топ збірок: лише ті, що мають час; сортовано спаданням, топ-8.
	list := make([]model.AnalyticsInstance, 0, len(byInstance))
	for _, ai := range byInstance {
		list = append(list, *ai)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Seconds != list[j].Seconds {
			return list[i].Seconds > list[j].Seconds
		}
		return list[i].InstanceID < list[j].InstanceID
	})
	if len(list) > 8 {
		list = list[:8]
	}
	out.TopInstances = list

	// Підсумки: грали в ті збірки, що мають час; середня сесія — від ділення.
	for _, ai := range list {
		if ai.Seconds > 0 {
			out.PlayedInstances++
		}
	}
	if out.TotalSessions > 0 {
		out.AvgSessionSeconds = out.TotalSeconds / out.TotalSessions
	}
	return out, nil
}

// ── Per-instance конфігурація (instance_configs у SQLite) ─────────────
// Тут зберігаються ЛИШЕ ті параметри збірки, які користувач має право
// редагувати на сторінці збірки (RAM, JVM-аргументи, вікно гри, команди,
// сервери). Версія гри, лоадер і фон для збірок Шаурма сюди принципово
// не потрапляють — вони приходять з маніфесту самої збірки і бекенд їх
// звідси навіть не читає.
func (m *Manager) loadInstanceConfigs() map[string]model.InstanceConfig {
	if m.st == nil {
		return map[string]model.InstanceConfig{}
	}
	all, err := m.st.LoadInstanceConfigs()
	if err != nil {
		return map[string]model.InstanceConfig{}
	}
	return all
}

// GetInstanceConfig повертає збережені override-параметри збірки, або
// порожній InstanceConfig (усі поля успадковують глобальні Settings),
// якщо для цього ID ще нічого не збережено.
//
// Конкурентність: SaveInstanceConfig — це UPSERT ОДНОГО рядка в БД, тож
// read-modify-write цикл на рівні всього файлу (і lost update, коли два
// збереження різних збірок перезаписували б файл по черзі) більше не
// існує. Лок m.mu лишаємо для узгодженості зі збереженням акаунтів.
func (m *Manager) GetInstanceConfig(id string) model.InstanceConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.st == nil {
		return model.InstanceConfig{ID: id}
	}
	cfg, ok, err := m.st.GetInstanceConfig(id)
	if err != nil || !ok {
		return model.InstanceConfig{ID: id}
	}
	return cfg
}

// AllInstanceConfigs повертає копію ВСІХ збережених per-instance конфігів
// (для ітерації — напр. збір усіх закріплених серверів по збірках).
func (m *Manager) AllInstanceConfigs() map[string]model.InstanceConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.loadInstanceConfigs()
}

// SaveInstanceConfig зберігає override-параметри однієї збірки, не
// торкаючись конфігурацій решти збірок.
func (m *Manager) SaveInstanceConfig(cfg model.InstanceConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.st == nil {
		return nil
	}
	return m.st.SaveInstanceConfig(cfg)
}

// DeleteInstanceConfig прибирає збережені override-и збірки (наприклад,
// коли її видаляють) — інакше конфіг лишався б сиротою назавжди.
func (m *Manager) DeleteInstanceConfig(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.st == nil {
		return nil
	}
	return m.st.DeleteInstanceConfig(id)
}

// RenameInstanceConfig копіює конфіг інстанції з oldID → newID і видаляє старий.
// Використовується при зміні ID кастомної збірки (ChangeCustomPackID).
func (m *Manager) RenameInstanceConfig(oldID, newID string) {
	cfg := m.GetInstanceConfig(oldID)
	if cfg.ID == "" && cfg.MaxRAMOverride == 0 && cfg.MinRAMOverride == 0 {
		return // немає що переносити
	}
	cfg.ID = newID
	_ = m.SaveInstanceConfig(cfg)
	_ = m.st.DeleteInstanceConfig(oldID)
}
