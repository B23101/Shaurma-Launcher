// Package sysinfo збирає інформацію про систему (CPU, RAM, вільний диск)
// для кроку «Продуктивність» майстра та сторінки налаштувань.
// На Windows використовується Win32 API (GlobalMemoryStatusEx +
// GetDiskFreeSpaceExW) без зайвих залежностей.
package sysinfo

import "shaurma-launcher-wails/internal/model"

// Collect повертає інформацію про систему. dir — тека, для якої треба
// вільне місце на диску (зазвичай тека даних лаунчера).
func Collect(dir string) model.SystemInfo {
	return collect(dir)
}
