// disabled_notice.go — автовідповідач на вході P2P, коли гравець сам вимкнув
// P2P у лаунчері.
//
// Без нього адреса <код>.play.… резолвиться на 127.0.0.2:25565, де ніхто не
// слухає, і гра показує голе «Connection refused». Тепер на цьому ж вході
// працює крихітний слухач без мережі, tsnet і VPS: він відповідає грі
// поясненням (MOTD у списку серверів, екран відмови при вході), а на спробу
// входу кличе onLogin — лаунчер пропонує увімкнути P2P.
package p2p

import (
	"bufio"
	"net"
	"sync/atomic"
	"time"

	"shaurma-launcher-backend/internal/crashguard"
)

// disabledHitGap — не частіше одного сигналу в лаунчер на цей проміжок
// (гра може ретраїти вхід, а вікно в лаунчері досить показати раз).
const disabledHitGap = 5 * time.Second

type disabledNotice struct {
	ln         net.Listener
	playSuffix string
	onLogin    func(address string)
	lastHit    atomic.Int64
}

func startDisabledNotice(playSuffix string, onLogin func(address string)) (*disabledNotice, error) {
	ln, err := net.Listen("tcp", guestExternalAddr)
	if err != nil {
		return nil, err
	}
	d := &disabledNotice{ln: ln, playSuffix: playSuffix, onLogin: onLogin}
	go d.loop()
	return d, nil
}

func (d *disabledNotice) loop() {
	for {
		c, err := d.ln.Accept()
		if err != nil {
			return // слухач закрито
		}
		go func() {
			defer crashguard.Recover("p2p.disabledNotice")
			d.serve(c)
		}()
	}
}

func (d *disabledNotice) serve(c net.Conn) {
	defer c.Close()
	_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
	br := bufio.NewReader(c)
	hs, err := ReadHandshake(br)
	if err != nil {
		return // не Minecraft
	}
	_ = c.SetReadDeadline(time.Time{})
	RejectClient(&bufConn{Conn: c, r: br}, hs.ProtocolVersion, hs.NextState, RejP2PDisabled, false)
	if hs.NextState == 1 || d.onLogin == nil {
		return // пінг списку серверів: досить MOTD, лаунчер не смикаємо
	}
	now := time.Now().UnixMilli()
	prev := d.lastHit.Load()
	if now-prev < disabledHitGap.Milliseconds() || !d.lastHit.CompareAndSwap(prev, now) {
		return
	}
	addr, _ := hs.Address(d.playSuffix)
	d.onLogin(addr)
}

func (d *disabledNotice) stop() {
	if d != nil && d.ln != nil {
		_ = d.ln.Close()
	}
}
