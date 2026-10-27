package main

// ── Файловий провідник: спільна логіка ─────────────────────────────────────
// Узагальнені функції та типи для файлового провідника, що НЕ залежать
// від конкретного типу об'єкта (збірка чи сервер). Використовуються
// пакфайлами (packfiles.go) та серверними файлами (serverfiles.go).

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// FileEntry — один елемент списку теки.
type FileEntry struct {
	Name     string `json:"name"`
	Path     string `json:"path"` // відносно кореня ("mods/foo.jar"), зі слешами
	IsDir    bool   `json:"isDir"`
	Size     int64  `json:"size"`
	Modified string `json:"modified"` // RFC3339
}

// DirListing — вміст однієї теки.
type DirListing struct {
	RootId  string      `json:"rootId"` // packId або serverId
	Path    string      `json:"path"`   // поточний відносний шлях
	Entries []FileEntry `json:"entries"`
}

// FileContent — результат читання файлу.
type FileContent struct {
	Content string `json:"content"`
	Size    int64  `json:"size"`
	Binary  bool   `json:"binary"` // не текст — редагувати не можна
	Mime    string `json:"mime,omitempty"`
}

// maxTextEditBytes — верхня межа розміру текстового файлу для редагування.
const maxTextEditBytes = 1 << 20 // 1 МБ

// binaryExts — розширення, що свідомо не відкриваються як текст.
var binaryExts = map[string]bool{
	".jar": true, ".zip": true, ".png": true, ".jpg": true, ".jpeg": true,
	".webp": true, ".gif": true, ".ogg": true, ".mp3": true, ".wav": true,
	".nbt": true, ".dat": true, ".class": true, ".dll": true, ".so": true,
	".dylib": true, ".bin": true, ".ttf": true, ".otf": true, ".woff": true,
	".woff2": true, ".ico": true, ".icns": true, ".json.gz": true, ".nbt.gz": true,
}

// resolveRelPath безпечно резолвить відносний шлях у абсолютний
// строго всередині rootDir. Порожній рядок/".?" = корінь теки.
func resolveRelPath(rootDir, relPath string) (string, error) {
	if relPath == "" || relPath == "." {
		return rootDir, nil
	}
	clean := filepath.Clean(filepath.FromSlash(relPath))
	if clean == "." || strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
		return "", fmt.Errorf("недопустимий шлях: %s", relPath)
	}
	abs := filepath.Join(rootDir, clean)
	root := filepath.Clean(rootDir)
	if abs != root && !strings.HasPrefix(abs, root+string(filepath.Separator)) {
		return "", fmt.Errorf("шлях виходить за межі теки")
	}
	return abs, nil
}

// validateFileName перевіряє ім'я файлу/теки за стандартами Windows і
// прибирає очевидний сміття. Окремо перевіряється дублікат (анти-дублікати).
func validateFileName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("порожня назва")
	}
	if name == "." || name == ".." {
		return "", fmt.Errorf("неприпустима назва")
	}
	if strings.ContainsAny(name, `<>:\"/\\|?*`) || strings.IndexByte(name, 0) >= 0 {
		return "", fmt.Errorf("назва містить неприпустимі символи: < > : \" / \\ | ? *")
	}
	if strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return "", fmt.Errorf("назва не може закінчуватись крапкою або пробілом")
	}
	// Резервовані імена Windows (CON, PRN, AUX, NUL, COM1..9, LPT1..9).
	base := strings.ToUpper(strings.TrimSuffix(name, filepath.Ext(name)))
	for _, r := range []string{"CON", "PRN", "AUX", "NUL"} {
		if base == r {
			return "", fmt.Errorf("назва зарезервована системою Windows")
		}
	}
	for i := 1; i <= 9; i++ {
		if base == fmt.Sprintf("COM%d", i) || base == fmt.Sprintf("LPT%d", i) {
			return "", fmt.Errorf("назва зарезервована системою Windows")
		}
	}
	return name, nil
}

// nameExistsIn перевіряє дублікат імені в теці регістро-незалежно (Windows).
func nameExistsIn(dir, name string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if strings.EqualFold(e.Name(), name) {
			return true
		}
	}
	return false
}

// mimeForText повертає MIME-підказку для текстового редактора (підсвітка).
func mimeForText(ext string) string {
	switch strings.ToLower(ext) {
	case ".json":
		return "application/json"
	case ".toml":
		return "application/toml"
	case ".properties", ".cfg", ".conf", ".ini":
		return "text/x-properties"
	case ".yaml", ".yml":
		return "application/yaml"
	case ".lua":
		return "text/x-lua"
	case ".mcfunction":
		return "text/x-mcfunction"
	case ".sh":
		return "text/x-sh"
	case ".md", ".txt", ".log", ".json5", ".jsonc":
		return "text/plain"
	case ".css":
		return "text/css"
	case ".html", ".htm":
		return "text/html"
	case ".xml":
		return "application/xml"
	}
	return "text/plain"
}

// listDir повертає вміст теки (теки перші, потім файли за алфавітом).
func listDir(rootDir, relPath, rootId string) (DirListing, error) {
	abs, err := resolveRelPath(rootDir, relPath)
	if err != nil {
		return DirListing{}, err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return DirListing{}, fmt.Errorf("теку не знайдено: %s", relPath)
	}
	if !st.IsDir() {
		return DirListing{}, fmt.Errorf("«%s» — не тека", relPath)
	}
	items, err := os.ReadDir(abs)
	if err != nil {
		return DirListing{}, err
	}
	entries := make([]FileEntry, 0, len(items))
	for _, it := range items {
		name := it.Name()
		info, err := it.Info()
		if err != nil {
			continue
		}
		p := name
		if relPath != "" {
			p = filepath.ToSlash(filepath.Join(relPath, name))
		}
		entries = append(entries, FileEntry{
			Name:     name,
			Path:     p,
			IsDir:    it.IsDir(),
			Size:     info.Size(),
			Modified: info.ModTime().Format(time.RFC3339),
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	return DirListing{RootId: rootId, Path: relPath, Entries: entries}, nil
}

// readTextFile читає файл. Для нетекстових/завеликих файлів
// повертає Binary=true (фронтенд запропонує перегляд/інші дії, а не
// редагування).
func readTextFile(rootDir, relPath string) (FileContent, error) {
	abs, err := resolveRelPath(rootDir, relPath)
	if err != nil {
		return FileContent{}, err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return FileContent{}, fmt.Errorf("файл не знайдено: %s", relPath)
	}
	if st.IsDir() {
		return FileContent{}, fmt.Errorf("«%s» — тека", relPath)
	}
	ext := strings.ToLower(filepath.Ext(abs))
	res := FileContent{Size: st.Size(), Mime: mimeForText(ext)}
	if binaryExts[ext] || st.Size() > maxTextEditBytes {
		res.Binary = true
		return res, nil
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return FileContent{}, err
	}
	// Контроль двійкового вмісту: якщо в перших байтах є NUL — не текст.
	if len(data) > 0 && data[0] == 0 {
		res.Binary = true
		return res, nil
	}
	res.Content = string(data)
	return res, nil
}

// writeTextFile зберігає відредагований текстовий файл
// (атомарно: тимчасовий файл + перейменування). Не дає перезаписати
// бінарний файл.
func writeTextFile(rootDir, relPath, content string) error {
	abs, err := resolveRelPath(rootDir, relPath)
	if err != nil {
		return err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("файл не знайдено: %s", relPath)
	}
	if st.IsDir() {
		return fmt.Errorf("«%s» — тека", relPath)
	}
	if st.Size() > maxTextEditBytes {
		return fmt.Errorf("файл завеликий для редагування")
	}
	if binaryExts[strings.ToLower(filepath.Ext(abs))] {
		return fmt.Errorf("це не текстовий файл")
	}
	tmp := abs + ".shrm-tmp"
	if err := os.WriteFile(tmp, []byte(content), 0644); err != nil {
		return err
	}
	return os.Rename(tmp, abs)
}

// renameEntry перейменовує файл/теку: перевірка імені за стандартами,
// анти-дублікат (регістро-незалежно), заборона виходу за межі теки.
func renameEntry(rootDir, relPath, newName string) error {
	abs, err := resolveRelPath(rootDir, relPath)
	if err != nil {
		return err
	}
	if _, err := os.Stat(abs); err != nil {
		return fmt.Errorf("елемент не знайдено: %s", relPath)
	}
	name, err := validateFileName(newName)
	if err != nil {
		return err
	}
	parent := filepath.Dir(abs)
	if nameExistsIn(parent, name) {
		return fmt.Errorf("файл або тека «%s» вже існують", name)
	}
	newAbs := filepath.Join(parent, name)
	if newAbs == abs {
		return nil // назва не змінилась
	}
	return os.Rename(abs, newAbs)
}

// createFolder створює теку.
func createFolder(rootDir, relPath, name string) error {
	parent, err := resolveRelPath(rootDir, relPath)
	if err != nil {
		return err
	}
	if st, err := os.Stat(parent); err != nil || !st.IsDir() {
		return fmt.Errorf("теку не знайдено: %s", relPath)
	}
	name, err = validateFileName(name)
	if err != nil {
		return err
	}
	if nameExistsIn(parent, name) {
		return fmt.Errorf("тека «%s» вже існує", name)
	}
	return os.MkdirAll(filepath.Join(parent, name), 0755)
}

// createFile створює ПОРОЖНІЙ файл у вказаній теці.
func createFile(rootDir, relPath, name string) error {
	parent, err := resolveRelPath(rootDir, relPath)
	if err != nil {
		return err
	}
	if st, err := os.Stat(parent); err != nil || !st.IsDir() {
		return fmt.Errorf("теку не знайдено: %s", relPath)
	}
	name, err = validateFileName(name)
	if err != nil {
		return err
	}
	if nameExistsIn(parent, name) {
		return fmt.Errorf("файл «%s» вже існує", name)
	}
	return os.WriteFile(filepath.Join(parent, name), []byte{}, 0644)
}

// deleteEntry видаляє файл/теку (рекурсивно для теки).
func deleteEntry(rootDir, relPath string) error {
	abs, err := resolveRelPath(rootDir, relPath)
	if err != nil {
		return err
	}
	if _, err := os.Stat(abs); err != nil {
		return fmt.Errorf("елемент не знайдено: %s", relPath)
	}
	return os.RemoveAll(abs)
}
