package main

// main_thread_ready.go — захист від гонки старту, знайденої емпірично на
// Linux (див. коментар у main.go): backend.so/updater.so можуть послати
// перший колбек (cgoEmit і т.п.) РАНІШЕ, ніж встиг стартувати
// application.Get()'s внутрішній диспетчер головного потоку — той
// реально ініціалізується лише ВСЕРЕДИНІ app.Run(), не в
// application.New(). Виклик application.InvokeAsync/InvokeSync у це
// вікно падає з SIGSEGV (nil pointer у dispatchOnMainThread), хоча сам
// application.Get() вже повертає не-nil *App.
//
// Рішення не покладається на конкретний (недокументований у цій версії
// alpha) хук життєвого циклу Wails — натомість самі колбеки чекають на
// закриття каналу mainThreadReady перед першим InvokeAsync/InvokeSync.
// Канал закривається (рівно один раз, sync.Once) з місця, де ми ТОЧНО
// знаємо, що головний потік живий: із самого початку showUpdaterSplash()/
// loadBackendAndShowMain() ці функції викликаються вже з тіла
// initModule-послідовності, тому маркер ставимо трохи раніше — одразу
// після успішного першого windowCreate("main"/"updater-splash", ...),
// яке саме і є першою операцією, що реально торкається UI-потоку. Якщо
// windowCreate ще не викликаний жодного разу — колбеки чекають (з
// таймаутом, щоб не зависнути назавжди при аварійному сценарії).
import (
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

var (
	mainThreadReadyCh   = make(chan struct{})
	mainThreadReadyOnce sync.Once
)

// markMainThreadReady — викликається рівно один раз, щойно UI-потік
// гарантовано живий (перший успішний windowCreate). Безпечно викликати
// повторно — реальне закриття каналу станеться лише разу.
func markMainThreadReady() {
	mainThreadReadyOnce.Do(func() {
		close(mainThreadReadyCh)
	})
}

// awaitMainThreadReady блокує виклик, поки головний потік не стане
// готовим, або поки не мине timeout (запобіжник від вічного зависання,
// якщо markMainThreadReady з якоїсь причини ніколи не викликано —
// тоді ми просто ризикуємо тим самим падінням, що й раніше, а не тихо
// висимо назавжди).
func awaitMainThreadReady(timeout time.Duration) {
	select {
	case <-mainThreadReadyCh:
	case <-time.After(timeout):
	}
}

// mainThreadReadyTimeout — скільки максимум чекати готовності головного
// потоку в safeInvokeAsync/safeInvokeSync, перш ніж все одно спробувати
// виклик (крайній випадок: markMainThreadReady чомусь не спрацював —
// краще спробувати і, можливо, впасти з тим самим SIGSEGV, ніж
// заблокувати колбек із .so назавжди, що саме по собі повісило б UI).
const mainThreadReadyTimeout = 5 * time.Second

// safeInvokeAsync — application.InvokeAsync з бар'єром готовності
// головного потоку (див. коментар вище файлу). Усі колбеки DLL/so->shell,
// які раніше викликали application.InvokeAsync напряму, тепер ідуть
// через цю обгортку.
func safeInvokeAsync(fn func()) {
	awaitMainThreadReady(mainThreadReadyTimeout)
	application.InvokeAsync(fn)
}

// safeInvokeSync — синхронний аналог safeInvokeAsync, для колбеків, що
// повертають значення в модуль (window_get_size, window_is_visible).
func safeInvokeSync(fn func()) {
	awaitMainThreadReady(mainThreadReadyTimeout)
	application.InvokeSync(fn)
}
