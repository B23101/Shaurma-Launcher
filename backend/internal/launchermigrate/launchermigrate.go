// Package launchermigrate — знаходження та імпорт збірок з інших лаунчерів
// (Prism Launcher, Modrinth App) у Shaurma Launcher.
//
// ГОЛОВНЕ ПРАВИЛО (антивірусна безпека): сканування виконується ТІЛЬКИ по
// відомих стандартних шляхах даних інших лаунчерів (%APPDATA%/PrismLauncher,
// %APPDATA%/com.modrinth.theseus тощо) — жодного рекурсивного обходу дисків,
// жодного читання чужих каталогів. Лаунчер шукає СВОЇ дані там, де вони
// фізично лежать за замовчуванням, і читає лише власні файли іншого
// лаунчера (mmc-pack.json, instance.cfg, app.db). Це та сама поведінка, що
// у будь-якого інструменту імпорту, і не виглядає для антивірусів як
// сканування приватних даних користувача.
//
// Імпорт — КОПІЮВАННЯ, ніколи не переміщення: дані інших лаунчерів
// НІКОЛИ не видаляються. Переноситься все (файли гри, налаштування,
// ID/назва/іконка), окрім кешів у теці Minecraft.
package launchermigrate

// LauncherKind — тип виявленого лаунчера.
type LauncherKind string

const (
	LauncherPrism    LauncherKind = "prism"    // Prism Launcher (mmc-pack.json)
	LauncherModrinth LauncherKind = "modrinth" // Modrinth App (theseus app.db)
)

// DisplayName — людська назва лаунчера для UI.
func (k LauncherKind) DisplayName() string {
	switch k {
	case LauncherPrism:
		return "Prism Launcher"
	case LauncherModrinth:
		return "Modrinth App"
	default:
		return string(k)
	}
}

// Pack — одна збірка (інстанс) іншого лаунчера, знайдена при скануванні.
type Pack struct {
	// Kind — лаунчер, з якого походить збірка (заповнюється сканером).
	Kind LauncherKind `json:"kind"`
	// ID — ідентифікатор збірки В ІНШОМУ лаунчері (назва теки інстансу /
	// шлях профілю). Зберігається в даних імпортованої збірки як
	// MigratedFrom.ID.
	ID string `json:"id"`
	// Name — назва збірки (з instance.cfg / app.db).
	Name string `json:"name"`
	// MCVersion — версія Minecraft (з mmc-pack.json / app.db).
	MCVersion string `json:"mcVersion"`
	// Loader — vanilla | fabric | forge | neoforge | quilt.
	Loader string `json:"loader"`
	// LoaderVersion — версія лоадера (може бути порожньою).
	LoaderVersion string `json:"loaderVersion"`
	// IconPath — АБСОЛЮТНИЙ шлях до PNG-іконки збірки, якщо вона є
	// (Prism: <dataDir>/icons/<iconKey>; Modrinth: icon_path з app.db або
	// <instance>/icon.png). Порожній рядок = іконки немає (тоді UI/бекенд
	// призначає пресет через ШІ).
	IconPath string `json:"iconPath,omitempty"`
	// AlreadyImported — чи збірка вже перенесена у цей лаунчер раніше
	// (у store є збірка з MigratedFrom.Kind == Kind && MigratedFrom.ID == ID).
	// Заповнює App (launchermigrate не знає про store); фронтенд показує
	// бейдж «Вже завантажено» і дозволяє пере-імпорт (дублікат отримає «(2)»).
	AlreadyImported bool `json:"alreadyImported,omitempty"`
	// GameDir — АБСОЛЮТНИЙ шлях до теки гри (Prism: <instance>/.minecraft
	// або <instance>/minecraft; Modrinth: сам інстанс <config>/profiles/<path>).
	GameDir string `json:"gameDir"`
	// SizeBytes — сумарний розмір файлів гри (для підказки «що переносимо»).
	SizeBytes int64 `json:"sizeBytes"`
	// ── Налаштування, які переносяться разом зі збіркою ──
	MaxRAMMB  int    `json:"maxRamMb,omitempty"`
	MinRAMMB  int    `json:"minRamMb,omitempty"`
	JavaPath  string `json:"javaPath,omitempty"`
	JvmArgs   string `json:"jvmArgs,omitempty"`
	HasConfig bool   `json:"hasConfig"` // чи є хоч якісь налаштування для переносу
}

// Launcher — знайдений лаунчер зі списком його збірок.
type Launcher struct {
	Kind    LauncherKind `json:"kind"`
	Name    string       `json:"name"`
	DataDir string       `json:"dataDir"`
	Found   bool         `json:"found"`
	Packs   []Pack       `json:"packs"`
}

// ScanResult — результат сканування (для UI: перемикачі між лаунчерами).
type ScanResult struct {
	Launchers []Launcher `json:"launchers"`
	// FoundLaunchers — скільки лаунчерів реально знайдено (їх теки існують).
	FoundLaunchers int `json:"foundLaunchers"`
}
