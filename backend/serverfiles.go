package main

// ── Файловий провідник сервера ──────────────────────────────────────────
// Тонкі Wails-обгортки для файлового менеджера сервера — корінь = тека
// самого сервера (srv.Dir), без підтеки .minecraft. Вся бізнес-логіка
// вже в filebrowser.go (спільна з провідником збірок).

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// serverRootDir повертає абсолютний шлях до кореневої теки сервера.
func (a *App) serverRootDir(serverID string) (string, error) {
	srv, err := a.srvAPI.GetServer(serverID)
	if err != nil {
		return "", fmt.Errorf("сервер не знайдено: %s", serverID)
	}
	return srv.Dir, nil
}

// isServerRunning перевіряє, чи запущений сервер (для захисту файлів).
func (a *App) isServerRunning(serverID string) bool {
	return a.srvAPI.Manager().IsRunning(serverID)
}

// protectedServerFiles — файли, які не можна редагувати/видаляти/
// перейменовувати поки сервер запущений (процес може тримати лок).
// Відносні шляхи (порівняння через strings.HasSuffix + перевірка
// співпадіння шляху на рівні сегментів).
var protectedServerFiles = []string{
	"server.properties",
	"eula.txt",
	"server.jar",
	"session.lock",
}

// isProtectedPath перевіряє, чи шлях відноситься до захищених файлів
// або знаходиться в захищених теках (worlds/, logs/).
func isProtectedPath(relPath string) bool {
	lower := strings.ToLower(filepath.ToSlash(relPath))
	for _, pf := range protectedServerFiles {
		if lower == pf || strings.HasSuffix(lower, "/"+pf) {
			return true
		}
	}
	// Теки, які можуть бути заблоковані процесом сервера.
	protectedDirs := []string{"worlds/", "logs/", "world/", "world_nether/", "world_the_end/"}
	for _, pd := range protectedDirs {
		if strings.HasPrefix(lower, pd) {
			return true
		}
	}
	return false
}

// checkServerRunning захищає від змін файлів запущеного сервера.
// Повертає помилку, якщо сервер запущений і файл належить до захищених.
func (a *App) checkServerRunning(serverID, relPath string) error {
	if a.isServerRunning(serverID) && isProtectedPath(relPath) {
		return fmt.Errorf("зупиніть сервер перед редагуванням файлів, які використовуються процесом")
	}
	return nil
}

// ListServerFiles повертає вміст теки сервера.
func (a *App) ListServerFiles(serverID, relPath string) (DirListing, error) {
	root, err := a.serverRootDir(serverID)
	if err != nil {
		return DirListing{}, err
	}
	return listDir(root, relPath, serverID)
}

// ReadServerTextFile читає файл сервера.
func (a *App) ReadServerTextFile(serverID, relPath string) (FileContent, error) {
	root, err := a.serverRootDir(serverID)
	if err != nil {
		return FileContent{}, err
	}
	return readTextFile(root, relPath)
}

// WriteServerTextFile зберігає відредагований текстовий файл сервера.
func (a *App) WriteServerTextFile(serverID, relPath, content string) error {
	root, err := a.serverRootDir(serverID)
	if err != nil {
		return err
	}
	if err := a.checkServerRunning(serverID, relPath); err != nil {
		return err
	}
	return writeTextFile(root, relPath, content)
}

// RenameServerEntry перейменовує файл/теку сервера.
func (a *App) RenameServerEntry(serverID, relPath, newName string) error {
	root, err := a.serverRootDir(serverID)
	if err != nil {
		return err
	}
	if err := a.checkServerRunning(serverID, relPath); err != nil {
		return err
	}
	return renameEntry(root, relPath, newName)
}

// CreateServerFolder створює теку в теці сервера.
func (a *App) CreateServerFolder(serverID, relPath, name string) error {
	root, err := a.serverRootDir(serverID)
	if err != nil {
		return err
	}
	return createFolder(root, relPath, name)
}

// CreateServerFile створює ПОРОЖНІЙ файл у теці сервера.
func (a *App) CreateServerFile(serverID, relPath, name string) error {
	root, err := a.serverRootDir(serverID)
	if err != nil {
		return err
	}
	return createFile(root, relPath, name)
}

// DeleteServerEntry видаляє файл/теку сервера.
func (a *App) DeleteServerEntry(serverID, relPath string) error {
	root, err := a.serverRootDir(serverID)
	if err != nil {
		return err
	}
	if err := a.checkServerRunning(serverID, relPath); err != nil {
		return err
	}
	return deleteEntry(root, relPath)
}

// RevealServerFile відкриває Провідник Windows з виділеним файлом сервера.
func (a *App) RevealServerFile(serverID, relPath string) error {
	root, err := a.serverRootDir(serverID)
	if err != nil {
		return err
	}
	abs, err := resolveRelPath(root, relPath)
	if err != nil {
		return err
	}
	if _, err := os.Stat(abs); err != nil {
		return fmt.Errorf("елемент не знайдено: %s", relPath)
	}
	return exec.Command("explorer.exe", "/select,"+abs).Start()
}

// GetServerFileAsset повертає файл сервера як data URL (для перегляду
// зображень у провіднику).
func (a *App) GetServerFileAsset(serverID, relPath string) (string, error) {
	root, err := a.serverRootDir(serverID)
	if err != nil {
		return "", err
	}
	abs, err := resolveRelPath(root, relPath)
	if err != nil {
		return "", err
	}
	if st, err := os.Stat(abs); err != nil || st.IsDir() {
		return "", fmt.Errorf("файл не знайдено: %s", relPath)
	}
	return localFileDataURL(abs)
}
