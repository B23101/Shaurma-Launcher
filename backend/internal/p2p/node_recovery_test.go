package p2p

import (
	"context"
	"errors"
	"net"
	"testing"
)

func TestRetryUsesFreshStateDirectory(t *testing.T) {
	cfg := nodeUpConfig{StateDir: "old", OnFreshState: func() (string, error) { return "fresh", nil }}
	if err := cfg.refreshState(); err != nil {
		t.Fatal(err)
	}
	if cfg.StateDir != "fresh" {
		t.Fatal("retry would reuse deleted state directory")
	}
	cfg.OnFreshState = func() (string, error) { return "", errors.New("disk failure") }
	if cfg.refreshState() == nil || cfg.StateDir != "fresh" {
		t.Fatal("failed reset changed state")
	}
}

func TestGuestListenerRecoversAfterPortReleased(t *testing.T) {
	busy, err := net.Listen("tcp", guestExternalAddr)
	if err != nil {
		t.Skipf("test port already occupied: %v", err)
	}
	defer busy.Close()
	api := NewAPI(t.TempDir(), func() string { return "" }, func() string { return "" })
	defer api.StopP2PGuestService()
	if err := api.EnsureP2PGuestService(); err == nil {
		t.Fatal("expected occupied port failure")
	}
	busy.Close()
	if err := api.EnsureP2PGuestService(); err != nil {
		t.Fatalf("listener did not recover: %v", err)
	}
	first := api.guest
	if err := api.EnsureP2PGuestService(); err != nil || api.guest != first {
		t.Fatal("working listener was replaced")
	}
}

func TestCanceledNodeDoesNotRenewIdentity(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := upFreshNode(ctx, nodeUpConfig{AuthKeyFunc: func(context.Context) (string, error) {
		t.Fatal("renewed identity after cancellation")
		return "", nil
	}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}
