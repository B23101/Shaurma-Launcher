package shaurmpack

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// Фікстура: збірка з модами (jar), конфігами (txt) і папкою, якої немає
// у виборі (saves). Використовується тестами вибору та швидкості експорту.
func packFixture(t *testing.T) string {
	t.Helper()
	gameDir := t.TempDir()
	mustDir := func(rel string) string {
		p := filepath.Join(gameDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(p, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		return p
	}
	mustFile := func(rel, content string) {
		p := filepath.Join(gameDir, filepath.FromSlash(rel))
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	mustDir("mods")
	mustDir("config")
	mustDir("saves")
	mustFile("mods/alpha.jar", "alpha-jar-bytes")
	mustFile("mods/beta.jar", "beta-jar-bytes")
	mustFile("config/video.txt", "renderDistance:12")
	mustFile("config/keybinds.txt", "jump:SPACE")
	mustFile("saves/level.dat", "world-bytes")
	return gameDir
}

func exportOptions(gameDir, outPath string) ExportOptions {
	return ExportOptions{
		GameDir:         gameDir,
		PackID:          "fixture",
		Name:            "Fixture",
		VersionID:       "1.0.0",
		OutPath:         outPath,
		LauncherName:    "Shaurma Launcher",
		LauncherVersion: "test",
	}
}

// Вибір користувача з модалки має ДІЯТИ: «вибрав лише mods» = у паку лише
// mods, а не вся тека збірки (це й був баг «він пакує всю папку»).
func TestExportHonoursSelection(t *testing.T) {
	gameDir := packFixture(t)
	outPath := filepath.Join(t.TempDir(), "sel.shaurmpack")

	opts := exportOptions(gameDir, outPath)
	// Модалка надсилає і папку, і файли всередині (як ExportCandidate) —
	// правило найглибшого шляху має покрити обидва випадки.
	opts.IncludePaths = []string{"mods", "mods/alpha.jar", "mods/beta.jar"}
	opts.ExcludePaths = nil

	if _, err := Export(opts); err != nil {
		t.Fatalf("Export: %v", err)
	}
	m, err := ReadManifest(outPath)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	for _, f := range m.Files {
		if f.Path != "mods/alpha.jar" && f.Path != "mods/beta.jar" {
			t.Errorf("у паку зайвий файл поза вибором: %s", f.Path)
		}
	}
	names := map[string]bool{}
	for _, f := range m.Files {
		names[f.Path] = true
	}
	if !names["mods/alpha.jar"] || !names["mods/beta.jar"] {
		t.Fatalf("вибрані моди не запаковано: %v", names)
	}
	if names["config/video.txt"] || names["saves/level.dat"] {
		t.Fatalf("невибрані файли потрапили в пак: %v", names)
	}
}

// Виключення окремої папки має перемагати включений батько (те саме дерево
// правил, що й у mrpack-експорті).
func TestExportExcludedWinsOverIncluded(t *testing.T) {
	gameDir := packFixture(t)
	outPath := filepath.Join(t.TempDir(), "excl.shaurmpack")

	opts := exportOptions(gameDir, outPath)
	opts.IncludePaths = []string{"config"}
	opts.ExcludePaths = []string{"config/keybinds.txt"}

	if _, err := Export(opts); err != nil {
		t.Fatalf("Export: %v", err)
	}
	m, _ := ReadManifest(outPath)
	if len(m.Files) != 1 || m.Files[0].Path != "config/video.txt" {
		t.Fatalf("очікували лише config/video.txt, отримали %+v", m.Files)
	}
}

// Без вибору (порожні IncludePaths) пакується вся збірка — стара поведінка
// для викликів, які не мають дерева вибору (напр. експорт теки у бандл).
func TestExportWithoutSelectionPacksEverything(t *testing.T) {
	gameDir := packFixture(t)
	outPath := filepath.Join(t.TempDir(), "all.shaurmpack")

	if _, err := Export(exportOptions(gameDir, outPath)); err != nil {
		t.Fatalf("Export: %v", err)
	}
	m, _ := ReadManifest(outPath)
	if len(m.Files) != 5 {
		t.Fatalf("без вибору очікували 5 файлів, отримали %d: %+v", len(m.Files), m.Files)
	}
}

// ШВИДКІСТЬ: онлайн-резолв не питає Modrinth/CurseForge про конфіги й інші
// дрібні файли (раніше — по запиту на кожен, через rate-limiter CF 4/с із
// ретраями по 300–700 мс: ~1 файл за секунду). Плюс мемо по хешу: дві
// однакові копії мода резолвляться один раз.
func TestExportSkipsOnlineResolveForNonContent(t *testing.T) {
	gameDir := packFixture(t)
	// Друга копія того самого мода (той самий вміст → той самий SHA-1).
	if err := os.WriteFile(filepath.Join(gameDir, "mods", "alpha-copy.jar"), []byte("alpha-jar-bytes"), 0644); err != nil {
		t.Fatalf("write copy: %v", err)
	}
	outPath := filepath.Join(t.TempDir(), "fast.shaurmpack")

	var modrinthCalls, cfCalls atomic.Int32
	opts := exportOptions(gameDir, outPath)
	opts.ResolveModrinth = func(sha1hex, fileName string, size int64) *ModrinthSource {
		modrinthCalls.Add(1)
		return nil
	}
	opts.ResolveCurseForge = func(sha1hex, fileName string, size int64) *CurseForgeSource {
		cfCalls.Add(1)
		return nil
	}

	start := time.Now()
	if _, err := Export(opts); err != nil {
		t.Fatalf("Export: %v", err)
	}
	elapsed := time.Since(start)
	t.Logf("експорт фікстури: %v (Modrinth-запитів %d, CurseForge %d)", elapsed, modrinthCalls.Load(), cfCalls.Load())

	// mods: alpha.jar, beta.jar, alpha-copy.jar (останні — один хеш).
	// Резолв потрібен лише для jar-ів: 2 унікальні хеші.
	if got := modrinthCalls.Load(); got != 2 {
		t.Errorf("Modrinth має спитати лише про 2 унікальні jar-и, а спитав %d разів", got)
	}
	if got := cfCalls.Load(); got != 2 {
		t.Errorf("CurseForge має спитати лише про 2 унікальні jar-и, а спитав %d разів", got)
	}
}
