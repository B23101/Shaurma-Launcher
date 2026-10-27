package main

// bridge_service.go — ЄДИНИЙ Wails-сервіс, який реєструє shell.exe.
// Це замінює `application.NewService(app)` з оригінального launcher/main.go:
// реєструється не сам App (230 методів), а один тонкий проксі-сервіс.
//
// НАЗВА "main.Bridge.Invoke" НІКОЛИ НЕ МІНЯЄТЬСЯ — саме тому фронтенд
// (wails.ts) більше НЕ вимагає оновлення shell.exe, коли в backend.dll
// з'являється новий метод App.*.

import (
	"encoding/json"
	"sync"
)

// Bridge — Wails-сервіс, прив'язаний до generic backend_invoke.
// mod підмінюється при переключенні updater.dll <-> backend.dll без
// перезапуску shell.exe чи перереєстрації сервісу.
type Bridge struct {
	mu  sync.RWMutex
	mod *dllModule // поточний завантажений модуль (updater або backend)
}

// bridgeSvc — глобальний екземпляр сервісу (для колбеків window/tray).
var bridgeSvc *Bridge

func newBridgeService() *Bridge {
	return &Bridge{}
}

// Invoke — ЄДИНИЙ метод, який бачить Wails. Викликається з фронтенду як
// Call.ByName('main.Bridge.Invoke', method, argsJson). Просто пересилає
// рядки у підвантажену DLL і повертає її JSON-відповідь.
//
// Сигнатура навмисно (string, string) -> (string, error): найпростіший
// можливий ABI через межу JS -> Wails -> syscall. Немає складних типів,
// тому Wails-серіалізація тривіальна і стабільна між версіями Wails.
func (b *Bridge) Invoke(method string, argsJSON string) (string, error) {
	// RLock тримається ПІД час invokeModule: swap (write lock) чекатиме
	// завершення виклику, тож між вивантаженням DLL і підміною модуля
	// Invoke не може влучити у звільнену пам'ять (FreeLibrary). Дедлок
	// неможливий: Invoke виконується на goroutine диспетчера Wails (НЕ на
	// UI-потоці), а swap — лише через application.InvokeAsync (UI-черга);
	// взаємне очікування RLock<->UI-потік вимагає, щоб DLL-метод синхронно
	// чекав UI-потік (InvokeSync) рівно в момент переходу фаз, чого не
	// буває (splash під час переходу нічого не викликає).
	b.mu.RLock()
	mod := b.mod
	if mod == nil {
		b.mu.RUnlock()
		return `{"ok":false,"error":"модуль не завантажено"}`, nil
	}
	res := mod.invokeModule(method, argsJSON)
	b.mu.RUnlock()
	return res, nil
}

// swap підміняє активний модуль (викликається лише внутрішньою логікою
// shell'а при переходах updater <-> backend — фронтенд нічого не помічає).
func (b *Bridge) swap(mod *dllModule) {
	b.mu.Lock()
	b.mod = mod
	b.mu.Unlock()
}

// backendDispatch — СИНХРОННИЙ виклик внутрішнього методу "__dispatch"
// (канал подій shell -> backend, див. bridge.h). Використовується хуками
// вікон і кліками трею. Викликається ЛИШЕ з UI-потоку, тож не може
// конкурувати з transition (той теж через InvokeAsync) — але якщо модуль
// вже вивантажено (mod == nil після swap(nil) у transition), повертає "".
func backendDispatch(event, dataJSON string) string {
	b := bridgeSvc
	if b == nil {
		return ""
	}
	b.mu.RLock()
	mod := b.mod
	b.mu.RUnlock()
	if mod == nil {
		return ""
	}
	args, _ := json.Marshal([]string{event, dataJSON})
	res := mod.invokeModule("__dispatch", string(args))
	var r struct {
		OK     bool   `json:"ok"`
		Result string `json:"result"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal([]byte(res), &r); err != nil || !r.OK {
		return ""
	}
	return r.Result
}
