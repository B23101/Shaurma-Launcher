// tinvite_gate.go — доступ до тимчасового запрошення лише за квитком від VPS.
//
// Гість (лаунчер) перед хендшейком Minecraft пише преамбулу «SHTI1 <id> <квиток>\n» у тунель.
// Хост читає її ПЕРШОЮ і питає VPS (admit), чи цей квиток видано запрошеному другові для
// цього запрошення. Без преамбули, з чужим/простроченим квитком або після закриття — обрив.
// Знати адресу недостатньо: адресу можна переслати, квиток — ні (він прив'язаний до акаунта).
package p2p

import (
	"bufio"
	"io"
	"net"
	"regexp"
	"strings"
	"sync"
	"time"
)

const preambleLen = 6 + 32 + 1 + 64 + 1

var rePreamble = regexp.MustCompile(`^SHTI1 ([a-f0-9]{32}) ([a-f0-9]{64})\n$`)

// HostGate — перевірка квитка на хості (true = пустити).
type HostGate struct {
	InviteID string
	Admit    func(id, ticket string) bool

	mu    sync.Mutex
	cache map[string]time.Time // квиток → до коли довіряємо без повторного admit
}

func (g *HostGate) allow(ticket string) bool {
	g.mu.Lock()
	if g.cache == nil {
		g.cache = map[string]time.Time{}
	}
	if until, ok := g.cache[ticket]; ok && time.Now().Before(until) {
		g.mu.Unlock()
		return true
	}
	g.mu.Unlock()
	if !g.Admit(g.InviteID, ticket) {
		return false
	}
	g.mu.Lock()
	if len(g.cache) > 256 {
		g.cache = map[string]time.Time{}
	}
	g.cache[ticket] = time.Now().Add(2 * time.Minute) // пінги списку серверів не б'ють по VPS
	g.mu.Unlock()
	return true
}

// readPreamble читає рівно преамбулу (5 с) і повертає квиток.
func readPreamble(c net.Conn, inviteID string) (string, *bufio.Reader, bool) {
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	defer c.SetReadDeadline(time.Time{}) //nolint:errcheck
	r := bufio.NewReaderSize(c, 512)
	buf := make([]byte, preambleLen)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", nil, false
	}
	m := rePreamble.FindSubmatch(buf)
	if m == nil || string(m[1]) != inviteID {
		return "", nil, false
	}
	return string(m[2]), r, true
}

// Preamble — що гість пише в тунель перед хендшейком.
func Preamble(id, ticket string) []byte { return []byte("SHTI1 " + id + " " + ticket + "\n") }

// bufConn — з'єднання, з якого вже частково прочитано у bufio (дочитуємо звідти).
type bufConn struct {
	net.Conn
	r *bufio.Reader
}

func (b *bufConn) Read(p []byte) (int, error) { return b.r.Read(p) }

// CloseWrite — передаємо напівзакриття нижньому з'єднанню. Без цього обгортка ховала
// CloseWrite, і коли гра/сервер закривались першими, гість не отримував FIN: тунель
// висів до таймауту, а кімната лишалась «зайнятою» (вхід знову — лише після рестарту).
func (b *bufConn) CloseWrite() error {
	if cw, ok := b.Conn.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite()
	}
	return nil
}

// SetHostGate вмикає доступ лише за квитком для хост-сесії subdomain (nil — вимкнути).
func (a *API) SetHostGate(subdomain string, g *HostGate) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	h, ok := a.hosts[strings.ToLower(strings.TrimSpace(subdomain))]
	if !ok {
		return false
	}
	h.gate.Store(g)
	return true
}

// SetGuestTicket — гість: для адреси addr додавати преамбулу з квитком (порожній id — прибрати).
func (a *API) SetGuestTicket(address, id, ticket string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.ensureGuestLocked(); err != nil {
		return err
	}
	key := strings.ToLower(strings.TrimSpace(address))
	if id == "" {
		a.guest.tickets.Delete(key)
		return nil
	}
	a.guest.tickets.Store(key, Preamble(id, ticket))
	return nil
}

func (g *GuestService) preambleFor(address string) []byte {
	if v, ok := g.tickets.Load(strings.ToLower(strings.TrimSpace(address))); ok {
		return v.([]byte) // тимчасове запрошення (SHTI1) має пріоритет
	}
	return g.p2pTickets.get(address) // квиток режиму доступу кімнати (SHP2P1, p2p_gate.go)
}

// withPreamble — нова послідовність «преамбула + хендшейк» (без зміни hs.Raw).
func withPreamble(pre, raw []byte) []byte {
	if len(pre) == 0 {
		return raw
	}
	out := make([]byte, 0, len(pre)+len(raw))
	return append(append(out, pre...), raw...)
}
