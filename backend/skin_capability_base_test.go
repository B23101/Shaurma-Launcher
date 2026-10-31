//go:build !shaurma

package main

import (
	"reflect"
	"testing"
)

func TestBaseIncludesSharedFeatures(t *testing.T) {
	// These methods are the bridge contract used by the shared frontend.
	app := reflect.TypeOf(&App{})
	for _, method := range []string{
		"GetShaurmaAccount", "GetFriendsState", "SendFriendInviteByHandle",
		"SendChatMessage", "GetChatHistory", "SetChatMuteAll", "SetPresenceMode",
		"ScanLocalServers", "CreateTempInvite", "JoinTempInvite", "SyncTempPackServers",
		"ShareStart", "ShareAccept", "ListShareRecoverable",
		"GetRemoteAccess", "SetRemoteGrant", "RemoteStartServer", "RemoteOpenConsole",
	} {
		if _, ok := app.MethodByName(method); !ok {
			t.Errorf("Base is missing shared feature binding %s", method)
		}
	}
}

func TestBaseSkinsDoNotEnablePrivatePacks(t *testing.T) {
	a := &App{}
	if !a.HasSkinSupport() || a.IsShaurmaEdition() {
		t.Fatal("Base must support skins without enabling the full edition")
	}
	packs, err := newPackProvider(a).GetPackIndex()
	if err != nil || len(packs) != 0 {
		t.Fatalf("Base private pack provider: %v, %v", packs, err)
	}
	if _, err := newPackProvider(a).GetPackByID("private-test-pack"); err == nil {
		t.Fatal("Base unexpectedly exposed a private pack")
	}
}
