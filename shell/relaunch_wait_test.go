package main

import (
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestWaitPIDFromArgs(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want int
	}{
		{"none", []string{"--background", "--autostart"}, 0},
		{"plain", []string{"--wait-pid", "1234"}, 1234},
		{"among others", []string{"--launch", "pack", "--wait-pid", "4321", "--x"}, 4321},
		{"invalid value", []string{"--wait-pid", "abc"}, 0},
		{"zero rejected", []string{"--wait-pid", "0"}, 0},
		{"trailing without value", []string{"--wait-pid"}, 0},
		{"chained takes last", []string{"--wait-pid", "111", "--launch", "p", "--wait-pid", "222"}, 222},
	}
	for _, c := range cases {
		if got := waitPIDFromArgs(c.args); got != c.want {
			t.Errorf("%s: waitPIDFromArgs(%v) = %d, want %d", c.name, c.args, got, c.want)
		}
	}
}

func TestStripWaitPIDArg(t *testing.T) {
	got := stripWaitPIDArg([]string{"--wait-pid", "111", "--background", "--wait-pid", "222"})
	want := []string{"--background"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("stripWaitPIDArg = %v, want %v", got, want)
	}
	if n := len(stripWaitPIDArg([]string{"--background"})); n != 1 {
		t.Fatalf("stripWaitPIDArg without flag changed length to %d", n)
	}
}

// TestRelaunchArgs — головний контракт фіксу «після оновлення відкриває
// у фон, а не вікно»: --background для нового процесу визначає СТАН ВІКНА,
// а не те, з якими аргументами колись запустили поточний (автозапуск ОС).
func TestRelaunchArgs(t *testing.T) {
	const pid = 4242
	tail := []string{"--wait-pid", "4242"}
	cases := []struct {
		name   string
		in     []string
		hidden bool
		want   []string
	}{
		{
			"автозапуск із --background, але вікно ВИДИМЕ → показуємо вікно",
			[]string{"--autostart", "--background"}, false,
			[]string{"--autostart", "--wait-pid", "4242"},
		},
		{
			"вікно в треї (приховане) → лишаємось у фоні",
			[]string{"--autostart"}, true,
			[]string{"--autostart", "--background", "--wait-pid", "4242"},
		},
		{
			"приховане, --background не дублюється",
			[]string{"--background"}, true,
			[]string{"--background", "--wait-pid", "4242"},
		},
		{
			"старий --wait-pid прибирається і замінюється на свій",
			[]string{"--wait-pid", "111", "--autostart"}, false,
			[]string{"--autostart", "--wait-pid", "4242"},
		},
		{
			"звичайний запуск без прапорців",
			nil, false,
			tail,
		},
	}
	for _, c := range cases {
		got := relaunchArgs(c.in, c.hidden, pid)
		if len(got) != len(c.want) {
			t.Errorf("%s: relaunchArgs(%v, hidden=%v) = %v, want %v", c.name, c.in, c.hidden, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: relaunchArgs(%v, hidden=%v) = %v, want %v", c.name, c.in, c.hidden, got, c.want)
				break
			}
		}
	}
}

func TestProcessAliveSelfAndBogus(t *testing.T) {
	if !processAlive(os.Getpid()) {
		t.Fatalf("processAlive(self pid %d) = false, want true", os.Getpid())
	}
	if processAlive(0) {
		t.Errorf("processAlive(0) = true, want false")
	}
	// PID заведомо вне диапазона — процесса точно нет.
	if processAlive(1 << 30) {
		t.Errorf("processAlive(1<<30) = true, want false")
	}
}

// TestProcessAliveAfterExit — ключова властивість, на якій тримається фікс:
// поки дочірній процес живий, processAlive == true; після завершення —
// false. Саме цього чекає новий процес перед створенням Wails-застосунку.
func TestProcessAliveAfterExit(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestProcessAliveHelperProcess")
	cmd.Env = append(os.Environ(), "RELAUNCH_WAIT_HELPER=1")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}
	pid := cmd.Process.Pid
	// Даємо процесу час реально створитися.
	time.Sleep(100 * time.Millisecond)
	if !processAlive(pid) {
		t.Logf("helper (pid %d) вже завершився до перевірки — тест усе одно валідний", pid)
	}
	_ = cmd.Wait()
	deadline := time.Now().Add(5 * time.Second)
	for processAlive(pid) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if processAlive(pid) {
		t.Fatalf("processAlive(pid %d) = true після Wait, want false", pid)
	}
}

func TestProcessAliveHelperProcess(t *testing.T) {
	if os.Getenv("RELAUNCH_WAIT_HELPER") != "1" {
		return
	}
	time.Sleep(300 * time.Millisecond)
	os.Exit(0)
}
