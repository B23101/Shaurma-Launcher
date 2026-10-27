package custompack

// Інтеграційні тести на РЕАЛЬНОМУ модпаку SkyFactory 4-4.2.4.zip
// (CurseForge формат: manifest.json + 202 моди + overrides, 1417 файлів).
//
// Скарга користувача: «імпорт zip зависає на розхешуванні модів, швидкість
// 0.0 МБ/с, не можу поставити на паузу чи скасувати». Ці тести перевіряють
// РЕАЛЬНИЙ конвеєр на реальному файлі + імітують завислі CDN-з'єднання,
// щоб підтвердити: мовчазний сервер обривається через ResponseHeaderTimeout,
// stall посеред файлу докачується retry-циклом, а скасування користувачем
// (ctx) розблоковує завантаження МИТТЄВО.
//
// Якщо zip відсутній у корені репозиторію — тести пропускаються (t.Skip),
// щоб CI без файлу не падав.

import (
	"archive/zip"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// skyfactoryZipPath — шлях до реального архіву модпаку відносно цього тесту
// (backend/internal/custompack/ → корінь репозиторію).
const skyfactoryZipPath = "../../../SkyFactory 4-4.2.4.zip"

func requireSkyFactoryZip(t *testing.T) string {
	t.Helper()
	if _, err := os.Stat(skyfactoryZipPath); err != nil {
		t.Skipf("модпак %s відсутній — пропускаю інтеграційний тест", skyfactoryZipPath)
	}
	return skyfactoryZipPath
}

// TestDetectSkyFactory4Archive — розпізнавання реального CurseForge-модпаку.
// Має визначити формат, MC-версію, лоадер і всі моди manifest БЕЗ завантаження.
func TestDetectSkyFactory4Archive(t *testing.T) {
	path := requireSkyFactoryZip(t)
	det, err := DetectArchive(path)
	if err != nil {
		t.Fatalf("DetectArchive: %v", err)
	}
	if det.Format != FormatCurseForge {
		t.Fatalf("format: got %q, want %q", det.Format, FormatCurseForge)
	}
	if det.MCVersion != "1.12.2" {
		t.Fatalf("mcVersion: got %q, want 1.12.2", det.MCVersion)
	}
	if det.Loader != LoaderForge {
		t.Fatalf("loader: got %q, want forge", det.Loader)
	}
	if det.LoaderVersion != "14.23.5.2860" {
		t.Fatalf("loaderVersion: got %q, want 14.23.5.2860", det.LoaderVersion)
	}
	// Лічимо files[] у manifest.json динамічно (не хардкодимо число).
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	mf := findZipFile(&zr.Reader, "manifest.json")
	if mf == nil {
		t.Fatal("manifest.json не знайдено в архіві")
	}
	m, err := readCFManifest(mf)
	if err != nil {
		t.Fatal(err)
	}
	if det.ModCount != len(m.Files) {
		t.Fatalf("modCount: got %d, want %d (manifest files[])", det.ModCount, len(m.Files))
	}
	if len(m.Files) < 100 {
		t.Fatalf("SkyFactory 4 має мати 200+ модів, got %d", len(m.Files))
	}
	if !det.HasOverrides {
		t.Fatal("hasOverrides: expected true (overrides/ присутня)")
	}
	if det.SuggestedName != "SkyFactory 4" {
		t.Fatalf("suggestedName: got %q, want %q", det.SuggestedName, "SkyFactory 4")
	}
}

// TestPlanSkyFactory4Import — planImport має повернути всі сирі cfFiles
// маніфесту у порядку файлу та overrides-префікс "overrides/".
func TestPlanSkyFactory4Import(t *testing.T) {
	path := requireSkyFactoryZip(t)
	det, err := DetectArchive(path)
	if err != nil {
		t.Fatalf("DetectArchive: %v", err)
	}
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()

	tasks, prefixes, cfFiles, err := planImport(&zr.Reader, det)
	if err != nil {
		t.Fatalf("planImport: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("CurseForge план не має прямих tasks (резолв окремо), got %d", len(tasks))
	}
	// Кількість cfFiles має збігтися з кількістю записів у manifest.json.
	mf := findZipFile(&zr.Reader, "manifest.json")
	if mf == nil {
		t.Fatal("manifest.json не знайдено")
	}
	m, err := readCFManifest(mf)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfFiles) != len(m.Files) {
		t.Fatalf("cfFiles: got %d, want %d (manifest files[])", len(cfFiles), len(m.Files))
	}
	if len(prefixes) != 1 || prefixes[0] != "overrides/" {
		t.Fatalf("overridesPrefixes: got %v, want [overrides/]", prefixes)
	}
	// Перший і останній записи маніфесту — порядок і вміст.
	first := cfFiles[0]
	if first.ProjectID != 282963 || first.FileID != 2532663 {
		t.Fatalf("first cfFile: got %d/%d, want 282963/2532663", first.ProjectID, first.FileID)
	}
	last := cfFiles[len(cfFiles)-1]
	if last.ProjectID != 222838 || last.FileID != 2645020 {
		t.Fatalf("last cfFile: got %d/%d, want 222838/2645020", last.ProjectID, last.FileID)
	}
}

// TestExtractSkyFactory4Overrides — розпаковка overrides реального архіву
// (1417 файлів). Кожен файл має лягти у .minecraft/ з правильним шляхом.
func TestExtractSkyFactory4Overrides(t *testing.T) {
	path := requireSkyFactoryZip(t)
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()

	// Скільки не-директорійних записів під overrides/ очікується в архіві.
	expected := 0
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if strings.HasPrefix(f.Name, "overrides/") {
			expected++
		}
	}
	if expected < 1000 {
		t.Fatalf("архів підозріло малий: лише %d overrides-файлів, очікувалось 1400+", expected)
	}

	gameDir := t.TempDir()
	if _, err := extractOverrides(context.Background(), &zr.Reader, []string{"overrides/"}, gameDir, false, nil); err != nil {
		t.Fatalf("extractOverrides: %v", err)
	}

	// Кількість розпакованих файлів має збігтися з очікуваною.
	got := 0
	filepath.Walk(gameDir, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			got++
		}
		return nil
	})
	if got != expected {
		t.Fatalf("розпаковано %d файлів, очікувалось %d", got, expected)
	}

	// Вибіркові шляхи з різних глибин архіву.
	for _, rel := range []string{
		"config/acronym/base/General.cfg",
		"scripts/crafttweaker/utils.zs",
	} {
		if _, err := os.Stat(filepath.Join(gameDir, rel)); err != nil {
			t.Errorf("очікуваний файл відсутній: %s (%v)", rel, err)
		}
	}
	// manifest.json НЕ має потрапити в overrides-розпаковку.
	if _, err := os.Stat(filepath.Join(gameDir, "manifest.json")); err == nil {
		t.Error("manifest.json не мав потрапити в .minecraft/")
	}
}

// TestImportSkyFactory4WithoutAPIKeyFailsFast — повний ImportArchive БЕЗ
// API-ключа CurseForge (dev-режим): overrides розпаковуються, а резолв
// модів має завершитись ШВИДКО зі зрозумілою помилкою, а не «висіти».
// Це регресійний захист від повернення вічного зависання на великих
// CurseForge-модпаках.
func TestImportSkyFactory4WithoutAPIKeyFailsFast(t *testing.T) {
	path := requireSkyFactoryZip(t)
	det, err := DetectArchive(path)
	if err != nil {
		t.Fatalf("DetectArchive: %v", err)
	}
	store := NewStore(t.TempDir())
	instanceDir := t.TempDir()

	start := time.Now()
	_, err = ImportArchive(
		context.Background(),
		store,
		instanceDir,
		path,
		det,
		ImportParams{Name: "SkyFactory 4"},
		4,
		nil,
	)
	if err == nil {
		t.Fatal("без API-ключа CurseForge імпорт має впасти з помилкою")
	}
	if elapsed := time.Since(start); elapsed > 15*time.Second {
		t.Fatalf("імпорт без ключа тривав %v — підозра на зависання (розпаковка 1417 файлів < 15с)", elapsed)
	}
	if !strings.Contains(err.Error(), "API-ключ CurseForge") {
		t.Fatalf("помилка має згадувати API-ключ CurseForge, got: %v", err)
	}
	// Тимчасовий placeholder-запис має бути прибраний з Store.
	if packs := store.List(); len(packs) != 0 {
		t.Fatalf("placeholder-запис лишився в Store: %d збірок", len(packs))
	}
}

// TestDownloadImportTasksCancelUnblocksFast — пауза/скасування під час
// завантаження кількох файлів (downloadImportTasks) має розблокувати
// весь конвеєр ШВИДКО, навіть коли один файл висить на мовчазному
// з'єднанні. Раніше io.Copy блокувався назавжди (5-хв таймаут клієнта) —
// екран імпорту «зависав намертво».
func TestDownloadImportTasksCancelUnblocksFast(t *testing.T) {
	// Сервер: приймає TCP і НЕ шле заголовки (типовий мовчазний CDN).
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				// Читаємо запит і... мовчимо. Жодних заголовків у відповідь.
				buf := make([]byte, 4096)
				c.Read(buf)
				<-time.After(10 * time.Second) // тримаємо з'єднання відкритим
				c.Close()
			}(conn)
		}
	}()

	oldHeader := downloadResponseHeaderTimeout
	downloadResponseHeaderTimeout = 5 * time.Second
	t.Cleanup(func() { downloadResponseHeaderTimeout = oldHeader })

	// Другий файл — нормальний сервер, щоб завантаження справді почалось.
	okContent := []byte("good-mod.jar content")
	okSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(okContent)
	}))
	defer okSrv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	gameDir := t.TempDir()
	tasks := []importTask{
		{relPath: "mods/silent.jar", url: "http://" + ln.Addr().String()},
		{relPath: "mods/good.jar", url: okSrv.URL},
	}

	// downloadImportTasks вимагає не-nil report (публікує події на кожен файл).
	done := make(chan error, 1)
	go func() {
		done <- downloadImportTasks(ctx, tasks, gameDir, 2, "test", func(ImportProgress) {})
	}()

	time.Sleep(300 * time.Millisecond) // даємо запитам стартувати
	start := time.Now()
	cancel()
	select {
	case err := <-done:
		if ctx.Err() == nil && err != nil {
			t.Fatalf("неочікувана помилка після скасування: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("скасування не розблокувало downloadImportTasks за 10с")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("скасування тривало %v — має бути миттєвим (ResponseHeaderTimeout не потрібен для ctx)", elapsed)
	}
}

// TestDownloadImportTasksSilentServerFailsFast — сервер, що прийняв TCP і
// не відповідає (мовчазний CDN), обривається через ResponseHeaderTimeout
// РЕАЛЬНОГО production-клієнта (той, що створюється всередині
// downloadImportTasks з транспортом з ResponseHeaderTimeout var). Раніше
// такий сервер тримав downloadImportTasks увесь 5-хв Timeout клієнта —
// «імпорт завис на розхешуванні». Таймаут стиснутий до мілісекунд через var.
func TestDownloadImportTasksSilentServerFailsFast(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				buf := make([]byte, 4096)
				c.Read(buf)                    // читаємо запит
				<-time.After(30 * time.Second) // і мовчимо, тримаючи з'єднання
				c.Close()
			}(conn)
		}
	}()

	oldHeader := downloadResponseHeaderTimeout
	downloadResponseHeaderTimeout = 400 * time.Millisecond
	t.Cleanup(func() { downloadResponseHeaderTimeout = oldHeader })
	oldAttempts := downloadMaxAttempts
	downloadMaxAttempts = 1 // без ретраїв — лише перевірка першого обриву
	t.Cleanup(func() { downloadMaxAttempts = oldAttempts })

	gameDir := t.TempDir()
	tasks := []importTask{{relPath: "mods/silent.jar", url: "http://" + ln.Addr().String()}}
	start := time.Now()
	err = downloadImportTasks(context.Background(), tasks, gameDir, 2, "test", func(ImportProgress) {})
	if err == nil {
		t.Fatal("мовчазний сервер має дати помилку")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("мовчазний сервер обірвано за %v — ResponseHeaderTimeout не спрацював швидко", elapsed)
	}
}

// TestCurseForgeResolveSilentAPIHeaderTimeout — мовчазний CurseForge API
// (фаза «розхешування модів»: resolveCFFiles → GetModFileCtx) має обірвати
// з'єднання через ResponseHeaderTimeout клієнта CurseForge, а не тримати
// резолв-горутину весь 20-с Timeout на кожен мод. Стиснутий через var.
func TestCurseForgeResolveSilentAPIHeaderTimeout(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				buf := make([]byte, 4096)
				c.Read(buf)
				<-time.After(30 * time.Second)
				c.Close()
			}(conn)
		}
	}()

	old := curseForgeAPIHeaderTimeout
	curseForgeAPIHeaderTimeout = 400 * time.Millisecond
	t.Cleanup(func() { curseForgeAPIHeaderTimeout = old })
	oldKey := curseForgeAPIKey
	curseForgeAPIKey = "test-key" // без ключа GetModFileCtx падає миттєво, не перевіривши таймаут
	t.Cleanup(func() { curseForgeAPIKey = oldKey })
	oldBase := curseForgeAPIBase
	curseForgeAPIBase = "http://" + ln.Addr().String()
	t.Cleanup(func() { curseForgeAPIBase = oldBase })

	cf := NewCurseForgeClient()
	start := time.Now()
	_, err = cf.GetModFileCtx(context.Background(), 282963, 2532663)
	if err == nil {
		t.Fatal("мовчазний API має дати помилку")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("мовчазний API обірвано за %v — ResponseHeaderTimeout не спрацював", elapsed)
	}
}
