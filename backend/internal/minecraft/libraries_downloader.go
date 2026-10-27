package minecraft

import (
	"shaurma-launcher-backend/internal/crashguard"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
)

// LibrariesDownloader відповідає за завантаження всіх бібліотек.
// Портовано з Prism: launcher/minecraft/update/LibrariesTask.cpp
//
// Логіка (ТОЧНО як у Prism):
//   1. Збір всіх бібліотек з LaunchProfile
//   2. Фільтрація за активністю (rules)
//   3. Паралельне завантаження
//   4. Перевірка SHA1
//   5. Перекачка пошкоджених файлів
type LibrariesDownloader struct {
	client     *http.Client
	maxWorkers int
	onProgress func(current, total int)
	onLog      func(msg string)
}

// NewLibrariesDownloader створює новий downloader.
func NewLibrariesDownloader() *LibrariesDownloader {
	return &LibrariesDownloader{
		client: &http.Client{
			Timeout: 0, // без timeout для великих файлів
		},
		maxWorkers: 6, // як у Prism
	}
}

// DownloadResult - результат завантаження одного файлу.
type DownloadResult struct {
	Library *Library
	Path    string
	Error   error
}

// DownloadLibraries завантажує всі бібліотеки з профілю.
// Портовано з Prism: LibrariesTask::executeTask()
func (ld *LibrariesDownloader) DownloadLibraries(
	profile *LaunchProfile,
	ctx *RuntimeContext,
	basePath string,
) error {
	
	// 1. Збір всіх бібліотек для завантаження
	var allLibraries []*Library
	
	// Звичайні бібліотеки
	allLibraries = append(allLibraries, profile.Libraries...)
	
	// Нативні бібліотеки
	allLibraries = append(allLibraries, profile.NativeLibraries...)
	
	// Maven файли
	allLibraries = append(allLibraries, profile.MavenFiles...)
	
	// Головний JAR
	if profile.MainJar != nil {
		allLibraries = append(allLibraries, profile.MainJar)
	}
	
	// 2. Фільтрація: лише ті що потребують завантаження
	toDownload := make([]*Library, 0)
	failedLocal := make([]*Library, 0)
	
	for _, lib := range allLibraries {
		// Перевірка чи є локальною
		if lib.IsLocal() {
			// Перевірка існування
			path := lib.ActualPath(ctx, basePath)
			if _, err := os.Stat(path); err != nil {
				failedLocal = append(failedLocal, lib)
			}
			continue
		}
		
		// Перевірка чи треба завантажувати
		if ld.needsDownload(lib, ctx, basePath) {
			toDownload = append(toDownload, lib)
		}
	}
	
	// Помилка якщо локальні файли відсутні
	if len(failedLocal) > 0 {
		return fmt.Errorf("відсутні локальні бібліотеки: %d файлів", len(failedLocal))
	}
	
	// 3. Паралельне завантаження
	if len(toDownload) > 0 {
		ld.log(fmt.Sprintf("Завантаження %d бібліотек...", len(toDownload)))
		
		if err := ld.downloadParallel(toDownload, ctx, basePath); err != nil {
			return err
		}
	}
	
	ld.log("Всі бібліотеки завантажено")
	return nil
}

// needsDownload перевіряє чи потрібно завантажувати бібліотеку.
// Портовано з Prism логіки.
func (ld *LibrariesDownloader) needsDownload(
	lib *Library,
	ctx *RuntimeContext,
	basePath string,
) bool {
	path := lib.ActualPath(ctx, basePath)
	
	// 1. Файл не існує → завантажити
	info, err := os.Stat(path)
	if err != nil {
		return true
	}
	
	// 2. Файл порожній → завантажити (Prism перевіряє size > 0)
	if info.Size() == 0 {
		return true
	}
	
	// 3. Always stale → завжди завантажити
	if lib.IsAlwaysStale() {
		return true
	}
	
	// 4. Перевірка SHA1 (якщо є)
	_, expectedSHA1 := lib.GetDownloadURL(ctx)
	if expectedSHA1 != "" {
		actualSHA1, err := ld.calculateSHA1(path)
		if err != nil || actualSHA1 != expectedSHA1 {
			return true // SHA1 не співпадає → перекачати
		}
	}
	
	// Файл валідний → не завантажувати
	return false
}

// downloadParallel завантажує бібліотеки паралельно.
// Портовано з Prism: NetJob з worker pool.
func (ld *LibrariesDownloader) downloadParallel(
	libraries []*Library,
	ctx *RuntimeContext,
	basePath string,
) error {
	
	total := len(libraries)
	results := make(chan DownloadResult, total)
	jobs := make(chan *Library, total)
	
	// Worker pool
	var wg sync.WaitGroup
	for i := 0; i < ld.maxWorkers; i++ {
		wg.Add(1)
		go func() {
			defer crashguard.Recover("libraries_downloader.go:165")
			defer wg.Done()
			for lib := range jobs {
				result := ld.downloadOne(lib, ctx, basePath)
				results <- result
			}
		}()
	}
	
	// Додаємо завдання
	for _, lib := range libraries {
		jobs <- lib
	}
	close(jobs)
	
	// Чекаємо завершення
	go func() {
		defer crashguard.Recover("libraries_downloader.go:181")
		wg.Wait()
		close(results)
	}()
	
	// Збір результатів
	completed := 0
	var errors []error
	
	for result := range results {
		completed++
		
		if ld.onProgress != nil {
			ld.onProgress(completed, total)
		}
		
		if result.Error != nil {
			errors = append(errors, result.Error)
			ld.log(fmt.Sprintf("Помилка: %s - %v", result.Library.String(), result.Error))
		} else {
			ld.log(fmt.Sprintf("Завантажено: %s", result.Library.String()))
		}
	}
	
	// Повертаємо першу помилку
	if len(errors) > 0 {
		return fmt.Errorf("не вдалося завантажити %d бібліотек", len(errors))
	}
	
	return nil
}

// downloadOne завантажує одну бібліотеку з retry.
// Портовано з Prism: Net::ApiDownload
func (ld *LibrariesDownloader) downloadOne(
	lib *Library,
	ctx *RuntimeContext,
	basePath string,
) DownloadResult {
	
	url, expectedSHA1 := lib.GetDownloadURL(ctx)
	path := lib.ActualPath(ctx, basePath)
	
	// Створюємо директорію
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return DownloadResult{lib, path, err}
	}
	
	// Retry до 3 разів (як у Prism)
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		err := ld.downloadFile(url, path, expectedSHA1)
		if err == nil {
			return DownloadResult{lib, path, nil}
		}
		lastErr = err
		
		// Видаляємо пошкоджений файл
		os.Remove(path)
	}
	
	return DownloadResult{lib, path, lastErr}
}

// downloadFile завантажує файл з URL.
func (ld *LibrariesDownloader) downloadFile(url, path, expectedSHA1 string) error {
	// GET запит
	resp, err := ld.client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	
	// Створюємо тимчасовий файл
	tmpPath := path + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	defer f.Close()
	
	// Копіюємо дані
	hash := sha1.New()
	writer := io.MultiWriter(f, hash)
	
	_, err = io.Copy(writer, resp.Body)
	if err != nil {
		os.Remove(tmpPath)
		return err
	}
	
	f.Close()
	
	// Перевірка SHA1
	if expectedSHA1 != "" {
		actualSHA1 := hex.EncodeToString(hash.Sum(nil))
		if actualSHA1 != expectedSHA1 {
			os.Remove(tmpPath)
			return fmt.Errorf("SHA1 mismatch: expected %s, got %s", expectedSHA1, actualSHA1)
		}
	}
	
	// Переміщуємо на місце
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	
	return nil
}

// calculateSHA1 обчислює SHA1 файлу.
func (ld *LibrariesDownloader) calculateSHA1(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	
	hash := sha1.New()
	if _, err := io.Copy(hash, f); err != nil {
		return "", err
	}
	
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// SetProgressHandler встановлює обробник прогресу.
func (ld *LibrariesDownloader) SetProgressHandler(f func(current, total int)) {
	ld.onProgress = f
}

// SetLogHandler встановлює обробник логів.
func (ld *LibrariesDownloader) SetLogHandler(f func(msg string)) {
	ld.onLog = f
}

func (ld *LibrariesDownloader) log(msg string) {
	if ld.onLog != nil {
		ld.onLog(msg)
	}
}
