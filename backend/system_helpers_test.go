package main

import "testing"

// TestAutoGameRAM перевіряє авто-підбір RAM під фізичну пам'ять
// (docs/dev/OPTIMIZATION_PLAN.md §1.1): на слабких ПК гра не має отримувати
// дефолтні 4096 МБ, бо Windows + лаунчер підуть у своп.
func TestAutoGameRAM(t *testing.T) {
	tests := []struct {
		name     string
		totalMB  int64
		expected int
	}{
		{"2 ГБ (мінімум)", 2048, 2048},
		{"4 ГБ", 4096, 2048},
		{"6 ГБ (ціль плану)", 6144, 2560},
		{"8 ГБ", 8192, 3072},
		{"12 ГБ", 12288, 3584},
		{"16+ ГБ — класичний дефолт", 16384, 4096},
		{"32 ГБ", 32768, 4096},
		{"невідомо (0) — дефолт", 0, 4096},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := autoGameRAM(tt.totalMB)
			if got != tt.expected {
				t.Errorf("autoGameRAM(%d) = %d, очікували %d", tt.totalMB, got, tt.expected)
			}
			// Інваріант: рекомендація не перевищує половину фізичної пам'яті
			// (щоб Windows + лаунчер мали де жити) — крім екстремальних
			// машин < 4 ГБ, де гра все одно не потягнеться і лаунчер має
			// дати максимум можливого.
			if tt.totalMB >= 4096 && int64(got) > tt.totalMB/2 {
				t.Errorf("autoGameRAM(%d) = %d перевищує половину пам'яті (%d)", tt.totalMB, got, tt.totalMB/2)
			}
		})
	}
}
