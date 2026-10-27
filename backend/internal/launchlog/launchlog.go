// Package launchlog — структурований лог лаунчера у стилі:
//
//	[2026-08-13 23:35:13.196] [info]  (App)                         Starting (pid 1288)
//
// Формат: [дата час.мс] [рівень] (компонент)                       повідомлення
//
// Особливості:
//   - Пише одночасно у файл (logs/launcher.log) і stderr
//   - Ротація при старті: launcher.log → .1 → .2 → .3 (старі видаляються)
//   - Рівні: debug / info / warn / error
//   - Thread-safe (один mutex на всіх)
//   - Глобальний singleton: Init один раз, далі Info/Warn/Error/Debug
package launchlog

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Level — рівень логування.
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

func (l Level) tag() string {
	switch l {
	case LevelDebug:
		return "debug"
	case LevelInfo:
		return "info "
	case LevelWarn:
		return "warn "
	case LevelError:
		return "error"
	}
	return "info "
}

var (
	mu       sync.Mutex
	writers  []io.Writer
	logFile  *os.File
	minLevel = LevelDebug
)

// Init ініціалізує глобальний логер.
// logsDir — тека де зберігати launcher.log.
// level — мінімальний рівень (LevelDebug = все).
func Init(logsDir string, level Level) error {
	mu.Lock()
	defer mu.Unlock()

	minLevel = level

	if err := os.MkdirAll(logsDir, 0755); err != nil {
		writers = []io.Writer{os.Stderr}
		return fmt.Errorf("launchlog: mkdir %s: %w", logsDir, err)
	}

	logPath := filepath.Join(logsDir, "launcher.log")

	// Ротація: .log → .log.1 → .log.2 → .log.3 (максимум 3 старих)
	for i := 2; i >= 1; i-- {
		src := fmt.Sprintf("%s.%d", logPath, i)
		dst := fmt.Sprintf("%s.%d", logPath, i+1)
		_ = os.Remove(dst)
		_ = os.Rename(src, dst)
	}
	_ = os.Rename(logPath, logPath+".1")

	// Закриваємо попередній файл якщо є
	if logFile != nil {
		_ = logFile.Close()
		logFile = nil
	}

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		writers = []io.Writer{os.Stderr}
		return fmt.Errorf("launchlog: open %s: %w", logPath, err)
	}
	logFile = f
	writers = []io.Writer{os.Stderr, f}
	return nil
}

// Close закриває файл логу (викликати при shutdown).
func Close() {
	mu.Lock()
	defer mu.Unlock()
	if logFile != nil {
		_ = logFile.Close()
		logFile = nil
	}
	writers = nil
}

// SetLevel змінює мінімальний рівень після Init.
func SetLevel(level Level) {
	mu.Lock()
	minLevel = level
	mu.Unlock()
}

// write — єдина точка запису рядка.
func write(level Level, component, msg string) {
	mu.Lock()
	defer mu.Unlock()
	if level < minLevel || len(writers) == 0 {
		return
	}
	now := time.Now()
	ms := now.UnixMilli() % 1000
	ts := now.Format("2006-01-02 15:04:05") + fmt.Sprintf(".%03d", ms)
	// Вирівнюємо компонент у дужках до 30 символів (як у прикладі)
	comp := "(" + component + ")"
	if len(comp) < 30 {
		comp = comp + strings.Repeat(" ", 30-len(comp))
	}
	line := fmt.Sprintf("[%s] [%s] %s %s\n", ts, level.tag(), comp, msg)
	for _, w := range writers {
		_, _ = io.WriteString(w, line)
	}
}

// --- Logger — логер конкретного компонента ---

// Logger прив'язаний до одного компонента (назви в дужках).
type Logger struct {
	component string
}

// Component повертає Logger для зазначеного компонента.
func Component(name string) *Logger { return &Logger{component: name} }

func (l *Logger) Debug(msg string, args ...any) {
	write(LevelDebug, l.component, fmt.Sprintf(msg, args...))
}
func (l *Logger) Info(msg string, args ...any) {
	write(LevelInfo, l.component, fmt.Sprintf(msg, args...))
}
func (l *Logger) Warn(msg string, args ...any) {
	write(LevelWarn, l.component, fmt.Sprintf(msg, args...))
}
func (l *Logger) Error(msg string, args ...any) {
	write(LevelError, l.component, fmt.Sprintf(msg, args...))
}

// --- Глобальні хелпери для швидкого логування ---

func Debug(component, msg string, args ...any) {
	write(LevelDebug, component, fmt.Sprintf(msg, args...))
}
func Info(component, msg string, args ...any) {
	write(LevelInfo, component, fmt.Sprintf(msg, args...))
}
func Warn(component, msg string, args ...any) {
	write(LevelWarn, component, fmt.Sprintf(msg, args...))
}
func Error(component, msg string, args ...any) {
	write(LevelError, component, fmt.Sprintf(msg, args...))
}

// --- CallerComponent — автовизначення компонента зі стеку ---

// CallerComponent повертає Logger, де ім'я компонента береться автоматично
// з назви пакету caller'а. Зручно для library-коду.
//
// Приклад: у пакеті internal/auth → компонент "Auth".
func CallerComponent() *Logger {
	pc, _, _, ok := runtime.Caller(1)
	if !ok {
		return Component("Unknown")
	}
	fn := runtime.FuncForPC(pc)
	if fn == nil {
		return Component("Unknown")
	}
	// "shaurma-launcher-backend/internal/auth.(*Authenticator).Login" → "auth"
	name := fn.Name()
	parts := strings.Split(name, "/")
	last := parts[len(parts)-1]
	pkg := strings.SplitN(last, ".", 2)[0]
	switch pkg {
	case "main":
		pkg = "App"
	case "":
		pkg = "Unknown"
	default:
		// capitalize
		pkg = strings.ToUpper(pkg[:1]) + pkg[1:]
	}
	return Component(pkg)
}
