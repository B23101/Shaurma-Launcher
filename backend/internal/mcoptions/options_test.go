package mcoptions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shaurma-launcher-backend/internal/model"
)

func testDefaults() model.MinecraftDefaults {
	d := model.DefaultMinecraftDefaults()
	d.Enabled = true
	d.Language = "en_us"
	d.GuiScale = 3
	d.MaxFps = 144
	return d
}

// ApplyNow створює файл з нуля, якщо його немає.
func TestApplyNowCreatesFile(t *testing.T) {
	packDir := t.TempDir()
	d := testDefaults()
	if err := ApplyNow(packDir, d); err != nil {
		t.Fatalf("ApplyNow: %v", err)
	}
	m, err := ParseFile(OptionsFilePath(packDir))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if m["lang"] != "en_us" || m["guiScale"] != "3" || m["maxFps"] != "144" {
		t.Fatalf("дефолти не записані: %v", m)
	}
}

// ApplyNow перезаписує лише «видимі» опції, решту файла не чіпає.
func TestApplyNowKeepsOtherKeys(t *testing.T) {
	packDir := t.TempDir()
	os.MkdirAll(filepath.Join(packDir, ".minecraft"), 0755)
	// Чужий файл з опціями модів і видимою опцією.
	other := map[string]string{
		"lang":      "de_de",
		"mod_foo":   "true",
		"custommod": "42",
		"version":   "3465",
	}
	if err := WriteFile(OptionsFilePath(packDir), other); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	d := testDefaults()
	if err := ApplyNow(packDir, d); err != nil {
		t.Fatalf("ApplyNow: %v", err)
	}
	m, _ := ParseFile(OptionsFilePath(packDir))
	if m["lang"] != "en_us" {
		t.Fatalf("видима опція не перезаписана: lang=%q", m["lang"])
	}
	if m["mod_foo"] != "true" || m["custommod"] != "42" {
		t.Fatalf("чужі опції змінені: %v", m)
	}
	if m["version"] != "3465" {
		t.Fatalf("version змінений: %q", m["version"])
	}
}

// ApplyNow з вимкненою функцією нічого не робить.
func TestApplyNowDisabled(t *testing.T) {
	packDir := t.TempDir()
	d := model.DefaultMinecraftDefaults() // Enabled=false
	if err := ApplyNow(packDir, d); err != nil {
		t.Fatalf("ApplyNow: %v", err)
	}
	if _, err := os.Stat(OptionsFilePath(packDir)); !os.IsNotExist(err) {
		t.Fatalf("файл створено при вимкненій функції")
	}
}

// ApplyBeforeLaunch: якщо файл вже існує (незалежно від вмісту) — домальовує
// «видимі» дефолтні опції поверх, не чіпаючи чужі ключі. Одноразовість
// застосування — відповідальність ВИКЛИКАЧА (App.applyMinecraftDefaultsBeforeLaunch
// звіряється з instance.json.MinecraftDefaultsApplied ПЕРЕД цим викликом);
// сама функція такого стану не знає і маркерів у файлі не лишає.
func TestApplyBeforeLaunchAppliesToExistingFile(t *testing.T) {
	packDir := t.TempDir()
	os.MkdirAll(filepath.Join(packDir, ".minecraft"), 0755)
	if err := WriteFile(OptionsFilePath(packDir), map[string]string{
		"lang":         "de_de",
		"custom_mod_x": "1",
	}); err != nil {
		t.Fatal(err)
	}
	ApplyBeforeLaunch(packDir, testDefaults())
	m, _ := ParseFile(OptionsFilePath(packDir))
	if m["lang"] != "en_us" {
		t.Fatalf("дефолти не застосовані: lang=%q", m["lang"])
	}
	if m["custom_mod_x"] != "1" {
		t.Fatalf("чужі опції змінені")
	}
}

// ApplyBeforeLaunch: файла немає — створюється з дефолтів.
func TestApplyBeforeLaunchCreatesFile(t *testing.T) {
	packDir := t.TempDir()
	ApplyBeforeLaunch(packDir, testDefaults())
	m, err := ParseFile(OptionsFilePath(packDir))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if m["lang"] != "en_us" {
		t.Fatalf("файл не створено з дефолтів: %v", m)
	}
}

// ApplyBeforeLaunch з вимкненою функцією нічого не робить.
func TestApplyBeforeLaunchDisabled(t *testing.T) {
	packDir := t.TempDir()
	os.MkdirAll(filepath.Join(packDir, ".minecraft"), 0755)
	if err := WriteFile(OptionsFilePath(packDir), map[string]string{"lang": "de_de"}); err != nil {
		t.Fatal(err)
	}
	d := model.DefaultMinecraftDefaults() // Enabled=false
	ApplyBeforeLaunch(packDir, d)
	m, _ := ParseFile(OptionsFilePath(packDir))
	if m["lang"] != "de_de" {
		t.Fatalf("файл змінено при вимкненій функції: %v", m)
	}
}

// CopyVisible копіює лише видимі опції між збірками.
func TestCopyVisible(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	os.MkdirAll(filepath.Join(src, ".minecraft"), 0755)
	os.MkdirAll(filepath.Join(dst, ".minecraft"), 0755)
	if err := WriteFile(OptionsFilePath(src), map[string]string{
		"lang":     "en_us",
		"maxFps":   "260",
		"mod_uniq": "1",
	}); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(OptionsFilePath(dst), map[string]string{
		"lang":   "de_de",
		"mod_dst": "keep",
	}); err != nil {
		t.Fatal(err)
	}
	if err := CopyVisible(src, dst); err != nil {
		t.Fatalf("CopyVisible: %v", err)
	}
	m, _ := ParseFile(OptionsFilePath(dst))
	if m["lang"] != "en_us" || m["maxFps"] != "260" {
		t.Fatalf("видимі опції не скопійовані: %v", m)
	}
	if m["mod_dst"] != "keep" {
		t.Fatalf("чужі опції цільової збірки змінені")
	}
}

// GetVisibleOptions повертає лише присутні видимі опції.
func TestGetVisibleOptions(t *testing.T) {
	packDir := t.TempDir()
	os.MkdirAll(filepath.Join(packDir, ".minecraft"), 0755)
	if err := WriteFile(OptionsFilePath(packDir), map[string]string{
		"lang":   "en_us",
		"mod_zz": "hidden",
	}); err != nil {
		t.Fatal(err)
	}
	got, err := GetVisibleOptions(packDir)
	if err != nil {
		t.Fatalf("GetVisibleOptions: %v", err)
	}
	if len(got) != 1 || got["lang"] != "en_us" {
		t.Fatalf("got = %v, want лише lang", got)
	}
}

// SetVisibleOptions редагує видимі опції, не чіпаючи чужі ключі.
func TestSetVisibleOptions(t *testing.T) {
	packDir := t.TempDir()
	os.MkdirAll(filepath.Join(packDir, ".minecraft"), 0755)
	if err := WriteFile(OptionsFilePath(packDir), map[string]string{
		"lang": "de_de", "mod_x": "1",
	}); err != nil {
		t.Fatal(err)
	}
	err := SetVisibleOptions(packDir, map[string]string{"lang": "en_us", "maxFps": "144", "bogus_key": "x"})
	if err != nil {
		t.Fatalf("SetVisibleOptions: %v", err)
	}
	m, _ := ParseFile(OptionsFilePath(packDir))
	if m["lang"] != "en_us" || m["maxFps"] != "144" {
		t.Fatalf("видимі опції не записані: %v", m)
	}
	if _, ok := m["bogus_key"]; ok {
		t.Fatalf("невидимий ключ записаний")
	}
	if m["mod_x"] != "1" {
		t.Fatalf("чужі опції змінені")
	}
}

// ImportFromPack читає видимі опції у MinecraftDefaults.
func TestImportFromPack(t *testing.T) {
	packDir := t.TempDir()
	os.MkdirAll(filepath.Join(packDir, ".minecraft"), 0755)
	if err := WriteFile(OptionsFilePath(packDir), map[string]string{
		"lang":                  "en_gb",
		"guiScale":              "2",
		"soundCategory_music":   "0.5",
		"key_key.forward":       "key.keyboard.up",
		"fullscreen":            "true",
		"mouseSensitivity":      "0.75",
		"mod_zz":                "hidden",
	}); err != nil {
		t.Fatal(err)
	}
	d, err := ImportFromPack(packDir)
	if err != nil {
		t.Fatalf("ImportFromPack: %v", err)
	}
	if d.Language != "en_gb" || d.GuiScale != 2 || !d.Fullscreen {
		t.Fatalf("imported = %+v", d)
	}
	if d.SoundLevels["music"] != 0.5 {
		t.Fatalf("звук не імпортований: %v", d.SoundLevels)
	}
	if d.KeyBindings["key.forward"] != "key.keyboard.up" {
		t.Fatalf("бінд не імпортований: %v", d.KeyBindings)
	}
	if d.MouseSensitivity != 0.75 {
		t.Fatalf("чутливість = %v", d.MouseSensitivity)
	}
}

// formatFloat пише "1.0" для цілих значень (як старий лаунчер).
func TestFormatFloat(t *testing.T) {
	cases := map[float64]string{1.0: "1.0", 0.0: "0.0", 0.5: "0.5", 1.25: "1.25"}
	for in, want := range cases {
		if got := formatFloat(in); got != want {
			t.Fatalf("formatFloat(%v) = %q, want %q", in, got, want)
		}
	}
}

// Roundtrip через VisibleKeys: всі ключі у options.txt — формат key:value.
func TestWriteFileRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "options.txt")
	m := map[string]string{"lang": "uk_ua", "version": "3465"}
	if err := WriteFile(path, m); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(raw), "lang:uk_ua\n") {
		t.Fatalf("формат файла: %q", string(raw))
	}
}
