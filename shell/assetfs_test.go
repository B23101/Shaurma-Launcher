package main

// assetfs_test.go — тест пріоритету шляхів asset-сервера.
//
// РЕГРЕСІЙНИЙ ТЕСТ: головне вікно лаунчера запитує "index.html", а splash
// апдейтера — "updater-splash.html". Обидві теки (frontend/ і
// updater-frontend/) містять index.html. Раніше обробник шукав
// updater-frontend ПЕРШИМ для всіх шляхів — головне вікно отримувало
// splash замість фронтенду лаунчера, і користувач бачив «екран оновлення
// на 90%» замість головного екрана.

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// buildAssetDirs створює тимчасові frontend/ та updater-frontend/ з різним
// вмістом і повертає handler через assetHandlerWithRoots (те саме, що
// assetHandler(), але з явними теками — реальна логіка, без підміни
// os.Executable).
func buildAssetDirs(t *testing.T) http.Handler {
	t.Helper()
	base := t.TempDir()
	fe := filepath.Join(base, "frontend")
	ue := filepath.Join(base, "updater-frontend")
	if err := os.MkdirAll(fe, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(ue, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(dir, name, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Обидві теки мають index.html — саме та ситуація, яка ловила баг.
	write(fe, "index.html", "FRONTEND-INDEX")
	write(fe, "app.js", "//app")
	write(ue, "index.html", "UPDATER-INDEX")
	// Асет тільки в updater-frontend (fonts/) — фолбек для splash.
	write(ue, "fonts/a.ttf", "font")
	return assetHandlerWithRoots(fe, ue)
}

func getAsset(t *testing.T, h http.Handler, path string) (string, int) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Body.String(), rec.Code
}

// TestAssetHandlerFrontendFirst — головне вікно (index.html) отримує
// фронтенд ЛАУНЧЕРА, а не splash апдейтера.
func TestAssetHandlerFrontendFirst(t *testing.T) {
	h := buildAssetDirs(t)

	body, code := getAsset(t, h, "/index.html")
	if code != 200 || body != "FRONTEND-INDEX" {
		t.Fatalf("/index.html: очікувано FRONTEND-INDEX (200), отримано code=%d body=%q", code, body)
	}

	body, code = getAsset(t, h, "/app.js")
	if code != 200 || body != "//app" {
		t.Fatalf("/app.js: очікувано //app, отримано code=%d body=%q", code, body)
	}

	// Splash апдейтера — лише через віртуальний шлях updater-splash.html.
	body, code = getAsset(t, h, "/updater-splash.html")
	if code != 200 || body != "UPDATER-INDEX" {
		t.Fatalf("/updater-splash.html: очікувано UPDATER-INDEX (200), отримано code=%d body=%q", code, body)
	}

	// Асет splash (fonts/) — фолбек на updater-frontend.
	body, code = getAsset(t, h, "/fonts/a.ttf")
	if code != 200 || body != "font" {
		t.Fatalf("/fonts/a.ttf: очікувано font, отримано code=%d body=%q", code, body)
	}

	// Невідомий шлях — 404.
	_, code = getAsset(t, h, "/nope.txt")
	if code != http.StatusNotFound {
		t.Fatalf("/nope.txt: очікувано 404, отримано %d", code)
	}
}

// TestAssetHandlerSplashFallback — коли updater-frontend НЕ встановлений,
// splash віддає мінімальний вбудований HTML (updaterSplashHTML), а головне
// вікно все одно отримує фронтенд лаунчера.
func TestAssetHandlerSplashFallback(t *testing.T) {
	fe := t.TempDir()
	if err := os.WriteFile(filepath.Join(fe, "index.html"), []byte("FRONTEND-INDEX"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := assetHandlerWithRoots(fe, "") // updater-frontend відсутній

	body, code := getAsset(t, h, "/index.html")
	if code != 200 || body != "FRONTEND-INDEX" {
		t.Fatalf("/index.html: очікувано FRONTEND-INDEX, отримано code=%d body=%q", code, body)
	}

	body, code = getAsset(t, h, "/updater-splash.html")
	if code != 200 || body != updaterSplashHTML {
		t.Fatalf("/updater-splash.html: очікувано вбудований splash, отримано code=%d len=%d", code, len(body))
	}
}
