package main

// crashlog.go — запис крашів shell-процесу (shell-crash.log поруч з exe).
//
// shell і backend.dll — це ДВА окремі Go-рантайми в одному процесі, і
// SetCrashOutput бекенда (logs/crash.log) НЕ покриває фатальні помилки
// рантайму shell'а. Тут те саме для shell: фатальні помилки Go, які не
// ловить recover (concurrent map writes, deadlock, збій стека), дописуються
// рантаймом у shell-crash.log, а паніки в колбеках DLL→shell та в main
// перехоплюються і пишуться разом зі стеком.

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
	"time"
)

var (
	crashLogMu   sync.Mutex
	crashLogFile *os.File
)

const maxShellCrashLogBytes = 512 << 10

// initCrashLog — викликати ПЕРШИМ у main(), до будь-якої іншої роботи.
func initCrashLog() {
	defer func() { _ = recover() }()
	path := filepath.Join(exeDir(), "shell-crash.log")
	if fi, err := os.Stat(path); err == nil && fi.Size() > maxShellCrashLogBytes {
		_ = os.Remove(path + ".1")
		_ = os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	crashLogMu.Lock()
	crashLogFile = f
	crashLogMu.Unlock()
	debug.SetTraceback("all")
	_ = debug.SetCrashOutput(f, debug.CrashOptions{})
}

// logShellPanic пише перехоплену паніку у shell-crash.log та bootstrap.log.
func logShellPanic(where string, r any, stack []byte) {
	defer func() { _ = recover() }()
	line := fmt.Sprintf("[%s] ПАНІКА в %s: %v\n%s\n",
		time.Now().Format("2006-01-02 15:04:05.000"), where, r, stack)
	crashLogMu.Lock()
	if crashLogFile != nil {
		_, _ = crashLogFile.WriteString(line)
		_ = crashLogFile.Sync()
	}
	crashLogMu.Unlock()
	bootstrapLog(exeDir(), "ПАНІКА в %s: %v", where, r)
}

// recoverCb — `defer recoverCb("cbName")` першим рядком кожного колбека
// DLL→shell. Колбек викликається з потоку бекенда; паніка тут (nil
// dispatcher, закрите вікно, збій WebView) пробила б межу рантаймів і
// вбила б процес без сліду. Тепер вона пишеться в лог, колбек повертає 0.
func recoverCb(name string) {
	if r := recover(); r != nil {
		logShellPanic("callback "+name, r, debug.Stack())
	}
}
