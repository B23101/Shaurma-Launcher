// bridge.h — спільний C ABI контракт між Shaurma.exe (shell) і
// backend.dll / updater.dll (компоненти).
//
// ПРИНЦИП: shell ЖОДНОГО РАЗУ не знає, які саме методи є у backend/updater.
// Він вміє викликати лише 4 функції нижче і давати колбеки. Все
// розширення функціоналу відбувається ВСЕРЕДИНІ dll (reflect-диспетчер
// по імені методу) — тому shell.exe не потребує перекомпіляції, коли
// в backend з'являється новий метод App.*.
//
// Обидві сторони (shell і dll) компілюються з ЦИМ САМИМ файлом, щоб не
// розсинхронитись по сигнатурах.
//
// Версія 2 (struct_version = 2): додано dialog_open_files, dialog_save_file,
// phase_complete, run_updater — всі поля додані В КІНЕЦЬ структури
// BridgeCallbacks, тож ABI старих DLL, зібраних з v1, лишається сумісним
// (вони просто не читають нові поля, якщо перевіряють struct_version).

#ifndef SHAURMA_BRIDGE_H
#define SHAURMA_BRIDGE_H

#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

// ---- Колбеки: DLL -> Shell -----------------------------------------
// ПРИНЦИП: shell НЕ має жодного спеціалізованого поняття "консоль",
// "трей", "buildID" тощо. Він виконує лише GENERIC команди над
// вікнами/меню/треєм за їх ID — вся семантика ("яке вікно для якої
// збірки", "коли ховати у трей") лишається в backend.dll так само, як
// зараз лишається в App.*. Shell — чистий виконавець команд, не потребує
// перекомпіляції, коли з'являється новий ТИП вікна чи новий пункт меню:
// це просто нові значення id/optionsJson, що передає backend.

// window_create(id, options_json) — створює вікно з даним ID (якщо вже
// існує — no-op). options_json: {"title":..,"width":..,"height":..,
// "minWidth":..,"minHeight":..,"frameless":..,"url":..} — той самий
// набір полів, що зараз WebviewWindowOptions у Wails. closable=false
// означає "хук WindowClosing підписаний, реальне закриття підтверджує
// backend" (замість Cancel — shell шле подію назад, див. нижче).
typedef void (*bridge_window_create_fn)(const char* id, const char* options_json);

// window_op(id, op) — generic операція над вікном:
// "show"|"hide"|"close"|"minimise"|"unminimise"|"maximise"|
// "unmaximise"|"focus"|"destroy".
typedef void (*bridge_window_op_fn)(const char* id, const char* op);

// window_set_size(id, w, h) / window_set_min_size(id, w, h)
typedef void (*bridge_window_set_size_fn)(const char* id, int w, int h);
typedef void (*bridge_window_set_min_size_fn)(const char* id, int w, int h);

// window_get_size(id) -> JSON {"w":..,"h":..}
typedef char* (*bridge_window_get_size_fn)(const char* id);

// window_is_visible(id) -> 1/0
typedef int (*bridge_window_is_visible_fn)(const char* id);

// tray_set(iconPngBase64, tooltip, menuJson) — створює/оновлює ОДИН
// системний трей. menuJson: [{"id":"show","label":"...","separator":
// false},...] — при кліку shell шле подію "tray:click:<id>" назад у
// backend через backend_invoke("__dispatch", ...) (той самий канал
// подій, тільки у зворотному напрямку — див. __dispatch у документації
// нижче), а backend вирішує, що робити (той самий принцип, що OnClick
// зараз кличе a.showWindow()/application.Get().Quit()).
typedef void (*bridge_tray_set_fn)(const char* icon_png_base64, const char* tooltip, const char* menu_json);

typedef void (*bridge_tray_destroy_fn)(void);

// emit(event, dataJson) — той самий універсальний канал подій ДЛЯ
// ФРОНТЕНДУ (не для команд shell'у) — це те, що зараз a.emit() шле на
// UI.
typedef void (*bridge_emit_fn)(const char* event, const char* data_json);

// dialog_open_file(options_json) -> шлях (порожній рядок якщо скасовано).
// options_json: {"title":..,"directories":true,"filters":[
//   {"name":"Зображення","extensions":["png","jpg"]}]}
typedef char* (*bridge_dialog_open_file_fn)(const char* options_json);

// dialog_open_files(options_json) -> JSON-масив шляхів "[\"a.jar\"]"
// (порожній масив якщо скасовано). Той самий options_json формат.
typedef char* (*bridge_dialog_open_files_fn)(const char* options_json);

// dialog_save_file(options_json) -> шлях (порожній рядок якщо скасовано).
// options_json: {"title":..,"defaultName":"screenshot.png","filters":[...]}
typedef char* (*bridge_dialog_save_file_fn)(const char* options_json);

// browser_open_url(url) — відкрити URL в системному браузері.
typedef void (*bridge_browser_open_url_fn)(const char* url);

// app_quit() — повне завершення застосунку (не "у трей"). ЄДИНА команда
// без generic-window-еквіваленту, бо завершення процесу — дія над самим
// shell, не над конкретним вікном.
typedef void (*bridge_app_quit_fn)(void);

// free_string(s) — звільнити рядок, виділений ШЕЛЛОМ (з
// window_get_size/dialog_*). DLL кличе цю функцію, щоб не плутати
// аллокатори різних CRT (shell.exe і dll можуть бути зібрані окремими
// компіляторами/runtime).
typedef void (*bridge_free_string_fn)(char* s);

// phase_complete() — updater.dll завершив свою роботу (перевірка,
// завантаження і встановлення оновлень — успішно АБО з помилкою, яку
// користувач вже бачив на splash). Shell викликає graceful shutdown
// updater.dll (backend_shutdown), підміняє міст на backend.dll
// (backend_init) і перемикає вікно з "updater-splash" на основний UI
// лаунчера. ОБИДВІ DLL лишаються завантаженими на весь час життя
// процесу: FreeLibrary на c-shared Go-бібліотеці — crash-ризик (Go
// Runtime не підтримує вивантаження таких DLL), це було причиною
// «зникання» лаунчера після перевірки оновлень.
typedef void (*bridge_phase_complete_fn)(void);

// run_updater() — backend.dll просить shell перейти у фазу оновлення
// (кнопка «Оновити» / автооновлення): shell вивантажує backend.dll,
// завантажує updater.dll і запускає його — той самий цикл, що при
// старті застосунку.
typedef void (*bridge_run_updater_fn)(void);

// Набір усіх колбеків, передається одним блоком у backend_init —
// додавання нового колбеку в майбутньому не ламає ABI старих DLL
// (нове поле в кінці структури, старі DLL його просто не читають,
// якщо перевіряють struct_version).
typedef struct {
    uint32_t                     struct_version; // = BRIDGE_CALLBACKS_VERSION
    bridge_emit_fn                emit;
    bridge_window_create_fn       window_create;
    bridge_window_op_fn           window_op;
    bridge_window_set_size_fn     window_set_size;
    bridge_window_get_size_fn     window_get_size;
    bridge_window_is_visible_fn   window_is_visible;
    bridge_tray_set_fn             tray_set;
    bridge_tray_destroy_fn         tray_destroy;
    bridge_dialog_open_file_fn    dialog_open_file;
    bridge_browser_open_url_fn    browser_open_url;
    bridge_app_quit_fn            app_quit;
    bridge_free_string_fn         free_string;
    // ---- v2 (BRIDGE_CALLBACKS_VERSION >= 2) ----
    bridge_dialog_open_files_fn   dialog_open_files;
    bridge_dialog_save_file_fn    dialog_save_file;
    bridge_window_set_min_size_fn window_set_min_size;
    bridge_phase_complete_fn      phase_complete;
    bridge_run_updater_fn         run_updater;
} BridgeCallbacks;

#define BRIDGE_CALLBACKS_VERSION 2u

// ---- Функції: Shell -> DLL ------------------------------------------
// Ці 4 символи ОБОВ'ЯЗКОВО експортує кожна dll (backend.dll,
// updater.dll) з однаковими іменами — shell шукає їх по імені через
// GetProcAddress після LoadLibrary, не знаючи наперед, яка саме dll
// підвантажена (backend чи updater, обидві реалізують той самий
// контракт "модуль з методами").

// backend_init(config_json, callbacks) -> 0 при успіху, !=0 код помилки.
// config_json: {"installDir":"...", "lang":"uk", "channel":"shaurma",
// "finalize":true} — все, що dll потребує знати про середовище одразу
// при старті.
typedef int (*backend_init_fn)(const char* config_json, const BridgeCallbacks* callbacks);

// backend_invoke(method, args_json) -> JSON-результат:
//   {"ok":true,"result":...}  або  {"ok":false,"error":"..."}
// Викликається СИНХРОННО з UI-потоку моста (Bridge.Invoke у Wails-сервісі
// shell'а) — довгі операції (завантаження, розпакування) DLL має
// виконувати у власних горутинах і повертати миттєво "started", а
// прогрес слати назад через emit().
//
// РЕЗЕРВОВАНІ внутрішні методи (викликає тільки shell, не фронтенд):
//   __dispatch(event, dataJson) -> actionJson
//       Канал подій shell -> backend (зворотний напрямок emit):
//       - "window:closing:<id>"  -> {"action":"hide"|"close"}
//         (shell питає ДО закриття вікна з closable:false; "hide" = сховати
//          замість знищити, "close" = дозволити закриття)
//       - "window:state"         -> "" (розмір/стан вікна змінились —
//         backend оновлює window-state.json)
//       - "window:ready:<id>"    -> {"width":..,"height":..,"x":..,"y":..,
//         "maximised":..,"fullscreen":..} або {} (відновити збережений стан)
//       - "tray:click:<id>"      -> "" (клік по пункту меню трею)
typedef char* (*backend_invoke_fn)(const char* method, const char* args_json);

// backend_shutdown() — коректне завершення (закриття БД, скасування
// фонових завдань). Викликається shell'ом при виході з застосунку і при
// переході між фазами (updater <-> backend). DLL при цьому НЕ
// вивантажується (FreeLibrary на c-shared Go-бібліотеку — crash-ризик),
// тож повторний initModule просто створює свіжий App всередині тієї ж
// завантаженої DLL.
typedef void (*backend_shutdown_fn)(void);

// backend_free(s) — звільнити рядок, виділений DLL (результат
// backend_invoke). Дзеркальна пара до bridge_free_string: кожна сторона
// звільняє ТІЛЬКИ те, що сама виділила.
typedef void (*backend_free_fn)(char* s);

#ifdef __cplusplus
}
#endif

#endif // SHAURMA_BRIDGE_H
