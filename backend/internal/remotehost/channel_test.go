package remotehost

import (
	"crypto/ed25519"
	"encoding/base64"
	"net"
	"sync"
	"testing"
	"time"

	"shaurma-launcher-backend/internal/remotechan"
	"shaurma-launcher-backend/internal/remotectl"
	"shaurma-launcher-backend/internal/social"
)

// countingNet рахує, скільки кадрів пішло через VPS: у прямому режимі — жодного.
type countingNet struct {
	mu sync.Mutex
	n  int
}

func (c *countingNet) bump() error { c.mu.Lock(); c.n++; c.mu.Unlock(); return nil }
func (c *countingNet) SendCmd(string, string, string, string, string, string, string) error {
	return c.bump()
}
func (c *countingNet) SendReply(string, string, string, string) error  { return c.bump() }
func (c *countingNet) SendStream(string, string, string, string) error { return c.bump() }
func (c *countingNet) count() int                                      { c.mu.Lock(); defer c.mu.Unlock(); return c.n }

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("не дочекався: %s", what)
}

func TestDirectChannelEndToEnd(t *testing.T) {
	r := newRig(t, remotectl.Rights{Console: true, Exec: true}, "ce")
	r.srv.state = "running"
	r.srv.tail = []Line{{ID: 1, L: "info", X: "історія"}}

	// лічильник VPS-кадрів
	cn := &countingNet{}
	r.host.d.Net = cn
	r.client.Net = cn

	// ключ каналу власника: його реєструє лаунчер у VPS, а VPS вкладає в мандат
	hostPub, hostPriv, _ := ed25519.GenerateKey(nil)

	// мандат VPS на p2p_open, прив'язаний до одноразового ключа друга
	eph, _ := remotechan.NewEphemeral()
	vpsPub, vpsPriv, _ := ed25519.GenerateKey(nil)
	md := social.RemoteMandate{V: 1, Owner: ownerID, Friend: friendID, Srv: srvID, Op: remotechan.OpP2POpen, Req: "req-p2p-0001", Caps: "ce",
		CT: eph.Hash(), PK: base64.StdEncoding.EncodeToString(hostPub), IAT: r.now.UnixMilli(), EXP: r.now.Add(3 * time.Minute).UnixMilli()}
	md.Sig = base64.StdEncoding.EncodeToString(ed25519.Sign(vpsPriv, md.SigningBytes()))

	a, b := net.Pipe()
	var (
		srvConn *remotechan.Conn
		who     string
		gotMd   social.RemoteMandate
		aerr    error
		cliConn *remotechan.Conn
		derr    error
		wg      sync.WaitGroup
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		srvConn, who, gotMd, aerr = remotechan.Accept(b, remotechan.AcceptParams{
			Self: ownerID, VPSKey: func() ed25519.PublicKey { return vpsPub },
			HostKey:  hostPriv,
			IsFriend: func(id string) bool { ok, _ := r.frs.Friend(id); return ok }, Now: func() time.Time { return r.now },
		})
	}()
	go func() {
		defer wg.Done()
		cliConn, derr = remotechan.Dial(a, remotechan.DialParams{Self: friendID, Owner: ownerID, ServerID: srvID, Mandate: md, Ephemeral: eph, VPSKey: vpsPub, Now: func() time.Time { return r.now }})
	}()
	wg.Wait()
	if aerr != nil || derr != nil {
		t.Fatalf("accept=%v dial=%v", aerr, derr)
	}
	go r.host.ServeChannel(srvConn, who, gotMd)
	go r.client.UseChannel(ownerID, srvID, cliConn)
	eventually(t, "канал зареєстровано", func() bool { return r.client.HasChannel(ownerID, srvID) })

	sid, _, err := r.client.OpenConsole(ownerID, srvID)
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, "консоль відкрита й історія прийшла", func() bool {
		ev := r.ev.lastResult()
		return ev != nil && ev.Code == "opened" && len(r.ev.lines()) == 1
	})
	r.host.OnLine(srvID, Line{ID: 2, L: "info", X: "наживо"})
	r.host.Flush()
	eventually(t, "рядок наживо", func() bool { return len(r.ev.lines()) == 2 })

	r.tick()
	if _, err := r.client.Exec(sid, "say через канал"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "команда виконана", func() bool {
		r.srv.mu.Lock()
		defer r.srv.mu.Unlock()
		return len(r.srv.cmds) == 1 && r.srv.cmds[0] == "say через канал"
	})
	if cn.count() != 0 {
		t.Fatalf("через VPS пішло %d кадрів, у прямому режимі має бути 0", cn.count())
	}

	// Власник знімає право локально → сесія закривається і в каналі.
	_ = r.store.SetGrantFull(ownerID, srvID, friendID, remotectl.Rights{Start: true}, r.now.UnixMilli())
	r.host.Flush()
	eventually(t, "кінець сесії", func() bool { return r.ev.ended() })

	// Порушення протоколу рве канал: сміття замість конверта.
	_ = cliConn.Send([]byte("not json"))
	eventually(t, "канал закрито", func() bool { return !r.client.HasChannel(ownerID, srvID) })
}
