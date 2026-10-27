package remotepipe

import (
	"bytes"
	"errors"
	"io"
	"os"
	"sync"
	"testing"
	"time"
)

// relay з'єднує дві сторони так, як це робить VPS: шматок з send одного кінця потрапляє в Push іншого.
func relay() (a, b *Conn, chunks *int) {
	n := 0
	var mu sync.Mutex
	var ca, cb *Conn
	ca = New("tun-0001", "B", 8, func(_, _ string, d []byte) error { mu.Lock(); n++; mu.Unlock(); cb.Push(d); return nil }, nil)
	cb = New("tun-0001", "A", 8, func(_, _ string, d []byte) error { ca.Push(d); return nil }, nil)
	return ca, cb, &n
}

func TestChunkedRoundTrip(t *testing.T) {
	a, b, n := relay()
	msg := bytes.Repeat([]byte("0123456789"), 5) // 50 байт → 7 шматків по 8
	if w, err := a.Write(msg); err != nil || w != len(msg) {
		t.Fatalf("write %d %v", w, err)
	}
	if *n != 7 {
		t.Fatalf("chunks=%d, want 7", *n)
	}
	got := make([]byte, len(msg))
	if _, err := io.ReadFull(b, got); err != nil || !bytes.Equal(got, msg) {
		t.Fatalf("read %v", err)
	}
}

func TestReadDeadlineAndWake(t *testing.T) {
	_, b, _ := relay()
	b.SetReadDeadline(time.Now().Add(30 * time.Millisecond))
	if _, err := b.Read(make([]byte, 4)); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("want deadline, got %v", err)
	}
	// дедлайн, виставлений ПІД ЧАС блокуючого Read, має його розбудити
	b.SetReadDeadline(time.Time{})
	res := make(chan error, 1)
	go func() { _, err := b.Read(make([]byte, 4)); res <- err }()
	time.Sleep(20 * time.Millisecond)
	b.SetReadDeadline(time.Now().Add(10 * time.Millisecond))
	select {
	case err := <-res:
		if !errors.Is(err, os.ErrDeadlineExceeded) {
			t.Fatalf("got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Read не прокинувся від нового дедлайну")
	}
}

func TestCloseDrainsThenEOF(t *testing.T) {
	a, b, _ := relay()
	a.Write([]byte("hi"))
	b.Close()
	buf := make([]byte, 8)
	n, err := b.Read(buf)
	if err != nil || string(buf[:n]) != "hi" {
		t.Fatalf("%q %v", buf[:n], err)
	}
	if _, err := b.Read(buf); err != io.EOF {
		t.Fatalf("want EOF, got %v", err)
	}
	if _, err := b.Write([]byte("x")); err == nil {
		t.Fatal("write after close must fail")
	}
}

func TestOverflowClosesInsteadOfDropping(t *testing.T) {
	c := New("tun-0002", "A", 8, nil, nil)
	for i := 0; i < inQueue; i++ {
		if !c.Push([]byte{1}) {
			t.Fatalf("push %d refused", i)
		}
	}
	if c.Push([]byte{1}) {
		t.Fatal("overflow must be refused")
	}
	if c.Push([]byte{1}) {
		t.Fatal("conn must stay closed")
	}
}

func TestOnCloseOnce(t *testing.T) {
	n := 0
	c := New("tun-0003", "A", 8, nil, func(*Conn) { n++ })
	c.Close()
	c.Close()
	if n != 1 {
		t.Fatalf("onClose called %d times", n)
	}
}

func TestSendErrorPropagates(t *testing.T) {
	boom := errors.New("boom")
	c := New("tun-0004", "A", 4, func(_, _ string, _ []byte) error { return boom }, nil)
	if _, err := c.Write([]byte("abcdef")); !errors.Is(err, boom) {
		t.Fatalf("got %v", err)
	}
}
