// mods.go — менеджер модів Minecraft-сервера (mods/ папка).
//
// ПОВНЕ сканування через internal/mods.Manager.ScanModsFull — той самий
// парсер jar-метаданих (fabric.mod.json/quilt.mod.json/mods.toml/
// neoforge.mods.toml/mcmod.info), що й менеджер модів збірки: реальна
// назва/версія/автор/іконка з самого jar (а не лише ім'я файлу), SHA1
// для точного матчингу на Modrinth, і Environment ("client"/"server"/
// "*") для позначення клієнтських модів, які на сервері не мають
// жодного ефекту. Раніше тут була лише проста тека-список (listJarFiles)
// без жодних метаданих — картки модів на сторінці сервера не показували
// ні іконок, ні версій з маніфесту, ні попередження "лише клієнтський".
package servers

import (
	"os"
	"path/filepath"
	"strings"

	"shaurma-launcher-backend/internal/model"
	"shaurma-launcher-backend/internal/mods"
)

// modsManager — один спільний екземпляр на весь процес: readJarCached
// усередині нього кешує метадані jar за (шлях, розмір, mtime), тож
// повторні сканування (перемикання вкладок, оновлення списку) не
// перечитують ZIP для файлів, що не змінювались.
var modsManager = mods.NewManager()

// ServerModInfo лишається для сумісності зі старим форматом плагінів
// (plugins.go) — bukkit-плагіни не мають структурованих метаданих типу
// fabric.mod.json, тож для них повний jar-сканер не застосовний.
type ServerModInfo struct {
	Name     string  `json:"name"`
	Version  string  `json:"version"`
	FileName string  `json:"fileName"`
	Enabled  bool    `json:"enabled"` // .disabled → false
	SizeMB   float64 `json:"sizeMB"`
	Source   string  `json:"source"` // "modrinth", "curseforge", "local"
	// SourceKind/SourcePath — файл походить з «єдиного джерела»
	// (modsource): "file" | "folder" і шлях. Такий файл не можна
	// вимкнути/видалити вручну.
	SourceKind    string `json:"sourceKind,omitempty"`
	SourcePath    string `json:"sourcePath,omitempty"`
	SourceMissing bool   `json:"sourceMissing,omitempty"`
}

// ListServerMods повертає ПОВНИЙ список модів з папки mods/ сервера:
// реальні метадані з jar (назва/версія/автор/іконка/SHA1/Environment),
// той самий формат model.ModEntry, що використовує менеджер модів
// збірки. HasUpdate/LatestVersion тут ЗАВЖДИ порожні — перевірка
// оновлень мережева і робиться окремим викликом (CheckServerModUpdates),
// щоб відкриття вкладки не блокувалось на мережі.
func ListServerMods(serverDir string) []model.ModEntry {
	modsDir := filepath.Join(serverDir, "mods")
	entries, err := modsManager.ScanModsFull(modsDir)
	if err != nil {
		return []model.ModEntry{}
	}
	return entries
}

// ToggleServerMod вмикає/вимикає мод (перейменовує .jar ↔ .jar.disabled).
func ToggleServerMod(serverDir, fileName string) error {
	return toggleServerFile(filepath.Join(serverDir, "mods"), fileName)
}

// DeleteServerMod видаляє мод (разом із .disabled-двійником, якщо він є).
func DeleteServerMod(serverDir, fileName string) error {
	return deleteServerFile(filepath.Join(serverDir, "mods"), fileName)
}

// ── Спільна робота з файлами content-тек (моди й плагіни) ─────────────
//
// БАГ, який тут виправлено (скарга «у вимкненого мода не перевіряється
// версія/іконка, а toggle додає ще один .disable»):
// ToggleServerMod/ToggleServerPlugin статили шлях `fileName` як є. Для
// ВИМКНЕНОГО файлу бекенд віддає FileName = "X.jar.disabled" (це реальне
// ім'я на диску), а фронтенд передає його ж назад. `os.Stat("X.jar.disabled")`
// успішний → код вважав файл УВІМКНЕНИМ і перейменовував його в
// "X.jar.disabled.disabled". Далі сканер бачив лише один хвостовий суфікс
// (ім'я ставало "X.jar.disabled"), і мод лишався вимкненим назавжди —
// ні увімкнути, ні вимкнути його було неможливо.
//
// Тепер стан визначається за РЕАЛЬНИМ станом на диску (яке з двох імен
// існує), а всі хвостові ".disabled" складаються до одного — тому toggle
// коректний для будь-якого варіанту імені й ДОДАТКОВО лікує вже зіпсовані
// файли "X.jar.disabled.disabled" при першому ж перемиканні.

// splitServerFileName розбирає ім'я на канонічну базу ("X.jar") і ознаку
// вимкненості, знімаючи ВСІ хвостові ".disabled".
func splitServerFileName(fileName string) (base string, disabled bool) {
	base = fileName
	for strings.HasSuffix(base, ".disabled") {
		base = strings.TrimSuffix(base, ".disabled")
		disabled = true
	}
	return base, disabled
}

// canonicalServerFile повертає ім'я, яке реально лежить у теці для цієї
// бази: "X.jar", "X.jar.disabled" або (пошкоджене) "X.jar.disabled.disabled".
func canonicalServerFile(dir, base string) (name string, enabled bool, ok bool) {
	candidates := []struct {
		name    string
		enabled bool
	}{
		{base, true},
		{base + ".disabled", false},
	}
	for _, c := range candidates {
		if _, err := os.Stat(filepath.Join(dir, c.name)); err == nil {
			return c.name, c.enabled, true
		}
	}
	// Пошкоджене ім'я з дубльованим суфіксом.
	for _, tail := range []string{".disabled.disabled", ".disabled.disabled.disabled"} {
		if _, err := os.Stat(filepath.Join(dir, base+tail)); err == nil {
			return base + tail, false, true
		}
	}
	return "", false, false
}

// toggleServerFile перемикає стан файла в теці (увімкнено ↔ вимкнено),
// керуючись реальним станом на диску, а не тим, яке ім'я передав виклик.
func toggleServerFile(dir, fileName string) error {
	base, _ := splitServerFileName(fileName)
	cur, enabled, ok := canonicalServerFile(dir, base)
	if !ok {
		return os.ErrNotExist
	}
	target := base
	if enabled {
		target = base + ".disabled"
	}
	if target == cur {
		return nil
	}
	if _, err := os.Stat(filepath.Join(dir, target)); err == nil {
		// Обидва імені існують (напр. після ручних експериментів):
		// прибираємо поточне, лишаємо канонічне цільове.
		return os.Remove(filepath.Join(dir, cur))
	}
	return os.Rename(filepath.Join(dir, cur), filepath.Join(dir, target))
}

// deleteServerFile видаляє файл і всі його варіанти іменування
// ("X.jar", "X.jar.disabled", "X.jar.disabled.disabled") — раніше
// видалявся лише один із них, і «видалений» мод лишався в теці другим
// іменем.
func deleteServerFile(dir, fileName string) error {
	base, _ := splitServerFileName(fileName)
	candidates := []string{
		base,
		base + ".disabled",
		base + ".disabled.disabled",
		base + ".disabled.disabled.disabled",
	}
	removed := false
	var firstErr error
	for _, name := range candidates {
		err := os.Remove(filepath.Join(dir, name))
		switch {
		case err == nil:
			removed = true
		case os.IsNotExist(err):
			// нема такого імені — нормально
		default:
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	if firstErr != nil {
		return firstErr
	}
	if !removed {
		return os.ErrNotExist
	}
	return nil
}

// MigrateModsFromPack копіює моди зі збірки на сервер.
func MigrateModsFromPack(packModsDir, serverModsDir string, clientOnlyFilter bool) (migrated, skipped []string, err error) {
	if err := os.MkdirAll(serverModsDir, 0755); err != nil {
		return nil, nil, err
	}

	entries, err := os.ReadDir(packModsDir)
	if err != nil {
		return nil, nil, err
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".jar") {
			continue
		}

		src := filepath.Join(packModsDir, e.Name())
		dst := filepath.Join(serverModsDir, e.Name())

		// Якщо файл вже є — пропускаємо.
		if _, err := os.Stat(dst); err == nil {
			skipped = append(skipped, e.Name())
			continue
		}

		if err := copyFile(src, dst); err != nil {
			skipped = append(skipped, e.Name())
			continue
		}
		migrated = append(migrated, e.Name())
	}

	return migrated, skipped, nil
}

// normalizeDisabledNames згортає дубльовані хвостові ".disabled" у канонічне
// ОДНЕ ("X.jar.disabled.disabled" → "X.jar.disabled"). Для модів це вже
// робить mods.migrateLegacyDisabled, але плагіни (plugins/) не проходять
// через internal/mods.Manager — раніше зіпсований багаторазовим toggle
// плагін лишався з іменем "X.jar.disabled.disabled", і у списку було видно
// «подвійний .disable». Ідемпотентно і безпечно: якщо канонічне ім'я вже
// зайняте — не чіпаємо.
func normalizeDisabledNames(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".disabled.disabled") {
			continue
		}
		base := name
		for strings.HasSuffix(base, ".disabled") {
			base = strings.TrimSuffix(base, ".disabled")
		}
		target := base + ".disabled"
		if _, err := os.Stat(filepath.Join(dir, target)); err == nil {
			continue
		}
		_ = os.Rename(filepath.Join(dir, name), filepath.Join(dir, target))
	}
}

// listJarFiles — легкий сканер без метаданих jar, для plugins/ (bukkit
// plugin.yml — інший формат, не читається internal/mods.Manager).
func listJarFiles(dir string) []ServerModInfo {
	var out []ServerModInfo

	// Спершу лікуємо вже зіпсовані подвійні суфікси — щоб у списку не
	// було видно «X.jar.disabled.disabled».
	normalizeDisabledNames(dir)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		lower := strings.ToLower(name)
		if !strings.HasSuffix(lower, ".jar") && !strings.HasSuffix(lower, ".jar.disabled") {
			continue
		}

		// splitServerFileName знімає ВСІ хвостові ".disabled", а не один:
		// так Name ніколи не містить залишкового суфікса (раніше
		// "X.jar.disabled" лишався в імені для багаторазово вимкненого файлу).
		base, disabled := splitServerFileName(name)

		info, _ := e.Info()
		sizeMB := float64(0)
		if info != nil {
			sizeMB = float64(info.Size()) / (1024 * 1024)
		}

		out = append(out, ServerModInfo{
			Name:     strings.TrimSuffix(base, ".jar"),
			FileName: name,
			Enabled:  !disabled,
			SizeMB:   sizeMB,
		})
	}

	return out
}
