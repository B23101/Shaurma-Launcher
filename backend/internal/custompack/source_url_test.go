package custompack

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"testing"
)

// makeTestMrpack створює мінімальний mrpack-архів без модів (тільки
// modrinth.index.json) — імпорт не потребує мережі.
func makeTestMrpack(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.mrpack")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	index := `{"formatVersion":1,"game":"minecraft","versionId":"1.20.1","name":"` + name + `","files":[],"dependencies":{"minecraft":"1.20.1"}}`
	w, err := zw.Create("modrinth.index.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(index)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return path
}

// TestImportArchiveKeepsSourceArchiveURL — регресійний тест головного бага
// «Завантажити не працює»: після імпорту інтернет-збірки фінальний Pack
// має зберегти SourceArchiveURL (пряме посилання на архів), щоб картка в
// «Моїх збірках» не втрачала здатність завантажуватись/оновлюватись.
// До фіксу ImportParams не мав поля SourceArchiveURL, тож фінальний запис
// його губив (порожній рядок) — кнопка «Завантажити» ставала мертвою.
func TestImportArchiveKeepsSourceArchiveURL(t *testing.T) {
	archivePath := makeTestMrpack(t, "TestPack")
	detected, err := DetectArchive(archivePath)
	if err != nil {
		t.Fatal(err)
	}

	storeDir := t.TempDir()
	instanceDir := t.TempDir()
	store := NewStore(storeDir)

	const srcURL = "https://cdn.modrinth.com/data/abc/versions/1/testpack-1.0.mrpack"
	pack, err := ImportArchive(
		context.Background(), store, instanceDir, archivePath, detected,
		ImportParams{
			Name:             "TestPack",
			Source:           "modrinth",
			SourceProjectID:  "abc",
			SourceVersionID:  "ver1",
			SourceArchiveURL: srcURL,
		},
		4, func(ImportProgress) {},
	)
	if err != nil {
		t.Fatal(err)
	}
	if pack.SourceArchiveURL != srcURL {
		t.Errorf("SourceArchiveURL після імпорту = %q, очікувалось %q", pack.SourceArchiveURL, srcURL)
	}
	if pack.Source != "modrinth" {
		t.Errorf("Source = %q, очікувалось modrinth", pack.Source)
	}
	if pack.SourceProjectID != "abc" {
		t.Errorf("SourceProjectID = %q, очікувалось abc", pack.SourceProjectID)
	}
	if pack.SourceVersionID != "ver1" {
		t.Errorf("SourceVersionID = %q, очікувалось ver1", pack.SourceVersionID)
	}

	// Запис має бути персистентним: перечитати зі сховища (той самий шлях,
	// що фронтенд бачить після перезапуску).
	reloaded, ok := store.Get(pack.ID)
	if !ok {
		t.Fatalf("pack %s не знайдено у сховищі після перезавантаження", pack.ID)
	}
	if reloaded.SourceArchiveURL != srcURL {
		t.Errorf("SourceArchiveURL після перезавантаження = %q, очікувалось %q", reloaded.SourceArchiveURL, srcURL)
	}
}
