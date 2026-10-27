// Пакет imageconvert — конвертація зображень та витяг домінантного
// кольору. Використовується браузером контенту: іконки модпаків з
// Modrinth/CurseForge часто приходять у форматі .webp, а картка збірки
// лаунчера показує PNG (packAssets / local-file://). Також рахується
// «колір збірки» (Pack.Color) — домінантний колір іконки, щоб картка й
// банер збірки підсвічувались кольором, який реально домінує на іконці
// (аналог того, як Prism витягує колір з іконки модпаку).
package imageconvert

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // реєструє декодер webp у image.Decode
)

// ToPNG читає зображення (png/jpg/webp/gif — формат визначається за
// магічними байтами через image.Decode) і переписує його у PNG.
// Використовується при імпорті іконки модпаку: Modrinth віддає іконки
// у webp, а картка/банер збірки читають png через local-file://.
func ToPNG(src io.Reader, dst io.Writer) error {
	img, _, err := image.Decode(src)
	if err != nil {
		return fmt.Errorf("не вдалося декодувати зображення: %w", err)
	}
	return png.Encode(dst, img)
}

// ConvertFileToPNG читає файл зображення будь-якого підтримуваного
// формату і зберігає PNG поруч із зазначеним шляхом (dst). Повертає
// шлях до PNG.
func ConvertFileToPNG(srcPath, dstPath string) error {
	in, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	defer out.Close()
	return ToPNG(in, out)
}

// NormalizeIconFile перетворює файл іконки у PNG: якщо файл уже PNG —
// лишає як є, інакше конвертує. Повертає шлях до PNG-файлу (той самий,
// якщо конвертація не потрібна, або новий .png поруч). Використовується
// коли джерело (іконка модпаку з мережі) може бути webp/jpg.
func NormalizeIconFile(srcPath string) (string, error) {
	if strings.EqualFold(ext(srcPath), ".png") {
		// PNG за сигнатурою — перевіряємо, щоб не думати, що webp з
		// розширенням .png це PNG (деякі CDN віддають конвертоване).
		if data, err := os.ReadFile(srcPath); err == nil && len(data) >= 8 &&
			bytes.Equal(data[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
			return srcPath, nil
		}
	}
	dst := strings.TrimSuffix(srcPath, ext(srcPath)) + ".png"
	if err := ConvertFileToPNG(srcPath, dst); err != nil {
		return "", err
	}
	return dst, nil
}

func ext(p string) string {
	i := strings.LastIndexByte(p, '.')
	if i < 0 {
		return ""
	}
	return strings.ToLower(p[i:])
}

// ── Домінантний колір ───────────────────────────────────────────────────

// DominantColor повертає домінантний (найчастіший за квантованою
// гістограмою) колір зображення у вигляді "#rrggbb". Алгоритм — як у
// простих «палітра з картинки» реалізаціях: зменшуємо зображення до
// 32×32, квантуємо кожен піксель до 4 біт на канал, рахуємо гістограму,
// ігноруючи майже прозорі та майже чорні/білі (вони не несуть «кольору
// збірки»), і беремо середній колір найбільшого кластера. Якщо картинка
// не декодується — повертаємо порожній рядок (викликач лишає колір за
// замовчуванням).
func DominantColor(src io.Reader) string {
	img, _, err := image.Decode(src)
	if err != nil {
		return ""
	}
	bounds := img.Bounds()
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 {
		return ""
	}
	// Зменшуємо до 32×32 — гістограма достатньо репрезентативна, а
	// обрахунок миттєвий навіть для 4K-скріншотів.
	thumb := image.NewRGBA(image.Rect(0, 0, 32, 32))
	draw.CatmullRom.Scale(thumb, thumb.Bounds(), img, bounds, draw.Over, nil)

	// Квантовані (4 біти/канал) пікселі → суми каналів для усереднення.
	type cluster struct {
		count      int
		r, g, b, a int64
	}
	hist := map[uint32]*cluster{}
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			c := thumb.RGBAAt(x, y)
			if c.A < 128 {
				continue // майже прозорі не рахуємо
			}
			q := uint32(c.R>>4)<<8 | uint32(c.G>>4)<<4 | uint32(c.B>>4)
			cl := hist[q]
			if cl == nil {
				cl = &cluster{}
				hist[q] = cl
			}
			cl.count++
			cl.r += int64(c.R)
			cl.g += int64(c.G)
			cl.b += int64(c.B)
			cl.a += int64(c.A)
		}
	}
	if len(hist) == 0 {
		return ""
	}
	clusters := make([]*cluster, 0, len(hist))
	for _, cl := range hist {
		clusters = append(clusters, cl)
	}
	sort.Slice(clusters, func(i, j int) bool { return clusters[i].count > clusters[j].count })

	// Беремо найбільший кластер; якщо він чорно-білий/сірий (насиченість
	// майже нульова) — шукаємо перший «кольоровий», інакше картка всіх
	// сірих іконок була б сірою і невиразною.
	best := clusters[0]
	for _, cl := range clusters {
		r := float64(cl.r) / float64(cl.count)
		g := float64(cl.g) / float64(cl.count)
		b := float64(cl.b) / float64(cl.count)
		maxc, minc := r, r
		if g > maxc {
			maxc = g
		}
		if b > maxc {
			maxc = b
		}
		if g < minc {
			minc = g
		}
		if b < minc {
			minc = b
		}
		// Насиченість > 0.15 або просто середній колір — беремо.
		if (maxc-minc) > 38 || cl.count >= best.count {
			best = cl
			if (maxc - minc) > 38 {
				break
			}
		}
	}
	r := best.r / int64(best.count)
	g := best.g / int64(best.count)
	b := best.b / int64(best.count)
	return fmt.Sprintf("#%02x%02x%02x", r, g, b)
}

// DominantColorFile — обгортка DominantColor для файлу на диску.
func DominantColorFile(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	return DominantColor(f)
}

// FetchIconToPNG завантажує іконку за URL у файл <dir>/icon.<ext>,
// нормалізує у PNG (webp→png) і повертає (шлях до PNG, домінантний
// колір #rrggbb). Використовується браузером контенту при встановленні
// модпаку: іконка проєкту з Modrinth/CurseForge стає іконкою збірки.
// Порожній шлях у разі помилки (викликач лишає пресет-іконку/колір).
func FetchIconToPNG(url, dir string) (string, string) {
	path := FetchImageToPNG(url, dir, "icon")
	if path == "" {
		return "", ""
	}
	return path, DominantColorFile(path)
}

// FetchImageToPNG — те саме завантаження+нормалізація, що в
// FetchIconToPNG, але без обчислення домінантного кольору і з довільним
// базовим ім'ям файлу (напр. "background" для фонового банера збірки,
// узятого з галереї проєкту Modrinth/CurseForge). Повертає порожній
// рядок при будь-якій помилці — викликач лишає збірку без фону.
func FetchImageToPNG(url, dir, baseName string) string {
	if url == "" {
		return ""
	}
	resp, err := http.Get(url)
	if err != nil {
		// Мовчазна відмова тут раніше означала, що картка збірки
		// лишалась без іконки без жодного сліду в логах — важко було
		// зрозуміти, чи іконка взагалі намагалась завантажитись.
		log.Printf("imageconvert: не вдалося завантажити %q (%s): %v", baseName, url, err)
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("imageconvert: %q (%s) відповів HTTP %d", baseName, url, resp.StatusCode)
		return ""
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil || len(data) == 0 {
		log.Printf("imageconvert: порожнє або пошкоджене тіло відповіді для %q (%s): %v", baseName, url, err)
		return ""
	}

	ext := guessExt(data)
	rawPath := filepath.Join(dir, baseName+"-raw"+ext)
	if err := os.WriteFile(rawPath, data, 0644); err != nil {
		log.Printf("imageconvert: не вдалося записати %q: %v", rawPath, err)
		return ""
	}

	// Анімаційні формати (баг 7): webp/gif зберігаємо ЯК Є, без конвертації
	// в PNG — інакше анімація (анімована іконка модпаку з браузера)
	// втрачалась би назавжди. Файл лишаємо під фінальним іменем
	// <dir>/<baseName>.webp (.gif) — localFileDataURL() у app.go вже вміє
	// віддавати такі файли з правильним MIME (image/webp, image/gif), а
	// DominantColorFile читає webp через golang.org/x/image/webp.
	if ext == ".webp" || ext == ".gif" {
		finalPath := filepath.Join(dir, baseName+ext)
		if err := os.Rename(rawPath, finalPath); err != nil {
			log.Printf("imageconvert: не вдалося перейменувати %q → %q: %v", rawPath, finalPath, err)
			return rawPath // краще повернути робочий шлях, ніж нічого
		}
		if ext == ".gif" {
			// GIF з loop-count=1 «відтворюється один раз і зупиняється» —
			// переписуємо NETSCAPE2.0-розширення на нескінченний цикл.
			NormalizeGIFLoopInfinite(finalPath)
		}
		return finalPath
	}

	pngPath, err := NormalizeIconFile(rawPath)
	if err != nil {
		log.Printf("imageconvert: не вдалося нормалізувати %q у PNG: %v", rawPath, err)
		os.Remove(rawPath)
		return ""
	}
	// Завжди приводимо фінальний шлях до <dir>/<baseName>.png — раніше
	// це робилось лише коли pngPath == rawPath (джерело вже було PNG за
	// сигнатурою), а у типовому випадку (Modrinth-іконки — webp) файл
	// лишався під іменем "<baseName>-raw.png", яке ніде більше не
	// очікується. Через це іконка технічно зберігалась на диску, але
	// показувалась/оновлювалась нестабільно — на відміну від фону, який
	// цей самий шлях не проходив.
	finalPath := filepath.Join(dir, baseName+".png")
	if pngPath != finalPath {
		if err := os.Rename(pngPath, finalPath); err != nil {
			log.Printf("imageconvert: не вдалося перейменувати %q → %q: %v", pngPath, finalPath, err)
			return pngPath // краще повернути робочий шлях, ніж нічого
		}
		pngPath = finalPath
	}
	if rawPath != finalPath {
		os.Remove(rawPath) // прибираємо тимчасовий "-raw" файл, якщо конвертація створила окремий PNG
	}
	return pngPath
}

// NormalizeGIFLoopInfinite переписує GIF так, щоб він відтворювався
// НЕСКІНЧЕННО: якщо файл містить NETSCAPE2.0-розширення з лічильником
// повторів != 0 (напр. loop-count=1 — «анімація грає один раз і зупиняється»),
// лічильник обнуляється (0 = нескінченно). Якщо розширення немає — нічого
// не робимо: стандартна поведінка декодерів і так — нескінченний цикл.
func NormalizeGIFLoopInfinite(path string) {
	data, err := os.ReadFile(path)
	if err != nil || len(data) < 16 || !bytes.Equal(data[:6], []byte{'G', 'I', 'F', '8', '7', 'a'}) && !bytes.Equal(data[:6], []byte{'G', 'I', 'F', '8', '9', 'a'}) {
		return
	}
	// NETSCAPE2.0 application extension: 0x21 0xFF 0x0B "NETSCAPE2.0"
	// 0x03 0x01 <loop_lo> <loop_hi> 0x00 — знаходимо сигнатуру і обнуляємо
	// лічильник (щоб не чіпати випадкові байти, перевіряємо обгортку).
	const ns = "NETSCAPE2.0"
	for i := 0; i+len(ns)+4 < len(data); i++ {
		if string(data[i:i+len(ns)]) != ns {
			continue
		}
		if i >= 3 && data[i-3] == 0x21 && data[i-2] == 0xFF && data[i-1] == 0x0B &&
			data[i+len(ns)] == 0x03 && data[i+len(ns)+1] == 0x01 {
			data[i+len(ns)+2] = 0x00
			data[i+len(ns)+3] = 0x00
			_ = os.WriteFile(path, data, 0644)
			return
		}
	}
}

// ResizePNGToFit повертає PNG-байти, зменшені так, щоб жодна сторона не
// перевищувала maxSide. Зображення, яке вже менше (або не декодується) —
// повертається як є: функція ніколи не «ламає» виклик через іконку.
//
// НАВІЩО. Іконка трею: у бекенді вшитий appicon.png 883×883, а Wails на
// Linux переганяє байти іконки в ARGB-pixmap (iconToPX) і віддає його
// через D-Bus як org.kde.StatusNotifierItem.IconPixmap — це
// 883×883×4 ≈ 3 МБ на КОЖНЕ оновлення іконки, тоді як панель показує
// 16–32 px. Масштабування тут прибирає і трафік D-Bus, і зайвий пік
// памʼяті на боці панелі.
func ResizePNGToFit(data []byte, maxSide int) []byte {
	if maxSide <= 0 || len(data) == 0 {
		return data
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return data
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= maxSide && h <= maxSide {
		return data
	}
	// Пропорційно, з збереженням співвідношення сторін.
	nw, nh := maxSide, maxSide
	if w > h {
		nh = h * maxSide / w
	} else {
		nw = w * maxSide / h
	}
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Over, nil)
	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return data
	}
	return buf.Bytes()
}

// guessExt визначає розширення за першими байтами (PNG/JPEG/WebP/GIF).
func guessExt(data []byte) string {
	if len(data) >= 8 && bytes.Equal(data[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
		return ".png"
	}
	if len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff {
		return ".jpg"
	}
	if len(data) >= 4 && data[0] == 'R' && data[1] == 'I' && data[2] == 'F' && data[3] == 'F' {
		return ".webp"
	}
	if len(data) >= 6 && string(data[:6]) == "GIF89a" {
		return ".gif"
	}
	return ".png"
}
