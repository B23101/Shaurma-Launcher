package p2p

import (
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

// Вхід у гру при вимкненому P2P: гравець отримує екран відмови з причиною,
// а лаунчер — один сигнал (повтор у межах disabledHitGap не дублюється).
func TestDisabledNoticeLogin(t *testing.T) {
	hits := make(chan string, 4)
	d := &disabledNotice{playSuffix: PlaySuffix, onLogin: func(a string) { hits <- a }}

	for i := 0; i < 2; i++ {
		srv, cli := net.Pipe()
		go d.serve(srv)
		_ = cli.SetDeadline(time.Now().Add(3 * time.Second))
		frame := buildHandshakeFrame(767, "abcd."+PlaySuffix, 25565, 2)
		login := []byte{0x02, 0x00, 0x00} // мінімальний Login Start, щоб клієнт «щось надіслав»
		go func() { _, _ = cli.Write(append(frame, login...)) }()
		reply, _ := io.ReadAll(cli)
		_ = cli.Close()
		if len(reply) == 0 {
			t.Fatalf("спроба %d: гравець не отримав відповіді", i)
		}
		if !strings.Contains(string(reply), "P2P") {
			t.Fatalf("спроба %d: у відповіді немає причини: %q", i, reply)
		}
	}

	select {
	case a := <-hits:
		if a != "abcd" && !strings.HasPrefix(a, "abcd") {
			t.Fatalf("неочікувана адреса: %q", a)
		}
	case <-time.After(time.Second):
		t.Fatal("лаунчер не отримав сигналу")
	}
	select {
	case a := <-hits:
		t.Fatalf("другий сигнал у межах %s: %q", disabledHitGap, a)
	case <-time.After(100 * time.Millisecond):
	}
}

// Пінг списку серверів не смикає лаунчер (досить MOTD).
func TestDisabledNoticeStatusNoHit(t *testing.T) {
	hits := 0
	d := &disabledNotice{playSuffix: PlaySuffix, onLogin: func(string) { hits++ }}
	srv, cli := net.Pipe()
	go d.serve(srv)
	_ = cli.SetDeadline(time.Now().Add(3 * time.Second))
	frame := buildHandshakeFrame(767, "abcd."+PlaySuffix, 25565, 1)
	go func() { _, _ = cli.Write(append(frame, 0x01, 0x00)) }()
	_, _ = io.ReadAll(cli)
	_ = cli.Close()
	time.Sleep(50 * time.Millisecond)
	if hits != 0 {
		t.Fatalf("пінг не мав викликати лаунчер, викликів: %d", hits)
	}
}
