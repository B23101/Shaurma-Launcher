//go:build windows

package main

// clipboard_windows_test.go — функціональна перевірка обох шляхів буфера
// обміну: запис (setClipboardText) і читання (getClipboardText). Саме ці два
// шляхи — єдині місця в застосунку, де ми торкаємось купи Windows, і саме в
// них розбір краш-дампів (docs/dev/РОЗСЛІДУВАННЯ_КРАШУ_0xC0000374.md) бачив
// місце виявлення пошкодження 0xC0000374. Тому тест перевіряє не «happy path
// API», а саме те, що читання повертає рівно те, що записали — включно з
// не-ASCII текстом (UTF-16, нуль-термінатор, обрізання по термінатору через
// ReadProcessMemory).

import (
	"image"
	"image/color"
	"strings"
	"testing"
)

func TestClipboardTextRoundTrip(t *testing.T) {
	// Буфер обміну — спільний ресурс робочого столу: зберігаємо, що там було,
	// і повертаємо назад, щоб тест не з'їв дані користувача.
	previous, _ := getClipboardText()
	t.Cleanup(func() { _ = setClipboardText(previous) })

	want := "привіт, шаурма 🍖 — invite @friend?n=Тест-42"
	if err := setClipboardText(want); err != nil {
		// У неінтерактивній сесії (CI, служба) буфера обміну може не бути
		// взагалі — тоді це не помилка коду.
		if strings.Contains(err.Error(), "зайнятий") {
			t.Skipf("буфер обміну недоступний у цій сесії: %v", err)
		}
		t.Fatalf("setClipboardText: %v", err)
	}

	got, err := getClipboardText()
	if err != nil {
		t.Fatalf("getClipboardText: %v", err)
	}
	if got != want {
		t.Fatalf("прочитано не те, що записано:\n got=%q\nwant=%q", got, want)
	}

	// Повторне читання мусить віддати те саме (кеш за номером послідовності
	// буфера не має «залипнути» на старому значенні).
	again, err := getClipboardText()
	if err != nil {
		t.Fatalf("getClipboardText (повторно): %v", err)
	}
	if again != want {
		t.Fatalf("повторне читання розійшлося з першим: %q != %q", again, want)
	}
}

// TestClipboardReadDoesNotTouchHeapForMissingText — коли в буфері не текст,
// читання має спокійно повернути "" (без відкриття буфера взагалі завдяки
// IsClipboardFormatAvailable): саме зайві дотики до купи й були проблемою.
func TestClipboardReadDoesNotTouchHeapForMissingText(t *testing.T) {
	previous, _ := getClipboardText()
	t.Cleanup(func() { _ = setClipboardText(previous) })

	img := image.NewRGBA(image.Rect(0, 0, 3, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 3; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 80), G: uint8(y * 120), B: 40, A: 255})
		}
	}
	if err := setClipboardImage(img); err != nil {
		if strings.Contains(err.Error(), "зайнятий") {
			t.Skipf("буфер обміну недоступний у цій сесії: %v", err)
		}
		t.Fatalf("setClipboardImage: %v", err)
	}
	// У буфері КАРТИНКА: читання тексту мусить повернути "" і без помилки
	// (інакше фронтенд показував би помилку при кожній вставці картинки).
	got, err := getClipboardText()
	if err != nil {
		t.Fatalf("getClipboardText на картинці: %v", err)
	}
	if got != "" {
		t.Fatalf("у буфері картинка, а прочитано текст: %q", got)
	}
}
