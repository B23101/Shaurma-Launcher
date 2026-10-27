package launchermigrate

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ── Prism Launcher ───────────────────────────────────────────────────────
// Дані Prism лежать у теці даних (AppData), за замовчуванням
// %APPDATA%/PrismLauncher на Windows. Інстанси — у <dataDir>/instances/<name>/,
// де кожен інстанс містить:
//   - mmc-pack.json — компоненти профілю (net.minecraft, fabric/forge/...);
//   - instance.cfg   — INI з назвою, іконкою, Java, пам'яттю, аргументами;
//   - .minecraft/    — тека гри (або minecraft/ для імпортованих модпаків).
// Іконки інстансів — у <dataDir>/icons/<iconKey> (PNG/SVG).
//
// Джерело: PrismLauncher-develop/launcher (PackProfile.cpp, BaseInstance.cpp,
// MinecraftInstance.cpp) — тут відтворюємо ті самі імена файлів і формати.

// mmcComponent — один компонент у mmc-pack.json (uid + version).
type mmcComponent struct {
	UID     string `json:"uid"`
	Version string `json:"version"`
}

type mmcPackJSON struct {
	FormatVersion int            `json:"formatVersion"`
	Components    []mmcComponent `json:"components"`
}

// prismInstanceCfg — розібраний instance.cfg (INI). Prism пише [General]-секцію
// з назвою/іконкою/Java/пам'яттю/аргументами.
type prismInstanceCfg struct {
	Name          string
	IconKey       string
	JavaPath      string
	JvmArgs       string
	MaxMemAllocMB int
	MinMemAllocMB int
	WindowWidth   int
	WindowHeight  int
}

// parseINI — мінімалістичний INI-парсер (секції [..], key=value, коментарі
// # і ;). Достатньо для instance.cfg Prism і prismlauncher.cfg. Ключі
// читаються без урахування регістру — Prism пише General/IconKey/JvmArgs.
func parseINI(data string) map[string]string {
	out := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(data))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "[") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])
		// Prism не екранує лапки в значеннях (JavaPath з пробілами пишеться
		// як є) — лишаємо значення без змін.
		out[strings.ToLower(key)] = val
	}
	return out
}

// parsePrismInstanceCfg розбирає instance.cfg інстансу Prism.
func parsePrismInstanceCfg(data string) prismInstanceCfg {
	kv := parseINI(data)
	var cfg prismInstanceCfg
	cfg.Name = kv["name"]
	cfg.IconKey = kv["iconkey"]
	cfg.JavaPath = kv["javapath"]
	cfg.JvmArgs = kv["jvmargs"]
	cfg.MaxMemAllocMB = atoiSafe(kv["maxmemalloc"])
	cfg.MinMemAllocMB = atoiSafe(kv["minmemalloc"])
	cfg.WindowWidth = atoiSafe(kv["windowwidth"])
	cfg.WindowHeight = atoiSafe(kv["windowheight"])
	return cfg
}

func atoiSafe(s string) int {
	if s == "" {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

// prismDataDir визначає теку даних Prism Launcher ТАК САМО, як це робить
// сам Prism (Application.cpp, порядок пріоритету):
//   1. env PRISMLAUNCHER_DATA_DIR (той самий <NAME>_DATA_DIR, що Prism
//      читає через QProcessEnvironment — на Windows NAME=PrismLauncher);
//   2. portable-режим: поруч з exe лежить тека UserData/ або файл
//      portable.txt (exe шукається через ярлики в Робочому столі та меню
//      «Пуск» — обмежені стандартні теки Windows, жодного обходу дисків);
//   3. дефолт %APPDATA%/PrismLauncher.
// Повертає "" лише якщо APPDATA недоступний.
func prismDataDir() string {
	if env := os.Getenv("PRISMLAUNCHER_DATA_DIR"); env != "" {
		return env
	}
	if dir := prismPortableDataDir(); dir != "" {
		return dir
	}
	if appdata := os.Getenv("APPDATA"); appdata != "" {
		return filepath.Join(appdata, "PrismLauncher")
	}
	return ""
}

// prismPortableDataDir шукає portable-установку Prism: ярлики на
// PrismLauncher.exe у Робочому столі та меню «Пуск» (обмежені теки) →
// шлях до exe → поруч UserData/ або portable.txt. Повертає "", якщо
// portable-режим не виявлено. .lnk читаються як текст (шлях до цілі
// зберігається в тілі файлу рядком) — ніякого повного парсингу формату.
func prismPortableDataDir() string {
	exeDirs := map[string]bool{}
	for _, dir := range knownShortcutDirs() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".lnk") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil || len(data) > 1<<20 {
				continue
			}
			// Шлях до PrismLauncher.exe в тілі .lnk (юнікод-рядок). Шукаємо
			// ім'я файлу без урахування регістру, потім відступаємо назад до
			// початку ПОВНОГО шляху (дисковий префікс "C:\" — сам шлях містить
			// багато розділювачів, тож зупиняємось на "x:\"). Fallback — до
			// початку рядка шляху (нульовий/керуючий байт до нього).
			s := strings.ToLower(string(data))
			idx := strings.Index(s, "prismlauncher.exe")
			if idx < 0 {
				continue
			}
			start := idx
			for start > 0 {
				if start >= 2 && s[start-1] == ':' && s[start] == '\\' {
					if (s[start-2] >= 'a' && s[start-2] <= 'z') || (s[start-2] >= '0' && s[start-2] <= '9') {
						start -= 2
						break
					}
				}
				if s[start-1] < 0x20 || s[start-1] == 0x7f {
					break // кінець попереднього блоку даних .lnk
				}
				start--
			}
			exePath := string(data[start : idx+len("PrismLauncher.exe")])
			if strings.ContainsAny(exePath, ":\\/") {
				exeDirs[filepath.Dir(exePath)] = true
			}
		}
	}
	// Portable-маркери біля exe: UserData/ (нова схема) або portable.txt.
	for exeDir := range exeDirs {
		if dirExists(filepath.Join(exeDir, "UserData")) {
			return filepath.Join(exeDir, "UserData")
		}
		if fileExists(filepath.Join(exeDir, "portable.txt")) {
			return exeDir
		}
	}
	return ""
}

// knownShortcutDirs — стандартні теки Windows з ярликами (Робочий стіл +
// меню «Пуск» користувача). Обмежений список — жодного обходу дисків.
func knownShortcutDirs() []string {
	var out []string
	if appdata := os.Getenv("APPDATA"); appdata != "" {
		// %APPDATA%\Microsoft\Windows\Start Menu\Programs — меню «Пуск».
		out = append(out, filepath.Join(appdata, "Microsoft", "Windows", "Start Menu", "Programs"))
	}
	if userprofile := os.Getenv("USERPROFILE"); userprofile != "" {
		out = append(out, filepath.Join(userprofile, "Desktop"))
		// OneDrive-десктоп (шлях може бути перенесений).
		out = append(out, filepath.Join(userprofile, "OneDrive", "Desktop"))
	}
	return out
}

// scanPrism сканує Prism Launcher у стандартній теці даних. Повертає
// Launcher з Found=false, якщо теки даних немає.
func scanPrism() Launcher {
	l := Launcher{Kind: LauncherPrism, Name: "Prism Launcher"}
	dir := prismDataDir()
	if dir == "" || !dirExists(dir) {
		l.DataDir = dir
		return l
	}
	l.DataDir = dir
	l.Found = true

	instancesDir := filepath.Join(dir, "instances")
	entries, err := os.ReadDir(instancesDir)
	if err != nil {
		return l
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		instDir := filepath.Join(instancesDir, e.Name())
		// Валідний інстанс Prism = mmc-pack.json + instance.cfg (як у
		// Modrinth App import_mmc::is_valid_mmc — той самий критерій).
		packPath := filepath.Join(instDir, "mmc-pack.json")
		cfgPath := filepath.Join(instDir, "instance.cfg")
		packData, err := os.ReadFile(packPath)
		if err != nil {
			continue
		}
		cfgData, err := os.ReadFile(cfgPath)
		if err != nil {
			continue
		}
		var mj mmcPackJSON
		if json.Unmarshal(packData, &mj) != nil || len(mj.Components) == 0 {
			continue
		}
		cfg := parsePrismInstanceCfg(string(cfgData))

		// Тека гри: .minecraft (звичайний інстанс) або minecraft (деякі
		// імпортовані модпаки) — той самий фолбек, що в Modrinth App
		// (import_mmc: minecraft, потім .minecraft).
		gameDir := filepath.Join(instDir, ".minecraft")
		if !dirExists(gameDir) {
			alt := filepath.Join(instDir, "minecraft")
			if dirExists(alt) {
				gameDir = alt
			} else {
				continue
			}
		}

		p := Pack{
			Kind:       LauncherPrism,
			ID:         e.Name(),
			Name:       firstNonEmpty(cfg.Name, e.Name()),
			GameDir:    gameDir,
			JavaPath:   cfg.JavaPath,
			JvmArgs:    cfg.JvmArgs,
			MaxRAMMB:   cfg.MaxMemAllocMB,
			MinRAMMB:   cfg.MinMemAllocMB,
			HasConfig:  cfg.JavaPath != "" || cfg.JvmArgs != "" || cfg.MaxMemAllocMB > 0 || cfg.MinMemAllocMB > 0,
			SizeBytes:  dirSize(gameDir),
			Loader:     "vanilla",
			MCVersion:  "",
			IconPath:   "",
		}
		// Компоненти → версія Minecraft + лоадер.
		for _, c := range mj.Components {
			switch c.UID {
			case "net.minecraft":
				p.MCVersion = c.Version
			case "net.fabricmc.fabric-loader":
				p.Loader, p.LoaderVersion = "fabric", c.Version
			case "net.minecraftforge":
				p.Loader, p.LoaderVersion = "forge", c.Version
			case "net.neoforged":
				p.Loader, p.LoaderVersion = "neoforge", c.Version
			case "org.quiltmc.quilt-loader":
				p.Loader, p.LoaderVersion = "quilt", c.Version
			}
		}
		// Іконка: <dataDir>/icons/<iconKey>. iconKey може бути без розширення
		// (Prism зберігає як є — PNG або SVG); беремо файл якщо він PNG.
		if cfg.IconKey != "" {
			if icon := findPrismIcon(dir, cfg.IconKey); icon != "" {
				p.IconPath = icon
			}
		}
		l.Packs = append(l.Packs, p)
	}
	return l
}

// findPrismIcon шукає файл іконки за iconKey у теці icons/ даних Prism.
// Повертає шлях, лише якщо це PNG (JPG теж конвертується — приймаємо
// .png/.jpg/.jpeg/.webp; SVG ігноруємо — картка лаунчера читає растери).
func findPrismIcon(dataDir, iconKey string) string {
	if iconKey == "" {
		return ""
	}
	iconsDir := filepath.Join(dataDir, "icons")
	for _, ext := range []string{".png", ".jpg", ".jpeg", ".webp"} {
		cand := filepath.Join(iconsDir, iconKey+ext)
		if fileExists(cand) {
			return cand
		}
	}
	// IconKey може вже містити розширення.
	if strings.Contains(iconKey, ".") {
		cand := filepath.Join(iconsDir, iconKey)
		if fileExists(cand) && strings.HasPrefix(strings.ToLower(cand), strings.ToLower(filepath.Join(iconsDir, iconKey))) {
			return cand
		}
	}
	return ""
}
