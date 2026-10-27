package shaurmpack

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExportImportRoundtrip(t *testing.T) {
	// Створюємо фейкову .minecraft/ збірку
	gameDir := t.TempDir()
	modsDir := filepath.Join(gameDir, "mods")
	configDir := filepath.Join(gameDir, "config")
	os.MkdirAll(modsDir, 0755)
	os.MkdirAll(configDir, 0755)

	// Додаємо файли
	os.WriteFile(filepath.Join(modsDir, "fabric-api.jar"), []byte("fake fabric api content"), 0644)
	os.WriteFile(filepath.Join(modsDir, "sodium.jar"), []byte("fake sodium content"), 0644)
	os.WriteFile(filepath.Join(configDir, "video.txt"), []byte("renderDistance:12"), 0644)

	// Експортуємо
	outPath := filepath.Join(t.TempDir(), "test-pack.shaurmpack")
	result, err := Export(ExportOptions{
		GameDir:       gameDir,
		PackID:        "test-pack",
		Name:          "Test Pack",
		VersionID:     "1.0.0",
		Description:   "Тестовий пак",
		Authors:       []string{"TestUser"},
		GameVersion:   "1.20.1",
		Loader:        "fabric",
		LoaderVersion: "0.15.11",
		Color:         "#ff0000",
		OutPath:       outPath,
		LauncherName:  "Shaurma Launcher",
		LauncherVersion: "1.0.0",
	})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	t.Logf("Exported: %d files (%d bundled, %d resolved), %d bytes, hash=%s",
		result.TotalFiles, result.BundledFiles, result.ResolvedFiles, result.TotalSize, result.Hash)

	// Перевіряємо що файл створився
	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf(" файл не створено: %v", err)
	}

	// Читаємо manifest
	manifest, err := ReadManifest(outPath)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if manifest.PackID != "test-pack" {
		t.Errorf("PackID = %q, want test-pack", manifest.PackID)
	}
	if manifest.Name != "Test Pack" {
		t.Errorf("Name = %q, want Test Pack", manifest.Name)
	}
	if manifest.GameVersion != "1.20.1" {
		t.Errorf("GameVersion = %q, want 1.20.1", manifest.GameVersion)
	}
	if manifest.Loader != "fabric" {
		t.Errorf("Loader = %q, want fabric", manifest.Loader)
	}
	if manifest.Color != "#ff0000" {
		t.Errorf("Color = %q, want #ff0000", manifest.Color)
	}
	if len(manifest.Files) != 3 {
		t.Errorf("Files count = %d, want 3", len(manifest.Files))
	}
	// Без резолверів: 2 jar-и → bundled/, config/video.txt → overrides/.
	if manifest.BundledCount != 2 {
		t.Errorf("BundledCount = %d, want 2 (jar-и без резолвера)", manifest.BundledCount)
	}
	if manifest.OverrideCount != 1 {
		t.Errorf("OverrideCount = %d, want 1 (config)", manifest.OverrideCount)
	}

	// Імпортуємо в нову теку
	targetDir := t.TempDir()
	result2, err := Import(ImportOptions{
		ShaurmPackPath: outPath,
		TargetGameDir:  targetDir,
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	t.Logf("Imported: %d files copied, %d linked", result2.FilesCopied, result2.FilesLinked)

	// Перевіряємо що файли розпаковані
	for _, f := range manifest.Files {
		path := filepath.Join(targetDir, filepath.FromSlash(f.Path))
		if _, err := os.Stat(path); err != nil {
			t.Errorf("файл %s не розпаковано: %v", f.Path, err)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.1", "1.0.0", 1},
		{"1.0.0", "1.0.1", -1},
		{"2.0.0", "1.9.9", 1},
		{"1.10.0", "1.9.9", 1},
		{"1.0.0", "2.0.0", -1},
		{"v1.0.0", "v1.0.0", 0},
		{"0.1.0", "1.0.0", -1},
	}
	for _, tt := range tests {
		got := CompareVersions(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestValidateManifest(t *testing.T) {
	// Валідний
	m := &Manifest{
		Version:     1,
		PackID:      "test",
		Name:        "Test",
		GameVersion: "1.20.1",
		Loader:      "fabric",
	}
	if err := ValidateManifest(m); err != nil {
		t.Errorf("ValidateManifest(valid) = %v", err)
	}

	// Без packId
	m2 := &Manifest{Version: 1, Name: "Test", GameVersion: "1.20.1", Loader: "fabric"}
	if err := ValidateManifest(m2); err == nil {
		t.Error("очікували помилку для порожнього packId")
	}

	// Без name
	m3 := &Manifest{Version: 1, PackID: "test", GameVersion: "1.20.1", Loader: "fabric"}
	if err := ValidateManifest(m3); err == nil {
		t.Error("очікували помилку для порожнього name")
	}

	// nil
	if err := ValidateManifest(nil); err == nil {
		t.Error("очікували помилку для nil маніфесту")
	}
}

func TestDetectFileType(t *testing.T) {
	tests := []struct {
		path string
		want FileType
	}{
		{"mods/fabric-api.jar", FileTypeMod},
		{"mods/sodium.zip", FileTypeMod},
		{"resourcepacks/faithful.zip", FileTypeResourcePack},
		{"shaderpacks/BSL.glsl", FileTypeShaderPack},
		{"config/video.txt", FileTypeConfig},
		{"options.txt", FileTypeOther},
	}
	for _, tt := range tests {
		got := detectFileType(tt.path)
		if got != tt.want {
			t.Errorf("detectFileType(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestPersonalization(t *testing.T) {
	// Експортуємо з персоналізацією
	gameDir := t.TempDir()
	os.MkdirAll(filepath.Join(gameDir, "mods"), 0755)
	os.WriteFile(filepath.Join(gameDir, "mods", "test.jar"), []byte("test"), 0644)

	outPath := filepath.Join(t.TempDir(), "test.shaurmpack")
	_, err := Export(ExportOptions{
		GameDir:     gameDir,
		PackID:      "test",
		Name:        "Test",
		GameVersion: "1.20.1",
		Loader:      "vanilla",
		OutPath:     outPath,
		Personalization: &Personalization{
			WelcomeMessage: "Ласкаво просимо!",
			Tags:           []string{"survival", "multiplayer"},
		},
	})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	// Читаємо персоналізацію
	p, err := ReadPersonalization(outPath)
	if err != nil {
		t.Fatalf("ReadPersonalization: %v", err)
	}
	if p.WelcomeMessage != "Ласкаво просимо!" {
		t.Errorf("WelcomeMessage = %q", p.WelcomeMessage)
	}
	if len(p.Tags) != 2 {
		t.Errorf("Tags count = %d, want 2", len(p.Tags))
	}
}

// ── .shaurmbundle тести ──

func TestBundleExportImportRoundtrip(t *testing.T) {
	// Створюємо дві фейкові збірки
	pack1Dir := t.TempDir()
	pack2Dir := t.TempDir()
	os.MkdirAll(filepath.Join(pack1Dir, "mods"), 0755)
	os.MkdirAll(filepath.Join(pack2Dir, "mods"), 0755)
	os.MkdirAll(filepath.Join(pack2Dir, "resourcepacks"), 0755)

	os.WriteFile(filepath.Join(pack1Dir, "mods", "mod1.jar"), []byte("mod1 content"), 0644)
	os.WriteFile(filepath.Join(pack2Dir, "mods", "mod2.jar"), []byte("mod2 content"), 0644)
	os.WriteFile(filepath.Join(pack2Dir, "resourcepacks", "faithful.zip"), []byte("rp content"), 0644)

	// Експортуємо бандл
	outPath := filepath.Join(t.TempDir(), "test.shaurmbundle")
	pack1JSON := []byte(`{"id":"pack1","name":"Pack One","color":"#ff0000","mcVersion":"1.20.1","loader":"fabric"}`)
	pack2JSON := []byte(`{"id":"pack2","name":"Pack Two","color":"#00ff00","mcVersion":"1.21.1","loader":"forge"}`)

	result, err := ExportBundle(BundleExportOptions{
		Packs: []BundlePackExport{
			{PackData: pack1JSON, PackID: "pack1", GameDir: pack1Dir},
			{PackData: pack2JSON, PackID: "pack2", GameDir: pack2Dir},
		},
		LauncherName:    "Shaurma Launcher",
		LauncherVersion: "1.0.0",
		OutPath:        outPath,
	})
	if err != nil {
		t.Fatalf("ExportBundle: %v", err)
	}
	t.Logf("Bundle: %d packs, %d files, %d bytes", result.PackCount, result.TotalFiles, result.TotalSize)

	if result.PackCount != 2 {
		t.Errorf("PackCount = %d, want 2", result.PackCount)
	}

	// Читаємо bundle manifest
	bundle, err := ReadBundleManifest(outPath)
	if err != nil {
		t.Fatalf("ReadBundleManifest: %v", err)
	}
	if bundle.Format != "shaurmbundle" {
		t.Errorf("Format = %q, want shaurmbundle", bundle.Format)
	}
	if len(bundle.Packs) != 2 {
		t.Errorf("Packs count = %d, want 2", len(bundle.Packs))
	}
	if bundle.Launcher.Name != "Shaurma Launcher" {
		t.Errorf("Launcher.Name = %q", bundle.Launcher.Name)
	}

	// Імпортуємо бандл
	targetDir := t.TempDir()
	importResult, err := ImportBundle(BundleImportOptions{
		ShaurmBundlePath: outPath,
		TargetBaseDir:    targetDir,
	})
	if err != nil {
		t.Fatalf("ImportBundle: %v", err)
	}
	if len(importResult.Packs) != 2 {
		t.Errorf("Imported packs = %d, want 2", len(importResult.Packs))
	}

	// Перевіряємо що папки створені
	for _, p := range importResult.Packs {
		packDir := filepath.Join(targetDir, p.PackID)
		if _, err := os.Stat(packDir); err != nil {
			t.Errorf("папка збірки %s не створена: %v", p.PackID, err)
		}
		instancePath := filepath.Join(packDir, "instance.json")
		if _, err := os.Stat(instancePath); err != nil {
			t.Errorf("instance.json збірки %s не створено: %v", p.PackID, err)
		}
	}
}
