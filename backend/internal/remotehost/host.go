package remotehost

import (
	"crypto/ed25519"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"shaurma-launcher-backend/internal/remotechan"
	"shaurma-launcher-backend/internal/remotectl"
	"shaurma-launcher-backend/internal/social"
)

// Crypto — E2E між лаунчерами друзів (social.ChatCrypto).
type Crypto interface {
	EncryptFor(accountID, text string) (ciphertextB64, nonceB64 string, err error)
	DecryptFrom(accountID, ciphertextB64, nonceB64 string) (string, error)
}

// Net — відправлення кадрів на VPS (social.PresenceClient).
type Net interface {
	SendCmd(reqID, ownerID, serverID, op, sid, ct, nonce string) error
	SendReply(reqID, toID, ct, nonce string) error
	SendStream(sid, toID, ct, nonce string) error
}

// Servers — що Host вміє робити з локальними серверами (адаптер над servers.Manager).
type Servers interface {
	Exists(id string) (name string, ok bool)
	State(id string) string
	Players(id string) int
	Start(id string) error
	Stop(id string) error
	SendCommand(id, cmd string) error
	Tail(id string, n int) []Line
}

// Friends — актуальний список друзів лаунчера.
type Friends interface {
	Friend(id string) (ok bool, sinceMs int64)
	Nick(id string) string
}

// HostDeps — залежності Host.
type HostDeps struct {
	Self    func() string // мій accountId
	VPSKey  func() ed25519.PublicKey
	Crypto  Crypto
	Net     Net
	Servers Servers
	Friends Friends
	Store   *remotectl.Store
	Now     func() time.Time // для тестів
	Async   func(func())     // для тестів; за замовчуванням go
	Emit    func(event string, payload any)
}

var errBadEnvelope = errors.New("remotehost: envelope too large")

const (
	maxSessions          = 6
	maxSessionsPerFriend = 2
	sessionMax           = 30 * time.Minute
	maxPending           = 500
	backlogLines         = 40
	chunksPerFlush       = 4
	chunkSoftLimit       = 1000
	maxLineRunes         = 200
	maxAuditCmd          = 120
)

type session struct {
	sid, friend, srv string
	out              func(envelope) error // куди слати потік: E2E через VPS або пряме зашифроване з'єднання
	tag              any
	expires          time.Time
	pending          []Line
	skipped          int
}

// Host — сторона власника.
type Host struct {
	d HostDeps

	mu       sync.Mutex
	seenIDs  map[string]time.Time
	sessions map[string]*session
}

// NewHost створює Host.
func NewHost(d HostDeps) *Host {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Async == nil {
		d.Async = func(f func()) { go f() }
	}
	if d.Emit == nil {
		d.Emit = func(string, any) {}
	}
	return &Host{d: d, seenIDs: map[string]time.Time{}, sessions: map[string]*session{}}
}

func capFor(op string) []byte {
	switch op {
	case social.RemoteOpStart:
		return []byte{'s'}
	case social.RemoteOpStop:
		return []byte{'x'}
	case social.RemoteOpConsoleExec:
		return []byte{'c', 'e'}
	default:
		return []byte{'c'}
	}
}

func (h *Host) audit(from, srv, op, result, cmd string) {
	name, _ := h.d.Servers.Exists(srv)
	if len(cmd) > maxAuditCmd {
		cmd = cmd[:maxAuditCmd]
	}
	h.d.Store.AddAudit(h.d.Self(), remotectl.AuditEntry{
		TS: h.d.Now().UnixMilli(), Friend: from, Nick: h.d.Friends.Nick(from), Server: srv, Name: name, Op: op, Result: result, Cmd: cmd,
	})
}

func (h *Host) seen(id string, now time.Time) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for k, t := range h.seenIDs {
		if now.Sub(t) > replayTTL {
			delete(h.seenIDs, k)
		}
	}
	if _, dup := h.seenIDs[id]; dup {
		return true
	}
	if len(h.seenIDs) > 2048 {
		h.seenIDs = map[string]time.Time{}
	}
	h.seenIDs[id] = now
	return false
}

// conduit — куди йдуть відповіді й потік: через VPS (E2E-конверти) або через пряме з'єднання.
type conduit struct {
	reply func(envelope)
	out   func(envelope) error
	tag   any // ідентифікатор каналу (для закриття його сесій); nil — шлях через VPS
}

// call — одна авторизована дія друга.
type call struct {
	h                         *Host
	from, srv, op, sid, reqID string
	cd                        conduit
}

func (c *call) reply(e envelope) {
	e.V, e.K, e.ID, e.Op, e.Srv, e.SID, e.TS = envVersion, "res", c.reqID, c.op, c.srv, c.sid, c.h.d.Now().UnixMilli()
	c.cd.reply(e)
}

func (h *Host) vpsConduit(in social.RemoteIncoming) conduit {
	enc := func(e envelope, send func(ct, n string) error) error {
		plain, ok := e.marshal()
		if !ok {
			return errBadEnvelope
		}
		ct, n, err := h.d.Crypto.EncryptFor(in.From, plain)
		if err != nil {
			return err
		}
		return send(ct, n)
	}
	return conduit{
		reply: func(e envelope) {
			_ = enc(e, func(ct, n string) error { return h.d.Net.SendReply(in.ReqID, in.From, ct, n) })
		},
		out: func(e envelope) error {
			return enc(e, func(ct, n string) error { return h.d.Net.SendStream(e.SID, in.From, ct, n) })
		},
	}
}

// Handle — обробка кадру rs_cmd_incoming. Жодної відповіді на команду, що не пройшла мандат/конверт:
// стороннім не даємо оракула.
func (h *Host) Handle(in social.RemoteIncoming) {
	now := h.d.Now()
	pub := h.d.VPSKey()
	if pub == nil {
		h.audit(in.From, in.ServerID, in.Op, "no_vps_key", "")
		return
	}
	if err := social.VerifyIncoming(pub, in.Mandate, h.d.Self(), in, now); err != nil {
		h.audit(in.From, in.ServerID, in.Op, "mandate_invalid", "")
		return
	}
	md := in.Mandate
	plain, err := h.d.Crypto.DecryptFrom(in.From, in.Ciphertext, in.Nonce)
	if err != nil {
		h.audit(in.From, md.Srv, md.Op, "bad_envelope", "")
		return
	}
	env, ok := parseEnvelope(plain, "cmd")
	// Вирішальні значення — з мандата й конверта, і вони мусять збігатися.
	if !ok || env.ID != in.ReqID || env.Op != md.Op || env.Srv != md.Srv || env.SID != md.SID || !fresh(env.TS, now) {
		h.audit(in.From, md.Srv, md.Op, "envelope_mismatch", "")
		return
	}
	h.execute(in.From, md, env, h.vpsConduit(in), now)
}

// execute — спільне ядро: повтори, права мандата, локальна політика власника, виконання.
func (h *Host) execute(from string, md *social.RemoteMandate, env envelope, cd conduit, now time.Time) {
	if h.seen(from+"|"+env.ID, now) {
		return
	}
	op, srv := env.Op, env.Srv
	c := &call{h: h, from: from, srv: srv, op: op, sid: env.SID, reqID: env.ID, cd: cd}
	if !social.ValidRemoteOp(op) {
		h.audit(from, srv, op, "envelope_mismatch", "")
		return
	}
	for _, k := range capFor(op) {
		if !md.HasCap(k) {
			h.audit(from, srv, op, remotectl.CodeNotAllowed, "")
			c.reply(envelope{Code: remotectl.CodeNotAllowed})
			return
		}
	}
	if _, exists := h.d.Servers.Exists(srv); !exists {
		h.audit(from, srv, op, remotectl.CodeNotAllowed, "")
		c.reply(envelope{Code: remotectl.CodeNotAllowed})
		return
	}
	isFriend, since := h.d.Friends.Friend(from)
	dec := h.d.Store.Authorize(h.d.Self(), from, srv, op, isFriend, since, now)
	if !dec.Allowed {
		h.audit(from, srv, op, dec.Code, "")
		c.reply(envelope{Code: dec.Code, State: h.d.Servers.State(srv)})
		return
	}
	switch op {
	case social.RemoteOpStart:
		h.doStart(c)
	case social.RemoteOpStop:
		h.doStop(c, dec.AllowStopWithPlayers)
	case social.RemoteOpConsoleOpen:
		h.doOpen(c, now)
	case social.RemoteOpConsoleExec:
		h.doExec(c, env.Cmd, dec, now)
	case social.RemoteOpConsoleClose:
		h.closeSession(c.sid, from)
		h.audit(from, srv, op, "ok", "")
		c.reply(envelope{OK: true, Code: "closed", Final: true})
	}
}

func (h *Host) doStart(c *call) {
	srv := c.srv
	switch st := h.d.Servers.State(srv); st {
	case "running", "starting":
		h.audit(c.from, srv, "start", "already_running", "")
		c.reply(envelope{OK: true, Code: "already_running", State: st, Final: true})
		return
	case "installing", "stopping":
		h.audit(c.from, srv, "start", "busy", "")
		c.reply(envelope{Code: "busy", State: st, Final: true})
		return
	}
	h.audit(c.from, srv, "start", "accepted", "")
	c.reply(envelope{OK: true, Code: "accepted", State: "starting"})
	h.d.Async(func() {
		if err := h.d.Servers.Start(srv); err != nil {
			h.audit(c.from, srv, "start", "start_failed", "")
			c.reply(envelope{Code: "start_failed", State: h.d.Servers.State(srv), Final: true})
		}
	})
}

func (h *Host) doStop(c *call, allowWithPlayers bool) {
	srv := c.srv
	st := h.d.Servers.State(srv)
	if st == "stopped" || st == "crashed" || st == "error" {
		h.audit(c.from, srv, "stop", "already_stopped", "")
		c.reply(envelope{OK: true, Code: "already_stopped", State: st, Final: true})
		return
	}
	if !allowWithPlayers && h.d.Servers.Players(srv) > 0 {
		h.audit(c.from, srv, "stop", remotectl.CodePlayers, "")
		c.reply(envelope{Code: remotectl.CodePlayers, State: st, Final: true})
		return
	}
	h.audit(c.from, srv, "stop", "accepted", "")
	c.reply(envelope{OK: true, Code: "accepted", State: "stopping"})
	h.d.Async(func() {
		if err := h.d.Servers.Stop(srv); err != nil {
			h.audit(c.from, srv, "stop", "stop_failed", "")
			c.reply(envelope{Code: "stop_failed", State: h.d.Servers.State(srv), Final: true})
		}
	})
}

func (h *Host) doOpen(c *call, now time.Time) {
	sid, srv := c.sid, c.srv
	h.mu.Lock()
	if old, ok := h.sessions[sid]; ok && (old.friend != c.from || old.srv != srv) {
		h.mu.Unlock()
		h.audit(c.from, srv, "console_open", "duplicate", "")
		c.reply(envelope{Code: "duplicate", Final: true})
		return
	}
	perFriend := 0
	for id, s := range h.sessions {
		if s.friend == c.from && id != sid {
			perFriend++
		}
	}
	if _, exists := h.sessions[sid]; !exists && (len(h.sessions) >= maxSessions || perFriend >= maxSessionsPerFriend) {
		h.mu.Unlock()
		h.audit(c.from, srv, "console_open", "busy", "")
		c.reply(envelope{Code: "busy", Final: true})
		return
	}
	s := &session{sid: sid, friend: c.from, srv: srv, out: c.cd.out, tag: c.cd.tag, expires: now.Add(sessionMax)}
	s.pending = append(s.pending, sanitizeLines(h.d.Servers.Tail(srv, backlogLines))...)
	h.sessions[sid] = s
	h.mu.Unlock()
	h.audit(c.from, srv, "console_open", "ok", "")
	c.reply(envelope{OK: true, Code: "opened", State: h.d.Servers.State(srv), Final: true})
	h.Flush()
}

func (h *Host) doExec(c *call, raw string, dec remotectl.Decision, now time.Time) {
	srv := c.srv
	h.mu.Lock()
	s := h.sessions[c.sid]
	live := s != nil && s.friend == c.from && s.srv == srv && now.Before(s.expires)
	h.mu.Unlock()
	if !live {
		h.audit(c.from, srv, "console_exec", "no_session", "")
		c.reply(envelope{Code: "no_session", Final: true})
		return
	}
	cmd, code := remotectl.CheckCommand(raw, dec.AllowPrivileged)
	if code != "" {
		h.audit(c.from, srv, "console_exec", code, cmd.Text)
		c.reply(envelope{Code: code, Final: true})
		return
	}
	if cmd.IsStop {
		// «stop» через консоль не обходить право Stop і політику гравців.
		isFriend, since := h.d.Friends.Friend(c.from)
		sd := h.d.Store.Authorize(h.d.Self(), c.from, srv, remotectl.OpStop, isFriend, since, now)
		if !sd.Allowed {
			h.audit(c.from, srv, "console_exec", sd.Code, cmd.Text)
			c.reply(envelope{Code: sd.Code, Final: true})
			return
		}
		if !sd.AllowStopWithPlayers && h.d.Servers.Players(srv) > 0 {
			h.audit(c.from, srv, "console_exec", remotectl.CodePlayers, cmd.Text)
			c.reply(envelope{Code: remotectl.CodePlayers, Final: true})
			return
		}
	}
	if err := h.d.Servers.SendCommand(srv, cmd.Text); err != nil {
		h.audit(c.from, srv, "console_exec", "exec_failed", cmd.Text)
		c.reply(envelope{Code: "exec_failed", State: h.d.Servers.State(srv), Final: true})
		return
	}
	h.audit(c.from, srv, "console_exec", "ok", cmd.Text)
	c.reply(envelope{OK: true, Code: "sent", State: h.d.Servers.State(srv), Final: true})
}

func (h *Host) closeSession(sid, friend string) {
	h.mu.Lock()
	if s := h.sessions[sid]; s != nil && s.friend == friend {
		delete(h.sessions, sid)
	}
	h.mu.Unlock()
}

// EndSession — VPS закрив сесію (rs_stream_end): просто забуваємо її.
func (h *Host) EndSession(sid string) {
	h.mu.Lock()
	delete(h.sessions, sid)
	h.mu.Unlock()
}

// EndAll закриває всі сесії (вихід з акаунта, пауза, зупинка лаунчера).
func (h *Host) EndAll() {
	h.mu.Lock()
	list := make([]*session, 0, len(h.sessions))
	for _, s := range h.sessions {
		list = append(list, s)
	}
	h.sessions = map[string]*session{}
	h.mu.Unlock()
	for _, s := range list {
		h.sendEnd(s)
	}
}

func sanitizeLine(l Line) (Line, bool) {
	var b strings.Builder
	n := 0
	for _, r := range l.X {
		if r == '\t' {
			r = ' '
		}
		if r < 0x20 || r == 0x7f || r == utf8.RuneError {
			continue
		}
		b.WriteRune(r)
		if n++; n >= maxLineRunes {
			break
		}
	}
	l.X = b.String()
	if len(l.T) > 8 {
		l.T = l.T[:8]
	}
	switch l.L {
	case "info", "warn", "error", "system":
	default:
		l.L = "info"
	}
	return l, l.X != ""
}

func sanitizeLines(in []Line) []Line {
	out := make([]Line, 0, len(in))
	for _, l := range in {
		if c, ok := sanitizeLine(l); ok {
			out = append(out, c)
		}
	}
	return out
}

// OnLine — новий рядок консолі сервера (підключити до servers.Manager.SetCallbacks).
func (h *Host) OnLine(serverID string, l Line) {
	c, ok := sanitizeLine(l)
	if !ok {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, s := range h.sessions {
		if s.srv != serverID {
			continue
		}
		s.pending = append(s.pending, c)
		if len(s.pending) > maxPending {
			drop := len(s.pending) - maxPending
			s.pending = s.pending[drop:]
			s.skipped += drop
		}
	}
}

func (h *Host) sendEnd(s *session) {
	_ = s.out(envelope{V: envVersion, K: "out", SID: s.sid, TS: h.d.Now().UnixMilli(), End: true})
}

// Flush (викликати кожні ~400 мс) перевіряє, що права живі, і шле накопичені рядки пачками.
func (h *Host) Flush() {
	now := h.d.Now()
	h.mu.Lock()
	ids := make([]string, 0, len(h.sessions))
	for id := range h.sessions {
		ids = append(ids, id)
	}
	h.mu.Unlock()
	for _, id := range ids {
		h.flushOne(id, now)
	}
}

func (h *Host) flushOne(sid string, now time.Time) {
	h.mu.Lock()
	s := h.sessions[sid]
	if s == nil {
		h.mu.Unlock()
		return
	}
	isFriend, since := h.d.Friends.Friend(s.friend)
	_, exists := h.d.Servers.Exists(s.srv)
	if !now.Before(s.expires) || !exists || !h.d.Store.HasConsole(h.d.Self(), s.friend, s.srv, isFriend, since) {
		delete(h.sessions, sid)
		h.mu.Unlock()
		h.sendEnd(s)
		return
	}
	var chunks []envelope
	for len(s.pending) > 0 && len(chunks) < chunksPerFlush {
		e := envelope{V: envVersion, K: "out", SID: sid, TS: now.UnixMilli()}
		if s.skipped > 0 {
			e.Lines = append(e.Lines, Line{L: "system", X: "… пропущено рядків: " + itoa(s.skipped)})
			s.skipped = 0
		}
		take := 0
		for take < len(s.pending) {
			e.Lines = append(e.Lines, s.pending[take])
			plain, ok := e.marshal()
			if !ok || len(plain) > chunkSoftLimit {
				e.Lines = e.Lines[:len(e.Lines)-1]
				break
			}
			take++
		}
		if take == 0 { // один рядок не влазить — викидаємо його
			s.pending = s.pending[1:]
			continue
		}
		s.pending = s.pending[take:]
		chunks = append(chunks, e)
	}
	out := s.out
	h.mu.Unlock()
	for _, e := range chunks {
		if out(e) != nil {
			return
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// ServeChannel обслуговує ПРЯМИЙ зашифрований канал з другом (див. remotechan). Канал уже
// автентифікований: VPS видав мандат на p2p_open, а сторони довели знання спільного секрету.
// Кожна команда все одно проходить ті самі перевірки (права мандата, локальна політика, ліміти,
// заборона повторів) — канал лише замінює релей через VPS. Блокує до закриття каналу.
func (h *Host) ServeChannel(ch *remotechan.Conn, friendID string, md social.RemoteMandate) {
	defer ch.Close()
	tag := new(int)
	defer h.dropByTag(tag)
	var wmu sync.Mutex
	send := func(e envelope) error {
		plain, ok := e.marshal()
		if !ok {
			return errBadEnvelope
		}
		wmu.Lock()
		defer wmu.Unlock()
		return ch.Send([]byte(plain))
	}
	cd := conduit{reply: func(e envelope) { _ = send(e) }, out: send, tag: tag}
	for {
		_ = ch.SetReadDeadline(h.d.Now().Add(2 * time.Minute)) // простій без трафіку — розрив
		msg, err := ch.Recv()
		if err != nil {
			return
		}
		env, ok := parseEnvelope(string(msg), "cmd")
		now := h.d.Now()
		if !ok || !fresh(env.TS, now) || env.Srv != md.Srv || env.ID == "" {
			h.audit(friendID, md.Srv, env.Op, "envelope_mismatch", "")
			return // порушення протоколу по автентифікованому каналу: рвемо
		}
		h.execute(friendID, &md, env, cd, now)
	}
}

func (h *Host) dropByTag(tag any) {
	h.mu.Lock()
	for id, s := range h.sessions {
		if s.tag == tag {
			delete(h.sessions, id)
		}
	}
	h.mu.Unlock()
}
