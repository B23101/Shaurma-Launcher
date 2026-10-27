// Пакет versions містить порт алгоритму FlexVer порівняння версій із
// Prism Launcher (launcher/Version.cpp — https://git.sleeping.town/exa/FlexVer).
//
// Це ТОЙ САМИЙ алгоритм, яким Prism сортує всі версії (Minecraft, лоадери,
// Java) та визначає "рекомендовану" версію. Скопійовано 1:1 з C++-коду:
//
//	- parse: рядок розбивається на секції Numeric / Textual / PreRelease;
//	  '+' та все після нього ігнорується (appendices);
//	  у Numeric-секціях зрізаються провідні нулі;
//	  '-' або ' ' на початку секції з нецифровим наступним символом → PreRelease.
//	- порівняння секцій: Numeric vs Numeric → за довжиною, потім кодопойнтами;
//	  Null vs PreRelease → Null більший (pre-release сортується НИЖЧЕ за release);
//	  Null vs Textual → Null менший; усе інше → текст-порівняння за кодопойнтами.
//
// SortDesc — сортування за спаданням (найновіші перші) — те саме, що
// VersionFilterModel у Prism (SortRole = rawTime, DescendingOrder), але на
// основі FlexVer, щоб порядок був коректним навіть без часових міток.
package versions

import "sort"

// SectionType — тип секції, як Section::Type у Prism.
type SectionType uint8

const (
	SectionNull      SectionType = iota // відсутня секція (рядок закінчився)
	SectionTextual                      // будь-який текст ('.', '-', слова)
	SectionNumeric                      // цифри (провідні нулі зрізані)
	SectionPreRelease                   // пре-реліз компонент ("-beta", " Pre-Release")
)

type section struct {
	t SectionType
	v string
}

func isDigit(r rune) bool { return r >= '0' && r <= '9' }

// parse розбиває рядок версії на секції — точний порт Version::parse().
func parse(s string) []section {
	var out []section
	i := 0
	len_ := len(s)
	for i < len_ {
		t := SectionTextual
		c := s[i]
		if c == '+' {
			break // ігноруємо appendices ("1.20.1+build.5" == "1.20.1")
		}
		var v string
		if c == '-' || c == ' ' {
			v += string(c)
			i++
			// "-" чи " " на початку секції: якщо наступний символ НЕ цифра —
			// це пре-реліз компонент ("-beta", " Pre-Release").
			if i < len_ && !isDigit(rune(s[i])) {
				t = SectionPreRelease
			}
		} else if isDigit(rune(c)) {
			t = SectionNumeric
		}
		for ; i < len_; i++ {
			r := s[i]
			if (isDigit(rune(r)) != (t == SectionNumeric)) || // зміна типу = нова секція
				(r == ' ' && t == SectionNumeric) || // числова секція + пробіл = пре-реліз
				(r == '-' && t != SectionPreRelease) || // "---" валідний пре-реліз компонент
				r == '+' {
				break
			}
			v += string(r)
		}
		if v != "" {
			if t == SectionNumeric {
				v = trimLeadingZeros(v)
			}
			out = append(out, section{t: t, v: v})
		}
	}
	return out
}

// trimLeadingZeros прибирає всі провідні нулі ("007" → "7", "0" → "").
func trimLeadingZeros(s string) string {
	j := 0
	for j < len(s) && s[j] == '0' {
		j++
	}
	return s[j:]
}

var nullSection = section{t: SectionNull}

// compareSections — порт Section::operator<=>.
func compareSections(a, b section) int {
	// Обидві числові: порівнюємо за довжиною, потім кодопойнтами.
	if a.t == SectionNumeric && b.t == SectionNumeric {
		if len(a.v) != len(b.v) {
			if len(a.v) < len(b.v) {
				return -1
			}
			return 1
		}
		return compareBytes(a.v, b.v)
	}
	// Null секції.
	if a.t == SectionNull {
		if b.t == SectionPreRelease {
			return 1 // null > prerelease
		}
		return -1 // null < textual
	}
	if b.t == SectionNull {
		if a.t == SectionPreRelease {
			return -1
		}
		return 1
	}
	// Текстове порівняння (різні типи або обидві текстові/пре-реліз).
	return compareBytes(a.v, b.v)
}

// compareBytes порівнює рядки послідовно за кодопойнтами (як QChar.unicode
// у Prism для ASCII ідентично), потім за довжиною.
func compareBytes(a, b string) int {
	minLen := len(a)
	if len(b) < minLen {
		minLen = len(b)
	}
	for i := 0; i < minLen; i++ {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	if len(a) != len(b) {
		if len(a) < len(b) {
			return -1
		}
		return 1
	}
	return 0
}

// Compare порівнює дві версії за FlexVer (порт Version::operator<=>).
// Повертає -1, якщо a < b; 0, якщо рівні; 1, якщо a > b.
//
// Приклад (як у Prism):
//
//	Compare("1.20.1-47.4.10", "1.20.1-47.4.9") > 0   // числове порівняння
//	Compare("1.20 Pre-Release 1", "1.20") < 0        // пре-реліз нижче релізу
//	Compare("1.20.1", "1.20") > 0
//	Compare("2.0", "10.0") < 0                       // за довжиною (2 < 10)
func Compare(a, b string) int {
	sa := parse(a)
	sb := parse(b)
	n := len(sa)
	if len(sb) > n {
		n = len(sb)
	}
	for i := 0; i < n; i++ {
		var x, y section
		if i < len(sa) {
			x = sa[i]
		} else {
			x = nullSection
		}
		if i < len(sb) {
			y = sb[i]
		} else {
			y = nullSection
		}
		if c := compareSections(x, y); c != 0 {
			return c
		}
	}
	return 0
}

// SortDesc сортує версії за спаданням (найновіша перша) за FlexVer.
// Це те саме відображення, що Prism показує у VersionSelectWidget
// (SortRole = rawTime, DescendingOrder) — найновіші зверху.
func SortDesc(versions []string) {
	sort.SliceStable(versions, func(i, j int) bool {
		return Compare(versions[i], versions[j]) > 0
	})
}
