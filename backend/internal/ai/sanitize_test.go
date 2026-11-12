package ai

import (
	"strings"
	"testing"
)

func TestSanitizeRemovesPrivateData(t *testing.T) {
	cases := []struct{ name, in string }{
		{"windows user", `C:\Users\Ivan Petrenko\AppData\Roaming\.shaurm\logs\latest.log`},
		{"windows slash", `C:/Users/ivan/AppData/x`},
		{"escaped", `C:\\Users\\ivan\\AppData`},
		{"linux user", `/home/ivan/.minecraft/mods`},
		{"setting user", `[Render thread/INFO]: Setting user: CoolNick123`},
		{"launch args", `--username CoolNick --uuid 069a79f4-44e9-4726-a5be-fca90e38aaf5 --accessToken eyJhbGciOiJIUzI1NiJ9.abcdefghijk.sig12345`},
		{"session", `Session ID is token:abcdef0123456789:0123456789abcdef`},
		{"ipv4", `Connecting to 203.0.113.45, 25565`},
		{"ipv4 port", `Lost connection 198.51.100.7:25565 timed out`},
		{"host", `Connecting to play.myserver.net, 25565`},
		{"shaurma p2p", `joined maniac-810988.play.shaurma-launcher.org:25565`},
		{"email", `user.name+tag@gmail.com`},
		{"query", `GET https://api.example.com/v1/x?key=SECRET123&user=ivan`},
		{"userinfo", `https://admin:pass@host.example/path`},
		{"ipv6", `from 2001:0db8:85a3:0000:0000:8a2e:0370:7334 failed`},
		{"hash", `hwid=` + strings.Repeat("ab", 32)},
	}
	for _, c := range cases {
		got := Sanitize(c.in, nil)
		for _, bad := range []string{"Ivan", "ivan", "CoolNick", "069a79f4", "eyJhbGci", "abcdef0123", "203.0.113.45", "198.51.100.7", "myserver", "maniac-810988", "gmail", "SECRET123", "admin:pass", "2001:0db8", "ababab"} {
			if strings.Contains(got, bad) {
				t.Errorf("%s: %q still contains %q → %q", c.name, c.in, bad, got)
			}
		}
	}
}

func TestSanitizeKeepsDiagnosticData(t *testing.T) {
	keep := []string{
		`[Server thread/INFO]: Starting minecraft server version 1.21.1`,
		`java.lang.NoClassDefFoundError: net/minecraft/client/Minecraft`,
		`Caused by: java.lang.OutOfMemoryError: Java heap space`,
		`Mod fabric-api-0.100.1+1.21.1 requires fabricloader >=0.15.11`,
		`Mod sodium 0.5.11.4 failed to load`,
		`at com.example.mod.Foo.bar(Foo.java:42)`,
		`Unsupported class file major version 65`,
		`Connecting to localhost, 25565`,
		`127.0.0.1:45678`,
	}
	for _, k := range keep {
		got := Sanitize(k, nil)
		if strings.Contains(k, "localhost") || strings.Contains(k, "127.0.0.1") {
			continue // їх змінювати не обовʼязково
		}
		if got != k {
			t.Errorf("зіпсовано діагностичний текст: %q → %q", k, got)
		}
	}
}

func TestSanitizeExactValues(t *testing.T) {
	priv := privateValues([]string{"Steve_77", "Alex"})
	got := Sanitize(`Player Steve_77 and alex joined; STEVE_77 left`, priv)
	if strings.Contains(strings.ToLower(got), "steve_77") || strings.Contains(strings.ToLower(got), "alex") {
		t.Fatalf("нік не прибрано: %q", got)
	}
}

func TestSanitizeCrashContextCoversMods(t *testing.T) {
	cc := CrashContext{LogWindow: `Setting user: Bob99`, Mods: []string{"coolmod-for-Bob99.jar"}, Private: []string{"Bob99"}}
	out := SanitizeCrashContext(cc)
	if strings.Contains(out.LogWindow, "Bob99") || strings.Contains(out.Mods[0], "Bob99") || out.Private != nil {
		t.Fatalf("очищення неповне: %+v", out)
	}
}
