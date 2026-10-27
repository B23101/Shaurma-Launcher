// Package crashguard — перехоплення паніків горутин, фатальних помилок
// рантайму (crash.log) і маркер аварійного завершення попередньої сесії.
//
// Відновлено за API, який використовує backend:
//
//	crashguard.OnPanic = appendError   // func(source, message, detail string)
//	crashguard.Init(logsDir)
//	defer crashguard.Recover("where")
//	crashguard.Report("where", r, stack)
//	crashguard.MarkCleanExit()
package crashguard

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
	"time"
)

// OnPanic — необов'язковий хук (напр. запис у буфер помилок лаунчера).
var OnPanic func(source, message, detail string)

var (
	mu        sync.Mutex
	logsDir   string
	crashFile *os.File
)

const (
	markerName   = "session.running"
	crashLogName = "crash.log"
)

// Init викликається один раз на старті після ініціалізації логів.
// Якщо маркер попередньої сесії лишився — вона завершилась аварійно.
func Init(dir string) {
	mu.Lock()
	defer mu.Unlock()
	logsDir = dir
	if dir == "" {
		return
	}
	_ = os.MkdirAll(dir, 0o755)

	marker := filepath.Join(dir, markerName)
	if _, err := os.Stat(marker); err == nil {
		msg := "попередня сесія завершилась аварійно (маркер не прибрано)"
		log.Print("crashguard: " + msg)
		if OnPanic != nil {
			OnPanic("crashguard", msg, "")
		}
	}
	_ = os.WriteFile(marker, []byte(time.Now().UTC().Format(time.RFC3339)), 0o644)

	// Фатальні помилки рантайму (fatal error, не перехоплювані recover) → crash.log.
	if f, err := os.OpenFile(filepath.Join(dir, crashLogName),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		crashFile = f
		_ = debug.SetCrashOutput(f, debug.CrashOptions{})
	}
}

// MarkCleanExit прибирає маркер — звичайний вихід із програми.
func MarkCleanExit() {
	mu.Lock()
	defer mu.Unlock()
	if logsDir != "" {
		_ = os.Remove(filepath.Join(logsDir, markerName))
	}
}

// Recover має викликатись напряму через defer: defer crashguard.Recover("x").
func Recover(where string) {
	if r := recover(); r != nil {
		Report(where, r, debug.Stack())
	}
}

// Report логує паніку, дописує в crash.log і викликає OnPanic.
func Report(where string, r any, stack []byte) {
	msg := fmt.Sprintf("паніка в %s: %v", where, r)
	log.Printf("%s\n%s", msg, stack)

	mu.Lock()
	dir := logsDir
	mu.Unlock()
	if dir != "" {
		if f, err := os.OpenFile(filepath.Join(dir, crashLogName),
			os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
			fmt.Fprintf(f, "[%s] %s\n%s\n\n", time.Now().UTC().Format(time.RFC3339), msg, stack)
			_ = f.Close()
		}
	}
	if hook := OnPanic; hook != nil {
		func() {
			defer func() { _ = recover() }() // хук не має валити обробник паніки
			hook(where, msg, string(stack))
		}()
	}
}
