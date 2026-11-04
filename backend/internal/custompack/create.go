package custompack

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"shaurma-launcher-backend/internal/imageconvert"
	"shaurma-launcher-backend/internal/safedel"
)

// CreateParams — усі поля з форми "Нова збірка → Вручну". Дзеркалить
// поля старого лаунчера (назва/опис/іконка, версія MC + лоадер, RAM,
// колір картки) — 1:1 з макетом 01-stvorennya-zbirky.html.
type CreateParams struct {
	Name              string
	Description       string
	IconSrcPath       string // локальний шлях до обраного файлу іконки (з діалогу вибору файлу), опційно
	BackgroundSrcPath string // локальний шлях до фону, опційно
	// BackgroundRemove — видалити вже встановлений фон збірки (лише для
	// Update; при Create ігнорується — фону ще немає). Окремий прапорець
	// потрібен, бо BackgroundSrcPath == "" означає «не чіпати фон», а не
	// «прибрати його»: без цього юзер фізично не міг видалити банер.
	BackgroundRemove bool
	Icon             string // пресет-іконка (Tabler, напр. "ti-puzzle"); PNG має пріоритет
	// IconChanged — true лише якщо користувач ЯВНО торкнувся вибору іконки
	// в цьому збереженні (клік по пресету або новий файл через діалог).
	// Без цього прапорця Update не може відрізнити "форма просто завжди
	// шле поточний iconName у payload" від "користувач свідомо обрав
	// пресет замість PNG" — і раніше видаляв робочий PNG при будь-якому
	// збереженні (навіть простої зміни назви), бо Icon непорожній.
	IconChanged bool
	Color       string

	MCVersion     string
	Loader        Loader
	LoaderVersion string

	UseCustomRAM bool
	MinRAMMB     int
	MaxRAMMB     int

	UseSeparateJava bool
	JavaPath        string
}

// PacksDir — тека, де живуть теки кастомних збірок:
// <instanceDir>/<packID>/ (той самий корінь, що й для Shaurma-збірок —
// settings.InstanceDir, як у app.LaunchInstance: gameDir = InstanceDir/id/.minecraft).
func PacksDir(instanceDir string) string { return instanceDir }

// DefaultPackName — порт InstanceName::name() з Prism: коли користувач не
// ввів назву збірки, вона формується як "<Loader> <Версія>" (напр.
// "Fabric 0.16.5", "Forge 47.4.10"). Для Vanilla назва — сама версія
// Minecraft (як Prism: setSuggestedPack(m_selectedVersion->descriptor(), ...)).
// Якщо версія лоадера ще не вибрана — лишається лише назва лоадера.
func DefaultPackName(loader Loader, loaderVersion, mcVersion string) string {
	name := ""
	switch loader {
	case LoaderFabric:
		name = "Fabric"
	case LoaderForge:
		name = "Forge"
	case LoaderNeoForge:
		name = "NeoForge"
	case LoaderQuilt:
		name = "Quilt"
	default:
		name = "" // vanilla → лише версія MC
	}
	if name != "" {
		if v := strings.TrimSpace(loaderVersion); v != "" {
			return name + " " + v
		}
		return name
	}
	if v := strings.TrimSpace(mcVersion); v != "" {
		return v
	}
	return ""
}

// Create створює нову кастомну збірку: генерує унікальний ID з назви,
// копіює іконку/фон (якщо обрані) у теку збірки, зберігає опис у Store.
// Файли самої гри (мінкрафт, моди) тут НЕ качаються — це відповідальність
// подальшого встановлення/запуску (окрема задача), Create лише фіксує
// конфігурацію, як і просив ужиток "початкова конфігурація, не запускає
// гру одразу".
func Create(store *Store, instanceDir string, p CreateParams) (Pack, error) {
	name := strings.TrimSpace(p.Name)
	// Порожня назва → дефолтна "<Loader> <Версія>" (порт Prism
	// InstanceName::name(): якщо користувач нічого не ввів — назва
	// формується з оригінальної назви + версії).
	if name == "" {
		if p.Loader == "" {
			p.Loader = LoaderVanilla
		}
		name = DefaultPackName(p.Loader, p.LoaderVersion, p.MCVersion)
	}
	if name == "" {
		return Pack{}, fmt.Errorf("назва збірки не може бути порожньою")
	}
	if strings.TrimSpace(p.MCVersion) == "" {
		return Pack{}, fmt.Errorf("не обрано версію Minecraft")
	}
	if p.Loader == "" {
		p.Loader = LoaderVanilla
	}
	// Захист від збірки без версії лоадера: якщо лоадер не Vanilla, версія
	// обов'язкова — інакше гра з таким лоадером не запуститься. Фронтенд
	// (CreatePackPage/PackBasicSection) вже блокує це в UI, але API
	// перевіряємо теж, бо форма — не єдиний виклик Create.
	if p.Loader != LoaderVanilla && strings.TrimSpace(p.LoaderVersion) == "" {
		return Pack{}, fmt.Errorf("не обрано версію лоадера %s", p.Loader)
	}

	id := store.UniqueID(name)
	packDir := filepath.Join(instanceDir, id)
	if err := os.MkdirAll(packDir, 0755); err != nil {
		return Pack{}, fmt.Errorf("не вдалося створити теку збірки: %w", err)
	}
	// .minecraft одразу — щоб OpenPackFolder і сторінка збірки бачили
	// коректну структуру ще до першого встановлення файлів гри.
	if err := os.MkdirAll(filepath.Join(packDir, ".minecraft"), 0755); err != nil {
		return Pack{}, fmt.Errorf("не вдалося створити .minecraft: %w", err)
	}

	pack := Pack{
		ID:              id,
		Name:            name,
		Description:     strings.TrimSpace(p.Description),
		Icon:            p.Icon,
		Color:           p.Color,
		MCVersion:       strings.TrimSpace(p.MCVersion),
		Loader:          string(p.Loader),
		LoaderVersion:   strings.TrimSpace(p.LoaderVersion),
		UseCustomRAM:    p.UseCustomRAM,
		MinRAMMB:        p.MinRAMMB,
		MaxRAMMB:        p.MaxRAMMB,
		UseSeparateJava: p.UseSeparateJava,
		JavaPath:        p.JavaPath,
		Source:          "manual",
	}

	if p.IconSrcPath != "" {
		dst, err := copyAssetInto(packDir, "icon", p.IconSrcPath)
		if err == nil {
			pack.IconPath = dst
		}
	}
	if p.BackgroundSrcPath != "" {
		dst, err := copyAssetInto(packDir, "background", p.BackgroundSrcPath)
		if err == nil {
			pack.BackgroundPath = dst
		}
	}

	if err := store.Save(pack); err != nil {
		return Pack{}, fmt.Errorf("не вдалося зберегти збірку: %w", err)
	}
	return pack, nil
}

// Update редагує вже створену кастомну збірку (сторінка редагування —
// ті самі поля, без зміни ID/теки на диску).
//
// Усі зміни запису застосовуються через store.Update — читання свіжого
// instance.json і запис під одним м'ютексом. Раніше тут було Get → правки →
// Save, і між Get та Save фоновий потік (імпорт, синхронізація) міг записати
// свою копію, затерши щойно обрану користувачем іконку.
func Update(store *Store, instanceDir, id string, p CreateParams) (Pack, error) {
	if _, ok := store.Get(id); !ok {
		return Pack{}, fmt.Errorf("збірку не знайдено: %s", id)
	}
	packDir := filepath.Join(instanceDir, id)
	name := strings.TrimSpace(p.Name)

	// Файлові операції без м'ютекса Store: копіювання нової іконки/фону
	// може бути повільним і не залежить від полів запису.
	var newIconPath, newBgPath string
	if p.IconSrcPath != "" {
		if dst, err := copyAssetInto(packDir, "icon", p.IconSrcPath); err == nil {
			newIconPath = dst
		}
	}
	if !p.BackgroundRemove && p.BackgroundSrcPath != "" {
		if dst, err := copyAssetInto(packDir, "background", p.BackgroundSrcPath); err == nil {
			newBgPath = dst
		}
	}

	pack, found, err := store.Update(id, func(existing *Pack) error {
		if name != "" {
			existing.Name = name
		}
		existing.Description = strings.TrimSpace(p.Description)
		// existing.Icon оновлюємо ЛИШЕ якщо форма реально несе нове значення
		// пресету (IconChanged) — інакше просте збереження назви/опису
		// перезаписувало б Icon значенням, яке фронтенд шле в payload завжди.
		// Явний вибір пресет-іконки (IconChanged && новий файл не обирався)
		// скасовує раніше завантажений PNG: інакше стара IconPath лишається
		// і скрізь у UI PNG має пріоритет над пресетом. IconSrcPath (новий
		// файл із діалогу) завжди має пріоритет — обробляється нижче.
		if p.IconChanged {
			existing.Icon = p.Icon
			if p.Icon != "" && p.IconSrcPath == "" {
				if existing.IconPath != "" {
					// IconPath відносний — розв'язуємо повний шлях для видалення.
					_ = os.Remove(filepath.Join(packDir, existing.IconPath))
				}
				existing.IconPath = ""
			}
		}
		if p.Color != "" {
			existing.Color = p.Color
		}
		if strings.TrimSpace(p.MCVersion) != "" {
			existing.MCVersion = strings.TrimSpace(p.MCVersion)
		}
		if p.Loader != "" {
			existing.Loader = string(p.Loader)
		}
		existing.LoaderVersion = strings.TrimSpace(p.LoaderVersion)
		// Той самий захист, що й у Create: не зберігати конфігурацію без
		// версії лоадера, якщо лоадер не Vanilla (перевірка ПІСЛЯ мержу).
		if Loader(existing.Loader) != LoaderVanilla && existing.LoaderVersion == "" {
			return fmt.Errorf("не обрано версію лоадера %s", existing.Loader)
		}
		existing.UseCustomRAM = p.UseCustomRAM
		existing.MinRAMMB = p.MinRAMMB
		existing.MaxRAMMB = p.MaxRAMMB
		existing.UseSeparateJava = p.UseSeparateJava
		existing.JavaPath = p.JavaPath

		if newIconPath != "" {
			existing.IconPath = newIconPath
		}
		if p.BackgroundRemove {
			// Явне видалення банера: прибираємо файл і чистимо шлях у записі.
			if existing.BackgroundPath != "" {
				_ = os.Remove(filepath.Join(packDir, existing.BackgroundPath))
			}
			existing.BackgroundPath = ""
		} else if newBgPath != "" {
			existing.BackgroundPath = newBgPath
		}
		return nil
	})
	if !found {
		return Pack{}, fmt.Errorf("збірку не знайдено: %s", id)
	}
	if err != nil {
		return Pack{}, err
	}
	return pack, nil
}

// Delete видаляє кастомну збірку: запис зі Store + всю теку на диску.
func Delete(store *Store, instanceDir, id string) error {
	if err := store.Delete(id); err != nil {
		return err
	}
	packDir := filepath.Join(instanceDir, id)
	// RemoveAll на теці конкретної збірки — безпечно, бо id завжди
	// підконтрольний slug (SlugifyName), а не довільний ввід користувача.
	return safedel.RemoveAll(packDir)
}

// copyAssetInto копіює файл іконки/фону в теку збірки під фіксованим
// іменем (зберігаючи розширення), повертає ВІДНОСНИЙ шлях (лише ім'я файлу).
// Фіксоване ім'я (icon.*, background.*) — щоб повторне редагування заміняло
// файл, а не плодило сміття в теці збірки. Повний шлях розв'язується
// динамічно через Pack.ResolveIconPath().
func copyAssetInto(packDir, baseName, srcPath string) (string, error) {
	ext := strings.ToLower(filepath.Ext(srcPath))
	if ext == "" {
		ext = ".png"
	}
	dst := filepath.Join(packDir, baseName+ext)
	in, err := os.Open(srcPath)
	if err != nil {
		return "", err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return "", err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return "", err
	}
	// Анімований GIF з loop-count=1 «грає один раз і зупиняється» —
	// нормалізуємо на нескінченний цикл (той самий фікс, що й для іконок
	// з браузера у imageconvert.FetchImageToPNG).
	if ext == ".gif" {
		imageconvert.NormalizeGIFLoopInfinite(dst)
	}
	// Повертаємо ВІДНОСНИЙ шлях (лише ім'я файлу) замість абсолютного:
	// pack.IconPath = "icon.png" замість "C:\...\packs\mymod\icon.png".
	// Повний шлях розв'язується динамічно через Pack.ResolveIconPath().
	return baseName + ext, nil
}
