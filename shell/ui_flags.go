package main

// ui_flags.go — прапорці UI для shell з налаштувань лаунчера.
//
// Налаштування лаунчера живуть у SQLite (ConfigDir/shaurma.db), до якої
// shell не має доступу (він не вантажить драйвер SQLite). Але деякі з них
// мають застосуватись ДО першого вікна WebView2 — це можна зробити лише
// зі shell'а (він створює вікна). Тому backend дублює кілька прапорців у
// простий JSON ConfigDir/ui.json (див. backend/app.go writeUIFlagsFile), а
// shell читає його на старті, до application.New.
//
// ВАЖЛИВО про механізм передачі: env-змінна WEBVIEW2_ADDITIONAL_BROWSER_
// ARGUMENTS ТУТ НЕ ПРАЦЮЄ — Wails v3 сам обнуляє її перед створенням
// середовища (preventEnvAndRegistryOverrides, щоб не дати застосунку
// перебити власні аргументи). Єдиний правильний шлях — передати аргументи
// через application.Options.Windows.AdditionalBrowserArgs (вони
// застосовуються глобально до ВСІХ вікон, бо WebView2 має одне спільне
// середовище).

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// uiFlags — підмножина налаштувань, які shell читає на старті.
type uiFlags struct {
	// HardwareAcceleration — GPU-прискорення WebView2-фронтенду. false →
	// усі вікна отримують --disable-gpu (вимкнення потребує перезапуску,
	// як і підказує напис у налаштуваннях).
	HardwareAcceleration *bool `json:"hardwareAcceleration"`
	// LowMemoryMode — «економний режим» (Налаштування → Продуктивність):
	// менша памʼять лаунчера за рахунок софтверного рендерингу WebKit
	// (див. applyLowMemoryWebKitEnv у webview_compat_linux.go) і вимкненого
	// GPU-прискорення самого webview-вікна (window_exec.go: WebviewGpuPolicy).
	// Теж потребує перезапуску — тому живе у ui.json, а не в bridge-виклику.
	LowMemoryMode *bool `json:"lowMemoryMode"`
}

// readUIFlags читає прапорці UI (ui.json у теці встановлення, пише його
// backend — див. backend/app.go writeUIFlagsFile). Порожній/битий файл або
// відсутність поля = дефолт (nil-поля, тобто «як завжди»).
func readUIFlags() uiFlags {
	var f uiFlags
	path := uiFlagsPath()
	if path == "" {
		return f
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return f
	}
	_ = json.Unmarshal(data, &f)
	return f
}

// lowMemoryModeEnabled — чи ввімкнено «економний режим». Викликається і
// на старті (env для WebKit, до першого GTK-виклику), і при створенні
// кожного вікна (GPU-політика webview).
func lowMemoryModeEnabled() bool {
	f := readUIFlags()
	return f.LowMemoryMode != nil && *f.LowMemoryMode
}

// launcherWebviewGpuPolicy — апаратне прискорення webview-вікна.
// Дефолт — як було досі (прискорення увімкнене) і додатково поважає
// вимкнене апаратне прискорення з ui.json: раніше цей прапорець на
// Windows лише додавав --disable-gpu, а на Linux не діяв взагалі — тепер
// діє (скоруючись політикою NEVER), бо GTK4 не має окремого шляху
// «--disable-gpu».
//
// На Linux ще два випадки примусового NEVER:
//   - робочого GPU нема (див. noWorkingGpu) — щоб політика не суперечила
//     env-фолбеку зі webview_compat_linux.go (ALWAYS + софтверний env =
//     WebKit пробує обидва шляхи і гальмує/блимає білим замість того,
//     щоб один раз піти софтом);
//   - безпечний режим після крашу пробного запуску (gpu_recovery_linux.go:
//     supervisor перезапустив нас із SHAURMA_NO_HARDWARE_ACCELERATION=1) —
//     GPU за евристикою є, але він щойно вбив процес, тож прискорення
//     вимикаємо примусово, не чекаючи на повторний краш.
func launcherWebviewGpuPolicy() application.WebviewGpuPolicy {
	if safeModeRequested() || lowMemoryModeEnabled() || noWorkingGpu() || gpuSafeModeRequested() {
		return application.WebviewGpuPolicyNever
	}
	f := readUIFlags()
	if f.HardwareAcceleration != nil && !*f.HardwareAcceleration {
		return application.WebviewGpuPolicyNever
	}
	return application.WebviewGpuPolicyAlways
}

// launcherBrowserArgs повертає додаткові аргументи WebView2 для ВСІХ вікон
// (передаються в WindowsOptions.AdditionalBrowserArgs). Коли користувач
// вимкнув GPU-прискорення — ["--disable-gpu"], інакше nil. Відсутність
// файлу або прапорця = дефолт (GPU увімкнено).
func launcherBrowserArgs() []string {
	// Безпечний режим після повторюваних збоїв (supervisor.go:
	// SHAURMA_SAFE_MODE=1): GPU-композитинг WebView2 — найбільша нативна
	// підсистема в нашому процесі, і саме вона бере участь у пошкодженні
	// купи. Без неї UI лишається повністю робочим (лише менш плавний).
	if safeModeRequested() {
		return []string{"--disable-gpu"}
	}
	// «Економний режим» на Windows — це той самий --disable-gpu: WebView2
	// без GPU-композитингу тримає помітно менше памʼяті (розплата — менш
	// плавні анімації), і саме заради цього режим і вмикають на слабкому
	// залізі.
	f := readUIFlags()
	if f.LowMemoryMode != nil && *f.LowMemoryMode {
		return []string{"--disable-gpu"}
	}
	if f.HardwareAcceleration == nil || *f.HardwareAcceleration {
		return nil
	}
	return []string{"--disable-gpu"}
}

// safeModeRequested — процес запущено наглядом у БЕЗПЕЧНОМУ РЕЖИМІ
// (supervisor.go: після повторюваних швидких збоїв). Один env-прапорець на
// весь застосунок: shell читає його тут (GPU-прапорці), backend —
// safeModeActive() (опитування буфера обміну, Discord RPC, таскбар).
func safeModeRequested() bool {
	return os.Getenv(supSafeEnv) != ""
}

// uiFlagsPath — шлях до ui.json у стабільній теці конфігурації. Backend
// пише його в a.cfg.ConfigDir() = installDir (та сама тека, де лежить
// launcher-location.json поруч з exe). Для shell це тека самого exe —
// installDir і є тека встановлення, тож шляхи збігаються.
func uiFlagsPath() string {
	return filepath.Join(exeDir(), "ui.json")
}
