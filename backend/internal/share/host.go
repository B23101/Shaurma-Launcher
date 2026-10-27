package share

import (
	"archive/tar"
	"compress/gzip"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Стани одержувача з погляду відправника.
const (
	StateWaiting      = "waiting"      // ключ видано, одержувач ще не підключався
	StateTransferring = "transferring" // качає
	StatePaused       = "paused"       // пауза (хто саме — PausedBy)
	StateDone         = "done"         // одержувач підтвердив, що все прийняв і перевірив
	StateDeclined     = "declined"     // відмовився
	StateFailed       = "failed"       // одержувач повідомив про помилку (Reason — код)
	StateRevoked      = "revoked"      // відправник скасував доступ
	StateCancelled    = "cancelled"    // одержувач сам зупинив прийом
	StateTimeout      = "timeout"      // не прийняв вчасно / зник посеред передачі й не повернувся
)

// IsFinalState — чи стан одержувача остаточний (передача з ним вирішена).
func IsFinalState(s string) bool {
	switch s {
	case StateDone, StateDeclined, StateFailed, StateRevoked, StateCancelled, StateTimeout:
		return true
	}
	return false
}

// Timeouts — ліміти очікування на боці відправника (нуль = значення за замовчуванням).
type Timeouts struct {
	// Accept — скільки чекати, поки одержувач узагалі підключиться (типово 1 год).
	Accept time.Duration
	// OfflineAfter — без жодного запиту стільки часу → «друг офлайн» (типово 30 с).
	OfflineAfter time.Duration
	// Offline — скільки чекати повернення зниклого одержувача (типово 15 хв).
	Offline time.Duration
}

func (t Timeouts) withDefaults() Timeouts {
	if t.Accept <= 0 {
		t.Accept = time.Hour
	}
	if t.OfflineAfter <= 0 {
		t.OfflineAfter = 30 * time.Second
	}
	if t.Offline <= 0 {
		t.Offline = 15 * time.Minute
	}
	return t
}

// RecipientState — знімок стану одного одержувача (для UI відправника).
type RecipientState struct {
	ID         string `json:"id"`
	State      string `json:"state"`
	PausedBy   string `json:"pausedBy,omitempty"` // "sender" | "receiver"
	BytesSent  int64  `json:"bytesSent"`          // реально віддано з цього ПК
	BytesDone  int64  `json:"bytesDone"`          // ПІДТВЕРДЖЕНО одержувачем (записано й звірено)
	FilesDone  int    `json:"filesDone"`
	FilesTotal int    `json:"filesTotal"`
	LastSeenMs int64  `json:"lastSeenMs"`
	Error      string `json:"error,omitempty"`
	// Reason — код причини для фінальних станів, крім done (див. Reason*).
	Reason string `json:"reason,omitempty"`
	// SpeedBPS — швидкість прийому, яку повідомив одержувач.
	SpeedBPS int64 `json:"speedBps"`
	// Offline — одержувач давно не виходив на зв'язок (ще не фінал, чекаємо повернення).
	Offline bool `json:"offline,omitempty"`
}

type recipient struct {
	id  string
	key string

	mu         sync.Mutex
	state      string
	recvPaused bool
	bytesDone  int64
	filesDone  int
	lastSeen   int64
	errText    string
	reason     string
	speed      int64
	created    time.Time
	offline    bool // останній стан «офлайн», про який уже сповістили

	sendPaused atomic.Bool
	revoked    atomic.Bool
	bytesSent  atomic.Int64
	sem        chan struct{} // одночасні потоки одного одержувача
}

// Host — сервер передачі на боці відправника.
type Host struct {
	root string
	m    *Manifest

	manifestJSON []byte
	manifestGz   []byte

	mu     sync.Mutex
	recips map[string]*recipient // за id акаунта
	byKey  map[string]*recipient

	onChange func(RecipientState)
	srv      *http.Server
	limits   Timeouts
}

// NewHost готує хост для root і маніфесту. onChange (необов'язковий) викликається
// при будь-якій зміні стану одержувача.
func NewHost(root string, m *Manifest, onChange func(RecipientState)) (*Host, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	var gz strings.Builder
	zw := gzip.NewWriter(&gz)
	_, _ = zw.Write(raw)
	_ = zw.Close()
	h := &Host{
		root: root, m: m, manifestJSON: raw, manifestGz: []byte(gz.String()),
		recips: map[string]*recipient{}, byKey: map[string]*recipient{}, onChange: onChange,
		limits: Timeouts{}.withDefaults(),
	}
	h.srv = &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	return h, nil
}

// SetTimeouts змінює ліміти очікування (до або під час роздачі).
func (h *Host) SetTimeouts(t Timeouts) {
	h.mu.Lock()
	h.limits = t.withDefaults()
	h.mu.Unlock()
}

// Manifest — маніфест, що роздається.
func (h *Host) Manifest() *Manifest { return h.m }

// ErrTooManyRecipients — ліміт одержувачів на одну передачу.
var ErrTooManyRecipients = errors.New("одну збірку можна передавати не більше ніж трьом друзям одночасно")

// AddRecipient видає ключ одержувачу (id — акаунт друга). Повторний виклик для
// того самого id повертає той самий ключ. Новий одержувач одразу потрапляє в onChange
// (стан waiting), щоб відправник бачив його ще до першого підключення.
func (h *Host) AddRecipient(id string) (string, error) {
	h.mu.Lock()
	if r, ok := h.recips[id]; ok {
		h.mu.Unlock()
		return r.key, nil
	}
	if len(h.recips) >= MaxRecipients {
		h.mu.Unlock()
		return "", ErrTooManyRecipients
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		h.mu.Unlock()
		return "", err
	}
	r := &recipient{id: id, key: hex.EncodeToString(b), state: StateWaiting, sem: make(chan struct{}, 4), created: time.Now()}
	h.recips[id] = r
	h.byKey[r.key] = r
	h.mu.Unlock()
	h.notify(r)
	return r.key, nil
}

// Serve приймає з'єднання на l. expectHandshake=true — кожне з'єднання починається
// з Minecraft-хендшейку (так приходить трафік через P2P-хост), його відкидаємо.
// Блокується до Close.
func (h *Host) Serve(l net.Listener, expectHandshake bool) error {
	if expectHandshake {
		l = skipListener{l}
	}
	err := h.srv.Serve(l)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Close зупиняє сервер і обриває активні потоки.
func (h *Host) Close() {
	h.mu.Lock()
	for _, r := range h.recips {
		r.revoked.Store(true)
	}
	h.mu.Unlock()
	_ = h.srv.Close()
}

// SetPaused — пауза/відновлення з боку відправника для одного одержувача.
func (h *Host) SetPaused(id string, paused bool) bool {
	r := h.get(id)
	if r == nil {
		return false
	}
	r.sendPaused.Store(paused)
	h.notify(r)
	return true
}

// Revoke закриває доступ одному одержувачу (його активні потоки обриваються).
// Повертає true, якщо одержувач існував і ще не мав фінального стану.
func (h *Host) Revoke(id string) bool { return h.revokeWith(id, ReasonCancelledSender) }

func (h *Host) revokeWith(id, reason string) bool {
	r := h.get(id)
	if r == nil {
		return false
	}
	r.revoked.Store(true)
	r.mu.Lock()
	changed := !IsFinalState(r.state)
	if changed {
		r.state, r.reason = StateRevoked, CleanReason(reason)
	}
	r.mu.Unlock()
	h.notify(r)
	return changed
}

// RevokeAll — передача закривається: усім, хто ще не завершив, доступ закрито з причиною
// reason (ReasonCancelledSender, ReasonTimeout…). Повертає id тих, кого це зачепило.
func (h *Host) RevokeAll(reason string) []string {
	var hit []string
	for _, id := range h.ids() {
		if h.revokeWith(id, reason) {
			hit = append(hit, id)
		}
	}
	return hit
}

// Pending — id одержувачів, що ще не мають фінального стану.
func (h *Host) Pending() []string {
	var out []string
	for _, s := range h.Snapshot() {
		if !IsFinalState(s.State) {
			out = append(out, s.ID)
		}
	}
	return out
}

// ApplyControl — фінальна подія від одержувача, що прийшла НЕ через P2P (зашифрований
// чат: друг відмовився чи зупинив прийом, коли тунель до нас не працює).
// event: "declined" | "cancelled" | "failed". Повертає true, якщо стан змінився.
func (h *Host) ApplyControl(id, event, reason string) bool {
	r := h.get(id)
	if r == nil {
		return false
	}
	r.mu.Lock()
	changed := false
	if r.state != StateDone && !r.revoked.Load() {
		switch event {
		case "declined":
			changed = r.state != StateDeclined
			r.state, r.reason = StateDeclined, ReasonDeclined
		case "cancelled":
			changed = !IsFinalState(r.state)
			if changed {
				r.state, r.reason = StateCancelled, ReasonCancelledReceiver
			}
		case "failed":
			changed = !IsFinalState(r.state)
			if changed {
				r.state, r.reason = StateFailed, CleanReason(reason)
			}
		}
	}
	r.mu.Unlock()
	if changed {
		h.notify(r)
	}
	return changed
}

// Sweep — періодична перевірка тайм-аутів (власник хоста викликає раз на кілька секунд):
// хто не підключився за Timeouts.Accept → StateTimeout/ReasonTimeout; хто зник посеред
// передачі — спершу Offline=true (видно в UI), а після Timeouts.Offline → StateTimeout/
// ReasonReceiverOffline.
func (h *Host) Sweep(now time.Time) {
	h.mu.Lock()
	lim := h.limits
	h.mu.Unlock()
	for _, id := range h.ids() {
		r := h.get(id)
		if r == nil {
			continue
		}
		r.mu.Lock()
		changed := false
		if !IsFinalState(r.state) {
			seen := time.UnixMilli(r.lastSeen)
			switch {
			case r.lastSeen == 0 && now.Sub(r.created) > lim.Accept:
				r.state, r.reason, changed = StateTimeout, ReasonTimeout, true
			case r.lastSeen != 0 && now.Sub(seen) > lim.Offline:
				r.state, r.reason, changed = StateTimeout, ReasonReceiverOffline, true
				r.revoked.Store(true) // активні потоки (якщо зависли) обриваємо
			default:
				off := r.lastSeen != 0 && now.Sub(seen) > lim.OfflineAfter
				if off != r.offline {
					r.offline, changed = off, true
				}
			}
		}
		r.mu.Unlock()
		if changed {
			h.notify(r)
		}
	}
}

func (h *Host) ids() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]string, 0, len(h.recips))
	for id := range h.recips {
		out = append(out, id)
	}
	return out
}

// Snapshot — стани всіх одержувачів.
func (h *Host) Snapshot() []RecipientState {
	h.mu.Lock()
	rs := make([]*recipient, 0, len(h.recips))
	for _, r := range h.recips {
		rs = append(rs, r)
	}
	h.mu.Unlock()
	out := make([]RecipientState, 0, len(rs))
	for _, r := range rs {
		out = append(out, h.stateOf(r))
	}
	return out
}

// AllSettled — чи кожен одержувач або завершив, або відмовився/скасований/впав
// (план: «передача завершена, коли кожен завершив або відмовився»).
func (h *Host) AllSettled() bool {
	for _, s := range h.Snapshot() {
		if !IsFinalState(s.State) {
			return false
		}
	}
	return true
}

func (h *Host) get(id string) *recipient {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.recips[id]
}

func (h *Host) stateOf(r *recipient) RecipientState {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := RecipientState{
		ID: r.id, State: r.state, BytesSent: r.bytesSent.Load(), BytesDone: r.bytesDone,
		FilesDone: r.filesDone, FilesTotal: len(h.m.Files), LastSeenMs: r.lastSeen, Error: r.errText,
	}
	switch {
	case !IsFinalState(s.State):
		s.Offline = r.offline
		if !r.offline {
			s.SpeedBPS = r.speed
		}
	case s.State != StateDone:
		s.Reason = CleanReason(r.reason)
	}
	switch s.State {
	case StateWaiting, StateTransferring, StatePaused:
		switch {
		case r.sendPaused.Load():
			s.State, s.PausedBy = StatePaused, "sender"
		case r.recvPaused:
			s.State, s.PausedBy = StatePaused, "receiver"
		case s.State == StatePaused:
			s.State = StateTransferring
		}
	}
	return s
}

func (h *Host) notify(r *recipient) {
	if h.onChange != nil {
		h.onChange(h.stateOf(r))
	}
}

func (h *Host) auth(w http.ResponseWriter, req *http.Request) *recipient {
	key := req.Header.Get(HeaderKey)
	if len(key) != 32 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return nil
	}
	h.mu.Lock()
	var found *recipient
	for k, r := range h.byKey {
		if subtle.ConstantTimeCompare([]byte(k), []byte(key)) == 1 {
			found = r
		}
	}
	h.mu.Unlock()
	if found == nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return nil
	}
	// Перегляд вмісту до прийняття — ще не «одержувач на зв'язку»: не чіпаємо ні стан,
	// ні lastSeen (інакше тайм-аут прийняття перетворився б на «зник посеред передачі»).
	preview := req.Method == http.MethodGet && req.URL.Path == "/v1/preview"
	found.mu.Lock()
	reason := found.reason
	gone := found.revoked.Load() || found.state == StateDeclined || found.state == StateTimeout
	back := false
	if !gone && !preview {
		found.lastSeen = time.Now().UnixMilli()
		back, found.offline = found.offline, false
	}
	found.mu.Unlock()
	if gone {
		if reason == "" {
			reason = ReasonCancelledSender
		}
		w.Header().Set(HeaderReason, CleanReason(reason))
		http.Error(w, "gone", http.StatusGone)
		return nil
	}
	if back {
		h.notify(found) // повернувся після «офлайн»
	}
	return found
}

func (h *Host) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	r := h.auth(w, req)
	if r == nil {
		return
	}
	switch {
	case req.Method == http.MethodGet && req.URL.Path == "/v1/manifest":
		h.serveManifest(w, req, r)
	case req.Method == http.MethodGet && req.URL.Path == "/v1/preview":
		h.writeManifest(w, req)
	case req.Method == http.MethodGet && req.URL.Path == "/v1/file":
		h.serveFile(w, req, r)
	case req.Method == http.MethodPost && req.URL.Path == "/v1/bundle":
		h.serveBundle(w, req, r)
	case req.Method == http.MethodPost && req.URL.Path == "/v1/progress":
		h.serveProgress(w, req, r)
	case req.Method == http.MethodPost && req.URL.Path == "/v1/decline":
		r.mu.Lock()
		if r.state != StateDone {
			r.state, r.reason = StateDeclined, ReasonDeclined
		}
		r.mu.Unlock()
		h.notify(r)
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, req)
	}
}

func (h *Host) serveManifest(w http.ResponseWriter, req *http.Request, r *recipient) {
	r.mu.Lock()
	// Одержувач (знову) підключився: чекання, власна зупинка чи збій — уже не фінал.
	if r.state == StateWaiting || r.state == StateCancelled || r.state == StateFailed {
		r.state, r.reason, r.errText = StateTransferring, "", ""
	}
	r.mu.Unlock()
	h.notify(r)
	h.writeManifest(w, req)
}

func (h *Host) writeManifest(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if strings.Contains(req.Header.Get("Accept-Encoding"), "gzip") {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Length", strconv.Itoa(len(h.manifestGz)))
		_, _ = w.Write(h.manifestGz)
		return
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(h.manifestJSON)))
	_, _ = w.Write(h.manifestJSON)
}

// gate — спільна перевірка перед віддачею даних: пауза відправника → 423.
func (h *Host) gate(w http.ResponseWriter, r *recipient) bool {
	if r.sendPaused.Load() {
		w.Header().Set("X-Share-Paused", "sender")
		w.Header().Set("Retry-After", "2")
		http.Error(w, "paused", http.StatusLocked)
		return false
	}
	select {
	case r.sem <- struct{}{}:
		return true
	default:
		w.Header().Set("Retry-After", "1")
		http.Error(w, "too many streams", http.StatusTooManyRequests)
		return false
	}
}

// abortWriter рахує віддані байти й обриває потік, коли відправник поставив
// паузу або скасував доступ.
type abortWriter struct {
	http.ResponseWriter
	r *recipient
}

func (a *abortWriter) Write(p []byte) (int, error) {
	if a.r.sendPaused.Load() || a.r.revoked.Load() {
		panic(http.ErrAbortHandler)
	}
	n, err := a.ResponseWriter.Write(p)
	a.r.bytesSent.Add(int64(n))
	return n, err
}

func (h *Host) openEntry(i int) (*os.File, FileEntry, error) {
	if i < 0 || i >= len(h.m.Files) {
		return nil, FileEntry{}, errors.New("bad index")
	}
	fe := h.m.Files[i]
	f, err := os.Open(filepath.Join(h.root, filepath.FromSlash(fe.Path)))
	if err != nil {
		return nil, fe, err
	}
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() != fe.Size {
		f.Close()
		return nil, fe, errors.New("file changed")
	}
	return f, fe, nil
}

func (h *Host) serveFile(w http.ResponseWriter, req *http.Request, r *recipient) {
	if !h.gate(w, r) {
		return
	}
	defer func() { <-r.sem }()
	i, err := strconv.Atoi(req.URL.Query().Get("i"))
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	f, fe, err := h.openEntry(i)
	if err != nil {
		code := http.StatusNotFound
		if err.Error() == "file changed" {
			code = http.StatusConflict // збірку змінили після побудови маніфесту
		}
		http.Error(w, err.Error(), code)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("ETag", `"`+fe.SHA256+`"`)
	http.ServeContent(&abortWriter{ResponseWriter: w, r: r}, req, "", time.Time{}, f)
}

func tarPad(n int64) int64 { return (512 - n%512) % 512 }

func (h *Host) serveBundle(w http.ResponseWriter, req *http.Request, r *recipient) {
	if !h.gate(w, r) {
		return
	}
	defer func() { <-r.sem }()
	var in struct {
		I []int `json:"i"`
	}
	if err := json.NewDecoder(io.LimitReader(req.Body, 64<<10)).Decode(&in); err != nil || len(in.I) == 0 || len(in.I) > 4*BundleMaxFiles {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	// Довжину відомо наперед: одержувач бачить обрив як помилку, а не «короткий файл».
	var total int64 = 1024
	seen := map[int]bool{}
	for _, i := range in.I {
		if i < 0 || i >= len(h.m.Files) || seen[i] {
			http.Error(w, "bad index", http.StatusBadRequest)
			return
		}
		seen[i] = true
		sz := h.m.Files[i].Size
		total += 512 + sz + tarPad(sz)
	}
	if total > 4*BundleMaxBytes+int64(len(in.I))*1024 {
		http.Error(w, "bundle too large", http.StatusRequestEntityTooLarge)
		return
	}
	w.Header().Set("Content-Type", "application/x-tar")
	w.Header().Set("Content-Length", strconv.FormatInt(total, 10))
	aw := &abortWriter{ResponseWriter: w, r: r}
	tw := tar.NewWriter(aw)
	for _, i := range in.I {
		f, fe, err := h.openEntry(i)
		if err != nil {
			panic(http.ErrAbortHandler) // посеред потоку чесної помилки не передати — рвемо
		}
		hdr := &tar.Header{Name: strconv.Itoa(i), Mode: 0o644, Size: fe.Size, Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(hdr); err != nil {
			f.Close()
			panic(http.ErrAbortHandler)
		}
		_, cerr := io.Copy(tw, f)
		f.Close()
		if cerr != nil {
			panic(http.ErrAbortHandler)
		}
	}
	if err := tw.Close(); err != nil {
		panic(http.ErrAbortHandler)
	}
}

// ProgressReport — те, що одержувач шле відправнику.
type ProgressReport struct {
	Bytes  int64  `json:"bytes"`
	Files  int    `json:"files"`
	State  string `json:"state"` // downloading | paused | done | error | cancelled
	Paused bool   `json:"paused"`
	Error  string `json:"error,omitempty"`
	// Reason — код причини для state=error (старі версії шлють лише Error).
	Reason string `json:"reason,omitempty"`
	// Speed — поточна швидкість прийому, байт/с (для ETA у відправника).
	Speed int64 `json:"speed,omitempty"`
}

func (h *Host) serveProgress(w http.ResponseWriter, req *http.Request, r *recipient) {
	var p ProgressReport
	if err := json.NewDecoder(io.LimitReader(req.Body, 8<<10)).Decode(&p); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	r.mu.Lock()
	if p.Bytes >= 0 && p.Bytes <= h.m.TotalBytes {
		r.bytesDone = p.Bytes
	}
	if p.Files >= 0 && p.Files <= len(h.m.Files) {
		r.filesDone = p.Files
	}
	r.recvPaused = p.Paused
	if p.Speed >= 0 && p.Speed < 1<<40 {
		r.speed = p.Speed
	}
	switch p.State {
	case "done":
		// «Готово» приймаємо лише якщо підтверджено ВСЕ — інакше це брехня або гонка.
		if r.bytesDone == h.m.TotalBytes && r.filesDone == len(h.m.Files) {
			r.state, r.reason = StateDone, ""
		}
	case "error":
		if r.state != StateDone {
			r.state = StateFailed
			r.errText = truncate(p.Error, 200)
			r.reason = CleanReason(p.Reason)
		}
	case "cancelled":
		if r.state != StateDone {
			r.state, r.reason = StateCancelled, ReasonCancelledReceiver
		}
	default:
		if r.state == StateFailed || r.state == StateWaiting || r.state == StateCancelled {
			r.state = StateTransferring
			r.errText, r.reason = "", ""
		}
	}
	r.mu.Unlock()
	h.notify(r)
	w.Header().Set("X-Share-Sender-Paused", strconv.FormatBool(r.sendPaused.Load()))
	w.WriteHeader(http.StatusNoContent)
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
