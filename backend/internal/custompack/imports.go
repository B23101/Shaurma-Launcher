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
	"shaurma-launcher-backend/internal/crashguard"
	"archive/zip"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"shaurma-launcher-backend/internal/config"
	"shaurma-launcher-backend/internal/launchlog"
	shsync "shaurma-launcher-backend/internal/sync"
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
	// SkippedPaths — заповнюється ОДИН РАЗ, у фінальному
	// ImportProgress{Stage: StageFinalizing, Done: true} виклику
	// UpdatePackFromSource: шляхи (config/foo.cfg, options.txt тощо),
	// які extractOverrides НЕ перезаписала при оновленні, бо вони вже
	// були на диску (protectedOverridePaths). nil для звичайних
	// проміжних подій прогресу і для первинного імпорту (ImportArchive) —
	// там перезапис усього безумовний і коректний.
	SkippedPaths []string
}

type ImportProgressFunc func(ImportProgress)

// ── Заблоковані автором моди (CurseForge 3rd party download opt-out) ─────
// Аналог BlockedModsDialog у Prism: після резолву manifest.json, якщо
// знайдено файли, які автор заборонив качати зі сторонніх джерел, імпорт
// СТАЄ НА ПАУЗУ і питає користувача — Modrinth fallback за sha1, пропустити
// або скасувати. Без цього заблоковані моди мовчки валили імпорт.

// BlockedCFMod — один мод, чий файл автор заблокував для сторонніх
// завантажень (downloadUrl порожній навіть через /download-url).
type BlockedCFMod struct {
	ProjectID  int    `json:"projectId"`
	FileID     int    `json:"fileId"`
	FileName   string `json:"fileName"`
	SHA1       string `json:"sha1"`
	WebsiteURL string `json:"websiteUrl"`
	Required   bool   `json:"required"`
}

// BlockedModsDecision — вибір користувача у діалозі заблокованих модів.
type BlockedModsDecision string

const (
	// BlockedDecisionModrinth — спробувати знайти ТОЙ САМИЙ файл на Modrinth
	// за sha1 (налаштування UseModrinthForBlocked / Prism FallbackMRBlockedMods).
	BlockedDecisionModrinth BlockedModsDecision = "modrinth"
	// BlockedDecisionSkip — пропустити заблоковані моди, продовжити з рештою.
	BlockedDecisionSkip BlockedModsDecision = "skip"
	// BlockedDecisionManual — користувач скачає моди ВРУЧНУ з прямого
	// посилання (сторінки CurseForge), імпорт продовжується БЕЗ них — як
	// Prism: BlockedModsDialog показує websiteUrl і дозволяє «Download anyway».
	BlockedDecisionManual BlockedModsDecision = "manual"
	// BlockedDecisionCancel — скасувати весь імпорт.
	BlockedDecisionCancel BlockedModsDecision = "cancel"
)

// BlockedHandlerFunc — колбек, який викликається ПІСЛЯ резолву, якщо знайдено
// заблоковані моди. Реалізація (App-рівень) показує користувачу діалог і
// повертає рішення. allowModrinth=false означає: Modrinth fallback уже
// спробований (усі знайдені на Modrinth моди вже в черзі) — діалог має
// пропонувати ЛИШЕ ручне встановлення / пропустити / скасувати. ctx
// скасовується при скасуванні/паузі імпорту — хендлер має відповісти на
// нього (перервати очікування).
type BlockedHandlerFunc func(ctx context.Context, blocked []BlockedCFMod, allowModrinth bool) (BlockedModsDecision, error)

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
	FormatVersion int               `json:"formatVersion"`
	Game          string            `json:"game"`
	VersionID     string            `json:"versionId"`
	Name          string            `json:"name"`
	Summary       string            `json:"summary"`
	Files         []mrpackFile      `json:"files"`
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
	Format        ImportFormat `json:"format"`
	SuggestedName string       `json:"suggestedName"`
	MCVersion     string       `json:"mcVersion"`
	Loader        Loader       `json:"loader"`
	LoaderVersion string       `json:"loaderVersion"`
	ModCount      int          `json:"modCount"`
	HasOverrides  bool         `json:"hasOverrides"`
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
// ВІДНОСНИЙ шлях (лише ім'я файлу) або "" якщо іконки немає.
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
	// Повертаємо ВІДНОСНИЙ шлях (лише ім'я файлу).
	return "icon" + ext
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
//
// Поля Source* заповнюються БРАУЗЕРОМ КОНТЕНТУ (встановлення модпаку як
// «інтернет-збірки»): вони записуються в Pack і живлять кнопку «Оновити
// з джерела» в контекстному меню. Для звичайного імпорту з файлу лишаються
// порожніми (Source="import").
type ImportParams struct {
	Name        string
	Color       string
	IconSrcPath string
	Icon        string // пресет-іконка (Tabler, напр. "ti-puzzle"); PNG має пріоритет
	// BackgroundSrcPath — фон (банер) збірки, вже лежить у teці збірки
	// (заповнюється браузером контенту через CreateBrowserModpack, який
	// завантажує featured-зображення з галереї Modrinth/CurseForge).
	// Раніше ImportArchive взагалі не знав про фон — placeholder і
	// фінальний Pack переписували запис без цього поля, тож фон,
	// збережений при створенні «не завантаженої» картки, губився щойно
	// користувач тиснув «Завантажити».
	BackgroundSrcPath string

	// Джерело «інтернет-збірки» (заповнює браузер контенту).
	Source            string // modrinth | curseforge (порожнє = import)
	SourceProjectID   string
	SourceVersionID   string
	SourceProjectSlug string
	// SourceArchiveURL — пряме посилання на архів модпаку. Критично для
	// «Завантажити»/«Оновити» інтернет-збірки: раніше цього поля не було,
	// тож фінальний Pack після імпорту втрачав посилання на архів і
	// кнопка «Завантажити» на картці «не завантаженої» збірки ставала
	// мертвою (SourceArchiveURL == ""). Тепер воно зберігається в
	// placeholder і фінальному записі.
	SourceArchiveURL string

	// ExistingID — якщо заданий, імпорт виконується В ІСНУЮЧУ збірку з цим
	// ID (а не створює нову). Використовується «Завантажити» для модпаку,
	// що вже створений як «не завантажена» інтернет-збірка: картка вже
	// є у списку (з іконкою/кольором), клік по «Завантажити» наповнює її
	// файлами, а не дублює.
	ExistingID string

	// ArchivePath — ПОВНИЙ шлях до локального архіву (імпорт з файлу).
	// Зберігається у Pack, щоб «Продовжити» після паузи могло відновити
	// імпорт з того самого файлу (див. Pack.ArchivePath). Для браузерних
	// модпаків порожнє — архів качається повторно за SourceArchiveURL.
	ArchivePath string

	// PreassignedID — ID, зарезервований викликачем ДО імпорту, щоб
	// фронтенд знав ID збірки ще до першої події прогресу (потрібен, зокрема,
	// діалогу заблокованих модів — подія import:blocked має нести packId).
	// Порожнє = генеруємо самі (UniqueID).
	PreassignedID string

	// BlockedHandler — обробник заблокованих автором модів (див. типи вище).
	// nil = мовчазно пробувати Modrinth fallback (стара поведінка).
	BlockedHandler BlockedHandlerFunc

	// UseModrinthForBlocked — налаштування лаунчера «Брати з Modrinth
	// заблоковані моди»: коли увімкнене, під час розпаковки заблоковані моди
	// беруться з Modrinth ОДРАЗУ БЕЗ діалогу (користувач уже дав згоду
	// галочкою), а діалог з'являється лише для тих, кого на Modrinth НЕ
	// знайшлось — і вже БЕЗ опції Modrinth (лише ручне встановлення).
	UseModrinthForBlocked bool
}

// importTask — один файл, що потребує довантаження.
type importTask struct {
	relPath  string
	url      string
	sha1     string
	fileSize int64
}

// planImport планує імпорт архіву: список файлів для довантаження (tasks),
// overrides-префікси та (для CurseForge) сирі записи manifest.json.
// Використовується і звичайним імпортом, і «Оновити з джерела» — щоб diff
// нового маніфесту проти старого рахувався тим самим кодом, що й перше
// встановлення. cfFiles повертаються НЕрезолвленими: резолв (GetModFile)
// робить викликач (потребує API-ключ і обробки опційних файлів).
func planImport(zr *zip.Reader, detected *DetectedPack) (tasks []importTask, overridesPrefixes []string, cfFiles []cfManifestFile, err error) {
	switch detected.Format {
	case FormatMrpack:
		f := findZipFile(zr, "modrinth.index.json")
		if f == nil {
			return nil, nil, nil, fmt.Errorf("modrinth.index.json не знайдено в архіві")
		}
		idx, err := readMrpackIndex(f)
		if err != nil {
			return nil, nil, nil, err
		}
		for _, mf := range mrpackClientFiles(idx.Files) {
			tasks = append(tasks, importTask{
				relPath:  mf.Path,
				url:      firstNonEmptyStr(mf.Downloads),
				sha1:     mf.Hashes["sha1"],
				fileSize: mf.FileSize,
			})
		}
		return tasks, []string{"overrides/", "client-overrides/"}, nil, nil

	case FormatCurseForge:
		f := findZipFile(zr, "manifest.json")
		if f == nil {
			return nil, nil, nil, fmt.Errorf("manifest.json не знайдено в архіві")
		}
		m, err := readCFManifest(f)
		if err != nil {
			return nil, nil, nil, err
		}
		if m.Overrides != "" {
			overridesPrefixes = []string{m.Overrides + "/"}
		} else {
			overridesPrefixes = []string{"overrides/"}
		}
		return nil, overridesPrefixes, m.Files, nil

	case FormatGeneric:
		// Немає маніфесту — весь вміст архіву трактуємо як overrides
		// (копіюємо структуру як є у .minecraft/), без окремого
		// довантаження файлів мережею.
		return nil, []string{""}, nil, nil

	default:
		return nil, nil, nil, fmt.Errorf("невідомий формат архіву")
	}
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

	id := strings.TrimSpace(p.ExistingID)
	if id == "" {
		id = strings.TrimSpace(p.PreassignedID)
	}
	if id == "" {
		id = store.UniqueID(name)
	}
	packDir := filepath.Join(instanceDir, id)
	gameDir := filepath.Join(packDir, ".minecraft")
	if err := os.MkdirAll(gameDir, 0755); err != nil {
		return Pack{}, fmt.Errorf("не вдалося створити теку збірки: %w", err)
	}
	// Windows: тека збірки не індексується Windows Search — інакше при
	// встановленні (розпаковка сотень/тисяч модів і асетів) індексатор
	// гальмує провідник.
	config.MarkNotIndexed(packDir)
	config.MarkNotIndexed(gameDir)

	// Джерело збірки: явно задане (браузер контенту — modrinth/curseforge)
	// або звичайний імпорт з файлу (import).
	source := strings.TrimSpace(p.Source)
	if source == "" {
		source = "import"
	}

	// Реєструємо збірку в Store ОДРАЗУ (ще до довантаження модів), щоб вона
	// з'явилась у GetBuilds/сайдбарі/на сторінці збірки з прогресом, поки
	// йде імпорт. Інакше після ImportModpackArchive (що повертає ID миттєво)
	// фронтенд не знаходив би збірку в списку і вікно установки (як у
	// Shaurma-збірок) не показалось би — лише готова збірка після всього.
	// Фінальний запис (з іконкою) зберігається наприкінці — Save оновлює
	// той самий ID і зберігає CreatedAt.
	placeholder := Pack{
		ID:                id,
		Name:              name,
		Icon:              p.Icon,
		Color:             p.Color,
		MCVersion:         detected.MCVersion,
		Loader:            string(detected.Loader),
		LoaderVersion:     detected.LoaderVersion,
		Source:            source,
		ImportFile:        filepath.Base(archivePath),
		ArchivePath:       p.ArchivePath,
		SourceProjectID:   p.SourceProjectID,
		SourceVersionID:   p.SourceVersionID,
		SourceProjectSlug: p.SourceProjectSlug,
		SourceArchiveURL:  p.SourceArchiveURL,
	}
	// IconPath у placeholder одразу — раніше цей рядок був лише для
	// BackgroundSrcPath, тож фон з'являвся у картці/сайдбарі одразу під
	// час імпорту (як «Shaurma»-збірки), а іконка — лише в кінці функції
	// (рядок з pack.IconPath нижче). Якщо імпорт довго тривав (завантаження
	// модів) або обривався помилкою до фінального Save, placeholder без
	// IconPath лишався видимим у UI — фон показувався, іконка ні, хоча
	// сам файл іконки вже лежав у packDir (переданий з browser_api.go при
	// CreateBrowserModpack).
	if p.IconSrcPath != "" {
		placeholder.IconPath = p.IconSrcPath
	}
	if p.BackgroundSrcPath != "" {
		placeholder.BackgroundPath = p.BackgroundSrcPath
	}
	if err := store.Save(placeholder); err != nil {
		return Pack{}, fmt.Errorf("не вдалося зареєструвати збірку: %w", err)
	}

	// Якщо імпорт обірветься (помилка/скасування) — прибираємо тимчасовий
	// запис, щоб у списку не лишалась "фантомна" збірка, яку не можна
	// встановити. Часткові файли на диску лишаються (як при скасуванні).
	// Для встановлення в ІСНУЮЧУ збірку (ExistingID) запис не видаляємо:
	// картка вже була у списку як «не завантажена», клік «Завантажити»
	// просто не вийшов — лишаємо картку, щоб можна було спробувати знову.
	committed := false
	defer func() {
		// Фантомний запис прибираємо лише при РЕАЛЬНІЙ помилці (не при
		// скасуванні користувачем). Після паузи (PauseDownload) запис має
		// ЛИШИТИСЬ — картка показує «Пауза»/«Продовжити», а «Продовжити»
		// відновлює імпорт у той самий pack ID. Повне скасування прибирає
		// запис сам (CancelModpackImport), коли треба.
		if !committed && p.ExistingID == "" && ctx.Err() == nil {
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

	var (
		tasks             []importTask
		overridesPrefixes []string
		cfFiles           []cfManifestFile
	)
	tasks, overridesPrefixes, cfFiles, err = planImport(&zr.Reader, detected)
	if err != nil {
		return Pack{}, err
	}

	// ── Розпаковка overrides напряму в .minecraft/ теки збірки ──
	if ctx.Err() != nil {
		return Pack{}, ctx.Err()
	}
	report(ImportProgress{Stage: StageExtracting})
	if _, err := extractOverrides(ctx, &zr.Reader, overridesPrefixes, gameDir, false, report); err != nil {
		return Pack{}, fmt.Errorf("не вдалося розпакувати вміст архіву: %w", err)
	}

	// ── CurseForge: резолвимо projectID/fileID у прямі URL перед чергою ──
	// ВАЖЛИВО (скарга «0/0 файлів і розпакування безкінечно»): резолв іде
	// ПОСЛІДОВНО по одному запиту на мод (20s таймаут + 3 ретраї кожен), і
	// БЕЗ жодних подій прогресу — на великих модпаках (50+ модів) екран
	// встановлення висів на "Розпаковка…" з "0/0 файлів" на кілька хвилин,
	// виглядаючи як вічне зависання. Тепер кожен резолвнутий/пропущений
	// мод публікує прогрес: FilesTotal = кількість модів маніфесту,
	// FilesCompleted = скільки вже оброблено, CurrentFile = назва мода.
	var blockedModsSkipped []BlockedModInfo
	if detected.Format == FormatCurseForge && len(cfFiles) > 0 {
		cf := NewCurseForgeClient()
		if !cf.HasAPIKey() {
			return Pack{}, fmt.Errorf("модпак використовує CurseForge (%d модів), але API-ключ CurseForge не налаштовано в цій збірці лаунчера — довантаження модів неможливе. Overrides вже розпаковано, теку можна доповнити модами вручну", len(cfFiles))
		}
		report(ImportProgress{
			Stage:          StageDownloading,
			CurrentFile:    "Резолв модів CurseForge…",
			FilesTotal:     len(cfFiles),
			FilesCompleted: 0,
		})
		// ПАРАЛЕЛЬНИЙ резолв: усі (projectID, fileID) → прямі URL одночасно
		// (замість послідовного циклу, який на великих модпаках виглядав
		// як вічне зависання — див. resolveCFFiles).
		resolved, blocked, err := resolveCFFiles(ctx, cf, cfFiles, maxConcurrent, report)
		if err != nil {
			return Pack{}, err
		}
		tasks = append(tasks, resolved...)
		// Заблоковані автором моди: питаємо користувача (діалог як у Prism) —
		// Modrinth fallback / пропустити / скачати вручну / скасувати. Без
		// хендлера (nil) — мовчазний Modrinth fallback, як було раніше.
		tasks, blockedModsSkipped, err = resolveBlockedMods(ctx, p.BlockedHandler, p.UseModrinthForBlocked, blocked, tasks, report)
		if err != nil {
			if len(blockedModsSkipped) > 0 {
				minimalPack := Pack{
					ID:          id,
					Name:        name,
					MCVersion:   detected.MCVersion,
					Loader:      string(detected.Loader),
					Source:      source,
					BlockedMods: blockedModsSkipped,
				}
				_ = store.Save(minimalPack)
			}
			return Pack{}, err
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

	// Список файлів, що прийшли З МАНІФЕСТУ (шляхи відносно .minecraft/):
	// завантажені моди + розпаковані overrides. Зберігається у Pack.SourceFiles
	// і використовується «Оновити з джерела» (diff проти нового маніфесту).
	manifestFiles := collectManifestPaths(tasks, overridesPrefixes, &zr.Reader, detected.Format)

	pack := Pack{
		ID:                id,
		Name:              name,
		Icon:              p.Icon,
		Color:             p.Color,
		MCVersion:         detected.MCVersion,
		Loader:            string(detected.Loader),
		LoaderVersion:     detected.LoaderVersion,
		Source:            source,
		ImportFile:        filepath.Base(archivePath),
		ArchivePath:       p.ArchivePath,
		SourceProjectID:   p.SourceProjectID,
		SourceVersionID:   p.SourceVersionID,
		SourceProjectSlug: p.SourceProjectSlug,
		SourceArchiveURL:  p.SourceArchiveURL,
		SourceFiles:       manifestFiles,
		BlockedMods:       blockedModsSkipped,
	}
	if iconSrc != "" {
		// Якщо іконка вже лежить у теці збірки під фіксованим іменем
		// (вбудована з архіву вийнята на початку) — не копіюємо саму в
		// себе (copyAssetInto обнулив би файл: os.Create до читання).
		if !filepath.IsAbs(iconSrc) {
			// extractEmbeddedIcon повернув відносний шлях — файл вже в packDir.
			pack.IconPath = iconSrc
		} else if dst, err := copyAssetInto(packDir, "icon", iconSrc); err == nil {
			pack.IconPath = dst
		}
	}
	if p.BackgroundSrcPath != "" {
		// Той самий принцип, що для іконки: фон, завантажений браузером
		// контенту, уже лежить у packDir під фіксованим іменем — просто
		// переносимо шлях, не копіюємо файл сам у себе.
		if samePathWithin(p.BackgroundSrcPath, packDir) {
			pack.BackgroundPath = filepath.Base(p.BackgroundSrcPath)
		} else if dst, err := copyAssetInto(packDir, "background", p.BackgroundSrcPath); err == nil {
			pack.BackgroundPath = dst
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

// resolveCFFiles резолвить список (projectID, fileID) у прямі URL модів
// CurseForge ПАРАЛЕЛЬНО (maxConcurrent воркерів) із збереженням порядку
// маніфесту. Раніше резолв ішов ПОСЛІДОВНО — один запит до API за раз, до
// 20 секунд таймауту кожен + ретраї на 429: на модпаку з 50-100 модами це
// 5-30 хвилин «мертвого» екрану встановлення (екран виглядав завислим,
// хоча прогрес і публікувався). Тепер усі запити йдуть паралельно, а
// помилки обов'язкових модів збираються в одну — без втрати порядку і без
// стрибків прогресу (кожен мод публікує подію по завершенню резолву).
//
// Повертає tasks (у порядку cfFiles) і кількість пропущених опційних модів.
// Обов'язковий мод, що не резолвнувся, валить весь імпорт з помилкою
// (та сама семантика, що й у послідовному циклі раніше).
func resolveCFFiles(ctx context.Context, cf *CurseForgeClient, cfFiles []cfManifestFile, maxConcurrent int, report func(ImportProgress)) ([]importTask, []BlockedCFMod, error) {
	tasks := make([]importTask, len(cfFiles))
	resolved := make([]bool, len(cfFiles))
	type fail struct {
		idx int
		err error
	}
	failCh := make(chan fail, 1)
	var done int32
	// Заблоковані автором моди збираємо під м'ютексом (воркери пишуть паралельно).
	var blockedMu sync.Mutex
	var blocked []BlockedCFMod

	// ДОЧІРНІЙ контекст з cancel: щойно обов'язковий мод впав — скасовуємо
	// решту запитів, що висять, і wg.Wait() завершується швидко. Без цього
	// горутини, що вже в польоті, продовжували б публікувати sync:progress
	// ПІСЛЯ повернення помилки — фронтенд «воскрешав» би збірку зі статусом
	// downloading після download:error (див. рев'ю resolveCFFiles).
	rctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup
	for i, mf := range cfFiles {
		if rctx.Err() != nil {
			break // помилка вже сталася (cancel викликано) — не запускаємо нові запити
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, mf cfManifestFile) {
			defer crashguard.Recover("imports.go:920")
			defer wg.Done()
			defer func() { <-sem }()
			info, err := cf.GetModFileCtx(rctx, mf.ProjectID, mf.FileID)
			if err != nil {
				if rctx.Err() != nil {
					return // скасовано через інший впалий мод — без подій прогресу
				}
				if mf.Required {
					select {
					case failCh <- fail{idx: idx, err: fmt.Errorf("обов'язковий мод (проєкт %d, файл %d): %w", mf.ProjectID, mf.FileID, err)}:
						cancel()
					default:
					}
					return
				}
				// опційний мод, що не резолвнувся — пропускаємо
				if report != nil {
					report(ImportProgress{
						Stage:          StageDownloading,
						CurrentFile:    fmt.Sprintf("Мод %d/%d (пропущено): проєкт %d, файл %d", idx+1, len(cfFiles), mf.ProjectID, mf.FileID),
						FilesTotal:     len(cfFiles),
						FilesCompleted: int(atomic.AddInt32(&done, 1)),
					})
				}
				return
			}
			// Заблокований автором файл (3rd party download opt-out): НЕ
			// вирішуємо тут — збираємо у список і повертаємо викликачу. Діалог
			// (як BlockedModsDialog у Prism) покаже користувачу список, і він
			// обере: Modrinth fallback за sha1 / пропустити / скасувати.
			if info.Blocked {
				// Посилання для РУЧНОГО скачування — у Prism-стилі:
				// {websiteUrl}/download/{fileId} (FlameInstanceCreationTask:
				// blocked_mod.websiteUrl = "%1/download/%2".arg(pack.websiteUrl,
				// fileId)). Старий формат /projects/{id}/files/{id} віддавав
				// 404 — це було «пряме посилання, яке завжди дає 404».
				// websiteUrl беремо з API (links.websiteUrl містить slug), при
				// збої — фолбек на числовий mc-mods-шлях (CurseForge
				// редиректить числові ID на slug-сторінку).
				websiteURL := cf.GetProjectWebsiteURL(rctx, mf.ProjectID)
				if websiteURL == "" {
					websiteURL = fmt.Sprintf("https://www.curseforge.com/minecraft/mc-mods/%d", mf.ProjectID)
				}
				blockedMu.Lock()
				blocked = append(blocked, BlockedCFMod{
					ProjectID:  mf.ProjectID,
					FileID:     mf.FileID,
					FileName:   info.FileName,
					SHA1:       info.SHA1,
					WebsiteURL: fmt.Sprintf("%s/download/%d", websiteURL, mf.FileID),
					Required:   mf.Required,
				})
				blockedMu.Unlock()
				if report != nil && rctx.Err() == nil {
					report(ImportProgress{
						Stage:          StageDownloading,
						CurrentFile:    fmt.Sprintf("Мод %d/%d: %s (заблоковано автором)", idx+1, len(cfFiles), info.FileName),
						FilesTotal:     len(cfFiles),
						FilesCompleted: int(atomic.AddInt32(&done, 1)),
					})
				}
				return
			}
			tasks[idx] = importTask{
				relPath:  "mods/" + info.FileName,
				url:      info.DownloadURL,
				sha1:     info.SHA1,
				fileSize: info.FileLength,
			}
			resolved[idx] = true
			if report != nil && rctx.Err() == nil {
				report(ImportProgress{
					Stage:          StageDownloading,
					CurrentFile:    fmt.Sprintf("Мод %d/%d: %s", idx+1, len(cfFiles), info.FileName),
					FilesTotal:     len(cfFiles),
					FilesCompleted: int(atomic.AddInt32(&done, 1)),
				})
			}
		}(i, mf)
	}
	wg.Wait()

	select {
	case e := <-failCh:
		cancel()
		return nil, nil, e.err
	default:
	}
	if ctx.Err() != nil {
		return nil, nil, ctx.Err()
	}

	// Стискаємо масив: лише розв'язані моди (порядок маніфесту збережено).
	out := make([]importTask, 0, len(tasks))
	for i, t := range tasks {
		if resolved[i] {
			out = append(out, t)
		}
	}
	return out, blocked, nil
}

// tryModrinthBlockedFallback шукає заблокований CurseForge-мод на Modrinth
// за sha1 (той самий підхід, що FallbackMRBlockedMods у Prism Launcher:
// FileResolvingTask::netJobFinished резолвить sha1 через /version_file).
// Реалізацію реєструє app.go (SetModrinthBlockedResolver) — там уже є
// ModrinthClient.GetVersionByHash; nil резолвер = fallback вимкнено.
func tryModrinthBlockedFallback(ctx context.Context, sha1hex string) (*ModrinthMirror, error) {
	if sha1hex == "" || modrinthResolver == nil {
		return nil, nil
	}
	return modrinthResolver(ctx, sha1hex)
}

// resolveBlockedMods застосовує рішення по заблокованих модах (діалог як у
// Prism). Двоетапний конвеєр, щоб «моди, яких немає на Modrinth, не губилися
// мовчки і не валили весь імпорт»:
//
//	ЕТАП 1 — рішення по всіх заблокованих:
//	  * useModrinthForBlocked=true  → Modrinth fallback БЕЗ діалогу (згода
//	    уже дана галочкою в налаштуваннях),
//	  * інакше → діалог (handler, allowModrinth=true): modrinth / manual /
//	    skip / cancel.
//	ЕТАП 2 — якщо після спроби Modrinth ЗАЛИШИЛИСЬ моди, яких там немає:
//	  * діалог показується ВДРУГЕ, але з allowModrinth=false — «усе, що могло
//	    завантажитись з Modrinth, уже завантажене, далі лише ручне встановлення»
//	    (manual / skip / cancel). Раніше обов'язковий незнайдений мод валив
//	    весь імпорт помилкою, а опційний тихо зникав — імпорт «зависав» на
//	    очікуванні замість того, щоб знову спитати користувача.
//
// Якщо handler == nil (виклик без UI, напр. тести) — мовчазний Modrinth
// fallback за замовчуванням; незнайдений обов'язковий мод валить імпорт
// з посиланням для ручного скачування, опційний — тихо пропускається.
// Повертає також список BlockedModInfo — моди, що лишились незавантаженими
// після всіх fallback (зберігаються в Pack.BlockedMods для попередження
// на сторінці збірки).
func resolveBlockedMods(ctx context.Context, handler BlockedHandlerFunc, useModrinthForBlocked bool, blocked []BlockedCFMod, tasks []importTask, report func(ImportProgress)) ([]importTask, []BlockedModInfo, error) {
	if len(blocked) == 0 {
		return tasks, nil, nil
	}
	decision := BlockedDecisionModrinth
	if handler != nil && !useModrinthForBlocked {
		d, err := handler(ctx, blocked, true)
		if err != nil {
			var skipped []BlockedModInfo
			for _, b := range blocked {
				skipped = append(skipped, BlockedModInfo{
					FileName:   b.FileName,
					WebsiteURL: b.WebsiteURL,
					Required:   b.Required,
					Sha1:       b.SHA1,
				})
			}
			return nil, skipped, err
		}
		decision = d
	}
	switch decision {
	case BlockedDecisionCancel:
		var skipped []BlockedModInfo
		for _, b := range blocked {
			skipped = append(skipped, BlockedModInfo{
				FileName:   b.FileName,
				WebsiteURL: b.WebsiteURL,
				Required:   b.Required,
				Sha1:       b.SHA1,
			})
		}
		return nil, skipped, nil
	case BlockedDecisionSkip, BlockedDecisionManual:
		// manual = skip на рівні конвеєра: посилання для ручного скачування
		// показав діалог, імпорт продовжується з рештою модів.
		var skipped []BlockedModInfo
		for _, b := range blocked {
			skipped = append(skipped, BlockedModInfo{
				FileName:   b.FileName,
				WebsiteURL: b.WebsiteURL,
				Required:   b.Required,
				Sha1:       b.SHA1,
			})
		}
		return tasks, skipped, nil
	default: // modrinth (обраний у діалозі АБО авто через useModrinthForBlocked)
		var stillBlocked []BlockedCFMod
		for i, b := range blocked {
			if ctx.Err() != nil {
				return nil, nil, ctx.Err()
			}
			// Прогрес fallback: без нього «скачати з Modrinth» виглядало
			// завислим (мовчазні мережеві запити без подій).
			if report != nil {
				report(ImportProgress{
					Stage:          StageDownloading,
					CurrentFile:    fmt.Sprintf("Пошук на Modrinth: %s (%d/%d)", b.FileName, i+1, len(blocked)),
					FilesTotal:     len(blocked),
					FilesCompleted: i,
				})
			}
			mirror, merr := tryModrinthBlockedFallback(ctx, b.SHA1)
			if merr == nil && mirror != nil && mirror.URL != "" {
				tasks = append(tasks, importTask{
					relPath: "mods/" + mirror.Filename,
					url:     mirror.URL,
					sha1:    b.SHA1,
				})
				continue
			}
			stillBlocked = append(stillBlocked, b)
		}
		// Етап 2: усе, що Modrinth зміг дати, вже в черзі. Якщо щось
		// лишилось — діалог ВДРУГЕ, але БЕЗ опції Modrinth (усе, що можна,
		// вже скачано, далі лише ручне встановлення / пропуск / скасування).
		if len(stillBlocked) == 0 {
			return tasks, nil, nil
		}
		if handler != nil {
			d2, err := handler(ctx, stillBlocked, false)
			if err != nil {
				var skipped []BlockedModInfo
				for _, b := range stillBlocked {
					skipped = append(skipped, BlockedModInfo{
						FileName:   b.FileName,
						WebsiteURL: b.WebsiteURL,
						Required:   b.Required,
						Sha1:       b.SHA1,
					})
				}
				return nil, skipped, err
			}
			switch d2 {
			case BlockedDecisionCancel:
				var skipped []BlockedModInfo
				for _, b := range stillBlocked {
					skipped = append(skipped, BlockedModInfo{
						FileName:   b.FileName,
						WebsiteURL: b.WebsiteURL,
						Required:   b.Required,
						Sha1:       b.SHA1,
					})
				}
				return nil, skipped, nil
			case BlockedDecisionManual, BlockedDecisionSkip:
				var skipped []BlockedModInfo
				for _, b := range stillBlocked {
					skipped = append(skipped, BlockedModInfo{
						FileName:   b.FileName,
						WebsiteURL: b.WebsiteURL,
						Required:   b.Required,
						Sha1:       b.SHA1,
					})
				}
				return tasks, skipped, nil
			default:
				// modrinth у другому раунді неможливий (кнопки немає) —
				// підстраховка: без них.
				return tasks, nil, nil
			}
		}
		// handler == nil: незнайдений обов'язковий мод валить імпорт зі
		// зрозумілою помилкою (посилання для ручного скачування), опційний
		// — тихо пропускається.
		for _, b := range stillBlocked {
			if b.Required {
				return nil, nil, fmt.Errorf("обов'язковий мод «%s»: автор заборонив сторонні завантаження, а Modrinth-дублікат за sha1 не знайдено. Скачай його вручну зі сторінки %s і поклади у теку mods", b.FileName, b.WebsiteURL)
			}
		}
		var skipped []BlockedModInfo
		for _, b := range stillBlocked {
			skipped = append(skipped, BlockedModInfo{
				FileName:   b.FileName,
				WebsiteURL: b.WebsiteURL,
				Required:   b.Required,
				Sha1:       b.SHA1,
			})
		}
		return tasks, skipped, nil
	}
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
//
// Resume/повторний запуск: файли, що вже розпаковані (існують на диску з
// тим самим розміром), пропускаються — раніше пауза→продовження заново
// розпаковувало ВСІ overrides, і екран знову показував «Розпакування…»
// навіть коли все вже було розпаковано.
//
// ctx: якщо не nil — перевіряється на кожному файлі (пауза/скасування
// перериває розпаковку без зависання; уже розпаковані файли лишаються).
//
// Прогрес: розпаковка великих CurseForge-збірок (сотні файлів overrides —
// ресурспаки, шейдери, конфіги) йшла МОВЧКИ: без жодних подій UI висів на
// «Розпаковка…» з нульовими лічильниками, схоже на вічне зависання. Тепер
// на кожен розпакований файл публікується подія (Stage=extracting з
// FilesTotal/FilesCompleted/CurrentFile), і користувач бачить живий прогрес,
// а не заморожений екран.
// protectedOverridePaths — теки/файли гравця, які extractOverrides НЕ
// перезаписує під час ОНОВЛЕННЯ (isUpdate=true), якщо вони вже існують на
// диску. Дзеркалить дефолти on_missing/first_install_only з офіційних
// Shaurma-збірок (model.DefaultSyncRules у pack-manager), але тут це
// вбудований локальний дефолт — кастомні збірки (Modrinth/CurseForge)
// не мають серверних SyncRules узагалі, і extractOverrides раніше
// перезаписувала буквально все, включно з config/ і options.txt гравця,
// при кожному натисканні «Оновити з джерела».
var protectedOverridePaths = []string{
	"config/",
	"resourcepacks/",
	"shaderpacks/",
	"options.txt",
	"servers.dat",
}

func isProtectedOverridePath(rel string) bool {
	relSlash := filepath.ToSlash(rel)
	for _, p := range protectedOverridePaths {
		if strings.HasSuffix(p, "/") {
			if relSlash == strings.TrimSuffix(p, "/") || strings.HasPrefix(relSlash, p) {
				return true
			}
		} else if relSlash == p {
			return true
		}
	}
	return false
}

// extractOverrides розпаковує overrides/ (mrpack) чи еквівалент
// (CurseForge overrides, generic archive) у gameDir.
//
// isUpdate=false (первинний імпорт, ImportArchive): нічого ще не
// встановлено — розпаковуємо все безумовно, як і раніше.
//
// isUpdate=true (UpdatePackFromSource, кнопка «Оновити з джерела»):
// файли/теки з protectedOverridePaths, які ВЖЕ існують на диску, НЕ
// перезаписуються — гравець міг змінити options.txt чи config/ поверх
// дефолтів збірки з моменту останнього оновлення, і ці зміни не мають
// зникати щоразу після «Оновити з джерела».
func extractOverrides(ctx context.Context, zr *zip.Reader, prefixes []string, gameDir string, isUpdate bool, report func(ImportProgress)) ([]string, error) {
	// Спершу збираємо список файлів для розпаковки — щоб прогресу було відомо
	// точне FilesTotal і послідовність обробки до початку запису на диск.
	type entry struct {
		f   *zip.File
		rel string
	}
	var entries []entry
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
		entries = append(entries, entry{f: f, rel: rel})
	}

	var skipped []string
	for i, e := range entries {
		if ctx != nil {
			select {
			case <-ctx.Done():
				return skipped, ctx.Err()
			default:
			}
		}
		dest := filepath.Join(gameDir, filepath.FromSlash(e.rel))
		// Оновлення (не перший імпорт): не чіпаємо «файл гравця» —
		// config/, options.txt тощо — якщо він уже є на диску. Перевіряємо
		// ЦЕ ПЕРШИМ, до resume-перевірки за розміром, бо розмір нового і
		// старого options.txt може випадково збігтись, а сенс тут інший:
		// не займати файл незалежно від того, resume це чи ні.
		if isUpdate && isProtectedOverridePath(e.rel) {
			if fi, err := os.Stat(dest); err == nil && !fi.IsDir() {
				skipped = append(skipped, e.rel)
				launchlog.Info("Sync", "не чіпаю (файл гравця, вже на диску): %s", e.rel)
				continue
			}
		}
		// Resume: файл уже розпакований з тим самим розміром — пропускаємо.
		// Порівняння розміру (а не лише наявності) захищає від «напіврозпакованого»
		// файла, якщо крах станеться всередині io.Copy.
		if fi, err := os.Stat(dest); err == nil && !fi.IsDir() && fi.Size() == int64(e.f.UncompressedSize64) {
			continue
		}
		if report != nil {
			report(ImportProgress{
				Stage:          StageExtracting,
				CurrentFile:    filepath.Base(e.f.Name),
				FilesTotal:     len(entries),
				FilesCompleted: i,
			})
		}
		if err := extractZipEntryTo(e.f, gameDir, e.rel); err != nil {
			return skipped, err
		}
	}
	if report != nil {
		report(ImportProgress{Stage: StageExtracting, FilesTotal: len(entries), FilesCompleted: len(entries)})
	}
	return skipped, nil
}

// UpdatePackFromSource оновлює «інтернет-збірку» новою версією модпаку з
// джерела (кнопка «Оновити з джерела» в контекстному меню):
//
//  1. планує НОВИЙ маніфест (planImport — той самий код, що перше встановлення);
//  2. diff старих SourceFiles проти нового маніфесту — файли, що зникли з
//     нової версії (моди/конфіги), видаляються з теки збірки;
//  3. розпаковує нові overrides і довантажує нові моди;
//  4. оновлює Pack: MCVersion/Loader/LoaderVersion беруться З НОВОЇ ВЕРСІЇ
//     (як просив ужиток — «підбираємо вже той лоадер і та версія»),
//     SourceVersionID — нова версія, SourceFiles — новий список.
//
// Користувацькі доповнення (моди, поставлені вручну після встановлення)
// не чіпаються: видаляються лише файли зі СТАРОГО маніфесту, яких немає
// в новому. Спільні шляхи перезаписуються новими файлами.
func UpdatePackFromSource(ctx context.Context, store *Store, instanceDir string, p Pack, archivePath string, detected *DetectedPack, newVersionID string, maxConcurrent int, blockedHandler BlockedHandlerFunc, useModrinthForBlocked bool, onProgress ImportProgressFunc) (Pack, error) {
	id := p.ID
	if id == "" {
		return Pack{}, fmt.Errorf("порожній ID збірки")
	}
	packDir := filepath.Join(instanceDir, id)
	gameDir := filepath.Join(packDir, ".minecraft")
	if err := os.MkdirAll(gameDir, 0755); err != nil {
		return Pack{}, fmt.Errorf("не вдалося створити теку збірки: %w", err)
	}
	// Windows: тека збірки не індексується Windows Search — інакше при
	// встановленні (розпаковка сотень/тисяч модів і асетів) індексатор
	// гальмує провідник.
	config.MarkNotIndexed(packDir)
	config.MarkNotIndexed(gameDir)

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

	tasks, overridesPrefixes, cfFiles, err := planImport(&zr.Reader, detected)
	if err != nil {
		report(ImportProgress{Stage: "", Done: true, Error: err})
		return Pack{}, err
	}

	// ── CurseForge: резолвимо projectID/fileID у прямі URL перед чергою ──
	// Той самий прогрес під час послідовного резолву, що й в ImportArchive
	// (скарга «0/0 файлів і розпакування безкінечно» при оновленні CF-збірок).
	var blockedModsSkipped []BlockedModInfo
	if detected.Format == FormatCurseForge && len(cfFiles) > 0 {
		cf := NewCurseForgeClient()
		if !cf.HasAPIKey() {
			report(ImportProgress{Stage: "", Done: true, Error: err})
			return Pack{}, fmt.Errorf("модпак використовує CurseForge (%d модів), але API-ключ CurseForge не налаштовано в цій збірці лаунчера — оновлення неможливе", len(cfFiles))
		}
		report(ImportProgress{
			Stage:          StageDownloading,
			CurrentFile:    "Резолв модів CurseForge…",
			FilesTotal:     len(cfFiles),
			FilesCompleted: 0,
		})
		// Паралельний резолв (той самий фікс зависання, що й в ImportArchive).
		resolved, blocked, err := resolveCFFiles(ctx, cf, cfFiles, maxConcurrent, report)
		if err != nil {
			report(ImportProgress{Stage: "", Done: true, Error: err})
			return Pack{}, err
		}
		tasks = append(tasks, resolved...)
		// Заблоковані автором моди: той самий діалог рішення, що й при імпорті.
		tasks, blockedModsSkipped, err = resolveBlockedMods(ctx, blockedHandler, useModrinthForBlocked, blocked, tasks, report)
		if err != nil {
			if len(blockedModsSkipped) > 0 {
				p.BlockedMods = blockedModsSkipped
				_ = store.Save(p)
			}
			report(ImportProgress{Stage: "", Done: true, Error: err})
			return Pack{}, err
		}
	}

	// Новий список файлів маніфесту — diff проти старого.
	newManifestFiles := collectManifestPaths(tasks, overridesPrefixes, &zr.Reader, detected.Format)

	// ── Стадія extracting: видаляємо файли старої версії, що зникли ──
	report(ImportProgress{Stage: StageExtracting, CurrentFile: "diff"})
	removed := 0
	for _, oldPath := range p.SourceFiles {
		if containsPath(newManifestFiles, oldPath) {
			continue // файл є в новій версії — перезапишеться нижче
		}
		oldAbs := filepath.Join(gameDir, filepath.FromSlash(oldPath))
		if fileExists(oldAbs) {
			if err := os.Remove(oldAbs); err == nil {
				removed++
			}
		}
	}

	// ── Розпаковка нових overrides напряму в .minecraft/ ──
	if ctx.Err() != nil {
		report(ImportProgress{Stage: "", Done: true, Error: ctx.Err()})
		return Pack{}, ctx.Err()
	}
	skippedPaths, err := extractOverrides(ctx, &zr.Reader, overridesPrefixes, gameDir, true, report)
	if err != nil {
		report(ImportProgress{Stage: "", Done: true, Error: err})
		return Pack{}, fmt.Errorf("не вдалося розпакувати вміст архіву: %w", err)
	}
	if len(skippedPaths) > 0 {
		launchlog.Info("Sync", "[%s] оновлення з джерела: НЕ чіпаю %d файл(ів) гравця: %s",
			id, len(skippedPaths), strings.Join(skippedPaths, ", "))
	}

	// ── Довантаження нових модів ──
	if len(tasks) > 0 {
		if err := downloadImportTasks(ctx, tasks, gameDir, maxConcurrent, id, report); err != nil {
			report(ImportProgress{Stage: "", Done: true, Error: err})
			return Pack{}, err
		}
	}

	report(ImportProgress{Stage: StageFinalizing})

	// Оновлюємо запис: версія гри/лоадер — З НОВОЇ ВЕРСІЇ модпаку,
	// SourceVersionID — нова версія, SourceFiles — новий список.
	updated := p
	updated.MCVersion = detected.MCVersion
	updated.Loader = string(detected.Loader)
	updated.LoaderVersion = detected.LoaderVersion
	updated.SourceVersionID = newVersionID
	updated.SourceFiles = newManifestFiles
	updated.BlockedMods = blockedModsSkipped
	updated.UpdatedAt = time.Now()
	if err := store.Save(updated); err != nil {
		report(ImportProgress{Stage: "", Done: true, Error: err})
		return Pack{}, err
	}

	report(ImportProgress{Stage: StageComplete, Done: true, SkippedPaths: skippedPaths})
	return updated, nil
}

func containsPath(list []string, p string) bool {
	p = strings.TrimPrefix(strings.TrimPrefix(p, "./"), "/")
	for _, x := range list {
		if strings.TrimPrefix(strings.TrimPrefix(x, "./"), "/") == p {
			return true
		}
	}
	return false
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

// fileMatchesSHA1 перевіряє, чи файл на диску відповідає очікуваному
// SHA1-хешу. Використовується при HTTP 416 (Range не задоволено): якщо
// .part уже містить ПОВНИЙ файл (попередня спроба впала на таймауті до
// перейменування), перейменовуємо без повторного качання.
func fileMatchesSHA1(path, wantSHA1 string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return false
	}
	sum := hex.EncodeToString(h.Sum(nil))
	return strings.EqualFold(sum, wantSHA1)
}

// collectManifestPaths збирає повний список файлів, що прийшли з маніфесту
// модпаку: завантажені моди (tasks[].relPath) + розпаковані overrides
// (шляхи всередині архіву під overrides-префіксами, без префіксу).
// Використовується для Pack.SourceFiles — «Оновити з джерела» видаляє
// ті, що зникли з нового маніфесту.
func collectManifestPaths(tasks []importTask, overridesPrefixes []string, zr *zip.Reader, format ImportFormat) []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		p = strings.TrimPrefix(p, "./")
		p = strings.TrimPrefix(p, "/")
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		out = append(out, p)
	}
	for _, t := range tasks {
		add(t.relPath)
	}
	// Overrides: копіюємо ті самі правила, що extractOverrides (без
	// actual-копіювання — лише збір шляхів).
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := f.Name
		rel := ""
		matched := false
		for _, prefix := range overridesPrefixes {
			if prefix == "" {
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
		add(rel)
	}
	return out
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
	if shsync.ValidateRelPath(relPath) != nil {
		return nil // диск/NUL/виконуваний файл (.exe/.bat…) — не розпаковуємо
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
	_, err = io.Copy(out, io.LimitReader(rc, 2<<30)) // стеля 2 ГБ на файл (zip-бомба)
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

	// ДОЧІРНІЙ контекст з cancel: щойно перший файл впав — скасовуємо решту
	// вантажень, що ще в польоті, щоб wg.Wait() не чекав їх до 5-хв таймауту
	// HTTP-клієнта (інакше помилка одного файла з'являлась би через хвилини,
	// і імпорт виглядав «завислим без причини»).
	dctx, dcancel := context.WithCancel(ctx)
	defer dcancel()

	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup
	// HTTP-клієнт з явним Transport: stallReader (нижче) обриває лише
	// завмерле ТІЛО відповіді, а фаза очікування ЗАГОЛОВКІВ (client.Do
	// перед першим байтом) без ResponseHeaderTimeout могла висіти весь
	// 5-хв Timeout клієнта на мовчазному CDN-з'єднанні — «0.0 МБ/с і
	// ніколи не відвисає». Тепер сервер, що прийняв TCP і не шле відповідь,
	// обривається через downloadResponseHeaderTimeout.
	client := &http.Client{
		Timeout: 5 * time.Minute,
		Transport: &http.Transport{
			// Proxy — збереження поведінки дефолтного транспорту (див.
			// NewCurseForgeClient: явний Transport не успадковує
			// ProxyFromEnvironment, тож без нього запити до CDN йшли б в
			// обхід системного/корпоративного проксі).
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   20 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			TLSHandshakeTimeout:   20 * time.Second,
			ResponseHeaderTimeout: downloadResponseHeaderTimeout,
			IdleConnTimeout:       90 * time.Second,
			ForceAttemptHTTP2:     true,
		},
	}

	for _, t := range tasks {
		if dctx.Err() != nil {
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
			defer crashguard.Recover("imports.go:1704")
			defer wg.Done()
			defer func() { <-sem }()

			mu.Lock()
			if firstErr != nil {
				mu.Unlock()
				return
			}
			mu.Unlock()
			if dctx.Err() != nil {
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

			_, err := downloadOneFile(dctx, client, task, gameDir, onBytes)
			if err != nil {
				if dctx.Err() != nil {
					return // скасовано користувачем АБО першою помилкою — без другого повідомлення
				}
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("%s: %w", task.relPath, err)
					dcancel() // прибираємо решту вантажень — помилка вже відома, чекати їх безглуздо
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

// downloadStallTimeout — скільки часу з'єднання може мовчати (жодного
// байта) перед тим, як downloadOneFile вважає його завислим і перезапускає
// завантаження. Скарга «качається, потім зависає на одному моді, швидкість
// 0.0 МБ/с і ніколи не відвисає» — це саме stall-з'єднання: сервер/CDN
// припиняє слати дані посеред файлу, TCP-з'єднання лишається відкритим, і
// io.Copy блокується назавжди (HTTP-таймаут клієнта в 5 хв — лише загальний
// ліміт запиту, тож екран «зависає» на хвилини). stallReader обриває таке
// читання через цей час, а retry-цикл downloadOneFile перезапускає файл з
// місця зупинки (Range + наявний .part) — замість вічного 0.0 МБ/с.
//
// var (не const): тест підміняє на мілісекунди, щоб перевірити stall-логіку
// без 30-секундного очікування.
// downloadResponseHeaderTimeout — скільки чекати перші ЗАГОЛОВКИ відповіді
// (фаза client.Do до першого байта). Сервер, що прийняв TCP і мовчить
// (типовий стіл CDN «швидкість 0.0 МБ/с»), обривається тут, а не висить
// увесь 5-хв Timeout клієнта. var (не const): тест зменшує для швидкої
// перевірки без 45-секундного очікування.
var downloadResponseHeaderTimeout = 20 * time.Second

var downloadStallTimeout = 15 * time.Second

// downloadMaxAttempts — скільки разів downloadOneFile повторює файл при
// тимчасових помилках (stall, обрив, 5xx/429). Кожна спроба докачує з
// місця зупинки через Range, тож повтор не витрачає вже завантажене.
// var (не const): тест зменшує, щоб швидко перевірити вичерпання спроб.
var downloadMaxAttempts = 3

// retryableDownloadError — тимчасова помилка завантаження (стіл з'єднання,
// обрив, 5xx, 429): має сенс повторити спробу з Range-докачкою.
type retryableDownloadError struct{ msg string }

func (e *retryableDownloadError) Error() string { return e.msg }

// fatalDownloadError — постійна помилка (404, 401/403, битий SHA1):
// повторювати безглуздо.
type fatalDownloadError struct{ msg string }

func (e *fatalDownloadError) Error() string { return e.msg }

// stallReader обриває читання з'єднання, якщо за stallTimeout не прийшло
// жодного байта. Таймер перезапускається на кожному прочитаному блоці;
// якщо він спрацював — закриваємо з'єднання (розблокування заблокованого
// Read) і наступні читання повертають retryableDownloadError.
//
// ctx-aware: при скасуванні користувачем (CancelModpackImport/PauseDownload
// — кнопка «Скасувати»/«Пауза» на сторінці збірки) заблокований Read має
// розблокуватись МИТТЄВО, а не чекати downloadStallTimeout. Без цього
// скасування під час завислого з'єднання «зависало» на 30с (користувач
// тисне «Скасувати» — нічого не відбувається).
type stallReader struct {
	r         io.ReadCloser
	timer     *time.Timer
	timeout   time.Duration
	mu        sync.Mutex
	stalled   bool
	done      chan struct{}
	closeOnce sync.Once
}

func newStallReader(ctx context.Context, r io.ReadCloser, timeout time.Duration) *stallReader {
	s := &stallReader{r: r, timeout: timeout, done: make(chan struct{})}
	s.timer = time.AfterFunc(timeout, s.stall)
	// Спостерігач за скасуванням: як тільки ctx скасовано — закриваємо
	// з'єднання так само, як таймер стілу (stall()), тож заблокований
	// s.r.Read нижче поверне помилку і retry-цикл побачить ctx.Err().
	// select на s.done (закривається в Close) гарантує, що горутина ВИЙДЕ
	// одразу після завершення читання, а не чекатиме ctx.Done до кінця
	// життя процесу (ctx імпорту при успішному завершенні не скасовується).
	go func() {
		defer crashguard.Recover("imports.go:1903")
		select {
		case <-ctx.Done():
		case <-s.done:
			return
		}
		s.mu.Lock()
		if !s.stalled {
			s.stalled = true
			s.r.Close()
		}
		s.mu.Unlock()
	}()
	return s
}

func (s *stallReader) stall() {
	s.mu.Lock()
	if !s.stalled {
		s.stalled = true
		// Закриваємо з'єднання — заблокований Read нижче поверне помилку,
		// яку stallReader.Read замінить на retryableDownloadError.
		s.r.Close()
	}
	s.mu.Unlock()
}

func (s *stallReader) Read(b []byte) (int, error) {
	s.mu.Lock()
	stalled := s.stalled
	s.mu.Unlock()
	if stalled {
		return 0, &retryableDownloadError{msg: fmt.Sprintf("з'єднання завмерло (жодного байта за %s)", s.timeout)}
	}
	n, rerr := s.r.Read(b)
	if n > 0 {
		// Блок даних прийшов — перезапускаємо таймер завмирання.
		s.timer.Stop()
		s.timer = time.AfterFunc(s.timeout, s.stall)
	}
	if rerr != nil {
		s.mu.Lock()
		stalled = s.stalled
		s.mu.Unlock()
		if stalled {
			return n, &retryableDownloadError{msg: fmt.Sprintf("з'єднання завмерло (жодного байта за %s)", s.timeout)}
		}
	}
	return n, rerr
}

func (s *stallReader) Close() error {
	s.timer.Stop()
	// Закриваємо done — спостерігач за ctx.Done вийде, навіть якщо ctx
	// імпорту ще живий (успішне завершення завантаження): жодного витоку
	// горутин на кожну спробу/файл.
	s.closeOnce.Do(func() { close(s.done) })
	return s.r.Close()
}

// downloadOneFile качає один файл модпаку з retry-циклом: на тимчасових
// помилках (стіл з'єднання, обрив, 5xx/429) перезапускає спробу, яка
// докачує з місця зупинки через Range + наявний .part. Без цього один
// завислий мод висів на 0.0 МБ/с назавжди і валив весь імпорт по закінченню
// 5-хвилинного таймауту HTTP-клієнта.
func downloadOneFile(ctx context.Context, client *http.Client, task importTask, gameDir string, onBytes func(int64)) (int64, error) {
	cleanRel := filepath.Clean(task.relPath)
	if cleanRel == "." || strings.HasPrefix(cleanRel, "..") || filepath.IsAbs(cleanRel) {
		return 0, &fatalDownloadError{msg: fmt.Sprintf("недопустимий шлях файлу в маніфесті: %s", task.relPath)}
	}
	// Суворіша перевірка: диски/NUL/`\..`, виконувані файли (.exe/.bat/.ps1…).
	if err := shsync.ValidateRelPath(task.relPath); err != nil {
		return 0, &fatalDownloadError{msg: fmt.Sprintf("небезпечний файл у збірці: %v", err)}
	}
	dest := filepath.Join(gameDir, cleanRel)
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return 0, err
	}

	// Resume/повторний запуск: файл уже повністю скачаний і SHA1 збігається —
	// НЕ качаємо з нуля. Раніше пауза→продовження (або повторне «Завантажити»
	// після скасування) перекачувало вже готові моди: .part немає (перейменований
	// у dest), тож downloadOneAttempt стартував з Range=0 і качав весь файл заново.
	// Тепер це ідемпотентно: вже скачаний мод пропускається миттєво.
	if task.sha1 != "" && fileExists(dest) && fileMatchesSHA1(dest, task.sha1) {
		if onBytes != nil {
			onBytes(task.fileSize) // щоб прогрес-байт не «стрибав» назад при resume
		}
		return task.fileSize, nil
	}

	var lastErr error
	for attempt := 1; attempt <= downloadMaxAttempts; attempt++ {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		if attempt > 1 {
			// Невелика пауза між спробами (429/5xx rate-limit friendly).
			select {
			case <-time.After(time.Duration(attempt-1) * 2 * time.Second):
			case <-ctx.Done():
				return 0, ctx.Err()
			}
		}
		n, err := downloadOneAttempt(ctx, client, task, dest, onBytes)
		if err == nil {
			return n, nil
		}
		lastErr = err
		var retry *retryableDownloadError
		if !errors.As(err, &retry) {
			// Фатальна помилка (404, 401/403, битий SHA1) — повторювати ні до чого.
			return 0, err
		}
	}
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	return 0, fmt.Errorf("%d спроби завантаження не вдалися: %w", downloadMaxAttempts, lastErr)
}

// downloadOneAttempt — одна спроба завантаження файлу з Range-докачкою,
// stall-детектором і перевіркою SHA1.
func downloadOneAttempt(ctx context.Context, client *http.Client, task importTask, dest string, onBytes func(int64)) (int64, error) {
	tmp := dest + ".part"
	// Range-продовження (пауза/крах/невдала спроба): якщо .part вже існує,
	// докачуємо з місця зупинки, а не з нуля. sha1-хеш наявної частини також
	// враховується (інакше перевірка після докачки дала б невірну суму).
	var resumeFrom int64
	if fi, err := os.Stat(tmp); err == nil {
		resumeFrom = fi.Size()
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
	if resumeFrom > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", resumeFrom))
	}

	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		return 0, &retryableDownloadError{msg: fmt.Sprintf("помилка з'єднання: %v", err)}
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case 200:
		// Сервер не підтримує Range — віддає повний контент, починаємо з нуля
		// (O_TRUNC важливий: без нього старий .part, довший за новий контент,
		// лишив би хвіст, і зіпсований файл перейменувався б у фінальний).
		resumeFrom = 0
	case 206:
		// Докачка з місця зупинки — як очікували.
	case 416:
		// Range неприйнятний. Реальний сценарій з retry-циклом: попередня
		// спроба СКАЧАЛА ВЕСЬ файл у .part і впала на 5-хв таймауті HTTP-клієнта
		// на останньому кроці (до перейменування). Тоді 416 — це «файл уже
		// повний»: перевіряємо SHA1 наявного .part і, якщо збігся, одразу
		// перейменовуємо в фінальний — замість повторного качання з нуля.
		if task.sha1 != "" && fileExists(tmp) && fileMatchesSHA1(tmp, task.sha1) {
			if err := os.Rename(tmp, dest); err == nil {
				return 0, nil
			}
		}
		// Інакше .part битий/неповний — стираємо і починаємо з нуля
		// (наступна спроба retry-циклу качає файл повністю).
		os.Remove(tmp)
		return 0, &retryableDownloadError{msg: "Range-запит не задоволено (HTTP 416)"}
	case 429:
		return 0, &retryableDownloadError{msg: "HTTP 429 (забагато запитів до CDN)"}
	default:
		if resp.StatusCode >= 500 {
			return 0, &retryableDownloadError{msg: fmt.Sprintf("HTTP %d (серверна помилка CDN)", resp.StatusCode)}
		}
		// Фатально (404/401/403/…): повтор безглуздий. Прибираємо .part від
		// ПОПЕРЕДНІХ спроб/запусків — інакше застарілий частковий файл від
		// цього ж шляху лишався б на диску і наступний запуск резюмив би
		// поверх чужого контенту (якщо за URL тепер інший файл).
		os.Remove(tmp)
		return 0, &fatalDownloadError{msg: fmt.Sprintf("HTTP %d", resp.StatusCode)}
	}

	flags := os.O_CREATE | os.O_WRONLY
	if resumeFrom == 0 {
		flags |= os.O_TRUNC
	}
	out, err := os.OpenFile(tmp, flags, 0644)
	if err != nil {
		return 0, err
	}
	if resumeFrom > 0 {
		if _, err := out.Seek(resumeFrom, io.SeekStart); err != nil {
			out.Close()
			return 0, err
		}
	}

	hasher := sha1.New()
	var writer io.Writer = out
	if task.sha1 != "" {
		writer = io.MultiWriter(out, hasher)
		// Хеш частини, що вже на диску (resume): читаємо її спочатку.
		if resumeFrom > 0 {
			if pf, err := os.Open(tmp); err == nil {
				io.Copy(hasher, io.LimitReader(pf, resumeFrom))
				pf.Close()
			}
		}
	}

	body := resp.Body
	if onBytes != nil {
		body = &progressReader{r: resp.Body, onBytes: onBytes}
	}
	// stall-детектор поверх прогрес-рідера: завмерле з'єднання обривається
	// через downloadStallTimeout замість вічного 0.0 МБ/с (див. downloadStallTimeout).
	body = newStallReader(ctx, body, downloadStallTimeout)
	defer body.Close()

	n, copyErr := io.Copy(writer, body)
	closeErr := out.Close()
	if copyErr != nil {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		// .part навмисно ЛИШАЄМО: наступна спроба докачає з місця зупинки.
		return 0, &retryableDownloadError{msg: fmt.Sprintf("обрив під час завантаження: %v", copyErr)}
	}
	if closeErr != nil {
		return 0, &retryableDownloadError{msg: fmt.Sprintf("помилка запису файлу: %v", closeErr)}
	}
	if task.sha1 != "" {
		sum := hex.EncodeToString(hasher.Sum(nil))
		if !strings.EqualFold(sum, task.sha1) {
			// Хеш не збігся — файл битий, лишати .part немає сенсу (наступна
			// спроба качала б поверх чужого контенту). Фатально: повтор CDN
			// не виправить постійно биту відповідь.
			os.Remove(tmp)
			return 0, &fatalDownloadError{msg: fmt.Sprintf("SHA1 не збігається (очікувано %s, отримано %s)", task.sha1, sum)}
		}
	}
	if err := os.Rename(tmp, dest); err != nil {
		return 0, err
	}
	return n, nil
}
