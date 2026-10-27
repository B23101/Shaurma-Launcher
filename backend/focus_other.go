//go:build !windows

package main

// bringWindowToFront — no-op на не-Windows (пара до focus_windows.go).
//
// ЧОМУ на Windows ця функція потрібна, а тут — ні: там лаунчер майже
// завжди запускається з shrm-updater.exe (ярлик у меню Пуск). Updater —
// foreground-процес, і поки він не завершиться, Windows не віддає фокус
// нашому вікну: воно відкривається ПОЗАДУ інших вікон («блимнуло і
// нічого»), тож доводиться штовхати його нагору через Win32
// (SetForegroundWindow + AllowSetForegroundWindow, див. focus_windows.go).
//
// На Linux такого «процесу попереду» у флоу запуску немає: shell сам
// створює своє вікно через GTK4/WebKitGTK, і фокус видає віконний
// менеджер за звичайними правилами сесії — X11, Wayland, а в WSL2 — WSLg
// (композитний WM, який сам активує щойно створене вікно). Примусове
// «підняти себе» тут не лише зайве, а й шкідливе: у Wayland клієнт взагалі
// не має права керувати позицією/стекінгом вікон, а спроби це зробити
// виглядають як фокус-крадіжка.
//
// Функція обов'язкова (а не прибрана за непотрібністю), бо app.go
// (ServiceStartup) викликає `go bringWindowToFront()` без розгалуження по
// GOOS — саме її відсутність валила Linux-збірку backend.so:
// "app.go:651:5: undefined: bringWindowToFront".
func bringWindowToFront() {}
