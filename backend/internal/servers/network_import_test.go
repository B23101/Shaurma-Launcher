package servers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Готова мережа, як її збирають вручну: тека з проксі й серверами в
// підтеках, velocity.toml від самого Velocity (таби, інлайн forced-hosts,
// власні ключі, яких лаунчер не знає).
const handVelocityToml = `config-version = "2.7"
bind = "0.0.0.0:25777"
motd = "<#B908FB>ᴄʏʙᴇʀ \n ᴄʟᴜʙ"
show-max-players = 500
online-mode = false
force-key-authentication = true
player-info-forwarding-mode = "modern"
forwarding-secret-file = "forwarding.secret"
ping-passthrough = "DISABLED"
enable-player-address-logging = true
forced-hosts = {}

[servers]
	lobby_01 = "127.0.0.1:49907"
	remote_box = "10.0.0.5:25565"
	try = ["lobby_01"]

[advanced]
	compression-threshold = 256
	accepts-transfers = false

[query]
	enabled = false
	port = 25577
	map = "&l&#ff006aCyber club"
`

func writeFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestImportNetworkFolderKeepsHandMadeConfig(t *testing.T) {
	data := t.TempDir()
	api := NewAPI(data, nil, nil)
	root := filepath.Join(t.TempDir(), "CYBERCLUB")

	proxyDir := filepath.Join(root, "velocity")
	writeFile(t, filepath.Join(proxyDir, "velocity.toml"), handVelocityToml)
	writeFile(t, filepath.Join(proxyDir, "velocity.jar"), "jar")
	writeFile(t, filepath.Join(proxyDir, "forwarding.secret"), "s3cret\n")

	hubDir := filepath.Join(root, "hub 1.21.4")
	writeFile(t, filepath.Join(hubDir, "purpur.yml"), "settings: {}\n")
	writeFile(t, filepath.Join(hubDir, "server.jar"), "jar")
	writeFile(t, filepath.Join(hubDir, "eula.txt"), "eula=true\n")
	writeFile(t, filepath.Join(hubDir, "server.properties"), "server-port=49907\nserver-ip=127.0.0.1\nonline-mode=false\n")

	info, err := api.DetectNetworkFolder(root)
	if err != nil || !info.HasProxy || len(info.Servers) != 2 {
		t.Fatalf("DetectNetworkFolder: %+v, %v", info, err)
	}
	res, err := api.ImportNetworkFolder(root)
	if err != nil || res.ProxyID == "" || len(res.Errors) != 0 {
		t.Fatalf("ImportNetworkFolder: %+v, %v", res, err)
	}

	view, err := api.NetworkGetView(res.ProxyID)
	if err != nil {
		t.Fatal(err)
	}
	var hub *NetworkMemberView
	for i := range view.Members {
		if view.Members[i].InNetwork {
			hub = &view.Members[i]
		}
	}
	if hub == nil || hub.Alias != "lobby_01" || !hub.InTry {
		t.Fatalf("hub має стати lobby_01 з входом: %+v", hub)
	}
	if len(view.External) != 1 || view.External[0].Alias != "remote_box" {
		t.Errorf("сервер поза лаунчером не збережено: %+v", view.External)
	}
	s := view.Settings.Proxy
	if s.OnlineMode || s.PingPassthrough != "disabled" || s.ForwardingMode != "modern" || !strings.Contains(s.Motd, "\n") {
		t.Errorf("налаштування проксі не прочитано: %+v", s)
	}
	if !view.Settings.LockBackends {
		t.Errorf("сервери вже на 127.0.0.1 — «лише через проксі» має бути увімкнено")
	}

	// Будь-яка зміна мережі переписує лише ключі лаунчера.
	if err := api.NetworkRepair(res.ProxyID); err != nil {
		t.Fatal(err)
	}
	cfgBytes, _ := os.ReadFile(filepath.Join(proxyDir, "velocity.toml"))
	cfg := string(cfgBytes)
	for _, want := range []string{
		`map = "&l&#ff006aCyber club"`, "accepts-transfers = false", "enable-player-address-logging = true",
		`"lobby_01" = "127.0.0.1:49907"`, `"remote_box" = "10.0.0.5:25565"`, `try = ["lobby_01"]`,
		`bind = "0.0.0.0:25777"`, "online-mode = false",
	} {
		if !strings.Contains(cfg, want) {
			t.Errorf("velocity.toml без %q:\n%s", want, cfg)
		}
	}
	if strings.Count(cfg, "forced-hosts") != 1 || strings.Count(cfg, "[servers]") != 1 {
		t.Errorf("дубль секцій у velocity.toml:\n%s", cfg)
	}
	// Paper: online-mode проксі (false), секрет із forwarding.secret.
	yml, _ := os.ReadFile(filepath.Join(hubDir, "config", "paper-global.yml"))
	if v, _ := getYAMLScalar(string(yml), []string{"proxies", "velocity", "online-mode"}); v != "false" {
		t.Errorf("paper online-mode = %q, очікували false (як у проксі)", v)
	}
	if v, _ := getYAMLScalar(string(yml), []string{"proxies", "velocity", "secret"}); v != "s3cret" {
		t.Errorf("paper secret = %q", v)
	}
}
