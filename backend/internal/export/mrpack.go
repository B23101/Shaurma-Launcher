package export

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ── .mrpack (Modrinth) експорт ──────────────────────────────────────────
// Формат за modrinth.com/docs/technical/mrpack: zip з modrinth.index.json
// у корені та overrides/ для файлів, яких немає в files[]. Кожен файл у
// files[] описує мод, який ВСТАНОВИТЬСЯ через downloads (посилання на
// Modrinth CDN) — це копія Prism ModrinthPackExportTask: файли, що
// резолвнулись на Modrinth, потрапляють в index (без тіла в архіві), а
// решта (конфіги, ресурспаки без резолву, saves...) — у overrides/.

// mrpackIndex — modrinth.index.json (схема PackFormat з Modrinth App).
type mrpackIndex struct {
	FormatVersion int                     `json:"formatVersion"`
	Game          string                  `json:"game"`
	Name          string                  `json:"name"`
	VersionID     string                  `json:"versionId"`
	Summary       string                  `json:"summary,omitempty"`
	Files         []mrpackIndexFile       `json:"files"`
	Dependencies  map[string]string       `json:"dependencies,omitempty"`
}

type mrpackIndexFile struct {
	Path      string            `json:"path"`
	Downloads []string          `json:"downloads"`
	Hashes    map[string]string `json:"hashes"`
	FileSize  int64             `json:"fileSize"`
	Env       map[string]string `json:"env,omitempty"`
}

// writeMrpack створює .mrpack архів. resolved — мапа відносний шлях → файл,
// що резолвнувся на Modrinth (піде в files[]). Усі інші включені файли
// (allFiles) — в overrides/. dependencies — {minecraft, fabric-loader, ...}.
func writeMrpack(outPath, gameDir string, allFiles []string, resolved map[string]ResolvedFile, deps map[string]string, name, version, summary string, progress ProgressFn) error {
	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()

	zw := zip.NewWriter(f)

	// 1. overrides/ — всі включені файли, КРІМ резолвнутих в index.
	overrides := make([]string, 0, len(allFiles))
	for _, p := range allFiles {
		if _, ok := resolved[p]; ok {
			continue // файл описаний в index, тіло не дублюємо
		}
		overrides = append(overrides, p)
	}
	for i, p := range overrides {
		entry := "overrides/" + p
		if err := zipAddFile(zw, entry, filepath.Join(gameDir, filepath.FromSlash(p))); err != nil {
			return fmt.Errorf("overrides %s: %w", p, err)
		}
		if progress != nil {
			progress(Progress{Stage: "pack", Current: i + 1, Total: len(overrides), FileName: p})
		}
	}

	// 2. modrinth.index.json — LAST (як Modrinth App: індекс останнім).
	idx := mrpackIndex{
		FormatVersion: 1,
		Game:          "minecraft",
		Name:          name,
		VersionID:     version,
		Files:         make([]mrpackIndexFile, 0, len(resolved)),
	}
	if summary != "" {
		idx.Summary = summary
	}
	if len(deps) > 0 {
		idx.Dependencies = deps
	}
	// Сортуємо шляхи для стабільного виводу.
	keys := make([]string, 0, len(resolved))
	for p := range resolved {
		keys = append(keys, p)
	}
	sort.Strings(keys)
	for _, p := range keys {
		r := resolved[p]
		env := map[string]string{"client": "required", "server": "required"}
		path := p
		// .disabled → optional + шлях без суфікса (Prism rename).
		if strings.HasSuffix(path, ".disabled") {
			path = strings.TrimSuffix(path, ".disabled")
			env["client"] = "optional"
			env["server"] = "optional"
		}
		if r.ServerOnly {
			env["client"] = "unsupported"
		}
		idx.Files = append(idx.Files, mrpackIndexFile{
			Path:      path,
			Downloads: []string{r.URL},
			Hashes:    map[string]string{"sha1": r.Sha1, "sha512": r.Sha512},
			FileSize:  r.Size,
			Env:       env,
		})
	}
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	if err := zipAddBytes(zw, "modrinth.index.json", data); err != nil {
		return err
	}

	if err := zw.Close(); err != nil {
		return err
	}
	return f.Close()
}

// ResolvedFile — файл, знайдений на Modrinth за хешем (піде в files[]).
// ModName/IconURL — для живого прогресу (назва/іконка мода в UI).
type ResolvedFile struct {
	URL        string
	Sha1       string
	Sha512     string
	Size       int64
	ServerOnly bool
	ModName    string
	IconURL    string
}

// zipAddFile додає файл з диска в архів zip під entry (Unix-шлях),
// СТРІМОМ (io.Copy), а не os.ReadFile — великі файли (світи saves,
// ресурспаки) не мають цілими лягати в RAM.
func zipAddFile(zw *zip.Writer, entry, absPath string) error {
	f, err := os.Open(absPath)
	if err != nil {
		return err
	}
	defer f.Close()
	w, err := zw.Create(entry)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, f)
	return err
}

// zipAddBytes додає буфер в архів zip (для дрібних JSON-індексів).
func zipAddBytes(zw *zip.Writer, entry string, data []byte) error {
	w, err := zw.Create(entry)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}
