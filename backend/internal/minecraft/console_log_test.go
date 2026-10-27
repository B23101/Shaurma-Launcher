//go:build !windows

package minecraft

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// lineSink — потоко-безпечний збирач рядків для перевірок у тестах.
type lineSink struct {
	mu    sync.Mutex
	lines []string
}

func (s *lineSink) add(l string) {
	s.mu.Lock()
	s.lines = append(s.lines, l)
	s.mu.Unlock()
}

func (s *lineSink) snapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.lines...)
}

func (s *lineSink) has(sub string) bool {
	for _, l := range s.snapshot() {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}

// waitFor чекає, поки умова стане істинною (або таймаут).
func waitFor(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("таймаут очікування: %s", what)
}

func TestTailFile_ReadsHistoryThenFollows(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.log")
	if err := os.WriteFile(path, []byte("old1\nold2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	sink := &lineSink{}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = TailFile(path, 0, stop, sink.add)
	}()

	waitFor(t, 2*time.Second, "історія old1/old2", func() bool { return sink.has("old2") })

	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString("new1\n")
	waitFor(t, 2*time.Second, "нові рядки new1", func() bool { return sink.has("new1") })
	f.WriteString("new2\n")
	f.Close()
	waitFor(t, 2*time.Second, "нові рядки new2", func() bool { return sink.has("new2") })

	close(stop)
	<-done

	got := sink.snapshot()
	want := []string{"old1", "old2", "new1", "new2"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("порядок/склад рядків: got %v want %v", got, want)
	}
}

// Неповний рядок (гра ще пише) не має віддаватись обрізаним.
func TestTailFile_HoldsPartialLineUntilNewline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.log")
	os.WriteFile(path, []byte("abc"), 0o644)

	sink := &lineSink{}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() { defer close(done); _ = TailFile(path, 0, stop, sink.add) }()

	time.Sleep(3 * tailPollInterval)
	if n := len(sink.snapshot()); n != 0 {
		t.Fatalf("неповний рядок віддано передчасно: %v", sink.snapshot())
	}
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString("def\n")
	f.Close()
	waitFor(t, 2*time.Second, "склеєний рядок abcdef", func() bool { return sink.has("abcdef") })

	close(stop)
	<-done
	if got := sink.snapshot(); len(got) != 1 || got[0] != "abcdef" {
		t.Fatalf("очікували один рядок abcdef, маємо %v", got)
	}
}

// При зупинці недописаний хвіст (обірваний краш) все одно віддається.
func TestTailFile_FlushesPartialOnStop(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.log")
	os.WriteFile(path, []byte("line\nGame crashed: stack"), 0o644)

	sink := &lineSink{}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() { defer close(done); _ = TailFile(path, 0, stop, sink.add) }()
	waitFor(t, 2*time.Second, "перший рядок", func() bool { return sink.has("line") })
	close(stop)
	<-done
	if !sink.has("Game crashed: stack") {
		t.Fatalf("хвіст без \\n загублено: %v", sink.snapshot())
	}
}

func TestTailFile_StripsCRLF(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.log")
	os.WriteFile(path, []byte("win\r\n"), 0o644)
	sink := &lineSink{}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() { defer close(done); _ = TailFile(path, 0, stop, sink.add) }()
	waitFor(t, 2*time.Second, "рядок win", func() bool { return len(sink.snapshot()) == 1 })
	close(stop)
	<-done
	if got := sink.snapshot()[0]; got != "win" {
		t.Fatalf("CR не прибрано: %q", got)
	}
}

func TestTailFile_MissingFileNoWait(t *testing.T) {
	stop := make(chan struct{})
	defer close(stop)
	err := TailFile(filepath.Join(t.TempDir(), "nope.log"), 0, stop, func(string) {})
	if err == nil {
		t.Fatal("очікували помилку відкриття неіснуючого файлу")
	}
}

func TestTailFile_WaitsForFileToAppear(t *testing.T) {
	path := filepath.Join(t.TempDir(), "later.log")
	sink := &lineSink{}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() { defer close(done); _ = TailFile(path, 2*time.Second, stop, sink.add) }()
	time.Sleep(150 * time.Millisecond)
	os.WriteFile(path, []byte("hello\n"), 0o644)
	waitFor(t, 2*time.Second, "рядок після появи файлу", func() bool { return sink.has("hello") })
	close(stop)
	<-done
}

// aliveFn — перевірка «процес ще живий» для тестів (kill -0).
func aliveFn(pid int) bool {
	// Зомбі (завершений, але не зібраний батьком) сигнал 0 приймає, тож
	// додатково дивимось /proc/<pid>/stat.
	if syscall.Kill(pid, 0) != nil {
		return false
	}
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return false
	}
	// формат: pid (comm) S ...
	i := strings.LastIndex(string(b), ")")
	if i < 0 || i+2 >= len(b) {
		return false
	}
	return b[i+2] != 'Z'
}

// ГОЛОВНИЙ СЦЕНАРІЙ БАГУ: гру запустили, лаунчер закрили повністю, гра
// лишилась жити й далі пише в консоль; лаунчер відкрили знову — консоль має
// показати І те, що було до закриття, І те, що гра написала за час, поки
// лаунчера не було, І нові рядки після відкриття.
func TestRestartLauncher_ReattachesToGameConsole(t *testing.T) {
	gameDir := t.TempDir()

	// «Гра»: пише лічильник раз на 100 мс у stdout і кожен 3-й — у stderr.
	script := `i=0; while [ $i -lt 600 ]; do echo "tick $i"; if [ $((i % 3)) -eq 0 ]; then echo "err $i" 1>&2; fi; i=$((i+1)); sleep 0.1; done`
	cmd := exec.Command("sh", "-c", script)
	cmd.Dir = gameDir
	// Окрема група процесів: «гру» не повинен зачепити наш тестовий Ctrl+C.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Те саме, що робить Launch: stdout+stderr → файл, дескриптор
	// успадковується дитиною.
	logf, err := attachConsoleLog(cmd, gameDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	logf.Close()
	pid := cmd.Process.Pid
	t.Cleanup(func() {
		syscall.Kill(-pid, syscall.SIGKILL)
		cmd.Wait()
	})

	// ── Перший «сеанс» лаунчера ──
	first := &lineSink{}
	l1 := NewLauncher()
	l1.SetOutputHandler(first.add)
	stop1 := make(chan struct{})
	done1 := make(chan struct{})
	go func() {
		defer close(done1)
		_ = TailFile(ConsoleLogPath(gameDir), time.Second, stop1, first.add)
	}()
	waitFor(t, 3*time.Second, "перші рядки гри", func() bool { return first.has("tick 3") })

	// ── Лаунчер «закрили повністю»: tail зупинено, усе, що було у
	// процесі лаунчера, зникло. Гра (окремий процес) далі живе. ──
	close(stop1)
	<-done1
	beforeClose := len(first.snapshot())
	if beforeClose == 0 {
		t.Fatal("до закриття не отримано жодного рядка")
	}
	// Гра пише далі, поки лаунчера немає.
	time.Sleep(800 * time.Millisecond)
	if !aliveFn(pid) {
		t.Fatal("гра мала лишитись живою після закриття лаунчера")
	}

	// ── Лаунчер «відкрили знову»: новий Launcher усиновлює PID. ──
	second := &lineSink{}
	exited := make(chan int, 1)
	l2 := NewLauncher()
	l2.SetOutputHandler(second.add)
	l2.SetExitHandler(func(code int) { exited <- code })
	if !l2.AdoptProcess(pid, gameDir, aliveFn) {
		t.Fatal("AdoptProcess відхилив живий PID")
	}
	if !l2.IsRunning() {
		t.Fatal("усиновлений Launcher має бути running")
	}

	// 1) Історія відновлена: рядки, написані ДО закриття, є.
	waitFor(t, 3*time.Second, "історія tick 0", func() bool { return second.has("tick 0") })
	waitFor(t, 3*time.Second, "історія stderr 'err 0'", func() bool { return second.has("err 0") })
	// 2) Рядки, написані, поки лаунчера НЕ БУЛО, теж є.
	waitFor(t, 3*time.Second, "рядки за час без лаунчера", func() bool {
		return second.has(fmt.Sprintf("tick %d", beforeClose+3))
	})
	// 3) Нові рядки після відкриття теж йдуть у живому режимі.
	n := len(second.snapshot())
	waitFor(t, 3*time.Second, "нові рядки після відкриття", func() bool { return len(second.snapshot()) > n+3 })

	// 4) Порядок і цілісність: tick-и без пропусків і дублікатів.
	last := -1
	for _, line := range second.snapshot() {
		var v int
		if _, e := fmt.Sscanf(line, "tick %d", &v); e == nil {
			if v != last+1 {
				t.Fatalf("розрив/дубль у послідовності: після tick %d прийшов tick %d", last, v)
			}
			last = v
		}
	}

	// 5) Завершення процесу відстежується: onExit прийде, tail дочитає хвіст.
	syscall.Kill(pid, syscall.SIGKILL)
	cmd.Wait() // збираємо зомбі, як це зробив би init
	select {
	case <-exited:
	case <-time.After(6 * time.Second):
		t.Fatal("onExit не прийшов після завершення усиновленої гри")
	}
	if l2.IsRunning() {
		t.Fatal("після виходу гри Launcher не має бути running")
	}
}

// Регресія: після виходу гри хвіст логу (краш-стек) не губиться.
func TestAdoptProcess_DrainsTailOnExit(t *testing.T) {
	gameDir := t.TempDir()
	cmd := exec.Command("sh", "-c", `echo start; sleep 0.4; echo "#@!@# Game crashed! #@!@#"; echo "at net.minecraft.Foo"`)
	cmd.Dir = gameDir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	logf, err := attachConsoleLog(cmd, gameDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	logf.Close()
	pid := cmd.Process.Pid

	sink := &lineSink{}
	exited := make(chan struct{}, 1)
	l := NewLauncher()
	l.SetOutputHandler(sink.add)
	l.SetExitHandler(func(int) { exited <- struct{}{} })
	if !l.AdoptProcess(pid, gameDir, aliveFn) {
		t.Fatal("AdoptProcess=false")
	}
	go cmd.Wait() // збирає зомбі, щоб aliveFn побачив завершення

	select {
	case <-exited:
	case <-time.After(6 * time.Second):
		t.Fatal("onExit не прийшов")
	}
	if !sink.has("Game crashed") || !sink.has("at net.minecraft.Foo") {
		t.Fatalf("краш-хвіст загублено: %v", sink.snapshot())
	}
	// І onExit прийшов ПІСЛЯ рядків (перевірено вже тим, що вони є до нього).
}

// Регресія живого запуску (не відновлення): Launch-шлях через
// finishLaunch/waitExit читає той самий файл і віддає код виходу.
func TestLiveLaunchPath_TailsFileAndReportsExitCode(t *testing.T) {
	gameDir := t.TempDir()
	cmd := exec.Command("sh", "-c", `echo hello; echo boom 1>&2; exit 3`)
	cmd.Dir = gameDir
	logf, err := attachConsoleLog(cmd, gameDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	logf.Close()

	sink := &lineSink{}
	code := make(chan int, 1)
	l := NewLauncher()
	l.SetOutputHandler(sink.add)
	l.SetExitHandler(func(c int) { code <- c })
	l.mu.Lock()
	l.running = true
	l.mu.Unlock()
	l.finishLaunch(cmd, LaunchConfig{GameDir: gameDir}, "sh", nil)

	select {
	case c := <-code:
		if c != 3 {
			t.Fatalf("код виходу: got %d want 3", c)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("onExit не прийшов")
	}
	if !sink.has("hello") || !sink.has("boom") {
		t.Fatalf("вивід (stdout+stderr) не дійшов: %v", sink.snapshot())
	}
	// Рядок виходу має прийти ПІСЛЯ всього виводу — гарантує waitExit.
}

// SaveLogs: вивід дублюється у logs/latest.log.
func TestLiveLaunchPath_SaveLogsWritesLatestLog(t *testing.T) {
	gameDir := t.TempDir()
	cmd := exec.Command("sh", "-c", `echo persisted-line`)
	cmd.Dir = gameDir
	logf, _ := attachConsoleLog(cmd, gameDir)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	logf.Close()

	done := make(chan struct{}, 1)
	l := NewLauncher()
	l.SetExitHandler(func(int) { done <- struct{}{} })
	l.mu.Lock()
	l.running = true
	l.mu.Unlock()
	l.finishLaunch(cmd, LaunchConfig{GameDir: gameDir, SaveLogs: true}, "sh", nil)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("onExit не прийшов")
	}
	b, err := os.ReadFile(filepath.Join(gameDir, "logs", "latest.log"))
	if err != nil {
		t.Fatalf("latest.log не створено: %v", err)
	}
	if !strings.Contains(string(b), "persisted-line") {
		t.Fatalf("latest.log без виводу гри: %q", b)
	}
}
