package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCleanupWebViewCachesPreservesUserData(t *testing.T) {
	profile := t.TempDir()
	remove := []string{"Default/Cache/data", "Default/Code Cache/js/data", "Default/GPUCache/data", "component_crx_cache/download"}
	keep := []string{"Default/Network/Cookies", "Default/Local Storage/leveldb/data", "Default/Preferences", "WidevineCdm/component", "Local State"}
	for _, name := range append(append([]string{}, remove...), keep...) {
		path := filepath.Join(profile, "EBWebView", name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("preserve"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := cleanupWebViewCaches(profile); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range remove {
		if _, err := os.Stat(filepath.Join(profile, "EBWebView", name)); !os.IsNotExist(err) {
			t.Errorf("cache remains: %s (%v)", name, err)
		}
	}
	for _, name := range keep {
		if data, err := os.ReadFile(filepath.Join(profile, "EBWebView", name)); err != nil || string(data) != "preserve" {
			t.Errorf("user data changed: %s (%v)", name, err)
		}
	}
}

func TestCleanupWebViewCachesMissingProfile(t *testing.T) {
	if err := cleanupWebViewCaches(filepath.Join(t.TempDir(), "missing")); err != nil {
		t.Fatal(err)
	}
}
