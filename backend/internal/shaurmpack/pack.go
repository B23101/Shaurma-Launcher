package shaurmpack

import (
	"archive/zip"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"shaurma-launcher-backend/internal/export"
)

// ExportOptions — параметри експорту в .shaurmpack.
type ExportOptions struct {
	// GameDir — шлях до .minecraft/ збірки
	GameDir string

	// IconFile — АБСОЛЮТНИЙ шлях до іконки збірки (PNG/GIF у корені теки
	// збірки, поруч з .minecraft/, напр. <instances>/<id>/icon.png).
	// Опційний: якщо вказаний і файл читається — кладеться в архів під
	// фіксованим іменем icon.png у КОРЕНІ (та сама конвенція, що mrpack
	// та .shaurmbundle з packs/<id>/icon.png), і імпортер відновлює її
	// як іконку збірки. Раніше .shaurmpack-експорт іконку НЕ зберігав
	// взагалі (вона живе поза .minecraft/, який сканується в overrides/),
	// через що при імпорті лаунчер ставив пресет-іконку за замовчуванням
	// і оригінальна персоналізація збірки губилась.
	IconFile string

	// Pack metadata
	PackID      string
	Name        string
	VersionID   string
	Description string
	Authors     []string
	License     string
	Changelog   string
	ProjectURL  string
	Color       string
	Icon        string // відносний шлях до іконки в overrides/

	// Minecraft
	GameVersion   string
	Loader        string
	LoaderVersion string

	// Launcher info
	LauncherName    string
	LauncherVersion string
	LauncherBuild   int

	// File lists (relative to GameDir, Unix '/')
	IncludePaths []string // файли для включення (порожній = всі)
	ExcludePaths []string // файли для виключення

	// CurseForge/Modrinth resolvers — функції для резолву хешів.
	// Повертають джерело якщо файл знайдений онлайн.
	//
	// ПОРЯДОК ВИКЛИКУ в Export(): СПЕРШУ ResolveModrinth, ПОТІМ
	// ResolveCurseForge (лише якщо Modrinth не дав результату) — див.
	// докладний коментар над Export().
	//
	// ResolveCurseForge МОЖЕ повернути джерело з Blocked=true (файл
	// існує й ідентифікований на CurseForge, але автор вимкнув сторонні
	// завантаження — той самий стан, що CFFileInfo.Blocked в
	// internal/custompack). У цьому випадку DownloadURL слід лишати
	// порожнім — Export() сам розпізнає Blocked і піде bundled-шляхом,
	// НЕ намагаючись хешувати/якати по порожньому чи неробочому URL.
	ResolveCurseForge func(sha1hex string, fileName string, fileSize int64) *CurseForgeSource
	ResolveModrinth   func(sha1hex string, fileName string, fileSize int64) *ModrinthSource

	// ResolveModrinthBatch / ResolveCurseForgeBatch — пакетні резолвери
	// (ПРІОРИТЕТНІ над per-file версіями вище, які лишаються як fallback і
	// для сумісності зі старими викликачами). Викликаються по одному разу на
	// весь експорт з УНІКАЛЬНИМИ хешами; ключ відповіді — SHA-1 файлу.
	ResolveModrinthBatch   ModrinthBatchResolver
	ResolveCurseForgeBatch CurseForgeBatchResolver

	// SHA1Of — опційний кешований SHA-1 файлу (напр. персистентний кеш
	// модів лаунчера). nil → хеш рахується читанням файлу. Викликається
	// паралельно з воркерів, тож реалізація має бути потокобезпечною.
	SHA1Of func(absPath string) (string, error)

	// Context — скасування експорту (nil = без скасування).
	Context context.Context

	// Personalization — додаткові метадані
	Personalization *Personalization

	// OutPath — шлях до вихідного .shaurmpack файлу
	OutPath string

	// Progress — опційний колбек прогресу експорту (може бути nil).
	// Викликається на кожному обробленому файлі: done/total файлів та
	// fileName поточного. Той самий механізм, що Progress в ExportBundle,
	// лише з іменем файлу — щоб фронтенд-модалка експорту показувала
	// живий відсоток і стрічку файлів, а не статичні 0%.
	Progress func(done int, total int, fileName string)
}

// ExportResult — результат експорту.
type ExportResult struct {
	OutPath       string // шлях до створеного файлу
	TotalFiles    int    // загальна кількість файлів
	BundledFiles  int    // файли без онлайн-джерела (bundled)
	ResolvedFiles int    // файли з онлайн-джерелом
	TotalSize     int64  // загальний розмір
	Hash          string // SHA-1 хеш .shaurmpack файлу
}

// Export створює .shaurmpack файл з вказаної збірки.
//
// Алгоритм (див. pipeline.go):
//  1. Збирає файли .minecraft/ за фільтрами (shouldSkipFile + вибір користувача).
//  2. Пул з 10 воркерів рахує SHA-1 (+ Murmur2 для CurseForge).
//  3. Резолв по унікальних хешах: Modrinth і CurseForge BATCH-запитами.
//     Якщо знайдено обидва джерела — у маніфесті зберігаються обидва
//     (Modrinth як основне). Заблокований автором CurseForge-запис у
//     маніфест НЕ пишеться.
//  4. Файл з робочим джерелом (Modrinth, або CurseForge без Blocked і з
//     DownloadURL) у ZIP НЕ потрапляє — лише хеш і посилання в маніфесті.
//  5. Файл, що міг би мати джерело (jar/zip у mods|resourcepacks|shaderpacks),
//     але його не знайдено/заблоковано → bundled/.
//  6. Файл, що не може мати онлайн-джерела (config, datapacks, …) → overrides/.
//  7. Пишеться manifest.json (файли відсортовані за шляхом), metadata/, icon.png.
func Export(opts ExportOptions) (*ExportResult, error) {
	if opts.GameDir == "" {
		return nil, fmt.Errorf("gameDir порожній")
	}
	if opts.OutPath == "" {
		return nil, fmt.Errorf("outPath порожній")
	}
	if opts.Name == "" {
		opts.Name = "Unnamed Pack"
	}
	if opts.VersionID == "" {
		opts.VersionID = "1.0.0"
	}
	if opts.LauncherName == "" {
		opts.LauncherName = "Shaurma Launcher"
	}
	if opts.LauncherVersion == "" {
		opts.LauncherVersion = "dev"
	}

	// 1. Скануємо файли
	//
	// Вибір користувача (IncludePaths/ExcludePaths) обробляємо ТИМ САМИМ
	// деревом правил, що й mrpack-експорт (export.PathFilter: найглибше
	// правило перемагає, папка покриває вміст). Раніше тут була точна
	// рівність рядків, через що вибір у модалці не діяв узагалі: у списку
	// вибраного лежать і папки («mods»), і файли, тож «вибрав лише mods,
	// а запакувалась уся тека».
	filter := export.NewPathFilter(opts.IncludePaths, opts.ExcludePaths)

	var manifest Manifest
	manifest.Version = FormatVersion
	manifest.PackID = opts.PackID
	manifest.Name = opts.Name
	manifest.Description = opts.Description
	manifest.VersionID = opts.VersionID
	manifest.Authors = opts.Authors
	manifest.License = opts.License
	manifest.Changelog = opts.Changelog
	manifest.ProjectURL = opts.ProjectURL
	manifest.Color = opts.Color
	manifest.GameVersion = opts.GameVersion
	manifest.Loader = opts.Loader
	manifest.LoaderVersion = opts.LoaderVersion
	manifest.Icon = opts.Icon
	manifest.Launcher = LauncherMeta{
		Name:    opts.LauncherName,
		Version: opts.LauncherVersion,
		Build:   opts.LauncherBuild,
	}
	manifest.CreatedAt = time.Now().UTC().Format(time.RFC3339)

	// 2. Збираємо кандидатів і готуємо резолвери.
	cands := collectCandidates(opts.GameDir, filter)
	totalFiles := len(cands)

	ctx := opts.Context
	if ctx == nil {
		ctx = context.Background()
	}

	// Перша подія прогресу (0/total) — одразу, ДО першого файлу: хешування й
	// мережевий резолв можуть зайняти секунди, і без цієї події модалка
	// експорту показувала б статичні «0%». Фінальна (total/total) — після
	// запису архіву.
	if opts.Progress != nil && totalFiles > 0 {
		opts.Progress(0, totalFiles, "")
	}

	rs := resolverSet{
		modrinth:        opts.ResolveModrinth,
		curseforge:      opts.ResolveCurseForge,
		modrinthBatch:   opts.ResolveModrinthBatch,
		curseforgeBatch: opts.ResolveCurseForgeBatch,
		sha1Of:          opts.SHA1Of,
	}
	// Хешування + резолв (пул воркерів, batch-запити). Прогрес на цьому етапі
	// не доходить до total — останній крок (запис ZIP) закриває його.
	planned := planFiles(ctx, cands, rs, func(done int, name string) {
		if opts.Progress != nil && done < totalFiles {
			opts.Progress(done, totalFiles, name)
		}
	})
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("експорт скасовано: %w", err)
	}
	for _, pl := range planned {
		if pl.err != nil {
			return nil, pl.err
		}
	}

	// 3. Створюємо ZIP і пишемо ЛИШЕ те, чого немає онлайн.
	outFile, err := os.Create(opts.OutPath)
	if err != nil {
		return nil, fmt.Errorf("створення файлу: %w", err)
	}
	defer outFile.Close()

	zipWriter := zip.NewWriter(outFile)
	defer zipWriter.Close()

	var totalSize int64
	var bundledCount, resolvedCount, overrideCount int
	manifest.Files = make([]PackFile, 0, len(planned))
	for _, pl := range planned {
		totalSize += pl.size
		switch pl.dest {
		case destSource:
			// Є робоче джерело (Modrinth / незаблокований CurseForge): файл у
			// ZIP НЕ пишемо — лише хеш і посилання в маніфесті.
			resolvedCount++
		case destBundled:
			// Джерела немає або його заблоковано автором — єдиний надійний
			// спосіб мати файл у паку: покласти байти в bundled/.
			bundledCount++
			if err := addFileToZip(zipWriter, pl.abs, BundledDir+pl.rel); err != nil {
				return nil, fmt.Errorf("додавання bundled %s: %w", pl.rel, err)
			}
		default:
			// Файл, який принципово не має онлайн-джерела (config, datapacks…).
			overrideCount++
			if err := addFileToZip(zipWriter, pl.abs, OverridesDir+pl.rel); err != nil {
				return nil, fmt.Errorf("додавання override %s: %w", pl.rel, err)
			}
		}
		manifest.Files = append(manifest.Files, pl.pf) // planned вже відсортований за шляхом
	}
	if opts.Progress != nil && totalFiles > 0 {
		opts.Progress(totalFiles, totalFiles, "")
	}

	// OverrideCount — файли, що лежать в overrides/ (не bundled/ і не онлайн).
	manifest.OverrideCount = overrideCount
	manifest.BundledCount = bundledCount
	manifest.ResolvedCount = resolvedCount


	// 6. Пишемо metadata/personalization.json
	if opts.Personalization != nil {
		if err := addJSONToZip(zipWriter, MetadataDir+"personalization.json", opts.Personalization); err != nil {
			return nil, fmt.Errorf("додавання personalization: %w", err)
		}
	}

	// 7. Пишемо manifest.json
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("маніфест JSON: %w", err)
	}
	if err := addBytesToZip(zipWriter, ManifestFile, manifestData); err != nil {
		return nil, fmt.Errorf("додавання manifest: %w", err)
	}

	// 8. Іконка збірки — у КОРІНЬ архіву під фіксованим іменем icon.png
	// (поза overrides/bundled — це не ігровий файл .minecraft/, а
	// персоналізація збірки). Тиха поведінка: немає/не читається/завелика
	// → просто без іконки в паку, як і було раніше.
	if opts.IconFile != "" {
		if st, err := os.Stat(opts.IconFile); err == nil && !st.IsDir() && st.Size() > 0 && st.Size() <= 2*1024*1024 {
			if data, rerr := os.ReadFile(opts.IconFile); rerr == nil {
				_ = addBytesToZip(zipWriter, "icon.png", data)
			}
		}
	}

	// Закриваємо ZIP перед обчисленням хешу
	if err := zipWriter.Close(); err != nil {
		return nil, fmt.Errorf("закриття ZIP: %w", err)
	}
	outFile.Close()

	// Обчислюємо SHA-1 всього .shaurmpack файлу
	fileHash, err := fileSHA1(opts.OutPath)
	if err != nil {
		return nil, err
	}

	return &ExportResult{
		OutPath:       opts.OutPath,
		TotalFiles:    len(manifest.Files),
		BundledFiles:  bundledCount,
		ResolvedFiles: resolvedCount,
		TotalSize:     totalSize,
		Hash:          fileHash,
	}, nil
}

// ── Допоміжні ──

// resolvableFile — чи має сенс питати Modrinth/CurseForge про цей файл.
//
// Так: jar/zip/litemod у mods, resourcepacks, shaderpacks — саме такі файли
// там і лежать. Ні: усе інше (config/*.txt, опції, датапаки-конфіги тощо) —
// таких файлів онлайн немає, вони все одно йдуть у bundled/, а кожен зайвий
// запит до CurseForge проходить через його rate-limiter (4/с) і ретраї по
// 300–700 мс. Саме через це експорт збірки з сотнями дрібних конфігів ішов
// зі швидкістю ~1 файл за секунду.
func resolvableFile(fileType FileType, relPath string) bool {
	switch fileType {
	case FileTypeMod, FileTypeResourcePack, FileTypeShaderPack:
	default:
		return false
	}
	switch strings.ToLower(filepath.Ext(relPath)) {
	case ".jar", ".zip", ".litemod":
		return true
	}
	return false
}

func fileSHA1(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func addFileToZip(zw *zip.Writer, srcPath, destPath string) error {
	f, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return err
	}

	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	header.Name = destPath
	header.Method = zipMethodFor(srcPath)

	writer, err := zw.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = io.Copy(writer, f)
	return err
}

// zipMethodFor — Store для вже стиснутих форматів (jar/zip/png/ogg…): повторне
// Deflate лише витрачає CPU без виграшу в розмірі; Deflate — для решти.
func zipMethodFor(name string) uint16 {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".jar", ".zip", ".litemod", ".png", ".jpg", ".jpeg", ".gif", ".ogg", ".mp3", ".mp4", ".gz", ".7z", ".mrpack":
		return zip.Store
	}
	return zip.Deflate
}

func addBytesToZip(zw *zip.Writer, name string, data []byte) error {
	header := &zip.FileHeader{
		Name:   name,
		Method: zip.Deflate,
	}
	writer, err := zw.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = writer.Write(data)
	return err
}

func addJSONToZip(zw *zip.Writer, name string, v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return addBytesToZip(zw, name, data)
}

func detectFileType(path string) FileType {
	lower := strings.ToLower(path)
	switch {
	case strings.HasPrefix(lower, "mods/") && strings.HasSuffix(lower, ".jar"):
		return FileTypeMod
	case strings.HasPrefix(lower, "mods/") && strings.HasSuffix(lower, ".zip"):
		return FileTypeMod
	case strings.HasPrefix(lower, "resourcepacks/") || strings.HasPrefix(lower, "resourcepacks\\"):
		return FileTypeResourcePack
	case strings.HasPrefix(lower, "shaderpacks/") || strings.HasPrefix(lower, "shaderpacks\\"):
		return FileTypeShaderPack
	case strings.HasPrefix(lower, "config/"):
		return FileTypeConfig
	default:
		return FileTypeOther
	}
}

func shouldSkipFile(relPath string) bool {
	lower := strings.ToLower(relPath)

	// Пропускаємо launcher-specific файли та теки
	skipPrefixes := []string{
		"logs/",
		"crash-reports/",
		"libraries/",
		"versions/",
		"assets/",
		"bin/",
		"caches/",
		".fabric/",
		".shaurma/",
	}
	for _, p := range skipPrefixes {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}

	// Пропускаємо окремі файли
	skipFiles := []string{
		"server-icon.png",
		"options.txt",
		"usercache.json",
		"usernamecache.json",
		"launcher_profiles.json",
	}
	// Пропускаємо архіви модпаків
	for _, ext := range []string{".shaurmpack", ".shaurmbundle", ".mrpack", ".zip"} {
		if strings.HasSuffix(lower, ext) && !strings.HasPrefix(lower, "mods/") && !strings.HasPrefix(lower, "resourcepacks/") && !strings.HasPrefix(lower, "shaderpacks/") {
			return true
		}
	}
	base := filepath.Base(relPath)
	for _, f := range skipFiles {
		if strings.ToLower(base) == f {
			return true
		}
	}

	return false
}

