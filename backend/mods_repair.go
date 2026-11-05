package main

import (
	"fmt"
	"sort"
	"strings"

	"shaurma-launcher-backend/internal/model"
)

// pickRepairModVersion обирає версію мода для автоматичного оновлення
// доктором крашів. Правила обережні: лише список, відфільтрований під
// Minecraft і лоадер збірки (strict); не поточна версія й не старіша за неї;
// стабільні релізи мають пріоритет над beta/alpha (їх беремо, лише якщо
// релізів немає). Другий результат — причина відмови (порожньо при успіху).
func pickRepairModVersion(opts []model.ModVersionOption, strict bool) (model.ModVersionOption, string) {
	if len(opts) == 0 {
		return model.ModVersionOption{}, "версій не знайдено в каталозі"
	}
	if !strict {
		return model.ModVersionOption{}, "каталог не підтвердив сумісність версій із цією збіркою"
	}
	sorted := append([]model.ModVersionOption(nil), opts...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].DatePublished > sorted[j].DatePublished })

	currentDate := ""
	for _, o := range sorted {
		if o.IsCurrent {
			currentDate = o.DatePublished
			break
		}
	}
	var release, other *model.ModVersionOption
	for i := range sorted {
		o := &sorted[i]
		if o.IsCurrent || o.URL == "" || o.Filename == "" {
			continue
		}
		if currentDate != "" && o.DatePublished != "" && o.DatePublished <= currentDate {
			continue
		}
		if t := strings.ToLower(o.VersionType); t == "" || t == "release" {
			if release == nil {
				release = o
			}
		} else if other == nil {
			other = o
		}
	}
	switch {
	case release != nil:
		return *release, ""
	case other != nil:
		return *other, ""
	}
	return model.ModVersionOption{}, "вже встановлено найновішу сумісну версію"
}

// updateModForRepair оновлює один мод до новішої сумісної версії: спершу
// резервна копія (відкат — вкладка «Моди»), потім заміна файлу.
// Повертає номер встановленої версії.
func (a *App) updateModForRepair(packID string, mod model.ModEntry, mcVersion, loader string) (string, error) {
	opts, strict := a.getModVersionsEx(packID, mod.FileName, mcVersion, loader)
	opt, reason := pickRepairModVersion(opts, strict)
	if reason != "" {
		return "", fmt.Errorf("%s", reason)
	}
	if err := a.UpdateOneModBackup(packID, mod); err != nil {
		return "", err
	}
	if err := a.InstallModVersion(packID, mod.FileName, opt); err != nil {
		return "", err
	}
	name := opt.VersionNumber
	if name == "" {
		name = opt.Filename
	}
	return name, nil
}
