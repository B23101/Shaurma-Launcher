//go:build production

package main

import "testing"

func TestProductionRequiresManifestSignature(t *testing.T) {
	if !manifestSigEnabled() {
		t.Fatal("production build accepted missing manifest verification keys")
	}
}
