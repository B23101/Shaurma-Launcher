package voicecfg

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, dir, rel, body string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func has(v Verdict, code string) bool {
	for _, i := range v.Issues {
		if i.Code == code {
			return true
		}
	}
	return false
}

func TestSVCMinusOneIsServerPort(t *testing.T) {
	d := t.TempDir()
	write(t, d, "mods/voicechat-fabric-1.20.1-2.5.0.jar", "x")
	write(t, d, "config/voicechat/voicechat-server.properties", "# c\nport=-1\nbind_address=\n")
	s := Detect(d, 25570)
	if len(s) != 1 || s[0].Port != 25570 || !s[0].SameAsServer {
		t.Fatalf("got %+v", s)
	}
}

func TestSVCDefaultAndCustom(t *testing.T) {
	d := t.TempDir()
	write(t, d, "plugins/voicechat-bukkit-2.5.jar", "x")
	if s := Detect(d, 25565); len(s) != 1 || s[0].Port != 24454 || s[0].ConfigPath != "" {
		t.Fatalf("default: %+v", s)
	}
	write(t, d, "plugins/voicechat/voicechat-server.properties", "port=50127\n")
	if s := Detect(d, 25565); s[0].Port != 50127 || s[0].SameAsServer {
		t.Fatalf("custom: %+v", s)
	}
}

func TestPlasmoZeroIsServerPort(t *testing.T) {
	d := t.TempDir()
	write(t, d, "plugins/PlasmoVoice-spigot-2.1.jar", "x")
	write(t, d, "plugins/PlasmoVoice/config.toml", "[host]\nip = \"0.0.0.0\"\nport = 0 # same\n[host.public]\nport = 28000\n")
	s := Detect(d, 25565)
	if len(s) != 1 || s[0].Kind != KindPlasmo || s[0].Port != 25565 || !s[0].SameAsServer {
		t.Fatalf("got %+v", s)
	}
	write(t, d, "plugins/PlasmoVoice/config.toml", "[host]\nport = 30001\n")
	if s := Detect(d, 25565); s[0].Port != 30001 || s[0].SameAsServer {
		t.Fatalf("explicit: %+v", s)
	}
}

func TestAddonsAndDisabled(t *testing.T) {
	d := t.TempDir()
	write(t, d, "mods/voicechat-interaction-1.0.jar", "x")
	write(t, d, "mods/plasmo-voice-addons.jar", "x")
	write(t, d, "mods/voicechat-fabric.jar.disabled", "x")
	s := Detect(d, 25565)
	if len(s) != 1 || s[0].Enabled {
		t.Fatalf("got %+v", s)
	}
}

func TestEvaluate(t *testing.T) {
	d := t.TempDir()
	write(t, d, "mods/voicechat-fabric.jar", "x")
	write(t, d, "config/voicechat/voicechat-server.properties", "port=24454\n")
	s := Detect(d, 25565)
	if v := Evaluate(s, 25565, false, 0, false); !has(v, "voice_found_udp_off") {
		t.Fatalf("expected udp_off: %+v", v.Issues)
	}
	if v := Evaluate(s, 25565, true, 25565, false); !has(v, "udp_port_mismatch") || v.AutoPort != 24454 {
		t.Fatalf("expected mismatch: %+v", v)
	}
	if v := Evaluate(s, 25565, true, 0, false); len(v.Issues) != 0 || v.EffectivePort != 24454 {
		t.Fatalf("clean: %+v", v)
	}
	if v := Evaluate(nil, 25565, true, 0, false); !has(v, "udp_no_voice_mod") {
		t.Fatalf("expected no_mod: %+v", v.Issues)
	}
	// два моди на одному порту
	write(t, d, "mods/plasmovoice-fabric.jar", "x")
	write(t, d, "config/plasmovoice/config.toml", "[host]\nport = 24454\n")
	if v := Evaluate(Detect(d, 25565), 25565, true, 0, false); !has(v, "voice_conflict") {
		t.Fatalf("expected conflict: %+v", v.Issues)
	}
}
