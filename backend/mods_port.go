package main

import (
	"fmt"
	"strings"

	"shaurma-launcher-backend/internal/model"
)

// ── Портування модів зі збірки на сервер ────────────────────────────────
//
// На відміну від міграції, портування НЕ шукає інших версій: збірка і
// сервер мусять мати ту саму версію гри й лоадер, і на сервер копіюються
// ті самі jar-файли, що вже працюють у збірці. Різниця з простим
// копіюванням — сторона мода: клієнтські моди (міні-карти, шейдери, HUD)
// на сервер не потрапляють, серверні й спільні вибрані одразу, а моди з
// невизначеною стороною показуються окремо — рішення за користувачем.
//
// Звіт — той самий ModMigrationReport (з полем Side у кожному рядку), а
// виконання — той самий MigrateMods: копіювання, заміна старих версій і
// резервні копії замінених файлів спільні з міграцією.

const (
	modSideServer  = "server"
	modSideClient  = "client"
	modSideUnknown = "unknown"
)

// AnalyzeModPort будує звіт портування модів зі збірки sourcePackID на
// сервер targetID ("srv:<id>").
func (a *App) AnalyzeModPort(sourcePackID, targetID string) (model.ModMigrationReport, error) {
	report := model.ModMigrationReport{Items: []model.MigrationItem{}}
	if !isServerModTarget(targetID) {
		return report, fmt.Errorf("портувати моди можна лише на сервер")
	}
	src, ok := a.migrationPackByID(sourcePackID)
	if !ok {
		return report, fmt.Errorf("збірку не знайдено")
	}
	dst, ok := a.migrationPackByID(targetID)
	if !ok {
		return report, fmt.Errorf("сервер не знайдено або його ядро не підтримує моди")
	}
	if !strings.EqualFold(src.Loader, dst.Loader) || src.MCVersion != dst.MCVersion {
		return report, fmt.Errorf("портування можливе лише за однакових версії гри й лоадера: збірка — %s %s, сервер — %s %s. Для різних версій скористайтеся міграцією",
			src.Loader, src.MCVersion, dst.Loader, dst.MCVersion)
	}
	// Однакові лоадер і версія → AnalyzeModMigration іде коротким шляхом
	// (LocalCopy, без мережевого пошуку версій) і для цілі-сервера сам
	// визначає сторону кожного мода.
	return a.AnalyzeModMigration(sourcePackID, targetID, dst.MCVersion, dst.Loader)
}

// attachModSides визначає сторону кожного мода звіту: спершу з метаданих
// jar (environment у fabric.mod.json/quilt.mod.json), далі — client_side/
// server_side проєкту на Modrinth (один batch-запит за sha1 і один за
// проєктами). Заодно підтягує іконку й джерело для рядків звіту.
func (a *App) attachModSides(sourcePackID string, items []model.MigrationItem) {
	byFile := map[string]model.ModEntry{}
	for _, m := range a.scanMigrationSource(sourcePackID) {
		byFile[m.FileName] = m
	}
	var hashes []string
	for i := range items {
		it := &items[i]
		m := byFile[it.SourceFileName]
		it.Side = modSide(m.Environment, "")
		if m.Sha1 != "" {
			hashes = append(hashes, strings.ToLower(m.Sha1))
		}
	}
	if a.modrinth == nil || len(hashes) == 0 {
		return
	}
	versions, err := a.modrinth.GetVersionsByHashes(hashes)
	if err != nil || len(versions) == 0 {
		return // офлайн — сторону визначає лише jar, решту вирішить користувач
	}
	var projIDs []string
	seen := map[string]bool{}
	for _, v := range versions {
		if v != nil && v.ProjectID != "" && !seen[v.ProjectID] {
			seen[v.ProjectID] = true
			projIDs = append(projIDs, v.ProjectID)
		}
	}
	projects, _ := a.modrinth.GetProjectsBatch(projIDs)
	for i := range items {
		it := &items[i]
		v := versions[strings.ToLower(byFile[it.SourceFileName].Sha1)]
		if v == nil {
			continue
		}
		p := projects[v.ProjectID]
		if p == nil {
			continue
		}
		it.Source, it.ProjectID, it.ProjectSlug, it.IconURL = "modrinth", p.ID, p.Slug, p.IconURL
		it.Side = modSide(byFile[it.SourceFileName].Environment, p.ServerSide)
	}
}

// modSide — сторона мода: явне environment у jar ("client"/"server") —
// найточніше; «*» чи порожнє уточнюється за server_side проєкту на
// Modrinth; інакше — не визначено.
func modSide(jarEnv, modrinthServerSide string) string {
	switch strings.ToLower(strings.TrimSpace(jarEnv)) {
	case "client":
		return modSideClient
	case "server":
		return modSideServer
	}
	switch modrinthServerSide {
	case "unsupported":
		return modSideClient
	case "required", "optional":
		return modSideServer
	}
	return modSideUnknown
}
