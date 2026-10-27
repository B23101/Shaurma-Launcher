package shortcuts

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestPresetGlyph_AllPresets(t *testing.T) {
	// Усі 24 пресети з CreatePackPage мають бути відомі.
	presets := []string{
		"puzzle", "star", "heart", "diamond", "crown", "skull",
		"flame", "bolt", "sword", "shield", "axe", "target",
		"ghost", "alien", "robot", "brain", "cat", "dog",
		"bug", "rocket", "planet", "moon", "sun", "snowflake",
	}
	for _, p := range presets {
		if _, ok := PresetGlyph(p); !ok {
			t.Errorf("пресет %q не знайдено у PresetGlyphs", p)
		}
		// З префіксом теж має працювати.
		if _, ok := PresetGlyph("ti-" + p); !ok {
			t.Errorf("пресет ti-%q не знайдено у PresetGlyphs", p)
		}
	}
	// Невідомий — false.
	if _, ok := PresetGlyph("ti-неіснує"); ok {
		t.Error("невідомий пресет мав би повернути false")
	}
	if _, ok := PresetGlyph(""); ok {
		t.Error("порожній пресет мав би повернути false")
	}
}

func TestPresetIconToPNG_ProducesValidPNG(t *testing.T) {
	for _, p := range []string{"puzzle", "crown", "moon"} {
		data, ok := PresetIconToPNG(p, "#ff8a00", 256)
		if !ok {
			t.Fatalf("PresetIconToPNG(%q) — false", p)
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("png.Decode(%q): %v", p, err)
		}
		b := img.Bounds()
		if b.Dx() != 256 || b.Dy() != 256 {
			t.Errorf("%q: розмір %dx%d, хочемо 256x256", p, b.Dx(), b.Dy())
		}
	}
}

func TestPresetIconToPNG_ColorApplied(t *testing.T) {
	data, ok := PresetIconToPNG("crown", "#1234ab", 256)
	if !ok {
		t.Fatal("PresetIconToPNG — false")
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("png.Decode: %v", err)
	}
	// Контраст: гліф малюється БІЛИМ поверх кольорової підкладки (раніше
	// був кольором збірки і зливався з квадратом — «колір квадрата дуже
	// схожий на колір іконки»). У центральній зоні (де точно гліф корони)
	// має бути багато БІЛИХ пікселів, а фон поза гліфом — прозорий.
	white := 0
	total := 0
	for y := 60; y < 200; y++ {
		for x := 60; x < 200; x++ {
			c := color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
			total++
			if c.R == 255 && c.G == 255 && c.B == 255 && c.A == 255 {
				white++
			}
		}
	}
	if white < total/20 {
		t.Errorf("білих пікселів у центрі: %d (з %d) — замало, гліф не відрендерився", white, total)
	}
	// Край полотна має бути прозорим (фон прозорий, не білий).
	edge := color.RGBAModel.Convert(img.At(2, 2)).(color.RGBA)
	if edge.A != 0 {
		t.Errorf("кут = rgba(%d,%d,%d,%d), хочемо прозорий (A=0)", edge.R, edge.G, edge.B, edge.A)
	}
}

func TestPresetIconToPNG_UnknownPreset(t *testing.T) {
	if _, ok := PresetIconToPNG("ti-нема", "#fff", 256); ok {
		t.Error("невідомий пресет мав би повернути false")
	}
}

func TestParseHexColor(t *testing.T) {
	cases := []struct {
		in   string
		want *color.RGBA
	}{
		{"#ff8a00", &color.RGBA{255, 138, 0, 255}},
		{"FF8A00", &color.RGBA{255, 138, 0, 255}},
		{"#f80", &color.RGBA{255, 136, 0, 255}}, // #f80 → #ff8800
		{"", nil},
		{"#ff8a0", nil},
		{"#gggggg", nil},
	}
	for _, c := range cases {
		got := ParseHexColor(c.in)
		if c.want == nil {
			if got != nil {
				t.Errorf("ParseHexColor(%q) = %v, хочемо nil", c.in, got)
			}
			continue
		}
		if got == nil || *got != *c.want {
			t.Errorf("ParseHexColor(%q) = %v, хочемо %v", c.in, got, c.want)
		}
	}
}

func TestPresetIconToPNG_NoColor_Defaults(t *testing.T) {
	// Без кольору — дефолтний оранжевий лаунчера (#ff8a00), а не падіння.
	data, ok := PresetIconToPNG("star", "", 128)
	if !ok {
		t.Fatal("PresetIconToPNG без кольору — false")
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("png.Decode: %v", err)
	}
	_ = img
	_ = image.Rect(0, 0, 128, 128)
}
