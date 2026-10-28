package main

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"strings"
)

// remoteIconMaxB64 — стеля мініатюри для друзів (VPS приймає до 2400 символів: rs_set має
// вміститись у кадр 8 КіБ разом із правами).
const remoteIconMaxB64 = 2400

// remoteIconThumb робить з data URL іконки сервера PNG-мініатюру 32×32 (base64), яку бачать
// друзі з доступом. "" — картинки нема, формат не підтримано або мініатюра завелика.
func remoteIconThumb(dataURL string) string {
	i := strings.Index(dataURL, ",")
	if !strings.HasPrefix(dataURL, "data:image/") || i < 0 {
		return ""
	}
	raw, err := base64.StdEncoding.DecodeString(dataURL[i+1:])
	if err != nil {
		return ""
	}
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return ""
	}
	for _, size := range []int{32, 24} {
		var buf bytes.Buffer
		enc := png.Encoder{CompressionLevel: png.BestCompression}
		if enc.Encode(&buf, boxResize(src, size)) != nil {
			return ""
		}
		if s := base64.StdEncoding.EncodeToString(buf.Bytes()); len(s) <= remoteIconMaxB64 {
			return s
		}
	}
	return ""
}

// boxResize — зменшення усередненням пікселів (без сторонніх бібліотек).
func boxResize(src image.Image, size int) *image.NRGBA {
	b := src.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		y0, y1 := b.Min.Y+y*b.Dy()/size, b.Min.Y+(y+1)*b.Dy()/size
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for x := 0; x < size; x++ {
			x0, x1 := b.Min.X+x*b.Dx()/size, b.Min.X+(x+1)*b.Dx()/size
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var r, g, bl, a, n uint64
			for yy := y0; yy < y1; yy++ {
				for xx := x0; xx < x1; xx++ {
					c := color.NRGBAModel.Convert(src.At(xx, yy)).(color.NRGBA)
					r, g, bl, a = r+uint64(c.R), g+uint64(c.G), bl+uint64(c.B), a+uint64(c.A)
					n++
				}
			}
			dst.SetNRGBA(x, y, color.NRGBA{R: uint8(r / n), G: uint8(g / n), B: uint8(bl / n), A: uint8(a / n)})
		}
	}
	return dst
}
