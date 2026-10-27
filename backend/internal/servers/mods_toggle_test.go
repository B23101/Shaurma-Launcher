package servers

import (
	"os"
	"path/filepath"
	"testing"
)

// Регресія: toggle ВИМКНЕНОГО мода додавав ще один суфікс ".disabled".
//
// Бекенд віддає FileName вимкненого файлу ЯК Є ("X.jar.disabled" — реальне
// ім'я на диску), фронтенд передає його ж назад у ToggleServerMod. Стара
// реалізація статила цей шлях, бачила існуючий файл, вважала його
// увімкненим і перейменовувала у "X.jar.disabled.disabled" — після чого мод
// неможливо було ні увімкнути, ні вимкнути, і UI показував його як
// "X.jar.disabled".
func TestToggleServerMod_DisabledFileNameDoesNotDoubleSuffix(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "mods", "example.jar.disabled"))

	if err := ToggleServerMod(dir, "example.jar.disabled"); err != nil {
		t.Fatalf("toggle: %v", err)
	}
	if !testFileExists(filepath.Join(dir, "mods", "example.jar")) {
		t.Fatal("після toggle вимкнений мод має стати увімкненим (example.jar)")
	}
	if testFileExists(filepath.Join(dir, "mods", "example.jar.disabled.disabled")) {
		t.Fatal("з'явився подвійний суфікс .disabled — це і був баг")
	}

	// І назад — вимкнути (тепер фронтенд передає "example.jar").
	if err := ToggleServerMod(dir, "example.jar"); err != nil {
		t.Fatalf("toggle back: %v", err)
	}
	if !testFileExists(filepath.Join(dir, "mods", "example.jar.disabled")) {
		t.Fatal("мод має стати вимкненим")
	}
	if testFileExists(filepath.Join(dir, "mods", "example.jar")) {
		t.Fatal("увімкнений файл не має лишатись")
	}
}

// Toggle має бути ідемпотентним щодо реального стану: якщо передати ім'я
// увімкненого файлу двічі — другий виклик нічого не змінює.
func TestToggleServerMod_IsIdempotentPerFileName(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "mods", "a.jar"))

	if err := ToggleServerMod(dir, "a.jar"); err != nil {
		t.Fatalf("1: %v", err)
	}
	if !testFileExists(filepath.Join(dir, "mods", "a.jar.disabled")) {
		t.Fatal("має стати вимкненим")
	}
	// Другий виклик із тим самим іменем "a.jar" (файлу a.jar уже немає) —
	// тепер це означає «увімкнути» (стан на диску), тому файл повернеться.
	if err := ToggleServerMod(dir, "a.jar"); err != nil {
		t.Fatalf("2: %v", err)
	}
	if testFileExists(filepath.Join(dir, "mods", "a.jar.disabled")) {
		t.Fatal("після другого toggle файл має бути увімкнений")
	}
	// Плагіни мають ту саму логіку — той самий спільний хелпер.
	mustWrite(t, filepath.Join(dir, "plugins", "a.jar"))
	if err := ToggleServerPlugin(dir, "a.jar"); err != nil {
		t.Fatalf("plugins toggle: %v", err)
	}
	if !testFileExists(filepath.Join(dir, "plugins", "a.jar.disabled")) {
		t.Fatal("плагін має вимкнутись у plugins/")
	}
}

// Файли, уже зіпсовані старою версією ("X.jar.disabled.disabled"), мають
// лікуватись при першому ж перемиканні.
func TestToggleServerMod_HealsDoubleDisabled(t *testing.T) {
	dir := t.TempDir()
	broken := filepath.Join(dir, "mods", "broken.jar.disabled.disabled")
	mustWrite(t, broken)

	if err := ToggleServerMod(dir, "broken.jar.disabled"); err != nil {
		t.Fatalf("toggle: %v", err)
	}
	if !testFileExists(filepath.Join(dir, "mods", "broken.jar")) {
		t.Fatal("зіпсований файл має стати увімкненим broken.jar")
	}
	if testFileExists(broken) {
		t.Fatal("подвійний суфікс має зникнути")
	}
}

// Видалення прибирає всі варіанти імені — раніше з теки лишався
// «видалений» мод під другим іменем.
func TestDeleteServerMod_RemovesAllNameVariants(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "mods", "dup.jar"))
	mustWrite(t, filepath.Join(dir, "mods", "dup.jar.disabled"))

	if err := DeleteServerMod(dir, "dup.jar.disabled"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if testFileExists(filepath.Join(dir, "mods", "dup.jar")) || testFileExists(filepath.Join(dir, "mods", "dup.jar.disabled")) {
		t.Fatal("мають зникнути обидва варіанти імені")
	}

	// Неіснуючий файл — ос.ErrNotExist, а не тихий успіх.
	if err := DeleteServerMod(dir, "nope.jar"); !os.IsNotExist(err) {
		t.Fatalf("очікували ErrNotExist, отримали %v", err)
	}
}

func TestSplitServerFileName(t *testing.T) {
	cases := []struct {
		in       string
		base     string
		disabled bool
	}{
		{"a.jar", "a.jar", false},
		{"a.jar.disabled", "a.jar", true},
		{"a.jar.disabled.disabled", "a.jar", true},
		{"weird.name.jar", "weird.name.jar", false},
	}
	for _, c := range cases {
		base, disabled := splitServerFileName(c.in)
		if base != c.base || disabled != c.disabled {
			t.Errorf("splitServerFileName(%q) = (%q, %v), очікували (%q, %v)", c.in, base, disabled, c.base, c.disabled)
		}
	}
}

func mustWrite(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("jar"), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// testFileExists — локальний хелпер тесту (detect.go має власну fileExists,
// тому ім'я інше, щоб не конфліктувало в одному пакеті).
func testFileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
