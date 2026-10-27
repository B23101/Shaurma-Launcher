//go:build !linux

package main

// noWorkingGpu — поза Linux завжди false: WebView2 сам коректно
// деградує до софтверного рендерингу, і власної перевірки /dev/dri там
// немає (див. gpu_policy_linux.go та webview_compat_linux.go).
func noWorkingGpu() bool { return false }
