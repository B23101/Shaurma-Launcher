package p2p

import (
	"net/http"
	"testing"
)

func TestLobbyErrorIsGateDenied(t *testing.T) {
	cases := []struct {
		name string
		le   *LobbyError
		want bool
	}{
		{"голий Forbidden від гейта", &LobbyError{Status: 403, Code: "Forbidden", Message: "Forbidden"}, true},
		{"порожнє тіло", &LobbyError{Status: 403}, true},
		{"рішення власника", &LobbyError{Status: 403, Code: CodeNotAllowed, Message: "Ви не в списку"}, false},
		{"403 з поясненням", &LobbyError{Status: 403, Code: "Невірний ownerSecret", Message: "Невірний ownerSecret"}, false},
		{"не 403", &LobbyError{Status: 404, Code: "Forbidden", Message: "Forbidden"}, false},
	}
	for _, c := range cases {
		if got := c.le.IsGateDenied(); got != c.want {
			t.Errorf("%s: IsGateDenied=%v, очікувалось %v", c.name, got, c.want)
		}
	}
}

func TestReasonForLobbyGateVsOwner(t *testing.T) {
	g := &GuestService{}
	if r := g.reasonForLobby(&LobbyError{Status: 403, Code: "Forbidden", Message: "Forbidden"}); r != RejGateDenied {
		t.Errorf("голий 403 має давати gateDenied, а дав %s", RejectKey(r))
	}
	if r := g.reasonForLobby(&LobbyError{Status: 403, Code: CodeNotAllowed, Message: "x"}); r != RejNotAllowed {
		t.Errorf("p2p_not_allowed має давати notAllowed, а дав %s", RejectKey(r))
	}
}

func TestClassifyFailureSource(t *testing.T) {
	h := http.Header{}
	if s := classifyFailureSource(403, h, "Forbidden", "Forbidden"); s == "" {
		t.Error("голий 403 має мати джерело-гейт")
	}
	if s := classifyFailureSource(403, h, CodeNotAllowed, "x"); s != "" {
		t.Errorf("відповідь P2P-логіки не має мати джерела, а має %q", s)
	}
	h.Set("Cf-Mitigated", "challenge")
	if s := classifyFailureSource(403, h, "", ""); s == "" {
		t.Error("cf-mitigated має визначатись як Cloudflare")
	}
}
