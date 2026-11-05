package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"shaurma-launcher-backend/internal/model"
	"shaurma-launcher-backend/internal/wardrobe"
	"testing"
)

func TestClearCachePreservesWardrobe(t *testing.T) {
	cfg := newTestConfig(t, t.TempDir())
	cache := cfg.CacheDir()
	skins := filepath.Join(cfg.Dir(), "skins")
	src := filepath.Join(cache, "skins", "saved-preset.png")
	mkFile(t, src, 64)
	mkFile(t, filepath.Join(cache, "discard.tmp"), 1)
	legacy := filepath.Join(cache, "skins", "unindexed.png")
	mkFile(t, legacy, 32)
	os.MkdirAll(skins, 0755)
	raw, _ := json.Marshal(map[string]any{"version": 2, "byAccount": map[string][]model.SkinPreset{"player": {{ID: "keep", Name: "My skin", SkinPath: src}}}})
	if err := os.WriteFile(filepath.Join(skins, "presets.json"), raw, 0644); err != nil {
		t.Fatal(err)
	}
	svc := wardrobe.NewService(skins, nil)
	a := &App{cfg: cfg, wardrobe: svc}
	if err := a.ClearCache(); err != nil {
		t.Fatal(err)
	}
	preset := svc.ListPresets("player")[0]
	if preset.ID != "keep" {
		t.Fatal(preset)
	}
	if _, err := os.Stat(preset.SkinPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Fatal("legacy texture removed", err)
	}
	if _, err := os.Stat(filepath.Join(cache, "discard.tmp")); !os.IsNotExist(err) {
		t.Fatal("cache not cleared", err)
	}
}
