package mods

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestParseJarEmbeddedMod — мод-обгортка (Sinytra Connector) без власних
// метаданих: мод читається з вкладеного jar, на який вказує
// Embedded-Dependencies-Mod у MANIFEST.MF (з перенесенням рядка).
func TestParseJarEmbeddedMod(t *testing.T) {
	var inner bytes.Buffer
	zw := zip.NewWriter(&inner)
	w, _ := zw.Create("META-INF/mods.toml")
	w.Write([]byte("modLoader=\"javafml\"\n[[mods]]\nmodId=\"connectormod\"\nversion=\"1.0.0\"\ndisplayName=\"Connector\"\n"))
	zw.Close()

	var outer bytes.Buffer
	zw = zip.NewWriter(&outer)
	w, _ = zw.Create("META-INF/MANIFEST.MF")
	w.Write([]byte("Manifest-Version: 1.0\r\nEmbedded-Dependencies-Mod: META-INF/jarjar/Connector-1.0.0-\r\n mod.jar\r\n"))
	w, _ = zw.Create("META-INF/jarjar/Connector-1.0.0-mod.jar")
	w.Write(inner.Bytes())
	zw.Close()

	path := filepath.Join(t.TempDir(), "Connector-1.0.0.jar")
	if err := os.WriteFile(path, outer.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	m := parseJar(path)
	if m.ID != "connectormod" || m.LoaderType != "forge" || len(m.Loaders) != 1 || m.Loaders[0] != "forge" {
		t.Fatalf("unexpected meta: id=%q loader=%q loaders=%v", m.ID, m.LoaderType, m.Loaders)
	}
}
