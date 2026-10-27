package main

// notify.go — сповіщення лаунчера, коли вікно неактивне/згорнуте в трей.
//
// Раніше тут запускався прихований `powershell.exe -WindowStyle Hidden`, що
// показував WinRT-тост. Прихований PowerShell із GUI-процесу — класична
// поведінкова ознака шкідників для антивірусних ML-евристик (і причина
// хибних «Trojan/Stealer» вердиктів), тому тепер NotifyOS показує ВБУДОВАНИЙ
// віджет сповіщень лаунчера (notify_window.go + NotifyPopup.svelte): без
// сторонніх процесів, з тими ж налаштуваннями «Сповіщення»/«Не турбувати».

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Дебаунс: ретраї качки/повторні installer:log для однієї й тієї ж помилки
// можуть слати кілька NotifyOS підряд — не спамимо користувача.
var (
	notifyMu     sync.Mutex
	lastNotifyAt time.Time
)

// Мінімальний інтервал між двома системними сповіщеннями.
const notifyMinInterval = 6 * time.Second

// NotifyOS показує сповіщення лаунчера (віджет у правому нижньому куті).
// На не-Windows платформах — no-op, як і раніше. Поважає налаштування
// «Сповіщення» / «Не турбувати».
func (a *App) NotifyOS(title, message string) error {
	if runtime.GOOS != "windows" {
		return nil
	}
	title = strings.TrimSpace(title)
	message = strings.TrimSpace(message)
	if title == "" && message == "" {
		return nil
	}
	if title == "" {
		title = "Shaurma Launcher"
	}
	if message == "" {
		message = title
	}
	if a.cfg != nil && !a.notificationsAllowed() {
		return nil
	}

	// Дебаунс: пропускаємо, якщо попереднє було менш ніж N секунд тому.
	notifyMu.Lock()
	if time.Since(lastNotifyAt) < notifyMinInterval {
		notifyMu.Unlock()
		return nil
	}
	lastNotifyAt = time.Now()
	notifyMu.Unlock()

	// Kind "info" — службове сповіщення без співрозмовника (клік лише ховає
	// картку, див. NotifyPopup.svelte). pushNotify обрізає title/body.
	a.pushNotify(NotifyPayload{Kind: "info", Title: title, Body: message})
	return nil
}

// execPath кешує os.Executable(): це дорогий syscall, а рахуємо один раз.
var execPath = sync.OnceValue(func() string {
	if p, err := os.Executable(); err == nil {
		return filepath.Clean(p)
	}
	return "."
})
