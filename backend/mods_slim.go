package main

// mods_slim.go — «схудання» списку модів на межі Go→JS.
//
// У рядку списку модів UI читає назву, версію, автора, прапорці, iconUrl і
// (як tooltip) опис. А ModEntry несе ще й DependsOn/Provides/MissingDeps/
// Loaders/Sha1 та повний опис (кілька КБ у деяких модів). Список модів
// відповідає ТРИЧІ за відкриття сторінки (скан → оновлення → сумісність),
// тож ці поля тричі їхали через міст і лишались у JS-купі, хоч жодне з них
// не відображається.
//
// Важливо: слайс копіюється, оригінал НЕ чіпаємо — той самий ModEntry
// лежить у modUpdateCache (Sha1 потрібен для smart-merge) і в
// attachLoaderCompat (Loaders). Худне лише те, що йде у JSON для фронтенду.

import "shaurma-launcher-backend/internal/model"

// uiModDescMax — довжина опису в tooltip (у рунах).
const uiModDescMax = 200

// uiSlimMethods — методи моста, що повертають список модів для UI-списку.
var uiSlimMethods = map[string]bool{
	"ScanModsFull":              true,
	"ScanModsWithCachedUpdates": true,
	"VerifyIncompatibleMods":    true,
	"CheckModUpdates":           true,
	"RefreshModUpdates":         true,
}

// slimModEntriesForUI повертає копію списку без важких, невидимих у UI полів.
func slimModEntriesForUI(in []model.ModEntry) []model.ModEntry {
	out := make([]model.ModEntry, len(in))
	copy(out, in)
	for i := range out {
		e := &out[i]
		e.DependsOn = nil
		e.Provides = nil
		e.MissingDeps = nil
		e.Loaders = nil
		e.Sha1 = ""
		if r := []rune(e.Description); len(r) > uiModDescMax {
			e.Description = string(r[:uiModDescMax]) + "…"
		}
	}
	return out
}

// slimBridgeResult застосовує slimModEntriesForUI до результатів відомих
// методів; усе інше повертає як є.
func slimBridgeResult(method string, result any) any {
	if !uiSlimMethods[method] {
		return result
	}
	if list, ok := result.([]model.ModEntry); ok {
		return slimModEntriesForUI(list)
	}
	return result
}
