package custompack

// Регресійні тести розпакування overrides CurseForge-модпаку (.zip).
//
// Скарга користувача: «Prism може розпакувати модпак, а мій лаунчер ні».
// Ці тести перевіряють САМЕ розпакування вмісту архіву в .minecraft/:
//   - overrides/ (і кастомний шлях overrides з manifest.json);
//   - верхній рівень-обгортку "<Назва>/" (деякі експортери загортають усе);
//   - виключення службових маніфестів (generic-режим);
//   - zip-slip захист (шлях ../ не має вийти за межі .minecraft/).

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"testing"
)

// buildTestZip створює zip-архів зі списком файлів (name → content).
func buildTestZip(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func openZip(t *testing.T, path string) *zip.Reader {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { zr.Close() })
	return &zr.Reader
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestExtractOverridesCurseForgeZip — типовий CF zip: manifest.json + overrides/.
func TestExtractOverridesCurseForgeZip(t *testing.T) {
	path := buildTestZip(t, map[string]string{
		"manifest.json":             `{"manifestType":"minecraftModpack"}`,
		"overrides/config/foo.cfg":  "foo=1",
		"overrides/options.txt":     "fov:0.0",
		"overrides/mods/some.jar":   "jar-bytes",
		"overrides/resourcepacks/x": "rp",
	})
	gameDir := t.TempDir()
	if _, err := extractOverrides(context.Background(), openZip(t, path), []string{"overrides/"}, gameDir, false, nil); err != nil {
		t.Fatalf("extractOverrides: %v", err)
	}
	// Вміст overrides/ розпакований у .minecraft/.
	for want, content := range map[string]string{
		"config/foo.cfg":  "foo=1",
		"options.txt":     "fov:0.0",
		"mods/some.jar":   "jar-bytes",
		"resourcepacks/x": "rp",
	} {
		got := readFile(t, filepath.Join(gameDir, want))
		if got != content {
			t.Fatalf("%s: got %q, want %q", want, got, content)
		}
	}
	// manifest.json НЕ розпаковується (він поза overrides/).
	if _, err := os.Stat(filepath.Join(gameDir, "manifest.json")); err == nil {
		t.Fatal("manifest.json не мав потрапити в .minecraft/")
	}
}

// TestExtractOverridesWithWrapper — CF zip, де все загорнуто у "<Назва>/".
func TestExtractOverridesWithWrapper(t *testing.T) {
	path := buildTestZip(t, map[string]string{
		"MyModpack/manifest.json":          `{}`,
		"MyModpack/overrides/config/a.cfg": "a=1",
		"MyModpack/overrides/mods/m.jar":   "m",
	})
	gameDir := t.TempDir()
	if _, err := extractOverrides(context.Background(), openZip(t, path), []string{"overrides/"}, gameDir, false, nil); err != nil {
		t.Fatalf("extractOverrides: %v", err)
	}
	if got := readFile(t, filepath.Join(gameDir, "config/a.cfg")); got != "a=1" {
		t.Fatalf("config/a.cfg: got %q", got)
	}
	if got := readFile(t, filepath.Join(gameDir, "mods/m.jar")); got != "m" {
		t.Fatalf("mods/m.jar: got %q", got)
	}
}

// TestExtractOverridesCustomManifestPath — manifest із кастомним полем
// "overrides" (не завжди "overrides/", деякі модпаки пишуть "pack/").
func TestExtractOverridesCustomManifestPath(t *testing.T) {
	path := buildTestZip(t, map[string]string{
		"manifest.json":     `{"overrides":"pack"}`,
		"pack/config/b.cfg": "b=1",
		"pack/mods/one.jar": "one",
	})
	gameDir := t.TempDir()
	if _, err := extractOverrides(context.Background(), openZip(t, path), []string{"pack/"}, gameDir, false, nil); err != nil {
		t.Fatalf("extractOverrides: %v", err)
	}
	if got := readFile(t, filepath.Join(gameDir, "config/b.cfg")); got != "b=1" {
		t.Fatalf("config/b.cfg: got %q", got)
	}
}

// TestExtractOverridesZipSlip — шкідливий шлях ../ не виходить за .minecraft/.
func TestExtractOverridesZipSlip(t *testing.T) {
	path := buildTestZip(t, map[string]string{
		"overrides/options.txt":    "ok=1",
		"overrides/../../evil.txt": "evil",
	})
	gameDir := t.TempDir()
	if _, err := extractOverrides(context.Background(), openZip(t, path), []string{"overrides/"}, gameDir, false, nil); err != nil {
		t.Fatalf("extractOverrides: %v", err)
	}
	if got := readFile(t, filepath.Join(gameDir, "options.txt")); got != "ok=1" {
		t.Fatalf("options.txt: got %q", got)
	}
	if _, err := os.Stat(filepath.Join(gameDir, "..", "evil.txt")); err == nil {
		t.Fatal("zip-slip: evil.txt не мав розпакуватись поза .minecraft/")
	}
}

// TestExtractOverridesGeneric — generic-режим (prefix ""): весь архів як
// overrides, службові маніфести пропускаються, обгортка знімається.
func TestExtractOverridesGeneric(t *testing.T) {
	path := buildTestZip(t, map[string]string{
		"Pack/config/g.cfg":        "g=1",
		"Pack/modrinth.index.json": `{}`,
		"Pack/manifest.json":       `{}`,
		"Pack/modlist.html":        `<ul></ul>`,
		"Pack/mods/z.jar":          "z",
	})
	gameDir := t.TempDir()
	if _, err := extractOverrides(context.Background(), openZip(t, path), []string{""}, gameDir, false, nil); err != nil {
		t.Fatalf("extractOverrides: %v", err)
	}
	if got := readFile(t, filepath.Join(gameDir, "config/g.cfg")); got != "g=1" {
		t.Fatalf("config/g.cfg: got %q", got)
	}
	if got := readFile(t, filepath.Join(gameDir, "mods/z.jar")); got != "z" {
		t.Fatalf("mods/z.jar: got %q", got)
	}
	for _, name := range []string{"modrinth.index.json", "manifest.json", "modlist.html"} {
		if _, err := os.Stat(filepath.Join(gameDir, name)); err == nil {
			t.Fatalf("%s не мав потрапити в .minecraft/", name)
		}
	}
}

// TestPlanImportCurseForgeOverridesPrefix — план CF імпорту: overrides-префікс
// з manifest.json (включно з кастомним "overrides" полем) + files[] для API.
func TestPlanImportCurseForgeOverridesPrefix(t *testing.T) {
	path := buildTestZip(t, map[string]string{
		"manifest.json": `{"minecraft":{"version":"1.20.1","modLoaders":[{"id":"forge-47.1.0"}]},"manifestType":"minecraftModpack","overrides":"overrides","files":[{"projectID":1,"fileID":2,"required":true}]}`,
	})
	det := &DetectedPack{Format: FormatCurseForge}
	tasks, prefixes, cfFiles, err := planImport(openZip(t, path), det)
	if err != nil {
		t.Fatalf("planImport: %v", err)
	}
	if len(prefixes) != 1 || prefixes[0] != "overrides/" {
		t.Fatalf("overrides prefixes: got %v, want [overrides/]", prefixes)
	}
	if len(cfFiles) != 1 || cfFiles[0].ProjectID != 1 || cfFiles[0].FileID != 2 {
		t.Fatalf("cfFiles: got %+v", cfFiles)
	}
	if len(tasks) != 0 {
		t.Fatalf("tasks для CF плануються лише після резолву API, got %d", len(tasks))
	}
}

// TestExtractOverridesUpdateProtectsPlayerFiles — БАГ: «Оновити з джерела»
// (UpdatePackFromSource, isUpdate=true) раніше перезаписувало config/ і
// options.txt БЕЗУМОВНО при кожному оновленні кастомної (Modrinth/
// CurseForge) збірки — гравець втрачав налаштування, змінені поверх
// дефолтів між оновленнями. Перевіряємо: якщо config/foo.cfg і
// options.txt вже на диску (гравець їх змінив) — при isUpdate=true вони
// НЕ перезаписуються новим вмістом з архіву, а звичайний mods/ файл
// оновлюється як завжди.
func TestExtractOverridesUpdateProtectsPlayerFiles(t *testing.T) {
	path := buildTestZip(t, map[string]string{
		"overrides/config/foo.cfg": "NEW-from-archive",
		"overrides/options.txt":    "NEW-from-archive",
		"overrides/mods/new.jar":   "NEW-mod-bytes",
	})
	gameDir := t.TempDir()

	// Гравець уже має свої версії цих файлів на диску (симулює стан
	// ПІСЛЯ попереднього встановлення + власні зміни).
	mustWrite(t, filepath.Join(gameDir, "config", "foo.cfg"), "player-edited-value")
	mustWrite(t, filepath.Join(gameDir, "options.txt"), "player-edited-value")

	skipped, err := extractOverrides(context.Background(), openZip(t, path), []string{"overrides/"}, gameDir, true, nil)
	if err != nil {
		t.Fatalf("extractOverrides: %v", err)
	}
	if len(skipped) != 2 {
		t.Fatalf("skipped: got %v, want 2 захищених шляхи (config/foo.cfg, options.txt)", skipped)
	}

	if got := readFile(t, filepath.Join(gameDir, "config", "foo.cfg")); got != "player-edited-value" {
		t.Fatalf("config/foo.cfg перезаписано при оновленні: got %q, хотіли зберегти зміни гравця", got)
	}
	if got := readFile(t, filepath.Join(gameDir, "options.txt")); got != "player-edited-value" {
		t.Fatalf("options.txt перезаписано при оновленні: got %q, хотіли зберегти зміни гравця", got)
	}
	// Звичайний файл (не в protectedOverridePaths) оновлюється як завжди.
	if got := readFile(t, filepath.Join(gameDir, "mods", "new.jar")); got != "NEW-mod-bytes" {
		t.Fatalf("mods/new.jar мав оновитись: got %q", got)
	}
}

// TestExtractOverridesUpdateInstallsMissingProtectedFiles — якщо
// захищеного файлу ЩЕ НЕМАЄ на диску навіть при isUpdate=true (гравець
// його видалив, або дуже стара версія збірки без цього файлу) —
// розпаковуємо як звичайний файл, а не пропускаємо назавжди.
func TestExtractOverridesUpdateInstallsMissingProtectedFiles(t *testing.T) {
	path := buildTestZip(t, map[string]string{
		"overrides/config/bar.cfg": "from-archive",
	})
	gameDir := t.TempDir()

	skipped, err := extractOverrides(context.Background(), openZip(t, path), []string{"overrides/"}, gameDir, true, nil)
	if err != nil {
		t.Fatalf("extractOverrides: %v", err)
	}
	if len(skipped) != 0 {
		t.Fatalf("skipped: got %v, хотіли 0 (файлу не було на диску)", skipped)
	}
	if got := readFile(t, filepath.Join(gameDir, "config", "bar.cfg")); got != "from-archive" {
		t.Fatalf("config/bar.cfg мав встановитись (був відсутній): got %q", got)
	}
}

// mustWrite створює файл (і батьківські теки) з заданим вмістом.
func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
