package main

// safe_mode_test.go — контракт безпечного режиму (safe_mode.go).
//
// Перевіряємо саме те, від чого залежить «лаунчер не падає»: у безпечному
// режимі автоперевірка буфера обміну не торкається купи Windows УЗАГАЛІ
// (ClipboardText повертає "" до будь-якого Win32-виклику), а хибні значення
// змінної не вмикають режим випадково.

import "testing"

func TestSafeModeActive(t *testing.T) {
	cases := map[string]bool{
		"":      false,
		"0":     false,
		"false": false,
		"1":     true,
		"true":  true,
	}
	for value, want := range cases {
		t.Setenv(safeModeEnv, value)
		if got := safeModeActive(); got != want {
			t.Errorf("safeModeActive() при %s=%q = %v, очікувалось %v", safeModeEnv, value, got, want)
		}
	}
}

func TestClipboardProbeDisabledInSafeMode(t *testing.T) {
	t.Setenv(safeModeEnv, "1")
	var a App
	if got := a.ClipboardText(); got != "" {
		t.Fatalf("у безпечному режимі ClipboardText() = %q, очікувався порожній рядок без звернення до буфера", got)
	}
}
