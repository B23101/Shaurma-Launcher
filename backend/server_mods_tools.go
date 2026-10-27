package main

// ── Інструменти над модами СЕРВЕРА: експорт списку й порівняння серверів ──
//
// У збірок обидві можливості вже були (ExportModList, PackCompareDialog),
// у серверів — ні: меню «⋯» рядка модів сервера не мало ні «експорту
// списку», ні порівняння з іншим сервером. Тут — та сама логіка, але
// джерело даних — mods/ конкретного сервера (internal/servers.ListServerMods
// + те саме збагачення з Modrinth, що й бейджі оновлень у UI).

import (
	"fmt"
	"sort"
	"strings"

	"shaurma-launcher-backend/internal/model"
)

// ExportServerModList повертає список модів сервера у вибраному форматі
// (html/markdown/plain/json/csv) — тим самим форматувальником, що й
// ExportModList для збірок (див. mods_export.go).
//
// Збагачення з Modrinth дає реальні посилання на проєкти в експорті;
// якщо мережа недоступна — список усе одно формується, просто без URL.
func (a *App) ExportServerModList(serverID string, opts ModListExportOptions) (string, error) {
	if _, err := a.srvAPI.GetServer(serverID); err != nil {
		return "", fmt.Errorf("сервер не знайдено: %w", err)
	}
	mods, err := a.serverModsWithSources(serverID)
	if err != nil {
		return "", err
	}
	if mods == nil {
		mods = []model.ModEntry{}
	}
	return exportModListFromMods(mods, opts)
}

// ── Порівняння модів двох серверів ──────────────────────────────────────
//
// Навіщо: тримати два сервери (напр. «основний» і «тестовий») з однаковим
// набором модів — типова потреба, а вручну звіряти списки по 100+ модів
// неможливо. Порівняння показує три групи: тільки на A, тільки на B і
// спільні (з різними версіями окремим позначенням — саме розбіжні версії
// ламають сумісність при переносі світу/конфігів).

// ServerModsCompareEntry — один мод у результаті порівняння.
type ServerModsCompareEntry struct {
	Name         string `json:"name"`
	FileName     string `json:"fileName"`
	Version      string `json:"version,omitempty"`
	OtherVersion string `json:"otherVersion,omitempty"`
	Enabled      bool   `json:"enabled"`
	OtherEnabled bool   `json:"otherEnabled"`
	// Source — "modrinth"/"curseforge"/"local" (для посилання на проєкт).
	Source string `json:"source,omitempty"`
	Slug   string `json:"slug,omitempty"`
	// VersionMismatch — мод є з обох боків, але версії різні.
	VersionMismatch bool `json:"versionMismatch,omitempty"`
}

// ServerModsCompareSide — коротка інформація про сторону порівняння.
type ServerModsCompareSide struct {
	ServerID string `json:"serverId"`
	Name     string `json:"name"`
	Core     string `json:"core"`
	Total    int    `json:"total"`
}

// ServerModsCompareResult — результат порівняння модів двох серверів.
type ServerModsCompareResult struct {
	A     ServerModsCompareSide    `json:"a"`
	B     ServerModsCompareSide    `json:"b"`
	OnlyA []ServerModsCompareEntry `json:"onlyA"`
	OnlyB []ServerModsCompareEntry `json:"onlyB"`
	Both  []ServerModsCompareEntry `json:"both"`
	// MismatchCount — скільки спільних модів мають різні версії.
	MismatchCount int `json:"mismatchCount"`
}

// modCompareKey — ключ зіставлення мода між серверами. Спершу ID з
// метаданих jar (fabric.mod.json/mods.toml — стабільний між версіями й
// перейменуваннями файлу), і лише якщо його немає — нормалізоване ім'я
// файлу без .jar та без суфікса .disabled.
func modCompareKey(m model.ModEntry) string {
	if strings.TrimSpace(m.ID) != "" {
		return "id:" + strings.ToLower(strings.TrimSpace(m.ID))
	}
	name := m.Name
	if name == "" {
		name = m.FileName
	}
	name = strings.TrimSuffix(strings.TrimSuffix(name, ".disabled"), ".jar")
	return "name:" + strings.ToLower(strings.TrimSpace(name))
}

// CompareServerMods порівнює набори модів двох серверів.
func (a *App) CompareServerMods(serverA, serverB string) (ServerModsCompareResult, error) {
	if serverA == "" || serverB == "" {
		return ServerModsCompareResult{}, fmt.Errorf("оберіть два сервери для порівняння")
	}
	if serverA == serverB {
		return ServerModsCompareResult{}, fmt.Errorf("це один і той самий сервер — оберіть інший")
	}
	srvA, err := a.srvAPI.GetServer(serverA)
	if err != nil {
		return ServerModsCompareResult{}, fmt.Errorf("сервер не знайдено: %w", err)
	}
	srvB, err := a.srvAPI.GetServer(serverB)
	if err != nil {
		return ServerModsCompareResult{}, fmt.Errorf("сервер не знайдено: %w", err)
	}

	// Збагачення (source/slug/версії) — те саме, що дає бейджі оновлень;
	// помилка мережі не має валити порівняння (тоді просто без посилань).
	modsA, err := a.serverModsWithSources(serverA)
	if err != nil {
		return ServerModsCompareResult{}, err
	}
	modsB, err := a.serverModsWithSources(serverB)
	if err != nil {
		return ServerModsCompareResult{}, err
	}

	onlyA, onlyB, both, mismatch := compareModSets(modsA, modsB)
	return ServerModsCompareResult{
		A:             ServerModsCompareSide{ServerID: srvA.ID, Name: srvA.Name, Core: srvA.Core, Total: len(modsA)},
		B:             ServerModsCompareSide{ServerID: srvB.ID, Name: srvB.Name, Core: srvB.Core, Total: len(modsB)},
		OnlyA:         onlyA,
		OnlyB:         onlyB,
		Both:          both,
		MismatchCount: mismatch,
	}, nil
}

// compareModSets — чиста (без IO) частина порівняння: розкладає два набори
// модів на «тільки A», «тільки B» і «є в обох» (з позначкою різних версій).
// Винесено окремо, щоб це можна було покрити тестами без D1/файлової
// системи (див. server_mods_tools_test.go).
func compareModSets(modsA, modsB []model.ModEntry) (onlyA, onlyB, both []ServerModsCompareEntry, mismatch int) {
	byA := make(map[string]model.ModEntry, len(modsA))
	for _, m := range modsA {
		if _, dup := byA[modCompareKey(m)]; !dup {
			byA[modCompareKey(m)] = m
		}
	}
	byB := make(map[string]model.ModEntry, len(modsB))
	for _, m := range modsB {
		if _, dup := byB[modCompareKey(m)]; !dup {
			byB[modCompareKey(m)] = m
		}
	}

	onlyA, onlyB, both = []ServerModsCompareEntry{}, []ServerModsCompareEntry{}, []ServerModsCompareEntry{}
	for key, m := range byA {
		if other, ok := byB[key]; ok {
			e := compareEntry(m)
			e.OtherVersion = other.Version
			e.OtherEnabled = other.Enabled
			if !strings.EqualFold(strings.TrimSpace(m.Version), strings.TrimSpace(other.Version)) &&
				m.Version != "" && other.Version != "" {
				e.VersionMismatch = true
				mismatch++
			}
			both = append(both, e)
			continue
		}
		onlyA = append(onlyA, compareEntry(m))
	}
	for key, m := range byB {
		if _, ok := byA[key]; ok {
			continue
		}
		onlyB = append(onlyB, compareEntry(m))
	}

	byName := func(list []ServerModsCompareEntry) {
		sort.Slice(list, func(i, j int) bool { return strings.ToLower(list[i].Name) < strings.ToLower(list[j].Name) })
	}
	byName(onlyA)
	byName(onlyB)
	byName(both)
	return onlyA, onlyB, both, mismatch
}

// compareEntry — рядок результату з одного мода.
func compareEntry(m model.ModEntry) ServerModsCompareEntry {
	name := m.Name
	if name == "" {
		name = m.FileName
	}
	return ServerModsCompareEntry{
		Name:     name,
		FileName: m.FileName,
		Version:  m.Version,
		Enabled:  m.Enabled,
		Source:   m.Source,
		Slug:     m.ProjectSlug,
	}
}
