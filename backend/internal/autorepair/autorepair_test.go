package autorepair

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func makeMod(t *testing.T, mods, name string, files map[string]string) string {
	t.Helper()
	os.MkdirAll(mods, 0o755)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for n, c := range files {
		w, _ := zw.Create(n)
		w.Write([]byte(c))
	}
	zw.Close()
	p := filepath.Join(mods, name)
	os.WriteFile(p, buf.Bytes(), 0o644)
	return p
}

func fab(v map[string]any) map[string]string {
	b, _ := json.Marshal(v)
	return map[string]string{"fabric.mod.json": string(b)}
}

type env struct {
	t    *testing.T
	game string
	mods string
}

func newEnv(t *testing.T) env {
	g := t.TempDir()
	return env{t, g, filepath.Join(g, "mods")}
}

func (e env) run(lines []string, mut func(*Input)) Report {
	empty := ""
	code := 1
	in := Input{Lines: lines, ExitCode: &code, GameDir: e.game, Loader: "fabric", CrashReport: &empty,
		EffectiveRAMMB: 2048, TotalRAMMB: 16384, RecommendedRAMMB: 4096}
	if mut != nil {
		mut(&in)
	}
	return Diagnose(in)
}

func kinds(r Report) []string {
	out := []string{}
	for _, i := range r.Issues {
		out = append(out, i.ID)
	}
	return out
}

func fixOf(t *testing.T, r Report, kind, action string) Fix {
	t.Helper()
	for _, i := range r.Issues {
		if i.ID == kind {
			for _, f := range i.Fixes {
				if f.Action == action {
					return f
				}
			}
		}
	}
	t.Fatalf("fix %s/%s not found in %+v", kind, action, kinds(r))
	return Fix{}
}

func TestFabricMissingDependency(t *testing.T) {
	e := newEnv(t)
	makeMod(t, e.mods, "sodium-extra.jar", fab(map[string]any{"id": "sodium-extra", "depends": map[string]any{"sodium": "*"}}))
	r := e.run([]string{
		"net.fabricmc.loader.impl.FormattedException: Mod resolution encountered an incompatible mod set!",
		" - Mod 'Sodium Extra' (sodium-extra) 0.5.1 requires version 0.5.0 or later of mod 'Sodium' (sodium), which is missing!",
	}, nil)
	if !reflect.DeepEqual(kinds(r), []string{"missing_dependencies"}) {
		t.Fatalf("kinds=%v", kinds(r))
	}
	if ids := stringList(fixOf(t, r, "missing_dependencies", "install_dependencies").Params["ids"]); !reflect.DeepEqual(ids, []string{"sodium"}) {
		t.Fatalf("ids=%v", ids)
	}
	if f := stringList(fixOf(t, r, "missing_dependencies", "disable_mods").Params["files"]); !reflect.DeepEqual(f, []string{"sodium-extra.jar"}) {
		t.Fatalf("files=%v", f)
	}
	if !r.Issues[0].Fixes[0].Recommended || r.Issues[0].Fixes[1].Recommended {
		t.Fatal("only first fix is recommended")
	}
}

func TestForgeMissingAndWrongMinecraft(t *testing.T) {
	e := newEnv(t)
	makeMod(t, e.mods, "mowzies.jar", map[string]string{"META-INF/mods.toml": "[[mods]]\nmodId=\"mowziesmobs\"\n"})
	makeMod(t, e.mods, "oldmod.jar", map[string]string{"META-INF/mods.toml": "[[mods]]\nmodId=\"oldmod\"\n"})
	r := e.run([]string{
		"\tMod ID: 'geckolib', Requested by: 'mowziesmobs', Expected range: '[4.2,)', Actual version: '[MISSING]'",
		"\tMod ID: 'minecraft', Requested by: 'oldmod', Expected range: '[1.19,1.20)', Actual version: '1.20.1'",
	}, func(in *Input) { in.Loader = "forge" })
	if ids := stringList(fixOf(t, r, "missing_dependencies", "install_dependencies").Params["ids"]); !reflect.DeepEqual(ids, []string{"geckolib"}) {
		t.Fatalf("ids=%v", ids)
	}
	if f := stringList(fixOf(t, r, "incompatible_mods", "disable_mods").Params["files"]); !reflect.DeepEqual(f, []string{"oldmod.jar"}) {
		t.Fatalf("files=%v", f)
	}
}

func TestStaticWrongLoaderDuplicatesCorrupted(t *testing.T) {
	e := newEnv(t)
	makeMod(t, e.mods, "fabric-ok.jar", fab(map[string]any{"id": "okmod"}))
	makeMod(t, e.mods, "forge-only.jar", map[string]string{"META-INF/mods.toml": "[[mods]]\nmodId=\"forgeonly\"\n"})
	old := makeMod(t, e.mods, "lithium-0.1.jar", fab(map[string]any{"id": "lithium"}))
	past := time.Now().Add(-time.Hour)
	os.Chtimes(old, past, past)
	makeMod(t, e.mods, "lithium-0.2.jar", fab(map[string]any{"id": "lithium"}))
	os.WriteFile(filepath.Join(e.mods, "broken.jar"), []byte("not a zip"), 0o644)
	r := e.run([]string{"Exception in thread main"}, nil)
	check := func(kind string, want []string) {
		if got := stringList(fixOf(t, r, kind, "disable_mods").Params["files"]); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: %v != %v", kind, got, want)
		}
	}
	check("wrong_loader_mods", []string{"forge-only.jar"})
	check("duplicate_mods", []string{"lithium-0.1.jar"})
	check("corrupted_mods", []string{"broken.jar"})
}

func TestConnectorAllowsFabricOnForge(t *testing.T) {
	e := newEnv(t)
	makeMod(t, e.mods, "connector.jar", map[string]string{"META-INF/mods.toml": "[[mods]]\nmodId=\"connector\"\n"})
	makeMod(t, e.mods, "fabric-only.jar", fab(map[string]any{"id": "fabriconly"}))
	r := e.run([]string{"crash"}, func(in *Input) { in.Loader = "forge" })
	for _, k := range kinds(r) {
		if k == "wrong_loader_mods" {
			t.Fatal("connector must allow fabric mods")
		}
	}
}

func TestMixinFailureMapsConfigToJar(t *testing.T) {
	e := newEnv(t)
	f := fab(map[string]any{"id": "badmod"})
	f["badmod.mixins.json"] = "{}"
	makeMod(t, e.mods, "badmod.jar", f)
	r := e.run([]string{
		"org.spongepowered.asm.mixin.transformer.throwables.MixinTransformerError: An unexpected critical error was encountered",
		"Caused by: org.spongepowered.asm.mixin.injection.throwables.InvalidInjectionException: Critical injection failure [PREINJECT Applicator Phase -> badmod.mixins.json:FooMixin]",
	}, nil)
	if f := stringList(fixOf(t, r, "mixin_failure", "disable_mods").Params["files"]); !reflect.DeepEqual(f, []string{"badmod.jar"}) {
		t.Fatalf("%v", f)
	}
}

func TestMemory(t *testing.T) {
	e := newEnv(t)
	r := e.run([]string{"java.lang.OutOfMemoryError: Java heap space"}, nil)
	if got := intParam(fixOf(t, r, "out_of_memory", "set_memory").Params["memoryMb"]); got != 4096 {
		t.Fatalf("oom target=%d", got)
	}
	r = e.run([]string{"Error occurred during initialization of VM", "Could not reserve enough space for 12582912KB object heap"},
		func(in *Input) { in.EffectiveRAMMB = 12288; in.TotalRAMMB = 8192 })
	if !reflect.DeepEqual(kinds(r), []string{"memory_too_high"}) {
		t.Fatalf("kinds=%v", kinds(r))
	}
	if got := intParam(fixOf(t, r, "memory_too_high", "set_memory").Params["memoryMb"]); got != 4096 {
		t.Fatalf("heap target=%d", got)
	}
}

func TestJavaVersion(t *testing.T) {
	e := newEnv(t)
	r := e.run([]string{"java.lang.UnsupportedClassVersionError: foo has been compiled by a more recent version of the Java Runtime (class file version 65.0), this version only recognizes class file versions up to 61.0"},
		func(in *Input) { in.JavaPath = "/opt/jdk17/bin/java" })
	if r.Issues[0].ID != "java_version" || !strings.Contains(r.Issues[0].Detail, "21") {
		t.Fatalf("%+v", r.Issues)
	}
	if got := intParam(fixOf(t, r, "java_version", "install_java").Params["major"]); got != 21 {
		t.Fatalf("major=%d", got)
	}
	fixOf(t, r, "java_version", "set_java")
	r = e.run([]string{" - Mod 'Foo' (foo) 1.0 requires version 21 or later of 'OpenJDK 64-Bit Server VM' (java), but only the wrong version is present: 17!"}, nil)
	if !strings.Contains(r.Issues[0].Detail, "21") {
		t.Fatalf("%+v", r.Issues[0])
	}
}

func TestLoaderOutdatedAndBroken(t *testing.T) {
	e := newEnv(t)
	makeMod(t, e.mods, "create.jar", map[string]string{"META-INF/neoforge.mods.toml": "[[mods]]\nmodId=\"create\"\n"})
	r := e.run([]string{"Mod create requires neoforge 21.1.77 or above", "Currently, neoforge is 21.1.50"},
		func(in *Input) { in.Loader = "neoforge"; in.CanUpdateLoader = true; in.LoaderVersion = "21.1.50" })
	f := fixOf(t, r, "loader_outdated", "update_loader")
	if f.Params["minimum"] != "21.1.77" {
		t.Fatalf("%+v", f.Params)
	}
	r = e.run([]string{"Error: Could not find or load main class net.fabricmc.loader.impl.launch.knot.KnotClient"}, func(in *Input) { in.CanUpdateLoader = true })
	if kinds(r)[0] != "loader_broken" {
		t.Fatalf("kinds=%v", kinds(r))
	}
	fixOf(t, r, "loader_broken", "repair_version")
	// без права міняти лоадер — лише repair
	r = e.run([]string{"Could not find or load main class net.fabricmc.loader.impl.launch.knot.KnotClient"}, nil)
	for _, fx := range r.Issues[0].Fixes {
		if fx.Action == "update_loader" {
			t.Fatal("update_loader must be hidden")
		}
	}
}

func TestBrokenConfigAndBackup(t *testing.T) {
	e := newEnv(t)
	os.MkdirAll(filepath.Join(e.game, "config"), 0o755)
	os.WriteFile(filepath.Join(e.game, "config", "mymod.toml"), []byte("x"), 0o644)
	r := e.run([]string{"Failed loading config file mymod.toml", "com.electronwill.nightconfig.core.io.ParsingException: bad"}, func(in *Input) { in.Loader = "forge" })
	f := fixOf(t, r, "broken_config", "backup_configs")
	files := stringList(f.Params["files"])
	if !reflect.DeepEqual(files, []string{"config/mymod.toml"}) {
		t.Fatalf("%v", files)
	}
	backup := t.TempDir()
	moved, err := BackupConfigs(e.game, backup, append(files, "config/../secret", "mods/x.jar"))
	if err != nil || !reflect.DeepEqual(moved, []string{"config/mymod.toml"}) {
		t.Fatalf("moved=%v err=%v", moved, err)
	}
	if _, err := os.Stat(filepath.Join(e.game, "config", "mymod.toml")); !os.IsNotExist(err) {
		t.Fatal("config must be moved")
	}
}

func TestAccountIssues(t *testing.T) {
	e := newEnv(t)
	accounts := []Account{{ID: "off", Name: "Steve", Active: true}, {ID: "ms1", Name: "Alex", Licensed: true}}
	r := e.run([]string{"Failed to verify username!"}, func(in *Input) { in.Accounts = accounts; in.CurrentAccountID = "off" })
	iss := r.Issues[0]
	if iss.ID != "account" || iss.Fixes[0].Action != "set_account" || iss.Fixes[0].Params["accountId"] != "ms1" {
		t.Fatalf("%+v", iss)
	}
	r = e.run([]string{"Caused by: AADSTS70000 invalid_grant"}, func(in *Input) { in.Accounts = accounts; in.CurrentAccountID = "ms1" })
	if r.Issues[0].Fixes[0].Action != "login_microsoft" {
		t.Fatalf("%+v", r.Issues[0].Fixes)
	}
}

func TestFallbacksAndMuting(t *testing.T) {
	e := newEnv(t)
	m := makeMod(t, e.mods, "newmod.jar", fab(map[string]any{"id": "newmod"}))
	_ = m
	r := e.run([]string{"something odd happened"}, func(in *Input) { in.LastSuccess = time.Now().Add(-time.Hour) })
	if !reflect.DeepEqual(kinds(r), []string{"recent_mods"}) {
		t.Fatalf("kinds=%v", kinds(r))
	}
	r = e.run([]string{"something odd happened"}, nil)
	if !reflect.DeepEqual(kinds(r), []string{"unknown_crash"}) {
		t.Fatalf("kinds=%v", kinds(r))
	}
	r = e.run([]string{"something odd happened"}, func(in *Input) { in.Muted = []string{"unknown_crash"} })
	if len(r.Issues) != 0 || !reflect.DeepEqual(r.MutedIssues, []string{"unknown_crash"}) {
		t.Fatalf("%+v", r)
	}
	// успішний запуск без помилок — тиша
	zero := 0
	r = Diagnose(Input{Lines: []string{"ok"}, ExitCode: &zero, GameDir: e.game, Loader: "fabric", CrashReport: new(string)})
	if len(r.Issues) != 0 {
		t.Fatalf("%+v", r.Issues)
	}
}

// Крах 26.x, коли графічного бекенду немає: гра не знаходить адаптерів,
// LWJGL абортить JVM (код 1, без crash-report). Лаунчер має показати
// «графіка» з порадами про драйвер, а не «винна Java» чи «невідома причина».
func TestGraphicsNoAdapter(t *testing.T) {
	e := newEnv(t)
	lines := []string{
		"[16:54:02] [main/WARN]: Could not find any graphics adapters! Probably the device is not on a bus we can probe, or there are no devices supporting 3D acceleration.",
		"[16:54:17] [Render thread/WARN]: Detected unexpected shutdown during last game startup: forcing preferred graphics API to OpenGL",
		"FATAL ERROR in native method: [LWJGL] Thread[#3,Render thread,5,main]: No context is current or a function that is not available in the current context was called. The JVM will abort execution.",
		`An error report file with more information is saved as: C:\Users\u\AppData\Roaming\.shaurm\installations\Fabric 26.3\.minecraft\hs_err_pid4242.log`,
	}
	r := e.run(lines, func(in *Input) { in.MinecraftVersion = "26.3"; in.DedicatedGPU = true })
	if got := kinds(r); !reflect.DeepEqual(got, []string{"graphics"}) {
		t.Fatalf("kinds=%v", got)
	}
	iss := r.Issues[0]
	if !strings.Contains(iss.Detail, "адаптера") || !strings.Contains(iss.Manual, "драйвер") {
		t.Fatalf("detail/manual=%+v", iss)
	}
	// Перемикач дискретної GPU увімкнено — перша ж дія має його вимкнути.
	if len(iss.Fixes) == 0 || iss.Fixes[0].Action != "set_dedicated_gpu" || iss.Fixes[0].Params["enabled"] != false {
		t.Fatalf("fixes=%+v", iss.Fixes)
	}
	// Старі GLFW-помилки й далі розпізнаються (інша, загальна підказка).
	r = e.run([]string{"[main/ERROR]: GLFW error 65542: WGL: The driver does not appear to support OpenGL"}, nil)
	if got := kinds(r); !reflect.DeepEqual(got, []string{"graphics"}) {
		t.Fatalf("glfw kinds=%v", got)
	}
	if !strings.Contains(r.Issues[0].Manual, "драйвер") || strings.Contains(r.Issues[0].Manual, "Що робити") {
		t.Fatalf("glfw manual=%q", r.Issues[0].Manual)
	}
}

func TestCrashReportSuspects(t *testing.T) {
	e := newEnv(t)
	makeMod(t, e.mods, "culprit-1.0.jar", fab(map[string]any{"id": "culprit"}))
	report := "---- Minecraft Crash Report ----\nSuspected Mods: \n\tCulprit (culprit), Version: 1.0\n\n"
	r := e.run([]string{"Game crashed"}, func(in *Input) { in.CrashReport = &report })
	if f := stringList(fixOf(t, r, "incompatible_mods", "disable_mods").Params["files"]); !reflect.DeepEqual(f, []string{"culprit-1.0.jar"}) {
		t.Fatalf("%v", f)
	}
}

type fakeExec struct {
	calls []string
	total int
}

func (f *fakeExec) rec(s string) { f.calls = append(f.calls, s) }
func (f *fakeExec) DisableMods(files []string) ([]string, []string) {
	f.rec("disable:" + strings.Join(files, ","))
	return files, nil
}
func (f *fakeExec) InstallDependencies(ids []string) ([]string, []string) {
	f.rec("install")
	return nil, ids
}
func (f *fakeExec) SetMemory(mb int) error   { f.rec("mem"); return nil }
func (f *fakeExec) SetJava(p string) error   { f.rec("java"); return nil }
func (f *fakeExec) InstallJava(m int) error  { f.rec("installjava"); return nil }
func (f *fakeExec) ResetJVMArguments() error { f.rec("jvm"); return nil }
func (f *fakeExec) RepairVersion() error     { f.rec("repair"); return nil }
func (f *fakeExec) UpdateLoader(m string) (string, error) {
	f.rec("loader")
	return "1.2", nil
}
func (f *fakeExec) BackupConfigs(files []string) ([]string, error) { f.rec("cfg"); return files, nil }
func (f *fakeExec) SetDedicatedGPU(b bool) error                   { f.rec("gpu"); return nil }
func (f *fakeExec) SetAccount(id string) (string, error)           { f.rec("acc"); return "N", nil }
func (f *fakeExec) TotalRAMMB() int                                { return f.total }

func TestApplyValidatesAndOrders(t *testing.T) {
	ex := &fakeExec{total: 8192}
	res, err := Apply(ex, []Fix{
		{ID: "a", Action: "repair_version"},
		{ID: "b", Action: "disable_mods", Params: map[string]any{"files": []any{"ok.jar", "../evil.jar", "sub/x.jar"}}},
		{ID: "c", Action: "set_memory", Params: map[string]any{"memoryMb": float64(99999)}},
		{ID: "d", Action: "set_memory", Params: map[string]any{"memoryMb": float64(4096)}},
		{ID: "e", Action: "set_java", Params: map[string]any{"javaPath": "/tmp/evil"}},
		{ID: "f", Action: "rm_rf"},
		{ID: "g", Action: "login_microsoft"},
		{ID: "h", Action: "set_account", Params: map[string]any{"accountId": "../x"}},
		{ID: "i", Action: "install_dependencies", Params: map[string]any{"ids": []any{"geckolib"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if ex.calls[len(ex.calls)-1] != "repair" {
		t.Fatalf("repair must be last: %v", ex.calls)
	}
	if ex.calls[0] != "disable:ok.jar" {
		t.Fatalf("path traversal must be filtered: %v", ex.calls)
	}
	ok := map[string]bool{}
	var login bool
	for _, r := range res {
		ok[r.ID] = r.OK
		if r.ID == "g" {
			login = r.NeedsLogin
		}
	}
	want := map[string]bool{"a": true, "b": true, "c": false, "d": true, "e": false, "f": false, "g": true, "h": false, "i": false}
	if !reflect.DeepEqual(ok, want) || !login {
		t.Fatalf("ok=%v", ok)
	}
	if _, err := Apply(ex, nil); err == nil {
		t.Fatal("empty must error")
	}
}

// ApplyProgress: інтерфейс має бачити КОЖЕН крок (done/total/label), інакше
// модалка «Авто-починка» виглядає завислою на довгих завантаженнях.
func TestApplyProgressReportsEveryStep(t *testing.T) {
	ex := &fakeExec{total: 8192}
	var steps []string
	res, err := ApplyProgress(ex, []Fix{
		{ID: "a", Action: "disable_mods", Label: "Вимкнути мод", Params: map[string]any{"files": []any{"bad.jar"}}},
		{ID: "b", Action: "set_memory", Label: "Пам'ять", Params: map[string]any{"memoryMb": float64(4096)}},
		{ID: "c", Action: "repair_version", Label: "Переставити версію"},
	}, func(done, total int, f Fix) {
		steps = append(steps, fmt.Sprintf("%d/%d:%s", done, total, f.Label))
	})
	if err != nil {
		t.Fatal(err)
	}
	// repair_version завжди останній (порядок у Apply), тому кроки саме такі.
	want := []string{"0/3:Вимкнути мод", "1/3:Пам'ять", "2/3:Переставити версію"}
	if !reflect.DeepEqual(steps, want) {
		t.Fatalf("steps=%v", steps)
	}
	if len(res) != 3 {
		t.Fatalf("results=%d", len(res))
	}
}

func TestStateAndMuted(t *testing.T) {
	dir := t.TempDir()
	if !LastSuccess(dir, "pack1").IsZero() {
		t.Fatal("unknown")
	}
	RecordLaunchResult(dir, "pack1", 1)
	if !LastSuccess(dir, "pack1").IsZero() {
		t.Fatal("failure must not record")
	}
	RecordLaunchResult(dir, "pack1", 0)
	if LastSuccess(dir, "pack1").IsZero() {
		t.Fatal("success must record")
	}
	RecordLaunchResult(dir, "../evil", 0)
	if _, err := os.Stat(filepath.Join(dir, "evil.json")); err == nil {
		t.Fatal("traversal")
	}
	m, _ := SetMuted(dir, []string{"graphics", "nonsense", "account"}, true)
	if !reflect.DeepEqual(m, []string{"account", "graphics"}) {
		t.Fatalf("%v", m)
	}
	m, _ = SetMuted(dir, []string{"account"}, false)
	if !reflect.DeepEqual(m, []string{"graphics"}) || !reflect.DeepEqual(LoadMuted(dir), []string{"graphics"}) {
		t.Fatalf("%v", m)
	}
}

func TestPickLoaderVersion(t *testing.T) {
	cases := []struct {
		in   []string
		min  string
		want string
	}{
		{[]string{"0.16.9", "0.16.10", "0.15.0"}, "", "0.16.10"},
		{[]string{"47.2.20", "47.3.0", "47.3.1-beta"}, "", "47.3.0"},
		{[]string{"0.16.9", "0.16.10"}, "0.17", ""},
		{[]string{"1.0.0-beta"}, "", "1.0.0-beta"},
	}
	for _, c := range cases {
		if got := PickLoaderVersion(c.in, c.min); got != c.want {
			t.Errorf("%v min=%q: %q != %q", c.in, c.min, got, c.want)
		}
	}
}
