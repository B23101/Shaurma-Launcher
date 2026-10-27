//go:build !windows

package main

// nativecrash_other.go — на решті платформ нативного перехоплювача немає:
// там аварія на рівні ОС лишає сигнал (SIGSEGV тощо), який Go показує
// звичайним fatal error у crash.log (див. crashguard.Init → SetCrashOutput).
// Windows-реалізація — nativecrash_windows.go.
func installNativeCrashReporter() {}

// nativeCrashHint — підказка для наглядача (Windows-реалізація пише
// про logs/crash-native.log). На решті платформ про причину аварійного
// виходу розповідає crash.log.
func nativeCrashHint(installDir string) string { return "" }
