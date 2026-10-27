package main

import (
	"shaurma-launcher-backend/internal/crashguard"
	"os"
	"path/filepath"
	"strings"

	"shaurma-launcher-backend/internal/api"
	"shaurma-launcher-backend/internal/custompack"
	"shaurma-launcher-backend/internal/export"
)

// ── Експорт збірки у .mrpack / .zip ─────────────────────────────────────
// Кнопка «Експорт» на сторінці збірки (PackInstancePage → ExportPackModal)
// відкриває модалку: вибір папок/файлів (зберігається у localStorage),
// тека призначення, формат (.mrpack і/або .zip), назва (дефолт — назва
// збірки). Якщо цільовий файл уже існує — фронтенд показує модалку
// перезапису і повторно викликає ExportPack з Force=true.
//
// Експорт іде асинхронно (go-routine), прогрес — подією export:progress
// на кожен файл (назва мода + іконка для резолвнутих). Реалізація —
// internal/export (патерн Prism ModrinthPackExportTask + Modrinth App).

// ExportCandidate — елемент дерева вибору (транзитний тип для фронтенду).
type ExportCandidate = export.ExportCandidate

// ExportPackRequest — параметри експорту з фронтенду.
type ExportPackRequest struct {
	PackID   string   `json:"packId"`
	Name     string   `json:"name"`    // назва файлу (без розширення)
	Version  string   `json:"version"` // mrpack versionId
	Summary  string   `json:"summary,omitempty"`
	Author   string   `json:"author,omitempty"` // автор (для curseforge manifest.json)
	Formats  []string `json:"formats"`          // ["mrpack"] | ["zip"] | ["curseforge"] | комбінації
	OutDir   string   `json:"outDir"`           // абсолютний шлях теки призначення
	Included []string `json:"included"`         // відносні шляхи (Unix '/')
	Excluded []string `json:"excluded"`         // відносні шляхи, зняті вручну
	Force    bool     `json:"force"`            // true = видалити існуючі файли
}

// ExportPackResult — результат експорту.
type ExportPackResult struct {
	Files     []string `json:"files"`               // повні шляхи створених файлів
	Conflicts []string `json:"conflicts,omitempty"` // лише коли !Force і файли є
}

// ListExportCandidates повертає дерево папок/файлів збірки для UI-модалки
// експорту (з дефолтним вибором і розмірами).
func (a *App) ListExportCandidates(packID string) ([]export.ExportCandidate, error) {
	build, err := a.resolveLaunchBuild(packID)
	if err != nil {
		return nil, errExportNotFound
	}
	if build.IsShaurma {
		return nil, errExportForbidden
	}
	gameDir := a.packRootDir(packID)
	if gameDir == "" {
		return nil, errExportNotFound
	}
	if _, err := os.Stat(gameDir); err != nil {
		return nil, err
	}
	return export.ScanCandidates(gameDir)
}

// ExportPack експортує збірку у обрані формати. При !Force і наявних
// цільових файлах повертає Conflicts (нічого не створюючи) — фронтенд
// показує модалку перезапису і викликає повторно з Force=true.
func (a *App) ExportPack(req ExportPackRequest) (ExportPackResult, error) {
	build, err := a.resolveLaunchBuild(req.PackID)
	if err != nil {
		return ExportPackResult{}, err
	}
	// Офіційні Shaurma-збірки експортувати заборонено (мод-пак належить
	// автору контенту, а не гравцю) — перевірка на бекенді, а не лише
	// приховуванням кнопки в UI, щоб обмеження не можна було обійти
	// прямим викликом ExportPack.
	if build.IsShaurma {
		return ExportPackResult{}, errExportForbidden
	}
	gameDir := a.packRootDir(req.PackID)
	if gameDir == "" {
		return ExportPackResult{}, errExportNotFound
	}
	if _, err := os.Stat(gameDir); err != nil {
		return ExportPackResult{}, err
	}
	if strings.TrimSpace(req.Name) == "" {
		return ExportPackResult{}, errExportName
	}
	if len(req.Formats) == 0 {
		return ExportPackResult{}, errExportFormat
	}
	if req.OutDir == "" {
		return ExportPackResult{}, errExportOutDir
	}
	if req.Version == "" {
		req.Version = "1.0.0"
	}

	// Цільові файли (один на формат).
	targets := make([]string, 0, len(req.Formats))
	for _, f := range req.Formats {
		targets = append(targets, filepath.Join(req.OutDir, exportTargetName(req.Name, f)))
	}

	// Перевірка конфліктів: якщо файл уже існує і !Force — повертаємо
	// список, НЕ починаючи експорт (модалка перезапису на фронтенді).
	if !req.Force {
		var conflicts []string
		for _, t := range targets {
			if _, err := os.Stat(t); err == nil {
				conflicts = append(conflicts, t)
			}
		}
		if len(conflicts) > 0 {
			return ExportPackResult{Conflicts: conflicts}, nil
		}
	} else {
		for _, t := range targets {
			_ = os.Remove(t)
		}
	}

	// «Єдине джерело» модів: ПЕРЕД хешуванням/пакуванням (будь-який формат)
	// оновлюємо mods/ із джерел, щоб у модпак потрапила актуальна версія.
	if err := a.syncSourcesForExport(req.PackID); err != nil {
		return ExportPackResult{}, err
	}

	// Прогрес — через emit (фронтенд-оверлей).
	progress := func(p export.Progress) {
		a.emit("export:progress", map[string]any{
			"packId":   req.PackID,
			"stage":    p.Stage,
			"current":  p.Current,
			"total":    p.Total,
			"fileName": p.FileName,
			"modName":  p.ModName,
			"iconUrl":  p.IconURL,
			"percent":  p.Percent,
		})
	}

	// Резолв sha512 → Modrinth: повертає мапу sha512 → ResolvedFile.
	// Назви/іконки модів для прогресу беремо пакетно через
	// GetProjectsBatch (до 50 проєктів за запит).
	resolveFn := func(hashes []string) (map[string]export.ResolvedFile, error) {
		versions, err := a.modrinth.GetVersionsByHashes512(hashes)
		if err != nil {
			return nil, err
		}
		// projectID → versionID (для іконок).
		projToVer := map[string]string{}
		for _, v := range versions {
			if v.ProjectID != "" && v.ID != "" {
				projToVer[v.ProjectID] = v.ID
			}
		}
		projIDs := make([]string, 0, len(projToVer))
		for id := range projToVer {
			projIDs = append(projIDs, id)
		}
		projects := map[string]*api.ModrinthProjectInfo{}
		if len(projIDs) > 0 {
			if ps, perr := a.modrinth.GetProjectsBatch(projIDs); perr == nil {
				projects = ps
			}
		}
		out := map[string]export.ResolvedFile{}
		for h, v := range versions {
			for _, f := range v.Files {
				if f.Sha512 != h {
					continue
				}
				r := export.ResolvedFile{
					URL:    f.URL,
					Sha1:   f.Sha1,
					Sha512: f.Sha512,
					Size:   f.Size,
				}
				if p, ok := projects[v.ProjectID]; ok {
					r.ModName = p.Title
					r.IconURL = p.IconURL
				}
				out[h] = r
				break
			}
		}
		return out, nil
	}

	// Запускаємо в go-routine: експорт може бути довгим (хешування сотень
	// файлів), а повертатись має одразу, щоб фронтенд показав оверлей.
	done := make(chan ExportPackResult, 1)
	errCh := make(chan error, 1)
	go func() {
		defer crashguard.Recover("export_pack.go:198")
		res, err := export.Export(gameDir, req.OutDir, export.ExportRequest{
			Name:         req.Name,
			Version:      req.Version,
			Summary:      req.Summary,
			Formats:      req.Formats,
			Included:     req.Included,
			Excluded:     req.Excluded,
			Dependencies: mrpackDependencies(build.Loader, build.MCVersion, build.LoaderVersion),
			Author:       req.Author,
			// CFResolver — резолв fingerprint → (projectID, fileID) на
			// CurseForge для формату curseforge (manifest.json files[]).
			// Без ключа/мережі — все піде в overrides/, пак валідний.
			CFResolver: cfExportAdapter{a.cfClient()},
		}, resolveFn, progress)
		if err != nil {
			a.emit("export:error", map[string]string{"packId": req.PackID, "message": err.Error()})
			errCh <- err
			return
		}
		a.emit("export:done", map[string]any{"packId": req.PackID, "files": res.Files})
		done <- ExportPackResult{Files: res.Files}
	}()

	select {
	case res := <-done:
		return res, nil
	case err := <-errCh:
		return ExportPackResult{}, err
	}
}

// exportTargetName — ім'я цільового файлу для формату (той самий, що
// internal/export пише на диск): mrpack → name.mrpack, zip → name.zip,
// curseforge → name-curseforge.zip.
func exportTargetName(name, format string) string {
	if format == "curseforge" {
		return export.SanitizeName(name) + "-curseforge.zip"
	}
	return export.SanitizeName(name) + "." + format
}

// cfExportAdapter адаптує custompack.CurseForgeClient (CFFingerprintMatch)
// під інтерфейс export.CFResolver (CFResolvedFile) — резолв fingerprint-ів
// для curseforge-експорту.
type cfExportAdapter struct {
	c *custompack.CurseForgeClient
}

func (a cfExportAdapter) ResolveFingerprints(fingerprints []string) (map[string]export.CFResolvedFile, error) {
	if a.c == nil {
		return nil, nil
	}
	matches, err := a.c.ResolveFingerprints(fingerprints)
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return map[string]export.CFResolvedFile{}, nil
	}
	out := make(map[string]export.CFResolvedFile, len(matches))
	for fp, m := range matches {
		out[fp] = export.CFResolvedFile{
			ProjectID: m.ProjectID,
			FileID:    m.FileID,
			ModName:   m.FileName,
		}
	}
	return out, nil
}

// mrpackDependencies будує dependencies для modrinth.index.json з
// лоадера/версії збірки (як Prism generateIndex: ключ за типом лоадера).
func mrpackDependencies(loader, mcVersion, loaderVersion string) map[string]string {
	deps := map[string]string{}
	if mcVersion != "" {
		deps["minecraft"] = mcVersion
	}
	switch strings.ToLower(loader) {
	case "fabric":
		if loaderVersion != "" {
			deps["fabric-loader"] = loaderVersion
		}
	case "quilt":
		if loaderVersion != "" {
			deps["quilt-loader"] = loaderVersion
		}
	case "forge":
		if loaderVersion != "" {
			deps["forge"] = loaderVersion
		}
	case "neoforge":
		if loaderVersion != "" {
			deps["neoforge"] = loaderVersion
		}
	}
	return deps
}

var (
	errExportName      = errString("назва експорту порожня")
	errExportFormat    = errString("не обрано жодного формату експорту")
	errExportOutDir    = errString("тека призначення не вказана")
	errExportNotFound  = errString("збірку не знайдено")
	errExportForbidden = errString("експорт офіційних Shaurma-збірок заборонено")
)

// packRootDir повертає теку гри збірки (.minecraft) для будь-якої збірки
// (кастомна чи Shaurma): InstanceDir/<packID>/.minecraft. Порожній рядок,
// якщо збірка не існує.
func (a *App) packRootDir(packID string) string {
	if packID == "" {
		return ""
	}
	if _, err := a.resolveLaunchBuild(packID); err != nil {
		return ""
	}
	return a.gameDir(packID)
}

type errString string

func (e errString) Error() string { return string(e) }

// ── Персистентні параметри експорту (ExportPackModal) ──────────────────
// Раніше жили ЛИШЕ в localStorage браузерного вікна (shaurma.export.
// <packId>) — переживало перезапуск профілю Wails, але НЕ переживало
// переустановлення лаунчера чи чистку профілю. Тепер додатково дублюються
// в SQLite (export_settings), той самий JSON-блоб, що фронтенд і так уже
// формує для localStorage — бекенд лише зберігає й повертає непрозоро,
// не знає власної схеми полів (назва, included/excluded, outDir,
// formats — усе визначає ExportPackModal.svelte).
//
// БД може бути недоступна (a.cfg.Store() == nil, напр. до першого
// запуску міграції або в тестовому оточенні) — у цьому разі методи тихо
// повертають "нічого не збережено" / нічого не роблять, а не падають:
// localStorage лишається робочим фолбеком незалежно від стану БД.

// GetExportSettings повертає збережений JSON параметрів останнього
// експорту для збірки, або "" якщо нічого не збережено (перший експорт
// цієї збірки, чи БД недоступна).
func (a *App) GetExportSettings(packID string) string {
	if packID == "" {
		return ""
	}
	st := a.cfg.Store()
	if st == nil {
		return ""
	}
	data, ok, err := st.LoadExportSettings(packID)
	if err != nil || !ok {
		return ""
	}
	return data
}

// SaveExportSettings зберігає JSON параметрів експорту для збірки
// (викликається з ExportPackModal при кожній успішній зміні вибору,
// не лише при фактичному експорті — той самий момент, коли раніше
// писалось у localStorage). Мовчки нічого не робить, якщо БД недоступна.
func (a *App) SaveExportSettings(packID, data string) error {
	if packID == "" {
		return nil
	}
	st := a.cfg.Store()
	if st == nil {
		return nil
	}
	return st.SaveExportSettings(packID, data)
}
