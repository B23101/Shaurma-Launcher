// Package remotectl — права друзів на ВІДДАЛЕНИЙ запуск/зупинку серверів власника.
//
// Це ОСТАТОЧНИЙ рубіж захисту. VPS теж перевіряє права (щоб відсікти зайве раніше), але він
// не довірений: рішення «виконувати чи ні» приймає лише лаунчер власника за цим файлом.
//
// Принципи:
//   - за замовчуванням нікому нічого не дозволено (нема запису = нема доступу);
//   - права прив'язані до accountId власника (інший акаунт Shaurma у лаунчері має свої права);
//   - право привʼязане до ДРУЖБИ: якщо друга видалили і додали знову, старий дозвіл не
//     воскресає (перевірка Grant.Added проти Friend.Since);
//   - кожна спроба (дозволена чи ні) пишеться в журнал — власник бачить, хто що пробував;
//   - ліміти частоти — на друга, на сервер і загальні.
package remotectl

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"shaurma-launcher-backend/internal/atomicfile"
)

const (
	MaxServers             = 12
	MaxGrantsPerServer     = 40
	MaxAudit               = 300
	staleSkewMs            = 5 * 60 * 1000 // допуск розсинхрону годинників при перевірці «дозвіл старіший за дружбу»
	minGapSameServer       = 3 * time.Second
	maxPerFriendPerMin     = 10
	maxGlobalPerMin        = 30
	maxExecPerFriendPerMin = 90
	MaxCommandLen          = 256
	FileName               = "remote_access.json"
)

// Op — дозволені дії.
const (
	OpStart        = "start"
	OpStop         = "stop"
	OpConsoleOpen  = "console_open"
	OpConsoleExec  = "console_exec"
	OpConsoleClose = "console_close"
)

// Коди відмов (стабільні — фронтенд перекладає за ними).
const (
	CodeDisabled    = "disabled"     // власник призупинив усі віддалені дії
	CodeNotFriend   = "not_friend"   // відправник уже не друг
	CodeNotAllowed  = "not_allowed"  // немає запису/права (не розрізняємо — щоб не підказувати)
	CodeRateLimited = "rate_limited" // надто часто
	CodePlayers     = "players_online"
	CodeBadCommand  = "bad_command" // команда порожня/задовга/з керуючими символами
	CodePrivileged  = "privileged"  // команда з «привілейованого» списку, а власник цього не дозволив
)

var (
	ErrBadInput = errors.New("remotectl: bad input")
	ErrTooMany  = errors.New("remotectl: too many entries")
	ErrBadOp    = errors.New("remotectl: bad op")
)

type Grant struct {
	Start bool `json:"start"`
	Stop  bool `json:"stop"`
	// Console — дивитись консоль; Exec — виконувати в ній команди (діє лише разом з Console).
	Console bool  `json:"console,omitempty"`
	Exec    bool  `json:"exec,omitempty"`
	Added   int64 `json:"added"` // unix ms, коли власник видав право
}

type Policy struct {
	// AllowStopWithPlayers=false (за замовчуванням): друг не може зупинити сервер, поки на ньому
	// грають люди. Захист «від дурня»: випадково вимкнути сервер посеред чужої гри.
	AllowStopWithPlayers bool `json:"allowStopWithPlayers"`
	// AllowPrivileged=false (за замовчуванням): друг не може в консолі видавати op/deop/whitelist/save-off.
	AllowPrivileged bool             `json:"allowPrivileged,omitempty"`
	Grants          map[string]Grant `json:"grants"`
}

type AuditEntry struct {
	TS     int64  `json:"ts"`
	Friend string `json:"friend"`
	Nick   string `json:"nick,omitempty"`
	Server string `json:"server"`
	Name   string `json:"name,omitempty"`
	Op     string `json:"op"`
	Result string `json:"result"`        // ok | accepted | один із Code* | start_failed | …
	Cmd    string `json:"cmd,omitempty"` // для console_exec: що саме виконано (обрізано)
}

type ownerData struct {
	Paused  bool               `json:"paused"`
	Servers map[string]*Policy `json:"servers"`
	Audit   []AuditEntry       `json:"audit"`
}

type fileData struct {
	Owners map[string]*ownerData `json:"owners"`
}

type Store struct {
	path string
	mu   sync.Mutex
	data fileData

	// Лише в пам'яті: частота дозволених команд.
	lastOK map[string]time.Time // friend|server → час
	perMin map[string][]time.Time
}

// Open відкриває (або створює порожнє) сховище в <dataDir>/.shaurma/remote_access.json.
func Open(dataDir string) *Store {
	s := &Store{
		path:   filepath.Join(dataDir, ".shaurma", FileName),
		data:   fileData{Owners: map[string]*ownerData{}},
		lastOK: map[string]time.Time{},
		perMin: map[string][]time.Time{},
	}
	if b, err := os.ReadFile(s.path); err == nil {
		var d fileData
		if json.Unmarshal(b, &d) == nil && d.Owners != nil {
			s.data = d
		}
	}
	return s
}

func (s *Store) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	return atomicfile.WriteJSONAtomic(s.path, s.data)
}

func (s *Store) owner(id string) *ownerData {
	o := s.data.Owners[id]
	if o == nil {
		o = &ownerData{Servers: map[string]*Policy{}}
		s.data.Owners[id] = o
	}
	if o.Servers == nil {
		o.Servers = map[string]*Policy{}
	}
	return o
}

// ── Перегляд ──────────────────────────────────────────────────────────────

type GrantView struct {
	AccountID string `json:"accountId"`
	Start     bool   `json:"start"`
	Stop      bool   `json:"stop"`
	Console   bool   `json:"console"`
	Exec      bool   `json:"exec"`
	Added     int64  `json:"added"`
}

type ServerView struct {
	ServerID             string      `json:"serverId"`
	AllowStopWithPlayers bool        `json:"allowStopWithPlayers"`
	AllowPrivileged      bool        `json:"allowPrivileged"`
	Grants               []GrantView `json:"grants"`
}

type View struct {
	Paused  bool         `json:"paused"`
	Servers []ServerView `json:"servers"`
}

func (s *Store) View(owner string) View {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := View{Servers: []ServerView{}}
	o := s.data.Owners[owner]
	if o == nil {
		return v
	}
	v.Paused = o.Paused
	for id, p := range o.Servers {
		sv := ServerView{ServerID: id, AllowStopWithPlayers: p.AllowStopWithPlayers, AllowPrivileged: p.AllowPrivileged, Grants: []GrantView{}}
		for a, g := range p.Grants {
			sv.Grants = append(sv.Grants, GrantView{AccountID: a, Start: g.Start, Stop: g.Stop, Console: g.Console, Exec: g.Exec, Added: g.Added})
		}
		sort.Slice(sv.Grants, func(i, j int) bool { return sv.Grants[i].AccountID < sv.Grants[j].AccountID })
		v.Servers = append(v.Servers, sv)
	}
	sort.Slice(v.Servers, func(i, j int) bool { return v.Servers[i].ServerID < v.Servers[j].ServerID })
	return v
}

// Shared — що публікувати на VPS: сервери, де є хоч одне право. Порожньо, якщо призупинено.
func (s *Store) Shared(owner string) []ServerView {
	v := s.View(owner)
	if v.Paused {
		return nil
	}
	out := make([]ServerView, 0, len(v.Servers))
	for _, sv := range v.Servers {
		if len(sv.Grants) > 0 {
			out = append(out, sv)
		}
	}
	return out
}

// ── Зміни ─────────────────────────────────────────────────────────────────

func validID(s string) bool { return s != "" && len(s) <= 64 }

// SetGrant задає права друга на сервері. start=stop=false — забрати право.
func (s *Store) SetGrant(owner, serverID, friendID string, start, stop bool, nowMs int64) error {
	return s.SetGrantFull(owner, serverID, friendID, Rights{Start: start, Stop: stop}, nowMs)
}

// Rights — повний набір прав друга на сервері.
type Rights struct{ Start, Stop, Console, Exec bool }

// SetGrantFull задає всі права друга. Exec без Console скидається (команди «наосліп» заборонені).
func (s *Store) SetGrantFull(owner, serverID, friendID string, r Rights, nowMs int64) error {
	r.Exec = r.Exec && r.Console
	start, stop := r.Start, r.Stop
	if !validID(owner) || !validID(serverID) || !validID(friendID) || owner == friendID {
		return ErrBadInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	o := s.owner(owner)
	p := o.Servers[serverID]
	if !start && !stop && !r.Console {
		if p == nil {
			return nil
		}
		delete(p.Grants, friendID)
		if len(p.Grants) == 0 && !p.AllowStopWithPlayers {
			delete(o.Servers, serverID)
		}
		return s.save()
	}
	if p == nil {
		if len(o.Servers) >= MaxServers {
			return ErrTooMany
		}
		p = &Policy{Grants: map[string]Grant{}}
		o.Servers[serverID] = p
	}
	if p.Grants == nil {
		p.Grants = map[string]Grant{}
	}
	g, existed := p.Grants[friendID]
	if !existed {
		if len(p.Grants) >= MaxGrantsPerServer {
			return ErrTooMany
		}
		g.Added = nowMs
	}
	g.Start, g.Stop, g.Console, g.Exec = start, stop, r.Console, r.Exec
	p.Grants[friendID] = g
	return s.save()
}

func (s *Store) SetAllowStopWithPlayers(owner, serverID string, v bool) error {
	if !validID(owner) || !validID(serverID) {
		return ErrBadInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	o := s.owner(owner)
	p := o.Servers[serverID]
	if p == nil {
		if !v {
			return nil
		}
		if len(o.Servers) >= MaxServers {
			return ErrTooMany
		}
		p = &Policy{Grants: map[string]Grant{}}
		o.Servers[serverID] = p
	}
	p.AllowStopWithPlayers = v
	return s.save()
}

func (s *Store) SetPaused(owner string, v bool) error {
	if !validID(owner) {
		return ErrBadInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.owner(owner).Paused = v
	return s.save()
}

// RemoveFriend забирає ВСІ права друга на всіх серверах. Повертає, скільки прав прибрано.
func (s *Store) RemoveFriend(owner, friendID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o := s.data.Owners[owner]
	if o == nil {
		return 0, nil
	}
	n := 0
	for id, p := range o.Servers {
		if _, ok := p.Grants[friendID]; ok {
			delete(p.Grants, friendID)
			n++
		}
		if len(p.Grants) == 0 && !p.AllowStopWithPlayers {
			delete(o.Servers, id)
		}
	}
	if n == 0 {
		return 0, nil
	}
	return n, s.save()
}

// RemoveServer забуває сервер (видалено з лаунчера).
func (s *Store) RemoveServer(owner, serverID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	o := s.data.Owners[owner]
	if o == nil || o.Servers[serverID] == nil {
		return nil
	}
	delete(o.Servers, serverID)
	return s.save()
}

// ── Авторизація ───────────────────────────────────────────────────────────

type Decision struct {
	Allowed bool
	Code    string // порожній, якщо Allowed
	// AllowStopWithPlayers — політика сервера (потрібна викликачу для дії stop).
	AllowStopWithPlayers bool
	AllowPrivileged      bool
}

// Authorize вирішує, чи можна друга friendID допустити до дії op на сервері. Не виконує дію.
// isFriend/friendSince — актуальні дані зі СПИСКУ ДРУЗІВ лаунчера (не з мережевого кадру).
func (s *Store) Authorize(owner, friendID, serverID, op string, isFriend bool, friendSince int64, now time.Time) Decision {
	deny := func(code string) Decision { return Decision{Code: code} }
	switch op {
	case OpStart, OpStop, OpConsoleOpen, OpConsoleExec, OpConsoleClose:
	default:
		return deny(CodeNotAllowed)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	o := s.data.Owners[owner]
	if o == nil {
		return deny(CodeNotAllowed)
	}
	if o.Paused {
		return deny(CodeDisabled)
	}
	if !isFriend {
		return deny(CodeNotFriend)
	}
	p := o.Servers[serverID]
	if p == nil {
		return deny(CodeNotAllowed)
	}
	g, ok := p.Grants[friendID]
	if !ok {
		return deny(CodeNotAllowed)
	}
	// Дозвіл старіший за теперішню дружбу = друга видаляли й додали знову: право недійсне.
	if friendSince > 0 && g.Added+staleSkewMs < friendSince {
		delete(p.Grants, friendID)
		if len(p.Grants) == 0 && !p.AllowStopWithPlayers {
			delete(o.Servers, serverID)
		}
		_ = s.save()
		return deny(CodeNotAllowed)
	}
	if (op == OpStart && !g.Start) || (op == OpStop && !g.Stop) ||
		((op == OpConsoleOpen || op == OpConsoleClose) && !g.Console) ||
		(op == OpConsoleExec && !(g.Console && g.Exec)) {
		return deny(CodeNotAllowed)
	}
	// Консоль — часта подія (набір тексту): свої стелі, не ті, що для запуску/зупинки.
	if op == OpConsoleOpen || op == OpConsoleExec || op == OpConsoleClose {
		lim, key := 20, "c:"+friendID
		if op == OpConsoleExec {
			lim, key = maxExecPerFriendPerMin, "x:"+friendID
		}
		kept := s.perMin[key][:0]
		for _, t := range s.perMin[key] {
			if now.Sub(t) < time.Minute {
				kept = append(kept, t)
			}
		}
		if len(kept) >= lim {
			s.perMin[key] = kept
			return deny(CodeRateLimited)
		}
		s.perMin[key] = append(kept, now)
		return Decision{Allowed: true, AllowStopWithPlayers: p.AllowStopWithPlayers, AllowPrivileged: p.AllowPrivileged}
	}

	// Частота — лише для ДОЗВОЛЕНИХ дій (відмови й так нічого не роблять).
	key := friendID + "|" + serverID
	if last, ok := s.lastOK[key]; ok && now.Sub(last) < minGapSameServer {
		return deny(CodeRateLimited)
	}
	prune := func(k string) int {
		kept := s.perMin[k][:0]
		for _, t := range s.perMin[k] {
			if now.Sub(t) < time.Minute {
				kept = append(kept, t)
			}
		}
		s.perMin[k] = kept
		return len(kept)
	}
	if prune("f:"+friendID) >= maxPerFriendPerMin || prune("*") >= maxGlobalPerMin {
		return deny(CodeRateLimited)
	}
	s.perMin["f:"+friendID] = append(s.perMin["f:"+friendID], now)
	s.perMin["*"] = append(s.perMin["*"], now)
	s.lastOK[key] = now
	return Decision{Allowed: true, AllowStopWithPlayers: p.AllowStopWithPlayers}
}

// ── Журнал ────────────────────────────────────────────────────────────────

func (s *Store) AddAudit(owner string, e AuditEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o := s.owner(owner)
	o.Audit = append(o.Audit, e)
	if len(o.Audit) > MaxAudit {
		o.Audit = append([]AuditEntry(nil), o.Audit[len(o.Audit)-MaxAudit:]...)
	}
	_ = s.save()
}

// Audit повертає записи від найновіших; serverID порожній — усі сервери.
func (s *Store) Audit(owner, serverID string, limit int) []AuditEntry {
	if limit <= 0 || limit > MaxAudit {
		limit = 50
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []AuditEntry{}
	o := s.data.Owners[owner]
	if o == nil {
		return out
	}
	for i := len(o.Audit) - 1; i >= 0 && len(out) < limit; i-- {
		if serverID == "" || o.Audit[i].Server == serverID {
			out = append(out, o.Audit[i])
		}
	}
	return out
}

// SetAllowPrivileged — чи можуть друзі в консолі видавати op/deop/whitelist/save-off.
func (s *Store) SetAllowPrivileged(owner, serverID string, v bool) error {
	if !validID(owner) || !validID(serverID) {
		return ErrBadInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	o := s.owner(owner)
	p := o.Servers[serverID]
	if p == nil {
		if !v {
			return nil
		}
		if len(o.Servers) >= MaxServers {
			return ErrTooMany
		}
		p = &Policy{Grants: map[string]Grant{}}
		o.Servers[serverID] = p
	}
	p.AllowPrivileged = v
	return s.save()
}

var privilegedCommands = map[string]bool{"op": true, "deop": true, "whitelist": true, "save-off": true}

// Command — розібрана команда консолі.
type Command struct {
	Text string // очищений текст без початкового «/»
	Verb string // перше слово в нижньому регістрі
	// IsStop — команда зупиняє сервер: її треба авторизувати як дію stop (право Stop, політика гравців),
	// а не як звичайну команду консолі. Інакше право Exec обходило б право Stop.
	IsStop bool
}

// CheckCommand очищує й класифікує команду друга. Відхиляє порожні, задовгі й такі, що містять
// керуючі символи (\n, \r, NUL…): через них в один рядок можна було б «заховати» кілька команд.
func CheckCommand(raw string, allowPrivileged bool) (Command, string) {
	t := raw
	for _, r := range t {
		if r < 0x20 || r == 0x7f {
			return Command{}, CodeBadCommand
		}
	}
	t = trimSpace(t)
	for len(t) > 0 && t[0] == '/' {
		t = trimSpace(t[1:])
	}
	if t == "" || len(t) > MaxCommandLen {
		return Command{}, CodeBadCommand
	}
	verb := t
	for i := 0; i < len(t); i++ {
		if t[i] == ' ' {
			verb = t[:i]
			break
		}
	}
	verb = lower(verb)
	// Мод-префікс простору імен: «minecraft:stop», «minecraft:op» — те саме.
	if i := indexByte(verb, ':'); i >= 0 {
		verb = verb[i+1:]
	}
	c := Command{Text: t, Verb: verb, IsStop: verb == "stop" || verb == "restart"}
	if privilegedCommands[verb] && !allowPrivileged {
		return c, CodePrivileged
	}
	return c, ""
}

func trimSpace(s string) string {
	i, j := 0, len(s)
	for i < j && s[i] == ' ' {
		i++
	}
	for j > i && s[j-1] == ' ' {
		j--
	}
	return s[i:j]
}

func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

// HasConsole — чи ЗАРАЗ у друга є право на перегляд консолі (без лімітів частоти й побічних ефектів).
// Викликається для вже відкритих сесій: власник міг зняти право або поставити паузу.
func (s *Store) HasConsole(owner, friendID, serverID string, isFriend bool, friendSince int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	o := s.data.Owners[owner]
	if o == nil || o.Paused || !isFriend {
		return false
	}
	p := o.Servers[serverID]
	if p == nil {
		return false
	}
	g, ok := p.Grants[friendID]
	if !ok || !g.Console {
		return false
	}
	return !(friendSince > 0 && g.Added+staleSkewMs < friendSince)
}
