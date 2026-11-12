//go:build windows

package main

// webview_memory_windows.go — керування цільовим рівнем пам'яті WebView2.
//
// ПРОБЛЕМА. Під час роботи Chromium (WebView2) нарощує кеші: декодовані
// картинки, скомпільований JS, текстури, дрібні аренові алокатори. Назад ОС
// він їх сам не віддає — тому в Диспетчері завдань «лаунчер ~400 МБ і не
// зменшується», навіть коли користувач давно нічого не робить. Go-хіп тут ні
// до чого: це нативна частина процесу.
//
// ЩО РОБИМО. ICoreWebView2_19::MemoryUsageTargetLevel (WebView2 Runtime
// 1.0.1823+) — офіційний спосіб сказати рушію «зараз пам'ять важливіша за
// швидкість»: він скидає кеші й повертає пам'ять ОС, скрипти при цьому НЕ
// зупиняються (на відміну від TrySuspend, що заморожує JS і зламало б події
// game:exit/чат у треї). Рівень Low виставляє backend (webview_memory.go), коли
// вікно неактивне, сховане в трей або користувач бездіяльний; Normal — як тільки
// користувач повернувся.
//
// ЧОМУ ЧЕРЕЗ reflect. Wails v3 не експортує ні ICoreWebView2, ні Chromium
// (пакет internal), а сам цей виклик потрібен лише тут. Версія Wails закріплена
// в go.mod, тому ми читаємо приватні поля impl.chromium.webview за іменем. Якщо
// після оновлення Wails структура зміниться, функція просто нічого не зробить
// (жодних паник — все під recover), а лаунчер працює як раніше.

import (
	"log"
	"reflect"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
)

const (
	// Позиції методів у vtable ICoreWebView2_19 (порядок з WebView2.idl:
	// IUnknown(3) + ICoreWebView2..._18 = 119 слотів, далі get/put
	// MemoryUsageTargetLevel).
	vtblQueryInterface              = 0
	vtblRelease                     = 2
	vtblPutMemoryUsageTargetLevel   = 120
	memoryUsageTargetLevelNormal    = 0
	memoryUsageTargetLevelLow       = 1
	wv2MemoryUnsupportedLogOnceText = "webview2: MemoryUsageTargetLevel недоступний (стара версія WebView2 Runtime або змінена структура Wails) — пропускаємо"
)

// IID ICoreWebView2_19 = {6921F954-79B0-437F-A997-C85811897C68}.
var iidICoreWebView2_19 = comGUID{
	Data1: 0x6921f954,
	Data2: 0x79b0,
	Data3: 0x437f,
	Data4: [8]byte{0xa9, 0x97, 0xc8, 0x58, 0x11, 0x89, 0x7c, 0x68},
}

// comGUID — розкладка GUID для COM (як windows.GUID, без зайвої залежності).
type comGUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

var (
	wv2MemMu          sync.Mutex
	wv2MemLow         bool // останній успішно виставлений рівень
	wv2MemUnsupported bool // runtime/структура не підтримує — більше не пробуємо
)

// setWebviewMemoryLevel виставляє рівень пам'яті WebView2 головного вікна.
// Безпечно викликати з будь-якого потоку: сама робота йде в UI-потоці (COM
// WebView2 — STA). Повторні виклики з тим самим рівнем ігноруються.
func setWebviewMemoryLevel(win *application.WebviewWindow, low bool) {
	if win == nil {
		return
	}
	wv2MemMu.Lock()
	skip := wv2MemUnsupported || wv2MemLow == low
	wv2MemMu.Unlock()
	if skip {
		return
	}
	application.InvokeAsync(func() {
		defer func() { _ = recover() }()
		applyWebviewMemoryLevel(win, low)
	})
}

// applyWebviewMemoryLevel — UI-потік.
func applyWebviewMemoryLevel(win *application.WebviewWindow, low bool) {
	wv := coreWebView2Of(win)
	if wv == nil {
		// WebView ще не створений — це не «непідтримується», спробуємо пізніше.
		return
	}
	var out unsafe.Pointer
	hr := comCall(wv, vtblQueryInterface,
		uintptr(unsafe.Pointer(&iidICoreWebView2_19)), uintptr(unsafe.Pointer(&out)))
	if hr != 0 || out == nil {
		markWebviewMemoryUnsupported()
		return
	}
	level := uintptr(memoryUsageTargetLevelNormal)
	if low {
		level = memoryUsageTargetLevelLow
	}
	hr = comCall(out, vtblPutMemoryUsageTargetLevel, level)
	comCall(out, vtblRelease)
	if hr != 0 {
		return // спробуємо ще раз при наступній зміні стану
	}
	wv2MemMu.Lock()
	wv2MemLow = low
	wv2MemMu.Unlock()
}

func markWebviewMemoryUnsupported() {
	wv2MemMu.Lock()
	first := !wv2MemUnsupported
	wv2MemUnsupported = true
	wv2MemMu.Unlock()
	if first {
		log.Print(wv2MemoryUnsupportedLogOnceText)
	}
}

// comCall викликає метод COM-об'єкта за номером слота vtable.
func comCall(obj unsafe.Pointer, slot int, args ...uintptr) uintptr {
	vtbl := *(*unsafe.Pointer)(obj)
	fn := *(*uintptr)(unsafe.Add(vtbl, uintptr(slot)*unsafe.Sizeof(uintptr(0))))
	all := make([]uintptr, 0, len(args)+1)
	all = append(all, uintptr(obj))
	all = append(all, args...)
	r, _, _ := syscall.SyscallN(fn, all...)
	runtime.KeepAlive(obj)
	return r
}

// coreWebView2Of дістає вказівник на ICoreWebView2 із
// WebviewWindow.impl.(*windowsWebviewWindow).chromium.webview. nil — якщо
// будь-який крок відсутній (вікно ще не ініціалізоване, інша версія Wails).
func coreWebView2Of(win *application.WebviewWindow) (p unsafe.Pointer) {
	defer func() {
		if recover() != nil {
			p = nil
		}
	}()
	impl := reflect.ValueOf(win).Elem().FieldByName("impl")
	if !impl.IsValid() || impl.Kind() != reflect.Interface || impl.IsNil() {
		return nil
	}
	w := impl.Elem()
	if w.Kind() != reflect.Ptr || w.IsNil() {
		return nil
	}
	chromium := w.Elem().FieldByName("chromium")
	if !chromium.IsValid() || chromium.Kind() != reflect.Ptr || chromium.IsNil() {
		return nil
	}
	wv := chromium.Elem().FieldByName("webview")
	if !wv.IsValid() || wv.Kind() != reflect.Ptr || wv.IsNil() {
		return nil
	}
	return wv.UnsafePointer()
}
