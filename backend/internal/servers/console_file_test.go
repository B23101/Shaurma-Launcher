package servers

// console_file_test.go — вивід сервера у файл (console_file.go) і відновлення
// консолі з нього після перезапуску лаунчера (recover_console.go).
//
// Навіщо: раніше stdout сервера був анонімним pipe, який жив усередині
// лаунчера. Після його смерті вивід губився, і відновлена консоль показувала
// лише те, що встигло потрапити в logs/latest.log. Тепер сервер пише у
// logs/shaurma-console.log через успадкований дескриптор, тому після
// перезапуску доступний ВЕСЬ вивід — разом із рядками, написаними, поки
// лаунчера не було.

import (
	"os"
	"strings"
	"testing"
	"time"
)

// TestOpenServerConsoleLogTruncates — кожен запуск сервера починає файл з
// нуля (консоль нового запуску не змішується з попереднім), але дописує в
// кінець (O_APPEND), щоб stdout/stderr не билися за спільний зсув.
func TestOpenServerConsoleLogTruncates(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepathDir(serverConsoleLogPath(dir)), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(serverConsoleLogPath(dir), []byte("stale from previous run\n"), 0644); err != nil {
		t.Fatal(err)
	}

	f, err := openServerConsoleLog(dir)
	if err != nil {
		t.Fatalf("openServerConsoleLog: %v", err)
	}
	if _, err := f.WriteString("line-1\n"); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	data, err := os.ReadFile(serverConsoleLogPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "stale") {
		t.Fatalf("старий вміст не обрізано: %q", string(data))
	}
	if !strings.Contains(string(data), "line-1") {
		t.Fatalf("новий рядок не записано: %q", string(data))
	}
}

// TestAdoptedConsoleUsesConsoleLogFile — після перезапуску лаунчера консоль
// бере історію з shaurma-console.log (а не з latest.log), і далі стежить саме
// за ним.
func TestAdoptedConsoleUsesConsoleLogFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepathDir(serverConsoleLogPath(dir)), 0755); err != nil {
		t.Fatal(err)
	}
	// latest.log існує (його пише сам сервер), але консоль має читати свій файл.
	if err := os.WriteFile(latestLogPath(dir), []byte("[10:00:00] [Server thread/INFO]: from-latest\n"), 0644); err != nil {
		t.Fatal(err)
	}
	consolePath := serverConsoleLogPath(dir)
	if err := os.WriteFile(consolePath, []byte("[10:00:01] [Server thread/INFO]: while-launcher-was-down\n"), 0644); err != nil {
		t.Fatal(err)
	}

	helper := spawnHelper(t)
	snap := snapshotProcess(helper.Process.Pid)
	if !snap.Alive {
		t.Skip("не вдалося отримати знімок процесу")
	}
	srv := &Server{ID: "srv-consolefile", Dir: dir, Core: "paper", Port: 25573}
	rec := runtimeRecord{PID: helper.Process.Pid, CreatedNs: snap.CreateNs, StartedAt: time.Now().UnixMilli(), Core: "paper", Port: 25573}
	writeRuntimeRecord(dir, rec)

	m := NewManager()
	if !m.adoptRecord(srv.ID, srv, rec, nil, func(string) bool { return true }) {
		t.Fatal("не усиновлено")
	}

	has := func(sub string) bool {
		for _, l := range m.GetServerConsoleSnapshot(srv.ID) {
			if strings.Contains(l.Text, sub) {
				return true
			}
		}
		return false
	}
	if !has("while-launcher-was-down") {
		t.Fatal("історія shaurma-console.log не потрапила в консоль")
	}
	if has("from-latest") {
		t.Fatal("консоль прочитала latest.log, хоча є shaurma-console.log")
	}

	f, err := os.OpenFile(consolePath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("[10:00:05] [Server thread/INFO]: live-console-line\n"); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) && !has("live-console-line") {
		time.Sleep(100 * time.Millisecond)
	}
	if !has("live-console-line") {
		t.Fatal("новий рядок shaurma-console.log не з'явився в консолі наживо")
	}
}

// filepathDir — filepath.Dir без імпорту filepath у тесті (щоб не плодити
// однойменні хелпери в пакеті).
func filepathDir(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[:i]
	}
	return "."
}
