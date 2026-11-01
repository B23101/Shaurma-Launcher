//go:build production

package main

import "testing"

// Політика: перевірка підпису вмикається лише коли вшито ключі.
func TestProductionManifestPolicyFollowsKeys(t *testing.T) {
	if got, want := manifestSigEnabled(), manifestSigEnabledWith(trustedKeys); got != want {
		t.Fatalf("manifestSigEnabled=%v, очікувалось %v", got, want)
	}
}
