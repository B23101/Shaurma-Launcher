// Package console — кільцевий буфер виводу гри.
//
// Головна вимога розумної консолі: гра пише логи НЕЗАЛЕЖНО від того,
// відкрите вікно консолі чи ні. Тому буфер живе на бекенді (Go), а не у
// фронтенді: SetOutputHandler пише рядок у буфер завжди, а фронтенд лише
// підписується на подію console:line (живий потік) і при відкритті вікна
// отримує знімок буфера (GetConsoleSnapshot) — все, що записалось, поки
// консоль була закрита, не губиться.
//
// Буфер обмежений РЯДКАМИ (налаштування consoleMaxLines, як у Prism):
// старі рядки відкидаються з голови, коли їх кількість перевищує ліміт.
// Так консоль витримує величезні сплески логів (наприклад 10 000 рядків
// за раз при краші) без заморожування — на противагу старому лаунчеру,
// де великий вивід "загальмовував" процес і доводилось його зупиняти й
// перезапускати.
package console

import (
	"regexp"
	"sync"
	"time"

	"shaurma-launcher-wails/internal/model"
)

// рівні логу, які розуміє консоль (color-coding у UI).
const (
	LevelInfo   = "info"
	LevelWarn   = "warn"
	LevelError  = "error"
	LevelSystem = "system"
)

var (
	reError = regexp.MustCompile(`(?i)(error|exception|fatal|crash|failed|unable to|outofmemory|\bsevere\b|\[stderr\]|\[error\]|\bthrowable\b)`)
	reWarn  = regexp.MustCompile(`(?i)(warn|warning|can't keep up|\bcareful\b)`)
)

// DetectLevel визначає рівень рядка за текстом. Порядок важливий: спершу
// помилка, потім warning, інакше info.
func DetectLevel(text string) string {
	if reError.MatchString(text) {
		return LevelError
	}
	if reWarn.MatchString(text) {
		return LevelWarn
	}
	return LevelInfo
}

// Buffer — потоко-безпечний кільцевий буфер рядків логу з лімітом по
// кількості рядків. Append — O(1) у середньому (amortized), без жодного
// блокування поза м'ютексом.
type Buffer struct {
	mu       sync.Mutex
	lines    []model.ConsoleLine
	maxLines int // consoleMaxLines з налаштувань
	nextID   uint64
}

// NewBuffer створює порожній буфер з лімітом рядків.
func NewBuffer(maxLines int) *Buffer {
	return &Buffer{maxLines: maxLines}
}

// SetMaxLines оновлює ліміт рядків (після зміни налаштування consoleMaxLines).
func (b *Buffer) SetMaxLines(n int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.maxLines = n
	b.trimLocked()
}

// Append додає рядок із автоматичним визначенням рівня. Повертає збережений
// рядок (з ID і часом), щоб бекенд міг розіслати його як подію.
func (b *Buffer) Append(text string) model.ConsoleLine {
	return b.AppendLevel(DetectLevel(text), text)
}

// AppendLevel додає рядок з явним рівнем (наприклад "system" для службових
// повідомлень лаунчера: запуск/зупинка/код виходу).
func (b *Buffer) AppendLevel(level, text string) model.ConsoleLine {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextID++
	line := model.ConsoleLine{
		ID:    b.nextID,
		Time:  time.Now().Format("15:04:05"),
		Level: level,
		Text:  text,
	}
	b.lines = append(b.lines, line)
	b.trimLocked()
	return line
}

// trimLocked відкидає найстаріші рядки, поки кількість не влізе у ліміт.
func (b *Buffer) trimLocked() {
	if b.maxLines <= 0 {
		return
	}
	if len(b.lines) > b.maxLines {
		b.lines = b.lines[len(b.lines)-b.maxLines:]
	}
}

// Snapshot повертає копію всіх рядків буфера (для відкриття/повторного
// відкриття вікна консолі).
func (b *Buffer) Snapshot() []model.ConsoleLine {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]model.ConsoleLine, len(b.lines))
	copy(out, b.lines)
	return out
}

// Recent повертає останні n рядків (для аналізу крашу).
func (b *Buffer) Recent(n int) []model.ConsoleLine {
	b.mu.Lock()
	defer b.mu.Unlock()
	if n <= 0 || len(b.lines) == 0 {
		return []model.ConsoleLine{}
	}
	if len(b.lines) <= n {
		out := make([]model.ConsoleLine, len(b.lines))
		copy(out, b.lines)
		return out
	}
	out := make([]model.ConsoleLine, n)
	copy(out, b.lines[len(b.lines)-n:])
	return out
}

// Clear очищає буфер (кнопка «Очистити консоль»).
func (b *Buffer) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lines = nil
	b.nextID = 0
}
