package launchermigrate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"shaurma-launcher-backend/internal/custompack"
	"shaurma-launcher-backend/internal/imageconvert"
)

// ── Імпорт збірки з іншого лаунчера ─────────────────────────────────────
// Імпорт — ЧИСТЕ КОПІЮВАННЯ: дані іншого лаунчера ніколи не видаляються і
// не модифікуються (антивірусна безпека + «міграція немає видаляти дані
// інших лаунчерів»). Переноситься все: файли гри (крім кешів у теці
// Minecraft — CacheExcluded), налаштування (RAM/Java), ID+назва+іконка
// збірки. Походження (з якого лаунчера і який ID там був) зберігається в
// даних збірки (Pack.MigratedFrom); якщо збірка з таким ID уже існує —
// назва/теки отримують суфікс «(2)» (custompack.Store.UniqueIDAndName).

// SuggestStyle — колбек призначення стилю збірці БЕЗ PNG-іконки
// (пресет-іконка + колір). Реалізується на боці App (Groq → JSON):
// launchermigrate не залежить від ai-пакета.
type SuggestStyle func(name, mcVersion, loader string) (icon, color string)

// ImportOptions — параметри імпорту однієї збірки.
type ImportOptions struct {
	// Store — реєстр кастомних збірок, куди створюється збірка.
	Store *custompack.Store
	// InstanceDir — тека збірок лаунчера (<dataDir>/installations).
	InstanceDir string
	// Suggest — призначення пресет-іконки+кольору для збірок без PNG.
	// Може бути nil — тоді використовується детермінований фолбек
	// (стабільний пресет+колір за назвою, без мережі).
	Suggest SuggestStyle
	// OnProgress — повідомлення прогресу (копіювання файлів).
	OnProgress func(p ImportProgress)
}

// ImportProgress — прогрес імпорту ОДНОЇ збірки.
type ImportProgress struct {
	Launcher string `json:"launcher"` // prism | modrinth (заповнює App)
	PackID   string `json:"packId"`   // ID у старому лаунчері
	PackName string `json:"packName"` // назва в старому лаунчері
	Stage    string `json:"stage"`    // copying | icon | done | error
	// FilesDone/FilesTotal — прогрес копіювання файлів (0/0 до старту).
	FilesDone  int    `json:"filesDone"`
	FilesTotal int    `json:"filesTotal"`
	NewPackID  string `json:"newPackId,omitempty"` // ID створеної збірки
	Error      string `json:"error,omitempty"`
}

func (o *ImportOptions) progress(p ImportProgress) {
	if o.OnProgress != nil {
		o.OnProgress(p)
	}
}

// Import переносить збірку pack з іншого лаунчера у Shaurma Launcher.
// Повертає створену custompack.Pack. Файли гри копіюються з pack.GameDir
// у <instanceDir>/<newID>/.minecraft з виключенням кешів; іконка копіюється
// з pack.IconPath; налаштування (RAM/Java/аргументи JVM) застосовуються
// через custompack.Pack + per-instance конфіг викликача.
//
// При ПЕРЕРВАНІЙ копії (помилка читання одного файлу) помилка повертається,
// а частково створена тека залишається — викликач вирішує (App прибирає
// теку при повній невдачі). Дані джерела НІКОЛИ не чіпаються.
func Import(pack Pack, opts ImportOptions) (custompack.Pack, error) {
	if opts.Store == nil {
		return custompack.Pack{}, fmt.Errorf("внутрішня помилка: реєстр збірок не задано")
	}
	if pack.GameDir == "" || !dirExists(pack.GameDir) {
		return custompack.Pack{}, fmt.Errorf("теку гри не знайдено: %s", pack.GameDir)
	}

	// Унікальний ID + назва з суфіксом «(2)» при колізії — картки і теки
	// не плутаються між оригіналом і перенесеною копією.
	name := firstNonEmpty(pack.Name, pack.ID, "Перенесена збірка")
	id, displayName := opts.Store.UniqueIDAndName(name)

	o := opts
	o.progress(ImportProgress{PackID: pack.ID, PackName: pack.Name, Stage: "copying"})

	packDir := filepath.Join(opts.InstanceDir, id)
	gameDir := filepath.Join(packDir, ".minecraft")

	// 1. Копіювання файлів гри (без кешів). Лічимо файли заздалегідь,
	// щоб прогрес був чесним (filesDone/filesTotal).
	total := countFiles(pack.GameDir)
	done, err := copyGameDirCounted(pack.GameDir, gameDir, total, func(n int) {
		o.progress(ImportProgress{
			PackID: pack.PackID(), PackName: pack.Name, Stage: "copying",
			FilesDone: n, FilesTotal: total,
		})
	})
	if err != nil {
		return custompack.Pack{}, fmt.Errorf("копіювання файлів гри: %w", err)
	}
	_ = done

	// 2. Іконка + колір.
	iconPath := ""
	presetIcon := ""
	color := ""
	o.progress(ImportProgress{PackID: pack.PackID(), PackName: pack.Name, Stage: "icon", FilesDone: total, FilesTotal: total})
	if pack.IconPath != "" && fileExists(pack.IconPath) {
		// PNG-іконка з іншого лаунчера → стає іконкою збірки; колір —
		// домінантний колір картинки (як у браузері контенту).
		iconPath = copyIcon(pack.IconPath, packDir)
		if iconPath != "" {
			color = imageconvert.DominantColorFile(iconPath)
		}
	} else {
		// Іконки немає — ШІ призначає пресет-іконку + колір (ОДИН запит
		// на збірку: іконка і колір в одному промті).
		presetIcon, color = o.suggestStyle(pack.Name, pack.MCVersion, pack.Loader)
	}
	// ВАЖЛИВО: пресет-іконка зберігається З префіксом "ti-" (як у
	// CreatePackPage: "ti-puzzle", "ti-star"...), бо фронтенд рендерить
	// <i class="ti {icon}">. ШІ/фолбек повертають ключ БЕЗ префікса
	// ("puzzle") — без нормалізації виходить <i class="ti puzzle"> =
	// порожня іконка (реальний баг: збірка без PNG отримувала невидиму
	// картку). Якщо іконка вже з префіксом (або не зі списку пресетів) —
	// не чіпаємо.
	presetIcon = normalizePresetIcon(presetIcon)

	// 3. Запис збірки з походженням міграції.
	p := custompack.Pack{
		ID:            id,
		Name:          displayName,
		IconPath:      iconPath,
		Icon:          presetIcon,
		Color:         color,
		MCVersion:     pack.MCVersion,
		Loader:        pack.Loader,
		LoaderVersion: pack.LoaderVersion,
		// RAM/Java переносимо ЛИШЕ якщо вони реально задані у джерелі —
		// інакше збірка успадковує глобальні налаштування лаунчера.
		UseCustomRAM: pack.MaxRAMMB > 0,
		MinRAMMB:     pack.MinRAMMB,
		MaxRAMMB:     pack.MaxRAMMB,
		JavaPath:     pack.JavaPath,
		Source:       "migrated",
		MigratedFrom: &custompack.MigrationOrigin{Kind: string(pack.LauncherKind()), ID: pack.ID},
	}
	// UseSeparateJava лише коли шлях реально існує — інакше збірка
	// запускатиметься з неіснуючої Java.
	p.UseSeparateJava = pack.JavaPath != "" && dirExists(pack.JavaPath)

	if err := opts.Store.Save(p); err != nil {
		return custompack.Pack{}, fmt.Errorf("збереження збірки: %w", err)
	}

	o.progress(ImportProgress{
		PackID: pack.PackID(), PackName: pack.Name, Stage: "done",
		FilesDone: total, FilesTotal: total, NewPackID: id,
	})
	return p, nil
}

// ── Стиль для збірок без PNG-іконки ────────────────────────────────────
// Оптимізація: SuggestStyle і SuggestIcon викликаються ПО ОДНОМУ РАЗУ на
// збірку і кешуються тут, щоб не ганяти ШІ двічі (іконка+колір в одному
// промті, а не двома запитами).

func (o ImportOptions) suggestStyle(name, mcVersion, loader string) (icon, color string) {
	if o.Suggest != nil {
		return o.Suggest(name, mcVersion, loader)
	}
	// Детермінований фолбек без мережі: стабільний пресет+колір за назвою.
	return fallbackStyle(name)
}

// fallbackStyle — детермінований вибір пресет-іконки і кольору за назвою
// збірки (без ШІ): той самий стиль для тієї самої назви — повторний
// імпорт/перезапуск не змінює картку.
func fallbackStyle(name string) (string, string) {
	presets := []string{"puzzle", "star", "diamond", "crown", "flame", "bolt", "sword", "shield", "rocket", "planet", "moon", "sun", "ghost", "alien", "robot", "cat", "dog", "bug", "target", "skull"}
	colors := []string{"#ff8a00", "#4fc3f7", "#81c784", "#ff8a65", "#ba68c8", "#fff176", "#f06292", "#aed581", "#4dd0e1", "#ffd54f"}
	h := fnv32(name)
	return presets[h%uint32(len(presets))], colors[(h/7)%uint32(len(colors))]
}

// normalizePresetIcon приводить ключ пресет-іконки до формату, який рендерить
// фронтенд: з префіксом "ti-" ("puzzle" → "ti-puzzle").
//
// ВАЖЛИВО (фікс «іконки, якої не існує»): будь-яке значення НЕ зі списку
// відомих пресетів (presetIconKeys) повертається ПОРОЖНІМ, а не «як є».
// Раніше ШІ міг вигадати іконку (напр. "ti-phoenix" чи "ti-magic-wand",
// яких немає в Tabler), і normalizePresetIcon пропускала її як валідну
// (бо з префіксом "ti-") — фронтенд рендерив <i class="ti ti-phoenix">,
// якої не існує в шрифті, і картка збірки лишалась БЕЗ іконки. Тепер
// невалідний ключ → "", а фронтенд показує дефолтну іконку (ti-puzzle),
// яка гарантовано існує.
func normalizePresetIcon(icon string) string {
	key := strings.TrimPrefix(icon, "ti-")
	for _, p := range presetIconKeys {
		if key == p {
			return "ti-" + key
		}
	}
	return ""
}

// presetIconKeys — всі пресети, які знає лаунчер (ті самі, що у
// CreatePackPage та shortcuts.PresetIconGlyphs). КОЖЕН ключ перевірений:
// існує у tabler-icons.min.css (файл фронтенду) — жодної «примарної»
// іконки, що рендерилась би порожнім квадратом. Використовується для
// валідації іконки перед тим, як додати префікс "ti-".
var presetIconKeys = []string{
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

// ── Допоміжні методи Pack (для зручності викликів з App) ────────────────

// PackID повертає ID збірки у старому лаунчері (для подій прогресу).
func (p Pack) PackID() string { return p.ID }

// LauncherKind повертає тип лаунчера, з якого походить збірка (Kind
// заповнюється сканером: prism | modrinth).
func (p Pack) LauncherKind() LauncherKind { return p.Kind }

// ── Файлові помічники імпорту ───────────────────────────────────────────

// copyIcon копіює іконку іншого лаунчера у теку збірки під фіксованим
// іменем icon.<ext> (як custompack.copyAssetInto). Повертає порожній
// рядок при помилці — збірка лишиться з пресет-іконкою.
func copyIcon(src, packDir string) string {
	ext := strings.ToLower(filepath.Ext(src))
	if ext == "" || (ext != ".png" && ext != ".jpg" && ext != ".jpeg" && ext != ".webp") {
		ext = ".png"
	}
	dst := filepath.Join(packDir, "icon"+ext)
	in, err := os.Open(src)
	if err != nil {
		return ""
	}
	defer in.Close()
	if err := os.MkdirAll(packDir, 0755); err != nil {
		return ""
	}
	out, err := os.Create(dst)
	if err != nil {
		return ""
	}
	defer out.Close()
	if _, err := copyStream(out, in); err != nil {
		os.Remove(dst)
		return ""
	}
	// Повертаємо ВІДНОСНИЙ шлях (лише ім'я файлу).
	return "icon" + ext
}

// countFiles рахує кількість ФАЙЛІВ у теці (без урахування кешів) —
// для чесного прогресу копіювання.
func countFiles(root string) int {
	n := 0
	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || CacheExcluded(filepath.ToSlash(rel)) {
			return nil
		}
		n++
		return nil
	})
	return n
}

// copyGameDirCounted — CopyGameDir з лічильником прогресу: після кожного
// скопійованого файлу викликає onFile(count). Повертає кількість файлів.
func copyGameDirCounted(src, dst string, total int, onFile func(n int)) (int, error) {
	if err := os.MkdirAll(dst, 0755); err != nil {
		return 0, err
	}
	count := 0
	var firstErr error
	filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil || rel == "." {
			return nil
		}
		if CacheExcluded(filepath.ToSlash(rel)) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0755)
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if err := copyFile(path, filepath.Join(dst, rel)); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			return nil
		}
		count++
		if onFile != nil {
			onFile(count)
		}
		return nil
	})
	return count, firstErr
}
