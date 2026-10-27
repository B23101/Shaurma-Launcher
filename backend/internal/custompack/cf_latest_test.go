package custompack

// Юніт-тести вибору «останнього дозволеного файлу» (CFLatestFile) — без
// мережі, на синтетичних списках. Списки вважаються відсортованими
// новіші-перші (так повертає GetModFiles): releaseType 1=release,
// 2=beta, 3=alpha, 0=невідомий (легасі).

import "testing"

func cfHit(id int, rt int) CFFileHit {
	return CFFileHit{ID: id, ReleaseType: rt}
}

func TestCFLatestFileChannelRules(t *testing.T) {
	tests := []struct {
		name       string
		files      []CFFileHit // новіші-перші
		allowBeta  bool
		allowAlpha bool
		wantID     int // 0 = nil (жоден дозволений)
	}{
		{
			name:   "beta над release, без дозволу прев'ю → останній release",
			files:  []CFFileHit{cfHit(30, 2), cfHit(29, 2), cfHit(28, 2), cfHit(27, 1), cfHit(26, 2)},
			wantID: 27,
		},
		{
			name:      "beta над release, з дозволом beta → найсвіжіша beta",
			files:     []CFFileHit{cfHit(30, 2), cfHit(29, 2), cfHit(28, 1)},
			allowBeta: true,
			wantID:    30,
		},
		{
			name:      "лише beta, без дозволу → nil",
			files:     []CFFileHit{cfHit(30, 2), cfHit(29, 2)},
			wantID:    0,
		},
		{
			name:      "лише beta, з дозволом → beta",
			files:     []CFFileHit{cfHit(30, 2), cfHit(29, 2)},
			allowBeta: true,
			wantID:    30,
		},
		{
			name:   "найсвіжіший release → він і береться (alpha нижче не заважає)",
			files:  []CFFileHit{cfHit(30, 1), cfHit(29, 3), cfHit(28, 3)},
			wantID: 30,
		},
		{
			name:       "alpha над release, з allowAlpha → alpha",
			files:      []CFFileHit{cfHit(30, 3), cfHit(29, 1)},
			allowAlpha: true,
			wantID:     30,
		},
		{
			name:   "alpha без жодного дозволу → останній release",
			files:  []CFFileHit{cfHit(30, 3), cfHit(29, 3), cfHit(28, 1)},
			wantID: 28,
		},
		{
			name:       "невідомий канал (легасі) — стабільний, береться завжди",
			files:      []CFFileHit{cfHit(30, 0), cfHit(29, 3)},
			allowAlpha: false,
			wantID:     30,
		},
		{
			name:   "порожній список → nil",
			files:  []CFFileHit{},
			wantID: 0,
		},
		{
			name:      "only alpha, allowBeta без allowAlpha → nil (alpha не дозволена)",
			files:     []CFFileHit{cfHit(30, 3), cfHit(29, 3)},
			allowBeta: true,
			wantID:    0,
		},
		{
			name:       "only alpha, allowAlpha → alpha",
			files:      []CFFileHit{cfHit(30, 3), cfHit(29, 3)},
			allowAlpha: true,
			wantID:     30,
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := CFLatestFile(tc.files, tc.allowBeta, tc.allowAlpha)
			if tc.wantID == 0 {
				if got != nil {
					t.Fatalf("CFLatestFile() = %+v, хотіли nil", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("CFLatestFile() = nil, хотіли файл %d", tc.wantID)
			}
			if got.ID != tc.wantID {
				t.Fatalf("CFLatestFile().ID = %d, хотіли %d", got.ID, tc.wantID)
			}
		})
	}
}
