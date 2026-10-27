package social

import (
	"strings"
	"testing"
)

// ТІ САМІ вектори, що в vps/internal/accounts/handle_test.go і worker/tests/test.mjs.
var handleVectors = []struct {
	in, out string
	ok      bool
}{
	{"bob", "bob", true},
	{"  @Bob_4821 ", "bob_4821", true},
	{"@@bob", "", false},
	{"ab", "", false},
	{strings.Repeat("a", 32), strings.Repeat("a", 32), true},
	{strings.Repeat("a", 33), "", false},
	{"bo..b", "", false},
	{"bo__b", "", false},
	{".bob", "", false},
	{"bob.", "", false},
	{"_bob", "", false},
	{"bob_", "", false},
	{"b.o_b", "b.o_b", true},
	{"bob-1", "", false},
	{"боб", "", false},
	{"bob smith", "", false},
	{"", "", false},
}

func TestNormalizeHandleVectors(t *testing.T) {
	for _, v := range handleVectors {
		got, ok := NormalizeHandle(v.in)
		if ok != v.ok || got != v.out {
			t.Errorf("NormalizeHandle(%q) = %q,%v; want %q,%v", v.in, got, ok, v.out, v.ok)
		}
	}
}

func TestSanitizeHandle(t *testing.T) {
	if SanitizeHandle("bob_1") != "bob_1" {
		t.Error("валідний лишається")
	}
	for _, bad := range []string{"@bob", "Bob", " bob", "bo‮b", "a", ""} {
		if SanitizeHandle(bad) != "" {
			t.Errorf("%q має відкидатись", bad)
		}
	}
}
