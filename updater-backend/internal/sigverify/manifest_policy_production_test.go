//go:build production

package sigverify

import "testing"

// Політика: перевірка підпису вмикається лише коли вшито ключі.
func TestProductionPolicyFollowsKeys(t *testing.T) {
	if got, want := Enabled(), len(keys(TrustedKeys)) > 0; got != want {
		t.Fatalf("Enabled=%v, очікувалось %v", got, want)
	}
}
