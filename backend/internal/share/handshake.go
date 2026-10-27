package share

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

// P2P-вхід гостя (127.0.0.2:25565) маршрутизує з'єднання за ДОМЕНОМ із першого
// пакета — Minecraft Handshake. Тож одержувач відкриває тунель, написавши
// справжній хендшейк із адресою кімнати (WriteHandshake), а відправник, куди
// хост-проксі ПЕРЕГРАЄ той самий пакет перед нашими даними, мусить його
// проковтнути (skipConn).

const handshakeMaxFrame = 2048

func putVarint(b []byte, v uint32) []byte {
	for {
		c := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			b = append(b, c|0x80)
		} else {
			return append(b, c)
		}
	}
}

// WriteHandshake пише Minecraft Handshake (id 0x00): версія протоколу, адреса,
// порт, наступний стан. state=1 (status) не займає слот кімнати гостьового
// сервісу — для передачі це потрібно, щоб не блокувати вхід у гру.
func WriteHandshake(w io.Writer, serverAddress string, state int) error {
	var p []byte
	p = putVarint(p, 0x00)
	p = putVarint(p, 763)
	p = putVarint(p, uint32(len(serverAddress)))
	p = append(p, serverAddress...)
	p = append(p, 0x63, 0xDD) // 25565
	p = putVarint(p, uint32(state))
	frame := putVarint(nil, uint32(len(p)))
	frame = append(frame, p...)
	_, err := w.Write(frame)
	return err
}

// DialMC відкриває з'єднання до P2P-входу гостя й одразу пише хендшейк.
func DialMC(ctx context.Context, gateway, serverAddress string, state int) (net.Conn, error) {
	var d net.Dialer
	d.Timeout = 10 * time.Second
	c, err := d.DialContext(ctx, "tcp", gateway)
	if err != nil {
		return nil, err
	}
	if err := WriteHandshake(c, serverAddress, state); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

// skipListener загортає кожне прийняте з'єднання в skipConn.
type skipListener struct{ net.Listener }

func (l skipListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &skipConn{Conn: c, br: bufio.NewReaderSize(c, 16<<10)}, nil
}

// skipConn при першому читанні відкидає один Minecraft-фрейм (хендшейк).
type skipConn struct {
	net.Conn
	br   *bufio.Reader
	once sync.Once
	err  error
}

var errBadHandshake = errors.New("share: перший пакет не є хендшейком")

func (c *skipConn) skip() {
	_ = c.Conn.SetReadDeadline(time.Now().Add(8 * time.Second))
	defer c.Conn.SetReadDeadline(time.Time{}) //nolint:errcheck
	var n uint32
	for i := 0; i < 5; i++ {
		b, err := c.br.ReadByte()
		if err != nil {
			c.err = err
			return
		}
		n |= uint32(b&0x7f) << (7 * i)
		if b&0x80 == 0 {
			break
		}
		if i == 4 {
			c.err = errBadHandshake
			return
		}
	}
	if n == 0 || n > handshakeMaxFrame {
		c.err = errBadHandshake
		return
	}
	if _, err := io.CopyN(io.Discard, c.br, int64(n)); err != nil {
		c.err = err
	}
}

func (c *skipConn) Read(p []byte) (int, error) {
	c.once.Do(c.skip)
	if c.err != nil {
		return 0, c.err
	}
	return c.br.Read(p)
}

func (c *skipConn) CloseWrite() error {
	if cw, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite()
	}
	return nil
}
