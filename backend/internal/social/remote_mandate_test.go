package social

import (
	"crypto/ed25519"
	"encoding/base64"
	"testing"
	"time"
)

// Той самий вектор, що в vps/internal/hub/remote_console_test.go (TestMandateKnownAnswer).
const (
	katPub = "0owiPhPnNtMwwIP5DpzIPDKxwArL1EbVaigAm/QE5Qw="
	katSig = "E3T2XapBbPb5jpdMrHm/+U7upfOZGsWLqXiQ60f7gnZ/TI9Xxyyo0jijBEmtLzf9lEQDr9ErOWK3USCynSJBBg=="
)

func katMandate() RemoteMandate {
	return RemoteMandate{V: 1, Owner: "aaaaaaaa-0000-0000-0000-000000000001", Friend: "bbbbbbbb-0000-0000-0000-000000000002",
		Srv: "srv1", Op: "console_exec", Req: "req-kat-0001", Caps: "sxce", CT: EnvelopeHash("Y2lwaGVy", "bm9uY2U="),
		SID: "sid-kat-0001", IAT: 1_700_000_000_000, EXP: 1_700_000_060_000, Sig: katSig}
}

func katKey(t *testing.T) ed25519.PublicKey {
	raw, err := base64.StdEncoding.DecodeString(katPub)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestMandateKnownAnswerMatchesVPS(t *testing.T) {
	if err := VerifyMandateSig(katKey(t), katMandate(), time.UnixMilli(1_700_000_010_000)); err != nil {
		t.Fatalf("підпис VPS не перевіряється лаунчером: %v", err)
	}
}

func TestVerifyIncomingBinding(t *testing.T) {
	m := katMandate()
	now := time.UnixMilli(1_700_000_010_000)
	in := RemoteIncoming{ReqID: m.Req, From: m.Friend, SID: m.SID, Op: m.Op, ServerID: m.Srv, Ciphertext: "Y2lwaGVy", Nonce: "bm9uY2U="}
	if err := VerifyIncoming(katKey(t), &m, m.Owner, in, now); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*RemoteIncoming, *string){
		"інший отримувач":    func(_ *RemoteIncoming, self *string) { *self = "cccccccc-0000-0000-0000-000000000003" },
		"інший відправник":   func(i *RemoteIncoming, _ *string) { i.From = "cccccccc-0000-0000-0000-000000000003" },
		"інший reqId":        func(i *RemoteIncoming, _ *string) { i.ReqID = "req-other-0001" },
		"інший sid":          func(i *RemoteIncoming, _ *string) { i.SID = "sid-other-0001" },
		"підмінений конверт": func(i *RemoteIncoming, _ *string) { i.Ciphertext = "ZXZpbA==" },
	}
	for name, mod := range cases {
		i, self := in, m.Owner
		mod(&i, &self)
		if VerifyIncoming(katKey(t), &m, self, i, now) == nil {
			t.Fatalf("%s: мандат прийнято", name)
		}
	}
	if VerifyIncoming(katKey(t), nil, m.Owner, in, now) != ErrMandateMissing {
		t.Fatal("відсутній мандат має відхилятись")
	}
	if VerifyIncoming(katKey(t), &m, m.Owner, in, now.Add(5*time.Minute)) == nil {
		t.Fatal("протермінований мандат прийнято")
	}
	forged := m
	forged.Caps = "x"
	if VerifyIncoming(katKey(t), &forged, m.Owner, in, now) == nil {
		t.Fatal("підроблені права пройшли")
	}
}

func TestKeyPinTOFU(t *testing.T) {
	stored := ""
	pin := KeyPin{Load: func() string { return stored }, Save: func(s string) { stored = s }}
	if _, err := pin.Check(katPub); err != nil || stored != katPub {
		t.Fatalf("перший ключ має закріпитись: %v %q", err, stored)
	}
	if _, err := pin.Check(katPub); err != nil {
		t.Fatal(err)
	}
	other := base64.StdEncoding.EncodeToString(make([]byte, 32))
	if _, err := pin.Check(other); err == nil || stored != katPub {
		t.Fatal("підміна ключа VPS має відхилятись")
	}
	if _, err := pin.Check("not-a-key"); err == nil {
		t.Fatal("сміття має відхилятись")
	}
}
