package p2p

import (
	"testing"
	"time"
)

// Тимчасова адреса запрошення (tmp1..tmp3) — не «збережена»: її не видно у списку для UI, її не можна
// закріпити за сервером, і вона зникає з книги, коли запрошення закрито.

func TestTempAddress_HiddenFromSavedList(t *testing.T) {
	srv, _ := claimServer(t)
	api := NewAPI(t.TempDir(), func() string { return "token-1234567890abcdef" }, func() string { return srv.URL })

	own, err := api.ClaimP2PAddress("prlool", "acc1", "server-1")
	if err != nil {
		t.Fatalf("claim prlool: %v", err)
	}
	tmp, err := api.ClaimP2PAddress("tmp1", "acc1", TempLabel)
	if err != nil {
		t.Fatalf("claim tmp1: %v", err)
	}

	list := api.ListClaimedAddresses("acc1")
	if len(list) != 1 || list[0].Subdomain != own.Subdomain {
		t.Fatalf("у збережених має бути лише звичайна адреса, отримали %+v", list)
	}
	for _, a := range list {
		if a.Subdomain == tmp.Subdomain {
			t.Fatalf("тимчасова адреса %s потрапила у збережені", tmp.Subdomain)
		}
	}
}

func TestTempAddress_CannotBeAssignedToServer(t *testing.T) {
	srv, _ := claimServer(t)
	api := NewAPI(t.TempDir(), func() string { return "token-1234567890abcdef" }, func() string { return srv.URL })

	tmp, err := api.ClaimP2PAddress("tmp2", "acc1", TempLabel)
	if err != nil {
		t.Fatalf("claim tmp2: %v", err)
	}
	if err := api.AssignClaimedAddress(tmp.Subdomain, "server-9", "acc1"); err == nil {
		t.Fatal("тимчасову адресу не можна закріплювати за сервером")
	}
}

func TestTempAddress_ReleaseForgetsIt(t *testing.T) {
	srv, _ := claimServer(t)
	api := NewAPI(t.TempDir(), func() string { return "token-1234567890abcdef" }, func() string { return srv.URL })

	tmp, err := api.ClaimP2PAddress("tmp1", "acc1", TempLabel)
	if err != nil {
		t.Fatalf("claim tmp1: %v", err)
	}
	if err := api.ReleaseTempAddress(tmp.Subdomain); err != nil {
		t.Fatalf("release: %v", err)
	}
	if _, ok := api.findClaimed("tmp1", "acc1"); ok {
		t.Fatal("після закриття запрошення адреса tmp1 лишилась у книзі")
	}
}

func TestTempAddress_PurgeKeepsOrdinaryAddresses(t *testing.T) {
	srv, _ := claimServer(t)
	api := NewAPI(t.TempDir(), func() string { return "token-1234567890abcdef" }, func() string { return srv.URL })

	own, err := api.ClaimP2PAddress("prlool", "acc1", "server-1")
	if err != nil {
		t.Fatalf("claim prlool: %v", err)
	}
	for _, base := range []string{"tmp1", "tmp2", "tmp3"} {
		if _, err := api.ClaimP2PAddress(base, "acc1", TempLabel); err != nil {
			t.Fatalf("claim %s: %v", base, err)
		}
	}

	if n := api.PurgeTempAddresses(5 * time.Second); n != 3 {
		t.Fatalf("очікували прибрати 3 тимчасові адреси, прибрали %d", n)
	}
	for _, base := range []string{"tmp1", "tmp2", "tmp3"} {
		if _, ok := api.findClaimed(base, "acc1"); ok {
			t.Fatalf("%s лишилась у книзі після purge", base)
		}
	}
	if _, ok := api.findClaimed("prlool", "acc1"); !ok {
		t.Fatalf("purge зачепив звичайну адресу %s", own.Subdomain)
	}
}

func TestIsTempBase(t *testing.T) {
	for _, b := range []string{"tmp1", "tmp3", " TMP2 "} {
		if !IsTempBase(b) {
			t.Errorf("%q має бути зарезервованою базою", b)
		}
	}
	for _, b := range []string{"tmp", "tmp10", "tmpx", "prlool", "my-tmp1"} {
		if IsTempBase(b) {
			t.Errorf("%q не має бути зарезервованою базою", b)
		}
	}
}
