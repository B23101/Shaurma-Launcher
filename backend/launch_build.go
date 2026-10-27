package main

import (
	"fmt"

	"shaurma-launcher-backend/internal/api"
	"shaurma-launcher-backend/internal/custompack"
)

// launchBuild — уніфікований опис збірки для запуску гри. Одне і те саме
// подання і для офіційних Shaurma-збірок (api.PackIndexEntry), і для
// кастомних (custompack.Pack), щоб LaunchInstance та консоль працювали з
// єдиним дескриптором, а не з різними типами.
type launchBuild struct {
	ID            string
	Name          string
	MCVersion     string
	Loader        string // vanilla | fabric | forge | neoforge | quilt
	LoaderVersion string
	IsShaurma     bool
	// RAM: UseCustomRAM=true — брати MinRAMMB/MaxRAMMB (кастомна збірка);
	// для Shaurma-збірок ці поля завжди порожні (override через icfg).
	UseCustomRAM bool
	MinRAMMB     int
	MaxRAMMB     int
	// Окрема Java: кастомна збірка зберігає свій шлях тут; Shaurma-збірка
	// тримає його в InstanceConfig (JavaPathOverride).
	UseSeparateJava bool
	JavaPath        string
	// LaunchArgs — аргументи запуску збірки з packs-index.json (пише Pack
	// Manager). Додаються до команди запуску гри (JVM-подібні — у JVM,
	// решта — у гру). RecommendedRAMMB — рекомендована RAM (для
	// попередження перед запуском).
	LaunchArgs       []string
	RecommendedRAMMB int
}

// resolveLaunchBuild знаходить збірку за ID і повертає уніфікований опис.
// Спершу перевіряється реєстр кастомних збірок (вони локальні й не
// залежать від мережі), потім — каталог Шаурма.
func (a *App) resolveLaunchBuild(id string) (*launchBuild, error) {
	// Спершу перевіряємо каталог Шаурма — Shaurma-збірки пріоритетніші
	// за кастомні (можуть бути зареєстровані в обох через orphan-скан).
	if a.packs != nil {
		if e, err := a.packs.GetPackByID(id); err == nil && e != nil {
			return packEntryToBuild(e), nil
		}
	}
	if cp, ok := a.customPacks.Get(id); ok {
		return &launchBuild{
			ID:              cp.ID,
			Name:            cp.Name,
			MCVersion:       cp.MCVersion,
			Loader:          string(cp.Loader),
			LoaderVersion:   cp.LoaderVersion,
			IsShaurma:       false,
			UseCustomRAM:    cp.UseCustomRAM,
			MinRAMMB:        cp.MinRAMMB,
			MaxRAMMB:        cp.MaxRAMMB,
			UseSeparateJava: cp.UseSeparateJava,
			JavaPath:        cp.JavaPath,
		}, nil
	}
	return nil, fmt.Errorf("збірку не знайдено: %s", id)
}

func packEntryToBuild(e *api.PackIndexEntry) *launchBuild {
	return &launchBuild{
		ID:               e.ID,
		Name:             e.Name,
		MCVersion:        e.MCVersion,
		Loader:           e.LoaderType,
		LoaderVersion:    e.LoaderVersion,
		IsShaurma:        true,
		LaunchArgs:       e.LaunchArgs,
		RecommendedRAMMB: e.RecommendedRAMMB,
	}
}

// packMeta повертає (name, mcVersion, loader) збірки незалежно від типу —
// для консолі та AI-аналізу, де не важлива решта полів.
func (a *App) packMeta(id string) (name, mcVersion, loader string) {
	if id == "" {
		return "", "", ""
	}
	if cp, ok := a.customPacks.Get(id); ok {
		return cp.Name, cp.MCVersion, string(cp.Loader)
	}
	if a.packs != nil {
		if e, err := a.packs.GetPackByID(id); err == nil && e != nil {
			return e.Name, e.MCVersion, e.LoaderType
		}
	}
	return "", "", ""
}

// packIconMeta повертає (iconURL, icon, color) збірки незалежно від типу —
// для консолі (окреме вікно без пропсів головного вікна). Формат той самий,
// що в BuildView: PNG через IconURL (для кастомних — local-file://<шлях>),
// пресет-іконка Tabler у Icon, hex колір у Color.
func (a *App) packIconMeta(id string) (iconURL, icon, color string) {
	if id == "" {
		return "", "", ""
	}
	if cp, ok := a.customPacks.Get(id); ok {
		if cp.IconPath != "" {
			iconURL = a.packAssetURL(cp, cp.IconPath)
		}
		return iconURL, cp.Icon, cp.Color
	}
	if a.packs != nil {
		// Шаурма-збірки завжди мають PNG-іконку (IconURL); Tabler-пресет
		// (Icon) у PackIndexEntry не існує — для них іконка лише з URL.
		if e, err := a.packs.GetPackByID(id); err == nil && e != nil {
			return e.IconURL, "", e.Color
		}
	}
	return "", "", ""
}

// packDeepMeta — розширена версія packMeta: додатково повертає версію
// лоадера (LoaderVersion). Потрібна AI-аналізу крашу — ШІ отримує і тип
// лоадера, і його версію, щоб оцінити сумісність, а не лише тип.
func (a *App) packDeepMeta(id string) (name, mcVersion, loader, loaderVersion string) {
	if id == "" {
		return "", "", "", ""
	}
	if cp, ok := a.customPacks.Get(id); ok {
		return cp.Name, cp.MCVersion, string(cp.Loader), cp.LoaderVersion
	}
	if a.packs != nil {
		if e, err := a.packs.GetPackByID(id); err == nil && e != nil {
			return e.Name, e.MCVersion, e.LoaderType, e.LoaderVersion
		}
	}
	return "", "", "", ""
}

// customPackOrNil повертає custompack.Pack, якщо ID належить кастомній
// збірці, інакше нульовий вказівник.
func (a *App) customPackOrNil(id string) *custompack.Pack {
	cp, ok := a.customPacks.Get(id)
	if !ok {
		return nil
	}
	return &cp
}
