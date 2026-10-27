//go:build !windows

package main

import "syscall"

// hiddenProcAttr — nil на не-Windows (пара до procattr_windows.go).
//
// Концепції «консольне вікно дочірнього процесу, яке треба приховати» тут
// не існує: дочірні процеси успадковують термінал батька, а окремих
// вікон консолі система для них не створює. Тому повертаємо nil —
// exec.Cmd із SysProcAttr == nil запускається як звичайно
// (див. notify.go: cmd.SysProcAttr = hiddenProcAttr()).
func hiddenProcAttr() *syscall.SysProcAttr { return nil }
