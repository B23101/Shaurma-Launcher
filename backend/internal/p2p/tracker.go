// tracker.go — облік гравців, які реально приєднались до хост-сесії.
//
// ── НАВІЩО ────────────────────────────────────────────────────────────
// Гравець, який хостить P2P-сервер, має бачити, ХТО до нього зайшов і який
// у нього пінг — інакше це «чорна скринька»: незрозуміло, чи тунель узагалі
// працює, чи хтось зайшов, чи це зависле з'єднання. Хостинговий проксі й так
// приймає КОЖНЕ з'єднання, тож достатньо його зареєструвати й порахувати
// трафік; пінг вимірюється окремо через tailscale Ping (див. host.go).
//
// Дані суто діагностичні: нічого не вирішують і нікуди не шлють. Тому
// трекер ніколи не повертає помилок і не блокує проксі — якщо щось пішло не
// так, гра має працювати далі, а UI просто не покаже рядок.
package p2p

import (
	"net"
	"sort"
	"sync"
	"time"
)

// HostConnection — один гість, під'єднаний до хост-сесії (для UI хоста).
type HostConnection struct {
	ID string `json:"id"`
	// GuestIP — tailnet-адреса гостя (100.64.0.0/10). За нею й рахується
	// пінг; порожня, якщо адреса не розпізналась.
	GuestIP     string `json:"guestIp,omitempty"`
	RemoteAddr  string `json:"remoteAddr"`
	ConnectedAt int64  `json:"connectedAt"` // unix ms
	LastSeen    int64  `json:"lastSeen"`    // unix ms — останній байт у будь-який бік
	// BytesIn — від гостя до хоста, BytesOut — від хоста до гостя.
	BytesIn  int64 `json:"bytesIn"`
	BytesOut int64 `json:"bytesOut"`
	// PingMs — останній виміряний мережевий RTT до гостя (0 = ще не міряли
	// або не вдалось).
	PingMs int `json:"pingMs"`
}

// connectionTracker — потокобезпечний реєстр активних гостей.
type connectionTracker struct {
	mu    sync.Mutex
	seq   int
	conns map[string]*trackedConn
}

type trackedConn struct {
	id string
	// mu захищає лічильники: Read/Write викликаються з двох горутин
	// pipeBoth одночасно.
	mu        sync.Mutex
	connected time.Time
	lastSeen  time.Time
	bytesIn   int64
	bytesOut  int64
	remote    string
	guestIP   string
	pingMs    int
}

func newConnectionTracker() *connectionTracker {
	return &connectionTracker{conns: make(map[string]*trackedConn)}
}

// add реєструє нове з'єднання і повертає його ID.
func (t *connectionTracker) add(remoteAddr string, guestIP string) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.seq++
	id := "c" + itoa(t.seq)
	now := time.Now()
	t.conns[id] = &trackedConn{
		id:        id,
		connected: now,
		lastSeen:  now,
		remote:    remoteAddr,
		guestIP:   guestIP,
	}
	return id
}

// remove прибирає з'єднання (гість від'єднався).
func (t *connectionTracker) remove(id string) {
	t.mu.Lock()
	delete(t.conns, id)
	t.mu.Unlock()
}

// setPing записує виміряний RTT для з'єднання (0 — вимір не вдався, старе
// значення не затираємо).
func (t *connectionTracker) setPing(id string, ms int) {
	if ms <= 0 {
		return
	}
	t.mu.Lock()
	c := t.conns[id]
	t.mu.Unlock()
	if c == nil {
		return
	}
	c.mu.Lock()
	c.pingMs = ms
	c.mu.Unlock()
}

// guests повертає копію списку активних з'єднань (найновіші спершу).
func (t *connectionTracker) guests() []HostConnection {
	t.mu.Lock()
	list := make([]*trackedConn, 0, len(t.conns))
	for _, c := range t.conns {
		list = append(list, c)
	}
	t.mu.Unlock()

	out := make([]HostConnection, 0, len(list))
	for _, c := range list {
		c.mu.Lock()
		out = append(out, HostConnection{
			ID:          c.id,
			GuestIP:     c.guestIP,
			RemoteAddr:  c.remote,
			ConnectedAt: c.connected.UnixMilli(),
			LastSeen:    c.lastSeen.UnixMilli(),
			BytesIn:     c.bytesIn,
			BytesOut:    c.bytesOut,
			PingMs:      c.pingMs,
		})
		c.mu.Unlock()
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ConnectedAt < out[j].ConnectedAt })
	return out
}

// wrap обгортає з'єднання «рахуючим» для конкретного гостя. Повертає false,
// якщо гостя вже прибрали з реєстру (тоді просто віддаємо оригінал).
func (t *connectionTracker) wrap(id string, conn net.Conn) (net.Conn, bool) {
	t.mu.Lock()
	tc := t.conns[id]
	t.mu.Unlock()
	if tc == nil {
		return conn, false
	}
	return &countingConn{Conn: conn, tr: tc}, true
}

// targets повертає пари (id, tailnet IP) для вимірювання пінгу.
func (t *connectionTracker) targets() [][2]string {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([][2]string, 0, len(t.conns))
	for id, c := range t.conns {
		if c.guestIP == "" {
			continue
		}
		out = append(out, [2]string{id, c.guestIP})
	}
	return out
}

// countingConn обгортає net.Conn і рахує трафік у трекері. CloseWrite
// прокидається далі — інакше io.Copy у pipeBoth (host.go) не зміг би
// коректно завершити напівзакрите з'єднання, і гра висіла б на розриві.
type countingConn struct {
	net.Conn
	tr *trackedConn
}

func (c *countingConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 {
		c.tr.mu.Lock()
		c.tr.bytesIn += int64(n)
		c.tr.lastSeen = time.Now()
		c.tr.mu.Unlock()
	}
	return n, err
}

func (c *countingConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	if n > 0 {
		c.tr.mu.Lock()
		c.tr.bytesOut += int64(n)
		c.tr.lastSeen = time.Now()
		c.tr.mu.Unlock()
	}
	return n, err
}

func (c *countingConn) CloseWrite() error {
	if cw, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite()
	}
	return nil
}

// hostOfIP витягує tailnet-адресу гостя з net.Addr з'єднання. Для з'єднань
// усередині tsnet RemoteAddr — це та сама 100.64.0.0/10 адреса вузла гостя
// (без порту), тому окремого API не потрібно.
func hostOfIP(addr net.Addr) string {
	if addr == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return addr.String()
	}
	return host
}

// itoa — мінімальна заміна strconv.Itoa, щоб не тягнути імпорт заради
// одного виклику в гарячому шляху (реєстр з'єднань).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
