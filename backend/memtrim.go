package main

// memtrim.go — керування «профілем памʼяті» лаунчера: як часто збирач
// сміття повертає сторінки ОС і чи вмикати агресивніший режим (налаштування
// «Економний режим»).
//
// ОБСЯГ ЦЬОГО ФАЙЛУ (ВАЖЛИВО, ЩОБ НЕ ЧЕКАТИ ЗАЙВОГО): усе нижче керує
// ЛИШЕ пам'яттю backend.so — Go-хіпом і glibc-аренами всередині ЦЬОГО
// cgo-модуля. Це той процес, що на Linux звично важить ~400 МБ. Окремо
// від нього живе WebKitGTK (WebProcess/NetworkProcess/GPUProcess —
// зазвичай ще ~300 МБ, саме ті "webview жере 300 МБ" з відгуків
// користувачів) — цей файл ЙОГО пам'яттю не керує і керувати не може:
// WebKit — інший процес, і Go-рантайм backend.so не має до нього прямого
// доступу. Приховування вікна в трей (shell: win.Hide()) НЕ гарантує, що
// WebKit сам звільнить чи хоча б призупинить WebProcess — це залежить
// від внутрішнього WebKitMemoryPressureMonitor (є, вмикається типово за
// замовчуванням у сучасних WebKitGTK, конфігурується лише при створенні
// WebContext, до появи першого вікна — Wails v3 наразі не дає для цього
// публічного API) і від того, чи WebKitGTK взагалі коректно розпізнає
// "вікно приховане" на конкретному композиторі (відомий незакритий клас
// проблем у самому WebKitGTK, напр. bugs.webkit.org #285167). Тобто
// частина скарги "лаунчер+webview їдять 700 МБ навіть у треї" —
// СПРАВЖНЯ і ЦИМ ФАЙЛОМ НЕ ВИРІШУЄТЬСЯ; нижче лікується лише половина
// (backend.so), а WebKit-половина лишається відкритим питанням до появи
// в Wails v3 контролю над WebKitMemoryPressureSettings.
//
// ПРОБЛЕМА, ЯКУ ЦЕ ЛІКУЄ (В МЕЖАХ backend.so). На Linux (і меншою мірою
// на Windows) backend.so виглядає як «з'їв 400 МБ і не віддає»: Go-
// рантайм звільняє об'єкти, але сторінки ядру віддає ліниво (за власним
// таймером — до кількох хвилин), а glibc-арени від рядків, що
// перетинали C ABI (JSON backend_invoke), не віддаються взагалі без
// malloc_trim. Раніше памʼять поверталась лише «на подію» (сканування
// модів, очищення кешів) — якщо користувач просто згорнув лаунчер у
// трей і запустив гру, пік RSS backend.so лишався таким назавжди.
//
// ЩО РОБИМО:
//   - прибиральник (startMemoryTrimmer) раз на memoryTrimInterval кличе
//     platform-специфічний maybeReleaseMemory (FreeOSMemory + malloc_trim на
//     Linux, no-op на решту платформ) — тобто навіть у треї/з грою в фоні
//     RSS backend.so поступово повертається до реально зайнятого;
//   - «економний режим» (settings.LowMemoryMode) додатково опускає GOGC —
//     збирач працює вдвічі частіше, тому пік купи менший (для слабких
//     машин з 4–8 ГБ ОЗП важливіше втримати пік, ніж зекономити 1% CPU),
//     і паралельно (shell/webview_compat_linux.go: applyLowMemoryWebKitEnv)
//     вимикає WebKit-композитинг — це єдиний наявний важіль на WebKit-
//     половину пам'яті, і він статичний (діє з першого вікна), а не
//     реакція на згортання в трей.
//
// Сама реалізація звільнення — платформна: memtrim_linux.go (cgo +
// malloc_trim) і memtrim_other.go (порожні no-op).

import (
	"shaurma-launcher-backend/internal/crashguard"
	"context"
	"os"
	"runtime"
	"runtime/debug"
	"time"
)

// memoryTrimInterval — як часто повертаємо вільні сторінки ОС, працюючи
// у фоні. 90 с (раніше 5 хв): куп лаунчера невелика, тож повний GC
// коштує лічені мілісекунди, а користувачі скаржились саме на «їсть RAM»
// у фоні. maybeReleaseMemory додатково тротлиться (memReleaseInterval).
const memoryTrimInterval = 90 * time.Second

// startMemoryTrimmer запускає фоновий прибиральник памʼяті до завершення
// ctx. Викликається один раз зі ServiceStartup.
func (a *App) startMemoryTrimmer() {
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	go func() {
		defer crashguard.Recover("memtrim.go:72")
		ticker := time.NewTicker(memoryTrimInterval)
		defer ticker.Stop()
		lastReport := time.Time{}
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				maybeReleaseMemory()
				// Звіт пам'яті (memreport.go) — після прибирання, щоб цифри
				// показували реально зайняте, а не сміття перед GC.
				if time.Since(lastReport) >= memReportInterval {
					lastReport = time.Now()
					a.reportMemory()
				}
			}
		}
	}()
}

// trimNowForHiddenWindow — вікно щойно приховано (трей / старт гри): саме
// той момент, коли зайва пам'ять лаунчера нікому не потрібна.
//
// Робить дві речі, які не робить фоновий прибиральник:
//   1. releaseMemoryNow — повний цикл GC + негайне повернення сторінок
//      Go-купи (можна було б покластись на maybeReleaseMemory, але він
//      тротлиться 15 с, а приховування — рідка й найкраща подія);
//   2. trimProcessWorkingSets (Windows) — робочі набори нашого процесу І
//      WebView2-нащадків: FreeOSMemory їх не чіпає, а в Диспетчері завдань
//      саме вони й «важать» (memtrim_windows.go).
//
// Асинхронно: приховування вікна не має чекати на повний цикл GC, інакше
// кнопка «згорнути в трей» відчувалась би як зависання.
func trimNowForHiddenWindow() {
	go func() {
		defer crashguard.Recover("trimNowForHiddenWindow")
		releaseMemoryNow()
		trimmed := trimProcessWorkingSets()
		// Скільки з «сотень мегабайт» — це справді Go-купа backend.dll, а
		// скільки — нативний Chromium: без цього числа будь-яка скарга
		// «бекенд їсть 100 МБ» знову зводиться до здогадів.
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		logWorkingSetTrim(trimmed, ms.HeapAlloc)
	}()
}

// applyMemoryProfile застосовує профіль памʼяті з налаштувань. Зміна
// налаштування застосовується одразу (без перезапуску): GOGC не потребує
// перестворення вікон чи рантайму.
func (a *App) applyMemoryProfile(lowMemoryMode bool) {
	// Якщо користувач/дистрибутив виставив GOGC у середовищі — це його
	// свідомий вибір (напр. GOGC=off для дебагу), не перебиваємо.
	if os.Getenv("GOGC") != "" {
		return
	}
	// М'яка стеля купи: це НЕ жорсткий ліміт (при більшому живому наборі
	// рантайм просто працює як раніше), а підказка GC — коли купа
	// наближається до межі, збирати сміття раніше, а не чекати «+100%».
	if lowMemoryMode {
		// 25: цикл GC починається, коли купа виросла лише на 25% — мінімальний
		// пік, трохи більше CPU на збирання. Стеля 64 МБ — м'яка (див. вище).
		debug.SetGCPercent(25)
		debug.SetMemoryLimit(64 << 20)
		return
	}
	// Дефолт (раніше GOGC=100 + 256 МБ): backend лаунчера в нормі тримає
	// ~20–30 МБ живих даних, тож GOGC=50 тримає пік ≈ 1.5× живого набору
	// замість 2×, а стеля 96 МБ змушує GC працювати частіше ще до того,
	// як купа розрослась. Це м'яка межа: при більшому живому наборі
	// (великий скан модів) рантайм просто не падає і працює як раніше.
	debug.SetGCPercent(50)
	debug.SetMemoryLimit(96 << 20)
}
