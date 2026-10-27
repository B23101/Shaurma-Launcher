package main

import "testing"

func TestIsSameModVersion(t *testing.T) {
	cases := []struct {
		name          string
		instFile      string
		instVersion   string
		remoteFile    string
		remoteVersion string
		wantSame      bool
	}{
		{
			name:          "файл ідентичний (звичайний + у версії файлу)",
			instFile:      "ImmediatelyFast-Forge-1.5.5+1.20.4.jar",
			instVersion:   "1.5.5+1.20.4",
			remoteFile:    "ImmediatelyFast-Forge-1.5.5+1.20.4.jar",
			remoteVersion: "1.5.5+1.20.4",
			wantSame:      true,
		},
		{
			name:          "файл на диску з URL-закодованим %2B, віддалений — з +",
			instFile:      "ImmediatelyFast-Forge-1.5.5%2B1.20.4.jar",
			instVersion:   "1.5.5+1.20.4",
			remoteFile:    "ImmediatelyFast-Forge-1.5.5+1.20.4.jar",
			remoteVersion: "1.5.5+1.20.4-forge",
			wantSame:      true,
		},
		{
			name:          "remote version_number з суфіксом лоадера — той самий реліз",
			instFile:      "ImmediatelyFast-Forge-1.5.5%2B1.20.4.jar",
			instVersion:   "1.5.5+1.20.4",
			remoteFile:    "ImmediatelyFast-Forge-1.5.5%2B1.20.4.jar",
			remoteVersion: "1.5.5+1.20.4-forge",
			wantSame:      true,
		},
		{
			name:          "версія в mods.toml без appendix проти Modrinth з appendix і лоадером",
			instFile:      "SomeMod-1.5.5.jar",
			instVersion:   "1.5.5",
			remoteFile:    "SomeMod-1.5.5+1.20.4-forge.jar",
			remoteVersion: "1.5.5+1.20.4-forge",
			wantSame:      true,
		},
		{
			name:          "fabric-суфікс теж не заважає",
			instFile:      "X-2.0.0+1.20.1.jar",
			instVersion:   "2.0.0+1.20.1",
			remoteFile:    "X-2.0.0%2B1.20.1-fabric.jar",
			remoteVersion: "2.0.0+1.20.1-fabric",
			wantSame:      true,
		},
		{
			name:          "справжнє оновлення — різні числові ядра",
			instFile:      "ImmediatelyFast-Forge-1.5.5%2B1.20.4.jar",
			instVersion:   "1.5.5+1.20.4",
			remoteFile:    "ImmediatelyFast-Forge-1.6.0%2B1.20.4.jar",
			remoteVersion: "1.6.0+1.20.4-forge",
			wantSame:      false,
		},
		{
			name:          "мажорний стрибок (10 > 2, а не лексикографічно)",
			instFile:      "A-2.0.jar",
			instVersion:   "2.0",
			remoteFile:    "A-10.0.jar",
			remoteVersion: "10.0",
			wantSame:      false,
		},
		{
			name:          "суфікс -fabric у версії не маскує іншу версію",
			instFile:      "A-1.5.5.jar",
			instVersion:   "1.5.5",
			remoteFile:    "A-1.5.6-fabric.jar",
			remoteVersion: "1.5.6-fabric",
			wantSame:      false,
		},
		{
			name:          "порожня версія — не вважаємо однаковою",
			instFile:      "no-meta.jar",
			instVersion:   "",
			remoteFile:    "no-meta.jar",
			remoteVersion: "1.0.0",
			wantSame:      true, // збіг за ім'ям файлу
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := isSameModVersion(c.instFile, c.instVersion, c.remoteFile, c.remoteVersion)
			if got != c.wantSame {
				t.Errorf("isSameModVersion(%q,%q,%q,%q) = %v, хотіли %v",
					c.instFile, c.instVersion, c.remoteFile, c.remoteVersion, got, c.wantSame)
			}
		})
	}
}

func TestVersionCore(t *testing.T) {
	cases := []struct{ in, want string }{
		{"1.5.5+1.20.4", "1.5.5"},
		{"1.5.5+1.20.4-forge", "1.5.5"},
		{"1.5.5-forge", "1.5.5"},
		{"2.0.0+1.20.1-fabric", "2.0.0"},
		{"1.20.1-47.2.0", "1.20.1-47.2.0"},
		{"1.5.5%2B1.20.4-forge", "1.5.5"},
		{"6.0.8", "6.0.8"},
	}
	for _, c := range cases {
		if got := versionCore(c.in); got != c.want {
			t.Errorf("versionCore(%q) = %q, хотіли %q", c.in, got, c.want)
		}
	}
}

func TestDecodeURLTokens(t *testing.T) {
	cases := map[string]string{
		"mod-1.0%2B1.20.jar":      "mod-1.0+1.20.jar",
		"mod%20name.jar":          "mod name.jar",
		"mod-%5B1.20%5D.jar":      "mod-[1.20].jar",
		"mod-%281.0%29%2C2.jar":   "mod-(1.0),2.jar",
		"caf%C3%A9.jar":           "café.jar",
		"100%.jar":                "100%.jar", // одинокий % — не токен
		"mod%2.jar":               "mod%2.jar", // обрізаний токен
		"bad%FF%FE.jar":           "bad%FF%FE.jar", // не UTF-8 — лишаємо
		"plain.jar":               "plain.jar",
	}
	for in, want := range cases {
		if got := decodeURLTokens(in); got != want {
			t.Errorf("decodeURLTokens(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsSameModVersionPercentNames(t *testing.T) {
	// Локальний файл із літеральними %XX, віддалений — із самими символами.
	if !isSameModVersion("mod-%5B1.20.1%5D-1.0.jar", "1.0", "mod-[1.20.1]-1.0.jar", "1.0.1") {
		t.Error("%5B/%5D у назві файлу мають вважатись тим самим файлом")
	}
	// Протилежний випадок: закодована віддалена назва, декодована локальна.
	if !isSameModVersion("mod-[1.20.1]-1.0.jar", "1.0", "mod-%5B1.20.1%5D-1.0.jar", "1.0.1") {
		t.Error("симетричне порівняння має збігатись")
	}
	// Справді інший файл не має ставати «тим самим».
	if isSameModVersion("mod-%5B1.20.1%5D-1.0.jar", "1.0", "mod-[1.20.1]-1.1.jar", "1.1") {
		t.Error("різні версії мають лишатись оновленням")
	}
}
