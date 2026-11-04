//go:build windows

package main

import "testing"

func TestURLProtocolCommandQuotesPathAndArgument(t *testing.T) {
	got := urlProtocolCommand(`C:\Program Files\Shaurma Launcher\ShaurmLauncher.exe`)
	want := `"C:\Program Files\Shaurma Launcher\ShaurmLauncher.exe" "%1"`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
