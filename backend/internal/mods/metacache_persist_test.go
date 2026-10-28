package mods

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeFabricJar(t *testing.T, path, id, name string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, _ := zw.Create("fabric.mod.json")
	w.Write([]byte(`{"schemaVersion":1,"id":"` + id + `","name":"` + name + `","version":"1.0.0"}`))
	zw.Close()
	f.Close()
}

// Після перезапуску (кеш у пам'яті порожній) метадані незміненого jar
// мають братися з файлу кешу на диску, а не з повторного розбору jar.
func TestMetaCacheSurvivesRestart(t *testing.T) {
	modsDir := t.TempDir()
	cacheDir := t.TempDir()
	jar := filepath.Join(modsDir, "demo.jar")
	writeFabricJar(t, jar, "demo", "Demo Mod")

	m := NewManager()
	m.SetCacheDir(cacheDir)
	mods, err := m.ScanModsFull(modsDir)
	if err != nil || len(mods) != 1 || mods[0].Name != "Demo Mod" {
		t.Fatalf("first scan: %v %+v", err, mods)
	}
	if _, err := os.Stat(metaCachePath(cacheDir, modsDir)); err != nil {
		t.Fatalf("meta cache file not written: %v", err)
	}

	// «Перезапуск»: чистимо пам'ять, а jar підміняємо іншим вмістом із тим
	// самим розміром і mtime — якщо скан візьме дані з диска, назва лишиться
	// старою (тобто jar не відкривався).
	info, _ := os.Stat(jar)
	jarMetaCacheMu.Lock()
	jarMetaCache = map[jarCacheKey]*jarMeta{}
	jarMetaCacheMu.Unlock()
	resetDiskMetaCacheLoadedFlags()
	writeFabricJar(t, jar, "demo", "Xemo Mod")
	if st, _ := os.Stat(jar); st.Size() != info.Size() {
		t.Skip("replacement jar size differs; cannot simulate unchanged file")
	}
	os.Chtimes(jar, time.Now(), info.ModTime())

	m2 := NewManager()
	m2.SetCacheDir(cacheDir)
	mods, err = m2.ScanModsFull(modsDir)
	if err != nil || len(mods) != 1 {
		t.Fatalf("second scan: %v %+v", err, mods)
	}
	if mods[0].Name != "Demo Mod" {
		t.Fatalf("expected cached name, got %q (jar was re-parsed)", mods[0].Name)
	}
}
