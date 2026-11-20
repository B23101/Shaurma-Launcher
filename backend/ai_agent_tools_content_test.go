package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAIDeleteContent(t *testing.T) {
	a, _, _ := contentTestApp(t, t.TempDir())
	for _, dir := range []string{"mods", "resourcepacks", "shaderpacks", "datapacks"} {
		t.Run(dir, func(t *testing.T) {
			name := "unwanted.zip"
			tool := "delete_content"
			if dir == "mods" {
				name, tool = "unwanted.jar", "delete_mod"
			}
			root := a.instanceContentDir("src-pack", dir)
			if err := os.MkdirAll(root, 0o755); err != nil {
				t.Fatal(err)
			}
			for _, n := range []string{name, name + ".disabled", "keep.zip"} {
				if err := os.WriteFile(filepath.Join(root, n), []byte("content"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			args := argMap{"pack_id": "src-pack", "content_dir": dir, "file_name": name + ".disabled"}
			if _, err := aiAgentTools[tool].run(a, args); err != nil {
				t.Fatal(err)
			}
			for _, n := range []string{name, name + ".disabled"} {
				if _, err := os.Stat(filepath.Join(root, n)); !os.IsNotExist(err) {
					t.Fatalf("%s remains: %v", n, err)
				}
			}
			if _, err := os.Stat(filepath.Join(root, "keep.zip")); err != nil {
				t.Fatalf("unrelated content: %v", err)
			}
			if _, err := aiAgentTools[tool].run(a, args); err == nil {
				t.Fatal("missing content reported as deleted")
			}
		})
	}
	root := a.instanceContentDir("src-pack", "resourcepacks")
	if err := os.MkdirAll(filepath.Join(root, "unpacked"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "unpacked", "pack.mcmeta"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := a.aiDeleteContent(argMap{"pack_id": "src-pack", "content_dir": "resourcepacks", "file_name": "unpacked"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "unpacked")); !os.IsNotExist(err) {
		t.Fatalf("unpacked content remains: %v", err)
	}
}

func TestAIDeleteContentRejectsUnsafeTargets(t *testing.T) {
	a, _, _ := contentTestApp(t, t.TempDir())
	for _, name := range []string{"", ".", "..", ".disabled", "...disabled", "../keep.zip", `..\keep.zip`, "C:keep.zip", "keep.zip ", "keep.zip.", "options.txt"} {
		if _, err := a.aiDeleteContent(argMap{"pack_id": "src-pack", "content_dir": "resourcepacks", "file_name": name}); err == nil {
			t.Errorf("accepted unsafe target %q", name)
		}
	}
	for _, dir := range []string{"config", "saves", "../resourcepacks"} {
		if _, err := a.aiDeleteContent(argMap{"pack_id": "src-pack", "content_dir": dir, "file_name": "keep.zip"}); err == nil {
			t.Errorf("accepted content_dir %q", dir)
		}
	}
	for _, tool := range []string{"delete_mod", "delete_content"} {
		if !aiAgentTools[tool].write || aiToolCat[tool] != "mods" || !aiToolProtected(tool) {
			t.Errorf("%s bypasses content permissions or original protection", tool)
		}
	}
}
