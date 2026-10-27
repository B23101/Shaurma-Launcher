//go:build windows

package main

// nativecrash_windows_test.go — перевірка нативного перехоплювача аварій
// (nativecrash_windows.go). Аварію на рівні ОС неможливо «зловити» всередині
// того самого процесу, тому тест працює за звичним для таких випадків
// прийомом: тестовий бінарник запускає САМ СЕБЕ (go test), у дитині ставить
// перехоплювач і піднімає справжнє виключення STATUS_HEAP_CORRUPTION
// (0xC0000374) — той самий код, що бачив користувач як
// «дочірній процес завершився аварійно: exit status 0xc0000374».
//
// Перевіряється три речі:
//   1. з'явився crash-native.log із кодом, назвою та адресою збою і НАЗВОЮ
//      модуля, у якому він стався;
//   2. з'явився міні-дамп (dbghelp!MiniDumpWriteDump);
//   3. дитина завершилась аварійно — перехоплювач НЕ перехоплює завершення,
//      він лише додає звіт перед ним. Сам КІНЦЕВИЙ код залежить від того, де
//      сталося виключення: на нативному потоці (це і є випадок користувача —
//      WebView2/GPU) процес завершує ОС з кодом 0xC0000374, а на потоці Go
//      власний рантайм друкує свою діагностику і виходить з кодом 2.

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

const (
	nativeCrashTestEnv  = "SHAURMA_NATIVE_CRASH_TEST"
	nativeCrashDirEnv   = "SHAURMA_CRASH_LOGS_DIR"
	nativeCrashTestCode = 0xC0000374 // STATUS_HEAP_CORRUPTION
)

// init виконується і в дитині: там він одразу ставить перехоплювач і падає так,
// ніби купу пошкодив хтось інший. Тестовий бінарник до тестів не доживає —
// процес завершує ОС (з тим самим кодом, що й до появи перехоплювача).
func init() {
	if os.Getenv(nativeCrashTestEnv) != "1" {
		return
	}
	installNativeCrashReporter()
	raiseHeapCorruption()
}

// raiseHeapCorruption піднімає справжнє системне виключення 0xC0000374.
// Повернення не передбачено: без обробника ОС завершує процес.
func raiseHeapCorruption() {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	setErrorMode := kernel32.NewProc("SetErrorMode")
	// SEM_FAILCRITICALERRORS | SEM_NOGPFAULTERRORBOX | SEM_NOOPENFILEERRORBOX —
	// щоб Windows не показував діалог «програма перестала відповідати»
	// під час тесту (він зупинив би прогін).
	_, _, _ = setErrorMode.Call(0x0001 | 0x0002 | 0x8000)
	raise := kernel32.NewProc("RaiseException")
	_, _, _ = raise.Call(uintptr(nativeCrashTestCode), 0, 0, 0)
	// Якщо виключення раптом «проковтнули» — це помилка: дитина не має йти далі.
	os.Exit(99)
}

func TestNativeCrashReporterWritesReport(t *testing.T) {
	dir := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	cmd := exec.Command(exe, "-test.run=TestNativeCrashReporterWritesReport")
	cmd.Env = append(os.Environ(),
		nativeCrashTestEnv+"=1",
		nativeCrashDirEnv+"="+dir,
	)
	out, err := cmd.CombinedOutput()

	code := 0
	var exit *exec.ExitError
	switch {
	case err == nil:
		t.Fatalf("дитина завершилась «нормально» — виключення не спрацювало.\nвивід:\n%s", out)
	case errors.As(err, &exit):
		code = exit.ExitCode()
	default:
		t.Fatalf("не вдалось запустити дитину: %v", err)
	}
	if code == 0 {
		t.Fatalf("код виходу дитини = 0 — очікували аварійне завершення.\nвивід:\n%s", out)
	}
	// На потоці Go рантайм сам відпрацьовує виключення (код 2); на нативному
	// потоці — так, як у користувача, 0xC0000374. Обидва варіанти «аварійні».
	t.Logf("дитина завершилась з кодом 0x%08X", uint32(code))

	// Шлях до звіту беремо через ту саму функцію, що й бойовий код.
	t.Setenv(nativeCrashDirEnv, dir)
	path := filepath.Join(nativeCrashLogsDir(), "crash-native.log")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("звіт %s не створено: %v\nвивід дитини:\n%s", path, err, out)
	}
	report := string(b)
	for _, want := range []string{"STATUS_HEAP_CORRUPTION", "код:", "адреса:", "потік:", "+0x"} {
		if !strings.Contains(report, want) {
			t.Errorf("у звіті немає %q:\n%s", want, report)
		}
	}
	if !strings.Contains(report, "exe:") {
		t.Errorf("у звіті немає шляху до exe:\n%s", report)
	}
	if dumps := nativeDumpList(dir); len(dumps) == 0 {
		t.Errorf("міні-дамп не записано (dbghelp недоступний?): директорія %s", dir)
	} else {
		t.Logf("звіт і дамп записано: %v", dumps)
	}
}
