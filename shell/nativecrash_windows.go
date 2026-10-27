//go:build windows

package main

// nativecrash_windows.go — перехоплювач НАТИВНИХ збоїв процесу (не Go-панік).
//
// Перехоплюються ДВА шляхи (див. installNativeCrashReporter): VEH — бачить
// кожне виключення першим, і фільтр необроблених виключень — страховка для
// решти кодів. Обидва повертають EXCEPTION_CONTINUE_SEARCH: поведінка процесу
// НЕ змінюється (за замовчуванням на нативному потоці це завершення з кодом
// виключення, на потоці Go — власна діагностика рантайму з кодом 2),
// додається лише звіт.
//
//
// Навіщо: recover ловить лише паніки Go, crashguard ловить fatal error рантайму
// (crash.log). АЛЕ аварія на рівні ОС — пошкодження купи (0xC0000374
// STATUS_HEAP_CORRUPTION), access violation у нативному модулі (WebView2, драйвер
// GPU, інжектована DLL) — не лишає ЖОДНОГО сліду: ні в launcher.log, ні в
// crash.log, ні в Event Log. Саме так виглядав звіт користувача:
//
//	supervisor: дочірній процес завершився аварійно: exit status 0xc0000374
//
// Тобто лаунчер просто зникав, а в логах не було навіть назви модуля-винуватця.
//
// Що робить цей файл: ставить SetUnhandledExceptionFilter і при необробленому
// виключенні ДО завершення процесу пише:
//   - код виключення (з людською назвою: STATUS_HEAP_CORRUPTION тощо),
//   - адресу збою + МОДУЛЬ і зсув усередині нього (головне для діагностики),
//   - останні рядки launcher.log (що лаунчер робив у ту мить),
//   - міні-дамп (dbghelp!MiniDumpWriteDump) — щоб потім можна було відкрити
//     збій у WinDbg і побачити стек і пошкоджену пам'ять.
//
// Файли: logs/crash-native.log (текст) і logs/crash-<дата>-<pid>.dmp (дамп).
// Короткий рядок дублюється в bootstrap.log (той самий, куди пише supervisor) —
// щоб порядок «що сталося → аварійне завершення» читався в одному місці.
//
// Обмеження, свідомі:
//   - пишемо лише ПЕРШЕ виключення в процесі (далі — щоб не зациклитись);
//   - дампів зберігаємо не більше nativeDumpKeep (аварійний перезапуск у циклі
//     не має забити диск);
//   - якщо ОС віддає керування вже з пошкодженою купою — частина системних
//     викликів (Toolhelp32Snapshot) може не спрацювати: тоді в звіті буде
//     голова адреси збою без імені модуля, але звіт усе одно запишеться.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// EXCEPTION_POINTERS / EXCEPTION_RECORD — дзеркала структур WinAPI.
// Паддінг лишаємо явним: важливі не поля самі собою, а точний збіг layout.
type exceptionRecord struct {
	ExceptionCode        uint32
	ExceptionFlags       uint32
	ExceptionRecord      uintptr
	ExceptionAddress     uintptr
	NumberParameters     uint32
	_pad                 uint32
	ExceptionInformation [15]uintptr
}

type exceptionPointers struct {
	ExceptionRecord uintptr
	ContextRecord   uintptr
}

// MINIDUMP_EXCEPTION_INFORMATION (dbghelp): ThreadId + вказівник на
// EXCEPTION_POINTERS + ClientPointers.
type minidumpExceptionInformation struct {
	ThreadID          uint32
	_pad              uint32
	ExceptionPointers uintptr
	ClientPointers    int32
	_pad2             int32
}

const (
	// EXCEPTION_CONTINUE_SEARCH — те саме, що повертає власний обробник
	// рантайму Go: ОС продовжує штатний шлях обробки (WER → завершення з тим
	// самим кодом виключення). Повертати звідси EXCEPTION_EXECUTE_HANDLER не
	// можна: у фільтрі верхнього рівня «обробити» означало б завершити процес
	// самим обробником — поведінка інша, ніж до встановлення фільтра.
	exceptionContinueSearch = 0 // EXCEPTION_CONTINUE_SEARCH

	// Міні-дамп: сегменти даних + памʼять, на яку посилаються стеки
	// (достатньо для розбору збою, але в рази менше за повний дамп).
	miniDumpWithDataSegs    = 0x00000001
	miniDumpWithIndirectRef = 0x00000040
	miniDumpWithThreadInfo  = 0x00001000

	nativeDumpKeep     = 3  // максимум .dmp у logs/
	nativeLogTailLines = 40 // хвіст launcher.log у звіті
	// nativeDumpTimeout — скільки максимум чекаємо міні-дамп у середині
	// обробки збою. Дамп — це добре, але зависання процесу замість аварійного
	// завершення — погано: обробник ніколи не чекає без межі.
	nativeDumpTimeout = 15 * time.Second
)

// exceptionNames — людські назви NTSTATUS-кодів, які реально трапляються.
var exceptionNames = map[uint32]string{
	0x80000003: "STATUS_BREAKPOINT",
	0xC0000005: "STATUS_ACCESS_VIOLATION",
	0xC0000017: "STATUS_NO_MEMORY",
	0xC000001D: "STATUS_ILLEGAL_INSTRUCTION",
	0xC0000025: "STATUS_NONCONTINUABLE_EXCEPTION",
	0xC000008C: "STATUS_ARRAY_BOUNDS_EXCEEDED",
	0xC0000094: "STATUS_INTEGER_DIVIDE_BY_ZERO",
	0xC00000FD: "STATUS_STACK_OVERFLOW",
	0xC0000374: "STATUS_HEAP_CORRUPTION",
	0xC0000409: "STATUS_STACK_BUFFER_OVERRUN",
	0xC000041D: "STATUS_FATAL_USER_CALLBACK_EXCEPTION",
	0xC0000602: "STATUS_FAIL_FAST_EXCEPTION",
	0xE06D7363: "C++ exception (msvcp)",
}

var nativeCrashReported atomic.Bool

// nativeCrashLogsDir — куди класти звіт. За замовчуванням logs/ біля exe — той
// самий каталог, що повертає cfg.LogsDir() у backend. Змінна
// SHAURMA_CRASH_LOGS_DIR перекриває шлях (використовується тестом
// nativecrash_windows_test.go, щоб не смітити в теку збірки).
func nativeCrashLogsDir() string {
	if d := os.Getenv("SHAURMA_CRASH_LOGS_DIR"); d != "" {
		return d
	}
	return filepath.Join(exeDir(), "logs")
}

// installNativeCrashReporter викликається у main() до створення вікон.
// У батьківському процесі-наглядачі не потрібна (там немає ні UI, ні модулів).
//
// Ставимо ДВА перехоплювачі:
//  1. VEH (AddVectoredExceptionHandler, first=1) — бачить кожне виключення
//     найпершим і доходить навіть туди, де фільтр необроблених виключень не
//     викликають (пошкодження купи повідомляє сам диспетчер купи);
//  2. фільтр необроблених виключень — страховка для решти кодів (access
//     violation у чужому модулі, fail-fast тощо), коли виключення дійшло до
//     кінця ланцюжка SEH.
//
// Обидва шляхи ведуть в один nativeCrashFilter; повідомляємо лише ПЕРШЕ
// виключення в процесі, тож дублю не буде.
func installNativeCrashReporter() {
	// SHAURMA_NO_NATIVE_REPORTER=1 — не ставити VEH/UEF, щоб виключення
	// гарантовано дійшло до WER (LocalDumps) / зовнішнього перехоплювача.
	if os.Getenv("SHAURMA_NO_NATIVE_REPORTER") == "1" {
		return
	}
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	addVeh := kernel32.NewProc("AddVectoredExceptionHandler")
	setFilter := kernel32.NewProc("SetUnhandledExceptionFilter")
	veh := syscall.NewCallback(nativeCrashFilter)
	flt := syscall.NewCallback(nativeCrashFilter)
	// Вказівники тримаємо в пакетних змінних: трамполіни мають жити весь процес.
	nativeCrashVehPtr = veh
	nativeCrashFilterPtr = flt
	_, _, _ = addVeh.Call(1, veh) // first=1: наш обробник — попереду ланцюжка
	_, _, _ = setFilter.Call(flt)
	// Підвантажуємо dbghelp ЗАРАЗ: ліниве завантаження DLL під час обробки
	// виключення може не пройти (лоадер недоступний) — і дамп би не записався.
	_ = nativeWriteDumpProc.Find()
}

// nativeCrashVehPtr / nativeCrashFilterPtr — тримачі трамполінів
// syscall.NewCallback.
var (
	nativeCrashVehPtr    uintptr
	nativeCrashFilterPtr uintptr
)

// nativeCrashFilter — власне фільтр необроблених виключень. Викликається ОС на
// потоці, що впав, ПЕРЕД завершенням процесу.
func nativeCrashFilter(infoPtr uintptr) uintptr {
	var (
		code  uint32
		addr  uintptr
		recAt uintptr
	)
	if infoPtr != 0 {
		ep := (*exceptionPointers)(nativePtr(infoPtr))
		recAt = ep.ExceptionRecord
		if recAt != 0 {
			er := (*exceptionRecord)(nativePtr(recAt))
			code = er.ExceptionCode
			addr = er.ExceptionAddress
		}
	}
	// Найдешевші перевірки — першими: код не з відомих нам аварійних (а таких
	// виключень у процесі багато: Guard-сторінки Go, C++-винятки WebView2,
	// точки зупину) — геть без роботи.
	if _, known := exceptionNames[code]; !known || nativeCrashReported.Load() {
		return exceptionContinueSearch
	}
	modName, modOff := nativeModuleFor(addr)
	if !nativeWorthReporting(code, modName) {
		return exceptionContinueSearch
	}
	// Перше ВАРТЕ УВАГИ виключення — пишемо; подальші (у т.ч. вкладене, поки
	// ми самі працюємо з пошкодженою купою) пропускаємо.
	if nativeCrashReported.Swap(true) {
		return exceptionContinueSearch
	}
	defer func() { _ = recover() }() // звіт не має завалити обробник

	threadID := uint32(windows.GetCurrentThreadId())

	dir := nativeCrashLogsDir()
	_ = os.MkdirAll(dir, 0o755)

	name := fmt.Sprintf("0x%08X", code)
	if human, ok := exceptionNames[code]; ok {
		name += " " + human
	}
	var where strings.Builder
	fmt.Fprintf(&where, "0x%X", addr)
	if modName != "" {
		fmt.Fprintf(&where, " (%s+0x%X)", modName, modOff)
	}
	dumpName, dumpSize, dumpErr := nativeWriteDump(dir, threadID, infoPtr)

	var b strings.Builder
	fmt.Fprintf(&b, "[%s] НЕОБРОБЛЕНЕ ВИКЛЮЧЕННЯ — аварійне завершення процесу\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&b, "  код:      %s\n", name)
	fmt.Fprintf(&b, "  адреса:   %s\n", where.String())
	if modName == "" {
		fmt.Fprintf(&b, "  модуль:   не визначено (адреса поза відомими модулями або модуль вивантажено)\n")
	}
	fmt.Fprintf(&b, "  потік:    %d, pid: %d\n", threadID, os.Getpid())
	fmt.Fprintf(&b, "  exe:      %s\n", os.Args[0])
	if dumpName != "" {
		fmt.Fprintf(&b, "  дамп:     %s (%d байт)\n", dumpName, dumpSize)
	} else if dumpErr != nil {
		fmt.Fprintf(&b, "  дамп:     не записано (%v)\n", dumpErr)
	} else {
		fmt.Fprintf(&b, "  дамп:     не записано (досягнуто ліміту %d дампів)\n", nativeDumpKeep)
	}
	fmt.Fprintf(&b, "  підказка: STATUS_HEAP_CORRUPTION/ACCESS_VIOLATION у чужому модулі =\n"+
		"            збій у нативному коді (WebView2, драйвер GPU, антивірус/оверлей),\n"+
		"            а не в Go: шукайте назву модуля вище у списку встановлених програм.\n")
	if tail := nativeTail(filepath.Join(dir, "launcher.log"), nativeLogTailLines); tail != "" {
		fmt.Fprintf(&b, "  --- останні %d рядків launcher.log ---\n%s\n", nativeLogTailLines, tail)
	}
	b.WriteString("\n")

	report := filepath.Join(dir, "crash-native.log")
	if f, err := os.OpenFile(report, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		_, _ = f.WriteString(b.String())
		_ = f.Close()
	}
	// Дублюємо коротко в bootstrap.log — поряд з рядком supervisor'а, щоб причина
	// й наслідок читались в одному файлі.
	bootstrapLog(exeDir(), "native crash: %s at %s (pid %d, dump %q)", name, where.String(), os.Getpid(), dumpName)
	return exceptionContinueSearch
}

// nativeWorthReporting — чи варто писати звіт саме для цього коду/місця.
// Звичайні «робочі» виключення (точка зупину, C++-винятки, переповнення
// стека) ігноруємо: їх обробляє сам застосунок, і звіт про них лише забивав
// би логи.
func nativeWorthReporting(code uint32, moduleName string) bool {
	switch code {
	case 0x80000003, 0x80000004, 0xE06D7363, 0xC00000FD, 0xC0000025:
		// Точка зупину, single-step, C++-виняток (msvcp), переповнення
		// стека (його обробляє рантайм Go), non-continuable — не наша аварія.
		return false
	case 0xC0000005:
		// Access violation у САМОМУ нашому exe — майже завжди Go-код, який
		// рантайм перетворює на паніку (процес живе далі). Цікаві саме AV у
		// ЧУЖИХ модулях: WebView2, драйвер GPU, антивірус/оверлей.
		return !nativeIsOwnExe(moduleName)
	}
	return true
}

// nativeIsOwnExe — чи це модуль нашого власного exe (а не стороння DLL).
func nativeIsOwnExe(moduleName string) bool {
	if moduleName == "" || len(os.Args) == 0 {
		return false
	}
	return strings.EqualFold(filepath.Base(moduleName), filepath.Base(os.Args[0]))
}

// nativeCrashHint — рядок для наглядача (supervisor.go): де шукати причину,
// якщо дочірній процес щойно залишив звіт нативного збою. Без цього в логу
// лишався б лише рядок «дочірній процес завершився аварійно: exit status
// 0xc0000374» — без жодної підказки, куди дивитись. Порожній рядок —
// звіту про щойний збій немає.
func nativeCrashHint(installDir string) string {
	dir := filepath.Join(installDir, "logs")
	st, err := os.Stat(filepath.Join(dir, "crash-native.log"))
	if err != nil || st.IsDir() {
		return ""
	}
	// Файл має бути СВІЖИМ: старий звіт від попереднього збою (багато днів
	// тому) не пояснював би те, що сталося зараз.
	if time.Since(st.ModTime()) > 15*time.Minute {
		return ""
	}
	return fmt.Sprintf(" — причину див. %s (модуль і зсув збою; міні-дампи у %s)",
		filepath.Join(dir, "crash-native.log"), dir)
}

// nativeModuleFor — назва модуля і зсув у ньому для адреси. Спершу шукаємо
// модуль, у чиї межі потрапляє адреса; якщо такий не знайдено (модуль уже
// вивантажено) — найближчий нижчий (зазвичай саме він і винен).
func nativeModuleFor(addr uintptr) (string, uintptr) {
	if addr == 0 {
		return "", 0
	}
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPMODULE|windows.TH32CS_SNAPMODULE32, windows.GetCurrentProcessId())
	if err != nil {
		return "", 0
	}
	defer windows.CloseHandle(snap)

	var me windows.ModuleEntry32
	me.Size = uint32(unsafe.Sizeof(me))
	bestName, bestBase := "", uintptr(0)
	for err := windows.Module32First(snap, &me); err == nil; err = windows.Module32Next(snap, &me) {
		base := me.ModBaseAddr
		if base == 0 {
			continue
		}
		name := windows.UTF16ToString(me.Module[:])
		if me.ModBaseSize > 0 && addr >= base && addr < base+uintptr(me.ModBaseSize) {
			return name, addr - base
		}
		if base <= addr && base > bestBase {
			bestName, bestBase = name, base
		}
	}
	if bestName != "" {
		return bestName, addr - bestBase
	}
	return "", 0
}

// nativeWriteDump пише міні-дамп через dbghelp!MiniDumpWriteDump.
// Повертає (імʼя файлу, розмір); якщо дамп не записано — ("", код помилки
// WinAPI або 0, коли причина невідома), щоб звіт міг її назвати.
func nativeWriteDump(dir string, threadID uint32, infoPtr uintptr) (name string, size int64, err error) {
	if nativeDumpsIn(dir) >= nativeDumpKeep {
		return "", 0, nil // цикл аварійних перезапусків не має забити диск
	}
	if err := nativeWriteDumpProc.Find(); err != nil {
		return "", 0, err
	}
	name = fmt.Sprintf("crash-%s-pid%d.dmp", time.Now().Format("20060102-150405"), os.Getpid())
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		return "", 0, err
	}

	mei := minidumpExceptionInformation{
		ThreadID:          threadID,
		ExceptionPointers: infoPtr,
		ClientPointers:    1, // EXCEPTION_POINTERS у цьому ж процесі
	}
	type dumpResult struct {
		size int64
		err  error
	}
	done := make(chan dumpResult, 1)
	go func() {
		// Дамп — НА ІНШОМУ ПОТОЦІ. Усередині обробки виключення прямий виклик
		// MiniDumpWriteDump НЕ проходить: dbghelp читає стеки потоків і отримує
		// ERROR_INVALID_ADDRESS, бо потік збою зараз у диспетчері виключень.
		// Свіжий потік дає йому звичайний контекст.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		proc, _ := windows.GetCurrentProcess()
		flags := uintptr(miniDumpWithDataSegs | miniDumpWithIndirectRef | miniDumpWithThreadInfo)
		// Спроба 1 — з контекстом виключення (є стек і регістри того, хто впав).
		r, _, callErr := nativeWriteDumpProc.Call(
			uintptr(proc), uintptr(os.Getpid()), f.Fd(), flags,
			uintptr(unsafe.Pointer(&mei)), 0, 0,
		)
		if r == 0 {
			// Спроба 2 — без контексту виключення. На частині систем dbghelp
			// відмовляється читати EXCEPTION_POINTERS потоку, який зараз у
			// диспетчері виключень (ERROR_INVALID_ADDRESS), а сам дамп (стеки,
			// модулі, памʼять) усе одно корисний.
			r, _, callErr = nativeWriteDumpProc.Call(
				uintptr(proc), uintptr(os.Getpid()), f.Fd(), flags, 0, 0, 0,
			)
		}
		if r == 0 {
			if errno, ok := callErr.(syscall.Errno); ok && errno != 0 {
				done <- dumpResult{err: fmt.Errorf("MiniDumpWriteDump повернув 0 (WinAPI: %v)", errno)}
				return
			}
			done <- dumpResult{err: errors.New("MiniDumpWriteDump повернув 0")}
			return
		}
		st, statErr := f.Stat()
		if statErr != nil {
			done <- dumpResult{err: statErr}
			return
		}
		done <- dumpResult{size: st.Size()}
	}()

	select {
	case r := <-done:
		_ = f.Close()
		if r.err != nil {
			// СКРИВАТИ файл треба ПІСЛЯ закриття хендла: Windows відмовляється
			// видаляти відкритий файл, і в лозі лишався б порожній .dmp.
			_ = os.Remove(path)
			return "", 0, r.err
		}
		return name, r.size, nil
	case <-time.After(nativeDumpTimeout):
		// Обробник збою не має зависнути: краще лишити текстовий звіт, ніж
		// підвісити процес, який і так уже вмирає.
		_ = f.Close()
		_ = os.Remove(path)
		return "", 0, fmt.Errorf("дамп не записано за %s", nativeDumpTimeout)
	}
}

// nativeWriteDumpProc — dbghelp!MiniDumpWriteDump. Резолвимо його вже при
// ВСТАНОВЛЕННІ перехоплювача (installNativeCrashReporter), а не в момент
// збою: під час обробки виключення лоадер Windows може бути недоступним
// (тримає лок завантаження DLL) — і «дамп не записано» було б саме з цієї
// причини.
var nativeWriteDumpProc = syscall.NewLazyDLL("dbghelp.dll").NewProc("MiniDumpWriteDump")

// nativeDumpsIn — скільки .dmp уже лежить у теці.
func nativeDumpsIn(dir string) int {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range ents {
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".dmp") {
			n++
		}
	}
	return n
}

// nativeTail — останні n рядків файлу (порожній рядок, якщо файлу немає).
func nativeTail(path string, n int) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(b), "\r\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// nativePtr перетворює адресу (uintptr) з системного виклику у вказівник.
// Той самий прийом, що в backend/clipboard_windows.go: vet і checkptr не бачать
// «підозрілої» прямої конвертації — а вона тут неминуча (адресу дає ОС).
func nativePtr(p uintptr) unsafe.Pointer {
	var out unsafe.Pointer
	*(*uintptr)(unsafe.Pointer(&out)) = p
	return out
}

// nativeDumpList — відсортований список дампів (для діагностики/тестів).
func nativeDumpList(dir string) []string {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".dmp") {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}
