//go:build !windows

package config

// MarkNotIndexed — no-op на Linux/macOS: там немає Windows Search, який
// гальмує Explorer під час встановлення збірки.
func MarkNotIndexed(dir string) {}
