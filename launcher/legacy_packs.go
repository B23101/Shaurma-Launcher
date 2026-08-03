package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"shaurma-launcher-wails/internal/builds"
	"shaurma-launcher-wails/internal/custompack"
)

// legacyCustomPack — запис custom-packs.json СТАРОГО лаунчера (Java-формат
// CustomPackMetadata). Назви полів відповідають @SerializedName старого
// коду: mc_version, loader_type (ВЕЛИКИМИ: VANILLA/FABRIC/FORGE/...),
// min_ram/max_ram рядком ("512M", "4G"), imported_from = "manual" або
// ім'я файлу модпаку, dir_name/custom_dir — тека збірки.
type legacyCustomPack struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	MCVersion     string   `json:"mc_version"`
	LoaderType    string   `json:"loader_type"`
	LoaderVersion string   `json:"loader_version"`
	CreatedAt     string   `json:"created_at"`
	UpdatedAt     string   `json:"updated_at"`
	Tags          []string `json:"tags"`
	MinRAM        string   `json:"min_ram"`
	MaxRAM        string   `json:"max_ram"`
	JavaVersion   int      `json:"java_version"`
	ImportedFrom  string   `json:"imported_from"`
	DirName       string   `json:"dir_name"`
	CustomDir     string   `json:"custom_dir"`
}

// legacyCustomPacksFile знаходить custom-packs.json старого лаунчера.
// Старий лаунчер тримав папку даних у %APPDATA%\.shaurm за замовчуванням
// (та сама, що у нового — див. config.defaultBaseDir), тому спершу пробуємо
// цей шлях, потім поточний baseDir (на випадок, якщо хтось переніс теку).
func legacyCustomPacksFile(cfgDir string) string {
	var paths []string
	if appdata := os.Getenv("APPDATA"); appdata != "" {
		paths = append(paths, filepath.Join(appdata, ".shaurm", "custom-packs.json"))
	}
	if cfgDir != "" {
		paths = append(paths, filepath.Join(cfgDir, "custom-packs.json"))
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if len(paths) > 0 {
		return paths[0]
	}
	return ""
}

// ImportLegacyCustomPacks переносить кастомні збірки зі старого лаунчера
// у новий: читає старий custom-packs.json, перекладає поля у новий
// custompack.Pack і дозаписує ті записи, яких ще немає у новому Store.
// Теки збірок у installations/ СПІЛЬНІ зі старим лаунчером (той самий
// baseDir), тож файли вже на місці — мігровані збірки одразу позначаються
// встановленими (картка "Готова", без вікна завантаження). Ідемпотентно:
// при повторному запуску записи, що вже є у новому Store, пропускаються.
func (a *App) ImportLegacyCustomPacks() (int, error) {
	if a.customPacks == nil || a.buildsReg == nil {
		return 0, nil
	}
	path := legacyCustomPacksFile(a.cfg.Dir())
	if path == "" {
		return 0, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, nil
	}
	var legacy []legacyCustomPack
	if err := json.Unmarshal(data, &legacy); err != nil {
		// Старий файл іншого формату або битий — не валимо запуск.
		return 0, nil
	}

	imported := 0
	instDir := a.cfg.InstancesDir()
	for _, lp := range legacy {
		id := strings.TrimSpace(lp.ID)
		if id == "" {
			continue
		}
		if _, exists := a.customPacks.Get(id); exists {
			continue
		}

		pack := custompack.Pack{
			ID:            id,
			Name:          firstNonEmpty(strings.TrimSpace(lp.Name), id),
			Description:   strings.TrimSpace(lp.Description),
			MCVersion:     strings.TrimSpace(lp.MCVersion),
			Loader:        strings.ToLower(strings.TrimSpace(lp.LoaderType)),
			LoaderVersion: strings.TrimSpace(lp.LoaderVersion),
			CreatedAt:     parseLegacyTime(lp.CreatedAt),
			UpdatedAt:     parseLegacyTime(lp.UpdatedAt),
		}
		if pack.Loader == "" {
			pack.Loader = "vanilla"
		}
		if lp.MinRAM != "" || lp.MaxRAM != "" {
			if min, ok := parseLegacyRAM(lp.MinRAM, 0); ok && min > 0 {
				pack.UseCustomRAM = true
				pack.MinRAMMB = min
			}
			if max, ok := parseLegacyRAM(lp.MaxRAM, 0); ok && max > 0 {
				pack.UseCustomRAM = true
				pack.MaxRAMMB = max
			}
			if pack.MinRAMMB <= 0 {
				pack.MinRAMMB = pack.MaxRAMMB / 2
			}
			if pack.MaxRAMMB <= 0 {
				pack.MaxRAMMB = pack.MinRAMMB * 2
			}
		}
		if lp.ImportedFrom != "" && !strings.EqualFold(lp.ImportedFrom, "manual") {
			pack.Source = "import"
			pack.ImportFile = lp.ImportedFrom
		} else {
			pack.Source = "manual"
		}

		// Тека збірки старого лаунчера (див. LauncherConfig.packDir(meta)):
		// customDir (відносно baseDir) → installations/dir_name → installations/id.
		packDir := filepath.Join(instDir, id)
		if lp.CustomDir != "" {
			cd := lp.CustomDir
			if !filepath.IsAbs(cd) {
				cd = filepath.Join(a.cfg.Dir(), cd)
			}
			packDir = cd
		} else if lp.DirName != "" {
			packDir = filepath.Join(instDir, lp.DirName)
		}
		if st, err := os.Stat(packDir); err == nil && st.IsDir() {
			if p := findLegacyAsset(packDir, "pack-icon"); p != "" {
				pack.IconPath = p
			}
			if p := findLegacyAsset(packDir, "pack-background"); p != "" {
				pack.BackgroundPath = p
			}
		}

		if err := a.customPacks.Save(pack); err != nil {
			continue
		}
		// Файли збірки вже в installations/<id> — позначаємо встановленою,
		// щоб картка показувала "Готова" одразу (як у старому лаунчері).
		a.buildsReg.SetInstalled(id, builds.KindCustom, "")
		imported++
	}
	return imported, nil
}

// parseLegacyTime розбирає ISO-час старого лаунчера (java.time.Instant).
func parseLegacyTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Now()
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	return time.Now()
}

// parseLegacyRAM перетворює рядок RAM старого лаунчера ("4G", "512M",
// "1536MB", "4096") у мегабайти. ok=false при порожньому/некоректному.
func parseLegacyRAM(s string, def int) (int, bool) {
	up := strings.ToUpper(strings.TrimSpace(s))
	if up == "" {
		return def, false
	}
	mult := 1
	switch {
	case strings.HasSuffix(up, "GB"):
		mult = 1024
		up = strings.TrimSuffix(up, "GB")
	case strings.HasSuffix(up, "G"):
		mult = 1024
		up = strings.TrimSuffix(up, "G")
	case strings.HasSuffix(up, "MB"):
		up = strings.TrimSuffix(up, "MB")
	case strings.HasSuffix(up, "M"):
		up = strings.TrimSuffix(up, "M")
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(up), 64)
	if err != nil || f <= 0 {
		return def, false
	}
	return int(f * float64(mult)), true
}

// findLegacyAsset шукає файл асета збірки старого лаунчера (pack-icon.*,
// pack-background.* — конвенція findPackAsset у CustomPackMetadata).
func findLegacyAsset(packDir, prefix string) string {
	for _, ext := range []string{".png", ".jpg", ".jpeg", ".gif", ".webp"} {
		p := filepath.Join(packDir, prefix+ext)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}
