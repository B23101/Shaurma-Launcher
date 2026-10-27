package shortcuts

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"testing"
)

// testPNG будує крихітний PNG (ширина×висота, суцільний колір) — неважливо
// який, важливо що це валідний PNG, який Windows зможе вставити в .lnk.
func testPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{255, 128, 0, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}

func TestPNGToICO_Header(t *testing.T) {
	pngData := testPNG(t, 32, 32)
	ico, err := PNGToICO(pngData)
	if err != nil {
		t.Fatalf("PNGToICO: %v", err)
	}

	// ICONDIR: reserved(2)=0, type(2)=1, count(2)=1
	if len(ico) < 22 {
		t.Fatalf("замало байт: %d", len(ico))
	}
	if ico[0] != 0 || ico[1] != 0 {
		t.Errorf("reserved = %d,%d, хочемо 0,0", ico[0], ico[1])
	}
	if got := binary.LittleEndian.Uint16(ico[2:]); got != 1 {
		t.Errorf("type = %d, хочемо 1 (icon)", got)
	}
	if got := binary.LittleEndian.Uint16(ico[4:]); got != 1 {
		t.Errorf("count = %d, хочемо 1", got)
	}

	// ICONDIRENTRY: bytesInRes == len(png), imageOffset == 22
	entry := ico[6:22]
	if got := binary.LittleEndian.Uint32(entry[8:]); int(got) != len(pngData) {
		t.Errorf("bytesInRes = %d, хочемо %d", got, len(pngData))
	}
	if got := binary.LittleEndian.Uint32(entry[12:]); got != 22 {
		t.Errorf("imageOffset = %d, хочемо 22", got)
	}

	// Дані PNG лежать з offset 22 до кінця — один-в-один.
	if !bytes.Equal(ico[22:], pngData) {
		t.Error("PNG-дані в ICO не збігаються з вхідними")
	}
}

func TestPNGToICO_Errors(t *testing.T) {
	if _, err := PNGToICO(nil); err == nil {
		t.Error("порожній PNG мав би бути помилкою")
	}
	if _, err := PNGToICO(make([]byte, 5*1024*1024)); err == nil {
		t.Error("величезний PNG мав би бути помилкою")
	}
}

// TestPNGToICO_RoundTrip переконується, що ICO, згенерований з іконки 256×256
// (типовий розмір іконок збірок), коректно оголошує розмір 256 через
// width/height = 0.
func TestPNGToICO_RoundTrip(t *testing.T) {
	pngData := testPNG(t, 256, 256)
	ico, err := PNGToICO(pngData)
	if err != nil {
		t.Fatalf("PNGToICO: %v", err)
	}
	if ico[6] != 0 || ico[7] != 0 {
		t.Errorf("width/height мають бути 0 (256), а є %d,%d", ico[6], ico[7])
	}
}
