package shaurmpack

import (
	"archive/zip"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func securityPack(t *testing.T, files []PackFile, entries map[string]string) string {
	t.Helper()
	name := filepath.Join(t.TempDir(), "test.shaurmpack")
	f, err := os.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	w, err := z.Create(ManifestFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(w).Encode(Manifest{Version: 1, PackID: "test", Name: "test", GameVersion: "1.20.1", Loader: "vanilla", Files: files}); err != nil {
		t.Fatal(err)
	}
	for name, data := range entries {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return name
}

func TestImportRejectsUnsafePathsBeforeWriting(t *testing.T) {
	for _, rel := range []string{"../escaped.txt", "a/../../escaped.txt", "C:/escaped.txt", "a\\..\\escaped.txt", "/escaped.txt", "mods/run.bat"} {
		t.Run(rel, func(t *testing.T) {
			for _, prefix := range []string{OverridesDir, BundledDir} {
				name := securityPack(t, nil, map[string]string{prefix + rel: "bad", OverridesDir + "ok.txt": "ok"})
				dir := t.TempDir()
				if _, err := Import(ImportOptions{ShaurmPackPath: name, TargetGameDir: dir}); err == nil {
					t.Fatal("unsafe archive accepted")
				}
				if entries, _ := os.ReadDir(dir); len(entries) != 0 {
					t.Fatal("wrote files before validation")
				}
			}
			name := securityPack(t, []PackFile{{Path: rel, Modrinth: &ModrinthSource{DownloadURL: "https://cdn.modrinth.com/file"}}}, nil)
			called := false
			_, err := Import(ImportOptions{ShaurmPackPath: name, TargetGameDir: t.TempDir(), ResolveDownload: func(string, PackFile) error { called = true; return nil }})
			if err == nil || called {
				t.Fatal("unsafe manifest reached resolver")
			}
			m, _ := ReadManifest(name)
			if ValidateManifest(m) == nil {
				t.Fatal("manifest validation accepted unsafe path")
			}
		})
	}
}

func TestImportRejectsCorruptDownload(t *testing.T) {
	h := sha1.Sum([]byte("expected"))
	pf := PackFile{Path: "mods/test.jar", Hash: hex.EncodeToString(h[:]), Size: 8, Modrinth: &ModrinthSource{DownloadURL: "https://cdn.modrinth.com/file"}}
	name := securityPack(t, []PackFile{pf}, nil)
	dir := t.TempDir()
	_, err := Import(ImportOptions{ShaurmPackPath: name, TargetGameDir: dir, ResolveDownload: func(dest string, _ PackFile) error { return os.WriteFile(dest, []byte("tampered"), 0644) }})
	if err == nil {
		t.Fatal("corrupt download accepted")
	}
	if _, err := os.Stat(filepath.Join(dir, "mods/test.jar")); !os.IsNotExist(err) {
		t.Fatal("published corrupt download")
	}
}

func TestImportRejectsExistingSymlink(t *testing.T) {
	outside := t.TempDir()
	dir := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, "mods")); err != nil {
		t.Skip("symlinks unavailable")
	}
	name := securityPack(t, nil, map[string]string{OverridesDir + "mods/escape.txt": "bad"})
	if _, err := Import(ImportOptions{ShaurmPackPath: name, TargetGameDir: dir}); err == nil {
		t.Fatal("symlink accepted")
	}
	if _, err := os.Stat(filepath.Join(outside, "escape.txt")); !os.IsNotExist(err) {
		t.Fatal("wrote outside root")
	}
}

func TestBundleRejectsTraversal(t *testing.T) {
	for _, id := range []string{"pack", "../escape"} {
		name := filepath.Join(t.TempDir(), "test.shaurmbundle")
		f, err := os.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		z := zip.NewWriter(f)
		w, err := z.Create(BundleManifestFile)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.NewEncoder(w).Encode(BundleManifest{Packs: []BundlePackInfo{{ID: id}}}); err != nil {
			t.Fatal(err)
		}
		w, err = z.Create("packs/" + id + "/overrides/../../escape.txt")
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte("bad"))
		z.Close()
		f.Close()
		dir := t.TempDir()
		if _, err := ImportBundle(BundleImportOptions{ShaurmBundlePath: name, TargetBaseDir: dir}); err == nil {
			t.Fatal("unsafe bundle accepted")
		}
		if entries, _ := os.ReadDir(dir); len(entries) != 0 {
			t.Fatal("wrote before validation")
		}
	}
}

func TestBundleImportStripsExecutableSettings(t *testing.T) {
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "options.txt"), []byte("ok"), 0644); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "bundle.shaurmbundle")
	metadata := []byte(`{"id":"old","name":"My pack","mcVersion":"1.20.1","loader":"fabric","useCustomCommands":true,"preLaunchCommandOverride":"untrusted command","wrapperCommandOverride":"untrusted wrapper","postExitCommandOverride":"untrusted command","envVarsOverride":"JAVA_TOOL_OPTIONS=untrusted","javaArgsOverride":"-javaagent:untrusted.jar","useSeparateJava":true,"javaPath":"untrusted.exe","archivePath":"outside","tempMods":{"../outside":{"expiresAt":"2000-01-01"}}}`)
	if _, err := ExportBundle(BundleExportOptions{OutPath: archive, Packs: []BundlePackExport{{PackID: "old", PackData: metadata, GameDir: source}}}); err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if _, err := ImportBundle(BundleImportOptions{ShaurmBundlePath: archive, TargetBaseDir: target, IDMap: map[string]string{"old": "new"}}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(target, "new", "instance.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"useCustomCommands", "preLaunchCommandOverride", "wrapperCommandOverride", "postExitCommandOverride", "envVarsOverride", "javaArgsOverride", "useSeparateJava", "javaPath", "archivePath", "tempMods"} {
		if _, ok := got[key]; ok {
			t.Errorf("imported dangerous setting %s", key)
		}
	}
	if string(got["name"]) != `"My pack"` || string(got["id"]) != `"new"` || string(got["loader"]) != `"fabric"` {
		t.Fatalf("lost safe metadata: %s", data)
	}
}

func TestPackArchiveRejectsOversizedHeaders(t *testing.T) {
	f := &zip.File{FileHeader: zip.FileHeader{Name: "overrides/bomb.txt", UncompressedSize64: 3 << 30}}
	if validatePackArchive([]*zip.File{f}, 8<<30) == nil {
		t.Fatal("oversized entry accepted")
	}
}
func TestBundleReservesDistinctDirectories(t *testing.T) {
	archive := securityPack(t, nil, map[string]string{
		BundleManifestFile:                     `{"packs":[{"id":"Survival"},{"id":"survival"}]}`,
		"packs/Survival/overrides/options.txt": "first",
		"packs/survival/overrides/options.txt": "second",
	})
	target := t.TempDir()
	result, err := ImportBundle(BundleImportOptions{
		ShaurmBundlePath: archive, TargetBaseDir: target,
		IDMap: map[string]string{"Survival": "same", "survival": "same"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Packs) != 2 {
		t.Fatal(result.Packs)
	}
	for i, want := range []string{"first", "second"} {
		data, err := os.ReadFile(filepath.Join(result.Packs[i].GameDir, "options.txt"))
		if err != nil || string(data) != want {
			t.Fatalf("pack %d: %q, %v", i, data, err)
		}
	}
}

func TestBundlePackCountLimit(t *testing.T) {
	data, err := json.Marshal(BundleManifest{Packs: make([]BundlePackInfo, maxBundlePacks+1)})
	if err != nil {
		t.Fatal(err)
	}
	archive := securityPack(t, nil, map[string]string{BundleManifestFile: string(data)})
	target := filepath.Join(t.TempDir(), "instances")
	if _, err := ReadBundleManifest(archive); err == nil {
		t.Fatal("oversized preview accepted")
	}
	if _, err := ImportBundle(BundleImportOptions{ShaurmBundlePath: archive, TargetBaseDir: target}); err == nil {
		t.Fatal("oversized bundle accepted")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("unexpected target: %v", err)
	}
}
