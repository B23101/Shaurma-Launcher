package p2p

import (
	"strings"
	"testing"
)

func TestHostSessionSetAccess(t *testing.T) {
	s := &HostSession{closed: make(chan struct{})}
	if s.p2pGate.Load() != nil {
		t.Fatal("нова сесія відкрита")
	}
	s.SetAccess("friends")
	g := s.p2pGate.Load()
	if g == nil {
		t.Fatal("friends вмикає гейт")
	}
	s.SetAccess("list")
	if s.p2pGate.Load() != g {
		t.Fatal("між обмеженими режимами гейт той самий: рішення приймає вузол")
	}
	s.SetAccess("all")
	if s.p2pGate.Load() != nil {
		t.Fatal("all вимикає гейт")
	}
	s.SetAccess("accounts")
	if s.p2pGate.Load() == nil {
		t.Fatal("accounts знову вмикає гейт")
	}
	s.SetAccess("")
	if s.p2pGate.Load() != nil {
		t.Fatal("порожній режим (старий вузол) = відкрито")
	}
}

func TestHostSessionAdmitCredentials(t *testing.T) {
	c, seen := recServer(t, 200, `{"ok":true,"accountId":"acc","expiresAt":1}`)
	s := &HostSession{Address: "x-111111.play.example.org", lobby: c, sessionToken: tk('d')}

	// Співхост: лише sessionToken оренди.
	res, err := s.admit(tk('a'))
	if err != nil || !res.OK || res.AccountID != "acc" {
		t.Fatalf("%+v %v", res, err)
	}
	if seen.path != "/p2p/admit" || seen.body["address"] != s.Address || seen.body["ticket"] != tk('a') ||
		seen.body["sessionToken"] != tk('d') || seen.body["ownerSecret"] != nil {
		t.Fatalf("тіло запиту співхоста: %+v", seen.body)
	}

	// Токен оренди оновили — admit бере новий.
	s.SetSessionToken(tk('e'))
	_, _ = s.admit(tk('a'))
	if seen.body["sessionToken"] != tk('e') {
		t.Fatalf("токен не оновився: %+v", seen.body)
	}

	// Власник: ownerSecret має пріоритет, sessionToken не шлемо.
	s.ownerSecret = strings.Repeat("a", 32)
	_, _ = s.admit(tk('a'))
	if seen.body["ownerSecret"] != s.ownerSecret || seen.body["sessionToken"] != nil {
		t.Fatalf("тіло запиту власника: %+v", seen.body)
	}
}
