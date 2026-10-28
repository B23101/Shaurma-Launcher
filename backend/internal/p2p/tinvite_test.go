package p2p

import (
	"bufio"
	"bytes"
	"net"
	"strings"
	"testing"
)

func TestProbeLocalServer_ParsesRealSigns(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		r := bufio.NewReader(c)
		n, _ := readVarint(r) // хендшейк
		_, _ = r.Discard(int(n))
		_, _ = readVarint(r) // status request
		_, _ = r.ReadByte()
		js := `{"version":{"name":"1.20.1"},"players":{"online":2,"max":8},"description":{"text":"§aМій ","extra":[{"text":"світ"}]},"forgeData":{"mods":[]}}`
		var body bytes.Buffer
		body.WriteByte(0)
		writeVarint(&body, len(js))
		body.WriteString(js)
		var out bytes.Buffer
		writeVarint(&out, body.Len())
		out.Write(body.Bytes())
		_, _ = c.Write(out.Bytes())
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	s, err := ProbeLocalServer(port)
	if err != nil {
		t.Fatal(err)
	}
	if s.Version != "1.20.1" || s.Players != 2 || s.Max != 8 || s.Motd != "Мій світ" || s.Kind != "modded" {
		t.Fatalf("%+v", s)
	}
	if _, err := ProbeLocalServer(1); err == nil {
		t.Fatal("закритий порт не може бути світом")
	}
}

func TestPreambleGate(t *testing.T) {
	id, ticket := strings.Repeat("a", 32), strings.Repeat("b", 64)
	check := func(data []byte, wantOK bool) {
		a, b := net.Pipe()
		go func() { _, _ = a.Write(data); a.Close() }()
		got, r, ok := readPreamble(b, id)
		if ok != wantOK {
			t.Fatalf("ok=%v для %q", ok, data[:min(20, len(data))])
		}
		if ok {
			rest := make([]byte, 4)
			n, _ := r.Read(rest)
			if got != ticket || string(rest[:n]) != "\x10MC!"[:n] {
				t.Fatalf("квиток/залишок: %q %q", got, rest[:n])
			}
		}
		b.Close()
	}
	check(append(Preamble(id, ticket), []byte("\x10MC!")...), true)
	check(append(Preamble(strings.Repeat("c", 32), ticket), []byte("\x10MC!")...), false) // чуже запрошення
	check([]byte("\x10\x00\xff\x05localhost.................................................................................................................."), false)
	g := &HostGate{InviteID: id, Admit: func(i, tk string) bool { return i == id && tk == ticket }}
	if !g.allow(ticket) || g.allow(strings.Repeat("d", 64)) {
		t.Fatal("gate")
	}
	if got := withPreamble(nil, []byte("x")); string(got) != "x" {
		t.Fatal("без квитка хендшейк не змінюється")
	}
}

// Квиток з VPS приходить під повною адресою, а гість шукає його за адресою з хендшейка без суфікса.
func TestPreambleForFullAddress(t *testing.T) {
	g := &GuestService{}
	pre := Preamble(strings.Repeat("a", 32), strings.Repeat("b", 64))
	g.tickets.Store(tinviteKey("TMP1-123456."+PlaySuffix+"."), pre)
	for _, addr := range []string{"tmp1-123456", "tmp1-123456." + PlaySuffix} {
		if got := g.preambleFor(addr); !bytes.Equal(got, pre) {
			t.Fatalf("%q: квиток не знайдено", addr)
		}
	}
	if got := g.preambleFor("tmp2-123456"); got != nil {
		t.Fatalf("чужа адреса отримала квиток: %q", got)
	}
}
