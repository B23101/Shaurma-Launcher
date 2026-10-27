//go:build windows

package main

// callbacks_windows.go — 16 колбеків DLL -> shell (BridgeCallbacks у
// bridge.h). Кожен отримує сирі uintptr-аргументи (C ABI через
// syscall.NewCallback), перетворює їх у Go-рядки і виконує generic-команду.
//
// Лінуксовий еквівалент — bridge_callbacks_linux.go (той самий контракт,
// реалізований через cgo "//export" замість syscall.NewCallback, бо
// останній є лише на Windows).
//
// ПРАВИЛО БЕЗПЕКИ ВІД ДЕДЛОКІВ: колбек, викликаний із UI-потоку (через
// backend_invoke усередині хука вікна), НЕ повинен блокувати UI-потік
// (InvokeSync). Тому всі «запустив і забув» команди (window_create/op,
// tray, quit, phase transitions) виконуються через safeInvokeAsync (application.InvokeAsync з бар'єром
// готовності головного потоку, main_thread_ready.go) —
// FIFO-черга зберігає порядок, але жоден виклик не чекає сам на себе.
// Синхронними (з поверненням значення) можуть бути ЛИШЕ window_get_size /
// window_is_visible / діалоги — вони викликаються тільки з потоків, що НЕ є
// UI-потоком (диспетчер Bridge.Invoke або горутини DLL).

import (
	"encoding/json"
	"strconv"

	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/sys/windows"
)

// ---- Події на фронтенд ---------------------------------------------------

// cbEmit — a.emit() backend'а: надсилає подію у Wails EventManager
// (frontend слухає через Events.On). АСИНХРОННИЙ: дані копіюються в замикання
// і передаються через InvokeAsync, щоб НЕ блокувати горутин DLL при великому
// потоці логів (prints на game console — сотні рядків/сек). Без цього
// app.Event.Emit синхронно чекав на доставку WebView2, блокуючи readOutput
// goroutine і зависаючи backend.
//
// FIX (launched-before-app.Run): до app.Run() dispatcher = nil,
// InvokeAsync падає з nil pointer dereference. Recover ловить
// паніку і повертає 0 — подія тихо пропускається (вона все одно
// не дійшла б до frontend, бо WebView2 ще не завантажив JS).
// Критична подія backend:ready перевидана shell'ом після створення
// головного вікна (loadBackendAndShowMain).
func cbEmit(eventPtr, dataPtr uintptr) uintptr {
	defer recoverCb("cbEmit")
	event := goStr(eventPtr)
	data := goStr(dataPtr)
	if app := application.Get(); app != nil {
		d := make([]byte, len(data))
		copy(d, data)
		// Захист: до app.Run() InvokeAsync падає (nil dispatcher).
		// Після app.Run() — працює нормально.
		func() {
			dropped := false
			defer func() {
				if r := recover(); r != nil {
					dropped = true
					bootstrapLog(exeDir(), "cbEmit: InvokeAsync panic (app not ready?): %v — event %q dropped", r, event)
				}
			}()
			safeInvokeAsync(func() {
				app.Event.Emit(event, json.RawMessage(d))
			})
			_ = dropped
		}()
	}
	return 0
}

// ---- Вікна ---------------------------------------------------------------

func cbWindowCreate(idPtr, optsPtr uintptr) uintptr {
	defer recoverCb("cbWindowCreate")
	id := goStr(idPtr)
	optsJSON := goStr(optsPtr)
	safeInvokeAsync(func() { windowCreate(id, optsJSON) })
	return 0
}

func cbWindowOp(idPtr, opPtr uintptr) uintptr {
	defer recoverCb("cbWindowOp")
	id := goStr(idPtr)
	op := goStr(opPtr)
	safeInvokeAsync(func() { windowOp(id, op) })
	return 0
}

func cbWindowSetSize(idPtr, w, h uintptr) uintptr {
	defer recoverCb("cbWindowSetSize")
	id := goStr(idPtr)
	safeInvokeAsync(func() { windowSetSize(id, int(w), int(h)) })
	return 0
}

func cbWindowSetMinSize(idPtr, w, h uintptr) uintptr {
	defer recoverCb("cbWindowSetMinSize")
	id := goStr(idPtr)
	safeInvokeAsync(func() { windowSetMinSize(id, int(w), int(h)) })
	return 0
}

// cbWindowGetSize — синхронний запит розміру вікна. Викликається лише з
// не-UI потоків (диспетчер/горутини DLL) — InvokeSync тут безпечний.
func cbWindowGetSize(idPtr uintptr) uintptr {
	defer recoverCb("cbWindowGetSize")
	id := goStr(idPtr)
	var w, h int
	safeInvokeSync(func() {
		if win := getWindow(id); win != nil {
			w, h = win.Size()
		}
	})
	return allocCString(`{"w":` + strconv.Itoa(w) + `,"h":` + strconv.Itoa(h) + `}`)
}

func cbWindowIsVisible(idPtr uintptr) uintptr {
	defer recoverCb("cbWindowIsVisible")
	id := goStr(idPtr)
	vis := false
	safeInvokeSync(func() {
		if win := getWindow(id); win != nil {
			vis = win.IsVisible()
		}
	})
	if vis {
		return 1
	}
	return 0
}

// ---- Трей ----------------------------------------------------------------

func cbTraySet(iconPtr, tipPtr, menuPtr uintptr) uintptr {
	defer recoverCb("cbTraySet")
	iconB64 := goStr(iconPtr)
	tooltip := goStr(tipPtr)
	menuJSON := goStr(menuPtr)
	safeInvokeAsync(func() { traySet(iconB64, tooltip, menuJSON) })
	return 0
}

func cbTrayDestroy() uintptr {
	defer recoverCb("cbTrayDestroy")
	safeInvokeAsync(func() { trayDestroy() })
	return 0
}

// ---- Діалоги (синхронні, повертають char*) -------------------------------

func cbDialogOpenFile(optsPtr uintptr) uintptr {
	defer recoverCb("cbDialogOpenFile")
	return allocCString(dialogOpenFile(goStr(optsPtr)))
}

func cbDialogOpenFiles(optsPtr uintptr) uintptr {
	defer recoverCb("cbDialogOpenFiles")
	return allocCString(dialogOpenFiles(goStr(optsPtr)))
}

func cbDialogSaveFile(optsPtr uintptr) uintptr {
	defer recoverCb("cbDialogSaveFile")
	return allocCString(dialogSaveFile(goStr(optsPtr)))
}

// ---- Браузер / вихід / фази ----------------------------------------------

// openExternalURL відкриває URL у системному браузері користувача.
//
// ВАЖЛИВО (фікс «відкриває Провідник на диску C»): Wails app.Browser.OpenURL
// на Windows викликає rundll32 url.dll,FileProtocolHandler <url> — цей
// механізм сумнозвісний крихкістю: на URL з '&' (Microsoft login має повно
// параметрів з &) він може обрізати або спотворити аргумент, а на порожньому
// аргументі Windows відкриває ПРОВІДНИК на диску C замість браузера. Тому
// відкриваємо напряму через ShellExecuteW (той самий виклик, що робить
// Windows для асоціацій .http/.html), з явною перевіркою порожнього URL —
// порожній аргумент ніколи не доходить до ShellExecute.
func openExternalURL(url string) {
	if url == "" {
		return
	}
	verb, _ := windows.UTF16PtrFromString("open")
	file, err := windows.UTF16PtrFromString(url)
	if err != nil {
		return
	}
	_ = windows.ShellExecute(0, verb, file, nil, nil, windows.SW_SHOWNORMAL)
}

func cbBrowserOpenURL(urlPtr uintptr) uintptr {
	defer recoverCb("cbBrowserOpenURL")
	u := goStr(urlPtr)
	safeInvokeAsync(func() {
		openExternalURL(u)
	})
	return 0
}

// cbAppQuit — повне завершення процесу (трей «Вийти» тощо).
func cbAppQuit() uintptr {
	defer recoverCb("cbAppQuit")
	safeInvokeAsync(func() {
		if app := application.Get(); app != nil {
			app.Quit()
		}
	})
	return 0
}

// cbFreeString — DLL звільняє рядок, виділений ШЕЛЛОМ (allocCString).
func cbFreeString(p uintptr) uintptr {
	defer recoverCb("cbFreeString")
	freeCString(p)
	return 0
}

// cbPhaseComplete — updater.dll завершив роботу: перемикаємось на backend.dll
// (вивантаження updater + завантаження backend + головне вікно замість splash).
func cbPhaseComplete() uintptr {
	defer recoverCb("cbPhaseComplete")
	safeInvokeAsync(transitionToBackend)
	return 0
}

// cbRunUpdater — backend.dll просить перейти у фазу оновлення (кнопка
// «Оновити»/автооновлення): вивантажуємо backend, завантажуємо updater.
func cbRunUpdater() uintptr {
	defer recoverCb("cbRunUpdater")
	safeInvokeAsync(transitionToUpdater)
	return 0
}
