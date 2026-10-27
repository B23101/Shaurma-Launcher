// trace.go — інструменти МАКСИМАЛЬНОГО логування P2P (без tsnet-залежностей;
// tsnet-частина — у trace_ts.go).
//
// Принцип той самий, що й у diag.go: у launcher.log має бути видно ЩО,
// НА ЯКОМУ кроці, СКІЛЬКИ це зайняло і ЧИМ закінчилось. Для цього:
//
//   - кожна сесія/з'єднання/запит має короткий ID (h1, g7, L42 …) — за ним
//     у логу склеюється весь життєвий цикл одного підключення;
//   - кожен крок міряється (stopwatch) — одразу видно, ДЕ саме «зависло»;
//   - HTTP-запити до lobby трасуються по фазах (DNS / connect / TLS / TTFB);
//   - у кінці кожного проксі-з'єднання — байти в обидва боки, час першого
//     байта і причина завершення (pipeStats);
//   - секрети (auth key, ownerSecret, токен) НІКОЛИ не потрапляють у лог —
//     лише маска (maskSecret / redactSecrets).
//
// Компоненти логу (колонка у дужках): p2p-api, p2p-host, p2p-guest, p2p-udp,
// p2p-lobby, p2p-node, p2p-net, p2p-tsnet.
//
// Змінні середовища:
//
//	SHAURMA_P2P_TSNET_LOG=0  — вимкнути прокидання «сирих» логів tsnet/magicsock
package p2p

import (
	"shaurma-launcher-backend/internal/crashguard"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"os"
	"regexp"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"shaurma-launcher-backend/internal/launchlog"
)

// Назви компонентів у launcher.log.
const (
	compAPI   = "p2p-api"
	compHost  = "p2p-host"
	compGuest = "p2p-guest"
	compUDP   = "p2p-udp"
	compLobby = "p2p-lobby"
	compNode  = "p2p-node"
	compNet   = "p2p-net"
	compTsnet = "p2p-tsnet"
)

// ── ID ────────────────────────────────────────────────────────────────────

var traceSeq atomic.Uint64

// newTraceID повертає короткий наскрізний ID: "g17", "h3", "L204".
func newTraceID(prefix string) string {
	return fmt.Sprintf("%s%d", prefix, traceSeq.Add(1))
}

// ── Секрети ───────────────────────────────────────────────────────────────

// maskSecret показує лише префікс і довжину: достатньо, щоб побачити «ключ
// порожній / не того формату», але не достатньо, щоб ним скористатись.
func maskSecret(s string) string {
	if s == "" {
		return "<ПОРОЖНІЙ>"
	}
	if len(s) <= 8 {
		return fmt.Sprintf("***(len=%d)", len(s))
	}
	return fmt.Sprintf("%s***(len=%d)", s[:6], len(s))
}

var (
	secretJSONRe = regexp.MustCompile(`("(?:authKey|ownerSecret|token|secret)"\s*:\s*")([^"]*)(")`)
	tskeyRe      = regexp.MustCompile(`tskey-[A-Za-z0-9_-]+`)
	urlUserRe    = regexp.MustCompile(`://[^/@\s]+@`)
)

// redactSecrets прибирає з довільного тексту auth key / ownerSecret / токени.
func redactSecrets(s string) string {
	s = secretJSONRe.ReplaceAllString(s, "${1}***${3}")
	s = tskeyRe.ReplaceAllString(s, "tskey-***")
	return urlUserRe.ReplaceAllString(s, "://***@")
}

// snippet — безпечний (редагований і обрізаний) фрагмент тіла для логу.
func snippet(b []byte, max int) string {
	s := redactSecrets(string(b))
	s = strings.NewReplacer("\r", " ", "\n", " ", "\t", " ").Replace(s)
	r := []rune(s)
	if len(r) > max {
		return string(r[:max]) + fmt.Sprintf("…(+%d симв.)", len(r)-max)
	}
	return s
}

// ── Час ───────────────────────────────────────────────────────────────────

type stopwatch time.Time

func startWatch() stopwatch { return stopwatch(time.Now()) }

func (s stopwatch) Elapsed() time.Duration { return time.Since(time.Time(s)) }

func (s stopwatch) String() string { return fmtDur(s.Elapsed()) }

func fmtDur(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.2fs", d.Seconds())
}

func fmtBytes(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%dB", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1fKiB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.2fMiB", float64(n)/(1024*1024))
	}
}

// logGate — ліміт частоти для логів, які можуть сипатись на кожен пакет
// (UDP-голос). Пропускає не частіше, ніж раз на every, і каже, скільки
// повідомлень було проковтнуто між спрацюваннями.
type logGate struct {
	every   time.Duration
	mu      sync.Mutex
	last    time.Time
	skipped uint64
}

func newLogGate(every time.Duration) *logGate { return &logGate{every: every} }

func (g *logGate) allow() (ok bool, skipped uint64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	if g.last.IsZero() || now.Sub(g.last) >= g.every {
		g.last = now
		skipped = g.skipped
		g.skipped = 0
		return true, skipped
	}
	g.skipped++
	return false, 0
}

// ── Проксі-з'єднання зі статистикою ───────────────────────────────────────

// pipeStats — підсумок одного проксі-з'єднання. A і B — два кінці pipeBothStats.
type pipeStats struct {
	AtoB, BtoA           int64
	AtoBErr, BtoAErr     error
	FirstAtoB, FirstBtoA time.Duration // -1 = жодного байта
	Dur                  time.Duration
}

type tapWriter struct {
	w     io.Writer
	n     atomic.Int64
	first atomic.Int64 // UnixNano першого запису; 0 = ще не було
}

func (t *tapWriter) Write(p []byte) (int, error) {
	n, err := t.w.Write(p)
	if n > 0 {
		t.n.Add(int64(n))
		t.first.CompareAndSwap(0, time.Now().UnixNano())
	}
	return n, err
}

func (t *tapWriter) firstAfter(start time.Time) time.Duration {
	f := t.first.Load()
	if f == 0 {
		return -1
	}
	return time.Duration(f - start.UnixNano())
}

func closeWrite(c net.Conn) {
	if cw, ok := c.(interface{ CloseWrite() error }); ok {
		_ = cw.CloseWrite()
	}
}

// pipeBothStats копіює трафік в обидва боки (так само, як і раніше pipeBoth),
// але повертає статистику: скільки байт пішло в кожен бік, коли з'явився
// перший байт і чим завершився кожен напрямок.
func pipeBothStats(a, b net.Conn) pipeStats {
	start := time.Now()
	ta := &tapWriter{w: b} // A → B
	tb := &tapWriter{w: a} // B → A
	var s pipeStats
	tuneConn(a)
	tuneConn(b)
	var wg sync.WaitGroup
	wg.Add(2)
	first := make(chan struct{}, 2)
	go func() {
		defer crashguard.Recover("trace.go:208")
		defer wg.Done()
		_, s.AtoBErr = copyPooled(ta, a)
		closeWrite(b)
		first <- struct{}{}
	}()
	go func() {
		defer crashguard.Recover("trace.go:213")
		defer wg.Done()
		_, s.BtoAErr = copyPooled(tb, b)
		closeWrite(a)
		first <- struct{}{}
	}()
	all := make(chan struct{})
	go func() { wg.Wait(); close(all) }()
	// Коли один бік закінчив, а другий мовчить довше за pipeHalfCloseGrace — це «зомбі»
	// (клієнт пішов, а FIN не дійшов): примусово закриваємо обидва кінці, інакше
	// з'єднання, слот кімнати й гравець на сервері висять, і зайти знову не виходить.
	<-first
	select {
	case <-all:
	case <-time.After(pipeHalfCloseGrace):
		_ = a.Close()
		_ = b.Close()
		<-all
	}
	s.AtoB, s.BtoA = ta.n.Load(), tb.n.Load()
	s.FirstAtoB, s.FirstBtoA = ta.firstAfter(start), tb.firstAfter(start)
	s.Dur = time.Since(start)
	return s
}

// describe — людський підсумок: aName/bName — як називати кінці в логу.
func (s pipeStats) describe(aName, bName string) string {
	first := func(d time.Duration) string {
		if d < 0 {
			return "байтів не було"
		}
		return "перший байт через " + fmtDur(d)
	}
	return fmt.Sprintf("%s→%s %s (%s) | %s→%s %s (%s) | тривалість %s | кінець: %s→%s=%s, %s→%s=%s",
		aName, bName, fmtBytes(s.AtoB), first(s.FirstAtoB),
		bName, aName, fmtBytes(s.BtoA), first(s.FirstBtoA),
		fmtDur(s.Dur),
		aName, bName, describeCopyErr(s.AtoBErr),
		bName, aName, describeCopyErr(s.BtoAErr))
}

// describeCopyErr: nil/EOF/закрите з'єднання — штатне завершення; решта — як є.
func describeCopyErr(err error) string {
	switch {
	case err == nil, errors.Is(err, io.EOF):
		return "ok(EOF)"
	case errors.Is(err, net.ErrClosed), strings.Contains(strings.ToLower(err.Error()), "use of closed network connection"):
		return "закрито"
	}
	return "ПОМИЛКА: " + err.Error()
}

// ── HTTP-трасування запитів до lobby ─────────────────────────────────────

// httpTimings збирає фази одного HTTP-запиту (DNS/connect/TLS/TTFB). Колбеки
// httptrace можуть приходити з різних горутин (Happy Eyeballs) — звідси mutex.
type httpTimings struct {
	mu                                     sync.Mutex
	start                                  time.Time
	dnsStart, dnsDone, connStart, connDone time.Time
	tlsStart, tlsDone, wroteReq, firstByte time.Time
	dnsAddrs                               []string
	dnsErr, connErr, tlsErr                error
	connAddr                               string
	reused                                 bool
}

func (t *httpTimings) clientTrace() *httptrace.ClientTrace {
	t.start = time.Now()
	return &httptrace.ClientTrace{
		DNSStart: func(httptrace.DNSStartInfo) { t.mu.Lock(); t.dnsStart = time.Now(); t.mu.Unlock() },
		DNSDone: func(i httptrace.DNSDoneInfo) {
			t.mu.Lock()
			t.dnsDone = time.Now()
			t.dnsErr = i.Err
			for _, a := range i.Addrs {
				t.dnsAddrs = append(t.dnsAddrs, a.IP.String())
			}
			t.mu.Unlock()
		},
		ConnectStart: func(_, addr string) {
			t.mu.Lock()
			if t.connStart.IsZero() {
				t.connStart = time.Now()
			}
			t.connAddr = addr
			t.mu.Unlock()
		},
		ConnectDone: func(_, addr string, err error) {
			t.mu.Lock()
			t.connDone = time.Now()
			t.connAddr = addr
			if err != nil {
				t.connErr = err
			}
			t.mu.Unlock()
		},
		TLSHandshakeStart: func() { t.mu.Lock(); t.tlsStart = time.Now(); t.mu.Unlock() },
		TLSHandshakeDone: func(_ tls.ConnectionState, err error) {
			t.mu.Lock()
			t.tlsDone = time.Now()
			t.tlsErr = err
			t.mu.Unlock()
		},
		GotConn:              func(i httptrace.GotConnInfo) { t.mu.Lock(); t.reused = i.Reused; t.mu.Unlock() },
		WroteRequest:         func(httptrace.WroteRequestInfo) { t.mu.Lock(); t.wroteReq = time.Now(); t.mu.Unlock() },
		GotFirstResponseByte: func() { t.mu.Lock(); t.firstByte = time.Now(); t.mu.Unlock() },
	}
}

func phase(a, b time.Time) string {
	if a.IsZero() || b.IsZero() {
		return "—"
	}
	return fmtDur(b.Sub(a))
}

func (t *httpTimings) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	var sb strings.Builder
	fmt.Fprintf(&sb, "dns=%s", phase(t.dnsStart, t.dnsDone))
	if len(t.dnsAddrs) > 0 {
		fmt.Fprintf(&sb, "[%s]", strings.Join(t.dnsAddrs, ","))
	}
	if t.dnsErr != nil {
		fmt.Fprintf(&sb, "(ПОМИЛКА: %v)", t.dnsErr)
	}
	fmt.Fprintf(&sb, " connect=%s", phase(t.connStart, t.connDone))
	if t.connAddr != "" {
		fmt.Fprintf(&sb, "(%s)", t.connAddr)
	}
	if t.connErr != nil {
		fmt.Fprintf(&sb, "(ПОМИЛКА: %v)", t.connErr)
	}
	fmt.Fprintf(&sb, " tls=%s", phase(t.tlsStart, t.tlsDone))
	if t.tlsErr != nil {
		fmt.Fprintf(&sb, "(ПОМИЛКА: %v)", t.tlsErr)
	}
	fmt.Fprintf(&sb, " відповідь=%s reused=%v", phase(t.wroteReq, t.firstByte), t.reused)
	return sb.String()
}

// ── Годинник ПК ───────────────────────────────────────────────────────────

// maxClockSkew — більший розсинхрон годинника ламає TLS і WireGuard-хендшейк
// (нода «не піднімається», хоч усе інше в порядку).
const maxClockSkew = 90 * time.Second

// checkClockSkew порівнює заголовок Date відповіді сервера з годинником ПК.
func checkClockSkew(h http.Header, rid string) {
	d := h.Get("Date")
	if d == "" {
		return
	}
	t, err := http.ParseTime(d)
	if err != nil {
		return
	}
	skew := time.Since(t)
	if skew < 0 {
		skew = -skew
	}
	if skew > maxClockSkew {
		launchlog.Warn(compLobby, "[%s] ГОДИННИК ПК розходиться із сервером на %s (сервер: %s) — TLS/WireGuard можуть не працювати; синхронізуйте час у ОС",
			rid, fmtDur(skew), d)
	}
}

// ── Середовище ────────────────────────────────────────────────────────────

var envLogOnce sync.Once

// logP2PEnvironment один раз за запуск пише «паспорт» середовища: ОС, версії,
// проксі, мережеві інтерфейси (VPN/Hyper-V/WSL часто ламають P2P), чи можна
// зайняти 127.0.0.2. Це перше, що треба дивитись у логу гравця, у якого P2P
// «не працює».
func logP2PEnvironment(controlURL, lobbyURL, playSuffix string) {
	envLogOnce.Do(func() {
		launchlog.Info(compNet, "═══ P2P середовище ═══")
		launchlog.Info(compNet, "ОС=%s/%s go=%s CPU=%d tailscale=%s", runtime.GOOS, runtime.GOARCH, runtime.Version(), runtime.NumCPU(), tailscaleModuleVersion())
		launchlog.Info(compNet, "headscale=%s lobby=%s play=*.%s MaxRooms=%d", controlURL, lobbyURL, playSuffix, MaxConcurrentRooms)
		launchlog.Info(compNet, "локальний час=%s (TZ=%s)", time.Now().Format(time.RFC3339), time.Now().Location())

		for _, k := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "all_proxy", "no_proxy"} {
			if v := os.Getenv(k); v != "" {
				launchlog.Warn(compNet, "проксі у середовищі %s=%s (може ламати з'єднання з lobby/Headscale)", k, redactSecrets(v))
			}
		}
		if os.Getenv("SHAURMA_P2P_TSNET_LOG") == "0" {
			launchlog.Info(compNet, "логи tsnet ВИМКНЕНО (SHAURMA_P2P_TSNET_LOG=0)")
		}

		if ifaces, err := net.Interfaces(); err != nil {
			launchlog.Warn(compNet, "не вдалося прочитати мережеві інтерфейси: %v", err)
		} else {
			for _, ifc := range ifaces {
				if ifc.Flags&net.FlagUp == 0 {
					continue
				}
				var addrs []string
				if as, aerr := ifc.Addrs(); aerr == nil {
					for _, a := range as {
						addrs = append(addrs, a.String())
					}
				}
				note := ""
				if looksLikeVPN(ifc.Name) {
					note = "  ⚠ схоже на VPN/віртуальний адаптер — може перехоплювати маршрути"
				}
				launchlog.Info(compNet, "інтерфейс %-18s mtu=%d flags=%s addrs=%s%s", ifc.Name, ifc.MTU, ifc.Flags, strings.Join(addrs, ","), note)
			}
		}

		// Чи можна взагалі зайняти 127.0.0.2 (macOS: потрібен alias).
		if ln, err := net.Listen("tcp", "127.0.0.2:0"); err != nil {
			launchlog.Error(compNet, "127.0.0.2 НЕДОСТУПНА для bind: %v  ⇒ %s", err, explainListenErr("127.0.0.2", err))
		} else {
			_ = ln.Close()
			launchlog.Info(compNet, "127.0.0.2 доступна для bind — ок")
		}
		launchlog.Info(compNet, "═══════════════════════")
	})
}

func looksLikeVPN(name string) bool {
	n := strings.ToLower(name)
	for _, p := range []string{"tun", "tap", "wg", "tailscale", "zt", "vpn", "ppp", "utun", "hamachi", "radmin", "wintun", "nord", "proton", "vethernet", "wsl", "docker"} {
		if strings.Contains(n, p) {
			return true
		}
	}
	return false
}

func tailscaleModuleVersion() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "?"
	}
	for _, d := range bi.Deps {
		if d.Path == "tailscale.com" {
			return d.Version
		}
	}
	return "?"
}

// ── Передпольотна перевірка Headscale ─────────────────────────────────────

// preflightControl викликається, коли tsnet-нода НЕ піднялась: окремо
// перевіряє DNS → TCP:443 → HTTPS /health контрол-сервера і пише результат
// кожного кроку. Так одразу видно, ЧИ це блокування DNS/порту/TLS, чи
// проблема глибше (auth key, ACL, node expired).
func preflightControl(parent context.Context, controlURL, tag string) {
	u, err := url.Parse(controlURL)
	if err != nil || u.Host == "" {
		launchlog.Error(compNode, "[%s] preflight: некоректний ControlURL %q: %v", tag, controlURL, err)
		return
	}
	host := u.Hostname()
	port := u.Port()
	if port == "" {
		port = "443"
	}
	ctx, cancel := context.WithTimeout(parent, 12*time.Second)
	defer cancel()

	sw := startWatch()
	addrs, derr := net.DefaultResolver.LookupIPAddr(ctx, host)
	if derr != nil {
		launchlog.Error(compNode, "[%s] preflight DNS %s: ПОМИЛКА за %s: %v", tag, host, sw, derr)
		return
	}
	var ips []string
	for _, a := range addrs {
		ips = append(ips, a.IP.String())
	}
	launchlog.Info(compNode, "[%s] preflight DNS %s → %s (%s)", tag, host, strings.Join(ips, ","), sw)

	sw = startWatch()
	d := net.Dialer{Timeout: 6 * time.Second}
	conn, cerr := d.DialContext(ctx, "tcp", net.JoinHostPort(host, port))
	if cerr != nil {
		launchlog.Error(compNode, "[%s] preflight TCP %s:%s: ПОМИЛКА за %s: %v  ⇒ порт заблоковано файрволом/провайдером/VPN", tag, host, port, sw, cerr)
		return
	}
	launchlog.Info(compNode, "[%s] preflight TCP %s:%s ok (%s), local=%s remote=%s", tag, host, port, sw, conn.LocalAddr(), conn.RemoteAddr())
	_ = conn.Close()

	sw = startWatch()
	tm := &httpTimings{}
	req, rerr := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, tm.clientTrace()), http.MethodGet, strings.TrimRight(controlURL, "/")+"/health", nil)
	if rerr != nil {
		return
	}
	req.Header.Set("User-Agent", lobbyUserAgent)
	resp, herr := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if herr != nil {
		launchlog.Error(compNode, "[%s] preflight HTTPS %s/health: ПОМИЛКА за %s: %v | %s  ⇒ TLS-перехоплення антивірусом / розсинхрон годинника / блокування SNI", tag, controlURL, sw, herr, tm)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	launchlog.Info(compNode, "[%s] preflight HTTPS /health → HTTP %d (%s) | %s | тіло=%q", tag, resp.StatusCode, sw, tm, snippet(body, 120))
	checkClockSkew(resp.Header, tag)
}
