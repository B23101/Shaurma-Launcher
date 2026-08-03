package main

// ── Файловий провідник збірки ────────────────────────────────────────────
// Розділ «Файли» для кастомних (модових) збірок: перегляд теки .minecraft
// збірки, навігація по теках, перегляд/редагування текстових файлів,
// перегляд зображень, відкриття у Провіднику, копіювання назви/шляху,
// перейменування (за стандартами імен файлів + анти-дублікати), створення
// тек і видалення. Лише список файлів/тек — Менеджер модів (вмикання/
// вимикання) реалізується окремо.
//
// ВАЖЛИВО про безпеку: усі операції приймають ПАК-відносний шлях і
// резолвлять його строго всередині <instanceDir>/<packID>/.minecraft
// (resolvePackPath) — вихід за межі теки збірки неможливий.

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// PackFileEntry — один елемент списку теки збірки.
type PackFileEntry struct {
	Name     string `json:"name"`
	Path     string `json:"path"` // відносно .minecraft ("mods/foo.jar"), зі слешами
	IsDir    bool   `json:"isDir"`
	Size     int64  `json:"size"`
	Modified string `json:"modified"` // RFC3339
}

// PackDirListing — вміст однієї теки збірки.
type PackDirListing struct {
	PackID  string          `json:"packId"`
	Path    string          `json:"path"` // поточний відносний шлях
	Entries []PackFileEntry `json:"entries"`
}

// PackFileContent — результат читання файлу збірки.
type PackFileContent struct {
	Content string `json:"content"`
	Size    int64  `json:"size"`
	Binary  bool   `json:"binary"` // не текст (зображення/архів/великий) — редагувати не можна
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

// packGameDir повертає абсолютний шлях до .minecraft теки кастомної збірки.
func (a *App) packGameDir(packID string) (string, error) {
	cp, ok := a.customPacks.Get(packID)
	if !ok {
		return "", fmt.Errorf("кастомну збірку не знайдено: %s", packID)
	}
	return filepath.Join(a.cfg.GetSettings().InstanceDir, cp.ID, ".minecraft"), nil
}

// resolvePackPath безпечно резолвить пак-відносний шлях у абсолютний
// строго всередині gameDir. Порожній рядок/"." = корінь теки.
func resolvePackPath(gameDir, relPath string) (string, error) {
	if relPath == "" || relPath == "." {
		return gameDir, nil
	}
	clean := filepath.Clean(filepath.FromSlash(relPath))
	if clean == "." || strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
		return "", fmt.Errorf("недопустимий шлях: %s", relPath)
	}
	abs := filepath.Join(gameDir, clean)
	root := filepath.Clean(gameDir)
	if abs != root && !strings.HasPrefix(abs, root+string(filepath.Separator)) {
		return "", fmt.Errorf("шлях виходить за межі теки збірки")
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
	if strings.ContainsAny(name, `<>:"/\|?*`) || strings.IndexByte(name, 0) >= 0 {
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

// ListPackFiles повертає вміст теки .minecraft збірки (теки перші, потім
// файли за алфавітом).
func (a *App) ListPackFiles(packID, relPath string) (PackDirListing, error) {
	gameDir, err := a.packGameDir(packID)
	if err != nil {
		return PackDirListing{}, err
	}
	abs, err := resolvePackPath(gameDir, relPath)
	if err != nil {
		return PackDirListing{}, err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return PackDirListing{}, fmt.Errorf("теку не знайдено: %s", relPath)
	}
	if !st.IsDir() {
		return PackDirListing{}, fmt.Errorf("«%s» — не тека", relPath)
	}
	items, err := os.ReadDir(abs)
	if err != nil {
		return PackDirListing{}, err
	}
	entries := make([]PackFileEntry, 0, len(items))
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
		entries = append(entries, PackFileEntry{
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
	return PackDirListing{PackID: packID, Path: relPath, Entries: entries}, nil
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

// ReadPackTextFile читає файл збірки. Для нетекстових/завеликих файлів
// повертає Binary=true (фронтенд запропонує перегляд/інші дії, а не
// редагування).
func (a *App) ReadPackTextFile(packID, relPath string) (PackFileContent, error) {
	gameDir, err := a.packGameDir(packID)
	if err != nil {
		return PackFileContent{}, err
	}
	abs, err := resolvePackPath(gameDir, relPath)
	if err != nil {
		return PackFileContent{}, err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return PackFileContent{}, fmt.Errorf("файл не знайдено: %s", relPath)
	}
	if st.IsDir() {
		return PackFileContent{}, fmt.Errorf("«%s» — тека", relPath)
	}
	ext := strings.ToLower(filepath.Ext(abs))
	res := PackFileContent{Size: st.Size(), Mime: mimeForText(ext)}
	if binaryExts[ext] || st.Size() > maxTextEditBytes {
		res.Binary = true
		return res, nil
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return PackFileContent{}, err
	}
	// Контроль двійкового вмісту: якщо в перших байтах є NUL — не текст.
	if bytes.IndexByte(data, 0) >= 0 {
		res.Binary = true
		res.Content = ""
		return res, nil
	}
	res.Content = string(data)
	return res, nil
}

// WritePackTextFile зберігає відредагований текстовий файл збірки
// (атомарно: тимчасовий файл + перейменування). Не дає перезаписати
// бінарний файл.
func (a *App) WritePackTextFile(packID, relPath, content string) error {
	gameDir, err := a.packGameDir(packID)
	if err != nil {
		return err
	}
	abs, err := resolvePackPath(gameDir, relPath)
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

// RenamePackEntry перейменовує файл/теку збірки: перевірка імені за
// стандартами, анти-дублікат (регістро-незалежно), заборона виходу за
// межі теки.
func (a *App) RenamePackEntry(packID, relPath, newName string) error {
	gameDir, err := a.packGameDir(packID)
	if err != nil {
		return err
	}
	abs, err := resolvePackPath(gameDir, relPath)
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

// CreatePackFolder створює теку в збірці.
func (a *App) CreatePackFolder(packID, relPath, name string) error {
	gameDir, err := a.packGameDir(packID)
	if err != nil {
		return err
	}
	parent, err := resolvePackPath(gameDir, relPath)
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

// DeletePackEntry видаляє файл/теку збірки (рекурсивно для теки).
func (a *App) DeletePackEntry(packID, relPath string) error {
	gameDir, err := a.packGameDir(packID)
	if err != nil {
		return err
	}
	abs, err := resolvePackPath(gameDir, relPath)
	if err != nil {
		return err
	}
	if _, err := os.Stat(abs); err != nil {
		return fmt.Errorf("елемент не знайдено: %s", relPath)
	}
	return os.RemoveAll(abs)
}

// RevealPackFile відкриває Провідник Windows з виділеним файлом/текою
// збірки (кнопка «Показати у теці»).
func (a *App) RevealPackFile(packID, relPath string) error {
	gameDir, err := a.packGameDir(packID)
	if err != nil {
		return err
	}
	abs, err := resolvePackPath(gameDir, relPath)
	if err != nil {
		return err
	}
	if _, err := os.Stat(abs); err != nil {
		return fmt.Errorf("елемент не знайдено: %s", relPath)
	}
	return exec.Command("explorer.exe", "/select,"+abs).Start()
}

// GetPackFileAsset повертає файл збірки як data URL (для перегляду
// зображень у провіднику). Той самий контракт local-file://, що GetPackAsset.
func (a *App) GetPackFileAsset(packID, relPath string) (string, error) {
	gameDir, err := a.packGameDir(packID)
	if err != nil {
		return "", err
	}
	abs, err := resolvePackPath(gameDir, relPath)
	if err != nil {
		return "", err
	}
	if st, err := os.Stat(abs); err != nil || st.IsDir() {
		return "", fmt.Errorf("файл не знайдено: %s", relPath)
	}
	return localFileDataURL(abs)
}
