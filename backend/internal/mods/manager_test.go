package mods

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

// TestParseModsTomlRealWorld перевіряє, що парсер mods.toml витягує
// displayName/version/modId з типових Forge-структур (KotlinForForge,
// GPU Memory Leak Fix тощо) — це корінь проблеми «мод показує версію
// unknown і не знаходиться на CurseForge».
func TestParseModsTomlRealWorld(t *testing.T) {
	cases := []struct {
		name        string
		toml        string
		wantName    string
		wantVersion string
		wantID      string
	}{
		{
			name: "KotlinForForge стиль (modLoader нагорі, [[mods]] з version)",
			toml: `modLoader="javafml"
loaderVersion="[47,)"
license="LGPLv3"
[[mods]]
modId="kotlinforforge"
version="4.12.0"
displayName="Kotlin For Forge"
displayURL="https://github.com/thedarkcolour/KotlinForForge"
authors="thedarkcolour"
[[dependencies.kotlinforforge]]
modId="forge"
mandatory=true
versionRange="[47,)"
ordering="NONE"
side="BOTH"
`,
			wantName: "Kotlin For Forge", wantVersion: "4.12.0", wantID: "kotlinforforge",
		},
		{
			name: "GPU Memory Leak Fix стиль",
			toml: `modLoader="javafml"
loaderVersion="[44,)"
license="MIT"
[[mods]]
modId="gpumemleakfix"
version="1.8"
displayName="GPU Memory Leak Fix"
authors="bre2el"
[[dependencies.gpumemleakfix]]
modId="forge"
mandatory=true
versionRange="[44,)"
ordering="NONE"
side="BOTH"
`,
			wantName: "GPU Memory Leak Fix", wantVersion: "1.8", wantID: "gpumemleakfix",
		},
		{
			name: "старий формат [mods] без displayName",
			toml: `modLoader="javafml"
loaderVersion="[31,)"
license="MIT"
[mods]
modId="smoothchunk"
version="4.1"
name="Smooth chunk"
`,
			wantName: "Smooth chunk", wantVersion: "4.1", wantID: "smoothchunk",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			zw := zip.NewWriter(&buf)
			w, err := zw.Create("META-INF/mods.toml")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := w.Write([]byte(tc.toml)); err != nil {
				t.Fatal(err)
			}
			zw.Close()

			zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
			if err != nil {
				t.Fatal(err)
			}
			var f *zip.File
			for _, zf := range zr.File {
				if zf.Name == "META-INF/mods.toml" {
					f = zf
					break
				}
			}
			if f == nil {
				t.Fatal("mods.toml not in zip")
			}
			meta := parseModsToml(f, "fake.jar")
			if meta == nil {
				t.Fatal("parseModsToml returned nil")
			}
			if !strings.EqualFold(meta.Name, tc.wantName) {
				t.Errorf("Name = %q, want %q", meta.Name, tc.wantName)
			}
			if meta.Version != tc.wantVersion {
				t.Errorf("Version = %q, want %q", meta.Version, tc.wantVersion)
			}
			if meta.ID != tc.wantID {
				t.Errorf("ID = %q, want %q", meta.ID, tc.wantID)
			}
		})
	}
}
