//go:build windows

package main

import "syscall"

// hiddenProcAttr — SysProcAttr для запуску дочірніх КОНСОЛЬНИХ процесів
// (powershell.exe у notify.go) без консольного вікна.
//
// ЧОМУ ЦЕ В ОКРЕМОМУ ФАЙЛІ (пара windows / !windows): notify.go
// збирається на ВСІХ платформах, а поле syscall.SysProcAttr.HideWindow
// існує ЛИШЕ на Windows. Через це Linux-збірка backend.so падала на
// самому лише згадуванні структури:
//
//	notify.go:82:41: unknown field HideWindow in struct literal of
//	                type "syscall".SysProcAttr
//
// Тепер notify.go кличе hiddenProcAttr(), а різниця платформ сидить тут.
// Це той самий прийом, що вже застосовано до hideConsoleWindow у
// internal/minecraft та internal/servers.
//
// HideWindow ставить STARTF_USESHOWWINDOW + SW_HIDE у STARTUPINFO: лаунчер
// зібрано як GUI (-H windowsgui), власної консолі він не має, тож без
// цього прапорця кожен спавн powershell.exe блимав би чорним вікном
// консолі приблизно на секунду.
func hiddenProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true}
}
