package launchermigrate

import (
	"hash/fnv"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ── Сканування ──────────────────────────────────────────────────────────

// Scan знаходить інші лаунчери (Prism Launcher, Modrinth App) у їхніх
// СТАНДАРТНИХ теках даних. Сканування обмежене відомими шляхами (див.
// коментар пакета — антивірусна безпека): жодного обходу дисків/чужих
// каталогів. Повертає завжди обидва лаунчери (з Found=true/false) —
// фронтенд показує перемикачі лише для знайдених.
func Scan() ScanResult {
	prism := scanPrism()
	modrinth := scanModrinth()
	launchers := []Launcher{prism, modrinth}
	found := 0
	for _, l := range launchers {
		if l.Found {
			found++
		}
	}
	return ScanResult{Launchers: launchers, FoundLaunchers: found}
}

// ── Файлові помічники ───────────────────────────────────────────────────

func dirExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// dirSize — сумарний розмір усіх файлів у теці (рекурсивно). Помилки
// доступу ігноруються (розмір — лише підказка для UI).
func dirSize(root string) int64 {
	var total int64
	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // недоступний файл не рахуємо
		}
		if d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

// ── Копіювання теки гри з виключенням кешів ─────────────────────────────
//
// Переносимо ВСЕ, крім кешів у теці Minecraft (ТЗ: «ми переносимо все від
// файлів до налаштувань, крім файлів кешів у папці майнкрафт»). Кеші —
// теки/файли, які лаунчер/гра відтворюють самі: їх переносити безглуздо
// (роздувають копію і можуть містити застарілі артефакти). Список — за
// стандартною структурою теки гри (vanilla .minecraft + Prism + Modrinth).

// cacheDirNames — теки-кеші, які НЕ переносяться (за іменем у корені теки
// гри). Регістр враховується без урахування регістру (Windows).
var cacheDirNames = map[string]bool{
	"cache":          true, // vanilla cache (game assets кеш)
	"webcache":       true, // webview/embedded browser кеш
	"webcache2":      true,
	"gpu-cache":      true,
	"gpushadercache": true,
	"shader_cache":   true,
	"shaderCache":    true,
	"logs":           true, // логи гри (не дані користувача)
	"crash-reports":  true, // звіти про краш
	"crash-reports-old": true,
	".fabric":        true, // Fabric cache (перестворюється при запуску)
	"mods-backup":    true, // резервні копії модів (тимчасові, у Prism)
	"backups":        true,
	"downloads":      true, // Prism: тека завантажень (не тека гри)
	"temp":           true,
	"tmp":            true,
	"natives":        true, // розпаковані нативки (перестворюються)
	"bin":            true, // старі нативки vanilla
	"saves_old":      true,
	"versions":       true, // завантажені версії (у Prism лежать поза грою)
}

// cacheFileNames — файли-кеші в корені теки гри, які не переносяться.
var cacheFileNames = map[string]bool{
	"usercache.json":        true, // кеш UUID→нік (перестворюється)
	"usernamecache.json":    true,
	"launcher_accounts.json": true, // акаунти ванільного лаунчера
	"launcher_profiles.json": true,
	"servers.dat.bak":       true, // резервна копія серверів
}

// CacheExcluded — чи шлях (відносний до кореня теки гри) є кешем і його
// НЕ треба переносити. path — відносний шлях з / розділювачем.
func CacheExcluded(path string) bool {
	path = strings.ReplaceAll(path, "\\", "/")
	first := path
	if idx := strings.IndexByte(path, '/'); idx >= 0 {
		first = path[:idx]
	}
	lower := strings.ToLower(first)
	if cacheDirNames[lower] {
		return true
	}
	// Файл у корені.
	if !strings.Contains(path, "/") && cacheFileNames[lower] {
		return true
	}
	return false
}

// CopyGameDir копіює вміст теки гри (src) у dst (теку .minecraft нової
// збірки), пропускаючи кеші (CacheExcluded). Створює dst якщо треба.
// Повертає кількість скопійованих файлів і першу помилку (не зупиняється
// на одному файлі — щоб один заблокований файл не загубив решту даних).
func CopyGameDir(src, dst string) (int, error) {
	if err := os.MkdirAll(dst, 0755); err != nil {
		return 0, err
	}
	count := 0
	var firstErr error
	filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return nil
		}
		if rel == "." {
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		if CacheExcluded(relSlash) {
			if d.IsDir() {
				return filepath.SkipDir // не заходимо в теку-кеш
			}
			return nil
		}
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0755)
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil // симлінки не переносимо (можуть вказувати назовні)
		}
		if err := copyFile(path, filepath.Join(dst, rel)); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			return nil
		}
		count++
		return nil
	})
	return count, firstErr
}

// fnv32 — 32-бітний FNV-1a хеш рядка (детермінований вибір стилю).
func fnv32(s string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return h.Sum32()
}

// copyStream копіює дані з r у w (для copyIcon).
func copyStream(w io.Writer, r io.Reader) (int64, error) {
	return io.Copy(w, r)
}

// copyFile копіює один файл (створює батьківську теку).
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	_, cpErr := io.Copy(out, in)
	closeErr := out.Close()
	if cpErr != nil {
		os.Remove(dst)
		return cpErr
	}
	return closeErr
}
