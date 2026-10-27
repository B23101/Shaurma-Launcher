package shaurmpack

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// walkZipCheck відкриває ZIP і, якщо знаходить запис name, читає його
// вміст і передає в cb.
func walkZipCheck(zipPath, name string, cb func(data []byte)) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				return err
			}
			defer rc.Close()
			data, err := io.ReadAll(rc)
			if err != nil {
				return err
			}
			cb(data)
			return nil
		}
	}
	return nil
}

// TestModrinthPriorityOverCurseForge — при наявності джерела на ОБОХ
// платформах у маніфесті зберігаються ОБИДВА (Modrinth — основне, і саме його
// бере імпорт). Файл у ZIP не потрапляє.
func TestModrinthPriorityOverCurseForge(t *testing.T) {
	gameDir := t.TempDir()
	modsDir := filepath.Join(gameDir, "mods")
	os.MkdirAll(modsDir, 0755)
	os.WriteFile(filepath.Join(modsDir, "dual-source.jar"), []byte("dual source content"), 0644)

	cfCalled := false
	outPath := filepath.Join(t.TempDir(), "dual.shaurmpack")
	result, err := Export(ExportOptions{
		GameDir:     gameDir,
		PackID:      "dual",
		Name:        "Dual",
		GameVersion: "1.20.1",
		Loader:      "fabric",
		OutPath:     outPath,
		ResolveModrinth: func(sha1hex, fileName string, size int64) *ModrinthSource {
			return &ModrinthSource{ProjectID: "abc", VersionID: "def", FileName: fileName, DownloadURL: "https://modrinth/dual.jar", Required: true}
		},
		ResolveCurseForge: func(sha1hex, fileName string, size int64) *CurseForgeSource {
			cfCalled = true
			return &CurseForgeSource{ProjectID: 1, FileID: 2, FileName: fileName, DownloadURL: "https://curseforge/dual.jar"}
		},
	})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if !cfCalled {
		t.Error("ResolveCurseForge мав викликатись — у маніфесті зберігаються обидва джерела")
	}
	if result.ResolvedFiles != 1 || result.BundledFiles != 0 {
		t.Errorf("resolved=%d bundled=%d, want resolved=1 bundled=0", result.ResolvedFiles, result.BundledFiles)
	}
	manifest, err := ReadManifest(outPath)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	pf := manifest.Files[0]
	if pf.Modrinth == nil {
		t.Error("очікували Modrinth джерело в маніфесті")
	}
	if pf.CurseForge == nil {
		t.Error("очікували і CurseForge джерело в маніфесті поряд з Modrinth")
	}
	if err := walkZipCheck(outPath, "overrides/mods/dual-source.jar", func([]byte) { t.Error("файл з джерелом НЕ має лежати в overrides/") }); err != nil {
		t.Fatal(err)
	}
	if err := walkZipCheck(outPath, "bundled/mods/dual-source.jar", func([]byte) { t.Error("файл з джерелом НЕ має лежати в bundled/") }); err != nil {
		t.Fatal(err)
	}
	if pf.Bundled {
		t.Error("файл з робочим Modrinth джерелом НЕ повинен бути bundled")
	}
}

// TestBlockedCurseForgeGoesBundled — файл, заблокований автором на
// CurseForge (Blocked=true), має піти в bundled/ як bundled=true, БЕЗ
// спроби хешування/завантаження по (порожньому) DownloadURL. Modrinth
// джерела немає — це єдиний кандидат.
func TestBlockedCurseForgeGoesBundled(t *testing.T) {
	gameDir := t.TempDir()
	modsDir := filepath.Join(gameDir, "mods")
	os.MkdirAll(modsDir, 0755)
	content := []byte("blocked mod content")
	os.WriteFile(filepath.Join(modsDir, "blocked-mod.jar"), content, 0644)

	outPath := filepath.Join(t.TempDir(), "blocked.shaurmpack")
	result, err := Export(ExportOptions{
		GameDir:     gameDir,
		PackID:      "blocked-pack",
		Name:        "Blocked Pack",
		GameVersion: "1.20.1",
		Loader:      "forge",
		OutPath:     outPath,
		ResolveModrinth: func(sha1hex, fileName string, size int64) *ModrinthSource {
			return nil // немає на Modrinth
		},
		ResolveCurseForge: func(sha1hex, fileName string, size int64) *CurseForgeSource {
			// Автор заблокував сторонні завантаження: DownloadURL порожній,
			// Blocked=true — так само, як реальний CFFileInfo.Blocked.
			return &CurseForgeSource{
				ProjectID:   111,
				FileID:      222,
				FileName:    fileName,
				DownloadURL: "",
				Blocked:     true,
			}
		},
	})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if result.BundledFiles != 1 || result.ResolvedFiles != 0 {
		t.Fatalf("bundled=%d resolved=%d, want bundled=1 resolved=0", result.BundledFiles, result.ResolvedFiles)
	}

	manifest, err := ReadManifest(outPath)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	pf := manifest.Files[0]
	if !pf.Bundled {
		t.Error("заблокований CurseForge-файл МАЄ бути Bundled=true")
	}
	if pf.CurseForge != nil {
		t.Error("заблокований CurseForge-запис НЕ має потрапляти в маніфест")
	}

	// Файл справді лежить у bundled/ всередині ZIP і його байти коректні.
	found := false
	err = walkZipCheck(outPath, "bundled/mods/blocked-mod.jar", func(data []byte) {
		found = true
		if string(data) != string(content) {
			t.Error("вміст bundled-файлу не збігається з оригіналом")
		}
	})
	if err != nil {
		t.Fatalf("walkZipCheck: %v", err)
	}
	if !found {
		t.Error("заблокований файл НЕ знайдено в bundled/ архіву")
	}

	// Import: заблокований файл має розпакуватись з bundled/, БЕЗ виклику
	// ResolveDownload (він би провалився з порожнім DownloadURL).
	resolveDownloadCalled := false
	targetDir := t.TempDir()
	importResult, err := Import(ImportOptions{
		ShaurmPackPath: outPath,
		TargetGameDir:  targetDir,
		ResolveDownload: func(destPath string, pf PackFile) error {
			resolveDownloadCalled = true
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if resolveDownloadCalled {
		t.Error("ResolveDownload НЕ мав викликатись для заблокованого CurseForge-файлу")
	}
	installedPath := filepath.Join(targetDir, "mods", "blocked-mod.jar")
	data, err := os.ReadFile(installedPath)
	if err != nil {
		t.Fatalf("заблокований файл не встановлено: %v", err)
	}
	if string(data) != string(content) {
		t.Error("встановлений вміст не збігається з оригіналом")
	}
	if importResult.FilesCopied < 1 {
		t.Errorf("FilesCopied=%d, want >=1", importResult.FilesCopied)
	}
}

// TestBlockedCurseForgeInconsistentManifest — страховий сценарій: якщо
// хтось вручну зібрав/підправив .shaurmpack так, що Bundled=false, але
// CurseForge.Blocked=true (неконсистентний стан), Import() ВСЕ ОДНО не
// повинен намагатись іти по DownloadURL — має спрацювати bundled fallback.
func TestBlockedCurseForgeInconsistentManifest(t *testing.T) {
	gameDir := t.TempDir()
	modsDir := filepath.Join(gameDir, "mods")
	os.MkdirAll(modsDir, 0755)
	content := []byte("inconsistent blocked content")
	os.WriteFile(filepath.Join(modsDir, "weird.jar"), content, 0644)

	outPath := filepath.Join(t.TempDir(), "weird.shaurmpack")
	_, err := Export(ExportOptions{
		GameDir:     gameDir,
		PackID:      "weird-pack",
		Name:        "Weird",
		GameVersion: "1.20.1",
		Loader:      "forge",
		OutPath:     outPath,
		ResolveCurseForge: func(sha1hex, fileName string, size int64) *CurseForgeSource {
			return &CurseForgeSource{ProjectID: 1, FileID: 2, FileName: fileName, Blocked: true}
		},
	})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	resolveDownloadCalled := false
	targetDir := t.TempDir()
	_, err = Import(ImportOptions{
		ShaurmPackPath: outPath,
		TargetGameDir:  targetDir,
		ResolveDownload: func(destPath string, pf PackFile) error {
			resolveDownloadCalled = true
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if resolveDownloadCalled {
		t.Error("ResolveDownload НЕ мав викликатись навіть у неконсистентному манІфесті (isBlockedCF guard)")
	}
	if _, err := os.Stat(filepath.Join(targetDir, "mods", "weird.jar")); err != nil {
		t.Errorf("файл мав розпакуватись через isBlockedCF-фолбек: %v", err)
	}
}
