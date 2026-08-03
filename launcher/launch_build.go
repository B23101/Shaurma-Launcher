package main

import (
	"fmt"

	"shaurma-launcher-wails/internal/api"
	"shaurma-launcher-wails/internal/custompack"
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
}

// resolveLaunchBuild знаходить збірку за ID і повертає уніфікований опис.
// Спершу перевіряється реєстр кастомних збірок (вони локальні й не
// залежать від мережі), потім — каталог Шаурма.
func (a *App) resolveLaunchBuild(id string) (*launchBuild, error) {
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
	if a.packs != nil {
		if e, err := a.packs.GetPackByID(id); err == nil && e != nil {
			return packEntryToBuild(e), nil
		}
	}
	return nil, fmt.Errorf("збірку не знайдено: %s", id)
}

func packEntryToBuild(e *api.PackIndexEntry) *launchBuild {
	return &launchBuild{
		ID:            e.ID,
		Name:          e.Name,
		MCVersion:     e.MCVersion,
		Loader:        e.LoaderType,
		LoaderVersion: e.LoaderVersion,
		IsShaurma:     true,
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

// customPackOrNil повертає custompack.Pack, якщо ID належить кастомній
// збірці, інакше нульовий вказівник.
func (a *App) customPackOrNil(id string) *custompack.Pack {
	cp, ok := a.customPacks.Get(id)
	if !ok {
		return nil
	}
	return &cp
}
