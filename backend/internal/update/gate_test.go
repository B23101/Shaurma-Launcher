package update

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeManifestCDN — мінімальний імітатор worker: віддає лише маніфест
// (лаунчер більше НІЧОГО не качає — це робить тільки апдейтер, updater.dll).
func fakeManifestCDN(t *testing.T, version string) *httptest.Server {
	t.Helper()
	manifest, err := json.Marshal(map[string]interface{}{
		"version":      version,
		"releaseNotes": "",
		"components":   []interface{}{},
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("User-Agent"), "ShaurmaLauncher/") {
			http.Error(w, "no UA", http.StatusNotFound)
			return
		}
		if len(r.Header.Get("X-Shaurma-Token")) < 16 {
			http.Error(w, "no token", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/launcher/launcher-version.json":
			w.Header().Set("Content-Type", "application/json")
			w.Write(manifest)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCheckNoUpdate(t *testing.T) {
	srv := fakeManifestCDN(t, "2.1.0")

	c := NewChecker("2.1.0")
	c.SetToken("0123456789abcdef")
	c.SetUpdateURL(srv.URL + "/launcher/launcher-version.json")

	info, err := c.Check()
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if info.Available {
		t.Fatalf("версії однакові — оновлення бути не має: %+v", info)
	}
	if info.Version != "2.1.0" {
		t.Fatalf("remote версія: %s, очікувано 2.1.0", info.Version)
	}
}

func TestCheckUpdateAvailable(t *testing.T) {
	srv := fakeManifestCDN(t, "2.2.0")

	c := NewChecker("2.1.0")
	c.SetToken("0123456789abcdef")
	c.SetUpdateURL(srv.URL + "/launcher/launcher-version.json")

	info, err := c.Check()
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !info.Available {
		t.Fatal("2.2.0 новіша за 2.1.0 — оновлення має бути доступним")
	}
}

func TestCheckNoToken(t *testing.T) {
	c := NewChecker("2.1.0") // токен не заданий
	if _, err := c.Check(); err == nil {
		t.Fatal("очікувалась помилка про порожній токен")
	}
}

// fakeBetaManifestCDN — імітатор worker, що віддає І stable, І beta
// маніфести (та сама поведінка, що у справжнього worker.js для
// launcher/launcher-version.json та launcher/launcher-version-beta.json).
func fakeBetaManifestCDN(t *testing.T, stableVersion, betaVersion string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("User-Agent"), "ShaurmaLauncher/") {
			http.Error(w, "no UA", http.StatusNotFound)
			return
		}
		if len(r.Header.Get("X-Shaurma-Token")) < 16 {
			http.Error(w, "no token", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/launcher/launcher-version.json":
			json.NewEncoder(w).Encode(map[string]interface{}{"version": stableVersion, "releaseNotes": ""})
		case "/launcher/launcher-version-beta.json":
			json.NewEncoder(w).Encode(map[string]interface{}{"version": betaVersion, "releaseNotes": ""})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestCheckBetaChannelFindsNewerBeta — репродукція скарги «канал beta,
// у beta вклали нову версію, а лаунчер каже "актуальна версія"». Стабільна
// версія на CDN = поточна, бета — новіша: при каналі beta Check() має
// знайти оновлення (той самий результат, що в апдейтера).
func TestCheckBetaChannelFindsNewerBeta(t *testing.T) {
	srv := fakeBetaManifestCDN(t, "2.1.0", "2.1.0.1")

	c := NewChecker("2.1.0")
	c.SetToken("0123456789abcdef")
	c.SetUpdateURL(srv.URL + "/launcher/launcher-version.json")
	c.SetChannel("beta")

	info, err := c.Check()
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !info.Available {
		t.Fatalf("beta-канал з новішою beta-версією (2.1.0.1 > 2.1.0) — оновлення має бути доступним: %+v", info)
	}
	if info.Version != "2.1.0.1" {
		t.Fatalf("версія оновлення: %s, очікувано 2.1.0.1 (з beta-маніфесту)", info.Version)
	}
}

// TestCheckBetaChannelStableHigherStillFindsStable — beta-маніфест старіший
// або відсутній: Check у каналі beta має впасти на stable (не «скотитись»
// нижче актуальної версії). Тут stable новіша за поточну — оновлення все одно
// має бути знайдено, але версія — зі stable.
func TestCheckBetaChannelStableHigherStillFindsStable(t *testing.T) {
	srv := fakeBetaManifestCDN(t, "2.2.0", "2.1.5")

	c := NewChecker("2.1.0")
	c.SetToken("0123456789abcdef")
	c.SetUpdateURL(srv.URL + "/launcher/launcher-version.json")
	c.SetChannel("beta")

	info, err := c.Check()
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !info.Available {
		t.Fatal("stable 2.2.0 новіша за поточну 2.1.0 — оновлення має бути доступним")
	}
	if info.Version != "2.2.0" {
		t.Fatalf("має використовуватись stable-версія (бета старіша): %s", info.Version)
	}
}

// TestCheckBetaChannelNoBetaFileFallsBackToStable — beta-маніфесту на CDN
// немає (404): канал beta тихо лишається на stable (та сама семантика, що
// в апдейтера fetchBestRemoteManifest).
func TestCheckBetaChannelNoBetaFileFallsBackToStable(t *testing.T) {
	srv := fakeManifestCDN(t, "2.2.0") // лише stable, beta → 404

	c := NewChecker("2.1.0")
	c.SetToken("0123456789abcdef")
	c.SetUpdateURL(srv.URL + "/launcher/launcher-version.json")
	c.SetChannel("beta")

	info, err := c.Check()
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !info.Available {
		t.Fatal("stable 2.2.0 новіша за поточну 2.1.0 — оновлення має бути доступним навіть без beta-файлу")
	}
}

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"2.0.0", "2.0.0", 0},
		{"2.0", "2.0.0", 0},
		{"2.1.0", "2.0.5", 1},
		{"2.0.5", "2.1.0", -1},
		{"1.9.0", "1.10.0", -1},
		{"v2.0.1", "2.0.0", 1},
		{"2.1.0-beta.1", "2.1.0", 0}, // pre-release суфікс відкидається
		{"", "2.0.0", -1},
		// Бета-схема: 4-й сегмент — бета-суфікс, а точковий 0 рівнозначний
		// відсутності (2.1.5.0 == 2.1.5).
		{"2.1.5", "2.1.5.0", 0},
		{"2.1.5.0", "2.1.5", 0},
		{"2.1.5.1", "2.1.5", 1},  // beta 2.1.5.1 ВИЩА за stable 2.1.5
		{"2.1.5", "2.1.5.1", -1}, // і навпаки
		{"2.1.5.1", "2.1.5.0", 1},
		{"2.1.5.10", "2.1.5.9", 1},
		{"2.1.5.1", "2.1.6", -1}, // мажорна перемога стабільної
		{"2.1.6", "2.1.5.9", 1},
	}
	for _, c := range cases {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q, %q) = %d, очікувано %d", c.a, c.b, got, c.want)
		}
	}
}
