package social

// Біо — короткий опис у профілі (до MaxBioRunes символів, один рядок). Дзеркало VPS
// accounts.NormalizeBio і frontend normalizeBio: ті самі тестові вектори (bio_test.go ↔
// vps/internal/accounts/profile_test.go ↔ frontend/scripts/check-profile.mjs).
//
// Лаунчер чистить біо ПОВТОРНО, хоч сервер уже це зробив: дані друзів — чужі, а текст потрапляє
// в інтерфейс. Біо завжди виводиться як ТЕКСТ (ніколи як HTML); bidi-override, нульова ширина
// й керуючі символи вирізаються, стопка діакритики обмежена.

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxCombiningRun = 2

func isBioHidden(r rune) bool {
	switch {
	case r == 0x200d: // ZWJ — частина складених емодзі
		return false
	case r <= 0x1f, r >= 0x7f && r <= 0x9f:
		return true
	case r >= 0x200b && r <= 0x200f:
		return true
	case r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069:
		return true
	case r == 0xfeff, r == 0x2028, r == 0x2029, r == 0x061c, r == 0x180e:
		return true
	}
	return false
}

// NormalizeBio повертає очищене біо. ok=false — недійсний UTF-8 або > MaxBioRunes символів після
// очищення (мовчки не обрізаємо). Порожній результат дозволений.
func NormalizeBio(raw string) (string, bool) {
	if !utf8.ValidString(raw) {
		return "", false
	}
	var b strings.Builder
	marks := 0
	lastSpace, visible := true, false
	for _, r := range raw {
		switch {
		case unicode.IsSpace(r):
			if !lastSpace {
				b.WriteByte(' ')
				lastSpace = true
			}
			marks = 0
			continue
		case isBioHidden(r):
			continue
		case r == 0xfffd:
			continue
		case unicode.In(r, unicode.Mn, unicode.Me, unicode.Mc):
			marks++
			if marks > maxCombiningRun {
				continue
			}
		default:
			marks = 0
			if r != 0x200d {
				visible = true
			}
		}
		b.WriteRune(r)
		lastSpace = false
	}
	if !visible {
		return "", true
	}
	out := strings.TrimRight(b.String(), " ")
	if utf8.RuneCountInString(out) > MaxBioRunes {
		return "", false
	}
	return out, true
}

// SanitizeBio — біо від сервера для відображення: очищене або "" (недійсне не показуємо).
func SanitizeBio(raw string) string {
	b, _ := NormalizeBio(raw)
	return b
}
