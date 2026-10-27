// Package remotepipe — net.Conn поверх тунелю через VPS (кадри rs_pipe / rs_pipe_in).
//
// VPS пересилає лише шматки байтів між двома сторонами заздалегідь зареєстрованого тунелю й
// нічого в них не розуміє. Поверх такого «труби» remotechan робить рукостискання й шифрує кадри,
// тому вмісту (консолі) не бачить навіть VPS. Пакет не знає ні про VPS, ні про remotechan:
// відправлення шматка передається функцією send, вхідні шматки — методом Push.
package remotepipe

import (
	"io"
	"net"
	"os"
	"sync"
	"time"
)

// inQueue — скільки вхідних шматків можна тримати непрочитаними. Переповнення = розрив
// (мовчки губити шматок не можна: AEAD-лічильник remotechan однаково розсиплеться).
const inQueue = 512

type addr string

func (a addr) Network() string { return "shaurma-pipe" }
func (a addr) String() string  { return string(a) }

// Conn — одна сторона тунелю. Безпечний для одного читача й кількох письменників.
type Conn struct {
	tunnel, peer string
	send         func(tunnel, to string, data []byte) error
	chunk        int
	onClose      func(*Conn)

	in   chan []byte
	done chan struct{}
	once sync.Once

	rmu  sync.Mutex // серіалізує Read
	rbuf []byte

	dmu  sync.Mutex
	rdl  time.Time
	wdl  time.Time
	wake chan struct{}
}

// New створює Conn. send відправляє один шматок (≤ chunk байтів) співрозмовнику peer;
// onClose (необов'язково) викликається рівно один раз при закритті.
func New(tunnel, peer string, chunk int, send func(tunnel, to string, data []byte) error, onClose func(*Conn)) *Conn {
	if chunk <= 0 {
		chunk = 16 * 1024
	}
	return &Conn{
		tunnel: tunnel, peer: peer, send: send, chunk: chunk, onClose: onClose,
		in: make(chan []byte, inQueue), done: make(chan struct{}), wake: make(chan struct{}, 1),
	}
}

func (c *Conn) Tunnel() string { return c.tunnel }
func (c *Conn) Peer() string   { return c.peer }

// Push віддає читачеві вхідний шматок. false — Conn закрито (або переповнено й тому закрито).
func (c *Conn) Push(data []byte) bool {
	select {
	case <-c.done:
		return false
	default:
	}
	b := append([]byte(nil), data...)
	select {
	case c.in <- b:
		return true
	default:
		_ = c.Close()
		return false
	}
}

func (c *Conn) deadlines() (r, w time.Time) {
	c.dmu.Lock()
	defer c.dmu.Unlock()
	return c.rdl, c.wdl
}

func (c *Conn) Read(p []byte) (int, error) {
	c.rmu.Lock()
	defer c.rmu.Unlock()
	for {
		if len(c.rbuf) > 0 {
			n := copy(p, c.rbuf)
			c.rbuf = c.rbuf[n:]
			return n, nil
		}
		if len(p) == 0 {
			return 0, nil
		}
		rdl, _ := c.deadlines()
		var tc <-chan time.Time
		var t *time.Timer
		if !rdl.IsZero() {
			d := time.Until(rdl)
			if d <= 0 {
				return 0, os.ErrDeadlineExceeded
			}
			t = time.NewTimer(d)
			tc = t.C
		}
		stop := func() {
			if t != nil {
				t.Stop()
			}
		}
		select {
		case b := <-c.in:
			stop()
			c.rbuf = b
		case <-c.done:
			stop()
			select { // що встигло прийти до закриття — віддаємо
			case b := <-c.in:
				c.rbuf = b
			default:
				return 0, io.EOF
			}
		case <-tc:
			return 0, os.ErrDeadlineExceeded
		case <-c.wake:
			stop() // змінився дедлайн — перерахувати
		}
	}
}

func (c *Conn) Write(p []byte) (int, error) {
	written := 0
	for len(p) > 0 {
		select {
		case <-c.done:
			return written, net.ErrClosed
		default:
		}
		if _, wdl := c.deadlines(); !wdl.IsZero() && !time.Now().Before(wdl) {
			return written, os.ErrDeadlineExceeded
		}
		n := len(p)
		if n > c.chunk {
			n = c.chunk
		}
		if err := c.send(c.tunnel, c.peer, p[:n]); err != nil {
			return written, err
		}
		written += n
		p = p[n:]
	}
	return written, nil
}

func (c *Conn) Close() error {
	c.once.Do(func() {
		close(c.done)
		if c.onClose != nil {
			c.onClose(c)
		}
	})
	return nil
}

func (c *Conn) LocalAddr() net.Addr  { return addr("local:" + c.tunnel) }
func (c *Conn) RemoteAddr() net.Addr { return addr(c.peer + ":" + c.tunnel) }

func (c *Conn) SetDeadline(t time.Time) error {
	_ = c.SetWriteDeadline(t)
	return c.SetReadDeadline(t)
}

func (c *Conn) SetReadDeadline(t time.Time) error {
	c.dmu.Lock()
	c.rdl = t
	c.dmu.Unlock()
	select {
	case c.wake <- struct{}{}:
	default:
	}
	return nil
}

func (c *Conn) SetWriteDeadline(t time.Time) error {
	c.dmu.Lock()
	c.wdl = t
	c.dmu.Unlock()
	return nil
}
