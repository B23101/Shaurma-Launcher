package export

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ── Кандидати на експорт (дерево вибору) ────────────────────────────────
// Скануємо теку гри збірки (.minecraft) і повертаємо плоский список
// папок/файлів для UI-дерева: шлях, тип, розмір, defaultSelected.
// Правила дефолтного вибору та never-exportable — порт Modrinth App
// export_mrpack.rs (DEFAULT_SELECTED_EXPORT_PATH_PREFIXES /
// NEVER_EXPORTABLE_PATH_PREFIXES) + Prism ignores (.DS_Store, thumbs.db,
// .pw.toml, logs/, crash-reports/, .cache/, .fabric/, .quilt/).

// ExportCandidate — один елемент дерева вибору (папка або файл).
type ExportCandidate struct {
	Path            string `json:"path"` // відносно gameDir, Unix '/'
	Type            string `json:"type"` // "directory" | "file"
	Size            int64  `json:"size,omitempty"`
	Modified        int64  `json:"modified,omitempty"`
	DefaultSelected bool   `json:"defaultSelected"`
}

// defaultSelectedPrefixes — папки, вибрані за замовчуванням (як Modrinth App).
var defaultSelectedPrefixes = []string{
	"mods", "datapacks", "resourcepacks", "shaderpacks", "config",
}

// neverExportablePrefixes — шляхи, що НІКОЛИ не експортуються (Modrinth App
// NEVER_EXPORTABLE_PATH_PREFIXES + Prism ignores logs/crash-reports/.cache).
var neverExportablePrefixes = []string{
	"profile.json",        // Modrinth App: свій профіль гри не експортуємо
	"modrinth_logs",       //
	"mods/.connector",     // конектор залежностей Modrinth App
	".sable/natives",      // нативні бібліотеки Sable
	"local/crash_assistant", //
	"mods/mcef-libraries", // MCEF (Chromium) не можна перепаковувати
	"mods/mcef-cache",     //
	"config/super_resolution/libraries", // ваговиті нативні libs
	"config/Veinminer/update",           //
	"config/epicfight/native",           //
	"essential",           // Essential (заборонено перепаковувати)
	".mixin.out",          // згенеровані mixin-класи
	".fabric",             // рантайм-кеш Fabric
	".quilt",              // рантайм-кеш Quilt
	"__MACOSX",            // сміття macOS
	"logs",                // Prism ignore
	"crash-reports",       // Prism ignore
	".cache",              // Prism ignore
}

// shallowPrefixes — теки, чий ВМІСТ не сканується рекурсивно (світи можуть
// містити десятки тисяч region/chunk-файлів — повний filepath.WalkDir по
// saves/ великої збірки займав секунди й насичував диск I/O, через що
// відкриття модалки дублювання/експорту "підвисало" (заодно гальмувалась і
// решта дискових операцій лаунчера, що йшли паралельно, напр. системний
// файловий діалог — той теж читає диск). saves/ і так НЕ входить у
// defaultSelectedPrefixes (не вибирається автоматично) і не потребує
// деталізації по чанках у цьому дереві — є окрема легша ListInstanceWorlds
// для вибору конкретного світу деінде в UI. Тут показуємо саму теку ОДНИМ
// вузлом (без розкриття вмісту), щоб її все ще можна було вибрати цілком.
var shallowPrefixes = []string{
	"saves",
}

// neverExportableSuffixes — файли, що ніколи не експортуються.
var neverExportableSuffixes = []string{
	".DS_Store", "thumbs.db", "Thumbs.db", ".pw.toml",
}

// isNeverExportable — чи шлях має бути прихований з експорту повністю.
func isNeverExportable(rel string) bool {
	rel = strings.TrimPrefix(rel, "/")
	for _, p := range neverExportablePrefixes {
		if rel == p || strings.HasPrefix(rel, p+"/") {
			return true
		}
	}
	lower := strings.ToLower(rel)
	for _, s := range neverExportableSuffixes {
		if strings.HasSuffix(lower, strings.ToLower(s)) {
			return true
		}
	}
	return false
}

// isDefaultSelected — чи папка/файл вибрані за замовчуванням (префіксний
// збіг: "mods" або "mods/...").
func isDefaultSelected(rel string) bool {
	rel = strings.TrimPrefix(rel, "/")
	for _, p := range defaultSelectedPrefixes {
		if rel == p || strings.HasPrefix(rel, p+"/") {
			return true
		}
	}
	return false
}

// isShallow — чи rel це одна з shallowPrefixes (точний збіг верхнього
// рівня, не префікс вкладеності — на відміну від isNeverExportable, тут
// нас цікавить лише "чи це САМА тека saves", а не будь-що під нею).
func isShallow(rel string) bool {
	rel = strings.TrimPrefix(rel, "/")
	for _, p := range shallowPrefixes {
		if rel == p {
			return true
		}
	}
	return false
}

// ScanCandidates сканує gameDir і повертає плоский список кандидатів
// (папки першими, відсортовані за шляхом). Симлінки і never-exportable
// пропускаються.
func ScanCandidates(gameDir string) ([]ExportCandidate, error) {
	return ScanCandidatesShallow(gameDir, isShallow)
}

// ScanCandidatesShallow — те саме, але shallow вирішує, які теки показати одним вузлом
// без розкриття вмісту (у сервера світи лежать у корені, а не в saves/).
func ScanCandidatesShallow(gameDir string, shallow func(rel string) bool) ([]ExportCandidate, error) {
	var out []ExportCandidate
	err := filepath.WalkDir(gameDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // файл зник під час скану — пропускаємо
		}
		if path == gameDir {
			return nil
		}
		rel, rerr := filepath.Rel(gameDir, path)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if isNeverExportable(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil // симлінки не експортуємо (як Modrinth App)
		}
		c := ExportCandidate{
			Path:            rel,
			Type:            "file",
			DefaultSelected: isDefaultSelected(rel),
		}
		if d.IsDir() {
			c.Type = "directory"
		} else {
			c.Size = info.Size()
			c.Modified = info.ModTime().Unix()
		}
		out = append(out, c)
		if d.IsDir() && shallow(rel) {
			// Сама тека потрапляє у список як один вузол (вибирається
			// цілком), але не спускаємось у вміст — див. shallowPrefixes.
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Type != out[j].Type {
			return out[i].Type == "directory" // папки першими
		}
		return out[i].Path < out[j].Path
	})
	return out, nil
}
