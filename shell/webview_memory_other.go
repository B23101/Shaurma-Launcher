//go:build !windows

package main

import "github.com/wailsapp/wails/v3/pkg/application"

// setWebviewMemoryLevel — лише Windows (WebView2). На Linux WebKitGTK
// керує пам'яттю сам (див. шапку backend/memtrim.go).
func setWebviewMemoryLevel(*application.WebviewWindow, bool) {}
