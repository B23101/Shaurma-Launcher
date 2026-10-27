package servers

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSplitLogChunk(t *testing.T) {
	lines, rest := splitLogChunk("", "a\r\nb\n\nc")
	if strings.Join(lines, "|") != "a|b" || rest != "c" {
		t.Fatalf("lines=%v rest=%q", lines, rest)
	}
	lines, rest = splitLogChunk(rest, "d\ne")
	if strings.Join(lines, "|") != "cd" || rest != "e" {
		t.Fatalf("недописаний рядок має склеюватись: lines=%v rest=%q", lines, rest)
	}
}

func TestReadTailLines(t *testing.T) {
	p := filepath.Join(t.TempDir(), "latest.log")
	var sb strings.Builder
	for i := 0; i < 10; i++ {
		sb.WriteString("line" + string(rune('0'+i)) + "\n")
	}
	sb.WriteString("partial-no-newline")
	if err := os.WriteFile(p, []byte(sb.String()), 0644); err != nil {
		t.Fatal(err)
	}
	lines, off, err := readTailLines(p, 3, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(lines, "|") != "line7|line8|line9" {
		t.Fatalf("lines=%v", lines)
	}
	if want := int64(len("line0\n") * 10); off != want {
		t.Fatalf("offset=%d, want %d (недописаний рядок не входить)", off, want)
	}
	// Малий maxBytes: перший (обрізаний) рядок відкидається.
	lines, _, _ = readTailLines(p, 100, 20)
	for _, l := range lines {
		if !strings.HasPrefix(l, "line") {
			t.Fatalf("обрізаний рядок у історії: %q", l)
		}
	}
}

// Консоль усиновленого сервера: історія з latest.log + нові рядки наживо;
// після завершення процесу статус — «stopped» (не «stopping»).
func TestAdoptedConsoleFollowsLatestLog(t *testing.T) {
	dir := t.TempDir()
	logPath := latestLogPath(dir)
	if err := os.MkdirAll(filepath.Dir(logPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, []byte("[10:00:00] [Server thread/INFO]: old-1\n[10:00:01] [Server thread/INFO]: old-2\n"), 0644); err != nil {
		t.Fatal(err)
	}

	helper := spawnHelper(t)
	snap := snapshotProcess(helper.Process.Pid)
	if !snap.Alive {
		t.Skip("не вдалося отримати знімок процесу")
	}
	srv := &Server{ID: "srv-console", Dir: dir, Core: "paper", Port: 25571}
	rec := runtimeRecord{PID: helper.Process.Pid, CreatedNs: snap.CreateNs, StartedAt: time.Now().UnixMilli(), Core: "paper", Port: 25571}
	writeRuntimeRecord(dir, rec)

	m := NewManager()
	var mu sync.Mutex
	var statuses []string
	m.SetCallbacks(nil, func(id string, s ServerStatus) {
		mu.Lock()
		statuses = append(statuses, s.Status)
		mu.Unlock()
	})
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
	if !has("old-1") || !has("old-2") {
		t.Fatal("історія latest.log не потрапила в консоль")
	}

	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("[10:00:05] [Server thread/INFO]: live-line\n")
	f.Close()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) && !has("live-line") {
		time.Sleep(100 * time.Millisecond)
	}
	if !has("live-line") {
		t.Fatal("новий рядок latest.log не з'явився в консолі наживо")
	}

	// «Стоп» на відновленому сервері → фінальний статус має бути stopped.
	done := make(chan struct{})
	go func() { _ = m.Stop(srv.ID, true); close(done) }()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("Stop завис")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(statuses) == 0 || statuses[len(statuses)-1] != "stopped" {
		t.Fatalf("фінальний статус після Stop = %v, очікували останній stopped", statuses)
	}
}

// Preflight/CheckPortFree: усиновлений сервер — «наш запущений», а не
// «невідомий процес»; сам для себе він «вільний».
func TestAdoptedServerCountsAsOurs(t *testing.T) {
	dir := t.TempDir()
	helper := spawnHelper(t)
	snap := snapshotProcess(helper.Process.Pid)
	if !snap.Alive {
		t.Skip("не вдалося отримати знімок процесу")
	}
	a := &Server{ID: "A", Name: "Alpha", Dir: dir, Core: "paper", Port: 25572}
	b := &Server{ID: "B", Name: "Beta", Dir: t.TempDir(), Core: "paper", Port: 25572}
	rec := runtimeRecord{PID: helper.Process.Pid, CreatedNs: snap.CreateNs, StartedAt: time.Now().UnixMilli(), Core: "paper", Port: 25572}
	m := NewManager()
	if !m.adoptRecord(a.ID, a, rec, nil, func(string) bool { return true }) {
		t.Fatal("не усиновлено")
	}
	all := map[string]*Server{"A": a, "B": b}

	if res := CheckPortFree(25572, all, "B", m.IsRunning); res.Free || res.OurServer != "A" {
		t.Fatalf("для B порт має бути зайнятий НАШИМ сервером A, отримали %+v", res)
	}
	if res := CheckPortFree(25572, all, "A", m.IsRunning); !res.Free {
		t.Fatalf("для самого A (він запущений) порт не має вважатись чужим: %+v", res)
	}
}
