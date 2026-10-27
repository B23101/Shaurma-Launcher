package shaurmpack

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	// BundleExtension — розширення файлу для групового паку.
	BundleExtension = ".shaurmbundle"

	// BundleManifestFile — шлях до маніфесту бандла в архіві.
	BundleManifestFile = "bundle.json"

	// BundleFilesFile — packs/<id>/files.json: файли збірки з онлайн-джерелом
	// (їх байтів в архіві немає — імпорт качає їх за посиланнями). Відсутній у
	// старих бандлах, де такі файли лежали в overrides/.
	BundleFilesFile = "files.json"
)

// ─────────────────────────────────────────────────────────────────────────────
// .shaurmbundle — формат групового експорту кількох збірок
//
// На відміну від .shaurmpack (одна збірка + версія), .shaurmbundle містить
// кілька збірок БЕЗ версії — для бекапу, перенесення або шерингу набору збірок.
//
// Структура архіву:
//   bundle.json                    — маніфест бандла (кількість збірок, launcher)
//   packs/<packId>/instance.json   — налаштування кожної збірки
//   packs/<packId>/overrides/      — файли кожної збірки
//   packs/<packId>/bundled/        — файли, що могли б мати джерело, але його не знайдено
//   packs/<packId>/files.json      — файли з онлайн-джерелом (байтів в архіві НЕМАЄ)
//   packs/<packId>/icon.png        — іконка збірки
//   packs/<packId>/background.png  — фон збірки
// ─────────────────────────────────────────────────────────────────────────────

// BundleManifest — головний маніфест .shaurmbundle файлу.
type BundleManifest struct {
	// Format — завжди "shaurmbundle" (для диференціації від .shaurmpack)
	Format string `json:"format"`

	// Паки в бандлі
	Packs []BundlePackInfo `json:"packs"`

	// Launcher metadata
	Launcher    LauncherMeta `json:"launcher"`
	CreatedAt   string       `json:"createdAt"`
	Description string       `json:"description,omitempty"`
}

// BundlePackInfo — інформація про одну збірку в бандлі.
type BundlePackInfo struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Description    string `json:"description,omitempty"`
	Color          string `json:"color,omitempty"`
	MCVersion      string `json:"mcVersion"`
	Loader         string `json:"loader"`
	LoaderVersion  string `json:"loaderVersion,omitempty"`
	IconPath       string `json:"iconPath,omitempty"`       // відносний шлях до icon.png
	BackgroundPath string `json:"backgroundPath,omitempty"` // відносний шлях до background.png
	FileCount      int    `json:"fileCount"`
	BundledCount   int    `json:"bundledCount"`
	ResolvedCount  int    `json:"resolvedCount"`
}

// BundleFiles — вміст packs/<id>/files.json.
type BundleFiles struct {
	Files []PackFile `json:"files"`
}

// BundleExportOptions — параметри експорту бандла.
type BundleExportOptions struct {
	// Packs — список збірок для експорту
	Packs []BundlePackExport

	// Launcher info
	LauncherName    string
	LauncherVersion string
	LauncherBuild   int

	// CurseForge/Modrinth resolvers
	ResolveCurseForge func(sha1hex string, fileName string, fileSize int64) *CurseForgeSource
	ResolveModrinth   func(sha1hex string, fileName string, fileSize int64) *ModrinthSource

	// Batch-резолвери (пріоритетні) і кешований SHA-1 — див. ExportOptions.
	ResolveModrinthBatch   ModrinthBatchResolver
	ResolveCurseForgeBatch CurseForgeBatchResolver
	SHA1Of                 func(absPath string) (string, error)

	// Context — скасування експорту (nil = без скасування).
	Context context.Context

	// Description — опис бандла
	Description string

	// Progress — опційний колбек прогресу: викликається після кожної
	// обробленої збірки з (done, total, packID).
	Progress func(done, total int, packID string)

	// OutPath — шлях до вихідного .shaurmbundle файлу
	OutPath string
}

// BundlePackExport — дані однієї збірки для експорту в бандл.
type BundlePackExport struct {
	// PackData — JSON instance.json збірки
	PackData []byte
	// PackID — ідентифікатор збірки (має бути в PackData)
	PackID  string
	GameDir string // шлях до .minecraft/ збірки

	// IconFile / BackgroundFile — АБСОЛЮТНІ шляхи до файлів іконки/фону
	// збірки. У лаунчері вони лежать у корені теки збірки (поруч з
	// .minecraft/), а не всередині нього — ExportBundle інакше не зміг би
	// їх знайти (стара логіка шукала відносно GameDir). Порожній шлях =
	// збірка без іконки/фону.
	IconFile       string
	BackgroundFile string
}

// BundleExportResult — результат експорту бандла.
type BundleExportResult struct {
	OutPath    string
	PackCount  int
	TotalFiles int
	TotalSize  int64
	Hash       string
}

// BundleImportOptions — параметри імпорту бандла.
type BundleImportOptions struct {
	// ShaurmBundlePath — шлях до .shaurmbundle файлу
	ShaurmBundlePath string

	// TargetBaseDir — базова тека куди створювати папки збірок
	// (напр. installations/). Для кожної збірки створюється
	// <TargetBaseDir>/<packId>/ з .minecraft/ всередині.
	TargetBaseDir string

	// IDMap — опційна мапа перейменувань ID збірок (оригінал → новий).
	// Потрібна, коли в цільовій теці вже є збірка з тим самим ID — тоді
	// архівний пак імпортується в теку <TargetBaseDir>/<новийID>/, а його
	// записи в архіві переписуються під новий префікс.
	IDMap map[string]string

	// Progress — опційний колбек прогресу: після кожної розпакованої
	// збірки (done, total, packID).
	Progress func(done, total int, packID string)

	// ResolveDownload — функція для завантаження файлів з CurseForge/Modrinth
	ResolveDownload func(destPath string, pf PackFile) error
}

// BundleImportResult — результат імпорту бандла.
type BundleImportResult struct {
	Packs      []BundleImportPack
	TotalFiles int
}

// BundleImportPack — результат імпорту однієї збірки з бандла.
type BundleImportPack struct {
	PackID     string
	Name       string
	GameDir    string
	FilesTotal int
	FilesCopied int
	FilesLinked int
	// FilesMissing — файли з онлайн-джерелом, які не вдалося завантажити
	// (в архіві їх байтів немає).
	FilesMissing int
}

// ExportBundle експортує кілька збірок в .shaurmbundle файл.
func ExportBundle(opts BundleExportOptions) (*BundleExportResult, error) {
	if opts.OutPath == "" {
		return nil, fmt.Errorf("outPath порожній")
	}
	if len(opts.Packs) == 0 {
		return nil, fmt.Errorf("немає збірок для експорту")
	}
	if opts.LauncherName == "" {
		opts.LauncherName = "Shaurma Launcher"
	}
	if opts.LauncherVersion == "" {
		opts.LauncherVersion = "dev"
	}

	ctx := opts.Context
	if ctx == nil {
		ctx = context.Background()
	}
	rs := resolverSet{
		modrinth:        opts.ResolveModrinth,
		curseforge:      opts.ResolveCurseForge,
		modrinthBatch:   opts.ResolveModrinthBatch,
		curseforgeBatch: opts.ResolveCurseForgeBatch,
		sha1Of:          opts.SHA1Of,
	}

	bundle := BundleManifest{
		Format:      "shaurmbundle",
		Description: opts.Description,
		Launcher: LauncherMeta{
			Name:    opts.LauncherName,
			Version: opts.LauncherVersion,
			Build:   opts.LauncherBuild,
		},
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}

	// Створюємо ZIP
	outFile, err := os.Create(opts.OutPath)
	if err != nil {
		return nil, fmt.Errorf("створення файлу: %w", err)
	}
	defer outFile.Close()
	zipWriter := zip.NewWriter(outFile)
	defer zipWriter.Close()

	var totalFiles int
	var totalSize int64

	// Скільки збірок РЕАЛЬНО піде в архів (з даними й текою гри) — щоб
	// лічильник прогресу (done/total) сходився в кінці, а не зависав на
	// меншому числі через пропущені порожні збірки.
	exportable := 0
	for _, p := range opts.Packs {
		if len(p.PackData) > 0 && p.GameDir != "" {
			exportable++
		}
	}

	// Перша подія (0/total) — одразу, до першої збірки: експорт пакує
	// збірки послідовно, і резолв/хешування файлів ПЕРШОЇ збірки може
	// йти довго — без цієї події модалка «експорт папки» весь цей час
	// висіла б на порожньому спінері без відсотків (виглядало як «0%»).
	if opts.Progress != nil && exportable > 0 {
		opts.Progress(0, exportable, "")
	}

	// Кожна збірка окремо
	for _, pack := range opts.Packs {
		if len(pack.PackData) == 0 || pack.GameDir == "" {
			continue
		}

		// Парсимо instance.json для отримання полів
		var packFields struct {
			ID             string `json:"id"`
			Name           string `json:"name"`
			Description    string `json:"description"`
			Color          string `json:"color"`
			MCVersion      string `json:"mcVersion"`
			Loader         string `json:"loader"`
			LoaderVersion  string `json:"loaderVersion"`
			IconPath       string `json:"iconPath"`
			BackgroundPath string `json:"backgroundPath"`
		}
		if err := json.Unmarshal(pack.PackData, &packFields); err != nil {
			continue
		}

		packID := pack.PackID
		if packID == "" {
			packID = packFields.ID
		}
		if packID == "" {
			packID = "unknown"
		}

		packPrefix := "packs/" + packID + "/"
		info := BundlePackInfo{
			ID:            packID,
			Name:          packFields.Name,
			Description:   packFields.Description,
			Color:         packFields.Color,
			MCVersion:     packFields.MCVersion,
			Loader:        packFields.Loader,
			LoaderVersion: packFields.LoaderVersion,
		}

		// Зберігаємо instance.json (налаштування збірки — сирий JSON)
		if err := addBytesToZip(zipWriter, packPrefix+"instance.json", pack.PackData); err != nil {
			return nil, fmt.Errorf("instance.json %s: %w", packID, err)
		}

		// Копіюємо іконку та фон. Спершу — абсолютні шляхи, передані
		// викликачем (IconFile/BackgroundFile: у лаунчері вони лежать у
		// корені теки збірки, поруч з .minecraft/), як fallback — стара
		// логіка відносно GameDir. У бандл файл завжди пишеться під
		// фіксованим ім'ям icon.png/background.png, а в маніфест
		// записується саме це відносне ім'я — імпортер кладе файли туди ж.
		iconSrc := pack.IconFile
		if iconSrc == "" && packFields.IconPath != "" {
			iconSrc = filepath.Join(pack.GameDir, filepath.FromSlash(packFields.IconPath))
		}
		if iconSrc != "" {
			if data, err := os.ReadFile(iconSrc); err == nil {
				if err := addBytesToZip(zipWriter, packPrefix+"icon.png", data); err == nil {
					info.IconPath = "icon.png"
				}
			}
		}
		bgSrc := pack.BackgroundFile
		if bgSrc == "" && packFields.BackgroundPath != "" {
			bgSrc = filepath.Join(pack.GameDir, filepath.FromSlash(packFields.BackgroundPath))
		}
		if bgSrc != "" {
			if data, err := os.ReadFile(bgSrc); err == nil {
				if err := addBytesToZip(zipWriter, packPrefix+"background.png", data); err == nil {
					info.BackgroundPath = "background.png"
				}
			}
		}

		// Збір → хешування/резолв (пул + batch) → запис. Та сама логіка, що й
		// в Export: файл з робочим джерелом у ZIP НЕ пишеться (його опис
		// лежить у packs/<id>/files.json), нерозпізнане → bundled/, файли без
		// можливого джерела (config…) → overrides/.
		cands := collectCandidates(pack.GameDir, nil)
		planned := planFiles(ctx, cands, rs, nil)
		if cerr := ctx.Err(); cerr != nil {
			return nil, fmt.Errorf("експорт скасовано: %w", cerr)
		}

		var sourced []PackFile
		for _, pl := range planned {
			if pl.err != nil {
				continue // як і раніше: нечитаний файл пропускаємо
			}
			totalSize += pl.size
			switch pl.dest {
			case destSource:
				info.ResolvedCount++
				sourced = append(sourced, pl.pf)
			case destBundled:
				info.BundledCount++
				if err := addFileToZip(zipWriter, pl.abs, packPrefix+BundledDir+pl.rel); err != nil {
					return nil, fmt.Errorf("сканування %s: %w", packID, err)
				}
			default:
				if err := addFileToZip(zipWriter, pl.abs, packPrefix+OverridesDir+pl.rel); err != nil {
					return nil, fmt.Errorf("сканування %s: %w", packID, err)
				}
			}
			info.FileCount++
			totalFiles++
		}
		if len(sourced) > 0 {
			if err := addJSONToZip(zipWriter, packPrefix+BundleFilesFile, BundleFiles{Files: sourced}); err != nil {
				return nil, fmt.Errorf("files.json %s: %w", packID, err)
			}
		}

		bundle.Packs = append(bundle.Packs, info)
		if opts.Progress != nil {
			opts.Progress(len(bundle.Packs), exportable, packID)
		}
	}

	// Пишемо bundle.json
	bundleData, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("маніфест JSON: %w", err)
	}
	if err := addBytesToZip(zipWriter, BundleManifestFile, bundleData); err != nil {
		return nil, fmt.Errorf("додавання bundle.json: %w", err)
	}

	if err := zipWriter.Close(); err != nil {
		return nil, fmt.Errorf("закриття ZIP: %w", err)
	}
	outFile.Close()

	hash, err := fileSHA1(opts.OutPath)
	if err != nil {
		return nil, err
	}

	return &BundleExportResult{
		OutPath:    opts.OutPath,
		PackCount:  len(bundle.Packs),
		TotalFiles: totalFiles,
		TotalSize:  totalSize,
		Hash:       hash,
	}, nil
}

// ReadBundleManifest читає bundle.json з .shaurmbundle файлу.
func ReadBundleManifest(path string) (*BundleManifest, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("відкриття .shaurmbundle: %w", err)
	}
	defer r.Close()

	for _, f := range r.File {
		if f.Name == BundleManifestFile {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			var bundle BundleManifest
			if err := json.NewDecoder(rc).Decode(&bundle); err != nil {
				return nil, fmt.Errorf("парсинг bundle.json: %w", err)
			}
			return &bundle, nil
		}
	}
	return nil, fmt.Errorf("bundle.json не знайдено в архіві")
}

// readBundleFiles читає packs/<id>/files.json (nil, якщо його немає/битий).
func readBundleFiles(r *zip.ReadCloser, packPrefix string) []PackFile {
	want := packPrefix + BundleFilesFile
	for _, f := range r.File {
		if f.Name != want {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil
		}
		defer rc.Close()
		var bf BundleFiles
		if err := json.NewDecoder(rc).Decode(&bf); err != nil {
			return nil
		}
		return bf.Files
	}
	return nil
}

// ImportBundle розпаковує .shaurmbundle та створює папки збірок.
func ImportBundle(opts BundleImportOptions) (*BundleImportResult, error) {
	if opts.ShaurmBundlePath == "" {
		return nil, fmt.Errorf("shaurmBundlePath порожній")
	}
	if opts.TargetBaseDir == "" {
		return nil, fmt.Errorf("targetBaseDir порожній")
	}

	r, err := zip.OpenReader(opts.ShaurmBundlePath)
	if err != nil {
		return nil, fmt.Errorf("відкриття .shaurmbundle: %w", err)
	}
	defer r.Close()

	bundle, err := ReadBundleManifest(opts.ShaurmBundlePath)
	if err != nil {
		return nil, err
	}

	result := &BundleImportResult{}

	// Кожна збірка окремо. destID може відрізнятися від packInfo.ID через
	// IDMap (колізія з уже встановленою збіркою) — файли в архіві лежать
	// під префіксом packs/<оригінальнийID>/, але розпаковуються в теку
	// <TargetBaseDir>/<destID>/.
	total := len(bundle.Packs)
	done := 0
	for _, packInfo := range bundle.Packs {
		destID := packInfo.ID
		if opts.IDMap != nil {
			if mapped, ok := opts.IDMap[packInfo.ID]; ok && mapped != "" {
				destID = mapped
			}
		}
		if destID == "" {
			destID = "pack"
		}

		packDir := filepath.Join(opts.TargetBaseDir, destID)
		gameDir := filepath.Join(packDir, ".minecraft")
		if err := os.MkdirAll(gameDir, 0755); err != nil {
			return nil, fmt.Errorf("створення теки %s: %w", destID, err)
		}

		packResult := BundleImportPack{
			PackID:  destID,
			Name:    packInfo.Name,
			GameDir: gameDir,
		}

		// Проходимо по ZIP та розпаковуємо файли цієї збірки
		packPrefix := "packs/" + packInfo.ID + "/"

		// files.json (нові бандли): файли з онлайн-джерелом лежать ЛИШЕ тут
		// (їх байтів в архіві немає). Старі бандли files.json не мають —
		// там такі файли просто в overrides/.
		var toDownload []PackFile
		for _, pf := range readBundleFiles(r, packPrefix) {
			if pf.HasWorkingSource() {
				toDownload = append(toDownload, pf)
			}
		}

		for _, f := range r.File {
			if !strings.HasPrefix(f.Name, packPrefix) {
				continue
			}
			relInPack := strings.TrimPrefix(f.Name, packPrefix)

			switch {
			case strings.HasPrefix(relInPack, OverridesDir):
				relPath := strings.TrimPrefix(relInPack, OverridesDir)
				if relPath == "" {
					continue
				}
				destPath := filepath.Join(gameDir, filepath.FromSlash(relPath))
				if err := extractFile(f, destPath); err == nil {
					packResult.FilesCopied++
					packResult.FilesTotal++
				}

			case strings.HasPrefix(relInPack, BundledDir):
				relPath := strings.TrimPrefix(relInPack, BundledDir)
				if relPath == "" {
					continue
				}
				destPath := filepath.Join(gameDir, filepath.FromSlash(relPath))
				if err := extractFile(f, destPath); err == nil {
					packResult.FilesCopied++
					packResult.FilesTotal++
				}

			case relInPack == "instance.json":
				// Зберігаємо instance.json в корінь папки збірки
				destPath := filepath.Join(packDir, "instance.json")
				if err := extractFile(f, destPath); err != nil {
					return nil, fmt.Errorf("instance.json %s: %w", packInfo.ID, err)
				}

			case relInPack == "icon.png":
				extractFile(f, filepath.Join(packDir, "icon.png"))

			case relInPack == "background.png":
				extractFile(f, filepath.Join(packDir, "background.png"))
			}
		}

		// Паралельне завантаження файлів з джерел (ліміт workerCount).
		if len(toDownload) > 0 {
			var mu sync.Mutex
			runPool(context.Background(), len(toDownload), func(i int) {
				pf := toDownload[i]
				var err error
				dest := filepath.Join(gameDir, filepath.FromSlash(pf.Path))
				if opts.ResolveDownload == nil {
					err = fmt.Errorf("немає ResolveDownload")
				} else if err = os.MkdirAll(filepath.Dir(dest), 0755); err == nil {
					err = opts.ResolveDownload(dest, pf)
				}
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					packResult.FilesMissing++
					return
				}
				packResult.FilesLinked++
				packResult.FilesTotal++
			})
		}

		result.Packs = append(result.Packs, packResult)
		result.TotalFiles += packResult.FilesTotal
		done++
		if opts.Progress != nil {
			opts.Progress(done, total, destID)
		}
	}

	return result, nil
}

// BundleToShaurmaPack конвертує один пак з бандла в .shaurmpack.
// Використовується коли користувач хоче експортувати одну збірку з бандла.
func BundleToShaurmaPack(bundlePath string, packID string, outPath string) error {
	r, err := zip.OpenReader(bundlePath)
	if err != nil {
		return err
	}
	defer r.Close()

	bundle, err := ReadBundleManifest(bundlePath)
	if err != nil {
		return err
	}

	// Знаходимо потрібний пак
	var packInfo *BundlePackInfo
	for i := range bundle.Packs {
		if bundle.Packs[i].ID == packID {
			packInfo = &bundle.Packs[i]
			break
		}
	}
	if packInfo == nil {
		return fmt.Errorf("збірку %q не знайдено в бандлі", packID)
	}

	// Створюємо .shaurmpack з файлів бандла
	outFile, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer outFile.Close()

	zipWriter := zip.NewWriter(outFile)
	defer zipWriter.Close()

	// Читаємо instance.json з бандла
	packPrefix := "packs/" + packID + "/"
	for _, f := range r.File {
		if !strings.HasPrefix(f.Name, packPrefix) {
			continue
		}
		relInPack := strings.TrimPrefix(f.Name, packPrefix)

		// Перейменовуємо префікси на формат .shaurmpack
		var destName string
		switch {
		case strings.HasPrefix(relInPack, OverridesDir):
			destName = relInPack
		case strings.HasPrefix(relInPack, BundledDir):
			destName = relInPack
		case relInPack == "instance.json":
			destName = ManifestFile
		case relInPack == "icon.png":
			destName = relInPack
		case relInPack == "background.png":
			destName = relInPack
		default:
			continue
		}

		// Копіюємо файл
		src, err := f.Open()
		if err != nil {
			continue
		}
		header, err := zip.FileInfoHeader(f.FileInfo())
		if err != nil {
			src.Close()
			continue
		}
		header.Name = destName
		header.Method = zip.Deflate
		dst, err := zipWriter.CreateHeader(header)
		if err != nil {
			src.Close()
			continue
		}
		io.Copy(dst, src)
		src.Close()
	}

	// Конвертуємо instance.json → manifest.json
	// (додаємо versionId якого немає в бандлі)
	instancePath := packPrefix + "instance.json"
	for _, f := range r.File {
		if f.Name == instancePath {
			src, _ := f.Open()
			if src != nil {
				var inst map[string]interface{}
				json.NewDecoder(src).Decode(&inst)
				src.Close()
				// Додаємо versionId
				if _, ok := inst["versionId"]; !ok {
					inst["versionId"] = "1.0.0"
				}
				// Файли з онлайн-джерелом (їх байтів у бандлі немає) — у files
				if sourced := readBundleFiles(r, packPrefix); len(sourced) > 0 {
					inst["files"] = sourced
				}
				// Пишемо як manifest.json
				data, _ := json.MarshalIndent(inst, "", "  ")
				addBytesToZip(zipWriter, ManifestFile, data)
			}
			break
		}
	}

	return nil
}

// fileSHA1ForBundle — обчислення SHA-1 (використовується тимчасово).
