// udp.go — UDP-тунель для ГОЛОСОВИХ ЧАТІВ через P2P (галочка в лаунчері).
//
// ── НАВІЩО ────────────────────────────────────────────────────────────
// Сам Minecraft спілкується TCP — цього досить для гри. Голосові моди
// (Simple Voice Chat, Plasmo Voice) ходять ОКРЕМИМ UDP-портом (SVC — 24454
// за замовчуванням). Без UDP-тунелю голос у P2P-кімнаті просто не працює.
//
// ── ЯК ────────────────────────────────────────────────────────────────
// Той самий підхід, що й для TCP, але без «хендшейку» (у UDP його немає):
//
//	Хост:  tailnet : <voicePort>  →  127.0.0.1 : <voicePort> (гра)
//	Гість: 127.0.0.1 : <voicePort> →  tailnet : <hostIP> : <voicePort>
//
// Кожне ДЖЕРЕЛО отримує власний сокет-«сесію» (map у udpRelay), бо голос
// двосторонній: відповідь сервера мусить повернутись саме тому гравцю, чиї
// пакети ми переслали. Сесія сама закривається, якщо мовчить довше
// udpPeerIdleTimeout — інакше map ріс би безмежно на кожен новий порт
// джерела (це і є основний захист від вичерпання ресурсів).
//
// ── БЕЗПЕКА (і чому в UI є попередження) ──────────────────────────────
//   - Слухаємо ЛИШЕ на tailnet-адресі ноди (хост) і ЛИШЕ на 127.0.0.1
//     (гість): назовні в інтернет нічого не відкривається.
//   - Форвардимо ЛИШЕ на 127.0.0.1 (хост) — цей код не вміє відправляти
//     пакети на довільні адреси, тож використати його як UDP-релей в
//     інтернет неможливо навіть при помилці в конфігурації.
//   - Але UDP-порт усередині Headscale-мережі видно ВСІМ її нодам (не лише
//     тому, хто знає адресу-«PIN», як для TCP). Тому фіча вимкнена за
//     замовчуванням, і в UI є чесне попередження: вмикати лише на час гри
//     з людьми, яким довіряєш.
package p2p

import (
	"shaurma-launcher-backend/internal/crashguard"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"shaurma-launcher-backend/internal/launchlog"
)

// udpPeerIdleTimeout — скільки «мовчання» тримаємо UDP-сесію джерела.
// Голосові моди шлють пакети постійно, поки з'єднання живе, тож тихі
// сесії — це покинуті клієнти.
const udpPeerIdleTimeout = 30 * time.Second

// udpDatagramSize — більше за типовий MTU не має сенсу (фрагментація тільки
// зашкодила б голосу), решту пакета відкидаємо.
const udpDatagramSize = 2048

type udpPeer struct {
	conn net.Conn
	last time.Time

	// Діагностика: скільки пакетів/байт пішло в кожен бік і коли вперше
	// прийшла відповідь. «Вийшло N пакетів, повернулось 0» — класична
	// ознака того, що голосовий мод на іншому кінці нічого не слухає.
	created           time.Time
	outPkts, outBytes atomic.Int64
	inPkts, inBytes   atomic.Int64
	firstOutNano      atomic.Int64
	firstInNano       atomic.Int64
}

// udpRelay — набір «сесій» (по одній на джерело пакетів).
type udpRelay struct {
	mu     sync.Mutex
	peers  map[string]*udpPeer
	closed bool

	// label — хто власник релея, для логів ("host h3", "guest prlool-482917").
	label string
	// writeErrGate обмежує повторювані помилки запису (голос шле ~50 пак/с).
	writeErrGate *logGate

	// Сумарні лічильники за весь час життя релея (не зникають із закритими сесіями) —
	// з них UI показує «голос реально ходить / мод не відповідає».
	totalOut, totalIn atomic.Int64
	totalSessions     atomic.Int64
}

func newUDPRelay() *udpRelay {
	return &udpRelay{peers: make(map[string]*udpPeer), label: "?", writeErrGate: newLogGate(5 * time.Second)}
}

// withLabel задає підпис для логів і повертає той самий релей.
func (r *udpRelay) withLabel(label string) *udpRelay {
	r.label = label
	return r
}

// forward пересилає один датаграм від src на ціль (dial створює з'єднання)
// і повертає відповіді з цієї цілі назад до src через pc.
//
// pc — «зовнішній» сокет (tailnet у хоста / локальний слухач у гостя),
// dial — лінивий коннектор до протилежного боку. Помилки не валять
// з'єднання: це UDP, тут втрата пакета — норма.
func (r *udpRelay) forward(pc net.PacketConn, src net.Addr, payload []byte, dial func() (net.Conn, error)) {
	if r.isClosed() {
		return
	}
	key := src.String()

	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	peer := r.peers[key]
	r.mu.Unlock()

	if peer == nil {
		dsw := startWatch()
		conn, err := dial()
		if err != nil {
			launchlog.Error(compUDP, "[%s] не вдалося відкрити сесію для %s (%s): %v", r.label, key, dsw, err)
			return
		}
		peer = &udpPeer{conn: conn, last: time.Now(), created: time.Now()}
		r.mu.Lock()
		if r.closed {
			r.mu.Unlock()
			_ = conn.Close()
			return
		}
		// Перегони: інша горутина могла встигнути створити сесію для того
		// самого джерела — тоді лишаємо одну, другу закриваємо.
		if existing := r.peers[key]; existing != nil {
			r.mu.Unlock()
			_ = conn.Close()
			peer = existing
		} else {
			r.peers[key] = peer
			r.mu.Unlock()
			r.totalSessions.Add(1)
			launchlog.Info(compUDP, "[%s] нова UDP-сесія для %s: %s → %s (діал %s, сесій тепер: %d)",
				r.label, key, addrOf(conn.LocalAddr()), addrOf(conn.RemoteAddr()), dsw, r.sessionCount())
			{
				_f, _a0, _a1, _a2, _a3 := r.readReplies, pc, src, peer, key
				go func() {
					defer crashguard.Recover("r.readReplies")
					_f(_a0, _a1, _a2, _a3)
				}()
			}
		}
	}

	peer.last = time.Now()
	if _, err := peer.conn.Write(payload); err != nil {
		if ok, skipped := r.writeErrGate.allow(); ok {
			launchlog.Error(compUDP, "[%s] запис у сесію %s: %v (проковтнуто схожих: %d)", r.label, key, err, skipped)
		}
		r.drop(key, peer, "помилка запису: "+err.Error())
		return
	}
	peer.outPkts.Add(1)
	peer.outBytes.Add(int64(len(payload)))
	r.totalOut.Add(1)
	peer.firstOutNano.CompareAndSwap(0, time.Now().UnixNano())
}

// readReplies читає відповіді з сесії та повертає їх джерелу через pc.
// Сесія живе, поки приходять відповіді; тиша довше udpPeerIdleTimeout —
// закриваємо (наступний пакет створить нову).
func (r *udpRelay) readReplies(pc net.PacketConn, src net.Addr, peer *udpPeer, key string) {
	buf := make([]byte, udpDatagramSize)
	for {
		_ = peer.conn.SetReadDeadline(time.Now().Add(udpPeerIdleTimeout))
		n, err := peer.conn.Read(buf)
		if err != nil {
			reason := "помилка читання: " + err.Error()
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				reason = "тиша " + fmtDur(udpPeerIdleTimeout) + " (простій)"
			}
			r.drop(key, peer, reason)
			return
		}
		if r.isClosed() {
			r.drop(key, peer, "релей закрито")
			return
		}
		peer.last = time.Now()
		peer.inPkts.Add(1)
		peer.inBytes.Add(int64(n))
		r.totalIn.Add(1)
		if peer.firstInNano.CompareAndSwap(0, time.Now().UnixNano()) {
			var after time.Duration
			if f := peer.firstOutNano.Load(); f != 0 {
				after = time.Duration(time.Now().UnixNano() - f)
			}
			launchlog.Info(compUDP, "[%s] ПЕРША відповідь для %s через %s після першого пакета (%d Б) — двосторонній UDP-шлях працює", r.label, key, fmtDur(after), n)
		}
		if _, err := pc.WriteTo(buf[:n], src); err != nil {
			r.drop(key, peer, "помилка відправки джерелу: "+err.Error())
			return
		}
	}
}

func (r *udpRelay) drop(key string, peer *udpPeer, reason string) {
	r.mu.Lock()
	removed := false
	if r.peers[key] == peer {
		delete(r.peers, key)
		removed = true
	}
	left := len(r.peers)
	r.mu.Unlock()
	_ = peer.conn.Close()
	if !removed {
		return // уже закрита іншою горутиною — не дублюємо підсумок
	}
	in, out := peer.inPkts.Load(), peer.outPkts.Load()
	lvl := launchlog.Info
	warn := ""
	if out > 0 && in == 0 {
		lvl = launchlog.Warn
		warn = "  ⚠ відповідей НЕ БУЛО: на тому кінці ніхто не слухає UDP (голосовий мод не запущений / інший порт / файрвол)"
	}
	lvl(compUDP, "[%s] UDP-сесія %s закрита: %s | жила %s | →%d пак/%s ←%d пак/%s | лишилось сесій: %d%s",
		r.label, key, reason, fmtDur(time.Since(peer.created)),
		out, fmtBytes(peer.outBytes.Load()), in, fmtBytes(peer.inBytes.Load()), left, warn)
}

// UDPStats — підсумок роботи UDP-релея для статусу P2P.
type UDPStats struct {
	// Sessions — скільки джерел (гостей) відкривали голосову сесію за весь час.
	Sessions int64 `json:"sessions"`
	// Active — скільки сесій живе зараз.
	Active int `json:"active"`
	// PktsOut — пакети від гостей у локальний голосовий мод; PktsIn — відповіді мода назад.
	PktsOut int64 `json:"pktsOut"`
	PktsIn  int64 `json:"pktsIn"`
}

// stats повертає знімок лічильників (безпечно для nil).
func (r *udpRelay) stats() UDPStats {
	if r == nil {
		return UDPStats{}
	}
	return UDPStats{Sessions: r.totalSessions.Load(), Active: r.sessionCount(), PktsOut: r.totalOut.Load(), PktsIn: r.totalIn.Load()}
}

func (r *udpRelay) sessionCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.peers)
}

func addrOf(a net.Addr) string {
	if a == nil {
		return "?"
	}
	return a.String()
}

func (r *udpRelay) isClosed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closed
}

// Close закриває всі UDP-сесії (викликається при Stop хостингу/сервісу).
func (r *udpRelay) Close() {
	r.mu.Lock()
	r.closed = true
	peers := r.peers
	r.peers = make(map[string]*udpPeer)
	r.mu.Unlock()
	if len(peers) > 0 {
		launchlog.Debug(compUDP, "[%s] релей закрито, обрізаю %d активних UDP-сесій", r.label, len(peers))
	}
	for _, p := range peers {
		_ = p.conn.Close()
	}
}
