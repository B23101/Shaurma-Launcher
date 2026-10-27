package main

// Колір іконки мода для дерева залежностей.
//
// Дерево показує лише іконки; щоб підсвітити ребра й смужки в підказці
// кольором мода, бекенд один раз рахує «домінантний» колір іконки.
// Рахуємо тут, а не в JS: loopback-сервер іконок не віддає CORS-заголовків,
// тож canvas у WebView «забруднюється» й getImageData кидає SecurityError.

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"math"
	"sync"
)

var _ = png.Decode // реєструє декодер PNG

// iconColorCache — jar-ключ (шлях|розмір|mtime вже враховано в IconDataOf) → колір.
var (
	iconColorMu    sync.Mutex
	iconColorCache = map[string]string{}
)

// dominantColor повертає #RRGGBB найвиразнішого кольору зображення:
// піксельні вибірки зважуються насиченістю, майже прозорі, майже білі
// та майже чорні пікселі ігноруються. "" — якщо виразного кольору нема.
func dominantColor(body []byte) string {
	img, _, err := image.Decode(bytes.NewReader(body))
	if err != nil {
		return ""
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == 0 || h == 0 {
		return ""
	}
	step := w / 32
	if h > w {
		step = h / 32
	}
	if step < 1 {
		step = 1
	}
	// 12 відер за відтінком; в кожному — зважена сума RGB.
	var wsum [12]float64
	var rs, gs, bs [12]float64
	for y := b.Min.Y; y < b.Max.Y; y += step {
		for x := b.Min.X; x < b.Max.X; x += step {
			r16, g16, b16, a16 := img.At(x, y).RGBA()
			if a16 < 0x8000 {
				continue
			}
			// RGBA() повертає premultiplied — знімаємо множення.
			r := float64(r16) / float64(a16)
			g := float64(g16) / float64(a16)
			bl := float64(b16) / float64(a16)
			mx := math.Max(r, math.Max(g, bl))
			mn := math.Min(r, math.Min(g, bl))
			if mx < 0.12 || mn > 0.92 {
				continue
			}
			sat := 0.0
			if mx > 0 {
				sat = (mx - mn) / mx
			}
			if sat < 0.18 {
				continue
			}
			var hue float64
			d := mx - mn
			switch mx {
			case r:
				hue = math.Mod((g-bl)/d, 6)
			case g:
				hue = (bl-r)/d + 2
			default:
				hue = (r-g)/d + 4
			}
			if hue < 0 {
				hue += 6
			}
			k := int(hue*2) % 12
			wt := sat * (0.4 + mx)
			wsum[k] += wt
			rs[k] += r * wt
			gs[k] += g * wt
			bs[k] += bl * wt
		}
	}
	best, bw := -1, 0.0
	for i := range wsum {
		if wsum[i] > bw {
			best, bw = i, wsum[i]
		}
	}
	if best < 0 {
		return ""
	}
	r, g, bl := rs[best]/bw, gs[best]/bw, bs[best]/bw
	return fmt.Sprintf("#%02x%02x%02x", int(r*255+.5), int(g*255+.5), int(bl*255+.5))
}

// colorOfJar — колір іконки jar з кешем за (шлях, розмір, mtime).
func (a *App) colorOfJar(jar string, key string) string {
	iconColorMu.Lock()
	if c, ok := iconColorCache[key]; ok {
		iconColorMu.Unlock()
		return c
	}
	iconColorMu.Unlock()
	col := ""
	if _, body, ok := decodeImageDataURL(a.modMgr.IconDataOf(jar)); ok {
		col = dominantColor(body)
	}
	iconColorMu.Lock()
	if len(iconColorCache) > 4000 {
		iconColorCache = map[string]string{}
	}
	iconColorCache[key] = col
	iconColorMu.Unlock()
	return col
}
