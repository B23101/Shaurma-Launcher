// Package download качає компоненти оновлення в тимчасову теку,
// звіряє SHA256 і повертає готові файли для install.Apply.
//
// Свідомо просте послідовне завантаження (не паралельне): shrm-updater —
// маленький, максимально незалежний і надійний бінарник, який має
// працювати навіть якщо в основному лаунчері (download-логіка) є баг.
// Компонент рівно один (launcher.exe, десятки МБ) — паралелізм тут не
// дає виграшу, тільки зайву складність.
package download

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"shrm-updater/internal/manifest"
)

// Progress — колбек прогресу для одного файлу, що якраз качається.
type Progress struct {
	FileIndex  int
	FileTotal  int
	FileName   string
	BytesDone  int64
	BytesTotal int64
}

type ProgressFunc func(Progress)

const (
	maxRetries    = 3
	retryBaseWait = 2 * time.Second
)

// DownloadedComponent — компонент, готовий до встановлення: лежить у
// tmpDir під тим самим ім'ям файлу (без підпапок — компонентів мало,
// плоска структура простіша й надійніша).
type DownloadedComponent struct {
	manifest.Component
	TempPath string
}

// Download качає всі components у tmpDir з ретраями і перевіркою
// SHA256. token — X-Shaurma-Token для CDN (обов'язковий, worker.js
// повертає 401 без нього, незалежно від каналу). Скасування — через
// ctx (наприклад, користувач натиснув "Скасувати" у вікні оновлення).
func Download(ctx context.Context, baseURL, token, tmpDir string, components []manifest.Component, lang string, onProgress ProgressFunc) ([]DownloadedComponent, error) {
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		if lang == "en" {
			return nil, fmt.Errorf("creating temp dir: %w", err)
		}
		return nil, fmt.Errorf("створення тимчасової теки: %w", err)
	}

	client := &http.Client{Timeout: 0} // без загального таймауту — керуємо через ctx

	results := make([]DownloadedComponent, 0, len(components))
	for i, c := range components {
		select {
		case <-ctx.Done():
			return results, ctx.Err()
		default:
		}

		tempPath := filepath.Join(tmpDir, filepath.Base(c.Path))

		var lastErr error
		ok := false
		for attempt := 1; attempt <= maxRetries; attempt++ {
			if err := downloadOne(ctx, client, baseURL, token, c, tempPath, i, len(components), onProgress); err != nil {
				lastErr = err
				select {
				case <-ctx.Done():
					return results, ctx.Err()
				case <-time.After(retryBaseWait * time.Duration(attempt)):
				}
				continue
			}
			ok = true
			break
		}
		if !ok {
			if lang == "en" {
				return results, fmt.Errorf("failed to download %s after %d attempts: %w", c.Path, maxRetries, lastErr)
			}
			return results, fmt.Errorf("завантаження %s не вдалось після %d спроб: %w", c.Path, maxRetries, lastErr)
		}

		results = append(results, DownloadedComponent{Component: c, TempPath: tempPath})
	}

	return results, nil
}

func downloadOne(ctx context.Context, client *http.Client, baseURL, token string, c manifest.Component, tempPath string, index, total int, onProgress ProgressFunc) error {
	url := baseURL + c.URL
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	// User-Agent обов'язковий — worker.js фільтрує усі запити без
	// префіксу ShaurmaLauncher/ (див. shrm-cdn-worker/worker.js, крок
	// 5 у fetch handler), інакше отримаємо 404 замість файлу.
	req.Header.Set("User-Agent", "ShaurmaLauncher/updater")
	req.Header.Set("X-Shaurma-Token", token)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, url)
	}

	// Пишемо в .part, атомарно перейменовуємо в кінці — якщо процес
	// впаде посеред скачування, наступний запуск не побачить
	// напівзавантажений файл як валідний (Diff все одно перекачає, бо
	// хеш не зійдеться, але .part не заважає паралельним спробам).
	partPath := tempPath + ".part"
	out, err := os.Create(partPath)
	if err != nil {
		return err
	}

	h := sha256.New()
	multi := io.MultiWriter(out, h)

	var bytesDone int64
	total64 := c.Size
	if resp.ContentLength > 0 {
		total64 = resp.ContentLength
	}

	buf := make([]byte, 256*1024)
	for {
		select {
		case <-ctx.Done():
			out.Close()
			os.Remove(partPath)
			return ctx.Err()
		default:
		}

		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := multi.Write(buf[:n]); werr != nil {
				out.Close()
				os.Remove(partPath)
				return werr
			}
			bytesDone += int64(n)
			if onProgress != nil {
				onProgress(Progress{
					FileIndex:  index,
					FileTotal:  total,
					FileName:   c.Path,
					BytesDone:  bytesDone,
					BytesTotal: total64,
				})
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			out.Close()
			os.Remove(partPath)
			return readErr
		}
	}
	out.Close()

	got := hex.EncodeToString(h.Sum(nil))
	if got != c.SHA256 {
		os.Remove(partPath)
		return fmt.Errorf("SHA256 mismatch for %s: expected %s, got %s", c.Path, c.SHA256, got)
	}

	return os.Rename(partPath, tempPath)
}
