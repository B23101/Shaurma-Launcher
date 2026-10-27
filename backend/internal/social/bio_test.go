package social

import (
	"strings"
	"testing"
)

// ТІ САМІ вектори, що в vps/internal/accounts/profile_test.go (bioVectors).
var bioVectors = []struct {
	name, in, out string
	ok            bool
}{
	{"звичайний", "Люблю шаурму", "Люблю шаурму", true},
	{"пробіли по краях", "  привіт  ", "привіт", true},
	{"переноси → пробіл", "рядок1\nрядок2\r\n\r\nрядок3", "рядок1 рядок2 рядок3", true},
	{"табуляції й повтори", "a\t\t b    c", "a b c", true},
	{"порожнє", "", "", true},
	{"лише пробіли", "   \n\t ", "", true},
	{"bidi-override вирізається", "abc\u202edef", "abcdef", true},
	{"нульова ширина вирізається", "a\u200bb\u200cc\ufeffd", "abcd", true},
	{"керуючі символи", "a\x00b\x07c\x1bd", "abcd", true},
	{"лише невидиме → порожнє", "\u200b\u200d\u200d\u202e", "", true},
	{"ZWJ-емодзі зберігається", "👨\u200d👩\u200d👧", "👨\u200d👩\u200d👧", true},
	{"рівно 120 символів", strings.Repeat("я", 120), strings.Repeat("я", 120), true},
	{"121 символ", strings.Repeat("я", 121), "", false},
	{"120 емодзі (по code point)", strings.Repeat("😀", 120), strings.Repeat("😀", 120), true},
	{"121 емодзі", strings.Repeat("😀", 121), "", false},
	{"недійсний UTF-8", "ab\xff\xfecd", "", false},
	{"зальго обрізається до 2 міток", "a" + strings.Repeat("\u0301", 10), "a\u0301\u0301", true},
	{"пробіли не рахуються до ліміту після очищення", strings.Repeat(" ", 500) + "ok", "ok", true},
}

func TestNormalizeBio(t *testing.T) {
	for _, v := range bioVectors {
		got, ok := NormalizeBio(v.in)
		if ok != v.ok || got != v.out {
			t.Errorf("%s: NormalizeBio(%q) = %q,%v; want %q,%v", v.name, v.in, got, ok, v.out, v.ok)
		}
	}
}

func TestSanitizeBioDropsInvalid(t *testing.T) {
	if SanitizeBio(strings.Repeat("x", 500)) != "" || SanitizeBio("ab\xff") != "" {
		t.Fatal("недійсне біо від сервера не має показуватись")
	}
	if SanitizeBio(" ok\n") != "ok" {
		t.Fatal("звичайне біо має очищатись")
	}
}

func TestSanitizeColorForProfile(t *testing.T) {
	if SanitizeColor("#FF8800") != "#ff8800" || SanitizeColor("red") != "" || SanitizeColor("#ff8800;x") != "" || SanitizeColor("url(x)") != "" {
		t.Fatal("колір: лише #RRGGBB")
	}
}
