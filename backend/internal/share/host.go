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
	StateFailed       = "failed"       // одержувач повідомив про помилку
	StateRevoked      = "revoked"      // відправник скасував доступ
)

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
	}
	h.srv = &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	return h, nil
}

// Manifest — маніфест, що роздається.
func (h *Host) Manifest() *Manifest { return h.m }

// ErrTooManyRecipients — ліміт одержувачів на одну передачу.
var ErrTooManyRecipients = errors.New("одну збірку можна передавати не більше ніж трьом друзям одночасно")

// AddRecipient видає ключ одержувачу (id — акаунт друга). Повторний виклик для
// того самого id повертає той самий ключ.
func (h *Host) AddRecipient(id string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if r, ok := h.recips[id]; ok {
		return r.key, nil
	}
	if len(h.recips) >= MaxRecipients {
		return "", ErrTooManyRecipients
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	r := &recipient{id: id, key: hex.EncodeToString(b), state: StateWaiting, sem: make(chan struct{}, 4)}
	h.recips[id] = r
	h.byKey[r.key] = r
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
func (h *Host) Revoke(id string) bool {
	r := h.get(id)
	if r == nil {
		return false
	}
	r.revoked.Store(true)
	r.mu.Lock()
	if r.state != StateDone && r.state != StateDeclined {
		r.state = StateRevoked
	}
	r.mu.Unlock()
	h.notify(r)
	return true
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
		switch s.State {
		case StateDone, StateDeclined, StateFailed, StateRevoked:
		default:
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
	if found.revoked.Load() {
		http.Error(w, "gone", http.StatusGone)
		return nil
	}
	found.mu.Lock()
	found.lastSeen = time.Now().UnixMilli()
	found.mu.Unlock()
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
	case req.Method == http.MethodGet && req.URL.Path == "/v1/file":
		h.serveFile(w, req, r)
	case req.Method == http.MethodPost && req.URL.Path == "/v1/bundle":
		h.serveBundle(w, req, r)
	case req.Method == http.MethodPost && req.URL.Path == "/v1/progress":
		h.serveProgress(w, req, r)
	case req.Method == http.MethodPost && req.URL.Path == "/v1/decline":
		r.mu.Lock()
		if r.state != StateDone {
			r.state = StateDeclined
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
	if r.state == StateWaiting {
		r.state = StateTransferring
	}
	r.mu.Unlock()
	h.notify(r)
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
	State  string `json:"state"` // downloading | paused | done | error
	Paused bool   `json:"paused"`
	Error  string `json:"error,omitempty"`
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
	switch p.State {
	case "done":
		// «Готово» приймаємо лише якщо підтверджено ВСЕ — інакше це брехня або гонка.
		if r.bytesDone == h.m.TotalBytes && r.filesDone == len(h.m.Files) {
			r.state = StateDone
		}
	case "error":
		r.state = StateFailed
		r.errText = truncate(p.Error, 200)
	default:
		if r.state == StateFailed || r.state == StateWaiting {
			r.state = StateTransferring
			r.errText = ""
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
