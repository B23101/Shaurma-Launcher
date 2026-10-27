package servers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Повний набір ключів, які генерує чистий Java 26.3 (69 шт.).
var java263Keys = strings.Fields(`
accepts-transfers allow-flight broadcast-console-to-ops broadcast-rcon-to-ops
bug-report-link chat-spam-threshold-seconds command-spam-threshold-seconds
difficulty enable-code-of-conduct enable-jmx-monitoring enable-query enable-rcon
enable-status enforce-secure-profile enforce-whitelist entity-broadcast-range-percentage
force-gamemode function-permission-level gamemode generate-structures generator-settings
hardcore hide-online-players initial-disabled-packs initial-enabled-packs level-name
level-seed level-type log-ips management-server-allowed-origins management-server-enabled
management-server-host management-server-port management-server-secret
management-server-tls-enabled management-server-tls-keystore
management-server-tls-keystore-password max-chained-neighbor-updates max-players
max-tick-time max-world-size motd network-compression-threshold online-mode
op-permission-level pause-when-empty-seconds player-idle-timeout prevent-proxy-connections
query.port rate-limit rcon.password rcon.port region-file-compression require-resource-pack
resource-pack resource-pack-id resource-pack-prompt resource-pack-sha1 server-ip server-port
simulation-distance spawn-protection status-heartbeat-interval sync-chunk-writes
text-filtering-config text-filtering-version use-native-transport view-distance white-list`)

func TestSchemaCoversEveryJava263Key(t *testing.T) {
	if len(java263Keys) != 69 {
		t.Fatalf("у тесті має бути 69 ключів, а їх %d", len(java263Keys))
	}
	for _, k := range java263Keys {
		e, ok := PropertiesSchema[k]
		if !ok {
			t.Errorf("ключ %q відсутній у схемі", k)
			continue
		}
		if e.Group == "" {
			t.Errorf("ключ %q без групи", k)
		}
		if e.Type == "" {
			t.Errorf("ключ %q без типу", k)
		}
	}
}

func TestSchemaHasNoDuplicatesAndNoBedrockKeys(t *testing.T) {
	seen := map[string]bool{}
	for _, e := range schemaList {
		if seen[e.Key] {
			t.Errorf("дубль у схемі: %s", e.Key)
		}
		seen[e.Key] = true
	}
	if _, ok := PropertiesSchema["server-name"]; ok {
		t.Error("server-name — ключ Bedrock, у Java його немає")
	}
}

func TestSchemaLimitsMatchWiki(t *testing.T) {
	tests := []struct {
		key, value string
		wantErr    bool
	}{
		{"view-distance", "2", true}, // мінімум 3
		{"view-distance", "3", false},
		{"view-distance", "32", false},
		{"view-distance", "33", true},
		{"simulation-distance", "2", true},
		{"op-permission-level", "0", false}, // 0..4
		{"op-permission-level", "5", true},
		{"function-permission-level", "0", true}, // 1..4
		{"function-permission-level", "4", false},
		{"server-port", "65534", false},
		{"server-port", "65535", true},
		{"rcon.port", "0", true},
		{"max-players", "0", false},
		{"max-players", "5000", false}, // раніше ліміт був 1000
		{"max-players", "2147483647", false},
		{"max-players", "2147483648", true},
		{"max-tick-time", "-1", false},
		{"max-tick-time", "-2", true},
		{"max-tick-time", "9223372036854775807", false},
		{"entity-broadcast-range-percentage", "9", true},
		{"entity-broadcast-range-percentage", "1000", false},
		{"entity-broadcast-range-percentage", "1001", true},
		{"max-world-size", "29999984", false},
		{"max-world-size", "29999985", true},
		{"network-compression-threshold", "-1", false},
		{"network-compression-threshold", "-2", true},
		{"spawn-protection", "0", false},
		{"spawn-protection", "500", false},
		{"rate-limit", "-1", true},
		{"rate-limit", "100000", false},
		{"region-file-compression", "lz4", false},
		{"region-file-compression", "zip", true},
		{"log-ips", "yes", true}, // bool, раніше такого ключа в схемі не було
		{"log-ips", "false", false},
		{"text-filtering-version", "2", true},
		{"unknown-mod-key", "whatever", false},
	}
	for _, tt := range tests {
		err := ValidateProperty(tt.key, tt.value)
		if (err != nil) != tt.wantErr {
			t.Errorf("ValidateProperty(%q, %q) = %v, wantErr %v", tt.key, tt.value, err, tt.wantErr)
		}
	}
}

func TestLevelTypeAcceptsCustomValues(t *testing.T) {
	for _, v := range []string{"minecraft\\:flat", "minecraft:flat", "terra:custom", "minecraft\\:single_biome_surface"} {
		if err := ValidateProperty("level-type", v); err != nil {
			t.Errorf("level-type=%q має бути дозволено: %v", v, err)
		}
	}
}

func TestMCVersionCompare(t *testing.T) {
	tests := []struct {
		a, b string
		want int
		ok   bool
	}{
		{"1.21.8", "1.21.9", -1, true},
		{"1.21.9", "1.21.9", 0, true},
		{"1.21.11", "1.21.9", 1, true}, // 11 > 9, не лексикографічно
		{"26.3", "1.21.11", 1, true},   // нова нумерація
		{"26.2", "26.3", -1, true},
		{"1.21", "1.21.0", 0, true},
		{"1.21.9-rc1", "1.21.9", 0, true},
		{"24w14a", "1.21.9", 0, false}, // снапшот
		{"", "1.21.9", 0, false},
	}
	for _, tt := range tests {
		got, ok := cmpMCVersion(tt.a, tt.b)
		if ok != tt.ok || (ok && got != tt.want) {
			t.Errorf("cmpMCVersion(%q,%q) = %d,%v; want %d,%v", tt.a, tt.b, got, ok, tt.want, tt.ok)
		}
	}
}

func TestPropStatusByVersion(t *testing.T) {
	pvp := PropertiesSchema["pvp"]
	if got := PropStatus(pvp, "1.21.8"); got != "" {
		t.Errorf("pvp у 1.21.8 має діяти, status=%q", got)
	}
	if got := PropStatus(pvp, "1.21.9"); got != "removed" {
		t.Errorf("pvp у 1.21.9 прибрано, status=%q", got)
	}
	if got := PropStatus(pvp, "26.3"); got != "removed" {
		t.Errorf("pvp у 26.3 прибрано, status=%q", got)
	}
	if got := PropStatus(pvp, ""); got != "" {
		t.Errorf("невідома версія — нічого не ховаємо, status=%q", got)
	}
	if got := PropStatus(PropertiesSchema["spawn-npcs"], "1.21.2"); got != "removed" {
		t.Errorf("spawn-npcs прибрано в 1.21.2, status=%q", got)
	}
	chat := PropertiesSchema["chat-spam-threshold-seconds"]
	if got := PropStatus(chat, "26.1"); got != "new" {
		t.Errorf("chat-spam з'явився в 26.2, status=%q", got)
	}
	if got := PropStatus(chat, "26.2"); got != "" {
		t.Errorf("chat-spam у 26.2 діє, status=%q", got)
	}
}

func TestWhitelistDefaultChangedIn263(t *testing.T) {
	wl := PropertiesSchema["white-list"]
	if got := defaultFor(wl, "26.2"); got != "false" {
		t.Errorf("26.2: %q", got)
	}
	if got := defaultFor(wl, "26.3"); got != "true" {
		t.Errorf("26.3: %q", got)
	}
	if got := defaultFor(wl, ""); got != "false" {
		t.Errorf("невідома версія: %q", got)
	}
}

func TestParseDeduplicatesKeysLastWins(t *testing.T) {
	content := "motd=first\nview-distance=8\nmotd=second\n"
	entries := parsePropEntries(content, "")
	if len(entries) != 2 {
		t.Fatalf("очікували 2 записи, отримали %d", len(entries))
	}
	if entries[0].Key != "motd" || entries[0].Value != "second" {
		t.Errorf("motd: %+v", entries[0])
	}
}

func TestParseInfersTypeOfUnknownKeys(t *testing.T) {
	entries := parsePropEntries("some-mod-flag=true\nsome-mod-count=42\nsome-mod-name=abc\nsome-zero=0\nsome-lead=007\n", "")
	want := map[string]string{
		"some-mod-flag": "bool", "some-mod-count": "int", "some-mod-name": "string",
		"some-zero": "int", "some-lead": "string",
	}
	for _, e := range entries {
		if e.Type != want[e.Key] {
			t.Errorf("%s: type=%q, want %q", e.Key, e.Type, want[e.Key])
		}
		if e.Known || e.Group != PropGroupOther {
			t.Errorf("%s: Known=%v Group=%q", e.Key, e.Known, e.Group)
		}
	}
}

func TestParseMarksRemovedKeysForNewVersion(t *testing.T) {
	entries := parsePropEntries("pvp=true\nmotd=x\n", "26.3")
	if entries[0].Status != "removed" {
		t.Errorf("pvp у 26.3: status=%q", entries[0].Status)
	}
	if entries[1].Status != "" {
		t.Errorf("motd: status=%q", entries[1].Status)
	}
}

func TestDefaultFileFor263HasExactlyModernKeys(t *testing.T) {
	dir := t.TempDir()
	if err := writeDefaultServerProperties(dir, "26.3"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "server.properties"))
	got := map[string]bool{}
	for _, e := range parsePropEntries(string(data), "26.3") {
		got[e.Key] = true
	}
	for _, k := range java263Keys {
		if k == "management-server-secret" {
			continue // генерує сам сервер
		}
		if !got[k] {
			t.Errorf("у згенерованому файлі 26.3 немає %q", k)
		}
	}
	for _, k := range []string{"pvp", "enable-command-block", "allow-nether", "spawn-monsters", "spawn-animals", "spawn-npcs", "server-name"} {
		if got[k] {
			t.Errorf("у файлі 26.3 не має бути прибраного ключа %q", k)
		}
	}
}

func TestDefaultFileForOldVersionKeepsLegacyKeys(t *testing.T) {
	dir := t.TempDir()
	if err := writeDefaultServerProperties(dir, "1.20.1"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "server.properties"))
	s := string(data)
	for _, k := range []string{"pvp=true", "spawn-npcs=true", "allow-nether=true"} {
		if !strings.Contains(s, k) {
			t.Errorf("у файлі 1.20.1 має бути %q", k)
		}
	}
	for _, k := range []string{"management-server-enabled", "chat-spam-threshold-seconds"} {
		if strings.Contains(s, k) {
			t.Errorf("у файлі 1.20.1 не має бути %q (ще не існує)", k)
		}
	}
}
