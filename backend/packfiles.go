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
// (resolveRelPath) — вихід за межі теки збірки неможливий.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// ── Беквards-сумісність типів ────────────────────────────────────────────
// Старі назви (PackFileEntry/PackDirListing/PackFileContent) лишаються
// валидними типами через аліаси, щоб PackFilesPanel.svelte та інші
// виклики не потребували масштабного перейменування.

type PackFileEntry = FileEntry
type PackDirListing = DirListing
type PackFileContent = FileContent

// packGameDir повертає абсолютний шлях до .minecraft теки кастомної збірки.
func (a *App) packGameDir(packID string) (string, error) {
	cp, ok := a.customPacks.Get(packID)
	if !ok {
		return "", fmt.Errorf("кастомну збірку не знайдено: %s", packID)
	}
	return filepath.Join(a.cfg.GetSettings().InstanceDir, cp.ID, ".minecraft"), nil
}

// ListPackFiles повертає вміст теки .minecraft збірки (теки перші, потім
// файли за алфавітом).
func (a *App) ListPackFiles(packID, relPath string) (PackDirListing, error) {
	gameDir, err := a.packGameDir(packID)
	if err != nil {
		return PackDirListing{}, err
	}
	listing, err := listDir(gameDir, relPath, packID)
	if err != nil {
		return PackDirListing{}, err
	}
	// Повертаємо аলіас — DirListing з RootId замість PackID.
	// Фронтенд читає .packId (стара назва JSON-поля) через RootId.
	return listing, nil
}

// ReadPackTextFile читає файл збірки. Для нетекстових/завеликих файлів
// повертає Binary=true (фронтенд запропонує перегляд/інші дії, а не
// редагування).
func (a *App) ReadPackTextFile(packID, relPath string) (PackFileContent, error) {
	gameDir, err := a.packGameDir(packID)
	if err != nil {
		return PackFileContent{}, err
	}
	return readTextFile(gameDir, relPath)
}

// WritePackTextFile зберігає відредагований текстовий файл збірки
// (атомарно: тимчасовий файл + перейменування). Не дає перезаписати
// бінарний файл.
func (a *App) WritePackTextFile(packID, relPath, content string) error {
	gameDir, err := a.packGameDir(packID)
	if err != nil {
		return err
	}
	return writeTextFile(gameDir, relPath, content)
}

// RenamePackEntry перейменовує файл/теку збірки: перевірка імені за
// стандартами, анти-дублікат (регістро-незалежно), заборона виходу за
// межі теки.
func (a *App) RenamePackEntry(packID, relPath, newName string) error {
	gameDir, err := a.packGameDir(packID)
	if err != nil {
		return err
	}
	return renameEntry(gameDir, relPath, newName)
}

// CreatePackFolder створює теку в збірці.
func (a *App) CreatePackFolder(packID, relPath, name string) error {
	gameDir, err := a.packGameDir(packID)
	if err != nil {
		return err
	}
	return createFolder(gameDir, relPath, name)
}

// CreatePackFile створює ПОРОЖНІЙ файл у вказаній теці збірки (кнопка
// «Створити файл» у провіднику). Ім'я валідується тими самими правилами,
// що й теки (validateFileName) + анти-дублікат.
func (a *App) CreatePackFile(packID, relPath, name string) error {
	gameDir, err := a.packGameDir(packID)
	if err != nil {
		return err
	}
	return createFile(gameDir, relPath, name)
}

// DeletePackEntry видаляє файл/теку збірки (рекурсивно для теки).
func (a *App) DeletePackEntry(packID, relPath string) error {
	if err := a.shareLockErr("build", packID); err != nil {
		return err
	}
	gameDir, err := a.packGameDir(packID)
	if err != nil {
		return err
	}
	return deleteEntry(gameDir, relPath)
}

// RevealPackFile відкриває Провідник Windows з виділеним файлом/текою
// збірки (кнопка «Показати у теці»).
func (a *App) RevealPackFile(packID, relPath string) error {
	gameDir, err := a.packGameDir(packID)
	if err != nil {
		return err
	}
	abs, err := resolveRelPath(gameDir, relPath)
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
	abs, err := resolveRelPath(gameDir, relPath)
	if err != nil {
		return "", err
	}
	if st, err := os.Stat(abs); err != nil || st.IsDir() {
		return "", fmt.Errorf("файл не знайдено: %s", relPath)
	}
	return localFileDataURL(abs)
}

// ── Legacy: readTextFile з NUL-контролем (лишається для зворотної ────────
// сумісності, якщо є виклики де-небудь окрім packfiles.go).

func init() {
	// readTextFile/filebrowser.go вже має NUL-контроль.
	// Цей init — нагадування, що legacy readTextFile більше не потрібен.
}
