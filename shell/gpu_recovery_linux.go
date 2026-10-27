//go:build linux

package main

// gpu_recovery_linux.go — «гібридний» захист від GPU-крашів WebKitGTK:
//
//   1. Pre-flight перевірка «заліза» ДО запуску вікна: якщо робочого GPU
//      нема (/dev/dri відсутній/недоступний, WSL2, headless, VNC) —
//      софтверний рендер вмикається одразу (це вже робить env-фолбек у
//      webview_compat_linux.go, тут дублювати нічого не треба).
//   2. Supervisor на випадок решти ~20%: у оточеннях, де /dev/dri Є, але
//      драйвер кривий (старі NVIDIA, частково прокинутий GPU в контейнер,
//      зламаний Mesa), WebKitGTK падає SEGFAULT-ом ПІД ЧАС ініціалізації —
//      Go-recover такий краш НЕ ловить (це не Go-panic, а смерть процесу
//      у cgo-коді). Тому перший запуск після «чистого» старту працює як
//      ПРОБНИЙ: біля exe створюється crash-маркер (.gpu-crash). Якщо
//      процес помер, не прибравши маркер, — наступний запуск стартує у
//      БЕЗПЕЧНОМУ режимі (апаратне прискорення вимкнено повністю) і лише
//      після ШТАТНОГО завершення прибирає маркер — наступний запуск знову
//      пробує GPU. Схема самовідновлюється: полагодив драйвер — лаунчер
//      сам повернеться на апаратне прискорення.
//
// Чому саме так, а не «перевірити /dev/dri і заспокоїтись»: сама лише
// перевірка наявності DRI-вузла не відрізняє «GPU працює» від «вузол є,
// а EGL/GLES падає» — і саме ці випадки давали «біле вікно/фриз/краш».
//
// Вимикач механізму: SHAURMA_NO_GPU_RECOVERY=1 (для дебагу самого механізму).
// Ручне керування рендером, як і раніше: SHAURMA_FORCE_GPU=1 (працює, навіть
// якщо евристика не впізнала GPU) та SHAURMA_FORCE_SOFTWARE=1.

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// gpuSafeModeEnv — env-прапорець, який probe-процес ставить дочірньому
// процесу при перезапуску у безпечному режимі. Читається:
//   - applyLinuxWebViewCompatEnv (webview_compat_linux.go) — WEBKIT_DISABLE_
//     COMPOSITING_MODE/WEBKIT_DISABLE_DMABUF_RENDERER для WebKit;
//   - launcherWebviewGpuPolicy (ui_flags.go) — WebviewGpuPolicyNever для вікон.
const gpuSafeModeEnv = "SHAURMA_NO_HARDWARE_ACCELERATION"

// gpuCrashMarkerName — маркер «пробний запуск не дожив до штатного
// завершення». Лежить біля exe (exeDir), як і решта файлів встановлення.
const gpuCrashMarkerName = ".gpu-crash"

// gpuStartupProbe — рішення, прийняте на старті цього процесу:
// true = це ПРОБНИЙ запуск зі звичайним апаратним прискоренням; якщо процес
// помре — наступний старт піде у безпечному режимі. Виставляється рівно
// один раз у recoverGPUStartup() і далі лише читається (зокрема з
// recover-дефера — перевіряти маркер на диску там пізно: пробний запуск
// САМ його щойно створив).
var gpuStartupProbe bool

// gpuSafeModeRequested — чи вимагається безпечний (програмний) рендер
// для ЦЬОГО запуску (env від probe-supervisor або руками користувача).
func gpuSafeModeRequested() bool {
	return envFlagTrue(gpuSafeModeEnv)
}

// gpuProbeActive — чи має цей запуск працювати у пробному режимі.
// Пробний режим має сенс лише коли:
//   - механізм не вимкнено вручну (SHAURMA_NO_GPU_RECOVERY);
//   - безпечний режим уже не форсовано (env-прапорці та «економний
//     режим» — там і так усе вимкнене, крашити нема чому);
//   - робочий GPU за евристикою Є (коли його нема — софтверний рендер
//     обирає env-фолбек, «пробувати» нічого);
//   - crash-маркера від попереднього запуску нема.
//
// Виклик має побічний ефект: hasWorkingGPU() мемоізована — перший виклик
// тут «прогріває» кеш до створення вікон, тож усі рішення в межах процесу
// ухвалюються за одним і тим самим станом /dev/dri.
func gpuProbeActive() bool {
	if envFlagTrue("SHAURMA_NO_GPU_RECOVERY") {
		return false
	}
	if envFlagTrue("SHAURMA_FORCE_GPU") ||
		envFlagTrue("SHAURMA_FORCE_SOFTWARE") ||
		gpuSafeModeRequested() ||
		lowMemoryModeEnabled() {
		return false
	}
	if !hasWorkingGPU() {
		return false
	}
	return !gpuCrashMarkerExists()
}

// gpuCrashMarkerPath — шлях до маркера ("" = теки exe не знайшли,
// механізм відключається).
func gpuCrashMarkerPath() string {
	dir := exeDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, gpuCrashMarkerName)
}

func gpuCrashMarkerExists() bool {
	p := gpuCrashMarkerPath()
	if p == "" {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}

// writeGPUCrashMarker створює маркер. false = записати не вдалось (тека
// тільки для читання, напр. AppImage-маунт) — тоді supervisor-перезапуск
// неможливий, і краш пробного запуску обробиться як звичайний
// crash-діалог замість тихого перезапуску.
func writeGPUCrashMarker() bool {
	p := gpuCrashMarkerPath()
	if p == "" {
		return false
	}
	if os.MkdirAll(filepath.Dir(p), 0o755) != nil {
		return false
	}
	content := fmt.Sprintf("pid=%d\nstarted=%s\n", os.Getpid(), time.Now().Format(time.RFC3339))
	return os.WriteFile(p, []byte(content), 0o644) == nil
}

func removeGPUCrashMarker() {
	if p := gpuCrashMarkerPath(); p != "" {
		_ = os.Remove(p)
	}
}

// recoverGPUStartup — точка входу механізму, викликається з main() ПЕРШОЮ
// чергою (до будь-якого GTK/WebKit виклику). Вирішує режим запуску:
//   - маркер є (минулий запуск упав) → цей старт у безпечному режимі;
//     маркер НЕ чіпаємо — його прибере ШТАТНЕ завершення (main після
//     app.Run), і лише тоді наступний запуск знову пробує GPU;
//   - маркера нема → пробний запуск: створюємо маркер одразу, щоб «смерть
//     процесу» (SEGFAULT у cgo, kill -9, батарея сіла) її лишила.
//
// Порядок із applyLinuxWebViewCompatEnv: вона вже відпрацювала і за
// відсутності GPU виставила софтверний env — тож here «безпечний режим»
// лише додає вимкнення прискорення туди, де GPU Є, але він крашить.
func recoverGPUStartup() {
	gpuStartupProbe = gpuProbeActive()
	if gpuStartupProbe {
		writeGPUCrashMarker()
	}
	if gpuSafeModeRequested() {
		println("GPU: safe mode (aparatne pryskorennia vymkneno) — " + gpuSafeModeEnv + "=1")
	} else if gpuStartupProbe {
		println("GPU: probe startup (hardware acceleration on, crash marker set)")
	}
}

// recoverGPUStartupRecover — defer у main(), ЗАРЕЄСТРОВАНИЙ ПІСЛЯ
// стандартного defer з showStartupCrashDialog: дефери виконуються LIFO,
// тож цей обробник перший — він перехоплює panic (recover) і вирішує,
// що робити, а стандартний обробник після нього бачить r == nil (no-op).
//
// Краш у cgo/WebKit (SEGFAULT) сюди НЕ потрапляє — Go не може відновити
// процес після нього; але маркер, створений на старті, залишається, і
// САМЕ ВІН переводить наступний запуск у безпечний режим.
func recoverGPUStartupRecover() {
	r := recover()
	if r == nil {
		return
	}
	stack := fmt.Sprintf("%v", r)
	if !gpuStartupProbe {
		// Не пробний старт (безпечний режим, форс-прапорці, економний
		// режим) — прискорення і так вимкнене, отже причина крашу інша.
		// Показуємо звичайний crash-діалог (як і робив би стандартний
		// обробник, якби ми не перехопили panic раніше).
		showStartupCrashDialog(stack)
		os.Exit(1)
	}
	// Пробний запуск упав ще ДО вікна — фіксуємо і тихо перезапускаємось
	// у безпечному режимі. Користувач бачить не «нічого не відбувається»,
	// а лаунчер, який просто відкрився трохи пізніше.
	if writeGPUCrashMarker() {
		if err := relaunchInGPUSafeMode(); err == nil {
			return // недосяжно: успішний syscall.Exec не повертає керування
		}
	}
	// Перезапуститись не вдалось (exe не знайдено, маркер не записався) —
	// хоч якось повідомити користувача.
	showStartupCrashDialog(stack + "\n\n(gpu safe-mode relaunch failed — next start will use software rendering)")
	os.Exit(1)
}

// relaunchInGPUSafeMode — exec(3) СЕБЕ ж із env-прапорцем безпечного
// режиму. Використовується саме Exec, а не повторний запуск новим
// процесом, щоб: не з'являвся другий запис у доку/таскбарі, CLI-аргументи
// (--launch/--account/--server, файл збірки) передались як є, і не
// лишилось «зомбі»-батька. Успішний Exec ніколи не повертає керування —
// поточний процес атомічно замінюється новим собою.
//
// SingleInstance-lock попереднього (упалого) процесу звільняється разом із
// процесом, тож конфлікту з ним немає.
func relaunchInGPUSafeMode() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	env := append(os.Environ(), gpuSafeModeEnv+"=1")
	argv := append([]string{os.Args[0]}, os.Args[1:]...)
	return syscall.Exec(exe, argv, env)
}
