package modgraph

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"
)

func writeJar(t *testing.T, dir, name string, files map[string]string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for n, c := range files {
		w, _ := zw.Create(n)
		w.Write([]byte(c))
	}
	zw.Close()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func fabricJSON(v map[string]any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestFabricRequiredOptionalAndMissing(t *testing.T) {
	dir := t.TempDir()
	writeJar(t, dir, "extra.jar", map[string]string{"fabric.mod.json": fabricJSON(map[string]any{
		"id": "sodium-extra", "version": "1.0",
		"depends":  map[string]any{"sodium": ">=0.5", "fabricloader": "*", "minecraft": "1.20.1", "java": ">=17"},
		"suggests": map[string]any{"modmenu": "*"},
	})})
	writeJar(t, dir, "sodium.jar", map[string]string{"fabric.mod.json": fabricJSON(map[string]any{"id": "sodium"})})
	writeJar(t, dir, "alone.jar", map[string]string{"fabric.mod.json": fabricJSON(map[string]any{
		"id": "alone", "depends": map[string]any{"geckolib": "*"},
	})})
	g, err := BuildGraph(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Nodes) != 3 {
		t.Fatalf("nodes=%d", len(g.Nodes))
	}
	var edges []string
	for _, e := range g.Edges {
		edges = append(edges, e.From+">"+e.To)
	}
	sort.Strings(edges)
	if !reflect.DeepEqual(edges, []string{"sodium-extra>sodium"}) {
		t.Fatalf("edges=%v", edges)
	}
	got := map[string]Missing{}
	for _, m := range g.Missing {
		got[m.ID] = m
	}
	if m, ok := got["geckolib"]; !ok || m.Optional || m.RequiredBy != "alone" {
		t.Fatalf("geckolib missing wrong: %+v", got)
	}
	if m, ok := got["modmenu"]; !ok || !m.Optional {
		t.Fatalf("modmenu should be optional-missing: %+v", got)
	}
	if _, ok := got["minecraft"]; ok {
		t.Fatal("minecraft is noise")
	}
	if _, ok := got["fabricloader"]; ok || len(got) != 2 {
		t.Fatalf("noise leaked: %+v", got)
	}
}

func TestProvidesAndJarInJar(t *testing.T) {
	dir := t.TempDir()
	// fabric-api «закриває» fabric-networking-api-v1 через provides
	writeJar(t, dir, "fabric-api.jar", map[string]string{"fabric.mod.json": fabricJSON(map[string]any{
		"id": "fabric-api", "provides": []string{"fabric-networking-api-v1"},
	})})
	// вкладений jar з cloth-config
	var inner bytes.Buffer
	zw := zip.NewWriter(&inner)
	w, _ := zw.Create("fabric.mod.json")
	w.Write([]byte(fabricJSON(map[string]any{"id": "cloth-config"})))
	zw.Close()
	writeJar(t, dir, "host.jar", map[string]string{
		"fabric.mod.json": fabricJSON(map[string]any{
			"id": "host", "depends": map[string]any{"fabric-networking-api-v1": "*", "cloth-config": "*", "fabric-lifecycle-events-v1": "*"},
		}),
		"META-INF/jars/cloth.jar": inner.String(),
	})
	g, _ := BuildGraph(dir)
	if len(g.Missing) != 0 {
		t.Fatalf("expected nothing missing, got %+v", g.Missing)
	}
}

func TestDisabledDependencyIsReported(t *testing.T) {
	dir := t.TempDir()
	writeJar(t, dir, "a.jar", map[string]string{"fabric.mod.json": fabricJSON(map[string]any{"id": "a", "depends": map[string]any{"b": "*"}})})
	writeJar(t, dir, "b.jar.disabled", map[string]string{"fabric.mod.json": fabricJSON(map[string]any{"id": "b"})})
	g, _ := BuildGraph(dir)
	if len(g.Missing) != 1 || g.Missing[0].DisabledFile != "b.jar.disabled" {
		t.Fatalf("missing=%+v", g.Missing)
	}
	for _, n := range g.Nodes {
		if n.ID == "b" && n.Enabled {
			t.Fatal("b must be disabled")
		}
	}
}

func TestForgeTomlAndNeoforge(t *testing.T) {
	dir := t.TempDir()
	toml := `modLoader="javafml"
loaderVersion="[47,)"
[[mods]]
modId="mowziesmobs"
version="1.6"
displayName="Mowzie's Mobs"
[[dependencies.mowziesmobs]]
    modId="forge"
    mandatory=true
    versionRange="[47,)"
[[dependencies.mowziesmobs]]
    modId="geckolib"
    mandatory=true
    versionRange="[4.2,)"
[[dependencies.mowziesmobs]]
    modId="jei"
    mandatory=false
`
	writeJar(t, dir, "mowzie.jar", map[string]string{"META-INF/mods.toml": toml})
	neo := `[[mods]]
modId="neomod"
[[dependencies.neomod]]
modId="neoforge"
type="required"
[[dependencies.neomod]]
modId="architectury"
type="optional"
`
	writeJar(t, dir, "neo.jar", map[string]string{"META-INF/neoforge.mods.toml": neo})
	mods, _ := Scan(dir)
	byID := map[string]ModFile{}
	for _, m := range mods {
		byID[m.ModID] = m
	}
	mm := byID["mowziesmobs"]
	if mm.Name != "Mowzie's Mobs" || mm.Version != "1.6" || mm.Loader != "forge" {
		t.Fatalf("manifest=%+v", mm.Manifest)
	}
	var req, opt []string
	for _, d := range mm.Dependencies {
		if d.Optional {
			opt = append(opt, d.ID)
		} else {
			req = append(req, d.ID)
		}
	}
	if !reflect.DeepEqual(req, []string{"forge", "geckolib"}) || !reflect.DeepEqual(opt, []string{"jei"}) {
		t.Fatalf("req=%v opt=%v", req, opt)
	}
	if byID["neomod"].Loader != "neoforge" {
		t.Fatalf("neo loader=%q", byID["neomod"].Loader)
	}
	g := FromMods(mods)
	ids := map[string]bool{}
	for _, m := range g.Missing {
		ids[m.ID] = true
	}
	if !ids["geckolib"] || !ids["jei"] || !ids["architectury"] || ids["forge"] || ids["neoforge"] {
		t.Fatalf("missing=%+v", g.Missing)
	}
}

func TestQuiltAndMcmod(t *testing.T) {
	dir := t.TempDir()
	writeJar(t, dir, "q.jar", map[string]string{"quilt.mod.json": `{"quilt_loader":{"id":"qmod","version":"2","metadata":{"name":"Q"},"depends":["qsl",{"id":"extra","versions":">=1","optional":true}]}}`})
	writeJar(t, dir, "old.jar", map[string]string{"mcmod.info": `[{"modid":"oldmod","name":"Old","useDependencies":["after:jei","coremod"]}]`})
	mods, _ := Scan(dir)
	if len(mods) != 2 {
		t.Fatal(len(mods))
	}
	q := mods[1]
	if q.ModID != "qmod" || len(q.Dependencies) != 2 || !q.Dependencies[1].Optional {
		t.Fatalf("%+v", q.Manifest)
	}
	o := mods[0]
	if o.ModID != "oldmod" || !o.Dependencies[0].Optional || o.Dependencies[1].Optional {
		t.Fatalf("%+v", o.Manifest)
	}
}

func TestRemovalImpactAndCycles(t *testing.T) {
	dir := t.TempDir()
	mk := func(id string, deps map[string]any, sug map[string]any) {
		writeJar(t, dir, id+".jar", map[string]string{"fabric.mod.json": fabricJSON(map[string]any{"id": id, "depends": deps, "suggests": sug})})
	}
	mk("core", nil, nil)
	mk("mid", map[string]any{"core": "*"}, nil)
	mk("top", map[string]any{"mid": "*"}, nil)
	mk("fan", nil, map[string]any{"core": "*"})
	g, _ := BuildGraph(dir)
	imp := RemovalImpact(g, "core")
	if !reflect.DeepEqual(imp.Breaks, []string{"mid", "top"}) || !reflect.DeepEqual(imp.Degrades, []string{"fan"}) {
		t.Fatalf("impact=%+v", imp)
	}
	for _, n := range g.Nodes {
		if n.ID == "core" && n.Dependents != 1 {
			t.Fatalf("core dependents=%d", n.Dependents)
		}
	}
	dir2 := t.TempDir()
	writeJar(t, dir2, "x.jar", map[string]string{"fabric.mod.json": fabricJSON(map[string]any{"id": "x", "depends": map[string]any{"y": "*"}})})
	writeJar(t, dir2, "y.jar", map[string]string{"fabric.mod.json": fabricJSON(map[string]any{"id": "y", "depends": map[string]any{"x": "*"}})})
	g2, _ := BuildGraph(dir2)
	if len(g2.Cycles) != 1 {
		t.Fatalf("cycles=%v", g2.Cycles)
	}
}

func TestCorruptedAndNonModsAreHandled(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "broken.jar"), []byte("not a zip"), 0o644)
	os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("x"), 0o644)
	os.Mkdir(filepath.Join(dir, "sub.jar"), 0o755)
	mods, err := Scan(dir)
	if err != nil || len(mods) != 1 || !mods[0].Corrupted {
		t.Fatalf("mods=%+v err=%v", mods, err)
	}
	if _, err := Scan(filepath.Join(dir, "nope")); err != nil {
		t.Fatalf("missing dir must be empty, not error: %v", err)
	}
	g := FromMods(mods)
	if len(g.Nodes) != 1 || !g.Nodes[0].Corrupted {
		t.Fatalf("%+v", g.Nodes)
	}
}

func TestMTimeAndPackages(t *testing.T) {
	dir := t.TempDir()
	p := writeJar(t, dir, "m.jar", map[string]string{
		"fabric.mod.json":              fabricJSON(map[string]any{"id": "m"}),
		"com/example/mymod/Main.class": "x",
		"m.mixins.json":                "{}",
	})
	old := time.Now().Add(-time.Hour)
	os.Chtimes(p, old, old)
	mods, _ := Scan(dir)
	if len(mods) != 1 || mods[0].MTime != old.Unix() {
		t.Fatalf("%+v", mods)
	}
	if !reflect.DeepEqual(mods[0].Packages, []string{"com/example/mymod"}) || !reflect.DeepEqual(mods[0].MixinConfigs, []string{"m.mixins.json"}) {
		t.Fatalf("%+v", mods[0].Manifest)
	}
}

func TestSinytraConnectorWrapper(t *testing.T) {
	dir := t.TempDir()
	// Connector: зовнішній jar без метаданих, сам мод — вкладений jar з
	// MANIFEST.MF → Embedded-Dependencies-Mod.
	var inner bytes.Buffer
	zw := zip.NewWriter(&inner)
	w, _ := zw.Create("META-INF/mods.toml")
	w.Write([]byte("modLoader=\"javafml\"\n[[mods]]\nmodId=\"connectormod\"\nversion=\"1.0\"\n"))
	zw.Close()
	writeJar(t, dir, "Connector-1.0.jar", map[string]string{
		"META-INF/MANIFEST.MF":                  "Manifest-Version: 1.0\r\nEmbedded-Dependencies-Mod: META-INF/jarjar/Connector-1.0-\r\n mod.jar\r\n",
		"META-INF/jarjar/Connector-1.0-mod.jar": inner.String(),
	})
	writeJar(t, dir, "axiom.jar", map[string]string{"fabric.mod.json": fabricJSON(map[string]any{"id": "axiom"})})
	writeJar(t, dir, "create.jar", map[string]string{"META-INF/mods.toml": "[[mods]]\nmodId=\"create\"\n"})
	g, _ := BuildGraph(dir)
	if !g.Connector {
		t.Fatal("connector not detected")
	}
	via := map[string]bool{}
	for _, n := range g.Nodes {
		via[n.ID] = n.ViaConnector
	}
	if !via["axiom"] || via["create"] || via["connectormod"] {
		t.Fatalf("unexpected viaConnector flags: %+v", via)
	}
}
