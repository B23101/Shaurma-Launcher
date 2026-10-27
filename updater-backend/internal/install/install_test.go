package install

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shrm-updater-backend/internal/download"
	"shrm-updater-backend/internal/manifest"
)

// Тести install.Apply — перевіряємо, що:
//  1. kind=archive (frontend.zip) розпаковується у ExtractTo і НЕ кладеться
//     на місце компонента (раніше replaceFile перейменовував щойно скачаний
//     zip з TEMP — точка відмови, коли антивірус тримає файл на скані без
//     FILE_SHARE_DELETE і rename падає 15 секунд).
//  2. Звичайні компоненти (backend.dll) ставляться на місце.
//  3. Маніфест записується з усіма компонентами.

// testZip створює zip-архів з одним файлом у пам'яті і повертає його байти.
func testZip(t *testing.T, name, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, err := w.Create(name)
	if err != nil {
		t.Fatalf("zip create: %v", err)
	}
	if _, err := f.Write([]byte(content)); err != nil {
		t.Fatalf("zip write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

func TestApplyArchiveExtractsWithoutPlacingZip(t *testing.T) {
	installDir := t.TempDir()
	// Тимчасовий zip у "TEMP" (окрема тека, як %TEMP%\shrm-update).
	tmpDir := t.TempDir()
	zipBytes := testZip(t, "index.html", "<html>hello</html>")
	zipPath := filepath.Join(tmpDir, "frontend.zip")
	if err := os.WriteFile(zipPath, zipBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	comp := manifest.Component{
		Path:      "frontend.zip",
		SHA256:    hexSHA(zipBytes),
		Size:      int64(len(zipBytes)),
		URL:       "/launcher/frontend.zip",
		Kind:      "archive",
		ExtractTo: "frontend",
	}
	remote := &manifest.Manifest{Version: "2.2.2", Components: []manifest.Component{comp}}

	if err := Apply(context.Background(), installDir, []download.DownloadedComponent{
		{Component: comp, TempPath: zipPath},
	}, remote, "uk", nil); err != nil {
		t.Fatalf("Apply archive: %v", err)
	}

	// 1. Розпакувалось у installDir/frontend/index.html.
	extracted := filepath.Join(installDir, "frontend", "index.html")
	data, err := os.ReadFile(extracted)
	if err != nil {
		t.Fatalf("розпакований frontend/index.html відсутній: %v", err)
	}
	if string(data) != "<html>hello</html>" {
		t.Errorf("вміст index.html неправильний: %q", data)
	}

	// 2. Сам zip НЕ має лежати на місці компонента (і .previous теж).
	if _, err := os.Stat(filepath.Join(installDir, "frontend.zip")); !os.IsNotExist(err) {
		t.Errorf("installDir/frontend.zip не має існувати (знайдено, err=%v)", err)
	}
	if _, err := os.Stat(filepath.Join(installDir, "frontend.zip.previous")); !os.IsNotExist(err) {
		t.Errorf("installDir/frontend.zip.previous не має існувати (err=%v)", err)
	}

	// 3. Тимчасовий zip у TEMP прибрано.
	if _, err := os.Stat(zipPath); !os.IsNotExist(err) {
		t.Errorf("тимчасовий zip у TEMP не прибрано (err=%v)", err)
	}

	// 4. Маніфест записаний і містить компонент.
	m, err := manifest.Load(filepath.Join(installDir, "launcher-version.json"))
	if err != nil {
		t.Fatalf("читання маніфесту: %v", err)
	}
	if m.Version != "2.2.2" || len(m.Components) != 1 {
		t.Errorf("маніфест неправильний: %+v", m)
	}
}

func TestApplyFileComponent(t *testing.T) {
	installDir := t.TempDir()
	tmpDir := t.TempDir()

	// Стара версія backend.dll на місці — має замінитись.
	oldPath := filepath.Join(installDir, "backend.dll")
	if err := os.WriteFile(oldPath, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Новий файл у TEMP.
	newPath := filepath.Join(tmpDir, "backend.dll")
	if err := os.WriteFile(newPath, []byte("new-content"), 0o644); err != nil {
		t.Fatal(err)
	}

	comp := manifest.Component{Path: "backend.dll", SHA256: hexSHA([]byte("new-content")), Size: 11, URL: "/launcher/backend.dll"}
	remote := &manifest.Manifest{Version: "2.2.2", Components: []manifest.Component{comp}}

	if err := Apply(context.Background(), installDir, []download.DownloadedComponent{
		{Component: comp, TempPath: newPath},
	}, remote, "uk", nil); err != nil {
		t.Fatalf("Apply file: %v", err)
	}

	data, err := os.ReadFile(oldPath)
	if err != nil {
		t.Fatalf("backend.dll після заміни: %v", err)
	}
	if string(data) != "new-content" {
		t.Errorf("вміст backend.dll неправильний: %q", data)
	}
	// Попередня версія — у .previous.
	if _, err := os.Stat(filepath.Join(installDir, "backend.dll.previous")); err != nil {
		t.Errorf("backend.dll.previous відсутній: %v", err)
	}
}

// TestReplaceFileCopyFallback перевіряє, що replaceFile коректно працює
// через звичайний rename (основний шлях), а copyFile — запасний шлях.
func TestReplaceFileCopyFallback(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.bin")
	dst := filepath.Join(dir, "dst.bin")
	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Основний шлях: rename (dst не існує).
	if err := replaceFile(context.Background(), src, dst); err != nil {
		t.Fatalf("replaceFile (rename): %v", err)
	}
	data, _ := os.ReadFile(dst)
	if string(data) != "hello" {
		t.Errorf("rename-шлях зіпсував вміст: %q", data)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("src має зникнути після rename (err=%v)", err)
	}

	// Запасний шлях: copyFile копіює, src лишається.
	src2 := filepath.Join(dir, "src2.bin")
	dst2 := filepath.Join(dir, "dst2.bin")
	if err := os.WriteFile(src2, []byte("copy-me"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(src2, dst2); err != nil {
		t.Fatalf("copyFile: %v", err)
	}
	data2, _ := os.ReadFile(dst2)
	if string(data2) != "copy-me" {
		t.Errorf("copyFile зіпсував вміст: %q", data2)
	}
	if _, err := os.Stat(src2); err != nil {
		t.Errorf("copyFile не має видаляти src: %v", err)
	}
}

// TestReplaceFileOverExisting перевіряє заміну вже існуючого файлу:
// старий іде в .previous, новий стає на місце.
func TestReplaceFileOverExisting(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.dll")
	dst := filepath.Join(dir, "dst.dll")
	if err := os.WriteFile(src, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := replaceFile(context.Background(), src, dst); err != nil {
		t.Fatalf("replaceFile over existing: %v", err)
	}
	data, _ := os.ReadFile(dst)
	if string(data) != "new" {
		t.Errorf("новий вміст неправильний: %q", data)
	}
	prev, err := os.ReadFile(dst + backupSuffix)
	if err != nil {
		t.Fatalf(".previous відсутній: %v", err)
	}
	if string(prev) != "old" {
		t.Errorf("старий вміст у .previous неправильний: %q", prev)
	}
}

// TestExtractArchiveZipSlip — захист від zip-slip має лишатись на місці.
func TestExtractArchiveZipSlip(t *testing.T) {
	installDir := t.TempDir()

	// Архів зі шляхом ../evil.txt.
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, _ := w.Create("../evil.txt")
	f.Write([]byte("evil"))
	w.Close()

	zipPath := filepath.Join(t.TempDir(), "bad.zip")
	if err := os.WriteFile(zipPath, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	err := extractArchive(zipPath, filepath.Join(installDir, "frontend"))
	if err == nil || !strings.Contains(err.Error(), "unsafe path") {
		t.Fatalf("очікували помилку zip-slip, отримали: %v", err)
	}
	if _, serr := os.Stat(filepath.Join(installDir, "..", "evil.txt")); !os.IsNotExist(serr) {
		t.Errorf("evil.txt вийшов за межі теки (err=%v)", serr)
	}
}

func hexSHA(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// Файл, підмінений у тимчасовій теці після завантаження, НЕ встановлюється.
func TestApplyRejectsTamperedFile(t *testing.T) {
	installDir := t.TempDir()
	tmpDir := t.TempDir()
	oldPath := filepath.Join(installDir, "backend.dll")
	if err := os.WriteFile(oldPath, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	newPath := filepath.Join(tmpDir, "backend.dll")
	if err := os.WriteFile(newPath, []byte("evil-content"), 0o644); err != nil {
		t.Fatal(err)
	}
	comp := manifest.Component{Path: "backend.dll", SHA256: hexSHA([]byte("new-content")), Size: 11, URL: "/x"}
	remote := &manifest.Manifest{Version: "2.2.2", Components: []manifest.Component{comp}}
	if err := Apply(context.Background(), installDir, []download.DownloadedComponent{
		{Component: comp, TempPath: newPath},
	}, remote, "uk", nil); err == nil {
		t.Fatal("очікували помилку цілісності, а Apply пройшов")
	}
	got, _ := os.ReadFile(oldPath)
	if string(got) != "old" {
		t.Fatalf("стара версія мала лишитись недоторканою, а там %q", got)
	}
}

// Архів із символічним посиланням відхиляється.
func TestExtractArchiveRejectsSymlink(t *testing.T) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	hdr := &zip.FileHeader{Name: "link"}
	hdr.SetMode(os.ModeSymlink | 0o777)
	f, _ := w.CreateHeader(hdr)
	_, _ = f.Write([]byte("/etc/passwd"))
	_ = w.Close()
	zp := filepath.Join(t.TempDir(), "a.zip")
	_ = os.WriteFile(zp, buf.Bytes(), 0o644)
	dest := filepath.Join(t.TempDir(), "frontend")
	if err := extractArchive(zp, dest); err == nil {
		t.Fatal("симлінк у архіві мав бути відхилений")
	}
}

// Невдала розпаковка не руйнує вже встановлену теку.
func TestExtractArchiveKeepsOldOnFailure(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "frontend")
	_ = os.MkdirAll(dest, 0o755)
	_ = os.WriteFile(filepath.Join(dest, "keep.txt"), []byte("ok"), 0o644)
	zp := filepath.Join(t.TempDir(), "bad.zip")
	_ = os.WriteFile(zp, testZip(t, "../evil.txt", "x"), 0o644)
	if err := extractArchive(zp, dest); err == nil {
		t.Fatal("очікували помилку")
	}
	if _, err := os.Stat(filepath.Join(dest, "keep.txt")); err != nil {
		t.Fatalf("стара тека зруйнована: %v", err)
	}
}
