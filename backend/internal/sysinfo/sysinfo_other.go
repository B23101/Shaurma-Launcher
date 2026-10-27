//go:build !windows && !linux

package sysinfo

import (
	"runtime"

	"shaurma-launcher-backend/internal/model"
)

// archName перетворює runtime.GOARCH у зрозумілу архітектуру UI.
func archName(goarch string) string {
	switch goarch {
	case "amd64":
		return "x64"
	case "386":
		return "x86"
	case "arm64":
		return "arm64"
	default:
		return goarch
	}
}

// collect — не-Windows fallback: повертає лише кількість ядер.
// Launcher орієнтований на Windows, цей варіант існує щоб код
// компілювався на інших ОС.
func collect(dir string) model.SystemInfo {
	info := model.SystemInfo{CPUCount: runtime.NumCPU()}
	info.Platform = runtime.GOOS
	info.Arch = archName(runtime.GOARCH)
	switch runtime.GOOS {
	case "darwin":
		info.OS = "macOS"
	case "linux":
		info.OS = "Linux"
	default:
		info.OS = runtime.GOOS
	}
	return info
}
