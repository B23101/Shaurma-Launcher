package main

// ── Галерея скріншотів збірки (розділ «Галерея» на сторінці збірки) ─────
// На відміну від «Файлів» (лише кастомні збірки), Галерея працює для
// БУДЬ-ЯКОЇ встановленої збірки — і Шаурма, і кастомної: скріншоти Minecraft
// лежать у <InstanceDir>/<packID>/.minecraft/screenshots незалежно від
// типу збірки, тому тека резолвиться напряму через packMinecraftDir
// (custompack_actions.go), а не через a.customPacks.
//
// Функціонал:
//   - список скріншотів (ім'я, розмір, дата, підпис/опис із gallery.Store);
//   - швидкий сигнал змін (ScreenshotsSignature) для polling з фронтенду —
//     той самий підхід, що PackServersSection (5с тик, порівняння підпису);
//   - перейменування/видалення/показати у Провіднику/відкрити у типовій
//     програмі перегляду Windows;
//   - підпис/опис (окремо від імені файлу);
//   - збереження відредагованого зображення (з фоторедактора, PNG data URL)
//     як перезапис оригінального файлу;
//   - копіювання зображення (оригінал або відредаговане) у буфер обміну
//     Windows через clipboard_windows.go (CF_DIB).
//
// Розділ на фронтенді взагалі не рендериться, якщо список порожній —
// адже без жодного скріншоту показувати порожню галерею немає смислу.

import (
	"shaurma-launcher-backend/internal/crashguard"
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // реєструє декодер webp у image.Decode (як imageconvert.go)
	"shaurma-launcher-backend/internal/gallery"
)

// ── Живе стеження за новими скріншотами (віджет у лаунчері) ─────────────
// Поки хоч одна збірка запущена (a.sessionsM.Snapshot()), фоновий цикл
// раз на screenshotWatchInterval звіряє вміст усіх теки screenshots/ цих
// збірок зі своїм останнім знімком. Новий файл → подія "gallery:new-
// screenshot" (App.svelte підписується і показує плаваючий віджет знизу
// справа — NewScreenshotWidget.svelte). Той самий підхід, що
// startQuitFlagWatcher (quitflag.go): звичайний polling-тикер, без
// нативного OS file-watch API (fsnotify тут не використовується жодним
// пакетом лаунчера).
//
// Після зупинки гри стеження за ЦІЄЮ збіркою триває ще
// screenshotWatchGrace — F2 у Minecraft пише файл на диск не миттєво з
// моментом закриття процесу гри, тож без «хвоста» останній скріншот
// сесії міг би пропасти повз віджет.
const (
	screenshotWatchInterval = 1500 * time.Millisecond
	screenshotWatchGrace    = 20 * time.Second
)

// NewScreenshotEvent — payload події "gallery:new-screenshot".
type NewScreenshotEvent struct {
	PackID   string          `json:"packId"`
	PackName string          `json:"packName"`
	Entry    ScreenshotEntry `json:"entry"`
}

// startScreenshotWatcher запускає фоновий цикл стеження. Викликається
// одноразово з ServiceStartup.
func (a *App) startScreenshotWatcher() {
	seen := map[string]map[string]int64{} // packID → fileName → ModifiedTs, останній знімок
	stoppedAt := map[string]time.Time{}   // packID → момент, коли збірка перестала бути в Snapshot()
	go func() {
		defer crashguard.Recover("screenshots.go:78")
		ticker := time.NewTicker(screenshotWatchInterval)
		defer ticker.Stop()
		for range ticker.C {
			a.screenshotWatchTick(seen, stoppedAt)
		}
	}()
}

func (a *App) screenshotWatchTick(seen map[string]map[string]int64, stoppedAt map[string]time.Time) {
	if a.sessionsM == nil {
		return
	}
	running := map[string]string{} // packID → назва збірки (для віджету)
	for _, s := range a.sessionsM.Snapshot() {
		running[s.BuildID] = s.BuildName
	}
	now := time.Now()
	// Збірки, що щойно зупинились, — лишаємо у watch-множині ще
	// screenshotWatchGrace, потім прибираємо зі спостереження зовсім
	// (і забуваємо їх знімок, щоб не тримати пам'ять про давно закриті
	// збірки).
	for packID := range seen {
		if _, ok := running[packID]; ok {
			delete(stoppedAt, packID)
			continue
		}
		if t, ok := stoppedAt[packID]; ok {
			if now.Sub(t) > screenshotWatchGrace {
				delete(seen, packID)
				delete(stoppedAt, packID)
			}
			continue
		}
		stoppedAt[packID] = now
	}
	for packID, packName := range running {
		isBaseline := false
		if _, ok := seen[packID]; !ok {
			seen[packID] = map[string]int64{}
			isBaseline = true // перший тик для цієї збірки — лише знімок, без подій
		}
		a.diffScreenshotsOnce(packID, packName, seen[packID], isBaseline)
	}
}

// diffScreenshotsOnce перечитує теку screenshots/ однієї збірки й емітить
// подію на кожен файл, якого не було у прошлому знімку. prev мутується
// на місці (наступний тик порівнюється з оновленим знімком).
func (a *App) diffScreenshotsOnce(packID, packName string, prev map[string]int64, isBaseline bool) {
	dir, err := a.screenshotsDir(packID)
	if err != nil {
		return
	}
	files, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cur := make(map[string]int64, len(files))
	for _, f := range files {
		if f.IsDir() || !screenshotExts[strings.ToLower(filepath.Ext(f.Name()))] {
			continue
		}
		info, err := f.Info()
		if err != nil {
			continue
		}
		ts := info.ModTime().UnixMilli()
		cur[f.Name()] = ts
		if prevTs, existed := prev[f.Name()]; existed && prevTs == ts {
			continue
		}
		if _, existed := prev[f.Name()]; existed {
			// Файл існував і не змінився за mtime — вже опрацьований.
			continue
		}
		// Новий файл, якого не було у прошлому знімку. isBaseline=true
		// для ПЕРШОГО тику спостереження за цією збіркою (щойно запущена
		// гра застає вже наявну купу старих скріншотів) — про такі файли
		// подія не шлеться, лише про справді нові, що з'явились ПІСЛЯ
		// того, як лаунчер почав дивитись на цю теку.
		if !isBaseline {
			w, h, _ := imageDimensions(filepath.Join(dir, f.Name()))
			var meta gallery.Meta
			if a.gallery != nil {
				meta = a.gallery.Get(packID, f.Name())
			}
			a.emit("gallery:new-screenshot", NewScreenshotEvent{
				PackID:   packID,
				PackName: packName,
				Entry: ScreenshotEntry{
					FileName:    f.Name(),
					Size:        info.Size(),
					Modified:    info.ModTime().Format(time.RFC3339),
					ModifiedTs:  ts,
					Title:       meta.Title,
					Description: meta.Description,
					Width:       w,
					Height:      h,
				},
			})
		}
	}
	// Заміщуємо вміст prev на cur "на місці" (той самий map, referenced з
	// seen[packID] у screenshotWatchTick).
	for k := range prev {
		if _, ok := cur[k]; !ok {
			delete(prev, k)
		}
	}
	for k, v := range cur {
		prev[k] = v
	}
}

// screenshotExts — розширення файлів, які Minecraft/OptiFine/шейдер-мод
// зберігають у screenshots/ (сам ванільний Minecraft пише лише .png, але
// деякі моди скріншотів/replay допускають інші формати).
var screenshotExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true,
}

// ScreenshotEntry — один елемент галереї.
type ScreenshotEntry struct {
	FileName    string `json:"fileName"`   // ім'я файлу на диску (з розширенням)
	Size        int64  `json:"size"`       // байти
	Modified    string `json:"modified"`   // RFC3339 — і дата створення скріну, і сортування
	ModifiedTs  int64  `json:"modifiedTs"` // unix-мс, зручно для фронтенд-сортування/групування за датою
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Width       int    `json:"width,omitempty"`
	Height      int    `json:"height,omitempty"`
}

// screenshotsDir повертає абсолютний шлях до теки screenshots/ збірки
// (не гарантує існування — Minecraft створює її сам лише після F2).
func (a *App) screenshotsDir(packID string) (string, error) {
	if packID == "" {
		return "", fmt.Errorf("не вказано packID")
	}
	return filepath.Join(a.packMinecraftDir(packID), "screenshots"), nil
}

// resolveScreenshotPath безпечно резолвить ім'я файлу скріншоту в
// абсолютний шлях строго всередині теки screenshots/ — той самий захист,
// що resolvePackPath (packfiles.go), але тут завжди рівно один рівень
// (без підтек), бо Minecraft ніколи не кладе скріншоти у підтеки.
func resolveScreenshotPath(dir, fileName string) (string, error) {
	if fileName == "" || strings.ContainsAny(fileName, `/\`) {
		return "", fmt.Errorf("недопустиме ім'я файлу: %s", fileName)
	}
	clean := filepath.Clean(fileName)
	if clean != fileName || clean == "." || clean == ".." {
		return "", fmt.Errorf("недопустиме ім'я файлу: %s", fileName)
	}
	return filepath.Join(dir, clean), nil
}

// ListScreenshots повертає всі скріншоти збірки, найновіші перші.
// Порожній слайс (не помилка), якщо теки screenshots/ ще не існує —
// фронтенд трактує це як «розділу немає».
func (a *App) ListScreenshots(packID string) ([]ScreenshotEntry, error) {
	dir, err := a.screenshotsDir(packID)
	if err != nil {
		return nil, err
	}
	files, err := os.ReadDir(dir)
	if err != nil {
		// Теки нема (збірка ще не запускалась/скрінів не робили) — це не
		// помилка, просто порожня галерея.
		return []ScreenshotEntry{}, nil
	}
	meta := a.gallery.GetAll(packID)
	out := make([]ScreenshotEntry, 0, len(files))
	for _, f := range files {
		if f.IsDir() {
			continue
		}
		if !screenshotExts[strings.ToLower(filepath.Ext(f.Name()))] {
			continue
		}
		info, err := f.Info()
		if err != nil {
			continue
		}
		m := meta[f.Name()]
		w, h := 0, 0
		if cfgW, cfgH, ok := imageDimensions(filepath.Join(dir, f.Name())); ok {
			w, h = cfgW, cfgH
		}
		out = append(out, ScreenshotEntry{
			FileName:    f.Name(),
			Size:        info.Size(),
			Modified:    info.ModTime().Format(time.RFC3339),
			ModifiedTs:  info.ModTime().UnixMilli(),
			Title:       m.Title,
			Description: m.Description,
			Width:       w,
			Height:      h,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModifiedTs > out[j].ModifiedTs })
	return out, nil
}

// imageDimensions читає лише заголовок зображення (image.DecodeConfig —
// без декодування пікселів) для розмірів у сітці галереї.
func imageDimensions(path string) (w, h int, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return 0, 0, false
	}
	return cfg.Width, cfg.Height, true
}

// ScreenshotsSignature — легкий «підпис» стану теки screenshots/ для
// частого polling з фронтенду (галерея відкрита → раз на кілька секунд),
// без повного перечитування списку й метаданих: кількість файлів +
// сумарний mtime (unix-секунди останнього змінення теки — досить, бо
// будь-яке створення/видалення/перейменування файлу оновлює mtime теки
// на більшості файлових систем Windows/NTFS).
func (a *App) ScreenshotsSignature(packID string) (string, error) {
	dir, err := a.screenshotsDir(packID)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(dir)
	if err != nil {
		return "0:0", nil
	}
	files, err := os.ReadDir(dir)
	if err != nil {
		return "0:0", nil
	}
	n := 0
	var latest int64
	for _, f := range files {
		if f.IsDir() || !screenshotExts[strings.ToLower(filepath.Ext(f.Name()))] {
			continue
		}
		n++
		if fi, err := f.Info(); err == nil {
			if ts := fi.ModTime().UnixMilli(); ts > latest {
				latest = ts
			}
		}
	}
	_ = info
	return strconv.Itoa(n) + ":" + strconv.FormatInt(latest, 10), nil
}

// GetScreenshotAsset повертає скріншот як data URL (для <img src> —
// WebView2 не читає file:// з довільних тек).
func (a *App) GetScreenshotAsset(packID, fileName string) (string, error) {
	dir, err := a.screenshotsDir(packID)
	if err != nil {
		return "", err
	}
	abs, err := resolveScreenshotPath(dir, fileName)
	if err != nil {
		return "", err
	}
	if st, err := os.Stat(abs); err != nil || st.IsDir() {
		return "", fmt.Errorf("скріншот не знайдено: %s", fileName)
	}
	return localFileDataURL(abs)
}

// thumbnailMaxDim — довша сторона мініатюри в сітці галереї. Скріншоти
// Minecraft часто 1920×1080+ і важать по кілька МБ — віддавати такий
// файл повністю на КОЖНУ картку сітки (могло бути й 100+ одночасно)
// перевантажує і диск/CPU (декодування PNG), і WebView2 (base64-рядки
// такого розміру у DOM) — саме це раніше й підвішувало лаунчер при
// відкритті галереї з великою кількістю скріншотів.
const thumbnailMaxDim = 320

// thumbnailCacheDir — тека кешу мініатюр усередині screenshots/ збірки
// (прихована крапкою — Minecraft і ListScreenshots її ігнорують: вона не
// потрапляє у список, бо не має розширення .png/.jpg на верхньому рівні
// імені файлу теки, а ListScreenshots читає лише файли, не теки).
const thumbnailCacheDirName = ".shrm-thumbs"

// GetScreenshotThumbnail повертає ЗМЕНШЕНУ копію скріншоту (JPEG,
// довша сторона thumbnailMaxDim) як data URL — для сітки галереї.
// Повний оригінал (GetScreenshotAsset) використовується лише коли
// картинку показують на весь екран (фоторедактор).
//
// Результат кешується на диск поруч зі скріншотами (.shrm-thumbs/) за
// іменем файлу + його mtime у назві — якщо оригінал перезаписали
// (наприклад, після редагування), кеш-файл із застарілим mtime просто
// перестає збігатись і мініатюра перегенерується автоматично.
func (a *App) GetScreenshotThumbnail(packID, fileName string) (string, error) {
	dir, err := a.screenshotsDir(packID)
	if err != nil {
		return "", err
	}
	abs, err := resolveScreenshotPath(dir, fileName)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(abs)
	if err != nil || st.IsDir() {
		return "", fmt.Errorf("скріншот не знайдено: %s", fileName)
	}

	cacheDir := filepath.Join(dir, thumbnailCacheDirName)
	cacheName := fmt.Sprintf("%s.%d.jpg", fileName, st.ModTime().UnixNano())
	cachePath := filepath.Join(cacheDir, cacheName)

	if data, err := os.ReadFile(cachePath); err == nil {
		return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(data), nil
	}

	f, err := os.Open(abs)
	if err != nil {
		return "", fmt.Errorf("не вдалось відкрити скріншот: %w", err)
	}
	img, _, err := image.Decode(f)
	f.Close()
	if err != nil {
		return "", fmt.Errorf("не вдалось декодувати зображення: %w", err)
	}

	thumb := resizeToFit(img, thumbnailMaxDim)
	var buf bytes.Buffer
	if err := jpegEncode(&buf, thumb, 82); err != nil {
		return "", fmt.Errorf("не вдалось закодувати мініатюру: %w", err)
	}
	data := buf.Bytes()

	// Кеш — best-effort: якщо запис не вдався (немає прав, диск повний
	// тощо) — все одно повертаємо згенеровану мініатюру, просто без кешу.
	if err := os.MkdirAll(cacheDir, 0755); err == nil {
		_ = os.WriteFile(cachePath, data, 0644)
	}

	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(data), nil
}

// jpegEncode кодує зображення у JPEG із заданою якістю (0-100).
func jpegEncode(w *bytes.Buffer, img image.Image, quality int) error {
	return jpeg.Encode(w, img, &jpeg.Options{Quality: quality})
}

// resizeToFit масштабує зображення так, щоб довша сторона не перевищувала
// maxDim (пропорції збережено). Якщо оригінал уже менший — повертається
// без змін (мініатюра не має смислу для вже маленької картинки).
func resizeToFit(img image.Image, maxDim int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= maxDim && h <= maxDim {
		return img
	}
	var newW, newH int
	if w >= h {
		newW = maxDim
		newH = int(float64(h) * float64(maxDim) / float64(w))
	} else {
		newH = maxDim
		newW = int(float64(w) * float64(maxDim) / float64(h))
	}
	if newW < 1 {
		newW = 1
	}
	if newH < 1 {
		newH = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	// CatmullRom — якісний інтерполятор (плавніше за nearest-neighbor,
	// швидший за повноцінний Lanczos) — достатньо для маленької мініатюри.
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Over, nil)
	return dst
}

// RenameScreenshot перейменовує файл скріншоту (перевірка імені за
// стандартами Windows + анти-дублікат) і переносить підпис/опис на нове
// ім'я у gallery.Store.
// removeThumbnailCache видаляє всі кешовані мініатюри конкретного файлу
// скріншоту (можуть лишитись кілька — з різними mtime у назві, якщо файл
// кілька разів перезаписували) — викликається при перейменуванні,
// видаленні й перезаписі оригіналу, щоб .shrm-thumbs/ не накопичував
// сироти назавжди.
func removeThumbnailCache(dir, fileName string) {
	cacheDir := filepath.Join(dir, thumbnailCacheDirName)
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return
	}
	prefix := fileName + "."
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), prefix) {
			os.Remove(filepath.Join(cacheDir, e.Name()))
		}
	}
}

func (a *App) RenameScreenshot(packID, fileName, newName string) error {
	dir, err := a.screenshotsDir(packID)
	if err != nil {
		return err
	}
	oldAbs, err := resolveScreenshotPath(dir, fileName)
	if err != nil {
		return err
	}
	if _, err := os.Stat(oldAbs); err != nil {
		return fmt.Errorf("скріншот не знайдено: %s", fileName)
	}
	ext := filepath.Ext(fileName)
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return fmt.Errorf("порожня назва")
	}
	// Якщо користувач не вказав розширення — лишаємо оригінальне
	// (перейменування скріншоту стосується імені, не формату файлу).
	if filepath.Ext(newName) == "" {
		newName += ext
	}
	validated, err := validateFileName(newName)
	if err != nil {
		return err
	}
	if !strings.EqualFold(validated, fileName) && nameExistsIn(dir, validated) {
		return fmt.Errorf("файл з такою назвою вже існує")
	}
	newAbs, err := resolveScreenshotPath(dir, validated)
	if err != nil {
		return err
	}
	if err := os.Rename(oldAbs, newAbs); err != nil {
		return fmt.Errorf("не вдалось перейменувати: %w", err)
	}
	removeThumbnailCache(dir, fileName)
	if a.gallery != nil {
		_ = a.gallery.Rename(packID, fileName, validated)
	}
	return nil
}

// DeleteScreenshot видаляє файл скріншоту разом з його метаданими.
func (a *App) DeleteScreenshot(packID, fileName string) error {
	dir, err := a.screenshotsDir(packID)
	if err != nil {
		return err
	}
	abs, err := resolveScreenshotPath(dir, fileName)
	if err != nil {
		return err
	}
	if err := os.Remove(abs); err != nil {
		return fmt.Errorf("не вдалось видалити скріншот: %w", err)
	}
	removeThumbnailCache(dir, fileName)
	if a.gallery != nil {
		_ = a.gallery.Delete(packID, fileName)
	}
	return nil
}

// SetScreenshotMeta зберігає підпис/опис скріншоту (окремо від імені
// файлу на диску — «Перейменувати» лишається для самого файлу).
func (a *App) SetScreenshotMeta(packID, fileName, title, description string) error {
	if a.gallery == nil {
		return fmt.Errorf("gallery store не інціалізовано")
	}
	return a.gallery.Set(packID, fileName, gallery.Meta{Title: strings.TrimSpace(title), Description: strings.TrimSpace(description)})
}

// RevealScreenshot відкриває Провідник із виділеним конкретним скріншотом.
func (a *App) RevealScreenshot(packID, fileName string) error {
	dir, err := a.screenshotsDir(packID)
	if err != nil {
		return err
	}
	abs, err := resolveScreenshotPath(dir, fileName)
	if err != nil {
		return err
	}
	if _, err := os.Stat(abs); err != nil {
		return fmt.Errorf("скріншот не знайдено: %s", fileName)
	}
	if runtime.GOOS == "windows" {
		return exec.Command("explorer.exe", "/select,"+abs).Start()
	}
	return xdgOpen(filepath.Dir(abs))
}

// OpenScreenshotInDefaultApp відкриває скріншот у типовій програмі
// перегляду зображень Windows (той самий застосунок, що подвійний клік
// у Провіднику) — на відміну від RevealScreenshot (лише виділити у теці).
func (a *App) OpenScreenshotInDefaultApp(packID, fileName string) error {
	dir, err := a.screenshotsDir(packID)
	if err != nil {
		return err
	}
	abs, err := resolveScreenshotPath(dir, fileName)
	if err != nil {
		return err
	}
	if _, err := os.Stat(abs); err != nil {
		return fmt.Errorf("скріншот не знайдено: %s", fileName)
	}
	if runtime.GOOS == "windows" {
		return openFileDefault(abs)
	}
	return xdgOpen(abs)
}

// CopyScreenshotPathToClipboard копіює абсолютний шлях файлу скріншоту
// (пункт контекстного меню «Копіювати шлях») — текстовий буфер обміну,
// той самий CopyToClipboard, що інші кнопки копіювання лаунчера.
func (a *App) CopyScreenshotPathToClipboard(packID, fileName string) error {
	dir, err := a.screenshotsDir(packID)
	if err != nil {
		return err
	}
	abs, err := resolveScreenshotPath(dir, fileName)
	if err != nil {
		return err
	}
	return setClipboardText(abs)
}

// CopyScreenshotToClipboard копіює саме ЗОБРАЖЕННЯ скріншоту (як є, без
// редагування) у системний буфер обміну Windows (Ctrl+V у Paint/Word/
// Discord тощо вставить картинку) — пункт контекстного меню «Копіювати».
func (a *App) CopyScreenshotToClipboard(packID, fileName string) error {
	dir, err := a.screenshotsDir(packID)
	if err != nil {
		return err
	}
	abs, err := resolveScreenshotPath(dir, fileName)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return fmt.Errorf("не вдалось прочитати скріншот: %w", err)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("не вдалось декодувати зображення: %w", err)
	}
	return setClipboardImage(img)
}

// CopyEditedImageToClipboard копіює зображення з фоторедактора (data:
// URL з canvas.toDataURL) у буфер обміну Windows — «Скопіювати» після
// редагування БЕЗ збереження файлу на диск.
func (a *App) CopyEditedImageToClipboard(dataURL string) error {
	img, err := decodeDataURLImage(dataURL)
	if err != nil {
		return err
	}
	return setClipboardImage(img)
}

// SaveEditedScreenshot перезаписує оригінальний файл скріншоту
// відредагованим зображенням (data: URL з canvas.toDataURL, завжди PNG
// з фронтенду) — кнопка «Зберегти» у фоторедакторі. Файл перезаписується
// атомарно (tmp + rename), щоб невдалий запис не лишив пошкоджений
// скріншот.
func (a *App) SaveEditedScreenshot(packID, fileName, dataURL string) error {
	dir, err := a.screenshotsDir(packID)
	if err != nil {
		return err
	}
	abs, err := resolveScreenshotPath(dir, fileName)
	if err != nil {
		return err
	}
	if _, err := os.Stat(abs); err != nil {
		return fmt.Errorf("скріншот не знайдено: %s", fileName)
	}
	img, err := decodeDataURLImage(dataURL)
	if err != nil {
		return err
	}
	tmp := abs + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("не вдалось створити тимчасовий файл: %w", err)
	}
	if err := png.Encode(out, img); err != nil {
		out.Close()
		os.Remove(tmp)
		return fmt.Errorf("не вдалось закодувати зображення: %w", err)
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, abs); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("не вдалось перезаписати скріншот: %w", err)
	}
	removeThumbnailCache(dir, fileName)
	return nil
}

// SaveEditedScreenshotAsCopy відкриває системний діалог «Зберегти як» і
// пише відредаговане зображення туди, куди вкаже користувач — а не мовчки
// поряд з оригіналом. Раніше ця функція просто клала файл
// "<ім'я>-edited.png" в ту саму теку зі скріншотами без жодного вибору з
// боку користувача, хоча кнопка називалась «Зберегти як…». Повертає
// порожній рядок (без помилки), якщо користувач закрив діалог без вибору
// файлу — фронтенд у цьому разі просто нічого не робить, без тосту про
// помилку.
func (a *App) SaveEditedScreenshotAsCopy(packID, fileName, dataURL string) (string, error) {
	dir, err := a.screenshotsDir(packID)
	if err != nil {
		return "", err
	}
	base := strings.TrimSuffix(fileName, filepath.Ext(fileName))
	defaultName := base + "-edited.png"
	for i := 2; nameExistsIn(dir, defaultName); i++ {
		defaultName = fmt.Sprintf("%s-edited-%d.png", base, i)
		if i > 999 {
			break
		}
	}

	// Generic bridge-діалог збереження (виконує shell.exe). defaultName
	// підставляється у поле «Ім'я файлу», фільтр PNG — тип файлу.
	target := bridgeDialogSaveFile("Зберегти копію скріншоту", defaultName, map[string][]string{
		"PNG image": {"png"},
	})
	if target == "" {
		// Користувач закрив діалог без вибору файлу — це не помилка.
		return "", nil
	}
	// Про всяк випадок, якщо повернувся шлях, що вказує на директорію
	// (без розширення і без файлу з таким іменем), підставляємо наш
	// заздалегідь підібраний унікальний defaultName.
	if info, statErr := os.Stat(target); statErr == nil && info.IsDir() {
		target = filepath.Join(target, defaultName)
	}
	if filepath.Ext(target) == "" {
		target += ".png"
	}

	img, err := decodeDataURLImage(dataURL)
	if err != nil {
		return "", err
	}
	out, err := os.Create(target)
	if err != nil {
		return "", fmt.Errorf("не вдалось створити файл: %w", err)
	}
	defer out.Close()
	if err := png.Encode(out, img); err != nil {
		os.Remove(target)
		return "", fmt.Errorf("не вдалось закодувати зображення: %w", err)
	}

	// Якщо користувач зберіг копію у ТІЙ САМІЙ теці зі скріншотами —
	// оновлюємо кеш мініатюр, щоб нова копія одразу з'явилась у галереї
	// без очікування наступного polling-тіка.
	if filepath.Dir(target) == dir {
		removeThumbnailCache(dir, filepath.Base(target))
	}
	return filepath.Base(target), nil
}

// decodeDataURLImage декодує "data:image/...;base64,XXXX" у image.Image.
func decodeDataURLImage(dataURL string) (image.Image, error) {
	idx := strings.Index(dataURL, ",")
	if !strings.HasPrefix(dataURL, "data:") || idx < 0 {
		return nil, fmt.Errorf("недопустимий формат data URL")
	}
	raw, err := base64.StdEncoding.DecodeString(dataURL[idx+1:])
	if err != nil {
		return nil, fmt.Errorf("не вдалось декодувати base64: %w", err)
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("не вдалось декодувати зображення: %w", err)
	}
	return img, nil
}
