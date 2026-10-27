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
	Manifest     *Manifest      // прочитаний маніфест
	GameDir      string         // куди розпаковано
	FilesTotal   int            // загальна кількість файлів
	FilesLinked  int            // файли з онлайн-джерелом (download)
	FilesCopied  int            // bundled файли (скопійовано з архіву)
	FilesRemoved int            // видалені файли (при оновленні)
	IsUpdate     bool           // чи це оновлення
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

	for _, f := range r.File {
		if f.Name == ManifestFile {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()

			var manifest Manifest
			if err := json.NewDecoder(rc).Decode(&manifest); err != nil {
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
			if err := json.NewDecoder(rc).Decode(&manifest); err != nil {
				return nil, fmt.Errorf("парсинг manifest.json: %w", err)
			}
			foundManifest = true
			break
		}
	}
	if !foundManifest {
		return nil, fmt.Errorf("manifest.json не знайдено")
	}

	result := &ImportResult{
		Manifest:  &manifest,
		GameDir:   opts.TargetGameDir,
		IsUpdate:  opts.UpdateOf != nil,
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
	if opts.ResolveDownload != nil {
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
			destPath := filepath.Join(opts.TargetGameDir, filepath.FromSlash(relPath))

			if err := extractFile(f, destPath); err != nil {
				return nil, fmt.Errorf("розпакування override %s: %w", relPath, err)
			}
			result.FilesCopied++
			report(ImportStageExtracting, relPath)

		// Bundled файли
		case strings.HasPrefix(name, BundledDir):
			relPath := strings.TrimPrefix(name, BundledDir)
			if relPath == "" {
				continue
			}
			destPath := filepath.Join(opts.TargetGameDir, filepath.FromSlash(relPath))

			if err := extractFile(f, destPath); err != nil {
				return nil, fmt.Errorf("розпакування bundled %s: %w", relPath, err)
			}
			result.FilesCopied++
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
	if opts.ResolveDownload != nil {
		for _, pf := range manifest.Files {
			if !pf.Bundled && pf.CurseForge != nil && pf.CurseForge.Blocked && !workingModrinth(pf.Modrinth) {
				if extractArchiveFallback(r, pf.Path, opts.TargetGameDir) {
					result.FilesCopied++
				}
			}
		}
	}
	if len(toDownload) > 0 {
		var mu sync.Mutex
		runPool(ctx, len(toDownload), func(i int) {
			pf := toDownload[i]
			destPath := filepath.Join(opts.TargetGameDir, filepath.FromSlash(pf.Path))
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
			err := os.MkdirAll(filepath.Dir(destPath), 0755)
			if err == nil {
				err = opts.ResolveDownload(destPath, pf)
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
			}
			mu.Unlock()
			report(ImportStageDownloading, pf.Path)
		})
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	if opts.Progress != nil {
		opts.Progress(ImportProgress{Stage: ImportStageFinalizing, FilesDone: progTotal, FilesTotal: progTotal})
	}

	// 5. Видалення старих файлів при оновленні
	if opts.UpdateOf != nil && opts.DeleteRemoved {
		for _, oldFile := range getInstalledFiles(opts.UpdateOf) {
			if !newFiles[oldFile] {
				delPath := filepath.Join(opts.TargetGameDir, filepath.FromSlash(oldFile))
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
				destPath := filepath.Join(targetGameDir, filepath.FromSlash(relPath))
				return extractFile(zf, destPath) == nil
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
func extractFile(f *zip.File, destPath string) error {
	// Створюємо директорію
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
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

	dst, err := os.Create(destPath)
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

	for _, f := range r.File {
		if f.Name == MetadataDir+"personalization.json" {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()

			var p Personalization
			if err := json.NewDecoder(rc).Decode(&p); err != nil {
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
