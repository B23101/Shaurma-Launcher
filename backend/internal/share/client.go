package share

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Стани одержувача (Progress.State).
const (
	ProgConnecting  = "connecting"
	ProgVerifying   = "verifying" // перевірка вже прийнятого після перезапуску
	ProgDownloading = "downloading"
	ProgPaused      = "paused"
	ProgDone        = "done"
	ProgError       = "error"
)

// Progress — прогрес для UI одержувача.
type Progress struct {
	State      string `json:"state"`
	PausedBy   string `json:"pausedBy,omitempty"` // "receiver" | "sender"
	BytesDone  int64  `json:"bytesDone"`
	BytesTotal int64  `json:"bytesTotal"`
	FilesDone  int    `json:"filesDone"`
	FilesTotal int    `json:"filesTotal"`
	SpeedBPS   int64  `json:"speedBps"`
	Current    string `json:"current,omitempty"`
	Error      string `json:"error,omitempty"`
	// Reason — код причини для State=error (див. Reason*).
	Reason string `json:"reason,omitempty"`
	// Reconnecting — зв'язок із відправником обірвався, пробуємо знову (ще не помилка).
	Reconnecting bool `json:"reconnecting,omitempty"`
	// CDNFiles / CDNBytes — скільки файлів уже взято напряму з Modrinth (а не від друга).
	CDNFiles int   `json:"cdnFiles,omitempty"`
	CDNBytes int64 `json:"cdnBytes,omitempty"`
}

// Options — параметри завантаження.
type Options struct {
	// Dial відкриває нове з'єднання до відправника (зазвичай DialMC через P2P-вхід).
	Dial func(ctx context.Context) (net.Conn, error)
	// Key — ключ одержувача з картки в чаті.
	Key string
	// StageDir — тека передачі (журнал + files/). Має бути ОКРЕМОЮ для кожного відправника/збірки.
	StageDir string
	// ExpectManifestID — ID вмісту з картки: якщо відправник підсуне інший — відмова.
	ExpectManifestID string
	// Parallel — одночасні потоки (за замовчуванням 3).
	Parallel   int
	OnProgress func(Progress)
	// StallTimeout — скільки без жодного прогресу повторювати спроби, перш ніж здатись
	// (за замовчуванням 10 хв). Прийняте лишається на диску — «продовжити» можливе.
	StallTimeout time.Duration
	// RetryBase — початкова затримка між спробами (за замовчуванням 1 с).
	RetryBase time.Duration
	// ExpectBytes / ExpectFiles — розмір і кількість файлів, які одержувач бачив, коли
	// погоджувався (з картки). Якщо маніфест інший — відмова (0 = не перевіряти).
	ExpectBytes int64
	ExpectFiles int
	// FreeSpace — вільне місце (байти) для теки; <0 = невідомо. nil = не перевіряти.
	FreeSpace func(dir string) int64
	// SenderGone — чи відправник точно офлайн (присутність друзів). Якщо так, після
	// SenderGoneGrace без зв'язку здаємося одразу, не чекаючи StallTimeout.
	SenderGone      func() bool
	SenderGoneGrace time.Duration
	// CDN — клієнт для файлів із src (Modrinth); nil = типовий. NoCDN — усе лише через P2P.
	CDN         *http.Client
	NoCDN       bool
	CDNParallel int
}

// Помилки, після яких повторювати марно.
var (
	ErrManifestMismatch = errors.New("відправник змінив вміст передачі — попросіть його надіслати збірку знову")
	ErrAccessClosed     = errors.New("доступ закрито відправником")
	ErrDeclined         = errors.New("ви відмовились від передачі")
	ErrDiskFull         = errors.New("недостатньо місця на диску")
	ErrSenderOffline    = errors.New("зв'язок із відправником втрачено")
	ErrIntegrity        = errors.New("файл пошкоджено під час передачі")
)

// AccessError — відправник закрив доступ; Reason — код причини з X-Share-Reason.
type AccessError struct{ Reason string }

func (e *AccessError) Error() string { return ErrAccessClosed.Error() }
func (e *AccessError) Unwrap() error { return ErrAccessClosed }

// ReasonOf — код причини (Reason*) для помилки завантаження.
func ReasonOf(err error) string {
	var ae *AccessError
	var hf hashFail
	switch {
	case err == nil:
		return ""
	case errors.As(err, &ae):
		return CleanReason(ae.Reason)
	case errors.Is(err, ErrAccessClosed):
		return ReasonAccess
	case errors.Is(err, ErrDeclined):
		return ReasonDeclined
	case errors.Is(err, ErrDiskFull) || isDiskFull(err):
		return ReasonDiskFull
	case errors.Is(err, ErrIntegrity) || errors.As(err, &hf):
		return ReasonIntegrity
	case errors.Is(err, ErrManifestMismatch):
		return ReasonChanged
	case errors.Is(err, ErrSenderOffline):
		return ReasonSenderOffline
	case errors.Is(err, context.Canceled):
		return ReasonCancelledReceiver
	}
	return ReasonError
}

type fatalErr struct{ error }

func (f fatalErr) Unwrap() error { return f.error }

// Downloader — сторона одержувача.
type Downloader struct {
	opt    Options
	client *http.Client

	mu       sync.Mutex
	m        *Manifest
	done     []bool  // за індексом файлу
	counted  []int64 // скільки байтів файлу вже враховано в bytesDone
	bytesDon int64
	filesDon int
	dirty    bool
	current  string

	pauseMu    sync.Mutex
	paused     bool
	pauseCh    chan struct{} // закривається, коли одержувач ставить паузу
	senderHold atomic.Bool
	lastProg   atomic.Int64 // UnixNano останнього реального прогресу
	speedBps   atomic.Int64
	declined   atomic.Bool

	failMu    sync.Mutex
	hashFails map[int]int

	cdnFiles int   // під d.mu
	cdnBytes int64 // під d.mu
	cdn      *http.Client

	stalled atomic.Bool  // зараз немає зв'язку з відправником (повторюємо)
	fatal   atomic.Value // error: фатальна помилка, що вже зупинила Run (для звіту)
}

// NewDownloader створює завантажувач.
func NewDownloader(opt Options) *Downloader {
	if opt.Parallel <= 0 {
		opt.Parallel = 3
	}
	if opt.StallTimeout <= 0 {
		opt.StallTimeout = 4 * time.Minute
	}
	if opt.SenderGoneGrace <= 0 {
		opt.SenderGoneGrace = 30 * time.Second
	}
	if opt.CDNParallel <= 0 {
		opt.CDNParallel = 4
	}
	if opt.RetryBase <= 0 {
		opt.RetryBase = time.Second
	}
	d := &Downloader{opt: opt, pauseCh: make(chan struct{}), cdn: opt.CDN}
	if d.cdn == nil {
		d.cdn = newCDNClient()
	}
	d.client = &http.Client{Transport: &http.Transport{
		DialContext:         func(ctx context.Context, _, _ string) (net.Conn, error) { return opt.Dial(ctx) },
		MaxIdleConnsPerHost: opt.Parallel,
		DisableCompression:  true, // самі просимо gzip лише для маніфесту
		IdleConnTimeout:     90 * time.Second,
	}}
	return d
}

// Pause ставить паузу з боку одержувача: активні запити обриваються, .part лишаються.
func (d *Downloader) Pause() {
	d.pauseMu.Lock()
	defer d.pauseMu.Unlock()
	if d.paused {
		return
	}
	d.paused = true
	close(d.pauseCh)
}

// Resume знімає паузу одержувача.
func (d *Downloader) Resume() {
	d.pauseMu.Lock()
	defer d.pauseMu.Unlock()
	if !d.paused {
		return
	}
	d.paused = false
	d.pauseCh = make(chan struct{})
}

func (d *Downloader) isPaused() (bool, <-chan struct{}) {
	d.pauseMu.Lock()
	defer d.pauseMu.Unlock()
	return d.paused, d.pauseCh
}

// reqCtx — контекст запиту, що скасовується паузою.
func (d *Downloader) reqCtx(parent context.Context) (context.Context, context.CancelFunc) {
	_, ch := d.isPaused()
	ctx, cancel := context.WithCancel(parent)
	go func() {
		select {
		case <-ch:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}

func (d *Downloader) do(ctx context.Context, method, path string, body []byte, hdr map[string]string) (*http.Response, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://share.local"+path, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set(HeaderKey, d.opt.Key)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	return d.client.Do(req)
}

// statusErr перетворює HTTP-статус на помилку з правильною «фатальністю».
func (d *Downloader) statusErr(resp *http.Response) error {
	switch resp.StatusCode {
	case http.StatusGone:
		return fatalErr{&AccessError{Reason: CleanReason(resp.Header.Get(HeaderReason))}}
	case http.StatusForbidden:
		return fatalErr{&AccessError{Reason: ReasonAccess}}
	case http.StatusLocked:
		d.senderHold.Store(true)
		return errSenderPaused
	case http.StatusTooManyRequests:
		return errBusy
	case http.StatusConflict:
		return fatalErr{ErrManifestMismatch}
	}
	return fmt.Errorf("відправник відповів %d", resp.StatusCode)
}

var (
	errSenderPaused = errors.New("відправник поставив на паузу")
	errBusy         = errors.New("відправник зайнятий")
	errPausedLocal  = errors.New("пауза")
)

// Manifest завантажує й перевіряє маніфест (з повторами при збоях мережі).
func (d *Downloader) Manifest(ctx context.Context) (*Manifest, error) {
	d.mu.Lock()
	if d.m != nil {
		m := d.m
		d.mu.Unlock()
		return m, nil
	}
	d.mu.Unlock()
	var m *Manifest
	err := d.retry(ctx, func() error {
		mm, err := d.getManifest(ctx, "/v1/manifest")
		m = mm
		return err
	})
	if err != nil {
		return nil, err
	}
	d.mu.Lock()
	d.m = m
	d.mu.Unlock()
	return m, nil
}

// ErrPreviewUnsupported — відправник зі старішою версією лаунчера не вміє показувати вміст.
var ErrPreviewUnsupported = errors.New("відправник має старішу версію лаунчера — перегляд вмісту недоступний")

// Preview — маніфест для перегляду вмісту ДО прийняття (одна спроба, без повторів):
// відправник при цьому не вважає одержувача підключеним. Ті самі перевірки, що й у Manifest.
func (d *Downloader) Preview(ctx context.Context) (*Manifest, error) {
	m, err := d.getManifest(ctx, "/v1/preview")
	var fe fatalErr
	if errors.As(err, &fe) {
		err = fe.error
	}
	return m, err
}

// getManifest — один запит маніфесту з повною перевіркою.
func (d *Downloader) getManifest(ctx context.Context, path string) (*Manifest, error) {
	m, err := func() (*Manifest, error) {
		resp, err := d.do(ctx, http.MethodGet, path, nil, map[string]string{"Accept-Encoding": "gzip"})
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound && path == "/v1/preview" {
			return nil, fatalErr{ErrPreviewUnsupported}
		}
		if resp.StatusCode != http.StatusOK {
			return nil, d.statusErr(resp)
		}
		var rd io.Reader = resp.Body
		if resp.Header.Get("Content-Encoding") == "gzip" {
			zr, zerr := gzipReader(resp.Body)
			if zerr != nil {
				return nil, zerr
			}
			rd = zr
		}
		var mm Manifest
		if err := json.NewDecoder(io.LimitReader(rd, 64<<20)).Decode(&mm); err != nil {
			return nil, err
		}
		if err := mm.Validate(); err != nil {
			return nil, fatalErr{err}
		}
		if d.opt.ExpectManifestID != "" && mm.ID() != d.opt.ExpectManifestID {
			return nil, fatalErr{ErrManifestMismatch}
		}
		// Погоджувались на конкретний розмір: маніфест не може бути більшим/іншим за картку.
		if (d.opt.ExpectBytes > 0 && mm.TotalBytes != d.opt.ExpectBytes) || (d.opt.ExpectFiles > 0 && len(mm.Files) != d.opt.ExpectFiles) {
			return nil, fatalErr{ErrManifestMismatch}
		}
		return &mm, nil
	}()
	return m, err
}

// retry повторює op з експоненційною паузою, доки є прогрес (або не вичерпано
// StallTimeout), і поважає паузу/скасування/фатальні помилки.
func (d *Downloader) retry(ctx context.Context, op func() error) error {
	delay := d.opt.RetryBase
	last := time.Now()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := d.waitIfPaused(ctx); err != nil {
			return err
		}
		before := d.lastProg.Load()
		err := op()
		if err == nil {
			d.stalled.Store(false)
			return nil
		}
		var fe fatalErr
		if errors.As(err, &fe) {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if p, _ := d.isPaused(); p || errors.Is(err, errPausedLocal) {
			continue // пауза — не збій
		}
		if d.lastProg.Load() != before {
			last = time.Now()
			delay = d.opt.RetryBase
		}
		if errors.Is(err, errSenderPaused) {
			d.stalled.Store(false)
			if serr := sleepCtx(ctx, 2*time.Second); serr != nil {
				return serr
			}
			last = time.Now() // пауза відправника не «зависання»
			continue
		}
		if !errors.Is(err, errBusy) && time.Since(last) > 2*d.opt.RetryBase {
			d.stalled.Store(true)
		}
		if time.Since(last) > d.opt.StallTimeout {
			return fmt.Errorf("%w: %v", ErrSenderOffline, err)
		}
		if d.opt.SenderGone != nil && time.Since(last) > d.opt.SenderGoneGrace && d.opt.SenderGone() {
			return fmt.Errorf("%w: відправник офлайн", ErrSenderOffline)
		}
		if serr := sleepCtx(ctx, delay); serr != nil {
			return serr
		}
		if delay *= 2; delay > 15*time.Second {
			delay = 15 * time.Second
		}
	}
}

func sleepCtx(ctx context.Context, dur time.Duration) error {
	t := time.NewTimer(dur)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (d *Downloader) waitIfPaused(ctx context.Context) error {
	for {
		p, _ := d.isPaused()
		if !p {
			return nil
		}
		if err := sleepCtx(ctx, 200*time.Millisecond); err != nil {
			return err
		}
	}
}

// ── стан і прогрес ───────────────────────────────────────────────────────

func (d *Downloader) setCounted(i int, n int64) {
	d.mu.Lock()
	d.bytesDon += n - d.counted[i]
	d.counted[i] = n
	d.mu.Unlock()
	d.lastProg.Store(time.Now().UnixNano())
}

func (d *Downloader) addCounted(i int, n int64) {
	d.mu.Lock()
	d.counted[i] += n
	d.bytesDon += n
	d.mu.Unlock()
	d.lastProg.Store(time.Now().UnixNano())
}

func (d *Downloader) markDone(i int) {
	d.mu.Lock()
	if !d.done[i] {
		d.done[i] = true
		d.filesDon++
		d.dirty = true
	}
	d.mu.Unlock()
}

func (d *Downloader) isDone(i int) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.done[i]
}

func (d *Downloader) snapshot(state string) Progress {
	d.mu.Lock()
	defer d.mu.Unlock()
	p := Progress{State: state, BytesDone: d.bytesDon, FilesDone: d.filesDon, Current: d.current, SpeedBPS: d.speedBps.Load(),
		CDNFiles: d.cdnFiles, CDNBytes: d.cdnBytes}
	if d.m != nil {
		p.BytesTotal, p.FilesTotal = d.m.TotalBytes, len(d.m.Files)
	}
	if state == ProgDownloading {
		if paused, _ := d.isPaused(); paused {
			p.State, p.PausedBy = ProgPaused, "receiver"
		} else if d.senderHold.Load() {
			p.State, p.PausedBy = ProgPaused, "sender"
		} else if d.stalled.Load() {
			p.Reconnecting, p.SpeedBPS = true, 0
		}
	}
	if state == ProgError {
		if e, ok := d.fatal.Load().(error); ok {
			p.Error, p.Reason = e.Error(), ReasonOf(e)
		}
	}
	return p
}

func (d *Downloader) emit(state string) {
	if d.opt.OnProgress != nil {
		d.opt.OnProgress(d.snapshot(state))
	}
}

func (d *Downloader) persist(complete bool) {
	d.mu.Lock()
	if d.m == nil || (!d.dirty && !complete) {
		d.mu.Unlock()
		return
	}
	j := journal{ManifestID: d.m.ID(), Complete: complete}
	for i, ok := range d.done {
		if ok {
			j.Done = append(j.Done, i)
		}
	}
	d.dirty = false
	d.mu.Unlock()
	_ = saveJournal(d.opt.StageDir, j)
}

func (d *Downloader) report(ctx context.Context, state, errText string) {
	d.reportReason(ctx, state, errText, "")
}

// ReportCancelled — сказати відправнику, що одержувач сам зупинив прийом (best-effort,
// коротко: тунель міг уже зникнути — тоді про це скаже повідомлення в чаті).
func (d *Downloader) ReportCancelled(ctx context.Context) {
	cctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	d.reportReason(cctx, "cancelled", "", ReasonCancelledReceiver)
}

func (d *Downloader) reportReason(ctx context.Context, state, errText, reason string) {
	d.mu.Lock()
	r := ProgressReport{Bytes: d.bytesDon, Files: d.filesDon, State: state, Error: errText, Reason: reason, Speed: d.speedBps.Load()}
	d.mu.Unlock()
	r.Paused, _ = d.isPaused()
	body, _ := json.Marshal(r)
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	resp, err := d.do(cctx, http.MethodPost, "/v1/progress", body, nil)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.Header.Get("X-Share-Sender-Paused") == "true" {
		d.senderHold.Store(true)
	} else if resp.Header.Get("X-Share-Sender-Paused") == "false" {
		d.senderHold.Store(false)
	}
}

// Decline повідомляє відправника, що одержувач відмовився (best-effort).
func (d *Downloader) Decline(ctx context.Context) error {
	d.declined.Store(true)
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	resp, err := d.do(cctx, http.MethodPost, "/v1/decline", []byte("{}"), nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// ── головний цикл ────────────────────────────────────────────────────────

type job struct {
	big  int   // індекс великого файлу, або -1
	many []int // партія дрібних
}

// Run качає все, що ще не прийнято. Повертає nil, коли УСІ файли прийняті й звірені
// (лежать у FilesDir(StageDir)). Помилка/скасування лишають недокачане на диску.
func (d *Downloader) Run(ctx context.Context) error {
	d.emit(ProgConnecting)
	m, err := d.Manifest(ctx)
	if err != nil {
		return d.failRun(err)
	}
	if err := d.prepare(ctx, m); err != nil {
		return d.failRun(err)
	}
	if raw, jerr := json.Marshal(m); jerr == nil {
		_ = os.WriteFile(filepath.Join(d.opt.StageDir, "manifest.json"), raw, 0o644)
	}
	// Місце на диску — до першого байта: «закінчилось місце на 90 %» гірше за чесне «ні» одразу.
	if d.opt.FreeSpace != nil {
		need := m.TotalBytes - d.confirmedBytes()
		if free := d.opt.FreeSpace(d.opt.StageDir); free >= 0 && free < need+diskMargin {
			err := fmt.Errorf("%w: потрібно ще %d МБ, вільно %d МБ", ErrDiskFull, (need+diskMargin)>>20, free>>20)
			return d.failRun(fatalErr{err})
		}
	}

	parent := ctx
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Прогрес-тікер: швидкість, подія в UI, журнал, звіт відправнику.
	tickDone := make(chan struct{})
	go func() {
		defer close(tickDone)
		t := time.NewTicker(500 * time.Millisecond)
		defer t.Stop()
		var lastBytes int64
		lastT := time.Now()
		n := 0
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			d.mu.Lock()
			cur := d.bytesDon
			d.mu.Unlock()
			now := time.Now()
			if dt := now.Sub(lastT).Seconds(); dt > 0 {
				inst := int64(float64(cur-lastBytes) / dt)
				if inst < 0 {
					inst = 0
				}
				d.speedBps.Store((d.speedBps.Load()*3 + inst) / 4)
			}
			lastBytes, lastT = cur, now
			d.emit(ProgDownloading)
			if n++; n%4 == 0 {
				d.persist(false)
				d.report(ctx, "downloading", "")
			}
		}
	}()

	jobs, cdnIdx := d.plan(m)
	// Черга P2P має вмістити й файли, що повернуться з CDN (відкат на P2P).
	queue := make(chan job, len(jobs)+len(cdnIdx))
	for _, j := range jobs {
		queue <- j
	}
	var wg sync.WaitGroup
	var errMu sync.Mutex
	var firstErr error
	fail := func(err error) {
		errMu.Lock()
		if firstErr == nil {
			firstErr = err
			cancel()
		}
		errMu.Unlock()
	}
	var pending atomic.Int64
	pending.Store(int64(len(jobs) + len(cdnIdx)))
	// Файли, що є на Modrinth, — паралельно з P2P напряму з CDN. Невдача → той самий файл
	// іде в чергу P2P (лічильник pending при цьому не зменшується: завдання переходить).
	if len(cdnIdx) > 0 {
		cdnQueue := make(chan int, len(cdnIdx))
		for _, i := range cdnIdx {
			cdnQueue <- i
		}
		close(cdnQueue)
		workers := d.opt.CDNParallel
		if workers > len(cdnIdx) {
			workers = len(cdnIdx)
		}
		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := range cdnQueue {
					if ctx.Err() != nil {
						return
					}
					err := d.fetchCDN(ctx, m, i)
					switch {
					case err == nil:
						pending.Add(-1)
					case errors.Is(err, errCDNFallback):
						queue <- job{big: i}
					default:
						fail(err)
						return
					}
				}
			}()
		}
	}
	for w := 0; w < d.opt.Parallel; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				if pending.Load() == 0 || ctx.Err() != nil {
					return
				}
				var j job
				select {
				case j = <-queue:
				case <-ctx.Done():
					return
				case <-time.After(100 * time.Millisecond):
					continue
				}
				if err := d.runJob(ctx, m, j); err != nil {
					fail(err)
					return
				}
				pending.Add(-1)
			}
		}()
	}
	wg.Wait()
	cancel()
	<-tickDone
	d.persist(false)

	errMu.Lock()
	err = firstErr
	errMu.Unlock()
	if err == nil && parent.Err() != nil {
		err = parent.Err() // скасування ззовні — не «успіх» і не «не завершено»
	}
	if err != nil {
		if errors.Is(err, context.Canceled) {
			d.emit(ProgPaused)
			return err
		}
		return d.failRun(err)
	}
	d.mu.Lock()
	all := d.filesDon == len(m.Files)
	d.mu.Unlock()
	if !all {
		return errors.New("передачу не завершено")
	}
	d.persist(true)
	d.emit(ProgDone)
	rctx, rc := context.WithTimeout(context.Background(), 8*time.Second)
	d.report(rctx, "done", "")
	rc()
	return nil
}

// diskMargin — запас вільного місця понад розмір передачі (журнал, тимчасові файли, ФС).
const diskMargin = 64 << 20

func (d *Downloader) confirmedBytes() int64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.bytesDon
}

// failRun — фатальне завершення Run: стан error з кодом причини в UI і (якщо зв'язок
// ще є) звіт відправнику. Закритий доступ відправник і так знає — йому не звітуємо.
func (d *Downloader) failRun(err error) error {
	d.fatal.Store(err)
	d.emit(ProgError)
	if !errors.Is(err, ErrAccessClosed) && !errors.Is(err, context.Canceled) {
		rctx, rc := context.WithTimeout(context.Background(), 5*time.Second)
		d.reportReason(rctx, "error", err.Error(), ReasonOf(err))
		rc()
	}
	return err
}

// prepare ініціалізує стан: журнал, уже прийняті файли, часткові .part.
func (d *Downloader) prepare(ctx context.Context, m *Manifest) error {
	stage := d.opt.StageDir
	files := FilesDir(stage)
	if j, ok := loadJournal(stage); ok && j.ManifestID != m.ID() {
		// Інший вміст — старі шматки не змішуємо.
		if err := os.RemoveAll(files); err != nil {
			return err
		}
		_ = os.Remove(journalPath(stage))
	}
	if err := os.MkdirAll(files, 0o755); err != nil {
		return fmt.Errorf("не вдалося створити теку прийому: %w", err)
	}
	d.mu.Lock()
	d.done = make([]bool, len(m.Files))
	d.counted = make([]int64, len(m.Files))
	d.mu.Unlock()
	if j, ok := loadJournal(stage); ok && len(j.Done) > 0 {
		// Після краху/вимкнення живлення файл із правильним розміром ще не означає правильний
		// вміст (кеш ФС міг не дописатись) — тож прийняте раніше переперевіряємо sha256.
		d.emit(ProgVerifying)
		for _, i := range j.Done {
			if err := ctx.Err(); err != nil {
				return err
			}
			if i < 0 || i >= len(m.Files) {
				continue
			}
			fe := m.Files[i]
			p, perr := d.safePath(fe.Path)
			if perr != nil {
				continue
			}
			st, err := os.Stat(p)
			if err != nil || !st.Mode().IsRegular() || st.Size() != fe.Size {
				continue
			}
			if sum, _, herr := hashFile(ctx, p); herr != nil || sum != fe.SHA256 {
				_ = os.Remove(p)
				continue
			}
			d.markDone(i)
			d.setCounted(i, fe.Size)
		}
	}
	return nil
}

func (d *Downloader) finalPath(rel string) string {
	return filepath.Join(FilesDir(d.opt.StageDir), filepath.FromSlash(rel))
}

func (d *Downloader) safePath(rel string) (string, error) {
	p := d.finalPath(rel)
	base := FilesDir(d.opt.StageDir)
	r, err := filepath.Rel(base, p)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) || filepath.IsAbs(r) {
		return "", fmt.Errorf("%w: %q", ErrUnsafePath, rel)
	}
	return p, nil
}

var emptySHA = func() string { s := sha256.Sum256(nil); return hex.EncodeToString(s[:]) }()

// plan розкладає недокачане на завдання: великі окремо, дрібні — партіями.
func (d *Downloader) plan(m *Manifest) ([]job, []int) {
	var big, small, cdn []int
	for i, f := range m.Files {
		if d.isDone(i) {
			continue
		}
		if !d.opt.NoCDN && f.Size > 0 && f.Src != nil && ValidCDNURL(f.Src.URL) {
			cdn = append(cdn, i)
			continue
		}
		if f.Size == 0 && f.SHA256 == emptySHA {
			if p, err := d.safePath(f.Path); err == nil {
				if os.MkdirAll(filepath.Dir(p), 0o755) == nil {
					if os.WriteFile(p, nil, 0o644) == nil {
						d.markDone(i)
						continue
					}
				}
			}
		}
		if f.Size >= BigFileThreshold {
			big = append(big, i)
		} else {
			small = append(small, i)
		}
	}
	// Великі — від найбільших, щоб хвіст не був одним довгим файлом.
	sort.Slice(big, func(a, b int) bool { return m.Files[big[a]].Size > m.Files[big[b]].Size })
	var jobs []job
	for _, i := range big {
		jobs = append(jobs, job{big: i})
	}
	var cur []int
	var curBytes int64
	flush := func() {
		if len(cur) > 0 {
			jobs = append(jobs, job{big: -1, many: cur})
			cur, curBytes = nil, 0
		}
	}
	for _, i := range small {
		if len(cur) >= BundleMaxFiles || curBytes+m.Files[i].Size > BundleMaxBytes {
			flush()
		}
		cur = append(cur, i)
		curBytes += m.Files[i].Size
	}
	flush()
	return jobs, cdn
}

func (d *Downloader) runJob(ctx context.Context, m *Manifest, j job) error {
	if j.big >= 0 {
		return d.retry(ctx, func() error { return d.fetchBig(ctx, m, j.big) })
	}
	return d.retry(ctx, func() error { return d.fetchBundle(ctx, m, j.many) })
}

type hashFail struct{ path string }

func (h hashFail) Error() string {
	return "контрольна сума не збіглась: " + h.path
}

func (h hashFail) Unwrap() error { return ErrIntegrity }

// diskErr — помилка запису: «диск заповнено» окремо (у UI — ReasonDiskFull).
func diskErr(err error) error {
	if isDiskFull(err) {
		return fmt.Errorf("%w: %v", ErrDiskFull, err)
	}
	return fmt.Errorf("запис на диск: %w", err)
}

func (d *Downloader) setCurrent(s string) {
	d.mu.Lock()
	d.current = s
	d.mu.Unlock()
}

// fetchBig — один великий файл з докачуванням через Range.
func (d *Downloader) fetchBig(parent context.Context, m *Manifest, i int) error {
	if d.isDone(i) {
		return nil
	}
	fe := m.Files[i]
	final, err := d.safePath(fe.Path)
	if err != nil {
		return fatalErr{err}
	}
	if err := os.MkdirAll(filepath.Dir(final), 0o755); err != nil {
		return fatalErr{err}
	}
	part := final + ".part"
	d.setCurrent(fe.Path)

	f, err := os.OpenFile(part, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fatalErr{err}
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	off := st.Size()
	if off > fe.Size {
		off = 0
		if err := f.Truncate(0); err != nil {
			return err
		}
	}
	h := sha256.New()
	if off > 0 {
		if _, err := io.CopyN(h, f, off); err != nil {
			off = 0
			h.Reset()
			if terr := f.Truncate(0); terr != nil {
				return terr
			}
		}
	}
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return err
	}
	d.setCounted(i, off)

	if off < fe.Size {
		ctx, cancel := d.reqCtx(parent)
		defer cancel()
		hdr := map[string]string{}
		if off > 0 {
			hdr["Range"] = "bytes=" + strconv.FormatInt(off, 10) + "-"
			hdr["If-Range"] = `"` + fe.SHA256 + `"`
		}
		resp, err := d.do(ctx, http.MethodGet, "/v1/file?i="+strconv.Itoa(i), nil, hdr)
		if err != nil {
			return d.pauseAware(parent, err)
		}
		defer resp.Body.Close()
		switch {
		case resp.StatusCode == http.StatusPartialContent && off > 0:
		case resp.StatusCode == http.StatusOK:
			if off > 0 { // сервер проігнорував Range — починаємо спочатку
				off = 0
				h.Reset()
				if _, err := f.Seek(0, io.SeekStart); err != nil {
					return err
				}
				if err := f.Truncate(0); err != nil {
					return err
				}
				d.setCounted(i, 0)
			}
		default:
			return d.statusErr(resp)
		}
		buf := make([]byte, 256<<10)
		written := off
		for {
			n, rerr := resp.Body.Read(buf)
			if n > 0 {
				if written+int64(n) > fe.Size {
					return d.dropBad(f, part, i, hashFail{fe.Path})
				}
				if _, werr := f.Write(buf[:n]); werr != nil {
					return fatalErr{diskErr(werr)}
				}
				h.Write(buf[:n])
				written += int64(n)
				d.addCounted(i, int64(n))
			}
			if rerr == io.EOF {
				break
			}
			if rerr != nil {
				return d.pauseAware(parent, rerr)
			}
		}
		if written != fe.Size {
			return io.ErrUnexpectedEOF
		}
	}
	return d.finishFile(f, part, final, i, h, fe)
}

func (d *Downloader) pauseAware(parent context.Context, err error) error {
	if parent.Err() != nil {
		return parent.Err()
	}
	if p, _ := d.isPaused(); p {
		return errPausedLocal
	}
	return err
}

func (d *Downloader) dropBad(f *os.File, part string, i int, err error) error {
	f.Close()
	_ = os.Remove(part)
	d.setCounted(i, 0)
	return err
}

// maxHashFails — скільки разів поспіль файл може не пройти перевірку, перш ніж
// передачу буде визнано зіпсованою (інакше вічний цикл «качаємо те саме»).
const maxHashFails = 3

func (d *Downloader) noteHashFail(i int) bool {
	d.failMu.Lock()
	defer d.failMu.Unlock()
	if d.hashFails == nil {
		d.hashFails = map[int]int{}
	}
	d.hashFails[i]++
	return d.hashFails[i] >= maxHashFails
}

func (d *Downloader) finishFile(f *os.File, part, final string, i int, h hash.Hash, fe FileEntry) error {
	if hex.EncodeToString(h.Sum(nil)) != fe.SHA256 {
		var err error = hashFail{fe.Path}
		if d.noteHashFail(i) {
			err = fatalErr{err}
		}
		return d.dropBad(f, part, i, err)
	}
	if err := f.Close(); err != nil {
		return fatalErr{err}
	}
	_ = os.Remove(final)
	if err := os.Rename(part, final); err != nil {
		return fatalErr{fmt.Errorf("не вдалося зберегти %s: %w", fe.Path, err)}
	}
	d.setCounted(i, fe.Size)
	d.markDone(i)
	return nil
}

// fetchBundle — партія дрібних файлів одним tar-потоком.
func (d *Downloader) fetchBundle(parent context.Context, m *Manifest, idxs []int) error {
	var want []int
	for _, i := range idxs {
		if !d.isDone(i) {
			want = append(want, i)
		}
	}
	if len(want) == 0 {
		return nil
	}
	body, _ := json.Marshal(map[string][]int{"i": want})
	ctx, cancel := d.reqCtx(parent)
	defer cancel()
	d.setCurrent(m.Files[want[0]].Path)
	resp, err := d.do(ctx, http.MethodPost, "/v1/bundle", body, nil)
	if err != nil {
		return d.pauseAware(parent, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return d.statusErr(resp)
	}
	allowed := make(map[int]bool, len(want))
	for _, i := range want {
		allowed[i] = true
	}
	tr := tar.NewReader(resp.Body)
	got := 0
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return d.pauseAware(parent, err)
		}
		i, cerr := strconv.Atoi(hdr.Name)
		if cerr != nil || !allowed[i] || hdr.Typeflag != tar.TypeReg || hdr.Size != m.Files[i].Size {
			return fatalErr{errors.New("відправник надіслав некоректну партію файлів")}
		}
		delete(allowed, i) // кожен індекс рівно раз
		fe := m.Files[i]
		final, perr := d.safePath(fe.Path)
		if perr != nil {
			return fatalErr{perr}
		}
		if err := os.MkdirAll(filepath.Dir(final), 0o755); err != nil {
			return fatalErr{err}
		}
		part := final + ".part"
		f, err := os.OpenFile(part, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			return fatalErr{err}
		}
		h := sha256.New()
		d.setCounted(i, 0)
		buf := make([]byte, 128<<10)
		for {
			n, rerr := tr.Read(buf)
			if n > 0 {
				if _, werr := f.Write(buf[:n]); werr != nil {
					f.Close()
					return fatalErr{diskErr(werr)}
				}
				h.Write(buf[:n])
				d.addCounted(i, int64(n))
			}
			if rerr == io.EOF {
				break
			}
			if rerr != nil {
				f.Close()
				_ = os.Remove(part)
				d.setCounted(i, 0)
				return d.pauseAware(parent, rerr)
			}
		}
		if err := d.finishFile(f, part, final, i, h, fe); err != nil {
			return err
		}
		got++
	}
	if got != len(want) {
		return io.ErrUnexpectedEOF // решту дотягне наступна спроба (готові вже в журналі)
	}
	return nil
}
