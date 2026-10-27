package java

import "testing"

func TestRecommendedMajor(t *testing.T) {
	cases := map[string]int{
		"1.7.10":  8,
		"1.16.5":  8,
		"1.17.1":  17,
		"1.20.4":  17,
		"1.20.5":  21,
		"1.21.1":  21,
		"26.1.2":  25,
		"25.0.3":  25,
		"1.21-pre1": 21,
		"":         17,
	}
	for in, want := range cases {
		if got := RecommendedMajor(in); got != want {
			t.Errorf("RecommendedMajor(%q) = %d, want %d", in, got, want)
		}
	}
}
