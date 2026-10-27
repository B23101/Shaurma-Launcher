// preset.go — рендеринг ПРЕСЕТ-іконок збірок (Tabler, напр. "ti-puzzle")
// у PNG для ярликів на робочому столі.
//
// Пресет-іконка — це не PNG-файл, а кодова точка у шрифті Tabler Icons
// (як у лаунчері: <i class="ti ti-puzzle" style="color:var(--pcol)">).
// Щоб ярлик мав іконку і для збірок з пресетом (а не лише для тих, у кого
// власний PNG), ми рендеримо гліф шрифту в PNG і фарбуємо його в колір
// збірки — точно як виглядає картка у лаунчері.
//
// Шрифт tabler-icons.ttf (v3.46, MIT) скопійований з node_modules
// фронтенду; кодові точки пресетів — з тих самих .ti-* класів CSS, які
// використовує фронтенд. Якщо шрифт/гліф раптом зміняться в новій версії
// іконок — тут лишиться ОСТАННЯ відома копія, іконки для ярликів не
// зламаються (на відміну від посилання на вбудований CSS фронту).

package shortcuts

import (
	"bytes"
	_ "embed"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

//go:embed tabler-icons.ttf
var tablerFontData []byte

// PresetIconGlyphs — кодова точка кожного пресету, який може обрати
// користувач у лаунчері (CreatePackPage presetIcons). Значення — те, що
// фронтенд підставляє у content: "\eb10" тощо.
var PresetIconGlyphs = map[string]rune{
	"puzzle":    0xeb10,
	"star":      0xeb2e,
	"heart":     0xeabe,
	"diamond":   0xeb65,
	"crown":     0xed12,
	"skull":     0xf292,
	"flame":     0xec2c,
	"bolt":      0xea38,
	"sword":     0xf030,
	"shield":    0xeb24,
	"axe":       0xef9f,
	"target":    0xeb35,
	"ghost":     0xeb8e,
	"alien":     0xebde,
	"robot":     0xf00b,
	"brain":     0xf59f,
	"cat":       0xf65b,
	"dog":       0xf660,
	"bug":       0xea48,
	"rocket":    0xec45,
	"planet":    0xec08,
	"moon":      0xeaf8,
	"sun":       0xeb30,
	"snowflake": 0xec0b,
	// Істоти
	"dragon": 0x10272,
	"fish":   0xef2b,
	"spider": 0xf293,
	"paw":    0xeff9,
	"bone":   0xedb8,
	"feather": 0xee8b,
	// Природа
	"leaf":      0xed4f,
	"tree":      0xef01,
	"mushroom":  0xef14,
	"flower":    0xeff6,
	"cactus":    0xf21b,
	"clover":    0xf1ea,
	// Їжа
	"apple":     0xef21,
	"cherry":    0xf511,
	"pizza":     0xedbb,
	"coffee":    0xef0e,
	"beer":      0xefa1,
	"cake":      0xf00f,
	"candy":     0xef0d,
	"cookie":    0xfdb1,
	"ice-cream": 0xeac2,
	"egg":       0xeb8a,
	"cheese":    0xef26,
	"salad":     0xf50a,
	// Битва
	"bow":     0xf096,
	"bomb":    0xf59c,
	"droplet": 0xea97,
	"helmet":  0xefca,
	"gavel":   0xef90,
	"swords":  0xf132,
	// Транспорт
	"plane":  0xeb6f,
	"car":    0xebbb,
	"bike":   0xea36,
	"truck":  0xebc4,
	"bus":    0xebe4,
	"train":  0xed96,
	"ship":   0xec84,
	"anchor": 0xeb76,
	// Світ / квести
	"map":      0xeae9,
	"compass":  0xea79,
	"key":      0xeac7,
	"lock":     0xeae2,
	"tower":    0xf2cb,
	"trophy":   0xeb45,
	"medal":    0xec78,
	"award":    0xea2c,
	"flag":     0xeaa6,
	"chess":    0xf382,
	"dice":     0xeb66,
	"pokeball": 0xeec1,
	"wand":     0xebcb,
	"meteor":   0xf1fd,
	"comet":    0xec76,
	"galaxy":   0xfcb6,
	"space":    0xec0c,
	// Погода
	"cloud":      0xea76,
	"cloud-rain": 0xea72,
	"cloud-snow": 0xea73,
	"umbrella":   0xebf1,
	"sunrise":    0xef1c,
	"sunset":     0xec31,
	"moon-stars": 0xece7,
	// Дім / предмети
	"home":       0xeac1,
	"building":   0xea4f,
	"fence":      0xef2a,
	"palette":    0xeb01,
	"brush":      0xebb8,
	"paint":      0xeb00,
	"pencil":     0xeb04,
	"ruler":      0xeb1a,
	"calculator": 0xeb80,
	"cpu":        0xef8e,
	"wallet":     0xeb75,
	"coins":      0xf65d,
	// Техніка
	"device-gamepad":  0xeb63,
	"device-desktop":  0xea89,
	"device-laptop":   0xeb64,
	"device-mobile":   0xea8a,
}

// PresetGlyph визначає, чи є вказаний пресет-іконки відомим гліфом Tabler.
func PresetGlyph(icon string) (rune, bool) {
	if icon == "" {
		return 0, false
	}
	g, ok := PresetIconGlyphs[strings.TrimPrefix(icon, "ti-")]
	if !ok {
		// Ключі в мапі без префікса "ti-" (та сама звірка, що у фронту).
		g, ok = PresetIconGlyphs[icon]
	}
	return g, ok
}

// PresetIconToPNG малює пресет-іконку збірки в PNG у стилі картки
// сайдбара (як .sp-ic у лаунчері): темний квадрат зі скругленими кутами,
// поверх нього квадрат-підкладка кольору збірки (напівпрозорий, 22%),
// і зверху — гліф Tabler. Розмір size×size (для .lnk зручно 256, що
// відповідає іконкам 256×256 у лаунчері).
//
// КОНТРАСТ: гліф малюємо БІЛИМ, а не кольором збірки — раніше колір
// квадрата-підкладки і гліф були одним і тим самим кольором, і на
// маленькій іконці ярлика вони зливалися («колір квадрата дуже схожий на
// колір іконки»). Білий гліф на кольоровій підкладці читається і на
// робочому столі, і в меню «Пуск».
//
// Колір приймаємо у вигляді "#rrggbb" / "#rgb" (або порожнього рядка —
// тоді оранжевий, дефолтний акцент лаунчера). Невідомий пресет або
// помилка шрифту повертають (nil, false) — викликач має fallback.
func PresetIconToPNG(icon, hexColor string, size int) ([]byte, bool) {
	g, ok := PresetGlyph(icon)
	if !ok {
		return nil, false
	}
	col := ParseHexColor(hexColor)
	if col == nil {
		col = &color.RGBA{255, 138, 0, 255} // var(--orange) лаунчера
	}

	f, err := opentype.Parse(tablerFontData)
	if err != nil {
		return nil, false
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{
		Size:    float64(size) * 0.55, // гліф займає ~55% полотна — вписується в підкладку
		DPI:     72,
		Hinting: font.HintingNone,
	})
	if err != nil {
		return nil, false
	}
	defer face.Close()

	img := image.NewRGBA(image.Rect(0, 0, size, size))

	// 1. Темна підкладка-картка (var(--bg-input-hi) лаунчера: #282828).
	radius := size / 4 // як у сайдбара: 7px радіус на 28px картку
	fillRoundedRect(img, color.RGBA{0x28, 0x28, 0x28, 255}, radius)

	// 2. Квадрат кольору збірки, напівпрозорий (22% — трохи насиченіше,
	// ніж 16% у сайдбарі, щоб колір читався на маленькій іконці).
	fillRoundedRect(img, color.RGBA{col.R, col.G, col.B, 56}, radius)

	// 3. Гліф БІЛИМ по центру (контраст проти кольорової підкладки).
	adv, _ := face.GlyphAdvance(g)
	glyphW := adv.Ceil()
	glyphH := (fixed.I(size) * 55 / 100).Ceil()

	dx := (size - glyphW) / 2
	dy := (size-glyphH)/2 + glyphH // baseline: низ гліфа на dy

	white := image.NewUniform(color.RGBA{255, 255, 255, 255})
	d := font.Drawer{
		Dst:  img,
		Src:  white,
		Face: face,
		Dot:  fixed.P(dx, dy),
	}
	d.DrawString(string(g))

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, false
	}
	return buf.Bytes(), true
}

// fillRoundedRect заливає скруглений прямокутник кольором col на полотні
// img (розмір полотна = розмір скругленого квадрата). Пікселі поза
// закругленими кутами лишаються прозорими.
func fillRoundedRect(img *image.RGBA, col color.RGBA, radius int) {
	size := img.Bounds().Dx()
	if radius < 0 {
		radius = 0
	}
	if radius > size/2 {
		radius = size / 2
	}
	r2 := radius * radius

	mask := image.NewAlpha(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			// Центр кола кута, до якого належить піксель (якщо він у куті).
			cx, cy := x, y
			switch {
			case x < radius && y < radius:
				cx, cy = radius, radius
			case x >= size-radius && y < radius:
				cx, cy = size-radius-1, radius
			case x < radius && y >= size-radius:
				cx, cy = radius, size-radius-1
			case x >= size-radius && y >= size-radius:
				cx, cy = size-radius-1, size-radius-1
			}
			if cx != x || cy != y {
				dx, dy := x-cx, y-cy
				if dx*dx+dy*dy > r2 {
					continue // поза закругленням — прозорий
				}
			}
			mask.SetAlpha(x, y, color.Alpha{255})
		}
	}

	draw.DrawMask(img, img.Bounds(), image.NewUniform(col), image.Point{}, mask, image.Point{}, draw.Over)
}

// ParseHexColor розбирає "#rrggbb" або "#rgb" у color.RGBA. Повертає nil
// для будь-якого некоректного формату.
func ParseHexColor(s string) *color.RGBA {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	switch len(s) {
	case 6:
	case 3:
		// "#abc" → "#aabbcc"
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	default:
		return nil
	}
	var r, g, b uint8
	for i := 0; i < 6; i++ {
		v := hexVal(s[i])
		if v < 0 {
			return nil
		}
		switch i {
		case 0:
			r = uint8(v) << 4
		case 1:
			r |= uint8(v)
		case 2:
			g = uint8(v) << 4
		case 3:
			g |= uint8(v)
		case 4:
			b = uint8(v) << 4
		case 5:
			b |= uint8(v)
		}
	}
	return &color.RGBA{r, g, b, 255}
}

func hexVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}
