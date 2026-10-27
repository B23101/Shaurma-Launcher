package social

// remote.go — віддалений запуск/зупинка серверів друзів: транспорт (кадри rs_*) і валідація.
//
// МОДЕЛЬ ДОВІРИ. VPS переносить команди як непрозорий E2E-конверт (NaCl box між двома
// лаунчерами — ті самі ключі, що в чаті). Тому:
//   - команду «від друга» VPS підробити не може (власник розшифровує її ключем ЦЬОГО друга);
//   - serverId/op, які VPS бачить відкритим текстом, власник ігнорує — береться лише вміст
//     конверта;
//   - у конверті є id (= reqId кадру), час і випадковий nonce: повтор перехопленого кадру
//     відсікається вікном часу й кешем побачених id.
// Права власник перевіряє сам (пакет remotectl) — VPS лише додатковий фільтр.

import (
	"encoding/base64"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	RemoteOpStart = "start"
	RemoteOpStop  = "stop"

	remoteTSWindow   = 5 * time.Minute
	remoteReplayTTL  = 15 * time.Minute
	remotePendingTTL = 2 * time.Minute

	maxRemoteServersPerOwner = 12
	maxRemoteServersTotal    = 200
	maxRemoteShares          = 12
	maxRemoteGrants          = 40
)

var remoteIDRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
var remoteReqRe = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

// ValidRemoteServerID — формат serverId (та сама межа, що на VPS).
func ValidRemoteServerID(s string) bool { return remoteIDRe.MatchString(s) }

// SanitizeRemoteState зводить будь-який стан до відомого (інакше «stopped»).
func SanitizeRemoteState(s string) string {
	switch s {
	case "stopped", "installing", "starting", "running", "stopping", "crashed", "error":
		return s
	}
	return "stopped"
}

// RemoteServer — сервер друга, доступний мені (друга сторона бачить саме це).
type RemoteServer struct {
	OwnerID     string `json:"ownerId"`
	OwnerOnline bool   `json:"ownerOnline"`
	ID          string `json:"id"`
	Name        string `json:"name"`
	State       string `json:"state"`
	Color       string `json:"color,omitempty"`
	Core        string `json:"core,omitempty"`
	CanStart    bool   `json:"canStart"`
	CanStop     bool   `json:"canStop"`
	CanConsole  bool   `json:"canConsole,omitempty"`
	CanExec     bool   `json:"canExec,omitempty"`
}

// RemoteOwnerList — усі мої доступні сервери одного друга.
type RemoteOwnerList struct {
	Owner   string
	Online  bool
	Servers []RemoteServer
}

// RemoteShareGrant / RemoteShare — що я (власник) публікую: хто що може на моєму сервері.
type RemoteShareGrant struct {
	AccountID string
	Start     bool
	Stop      bool
	Console   bool
	Exec      bool
}

type RemoteShare struct {
	ServerID string
	Name     string
	State    string
	Color    string
	Core     string
	// HostKey — base64 публічного ключа Ed25519 для прямого каналу (див. remotechan); "" — без P2P.
	HostKey string
	Grants  []RemoteShareGrant
}

// RemoteCommand — розшифрована й перевірена на формат команда друга. Права ще НЕ перевірено —
// це робить обробник у App (remotectl).
type RemoteCommand struct {
	From     string
	ReqID    string
	ServerID string
	Op       string
	TS       int64
	SID      string
	Mandate  *RemoteMandate
	// Late дозволяє надіслати ще до двох відповідей на ту саму команду (напр. «запуск не вдався»
	// вже після того, як ми відповіли accepted).
	Late func(RemoteResult)
}

// RemoteResult — відповідь власника.
type RemoteResult struct {
	OK    bool   `json:"ok"`
	Code  string `json:"code,omitempty"` // стабільний код (accepted, already_running, not_allowed, …)
	State string `json:"state,omitempty"`
	Final bool   `json:"final,omitempty"`
}

// RemoteResultEvent — подія remote:result для UI друга.
type RemoteResultEvent struct {
	ReqID    string `json:"reqId"`
	OwnerID  string `json:"ownerId"`
	ServerID string `json:"serverId"`
	Op       string `json:"op"`
	OK       bool   `json:"ok"`
	Code     string `json:"code,omitempty"`
	State    string `json:"state,omitempty"`
	Final    bool   `json:"final"`
}

// ── Конверт ──────────────────────────────────────────────────────────────

type remoteEnvelope struct {
	V     int    `json:"v"`
	K     string `json:"k"` // "cmd" | "res"
	ID    string `json:"id"`
	Op    string `json:"op"`
	Srv   string `json:"srv"`
	TS    int64  `json:"ts"`
	N     string `json:"n,omitempty"`
	SID   string `json:"sid,omitempty"`
	OK    bool   `json:"ok,omitempty"`
	Code  string `json:"code,omitempty"`
	State string `json:"state,omitempty"`
	Final bool   `json:"final,omitempty"`
}

// parseRemoteEnvelope розбирає розшифрований конверт і перевіряє форму. Час НЕ перевіряється тут.
func parseRemoteEnvelope(plain, wantKind string) (remoteEnvelope, bool) {
	var e remoteEnvelope
	if len(plain) > MaxPlaintextBytes || json.Unmarshal([]byte(plain), &e) != nil {
		return e, false
	}
	if e.V != 1 || e.K != wantKind || !remoteReqRe.MatchString(e.ID) || !remoteIDRe.MatchString(e.Srv) ||
		!ValidRemoteOp(e.Op) || (len(e.SID) > 0 && !remoteReqRe.MatchString(e.SID)) {
		return e, false
	}
	e.Code = strings.ToLower(CleanText(e.Code, 32))
	e.State = SanitizeRemoteState(e.State)
	return e, true
}

func timeFresh(ts int64, now time.Time) bool {
	d := now.Sub(time.UnixMilli(ts))
	if d < 0 {
		d = -d
	}
	return d <= remoteTSWindow
}

// ── Кадри від VPS ────────────────────────────────────────────────────────

type rsServerWire struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	State      string `json:"state"`
	Color      string `json:"color"`
	Core       string `json:"core"`
	CanStart   bool   `json:"canStart"`
	CanStop    bool   `json:"canStop"`
	CanConsole bool   `json:"canConsole"`
	CanExec    bool   `json:"canExec"`
}

type rsOwnerWire struct {
	Owner   string         `json:"owner"`
	Online  bool           `json:"online"`
	Servers []rsServerWire `json:"servers"`
}

// cleanOwnerList санітизує список власника. nil → власник не друг або порожньо.
func (p *PresenceClient) cleanOwnerList(w rsOwnerWire) (RemoteOwnerList, bool) {
	if !ValidAccountID(w.Owner) || !p.isFriend(w.Owner) {
		return RemoteOwnerList{}, false
	}
	out := RemoteOwnerList{Owner: w.Owner, Online: w.Online, Servers: []RemoteServer{}}
	seen := map[string]bool{}
	for _, s := range w.Servers {
		if !remoteIDRe.MatchString(s.ID) || seen[s.ID] || len(out.Servers) >= maxRemoteServersPerOwner {
			continue
		}
		if !s.CanStart && !s.CanStop && !s.CanConsole {
			continue
		}
		seen[s.ID] = true
		name := CleanText(s.Name, MaxPackNameLen)
		if name == "" {
			name = s.ID
		}
		out.Servers = append(out.Servers, RemoteServer{
			OwnerID: w.Owner, OwnerOnline: w.Online, ID: s.ID, Name: name,
			State: SanitizeRemoteState(s.State), Color: SanitizeColor(s.Color), Core: CleanText(s.Core, 24),
			CanStart: s.CanStart, CanStop: s.CanStop, CanConsole: s.CanConsole, CanExec: s.CanConsole && s.CanExec,
		})
	}
	return out, true
}

func (p *PresenceClient) handleRemoteSnapshot(ev inEvent) {
	if p.cfg.OnRemoteSnapshot == nil {
		return
	}
	lists := make([]RemoteOwnerList, 0, len(ev.Owners))
	total := 0
	for _, w := range ev.Owners {
		l, ok := p.cleanOwnerList(w)
		if !ok || len(l.Servers) == 0 || total+len(l.Servers) > maxRemoteServersTotal {
			continue
		}
		total += len(l.Servers)
		lists = append(lists, l)
	}
	p.cfg.OnRemoteSnapshot(lists)
}

func (p *PresenceClient) handleRemoteList(ev inEvent) {
	if p.cfg.OnRemoteList == nil {
		return
	}
	l, ok := p.cleanOwnerList(rsOwnerWire{Owner: ev.Owner, Online: ev.Online, Servers: ev.Servers})
	if !ok {
		return
	}
	p.cfg.OnRemoteList(l)
}

// RemoteIncoming — сирий (ще зашифрований) кадр rs_cmd_incoming.
type RemoteIncoming struct {
	ReqID, From, ServerID, Op, SID, Ciphertext, Nonce string
	Mandate                                           *RemoteMandate
}

// RemoteResultFrame — сирий кадр rs_cmd_result: або Code (відмова VPS), або зашифрована відповідь.
type RemoteResultFrame struct {
	ReqID, From, Code, Ciphertext, Nonce string
}

func (p *PresenceClient) handleRemoteIncoming(ev inEvent) {
	// Відправник МУСИТЬ бути другом (VPS це вже перевірив, але не довіряємо і йому).
	if p.cfg.OnRemoteCmd == nil || !p.isFriend(ev.From) || !remoteReqRe.MatchString(ev.ReqID) ||
		ev.Ciphertext == "" || len(ev.Ciphertext) > maxCiphertextB64Len || ev.Nonce == "" || len(ev.Nonce) > 40 {
		return
	}
	p.cfg.OnRemoteCmd(RemoteIncoming{ReqID: ev.ReqID, From: ev.From, ServerID: ev.ServerID, Op: ev.Op, SID: ev.SID,
		Ciphertext: ev.Ciphertext, Nonce: ev.Nonce, Mandate: ev.Mandate})
}

func (p *PresenceClient) handleRemoteResult(ev inEvent) {
	if p.cfg.OnRemoteResult == nil || !remoteReqRe.MatchString(ev.ReqID) {
		return
	}
	f := RemoteResultFrame{ReqID: ev.ReqID, Code: strings.ToLower(CleanText(ev.Code, 32))}
	if f.Code == "" {
		if !p.isFriend(ev.From) || ev.Ciphertext == "" || len(ev.Ciphertext) > maxCiphertextB64Len || ev.Nonce == "" || len(ev.Nonce) > 40 {
			return
		}
		f.From, f.Ciphertext, f.Nonce = ev.From, ev.Ciphertext, ev.Nonce
	}
	p.cfg.OnRemoteResult(f)
}

// ── Кадри до VPS ─────────────────────────────────────────────────────────

// SendRemoteCmd ставить E2E-команду в чергу.
func (p *PresenceClient) SendRemoteCmd(reqID, ownerID, serverID, op, sid, ct, nonce string) error {
	console := strings.HasPrefix(op, "console_")
	if !remoteReqRe.MatchString(reqID) || !ValidAccountID(ownerID) || !remoteIDRe.MatchString(serverID) ||
		!ValidRemoteOp(op) || (console && !remoteReqRe.MatchString(sid)) || (!console && sid != "") || ct == "" || nonce == "" {
		return ErrBadInput
	}
	m := map[string]any{
		"type": "rs_cmd", "reqId": reqID, "toAccountId": ownerID, "serverId": serverID, "op": op,
		"ciphertext": ct, "nonce": nonce,
	}
	if console {
		m["sid"] = sid
	}
	frame, _ := json.Marshal(m)
	if !p.enqueue(frame) {
		return ErrQueueFull
	}
	p.poke()
	return nil
}

// SendRemoteReply ставить E2E-відповідь власника в чергу.
func (p *PresenceClient) SendRemoteReply(reqID, toID, ct, nonce string) error {
	if !remoteReqRe.MatchString(reqID) || !ValidAccountID(toID) || ct == "" || nonce == "" {
		return ErrBadInput
	}
	frame, _ := json.Marshal(map[string]any{
		"type": "rs_reply", "reqId": reqID, "toAccountId": toID, "ciphertext": ct, "nonce": nonce,
	})
	if !p.enqueue(frame) {
		return ErrQueueFull
	}
	p.poke()
	return nil
}

// remoteShares — бажаний стан публікації (власник). Тримається в пам'яті й повторюється після
// кожного (ре)конекту: VPS нічого не пам'ятає між рестартами.
type remoteShares struct {
	want      map[string]RemoteShare
	dirty     map[string]bool // потрібен повний rs_set
	stateOnly map[string]bool // достатньо rs_state
	clear     map[string]bool // потрібен rs_clear
	needKeep  bool
	// maybeStale: VPS міг запам'ятати публікацію з попереднього запуску. Без цього порожній
	// rs_keep не шлемо — зайвий кадр з'їдає бюджет 5 кадрів/с у тих, хто нічого не публікує.
	maybeStale bool
}

func newRemoteShares() remoteShares {
	return remoteShares{want: map[string]RemoteShare{}, dirty: map[string]bool{}, stateOnly: map[string]bool{}, clear: map[string]bool{}}
}

func sameShare(a, b RemoteShare) bool {
	if a.ServerID != b.ServerID || a.Name != b.Name || a.Color != b.Color || a.Core != b.Core || a.HostKey != b.HostKey || len(a.Grants) != len(b.Grants) {
		return false
	}
	for i := range a.Grants {
		if a.Grants[i] != b.Grants[i] {
			return false
		}
	}
	return true
}

// SetShares — повна заміна того, що публікується. Невалідне відкидається.
func (p *PresenceClient) SetShares(list []RemoteShare) {
	next := map[string]RemoteShare{}
	for _, s := range list {
		if !remoteIDRe.MatchString(s.ServerID) || len(next) >= maxRemoteShares {
			continue
		}
		c := RemoteShare{ServerID: s.ServerID, Name: CleanText(s.Name, MaxPackNameLen), State: SanitizeRemoteState(s.State),
			Color: SanitizeColor(s.Color), Core: CleanText(s.Core, 24)}
		seen := map[string]bool{}
		for _, g := range s.Grants {
			if !ValidAccountID(g.AccountID) || seen[g.AccountID] || (!g.Start && !g.Stop && !g.Console) || len(c.Grants) >= maxRemoteGrants {
				continue
			}
			g.Exec = g.Exec && g.Console
			seen[g.AccountID] = true
			c.Grants = append(c.Grants, g)
		}
		if len(c.Grants) == 0 {
			continue
		}
		sort.Slice(c.Grants, func(i, j int) bool { return c.Grants[i].AccountID < c.Grants[j].AccountID })
		next[s.ServerID] = c
	}
	p.mu.Lock()
	rs := &p.rshares
	for id := range rs.want {
		if _, keep := next[id]; !keep {
			rs.clear[id] = true
			delete(rs.dirty, id)
			delete(rs.stateOnly, id)
		}
	}
	for id, n := range next {
		old, had := rs.want[id]
		delete(rs.clear, id)
		switch {
		case !had || !sameShare(old, n):
			rs.dirty[id] = true
			delete(rs.stateOnly, id)
		case old.State != n.State && !rs.dirty[id]:
			rs.stateOnly[id] = true
		}
	}
	rs.want = next
	if len(next) > 0 {
		rs.maybeStale = true
	}
	p.mu.Unlock()
	p.poke()
}

// AssumeRemoteStale викликається, коли локально вже є/були права: тоді навіть порожній
// перелік треба підтвердити rs_keep, щоб VPS забув застарілі описи.
func (p *PresenceClient) AssumeRemoteStale() {
	p.mu.Lock()
	p.rshares.maybeStale = true
	p.mu.Unlock()
}

// SetShareState — лише стан одного вже опублікованого сервера.
func (p *PresenceClient) SetShareState(serverID, state string) {
	state = SanitizeRemoteState(state)
	p.mu.Lock()
	rs := &p.rshares
	s, ok := rs.want[serverID]
	if !ok || s.State == state {
		p.mu.Unlock()
		return
	}
	s.State = state
	rs.want[serverID] = s
	if !rs.dirty[serverID] {
		rs.stateOnly[serverID] = true
	}
	p.mu.Unlock()
	p.poke()
}

// resyncShares — після (ре)конекту VPS порожній: повторюємо все й завершуємо rs_keep.
func (p *PresenceClient) resyncShares() {
	p.mu.Lock()
	rs := &p.rshares
	for id := range rs.want {
		rs.dirty[id] = true
	}
	rs.stateOnly = map[string]bool{}
	rs.needKeep = len(rs.want) > 0 || rs.maybeStale
	p.mu.Unlock()
}

// takeShare повертає наступний кадр публікації або nil.
func (p *PresenceClient) takeShare() []byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	rs := &p.rshares
	for _, id := range sortedBoolKeys(rs.clear) {
		delete(rs.clear, id)
		b, _ := json.Marshal(map[string]any{"type": "rs_clear", "serverId": id})
		return b
	}
	for _, id := range sortedBoolKeys(rs.dirty) {
		delete(rs.dirty, id)
		delete(rs.stateOnly, id)
		s, ok := rs.want[id]
		if !ok {
			continue
		}
		grants := make([]map[string]any, 0, len(s.Grants))
		for _, g := range s.Grants {
			grants = append(grants, map[string]any{"a": g.AccountID, "s": g.Start, "x": g.Stop, "c": g.Console, "e": g.Exec})
		}
		b, _ := json.Marshal(map[string]any{
			"type": "rs_set", "serverId": s.ServerID, "name": s.Name, "state": s.State,
			"color": s.Color, "core": s.Core, "grants": grants, "hk": s.HostKey,
		})
		return b
	}
	for _, id := range sortedBoolKeys(rs.stateOnly) {
		delete(rs.stateOnly, id)
		if s, ok := rs.want[id]; ok {
			b, _ := json.Marshal(map[string]any{"type": "rs_state", "serverId": id, "state": s.State})
			return b
		}
	}
	if rs.needKeep {
		rs.needKeep = false
		ids := make([]string, 0, len(rs.want))
		for id := range rs.want {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		b, _ := json.Marshal(map[string]any{"type": "rs_keep", "serverIds": ids})
		return b
	}
	return nil
}

func sortedBoolKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ValidRemoteOp — відома операція віддаленого керування.
func ValidRemoteOp(op string) bool {
	switch op {
	case RemoteOpStart, RemoteOpStop, RemoteOpConsoleOpen, RemoteOpConsoleExec, RemoteOpConsoleClose:
		return true
	}
	return false
}

// RemoteStreamFrame — сирий кадр rs_stream_in (E2E-шматок консолі від власника).
type RemoteStreamFrame struct{ SID, From, Ciphertext, Nonce string }

func (p *PresenceClient) handleRemoteStreamIn(ev inEvent) {
	if p.cfg.OnRemoteStream == nil || !p.isFriend(ev.From) || !remoteReqRe.MatchString(ev.SID) ||
		ev.Ciphertext == "" || len(ev.Ciphertext) > maxCiphertextB64Len || ev.Nonce == "" || len(ev.Nonce) > 40 {
		return
	}
	p.cfg.OnRemoteStream(RemoteStreamFrame{SID: ev.SID, From: ev.From, Ciphertext: ev.Ciphertext, Nonce: ev.Nonce})
}

func (p *PresenceClient) handleRemoteStreamEnd(ev inEvent) {
	if p.cfg.OnRemoteStreamEnd == nil || !remoteReqRe.MatchString(ev.SID) {
		return
	}
	p.cfg.OnRemoteStreamEnd(ev.SID, strings.ToLower(CleanText(ev.Reason, 24)))
}

func (p *PresenceClient) handleRemoteKey(ev inEvent) {
	if p.cfg.OnRemoteKey != nil && ev.Key != "" && len(ev.Key) <= 64 {
		p.cfg.OnRemoteKey(ev.Key)
	}
}

// SendRemoteStream ставить E2E-шматок консолі (власник → друг) у чергу.
func (p *PresenceClient) SendRemoteStream(sid, toID, ct, nonce string) error {
	if !remoteReqRe.MatchString(sid) || !ValidAccountID(toID) || ct == "" || nonce == "" {
		return ErrBadInput
	}
	frame, _ := json.Marshal(map[string]any{"type": "rs_stream", "sid": sid, "toAccountId": toID, "ciphertext": ct, "nonce": nonce})
	if !p.enqueue(frame) {
		return ErrQueueFull
	}
	p.poke()
	return nil
}

// P2PMandateFrame — відповідь VPS на rs_p2p_open: мандат на пряме підключення або код відмови.
type P2PMandateFrame struct {
	ReqID, Owner, ServerID, Code string
	Mandate                      *RemoteMandate
}

func (p *PresenceClient) handleRemoteP2PMandate(ev inEvent) {
	if p.cfg.OnRemoteP2PMandate == nil || !remoteReqRe.MatchString(ev.ReqID) {
		return
	}
	f := P2PMandateFrame{ReqID: ev.ReqID, Owner: ev.Owner, ServerID: ev.ServerID, Code: strings.ToLower(CleanText(ev.Code, 32))}
	if f.Code == "" {
		if ev.Mandate == nil || !p.isFriend(ev.Owner) {
			return
		}
		f.Mandate = ev.Mandate
	}
	p.cfg.OnRemoteP2PMandate(f)
}

// SendRemoteP2POpen просить у VPS мандат на пряме підключення до власника. eph — хеш одноразового ключа.
func (p *PresenceClient) SendRemoteP2POpen(reqID, ownerID, serverID, eph string) error {
	if !remoteReqRe.MatchString(reqID) || !ValidAccountID(ownerID) || !remoteIDRe.MatchString(serverID) || len(eph) != 44 {
		return ErrBadInput
	}
	frame, _ := json.Marshal(map[string]any{"type": "rs_p2p_open", "reqId": reqID, "toAccountId": ownerID, "serverId": serverID, "eph": eph})
	if !p.enqueue(frame) {
		return ErrQueueFull
	}
	p.poke()
	return nil
}

// PipeFrame — шматок шифротексту тунелю (rs_pipe_in).
type PipeFrame struct {
	Tunnel, From string
	Data         []byte
}

// MaxPipeChunk — найбільший шматок тунелю, який приймає VPS (до base64).
const MaxPipeChunk = 16 * 1024

func (p *PresenceClient) handleRemotePipeIn(ev inEvent) {
	if p.cfg.OnRemotePipe == nil || !remoteReqRe.MatchString(ev.Tunnel) || !p.isFriend(ev.From) || len(ev.Data) > 24000 {
		return
	}
	raw, err := base64.StdEncoding.DecodeString(ev.Data)
	if err != nil || len(raw) == 0 || len(raw) > MaxPipeChunk+64 {
		return
	}
	p.cfg.OnRemotePipe(PipeFrame{Tunnel: ev.Tunnel, From: ev.From, Data: raw})
}

func (p *PresenceClient) handleRemotePipeEnd(ev inEvent) {
	if p.cfg.OnRemotePipeEnd != nil && remoteReqRe.MatchString(ev.Tunnel) {
		p.cfg.OnRemotePipeEnd(ev.Tunnel, strings.ToLower(CleanText(ev.Reason, 24)))
	}
}

// SendRemotePipe ставить шматок шифротексту тунелю в чергу (до VPS → другій стороні).
func (p *PresenceClient) SendRemotePipe(tunnel, toID string, data []byte) error {
	if !remoteReqRe.MatchString(tunnel) || !ValidAccountID(toID) || len(data) == 0 || len(data) > MaxPipeChunk+64 {
		return ErrBadInput
	}
	frame, _ := json.Marshal(map[string]any{"type": "rs_pipe", "tunnel": tunnel, "toAccountId": toID, "data": base64.StdEncoding.EncodeToString(data)})
	if !p.enqueue(frame) {
		return ErrQueueFull
	}
	p.poke()
	return nil
}
