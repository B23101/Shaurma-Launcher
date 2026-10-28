package remotehost

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"shaurma-launcher-backend/internal/remotechan"
	"shaurma-launcher-backend/internal/social"
)

// ErrBadRequest — невалідні параметри команди.
var ErrBadRequest = errors.New("remotehost: bad request")

const (
	pendingTTL    = 2 * time.Minute
	maxPendingCmd = 16
)

// ResultEvent — подія remote:result для UI друга.
type ResultEvent struct {
	ReqID    string `json:"reqId"`
	OwnerID  string `json:"ownerId"`
	ServerID string `json:"serverId"`
	Op       string `json:"op"`
	SID      string `json:"sid,omitempty"`
	OK       bool   `json:"ok"`
	Code     string `json:"code,omitempty"`
	State    string `json:"state,omitempty"`
	Final    bool   `json:"final"`
}

// ConsoleEvent — подія remote:console: нові рядки (або кінець сесії).
type ConsoleEvent struct {
	SID      string `json:"sid"`
	OwnerID  string `json:"ownerId"`
	ServerID string `json:"serverId"`
	Lines    []Line `json:"lines,omitempty"`
	End      bool   `json:"end,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

type pendingCmd struct {
	owner, srv, op, sid string
	at                  time.Time
}

type clientSession struct {
	owner, srv string
	lastID     uint64
	open       bool
}

// Client — сторона ДРУГА: шле команди й приймає відповіді та потік консолі.
type Client struct {
	Crypto Crypto
	Net    Net
	Emit   func(event string, payload any)
	Now    func() time.Time

	mu   sync.Mutex
	pend map[string]*pendingCmd
	sess map[string]*clientSession
	// Прямі канали — по парі власник|сервер: власник прив'язує канал до сервера з мандата
	// (ServeChannel рве його на команді для іншого сервера), тож спільний канал на власника ламав
	// керування другим сервером того самого друга.
	chs map[string]*remotechan.Conn
}

func chKey(owner, srv string) string { return owner + "|" + srv }

// NewClient створює Client.
func NewClient(c Crypto, n Net, emit func(string, any)) *Client {
	if emit == nil {
		emit = func(string, any) {}
	}
	return &Client{Crypto: c, Net: n, Emit: emit, Now: time.Now, pend: map[string]*pendingCmd{}, sess: map[string]*clientSession{}, chs: map[string]*remotechan.Conn{}}
}

func randID(prefix string) string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic("remotehost: crypto/rand: " + err.Error())
	}
	return prefix + hex.EncodeToString(b)
}

// Command надсилає start/stop/p2p_on/p2p_off. Повертає reqId — за ним UI зіставляє подію remote:result.
func (c *Client) Command(ownerID, serverID, op string) (string, error) {
	switch op {
	case social.RemoteOpStart, social.RemoteOpStop, social.RemoteOpP2POn, social.RemoteOpP2POff:
	default:
		return "", ErrBadRequest
	}
	return c.send(ownerID, serverID, op, "", "")
}

// OpenConsole відкриває сесію консолі; повертає sid і reqId.
func (c *Client) OpenConsole(ownerID, serverID string) (sid, reqID string, err error) {
	sid = randID("s")
	// Сесію реєструємо ДО відправки: перший шматок потоку може прийти раніше за повернення з send.
	c.mu.Lock()
	c.sess[sid] = &clientSession{owner: ownerID, srv: serverID}
	c.mu.Unlock()
	reqID, err = c.send(ownerID, serverID, social.RemoteOpConsoleOpen, sid, "")
	if err != nil {
		c.mu.Lock()
		delete(c.sess, sid)
		c.mu.Unlock()
		return "", "", err
	}
	return sid, reqID, nil
}

// Exec шле команду в консоль відкритої сесії.
func (c *Client) Exec(sid, cmd string) (string, error) {
	c.mu.Lock()
	s := c.sess[sid]
	c.mu.Unlock()
	if s == nil || cmd == "" || len(cmd) > 256 {
		return "", ErrBadRequest
	}
	return c.send(s.owner, s.srv, social.RemoteOpConsoleExec, sid, cmd)
}

// CloseConsole закриває сесію (і локально, і на власнику).
func (c *Client) CloseConsole(sid string) error {
	c.mu.Lock()
	s := c.sess[sid]
	delete(c.sess, sid)
	c.mu.Unlock()
	if s == nil {
		return nil
	}
	_, err := c.send(s.owner, s.srv, social.RemoteOpConsoleClose, sid, "")
	return err
}

func (c *Client) send(owner, srv, op, sid, cmd string) (string, error) {
	now := c.Now()
	reqID := randID("r")
	env := envelope{V: envVersion, K: "cmd", ID: reqID, Op: op, Srv: srv, SID: sid, TS: now.UnixMilli(), N: randID(""), Cmd: cmd}
	plain, ok := env.marshal()
	if !ok {
		return "", ErrBadRequest
	}
	ct, nonce, err := c.Crypto.EncryptFor(owner, plain)
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	for k, p := range c.pend {
		if now.Sub(p.at) > pendingTTL {
			delete(c.pend, k)
		}
	}
	if len(c.pend) >= maxPendingCmd {
		c.mu.Unlock()
		return "", ErrBadRequest
	}
	c.pend[reqID] = &pendingCmd{owner: owner, srv: srv, op: op, sid: sid, at: now}
	c.mu.Unlock()
	if ch := c.channelFor(owner, srv); ch != nil {
		// Пряма дорога: той самий конверт, але у зашифрований канал (шифрування канал дає сам).
		if err := ch.Send([]byte(plain)); err == nil {
			return reqID, nil
		}
		c.dropChannel(owner, srv, ch) // канал помер — нижче піде через VPS
	}
	if err := c.Net.SendCmd(reqID, owner, srv, op, sid, ct, nonce); err != nil {
		c.mu.Lock()
		delete(c.pend, reqID)
		c.mu.Unlock()
		return "", err
	}
	return reqID, nil
}

// HandleResult — кадр rs_cmd_result: або код відмови VPS, або E2E-відповідь власника.
func (c *Client) HandleResult(f social.RemoteResultFrame) {
	c.mu.Lock()
	p := c.pend[f.ReqID]
	c.mu.Unlock()
	if p == nil {
		return // чужий/застарілий reqId
	}
	ev := ResultEvent{ReqID: f.ReqID, OwnerID: p.owner, ServerID: p.srv, Op: p.op, SID: p.sid, Final: true}
	if f.Code != "" {
		ev.Code = f.Code
		c.finish(f.ReqID, p, false)
		c.Emit("remote:result", ev)
		return
	}
	if f.From != p.owner {
		return
	}
	plain, err := c.Crypto.DecryptFrom(f.From, f.Ciphertext, f.Nonce)
	if err != nil {
		return
	}
	env, ok := parseEnvelope(plain, "res")
	if !ok {
		return
	}
	c.applyResult(f.ReqID, p, ev, env)
}

func (c *Client) applyResult(reqID string, p *pendingCmd, ev ResultEvent, env envelope) {
	if env.ID != reqID || env.Op != p.op || env.Srv != p.srv || env.SID != p.sid || !fresh(env.TS, c.Now()) {
		return
	}
	ev.OK, ev.Code, ev.State, ev.Final = env.OK, env.Code, SanitizeState(env.State), env.Final || !env.OK
	if p.op == social.RemoteOpConsoleOpen {
		c.mu.Lock()
		if s := c.sess[p.sid]; s != nil {
			s.open = env.OK
			if !env.OK {
				delete(c.sess, p.sid)
			}
		}
		c.mu.Unlock()
	}
	if ev.Final {
		c.finish(reqID, p, true)
	}
	c.Emit("remote:result", ev)
}

func (c *Client) finish(reqID string, p *pendingCmd, _ bool) {
	c.mu.Lock()
	delete(c.pend, reqID)
	if p.op == social.RemoteOpConsoleOpen {
		if s := c.sess[p.sid]; s != nil && !s.open {
			delete(c.sess, p.sid)
		}
	}
	c.mu.Unlock()
}

// HandleStream — кадр rs_stream_in: шматок консолі.
func (c *Client) HandleStream(f social.RemoteStreamFrame) {
	c.mu.Lock()
	s := c.sess[f.SID]
	c.mu.Unlock()
	if s == nil || f.From != s.owner {
		return
	}
	plain, err := c.Crypto.DecryptFrom(f.From, f.Ciphertext, f.Nonce)
	if err != nil {
		return
	}
	env, ok := parseEnvelope(plain, "out")
	if !ok {
		return
	}
	c.applyOut(f.SID, s, env)
}

func (c *Client) applyOut(sid string, s *clientSession, env envelope) {
	if env.SID != sid || !fresh(env.TS, c.Now()) {
		return
	}
	f := struct{ SID string }{sid}
	ev := ConsoleEvent{SID: f.SID, OwnerID: s.owner, ServerID: s.srv}
	c.mu.Lock()
	for _, l := range env.Lines {
		if l.ID != 0 {
			if l.ID <= s.lastID {
				continue // повтор вже показаного рядка
			}
			s.lastID = l.ID
		}
		if cl, ok := sanitizeLine(l); ok {
			ev.Lines = append(ev.Lines, cl)
		}
	}
	if env.End {
		delete(c.sess, f.SID)
		ev.End, ev.Reason = true, "closed"
	}
	c.mu.Unlock()
	if len(ev.Lines) > 0 || ev.End {
		c.Emit("remote:console", ev)
	}
}

// HandleStreamEnd — кадр rs_stream_end від VPS (права зняті, власник офлайн, час вичерпано…).
func (c *Client) HandleStreamEnd(sid, reason string) {
	c.mu.Lock()
	s := c.sess[sid]
	delete(c.sess, sid)
	c.mu.Unlock()
	if s != nil {
		c.Emit("remote:console", ConsoleEvent{SID: sid, OwnerID: s.owner, ServerID: s.srv, End: true, Reason: reason})
	}
}

// SanitizeState зводить стан до відомого.
func SanitizeState(s string) string { return social.SanitizeRemoteState(s) }

func (c *Client) channelFor(owner, srv string) *remotechan.Conn {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.chs[chKey(owner, srv)]
}

func (c *Client) dropChannel(owner, srv string, ch *remotechan.Conn) {
	c.mu.Lock()
	if k := chKey(owner, srv); c.chs[k] == ch {
		delete(c.chs, k)
	}
	c.mu.Unlock()
	_ = ch.Close()
}

// UseChannel переключає команди до цього сервера власника на пряме зашифроване з'єднання і читає
// з нього відповіді та потік консолі. Блокує до закриття каналу; після цього команди знову йдуть через VPS.
func (c *Client) UseChannel(owner, srv string, ch *remotechan.Conn) {
	k := chKey(owner, srv)
	c.mu.Lock()
	if old := c.chs[k]; old != nil {
		_ = old.Close()
	}
	c.chs[k] = ch
	c.mu.Unlock()
	defer c.dropChannel(owner, srv, ch)
	for {
		_ = ch.SetReadDeadline(c.Now().Add(2 * time.Minute))
		msg, err := ch.Recv()
		if err != nil {
			return
		}
		if env, ok := parseEnvelope(string(msg), "res"); ok {
			c.mu.Lock()
			p := c.pend[env.ID]
			c.mu.Unlock()
			if p != nil && p.owner == owner {
				c.applyResult(env.ID, p, ResultEvent{ReqID: env.ID, OwnerID: p.owner, ServerID: p.srv, Op: p.op, SID: p.sid, Final: true}, env)
			}
			continue
		}
		if env, ok := parseEnvelope(string(msg), "out"); ok {
			c.mu.Lock()
			s := c.sess[env.SID]
			c.mu.Unlock()
			if s != nil && s.owner == owner {
				c.applyOut(env.SID, s, env)
			}
		}
	}
}

// HasChannel — чи є зараз пряме з'єднання з цим сервером власника.
func (c *Client) HasChannel(owner, srv string) bool { return c.channelFor(owner, srv) != nil }

// Reset забуває всі команди, сесії й канали (змінився акаунт Shaurma: старі належать іншій особі).
func (c *Client) Reset() {
	c.mu.Lock()
	chs := c.chs
	c.pend, c.sess, c.chs = map[string]*pendingCmd{}, map[string]*clientSession{}, map[string]*remotechan.Conn{}
	c.mu.Unlock()
	for _, ch := range chs {
		_ = ch.Close()
	}
}
