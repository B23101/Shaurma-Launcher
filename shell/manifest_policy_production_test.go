//go:build production

package main

import "testing"

// Політика: у production-збірці перевірка підпису увімкнена завжди, навіть без вшитих ключів.
func TestProductionManifestPolicyAlwaysRequiresSignature(t *testing.T) {
	if !manifestSigEnabled() {
		t.Fatal("production build must always require a signed manifest")
	}
}
