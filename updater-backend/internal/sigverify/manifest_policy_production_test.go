//go:build production

package sigverify

import "testing"

func TestProductionRequiresManifestSignature(t *testing.T) {
	if !Enabled() {
		t.Fatal("production build accepted missing manifest verification keys")
	}
}
