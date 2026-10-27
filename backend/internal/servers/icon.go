// icon.go — обробка іконок сервера (resize до 64×64, пресети).
package servers

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "golang.org/x/image/webp" // підтримка webp

	"shaurma-launcher-backend/internal/imageconvert"
)

// SetServerIcon записує іконку сервера (server-icon.png 64×64).
// Приймає base64 data URL (data:image/png;base64,...) або чистий base64.
func SetServerIcon(serverDir, imageDataBase64 string) (string, error) {
	// Декодуємо base64.
	raw, mimeType, err := decodeBase64Image(imageDataBase64)
	if err != nil {
		return "", fmt.Errorf("декодування base64: %w", err)
	}

	// Декодуємо зображення.
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("декодування зображення (%s): %w", mimeType, err)
	}

	// Resize до 64×64 (NearestNeighbor — працює без x/image/draw).
	resized := image.NewRGBA(image.Rect(0, 0, 64, 64))
	srcBounds := img.Bounds()
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			srcX := srcBounds.Min.X + x*(srcBounds.Dx())/64
			srcY := srcBounds.Min.Y + y*(srcBounds.Dy())/64
			resized.Set(x, y, img.At(srcX, srcY))
		}
	}

	// Записуємо як PNG.
	iconPath := filepath.Join(serverDir, "server-icon.png")
	f, err := os.Create(iconPath)
	if err != nil {
		return "", fmt.Errorf("створення server-icon.png: %w", err)
	}
	defer f.Close()

	if err := png.Encode(f, resized); err != nil {
		return "", fmt.Errorf("кодування PNG: %w", err)
	}

	return toDataURL(resized), nil
}

// iconCache кешує data-URL іконки сервера за (шлях, mtime) — список
// серверів (ListServers → toView) перечитується раз на кілька секунд,
// поки хоч один сервер запущений, і без кешу кожне оновлення читало б
// PNG з диска + робило png.Decode + base64 для кожного сервера.
type iconCacheEntry struct {
	mtime time.Time
	data  string
}

var iconCache sync.Map // serverDir → iconCacheEntry

// GetServerIcon читає server-icon.png і повертає data URL (з кешем за mtime).
func GetServerIcon(serverDir string) (string, error) {
	iconPath := filepath.Join(serverDir, "server-icon.png")
	info, err := os.Stat(iconPath)
	if err != nil {
		iconCache.Delete(serverDir)
		return "", err
	}
	if cached, ok := iconCache.Load(serverDir); ok {
		e := cached.(iconCacheEntry)
		if e.mtime.Equal(info.ModTime()) {
			return e.data, nil
		}
		iconCache.Delete(serverDir)
	}

	data, err := os.ReadFile(iconPath)
	if err != nil {
		return "", err
	}

	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("декодування server-icon.png: %w", err)
	}

	dataURL := toDataURL(img)
	iconCache.Store(serverDir, iconCacheEntry{mtime: info.ModTime(), data: dataURL})
	return dataURL, nil
}

// ResetServerIcon видаляє server-icon.png.
func ResetServerIcon(serverDir string) error {
	return os.Remove(filepath.Join(serverDir, "server-icon.png"))
}

// ── Іконка ЛАУНЧЕРА (окремо від протокольної server-icon.png) ───────────
//
// SetServerIcon/GetServerIcon вище працюють з server-icon.png — файлом,
// який читає ігровий клієнт Minecraft у списку серверів (64×64, жорсткий
// формат). Для картки/сайдбару ЛАУНЧЕРА це не годиться: 64×64 милить на
// великих екранах, а сам файл має лишатись протокольним. Тому для UI
// лаунчера — окремий файл у <serverDir>/.shaurma/launcher-icon.<ext>:
// копіюємо вихідний файл як є (як custompack.copyAssetInto — без
// примусового ресайзу), щоб іконка не втрачала якість.
const (
	shaurmaDirName   = ".shaurma"
	launcherIconBase = "launcher-icon"
)

// launcherIconExts — розширення, які приймаємо для лаунчер-іконки. Порядок
// має значення лише як fallback-детект, коли LauncherIconPath порожній.
var launcherIconExts = []string{".png", ".jpg", ".jpeg", ".webp", ".gif", ".bmp"}

// SetLauncherIcon копіює обраний користувачем файл-зображення у
// <serverDir>/.shaurma/launcher-icon.<ext> і повертає ВІДНОСНИЙ шлях
// (лише ім'я файлу) для збереження в Server.LauncherIconPath.
func SetLauncherIcon(serverDir, srcPath string) (string, error) {
	if strings.TrimSpace(srcPath) == "" {
		return "", fmt.Errorf("не вказано шлях до іконки")
	}
	ext := strings.ToLower(filepath.Ext(srcPath))
	if ext == "" {
		ext = ".png"
	}
	targetDir := filepath.Join(serverDir, shaurmaDirName)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return "", fmt.Errorf("створення .shaurma: %w", err)
	}
	// Прибираємо попередній файл з ІНШИМ розширенням, щоб не плодити
	// launcher-icon.png + launcher-icon.jpg одночасно (фіксоване ім'я, як
	// icon.* у збірках: повторне редагування заміняє, а не додає).
	for _, e := range launcherIconExts {
		if e != ext {
			_ = os.Remove(filepath.Join(targetDir, launcherIconBase+e))
		}
	}
	dst := filepath.Join(targetDir, launcherIconBase+ext)
	in, err := os.Open(srcPath)
	if err != nil {
		return "", fmt.Errorf("відкриття іконки: %w", err)
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return "", fmt.Errorf("створення launcher-icon: %w", err)
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return "", fmt.Errorf("копіювання іконки: %w", err)
	}
	// Анімований GIF з loop-count=1 «грає один раз і зупиняється» —
	// нормалізуємо на нескінченний цикл (той самий фікс, що у збірках).
	if ext == ".gif" {
		imageconvert.NormalizeGIFLoopInfinite(dst)
	}
	return launcherIconBase + ext, nil
}

// launcherIconCache кешує data URL лаунчер-іконки за (шлях, mtime, size) —
// ListServers перечитується раз на кілька секунд, поки хоч один сервер
// запущений; без кешу це було б base64-кодування файлу на кожен запит.
type launcherIconCacheEntry struct {
	mtime time.Time
	size  int64
	data  string
}

var launcherIconCache sync.Map // abs path → launcherIconCacheEntry

// GetLauncherIcon читає лаунчер-іконку сервера і повертає data URL.
// relPath — Server.LauncherIconPath; якщо порожній, намагаємось угадати
// launcher-icon.<ext> (напр. після ручного копіювання файлу).
func GetLauncherIcon(serverDir, relPath string) (string, error) {
	path := ""
	if relPath != "" {
		path = filepath.Join(serverDir, shaurmaDirName, filepath.Base(relPath))
	} else {
		for _, e := range launcherIconExts {
			cand := filepath.Join(serverDir, shaurmaDirName, launcherIconBase+e)
			if _, err := os.Stat(cand); err == nil {
				path = cand
				break
			}
		}
	}
	if path == "" {
		return "", os.ErrNotExist
	}
	info, err := os.Stat(path)
	if err != nil {
		launcherIconCache.Delete(path)
		return "", err
	}
	if cached, ok := launcherIconCache.Load(path); ok {
		e := cached.(launcherIconCacheEntry)
		if e.mtime.Equal(info.ModTime()) && e.size == info.Size() {
			return e.data, nil
		}
		launcherIconCache.Delete(path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	dataURL := "data:" + mimeForExt(filepath.Ext(path)) + ";base64," + base64.StdEncoding.EncodeToString(data)
	launcherIconCache.Store(path, launcherIconCacheEntry{mtime: info.ModTime(), size: info.Size(), data: dataURL})
	return dataURL, nil
}

// ResetLauncherIcon видаляє лаунчер-іконку (усі можливі розширення).
func ResetLauncherIcon(serverDir string) error {
	removed := false
	for _, e := range launcherIconExts {
		path := filepath.Join(serverDir, shaurmaDirName, launcherIconBase+e)
		if err := os.Remove(path); err == nil {
			removed = true
			launcherIconCache.Delete(path)
		}
	}
	if !removed {
		return os.ErrNotExist
	}
	return nil
}

// mimeForExt повертає MIME-тип для розширення файлу іконки.
func mimeForExt(ext string) string {
	switch strings.ToLower(ext) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	case ".bmp":
		return "image/bmp"
	default:
		return "image/png"
	}
}

// PresetIcons — вбудовані пресет-іконки (кольорові квадрати з символами).
// Повертає мапу ім'я → data URL.
func PresetIcons() map[string]string {
	presets := map[string]color.RGBA{
		"grass":    {R: 95, G: 159, B: 53, A: 255},
		"stone":    {R: 125, G: 125, B: 125, A: 255},
		"wood":     {R: 156, G: 127, B: 78, A: 255},
		"fire":     {R: 255, G: 85, B: 28, A: 255},
		"water":    {R: 58, G: 146, B: 255, A: 255},
		"diamond":  {R: 0, G: 214, B: 225, A: 255},
		"gold":     {R: 255, G: 215, B: 0, A: 255},
		"emerald":  {R: 28, G: 200, B: 83, A: 255},
		"redstone": {R: 255, G: 0, B: 0, A: 255},
		"lapis":    {R: 30, G: 76, B: 164, A: 255},
		"nether":   {R: 138, G: 36, B: 36, A: 255},
		"end":      {R: 20, G: 14, B: 40, A: 255},
	}

	out := make(map[string]string, len(presets))
	for name, c := range presets {
		img := image.NewRGBA(image.Rect(0, 0, 64, 64))
		for y := 0; y < 64; y++ {
			for x := 0; x < 64; x++ {
				img.Set(x, y, c)
			}
		}
		out[name] = toDataURL(img)
	}
	return out
}

// ── допоміжні ──────────────────────────────────────────────────────────

func decodeBase64Image(dataURL string) (raw []byte, mimeType string, err error) {
	// data:image/png;base64,XXXX або просто XXXX
	if strings.HasPrefix(dataURL, "data:") {
		parts := strings.SplitN(dataURL, ",", 2)
		if len(parts) != 2 {
			return nil, "", fmt.Errorf("невалідний data URL")
		}
		header := parts[0] // data:image/png;base64
		idx := strings.Index(header, ":")
		if idx >= 0 {
			mimeType = header[idx+1:]
			if slashIdx := strings.Index(mimeType, ";"); slashIdx >= 0 {
				mimeType = mimeType[:slashIdx]
			}
		}
		dataURL = parts[1]
	}
	raw, err = base64.StdEncoding.DecodeString(dataURL)
	return
}

func toDataURL(img image.Image) string {
	var buf bytes.Buffer
	png.Encode(&buf, img)
	b64 := base64.StdEncoding.EncodeToString(buf.Bytes())
	return "data:image/png;base64," + b64
}
