//go:build !linux

package main

// window_pos_other.go — на Windows/macOS клієнт завжди може читати й
// задавати позицію власного вікна (див. коментар у window_pos_linux.go,
// де це не так через Wayland).
func windowPositioningSupported() bool { return true }
