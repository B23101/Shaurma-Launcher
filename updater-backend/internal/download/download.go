// Package download качає компоненти оновлення в тимчасову теку і повертає
// готові файли для install.Apply.
//
// Завантаження — ПАРАЛЕЛЬНЕ (кілька файлів одночасно, з обмеженням
// concurrency): так оновлення з кількох компонентів качається в рази
// швидше, ніж послідовно. Прогрес — СПІЛЬНИЙ: фронтенд отримує агреговані
// байти по ВСІХ файлах (один спільний прогрес-бар), а не поточний файл
// окремо. Назви компонентів користувачу не показуються (це внутрішні
// деталі — користувачу треба лише «скільки відсотків»).
package download

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"shrm-updater-backend/internal/manifest"
	"shrm-updater-backend/internal/neterr"
)

// Progress — колбек прогресу завантаження. BytesDone/BytesTotal — байти
// ПОТОЧНОГО файлу; TotalBytesDone/TotalBytesTotal — агреговані байти по
// ВСІХ файлах (для спільного прогрес-бара). FileName лишається лише для
// внутрішніх логів — фронтенд його не показує.
type Progress struct {
	FileIndex  int
	FileTotal  int
	FileName   string
	BytesDone  int64
	BytesTotal int64

	// Агрегований прогрес по всьому завантаженню.
	TotalBytesDone  int64
	TotalBytesTotal int64
}

type ProgressFunc func(Progress)

const (
	maxRetries    = 3
	retryBaseWait = 2 * time.Second

	// maxConcurrent — скільки файлів качається одночасно. 4 — добрий
	// баланс швидкості й навантаження на канал/CDN.
	maxConcurrent = 4

	// downloadTimeout — НАЙБІЛЬШИЙ період, за який має завантажитись ОДИН
	// файл. Client{Timeout: 0} (без обмежень) означав: обірване з'єднання
	// або "застряглий" сервер (перестав слати байти, але не закрив сокет)
	// тримали апдейтер на splash ВІЧНО на одному відсотку (реальний
	// інцидент: "завис на 90% і не перемикає на новий екран"). 10 хвилин
	// з запасом покривають 32МБ backend.dll навіть на повільному каналі,
	// але гарантують, що процес ВИЙДЕ зі стану зависання (поверне
	// помилку, retry, і зрештою — картку помилки замість вічного splash).
	downloadTimeout = 10 * time.Minute
)

// DownloadedComponent — компонент, готовий до встановлення: лежить у
// tmpDir під тим самим ім'ям файлу (без підпапок — компонентів мало,
// плоска структура простіша й надійніша).
type DownloadedComponent struct {
	manifest.Component
	TempPath string
}

// Download качає всі components у tmpDir ПАРАЛЕЛЬНО (до maxConcurrent
// файлів одночасно) з ретраями. token — X-Shaurma-Token для CDN
// (обов'язковий, worker.js повертає 401 без нього, незалежно від каналу).
// Скасування — через ctx (наприклад, користувач натиснув "Скасувати" у
// вікні оновлення).
//
// Прогрес — спільний: колбек отримує агреговані байти по всіх файлах,
// тож фронтенд малює ОДИН спільний прогрес-бар, а не поточний файл.
//
// SHA256 і розмір звіряються ЖОРСТКО (див. verifyIntegrity); історично було м'яко: кожен файл
// перевіряється на збіг з хешем маніфесту, але розбіжність лише пишеться
// в update-sha-warnings.log і НЕ зупиняє оновлення. Рішення «що качати»
// приймається ЗА ВЕРСІЄЮ в core.Run (нова версія = качаємо всі
// компоненти), а не за збігом хешів — застарілий чи неузгоджений файл на
// CDN не має зупиняти оновлення (раніше жорстка sha-перевірка на
// півшляху ламала качку і лаунчер лишався без стилів). М'яка перевірка
// дає діагностику «CDN віддав інший файл» без ризику для оновлення.
func Download(ctx context.Context, baseURL, token, tmpDir string, components []manifest.Component, lang string, onProgress ProgressFunc) ([]DownloadedComponent, error) {
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		if lang == "en" {
			return nil, fmt.Errorf("creating temp dir: %w", err)
		}
		return nil, fmt.Errorf("створення тимчасової теки: %w", err)
	}

	// Сумарний розмір усіх файлів — знаменник спільного прогресу.
	var totalBytes int64
	for _, c := range components {
		totalBytes += c.Size
	}

	// Синхронізація паралельних качок.
	var mu sync.Mutex
	results := make([]DownloadedComponent, 0, len(components))
	var firstErr error
	var doneBytes int64

	// Додаємо байти до спільного лічильника і звітуємо про агрегований
	// прогрес (м'яко: максимум ~30 подій/сек, щоб не засипати фронтенд).
	var lastReport time.Time
	reportLocked := func(force bool) {
		if onProgress == nil {
			return
		}
		if !force && time.Since(lastReport) < 33*time.Millisecond {
			return
		}
		lastReport = time.Now()
		onProgress(Progress{
			TotalBytesDone:  doneBytes,
			TotalBytesTotal: totalBytes,
		})
	}

	client := &http.Client{
		Timeout: downloadTimeout,
		// Редиректи лише на https і без витоку X-Shaurma-Token на чужий хост.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "https" {
				return fmt.Errorf("редирект на небезпечну схему %q відхилено", req.URL.Scheme)
			}
			if len(via) >= 5 {
				return fmt.Errorf("забагато редиректів")
			}
			if req.URL.Host != via[0].URL.Host {
				req.Header.Del("X-Shaurma-Token")
			}
			return nil
		},
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, maxConcurrent)

	for i, c := range components {
		select {
		case <-ctx.Done():
			// Скасування до старту черги — виходимо одразу.
			mu.Lock()
			firstErr = ctx.Err()
			mu.Unlock()
			wg.Wait()
			return results, ctx.Err()
		default:
		}

		sem <- struct{}{} // чекаємо слота в пулі
		wg.Add(1)
		go func(i int, c manifest.Component) {
			defer wg.Done()
			defer func() { <-sem }()

			tempPath := filepath.Join(tmpDir, filepath.Base(c.Path))

			var lastErr error
			ok := false
			var fileDone int64 // байти цього файлу, вже додані до спільного лічильника
			for attempt := 1; attempt <= maxRetries; attempt++ {
				// Кожна спроба качає файл НАНОВО з нуля — спільний лічильник
				// уже містить байти попередньої невдалої спроби, тож спершу
				// віднімаємо їх (інакше байти задвоювались би при ретраї).
				if fileDone > 0 {
					mu.Lock()
					doneBytes -= fileDone
					fileDone = 0
					reportLocked(false)
					mu.Unlock()
				}
				if err := downloadOne(ctx, client, baseURL, token, c, tempPath, i, len(components), func(p Progress) {
					// Колбек дає КУМУЛЯТИВНІ байти поточного файлу — додаємо
					// лише різницю, щоб спільний лічильник не роздувався.
					delta := p.BytesDone - fileDone
					if delta <= 0 {
						return
					}
					mu.Lock()
					fileDone += delta
					doneBytes += delta
					reportLocked(false)
					mu.Unlock()
				}); err != nil {
					lastErr = err
					// Таймаут/обрив з'єднання — не витрачаємо час на повторні
					// спроби одного і того ж завислого файлу: якщо з'єднання
					// не відповідає 10 хвилин, спроби не допоможуть. Виходимо
					// одразу з чіткою помилкою (splash покаже картку помилки
					// замість вічного прогресу 90%).
					if errors.Is(err, context.DeadlineExceeded) ||
						strings.Contains(err.Error(), "Timeout") || strings.Contains(err.Error(), "timed out") {
						mu.Lock()
						if firstErr == nil {
							firstErr = fmt.Errorf("%s: %w", c.Path, err)
						}
						mu.Unlock()
						return
					}
					select {
					case <-ctx.Done():
						mu.Lock()
						if firstErr == nil {
							firstErr = ctx.Err()
						}
						mu.Unlock()
						return
					case <-time.After(retryBaseWait * time.Duration(attempt)):
					}
					continue
				}
				ok = true
				break
			}
			if !ok {
				if firstErr == nil {
					firstErr = fmt.Errorf("%s: %w", c.Path, lastErr)
				}
				return
			}

			mu.Lock()
			results = append(results, DownloadedComponent{Component: c, TempPath: tempPath})
			reportLocked(true)
			mu.Unlock()
		}(i, c)
	}
	wg.Wait()

	// Фінальний звіт з точними цифрами (force) — фронтенд бачить 100%.
	mu.Lock()
	reportLocked(true)
	mu.Unlock()

	if firstErr != nil {
		return results, firstErr
	}
	return results, nil
}

// downloadOne качає один файл. Колбек onProgress викликається з байтами
// САМЕ цього файлу (FileIndex/FileTotal для «файл N із M»). Назва файлу в
// прогреси не передається на фронтенд — це внутрішня деталь.
func downloadOne(ctx context.Context, client *http.Client, baseURL, token string, c manifest.Component, tempPath string, index, total int, onProgress ProgressFunc) error {
	url := baseURL + c.URL
	// БЕЗПЕКА: токен CDN і файли лаунчера ходять ЛИШЕ через HTTPS.
	if u, perr := neturl.Parse(url); perr != nil || u.Scheme != "https" {
		return fmt.Errorf("відхилено небезпечну адресу завантаження (потрібен https)")
	}
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
		return neterr.FriendlyPlain(err)
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
	fail := func(e error) error {
		out.Close()
		os.Remove(partPath)
		return e
	}

	var bytesDone int64
	total64 := c.Size
	if resp.ContentLength > 0 {
		total64 = resp.ContentLength
	}

	// БЕЗПЕКА: хеш рахується НА ЛЬОТУ по тих самих байтах, що пишуться на
	// диск (без повторного читання — немає вікна підміни між «скачали» і
	// «перевірили»), а розмір жорстко обмежено заявленим у маніфесті:
	// «нескінченна» відповідь не заб'є диск.
	h := sha256.New()
	maxBytes := int64(-1)
	if c.Size > 0 {
		maxBytes = c.Size
	}

	buf := make([]byte, 256*1024)
	for {
		select {
		case <-ctx.Done():
			return fail(ctx.Err())
		default:
		}

		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if maxBytes >= 0 && bytesDone+int64(n) > maxBytes {
				return fail(fmt.Errorf("%s: %w", c.Path, ErrIntegrity))
			}
			if _, werr := out.Write(buf[:n]); werr != nil {
				return fail(werr)
			}
			h.Write(buf[:n])
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
			return fail(readErr)
		}
	}
	// Sync + Close з перевіркою помилок: на повному диску/збої запису
	// тихо «успішний» файл — це майбутній битий лаунчер.
	if err := out.Sync(); err != nil {
		return fail(err)
	}
	if err := out.Close(); err != nil {
		os.Remove(partPath)
		return err
	}

	// ЖОРСТКА перевірка цілісності (раніше була «м'якою» — лише лог).
	// Файл, що не збігся з маніфестом, НІКОЛИ не потрапляє до встановлення.
	if err := verifyIntegrity(c, bytesDone, hex.EncodeToString(h.Sum(nil))); err != nil {
		logSHAWarning(err.Error())
		os.Remove(partPath)
		return err
	}

	// Rename .part → tempPath з ретраями: антивірус сканує свіжий файл і
	// тримає хендл без FILE_SHARE_DELETE — типова причина «file still
	// locked» на першому встановленні.
	if err := renameWithRetry(ctx, partPath, tempPath); err != nil {
		os.Remove(partPath)
		return err
	}
	return nil
}

// ErrIntegrity — файл не пройшов перевірку розміру/SHA-256 з маніфесту.
var ErrIntegrity = errors.New("перевірка цілісності не пройдена (файл підмінено або пошкоджено)")

// verifyIntegrity звіряє фактичні розмір і SHA-256 з очікуваними в
// маніфесті. Маніфест без sha256 не приймається: необов'язкова перевірка —
// це відсутня перевірка.
func verifyIntegrity(c manifest.Component, gotSize int64, gotHash string) error {
	if c.SHA256 == "" {
		return fmt.Errorf("%s: у маніфесті немає sha256: %w", c.Path, ErrIntegrity)
	}
	if c.Size > 0 && gotSize != c.Size {
		return fmt.Errorf("%s: розмір %d замість %d: %w", c.Path, gotSize, c.Size, ErrIntegrity)
	}
	if !strings.EqualFold(gotHash, c.SHA256) {
		return fmt.Errorf("%s: sha256 %s замість %s: %w", c.Path, gotHash, c.SHA256, ErrIntegrity)
	}
	return nil
}

// VerifyFile — повторна перевірка файлу безпосередньо перед
// встановленням (захист від підміни в тимчасовій теці між завантаженням
// і застосуванням).
func VerifyFile(c manifest.Component, path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	got, err := fileSHA256(path)
	if err != nil {
		return err
	}
	return verifyIntegrity(c, fi.Size(), got)
}

// renameWithRetry — os.Rename з очікуванням, поки файл звільниться.
func renameWithRetry(ctx context.Context, src, dst string) error {
	var last error
	for i := 0; i < 20; i++ {
		_ = os.Remove(dst)
		if last = os.Rename(src, dst); last == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return last
}

// shaWarningsLogPath — шлях до логу м'яких sha-попереджень. Поряд з exe
// (installDir), щоб було видно розробнику/підтримці, але всередині
// системної теки TEMP, якщо запис біля exe неможливий (обмежені права).
func shaWarningsLogPath() string {
	if exe, err := os.Executable(); err == nil {
		if dir := filepath.Dir(exe); dir != "" {
			if f, err := os.OpenFile(filepath.Join(dir, "update-sha-warnings.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
				f.Close()
				return filepath.Join(dir, "update-sha-warnings.log")
			}
		}
	}
	return filepath.Join(os.TempDir(), "shrm-update-sha-warnings.log")
}

// logSHAWarning дописує рядок у лог м'яких sha-попереджень (створюється
// при потребі). Невдача логування ігнорується — діагностика не має ламати
// оновлення. Захист від безмежного росту: якщо лог перевищує 512 КБ —
// обрізаємо до 256 КБ (лишаємо кінець — найсвіжіші попередження).
func logSHAWarning(msg string) {
	line := time.Now().Format("2006-01-02 15:04:05") + " " + msg + "\n"
	path := shaWarningsLogPath()
	if fi, err := os.Stat(path); err == nil && fi.Size() > 512*1024 {
		if data, rerr := os.ReadFile(path); rerr == nil {
			keep := len(data) - 256*1024
			if keep < 0 {
				keep = 0
			}
			// Відрізаємо до межі на межі рядка, щоб не лишити обрізаний рядок.
			start := keep
			if keep > 0 {
				if nl := strings.IndexByte(string(data[keep:]), '\n'); nl >= 0 {
					start = keep + nl + 1
				}
			}
			_ = os.WriteFile(path, data[start:], 0o644)
		}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line)
}

// fileSHA256 — sha256 (hex) вмісту файлу для м'якої перевірки.
func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
