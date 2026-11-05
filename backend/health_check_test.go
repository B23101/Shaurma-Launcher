package main

import "testing"

func TestSuggestRAM(t *testing.T) {
	cases := []struct {
		alloc, rec int
		avail      int64
		want       int
	}{
		{13824, 0, 9226, 8192},    // більше, ніж вільно → вільне мінус запас, вниз до 512
		{4096, 8192, 32000, 8192}, // нижче рекомендованої → рекомендована
		{4096, 8192, 5000, 3584},  // рекомендована не влазить → що влазить
		{6144, 0, 1500, 2048},     // дуже мало вільної → не менше 2 ГБ
		{6000, 0, 0, 5632},        // невідома вільна → поточна, округлена
	}
	for _, c := range cases {
		if got := suggestRAM(c.alloc, c.rec, c.avail); got != c.want {
			t.Errorf("suggestRAM(%d,%d,%d) = %d, want %d", c.alloc, c.rec, c.avail, got, c.want)
		}
	}
}
