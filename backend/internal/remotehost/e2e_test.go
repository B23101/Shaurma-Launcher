package remotehost

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"shaurma-launcher-backend/internal/remotectl"
	"shaurma-launcher-backend/internal/social"
)

const (
	ownerID  = "aaaaaaaa-0000-0000-0000-000000000001"
	friendID = "bbbbbbbb-0000-0000-0000-000000000002"
	srvID    = "srv1"
)

// ── фейки ────────────────────────────────────────────────────────────────

type pairCrypto struct {
	peerID string
	shared social.SharedKey
}

func (c pairCrypto) EncryptFor(id, text string) (string, string, error) {
	if id != c.peerID {
		return "", "", fmt.Errorf("unexpected peer %s", id)
	}
	return social.Encrypt(c.shared, []byte(text))
}
func (c pairCrypto) DecryptFrom(id, ct, n string) (string, error) {
	if id != c.peerID {
		return "", fmt.Errorf("unexpected peer %s", id)
	}
	b, err := social.Decrypt(c.shared, ct, n)
	return string(b), err
}

type fakeServers struct {
	mu      sync.Mutex
	state   string
	players int
	started int
	stopped int
	cmds    []string
	tail    []Line
}

func (f *fakeServers) Exists(id string) (string, bool) { return "Мій сервер", id == srvID }
func (f *fakeServers) State(string) string             { f.mu.Lock(); defer f.mu.Unlock(); return f.state }
func (f *fakeServers) Players(string) int              { return f.players }
func (f *fakeServers) Start(string) error {
	f.mu.Lock()
	f.started++
	f.state = "starting"
	f.mu.Unlock()
	return nil
}
func (f *fakeServers) Stop(string) error {
	f.mu.Lock()
	f.stopped++
	f.state = "stopping"
	f.mu.Unlock()
	return nil
}
func (f *fakeServers) SendCommand(_ string, c string) error {
	f.mu.Lock()
	f.cmds = append(f.cmds, c)
	f.mu.Unlock()
	return nil
}
func (f *fakeServers) Tail(string, int) []Line { return f.tail }

type fakeFriends struct{ friend bool }

func (f *fakeFriends) Friend(id string) (bool, int64) { return f.friend && id == friendID, 0 }
func (f *fakeFriends) Nick(string) string             { return "Друг" }

// fakeVPS: підписує мандати й перекладає кадри між сторонами (як справжній VPS, але без власних перевірок —
// вони протестовані в пакеті hub). Тести вмикають «брехню» через tamper.
type fakeVPS struct {
	priv   ed25519.PrivateKey
	caps   string
	tamper func(in *social.RemoteIncoming)
	host   *Host
	client *Client
	nowFn  func() time.Time
	// скільки разів кадр доставляти (для тесту повтору)
	times int
}

type clientNet struct{ v *fakeVPS }

func (n clientNet) SendCmd(reqID, owner, srv, op, sid, ct, nonce string) error {
	v := n.v
	md := social.RemoteMandate{V: 1, Owner: owner, Friend: friendID, Srv: srv, Op: op, Req: reqID, Caps: v.caps,
		CT: social.EnvelopeHash(ct, nonce), SID: sid, IAT: v.nowFn().UnixMilli(), EXP: v.nowFn().Add(time.Minute).UnixMilli()}
	md.Sig = base64.StdEncoding.EncodeToString(ed25519.Sign(v.priv, md.SigningBytes()))
	in := social.RemoteIncoming{ReqID: reqID, From: friendID, ServerID: srv, Op: op, SID: sid, Ciphertext: ct, Nonce: nonce, Mandate: &md}
	if v.tamper != nil {
		v.tamper(&in)
	}
	for i := 0; i < v.times; i++ {
		v.host.Handle(in)
	}
	return nil
}
func (clientNet) SendReply(string, string, string, string) error  { return nil }
func (clientNet) SendStream(string, string, string, string) error { return nil }

// hostNet з доставкою до клієнта
type deliverNet struct{ v *fakeVPS }

func (n deliverNet) SendReply(reqID, to, ct, nonce string) error {
	n.v.client.HandleResult(social.RemoteResultFrame{ReqID: reqID, From: ownerID, Ciphertext: ct, Nonce: nonce})
	return nil
}
func (n deliverNet) SendStream(sid, to, ct, nonce string) error {
	n.v.client.HandleStream(social.RemoteStreamFrame{SID: sid, From: ownerID, Ciphertext: ct, Nonce: nonce})
	return nil
}
func (deliverNet) SendCmd(string, string, string, string, string, string, string) error { return nil }

type events struct {
	mu      sync.Mutex
	results []ResultEvent
	console []ConsoleEvent
}

func (e *events) emit(name string, p any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	switch v := p.(type) {
	case ResultEvent:
		e.results = append(e.results, v)
	case ConsoleEvent:
		e.console = append(e.console, v)
	}
}
func (e *events) lastResult() *ResultEvent {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.results) == 0 {
		return nil
	}
	r := e.results[len(e.results)-1]
	return &r
}
func (e *events) lines() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []string
	for _, c := range e.console {
		for _, l := range c.Lines {
			out = append(out, l.X)
		}
	}
	return out
}
func (e *events) ended() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, c := range e.console {
		if c.End {
			return true
		}
	}
	return false
}

type rig struct {
	t      *testing.T
	vps    *fakeVPS
	host   *Host
	client *Client
	srv    *fakeServers
	store  *remotectl.Store
	ev     *events
	frs    *fakeFriends
	now    time.Time
}

func newRig(t *testing.T, rights remotectl.Rights, caps string) *rig {
	t.Helper()
	pubO, secO, _ := social.GenerateIdentity()
	pubF, secF, _ := social.GenerateIdentity()
	vpsPub, vpsPriv, _ := ed25519.GenerateKey(nil)
	r := &rig{t: t, now: time.Now(), ev: &events{}, srv: &fakeServers{state: "stopped"}, frs: &fakeFriends{friend: true}}
	r.store = remotectl.Open(t.TempDir())
	if rights != (remotectl.Rights{}) {
		if err := r.store.SetGrantFull(ownerID, srvID, friendID, rights, r.now.UnixMilli()); err != nil {
			t.Fatal(err)
		}
	}
	r.vps = &fakeVPS{priv: vpsPriv, caps: caps, times: 1, nowFn: func() time.Time { return r.now }}
	r.host = NewHost(HostDeps{
		Self: func() string { return ownerID }, VPSKey: func() ed25519.PublicKey { return vpsPub },
		Crypto: pairCrypto{friendID, social.Precompute(pubF, secO)}, Net: deliverNet{r.vps},
		Servers: r.srv, Friends: r.frs, Store: r.store, Now: func() time.Time { return r.now },
		Async: func(f func()) { f() },
	})
	r.client = NewClient(pairCrypto{ownerID, social.Precompute(pubO, secF)}, clientNet{r.vps}, r.ev.emit)
	r.client.Now = func() time.Time { return r.now }
	r.vps.host, r.vps.client = r.host, r.client
	return r
}

func (r *rig) tick() { r.now = r.now.Add(5 * time.Second) } // обхід cooldown між командами

// ── start / stop ─────────────────────────────────────────────────────────

func TestStartHappyPath(t *testing.T) {
	r := newRig(t, remotectl.Rights{Start: true}, "s")
	if _, err := r.client.Command(ownerID, srvID, "start"); err != nil {
		t.Fatal(err)
	}
	if r.srv.started != 1 {
		t.Fatal("сервер мав запуститись")
	}
	if ev := r.ev.lastResult(); ev == nil || !ev.OK || ev.Code != "accepted" {
		t.Fatalf("очікував accepted: %+v", ev)
	}
	if a := r.store.Audit(ownerID, srvID, 5); len(a) == 0 || a[0].Op != "start" || a[0].Friend != friendID {
		t.Fatalf("дію мають записати в журнал: %+v", a)
	}
}

func TestWithoutValidMandateNothingRuns(t *testing.T) {
	for name, tamper := range map[string]func(*social.RemoteIncoming){
		"без мандата": func(in *social.RemoteIncoming) { in.Mandate = nil },
		"чужий підпис": func(in *social.RemoteIncoming) {
			_, other, _ := ed25519.GenerateKey(nil)
			in.Mandate.Sig = base64.StdEncoding.EncodeToString(ed25519.Sign(other, in.Mandate.SigningBytes()))
		},
		"підроблені права": func(in *social.RemoteIncoming) { in.Mandate.Caps = "sxce" },
		"інший відправник": func(in *social.RemoteIncoming) { in.From = "cccccccc-0000-0000-0000-000000000003" },
		"підмінений конверт": func(in *social.RemoteIncoming) {
			in.Ciphertext = base64.StdEncoding.EncodeToString([]byte("zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"))
		},
	} {
		r := newRig(t, remotectl.Rights{Start: true}, "s")
		r.vps.tamper = tamper
		_, _ = r.client.Command(ownerID, srvID, "start")
		if r.srv.started != 0 {
			t.Fatalf("%s: сервер запустився без чинного мандата", name)
		}
		if r.ev.lastResult() != nil {
			t.Fatalf("%s: стороннім не відповідаємо", name)
		}
	}
}

func TestMandateOpMustMatchEnvelope(t *testing.T) {
	// VPS (або хтось на шляху) підписує мандат на ІНШУ дію, ніж у конверті друга.
	r := newRig(t, remotectl.Rights{Start: true, Stop: true}, "sx")
	r.vps.tamper = func(in *social.RemoteIncoming) {
		in.Mandate.Op = "stop"
		in.Mandate.Sig = base64.StdEncoding.EncodeToString(ed25519.Sign(r.vps.priv, in.Mandate.SigningBytes()))
		in.Op = "stop"
	}
	r.srv.state = "running"
	_, _ = r.client.Command(ownerID, srvID, "start")
	if r.srv.stopped != 0 || r.srv.started != 0 {
		t.Fatal("розбіжність мандата й конверта має відсікатись")
	}
}

func TestReplayedFrameRunsOnce(t *testing.T) {
	r := newRig(t, remotectl.Rights{Start: true}, "s")
	r.vps.times = 3
	_, _ = r.client.Command(ownerID, srvID, "start")
	if r.srv.started != 1 {
		t.Fatalf("повтор кадру виконався %d разів", r.srv.started)
	}
}

func TestLocalPolicyIsStillEnforced(t *testing.T) {
	// VPS каже «можна», але власник локально нічого не видавав.
	r := newRig(t, remotectl.Rights{}, "sx")
	_, _ = r.client.Command(ownerID, srvID, "start")
	if r.srv.started != 0 {
		t.Fatal("локальна політика власника має остаточне слово")
	}
	if ev := r.ev.lastResult(); ev == nil || ev.OK || ev.Code != remotectl.CodeNotAllowed {
		t.Fatalf("очікував not_allowed: %+v", ev)
	}
	// не друг (локально)
	r2 := newRig(t, remotectl.Rights{Start: true}, "s")
	r2.frs.friend = false
	_, _ = r2.client.Command(ownerID, srvID, "start")
	if r2.srv.started != 0 {
		t.Fatal("не-друг не має запускати")
	}
}

func TestStopBlockedWhilePlayersOnline(t *testing.T) {
	r := newRig(t, remotectl.Rights{Stop: true}, "x")
	r.srv.state, r.srv.players = "running", 2
	_, _ = r.client.Command(ownerID, srvID, "stop")
	if r.srv.stopped != 0 {
		t.Fatal("не можна зупиняти при гравцях")
	}
	if ev := r.ev.lastResult(); ev == nil || ev.Code != remotectl.CodePlayers {
		t.Fatalf("%+v", ev)
	}
	r.srv.players = 0
	r.tick()
	_, _ = r.client.Command(ownerID, srvID, "stop")
	if r.srv.stopped != 1 {
		t.Fatal("без гравців зупинка має спрацювати")
	}
}

// ── консоль ──────────────────────────────────────────────────────────────

func openConsole(t *testing.T, r *rig) string {
	t.Helper()
	sid, _, err := r.client.OpenConsole(ownerID, srvID)
	if err != nil {
		t.Fatal(err)
	}
	if ev := r.ev.lastResult(); ev == nil || !ev.OK || ev.Code != "opened" {
		t.Fatalf("консоль не відкрилась: %+v", ev)
	}
	return sid
}

func TestConsoleStreamAndExec(t *testing.T) {
	r := newRig(t, remotectl.Rights{Console: true, Exec: true}, "ce")
	r.srv.state = "running"
	r.srv.tail = []Line{{ID: 1, T: "10:00:00", L: "info", X: "старий рядок"}}
	sid := openConsole(t, r)
	if got := r.ev.lines(); len(got) != 1 || got[0] != "старий рядок" {
		t.Fatalf("історія консолі: %v", got)
	}
	r.host.OnLine(srvID, Line{ID: 2, T: "10:00:01", L: "info", X: "Steve joined the game"})
	r.host.OnLine(srvID, Line{ID: 3, T: "10:00:02", L: "warn", X: "ескейп \x1b[31m тест \x00"})
	r.host.Flush()
	got := r.ev.lines()
	if len(got) != 3 || got[1] != "Steve joined the game" || strings.ContainsAny(got[2], "\x1b\x00") {
		t.Fatalf("потік: %q", got)
	}
	// повтор уже показаного рядка ігнорується
	r.host.OnLine(srvID, Line{ID: 3, X: "дубль"})
	r.host.Flush()
	if len(r.ev.lines()) != 3 {
		t.Fatal("дубль не мав показатись")
	}

	r.tick()
	if _, err := r.client.Exec(sid, "say привіт"); err != nil {
		t.Fatal(err)
	}
	if len(r.srv.cmds) != 1 || r.srv.cmds[0] != "say привіт" {
		t.Fatalf("команда не дійшла: %v", r.srv.cmds)
	}
	if a := r.store.Audit(ownerID, srvID, 3); a[0].Op != "console_exec" || a[0].Cmd != "say привіт" {
		t.Fatalf("журнал: %+v", a[0])
	}
}

func TestConsoleExecSafety(t *testing.T) {
	r := newRig(t, remotectl.Rights{Console: true, Exec: true}, "ce")
	r.srv.state, r.srv.players = "running", 1
	sid := openConsole(t, r)
	try := func(cmd string) *ResultEvent {
		r.now = r.now.Add(4 * time.Second)
		if _, err := r.client.Exec(sid, cmd); err != nil {
			t.Fatal(err)
		}
		return r.ev.lastResult()
	}
	if ev := try("op Steve"); ev.Code != remotectl.CodePrivileged {
		t.Fatalf("op: %+v", ev)
	}
	if ev := try("say a\nstop"); ev.Code != remotectl.CodeBadCommand {
		t.Fatalf("багаторядкова: %+v", ev)
	}
	if ev := try("stop"); ev.Code != remotectl.CodeNotAllowed {
		t.Fatalf("stop без права Stop обійшов перевірку: %+v", ev)
	}
	if len(r.srv.cmds) != 0 {
		t.Fatalf("жодна з небезпечних команд не мала дійти до сервера: %v", r.srv.cmds)
	}
	// Тепер є право Stop, але гравці онлайн.
	_ = r.store.SetGrantFull(ownerID, srvID, friendID, remotectl.Rights{Console: true, Exec: true, Stop: true}, r.now.UnixMilli())
	if ev := try("stop"); ev.Code != remotectl.CodePlayers || len(r.srv.cmds) != 0 {
		t.Fatalf("stop при гравцях: %+v %v", ev, r.srv.cmds)
	}
	r.srv.players = 0
	if ev := try("stop"); !ev.OK || len(r.srv.cmds) != 1 {
		t.Fatalf("stop без гравців: %+v %v", ev, r.srv.cmds)
	}
}

func TestConsoleViewOnlyCannotExec(t *testing.T) {
	r := newRig(t, remotectl.Rights{Console: true}, "c")
	sid := openConsole(t, r)
	_, _ = r.client.Exec(sid, "say hi")
	if len(r.srv.cmds) != 0 {
		t.Fatal("без права Exec команди недопустимі")
	}
	if ev := r.ev.lastResult(); ev.OK || ev.Code != remotectl.CodeNotAllowed {
		t.Fatalf("%+v", ev)
	}
}

func TestConsoleEndsWhenOwnerRevokesLocally(t *testing.T) {
	r := newRig(t, remotectl.Rights{Console: true}, "c")
	openConsole(t, r)
	_ = r.store.SetGrantFull(ownerID, srvID, friendID, remotectl.Rights{Start: true}, r.now.UnixMilli())
	r.host.OnLine(srvID, Line{ID: 9, X: "секрет"})
	r.host.Flush()
	if !r.ev.ended() {
		t.Fatal("друг має отримати кінець сесії")
	}
	for _, l := range r.ev.lines() {
		if l == "секрет" {
			t.Fatal("після відкликання рядки більше не шлемо")
		}
	}
}

func TestConsoleEndsOnPauseAndFriendRemoval(t *testing.T) {
	r := newRig(t, remotectl.Rights{Console: true}, "c")
	openConsole(t, r)
	r.frs.friend = false
	r.host.Flush()
	if !r.ev.ended() {
		t.Fatal("після видалення з друзів сесія має закритись")
	}
	r2 := newRig(t, remotectl.Rights{Console: true}, "c")
	openConsole(t, r2)
	_ = r2.store.SetPaused(ownerID, true)
	r2.host.Flush()
	if !r2.ev.ended() {
		t.Fatal("пауза має закривати сесії")
	}
}

func TestConsoleChunkingKeepsOrderAndSize(t *testing.T) {
	r := newRig(t, remotectl.Rights{Console: true}, "c")
	r.srv.state = "running"
	openConsole(t, r)
	var want []string
	for i := 1; i <= 60; i++ {
		x := fmt.Sprintf("рядок %03d <тег> & \"лапки\" %s", i, strings.Repeat("я", 150))
		want = append(want, x[:min(len(x), 1000)])
		r.host.OnLine(srvID, Line{ID: uint64(i + 10), T: "10:00:00", L: "info", X: x})
	}
	for i := 0; i < 30 && len(r.ev.lines()) < 60; i++ {
		r.host.Flush()
	}
	got := r.ev.lines()
	if len(got) != 60 {
		t.Fatalf("доставлено %d з 60", len(got))
	}
	for i, l := range got {
		if !strings.HasPrefix(l, fmt.Sprintf("рядок %03d", i+1)) {
			t.Fatalf("порядок порушено на %d: %.20s", i, l)
		}
	}
}

func TestClientIgnoresForeignStreamsAndResults(t *testing.T) {
	r := newRig(t, remotectl.Rights{Console: true}, "c")
	sid := openConsole(t, r)
	before := len(r.ev.lines())
	// кадр від «не власника»
	r.client.HandleStream(social.RemoteStreamFrame{SID: sid, From: "cccccccc-0000-0000-0000-000000000003", Ciphertext: "AAAA", Nonce: "AAAA"})
	// невідомий sid
	r.client.HandleStream(social.RemoteStreamFrame{SID: "sid-unknown-1", From: ownerID, Ciphertext: "AAAA", Nonce: "AAAA"})
	// сміття від власника
	r.client.HandleStream(social.RemoteStreamFrame{SID: sid, From: ownerID, Ciphertext: "AAAA", Nonce: "AAAA"})
	r.client.HandleResult(social.RemoteResultFrame{ReqID: "r-unknown-1", From: ownerID, Ciphertext: "AAAA", Nonce: "AAAA"})
	if len(r.ev.lines()) != before {
		t.Fatal("сторонні кадри не мають показуватись")
	}
	r.client.HandleStreamEnd(sid, "revoked")
	if !r.ev.ended() {
		t.Fatal("rs_stream_end має завершувати сесію")
	}
}

func TestVPSRefusalIsSurfaced(t *testing.T) {
	r := newRig(t, remotectl.Rights{Start: true}, "s")
	r.vps.times = 0 // кадр до власника не доходить: VPS відмовляє сам
	rq, err := r.client.send(ownerID, srvID, "start", "", "")
	if err != nil {
		t.Fatal(err)
	}
	r.client.HandleResult(social.RemoteResultFrame{ReqID: rq, Code: "not_allowed"})
	if ev := r.ev.lastResult(); ev == nil || ev.OK || ev.Code != "not_allowed" {
		t.Fatalf("%+v", ev)
	}
}
