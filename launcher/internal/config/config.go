package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"shaurma-launcher-wails/internal/atomicfile"
	"shaurma-launcher-wails/internal/model"
)

var DefaultSettings = model.Settings{
	Language:      "uk",
	Accent:        "orange",
	AccentCustom:  "#ff8a00",
	Theme:         "dark",
	Font:          "inter",
	AutoUpdate:    true,
	UpdateChannel: "stable",
	CloseOnLaunch: false,	ShowConsole:    false,
	SaveLogs:       true,
	ConfirmOnStop:  true,
	MaxRAM:        4096,

	ConsoleMaxLines:      3000,
	ConsoleInfoColor:     "#8fd3ff",
	ConsoleWarnColor:     "#ffd166",
	ConsoleErrorColor:    "#ff6b6b",
	ConsoleWrap:          false,
	ConsoleAI:            false,
	ConsoleFontSize:      12,
	ConsoleFontFamily:    "mono",
	ConsoleFontMode:      "all",
	ConsoleInfoFont:      "mono",
	ConsoleWarnFont:      "mono",
	ConsoleErrorFont:     "mono",
	ShowConsoleOnCrash:   true,
	MaxConcurrentDownloads: 10,
	MaxRetries:           6,
	HTTPTimeoutSec:       60,
	WindowWidth:          854,
	WindowHeight:         480,
	RecordGameTime:       true,
}

type Manager struct {
	mu              sync.RWMutex
	settings        model.Settings
	accounts        []model.Account
	dir             string
	instDirOverride string // кастомна тека збірок (installations) з launcher-location.json
	javaDirOverride string // кастомна тека Java з launcher-location.json
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
func NewManager() *Manager {
	m := &Manager{dir: defaultBaseDir(), settings: DefaultSettings}
	m.loadLocationConfig()
	return m
}

// LocationConfigFile — файл, де зберігається ПАПКА ДАНИХ лаунчера (baseDir)
// і кастомні шляхи підтек (installations/java). Лежить у стабільній теці
// %LOCALAPPDATA%\ShaurmLauncher (там, де Inno Setup ставить лаунчер), а НЕ
// в самій папці даних: якщо користувач перенесе/видалить папку даних,
// лаунчер завжди знає, де його справжні теки, і шляхи не гуляються.
func (m *Manager) LocationConfigFile() string {
	if os.Getenv("OS") == "Windows_NT" {
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			return filepath.Join(local, "ShaurmLauncher", "launcher-location.json")
		}
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "shaurm-launcher", "launcher-location.json")
}

func (m *Manager) loadLocationConfig() {
	data, err := os.ReadFile(m.LocationConfigFile())
	if err != nil {
		return
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

// DefaultBaseDir — системний дефолт папки даних лаунчера (для кнопки
// «За замовчуванням» на вкладці «Теки та шляхи»).
func (m *Manager) DefaultBaseDir() string { return defaultBaseDir() }

// FolderPaths повертає поточні шляхи всіх тек лаунчера для вкладки
// «Теки та шляхи»: папка даних, installations, java, cache, logs, config
// + кастомні значення підтек ("" = дефолт відносно папки даних).
func (m *Manager) FolderPaths() model.FolderPaths {
	return model.FolderPaths{
		BaseDir:             m.dir,
		Installations:       m.InstancesDir(),
		Java:                m.JavaDir(),
		Cache:               m.CacheDir(),
		Logs:                m.LogsDir(),
		Config:              m.ConfigDir(),
		InstallationsCustom: m.instDirOverride,
		JavaCustom:          m.javaDirOverride,		CacheCustom:          m.cacheDirOverride,
		LogsCustom:           m.logsDirOverride,
		DefaultBaseDir:      m.DefaultBaseDir(),
	}
}

// SetAppDir змінює папку даних лаунчера на вибрану користувачем теку і
// зберігає її (разом з кастомними підтеками) у launcher-location.json.
// Кастомні підтеки (installations/java), які користувач уже сам задав,
// лишаються як є — при перенесенні папки даних не чіпаємо ручний вибір.
func (m *Manager) SetAppDir(newDir string) error {
	if newDir == "" {
		return fmt.Errorf("порожня папка даних")
	}
	m.dir = newDir
	if err := m.EnsureDirs(); err != nil {
		return err
	}
	return m.SaveLocationConfig(newDir, m.instDirOverride, m.javaDirOverride, m.cacheDirOverride, m.logsDirOverride)
}

// SetFolderPaths зберігає кастомні шляхи підтек (installations/java) у
// launcher-location.json. Завжди зберігає АБСОЛЮТНІ шляхи, які користувач
// остаточно обрав у майстрі/налаштуваннях: навіть якщо папку даних
// перенесуть чи видалять, лаунчер знає, де лежать його збірки і Java.
// Порожній рядок = "використовувати дефолт відносно папки даних".
func (m *Manager) SetFolderPaths(installations, javaDir string) error {
	m.instDirOverride = installations
	m.javaDirOverride = javaDir
	return m.SaveLocationConfig(m.dir, installations, javaDir, m.cacheDirOverride, m.logsDirOverride)
}

// SetFolderPath змінює одну теку лаунчера за іменем (kind): baseDir |
// installations | java | cache | logs. Для baseDir використовується
// SetAppDir (перебудова залежних компонентів на боці App); для решти —
// зберігається override у launcher-location.json. Порожній path = дефолт
// відносно папки даних.
func (m *Manager) SetFolderPath(kind, path string) error {
	switch kind {
	case "baseDir":
		return m.SetAppDir(path)
	case "installations":
		return m.SetFolderPaths(path, m.javaDirOverride)
	case "java":
		return m.SetFolderPaths(m.instDirOverride, path)
	case "cache":
		m.cacheDirOverride = path
	case "logs":
		m.logsDirOverride = path
	default:
		return fmt.Errorf("невідома тека: %s", kind)
	}
	return m.SaveLocationConfig(m.dir, m.instDirOverride, m.javaDirOverride, m.cacheDirOverride, m.logsDirOverride)
}

func (m *Manager) CacheDir() string {
	if m.cacheDirOverride != "" {
		return m.cacheDirOverride
	}
	return filepath.Join(m.dir, "cache")
}
func (m *Manager) LogsDir() string {
	if m.logsDirOverride != "" {
		return m.logsDirOverride
	}
	return filepath.Join(m.dir, "logs")
}

// ConfigDir — стабільна тека конфігурації лаунчера (%LOCALAPPDATA%\
// ShaurmLauncher, та сама, де лежить launcher-location.json). Сюди пишуться
// НАЛАШТУВАННЯ, чернетка майстра та акаунти — речі, потрібні лаунчеру для
// запуску. На відміну від папки даних (baseDir) ця тека НІКОЛИ не змінюється:
// перенесення чи видалення папки даних не може загубити конфігурацію.
func (m *Manager) ConfigDir() string {
	return filepath.Dir(m.LocationConfigFile())
}

// SettingsPath — файл налаштувань у стабільній теці конфігурації. Раніше
// лежав у папці даних, але папку даних користувач може змінити в
// налаштуваннях — і лаунчер втрачав би свої ж налаштування.
func (m *Manager) SettingsPath() string { return filepath.Join(m.ConfigDir(), "settings.json") }
func (m *Manager) AccountsPath() string { return filepath.Join(m.ConfigDir(), "accounts.json") }
func (m *Manager) SetupProgressPath() string {
	return filepath.Join(m.ConfigDir(), "setup-progress.json")
}

// InstancesDir — тека збірок лаунчера. За замовчуванням це папка даних/
// installations (як у старого лаунчера), але користувач може задати власну
// теку через launcher-location.json — тоді вона має пріоритет.
func (m *Manager) InstancesDir() string {
	if m.instDirOverride != "" {
		return m.instDirOverride
	}
	return filepath.Join(m.dir, "installations")
}

// JavaDir — власна тека вбудованої Java лаунчера. Лаунчер сам
// встановлює Java сюди при першому запуску, якщо її ще немає
// (це за ТЗ: "лаунчер має власну теку з java, він сам її поставить").
func (m *Manager) JavaDir() string {
	if m.javaDirOverride != "" {
		return m.javaDirOverride
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

func (m *Manager) Load() error {
	m.migrateConfigFiles()
	m.EnsureDirs()
	if err := m.loadSettings(); err != nil {
		m.settings = DefaultSettings
	}
	m.normalize()
	m.loadAccounts()
	return nil
}

func (m *Manager) loadSettings() error {
	data, err := os.ReadFile(m.SettingsPath())
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &m.settings)
}

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
	if s.AccentCustom == "" {
		s.AccentCustom = DefaultSettings.AccentCustom
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
	return atomicfile.WriteJSONAtomic(m.SettingsPath(), m.settings)
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

func (m *Manager) loadAccounts() {
	data, err := os.ReadFile(m.AccountsPath())
	if err != nil {
		m.accounts = []model.Account{}
		return
	}
	m.accounts = []model.Account{}
	json.Unmarshal(data, &m.accounts)
}

func (m *Manager) SaveAccounts() error {
	return atomicfile.WriteJSONAtomic(m.AccountsPath(), m.accounts)
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

func (m *Manager) AddAccount(a model.Account) error {
	m.mu.Lock()
	m.accounts = append(m.accounts, a)
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

func (m *Manager) IsFirstRun() bool {
	_, err := os.Stat(m.SettingsPath())
	return os.IsNotExist(err)
}

func (m *Manager) SetupComplete(s model.Settings) error {
	m.mu.Lock()
	m.settings = s
	m.normalize()
	m.mu.Unlock()
	if err := m.SaveSettings(); err != nil {
		return err
	}
	// Майстер успішно завершено — чернетка кроків більше не потрібна.
	// Ігноруємо помилку видалення (файл міг вже не існувати).
	os.Remove(m.SetupProgressPath())
	return nil
}

// SaveSetupProgress пишеться при КОЖНІЙ зміні кроку/поля майстра.
// Атомарний запис (через .tmp + rename) — щоб краш процесу посеред
// запису не лишив биту чернетку, через яку майстер не зміг би
// відновитись взагалі.
func (m *Manager) SaveSetupProgress(p model.SetupProgress) error {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(m.ConfigDir(), 0755); err != nil {
		return err
	}
	tmp := m.SetupProgressPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, m.SetupProgressPath())
}

// LoadSetupProgress повертає збережений крок майстра. Якщо файлу
// немає (перший запуск взагалі, або майстер вже колись завершено і
// чернетку прибрали) — повертає прогрес з Step=0 і nil-помилку: виклик
// має трактувати Step==0 як "почати з кроку 1", а не як помилку.
func (m *Manager) LoadSetupProgress() (model.SetupProgress, error) {
	var p model.SetupProgress
	data, err := os.ReadFile(m.SetupProgressPath())
	if os.IsNotExist(err) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	if err := json.Unmarshal(data, &p); err != nil {
		// Пошкоджений JSON — не валимо весь запуск лаунчера через
		// биту чернетку майстра, просто починаємо з кроку 1.
		return model.SetupProgress{}, nil
	}
	return p, nil
}

// ── Ігровий час (playtime.json у стабільній теці конфігурації) ──────────
// Записується час, проведений у кожній збірці, якщо увімкнено
// RecordGameTime. Файл лежить у ConfigDir (як і settings.json), щоб
// переживати перенесення папки даних.
func (m *Manager) PlaytimePath() string {
	return filepath.Join(m.ConfigDir(), "playtime.json")
}

func (m *Manager) GetPlaytime() map[string]int64 {
	data, err := os.ReadFile(m.PlaytimePath())
	if err != nil {
		return map[string]int64{}
	}
	var pt map[string]int64
	if err := json.Unmarshal(data, &pt); err != nil {
		return map[string]int64{}
	}
	return pt
}

// AddPlaytime додає секунди до збірки з вказаним ID.
func (m *Manager) AddPlaytime(instanceID string, seconds int64) error {
	pt := m.GetPlaytime()
	pt[instanceID] += seconds
	return atomicfile.WriteJSONAtomic(m.PlaytimePath(), pt)
}

// ── Per-instance конфігурація (instance-configs.json у стабільній теці
// конфігурації) ───────────────────────────────────────────────────────
// Тут зберігаються ЛИШЕ ті параметри збірки, які користувач має право
// редагувати на сторінці збірки (RAM, JVM-аргументи, вікно гри, команди,
// сервери). Версія гри, лоадер і фон для збірок Шаурма сюди принципово
// не потрапляють — вони приходять з маніфесту самої збірки і бекенд їх
// звідси навіть не читає.
func (m *Manager) InstanceConfigsPath() string {
	return filepath.Join(m.ConfigDir(), "instance-configs.json")
}

func (m *Manager) loadInstanceConfigs() map[string]model.InstanceConfig {
	data, err := os.ReadFile(m.InstanceConfigsPath())
	if err != nil {
		return map[string]model.InstanceConfig{}
	}
	var all map[string]model.InstanceConfig
	if err := json.Unmarshal(data, &all); err != nil {
		return map[string]model.InstanceConfig{}
	}
	return all
}

func (m *Manager) saveInstanceConfigs(all map[string]model.InstanceConfig) error {
	return atomicfile.WriteJSONAtomic(m.InstanceConfigsPath(), all)
}

// GetInstanceConfig повертає збережені override-параметри збірки, або
// порожній InstanceConfig (усі поля успадковують глобальні Settings),
// якщо для цього ID ще нічого не збережено.
// GetInstanceConfig повертає збережені override-параметри збірки, або
// порожній InstanceConfig (усі поля успадковують глобальні Settings),
// якщо для цього ID ще нічого не збережено.
//
// Лок (m.mu): read-modify-write цикл нижче (Save/Delete) мусить бути
// атомарним — без нього два одночасні збереження різних збірок читали б
// файл кожне у свою копію і останній запис тихо стирав би зміну першого
// (класичний lost update).
func (m *Manager) GetInstanceConfig(id string) model.InstanceConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	all := m.loadInstanceConfigs()
	if cfg, ok := all[id]; ok {
		return cfg
	}
	return model.InstanceConfig{ID: id}
}

// SaveInstanceConfig зберігає override-параметри однієї збірки, не
// торкаючись конфігурацій решти збірок.
func (m *Manager) SaveInstanceConfig(cfg model.InstanceConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	all := m.loadInstanceConfigs()
	all[cfg.ID] = cfg
	return m.saveInstanceConfigs(all)
}

// DeleteInstanceConfig прибирає збережені override-и збірки (наприклад,
// коли її видаляють) — інакше конфіг лишався б сиротою назавжди.
func (m *Manager) DeleteInstanceConfig(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	all := m.loadInstanceConfigs()
	delete(all, id)
	return m.saveInstanceConfigs(all)
}
