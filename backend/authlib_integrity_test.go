package main

import (
	"os"
	"testing"
)

func TestAuthlibRejectsTamperedCacheAndDownloads(t *testing.T) {
	a := newSkinsTestApp(t, false)
	path, err := a.authlibInjectorJar()
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body[len(body)-1] ^= 1
	if !looksLikeJar(body) || trustedAuthlibJar(body) {
		t.Fatal("a plausible JAR with a modified byte passed integrity validation")
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	previousFetch := authlibFetchFn
	t.Cleanup(func() { authlibFetchFn = previousFetch })
	calls := 0
	authlibFetchFn = func(*App, string, bool) ([]byte, error) { calls++; return body, nil }
	if result, err := a.authlibInjectorJar(); err == nil || result != "" {
		t.Fatal("tampered cached/downloaded injector was accepted")
	}
	if calls != 2 {
		t.Fatalf("expected verified CDN and upstream attempts, got %d", calls)
	}
}
