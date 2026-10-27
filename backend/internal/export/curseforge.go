package export

import (
	"archive/zip"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
)

// ── .zip (CurseForge) експорт ───────────────────────────────────────────
// Формат за docs.curseforge.com (minecraftModpack): zip з manifest.json у
// корені та overrides/ для файлів, яких немає в files[]. Кожен файл у
// files[] описує мод через (projectID, fileID) — це копія Prism
// FlamePackExportTask: моди, що резолвнулись на CurseForge за fingerprint,
// потрапляють в manifest (без тіла в архіві), а решта (конфіги, світи,
// нерозв'язані моди...) — у overrides/.

// cfManifest — manifest.json формату CurseForge minecraftModpack.
type cfManifest struct {
	ManifestType    string           `json:"manifestType"`
	ManifestVersion int              `json:"manifestVersion"`
	Name            string           `json:"name"`
	Version         string           `json:"version"`
	Author          string           `json:"author,omitempty"`
	Overrides       string           `json:"overrides"`
	Minecraft       cfMinecraft      `json:"minecraft"`
	Files           []cfManifestFile `json:"files"`
}

type cfMinecraft struct {
	Version    string        `json:"version"`
	ModLoaders []cfModLoader `json:"modLoaders"`
}

type cfModLoader struct {
	ID      string `json:"id"`
	Primary bool   `json:"primary"`
}

type cfManifestFile struct {
	ProjectID int  `json:"projectID"`
	FileID    int  `json:"fileID"`
	Required  bool `json:"required"`
}

// CFResolvedFile — файл, знайдений на CurseForge за fingerprint (піде в
// files[] manifest.json). ProjectID/FileID — числові ID CurseForge.
type CFResolvedFile struct {
	ProjectID int
	FileID    int
	ModName   string
	IconURL   string
}

// ── Fingerprint (Murmur2, той самий алгоритм, що в Prism) ──────────────
// CurseForge ідентифікує файли модів за «fingerprint» — 32-бітним Murmur2
// хешем вмісту з фільтрацією пробілів/табів/переносів. Точна копія
// реалізації Prism Launcher (libraries/murmur2/src/MurmurHash2.cpp +
// modplatform/helpers/HashUtils.cpp): константа m=0x5bd1e995, r=24,
// початковий хеш 1^size (size = розмір після фільтрації), обробка по 4
// байти, фінальний зсув. Без цього POST /v1/fingerprints не знаходить файл.

// cfFilterOut — символи, що ігноруються при обчисленні fingerprint
// (Prism: tab, newline, CR, space).
func cfFilterOut(c byte) bool {
	return c == 9 || c == 10 || c == 13 || c == 32
}

// Murmur2Fingerprint рахує CurseForge fingerprint файлу (рядок десяткового
// числа — саме так його очікує POST /v1/fingerprints).
func Murmur2Fingerprint(absPath string) (string, error) {
	data, err := os.ReadFile(absPath)
	if err != nil {
		return "", err
	}
	// Розмір ПІСЛЯ фільтрації — потрібен для початкового хешу 1^size.
	size := 0
	for _, c := range data {
		if !cfFilterOut(c) {
			size++
		}
	}

	const m uint32 = 0x5bd1e995
	const r = 24
	h := uint32(1) ^ uint32(size)
	remaining := uint32(size)

	var quad [4]byte
	idx := 0
	// mix обробляє накопичені 4 байти (або фінальний хвіст).
	mix := func() {
		if remaining >= 4 {
			k := binary.LittleEndian.Uint32(quad[:])
			k *= m
			k ^= k >> r
			k *= m
			h *= m
			h ^= k
			remaining -= 4
		} else {
			// Фінальний mix: останні 0-3 байти + зсуви.
			switch remaining {
			case 3:
				h ^= uint32(quad[2]) << 16
				fallthrough
			case 2:
				h ^= uint32(quad[1]) << 8
				fallthrough
			case 1:
				h ^= uint32(quad[0])
				h *= m
			}
			h ^= h >> 13
			h *= m
			h ^= h >> 15
			remaining = 0
		}
	}
	for _, c := range data {
		if cfFilterOut(c) {
			continue
		}
		quad[idx] = c
		idx = (idx + 1) % 4
		if idx == 0 {
			mix()
		}
	}
	// Останній mix — як Prism робить після циклу читання.
	mix()

	return strconv.FormatUint(uint64(h), 10), nil
}

// resolveModsCF хешує кандидатів (fingerprint) і резолвить їх через
// CFResolver. Повертає мапу шлях → CFResolvedFile. Прогрес — по файлу.
func resolveModsCF(gameDir string, allFiles []string, resolver CFResolver, progress ProgressFn) map[string]CFResolvedFile {
	if resolver == nil {
		return nil
	}
	var candPaths []string
	for _, p := range allFiles {
		if isResolvableCandidate(p) {
			candPaths = append(candPaths, p)
		}
	}
	if len(candPaths) == 0 {
		return nil
	}
	type cand struct {
		path        string
		fingerprint string
	}
	cands := make([]cand, 0, len(candPaths))
	for i, p := range candPaths {
		fp, err := Murmur2Fingerprint(filepath.Join(gameDir, filepath.FromSlash(p)))
		if err != nil {
			continue
		}
		cands = append(cands, cand{path: p, fingerprint: fp})
		if progress != nil {
			progress(Progress{Stage: "hash", Current: i + 1, Total: len(candPaths), FileName: p})
		}
	}
	if len(cands) == 0 {
		return nil
	}
	fps := make([]string, 0, len(cands))
	for _, c := range cands {
		fps = append(fps, c.fingerprint)
	}
	found, err := resolver.ResolveFingerprints(fps)
	if err != nil {
		return nil // мережа/API недоступні — все піде в overrides (валідний пак)
	}
	out := map[string]CFResolvedFile{}
	for _, c := range cands {
		if fv, ok := found[c.fingerprint]; ok {
			out[c.path] = fv
			if progress != nil {
				progress(Progress{Stage: "resolve", FileName: c.path, ModName: fv.ModName, IconURL: fv.IconURL})
			}
		}
	}
	return out
}

// writeCurseForgeZip створює .zip архів CurseForge-формату. resolved — мапа
// відносний шлях → файл, що резолвнувся на CurseForge (піде в files[]).
// Усі інші включені файли — в overrides/. deps — {minecraft, fabric-loader,
// ...} (тільки minecraft + лоадер використовуються у manifest).
func writeCurseForgeZip(outPath, gameDir string, allFiles []string, resolved map[string]CFResolvedFile, deps map[string]string, name, version, summary, author string, progress ProgressFn) error {
	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()

	zw := zip.NewWriter(f)

	// 1. overrides/ — всі включені файли, КРІМ резолвнутих в manifest.
	overrides := make([]string, 0, len(allFiles))
	for _, p := range allFiles {
		if _, ok := resolved[p]; ok {
			continue // файл описаний в manifest, тіло не дублюємо
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

	// 2. manifest.json — LAST (як Prism: manifest останнім).
	m := cfManifest{
		ManifestType:    "minecraftModpack",
		ManifestVersion: 1,
		Name:            name,
		Version:         version,
		Author:          author,
		Overrides:       "overrides",
		Files:           make([]cfManifestFile, 0, len(resolved)),
	}
	if v, ok := deps["minecraft"]; ok {
		m.Minecraft.Version = v
	}
	if id := cfLoaderID(deps); id != "" {
		m.Minecraft.ModLoaders = []cfModLoader{{ID: id, Primary: true}}
	}
	// Сортуємо шляхи для стабільного виводу.
	keys := make([]string, 0, len(resolved))
	for p := range resolved {
		keys = append(keys, p)
	}
	sort.Strings(keys)
	for _, p := range keys {
		r := resolved[p]
		m.Files = append(m.Files, cfManifestFile{ProjectID: r.ProjectID, FileID: r.FileID, Required: true})
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := zipAddBytes(zw, "manifest.json", data); err != nil {
		return err
	}

	if err := zw.Close(); err != nil {
		return err
	}
	return f.Close()
}

// cfLoaderID будує id лоадера для manifest.json (формат Prism: "forge-47.2.0",
// "fabric-0.16.5", "neoforge-21.1.150" тощо).
func cfLoaderID(deps map[string]string) string {
	for _, key := range []string{"fabric-loader", "quilt-loader", "forge", "neoforge"} {
		if v, ok := deps[key]; ok && v != "" {
			return key + "-" + v
		}
	}
	return ""
}

// CFResolver — знаходить файли на CurseForge за fingerprint.
type CFResolver interface {
	ResolveFingerprints(fingerprints []string) (map[string]CFResolvedFile, error)
}
