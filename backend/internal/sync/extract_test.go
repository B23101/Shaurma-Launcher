package sync

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

// makeArchiveZips створює реальний .zip архів у .override-archives/ збірки —
// саме так його кладе downloadTask перед розпаковкою.
func makeArchiveZips(t *testing.T, installDir, archiveKey string, files map[string]string) string {
	t.Helper()
	archivesDir := filepath.Join(installDir, ".override-archives")
	if err := os.MkdirAll(archivesDir, 0755); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(archivesDir, sanitizeName(archiveKey)+".zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return zipPath
}

func newRunnerForTest(installDir string) *Runner {
	return &Runner{
		InstallDir:   installDir,
		partialPaths: map[string]bool{},
	}
}

// TestExtractOverrideFolderRemovesZip — РЕГРЕСІЙНИЙ ТЕСТ основного бага:
// після успішної розпаковки архів-файл має бути видалений з
// .override-archives/. Раніше zip лишався назавжди (видалявся лише на
// успішному шляху через os.RemoveAll всієї теки наприкінці Run — а при
// помилці розпаковки або повторних запусках сміття копилось).
func TestExtractOverrideFolderRemovesZip(t *testing.T) {
	dir := t.TempDir()
	// Спершу створюємо .override-archives/my-config.zip
	zipPath := makeArchiveZips(t, dir, "configs/my-config.zip", map[string]string{
		"options.txt": "width=800",
		"servers.txt": "localhost",
	})

	r := newRunnerForTest(dir)
	err := r.extractOverrideFolder(Task{
		Kind:        KindOverrideFolder,
		DestRelPath: "config",
		ManifestKey: "configs/my-config.zip",
	})
	if err != nil {
		t.Fatalf("розпаковка не мала впасти: %v", err)
	}

	// 1. Zip ВИДАЛЕНО (основний фікс).
	if _, err := os.Stat(zipPath); !os.IsNotExist(err) {
		t.Errorf("zip після розпаковки має бути видалений, але файл досі існує: %s", zipPath)
	}

	// 2. Вміст розпаковано за призначенням.
	for _, f := range []string{"options.txt", "servers.txt"} {
		if _, err := os.Stat(filepath.Join(dir, "config", f)); err != nil {
			t.Errorf("розпакований файл %s відсутній: %v", f, err)
		}
	}
}

// TestExtractOverrideFolderKeepsZipOnError — помилка розпаковки (битий zip)
// тепер ЛИШАЄ файл на диску: архів може знадобитись наступному запуску, і
// downloadTask пропустить качку за хешем (resume без повторного качання).
// Раніше defer os.Remove стирав zip навіть при помилці, тож наступний запуск
// був змушений КАЧАТИ архів знову, хоч він уже лежав на диску.
func TestExtractOverrideFolderKeepsZipOnError(t *testing.T) {
	dir := t.TempDir()
	archivesDir := filepath.Join(dir, ".override-archives")
	if err := os.MkdirAll(archivesDir, 0755); err != nil {
		t.Fatal(err)
	}
	badZip := filepath.Join(archivesDir, "broken.zip")
	// Записуємо НЕ-валидний zip (випадковий вміст) — zip.OpenReader впаде.
	if err := os.WriteFile(badZip, []byte("this is definitely not a zip file"), 0644); err != nil {
		t.Fatal(err)
	}

	r := newRunnerForTest(dir)
	err := r.extractOverrideFolder(Task{
		Kind:        KindOverrideFolder,
		DestRelPath: "config",
		ManifestKey: "broken",
	})
	if err == nil {
		t.Fatal("битий zip має дати помилку розпаковки")
	}

	if _, statErr := os.Stat(badZip); os.IsNotExist(statErr) {
		t.Errorf("битий zip має ЛИШИТИСЬ при помилці розпаковки (для resume), але видалений: %s", badZip)
	}
}

// TestExtractOverrideFolderSkipsAlreadyExtracted — «пауза→продовження не
// повертає до розпакування»: якщо всі записи архіву вже на диску з тим самим
// розміром, повторний виклик НЕ стирає destDir і НЕ розпаковує заново — лише
// прибирає архів. Раніше повторна розпаковка робила RemoveAll + все спочатку,
// затираючи навіть зміни користувача.
func TestExtractOverrideFolderSkipsAlreadyExtracted(t *testing.T) {
	dir := t.TempDir()
	zipPath := makeArchiveZips(t, dir, "cfg", map[string]string{"options.txt": "hello"})

	r := newRunnerForTest(dir)
	task := Task{Kind: KindOverrideFolder, DestRelPath: "config", ManifestKey: "cfg"}
	if err := r.extractOverrideFolder(task); err != nil {
		t.Fatal(err)
	}
	destFile := filepath.Join(dir, "config", "options.txt")
	if _, err := os.Stat(destFile); err != nil {
		t.Fatalf("перша розпаковка не створила файл: %v", err)
	}
	if _, err := os.Stat(zipPath); !os.IsNotExist(err) {
		t.Fatal("після успішної розпаковки архів мав бути видалений")
	}

	// «Resume»: архів з'являється знову (перезапуск після паузи до запису
	// стану). Вміст файлу той самий за розміром (5 байт) — розпаковка має
	// бути пропущена, і навіть ЗМІНЕНИЙ вміст того ж розміру не затирається.
	makeArchiveZips(t, dir, "cfg", map[string]string{"options.txt": "other"})
	if err := r.extractOverrideFolder(task); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(destFile)
	if string(got) != "hello" {
		t.Errorf("уже розпакована тека НЕ мала перезаписуватись при resume (skip за розміром), файл: %q", got)
	}
	if _, err := os.Stat(zipPath); !os.IsNotExist(err) {
		t.Errorf("архів мав бути прибраний після пропуску розпаковки")
	}
}
