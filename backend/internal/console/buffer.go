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
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"shaurma-launcher-backend/internal/model"
)

// рівні логу, які розуміє консоль (color-coding у UI).
const (
	LevelInfo   = "info"
	LevelWarn   = "warn"
	LevelError  = "error"
	LevelSystem = "system"
	// LevelBanner — ASCII-арт банер запуску: у UI малюється БЕЗ часу й бейджа
	// рівня, суворо моноширинним шрифтом і щільними рядками, інакше блоки
	// «█» розсипаються.
	LevelBanner = "banner"
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
	mu        sync.Mutex
	lines     []model.ConsoleLine
	maxLines  int // consoleMaxLines з налаштувань
	nextID    uint64
	textPool  map[string]string
	textOrder []string
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
	line := b.appendLocked(level, text)
	// Спостерігач — ПОЗА мьютексом буфера і лише для рядків гри (не службових
	// повідомлень лаунчера). Помилка в ньому не має ламати консоль.
	if level != LevelSystem && level != LevelBanner {
		if fn := lineObserver.Load(); fn != nil {
			func() {
				defer func() { _ = recover() }()
				(*fn)(text)
			}()
		}
	}
	return line
}

func (b *Buffer) appendLocked(level, text string) model.ConsoleLine {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextID++
	line := model.ConsoleLine{
		ID:    b.nextID,
		Time:  time.Now().Format("15:04:05"),
		Level: level,
		Text:  b.shareTextLocked(text),
	}
	b.lines = append(b.lines, line)
	b.trimLocked()
	return line
}

// lineObserver — необов'язковий глобальний спостерігач рядків гри (скін-проксі
// вчить за ним ніки гравців). Має бути ДЕШЕВИМ: викликається на кожен рядок.
var lineObserver atomic.Pointer[func(string)]

// SetLineObserver встановлює спостерігача рядків (nil — прибрати).
func SetLineObserver(fn func(string)) {
	if fn == nil {
		lineObserver.Store(nil)
		return
	}
	lineObserver.Store(&fn)
}

// trimLocked відкидає найстаріші рядки, поки кількість не влізе у ліміт.
func (b *Buffer) trimLocked() {
	if b.maxLines <= 0 {
		return
	}
	if len(b.lines) > b.maxLines {
		drop := len(b.lines) - b.maxLines
		clear(b.lines[:drop]) // release strings held by the discarded backing-array prefix
		b.lines = b.lines[drop:]
	}
}

// AppendRaw додає вже готовий ConsoleLine у буфер (зі збереженням його ID
// і часу). Використовується для буферизації серверних логів, де рядок
// вже має правильні ID/Time і не потребує перестворення.
func (b *Buffer) AppendRaw(line model.ConsoleLine) {
	b.mu.Lock()
	defer b.mu.Unlock()
	line.Text = b.shareTextLocked(line.Text)
	b.lines = append(b.lines, line)
	b.trimLocked()
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
	b.textPool = nil
	b.textOrder = nil
}

// Page contains only the requested window; counts describe the complete buffer.
type Page struct {
	Lines    []model.ConsoleLine `json:"lines"`
	Offset   int                 `json:"offset"`
	Total    int                 `json:"total"`
	Count    int                 `json:"count"`
	Errors   int                 `json:"errors"`
	Warnings int                 `json:"warnings"`
	LastID   uint64              `json:"lastId"`
}

// Page filters on the backend without copying the full log into the WebView.
// A negative offset requests the tail. after hides rows cleared in a view.
func (b *Buffer) Page(offset, limit int, level, query string, after uint64) Page {
	b.mu.Lock()
	defer b.mu.Unlock()
	if limit < 1 {
		limit = 200
	}
	if limit > 500 {
		limit = 500
	}
	p := Page{Lines: make([]model.ConsoleLine, 0, limit)}
	query = strings.ToLower(query)
	matches := func(l model.ConsoleLine) bool {
		return l.ID > after && (level == "" || level == "all" || l.Level == level) && (query == "" || strings.Contains(strings.ToLower(l.Text), query))
	}
	for _, l := range b.lines {
		if l.ID > p.LastID {
			p.LastID = l.ID
		}
		if l.ID <= after {
			continue
		}
		p.Count++
		if l.Level == "error" {
			p.Errors++
		}
		if l.Level == "warn" {
			p.Warnings++
		}
		if matches(l) {
			p.Total++
		}
	}
	if offset < 0 {
		offset = p.Total - limit
	}
	if offset > p.Total-limit {
		offset = p.Total - limit
	}
	if offset < 0 {
		offset = 0
	}
	p.Offset = offset
	n := 0
	for _, l := range b.lines {
		if !matches(l) {
			continue
		}
		if n >= offset && len(p.Lines) < limit {
			p.Lines = append(p.Lines, l)
		}
		n++
		if len(p.Lines) == limit {
			break
		}
	}
	return p
}

// Share repeated recent messages by reference, with a small bounded dictionary.
// Arbitrarily long or unique logs cannot grow this cache without limit.
func (b *Buffer) shareTextLocked(text string) string {
	if len(text) > 4096 {
		return text
	}
	if v, ok := b.textPool[text]; ok {
		return v
	}
	if b.textPool == nil {
		b.textPool = make(map[string]string)
	}
	if len(b.textOrder) == 32 {
		delete(b.textPool, b.textOrder[0])
		copy(b.textOrder, b.textOrder[1:])
		b.textOrder = b.textOrder[:31]
	}
	b.textPool[text] = text
	b.textOrder = append(b.textOrder, text)
	return text
}
