// pipe_tune.go — згладжування P2P-тунелю: менше мікро-пауз і стрибків пінгу.
//
// Звідки бралися стрибки:
//  1. io.Copy через tapWriter не вміє ReaderFrom → на КОЖНЕ з'єднання й КОЖЕН
//     напрямок виділявся свіжий 32 КБ буфер. Один гість = десятки КБ сміття,
//     GC частіше зупиняє світ — пінг «пилкою». Тепер буфери беруться з пулу.
//  2. На сокетах не було явного TCP_NODELAY/keepalive: дрібні Minecraft-пакети
//     (рух, чат, keep-alive) могли злипатись алгоритмом Nagle.
//  3. Перші пакети нового тунелю йдуть через DERP-релей, а перехід на пряму
//     лінію (direct) дає різкий стрибок затримки. warmDirectPath одразу після
//     діалу розсилає disco-пінги й підштовхує вузол швидше знайти прямий шлях.
package p2p

import (
	"io"
	"net"
	"net/netip"
	"sync"
	"time"

	"shaurma-launcher-backend/internal/crashguard"
	"shaurma-launcher-backend/internal/launchlog"

	"tailscale.com/tsnet"
)

const (
	pipeBufSize     = 32 << 10
	tcpKeepAliveGap = 20 * time.Second

	// pipeHalfCloseGrace — скільки чекаємо другий напрямок після того, як перший закінчився.
	// Minecraft не користується напівзакритими з'єднаннями, тож усе довше — зависання.
	pipeHalfCloseGrace = 8 * time.Second
)

var pipeBufPool = sync.Pool{New: func() any {
	b := make([]byte, pipeBufSize)
	return &b
}}

// copyPooled — io.Copy з буфером із пулу. Пише одразу, як лише щось прочитано
// (без накопичення), тож затримка мінімальна.
func copyPooled(dst io.Writer, src io.Reader) (written int64, err error) {
	bp := pipeBufPool.Get().(*[]byte)
	defer pipeBufPool.Put(bp)
	buf := *bp
	for {
		nr, er := src.Read(buf)
		if nr > 0 {
			nw, ew := dst.Write(buf[:nr])
			if nw > 0 {
				written += int64(nw)
			}
			if ew != nil {
				return written, ew
			}
			if nw != nr {
				return written, io.ErrShortWrite
			}
		}
		if er != nil {
			if er != io.EOF {
				err = er
			}
			return written, err
		}
	}
}

// tuneConn вмикає TCP_NODELAY і keepalive там, де з'єднання — звичайний TCP.
// Для обгорток (tsnet/gonet тощо) мовчки нічого не робить.
func tuneConn(c net.Conn) {
	tc, ok := c.(*net.TCPConn)
	if !ok {
		return
	}
	_ = tc.SetNoDelay(true)
	_ = tc.SetKeepAlive(true)
	_ = tc.SetKeepAlivePeriod(tcpKeepAliveGap)
}

var (
	warmMu   sync.Mutex
	warmLast = map[string]time.Time{}
)

// warmDirectPath у фоні шле кілька disco-пінгів до хоста, щоб tsnet швидше
// перейшов із DERP на пряму лінію ще ДО того, як гравець почне грати. Не частіше
// ніж раз на 45 с на хост, помилки ігноруються (це лише прискорювач).
func warmDirectPath(ts *tsnet.Server, hostIP, tag string) {
	if ts == nil || hostIP == "" {
		return
	}
	if _, err := netip.ParseAddr(hostIP); err != nil {
		return
	}
	warmMu.Lock()
	if t, ok := warmLast[hostIP]; ok && time.Since(t) < 45*time.Second {
		warmMu.Unlock()
		return
	}
	warmLast[hostIP] = time.Now()
	warmMu.Unlock()

	go func() {
		defer crashguard.Recover("warmDirectPath")
		var last int
		for i := 0; i < 4; i++ {
			_, ms := pingPeer(ts, hostIP, 3*time.Second)
			if ms > 0 {
				last = ms
			}
			time.Sleep(600 * time.Millisecond)
		}
		launchlog.Debug(compGuest, "[%s] прогрів шляху до %s: останній пінг %dms", tag, hostIP, last)
	}()
}
