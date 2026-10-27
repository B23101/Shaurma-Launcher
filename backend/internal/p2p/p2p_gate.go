// p2p_gate.go — допуск гостей до хоста за квитком вузла (режими доступу friends/accounts/list).
//
// Гість (лаунчер) перед хендшейком Minecraft пише преамбулу «SHP2P1 <квиток>\n» у тунель. Хост читає її
// ПЕРШОЮ і питає вузол POST /p2p/admit. Знати IP:порт недостатньо — квиток видано конкретному акаунту
// при join і діє 10 хв. Окремо від tinvite_gate.go (SHTI1): та сама ідея, але власна преамбула й таблиця.
//
// Хост (P2PGate): кеш відповідей, облік підключених гостей за акаунтом і періодична повторна перевірка.
// Гість (ticketStore): тримає квиток за адресою й оновлює його заздалегідь, щоб довга гра пережила
// перепідключення. Файл залежить лише від stdlib — логіку можна перевіряти без tsnet/wails.
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

const (
	p2pPreambleMagic = "SHP2P1 "
	p2pPreambleLen   = len(p2pPreambleMagic) + 64 + 1

	// p2pTicketTTL — страхова стеля життя квитка на вузлі (реально — поки живий хост/світ) (vps/internal/p2p/tickets.go, ticketTTL).
	p2pTicketTTL = 24 * time.Hour
	// p2pTicketRefreshLead — за скільки до кінця гість повторно робить join і міняє квиток.
	p2pTicketRefreshLead = 60 * time.Second
	// p2pTicketRetry — пауза перед повтором, якщо оновлення квитка не вдалось.
	p2pTicketRetry = 30 * time.Second

	// Кеш відповідей admit на хості: позитивні ≤60 с (пінги списку серверів не б'ють по вузлу),
	// негативні коротко — лише щоб одним квитком не довбили вузол.
	gateAdmitOKTTL   = 60 * time.Second
	gateAdmitDenyTTL = 10 * time.Second
	// gateRecheckEvery — як часто хост перевіряє вже підключених гостей.
	gateRecheckEvery = 30 * time.Second
	// gateRecheckMargin — запас до спливу квитка: пізніше admit із ним завжди false, тож «відмова»
	// там означала б не «доступ забрано», а «квиток протух» (і рвала б будь-яку гру на 10-й хвилині).
	// Запас більший за gateRecheckEvery і покриває розбіжність годинників.
	gateRecheckMargin = 45 * time.Second
)

var (
	reP2PPreamble = regexp.MustCompile(`^SHP2P1 ([a-f0-9]{64})\n$`)
	reP2PTicket   = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

// P2PPreamble — що гість пише в тунель перед хендшейком.
func P2PPreamble(ticket string) []byte { return []byte(p2pPreambleMagic + ticket + "\n") }

// readP2PPreamble читає рівно преамбулу (5 с) і повертає квиток. Чужий початок відхиляється одразу після
// перших байтів magic — клієнт без преамбули (звичайний Minecraft) не чекає таймауту.
func readP2PPreamble(c net.Conn) (string, *bufio.Reader, bool) {
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	defer c.SetReadDeadline(time.Time{}) //nolint:errcheck
	r := bufio.NewReaderSize(c, 512)
	buf := make([]byte, p2pPreambleLen)
	n := len(p2pPreambleMagic)
	if _, err := io.ReadFull(r, buf[:n]); err != nil || string(buf[:n]) != p2pPreambleMagic {
		return "", nil, false
	}
	if _, err := io.ReadFull(r, buf[n:]); err != nil {
		return "", nil, false
	}
	m := reP2PPreamble.FindSubmatch(buf)
	if m == nil {
		return "", nil, false
	}
	return string(m[1]), r, true
}

// ── хост ──────────────────────────────────────────────────────────────────

// AdmitFunc питає вузол про квиток (виклик admit від імені хоста).
type AdmitFunc func(ticket string) (AdmitResult, error)

// P2PGate — перевірка квитків на хості (режим доступу != all).
type P2PGate struct {
	Admit AdmitFunc
	// Mode — чинний режим доступу кімнати (friends/list/accounts): від нього залежить, що саме
	// скаже відхиленому гостю лаунчер (reject.go). nil — загальна відмова.
	Mode func() string
	now  func() time.Time // підміняється в тестах

	mu       sync.Mutex
	cache    map[string]gateVerdict
	accounts map[string]*gateAccount // акаунт гостя → його підключення
}

type gateVerdict struct {
	res   AdmitResult
	until time.Time
}

// gateAccount — підключення одного акаунта й найсвіжіший квиток, яким він проходив. Перевіряємо саме його:
// вузол тримає лише 3 останні квитки гостя, тож старий квиток цього з'єднання міг бути витіснений
// пінгами списку серверів, хоча доступ нікуди не зник.
type gateAccount struct {
	ticket string
	exp    time.Time
	conns  map[net.Conn]struct{}
}

// NewP2PGate створює гейт.
func NewP2PGate(admit AdmitFunc) *P2PGate { return &P2PGate{Admit: admit} }

func (g *P2PGate) clock() time.Time {
	if g.now != nil {
		return g.now()
	}
	return time.Now()
}

// check — рішення за квитком (з кешем). Збій вузла = «не пускати», без кешу.
func (g *P2PGate) check(ticket string) (AdmitResult, bool) {
	res, ok, _ := g.checkErr(ticket)
	return res, ok
}

// checkErr — те саме, але ще каже, чи відмова спричинена недоступністю вузла
// (тоді гостю не «вас не пускають», а «спробуйте за хвилину»).
func (g *P2PGate) checkErr(ticket string) (res AdmitResult, ok, unavailable bool) {
	now := g.clock()
	g.mu.Lock()
	if v, hit := g.cache[ticket]; hit && now.Before(v.until) {
		g.mu.Unlock()
		return v.res, v.res.OK, false
	}
	g.mu.Unlock()

	res, err := g.Admit(ticket)
	if err != nil {
		return AdmitResult{}, false, true
	}
	ttl := gateAdmitOKTTL
	if !res.OK {
		ttl = gateAdmitDenyTTL
	}
	g.mu.Lock()
	if g.cache == nil || len(g.cache) > 256 {
		g.cache = map[string]gateVerdict{}
	}
	g.cache[ticket] = gateVerdict{res: res, until: now.Add(ttl)}
	g.mu.Unlock()
	return res, res.OK, false
}

// Accept читає преамбулу гостя й питає вузол. ok=false — з'єднання треба закрити (без преамбули, чужий чи
// протухлий квиток, відмова вузла або його недоступність). Інакше повертає з'єднання для проксіювання
// (з уже прочитаним bufio) і release, який треба викликати, коли гість від'єднався.
func (g *P2PGate) Accept(c net.Conn) (conn net.Conn, release func(), ok bool) {
	conn, release, ok, _ = g.AcceptReason(c)
	return conn, release, ok
}

// AcceptReason — Accept, що ще й каже, ЧОМУ відмовлено. deny != RejNone лише коли гість пред'явив
// коректну преамбулу (отже знає протокол і зможе прочитати відповідь): хост тоді надсилає причину
// кодом (SendHostReject). Без преамбули або з сміттям — тиша, як і раніше: сканерам і чужим клієнтам
// нічого не відповідаємо.
func (g *P2PGate) AcceptReason(c net.Conn) (conn net.Conn, release func(), ok bool, deny RejectReason) {
	ticket, r, pok := readP2PPreamble(c)
	if !pok {
		return nil, nil, false, RejNone
	}
	res, allowed, unavailable := g.checkErr(ticket)
	if !allowed {
		if unavailable {
			return nil, nil, false, RejCheckUnavailable
		}
		mode := ""
		if g.Mode != nil {
			mode = g.Mode()
		}
		return nil, nil, false, rejectForMode(mode)
	}
	return &bufConn{Conn: c, r: r}, g.track(res, ticket, c), true, RejNone
}

func (g *P2PGate) track(res AdmitResult, ticket string, c net.Conn) func() {
	if res.AccountID == "" {
		return func() {}
	}
	id := res.AccountID
	exp := time.UnixMilli(res.ExpiresAt)
	g.mu.Lock()
	if g.accounts == nil {
		g.accounts = map[string]*gateAccount{}
	}
	a := g.accounts[id]
	if a == nil {
		a = &gateAccount{conns: map[net.Conn]struct{}{}}
		g.accounts[id] = a
	}
	if a.ticket == "" || exp.After(a.exp) {
		a.ticket, a.exp = ticket, exp
	}
	a.conns[c] = struct{}{}
	g.mu.Unlock()
	return func() {
		g.mu.Lock()
		if cur := g.accounts[id]; cur != nil {
			delete(cur.conns, c)
			if len(cur.conns) == 0 {
				delete(g.accounts, id)
			}
		}
		g.mu.Unlock()
	}
}

// Recheck повторно питає вузол про вже підключених гостей і рве з'єднання тих, кому відмовлено (розірвана
// дружба, чорний список, зміна режиму). Акаунти, чий квиток уже спливає, пропускаємо (див. gateRecheckMargin),
// як і збої вузла — мережеве блимання не має виганяти гравців.
func (g *P2PGate) Recheck() {
	now := g.clock()
	type job struct{ id, ticket string }
	var jobs []job
	g.mu.Lock()
	for id, a := range g.accounts {
		if len(a.conns) > 0 && now.Before(a.exp.Add(-gateRecheckMargin)) {
			jobs = append(jobs, job{id, a.ticket})
		}
	}
	g.mu.Unlock()

	for _, j := range jobs {
		res, err := g.Admit(j.ticket)
		if err != nil || res.OK {
			continue
		}
		g.mu.Lock()
		a := g.accounts[j.id]
		if a == nil || a.ticket != j.ticket { // за цей час з'явився новіший квиток — рішення на наступному проході
			g.mu.Unlock()
			continue
		}
		conns := make([]net.Conn, 0, len(a.conns))
		for c := range a.conns {
			conns = append(conns, c)
		}
		delete(g.accounts, j.id)
		delete(g.cache, j.ticket)
		g.mu.Unlock()
		for _, c := range conns {
			_ = c.Close()
		}
	}
}

// ── гість ─────────────────────────────────────────────────────────────────

// ticketStore — квитки допуску гостя за адресою кімнати (з відповіді join). Квиток оновлюється заздалегідь, поки
// кімната жива, щоб преамбула нового з'єднання (перепідключення посеред довгої гри) не була простроченою.
type ticketStore struct {
	mu sync.Mutex
	m  map[string]*guestTicket

	refresh func(address string) error // повторний join; успіх сам кличе remember з новим квитком
	alive   func(address string) bool  // кімната ще жива (інакше квиток не оновлюємо, а забуваємо)
	stopped func() bool                // сервіс зупинено

	ttl, lead, retry time.Duration // підміняються в тестах
}

type guestTicket struct {
	address string
	pre     []byte
	until   time.Time
	timer   *time.Timer
}

func newTicketStore() *ticketStore {
	return &ticketStore{
		m:   map[string]*guestTicket{},
		ttl: p2pTicketTTL, lead: p2pTicketRefreshLead, retry: p2pTicketRetry,
	}
}

func ticketKey(address string) string { return strings.ToLower(strings.TrimSpace(address)) }

// remember зберігає квиток із відповіді join і планує його оновлення. Порожній або некоректний квиток
// (режим all, відповідь без квитка) прибирає збережений: преамбула в гру без гейта зламала б хендшейк.
func (s *ticketStore) remember(address, ticket string, expiresAtMs int64) {
	if s == nil {
		return
	}
	key := ticketKey(address)
	s.mu.Lock()
	defer s.mu.Unlock()
	if old := s.m[key]; old != nil && old.timer != nil {
		old.timer.Stop()
	}
	if !reP2PTicket.MatchString(ticket) {
		delete(s.m, key)
		return
	}
	now := time.Now()
	// Життя рахуємо від моменту отримання (годинники гостя й вузла можуть розходитись); ticketExpiresAt лише
	// скорочує його, коли вузол каже, що квиток спливе раніше.
	life := s.ttl
	if expiresAtMs > 0 {
		if d := time.UnixMilli(expiresAtMs).Sub(now); d > 0 && d < life {
			life = d
		}
	}
	t := &guestTicket{address: address, pre: P2PPreamble(ticket), until: now.Add(life)}
	delay := life - s.lead
	if delay < s.retry {
		delay = s.retry
	}
	t.timer = time.AfterFunc(delay, func() { s.refreshOne(key, t) })
	s.m[key] = t
}

// get — преамбула з чинним квитком або nil (немає чи прострочений: краще не слати нічого, ніж чужий формат).
func (s *ticketStore) get(address string) []byte {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if t := s.m[ticketKey(address)]; t != nil && time.Now().Before(t.until) {
		return t.pre
	}
	return nil
}

func (s *ticketStore) refreshOne(key string, t *guestTicket) {
	s.mu.Lock()
	cur := s.m[key]
	s.mu.Unlock()
	if cur != t {
		return // квиток уже замінено
	}
	if (s.stopped != nil && s.stopped()) || (s.alive != nil && !s.alive(t.address)) {
		s.mu.Lock()
		if s.m[key] == t {
			delete(s.m, key)
		}
		s.mu.Unlock()
		return
	}
	if s.refresh != nil && s.refresh(t.address) == nil {
		return
	}
	s.mu.Lock()
	if s.m[key] == t {
		t.timer = time.AfterFunc(s.retry, func() { s.refreshOne(key, t) })
	}
	s.mu.Unlock()
}

// stopAll зупиняє оновлення й забуває квитки (зупинка сервісу).
func (s *ticketStore) stopAll() {
	if s == nil {
		return
	}
	s.mu.Lock()
	for k, t := range s.m {
		if t.timer != nil {
			t.timer.Stop()
		}
		delete(s.m, k)
	}
	s.mu.Unlock()
}
