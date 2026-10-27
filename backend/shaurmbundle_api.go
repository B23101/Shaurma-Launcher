package main

// ── .shaurmbundle — груповий формат кількох збірок (папка → один файл) ──
// Окремий шлях від .shaurmpack (одна збірка) і від ExportPack (.mrpack/
// curseforge): тут — експорт ЦІЛОЇ ПАПКИ з «Мої збірки» (кілька кастомних
// збірок в одному файлі) та імпорт набору збірок назад. Реалізація
// формату — internal/shaurmpack (ExportBundle/ImportBundle); цей файл —
// лише Wails-API шар: валідація, реєстрація збірок у customPacks/registry,
// обробка колізій ID та події відкриття файлу (.shaurmpack/.shaurmbundle)
// з ОС (Windows «Відкрити за допомогою…» → авто-імпорт).

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"shaurma-launcher-backend/internal/builds"
	"shaurma-launcher-backend/internal/config"
	"shaurma-launcher-backend/internal/custompack"
	"shaurma-launcher-backend/internal/export"
	"shaurma-launcher-backend/internal/shaurmpack"
)

// ── Прев'ю ────────────────────────────────────────────────────────────────

type ShaurmbundlePreviewPack struct {
	PackID        string `json:"packId"`
	Name          string `json:"name"`
	Description   string `json:"description,omitempty"`
	Color         string `json:"color,omitempty"`
	MCVersion     string `json:"mcVersion"`
	Loader        string `json:"loader"`
	LoaderVersion string `json:"loaderVersion,omitempty"`
	FileCount     int    `json:"fileCount"`
	BundledCount  int    `json:"bundledCount"`
	ResolvedCount int    `json:"resolvedCount"`
}

type ShaurmbundlePreview struct {
	Format        string                    `json:"format"`
	Description   string                    `json:"description,omitempty"`
	LauncherName  string                    `json:"launcherName,omitempty"`
	CreatedAt     string                    `json:"createdAt,omitempty"`
	PackCount     int                       `json:"packCount"`
	TotalFiles    int                       `json:"totalFiles"`
	BundledFiles  int                       `json:"bundledFiles"`
	ResolvedFiles int                       `json:"resolvedFiles"`
	Packs         []ShaurmbundlePreviewPack `json:"packs"`
}

// PreviewShaurmbundle читає bundle.json з .shaurmbundle БЕЗ розпаковування
// (швидка операція для прев'ю перед імпортом).
func (a *App) PreviewShaurmbundle(path string) (*ShaurmbundlePreview, error) {
	m, err := shaurmpack.ReadBundleManifest(path)
	if err != nil {
		return nil, err
	}
	if err := validateBundleManifest(m); err != nil {
		return nil, err
	}
	pv := &ShaurmbundlePreview{
		Format:       m.Format,
		Description:  m.Description,
		LauncherName: m.Launcher.Name,
		CreatedAt:    m.CreatedAt,
		PackCount:    len(m.Packs),
		Packs:        make([]ShaurmbundlePreviewPack, 0, len(m.Packs)),
	}
	for _, p := range m.Packs {
		pv.Packs = append(pv.Packs, ShaurmbundlePreviewPack{
			PackID:        p.ID,
			Name:          p.Name,
			Description:   p.Description,
			Color:         p.Color,
			MCVersion:     p.MCVersion,
			Loader:        p.Loader,
			LoaderVersion: p.LoaderVersion,
			FileCount:     p.FileCount,
			BundledCount:  p.BundledCount,
			ResolvedCount: p.ResolvedCount,
		})
		pv.TotalFiles += p.FileCount
		pv.BundledFiles += p.BundledCount
		pv.ResolvedFiles += p.ResolvedCount
	}
	return pv, nil
}

func validateBundleManifest(m *shaurmpack.BundleManifest) error {
	if m == nil {
		return fmt.Errorf("порожній маніфест бандла")
	}
	if m.Format != "shaurmbundle" {
		return fmt.Errorf("некоректний формат: %q (очікувався shaurmbundle)", m.Format)
	}
	if len(m.Packs) == 0 {
		return fmt.Errorf("у бандлі немає жодної збірки")
	}
	seen := map[string]bool{}
	for _, p := range m.Packs {
		if p.ID == "" {
			return fmt.Errorf("у бандлі є збірка без ID")
		}
		if seen[p.ID] {
			return fmt.Errorf("у бандлі дублюється ID %q", p.ID)
		}
		seen[p.ID] = true
	}
	return nil
}

// ── Імпорт ────────────────────────────────────────────────────────────────

type ShaurmbundleImportPack struct {
	PackID string `json:"packId"`
	Name   string `json:"name"`
}

type ShaurmbundleImportResult struct {
	Packs       []ShaurmbundleImportPack `json:"packs"`
	TotalFiles  int                      `json:"totalFiles"`
	FilesCopied int                      `json:"filesCopied"`
	// FolderID — папка («Мої збірки»), до якої додано імпортовані збірки
	// (якщо користувач обрав її при імпорті з майстра).
	FolderID string `json:"folderId,omitempty"`
}

// ImportShaurmbundleFile імпортує .shaurmbundle: кожна збірка бандла стає
// окремою кастомною збіркою. Синхронний виклик (всі файли вже всередині
// архіву — онлайн-резолв не потрібен), прогрес по збірках шле подіями
// shaurmbundle:import:progress. При колізії ID з існуючою збіркою нова
// отримує суфікс -2/-3 (як Prism), конфліктів не створюється.
func (a *App) ImportShaurmbundleFile(path, folderID string) (ShaurmbundleImportResult, error) {
	if !strings.EqualFold(filepath.Ext(path), shaurmbundleExt) {
		return ShaurmbundleImportResult{}, fmt.Errorf("очікувався файл %s", shaurmbundleExt)
	}
	m, err := shaurmpack.ReadBundleManifest(path)
	if err != nil {
		return ShaurmbundleImportResult{}, err
	}
	if err := validateBundleManifest(m); err != nil {
		return ShaurmbundleImportResult{}, fmt.Errorf("некоректний .shaurmbundle: %w", err)
	}

	// Колізії ID: якщо тека збірки з таким ID вже існує (або збірка вже
	// зареєстрована) — перейменовуємо ID у межах імпорту (id-2, id-3, …).
	instancesDir := a.cfg.InstancesDir()
	taken := map[string]bool{}
	for _, cp := range a.customPacks.List() {
		taken[cp.ID] = true
	}
	idMap := map[string]string{}
	for _, p := range m.Packs {
		if _, exists := os.Stat(filepath.Join(instancesDir, p.ID)); exists == nil {
			taken[p.ID] = true
		}
		if taken[p.ID] {
			idMap[p.ID] = nextAvailableID(p.ID, taken)
		} else {
			taken[p.ID] = true
		}
	}

	importOpts := shaurmpack.BundleImportOptions{
		ShaurmBundlePath: path,
		TargetBaseDir:    instancesDir,
		IDMap:            idMap,
		ResolveDownload:  a.shaurmpackDownloadResolver(),
		Progress: func(done, total int, packID string) {
			a.emit("shaurmbundle:import:progress", map[string]any{
				"done": done, "total": total, "packId": packID,
			})
		},
	}
	result, err := shaurmpack.ImportBundle(importOpts)
	if err != nil {
		return ShaurmbundleImportResult{}, fmt.Errorf("імпорт .shaurmbundle: %w", err)
	}

	imported := make([]ShaurmbundleImportPack, 0, len(result.Packs))
	importedIDs := make([]string, 0, len(result.Packs))
	var totalCopied int
	for _, pr := range result.Packs {
		finalID := pr.PackID
		packDir := filepath.Join(instancesDir, finalID)
		gameDir := filepath.Join(packDir, ".minecraft")
		config.MarkNotIndexed(packDir)
		config.MarkNotIndexed(gameDir)

		// ImportBundle уже написав instance.json у теку збірки — читаємо
		// з диску. Якщо файла немає/він битий — реєструємо мінімальний
		// запис з даних маніфесту, щоб збірка все одно з'явилась у списку.
		pack, ok := a.customPacks.Get(finalID)
		if !ok {
			pack = custompack.Pack{
				Name:        firstNonEmpty(pr.Name, finalID),
				Loader:      "vanilla",
				Source:      "import",
				ImportFile:  filepath.Base(path),
				CreatedAt:   time.Now(),
				UpdatedAt:   time.Now(),
				Description: "",
			}
		}
		// ID у записаному instance.json може бути старим (з іншої машини)
		// — переписуємо під фактичну теку, інакше Store побачить збірку
		// під двома ID одночасно.
		pack.ID = finalID
		if pack.Name == "" {
			pack.Name = pr.Name
		}
		// Іконка/фон у бандлі завжди лежать під фіксованими іменами в корені
		// теки збірки — перенаправляємо поля на них (старі шляхи з іншої
		// машини тут не існують).
		if _, err := os.Stat(filepath.Join(packDir, "icon.png")); err == nil {
			pack.IconPath = "icon.png"
		}
		if _, err := os.Stat(filepath.Join(packDir, "background.png")); err == nil {
			pack.BackgroundPath = "background.png"
		}
		pack.Source = firstNonEmpty(pack.Source, "import")
		pack.ImportFile = filepath.Base(path)
		if pack.CreatedAt.IsZero() {
			pack.CreatedAt = time.Now()
		}
		if err := a.customPacks.Save(pack); err != nil {
			return ShaurmbundleImportResult{}, fmt.Errorf("реєстрація збірки %s: %w", finalID, err)
		}
		a.buildsReg.SetInstalled(finalID, builds.KindCustom, "")
		imported = append(imported, ShaurmbundleImportPack{PackID: finalID, Name: pack.Name})
		importedIDs = append(importedIDs, finalID)
		totalCopied += pr.FilesCopied
	}

	// Папка-призначення з майстра імпорту («Мої збірки») — додаємо туди
	// імпортовані збірки, щоб вони одразу з'явились у вибраній папці.
	if folderID != "" && len(importedIDs) > 0 {
		a.addPacksToFolder(folderID, importedIDs)
	}

	out := ShaurmbundleImportResult{
		Packs:       imported,
		TotalFiles:  result.TotalFiles,
		FilesCopied: totalCopied,
		FolderID:    folderID,
	}
	// Папка-призначення (майстер імпорту) — оновлюється фронтендом; тут
	// лише емітимо про завершення.
	if len(imported) > 0 {
		a.emit("builds:changed", imported[0].PackID)
		a.emit("shaurmbundle:import:done", map[string]any{
			"packs": imported, "totalFiles": result.TotalFiles, "folderId": folderID,
		})
	}
	return out, nil
}

// nextAvailableID повертає перший вільний ID виду <base>-2/-3/… (або сам
// base, якщо він вільний — викликається ЛИШЕ для колізій, тож одразу з
// суфіксом).
func nextAvailableID(base string, taken map[string]bool) string {
	if base == "" {
		base = "pack"
	}
	if !taken[base] {
		taken[base] = true
		return base
	}
	for i := 2; ; i++ {
		cand := fmt.Sprintf("%s-%d", base, i)
		if !taken[cand] {
			taken[cand] = true
			return cand
		}
	}
}

// addPacksToFolder додає збірки в папку «Мої збірки» (зберігає .build-layout
// цілком — GetPackFolders/SavePackFolders). Якщо папки вже немає — ігнорує.
func (a *App) addPacksToFolder(folderID string, packIDs []string) {
	if folderID == "" || len(packIDs) == 0 {
		return
	}
	folders := a.GetPackFolders()
	changed := false
	for i := range folders {
		if folders[i].ID != folderID {
			continue
		}
		exists := map[string]bool{}
		for _, p := range folders[i].PackIds {
			exists[p] = true
		}
		for _, p := range packIDs {
			if !exists[p] {
				folders[i].PackIds = append(folders[i].PackIds, p)
				exists[p] = true
				changed = true
			}
		}
		break
	}
	if changed {
		_ = a.SavePackFolders(folders)
	}
}

// ── Експорт папки → .shaurmbundle ─────────────────────────────────────────

type ExportShaurmbundleRequest struct {
	// FolderID — ID папки з «Мої збірки» (модель PackFolder з .build-layout).
	FolderID string `json:"folderId"`
	// OutDir — тека призначення; назва файлу = <Name>.shaurmbundle.
	OutDir string `json:"outDir"`
	Name   string `json:"name"`
	Force  bool   `json:"force"`
}

type ExportShaurmbundleResult struct {
	OutPath    string `json:"outPath"`
	Conflict   bool   `json:"conflict,omitempty"`
	PackCount  int    `json:"packCount"`
	TotalFiles int    `json:"totalFiles"`
	TotalSize  int64  `json:"totalSize"`
}

// ExportShaurmbundle експортує ВСІ кастомні збірки папки в один
// .shaurmbundle (бекап/перенесення/шер набору збірок). Порожні/ще не
// скачані збірки (немає .minecraft/) пропускаються; якщо в папці немає
// жодної збірки для експорту — повертається зрозуміла помилка.
func (a *App) ExportShaurmbundle(req ExportShaurmbundleRequest) (ExportShaurmbundleResult, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return ExportShaurmbundleResult{}, errExportName
	}
	if req.OutDir == "" {
		return ExportShaurmbundleResult{}, errExportOutDir
	}

	// Знаходимо папку і її збірки.
	folders := a.GetPackFolders()
	var folderPackIDs []string
	found := false
	for _, f := range folders {
		if f.ID == req.FolderID {
			folderPackIDs = f.PackIds
			found = true
			break
		}
	}
	if !found {
		return ExportShaurmbundleResult{}, fmt.Errorf("папку не знайдено: %s", req.FolderID)
	}

	type bundleEntry struct {
		cp      custompack.Pack
		gameDir string
	}
	entries := make([]bundleEntry, 0, len(folderPackIDs))
	seen := map[string]bool{}
	for _, pid := range folderPackIDs {
		if seen[pid] {
			continue
		}
		seen[pid] = true
		// Офіційні Shaurma-збірки не експортуються (той самий заборонний
		// виняток, що і в ExportShaurmpack) — лише кастомні.
		if b, err := a.resolveLaunchBuild(pid); err == nil && b.IsShaurma {
			continue
		}
		cp, ok := a.customPacks.Get(pid)
		if !ok {
			continue
		}
		gameDir := a.packRootDir(pid)
		if gameDir == "" {
			continue
		}
		if st, err := os.Stat(gameDir); err != nil || !st.IsDir() {
			// Збірка ще не скачана — файлів немає, експортувати нічого.
			continue
		}
		entries = append(entries, bundleEntry{cp: cp, gameDir: gameDir})
	}
	if len(entries) == 0 {
		return ExportShaurmbundleResult{}, fmt.Errorf("у папці немає встановлених збірок для експорту")
	}

	outPath := filepath.Join(req.OutDir, export.SanitizeName(name)+shaurmbundleExt)
	if !req.Force {
		if _, err := os.Stat(outPath); err == nil {
			return ExportShaurmbundleResult{OutPath: outPath, Conflict: true}, nil
		}
	}

	// «Єдине джерело» модів: ПЕРЕД пакуванням оновлюємо mods/ кожної збірки
	// із її джерел (у бандл має потрапити актуальна версія).
	for _, e := range entries {
		if err := a.syncSourcesForExport(e.cp.ID); err != nil {
			return ExportShaurmbundleResult{}, fmt.Errorf("збірка %s: %w", e.cp.Name, err)
		}
	}

	packs := make([]shaurmpack.BundlePackExport, 0, len(entries))
	for _, e := range entries {
		instanceRoot := filepath.Dir(e.gameDir) // InstanceDir/<id>/ (.minecraft — поруч)
		packData, _ := json.Marshal(e.cp)
		iconFile := ""
		if e.cp.IconPath != "" {
			cand := e.cp.IconPath
			if !filepath.IsAbs(cand) {
				cand = filepath.Join(instanceRoot, cand)
			}
			if st, err := os.Stat(cand); err == nil && !st.IsDir() {
				iconFile = cand
			}
		}
		bgFile := ""
		if e.cp.BackgroundPath != "" {
			cand := e.cp.BackgroundPath
			if !filepath.IsAbs(cand) {
				cand = filepath.Join(instanceRoot, cand)
			}
			if st, err := os.Stat(cand); err == nil && !st.IsDir() {
				bgFile = cand
			}
		}
		packs = append(packs, shaurmpack.BundlePackExport{
			PackID:         e.cp.ID,
			PackData:       packData,
			GameDir:        e.gameDir,
			IconFile:       iconFile,
			BackgroundFile: bgFile,
		})
	}

	total := len(packs)
	done := 0
	result, err := shaurmpack.ExportBundle(shaurmpack.BundleExportOptions{
		Packs:             packs,
		OutPath:           outPath,
		LauncherName:      "Shaurma Launcher",
		LauncherVersion:   appVersion,
		ResolveModrinthBatch:   a.shaurmpackModrinthBatchResolver(),
		ResolveCurseForgeBatch: a.shaurmpackCurseForgeBatchResolver(),
		SHA1Of:                 a.shaurmpackSHA1Of(),
		Progress: func(d int, t int, packID string) {
			done = d
			a.emit("shaurmbundle:export:progress", map[string]any{
				"done": done, "total": total, "packId": packID, "outPath": outPath,
			})
		},
	})
	if err != nil {
		return ExportShaurmbundleResult{}, err
	}
	return ExportShaurmbundleResult{
		OutPath:    outPath,
		PackCount:  result.PackCount,
		TotalFiles: result.TotalFiles,
		TotalSize:  result.TotalSize,
	}, nil
}

// ── Відкриття .shaurmpack/.shaurmbundle з ОС (Windows «Open with») ───────
// Shell передає шлях до файлу як аргумент; подія packfile:open може
// прийти ДО того, як фронтенд підписався на події — тому зберігаємо шлях
// як pending (та сама схема, що PendingShortcutLaunch у shortcuts.go), і
// фронтенд забирає його через TakePendingPackImportFile на маунті.

var (
	pendingPackImportFile   string
	pendingPackImportFileMu sync.Mutex
)

// setPendingPackImportFile запам'ятовує шлях до файлу збірки (останній
// виклик перемагає).
func setPendingPackImportFile(path string) {
	pendingPackImportFileMu.Lock()
	pendingPackImportFile = path
	pendingPackImportFileMu.Unlock()
}

// TakePendingPackImportFile повертає і очищає шлях файлу збірки, відкритий
// через ОС.
func (a *App) TakePendingPackImportFile() string {
	pendingPackImportFileMu.Lock()
	defer pendingPackImportFileMu.Unlock()
	p := pendingPackImportFile
	pendingPackImportFile = ""
	return p
}
