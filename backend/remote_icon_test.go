package main

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"testing"
)

// Іконка лаунчера (будь-якого розміру) стає PNG-мініатюрою, що вміщується в
// ліміт VPS; не-картинка — порожньо.
func TestRemoteIconThumb(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 256, 256))
	for y := 0; y < 256; y++ {
		for x := 0; x < 256; x++ {
			src.SetNRGBA(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: uint8(x ^ y), A: 255})
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, src)
	thumb := remoteIconThumb("data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()))
	if thumb == "" || len(thumb) > remoteIconMaxB64 {
		t.Fatalf("мініатюра: %d символів", len(thumb))
	}
	raw, _ := base64.StdEncoding.DecodeString(thumb)
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil || img.Bounds().Dx() > 32 {
		t.Fatalf("мініатюра має бути PNG до 32×32: %v %v", err, img)
	}
	if remoteIconThumb("data:text/html;base64,PHNjcmlwdD4=") != "" || remoteIconThumb("") != "" {
		t.Error("не-картинка не має ставати мініатюрою")
	}
}
