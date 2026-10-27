package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ── Міграція папки даних з прогресом та crash recovery ────────────────────

// migrationPendingFile — файл-маркер незавершеної міграції.
// Якщо лаунчер крашнувся під час перенесення, при наступному запуску
// він знайде цей файл і запропонує/продовжить міграцію.
const migrationPendingFile = ".migration-pending"

// MigrationState — стан міграції для crash recovery.
type MigrationState struct {
	OldDir      string   `json:"oldDir"`
	NewDir      string   `json:"newDir"`
	CurrentSub  string   `json:"currentSub"`
	CurrentFile string   `json:"currentFile"`
	TotalFiles  int      `json:"totalFiles"`
	DoneFiles   int      `json:"doneFiles"`
	TotalBytes  int64    `json:"totalBytes"`
	DoneBytes   int64    `json:"doneBytes"`
	StartedAt   string   `json:"startedAt"`
	CompletedAt string   `json:"completedAt,omitempty"`
	Error       string   `json:"error,omitempty"`
	SubsLeft    []string `json:"subsLeft"`
}

// MigrationProgress — подія прогресу для фронтенду.
type MigrationProgress struct {
	Stage      string  `json:"stage"`   // "preparing" | "copying" | "cleanup" | "done" | "error"
	Current    string  `json:"current"` // поточна тека/файл
	FilesDone  int     `json:"filesDone"`
	FilesTotal int     `json:"filesTotal"`
	BytesDone  int64   `json:"bytesDone"`
	BytesTotal int64   `json:"bytesTotal"`
	Percent    float64 `json:"percent"`
	Error      string  `json:"error,omitempty"`
}

// migrationMu захищає від одночасної міграції.
var migrationMu sync.Mutex

// CheckPendingMigration перевіряє чи є незавершена міграція (crash recovery).
// Викликається при старті лаунчера.
func (a *App) CheckPendingMigration() *MigrationState {
	pendingPath := filepath.Join(a.cfg.Dir(), migrationPendingFile)
	raw, err := os.ReadFile(pendingPath)
	if err != nil {
		return nil
	}
	var state MigrationState
	if json.Unmarshal(raw, &state) != nil {
		return nil
	}
	if state.OldDir == "" || state.NewDir == "" {
		return nil
	}
	return &state
}

// SaveMigrationState зберігає стан міграції на диск.
func (a *App) SaveMigrationState(state *MigrationState) {
	pendingPath := filepath.Join(a.cfg.Dir(), migrationPendingFile)
	data, _ := json.MarshalIndent(state, "", "  ")
	_ = os.WriteFile(pendingPath, data, 0644)
}

// RemoveMigrationMarker видаляє файл-маркер після завершення міграції.
func (a *App) RemoveMigrationMarker() {
	_ = os.Remove(filepath.Join(a.cfg.Dir(), migrationPendingFile))
}

// SetAppDir змінює папку даних лаунчера з ПОВНИМ перенесенням даних
// та візуальним прогрес-баром. Не дозволяє закрити лаунчер поки
// перенесення не завершиться. При краші — продовжує при наступному старті.
func (a *App) SetAppDir(dir string) error {
	if err := a.assertFoldersChangeable(); err != nil {
		return err
	}
	if filepath.Base(dir) != ".shaurm" {
		dir = filepath.Join(dir, ".shaurm")
	}
	oldDir := a.cfg.Dir()
	if dir == "" {
		return fmt.Errorf("порожня папка даних")
	}
	if samePath(oldDir, dir) {
		return nil // нічого змінювати
	}

	if !migrationMu.TryLock() {
		return fmt.Errorf("міграція вже виконується")
	}
	defer migrationMu.Unlock()

	// Емітуємо "блокувати закриття"
	a.emit("migration:block-close", true)

	// Виконуємо міграцію з прогресом
	if err := a.migrateDataDirWithProgress(oldDir, dir); err != nil {
		a.emit("migration:progress", MigrationProgress{
			Stage: "error",
			Error: err.Error(),
		})
		a.emit("migration:block-close", false)
		return fmt.Errorf("перенесення папки даних: %w", err)
	}

	// Файли переїхали — ремапимо асети
	if a.customPacks != nil {
		if err := a.customPacks.RemapAssetPaths(oldDir, dir); err != nil {
			log.Printf("SetAppDir: RemapAssetPaths %s → %s: %v", oldDir, dir, err)
		}
	}

	// Записуємо новий шлях
	if err := a.cfg.SetAppDir(dir); err != nil {
		a.emit("migration:block-close", false)
		return err
	}
	a.rewireComponents()
	a.applyDownloadLimits()

	// Завершення
	a.RemoveMigrationMarker()
	a.emit("migration:progress", MigrationProgress{
		Stage:   "done",
		Current: dir,
	})
	a.emit("migration:block-close", false)

	// Лаунчер має перезапуститись для повного оновлення кешів
	a.emit("migration:restart-required", dir)

	return nil
}

// ResumeMigration продовжує незавершену міграцію після крашу.
func (a *App) ResumeMigration() error {
	state := a.CheckPendingMigration()
	if state == nil {
		return nil // нема чого відновлювати
	}

	if !migrationMu.TryLock() {
		return fmt.Errorf("міграція вже виконується")
	}
	defer migrationMu.Unlock()

	a.emit("migration:block-close", true)

	// Перевіряємо що теки ще існують
	if _, err := os.Stat(state.OldDir); os.IsNotExist(err) {
		// Стара тека вже видалена — міграція фактично завершена
		a.RemoveMigrationMarker()
		a.emit("migration:block-close", false)
		return nil
	}

	if err := a.migrateDataDirResume(state); err != nil {
		a.emit("migration:progress", MigrationProgress{
			Stage: "error",
			Error: err.Error(),
		})
		a.emit("migration:block-close", false)
		return err
	}

	// Ремапимо асети
	if a.customPacks != nil {
		if err := a.customPacks.RemapAssetPaths(state.OldDir, state.NewDir); err != nil {
			log.Printf("ResumeMigration: RemapAssetPaths: %v", err)
		}
	}

	// Записуємо новий шлях
	if err := a.cfg.SetAppDir(state.NewDir); err != nil {
		a.emit("migration:block-close", false)
		return err
	}
	a.rewireComponents()
	a.applyDownloadLimits()

	a.RemoveMigrationMarker()
	a.emit("migration:progress", MigrationProgress{Stage: "done", Current: state.NewDir})
	a.emit("migration:block-close", false)
	a.emit("migration:restart-required", state.NewDir)
	return nil
}

// migrateDataDirWithProgress переносить дані з прогресом та crash recovery.
// moveSubs — теки з юзерськими даними, які ПЕРЕНОСЯТЬСЯ (єдині копія).
var moveSubs = []string{"installations", "skins", "skin-snapshots"}

// deleteSubs — теки з кешованими/перекачуваними даними, які ВИДАЛЯЮТЬСЯ
// (automatic re-download: minecraft libs/assets, java runtime, temp cache).
var deleteSubs = []string{"minecraft", "java", "cache", "fonts"}

func (a *App) migrateDataDirWithProgress(oldDir, newDir string) error {
	allSubs := append(moveSubs, deleteSubs...)

	// Підрахунок файлів для прогресу (лише move-суби)
	a.emit("migration:progress", MigrationProgress{Stage: "preparing", Current: "Підрахунок файлів..."})
	totalFiles, totalBytes := a.countMigrationFiles(oldDir, moveSubs)

	state := &MigrationState{
		OldDir:     oldDir,
		NewDir:     newDir,
		TotalFiles: totalFiles,
		TotalBytes: totalBytes,
		StartedAt:  time.Now().Format(time.RFC3339),
		SubsLeft:   allSubs,
	}
	a.SaveMigrationState(state)
	return a.executeMigration(state)
}

// migrateDataDirResume продовжує міграцію з місця зупинки.
func (a *App) migrateDataDirResume(state *MigrationState) error {
	return a.executeMigration(state)
}

// executeMigration виконує саму міграцію файлів з прогресом.
func (a *App) executeMigration(state *MigrationState) error {
	doneFiles := state.DoneFiles
	doneBytes := state.DoneBytes

	for len(state.SubsLeft) > 0 {
		sub := state.SubsLeft[0]
		state.SubsLeft = state.SubsLeft[1:]
		state.CurrentSub = sub

		src := filepath.Join(state.OldDir, sub)
		dst := filepath.Join(state.NewDir, sub)

		if _, err := os.Stat(src); os.IsNotExist(err) {
			state.DoneFiles = doneFiles
			state.DoneBytes = doneBytes
			a.SaveMigrationState(state)
			continue
		}

		isDelete := false
		for _, ds := range deleteSubs {
			if sub == ds {
				isDelete = true
				break
			}
		}

		if isDelete {
			// Перекачується автоматично — просто видаляємо
			a.emit("migration:progress", MigrationProgress{
				Stage:   "delete",
				Current: sub,
				Percent: 0,
			})
			os.RemoveAll(src)
			log.Printf("migration: видалено кешову теку %s (перекачується автоматично)", src)
		} else {
			// Юзерські дані — переносимо
			if err := a.moveTreeWithProgress(src, dst, state, &doneFiles, &doneBytes); err != nil {
				state.Error = err.Error()
				a.SaveMigrationState(state)
				return fmt.Errorf("перенесення %s: %w", sub, err)
			}
		}

		state.DoneFiles = doneFiles
		state.DoneBytes = doneBytes
		a.SaveMigrationState(state)
	}

	// Все перенеслося — прибираємо стару папку
	state.CurrentSub = "Очищення..."
	a.emit("migration:progress", MigrationProgress{
		Stage:      "cleanup",
		Current:    "Видалення старої папки...",
		FilesDone:  doneFiles,
		FilesTotal: state.TotalFiles,
		Percent:    100,
	})

	if rest, _ := os.ReadDir(state.OldDir); len(rest) == 0 {
		_ = os.RemoveAll(state.OldDir)
		log.Printf("SetAppDir: стара папка видалена: %s", state.OldDir)
	}

	state.CompletedAt = time.Now().Format(time.RFC3339)
	a.SaveMigrationState(state)
	return nil
}

// moveTreeWithProgress рекурсивно переносить теку з емітом прогресу.
func (a *App) moveTreeWithProgress(src, dst string, state *MigrationState, doneFiles *int, doneBytes *int64) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}

	var firstErr error
	for _, e := range entries {
		from := filepath.Join(src, e.Name())
		to := filepath.Join(dst, e.Name())

		if e.IsDir() {
			if err := a.moveTreeWithProgress(from, to, state, doneFiles, doneBytes); err != nil && firstErr == nil {
				firstErr = err
			}
			continue
		}

		if fileExists(to) {
			*doneFiles++
			continue
		}

		// Копіюємо з прогресом
		info, _ := os.Stat(from)
		fileSize := int64(0)
		if info != nil {
			fileSize = info.Size()
		}

		if err := copyFileWithProgress(from, to, fileSize, state, doneFiles, doneBytes); err != nil && firstErr == nil {
			firstErr = err
		}

		// Еміт прогресу кожні 50 мс
		percent := float64(0)
		if state.TotalFiles > 0 {
			percent = float64(*doneFiles) / float64(state.TotalFiles) * 100
		}
		a.emit("migration:progress", MigrationProgress{
			Stage:      "copying",
			Current:    from,
			FilesDone:  *doneFiles,
			FilesTotal: state.TotalFiles,
			BytesDone:  *doneBytes,
			BytesTotal: state.TotalBytes,
			Percent:    percent,
		})
	}

	// Прибираємо порожню теку-джерело
	if firstErr == nil {
		if rest, _ := os.ReadDir(src); len(rest) == 0 {
			_ = os.RemoveAll(src)
		}
	}
	return firstErr
}

// copyFileWithProgress копіює файл з розрахунком прогресу.
// Запис через .tmp + rename для атомарності.
func copyFileWithProgress(src, dst string, fileSize int64, state *MigrationState, doneFiles *int, doneBytes *int64) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	tmpPath := dst + ".tmp"
	out, err := os.Create(tmpPath)
	if err != nil {
		return err
	}

	written, err := io.Copy(out, in)
	out.Close()
	*doneBytes += written

	if err != nil {
		os.Remove(tmpPath)
		return err
	}

	// Атомарний rename
	if err := os.Rename(tmpPath, dst); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}

// countMigrationFiles рахує загальну кількість файлів та байт.
func (a *App) countMigrationFiles(baseDir string, subs []string) (int, int64) {
	var totalFiles int
	var totalBytes int64
	for _, sub := range subs {
		dir := filepath.Join(baseDir, sub)
		filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			info, err := d.Info()
			if err == nil {
				totalBytes += info.Size()
			}
			totalFiles++
			return nil
		})
	}
	return totalFiles, totalBytes
}

// assertFoldersChangeable — захист від зміни тек під час запущеної збірки.
func (a *App) assertFoldersChangeable() error {
	if a.sessionsM.AnyRunning() {
		return fmt.Errorf("не можна змінювати теки, поки запущена збірка — спершу завершіть гру")
	}
	return nil
}

// RestartLauncher перезапускає лаунчер (емітує подію фронтенду).

func (a *App) RestartLauncher() {
	a.emit("migration:restart", nil)
}

// IsMigrating повертає true якщо міграція виконується.
func (a *App) IsMigrating() bool {
	if migrationMu.TryLock() {
		migrationMu.Unlock()
		return false // mutex був вільний → міграція НЕ виконується
	}
	return true // mutex заблокований → міграція виконується
}
