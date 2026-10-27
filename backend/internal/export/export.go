package export

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ── Оркестрація експорту ────────────────────────────────────────────────
// Export збирає файли збірки за selection (included/excluded), для
// кандидатів на резолв (моди/ресурспаки/шейдери) рахує sha1+sha512,
// резолвить через Modrinth (resolveFn) і пише один або два архіви
// (.mrpack / .zip) в outDir. Прогрес — через ProgressFn на кожному файлі.

// Progress — одна подія прогресу експорту (для emit на фронтенд).
type Progress struct {
	Stage    string  `json:"stage"`    // scan | hash | resolve | pack | done
	Current  int     `json:"current"`  // оброблено файлів (у поточній фазі)
	Total    int     `json:"total"`    // всього файлів (у поточній фазі)
	FileName string  `json:"fileName"` // відносний шлях поточного файлу
	ModName  string  `json:"modName,omitempty"`
	IconURL  string  `json:"iconUrl,omitempty"`
	Percent  float64 `json:"percent,omitempty"`
}

// ProgressFn — колбек прогресу (може бути nil).
type ProgressFn func(p Progress)

// Resolver — знаходить файли на Modrinth за sha512 (map sha512 → файл).
type Resolver interface {
	ResolveSha512(hashes []string) (map[string]ResolvedFile, error)
}

// ExportRequest — параметри експорту.
type ExportRequest struct {
	// Name — назва файлів (без розширення), також name/versionId у mrpack.
	Name    string
	Version string
	Summary string
	// Formats — "mrpack" і/або "zip" і/або "curseforge".
	Formats []string
	// OutDir — тека призначення (абсолютний шлях, вже існує).
	OutDir string
	// Included/Excluded — відносні шляхи (Unix '/') з UI-дерева.
	Included []string
	Excluded []string
	// Dependencies — {minecraft, fabric-loader, ...} для mrpack index / curseforge manifest.
	Dependencies map[string]string
	// Author — автор збірки (для curseforge manifest.json author).
	Author string
	// Resolver — опційний (Modrinth sha512); nil = без мережевого резолву (все в overrides).
	Resolver Resolver
	// CFResolver — опційний (CurseForge fingerprint); nil = формат curseforge
	// без резолву (всі файли в overrides/ — валідний, але без посилань на моди).
	CFResolver CFResolver
}

// ExportResult — створені файли.
type ExportResult struct {
	Files []string `json:"files"`
}

// modPrefixes — теки, файли з яких намагаємось резолвнути в Modrinth
// (Prism ModrinthPackExportTask::PREFIXES).
var modPrefixes = []string{"mods/", "coremods/", "resourcepacks/", "texturepacks/", "shaderpacks/"}

// modExtensions — розширення, що резолвнуться (Prism FILE_EXTENSIONS).
var modExtensions = map[string]bool{".jar": true, ".litemod": true, ".zip": true}

// isResolvableCandidate — чи файл претендує на резолв у Modrinth.
func isResolvableCandidate(rel string) bool {
	for _, p := range modPrefixes {
		if !strings.HasPrefix(rel, p) {
			continue
		}
		name := rel[len(p):]
		// .disabled — той самий файл, лише вимкнений (Prism обробляє).
		if strings.HasSuffix(name, ".disabled") {
			name = strings.TrimSuffix(name, ".disabled")
		}
		if dot := strings.LastIndex(name, "."); dot >= 0 {
			if modExtensions[strings.ToLower(name[dot:])] {
				return true
			}
		}
	}
	return false
}

// collectFiles обходить gameDir і повертає всі файли, що входять у
// selection (уникнути симлінків, never-exportable відсікається на рівні
// candidates; тут ще раз страхуємось).
func collectFiles(gameDir string, sel *exportSelection) ([]string, error) {
	var out []string
	err := filepath.WalkDir(gameDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if path == gameDir || d.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(gameDir, path)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if isNeverExportable(rel) {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		if !sel.included(rel) {
			return nil
		}
		out = append(out, rel)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// resolveMods хешує кандидатів і резолвить їх через Resolver.
// Повертає мапу шлях → ResolvedFile. Прогрес — по кожному файлу.
func resolveMods(gameDir string, allFiles []string, resolver Resolver, progress ProgressFn) map[string]ResolvedFile {
	if resolver == nil {
		return nil
	}
	// 1. Хешуємо кандидатів.
	type cand struct {
		path   string
		hashes fileHashes
	}
	// Спершу збираємо список кандидатів (Total для прогресу = фінальна
	// кількість, інакше бар «стрибав» на 100% з першого файлу).
	var candPaths []string
	for _, p := range allFiles {
		if isResolvableCandidate(p) {
			candPaths = append(candPaths, p)
		}
	}
	if len(candPaths) == 0 {
		return nil
	}
	cands := make([]cand, 0, len(candPaths))
	for i, p := range candPaths {
		h, err := hashFile(filepath.Join(gameDir, filepath.FromSlash(p)))
		if err != nil {
			continue
		}
		cands = append(cands, cand{path: p, hashes: h})
		if progress != nil {
			progress(Progress{Stage: "hash", Current: i + 1, Total: len(candPaths), FileName: p})
		}
	}
	if len(cands) == 0 {
		return nil
	}

	// 2. Резолв пакетами по 200 sha512.
	hashes := make([]string, 0, len(cands))
	for _, c := range cands {
		hashes = append(hashes, c.hashes.Sha512)
	}
	found, err := resolver.ResolveSha512(hashes)
	if err != nil {
		return nil // мережа недоступна — все піде в overrides (валидний пак)
	}
	out := map[string]ResolvedFile{}
	for _, c := range cands {
		if fv, ok := found[c.hashes.Sha512]; ok {
			out[c.path] = fv
			if progress != nil {
				progress(Progress{Stage: "resolve", FileName: c.path, ModName: fv.ModName, IconURL: fv.IconURL})
			}
		}
	}
	return out
}

// Export виконує експорт збірки (gameDir) в обрані формати.
// resolveFn — функція резолву sha512 (може бути nil). Повертає шляхи
// створених файлів.
func Export(gameDir, outDir string, req ExportRequest, resolveFn func([]string) (map[string]ResolvedFile, error), progress ProgressFn) (ExportResult, error) {
	if resolveFn != nil {
		req.Resolver = resolverAdapter(resolveFn)
	}
	return exportCore(gameDir, outDir, req, progress)
}

// resolverAdapter обгортає функцію в Resolver-інтерфейс.
func resolverAdapter(fn func([]string) (map[string]ResolvedFile, error)) Resolver {
	return fnResolver(fn)
}

type fnResolver func([]string) (map[string]ResolvedFile, error)

func (f fnResolver) ResolveSha512(hashes []string) (map[string]ResolvedFile, error) {
	return f(hashes)
}

func exportCore(gameDir, outDir string, req ExportRequest, progress ProgressFn) (ExportResult, error) {
	sel := newExportSelection(req.Included, req.Excluded)
	allFiles, err := collectFiles(gameDir, sel)
	if err != nil {
		return ExportResult{}, fmt.Errorf("сканування збірки: %w", err)
	}
	if progress != nil {
		progress(Progress{Stage: "scan", Total: len(allFiles), FileName: ""})
	}

	if len(allFiles) == 0 {
		return ExportResult{}, fmt.Errorf("немає файлів для експорту (нічого не вибрано)")
	}

	// Хешування та резолв на Modrinth потрібні ЛИШЕ для .mrpack (там моди
	// описуються посиланнями в index.json, як у Prism ModrinthPackExportTask);
	// для curseforge — резолв на CurseForge за fingerprint (manifest.json
	// files[] з projectID/fileID). Для .zip ми просто пакуємо файли тілом —
	// жодного хешування чи мережі, так само як generic/Prism zip export.
	var resolved map[string]ResolvedFile
	var resolvedCF map[string]CFResolvedFile
	needsResolve := false
	needsCFResolve := false
	for _, f := range req.Formats {
		if f == "mrpack" {
			needsResolve = true
		}
		if f == "curseforge" {
			needsCFResolve = true
		}
	}
	if needsResolve {
		resolved = resolveMods(gameDir, allFiles, req.Resolver, progress)
	}
	if needsCFResolve {
		resolvedCF = resolveModsCF(gameDir, allFiles, req.CFResolver, progress)
	}

	var res ExportResult

	for _, format := range req.Formats {
		var outPath string
		switch format {
		case "mrpack":
			outPath = filepath.Join(outDir, SanitizeName(req.Name)+".mrpack")
			if err := writeMrpack(outPath, gameDir, allFiles, resolved, req.Dependencies, req.Name, req.Version, req.Summary, progress); err != nil {
				return res, fmt.Errorf("mrpack: %w", err)
			}
		case "zip":
			outPath = filepath.Join(outDir, SanitizeName(req.Name)+".zip")
			if err := writeZip(outPath, gameDir, allFiles, progress); err != nil {
				return res, fmt.Errorf("zip: %w", err)
			}
		case "curseforge":
			outPath = filepath.Join(outDir, SanitizeName(req.Name)+"-curseforge.zip")
			if err := writeCurseForgeZip(outPath, gameDir, allFiles, resolvedCF, req.Dependencies, req.Name, req.Version, req.Summary, req.Author, progress); err != nil {
				return res, fmt.Errorf("curseforge: %w", err)
			}
		default:
			return res, fmt.Errorf("невідомий формат: %s", format)
		}
		res.Files = append(res.Files, outPath)
	}
	if progress != nil {
		progress(Progress{Stage: "done", Total: len(res.Files)})
	}
	return res, nil
}

// SanitizeName прибирає символи, недопустимі в імені файлу Windows.
func SanitizeName(name string) string {
	var sb strings.Builder
	for _, r := range name {
		switch r {
		case '<', '>', ':', '"', '/', '\\', '|', '?', '*':
			sb.WriteRune('_')
		default:
			sb.WriteRune(r)
		}
	}
	out := strings.TrimSpace(sb.String())
	if out == "" {
		return "pack"
	}
	return out
}
