package shaurmpack

import (
	"archive/zip"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func sha1hex(s string) string {
	h := sha1.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

func zipNames(t *testing.T, path string) map[string]*zip.File {
	t.Helper()
	r, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("zip: %v", err)
	}
	t.Cleanup(func() { r.Close() })
	m := map[string]*zip.File{}
	for _, f := range r.File {
		m[f.Name] = f
	}
	return m
}

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, c := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(c), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

// Файл з джерелом → не в ZIP; без джерела → bundled/; config → overrides/;
// заблокований CF → bundled/ без CF-запису; лише CF (робочий) → джерело.
func TestExportClassification(t *testing.T) {
	gameDir := t.TempDir()
	writeFiles(t, gameDir, map[string]string{
		"mods/on-modrinth.jar": "mr",
		"mods/on-cf.jar":       "cf",
		"mods/blocked.jar":     "blocked",
		"mods/unknown.jar":     "unknown",
		"mods/sub/nested.jar":  "nested",
		"resourcepacks/rp.zip": "rp",
		"config/a.toml":        "cfg",
		"datapacks/x.json":     "{}",
	})
	out := filepath.Join(t.TempDir(), "c.shaurmpack")

	var mrCalls, cfCalls atomic.Int32
	var gotLookups []CFLookup
	res, err := Export(ExportOptions{
		GameDir: gameDir, PackID: "c", Name: "C", OutPath: out,
		ResolveModrinthBatch: func(ctx context.Context, sha1s []string) map[string]*ModrinthSource {
			mrCalls.Add(1)
			return map[string]*ModrinthSource{
				sha1hex("mr"):     {ProjectID: "p", VersionID: "v", FileName: "mr.jar", DownloadURL: "https://m/mr.jar"},
				sha1hex("nested"): {ProjectID: "p2", VersionID: "v2", FileName: "n.jar", DownloadURL: "https://m/n.jar"},
			}
		},
		ResolveCurseForgeBatch: func(ctx context.Context, files []CFLookup) map[string]*CurseForgeSource {
			cfCalls.Add(1)
			gotLookups = files
			return map[string]*CurseForgeSource{
				sha1hex("mr"):      {ProjectID: 1, FileID: 2, FileName: "mr.jar"}, // поряд з Modrinth: без URL
				sha1hex("cf"):      {ProjectID: 3, FileID: 4, FileName: "cf.jar", DownloadURL: "https://cf/cf.jar"},
				sha1hex("blocked"): {ProjectID: 5, FileID: 6, FileName: "b.jar", Blocked: true},
				sha1hex("rp"):      {ProjectID: 7, FileID: 8, FileName: "rp.zip", DownloadURL: "https://cf/rp.zip"},
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if mrCalls.Load() != 1 || cfCalls.Load() != 1 {
		t.Errorf("batch мав викликатись по 1 разу: mr=%d cf=%d", mrCalls.Load(), cfCalls.Load())
	}
	// 6 резолвабельних (5 jar + 1 zip), конфіги/датапаки не шукаємо.
	if len(gotLookups) != 6 {
		t.Errorf("CF lookups = %d, want 6", len(gotLookups))
	}
	for _, l := range gotLookups {
		if l.Fingerprint == "" || l.RelPath == "" {
			t.Errorf("lookup без fingerprint/relPath: %+v", l)
		}
		if l.SHA1 == sha1hex("mr") && l.NeedDetails {
			t.Error("для файлу з Modrinth NeedDetails має бути false")
		}
		if l.SHA1 == sha1hex("blocked") && !l.NeedDetails {
			t.Error("для файлу без Modrinth NeedDetails має бути true")
		}
	}

	z := zipNames(t, out)
	for _, name := range []string{"mods/on-modrinth.jar", "mods/on-cf.jar", "mods/sub/nested.jar", "resourcepacks/rp.zip"} {
		if z["overrides/"+name] != nil || z["bundled/"+name] != nil {
			t.Errorf("%s має джерело, але лежить в архіві", name)
		}
	}
	for _, name := range []string{"mods/blocked.jar", "mods/unknown.jar"} {
		if z["bundled/"+name] == nil {
			t.Errorf("%s має бути в bundled/", name)
		}
	}
	for _, name := range []string{"config/a.toml", "datapacks/x.json"} {
		if z["overrides/"+name] == nil {
			t.Errorf("%s має бути в overrides/", name)
		}
	}
	if z["bundled/mods/unknown.jar"] != nil && z["bundled/mods/unknown.jar"].Method != zip.Store {
		t.Error("jar має писатись без стиснення (Store)")
	}
	if z["overrides/config/a.toml"].Method != zip.Deflate {
		t.Error("текст має стискатись (Deflate)")
	}

	m, err := ReadManifest(out)
	if err != nil {
		t.Fatal(err)
	}
	if res.ResolvedFiles != 4 || res.BundledFiles != 2 || m.OverrideCount != 2 || m.ResolvedCount != 4 {
		t.Errorf("лічильники: resolved=%d bundled=%d overrides=%d m.resolved=%d", res.ResolvedFiles, res.BundledFiles, m.OverrideCount, m.ResolvedCount)
	}
	paths := make([]string, len(m.Files))
	by := map[string]PackFile{}
	for i, f := range m.Files {
		paths[i] = f.Path
		by[f.Path] = f
	}
	if !sort.StringsAreSorted(paths) {
		t.Errorf("manifest.Files не відсортовано: %v", paths)
	}
	if f := by["mods/on-modrinth.jar"]; f.Modrinth == nil || f.CurseForge == nil {
		t.Errorf("обидва джерела мали зберегтись: %+v", f)
	}
	if f := by["mods/on-cf.jar"]; f.Modrinth != nil || !workingCurseForge(f.CurseForge) {
		t.Errorf("on-cf: очікували лише робочий CF: %+v", f)
	}
	if f := by["mods/blocked.jar"]; !f.Bundled || f.CurseForge != nil {
		t.Errorf("blocked: Bundled=true і без CF: %+v", f)
	}
	if f := by["config/a.toml"]; f.Bundled || f.HasWorkingSource() {
		t.Errorf("config: ні bundled, ні джерело: %+v", f)
	}
}

// Той самий вміст у двох файлах → один унікальний хеш у batch.
func TestExportBatchGetsUniqueHashes(t *testing.T) {
	gameDir := t.TempDir()
	writeFiles(t, gameDir, map[string]string{"mods/a.jar": "same", "mods/b.jar": "same", "mods/c.jar": "other"})
	var got []string
	_, err := Export(ExportOptions{
		GameDir: gameDir, Name: "U", OutPath: filepath.Join(t.TempDir(), "u.shaurmpack"),
		ResolveModrinthBatch: func(ctx context.Context, s []string) map[string]*ModrinthSource {
			got = append([]string(nil), s...)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("унікальних хешів 2, отримано %d: %v", len(got), got)
	}
}

func TestExportCancelled(t *testing.T) {
	gameDir := t.TempDir()
	writeFiles(t, gameDir, map[string]string{"mods/a.jar": "a"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Export(ExportOptions{GameDir: gameDir, Name: "X", OutPath: filepath.Join(t.TempDir(), "x.shaurmpack"), Context: ctx})
	if err == nil {
		t.Fatal("очікували помилку скасування")
	}
}

// Хешування йде паралельно (>1 воркер одночасно) — через SHA1Of-хук.
func TestExportHashesInParallel(t *testing.T) {
	gameDir := t.TempDir()
	files := map[string]string{}
	for i := 0; i < 40; i++ {
		files[fmt.Sprintf("mods/m%02d.jar", i)] = fmt.Sprintf("content-%d", i)
	}
	writeFiles(t, gameDir, files)
	var cur, peak atomic.Int32
	_, err := Export(ExportOptions{
		GameDir: gameDir, Name: "P", OutPath: filepath.Join(t.TempDir(), "p.shaurmpack"),
		SHA1Of: func(abs string) (string, error) {
			n := cur.Add(1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			time.Sleep(5 * time.Millisecond)
			cur.Add(-1)
			return "", nil // порожньо → порахує сам
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if peak.Load() < 2 {
		t.Errorf("хешування не паралельне (peak=%d)", peak.Load())
	}
	if peak.Load() > workerCount {
		t.Errorf("перевищено ліміт воркерів: %d", peak.Load())
	}
}

// Нове формат: Import качає файли з джерелом паралельно і не чекає їх в архіві.
func TestImportDownloadsSourcedFilesInParallel(t *testing.T) {
	gameDir := t.TempDir()
	files := map[string]string{"config/a.toml": "cfg"}
	for i := 0; i < 20; i++ {
		files[fmt.Sprintf("mods/m%02d.jar", i)] = fmt.Sprintf("jar-%d", i)
	}
	writeFiles(t, gameDir, files)
	out := filepath.Join(t.TempDir(), "d.shaurmpack")
	if _, err := Export(ExportOptions{
		GameDir: gameDir, Name: "D", OutPath: out,
		ResolveModrinthBatch: func(ctx context.Context, s []string) map[string]*ModrinthSource {
			m := map[string]*ModrinthSource{}
			for _, h := range s {
				m[h] = &ModrinthSource{ProjectID: "p", VersionID: "v", FileName: h, DownloadURL: "https://m/" + h}
			}
			return m
		},
	}); err != nil {
		t.Fatal(err)
	}
	if z := zipNames(t, out); len(z) > 5 { // manifest + overrides/config/a.toml (+ каталоги)
		for n := range z {
			if filepath.Dir(n) == "overrides/mods" || filepath.Dir(n) == "bundled/mods" {
				t.Errorf("мод в архіві: %s", n)
			}
		}
	}

	var cur, peak atomic.Int32
	var mu sync.Mutex
	downloaded := map[string]bool{}
	target := t.TempDir()
	res, err := Import(ImportOptions{
		ShaurmPackPath: out, TargetGameDir: target,
		ResolveDownload: func(dest string, pf PackFile) error {
			n := cur.Add(1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			time.Sleep(10 * time.Millisecond)
			cur.Add(-1)
			mu.Lock()
			downloaded[pf.Path] = true
			mu.Unlock()
			return os.WriteFile(dest, []byte("dl"), 0644)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(downloaded) != 20 || res.FilesLinked != 20 {
		t.Errorf("downloaded=%d linked=%d, want 20", len(downloaded), res.FilesLinked)
	}
	if peak.Load() < 2 || peak.Load() > workerCount {
		t.Errorf("паралельність качання: peak=%d (очікувано 2..%d)", peak.Load(), workerCount)
	}
	if b, err := os.ReadFile(filepath.Join(target, "config", "a.toml")); err != nil || string(b) != "cfg" {
		t.Errorf("config з overrides/ не розпаковано: %v", err)
	}
}

// Старий формат: файл з джерелом дублюється в overrides/. З ResolveDownload
// дубль НЕ розпаковується, але служить фолбеком при провалі завантаження;
// без ResolveDownload — розпаковується (стара поведінка).
func TestImportOldFormatCompat(t *testing.T) {
	old := filepath.Join(t.TempDir(), "old.shaurmpack")
	mf := Manifest{
		Version: 1, PackID: "old", Name: "Old", VersionID: "1.0.0",
		Files: []PackFile{
			{Path: "mods/ok.jar", Hash: "h1", FileType: FileTypeMod, Modrinth: &ModrinthSource{DownloadURL: "https://m/ok"}},
			{Path: "mods/fail.jar", Hash: "h2", FileType: FileTypeMod, Modrinth: &ModrinthSource{DownloadURL: "https://m/fail"}},
			{Path: "config/c.txt", Hash: "h3", FileType: FileTypeConfig, Bundled: true},
		},
	}
	f, _ := os.Create(old)
	zw := zip.NewWriter(f)
	add := func(name, c string) {
		w, _ := zw.Create(name)
		w.Write([]byte(c))
	}
	data, _ := json.Marshal(mf)
	add("manifest.json", string(data))
	add("overrides/mods/ok.jar", "from-archive-ok")
	add("overrides/mods/fail.jar", "from-archive-fail")
	add("bundled/config/c.txt", "cfg")
	zw.Close()
	f.Close()

	// З резолвером: ok.jar качається (і не розпаковується з архіву), fail.jar → фолбек з overrides/.
	target := t.TempDir()
	if _, err := Import(ImportOptions{
		ShaurmPackPath: old, TargetGameDir: target,
		ResolveDownload: func(dest string, pf PackFile) error {
			if pf.Path == "mods/fail.jar" {
				return fmt.Errorf("мережа")
			}
			return os.WriteFile(dest, []byte("downloaded"), 0644)
		},
	}); err != nil {
		t.Fatal(err)
	}
	read := func(rel string) string {
		b, _ := os.ReadFile(filepath.Join(target, filepath.FromSlash(rel)))
		return string(b)
	}
	if read("mods/ok.jar") != "downloaded" {
		t.Errorf("ok.jar = %q, want downloaded", read("mods/ok.jar"))
	}
	if read("mods/fail.jar") != "from-archive-fail" {
		t.Errorf("fail.jar = %q, want фолбек з архіву", read("mods/fail.jar"))
	}
	if read("config/c.txt") != "cfg" {
		t.Error("bundled config не розпаковано")
	}

	// Без резолвера: усе, що є в архіві, розпаковується.
	target2 := t.TempDir()
	if _, err := Import(ImportOptions{ShaurmPackPath: old, TargetGameDir: target2}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(target2, "mods", "ok.jar")); string(b) != "from-archive-ok" {
		t.Errorf("без резолвера ok.jar = %q", b)
	}
}

// Бандл: файли з джерелом — лише у files.json, імпорт їх докачує.
func TestBundleSourcedFilesNotInZipAndRedownloaded(t *testing.T) {
	gameDir := t.TempDir()
	writeFiles(t, gameDir, map[string]string{"mods/a.jar": "aaa", "mods/b.jar": "bbb", "config/x.txt": "x"})
	out := filepath.Join(t.TempDir(), "b.shaurmbundle")
	_, err := ExportBundle(BundleExportOptions{
		Packs:   []BundlePackExport{{PackID: "p1", PackData: []byte(`{"id":"p1","name":"P1"}`), GameDir: gameDir}},
		OutPath: out,
		ResolveModrinthBatch: func(ctx context.Context, s []string) map[string]*ModrinthSource {
			return map[string]*ModrinthSource{
				sha1hex("aaa"): {ProjectID: "p", VersionID: "v", FileName: "a.jar", DownloadURL: "https://m/a.jar"},
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	z := zipNames(t, out)
	if z["packs/p1/overrides/mods/a.jar"] != nil || z["packs/p1/bundled/mods/a.jar"] != nil {
		t.Error("a.jar має джерело — в архіві його бути не повинно")
	}
	if z["packs/p1/bundled/mods/b.jar"] == nil {
		t.Error("b.jar без джерела → bundled/")
	}
	if z["packs/p1/overrides/config/x.txt"] == nil {
		t.Error("config → overrides/")
	}
	if z["packs/p1/files.json"] == nil {
		t.Fatal("files.json відсутній")
	}

	target := t.TempDir()
	res, err := ImportBundle(BundleImportOptions{
		ShaurmBundlePath: out, TargetBaseDir: target,
		ResolveDownload: func(dest string, pf PackFile) error { return os.WriteFile(dest, []byte("dl:"+pf.Path), 0644) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Packs[0].FilesLinked != 1 || res.Packs[0].FilesMissing != 0 {
		t.Errorf("linked=%d missing=%d", res.Packs[0].FilesLinked, res.Packs[0].FilesMissing)
	}
	if b, _ := os.ReadFile(filepath.Join(target, "p1", ".minecraft", "mods", "a.jar")); string(b) != "dl:mods/a.jar" {
		t.Errorf("a.jar не докачано: %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(target, "p1", ".minecraft", "mods", "b.jar")); string(b) != "bbb" {
		t.Errorf("b.jar з bundled/: %q", b)
	}
}

func exportSourcedPack(t *testing.T, nMods int) string {
	t.Helper()
	gameDir := t.TempDir()
	files := map[string]string{"config/a.toml": "cfg"}
	for i := 0; i < nMods; i++ {
		files[fmt.Sprintf("mods/m%02d.jar", i)] = fmt.Sprintf("jar-%d", i)
	}
	writeFiles(t, gameDir, files)
	out := filepath.Join(t.TempDir(), "p.shaurmpack")
	if _, err := Export(ExportOptions{
		GameDir: gameDir, Name: "P", OutPath: out,
		ResolveModrinthBatch: func(ctx context.Context, s []string) map[string]*ModrinthSource {
			m := map[string]*ModrinthSource{}
			for _, h := range s {
				m[h] = &ModrinthSource{ProjectID: "p", VersionID: "v", FileName: h, DownloadURL: "https://m/" + h}
			}
			return m
		},
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

// Прогрес імпорту: перша подія 0/total, лічильник зростає до total, остання — finalizing.
func TestImportProgress(t *testing.T) {
	out := exportSourcedPack(t, 5)
	var mu sync.Mutex
	var evs []ImportProgress
	_, err := Import(ImportOptions{
		ShaurmPackPath: out, TargetGameDir: t.TempDir(),
		ResolveDownload: func(dest string, pf PackFile) error { return os.WriteFile(dest, []byte("x"), 0644) },
		Progress: func(p ImportProgress) {
			mu.Lock()
			evs = append(evs, p)
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) < 3 {
		t.Fatalf("мало подій: %+v", evs)
	}
	total := evs[0].FilesTotal
	if total != 6 { // 1 config + 5 модів
		t.Errorf("total=%d, want 6", total)
	}
	if evs[0].FilesDone != 0 {
		t.Errorf("перша подія має бути 0/total: %+v", evs[0])
	}
	prev := 0
	for _, e := range evs[1 : len(evs)-1] {
		if e.FilesDone < prev || e.FilesTotal != total {
			t.Errorf("лічильник не монотонний: %+v (prev %d)", e, prev)
		}
		prev = e.FilesDone
	}
	last := evs[len(evs)-1]
	if last.Stage != ImportStageFinalizing || last.FilesDone != total {
		t.Errorf("остання подія: %+v", last)
	}
}

func TestImportCancelled(t *testing.T) {
	out := exportSourcedPack(t, 3)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Import(ImportOptions{ShaurmPackPath: out, TargetGameDir: t.TempDir(), Context: ctx,
		ResolveDownload: func(dest string, pf PackFile) error { return os.WriteFile(dest, []byte("x"), 0644) }})
	if err == nil {
		t.Fatal("очікували помилку скасування")
	}
}

// SkipExisting: цілий файл (розмір+SHA-1) не качається вдруге.
func TestImportSkipExisting(t *testing.T) {
	gameDir := t.TempDir()
	writeFiles(t, gameDir, map[string]string{"mods/keep.jar": "keep-bytes", "mods/new.jar": "new-bytes"})
	out := filepath.Join(t.TempDir(), "s.shaurmpack")
	if _, err := Export(ExportOptions{GameDir: gameDir, Name: "S", OutPath: out,
		ResolveModrinthBatch: func(ctx context.Context, s []string) map[string]*ModrinthSource {
			m := map[string]*ModrinthSource{}
			for _, h := range s {
				m[h] = &ModrinthSource{DownloadURL: "https://m/" + h}
			}
			return m
		}}); err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	writeFiles(t, target, map[string]string{"mods/keep.jar": "keep-bytes"}) // уже завантажений
	var calls atomic.Int32
	res, err := Import(ImportOptions{ShaurmPackPath: out, TargetGameDir: target, SkipExisting: true,
		ResolveDownload: func(dest string, pf PackFile) error {
			calls.Add(1)
			return os.WriteFile(dest, []byte("new-bytes"), 0644)
		}})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || res.FilesLinked != 2 {
		t.Errorf("calls=%d linked=%d, want 1 та 2", calls.Load(), res.FilesLinked)
	}
}
