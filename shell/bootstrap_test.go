package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// TestValidateManifestArchitecture — маніфест СТАРОЇ монолітної
// архітектури (launcher.exe + shrm-updater.exe) має бути відхилений:
// інакше bootstrap качає старий launcher.exe у нову DLL-установку
// (реальний інцидент серпня 2026 — застарілий деплой на CDN). Неповний
// маніфест без обов'язкових компонентів — теж відхиляється.
func TestValidateManifestArchitecture(t *testing.T) {
	oldArch := &bootstrapRemoteManifest{Version: "2.0.3", Components: []bootstrapComponent{
		{Path: "launcher.exe"},
		{Path: "shrm-updater.exe"},
	}}
	if err := validateManifestArchitecture(oldArch); err == nil {
		t.Fatal("стара архітектура (launcher.exe) має бути відхилена")
	}

	missing := &bootstrapRemoteManifest{Version: "2.1.0", Components: []bootstrapComponent{
		{Path: backendLibraryName()},
		{Path: updaterLibraryName()},
	}}
	if err := validateManifestArchitecture(missing); err == nil {
		t.Fatal("неповний маніфест (без frontend.zip) має бути відхилений")
	}

	// Тестові дані використовують backendLibraryName()/updaterLibraryName()
	// замість буквальних "backend.dll"/"updater.dll" — на Linux ці функції
	// повертають "backend.so"/"updater.so", і validateManifestArchitecture
	// звіряє маніфест саме проти імен ПОТОЧНОЇ ОС (той самий контракт, що
	// й у продакшн-коді bootstrap.go), тож тест лишається коректним на
	// обох платформах, а не лише на Windows.
	ok := &bootstrapRemoteManifest{Version: "2.1.0", Components: []bootstrapComponent{
		{Path: "ShaurmLauncher.exe"},
		{Path: backendLibraryName()},
		{Path: updaterLibraryName()},
		{Path: "frontend.zip", Kind: "archive", ExtractTo: "frontend"},
		{Path: "updater-frontend.zip", Kind: "archive", ExtractTo: "updater-frontend"},
	}}
	if err := validateManifestArchitecture(ok); err != nil {
		t.Fatalf("нова архітектура має пройти валідацію: %v", err)
	}
}

// TestBootstrapDownload verifies the full bootstrap flow against a local
// httptest server: manifest fetch, component download with sha256 check,
// archive extraction, local manifest write.
func TestBootstrapDownload(t *testing.T) {
	// 1. Build fake components.
	updContent := []byte("updater-dll-content-12345")
	bkContent := []byte("backend-dll-content-67890")
	zipContent := buildTestZip(t, map[string]string{"index.html": "<html>hi</html>"})

	shaOf := func(b []byte) string {
		s := sha256.Sum256(b)
		return hex.EncodeToString(s[:])
	}

	// Поточний exe (у тесті — тестовий бінарник): bootstrapAll має його
	// пропустити (запущений файл не можна перезаписати).
	selfExe, _ := os.Executable()
	manifest := bootstrapRemoteManifest{
		Version: "9.9.9-boot",
		Components: []bootstrapComponent{
			{Path: filepath.Base(selfExe), URL: "/launcher/skip.exe", SHA256: "deadbeef", Size: 1},
			{Path: "updater.dll", URL: "/launcher/updater.dll", SHA256: shaOf(updContent), Size: int64(len(updContent))},
			{Path: "backend.dll", URL: "/launcher/backend.dll", SHA256: shaOf(bkContent), Size: int64(len(bkContent))},
			{Path: "frontend.zip", URL: "/launcher/frontend.zip", SHA256: shaOf(zipContent), Size: int64(len(zipContent)), Kind: "archive", ExtractTo: "frontend"},
		},
	}
	mj, _ := json.Marshal(manifest)

	// 2. httptest server serving the fake CDN.
	mux := http.NewServeMux()
	mux.HandleFunc("/launcher/launcher-version.json", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Shaurma-Token") != "TESTTOKEN" {
			http.Error(w, "no token", http.StatusUnauthorized)
			return
		}
		w.Write(mj)
	})
	mux.HandleFunc("/launcher/updater.dll", func(w http.ResponseWriter, r *http.Request) { w.Write(updContent) })
	mux.HandleFunc("/launcher/backend.dll", func(w http.ResponseWriter, r *http.Request) { w.Write(bkContent) })
	mux.HandleFunc("/launcher/frontend.zip", func(w http.ResponseWriter, r *http.Request) { w.Write(zipContent) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// 3. Point the package globals at the test server.
	oldBase, oldToken := cdnBase, cdnToken
	cdnBase, cdnToken = srv.URL, "TESTTOKEN"
	defer func() { cdnBase, cdnToken = oldBase, oldToken }()

	installDir := t.TempDir()
	// Тестуємо реальний бізнес-шлях bootstrapAll (той самий, що викликає
	// bootstrapUpdater); лише installDir підставляємо явно (exeDir у тесті
	// вказав би на теки go test).
	if err := bootstrapAll(installDir, &manifest); err != nil {
		t.Fatalf("bootstrap failed: %v", err)
	}

	// 4. Verify files on disk.
	assertFile := func(rel, want string) {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(installDir, rel))
		if err != nil {
			t.Fatalf("missing %s: %v", rel, err)
		}
		if string(data) != want {
			t.Fatalf("%s content mismatch: got %q want %q", rel, data, want)
		}
	}
	assertFile("updater.dll", string(updContent))
	assertFile("backend.dll", string(bkContent))
	// Архів після розпаковки прибирається (більше не потрібен — локальний
	// маніфест вже записаний, Diff порівнює маніфести, а не файли).
	if _, err := os.Stat(filepath.Join(installDir, "frontend.zip")); err == nil {
		t.Fatalf("frontend.zip має бути видалений після розпаковки")
	}
	// Extracted frontend.
	idx, err := os.ReadFile(filepath.Join(installDir, "frontend", "index.html"))
	if err != nil || string(idx) != "<html>hi</html>" {
		t.Fatalf("frontend/index.html extraction failed: %v", err)
	}
	// Local manifest written.
	lm, err := os.ReadFile(filepath.Join(installDir, "launcher-version.json"))
	if err != nil {
		t.Fatalf("local manifest missing: %v", err)
	}
	var back bootstrapRemoteManifest
	if err := json.Unmarshal(lm, &back); err != nil || back.Version != "9.9.9-boot" {
		t.Fatalf("local manifest parse failed: %v (%s)", err, lm)
	}
	// No leftover .part files.
	matches, _ := filepath.Glob(filepath.Join(installDir, "*.part"))
	if len(matches) != 0 {
		t.Fatalf("leftover .part files: %v", matches)
	}
}

// TestBootstrapShaMismatchRejected — SHA256 у маніфесті НЕ збігається з
// вмістом файлу на CDN (підміна/MitM/неузгоджений деплой). ЗМІНА ПОВЕДІНКИ:
// раніше (за старим ТЗ) така розбіжність лише логувалась і файл лишався на
// диску — а updater.dll/backend.dll завантажуються в процес, тож це давало
// б виконання чужого коду. Тепер файл відхиляється, не лишається на диску
// (ні під справжнім ім'ям, ні як .part), встановлення зупиняється з
// помилкою.
func TestBootstrapShaMismatchRejected(t *testing.T) {
	updContent := []byte("updater-dll-content-12345")
	wrongHash := fmt.Sprintf("%064x", 0)

	mux := http.NewServeMux()
	mux.HandleFunc("/launcher/updater.dll", func(w http.ResponseWriter, r *http.Request) { w.Write(updContent) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	oldBase, oldToken, oldDelay := cdnBase, cdnToken, bootstrapRetryDelay
	cdnBase, cdnToken, bootstrapRetryDelay = srv.URL, "TESTTOKEN", 0
	defer func() { cdnBase, cdnToken, bootstrapRetryDelay = oldBase, oldToken, oldDelay }()

	m := bootstrapRemoteManifest{
		Version: "1.0.0",
		Components: []bootstrapComponent{
			{Path: "updater.dll", URL: "/launcher/updater.dll", SHA256: wrongHash, Size: int64(len(updContent))},
		},
	}
	installDir := t.TempDir()
	if err := bootstrapAll(installDir, &m); err == nil {
		t.Fatal("файл із невірним sha256 має бути відхилений")
	}
	for _, f := range []string{"updater.dll", "updater.dll.part", "launcher-version.json"} {
		if _, err := os.Stat(filepath.Join(installDir, f)); err == nil {
			t.Fatalf("%s не має лишатись на диску після відхилення", f)
		}
	}
}

// TestBootstrapRejectsOversizeAndShort — CDN віддає більше/менше байтів, ніж
// заявлено в маніфесті.
func TestBootstrapRejectsOversizeAndShort(t *testing.T) {
	content := []byte("0123456789")
	sum := sha256.Sum256(content)
	hexSum := hex.EncodeToString(sum[:])

	serve := func(body []byte) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(body) }))
	}
	oldBase, oldToken, oldDelay := cdnBase, cdnToken, bootstrapRetryDelay
	defer func() { cdnBase, cdnToken, bootstrapRetryDelay = oldBase, oldToken, oldDelay }()
	bootstrapRetryDelay = 0

	for name, body := range map[string][]byte{
		"більше": append(append([]byte{}, content...), 'X'),
		"менше":  content[:5],
	} {
		srv := serve(body)
		cdnBase, cdnToken = srv.URL, "T"
		c := bootstrapComponent{Path: "backend.dll", URL: "/launcher/backend.dll", SHA256: hexSum, Size: int64(len(content))}
		dir := t.TempDir()
		if err := downloadComponent(urlOf(c), c, dir); err == nil {
			t.Fatalf("%s: невідповідність розміру не відхилена", name)
		}
		if _, err := os.Stat(filepath.Join(dir, "backend.dll")); err == nil {
			t.Fatalf("%s: файл лишився на диску", name)
		}
		srv.Close()
	}

	// Позитивний контроль: точні байти проходять.
	srv := serve(content)
	defer srv.Close()
	cdnBase, cdnToken = srv.URL, "T"
	c := bootstrapComponent{Path: "backend.dll", URL: "/launcher/backend.dll", SHA256: hexSum, Size: int64(len(content))}
	if err := downloadComponent(urlOf(c), c, t.TempDir()); err != nil {
		t.Fatalf("коректний файл відхилено: %v", err)
	}
}

// TestValidateComponentAndURL — небезпечні шляхи/адреси з маніфесту
// відхиляються до завантаження; токен не віддається чужому хосту.
func TestValidateComponentAndURL(t *testing.T) {
	oldBase := cdnBase
	cdnBase = "https://cdn.example.test"
	defer func() { cdnBase = oldBase }()

	good := bootstrapComponent{
		Path: "backend.dll", URL: "/launcher/backend.dll", Size: 5,
		SHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	}
	if err := validateComponent(good); err != nil {
		t.Fatalf("коректний компонент відхилено: %v", err)
	}
	bad := map[string]func(c *bootstrapComponent){
		"..-шлях":           func(c *bootstrapComponent) { c.Path = "../evil.dll" },
		"абсолютний":        func(c *bootstrapComponent) { c.Path = "/etc/x" },
		"диск":              func(c *bootstrapComponent) { c.Path = `C:\x.dll` },
		"чужий хост":        func(c *bootstrapComponent) { c.URL = "https://evil.example/x" },
		"userinfo-трюк":     func(c *bootstrapComponent) { c.URL = "@evil.example/x" },
		"http":              func(c *bootstrapComponent) { c.URL = "http://cdn.example.test/x" },
		"без sha":           func(c *bootstrapComponent) { c.SHA256 = "" },
		"без розміру":       func(c *bootstrapComponent) { c.Size = 0 },
		"extractTo за межі": func(c *bootstrapComponent) { c.Kind = "archive"; c.ExtractTo = "../x" },
	}
	for name, mut := range bad {
		c := good
		mut(&c)
		if err := validateComponent(c); err == nil {
			t.Errorf("%s: небезпечний компонент пройшов: %+v", name, c)
		}
	}
}

// TestCDNClientRefusesCrossHostRedirect — токен каналу не має іти на інший
// хост через редирект.
func TestCDNClientRefusesCrossHostRedirect(t *testing.T) {
	evilHit := false
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		evilHit = true
		w.Write([]byte("x"))
	}))
	defer evil.Close()
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, evil.URL+"/steal", http.StatusFound)
	}))
	defer cdn.Close()
	oldToken := cdnToken
	cdnToken = "SECRET"
	defer func() { cdnToken = oldToken }()
	if _, err := fetchWithToken(cdn.URL + "/launcher/launcher-version.json"); err == nil {
		t.Fatal("редирект на інший хост мав бути відхилений")
	}
	if evilHit {
		t.Fatal("запит дійшов до стороннього хоста")
	}
}

// TestExtractZipZipSlip — zip-slip захист: шляхи типу ../ не виходять за межі.
func TestExtractZipZipSlip(t *testing.T) {
	// Створюємо zip з небезпечним ім'ям напряму через zip.Writer (Go дозволяє
	// писати такі імена в архів; розпаковувач має їх відхилити).
	badPath := writeTemp(t, "bad.zip", nil)
	fw, err := os.Create(badPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(fw)
	zf, _ := zw.Create("../evil.txt")
	zf.Write([]byte("boom"))
	zw.Close()
	fw.Close()

	dest := filepath.Join(t.TempDir(), "out")
	err = extractZipTo(badPath, dest)
	if err == nil {
		t.Fatal("zip-slip archive should be rejected")
	}
	if _, statErr := os.Stat(filepath.Join(filepath.Dir(dest), "evil.txt")); statErr == nil {
		t.Fatal("evil.txt escaped the extraction dir")
	}
}

// buildTestZip створює zip у пам'яті з заданими файлами.
func buildTestZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		full := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, []byte(content), 0o644)
	}
	zipPath := filepath.Join(t.TempDir(), "a.zip")
	zipDir(dir, zipPath)
	data, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatalf("read zip: %v", err)
	}
	return data
}

func writeTemp(t *testing.T, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// zipDir пакує теку dir у zipPath (вміст теки — в корінь zip).
func zipDir(dir, zipPath string) {
	out, err := os.Create(zipPath)
	if err != nil {
		panic(err)
	}
	zw := zip.NewWriter(out)
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		rel = filepath.ToSlash(rel)
		f, err := zw.Create(rel)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, err = f.Write(data)
		return err
	})
	zw.Close()
	out.Close()
}
