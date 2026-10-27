//go:build windows

package main

// flatpak_seed_windows.go — Flatpak існує лише на Linux, тому на Windows
// це просто no-op. Існує, щоб безумовний виклик seedFromFlatpakIfNeeded()
// у main.go (спільний для обох платформ) компілювався й тут без
// build-тегів у самому main.go.
func seedFromFlatpakIfNeeded() {}
