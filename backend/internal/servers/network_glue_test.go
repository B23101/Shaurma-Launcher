package servers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Paper після першого старту сам пише proxies.velocity з enabled: false і
// secret: ”. Раніше glue бачив «secret: » і вважав, що все налаштовано, —
// forwarding так і лишався вимкненим. Ключі мають ставати на своє місце.
func TestGlueFixesPaperGeneratedConfig(t *testing.T) {
	api, proxy, backend := newTestNetworkEnv(t)
	generated := `_version: 29
proxies:
  bungee-cord:
    online-mode: true
  proxy-protocol: false
  velocity:
    enabled: false
    online-mode: true
    secret: ''
scoreboards:
  save-empty-scoreboard-teams: true
`
	file := filepath.Join(backend.Dir, "config", "paper-global.yml")
	if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(generated), 0644); err != nil {
		t.Fatal(err)
	}
	if err := api.NetworkSave(proxy.ID, []NetworkMemberView{{ServerID: backend.ID, InNetwork: true, Order: 1, AutoStart: true, InTry: true}}); err != nil {
		t.Fatalf("NetworkSave: %v", err)
	}
	data, _ := os.ReadFile(file)
	doc := string(data)
	secret, _ := os.ReadFile(filepath.Join(proxy.Dir, "forwarding.secret"))
	if v, _ := getYAMLScalar(doc, []string{"proxies", "velocity", "enabled"}); v != "true" {
		t.Errorf("velocity.enabled = %q, очікували true:\n%s", v, doc)
	}
	if v, _ := getYAMLScalar(doc, []string{"proxies", "velocity", "secret"}); v != strings.TrimSpace(string(secret)) {
		t.Errorf("velocity.secret = %q, очікували секрет проксі", v)
	}
	if strings.Count(doc, "proxies:") != 1 || !strings.Contains(doc, "save-empty-scoreboard-teams: true") {
		t.Errorf("файл зіпсовано (дубль proxies або втрачені ключі):\n%s", doc)
	}
	issues, err := api.NetworkDiagnose(proxy.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range issues {
		if i.Level == "error" {
			t.Errorf("після налаштування діагностика бачить помилку: %+v", i)
		}
	}
}

func TestSetYAMLScalarCreatesMissingPath(t *testing.T) {
	doc := setYAMLScalar("settings:\n  foo: 1\n", []string{"settings", "velocity-support", "enabled"}, "true")
	if v, ok := getYAMLScalar(doc, []string{"settings", "velocity-support", "enabled"}); !ok || v != "true" {
		t.Fatalf("ключ не створено:\n%s", doc)
	}
	if v, _ := getYAMLScalar(doc, []string{"settings", "foo"}); v != "1" {
		t.Errorf("сусідній ключ втрачено:\n%s", doc)
	}
	empty := setYAMLScalar("", []string{"proxies", "velocity", "secret"}, "'x'")
	if v, _ := getYAMLScalar(empty, []string{"proxies", "velocity", "secret"}); v != "x" {
		t.Errorf("у порожньому файлі ключ не створено:\n%s", empty)
	}
}

func TestAliasFromName(t *testing.T) {
	cases := map[string]string{
		"Виживання":        "vyzhyvannia",
		"Лобі 2":           "lobi-2",
		"My Survival!!":    "my-survival",
		"Creative_World":   "creative_world",
		"hub 1.21.4":       "hub",
		"chunkbattle_1.21": "chunkbattle",
		"   ":              "",
	}
	for in, want := range cases {
		if got := AliasFromName(in); got != want {
			t.Errorf("AliasFromName(%q) = %q, очікували %q", in, got, want)
		}
	}
	taken := map[string]bool{"lobby": true}
	if got := uniqueAlias("lobby", taken); got != "lobby-2" {
		t.Errorf("uniqueAlias = %q, очікували lobby-2", got)
	}
}

// Старий network.json (до автозапуску й налаштувань проксі): усі сервери
// стартували з мережею і приймали вхід — так і має лишитись.
func TestLoadNetworkMigratesOldFormat(t *testing.T) {
	dir := t.TempDir()
	old := `{"id":"p","name":"P","servers":[{"serverId":"p","isProxy":true},{"serverId":"a","order":1}]}`
	_ = os.MkdirAll(filepath.Join(dir, ".shaurma"), 0755)
	if err := os.WriteFile(filepath.Join(dir, ".shaurma", "network.json"), []byte(old), 0644); err != nil {
		t.Fatal(err)
	}
	net, err := LoadNetwork(dir)
	if err != nil {
		t.Fatal(err)
	}
	m, ok := net.Member("a")
	if !ok || !m.AutoStart || !m.InTry {
		t.Errorf("учасник старої мережі має стартувати й приймати вхід: %+v", m)
	}
	if net.Proxy.ForwardingMode != "modern" || net.Version != networkVersion {
		t.Errorf("налаштування проксі не проставлено: %+v", net)
	}
}

func TestRenderVelocityConfigQuotesNames(t *testing.T) {
	cfg := ApplyVelocityConfig("", 25577, DefaultProxySettings(), []VelocityBackend{
		{Alias: "lobby", Address: "127.0.0.1:25566", InTry: true, ForcedHost: "lobby.example.com"},
		{Alias: "survival", Address: "127.0.0.1:25567"},
	})
	for _, want := range []string{
		`config-version = "2.7"`, `bind = "0.0.0.0:25577"`,
		`"lobby" = "127.0.0.1:25566"`, `"survival" = "127.0.0.1:25567"`,
		`try = ["lobby"]`, `forced-hosts = {"lobby.example.com" = ["lobby"]}`,
		`forwarding-secret-file = "forwarding.secret"`,
	} {
		if !strings.Contains(cfg, want) {
			t.Errorf("velocity.toml без %s:\n%s", want, cfg)
		}
	}
}

// Зміна порту сервера одразу потрапляє в конфіг проксі.
func TestPortChangeSyncsProxy(t *testing.T) {
	api, proxy, backend := newTestNetworkEnv(t)
	if err := api.NetworkSave(proxy.ID, []NetworkMemberView{{ServerID: backend.ID, InNetwork: true, Order: 1, InTry: true}}); err != nil {
		t.Fatal(err)
	}
	if err := api.SaveServerProperties(backend.ID, []PropUpdate{{Key: "server-port", Value: "25599"}}); err != nil {
		t.Fatal(err)
	}
	cfg, _ := os.ReadFile(filepath.Join(proxy.Dir, "velocity.toml"))
	if !strings.Contains(string(cfg), `"vyzhyvannia" = "127.0.0.1:25599"`) {
		t.Errorf("проксі не отримав новий порт:\n%s", cfg)
	}
}

// Сервер лаунчера, доданий вручну як «поза лаунчером» (127.0.0.1:його
// порт), прив'язується до мережі зі своїм ім'ям — і зміна порту доходить
// до проксі.
func TestManualEntryLinksLauncherServer(t *testing.T) {
	api, proxy, backend := newTestNetworkEnv(t)
	if err := api.NetworkSaveExternal(proxy.ID, []ExternalServer{{Alias: "lobby_01", Address: "127.0.0.1:25566", InTry: true}}); err != nil {
		t.Fatal(err)
	}
	net, _ := LoadNetwork(proxy.Dir)
	m, ok := net.Member(backend.ID)
	if !ok || m.Alias != "lobby_01" || !m.InTry || len(net.External) != 0 {
		t.Fatalf("запис не прив'язано до сервера лаунчера: %+v, external=%+v", m, net.External)
	}
	if err := api.SaveServerProperties(backend.ID, []PropUpdate{{Key: "server-port", Value: "25601"}}); err != nil {
		t.Fatal(err)
	}
	cfg, _ := os.ReadFile(filepath.Join(proxy.Dir, "velocity.toml"))
	if !strings.Contains(string(cfg), `"lobby_01" = "127.0.0.1:25601"`) {
		t.Errorf("новий порт не потрапив у проксі:\n%s", cfg)
	}
}
