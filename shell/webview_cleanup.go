package main

import (
	"errors"
	"fmt"
	"os"
)

// cleanupWebViewCaches runs before WebView2 opens the profile. Only disposable
// caches are removed; cookies, local storage and installed browser components
// must survive. OpenRoot confines traversal even if a directory is a junction.
// Locked files are reported and retried on the next launch.
func cleanupWebViewCaches(profile string) error {
	root, err := os.OpenRoot(profile)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer root.Close()
	var failures []error
	for _, path := range []string{
		"EBWebView/component_crx_cache",
		"EBWebView/extensions_crx_cache",
		"EBWebView/GPUPersistentCache",
		"EBWebView/GrShaderCache",
		"EBWebView/ShaderCache",
		"EBWebView/Default/Cache",
		"EBWebView/Default/Code Cache",
		"EBWebView/Default/GPUCache",
		"EBWebView/Default/DawnGraphiteCache",
		"EBWebView/Default/DawnWebGPUCache",
	} {
		if err := root.RemoveAll(path); err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", path, err))
		}
	}
	return errors.Join(failures...)
}
