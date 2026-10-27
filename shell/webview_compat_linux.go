//go:build linux

package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// applyLinuxWebViewCompatEnv виставляє змінні середовища для
// WebKitGTK/Mesa/EGL ДО створення першого GTK/WebKit-контексту
// (application.New). Викликається з main() першою чергою на Linux.
//
// Проблема, яку це виправляє: "вікно повністю біле" — сторінка фізично
// не рендериться. У логах це супроводжується:
//
//	libEGL warning: failed to get driver name for fd -1
//	libEGL warning: MESA-LOADER: failed to retrieve device information
//	MESA: error: ZINK: failed to choose pdev
//	libEGL warning: egl: failed to create dri2 screen
//
// Це означає, що WebKitGTK намагається рендерити через апаратне
// прискорення (DMA-BUF composited rendering через EGL/GL), а драйвер
// GPU в поточному оточенні (типово: WSL2 без прокинутого GPU, віддалений
// X11/VNC, застарілий чи відсутній Mesa-драйвер, Wayland-less headless
// сесія) не може віддати робочий EGL-дисплей. WebKit у такому разі МОВЧКИ
// (без панічної помилки в застосунку) не малює композитний шар — звідси
// суцільно біле вікно замість краху.
//
// ВАЖЛИВО (виправлення «лаунчер гальмує/фрізить на Linux»): раніше три
// змінні нижче виставлялись БЕЗУМОВНО — тобто КОЖНОМУ користувачеві
// Linux, включно з машинами, де GPU працює ідеально. Наслідок: увесь UI
// лаунчера рендериться Cairo/llvmpipe на CPU — скрол списку модів,
// анімації, блюр і будь-яка перемальовка стають ривками, а процес їсть
// помітно більше CPU і пам'яті під софтверні буфери. Тепер софтверний
// фолбек вмикається ЛИШЕ коли робочого GPU справді нема (hasWorkingGPU())
// — тобто рівно в тих оточеннях, заради яких він і писався (WSL2,
// headless, VNC). Для NVIDIA з пропрієтарним драйвером лишаємо вимкненим
// тільки DMABUF-рендерер: це відома несумісність WebKitGTK саме з ним,
// композитинг при цьому працює нормально.
//
// Виставляється лише якщо змінна ще НЕ задана — не перебиває свідомий
// вибір користувача/дистрибутива (напр. якщо хтось вже експортував
// WEBKIT_DISABLE_COMPOSITING_MODE=0, бо в нього GPU реально працює).
//
// Кожну виставлену НАМИ змінну записуємо в SHAURMA_COMPAT_ENV, щоб
// дочірні процеси (гра, системний браузер, файловий менеджер) могли
// прибрати їх зі свого середовища — див. internal/childenv (backend) і
// browser_open_linux.go (shell). Без цього Minecraft успадковував
// LIBGL_ALWAYS_SOFTWARE=1 і запускався на програмному рендерері, а
// системний браузер для входу в Microsoft — так само.
func applyLinuxWebViewCompatEnv() {
	switch {
	case !hasWorkingGPU():
		// Нема робочого DRI-пристрою (WSL2 без GPU, headless, VNC) —
		// повний софтверний шлях, як і було раніше.
		compatSetIfUnset("WEBKIT_DISABLE_COMPOSITING_MODE", "1")
		compatSetIfUnset("WEBKIT_DISABLE_DMABUF_RENDERER", "1")
		compatSetIfUnset("LIBGL_ALWAYS_SOFTWARE", "1")
	case gpuSafeModeRequested():
		// Безпечний режим після крашу пробного запуску (supervisor із
		// gpu_recovery_linux.go перезапустив нас з SHAURMA_NO_HARDWARE_
		// ACCELERATION=1): GPU за евристикою Є (/dev/dri доступний), але
		// він щойно вбив процес — вимикаємо ВСЕ апаратне, не чекаючи на
		// повторний краш. LIBGL_ALWAYS_SOFTWARE тут не потрібен: компо-
		// зитинг і dma-buf вимкнені, а GL-пути, що лишились, драйвер
		// обробляє сам; форсувати llvmpipe поверх — зайвий ризик.
		compatSetIfUnset("WEBKIT_DISABLE_COMPOSITING_MODE", "1")
		compatSetIfUnset("WEBKIT_DISABLE_DMABUF_RENDERER", "1")
	case hasNvidiaProprietary():
		// GPU є, але драйвер NVIDIA: dma-buf шлях WebKitGTK з ним
		// стабільно ламається (чорні/порожні ділянки). Композитинг
		// лишаємо УВІМКНЕНИМ — він там працює.
		compatSetIfUnset("WEBKIT_DISABLE_DMABUF_RENDERER", "1")
	}

	// «Економний режим» із налаштувань лаунчера (ui.json) — теж env для
	// WebKitGTK, тому й теж мусить бути виставлений тут, до першого
	// GTK/WebKit виклику.
	applyLowMemoryWebKitEnv()

	// Маркер для дочірніх процесів (гра, системний браузер, файловий
	// менеджер): перелік змінних, які виставив САМ лаунчер, щоб вони не
	// успадкували, напр., LIBGL_ALWAYS_SOFTWARE=1 і не пішли на
	// програмному рендерері.
	if len(compatEnvAppliedKeys) > 0 {
		keys := append(append([]string{}, compatEnvAppliedKeys...), compatEnvMarker)
		os.Setenv(compatEnvMarker, strings.Join(keys, ","))
	}
}

// compatEnvAppliedKeys — ключі, виставлені САМИМ лаунчером (усіма
// блоками цього файлу). Заповнюється лише через compatSetIfUnset.
var compatEnvAppliedKeys []string

// compatSetIfUnset виставляє змінну, якщо вона ще не задана (свідомий
// вибір користувача/дистрибутива не перебиваємо), і запам'ятовує ключ для
// маркера.
func compatSetIfUnset(key, value string) {
	if _, ok := os.LookupEnv(key); ok {
		return
	}
	os.Setenv(key, value)
	compatEnvAppliedKeys = append(compatEnvAppliedKeys, key)
}

// applyLowMemoryWebKitEnv — «економний режим» (Налаштування →
// Продуктивність) для WebKitGTK: головне джерело памʼяті у лаунчері — не
// наш Go-код, а WebKit-процеси (WebProcess/NetworkProcess/GPUProcess), і
// найбільший шматок там — буфери апаратного композитингу (accelerated
// compositing) та dma-buf-рендерер.
//
//	WEBKIT_DISABLE_COMPOSITING_MODE=1 — рендеринг без GPU-композитингу:
//	  менше буферів, менше VRAM/RAM під шари, менше роботи GPU-процесу;
//	  ціна — плавність анімацій і blur у UI (тому це ОПЦІЯ, а не дефолт);
//	WEBKIT_DISABLE_DMABUF_RENDERER=1 — без dma-buf шляху (у ньому ж
//	  WebKit тримає проміжні буфери кадрів).
//
// Опція безпечна на будь-якому GPU/DRI: це вимкнення шляхів, а не вимога
// до драйвера (саме тому вона не конфліктує з hasWorkingGPU-гілкою вище —
// там софтверний рендеринг форсується ЗАРАДИ БІЛОГО ВІКНА, тут — ЗАРАДИ
// ПАМ'ЯТІ).
func applyLowMemoryWebKitEnv() {
	// Безпечний режим після GPU-крашу (gpu_recovery_linux.go) вимикає
	// ті самі шляхи, що й економний режим, — застосовуємо його
	// безумовно, незалежно від налаштування користувача.
	if gpuSafeModeRequested() {
		compatSetIfUnset("WEBKIT_DISABLE_COMPOSITING_MODE", "1")
		compatSetIfUnset("WEBKIT_DISABLE_DMABUF_RENDERER", "1")
		return
	}
	if !lowMemoryModeEnabled() {
		return
	}
	compatSetIfUnset("WEBKIT_DISABLE_COMPOSITING_MODE", "1")
	compatSetIfUnset("WEBKIT_DISABLE_DMABUF_RENDERER", "1")
}

// compatEnvMarker — ім'я змінної зі списком compat-змінних, виставлених
// САМИМ лаунчером. Контракт із backend.so: internal/childenv.CompatEnvMarker
// містить рівно цей самий рядок — при зміні правити ОБИДВА місця.
const compatEnvMarker = "SHAURMA_COMPAT_ENV"

// hasWorkingGPU — чи є в цьому оточенні реальний GPU, доступний для
// рендерингу. Результат мемоізується (sync.OnceValue): його питають і
// env-фолбек на старті, і кожне створення вікна (gpu_policy_linux.go), а
// перевірка торкається диска (/dev/dri) — повторювати її на кожне вікно
// сенсу немає, тим паче результат у межах процесу не змінюється.
var hasWorkingGPU = sync.OnceValue(detectWorkingGPU)

// detectWorkingGPU — сама перевірка. Навмисно дешева і консервативна:
// будь-який сумнів трактується як «GPU нема» (тоді вмикається софтверний
// фолбек — гірше за продуктивність, але гарантовано без білого вікна).
func detectWorkingGPU() bool {
	// Аварійний вимикач для тих, у кого GPU точно працює, а евристика
	// нижче чомусь не пропустила його (нестандартний драйвер, GPU
	// прокинуто в контейнер без /dev/dri, рідкісний blob). Без цього
	// єдиним способом вийти з софтверного рендерингу було б редагування
	// коду: лаунчер гальмував би на llvmpipe при живому GPU.
	if envFlagTrue("SHAURMA_FORCE_GPU") {
		return true
	}
	// Явне прохання «тільки софт» — дзеркало до попереднього (напр.
	// відомо, що драйвер малює артефакти).
	if envFlagTrue("SHAURMA_FORCE_SOFTWARE") {
		return false
	}
	// WSL2: /dev/dri може існувати (WSLg прокидає d3d12), але EGL-шлях
	// WebKitGTK там історично давав саме ті libEGL/ZINK помилки, заради
	// яких цей файл і з'явився. Лишаємо софтверний фолбек.
	if isWSL() {
		return false
	}
	// Немає ні Wayland-, ні X11-дисплея — рендерити все одно нікуди.
	if os.Getenv("WAYLAND_DISPLAY") == "" && os.Getenv("DISPLAY") == "" {
		return false
	}
	// Render-node (сучасний шлях) або card-пристрій (старіші ядра).
	nodes, _ := filepath.Glob("/dev/dri/renderD*")
	if len(nodes) == 0 {
		nodes, _ = filepath.Glob("/dev/dri/card*")
	}
	for _, p := range nodes {
		// Саме відкриття на читання-запис: у Flatpak/контейнері без
		// --device=dri файл може бути видимим, але недоступним.
		f, err := os.OpenFile(p, os.O_RDWR, 0)
		if err != nil {
			continue
		}
		f.Close()
		return true
	}
	return false
}

// isWSL — чи виконуємось усередині WSL (там GPU-шлях WebKitGTK ненадійний).
func isWSL() bool {
	if os.Getenv("WSL_DISTRO_NAME") != "" || os.Getenv("WSL_INTEROP") != "" {
		return true
	}
	data, err := os.ReadFile("/proc/version")
	if err != nil {
		return false
	}
	v := strings.ToLower(string(data))
	return strings.Contains(v, "microsoft") || strings.Contains(v, "wsl")
}

// envFlagTrue — чи змінна середовища виставлена як «увімкнено»
// (1/true/yes/on, без урахування регістру). Порожнє значення і будь-яке
// інше — false, щоб випадкове `SHAURMA_FORCE_GPU=` не вмикало режим.
func envFlagTrue(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// hasNvidiaProprietary — чи завантажений пропрієтарний драйвер NVIDIA
// (nouveau сюди не потрапляє: у нього немає /proc/driver/nvidia).
func hasNvidiaProprietary() bool {
	for _, p := range []string{"/proc/driver/nvidia/version", "/sys/module/nvidia/version", "/dev/nvidiactl"} {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}
