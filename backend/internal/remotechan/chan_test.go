package remotechan

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net"
	"sync"
	"testing"
	"time"

	"shaurma-launcher-backend/internal/social"
)

const (
	owner  = "aaaaaaaa-0000-0000-0000-000000000001"
	friend = "bbbbbbbb-0000-0000-0000-000000000002"
	srv    = "srv1"
)

type env struct {
	vpsPub   ed25519.PublicKey
	vpsPriv  ed25519.PrivateKey
	hostPub  ed25519.PublicKey
	hostPriv ed25519.PrivateKey
	now      time.Time
}

func newEnv() *env {
	pub, priv, _ := ed25519.GenerateKey(nil)
	hp, hpriv, _ := ed25519.GenerateKey(nil)
	return &env{vpsPub: pub, vpsPriv: priv, hostPub: hp, hostPriv: hpriv, now: time.Now()}
}

func (e *env) mandate(eph *Ephemeral, mod func(*social.RemoteMandate)) social.RemoteMandate {
	m := social.RemoteMandate{V: 1, Owner: owner, Friend: friend, Srv: srv, Op: OpP2POpen, Req: "req-p2p-0001", Caps: "sxce",
		CT: eph.Hash(), PK: base64.StdEncoding.EncodeToString(e.hostPub), IAT: e.now.UnixMilli(), EXP: e.now.Add(3 * time.Minute).UnixMilli()}
	if mod != nil {
		mod(&m)
	}
	m.Sig = base64.StdEncoding.EncodeToString(ed25519.Sign(e.vpsPriv, m.SigningBytes()))
	return m
}

func (e *env) accept(hostKey ed25519.PrivateKey, isFriend bool) AcceptParams {
	return AcceptParams{
		Self: owner, VPSKey: func() ed25519.PublicKey { return e.vpsPub }, HostKey: hostKey,
		IsFriend: func(string) bool { return isFriend }, Now: func() time.Time { return e.now },
	}
}

type tap struct {
	net.Conn
	mu     sync.Mutex
	log    [][]byte
	mutate func([]byte) []byte
}

func (t *tap) Write(b []byte) (int, error) {
	t.mu.Lock()
	t.log = append(t.log, append([]byte(nil), b...))
	m := t.mutate
	t.mu.Unlock()
	if m != nil {
		if nb := m(append([]byte(nil), b...)); nb != nil {
			_, err := t.Conn.Write(nb)
			return len(b), err
		}
		return len(b), nil // «втрачено» в дорозі
	}
	return t.Conn.Write(b)
}

type result struct {
	c      *Conn
	friend string
	err    error
}

func handshake(e *env, dp func(*DialParams), ap AcceptParams) (cli, srvr result, t *tap) {
	a, b := net.Pipe()
	t = &tap{Conn: a}
	eph, _ := NewEphemeral()
	p := DialParams{Self: friend, Owner: owner, ServerID: srv, Mandate: e.mandate(eph, nil), Ephemeral: eph, VPSKey: e.vpsPub, Now: func() time.Time { return e.now }}
	if dp != nil {
		dp(&p)
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		c, err := Dial(t, p)
		cli = result{c: c, err: err}
		if err != nil {
			a.Close()
		}
	}()
	go func() {
		defer wg.Done()
		c, f, _, err := Accept(b, ap)
		srvr = result{c: c, friend: f, err: err}
		if err != nil {
			b.Close()
		}
	}()
	wg.Wait()
	return
}

func TestHappyPathAndConfidentiality(t *testing.T) {
	e := newEnv()
	cli, srvr, tp := handshake(e, nil, e.accept(e.hostPriv, true))
	if cli.err != nil || srvr.err != nil {
		t.Fatalf("dial=%v accept=%v", cli.err, srvr.err)
	}
	if srvr.friend != friend {
		t.Fatalf("friend=%s", srvr.friend)
	}
	secret := []byte(`{"cmd":"op Steve","секрет":"паролі-не-тут"}`)
	go cli.c.Send(secret)
	got, err := srvr.c.Recv()
	if err != nil || !bytes.Equal(got, secret) {
		t.Fatalf("recv=%q err=%v", got, err)
	}
	go srvr.c.Send([]byte("відповідь"))
	if got, err := cli.c.Recv(); err != nil || string(got) != "відповідь" {
		t.Fatalf("%q %v", got, err)
	}
	tp.mu.Lock()
	defer tp.mu.Unlock()
	for _, w := range tp.log {
		if bytes.Contains(w, []byte("op Steve")) || bytes.Contains(w, []byte("секрет")) {
			t.Fatal("відкритий текст потрапив у мережу")
		}
	}
}

func TestMandateIsRequiredAndBound(t *testing.T) {
	e := newEnv()
	cases := map[string]func(*DialParams){
		"чужий ключ VPS": func(p *DialParams) {
			_, other, _ := ed25519.GenerateKey(nil)
			p.Mandate.Sig = base64.StdEncoding.EncodeToString(ed25519.Sign(other, p.Mandate.SigningBytes()))
		},
		"мандат без підпису": func(p *DialParams) { p.Mandate = social.RemoteMandate{} },
		"мандат на інший ephemeral": func(p *DialParams) {
			other, _ := NewEphemeral()
			p.Mandate = e.mandate(other, nil) // перехоплений мандат + власний ключ рукостискання
		},
		"інша операція": func(p *DialParams) {
			p.Mandate = e.mandate(p.Ephemeral, func(m *social.RemoteMandate) { m.Op = "console_exec" })
		},
		"інший власник": func(p *DialParams) {
			p.Mandate = e.mandate(p.Ephemeral, func(m *social.RemoteMandate) { m.Owner = "cccccccc-0000-0000-0000-000000000003" })
		},
		"інший друг": func(p *DialParams) {
			p.Mandate = e.mandate(p.Ephemeral, func(m *social.RemoteMandate) { m.Friend = "cccccccc-0000-0000-0000-000000000003" })
		},
		"без права на консоль": func(p *DialParams) {
			p.Mandate = e.mandate(p.Ephemeral, func(m *social.RemoteMandate) { m.Caps = "sx" })
		},
		"протермінований": func(p *DialParams) {
			p.Mandate = e.mandate(p.Ephemeral, func(m *social.RemoteMandate) {
				m.IAT = e.now.Add(-10 * time.Minute).UnixMilli()
				m.EXP = e.now.Add(-5 * time.Minute).UnixMilli()
			})
		},
	}
	for name, mod := range cases {
		_, srvr, _ := handshake(e, mod, e.accept(e.hostPriv, true))
		if srvr.err == nil {
			t.Fatalf("%s: власник прийняв з'єднання", name)
		}
	}
}

func TestOwnerImpersonationFails(t *testing.T) {
	e := newEnv()
	// Мережа посередині підставляє «власника» зі своїм ключем: VPS вказав у мандаті ІНШИЙ ключ.
	_, fakePriv, _ := ed25519.GenerateKey(nil)
	cli, srvr, _ := handshake(e, nil, e.accept(fakePriv, true))
	if srvr.err == nil {
		t.Fatal("підставний власник не мав би прийняти мандат, виданий на інший ключ")
	}
	_ = cli
	// Підставний власник, що все ж відповів би (обходить власну перевірку), клієнт відхиляє за підписом.
	a, b := net.Pipe()
	eph, _ := NewEphemeral()
	done := make(chan error, 1)
	go func() {
		_, err := Dial(a, DialParams{Self: friend, Owner: owner, ServerID: srv, Mandate: e.mandate(eph, nil), Ephemeral: eph, VPSKey: e.vpsPub, Now: func() time.Time { return e.now }})
		done <- err
	}()
	go func() {
		// власна «фейкова» реалізація: читає hello і відповідає підписом не тим ключем
		mg := make([]byte, len(magic))
		b.Read(mg)
		hb, _ := readFrame(b, maxHello)
		_, fake, _ := ed25519.GenerateKey(nil)
		x, _ := NewEphemeral()
		xp := x.priv.PublicKey().Bytes()
		th := sha256.Sum256(append(append([]byte{}, hb...), xp...))
		rep, _ := json.Marshal(helloReply{Eph: xp, Sig: ed25519.Sign(fake, hostSigInput(th[:]))})
		writeFrame(b, rep)
	}()
	if err := <-done; err != ErrAuth {
		t.Fatalf("клієнт прийняв підробленого власника: %v", err)
	}
}

func TestClientRefusesUnsignedOrForeignMandate(t *testing.T) {
	e := newEnv()
	eph, _ := NewEphemeral()
	a, _ := net.Pipe()
	_, other, _ := ed25519.GenerateKey(nil)
	bad := e.mandate(eph, nil)
	bad.Sig = base64.StdEncoding.EncodeToString(ed25519.Sign(other, bad.SigningBytes()))
	if _, err := Dial(a, DialParams{Self: friend, Owner: owner, ServerID: srv, Mandate: bad, Ephemeral: eph, VPSKey: e.vpsPub, Now: func() time.Time { return e.now }}); err != ErrMandate {
		t.Fatalf("мандат з чужим підписом: %v", err)
	}
	noPK := e.mandate(eph, func(m *social.RemoteMandate) { m.PK = "" })
	if _, err := Dial(a, DialParams{Self: friend, Owner: owner, ServerID: srv, Mandate: noPK, Ephemeral: eph, VPSKey: e.vpsPub, Now: func() time.Time { return e.now }}); err != ErrMandate {
		t.Fatalf("мандат без ключа власника: %v", err)
	}
}

func TestNonFriendRejectedLocally(t *testing.T) {
	e := newEnv()
	_, srvr, _ := handshake(e, nil, e.accept(e.hostPriv, false))
	if srvr.err == nil {
		t.Fatal("не-друг має відхилятись, навіть із мандатом")
	}
}

func TestTamperReplayAndDropBreakChannel(t *testing.T) {
	// підміна біта
	e := newEnv()
	cli, srvr, tp := handshake(e, nil, e.accept(e.hostPriv, true))
	if cli.err != nil || srvr.err != nil {
		t.Fatal(cli.err, srvr.err)
	}
	tp.mutate = func(b []byte) []byte { b[len(b)-1] ^= 1; return b }
	go cli.c.Send([]byte("say hi"))
	if _, err := srvr.c.Recv(); err == nil {
		t.Fatal("підмінений кадр прийнято")
	}

	// повтор кадру
	e = newEnv()
	cli, srvr, tp = handshake(e, nil, e.accept(e.hostPriv, true))
	go cli.c.Send([]byte("say once"))
	if got, err := srvr.c.Recv(); err != nil || string(got) != "say once" {
		t.Fatal(got, err)
	}
	tp.mu.Lock()
	frame := tp.log[len(tp.log)-1]
	tp.mu.Unlock()
	go tp.Conn.Write(frame) // той самий кадр ще раз
	if _, err := srvr.c.Recv(); err == nil {
		t.Fatal("повторений кадр прийнято")
	}

	// втрата кадру (перший «зник», другий — вже не той лічильник)
	e = newEnv()
	cli, srvr, tp = handshake(e, nil, e.accept(e.hostPriv, true))
	dropped := false
	tp.mutate = func(b []byte) []byte {
		if !dropped {
			dropped = true
			return nil
		}
		return b
	}
	go func() { cli.c.Send([]byte("one")); cli.c.Send([]byte("two")) }()
	if _, err := srvr.c.Recv(); err == nil {
		t.Fatal("пропуск кадру мав порвати канал")
	}
}

func TestChannelExpires(t *testing.T) {
	e := newEnv()
	cli, srvr, _ := handshake(e, nil, e.accept(e.hostPriv, true))
	cli.c.deadline = time.Now().Add(-time.Second)
	if err := cli.c.Send([]byte("x")); err != ErrClosed {
		t.Fatalf("протермінований канал: %v", err)
	}
	_ = srvr
}

func TestGarbageInputRejected(t *testing.T) {
	e := newEnv()
	for _, garbage := range [][]byte{[]byte("GET / HTTP/1.1\r\n\r\n"), []byte("SHRC1\xff\xff\xff\xff"), []byte("SHRC1\x00\x00\x00\x02{}")} {
		a, b := net.Pipe()
		done := make(chan error, 1)
		go func() { _, _, _, err := Accept(b, e.accept(e.hostPriv, true)); done <- err }()
		go func() { a.Write(garbage); a.Close() }()
		if err := <-done; err == nil {
			t.Fatalf("сміття %q прийнято", garbage)
		}
	}
}
