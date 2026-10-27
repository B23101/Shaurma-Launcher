//go:build cgo_typecheck

// zz_cgo_stub.go — ТИМЧАСОВИЙ СТУБ для локальної перевірки типів.
//
// На цій машині cgo зламано (cc1.exe з MSYS2 не запускається), тому
// backend.dll не компілюється взагалі. Щоб усе одно отримати повну перевірку
// ТИПІв решти пакета (у т.ч. нових файлів), два cgo-файли виключаються через
// CGO_ENABLED=0, а їхні символи відтворюються тут заглушками.
//
// Активується ЛИШЕ з явним -tags cgo_typecheck — у звичайних збірках
// (backend.dll / base / shaurma) цей файл ігнорується.
package main

import (
	"encoding/json"
	"reflect"
	"unsafe"
)

type initConfig struct {
	InstallDir string `json:"installDir"`
	Lang       string `json:"lang"`
	Channel    string `json:"channel"`
}

var backendInitConfig initConfig
var appInstance *App
var appValue reflect.Value

type invokeResult struct {
	OK     bool   `json:"ok"`
	Result any    `json:"result"`
	Error  string `json:"error,omitempty"`
}

func setBridgeCallbacks(_ unsafe.Pointer)       {}
func bridgeCallbacksVersion() int               { return 3 }
func bridgeWindowCreate(_, _ string)            {}
func bridgeWindowOp(_, _ string)                {}
func bridgeWindowSetSize(_ string, _, _ int)    {}
func bridgeWindowSetMinSize(_ string, _, _ int) {}
func bridgeWindowGetSize(_ string) (int, int)   { return 0, 0 }
func bridgeWindowIsVisible(_ string) bool       { return false }
func bridgeTraySet(_, _, _ string)              {}
func bridgeTrayDestroy()                        {}
func bridgeDialogOpenFile(_ string, _ bool, _ map[string][]string) string {
	return ""
}
func bridgeDialogOpenFiles(_ string, _ map[string][]string) []string { return nil }
func bridgeDialogSaveFile(_, _ string, _ map[string][]string) string {
	return ""
}
func bridgeBrowserOpenURL(_ string) {}
func bridgeAppQuit()                {}
func bridgePhaseComplete()          {}
func bridgeRunUpdater() error       { return nil }
func bridgeEmit(_, _ string)        {}

func dispatch(_, _ string) invokeResult { return invokeResult{} }

func quoteJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func (a *App) handleShellEvent(_, _ string) string { return "" }
