// Імпорт готового модпаку у кастомну збірку — за зразком того, як це
// робить Prism Launcher: розпакувати архів, прочитати маніфест, довантажити
// файли модів за посиланнями/хешами з маніфесту, перенести overrides у
// теку збірки. НЕ використовує серверну систему синхронізації Shaurma
// (internal/sync) — та прив'язана до власного Worker-протоколу V2 і не
// має сенсу для довільних чужих архівів. Натомість публікує прогрес у
// той самий контракт builds.Progress/Stage, яким живиться UI картки та
// сторінки збірки — тому візуально користувач бачить один і той самий
// прогрес-бар незалежно від того, Shaurma це збірка чи імпортована.
package custompack

import (
	"archive/zip"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ImportFormat — розпізнаний формат архіву модпаку.
type ImportFormat string

const (
	FormatMrpack     ImportFormat = "mrpack"     // Modrinth: modrinth.index.json
	FormatCurseForge ImportFormat = "curseforge" // manifest.json (manifestType: minecraftModpack)
	FormatGeneric    ImportFormat = "generic"    // Prism/MultiMC-подібний zip без розпізнаного маніфесту: лише overrides
)

// ── Ті самі рядкові стадії, що й internal/sync.Stage — щоб UI (читає
// builds.Progress.Stage як звичайний рядок) показував однакові підписи
// для Shaurma- і кастомних збірок без жодних змін на фронтенді. ──
const (
	StageReadingManifest = "manifest_fetched"
	StageDownloading     = "downloading"
	StageExtracting      = "extracting"
	StageFinalizing      = "finalizing"
	StageComplete        = "complete"
)

// ImportProgress — той самий набір полів, що syncengine.RunnerProgress /
// builds.Progress (App.recordSyncProgress мапить один в один) — так
// App.recordCustomImportProgress зможе публікувати у ту саму
// dlTransient-мапу, яку читають картка й сторінка збірки.
type ImportProgress struct {
	PackID          string
	FilesTotal      int
	FilesCompleted  int
	BytesTotal      int64
	BytesDownloaded int64
	CurrentFile     string
	SpeedBPS        float64
	Stage           string
	Done            bool
	Error           error
}

type ImportProgressFunc func(ImportProgress)

// mrpackFile — один запис files[] у modrinth.index.json.
type mrpackFile struct {
	Path      string            `json:"path"`
	Hashes    map[string]string `json:"hashes"`
	Env       map[string]string `json:"env,omitempty"`
	Downloads []string          `json:"downloads"`
	FileSize  int64             `json:"fileSize"`
}

// mrpackIndex — modrinth.index.json (розділ "Modrinth Modpack Format").
type mrpackIndex struct {
	FormatVersion int          `json:"formatVersion"`
	Game          string       `json:"game"`
	VersionID     string       `json:"versionId"`
	Name          string       `json:"name"`
	Summary       string       `json:"summary"`
	Files         []mrpackFile `json:"files"`
	Dependencies  map[string]string `json:"dependencies"` // "minecraft", "fabric-loader", "forge", "quilt-loader", "neoforge"
}

// cfModLoader — один запис minecraft.modLoaders[] у CurseForge manifest.json.
type cfModLoader struct {
	ID      string `json:"id"` // напр. "forge-47.2.0" або "fabric-0.15.3"
	Primary bool   `json:"primary"`
}

// cfManifestFile — один запис files[] у CurseForge manifest.json.
type cfManifestFile struct {
	ProjectID int  `json:"projectID"`
	FileID    int  `json:"fileID"`
	Required  bool `json:"required"`
}

// cfManifest — CurseForge manifest.json.
type cfManifest struct {
	Minecraft struct {
		Version    string        `json:"version"`
		ModLoaders []cfModLoader `json:"modLoaders"`
	} `json:"minecraft"`
	ManifestType string           `json:"manifestType"`
	Name         string           `json:"name"`
	Version      string           `json:"version"`
	Author       string           `json:"author"`
	Files        []cfManifestFile `json:"files"`
	Overrides    string           `json:"overrides"`
}

// DetectedPack — результат розпізнавання архіву ще ДО завантаження файлів
// (те, що показується у прев'ю "Файл розпізнано" на вкладці Імпортувати:
// назва, версія MC, лоадер, кількість модів). JSON-теги збігаються з
// фронтенд-типом DetectedPack у lib/types.ts — без них Wails серіалізував
// би поля як "Format"/"SuggestedName", і фронтенд читав би порожні значення
// (імпорт "не розпізнавав" би жоден формат).
type DetectedPack struct {
	Format            ImportFormat `json:"format"`
	SuggestedName     string       `json:"suggestedName"`
	MCVersion         string       `json:"mcVersion"`
	Loader            Loader       `json:"loader"`
	LoaderVersion     string       `json:"loaderVersion"`
	ModCount          int          `json:"modCount"`
	HasOverrides      bool         `json:"hasOverrides"`
	// UnresolvedCFCount — скільки файлів CurseForge не вдасться
	// довантажити без API-ключа (HasAPIKey()==false). 0 для mrpack.
	UnresolvedCFCount int `json:"unresolvedCFCount"`
	// IconDataURL — вбудована іконка модпаку (icon.png/pack.png у корені
	// архіву або overrides), передана як data URL. Фронтенд показує її у
	// прев'ю імпорту одразу після розпізнавання файлу, ще до встановлення.
	// Під час імпорту бекенд виймає її сам, тому це лише для прев'ю.
	IconDataURL string `json:"iconDataUrl,omitempty"`
}

// DetectArchive відкриває .mrpack/.zip і розпізнає формат та базові
// параметри БЕЗ завантаження файлів модів — швидка операція для прев'ю
// одразу після вибору файлу (як processImportFile у старому лаунчері,
// але з реальним парсингом замість setTimeout-імітації).
func DetectArchive(archivePath string) (*DetectedPack, error) {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return nil, fmt.Errorf("не вдалося відкрити архів: %w", err)
	}
	defer zr.Close()

	baseName := strings.TrimSuffix(filepath.Base(archivePath), filepath.Ext(archivePath))

	if f := findZipFile(&zr.Reader, "modrinth.index.json"); f != nil {
		idx, err := readMrpackIndex(f)
		if err != nil {
			return nil, err
		}
		loader, loaderVer := detectMrpackLoader(idx.Dependencies)
		name := idx.Name
		if name == "" {
			name = baseName
		}
		return &DetectedPack{
			Format:        FormatMrpack,
			SuggestedName: name,
			MCVersion:     idx.Dependencies["minecraft"],
			Loader:        loader,
			LoaderVersion: loaderVer,
			ModCount:      len(idx.Files),
			HasOverrides:  findZipDir(&zr.Reader, "overrides/") || findZipDir(&zr.Reader, "client-overrides/"),
			IconDataURL:   embeddedIconDataURL(&zr.Reader),
		}, nil
	}

	if f := findZipFile(&zr.Reader, "manifest.json"); f != nil {
		m, err := readCFManifest(f)
		if err == nil && strings.EqualFold(m.ManifestType, "minecraftModpack") {
			loader, loaderVer := detectCFLoader(m.Minecraft.ModLoaders)
			name := m.Name
			if name == "" {
				name = baseName
			}
			cf := NewCurseForgeClient()
			unresolved := 0
			if !cf.HasAPIKey() {
				unresolved = len(m.Files)
			}
			return &DetectedPack{
				Format:            FormatCurseForge,
				SuggestedName:     name,
				MCVersion:         m.Minecraft.Version,
				Loader:            loader,
				LoaderVersion:     loaderVer,
				ModCount:          len(m.Files),
				HasOverrides:      m.Overrides != "" && findZipDir(&zr.Reader, m.Overrides+"/"),
				UnresolvedCFCount: unresolved,
			}, nil
		}
	}

	// Generic zip (Prism/MultiMC export чи довільний архів з mods/): немає
	// розпізнаного маніфесту — імпортуємо як є, версію/лоадер користувач
	// підтверджує вручну (форма показує ці поля редагованими, на відміну
	// від mrpack/CurseForge, де вони визначені й лише відображаються).
	hasMods := findZipDir(&zr.Reader, "mods/") || findZipDir(&zr.Reader, "minecraft/mods/") || findZipDir(&zr.Reader, ".minecraft/mods/")
	modCount := 0
	for _, f := range zr.File {
		if strings.HasSuffix(f.Name, ".jar") && strings.Contains(f.Name, "mods/") {
			modCount++
		}
	}
	return &DetectedPack{
		Format:        FormatGeneric,
		SuggestedName: baseName,
		ModCount:      modCount,
		HasOverrides:  hasMods,
	}, nil
}

func findZipFile(zr *zip.Reader, name string) *zip.File {
	for _, f := range zr.File {
		// Маніфест може лежати як у корені архіву, так і в одній
		// вкладеній теці (деякі експортери загортають усе в "<name>/").
		trimmed := f.Name
		if idx := strings.Index(trimmed, "/"); idx >= 0 {
			rest := trimmed[idx+1:]
			if rest == name {
				return f
			}
		}
		if trimmed == name {
			return f
		}
	}
	return nil
}

func findZipDir(zr *zip.Reader, prefix string) bool {
	for _, f := range zr.File {
		if strings.Contains(f.Name, prefix) {
			return true
		}
	}
	return false
}

// embeddedIconCandidates — типові імена іконки модпаку (конвенція
// Modrinth/Prism). Шукаються у корені архіву та у overrides-теках.
var embeddedIconCandidates = []string{"icon.png", "pack.png", "modpack.png"}

// findEmbeddedIconFile шукає вбудовану іконку модпаку в архіві: у корені
// (mrpack-специфікація дозволяє icon.png) або в overrides/client-overrides
// (типовий шлях Prism/Modrinth). Враховує одну провідну теку-обгортку.
func findEmbeddedIconFile(zr *zip.Reader) *zip.File {
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := strings.ReplaceAll(f.Name, "\\", "/")
		rel := stripArchiveWrapper(name)
		for _, cand := range embeddedIconCandidates {
			if rel == cand {
				return f
			}
			for _, prefix := range []string{"overrides/", "client-overrides/"} {
				if strings.HasPrefix(rel, prefix) && rel[len(prefix):] == cand {
					return f
				}
			}
		}
	}
	return nil
}

// embeddedIconDataURL читає вбудовану іконку архіву як data URL (для прев'ю
// у формі імпорту — бекенд, а не WebView, читає файл). Порожній рядок, якщо
// іконки немає або вона завелика.
func embeddedIconDataURL(zr *zip.Reader) string {
	f := findEmbeddedIconFile(zr)
	if f == nil || f.UncompressedSize64 > 2*1024*1024 {
		return ""
	}
	rc, err := f.Open()
	if err != nil {
		return ""
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil || len(data) == 0 {
		return ""
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(data)
}

// extractEmbeddedIcon виймає вбудовану іконку архіву у packDir як
// <packDir>/icon.<ext> (та сама ціль, що copyAssetInto для PNG). Повертає
// шлях до скопійованого файлу або "" якщо іконки немає.
func extractEmbeddedIcon(zr *zip.Reader, packDir string) string {
	f := findEmbeddedIconFile(zr)
	if f == nil {
		return ""
	}
	ext := filepath.Ext(f.Name)
	if ext == "" {
		ext = ".png"
	}
	dst := filepath.Join(packDir, "icon"+ext)
	rc, err := f.Open()
	if err != nil {
		return ""
	}
	defer rc.Close()
	out, err := os.Create(dst)
	if err != nil {
		return ""
	}
	defer out.Close()
	if _, err := io.Copy(out, rc); err != nil {
		os.Remove(dst)
		return ""
	}
	return dst
}

func readMrpackIndex(f *zip.File) (*mrpackIndex, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}
	var idx mrpackIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("modrinth.index.json пошкоджено: %w", err)
	}
	return &idx, nil
}

func readCFManifest(f *zip.File) (*cfManifest, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}
	var m cfManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("manifest.json пошкоджено: %w", err)
	}
	return &m, nil
}

func detectMrpackLoader(deps map[string]string) (Loader, string) {
	if v, ok := deps["fabric-loader"]; ok {
		return LoaderFabric, v
	}
	if v, ok := deps["quilt-loader"]; ok {
		return LoaderQuilt, v
	}
	if v, ok := deps["neoforge"]; ok {
		return LoaderNeoForge, v
	}
	if v, ok := deps["forge"]; ok {
		return LoaderForge, v
	}
	return LoaderVanilla, ""
}

func detectCFLoader(loaders []cfModLoader) (Loader, string) {
	pick := ""
	for _, l := range loaders {
		if l.Primary {
			pick = l.ID
			break
		}
	}
	if pick == "" && len(loaders) > 0 {
		pick = loaders[0].ID
	}
	if pick == "" {
		return LoaderVanilla, ""
	}
	// Формат "<loader>-<version>", напр. "forge-47.2.0", "fabric-0.15.3".
	parts := strings.SplitN(pick, "-", 2)
	name := strings.ToLower(parts[0])
	ver := ""
	if len(parts) == 2 {
		ver = parts[1]
	}
	switch name {
	case "forge":
		return LoaderForge, ver
	case "fabric":
		return LoaderFabric, ver
	case "quilt":
		return LoaderQuilt, ver
	case "neoforge":
		return LoaderNeoForge, ver
	default:
		return LoaderVanilla, ""
	}
}

// ── Виконання імпорту (завантаження файлів + overrides) ─────────────────

// ImportParams — параметри, підтверджені користувачем на прев'ю-екрані
// вкладки "Імпортувати" (назва можна змінити, версія/лоадер для mrpack і
// CurseForge беруться з маніфесту й не редагуються — так само, як просив
// ужиток "ті самі параметри, лише без вибору версії гри чи лоадера").
type ImportParams struct {
	Name        string
	Color       string
	IconSrcPath string
	Icon        string // пресет-іконка (Tabler, напр. "ti-puzzle"); PNG має пріоритет
}

// importTask — один файл, що потребує довантаження.
type importTask struct {
	relPath  string
	url      string
	sha1     string
	fileSize int64
}

// ImportArchive виконує повний конвеєр: розпаковує overrides у теку
// збірки, резолвить і довантажує моди (mrpack: прямі URL з маніфесту;
// CurseForge: через CurseForgeClient.GetModFile), реєструє збірку в
// Store. maxConcurrent — паралельність завантажень файлів модів (той
// самий смисл, що syncengine "N/2 і N/2", тут простіше — одна черга).
func ImportArchive(ctx context.Context, store *Store, instanceDir, archivePath string, detected *DetectedPack, p ImportParams, maxConcurrent int, onProgress ImportProgressFunc) (Pack, error) {
	name := strings.TrimSpace(p.Name)
	if name == "" {
		name = detected.SuggestedName
	}
	if name == "" {
		return Pack{}, fmt.Errorf("не вдалося визначити назву збірки")
	}
	if maxConcurrent < 1 {
		maxConcurrent = 4
	}

	id := store.UniqueID(name)
	packDir := filepath.Join(instanceDir, id)
	gameDir := filepath.Join(packDir, ".minecraft")
	if err := os.MkdirAll(gameDir, 0755); err != nil {
		return Pack{}, fmt.Errorf("не вдалося створити теку збірки: %w", err)
	}

	// Реєструємо збірку в Store ОДРАЗУ (ще до довантаження модів), щоб вона
	// з'явилась у GetBuilds/сайдбарі/на сторінці збірки з прогресом, поки
	// йде імпорт. Інакше після ImportModpackArchive (що повертає ID миттєво)
	// фронтенд не знаходив би збірку в списку і вікно установки (як у
	// Shaurma-збірок) не показалось би — лише готова збірка після всього.
	// Фінальний запис (з іконкою) зберігається наприкінці — Save оновлює
	// той самий ID і зберігає CreatedAt.
	placeholder := Pack{
		ID:            id,
		Name:          name,
		Icon:          p.Icon,
		Color:         p.Color,
		MCVersion:     detected.MCVersion,
		Loader:        string(detected.Loader),
		LoaderVersion: detected.LoaderVersion,
		Source:        "import",
		ImportFile:    filepath.Base(archivePath),
	}
	if err := store.Save(placeholder); err != nil {
		return Pack{}, fmt.Errorf("не вдалося зареєструвати збірку: %w", err)
	}

	// Якщо імпорт обірветься (помилка/скасування) — прибираємо тимчасовий
	// запис, щоб у списку не лишалась "фантомна" збірка, яку не можна
	// встановити. Часткові файли на диску лишаються (як при скасуванні).
	committed := false
	defer func() {
		if !committed {
			_ = store.Delete(id)
		}
	}()

	report := func(pr ImportProgress) {
		pr.PackID = id
		if onProgress != nil {
			onProgress(pr)
		}
	}
	report(ImportProgress{Stage: StageReadingManifest})

	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		report(ImportProgress{Stage: "", Done: true, Error: err})
		return Pack{}, fmt.Errorf("не вдалося відкрити архів: %w", err)
	}
	defer zr.Close()

	// Вбудована іконка модпаку (icon.png у корені/overrides) — виймаємо
	// ОДРАЗУ, щоб placeholder у Store вже мав IconPath і сайдбар/картка
	// показували іконку модпаку під час встановлення, а не наприкінці.
	// Пріоритет: явно обрана користувачем PNG > вбудована іконка архіву >
	// іконка з розпакованих overrides > пресет (p.Icon).
	iconSrc := p.IconSrcPath
	if iconSrc == "" {
		iconSrc = extractEmbeddedIcon(&zr.Reader, packDir)
		if iconSrc != "" {
			placeholder.IconPath = iconSrc
			if err := store.Save(placeholder); err != nil {
				return Pack{}, fmt.Errorf("не вдалося оновити збірку: %w", err)
			}
		}
	}

	var tasks []importTask
	var overridesPrefixes []string
	var cfFiles []cfManifestFile

	switch detected.Format {
	case FormatMrpack:
		f := findZipFile(&zr.Reader, "modrinth.index.json")
		if f == nil {
			return Pack{}, fmt.Errorf("modrinth.index.json не знайдено в архіві")
		}
		idx, err := readMrpackIndex(f)
		if err != nil {
			return Pack{}, err
		}
		for _, mf := range mrpackClientFiles(idx.Files) {
			tasks = append(tasks, importTask{
				relPath:  mf.Path,
				url:      firstNonEmptyStr(mf.Downloads),
				sha1:     mf.Hashes["sha1"],
				fileSize: mf.FileSize,
			})
		}
		overridesPrefixes = []string{"overrides/", "client-overrides/"}

	case FormatCurseForge:
		f := findZipFile(&zr.Reader, "manifest.json")
		if f == nil {
			return Pack{}, fmt.Errorf("manifest.json не знайдено в архіві")
		}
		m, err := readCFManifest(f)
		if err != nil {
			return Pack{}, err
		}
		cfFiles = m.Files
		if m.Overrides != "" {
			overridesPrefixes = []string{m.Overrides + "/"}
		} else {
			overridesPrefixes = []string{"overrides/"}
		}

	case FormatGeneric:
		// Немає маніфесту — весь вміст архіву трактуємо як overrides
		// (копіюємо структуру як є у .minecraft/), без окремого
		// довантаження файлів мережею.
		overridesPrefixes = []string{""}

	default:
		return Pack{}, fmt.Errorf("невідомий формат архіву")
	}

	// ── Розпаковка overrides напряму в .minecraft/ теки збірки ──
	if ctx.Err() != nil {
		return Pack{}, ctx.Err()
	}
	report(ImportProgress{Stage: StageExtracting})
	if err := extractOverrides(&zr.Reader, overridesPrefixes, gameDir); err != nil {
		return Pack{}, fmt.Errorf("не вдалося розпакувати вміст архіву: %w", err)
	}

	// ── CurseForge: резолвимо projectID/fileID у прямі URL перед чергою ──
	if detected.Format == FormatCurseForge && len(cfFiles) > 0 {
		cf := NewCurseForgeClient()
		if !cf.HasAPIKey() {
			return Pack{}, fmt.Errorf("модпак використовує CurseForge (%d модів), але API-ключ CurseForge не налаштовано в цій збірці лаунчера — довантаження модів неможливе. Overrides вже розпаковано, теку можна доповнити модами вручну", len(cfFiles))
		}
		for _, mf := range cfFiles {
			info, err := cf.GetModFile(mf.ProjectID, mf.FileID)
			if err != nil {
				if mf.Required {
					return Pack{}, fmt.Errorf("обов'язковий мод (проєкт %d, файл %d): %w", mf.ProjectID, mf.FileID, err)
				}
				continue // опційний мод, що не резолвнувся — пропускаємо, не валимо весь імпорт
			}
			tasks = append(tasks, importTask{
				relPath:  "mods/" + info.FileName,
				url:      info.DownloadURL,
				sha1:     info.SHA1,
				fileSize: info.FileLength,
			})
		}
	}

	// ── Довантаження файлів модів (mrpack прямі URL / резолвнутий CurseForge) ──
	if len(tasks) > 0 {
		if err := downloadImportTasks(ctx, tasks, gameDir, maxConcurrent, id, report); err != nil {
			return Pack{}, err
		}
	}

	report(ImportProgress{Stage: StageFinalizing})

	// Іконка модпаку: спершу явно обрана користувачем, потім вбудована в
	// архів (уже вийнята вище), далі іконка з розпакованих overrides
	// (icon.png у корені .minecraft/, як у Prism).
	if iconSrc == "" {
		iconSrc = findExtractedIcon(gameDir)
	}

	pack := Pack{
		ID:            id,
		Name:          name,
		Icon:          p.Icon,
		Color:         p.Color,
		MCVersion:     detected.MCVersion,
		Loader:        string(detected.Loader),
		LoaderVersion: detected.LoaderVersion,
		Source:        "import",
		ImportFile:    filepath.Base(archivePath),
	}
	if iconSrc != "" {
		// Якщо іконка вже лежить у теці збірки під фіксованим іменем
		// (вбудована з архіву вийнята на початку) — не копіюємо саму в
		// себе (copyAssetInto обнулив би файл: os.Create до читання).
		if filepath.Dir(iconSrc) == packDir {
			pack.IconPath = iconSrc
		} else if dst, err := copyAssetInto(packDir, "icon", iconSrc); err == nil {
			pack.IconPath = dst
		}
	}
	if err := store.Save(pack); err != nil {
		return Pack{}, err
	}
	committed = true

	report(ImportProgress{Stage: StageComplete, Done: true})
	return pack, nil
}

// mrpackClientFiles фільтрує файли маніфесту за env.client (пропускаємо
// ті, де client="unsupported" — серверні-only файли непотрібні лаунчеру).
func mrpackClientFiles(files []mrpackFile) []mrpackFile {
	out := make([]mrpackFile, 0, len(files))
	for _, f := range files {
		if f.Env != nil && f.Env["client"] == "unsupported" {
			continue
		}
		out = append(out, f)
	}
	return out
}

func firstNonEmptyStr(list []string) string {
	for _, s := range list {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

// extractOverrides копіює вміст overrides-тек (mrpack/CurseForge) або
// всього архіву (generic) у gameDir (.minecraft/ збірки), пропускаючи
// сам файл маніфесту. Шляхи всередині архіву перевіряються на вихід за
// межі призначення (захист від zip-slip / ../ у modrinth.index.json —
// та сама небезпека, на яку прямо вказує офіційна специфікація mrpack).
func extractOverrides(zr *zip.Reader, prefixes []string, gameDir string) error {
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := f.Name
		rel := ""
		matched := false
		for _, prefix := range prefixes {
			if prefix == "" {
				// generic: весь архів як overrides, крім явних маніфестів.
				base := filepath.Base(name)
				if base == "modrinth.index.json" || base == "manifest.json" || base == "modlist.html" {
					continue
				}
				rel = stripArchiveWrapper(name)
				matched = true
				break
			}
			idx := strings.Index(name, prefix)
			if idx < 0 {
				continue
			}
			rel = name[idx+len(prefix):]
			matched = true
			break
		}
		if !matched || rel == "" {
			continue
		}
		if err := extractZipEntryTo(f, gameDir, rel); err != nil {
			return err
		}
	}
	return nil
}

// stripArchiveWrapper прибирає єдину провідну теку-обгортку в шляху
// generic-архіву (напр. "MyPack-1.0/mods/foo.jar" → "mods/foo.jar"),
// якщо весь архів загорнутий в одну кореневу теку.
func stripArchiveWrapper(name string) string {
	idx := strings.Index(name, "/")
	if idx < 0 {
		return name
	}
	return name[idx+1:]
}

func extractZipEntryTo(f *zip.File, destRoot, relPath string) error {
	cleanRel := filepath.Clean(relPath)
	if cleanRel == "." || strings.HasPrefix(cleanRel, "..") || filepath.IsAbs(cleanRel) {
		return nil // zip-slip захист: тихо пропускаємо шкідливий/сміттєвий шлях
	}
	destPath := filepath.Join(destRoot, cleanRel)
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, rc)
	return err
}

// findExtractedIcon шукає типове ім'я іконки модпаку в розпакованому
// gameDir (icon.png в корені overrides — конвенція Modrinth/Prism).
func findExtractedIcon(gameDir string) string {
	for _, name := range []string{"icon.png", "pack.png", "modpack.png"} {
		p := filepath.Join(gameDir, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// downloadImportTasks завантажує список файлів модів у modsDir паралельно
// (maxConcurrent воркерів), верифікує SHA1 при наявності, і публікує
// прогрес у той самий контракт, яким живиться картка/сторінка збірки.
func downloadImportTasks(ctx context.Context, tasks []importTask, gameDir string, maxConcurrent int, packID string, report func(ImportProgress)) error {
	var bytesTotal int64
	for _, t := range tasks {
		bytesTotal += t.fileSize
	}

	var (
		filesDone   int32
		bytesDone   int64
		mu          sync.Mutex
		firstErr    error
		speedWindow = time.Now()
		speedBytes  int64
	)

	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup
	client := &http.Client{Timeout: 5 * time.Minute}

	for _, t := range tasks {
		if ctx.Err() != nil {
			break
		}
		if t.url == "" {
			mu.Lock()
			if firstErr == nil {
				firstErr = fmt.Errorf("файл %s не має посилання на завантаження", t.relPath)
			}
			mu.Unlock()
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(task importTask) {
			defer wg.Done()
			defer func() { <-sem }()

			mu.Lock()
			if firstErr != nil {
				mu.Unlock()
				return
			}
			mu.Unlock()
			if ctx.Err() != nil {
				return
			}

			report(ImportProgress{
				Stage:           StageDownloading,
				CurrentFile:     task.relPath,
				FilesTotal:      len(tasks),
				FilesCompleted:  int(atomic.LoadInt32(&filesDone)),
				BytesTotal:      bytesTotal,
				BytesDownloaded: atomic.LoadInt64(&bytesDone),
			})

			// ПЛАВНИЙ прогрес: downloadOneFile викликає onBytes на кожному
			// прочитаному блоці, тут оновлюємо спільні лічильники і
			// публікуємо подію не частіше ніж раз на ~150мс (замість
			// стрибків 0%→100% по завершенні файлів).
			lastReport := time.Now()
			onBytes := func(delta int64) {
				atomic.AddInt64(&bytesDone, delta)
				atomic.AddInt64(&speedBytes, delta)
				if time.Since(lastReport) < 150*time.Millisecond {
					return
				}
				lastReport = time.Now()
				mu.Lock()
				elapsed := time.Since(speedWindow).Seconds()
				var speed float64
				if elapsed > 0 {
					speed = float64(atomic.LoadInt64(&speedBytes)) / elapsed
				}
				if elapsed > 1 {
					speedWindow = time.Now()
					atomic.StoreInt64(&speedBytes, 0)
				}
				mu.Unlock()
				report(ImportProgress{
					Stage:           StageDownloading,
					CurrentFile:     task.relPath,
					FilesTotal:      len(tasks),
					FilesCompleted:  int(atomic.LoadInt32(&filesDone)),
					BytesTotal:      bytesTotal,
					BytesDownloaded: atomic.LoadInt64(&bytesDone),
					SpeedBPS:        speed,
				})
			}

			_, err := downloadOneFile(ctx, client, task, gameDir, onBytes)
			if err != nil {
				if ctx.Err() != nil {
					return // скасовано користувачем — не помилка, що зупиняє все з повідомленням
				}
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("%s: %w", task.relPath, err)
				}
				mu.Unlock()
				return
			}
			// bytesDone/speedBytes вже оновлені через onBytes (по блоках),
			// тут лише лічильник файлів і підсумковий репорт по файлу.
			newDone := atomic.AddInt32(&filesDone, 1)

			mu.Lock()
			elapsed := time.Since(speedWindow).Seconds()
			var speed float64
			if elapsed > 0 {
				speed = float64(atomic.LoadInt64(&speedBytes)) / elapsed
			}
			if elapsed > 1 {
				speedWindow = time.Now()
				atomic.StoreInt64(&speedBytes, 0)
			}
			mu.Unlock()

			report(ImportProgress{
				Stage:           StageDownloading,
				CurrentFile:     task.relPath,
				FilesTotal:      len(tasks),
				FilesCompleted:  int(newDone),
				BytesTotal:      bytesTotal,
				BytesDownloaded: atomic.LoadInt64(&bytesDone),
				SpeedBPS:        speed,
			})
		}(t)
	}
	wg.Wait()

	if ctx.Err() != nil {
		return ctx.Err()
	}
	if firstErr != nil {
		report(ImportProgress{Stage: StageDownloading, Done: true, Error: firstErr})
		return firstErr
	}
	return nil
}

// progressReader — обгортка io.Reader, що сповіщає про кожен прочитаний
// блок (для плавного прогресу під час одного файлу).
type progressReader struct {
	r       io.Reader
	onBytes func(int64)
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if n > 0 && p.onBytes != nil {
		p.onBytes(int64(n))
	}
	return n, err
}

func (p *progressReader) Close() error {
	if c, ok := p.r.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

func downloadOneFile(ctx context.Context, client *http.Client, task importTask, gameDir string, onBytes func(int64)) (int64, error) {
	cleanRel := filepath.Clean(task.relPath)
	if cleanRel == "." || strings.HasPrefix(cleanRel, "..") || filepath.IsAbs(cleanRel) {
		return 0, fmt.Errorf("недопустимий шлях файлу в маніфесті: %s", task.relPath)
	}
	dest := filepath.Join(gameDir, cleanRel)
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return 0, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, task.url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "shaurma-team/shaurma-launcher/2.0 (+custompack-import; shaurmaofficial2210@gmail.com)")
	// CurseForge CDN вимагає x-api-key на прямих завантаженнях файлів
	// (edge.forgecdn.net) — без нього повертає 401. Modrinth-файли ключа
	// не потребують, тож додаємо заголовок лише для forgecdn.net-посилань.
	if IsCurseForgeCDNURL(task.url) && HasCurseForgeAPIKey() {
		req.Header.Set("x-api-key", CurseForgeAPIKeyValue())
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	tmp := dest + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return 0, err
	}

	hasher := sha1.New()
	var writer io.Writer = out
	if task.sha1 != "" {
		writer = io.MultiWriter(out, hasher)
	}

	body := resp.Body
	if onBytes != nil {
		body = &progressReader{r: resp.Body, onBytes: onBytes}
	}
	n, copyErr := io.Copy(writer, body)
	closeErr := out.Close()
	if copyErr != nil {
		os.Remove(tmp)
		return 0, copyErr
	}
	if closeErr != nil {
		os.Remove(tmp)
		return 0, closeErr
	}
	if task.sha1 != "" {
		sum := hex.EncodeToString(hasher.Sum(nil))
		if !strings.EqualFold(sum, task.sha1) {
			os.Remove(tmp)
			return 0, fmt.Errorf("SHA1 не збігається (очікувано %s, отримано %s)", task.sha1, sum)
		}
	}
	if err := os.Rename(tmp, dest); err != nil {
		return 0, err
	}
	return n, nil
}
