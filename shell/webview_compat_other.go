//go:build !linux

package main

// webview_compat_other.go — no-op поза Linux. WebView2 (Windows) не має
// проблем із EGL/Mesa/DMA-BUF, тому цей клас фіксів там не потрібен.
// gpuSafeModeRequested див. gpu_recovery_other.go.
func applyLinuxWebViewCompatEnv() {}
