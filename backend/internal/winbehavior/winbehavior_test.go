package winbehavior

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func rawOf(t *testing.T, js string) map[string]json.RawMessage {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(js), &m); err != nil {
		t.Fatalf("bad json %q: %v", js, err)
	}
	return m
}

func TestDefaults(t *testing.T) {
	d := Defaults()
	if d.StartupMode != StartupWindow {
		t.Errorf("новий користувач має отримати автозапуск з вікном, got %q", d.StartupMode)
	}
	if d.GameLaunchAction != LaunchKeep || d.GameExitAction != ExitRestore || d.CloseAction != CloseQuit || d.MinimizeAction != MinimizeTaskbar {
		t.Errorf("несподівані дефолти: %+v", d)
	}
	if !d.RestoreOnCrash {
		t.Error("restoreOnCrash має бути true за замовчуванням")
	}
	if !reflect.DeepEqual(Normalize(d), d) {
		t.Error("дефолти мають бути вже нормалізовані")
	}
}

func TestNormalize(t *testing.T) {
	got := Normalize(Settings{StartupMode: "bogus", CloseAction: "x", MinimizeAction: "y",
		GameLaunchAction: "z", GameExitAction: "q", GameLaunchDelaySec: -5})
	want := Defaults()
	want.RestoreOnCrash = false // Normalize не чіпає bool
	if got != want {
		t.Errorf("Normalize(garbage) = %+v, want %+v", got, want)
	}
	if n := Normalize(Settings{GameLaunchDelaySec: 9999}); n.GameLaunchDelaySec != MaxLaunchDelaySec {
		t.Errorf("затримку не стиснуто: %d", n.GameLaunchDelaySec)
	}
	// Коректні значення не змінюються.
	ok := Settings{StartupMode: StartupWindow, CloseAction: CloseTray, MinimizeAction: MinimizeTray,
		GameLaunchAction: LaunchMinimize, GameLaunchDelaySec: 15, GameExitAction: ExitQuit}
	if Normalize(ok) != ok {
		t.Error("валідні значення не мають змінюватись")
	}
}

func TestMigrateLegacy(t *testing.T) {
	cases := []struct {
		name string
		js   string
		want Settings
	}{
		{
			name: "порожній старий файл → ніякого автозапуску для існуючих",
			js:   `{"language":"uk"}`,
			want: Settings{StartupMode: StartupNever, CloseAction: CloseQuit, MinimizeAction: MinimizeTaskbar,
				GameLaunchAction: LaunchKeep, GameExitAction: ExitRestore, RestoreOnCrash: true},
		},
		{
			name: "openOnStartup=true зберігає показ вікна",
			js:   `{"openOnStartup":true}`,
			want: Settings{StartupMode: StartupWindow, CloseAction: CloseQuit, MinimizeAction: MinimizeTaskbar,
				GameLaunchAction: LaunchKeep, GameExitAction: ExitRestore, RestoreOnCrash: true},
		},
		{
			name: "closeOnLaunch=true → трей, повернення після гри",
			js:   `{"closeOnLaunch":true}`,
			want: Settings{StartupMode: StartupNever, CloseAction: CloseQuit, MinimizeAction: MinimizeTaskbar,
				GameLaunchAction: LaunchTray, GameExitAction: ExitRestore, RestoreOnCrash: true},
		},
		{
			name: "обидва старі прапорці → трей + вихід (як було: exit мав пріоритет)",
			js:   `{"closeOnLaunch":true,"exitOnGameClose":true}`,
			want: Settings{StartupMode: StartupNever, CloseAction: CloseQuit, MinimizeAction: MinimizeTaskbar,
				GameLaunchAction: LaunchTray, GameExitAction: ExitQuit, RestoreOnCrash: true},
		},
		{
			name: "exitOnGameClose без closeOnLaunch → вікно лишається, вихід після гри",
			js:   `{"exitOnGameClose":true}`,
			want: Settings{StartupMode: StartupNever, CloseAction: CloseQuit, MinimizeAction: MinimizeTaskbar,
				GameLaunchAction: LaunchKeep, GameExitAction: ExitQuit, RestoreOnCrash: true},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := MigrateLegacy(rawOf(t, c.js), Settings{})
			if got != c.want {
				t.Errorf("got  %+v\nwant %+v", got, c.want)
			}
		})
	}
}

// Уже збережений вибір у новому форматі не перезаписується старими ключами.
func TestMigrateKeepsNewKeys(t *testing.T) {
	raw := rawOf(t, `{"startupMode":"background","gameLaunchAction":"minimize","gameExitAction":"stay",
		"closeAction":"tray","minimizeAction":"tray","restoreOnCrash":false,
		"openOnStartup":true,"closeOnLaunch":true,"exitOnGameClose":true}`)
	cur := Settings{StartupMode: StartupBackground, CloseAction: CloseTray, MinimizeAction: MinimizeTray,
		GameLaunchAction: LaunchMinimize, GameLaunchDelaySec: 7, GameExitAction: ExitStay, RestoreOnCrash: false}
	if got := MigrateLegacy(raw, cur); got != cur {
		t.Errorf("новий вибір перезаписано: %+v", got)
	}
}

func TestAutostart(t *testing.T) {
	if AutostartEnabled(StartupNever) || !AutostartEnabled(StartupWindow) || !AutostartEnabled(StartupBackground) {
		t.Error("AutostartEnabled")
	}
	if AutostartArgs(StartupNever) != nil {
		t.Error("never не має аргументів")
	}
	if !HasFlag(AutostartArgs(StartupBackground), FlagBackground) {
		t.Error("background без прапорця --background")
	}
	if HasFlag(AutostartArgs(StartupWindow), FlagBackground) {
		t.Error("window не повинен ховати вікно")
	}
	if !HasFlag(AutostartArgs(StartupWindow), FlagAutostart) {
		t.Error("window має позначати автозапуск")
	}
}

func TestPlanGameLaunch(t *testing.T) {
	s := Defaults()
	if p := PlanGameLaunch(s); p.Action != WindowNone {
		t.Errorf("keep: %+v", p)
	}
	s.GameLaunchAction, s.GameLaunchDelaySec = LaunchMinimize, 10
	if p := PlanGameLaunch(s); p.Action != WindowMinimize || p.Delay != 10*time.Second {
		t.Errorf("minimize: %+v", p)
	}
	s.GameLaunchAction, s.GameLaunchDelaySec = LaunchTray, 0
	if p := PlanGameLaunch(s); p.Action != WindowHide || p.Delay != 0 {
		t.Errorf("tray: %+v", p)
	}
}

func TestPlanGameExit(t *testing.T) {
	mk := func(exit string, crashRestore bool) Settings {
		s := Defaults()
		s.GameExitAction, s.RestoreOnCrash = exit, crashRestore
		return s
	}
	cases := []struct {
		name string
		s    Settings
		c    ExitContext
		want WindowAction
	}{
		{"restore", mk(ExitRestore, true), ExitContext{}, WindowShow},
		{"stay", mk(ExitStay, true), ExitContext{}, WindowNone},
		{"quit", mk(ExitQuit, true), ExitContext{}, WindowQuit},
		{"quit заблоковано серверами → показати", mk(ExitQuit, true), ExitContext{QuitBlocked: true}, WindowShow},
		{"інша гра ще йде → не чіпати (restore)", mk(ExitRestore, true), ExitContext{OtherSessionsRunning: true}, WindowNone},
		{"інша гра ще йде → не виходити (quit)", mk(ExitQuit, true), ExitContext{OtherSessionsRunning: true}, WindowNone},
		{"краш при quit → показати", mk(ExitQuit, true), ExitContext{Crashed: true}, WindowShow},
		{"краш при stay → показати", mk(ExitStay, true), ExitContext{Crashed: true}, WindowShow},
		{"краш, але restoreOnCrash вимкнено + quit", mk(ExitQuit, false), ExitContext{Crashed: true}, WindowQuit},
		{"краш, restoreOnCrash вимкнено + stay", mk(ExitStay, false), ExitContext{Crashed: true}, WindowNone},
		{"краш і інша гра йде → не чіпати", mk(ExitQuit, true), ExitContext{Crashed: true, OtherSessionsRunning: true}, WindowNone},
	}
	for _, c := range cases {
		if got := PlanGameExit(c.s, c.c); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestPlanClose(t *testing.T) {
	s := Defaults()
	if PlanClose(s, CloseContext{}) != CloseProceed {
		t.Error("quit без гри має закривати")
	}
	if PlanClose(s, CloseContext{GameRunning: true}) != CloseToTray {
		t.Error("під час гри × має ховати в трей")
	}
	s.CloseAction = CloseTray
	if PlanClose(s, CloseContext{}) != CloseToTray {
		t.Error("closeAction=tray має ховати в трей")
	}
}

func TestCloseTrayReason(t *testing.T) {
	s := Defaults()
	cases := []struct {
		name   string
		close  string
		ctx    CloseContext
		reason string
		forced bool
	}{
		{"нічого не йде", CloseQuit, CloseContext{}, TrayReasonNone, false},
		{"гра", CloseQuit, CloseContext{GameRunning: true}, TrayReasonGame, true},
		{"p2p", CloseQuit, CloseContext{P2PActive: true}, TrayReasonP2P, true},
		{"гра і p2p — гра першою", CloseQuit, CloseContext{GameRunning: true, P2PActive: true}, TrayReasonGame, true},
		{"вибір користувача", CloseTray, CloseContext{}, TrayReasonCloseAction, false},
		{"вибір + гра — теж примусово", CloseTray, CloseContext{GameRunning: true}, TrayReasonGame, true},
	}
	for _, c := range cases {
		s.CloseAction = c.close
		if got := CloseTrayReason(s, c.ctx); got != c.reason {
			t.Errorf("%s: reason=%q, want %q", c.name, got, c.reason)
		}
		if got := CloseTrayForced(s, c.ctx); got != c.forced {
			t.Errorf("%s: forced=%v, want %v", c.name, got, c.forced)
		}
	}
}

func TestWindowActionString(t *testing.T) {
	for a, want := range map[WindowAction]string{WindowNone: "none", WindowMinimize: "minimize", WindowHide: "hide", WindowShow: "show", WindowQuit: "quit"} {
		if a.String() != want {
			t.Errorf("%d → %q, want %q", a, a.String(), want)
		}
	}
}
