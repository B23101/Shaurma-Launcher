package mods

// icon.go — витяг іконки мода з jar у вигляді data-URL.
//
// ЧОМУ ЦЕ ОКРЕМИЙ ФАЙЛ І ЧОМУ З МАСШТАБУВАННЯМ (виправлення «лаунчер
// їсть до 2 ГБ на збірці з 300+ модів»):
//
// Раніше іконка бралася з jar ЯК Є (ліміт — аж 3 МБ) і кодувалась у
// base64 (+33% до розміру) прямо в ModEntry.IconData. Автори модів
// кладуть у jar іконку ресурспаку 256×256, 512×512, а подекуди й
// 1024×1024 PNG — це сотні кілобайт на МОД. Далі цей самий рядок
// одночасно існує у: кеші метаданих jar, слайсі результату скану, кеші
// перевірки оновлень, JSON-буфері backend_invoke, C-рядку на межі
// backend.so → shell, Go-копії в shell і, нарешті, у JSON-повідомленні
// Wails IPC. Тобто кожен мегабайт іконок множився разів на шість — на
// 300 модах це і давало гігабайти RSS процесу лаунчера (саме процесу, а
// не WebView: WebKitGTK живе в окремих процесах).
//
// UI показує ці іконки розміром ~32-48 CSS-пікселів. Тому тут іконка
// зменшується до iconMaxSide і перекодовується в PNG: типова вага
// падає з сотень КБ до 2-4 КБ, тобто весь список на 300 модів — це
// ~1 МБ замість сотень мегабайт, і всі копії вздовж ланцюга стають
// мізерними. Візуально різниці нема (на HiDPI 64px все ще з запасом).
//
// Іконки, менші за iconKeepAsIsBytes, віддаються без перекодування —
// не варто витрачати CPU на декод/енкод того, що й так крихітне.

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"image"
	_ "image/gif"  // декодер gif для image.Decode
	_ "image/jpeg" // декодер jpeg для image.Decode
	"image/png"
	"io"
	"path/filepath"
	"strings"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // декодер webp для image.Decode
)

const (
	// iconMaxSourceBytes — більший файл всередині jar іконкою не вважаємо
	// (раніше було 3 МБ: такий «іконкою» і не є, а декодувати його — це
	// ще й десятки мегабайт RGBA у пам'яті на час декоду).
	iconMaxSourceBytes = 1 << 20 // 1 МБ

	// iconKeepAsIsBytes — до цього розміру іконка віддається як є.
	iconKeepAsIsBytes = 8 << 10 // 8 КБ

	// iconMaxSide — максимальна сторона мініатюри в пікселях.
	iconMaxSide = 64
)

// readZipFileDataURL повертає data-URL іконки з файлу всередині jar
// (порожній рядок — якщо це не придатне зображення).
func readZipFileDataURL(f *zip.File) string {
	if f.UncompressedSize64 > iconMaxSourceBytes {
		return ""
	}
	rc, err := f.Open()
	if err != nil {
		return ""
	}
	defer rc.Close()
	// LimitReader — захист від зіпсованого/зловмисного заголовка zip, де
	// UncompressedSize64 бреше про реальний розмір потоку.
	data, err := io.ReadAll(io.LimitReader(rc, iconMaxSourceBytes+1))
	if err != nil || len(data) == 0 || len(data) > iconMaxSourceBytes {
		return ""
	}

	ext := strings.ToLower(filepath.Ext(f.Name))

	// SVG — векторний, масштабувати нічого; беремо лише якщо він дрібний.
	if ext == ".svg" {
		if len(data) > iconKeepAsIsBytes {
			return ""
		}
		return dataURL("image/svg+xml", data)
	}

	// Уже дрібне — віддаємо без витрат на декод/енкод.
	if len(data) <= iconKeepAsIsBytes {
		return dataURL(mimeByExt(ext), data)
	}

	if thumb := thumbnailPNG(data); len(thumb) > 0 && len(thumb) < len(data) {
		return dataURL("image/png", thumb)
	}

	// Декодувати не вдалося (екзотичний формат — напр. анімований webp,
	// який x/image/webp не читає). Лишаємо оригінал, але лише якщо він
	// усе ще в розумних межах: інакше краще взагалі без іконки, ніж
	// піваркуша base64 на кожен мод.
	if len(data) <= 64<<10 {
		return dataURL(mimeByExt(ext), data)
	}
	return ""
}

// thumbnailPNG зменшує зображення до iconMaxSide по більшій стороні і
// повертає PNG-байти. nil — якщо формат не розпізнано.
func thumbnailPNG(data []byte) []byte {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return nil
	}
	if w > iconMaxSide || h > iconMaxSide {
		if w >= h {
			h = h * iconMaxSide / w
			w = iconMaxSide
		} else {
			w = w * iconMaxSide / h
			h = iconMaxSide
		}
		if w < 1 {
			w = 1
		}
		if h < 1 {
			h = 1
		}
		dst := image.NewNRGBA(image.Rect(0, 0, w, h))
		// CatmullRom — та сама якість масштабування, що вже
		// використовується для іконок збірок (internal/imageconvert).
		draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Over, nil)
		img = dst
	}
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(&buf, img); err != nil {
		return nil
	}
	return buf.Bytes()
}

func dataURL(mime string, data []byte) string {
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
}

func mimeByExt(ext string) string {
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".svg":
		return "image/svg+xml"
	case ".webp":
		return "image/webp"
	}
	return "image/png"
}
