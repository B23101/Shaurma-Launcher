package main

import (
	"fmt"
	"time"

	"shaurma-launcher-backend/internal/custompack"
	"shaurma-launcher-backend/internal/model"
)

// ── Тимчасові моди («становлення на строк») ─────────────────────────────
// Гравець ставить дату на встановлений мод — коли вона настає, лаунчер
// автоматично видаляє файл або вимикає його (залежно від вибору при
// встановленні дати чи Settings.TempModsDeleteByDefault). Разом з модом
// можна позначити тимчасовими і його залежності, підтягнуті лише заради
// нього — щоб видалення тимчасового мода не лишало сирітські залежності,
// не потрібні жодному іншому увімкненому моду.
//
// Дані живуть у Pack.TempMods (custompack.Store.SetTempMod/GetTempMods),
// спрацювання перевіряється у фоні раз на годину (maintenance.go →
// cleanupExpiredTempMods), не при кожному відкритті сторінки модів —
// видалення файлу не повинно залежати від того, чи дивиться зараз
// користувач на список модів.

// TempModInfo — тимчасова мітка одного мода для фронтенду: те саме, що
// зберігається в Pack.TempMods, плюс обчислене "скільки лишилось" —
// менеджер модів показує "видалиться через 3 дні", а не саму лише дату,
// без потреби рахувати різницю на фронтенді (часові пояси/UTC — зручніше
// один раз порахувати на бекенді).
type TempModInfo struct {
	FileName          string `json:"fileName"`
	ExpiresAt         string `json:"expiresAt"` // RFC3339
	Action            string `json:"action"`    // delete | disable
	AddedByDependency bool   `json:"addedByDependency,omitempty"`
	RequiredBy        string `json:"requiredBy,omitempty"`
	// SecondsRemaining — може бути ВІД'ЄМНИМ, якщо строк уже настав, але
	// прибиральник (раз/годину) ще не встиг обробити запис — фронтенд
	// показує це як "видаляється незабаром" замість негативного часу.
	SecondsRemaining int64 `json:"secondsRemaining"`
}

func toTempModInfo(fileName string, e custompack.TempModEntry) TempModInfo {
	return TempModInfo{
		FileName:          fileName,
		ExpiresAt:         e.ExpiresAt.Format(time.RFC3339),
		Action:            e.Action,
		AddedByDependency: e.AddedByDependency,
		RequiredBy:        e.RequiredBy,
		SecondsRemaining:  int64(time.Until(e.ExpiresAt).Seconds()),
	}
}

// GetTempMods повертає всі тимчасові мітки збірки — менеджер модів
// показує індикатор "час до видалення" на рядку кожного позначеного мода.
func (a *App) GetTempMods(packID string) []TempModInfo {
	st := a.customPacks
	if st == nil {
		return []TempModInfo{}
	}
	m := st.GetTempMods(packID)
	out := make([]TempModInfo, 0, len(m))
	for fileName, e := range m {
		out = append(out, toTempModInfo(fileName, e))
	}
	return out
}

// resolveTempModAction визначає дію за замовчуванням, якщо гравець явно
// не вибрав — Settings.TempModsDeleteByDefault на МОМЕНТ встановлення
// мітки (записується явно в TempModEntry.Action, а не читається заново
// при спрацюванні — див. коментар на TempModEntry.Action).
func (a *App) resolveTempModAction(action string) string {
	if action == "delete" || action == "disable" {
		return action
	}
	if a.cfg.GetSettings().TempModsDeleteByDefault {
		return "delete"
	}
	return "disable"
}

// findModByProvidedID шукає серед просканованих модів той, чий ID або
// Provides містить шуканий modId — той самий резолв, що потрібен для
// "required" залежностей версій браузера контенту (ModEntry.DependsOn
// містить modId з метаданих jar, а не FileName).
func findModByProvidedID(all []model.ModEntry, modID string) *model.ModEntry {
	for i := range all {
		if all[i].ID == modID {
			return &all[i]
		}
		for _, p := range all[i].Provides {
			if p == modID {
				return &all[i]
			}
		}
	}
	return nil
}

// SetModExpiry ставить дату автоматичного видалення/вимкнення на мод
// (fileName — ModEntry.FileName, той самий ідентифікатор, що в списку
// модів менеджера). action: "delete" | "disable" | "" (порожньо — бере
// дефолт з Settings.TempModsDeleteByDefault). includeDependencies=true —
// залежності цього мода (ModEntry.DependsOn), яких потребує ЛИШЕ він,
// позначаються тим самим строком (AddedByDependency=true), щоб видалення
// мода пізніше не лишало сирітські залежності.
func (a *App) SetModExpiry(packID, fileName, expiresAtRFC3339, action string, includeDependencies bool) error {
	if a.customPacks == nil {
		return fmt.Errorf("сховище збірок не ініціалізовано")
	}
	expiresAt, err := time.Parse(time.RFC3339, expiresAtRFC3339)
	if err != nil {
		return fmt.Errorf("некоректна дата: %w", err)
	}
	resolvedAction := a.resolveTempModAction(action)

	if err := a.customPacks.SetTempMod(packID, fileName, custompack.TempModEntry{
		ExpiresAt: expiresAt,
		Action:    resolvedAction,
	}); err != nil {
		return err
	}

	if includeDependencies {
		a.propagateTempModToDeps(packID, fileName, expiresAt, resolvedAction)
	}
	return nil
}

// propagateTempModToDeps позначає тимчасовими залежності мода fileName —
// РЕКУРСИВНО (залежність може мати власні залежності), але лише ті, що
// ще не мають ІНШОЇ (раніше поставленої вручну) тимчасової мітки —
// ручний вибір гравця для конкретної залежності ніколи не
// перезаписується автоматичним поширенням від іншого мода.
func (a *App) propagateTempModToDeps(packID, fileName string, expiresAt time.Time, action string) {
	all, err := a.modMgr.ScanModsFull(a.instanceModsDir(packID))
	if err != nil {
		return
	}
	var root *model.ModEntry
	for i := range all {
		if all[i].FileName == fileName {
			root = &all[i]
			break
		}
	}
	if root == nil {
		return
	}
	existing := a.customPacks.GetTempMods(packID)

	visited := map[string]bool{fileName: true}
	var walk func(depIDs []string, requiredBy string)
	walk = func(depIDs []string, requiredBy string) {
		for _, depID := range depIDs {
			dep := findModByProvidedID(all, depID)
			if dep == nil || visited[dep.FileName] {
				continue
			}
			visited[dep.FileName] = true
			if e, has := existing[dep.FileName]; has && !e.AddedByDependency {
				// Залежність уже має СВОЮ ручну мітку (гравець сам
				// поставив дату саме на неї) — не чіпаємо.
				continue
			}
			_ = a.customPacks.SetTempMod(packID, dep.FileName, custompack.TempModEntry{
				ExpiresAt:         expiresAt,
				Action:            action,
				AddedByDependency: true,
				RequiredBy:        requiredBy,
			})
			walk(dep.DependsOn, requiredBy)
		}
	}
	walk(root.DependsOn, fileName)
}

// RemoveModExpiry знімає тимчасову мітку з мода — «зробити постійним».
// Не чіпає мітки на ІНШИХ модах (навіть якщо вони були підтягнуті як
// залежність саме цього мода) — прибирання «осиротілих» залежностей
// відбувається лише при РЕАЛЬНОМУ видаленні/вимкненні (cleanupExpiredTempMods),
// а не при простому знятті мітки: гравець міг просто передумати щодо
// строку головного мода, а не захотіти лишити залежність назавжди
// тимчасовою чи назавжди постійною — це окреме рішення.
func (a *App) RemoveModExpiry(packID, fileName string) error {
	if a.customPacks == nil {
		return fmt.Errorf("сховище збірок не ініціалізовано")
	}
	return a.customPacks.RemoveTempMod(packID, fileName)
}
