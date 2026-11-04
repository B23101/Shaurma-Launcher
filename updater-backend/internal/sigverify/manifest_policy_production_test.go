//go:build production

package sigverify

import "testing"

// Політика: у production-збірці перевірка підпису увімкнена завжди, навіть без вшитих ключів.
func TestProductionManifestPolicyAlwaysRequiresSignature(t *testing.T) {
	if !Enabled() {
		t.Fatal("production build must always require a signed manifest")
	}
}
