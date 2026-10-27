package main

// launcher_errors.go — тихий кільцевий буфер помилок лаунчера.
//
// Всі внутрішні помилки (catch у фронтенді → AppendLauncherError,
// бекендові помилки → a.logError) пишуться сюди без показу юзеру.
// Буфер тримає останні maxErrorEntries записів (нові витісняють старі).
// Юзер може будь-коли викликати ExportLauncherErrors — отримає діалог
// «Зберегти файл» і JSON з усіма накопиченими помилками.
//
// Структура запису:
//   {
//     "time": "2026-08-13T23:47:00Z",
//     "source": "cleanupUnusedVersions",   // звідки (кнопка/метод — контекст)
//     "message": "видалення версії: ...",  // суть
//     "detail": "full stack / raw error"  // опціонально
//   }
//
// Журнал ЛИШЕ локальний: нічого не надсилається в мережу (ні на worker, ні на
// VPS). Користувач сам вирішує, чи ділитися файлом (ExportLauncherErrors).

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const maxErrorEntries = 500

// LauncherErrorEntry — один запис у журналі помилок.
type LauncherErrorEntry struct {
	Time    string `json:"time"`
	Source  string `json:"source"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
}

var (
	errBufMu sync.Mutex
	errBuf   = make([]LauncherErrorEntry, 0, maxErrorEntries)
)

// logError — внутрішній хелпер для бекенду: тихо пише помилку в буфер.
// Використовуй так: a.logError("cleanupUnusedVersions", err)
func (a *App) logError(source string, err error) {
	if err == nil {
		return
	}
	appendError(source, err.Error(), "")
}

// logErrorDetail — як logError, але з додатковим детальним текстом.
func (a *App) logErrorDetail(source, message, detail string) {
	appendError(source, message, detail)
}

func appendError(source, message, detail string) {
	e := LauncherErrorEntry{
		Time:    time.Now().UTC().Format(time.RFC3339),
		Source:  source,
		Message: message,
		Detail:  detail,
	}
	errBufMu.Lock()
	defer errBufMu.Unlock()
	if len(errBuf) >= maxErrorEntries {
		// витісняємо найстаріший запис (зрушуємо зліва)
		copy(errBuf, errBuf[1:])
		errBuf[len(errBuf)-1] = e
	} else {
		errBuf = append(errBuf, e)
	}
}

// AppendLauncherError — публічний метод для фронтенду:
// JS catch-блоки передають сюди помилки, не показуючи юзеру нічого зайвого.
func (a *App) AppendLauncherError(source, message, detail string) {
	appendError(source, message, detail)
}

// GetLauncherErrors повертає всі накопичені помилки (для дебагу з фронтенду,
// якщо колись знадобиться показати кількість у UI).
func (a *App) GetLauncherErrors() []LauncherErrorEntry {
	errBufMu.Lock()
	defer errBufMu.Unlock()
	out := make([]LauncherErrorEntry, len(errBuf))
	copy(out, errBuf)
	return out
}

// GetLauncherErrorCount повертає кількість записаних помилок.
func (a *App) GetLauncherErrorCount() int {
	errBufMu.Lock()
	defer errBufMu.Unlock()
	return len(errBuf)
}

// ExportLauncherErrors відкриває діалог «Зберегти файл» і записує JSON
// з усіма помилками. Повертає шлях до збереженого файлу або "" якщо
// скасовано. Формат JSON — масив об'єктів, human-readable (відступи 2 пробели).
func (a *App) ExportLauncherErrors() (string, error) {
	errBufMu.Lock()
	snapshot := make([]LauncherErrorEntry, len(errBuf))
	copy(snapshot, errBuf)
	errBufMu.Unlock()

	defaultName := fmt.Sprintf("shaurma-errors-%s.json", time.Now().Format("2006-01-02_15-04-05"))

	path := bridgeDialogSaveFile("Зберегти журнал помилок лаунчера", defaultName, map[string][]string{
		"JSON (*.json)": {"json"},
	})
	if path == "" {
		return "", nil // скасовано — не помилка
	}
	if !strings.HasSuffix(strings.ToLower(path), ".json") {
		path += ".json"
	}

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return "", fmt.Errorf("серіалізація: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", err
	}
	return path, nil
}

// AutoSaveLauncherErrorLog автоматично зберігає журнал помилок у теку логів
// при закритті лаунчера (викликається з shutdown). Файл
// перезаписується при кожному запуску, щоб не копичитись роками.
func (a *App) AutoSaveLauncherErrorLog() {
	errBufMu.Lock()
	if len(errBuf) == 0 {
		errBufMu.Unlock()
		return
	}
	snapshot := make([]LauncherErrorEntry, len(errBuf))
	copy(snapshot, errBuf)
	errBufMu.Unlock()

	logsDir := a.cfg.LogsDir()
	_ = os.MkdirAll(logsDir, 0755)
	path := filepath.Join(logsDir, "launcher-errors.json")

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0644)
}
