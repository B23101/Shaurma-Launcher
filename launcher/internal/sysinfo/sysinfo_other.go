//go:build !windows

package sysinfo

import (
	"runtime"

	"shaurma-launcher-wails/internal/model"
)

// collect — не-Windows fallback: повертає лише кількість ядер.
// Launcher орієнтований на Windows, цей варіант існує щоб код
// компілювався на інших ОС.
func collect(dir string) model.SystemInfo {
	return model.SystemInfo{CPUCount: runtime.NumCPU()}
}
