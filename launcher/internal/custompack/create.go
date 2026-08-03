package custompack

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// CreateParams — усі поля з форми "Нова збірка → Вручну". Дзеркалить
// поля старого лаунчера (назва/опис/іконка, версія MC + лоадер, RAM,
// колір картки) — 1:1 з макетом 01-stvorennya-zbirky.html.
type CreateParams struct {
	Name        string
	Description string
	IconSrcPath string // локальний шлях до обраного файлу іконки (з діалогу вибору файлу), опційно
	BackgroundSrcPath string // локальний шлях до фону, опційно
	Icon        string // пресет-іконка (Tabler, напр. "ti-puzzle"); PNG має пріоритет
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

// Create створює нову кастомну збірку: генерує унікальний ID з назви,
// копіює іконку/фон (якщо обрані) у теку збірки, зберігає опис у Store.
// Файли самої гри (мінкрафт, моди) тут НЕ качаються — це відповідальність
// подальшого встановлення/запуску (окрема задача), Create лише фіксує
// конфігурацію, як і просив ужиток "початкова конфігурація, не запускає
// гру одразу".
func Create(store *Store, instanceDir string, p CreateParams) (Pack, error) {
	name := strings.TrimSpace(p.Name)
	if name == "" {
		return Pack{}, fmt.Errorf("назва збірки не може бути порожньою")
	}
	if strings.TrimSpace(p.MCVersion) == "" {
		return Pack{}, fmt.Errorf("не обрано версію Minecraft")
	}
	if p.Loader == "" {
		p.Loader = LoaderVanilla
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
		ID:            id,
		Name:          name,
		Description:   strings.TrimSpace(p.Description),
		Icon:          p.Icon,
		Color:         p.Color,
		MCVersion:     strings.TrimSpace(p.MCVersion),
		Loader:        string(p.Loader),
		LoaderVersion: strings.TrimSpace(p.LoaderVersion),
		UseCustomRAM:  p.UseCustomRAM,
		MinRAMMB:      p.MinRAMMB,
		MaxRAMMB:      p.MaxRAMMB,
		UseSeparateJava: p.UseSeparateJava,
		JavaPath:      p.JavaPath,
		Source:        "manual",
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
func Update(store *Store, instanceDir, id string, p CreateParams) (Pack, error) {
	existing, ok := store.Get(id)
	if !ok {
		return Pack{}, fmt.Errorf("збірку не знайдено: %s", id)
	}
	name := strings.TrimSpace(p.Name)
	if name != "" {
		existing.Name = name
	}
	existing.Description = strings.TrimSpace(p.Description)
	existing.Icon = p.Icon
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
	existing.UseCustomRAM = p.UseCustomRAM
	existing.MinRAMMB = p.MinRAMMB
	existing.MaxRAMMB = p.MaxRAMMB
	existing.UseSeparateJava = p.UseSeparateJava
	existing.JavaPath = p.JavaPath

	packDir := filepath.Join(instanceDir, id)
	if p.IconSrcPath != "" {
		if dst, err := copyAssetInto(packDir, "icon", p.IconSrcPath); err == nil {
			existing.IconPath = dst
		}
	}
	if p.BackgroundSrcPath != "" {
		if dst, err := copyAssetInto(packDir, "background", p.BackgroundSrcPath); err == nil {
			existing.BackgroundPath = dst
		}
	}

	if err := store.Save(existing); err != nil {
		return Pack{}, err
	}
	return existing, nil
}

// Delete видаляє кастомну збірку: запис зі Store + всю теку на диску.
func Delete(store *Store, instanceDir, id string) error {
	if err := store.Delete(id); err != nil {
		return err
	}
	packDir := filepath.Join(instanceDir, id)
	// RemoveAll на теці конкретної збірки — безпечно, бо id завжди
	// підконтрольний slug (SlugifyName), а не довільний ввід користувача.
	return os.RemoveAll(packDir)
}

// copyAssetInto копіює файл іконки/фону в теку збірки під фіксованим
// іменем (зберігаючи розширення), повертає новий шлях. Фіксоване ім'я
// (icon.*, background.*) — щоб повторне редагування заміняло файл, а не
// плодило сміття в теці збірки.
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
	return dst, nil
}
