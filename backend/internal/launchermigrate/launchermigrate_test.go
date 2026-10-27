package launchermigrate

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"shaurma-launcher-backend/internal/custompack"
)

// TestCacheExcluded перевіряє, що кеші в теці Minecraft не переносяться,
// а користувацькі дані (світи, моди, конфіги) — переносяться.
func TestCacheExcluded(t *testing.T) {
	excluded := []string{
		"cache", "cache/pack.png", "webcache2", "logs/latest.log",
		"crash-reports/crash-1.txt", ".fabric/remappedJars", "versions/1.20.1.json",
		"usercache.json", "launcher_accounts.json", "servers.dat.bak",
	}
	for _, p := range excluded {
		if !CacheExcluded(p) {
			t.Errorf("CacheExcluded(%q) = false, очікували true (кеш)", p)
		}
	}
	kept := []string{
		"saves/world/level.dat", "mods/create.jar", "config/options.txt",
		"resourcepacks/pack/icon.png", "options.txt", "servers.dat", "shaderpacks/x.zip",
	}
	for _, p := range kept {
		if CacheExcluded(p) {
			t.Errorf("CacheExcluded(%q) = true, очікували false (дані)", p)
		}
	}
}

// TestFallbackStyle — детермінований стиль: та сама назва → той самий
// пресет+колір, і пресет завжди з відомого списку.
func TestFallbackStyle(t *testing.T) {
	icon1, color1 := fallbackStyle("Cobblemon")
	icon2, color2 := fallbackStyle("Cobblemon")
	if icon1 != icon2 || color1 != color2 {
		t.Errorf("fallbackStyle не детермінований: (%s,%s) vs (%s,%s)", icon1, color1, icon2, color2)
	}
	if icon1 == "" || !strings.HasPrefix(color1, "#") {
		t.Errorf("fallbackStyle повернув порожній стиль: (%s,%s)", icon1, color1)
	}
}

// TestNormalizePresetIcon — іконка зі ШІ/фолбеку (ключ БЕЗ префікса
// "ti-", напр. "puzzle") має стати "ti-puzzle" — інакше фронтенд
// рендерить <i class="ti puzzle"> (порожня іконка). Це був реальний баг:
// збірка без PNG отримувала невидиму картку.
//
// Додатково: іконка, якої НЕМАЄ в presetIconKeys (ШІ вигадав, або ключ
// з префіксом "ti-", якого немає в Tabler) — нормалізується в "" (фронтенд
// покаже дефолтну ti-puzzle). Раніше невалідний ключ проходив «як є» і
// картка лишалась БЕЗ іконки (напр. ti-phoenix, якої немає в шрифті).
func TestNormalizePresetIcon(t *testing.T) {
	cases := []struct{ in, want string }{
		{"puzzle", "ti-puzzle"},
		{"star", "ti-star"},
		{"diamond", "ti-diamond"},
		{"snowflake", "ti-snowflake"},
		{"dragon", "ti-dragon"}, // нові пресети теж валідні
		{"device-gamepad", "ti-device-gamepad"},
		{"ti-puzzle", "ti-puzzle"},      // вже з префіксом — нормалізуємо
		{"ti-device-gamepad", "ti-device-gamepad"},
		{"", ""},
		{"not-a-real-icon", ""},         // не зі списку → "" (дефолт фронтенду)
		{"ti-phoenix", ""},              // з префіксом, але НЕ існує в Tabler
		{"ti-magic-wand", ""},           // теж не існує
		{"Puzzle", ""},                  // регістр — не валідний ключ
	}
	for _, c := range cases {
		if got := normalizePresetIcon(c.in); got != c.want {
			t.Errorf("normalizePresetIcon(%q) = %q, очікували %q", c.in, got, c.want)
		}
	}
}

// TestPrismPortableViaShortcut — portable-установка Prism знаходиться через
// ярлик на PrismLauncher.exe у Робочому столі (біля exe лежить UserData/).
func TestPrismPortableViaShortcut(t *testing.T) {
	root := t.TempDir()
	exeDir := filepath.Join(root, "PrismPortable")
	userData := filepath.Join(exeDir, "UserData")
	if err := os.MkdirAll(filepath.Join(userData, "instances", "p1"), 0755); err != nil {
		t.Fatal(err)
	}
	// Пакетний скрипт лаунчера (портативна тека).
	if err := os.WriteFile(filepath.Join(userData, "instances", "p1", "mmc-pack.json"), []byte(`{"formatVersion":1,"components":[{"uid":"net.minecraft","version":"1.20.1"}]}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userData, "instances", "p1", "instance.cfg"), []byte("[General]\nname=P1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(userData, "instances", "p1", ".minecraft"), 0755); err != nil {
		t.Fatal(err)
	}
	// Ярлик на робочому столі: у тілі .lnk зберігається шлях до exe.
	fakeExe := filepath.Join(exeDir, "PrismLauncher.exe")
	if err := os.WriteFile(fakeExe, []byte("exe"), 0644); err != nil {
		t.Fatal(err)
	}
	desktop := filepath.Join(root, "Desktop")
	if err := os.MkdirAll(desktop, 0755); err != nil {
		t.Fatal(err)
	}
	lnkContent := "\x00\x00" + strings.ToUpper(fakeExe) + "\x00" // шлях юнікод-рядком
	if err := os.WriteFile(filepath.Join(desktop, "Prism.lnk"), []byte(lnkContent), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("USERPROFILE", root)
	t.Setenv("APPDATA", filepath.Join(root, "AppData", "Roaming"))

	got := prismPortableDataDir()
	if got == "" {
		t.Fatal("prismPortableDataDir() = \"\" — portable-теку не знайдено через ярлик")
	}
	if !strings.EqualFold(filepath.Clean(got), filepath.Clean(userData)) {
		t.Errorf("portable тека = %q, очікували %q", got, userData)
	}

	// Повне сканування знаходить збірку.
	scan := scanPrism()
	if !scan.Found || len(scan.Packs) != 1 {
		t.Errorf("scanPrism: Found=%v, Packs=%d — очікували 1 збірку", scan.Found, len(scan.Packs))
	}
}

// TestImportCopiesGameDir — імпорт копіює файли гри (без кешів), переносить
// іконку/налаштування і зберігає походження міграції.
func TestImportCopiesGameDir(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src-game")
	dst := filepath.Join(root, "installations")
	for _, p := range []string{"saves/world/level.dat", "mods/create.jar", "config/options.txt"} {
		if err := os.MkdirAll(filepath.Join(src, filepath.Dir(p)), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(src, p), []byte("data-"+p), 0644); err != nil {
			t.Fatal(err)
		}
	}
	// Кеші — не переносяться.
	if err := os.MkdirAll(filepath.Join(src, "cache"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "cache", "big.bin"), []byte("cache"), 0644); err != nil {
		t.Fatal(err)
	}
	// Іконка.
	iconPath := filepath.Join(root, "icon.png")
	if err := os.WriteFile(iconPath, []byte("png"), 0644); err != nil {
		t.Fatal(err)
	}

	st := custompack.NewStore(filepath.Join(root, "config"))
	pack := Pack{
		Kind:       LauncherPrism,
		ID:         "my-pack",
		Name:       "My Pack",
		MCVersion:  "1.20.1",
		Loader:     "fabric",
		GameDir:    src,
		IconPath:   iconPath,
		MaxRAMMB:   4096,
		JvmArgs:    "-Xss4m",
		HasConfig:  true,
	}
	imported, err := Import(pack, ImportOptions{Store: st, InstanceDir: dst})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if imported.Source != "migrated" {
		t.Errorf("Source = %q, очікували migrated", imported.Source)
	}
	if imported.MigratedFrom == nil || imported.MigratedFrom.Kind != "prism" || imported.MigratedFrom.ID != "my-pack" {
		t.Errorf("MigratedFrom = %+v, очікували {prism my-pack}", imported.MigratedFrom)
	}
	if imported.IconPath == "" {
		t.Errorf("іконку не скопійовано")
	}
	gameDir := filepath.Join(dst, imported.ID, ".minecraft")
	for _, p := range []string{"saves/world/level.dat", "mods/create.jar", "config/options.txt"} {
		if _, err := os.Stat(filepath.Join(gameDir, p)); err != nil {
			t.Errorf("файл %s не скопійовано: %v", p, err)
		}
	}
	if _, err := os.Stat(filepath.Join(gameDir, "cache", "big.bin")); err == nil {
		t.Errorf("кеш cache/big.bin скопійовано — а не мав бути")
	}
	// Джерело не чіпається.
	if _, err := os.Stat(filepath.Join(src, "cache", "big.bin")); err != nil {
		t.Errorf("джерело було змінено: %v", err)
	}
	// Дулікат: імпорт тієї самої збірки ще раз дає «(2)» у назві та новий ID.
	second, err := Import(pack, ImportOptions{Store: st, InstanceDir: dst})
	if err != nil {
		t.Fatalf("Import (2): %v", err)
	}
	if second.ID == imported.ID {
		t.Errorf("ID дубліката = %q — очікували інший (суфікс -2)", second.ID)
	}
	if !strings.Contains(second.Name, "(2)") {
		t.Errorf("назва дубліката = %q — очікували суфікс (2)", second.Name)
	}
}

// TestModrinthScanNewDirName перевіряє, що Modrinth App знаходиться у НОВІЙ
// теці даних %APPDATA%/ModrinthApp (версії, де назва змінилась зі старої
// com.modrinth.theseus). Реальний випадок: у користувача стояв ModrinthApp,
// а сканер шукав лише стару назву — лаунчер не знаходився.
func TestModrinthScanNewDirName(t *testing.T) {
	appData := t.TempDir()
	t.Setenv("APPDATA", appData)
	t.Setenv("THESEUS_CONFIG_DIR", "")

	// Нова тека даних: %APPDATA%/ModrinthApp з app.db та profiles/<inst>.
	dir := filepath.Join(appData, "ModrinthApp")
	if err := os.MkdirAll(filepath.Join(dir, "profiles", "my-pack"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "profiles", "my-pack", "mods"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "profiles", "my-pack", "mods", "a.jar"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Мінімальна поточна схема: instances + instance_content_sets + settings.
	dbPath := filepath.Join(dir, "app.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stmts := []string{
		`CREATE TABLE settings (custom_dir TEXT)`, `INSERT INTO settings (custom_dir) VALUES ('')`,
		`CREATE TABLE instances (id INTEGER PRIMARY KEY, path TEXT, name TEXT, icon_path TEXT)`,
		`INSERT INTO instances (id, path, name, icon_path) VALUES (1, 'my-pack', 'My Pack', '')`,
		`CREATE TABLE instance_content_sets (instance_id INTEGER, game_version TEXT, loader TEXT, loader_version TEXT)`,
		`INSERT INTO instance_content_sets (instance_id, game_version, loader, loader_version) VALUES (1, '1.20.1', 'fabric', '0.15.0')`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("schema %q: %v", s, err)
		}
	}
	db.Close()

	l := scanModrinth()
	if !l.Found {
		t.Fatalf("Modrinth App не знайдено у %q (нова тека даних)", dir)
	}
	if l.DataDir != dir {
		t.Errorf("DataDir = %q, очікували %q", l.DataDir, dir)
	}
	if len(l.Packs) != 1 {
		t.Fatalf("Packs = %d, очікували 1", len(l.Packs))
	}
	p := l.Packs[0]
	if p.ID != "my-pack" || p.Name != "My Pack" || p.MCVersion != "1.20.1" || p.Loader != "fabric" {
		t.Errorf("збірка неправильна: %+v", p)
	}
	if p.GameDir != filepath.Join(dir, "profiles", "my-pack") {
		t.Errorf("GameDir = %q", p.GameDir)
	}
}

// TestModrinthScanOldDirName — те саме для старої назви теки
// com.modrinth.theseus (сумісність зі старими версіями Modrinth App).
func TestModrinthScanOldDirName(t *testing.T) {
	appData := t.TempDir()
	t.Setenv("APPDATA", appData)
	t.Setenv("THESEUS_CONFIG_DIR", "")

	dir := filepath.Join(appData, "com.modrinth.theseus")
	if err := os.MkdirAll(filepath.Join(dir, "profiles", "old-pack"), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{
		`CREATE TABLE settings (custom_dir TEXT)`, `INSERT INTO settings (custom_dir) VALUES ('')`,
		`CREATE TABLE instances (id INTEGER PRIMARY KEY, path TEXT, name TEXT, icon_path TEXT)`,
		`INSERT INTO instances (id, path, name, icon_path) VALUES (1, 'old-pack', 'Old Pack', '')`,
		`CREATE TABLE instance_content_sets (instance_id INTEGER, game_version TEXT, loader TEXT, loader_version TEXT)`,
		`INSERT INTO instance_content_sets (instance_id, game_version, loader, loader_version) VALUES (1, '1.19.2', 'forge', '43.2.0')`,
	} {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("schema: %v", err)
		}
	}
	db.Close()

	l := scanModrinth()
	if !l.Found {
		t.Fatalf("Modrinth App не знайдено у старій теці %q", dir)
	}
	if len(l.Packs) != 1 || l.Packs[0].Name != "Old Pack" {
		t.Errorf("збірки неправильні: %+v", l.Packs)
	}
}

// TestModrinthScanMissingDir — якщо теки даних немає взагалі — "не знайдено".
func TestModrinthScanMissingDir(t *testing.T) {
	appData := t.TempDir()
	t.Setenv("APPDATA", appData)
	t.Setenv("THESEUS_CONFIG_DIR", "")
	l := scanModrinth()
	if l.Found {
		t.Error("без теки даних Modrinth App має бути Found=false")
	}
}
