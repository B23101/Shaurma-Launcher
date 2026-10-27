package mcoptions

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"shaurma-launcher-backend/internal/model"
)

// OptionsFilePath повертає шлях options.txt збірки (packDir — тека збірки,
// гривня; options.txt живе у підтеці .minecraft).
func OptionsFilePath(packDir string) string {
	return filepath.Join(packDir, ".minecraft", "options.txt")
}

// ParseFile читає options.txt у map<key, value>. Значення — все, що після
// ПЕРШОГО двокрапки у рядку (як у старого лаунчера). Файла немає → помилка
// os.ErrNotExist.
func ParseFile(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	m := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		if i := strings.IndexByte(line, ':'); i > 0 {
			key := strings.TrimSpace(line[:i])
			if key == "" {
				continue
			}
			m[key] = strings.TrimSpace(line[i+1:])
		}
	}
	return m, nil
}

// WriteFile записує options.txt у форматі key:value. Порядок ключів —
// стабільний (алфавітний), щоб файл не змінювався між записами.
func WriteFile(path string, m map[string]string) error {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteByte(':')
		b.WriteString(m[k])
		b.WriteByte('\n')
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(b.String()), 0644)
}

// ApplyBeforeLaunch застосовує дефолтні налаштування до options.txt збірки
// ПЕРЕД запуском гри — ОДНОРАЗОВО для життя збірки. Викликач (App.
// applyMinecraftDefaultsBeforeLaunch) відповідає за те, щоб покликати цю
// функцію лише коли ще не застосовувались (instance.json.
// MinecraftDefaultsApplied == false) — сама функція такого стану не знає
// і не зберігає (немає поняття packID на цьому рівні).
//
//  1. defaults.Enabled == false → нічого не робити.
//  2. options.txt не існує → створити з дефолтів.
//  3. options.txt існує → домалювати «видимі» дефолтні опції поверх (не
//     чіпаючи решту файла), як WriteVisibleDefaults.
func ApplyBeforeLaunch(packDir string, d model.MinecraftDefaults) {
	if !d.Enabled {
		return
	}
	optionsFile := OptionsFilePath(packDir)
	m, err := ParseFile(optionsFile)
	if os.IsNotExist(err) {
		createFromDefaults(optionsFile, d)
		return
	}
	if err != nil {
		return
	}
	fillFromDefaults(m, d)
	_ = WriteFile(optionsFile, m)
}

// ApplyNow застосовує дефолти до options.txt збірки ОДРАЗУ — викликається
// після скачування збірки Shaurma та імпорту mrpack/zip (лише при першому
// встановленні — викликач перевіряє instance.json.MinecraftDefaultsApplied
// ПЕРЕД викликом). Створює файл з нуля або перезаписує лише «видимі»
// опції у вже наявному, не чіпаючи решту файла.
func ApplyNow(packDir string, d model.MinecraftDefaults) error {
	if !d.Enabled {
		return nil
	}
	optionsFile := OptionsFilePath(packDir)
	m := map[string]string{}
	if _, err := os.Stat(optionsFile); err == nil {
		if parsed, perr := ParseFile(optionsFile); perr == nil {
			m = parsed
		}
	}
	if len(m) == 0 {
		m["version"] = "3465"
	}
	fillFromDefaults(m, d)
	return WriteFile(optionsFile, m)
}

// ImportFromPack читає «видимі» опції options.txt збірки у MinecraftDefaults
// (кнопка «Імпортувати з збірки» у налаштуваннях лаунчера).
func ImportFromPack(packDir string) (model.MinecraftDefaults, error) {
	m, err := ParseFile(OptionsFilePath(packDir))
	if err != nil {
		return model.MinecraftDefaults{}, err
	}
	d := model.DefaultMinecraftDefaults()
	mapToDefaults(m, &d)
	return d, nil
}

// CopyVisible копіює «видимі» опції options.txt з однієї збірки в іншу,
// не чіпаючи інші ключі цільового файла.
func CopyVisible(srcPackDir, dstPackDir string) error {
	src, err := ParseFile(OptionsFilePath(srcPackDir))
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("options.txt не знайдено у збірці-джерелі")
		}
		return err
	}
	dst := map[string]string{}
	if _, err := os.Stat(OptionsFilePath(dstPackDir)); err == nil {
		if parsed, perr := ParseFile(OptionsFilePath(dstPackDir)); perr == nil {
			dst = parsed
		}
	}
	for _, k := range VisibleKeys() {
		if v, ok := src[k]; ok {
			dst[k] = v
		}
	}
	return WriteFile(OptionsFilePath(dstPackDir), dst)
}

// GetVisibleOptions повертає з options.txt лише ті «видимі» опції, що
// реально є у файлі (для розділу «Гра» на сторінці збірки). Всі ключі
// прив'язок клавіш (key_*) включаються ЦІЛКОМ, а не лише ванільний
// список bindKeys() — так у розділ «Гра» і в «Бінди клавіш» потрапляють
// і кастомні бінди модів (Iris, JEI, Sodium тощо), які теж зберігаються
// у options.txt як key_<modkey.name>, у тому ж форматі, що ванільні.
func GetVisibleOptions(packDir string) (map[string]string, error) {
	m, err := ParseFile(OptionsFilePath(packDir))
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, k := range VisibleKeys() {
		if v, ok := m[k]; ok {
			out[k] = v
		}
	}
	for k, v := range m {
		if strings.HasPrefix(k, "key_") {
			out[k] = v
		}
	}
	return out, nil
}

// SetVisibleOptions перезаписує «видимі» опції у options.txt збірки (решта
// файла лишається без змін; якщо файла немає — створюється). Будь-який
// ключ key_* приймається (не лише ванільний bindKeys()) — щоб зберігались
// і кастомні бінди модів, надіслані з фронтенду.
func SetVisibleOptions(packDir string, options map[string]string) error {
	optionsFile := OptionsFilePath(packDir)
	m := map[string]string{}
	if _, err := os.Stat(optionsFile); err == nil {
		if parsed, perr := ParseFile(optionsFile); perr == nil {
			m = parsed
		}
	}
	if len(m) == 0 {
		m["version"] = "3465"
	}
	for k, v := range options {
		if _, ok := visibleKeySet[k]; ok || strings.HasPrefix(k, "key_") {
			m[k] = v
		}
	}
	return WriteFile(optionsFile, m)
}

// visibleKeySet — швидкий пошук «видимих» ключів для SetVisibleOptions.
var visibleKeySet = func() map[string]struct{} {
	s := map[string]struct{}{}
	for _, k := range VisibleKeys() {
		s[k] = struct{}{}
	}
	return s
}()

// ── Внутрішня логіка ────────────────────────────────────────────────────

func createFromDefaults(optionsFile string, d model.MinecraftDefaults) {
	m := map[string]string{"version": "3465"}
	fillFromDefaults(m, d)
	_ = WriteFile(optionsFile, m)
}

// fillFromDefaults записує у map всі «видимі» опції з MinecraftDefaults.
func fillFromDefaults(m map[string]string, d model.MinecraftDefaults) {
	if d.Language != "" {
		m["lang"] = d.Language
	}
	m["guiScale"] = strconv.Itoa(d.GuiScale)
	for cat, v := range d.SoundLevels {
		m["soundCategory_"+cat] = formatFloat(v)
	}
	for k, v := range d.KeyBindings {
		m["key_"+k] = v
	}
	m["fullscreen"] = strconv.FormatBool(d.Fullscreen)
	m["enableVsync"] = strconv.FormatBool(d.EnableVsync)
	m["maxFps"] = strconv.Itoa(d.MaxFps)
	m["autoJump"] = strconv.FormatBool(d.AutoJump)
	m["mouseSensitivity"] = formatFloat(d.MouseSensitivity)
	m["invertYMouse"] = strconv.FormatBool(d.InvertYMouse)
	m["fov"] = formatFloat(d.Fov)
	m["darkMojangStudiosBackground"] = strconv.FormatBool(d.DarkMojangStudiosBackground)
}

// mapToDefaults конвертує map з options.txt назад у MinecraftDefaults.
func mapToDefaults(m map[string]string, d *model.MinecraftDefaults) {
	if v, ok := m["lang"]; ok && v != "" {
		d.Language = v
	}
	if v, ok := m["guiScale"]; ok {
		d.GuiScale = parseInt(v, 0)
	}
	for cat := range d.SoundLevels {
		if v, ok := m["soundCategory_"+cat]; ok {
			d.SoundLevels[cat] = parseFloat(v, 1)
		}
	}
	for k, v := range m {
		if strings.HasPrefix(k, "key_") {
			d.KeyBindings[strings.TrimPrefix(k, "key_")] = v
		}
	}
	if v, ok := m["fullscreen"]; ok {
		d.Fullscreen = v == "true"
	}
	if v, ok := m["enableVsync"]; ok {
		d.EnableVsync = v == "true"
	}
	if v, ok := m["maxFps"]; ok {
		d.MaxFps = parseInt(v, d.MaxFps)
	}
	if v, ok := m["autoJump"]; ok {
		d.AutoJump = v == "true"
	}
	if v, ok := m["mouseSensitivity"]; ok {
		d.MouseSensitivity = parseFloat(v, d.MouseSensitivity)
	}
	if v, ok := m["invertYMouse"]; ok {
		d.InvertYMouse = v == "true"
	}
	if v, ok := m["fov"]; ok {
		d.Fov = parseFloat(v, d.Fov)
	}
	if v, ok := m["darkMojangStudiosBackground"]; ok {
		d.DarkMojangStudiosBackground = v == "true"
	}
}

// formatFloat пише цілі значення з ".0" (як старий лаунчер: 1.0, 0.0),
// дробові — звичайно (0.5, 1.25).
func formatFloat(v float64) string {
	if v == math.Floor(v) {
		return fmt.Sprintf("%d.0", int(v))
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func parseInt(s string, def int) int {
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return def
	}
	return v
}

func parseFloat(s string, def float64) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return def
	}
	return v
}
