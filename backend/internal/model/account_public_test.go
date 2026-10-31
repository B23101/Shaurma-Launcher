package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAccountPublicStripsTokens(t *testing.T) {
	a := Account{ID: "1", Username: "Steve", AccessToken: "mc-secret", MsAccessToken: "ms-secret", RefreshToken: "rt-secret", MsIDToken: "id-secret"}
	p := a.Public()
	raw, _ := json.Marshal(p)
	for _, s := range []string{"mc-secret", "ms-secret", "rt-secret", "id-secret"} {
		if strings.Contains(string(raw), s) {
			t.Fatalf("token leaked into public account: %s", raw)
		}
	}
	if p.ID != "1" || p.Username != "Steve" {
		t.Fatalf("public account lost identity fields: %+v", p)
	}
	if a.RefreshToken != "rt-secret" {
		t.Fatal("Public must not mutate the original")
	}
}
