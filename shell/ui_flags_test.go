package main

// ui_flags_test.go — безпечний режим мусить вимикати GPU-прискорення WebView2
// незалежно від ui.json (і від того, чи файл узагалі є).

import (
	"reflect"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestSafeModeForcesDisableGpu(t *testing.T) {
	t.Setenv(supSafeEnv, "1")
	if got, want := launcherBrowserArgs(), []string{"--disable-gpu"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("launcherBrowserArgs() у безпечному режимі = %v, очікувалось %v", got, want)
	}
	if got := launcherWebviewGpuPolicy(); got != application.WebviewGpuPolicyNever {
		t.Fatalf("launcherWebviewGpuPolicy() у безпечному режимі = %v, очікувалось %v", got, application.WebviewGpuPolicyNever)
	}
}

func TestSafeModeRequestedValues(t *testing.T) {
	cases := map[string]bool{"": false, "0": true, "1": true}
	for value, want := range cases {
		t.Setenv(supSafeEnv, value)
		if got := safeModeRequested(); got != want {
			t.Errorf("safeModeRequested() при %q = %v, очікувалось %v", value, got, want)
		}
	}
}

// TestNormalModeDoesNotForceDisableGpu — поза безпечним режимом прапорець
// SHAURMA_SAFE_MODE не впливає: launcherBrowserArgs лишає рішення за ui.json
// (у тестах файлу немає — дефолт «GPU увімкнено» → nil).
func TestNormalModeDoesNotForceDisableGpu(t *testing.T) {
	t.Setenv(supSafeEnv, "")
	if got := launcherBrowserArgs(); got != nil {
		t.Fatalf("launcherBrowserArgs() без безпечного режиму = %v, очікувався nil (GPU увімкнено)", got)
	}
}
