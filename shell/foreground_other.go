//go:build !windows

package main

// foreground_other.go — поза Windows foreground lock немає (Linux: оболонка
// сама вирішує фокус), тож це no-op; пара до foreground_windows.go.

import "github.com/wailsapp/wails/v3/pkg/application"

func allowAnyForeground() {}

func forceForeground(_ *application.WebviewWindow) {}
