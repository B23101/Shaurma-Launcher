//go:build !linux

package main

// gpu_recovery_other.go — no-op-заглушки механізму gpu_recovery_linux.go
// для Windows/macOS. На Windows GPU-стек інший (WebView2): він сам
// коректно деградує до софтверного рендерингу і не SEGFAULT-иться на
// кривих драйверах, тому supervisor-перезапуск там не потрібен. /dev/dri
// — це суто Linux-концепція (DRI-підсистема ядра).
//
// ВАЖЛИВО: recoverGPUStartupRecover тут СВІДОМО порожній і НЕ кличе
// recover() — інакше він поглинув би panic раніше за стандартний
// обробник у main() (дефери виконуються LIFO), і crash-діалог
// showStartupCrashDialog ніколи б не показався.

func recoverGPUStartup()        {}
func recoverGPUStartupRecover() {}
func removeGPUCrashMarker()     {}

// gpuSafeModeRequested — поза Linux прапорець SHAURMA_NO_HARDWARE_ACCELERATION
// ніхто не виставляє (його ставить лише Linux-supervisor), тож завжди false.
func gpuSafeModeRequested() bool { return false }
