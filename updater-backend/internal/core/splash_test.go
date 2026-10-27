package core

import (
	"testing"
	"time"
)

// TestHoldSplash_AlreadyElapsed — якщо з моменту старту вже минуло більше
// за мінімум, holdSplash НЕ додає зайвої затримки (сплеш і так висів
// достатньо — напр. довгий запит до CDN).
func TestHoldSplash_AlreadyElapsed(t *testing.T) {
	start := time.Now().Add(-2 * minSplashShow)
	began := time.Now()
	holdSplash(start, minSplashShow)
	if d := time.Since(began); d > 100*time.Millisecond {
		t.Fatalf("holdSplash спав надто довго (%v) при вже пройденому мінімумі", d)
	}
}

// TestHoldSplash_HoldsMinimum — якщо старт був щойно, сплеш тримається
// ~minSplashShow (не менше мінімуму мінус невеликий допуск на таймер).
func TestHoldSplash_HoldsMinimum(t *testing.T) {
	start := time.Now()
	began := time.Now()
	holdSplash(start, minSplashShow)
	elapsed := time.Since(began)
	if elapsed < minSplashShow-150*time.Millisecond {
		t.Fatalf("holdSplash протримав екран лише %v, очікувалось ≥ %v", elapsed, minSplashShow)
	}
	if elapsed > minSplashShow+500*time.Millisecond {
		t.Fatalf("holdSplash протримав екран занадто довго: %v", elapsed)
	}
}
