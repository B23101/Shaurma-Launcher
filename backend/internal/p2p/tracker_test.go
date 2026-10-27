package p2p

import (
	"io"
	"net"
	"testing"
)

// Трекер — те, що дає власнику побачити «хто зайшов і який у нього пінг».
// Тут перевіряємо, що він рахує саме те, що треба, і не блокує проксі.

func TestTracker_CountsBytesBothDirections(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	tr := newConnectionTracker()
	id := tr.add("100.64.0.7:5555", "100.64.0.7")
	wrapped, ok := tr.wrap(id, server)
	if !ok {
		t.Fatal("wrap мав пройти для щойно доданого з'єднання")
	}

	// гість → хост (BytesIn)
	go func() { _, _ = client.Write([]byte("hello")) }()
	buf := make([]byte, 5)
	if _, err := io.ReadFull(wrapped, buf); err != nil {
		t.Fatalf("read: %v", err)
	}

	// хост → гість (BytesOut)
	go func() { _, _ = io.ReadFull(client, buf) }()
	if _, err := wrapped.Write([]byte("world")); err != nil {
		t.Fatalf("write: %v", err)
	}

	guests := tr.guests()
	if len(guests) != 1 {
		t.Fatalf("очікували 1 з'єднання, отримали %d", len(guests))
	}
	g := guests[0]
	if g.BytesIn != 5 {
		t.Errorf("BytesIn = %d, want 5", g.BytesIn)
	}
	if g.BytesOut != 5 {
		t.Errorf("BytesOut = %d, want 5", g.BytesOut)
	}
	if g.GuestIP != "100.64.0.7" {
		t.Errorf("GuestIP = %q", g.GuestIP)
	}
	if g.RemoteAddr != "100.64.0.7:5555" {
		t.Errorf("RemoteAddr = %q", g.RemoteAddr)
	}
	if g.ConnectedAt == 0 || g.LastSeen == 0 {
		t.Error("часові мітки мають бути заповнені")
	}
}

func TestTracker_RemoveFreesGuest(t *testing.T) {
	tr := newConnectionTracker()
	id := tr.add("100.64.0.8:1", "100.64.0.8")
	if len(tr.guests()) != 1 {
		t.Fatal("гість мав з'явитись")
	}
	tr.remove(id)
	if len(tr.guests()) != 0 {
		t.Fatal("після remove гість мав зникнути")
	}
	if _, ok := tr.wrap(id, nil); ok {
		t.Fatal("wrap для видаленого гостя має повертати false")
	}
}

func TestTracker_PingStorage(t *testing.T) {
	tr := newConnectionTracker()
	id := tr.add("100.64.0.9:1", "100.64.0.9")

	tr.setPing(id, 42)
	if got := tr.guests()[0].PingMs; got != 42 {
		t.Fatalf("PingMs = %d, want 42", got)
	}
	// Невдалий вимір (0) НЕ має затирати попередній успішний пінг.
	tr.setPing(id, 0)
	if got := tr.guests()[0].PingMs; got != 42 {
		t.Fatalf("0 не має затирати пінг, отримали %d", got)
	}
	// Пінг для невідомого з'єднання — no-op, без паніки.
	tr.setPing("nope", 10)
}

func TestTracker_TargetsSkipsEmptyIP(t *testing.T) {
	tr := newConnectionTracker()
	tr.add("100.64.0.10:1", "100.64.0.10")
	tr.add("local", "") // без tailnet-IP — пінг міряти ні по чому
	targets := tr.targets()
	if len(targets) != 1 {
		t.Fatalf("очікували 1 ціль для пінгу, отримали %d", len(targets))
	}
	if targets[0][1] != "100.64.0.10" {
		t.Fatalf("ціль = %v", targets[0])
	}
}

func TestHostOfIP(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"100.64.0.5:51234", "100.64.0.5"},
		{"100.64.0.5", "100.64.0.5"},
		{"[fd7a::1]:1234", "fd7a::1"},
	}
	for _, c := range cases {
		addr, err := net.ResolveTCPAddr("tcp", c.in)
		if err != nil {
			// IPv6 без порту не резолвиться як tcp — перевіряємо напряму.
			if got := hostOfIP(&fakeAddr{s: c.in}); got != c.want {
				t.Errorf("hostOfIP(%q) = %q, want %q", c.in, got, c.want)
			}
			continue
		}
		if got := hostOfIP(addr); got != c.want {
			t.Errorf("hostOfIP(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if hostOfIP(nil) != "" {
		t.Error("hostOfIP(nil) має бути порожнім")
	}
}

type fakeAddr struct{ s string }

func (a *fakeAddr) Network() string { return "tcp" }
func (a *fakeAddr) String() string  { return a.s }
