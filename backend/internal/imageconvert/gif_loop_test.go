package imageconvert

import (
	"os"
	"path/filepath"
	"testing"
)

// ── NormalizeGIFLoopInfinite (баг 3: анімація «грає один раз і зупиняється») ──
// Функція має обнулити лічильник повторів NETSCAPE2.0-розширення (loop-count),
// щоб GIF відтворювався нескінченно. Якщо розширення немає — файл не чіпаємо.

// makeTestGIF будує мінімальний валідний GIF89a з NETSCAPE2.0 loop-count=1.
func makeTestGIF(loopLo, loopHi byte) []byte {
	gif := []byte("GIF89a")
	// Logical Screen Descriptor: 1x1, GCT відсутня.
	gif = append(gif, 0x01, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00)
	// NETSCAPE2.0 Application Extension (loop count).
	gif = append(gif, 0x21, 0xFF, 0x0B)
	gif = append(gif, []byte("NETSCAPE2.0")...)
	gif = append(gif, 0x03, 0x01, loopLo, loopHi, 0x00)
	// Image Descriptor 1x1 + дані.
	gif = append(gif, 0x2C, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00)
	gif = append(gif, 0x02, 0x02, 0x44, 0x01, 0x00)
	// Trailer.
	gif = append(gif, 0x3B)
	return gif
}

func TestNormalizeGIFLoopInfiniteFixesLoopCountOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), "icon.gif")
	if err := os.WriteFile(path, makeTestGIF(1, 0), 0644); err != nil {
		t.Fatal(err)
	}
	NormalizeGIFLoopInfinite(path)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Лічильник має стати 0 (нескінченно), решта байтів не змінюється.
	needle := []byte("NETSCAPE2.0")
	idx := -1
	for i := 0; i+len(needle)+4 < len(data); i++ {
		if string(data[i:i+len(needle)]) == string(needle) {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("NETSCAPE2.0 розширення не знайдено у результаті")
	}
	if data[idx+len(needle)+2] != 0x00 || data[idx+len(needle)+3] != 0x00 {
		t.Fatalf("loop-count не обнулений: %x %x", data[idx+len(needle)+2], data[idx+len(needle)+3])
	}
}

func TestNormalizeGIFLoopInfiniteKeepsInfiniteLoop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "icon.gif")
	if err := os.WriteFile(path, makeTestGIF(0, 0), 0644); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	NormalizeGIFLoopInfinite(path)
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("файл змінився, хоча loop вже нескінченний")
	}
}

func TestNormalizeGIFLoopInfiniteIgnoresNonGIF(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not_gif.bin")
	if err := os.WriteFile(path, []byte("PNG-not-a-gif-here"), 0644); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	NormalizeGIFLoopInfinite(path)
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("не-GIF файл був змінений")
	}
}
