package launchermigrate

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// ── Modrinth App (theseus) ───────────────────────────────────────────────
// Дані Modrinth App лежать у теці даних %APPDATA%/<назва-теки> (settings
// dir, де живе SQLite app.db) і у config dir (теки інстансів — за
// замовчуванням та сама тека, можна перенести через налаштування
// custom_dir, збережене в app.db).
//
// НАЗВА ТЕКИ ДАНИХ ЗМІНЮВАЛАСЬ МІЖ ВЕРСІЯМИ Modrinth App:
//   - стара: %APPDATA%/com.modrinth.theseus (theseus — колишня назва);
//   - нова:  %APPDATA%/ModrinthApp.
// Перевіряємо ОБИДВА варіанти (+ env THESEUS_CONFIG_DIR, офіційний спосіб
// перенести теку даних) — інакше сканер не знайде лаунчер у користувачів
// з новішою версією (реальний випадок: у користувача стояв ModrinthApp,
// а сканер шукав лише com.modrinth.theseus).
//
//   - <settingsDir>/app.db — SQLite: таблиці instances (id, path, name,
//     icon_path, last_played...) та instance_content_sets (instance_id,
//     game_version, loader, loader_version). Також settings (custom_dir).
//   - <configDir>/profiles/<path>/ — інстанс; ТЕКА ІНСТАНСУ і є текою гри
//     (mods/, resourcepacks/, saves/, config/ лежать прямо в ній — див.
//     state/instances/adapters/filesystem.rs: instance_dir.join("mods")).
//   - Іконка: icon_path з app.db (абсолютний шлях до закешованого PNG у
//     <configDir>/caches/icons/<hash>.png або <configDir>/icons/).
//
// Джерело: modrith app/packages/app-lib/src/state (dirs.rs, db.rs,
// instances/adapters/filesystem.rs, migrations/*.sql).
//
// БАЗА ЧИТАЄТЬСЯ ЛИШЕ ДЛЯ ЧИТАННЯ (mode=ro): лаунчер НІКОЛИ не пише в дані
// Modrinth App. Якщо база зайнята іншим процесом — обробляємо як «не
// знайдено» (м'який фолбек без помилок у UI).

// theseusDBFileName — ім'я файлу бази (поточне; запасний варіант для
// старих версій, де файл називався theseus_profiles.db).
const (
	theseusDBFileName       = "app.db"
	theseusLegacyDBFileName = "theseus_profiles.db"
)

// modrinthInstance — рядок з instances + instance_content_sets.
type modrinthInstance struct {
	Path          string
	Name          string
	IconPath      string
	GameVersion   string
	Loader        string
	LoaderVersion string
}

// scanModrinth сканує Modrinth App. Повертає Launcher з Found=false, якщо
// теки даних немає (або базу не вдалось відкрити).
func scanModrinth() Launcher {
	l := Launcher{Kind: LauncherModrinth, Name: "Modrinth App"}
	settingsDir := modrinthSettingsDir()
	if settingsDir == "" || !dirExists(settingsDir) {
		l.DataDir = settingsDir
		return l
	}
	l.DataDir = settingsDir

	// Config dir: за замовчуванням = settings dir; кастомний — з app.db.
	configDir := settingsDir
	dbPath := filepath.Join(settingsDir, theseusDBFileName)
	if !fileExists(dbPath) {
		dbPath = filepath.Join(settingsDir, theseusLegacyDBFileName)
	}
	rows, ok := readTheseusInstances(dbPath)
	if !ok {
		// База недоступна (зайнята/пошкоджена) — не знайшли. М'яко.
		return l
	}

	// custom_dir (якщо користувач переніс теку інстансів).
	if cd := readTheseusCustomDir(dbPath); cd != "" {
		configDir = cd
	}

	instancesDir := filepath.Join(configDir, "profiles")
	if !dirExists(instancesDir) {
		l.Found = true // лаунчер є, але інстансів немає
		return l
	}
	l.Found = true

	for _, inst := range rows {
		instDir := filepath.Join(instancesDir, inst.Path)
		if !dirExists(instDir) {
			continue
		}
		// Тека інстансу Modrinth App = тека гри (mods/ прямо в ній).
		p := Pack{
			Kind:          LauncherModrinth,
			ID:            inst.Path,
			Name:          firstNonEmpty(inst.Name, inst.Path),
			MCVersion:     inst.GameVersion,
			Loader:        normalizeModrinthLoader(inst.Loader),
			LoaderVersion: inst.LoaderVersion,
			GameDir:       instDir,
			IconPath:      "",
			SizeBytes:     dirSize(instDir),
		}
		// Іконка: icon_path з бази (абсолютний) або <instance>/icon.png
		// (Modrinth App кладе icon.png в теки імпортованих модпаків).
		if inst.IconPath != "" && fileExists(inst.IconPath) {
			p.IconPath = inst.IconPath
		} else {
			if cand := filepath.Join(instDir, "icon.png"); fileExists(cand) {
				p.IconPath = cand
			}
		}
		l.Packs = append(l.Packs, p)
	}
	return l
}

// modrinthSettingsDir повертає теку даних Modrinth App (або "").
// Перевіряємо env THESEUS_CONFIG_DIR, потім обидва відомі імені теки в
// %APPDATA%: com.modrinth.theseus (стара версія) і ModrinthApp (нова).
// Повертається ПЕРШИЙ наявний варіант.
func modrinthSettingsDir() string {
	if env := os.Getenv("THESEUS_CONFIG_DIR"); env != "" && dirExists(env) {
		return env
	}
	if appdata := os.Getenv("APPDATA"); appdata != "" {
		for _, name := range []string{"com.modrinth.theseus", "ModrinthApp"} {
			cand := filepath.Join(appdata, name)
			if dirExists(cand) {
				return cand
			}
		}
	}
	return ""
}

// readTheseusInstances читає інстанси з app.db (лише читання). Повертає
// ok=false при будь-якій помилці (база зайнята/пошкоджена/стара схема).
func readTheseusInstances(dbPath string) ([]modrinthInstance, bool) {
	if dbPath == "" || !fileExists(dbPath) {
		return nil, false
	}
	dsn := "file:" + filepath.ToSlash(dbPath) + "?mode=ro"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, false
	}
	defer db.Close()

	// Схема могла еволюціонувати (модульні міграції Modrinth App). Спершу
	// пробуємо поточну (instances + instance_content_sets), потім стару
	// (profiles з плоскими колонками) — той самий фолбек, що в
	// legacy_converter.rs.
	if rows, ok := queryTheseusCurrent(db); ok {
		return rows, true
	}
	return queryTheseusLegacy(db)
}

// queryTheseusCurrent читає поточну схему (instances + instance_content_sets).
func queryTheseusCurrent(db *sql.DB) ([]modrinthInstance, bool) {
	q := `SELECT i.path, i.name, COALESCE(i.icon_path, ''),
		COALESCE(cs.game_version, ''), COALESCE(cs.loader, ''), COALESCE(cs.loader_version, '')
	FROM instances i
	LEFT JOIN instance_content_sets cs ON cs.instance_id = i.id`
	rows, err := db.Query(q)
	if err != nil {
		return nil, false
	}
	defer rows.Close()
	var out []modrinthInstance
	for rows.Next() {
		var inst modrinthInstance
		if err := rows.Scan(&inst.Path, &inst.Name, &inst.IconPath,
			&inst.GameVersion, &inst.Loader, &inst.LoaderVersion); err != nil {
			return nil, false
		}
		out = append(out, inst)
	}
	if err := rows.Err(); err != nil {
		return nil, false
	}
	return out, true
}

// queryTheseusLegacy читає стару схему (профілі до переходу на модульну
// схему: таблиця profiles з колонками path/name/icon_path/game_version/
// mod_loader/mod_loader_version).
func queryTheseusLegacy(db *sql.DB) ([]modrinthInstance, bool) {
	q := `SELECT path, name, COALESCE(icon_path, ''), COALESCE(game_version, ''),
		COALESCE(mod_loader, ''), COALESCE(mod_loader_version, '')
	FROM profiles`
	rows, err := db.Query(q)
	if err != nil {
		return nil, false
	}
	defer rows.Close()
	var out []modrinthInstance
	for rows.Next() {
		var inst modrinthInstance
		if err := rows.Scan(&inst.Path, &inst.Name, &inst.IconPath,
			&inst.GameVersion, &inst.Loader, &inst.LoaderVersion); err != nil {
			return nil, false
		}
		out = append(out, inst)
	}
	if err := rows.Err(); err != nil {
		return nil, false
	}
	return out, true
}

// readTheseusCustomDir читає custom_dir з таблиці settings (перенесена
// тека інстансів Modrinth App).
func readTheseusCustomDir(dbPath string) string {
	if dbPath == "" || !fileExists(dbPath) {
		return ""
	}
	dsn := "file:" + filepath.ToSlash(dbPath) + "?mode=ro"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return ""
	}
	defer db.Close()
	var customDir sql.NullString
	err = db.QueryRow(`SELECT custom_dir FROM settings LIMIT 1`).Scan(&customDir)
	if err != nil {
		return ""
	}
	if !customDir.Valid || customDir.String == "" {
		return ""
	}
	dir := strings.TrimSpace(customDir.String)
	if dir == "" {
		return ""
	}
	return dir
}

// normalizeModrinthLoader приводить loader з app.db до канонічного вигляду
// (Modrinth App зберігає "forge"/"fabric"/"quilt"/"neoforge"/"vanilla";
// на випадок варіантів на кшталт "Forge" — нормалізуємо).
func normalizeModrinthLoader(loader string) string {
	switch strings.ToLower(strings.TrimSpace(loader)) {
	case "forge", "fabric", "quilt", "neoforge", "vanilla":
		return strings.ToLower(strings.TrimSpace(loader))
	default:
		return "vanilla"
	}
}

var _ = fmt.Sprintf // (зарезервовано для подальших діагностичних повідомлень)
