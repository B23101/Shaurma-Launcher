package config

import (
	"testing"

	"shaurma-launcher-backend/internal/winbehavior"
)

// Схема налаштувань вікна має бути узгоджена: дефолти збігаються з
// winbehavior.Defaults(), кожен "choice" має варіанти з описами, а
// showIf посилається на існуючі ключі.
func TestWindowSchemaConsistency(t *testing.T) {
	schema := SettingsSchema()
	byKey := map[string]int{}
	for i, d := range schema {
		if _, dup := func() (int, bool) { v, ok := byKey[d.Key]; return v, ok }(); dup {
			t.Errorf("дублікат ключа в схемі: %s", d.Key)
		}
		byKey[d.Key] = i
	}
	wd := winbehavior.Defaults()
	want := map[string]any{
		"startupMode": wd.StartupMode, "closeAction": wd.CloseAction, "minimizeAction": wd.MinimizeAction,
		"gameLaunchAction": wd.GameLaunchAction, "gameLaunchDelaySec": wd.GameLaunchDelaySec,
		"gameExitAction": wd.GameExitAction, "restoreOnCrash": wd.RestoreOnCrash,
	}
	for k, def := range want {
		i, ok := byKey[k]
		if !ok {
			t.Errorf("у схемі немає %s", k)
			continue
		}
		if schema[i].Default != def {
			t.Errorf("%s: default схеми %v ≠ winbehavior %v", k, schema[i].Default, def)
		}
	}
	for _, d := range schema {
		if d.Type == "choice" {
			if len(d.Options) < 2 {
				t.Errorf("%s: choice без варіантів", d.Key)
			}
			for _, o := range d.Options {
				if o.Desc == "" || o.Label == "" {
					t.Errorf("%s/%s: немає label/desc", d.Key, o.Value)
				}
			}
		}
		for _, c := range d.ShowIf {
			if _, ok := byKey[c.Key]; !ok {
				t.Errorf("%s: showIf на невідомий ключ %s", d.Key, c.Key)
			}
		}
	}
	for _, gone := range []string{"openOnStartup", "closeOnLaunch", "exitOnGameClose", "hideOnGameOpen"} {
		if _, ok := byKey[gone]; ok {
			t.Errorf("застарілий ключ %s лишився в схемі", gone)
		}
	}
}
