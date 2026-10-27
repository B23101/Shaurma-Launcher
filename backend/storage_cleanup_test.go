package main

import (
	"os"
	"path/filepath"
	"testing"

	"shaurma-launcher-backend/internal/builds"
	"shaurma-launcher-backend/internal/config"
	"shaurma-launcher-backend/internal/custompack"
	"shaurma-launcher-backend/internal/sessions"
)

// mkFile створює файл із заданим розміром (швидко, через Truncate).
func mkFile(t *testing.T, path string, size int64) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	if err := f.Truncate(size); err != nil {
		t.Fatalf("truncate %s: %v", path, err)
	}
	f.Close()
}

// newTestConfig створює config.Manager, що дивиться ВИКЛЮЧНО у тимчасову
// теку: і LOCALAPPDATA, і APPDATA перенаправляються у tmp, тож і стабільна
// тека конфігурації (ConfigDir), і папка даних за замовчуванням
// (defaultBaseDir) опиняються в tmp. Без цього тест міг перезаписати
// РЕАЛЬНИЙ launcher-location.json (що й сталося один раз — baseDir почав
// вказувати на tmp, і всі збірки виглядали «невстановленими»).
//
// ВАЖЛИВО: env-змінні задаються через t.Setenv — Go сам відновить їх
// після тесту, тож реальний конфіг не чіпається.
func newTestConfig(t *testing.T, dir string) *config.Manager {
	t.Helper()
	t.Setenv("LOCALAPPDATA", filepath.Join(dir, "_config"))
	t.Setenv("APPDATA", filepath.Join(dir, "_roaming"))
	// installDir порожній — фолбек на LOCALAPPDATA (перенаправлений у tmp)
	m := config.NewManager("")
	if err := m.SetAppDir(dir); err != nil {
		t.Fatalf("SetAppDir: %v", err)
	}
	return m
}

// newTestApp будує App з однією ВСТАНОВЛЕНОЮ кастомною збіркою заданої
// MC-версії (реєстр + custompack.Store), щоб колектори «використаних
// версій» вважали її живою. loader/loaderVersion — тип і версія лоадера
// збірки (для тестів профілів лоадерів).
func newTestApp(t *testing.T, dir, packID, mcVersion string, lv ...struct{ Loader, Version string }) (*App, *config.Manager) {
	t.Helper()
	cfg := newTestConfig(t, dir)

	reg := builds.NewRegistry(filepath.Join(dir, "_builds.json"))
	reg.Load()
	reg.SetInstalled(packID, builds.KindCustom, "1.0")

	loader, loaderVersion := "vanilla", ""
	if len(lv) > 0 {
		loader = lv[0].Loader
		loaderVersion = lv[0].Version
	}
	store := custompack.NewStore(cfg.ConfigDir())
	if err := store.Save(custompack.Pack{
		ID:            packID,
		Name:          packID,
		MCVersion:     mcVersion,
		Loader:        loader,
		LoaderVersion: loaderVersion,
	}); err != nil {
		t.Fatalf("custompack save: %v", err)
	}

	return &App{
		cfg:         cfg,
		buildsReg:   reg,
		customPacks: store,
		sessionsM:   sessions.NewManager(),
	}, cfg
}

// TestDirSizeParallel перевіряє, що паралельний підрахунок розміру теки
// збігається з реальним (вкладені теки, файли різних розмірів).
func TestDirSizeParallel(t *testing.T) {
	tmp := t.TempDir()
	mkFile(t, filepath.Join(tmp, "a", "f1.bin"), 1000)
	mkFile(t, filepath.Join(tmp, "a", "b", "f2.bin"), 2000)
	mkFile(t, filepath.Join(tmp, "c", "f3.bin"), 4096)
	mkFile(t, filepath.Join(tmp, "c", "f4.bin"), 1)

	got := dirSizeParallel(tmp)
	if got != 7097 {
		t.Errorf("dirSizeParallel = %d, want 7097", got)
	}
	empty := t.TempDir()
	if got := dirSizeParallel(empty); got != 0 {
		t.Errorf("dirSizeParallel(empty) = %d, want 0", got)
	}
	if got := dirSizeParallel(filepath.Join(tmp, "nope")); got != 0 {
		t.Errorf("dirSizeParallel(missing) = %d, want 0", got)
	}
}

// TestSortStorageVersions перевіряє сортування списку версій за розміром.
func TestSortStorageVersions(t *testing.T) {
	list := []StorageVersionEntry{
		{ID: "small", Bytes: 10},
		{ID: "big", Bytes: 5000},
		{ID: "mid", Bytes: 300},
	}
	sortStorageVersions(list)
	if list[0].ID != "big" || list[1].ID != "mid" || list[2].ID != "small" {
		t.Errorf("sortStorageVersions порядок неправильний: %+v", list)
	}
}

// TestSortStorageSuggestions перевіряє, що рекомендовані збірки йдуть
// першими, всередині — за розміром.
func TestSortStorageSuggestions(t *testing.T) {
	list := []StorageSuggestion{
		{ID: "small", Name: "s", Bytes: 10, Recommended: false},
		{ID: "rec-big", Name: "rb", Bytes: 9000, Recommended: true},
		{ID: "rec-small", Name: "rs", Bytes: 3000, Recommended: true},
	}
	sortStorageSuggestions(list)
	if list[0].ID != "rec-big" || list[1].ID != "rec-small" || list[2].ID != "small" {
		t.Errorf("sortStorageSuggestions порядок неправильний: %+v", list)
	}
}

// TestCachedDirSize перевіряє кеш розмірів та інвалідацію.
func TestCachedDirSize(t *testing.T) {
	tmp := t.TempDir()
	mkFile(t, filepath.Join(tmp, "f.bin"), 1234)
	if got := cachedDirSize(tmp); got != 1234 {
		t.Fatalf("cachedDirSize = %d, want 1234", got)
	}
	if got := cachedDirSize(tmp); got != 1234 {
		t.Fatalf("cachedDirSize (hit) = %d, want 1234", got)
	}
	mkFile(t, filepath.Join(tmp, "f2.bin"), 100)
	invalidateStorageSizes()
	if got := cachedDirSize(tmp); got != 1334 {
		t.Errorf("cachedDirSize (after invalidate) = %d, want 1334", got)
	}
}

// TestAnalyzeStorageVersionsNeoForgeProfile — регресійний тест бага
// «профілі лоадерів рахувались невикористаними»: NeoForge нумерує профілі
// без MC-версії в імені теки (neoforge-21.11.45 для MC 1.21.11). Профіль
// має бути «використаним», бо збірка з loader=neoforge і LV=21.11.45
// встановлена.
func TestAnalyzeStorageVersionsNeoForgeProfile(t *testing.T) {
	tmp := t.TempDir()
	mcDir := filepath.Join(tmp, "minecraft")
	// Ванільний корінь + профіль NeoForge, що від нього успадковується.
	for _, v := range []struct {
		dir  string
		json string
	}{
		{"1.21.11", `{"id":"1.21.11"}`},
		{"neoforge-21.11.45", `{"id":"neoforge-21.11.45","inheritsFrom":"1.21.11"}`},
	} {
		if err := os.MkdirAll(filepath.Join(mcDir, "versions", v.dir), 0755); err != nil {
			t.Fatalf("mkdir %s: %v", v.dir, err)
		}
		if err := os.WriteFile(filepath.Join(mcDir, "versions", v.dir, v.dir+".json"), []byte(v.json), 0644); err != nil {
			t.Fatalf("write %s: %v", v.dir, err)
		}
	}

	a, _ := newTestApp(t, tmp, "neoforge-pack", "1.21.11", struct{ Loader, Version string }{"neoforge", "21.11.45"})
	info := a.AnalyzeStorageVersions()
	for _, v := range info.Installed {
		if v.ID == "neoforge-21.11.45" && !v.Used {
			t.Errorf("NeoForge-профіль neoforge-21.11.45 має бути ВИКОРИСТАНИМ (збірка neo forge 1.21.11 встановлена)")
		}
		if v.ID == "1.21.11" && !v.Used {
			t.Errorf("корінь 1.21.11 має бути використаним")
		}
	}
}

// TestDeletePackAutoCleanup перевіряє РЕГРЕСІЮ, яку знайшло рев'ю:
// авто-очищення версій після DeletePack мало місце ДО видалення збірки з
// реєстру, тож версія досі рахувалась «використаною» і ніколи не
// видалялась. Тепер: після видалення збірки її версія (якщо її більше
// ніхто не використовує) зникає з minecraft/versions.
func TestDeletePackAutoCleanup(t *testing.T) {
	tmp := t.TempDir()
	mcDir := filepath.Join(tmp, "minecraft")
	if err := os.MkdirAll(filepath.Join(mcDir, "versions", "1.16.5"), 0755); err != nil {
		t.Fatalf("mkdir 1.16.5: %v", err)
	}
	if err := os.WriteFile(filepath.Join(mcDir, "versions", "1.16.5", "1.16.5.json"), []byte(`{"id":"1.16.5"}`), 0644); err != nil {
		t.Fatalf("write json: %v", err)
	}
	mkFile(t, filepath.Join(mcDir, "versions", "1.16.5", "1.16.5.jar"), 2500)

	a, cfg := newTestApp(t, tmp, "del-pack", "1.16.5")
	// Увімкнене авто-очищення (дефолт) + тека установки збірки, щоб
	// DeletePack мав що видаляти.
	mkdir := filepath.Join(cfg.GetSettings().InstanceDir, "del-pack")
	if err := os.MkdirAll(mkdir, 0755); err != nil {
		t.Fatalf("mkdir instance: %v", err)
	}

	if err := a.DeletePack("del-pack"); err != nil {
		t.Fatalf("DeletePack: %v", err)
	}
	// Версія 1.16.5 більше не потрібна жодній збірці — тека має зникнути.
	if _, err := os.Stat(filepath.Join(mcDir, "versions", "1.16.5")); !os.IsNotExist(err) {
		t.Errorf("версія 1.16.5 не видалена після DeletePack (регресія авто-очищення)")
	}
	// А версія, яку ніхто не видаляв — нічого не зламалось.
	if _, err := os.Stat(mcDir); err != nil {
		t.Errorf("minecraft dir зник: %v", err)
	}
}

// TestAnalyzeOrphanLibraries без встановлених збірок — всі бібліотеки зайві.
func TestAnalyzeOrphanLibrariesEmpty(t *testing.T) {
	tmp := t.TempDir()
	mcDir := filepath.Join(tmp, "minecraft")
	mkFile(t, filepath.Join(mcDir, "libraries", "net", "minecraft", "1.20.1", "a.jar"), 500)
	mkFile(t, filepath.Join(mcDir, "libraries", "org", "x", "y.jar"), 300)
	if err := os.MkdirAll(filepath.Join(mcDir, "versions"), 0755); err != nil {
		t.Fatalf("mkdir versions: %v", err)
	}

	a, _ := newTestApp(t, tmp, "orphan-pack", "9.9.9")
	out := a.AnalyzeOrphanLibraries()
	if !out.ScanDone {
		t.Fatalf("scan не завершився: %+v", out)
	}
	if out.OrphanFiles != 2 {
		t.Errorf("OrphanFiles = %d, want 2 (жодна збірка не використовує бібліотеки)", out.OrphanFiles)
	}
	if out.OrphanBytes != 800 {
		t.Errorf("OrphanBytes = %d, want 800", out.OrphanBytes)
	}
}

// TestCleanupOrphanLibraries видаляє зайві файли і лишає ті, що згадуються
// використовуваною версією (збірка 1.20.1 у реєстрі).
func TestCleanupOrphanLibraries(t *testing.T) {
	tmp := t.TempDir()
	mcDir := filepath.Join(tmp, "minecraft")
	if err := os.MkdirAll(filepath.Join(mcDir, "versions", "1.20.1"), 0755); err != nil {
		t.Fatalf("mkdir version: %v", err)
	}
	usedJSON := `{"id":"1.20.1","libraries":[{"name":"net.minecraft:1.20.1","downloads":{"artifact":{"path":"net/minecraft/1.20.1/1.20.1.jar"}}}]}`
	if err := os.WriteFile(filepath.Join(mcDir, "versions", "1.20.1", "1.20.1.json"), []byte(usedJSON), 0644); err != nil {
		t.Fatalf("write version json: %v", err)
	}
	mkFile(t, filepath.Join(mcDir, "versions", "1.20.1", "1.20.1.jar"), 2000)
	mkFile(t, filepath.Join(mcDir, "libraries", "net", "minecraft", "1.20.1", "1.20.1.jar"), 1000)
	mkFile(t, filepath.Join(mcDir, "libraries", "org", "leftover", "leftover.jar"), 700)

	a, _ := newTestApp(t, tmp, "used-pack", "1.20.1")
	out := a.CleanupOrphanLibraries()
	if out.RemovedFiles != 1 {
		t.Errorf("RemovedFiles = %d, want 1 (лише leftover.jar)", out.RemovedFiles)
	}
	if out.RemovedBytes != 700 {
		t.Errorf("RemovedBytes = %d, want 700", out.RemovedBytes)
	}
	if _, err := os.Stat(filepath.Join(mcDir, "libraries", "net", "minecraft", "1.20.1", "1.20.1.jar")); err != nil {
		t.Errorf("використовувана бібліотека видалена! %v", err)
	}
}

// TestAnalyzeStorageVersions з встановленою збіркою 1.20.1: версія
// позначається як використовувана, стороння — як зайва.
func TestAnalyzeStorageVersions(t *testing.T) {
	tmp := t.TempDir()
	mcDir := filepath.Join(tmp, "minecraft")
	if err := os.MkdirAll(filepath.Join(mcDir, "versions", "1.20.1"), 0755); err != nil {
		t.Fatalf("mkdir 1.20.1: %v", err)
	}
	if err := os.WriteFile(filepath.Join(mcDir, "versions", "1.20.1", "1.20.1.json"), []byte(`{"id":"1.20.1"}`), 0644); err != nil {
		t.Fatalf("write json: %v", err)
	}
	mkFile(t, filepath.Join(mcDir, "versions", "1.20.1", "1.20.1.jar"), 5000)
	// Стороння, ніким не використовувана версія.
	if err := os.MkdirAll(filepath.Join(mcDir, "versions", "1.16.5"), 0755); err != nil {
		t.Fatalf("mkdir 1.16.5: %v", err)
	}
	mkFile(t, filepath.Join(mcDir, "versions", "1.16.5", "1.16.5.jar"), 3000)

	a, _ := newTestApp(t, tmp, "used-pack", "1.20.1")
	info := a.AnalyzeStorageVersions()
	if len(info.Installed) != 2 {
		t.Fatalf("Installed = %d, want 2", len(info.Installed))
	}
	for _, v := range info.Installed {
		if v.ID == "1.20.1" && !v.Used {
			t.Errorf("версія 1.20.1 має бути використана (збірка в реєстрі)")
		}
		if v.ID == "1.16.5" && v.Used {
			t.Errorf("версія 1.16.5 не має бути використана")
		}
	}
	if len(info.Unused) != 1 || info.Unused[0] != "1.16.5" {
		t.Errorf("Unused = %+v, want [1.16.5]", info.Unused)
	}

	// Видаляємо зайву версію — тека має зникнути, а 1.20.1 лишитися.
	if err := a.CleanupUnusedVersions(info.Unused); err != nil {
		t.Fatalf("CleanupUnusedVersions: %v", err)
	}
	if _, err := os.Stat(filepath.Join(mcDir, "versions", "1.16.5")); !os.IsNotExist(err) {
		t.Errorf("тека 1.16.5 не видалена")
	}
	if _, err := os.Stat(filepath.Join(mcDir, "versions", "1.20.1")); err != nil {
		t.Errorf("тека 1.20.1 видалена помилково! %v", err)
	}
}
