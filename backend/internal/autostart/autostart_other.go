//go:build !windows && !linux

// autostart_other.go — no-op версія для платформ, відмінних від Windows
// і Linux (macOS виключено з плану підтримки, лишається як безпечний
// fallback, якщо колись знадобиться), плюс середовища без реалізації.
// Linux має власну autostart_linux.go (.desktop у ~/.config/autostart/).
package autostart

func Set(enabled bool, exePath string, args ...string) error { return nil }
func IsEnabled() bool                                        { return false }
