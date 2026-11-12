package shaurmpack

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"shaurma-launcher-backend/internal/safedel"
	shsync "shaurma-launcher-backend/internal/sync"
)

// ImportOptions — параметри імпорту .shaurmpack.
type ImportOptions struct {
	// ShaurmPackPath — шлях до .shaurmpack файлу
	ShaurmPackPath string

	// TargetGameDir — куди розпаковувати overrides (тимчасова тека або .minecraft/)
	TargetGameDir string

	// ResolveDownload — функція для завантаження файлів за CurseForge/Modrinth джерелом.
	// Параметри: шлях для збереження, джерело. Повертає помилку.
	ResolveDownload func(destPath string, pf PackFile) error

	// ResolveDownloadHashed — швидкий шлях завантаження (має пріоритет над
	// ResolveDownload). Резолвер пише файл у destPath і повертає SHA-1, який
	// порахував ПІД ЧАС самого завантаження (потоково). Import не перечитує
	// файл ще раз для перевірки й не копіює його між теками: звіряє розмір і
	// SHA-1, після чого атомарно перейменовує готовий файл на місце. Це
	// прибирає два зайві проходи по диску на кожен мод, а перевірка
	// цілісності лишається повною — у фінальному шляху ніколи не з'являється
	// неперевірений файл.
	ResolveDownloadHashed func(destPath string, pf PackFile) (sha1Hex string, err error)

	// UpdateOf — якщо заповнено, це ОНОВЛЕННЯ існуючої збірки.
	// Імпортер порівняє версії і видалить файли яких немає в новій версії.
	UpdateOf *InstalledInfo

	// DeleteRemoved — видаляти файли яких немає в новій версії (при оновленні)
	DeleteRemoved bool

	// Context — скасування імпорту (nil = без скасування). При скасуванні
	// Import повертає ctx.Err(); частково розпаковане лишається на диску
	// (прибирає викликач).
	Context context.Context

	// Progress — опційний колбек прогресу (може бути nil). Викликається
	// послідовно (під м'ютексом) після кожного розпакованого/завантаженого
	// файлу: стадія, зроблено/всього файлів і ім'я поточного файлу.
	Progress func(ImportProgress)

	// SkipExisting — не качати файл, якщо він уже є на диску з тим самим
	// розміром і SHA-1 (відновлення імпорту після паузи/обриву).
	SkipExisting bool
}

// Стадії ImportProgress.Stage (збігаються зі стадіями картки збірки).
const (
	ImportStageExtracting  = "extracting"
	ImportStageDownloading = "downloading"
	ImportStageFinalizing  = "finalizing"
)

// ImportProgress — стан імпорту для ImportOptions.Progress.
type ImportProgress struct {
	Stage       string
	FilesDone   int
	FilesTotal  int
	CurrentFile string
}

// ImportResult — результат імпорту.
type ImportResult struct {
	Manifest      *Manifest      // прочитаний маніфест
	GameDir       string         // куди розпаковано
	FilesTotal    int            // загальна кількість файлів
	FilesLinked   int            // файли з онлайн-джерелом (download)
	FilesCopied   int            // bundled файли (скопійовано з архіву)
	FilesRemoved  int            // видалені файли (при оновленні)
	IsUpdate      bool           // чи це оновлення
	InstalledInfo *InstalledInfo // для збереження в instance.json
}

// ReadManifest читає manifest.json з .shaurmpack файлу без розпаковування.
// Використовується для перевірки версії та показу інформації перед імпортом.
func ReadManifest(shaurmPackPath string) (*Manifest, error) {
	r, err := zip.OpenReader(shaurmPackPath)
	if err != nil {
		return nil, fmt.Errorf("відкриття .shaurmpack: %w", err)
	}
	defer r.Close()
	if err := validatePackArchive(r.File, 8<<30); err != nil {
		return nil, err
	}

	for _, f := range r.File {
		if f.Name == ManifestFile {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()

			var manifest Manifest
			if err := json.NewDecoder(io.LimitReader(rc, 16<<20)).Decode(&manifest); err != nil {
				return nil, fmt.Errorf("парсинг manifest.json: %w", err)
			}
			if manifest.Version == 0 {
				manifest.Version = 1 // backward compat
			}
			return &manifest, nil
		}
	}
	return nil, fmt.Errorf("manifest.json не знайдено в архіві")
}

// CheckUpdate перевіряє чи потрібне оновлення.
// Повертає true якщо newVersion більша за installedVersion.
func CheckUpdate(installed *InstalledInfo, newVersion string) bool {
	if installed == nil {
		return false
	}
	return newVersion != "" && newVersion != installed.SourceVersion
}

// Import розпаковує .shaurmpack в цільову теку.
//
// Алгоритм:
// 1. Читає manifest.json
// 2. Визначає чи це оновлення (порівняння версій)
// 3. Розпаковує bundled/ файли (які не мають онлайн-джерела)
// 4. Завантажує ResolveDownload файли (CurseForge/Modrinth)
// 5. Розпаковує overrides/ файли в .minecraft/
// 6. При оновленні — видаляє файли яких немає в новій версії
func Import(opts ImportOptions) (*ImportResult, error) {
	if opts.ShaurmPackPath == "" {
		return nil, fmt.Errorf("shaurmPackPath порожній")
	}
	if opts.TargetGameDir == "" {
		return nil, fmt.Errorf("targetGameDir порожній")
	}

	r, err := zip.OpenReader(opts.ShaurmPackPath)
	if err != nil {
		return nil, fmt.Errorf("відкриття .shaurmpack: %w", err)
	}
	defer r.Close()
	if err := validatePackArchive(r.File, 8<<30); err != nil {
		return nil, err
	}

	// 1. Читаємо manifest з відкритого архіву
	var manifest Manifest
	foundManifest := false
	for _, f := range r.File {
		if f.Name == ManifestFile {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			if err := json.NewDecoder(io.LimitReader(rc, 16<<20)).Decode(&manifest); err != nil {
				return nil, fmt.Errorf("парсинг manifest.json: %w", err)
			}
			foundManifest = true
			break
		}
	}
	if !foundManifest {
		return nil, fmt.Errorf("manifest.json не знайдено")
	}

	// Reject all untrusted paths before extracting or invoking a resolver.
	for _, pf := range manifest.Files {
		if err := shsync.ValidateRelPath(pf.Path); err != nil {
			return nil, err
		}
	}
	for _, f := range r.File {
		for _, prefix := range []string{OverridesDir, BundledDir} {
			if strings.HasPrefix(f.Name, prefix) {
				rel := strings.TrimSuffix(strings.TrimPrefix(f.Name, prefix), "/")
				if rel == "" {
					continue
				}
				if _, err := safeDestination(opts.TargetGameDir, rel); err != nil {
					return nil, err
				}
				if f.Mode()&os.ModeSymlink != 0 {
					return nil, fmt.Errorf("archive symlink: %s", f.Name)
				}
			}
		}
	}

	result := &ImportResult{
		Manifest: &manifest,
		GameDir:  opts.TargetGameDir,
		IsUpdate: opts.UpdateOf != nil,
	}

	// 2. Перевірка оновлення
	if opts.UpdateOf != nil {
		if !CheckUpdate(opts.UpdateOf, manifest.VersionID) {
			// Версія не змінилась
			result.IsUpdate = false
		}
	}

	// Збираємо set файлів в новому паку для видалення старих
	newFiles := make(map[string]bool, len(manifest.Files))
	for _, f := range manifest.Files {
		newFiles[f.Path] = true
	}

	// Файли, які будуть завантажені з онлайн-джерела: їх НЕ розпаковуємо з
	// overrides/ (старі паки клали туди дублікат кожного мода — подвійна
	// робота). Якщо завантаження не вдасться — фолбек з bundled/ або overrides/.
	// Без ResolveDownload качати нічого, тож розпаковуємо все, що є в архіві.
	var toDownload []PackFile
	skipExtract := map[string]bool{}
	canDownload := opts.ResolveDownload != nil || opts.ResolveDownloadHashed != nil
	if canDownload {
		for _, pf := range manifest.Files {
			if pf.Bundled || !pf.HasWorkingSource() || (pf.CurseForge != nil && pf.CurseForge.Blocked && !workingModrinth(pf.Modrinth)) {
				continue
			}
			toDownload = append(toDownload, pf)
			skipExtract[pf.Path] = true
		}
	}

	ctx := opts.Context
	if ctx == nil {
		ctx = context.Background()
	}
	// Загальна кількість кроків прогресу: файли до розпакування + файли до
	// завантаження.
	nExtract := 0
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		switch {
		case strings.HasPrefix(f.Name, OverridesDir):
			if rel := strings.TrimPrefix(f.Name, OverridesDir); rel != "" && !skipExtract[rel] {
				nExtract++
			}
		case strings.HasPrefix(f.Name, BundledDir):
			if strings.TrimPrefix(f.Name, BundledDir) != "" {
				nExtract++
			}
		}
	}
	progTotal := nExtract + len(toDownload)
	progDone := 0
	var progMu sync.Mutex
	report := func(stage, file string) {
		if opts.Progress == nil {
			return
		}
		progMu.Lock()
		progDone++
		opts.Progress(ImportProgress{Stage: stage, FilesDone: progDone, FilesTotal: progTotal, CurrentFile: file})
		progMu.Unlock()
	}
	if opts.Progress != nil {
		opts.Progress(ImportProgress{Stage: ImportStageExtracting, FilesTotal: progTotal})
	}

	// 3a. Завантаження стартують ОДРАЗУ і йдуть паралельно з розпаковуванням
	// overrides/bundled. Шляхи не перетинаються: файли з toDownload у skipExtract
	// і з архіву не розпаковуються; зайві запити лише на фолбек у
	// download-воркері, а zip.Reader безпечний для паралельного читання. Великі
	// файли качаємо першими, щоб найдовший не стартував останнім і не розтягував
	// «хвіст» імпорту.
	sort.SliceStable(toDownload, func(i, j int) bool { return toDownload[i].Size > toDownload[j].Size })
	var resMu sync.Mutex
	var downloadErr error
	dctx, dcancel := context.WithCancel(ctx)
	dlDone := make(chan struct{})
	// Помилка розпакування чи скасування: зупиняємо нові завантаження й
	// дочікуємось воркерів ДО закриття архіву.
	defer func() { dcancel(); <-dlDone }()
	if len(toDownload) == 0 {
		close(dlDone)
	} else {
		go func() {
			defer close(dlDone)
			mu := &resMu
			runPool(dctx, len(toDownload), func(i int) {
				pf := toDownload[i]
				destPath, pathErr := safeDestination(opts.TargetGameDir, pf.Path)
				if pathErr != nil {
					mu.Lock()
					if downloadErr == nil {
						downloadErr = pathErr
					}
					mu.Unlock()
					return
				}
				// Відновлення після паузи: уже завантажений цілий файл не качаємо.
				if opts.SkipExisting && fileMatches(destPath, pf) {
					mu.Lock()
					result.FilesLinked++
					result.FilesTotal++
					mu.Unlock()
					report(ImportStageDownloading, pf.Path)
					return
				}
				// Тека призначення може не існувати (з архіву моди більше не
				// розпаковуються) — створюємо, щоб резолвер не мусив цього знати.
				err := os.MkdirAll(opts.TargetGameDir, 0755)
				if err == nil {
					err = resolveVerified(opts, pf)
				}

				// Старі паки: якщо завантаження не вдалось — беремо копію з
				// архіву (bundled/ або overrides/), якщо вона там є.
				copied := err != nil && extractArchiveFallback(r, pf.Path, opts.TargetGameDir)
				mu.Lock()
				switch {
				case err == nil:
					result.FilesLinked++
					result.FilesTotal++
				case copied:
					result.FilesCopied++
				default:
					if downloadErr == nil {
						downloadErr = err
					}
				}
				mu.Unlock()
				report(ImportStageDownloading, pf.Path)
			})
		}()
	}

	// 3. Проходимо по ZIP файлах
	for _, f := range r.File {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name := f.Name

		switch {
		// Override файли
		case strings.HasPrefix(name, OverridesDir):
			relPath := strings.TrimPrefix(name, OverridesDir)
			if relPath == "" || skipExtract[relPath] {
				continue
			}
			_, err := safeDestination(opts.TargetGameDir, relPath)
			if err != nil {
				return nil, err
			}

			if err := extractFile(f, opts.TargetGameDir, relPath); err != nil {
				return nil, fmt.Errorf("розпакування override %s: %w", relPath, err)
			}
			resMu.Lock()
			result.FilesCopied++
			resMu.Unlock()
			report(ImportStageExtracting, relPath)

		// Bundled файли
		case strings.HasPrefix(name, BundledDir):
			relPath := strings.TrimPrefix(name, BundledDir)
			if relPath == "" {
				continue
			}
			_, err := safeDestination(opts.TargetGameDir, relPath)
			if err != nil {
				return nil, err
			}

			if err := extractFile(f, opts.TargetGameDir, relPath); err != nil {
				return nil, fmt.Errorf("розпакування bundled %s: %w", relPath, err)
			}
			resMu.Lock()
			result.FilesCopied++
			resMu.Unlock()
			report(ImportStageExtracting, relPath)

		// Metadata
		case strings.HasPrefix(name, MetadataDir):
			// Обробляється окремо якщо потрібно
		}
	}

	// 4. Паралельне завантаження файлів з онлайн-джерел (ліміт workerCount).
	//
	// Заблоковані CurseForge-файли (Blocked, без робочого Modrinth) сюди не
	// потрапляють: нові паки кладуть їх у bundled/ (вже розпаковано вище), а
	// для неконсистентного/ручного .shaurmpack (Bundled=false, але Blocked) —
	// страховий фолбек з bundled/ (потім overrides/), без запиту по порожньому
	// DownloadURL.
	if canDownload {
		for _, pf := range manifest.Files {
			if !pf.Bundled && pf.CurseForge != nil && pf.CurseForge.Blocked && !workingModrinth(pf.Modrinth) {
				if extractArchiveFallback(r, pf.Path, opts.TargetGameDir) {
					resMu.Lock()
					result.FilesCopied++
					resMu.Unlock()
				}
			}
		}
	}
	// Дочікуємось завантажень, що йшли паралельно з розпаковуванням.
	<-dlDone
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	resMu.Lock()
	dlErr := downloadErr
	resMu.Unlock()
	if dlErr != nil {
		return nil, dlErr
	}
	if opts.Progress != nil {
		opts.Progress(ImportProgress{Stage: ImportStageFinalizing, FilesDone: progTotal, FilesTotal: progTotal})
	}

	// 5. Видалення старих файлів при оновленні
	if opts.UpdateOf != nil && opts.DeleteRemoved {
		for _, oldFile := range getInstalledFiles(opts.UpdateOf) {
			if !newFiles[oldFile] {
				delPath, err := safeDestination(opts.TargetGameDir, oldFile)
				if err != nil {
					return nil, err
				}
				if err := os.Remove(delPath); err == nil {
					result.FilesRemoved++
				}
			}
		}
	}

	// Створюємо InstalledInfo для збереження в instance.json
	result.InstalledInfo = &InstalledInfo{
		SourcePackID:  manifest.PackID,
		SourceVersion: manifest.VersionID,
		InstalledAt:   manifest.CreatedAt,
	}

	return result, nil
}

// extractArchiveFallback шукає relPath у bundled/, а потім в overrides/
// всередині вже відкритого архіву й розпаковує в targetGameDir. true — файл
// знайдено й розпаковано. Використовується для заблокованих CurseForge-файлів
// і як фолбек при провалі ResolveDownload (старі паки мають дублікат у
// overrides/).
func extractArchiveFallback(r *zip.ReadCloser, relPath, targetGameDir string) bool {
	for _, prefix := range []string{BundledDir, OverridesDir} {
		want := prefix + relPath
		for _, zf := range r.File {
			if zf.Name == want {
				_, err := safeDestination(targetGameDir, relPath)
				if err != nil || zf.Mode()&os.ModeSymlink != 0 {
					return false
				}
				return extractFile(zf, targetGameDir, relPath) == nil
			}
		}
	}
	return false
}

// fileMatches — файл існує, має розмір з маніфесту і (якщо відомий) той самий SHA-1.
func fileMatches(path string, pf PackFile) bool {
	st, err := os.Stat(path)
	if err != nil || st.IsDir() || (pf.Size > 0 && st.Size() != pf.Size) {
		return false
	}
	if pf.Hash == "" {
		return pf.Size > 0
	}
	h, err := fileSHA1(path)
	return err == nil && strings.EqualFold(h, pf.Hash)
}

// extractFile витягує один файл з ZIP в указану теку.
func extractFile(f *zip.File, base, rel string) error {
	if _, err := safeDestination(base, strings.TrimSuffix(rel, "/")); err != nil {
		return err
	}
	if f.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("archive symlink")
	}
	if err := os.MkdirAll(base, 0755); err != nil {
		return err
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		return err
	}
	defer root.Close()
	destPath := filepath.FromSlash(strings.ReplaceAll(rel, "\\", "/"))

	// Створюємо директорію
	if err := root.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return err
	}

	// Пропускаємо директорії
	if f.FileInfo().IsDir() {
		return nil
	}

	src, err := f.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := root.OpenFile(destPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer dst.Close()

	_, err = io.Copy(dst, src)
	return err
}

// ReadPersonalization читає metadata/personalization.json з .shaurmpack.
func ReadPersonalization(shaurmPackPath string) (*Personalization, error) {
	r, err := zip.OpenReader(shaurmPackPath)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	if err := validatePackArchive(r.File, 8<<30); err != nil {
		return nil, err
	}

	for _, f := range r.File {
		if f.Name == MetadataDir+"personalization.json" {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()

			var p Personalization
			if err := json.NewDecoder(io.LimitReader(rc, 16<<20)).Decode(&p); err != nil {
				return nil, err
			}
			return &p, nil
		}
	}
	return nil, nil
}

// getInstalledFiles повертає список файлів з попереднього встановлення.
// Тимчасово повертає порожній список — реалізація залежить від store.
func getInstalledFiles(info *InstalledInfo) []string {
	_ = info
	return nil
}

// safeDestination also rejects existing links, which lexical containment cannot detect.
func safeDestination(base, rel string) (string, error) {
	if err := shsync.ValidateRelPath(rel); err != nil {
		return "", err
	}
	dest, err := shsync.SafeJoin(base, rel)
	if err != nil {
		return "", err
	}
	current := base
	for _, part := range strings.Split(strings.ReplaceAll(rel, "\\", "/"), "/") {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil && !os.IsNotExist(err) {
			return "", err
		}
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("symlink destination: %s", rel)
		}
	}
	return dest, nil
}

// Resolve into an isolated staging directory; publish only verified bytes through a root handle.
func resolveVerified(opts ImportOptions, pf PackFile) error {
	if pf.Hash == "" {
		return fmt.Errorf("missing download hash: %s", pf.Path)
	}
	if opts.ResolveDownloadHashed != nil {
		return resolveVerifiedStreaming(opts, pf)
	}
	staging, err := os.MkdirTemp("", "shaurmpack-download-")
	if err != nil {
		return err
	}
	defer safedel.RemoveAll(staging)
	tmp := filepath.Join(staging, "download")
	if err := opts.ResolveDownload(tmp, pf); err != nil {
		return err
	}
	if !fileMatches(tmp, pf) {
		return fmt.Errorf("download integrity mismatch: %s", pf.Path)
	}
	src, err := os.Open(tmp)
	if err != nil {
		return err
	}
	defer src.Close()
	root, err := os.OpenRoot(opts.TargetGameDir)
	if err != nil {
		return err
	}
	defer root.Close()
	rel := filepath.FromSlash(strings.ReplaceAll(pf.Path, "\\", "/"))
	if err := root.MkdirAll(filepath.Dir(rel), 0755); err != nil {
		return err
	}
	dst, err := root.OpenFile(rel, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	_, err = io.Copy(dst, src)
	closeErr := dst.Close()
	if err != nil {
		return err
	}
	return closeErr
}

// Bound archive work before extraction or JSON allocation.
func validatePackArchive(files []*zip.File, maxTotal uint64) error {
	if len(files) > 100000 {
		return fmt.Errorf("too many pack archive entries")
	}
	var total uint64
	for _, f := range files {
		if f.UncompressedSize64 > 2<<30 {
			return fmt.Errorf("pack entry exceeds 2 GiB: %s", f.Name)
		}
		total += f.UncompressedSize64
		if total > maxTotal {
			return fmt.Errorf("pack archive exceeds size limit (%d bytes)", maxTotal)
		}
	}
	return nil
}

// resolveVerifiedStreaming — завантаження з потоковою перевіркою SHA-1.
//
// Файл качається одразу в теку призначення під тимчасовим іменем
// <файл>.part (та сама файлова система, тож фінальний крок — миттєве
// перейменування, а не копіювання). SHA-1 рахується резолвером на льоту, поки
// байти йдуть на диск, тому окремих проходів «перечитати для хешу» і
// «скопіювати з temp у збірку» більше немає. Безпека та сама, що й раніше:
//   - шлях розв'язується safeDestination (без symlink і виходу за корінь),
//     а тека створюється через os.Root;
//   - у фінальному імені файл з'являється лише після звірки розміру й SHA-1;
//   - при будь-якій помилці .part видаляється.
func resolveVerifiedStreaming(opts ImportOptions, pf PackFile) error {
	final, err := safeDestination(opts.TargetGameDir, pf.Path)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(opts.TargetGameDir)
	if err != nil {
		return err
	}
	defer root.Close()
	rel := filepath.FromSlash(strings.ReplaceAll(pf.Path, "\\", "/"))
	if err := root.MkdirAll(filepath.Dir(rel), 0755); err != nil {
		return err
	}
	stage := final + ".part"
	// Лишок попереднього обриву (або підкинутий symlink) прибираємо самі —
	// Remove видаляє сам запис, а не те, на що він вказує.
	_ = root.Remove(rel + ".part")
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(stage)
		}
	}()
	got, err := opts.ResolveDownloadHashed(stage, pf)
	if err != nil {
		return err
	}
	st, err := os.Lstat(stage)
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() || (pf.Size > 0 && st.Size() != pf.Size) || !strings.EqualFold(got, pf.Hash) {
		return fmt.Errorf("download integrity mismatch: %s", pf.Path)
	}
	if err := os.Rename(stage, final); err != nil {
		return err
	}
	ok = true
	return nil
}
