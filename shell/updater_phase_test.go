package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestManifestHashChangesOnUpdate — перевірка детектора «оновлення реально
// встановилось»: маніфест апдейтер переписує при кожному успішному
// застосуванні (install.Apply), тож хеш ДО і ПІСЛЯ циклу оновлення має
// відрізнятись. transitionToBackend покладається на цю різницю, щоб
// вирішити, чи перезапускати процес для підхоплення нової backend.dll.
func TestManifestHashChangesOnUpdate(t *testing.T) {
	dir := t.TempDir()

	// Файлу ще немає — хеш порожній (перший запуск / bootstrap не закінчився).
	if h := manifestHashOf(dir); h != "" {
		t.Fatalf("очікували порожній хеш без файлу, отримали %q", h)
	}

	// Стара версія на місці.
	old := []byte(`{"version":"2.0.0","components":[]}`)
	if err := os.WriteFile(filepath.Join(dir, "launcher-version.json"), old, 0o644); err != nil {
		t.Fatal(err)
	}
	before := manifestHashOf(dir)
	if before == "" {
		t.Fatal("хеш наявного маніфесту не має бути порожнім")
	}

	// Апдейтер переписав маніфест новою версією — хеш має змінитись.
	next := []byte(`{"version":"2.1.0","components":[{"path":"backend.dll","sha256":"abc"}]}`)
	if err := os.WriteFile(filepath.Join(dir, "launcher-version.json"), next, 0o644); err != nil {
		t.Fatal(err)
	}
	after := manifestHashOf(dir)

	if after == before {
		t.Fatal("хеш маніфесту має змінитись після оновлення версії")
	}

	// Той самий вміст — той самий хеш (стабільність детектора).
	again := manifestHashOf(dir)
	if again != after {
		t.Fatal("хеш одного й того ж файлу має бути стабільним")
	}
}

// TestManifestHashNoFalsePositiveOnSameContent — детектор не має спрацьовувати
// (перезапускати процес), коли маніфест НЕ змінювався: ручне оновлення, яке
// виявило «актуальна версія», не чіпає файл, тож перезапуск не потрібен.
func TestManifestHashNoFalsePositiveOnSameContent(t *testing.T) {
	dir := t.TempDir()
	content := []byte(`{"version":"2.0.0","components":[]}`)
	if err := os.WriteFile(filepath.Join(dir, "launcher-version.json"), content, 0o644); err != nil {
		t.Fatal(err)
	}

	h1 := manifestHashOf(dir)
	h2 := manifestHashOf(dir) // нічого не змінилось
	if h1 != h2 {
		t.Fatalf("незмінений маніфест має давати той самий хеш: %q != %q", h1, h2)
	}
}
