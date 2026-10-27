package main

// supervisor.go — «лаунчер ніколи не падає сам по собі».
//
// Паніки ловлять recover'и, але фатальні помилки Go-рантайму (concurrent map writes, збій стека, SIGSEGV у
// cgo/WebView) і нативні аварії ОС (0xC0000374, 0xC0000421…) процес не переживає. Тому перший (батьківський)
// процес shell — лише тонкий нагляд: він НЕ створює UI/мутекс, а запускає себе ж дочірнім процесом
// (SHAURMA_SUPERVISED=1) і чекає. Якщо дочірній завершився аварійно (код ≠ 0 / сигнал), нагляд піднімає його
// знову З ВІКНОМ; прапорець --background лишається лише коли головне вікно було сховане на момент збою (див.
// маркер supHiddenFile). Після збою дочірній отримує SHAURMA_RESTARTED=1 — backend покаже тост «Перезапущено
// після збою».
//
// Штатний вихід (код 0, «Вийти», оновлення) нагляд не чіпає.
//
// НАГЛЯД БІЛЬШЕ НЕ ЗДАЄТЬСЯ (було: «не більше 5 перезапусків за 10 хв → exit 1»).
// Стара політика робила рівно те, від чого вона мала захищати: після кількох
// швидких збоїв лаунчер просто переставав запускатись, і користувач бачив
// «клікаю — нічого не відбувається». Тепер:
//
//   - перезапуск відбувається ЗАВЖДИ, з експоненційною паузою (2с → 30с), щоб
//     не крутити процесор у гарячому циклі;
//   - після «здорової» сесії (прожив supHealthyRun) лічильник і пауза
//     скидаються — випадковий збій раз на годину не накопичується;
//   - якщо збої йдуть підряд і процес не встигає піднятись, нагляд вмикає
//     БЕЗПЕЧНИЙ РЕЖИМ (SHAURMA_SAFE_MODE=1): без GPU-прискорення WebView2, без
//     автоматичного опитування буфера обміну, без Discord RPC і прогресу в
//     таскбарі. Це прибирає з процесу саме ті нативні шляхи, які могли
//     отруїти купу, тож навіть якщо конкретна збірка WebView2/драйвера
//     ламається, лаунчер усе одно піднімається з робочим UI.

import (
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

const (
	supEnv        = "SHAURMA_SUPERVISED"
	supOffEnv     = "SHAURMA_NO_SUPERVISOR"
	supRestartEnv = "SHAURMA_RESTARTED"
	supSafeEnv    = "SHAURMA_SAFE_MODE"
	supHiddenFile = "window_hidden.flag"

	// supHealthyRun — сесія, що прожила стільки, вважається нормальною:
	// лічильник збоїв і пауза скидаються (збій «раз на годину» не має
	// наближати безпечний режим).
	supHealthyRun = 60 * time.Second

	// supSafeAfter — стільки збоїв ПІДРЯД (кожен — коротша сесія за
	// supHealthyRun) переводять нагляд у безпечний режим.
	supSafeAfter = 2

	supBackoffMin = 2 * time.Second
	supBackoffMax = 30 * time.Second
)

// supState — політика перезапусків. Винесена в тип, щоб її можна було
// перевірити тестом без запуску процесів (supervisor_test.go).
type supState struct {
	// consecutive — скільки разів підряд дочірній процес помер, не проживши
	// supHealthyRun.
	consecutive int
	// backoff — пауза перед наступним стартом (подвоюється до supBackoffMax).
	backoff time.Duration
	// safeMode — дочірній отримує SHAURMA_SAFE_MODE=1.
	safeMode bool
}

func newSupState() supState { return supState{backoff: supBackoffMin} }

// afterCrash опрацьовує один аварійний вихід і повертає паузу перед
// наступним стартом та чи було ЩОЙНО ввімкнено безпечний режим (для логу).
//
// uptime — скільки прожив дочірній процес.
func (s *supState) afterCrash(uptime time.Duration) (delay time.Duration, enteredSafeMode bool) {
	if uptime >= supHealthyRun {
		// Здорова сесія: це не «цикл», а випадковий збій — починаємо з нуля.
		s.consecutive, s.backoff, s.safeMode = 0, supBackoffMin, false
	} else {
		s.consecutive++
	}
	if s.consecutive >= supSafeAfter && !s.safeMode {
		s.safeMode = true
		s.backoff = supBackoffMin // безпечний режим стартуємо без довгої паузи
		enteredSafeMode = true
	}
	delay = s.backoff
	if s.backoff < supBackoffMax {
		s.backoff *= 2
		if s.backoff > supBackoffMax {
			s.backoff = supBackoffMax
		}
	}
	return delay, enteredSafeMode
}

// superviseIfParent — викликати у main() одразу після initCrashLog. У батьківському процесі не повертається.
func superviseIfParent() {
	if os.Getenv(supEnv) != "" || os.Getenv(supOffEnv) != "" || waitPIDFromArgs(os.Args) != 0 {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return // без нагляду краще працювати, ніж не запуститись
	}
	args := os.Args[1:]
	st := newSupState()
	restarted := false
	// Маркер від попередньої сесії застарів: перший старт визначається аргументами користувача.
	setWindowHiddenMarker(false)
	for {
		cmd := exec.Command(exe, args...)
		cmd.Dir = exeDir()
		env := append(os.Environ(), supEnv+"=1")
		if restarted {
			env = append(env, supRestartEnv+"=1")
		}
		if st.safeMode {
			env = append(env, supSafeEnv+"=1")
		}
		cmd.Env = env
		cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, os.Stdout, os.Stderr
		started := time.Now()
		if err := cmd.Start(); err != nil {
			bootstrapLog(exeDir(), "supervisor: старт дочірнього: %v — працюю без нагляду", err)
			return
		}
		err := cmd.Wait()
		if err == nil {
			os.Exit(0) // штатний вихід
		}
		// %s — підказка про звіт нативного збою (logs/crash-native.log), якщо
		// дочірній процес встиг його записати. Сам рядок виходу без неї не
		// пояснює нічого: «exit status 0xc0000374» = пошкодження купи у нативному
		// модулі (WebView2/GPU/антивірус), а не у Go-коді.
		bootstrapLog(exeDir(), "supervisor: дочірній процес завершився аварійно: %v%s", err, nativeCrashHint(exeDir()))
		delay, enteredSafeMode := st.afterCrash(time.Since(started))
		if enteredSafeMode {
			bootstrapLog(exeDir(), "supervisor: %d збоїв підряд — перезапуск у БЕЗПЕЧНОМУ РЕЖИМІ "+
				"(SHAURMA_SAFE_MODE=1: без GPU, без опитування буфера обміну, без Discord RPC/таскбара)", st.consecutive)
		}
		bootstrapLog(exeDir(), "supervisor: перезапуск через %s (безпечний режим: %v)", delay, st.safeMode)
		time.Sleep(delay)
		// Перезапуск з вікном; тихий (--background) — лише якщо вікно було сховане на момент збою.
		args = restartArgs(args, windowHiddenMarkerSet())
		restarted = true
	}
}

// restartArgs — аргументи перезапуску після збою: --background лишається/додається лише коли головне
// вікно було сховане (windowHidden), інакше прибирається, щоб лаунчер піднявся з вікном.
func restartArgs(args []string, windowHidden bool) []string {
	out := make([]string, 0, len(args)+1)
	for _, a := range args {
		if a != "--background" {
			out = append(out, a)
		}
	}
	if windowHidden {
		out = append(out, "--background")
	}
	return out
}

// Маркер «головне вікно сховане»: його ставить shell (windowOp hide / фоновий старт) і знімає при show.
// Нагляд читає його після збою, бо сам стану вікна не бачить.
func windowHiddenMarkerPath() string { return filepath.Join(exeDir(), supHiddenFile) }

func windowHiddenMarkerSet() bool {
	_, err := os.Stat(windowHiddenMarkerPath())
	return err == nil
}

func setWindowHiddenMarker(hidden bool) {
	if hidden {
		_ = os.WriteFile(windowHiddenMarkerPath(), []byte("1"), 0o644)
		return
	}
	_ = os.Remove(windowHiddenMarkerPath())
}

// withoutEnv повертає копію середовища без змінної key.
func withoutEnv(env []string, key string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if len(kv) > len(key) && kv[:len(key)+1] == key+"=" {
			continue
		}
		out = append(out, kv)
	}
	return out
}
