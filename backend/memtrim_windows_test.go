//go:build windows

package main

// memtrim_windows_test.go — вимірює РЕАЛЬНИЙ ефект trimProcessWorkingSets,
// а не лише те, що функція не падає: виділяємо й «прогріваємо» ~48 МБ,
// знімаємо робочий набір, тримаємо, віддаємо ОС і знімаємо знову.
//
// Тест свідомо не перевіряє приватні байти (commit): EmptyWorkingSet їх не
// змінює за визначенням — сторінки йдуть у standby/paging, а не звільняються.
// Саме робочий набір показує Диспетчер завдань як «пам'ять» застосунку.

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

type processMemoryCounters struct {
	Cb                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
}

var procGetProcessMemoryInfo = syscall.NewLazyDLL("psapi.dll").NewProc("GetProcessMemoryInfo")

// currentWorkingSet — робочий набір ПОТОЧНОГО процесу (байти).
func currentWorkingSet(t *testing.T) uintptr {
	t.Helper()
	var c processMemoryCounters
	c.Cb = uint32(unsafe.Sizeof(c))
	r, _, err := procGetProcessMemoryInfo.Call(
		uintptr(windows.CurrentProcess()), uintptr(unsafe.Pointer(&c)), uintptr(c.Cb))
	if r == 0 {
		t.Fatalf("GetProcessMemoryInfo: %v", err)
	}
	return c.WorkingSetSize
}

// workingSetOf — робочий набір ІНШОГО процесу (як його бачить Диспетчер
// завдань). Саме так перевіряється головний випадок: WebView2 — окремий
// процес, і FreeOSMemory його не чіпає.
func workingSetOf(t *testing.T, pid uint32) uintptr {
	t.Helper()
	h, err := windows.OpenProcess(
		windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_VM_READ, false, pid)
	if err != nil {
		t.Fatalf("OpenProcess(%d): %v", pid, err)
	}
	defer windows.CloseHandle(h)
	var c processMemoryCounters
	c.Cb = uint32(unsafe.Sizeof(c))
	r, _, merr := procGetProcessMemoryInfo.Call(uintptr(h), uintptr(unsafe.Pointer(&c)), uintptr(c.Cb))
	if r == 0 {
		t.Fatalf("GetProcessMemoryInfo(%d): %v", pid, merr)
	}
	return c.WorkingSetSize
}

// TestMemTrimHelperChild — не тест, а «дитина»: тримає ~32 МБ гарячих
// сторінок, поки батько знімає її робочий набір. Запускається лише з
// TestTrimProcessWorkingSetsTrimsChildProcess (інакше skip).
func TestMemTrimHelperChild(t *testing.T) {
	if os.Getenv("SHAURMA_MEMTRIM_HELPER") != "1" {
		t.Skip("службовий процес для TestTrimProcessWorkingSetsTrimsChildProcess")
	}
	blocks := make([][]byte, 0, 32)
	for i := 0; i < 32; i++ {
		b := make([]byte, 1<<20)
		for j := 0; j < len(b); j += 4096 {
			b[j] = byte(i + 1)
		}
		blocks = append(blocks, b)
	}
	fmt.Println("MEMTRIM-READY")
	runtime.KeepAlive(blocks)
	time.Sleep(30 * time.Second)
}

// TestTrimProcessWorkingSetsTrimsChildProcess — саме той випадок, який
// турбує користувача: «webview жере пам'ять». Дочірній процес (у лаунчері —
// msedgewebview2.exe) тримає 32 МБ; після тримування його робочий набір
// мусить упасти, а процес — лишитись живим.
func TestTrimProcessWorkingSetsTrimsChildProcess(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	cmd := exec.Command(exe, "-test.run=TestMemTrimHelperChild", "-test.v")
	cmd.Env = append(os.Environ(), "SHAURMA_MEMTRIM_HELPER=1")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("старт дитини: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	// Чекаємо, поки дитина реально виділить пам'ять (а не просто стартує).
	sc := bufio.NewScanner(out)
	ready := false
	for sc.Scan() {
		if sc.Text() == "MEMTRIM-READY" {
			ready = true
			break
		}
	}
	if !ready {
		t.Fatalf("дитина не повідомила готовність: %v", sc.Err())
	}

	pid := uint32(cmd.Process.Pid)
	before := workingSetOf(t, pid)
	if trimmed := trimProcessWorkingSets(); trimmed < 2 {
		t.Fatalf("оброблено %d процесів — очікувались як мінімум власний і дочірній", trimmed)
	}
	time.Sleep(200 * time.Millisecond)
	after := workingSetOf(t, pid)

	if after >= before {
		t.Fatalf("робочий набір дитини не зменшився: %.1f МБ → %.1f МБ",
			float64(before)/(1<<20), float64(after)/(1<<20))
	}
	// Дитина жива (GetProcessMemoryInfo вище відпрацював після тримування) —
	// тобто EmptyWorkingSet не вбиває процес, лише вивантажує його сторінки.
	t.Logf("робочий набір дитини: %.1f МБ → %.1f МБ",
		float64(before)/(1<<20), float64(after)/(1<<20))
}

func TestTrimProcessWorkingSetsReleasesWorkingSet(t *testing.T) {
	// 48 МБ із дотиком до кожної сторінки: інакше сторінки не увійшли б у
	// робочий набір і «зменшувати» було б нічого.
	blocks := make([][]byte, 0, 48)
	for i := 0; i < 48; i++ {
		b := make([]byte, 1<<20)
		for j := 0; j < len(b); j += 4096 {
			b[j] = byte(i + 1)
		}
		blocks = append(blocks, b)
	}

	before := currentWorkingSet(t)
	trimmed := trimProcessWorkingSets()
	after := currentWorkingSet(t)
	runtime.KeepAlive(blocks)

	if trimmed == 0 {
		t.Fatal("trimProcessWorkingSets не обробив жодного процесу (навіть власного)")
	}
	if after >= before {
		t.Fatalf("робочий набір не зменшився: %.1f МБ → %.1f МБ",
			float64(before)/(1<<20), float64(after)/(1<<20))
	}
	t.Logf("робочий набір: %.1f МБ → %.1f МБ (процесів оброблено: %d)",
		float64(before)/(1<<20), float64(after)/(1<<20), trimmed)
}
