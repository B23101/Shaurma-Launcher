package export

import (
	"archive/zip"
	"crypto/sha1"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sha512Hex(s string) string {
	h := sha512.Sum512([]byte(s))
	return hex.EncodeToString(h[:])
}

func sha1Hex(s string) string {
	h := sha1.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

// writeTestPack створює тимчасову теку збірки з типовими файлами.
func writeTestPack(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"mods/create.jar":               "create-jar-bytes",
		"mods/jei.jar.disabled":         "jei-disabled",
		"config/example.toml":           "config-bytes",
		"resourcepacks/custom.zip":      "pack-bytes",
		"shaderpacks/bsl.zip":           "shader-bytes",
		"saves/world1/level.dat":        "level-bytes",
		"options.txt":                   "options-bytes",
		"logs/latest.log":               "should-not-export",
		".fabric/remappedJars/x.jar":    "should-not-export",
		"mods/.DS_Store":                "should-not-export",
		"mods/.connector/connector.jar": "should-not-export",
	}
	for rel, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestScanCandidates(t *testing.T) {
	dir := writeTestPack(t)
	cands, err := ScanCandidates(dir)
	if err != nil {
		t.Fatal(err)
	}
	// logs/, .fabric/, .DS_Store, .connector не мають з'явитись.
	for _, c := range cands {
		if strings.Contains(c.Path, "logs") || strings.Contains(c.Path, ".fabric") ||
			strings.Contains(c.Path, ".DS_Store") || strings.Contains(c.Path, ".connector") {
			t.Errorf("never-exportable шлях потрапив у кандидатів: %s", c.Path)
		}
	}
	// Дефолтний вибір: mods/config/resourcepacks/shaderpacks — так, saves — ні.
	byPath := map[string]ExportCandidate{}
	for _, c := range cands {
		byPath[c.Path] = c
	}
	if !byPath["mods"].DefaultSelected {
		t.Error("mods має бути defaultSelected")
	}
	if byPath["saves"].DefaultSelected {
		t.Error("saves не має бути defaultSelected")
	}
}

// TestScanCandidatesSavesShallow — регресія: saves/ не сканується рекурсивно
// (велика колекція світів раніше robила ScanCandidates повільним настільки,
// що модалка дублювання/експорту "підвисала" — filepath.WalkDir по кожному
// region/chunk-файлу кожного світу). Сама тека saves лишається у списку
// кандидатів ОДНИМ вузлом (щоб її можна було вибрати цілком), але її вміст
// (світи, файли світів) не перелічується.
func TestScanCandidatesSavesShallow(t *testing.T) {
	dir := writeTestPack(t)
	// Додатковий "важкий" вміст світу поверх writeTestPack — якби сканування
	// заходило всередину saves/, ці шляхи потрапили б у результат.
	extra := map[string]string{
		"saves/world1/region/r.0.0.mca":  "region-bytes",
		"saves/world1/playerdata/x.dat":  "player-bytes",
		"saves/world2/level.dat":         "level2-bytes",
	}
	for rel, content := range extra {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	cands, err := ScanCandidates(dir)
	if err != nil {
		t.Fatal(err)
	}

	var savesNode *ExportCandidate
	for i := range cands {
		if cands[i].Path == "saves" {
			savesNode = &cands[i]
		}
		if strings.HasPrefix(cands[i].Path, "saves/") {
			t.Errorf("вміст saves/ не має скануватись рекурсивно, знайдено: %s", cands[i].Path)
		}
	}
	if savesNode == nil {
		t.Fatal("saves має лишитись у списку кандидатів як один вузол")
	}
	if savesNode.Type != "directory" {
		t.Errorf("saves має бути типу directory, отримали %s", savesNode.Type)
	}
}

func TestSelection(t *testing.T) {
	sel := newExportSelection(
		[]string{"mods", "config", "resourcepacks"},
		[]string{"mods/skip.jar"},
	)
	if !sel.included("mods/create.jar") {
		t.Error("mods/create.jar має бути включений")
	}
	if sel.included("mods/skip.jar") {
		t.Error("mods/skip.jar має бути виключений (глибше правило)")
	}
	if !sel.included("config/example.toml") {
		t.Error("config/example.toml має бути включений")
	}
	if sel.included("saves/world1/level.dat") {
		t.Error("saves має бути виключений")
	}
}

// fakeResolver імітує Modrinth: знає хеші create.jar і jei.jar.
func fakeResolver(t *testing.T, dir string) Resolver {
	return fnResolver(func(hashes []string) (map[string]ResolvedFile, error) {
		out := map[string]ResolvedFile{}
		for _, h := range hashes {
			// Хеш sha512 "create-jar-bytes" — порахуємо на льоту.
			if h == sha512Hex("create-jar-bytes") {
				out[h] = ResolvedFile{
					URL: "https://cdn.modrinth.com/data/create.jar",
					Sha1: sha1Hex("create-jar-bytes"),
					Sha512: h,
					Size: int64(len("create-jar-bytes")),
					ModName: "Create",
					IconURL: "https://cdn.modrinth.com/icon.png",
				}
			}
		}
		return out, nil
	})
}

func TestExportMrpack(t *testing.T) {
	dir := writeTestPack(t)
	outDir := t.TempDir()
	req := ExportRequest{
		Name:    "My Pack",
		Version: "1.0.0",
		Formats: []string{"mrpack"},
		OutDir:  outDir,
		Included: []string{"mods", "config", "resourcepacks", "shaderpacks"},
		Excluded: nil,
		Dependencies: map[string]string{"minecraft": "1.20.1", "fabric-loader": "0.15.0"},
	}
	res, err := Export(dir, outDir, req, func(hashes []string) (map[string]ResolvedFile, error) {
		return fakeResolver(t, dir).ResolveSha512(hashes)
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Files) != 1 || !strings.HasSuffix(res.Files[0], ".mrpack") {
		t.Fatalf("очікували 1 mrpack, отримали %v", res.Files)
	}

	// Перевіряємо вміст архіву.
	zr, err := zip.OpenReader(res.Files[0])
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()

	var indexData []byte
	var hasOverrides bool
	for _, f := range zr.File {
		if f.Name == "modrinth.index.json" {
			rc, _ := f.Open()
			buf := make([]byte, f.UncompressedSize64)
			_, _ = rc.Read(buf)
			indexData = buf
			rc.Close()
		}
		if strings.HasPrefix(f.Name, "overrides/") {
			hasOverrides = true
		}
	}
	if len(indexData) == 0 {
		t.Fatal("modrinth.index.json відсутній")
	}
	var idx mrpackIndex
	if err := json.Unmarshal(indexData, &idx); err != nil {
		t.Fatalf("index невалідний JSON: %v", err)
	}
	if idx.FormatVersion != 1 || idx.Game != "minecraft" || idx.Name != "My Pack" || idx.VersionID != "1.0.0" {
		t.Errorf("index header невірний: %+v", idx)
	}
	if idx.Dependencies["minecraft"] != "1.20.1" || idx.Dependencies["fabric-loader"] != "0.15.0" {
		t.Errorf("dependencies невірні: %v", idx.Dependencies)
	}
	// create.jar резолвнувся → у files[], не в overrides.
	foundCreate := false
	for _, f := range idx.Files {
		if f.Path == "mods/create.jar" {
			foundCreate = true
			if f.Downloads[0] != "https://cdn.modrinth.com/data/create.jar" {
				t.Error("downloads невірний")
			}
			if f.Hashes["sha1"] == "" || f.Hashes["sha512"] == "" {
				t.Error("sha1/sha512 мають бути")
			}
		}
		if f.Path == "mods/jei.jar" { // .disabled → без суфікса
			if f.Env["client"] != "optional" {
				t.Errorf("jei.jar.disabled має бути optional, отримали %v", f.Env)
			}
		}
	}
	if !foundCreate {
		t.Error("create.jar не знайдено в files[]")
	}
	// config і shaderpacks — в overrides.
	if !hasOverrides {
		t.Error("очікували overrides/ в архіві (config, shaderpacks)")
	}
}

func TestExportZipRoundTrip(t *testing.T) {
	dir := writeTestPack(t)
	outDir := t.TempDir()
	req := ExportRequest{
		Name:    "RoundTrip",
		Formats: []string{"zip"},
		OutDir:  outDir,
		Included: []string{"mods", "config"},
	}
	res, err := Export(dir, outDir, req, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.OpenReader(res.Files[0])
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
	}
	// Файли в корені архіву у структурі .minecraft (як generic zip Prism).
	if !names["mods/create.jar"] || !names["config/example.toml"] {
		t.Errorf("zip має містити файли в корені: %v", names)
	}
	// Вимкнені моди теж експортуються (тіло, без резолву).
	if !names["mods/jei.jar.disabled"] {
		t.Error("zip має містити .disabled файл")
	}
}

func TestExportNoFiles(t *testing.T) {
	dir := writeTestPack(t)
	outDir := t.TempDir()
	req := ExportRequest{
		Name:    "Empty",
		Formats: []string{"mrpack"},
		OutDir:  outDir,
		Included: []string{"nonexistent-dir"}, // такої теки немає → нічого експортувати
	}
	_, err := Export(dir, outDir, req, nil, nil)
	if err == nil {
		t.Fatal("очікували помилку «немає файлів»")
	}
}

func TestSanitizeName(t *testing.T) {
	if got := SanitizeName("A:B/C"); got != "A_B_C" {
		t.Errorf("SanitizeName: %q", got)
	}
	if got := SanitizeName("   "); got != "pack" {
		t.Errorf("SanitizeName порожній: %q", got)
	}
}
