// trace_ts.go — трасування, що потребує tsnet: прокидання логів tsnet у
// launcher.log, дамп стану ноди (чи йде трафік НАПРЯМУ, чи через DERP-релей),
// діагностика збою діалу до хоста і періодичний «пульс» ноди.
//
// Головне питання будь-якої P2P-проблеми: «нода зареєструвалась? хост видно в
// netmap? між нами direct-шлях чи релей? пінг проходить?». Усе це тут.
package p2p

import (
	"shaurma-launcher-backend/internal/crashguard"
	"context"
	"fmt"
	"net/netip"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"shaurma-launcher-backend/internal/launchlog"

	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tailcfg"
	"tailscale.com/tsnet"
)

// tsnetFullLines — скільки перших рядків tsnet-логу пишемо без фільтра (старт
// ноди — найцікавіше). Далі лише «важливі» рядки, щоб launcher.log не роздувало
// magicsock-шумом за годину гри.
const tsnetFullLines = 4000

var tsnetImportant = []string{
	"error", "fail", "timeout", "timed out", "refused", "denied", "unreachable",
	"derp", "health", "warning", "expired", "invalid", "control:", "netmap", "relay",
	"direct", "handshake", "login", "auth",
}

// tsnetLogf — Logf для tsnet.Server: усе (з редагуванням секретів) іде в
// launcher.log, компонент p2p-tsnet, рівень debug. Раніше тут стояла заглушка,
// тому при збої реєстрації ноди в лозі не було НІЧОГО від самого tsnet.
func tsnetLogf(role string) func(string, ...any) {
	if os.Getenv("SHAURMA_P2P_TSNET_LOG") == "0" {
		return func(string, ...any) {}
	}
	var n atomic.Int64
	var noticed atomic.Bool
	return func(format string, args ...any) {
		line := redactSecrets(strings.TrimRight(fmt.Sprintf(format, args...), "\n"))
		if cnt := n.Add(1); cnt > tsnetFullLines {
			low := strings.ToLower(line)
			important := false
			for _, k := range tsnetImportant {
				if strings.Contains(low, k) {
					important = true
					break
				}
			}
			if !important {
				return
			}
			if noticed.CompareAndSwap(false, true) {
				launchlog.Info(compTsnet, "[%s] ліміт %d рядків tsnet вичерпано — далі лише важливі (error/derp/health/…)", role, tsnetFullLines)
			}
		}
		launchlog.Debug(compTsnet, "[%s] %s", role, line)
	}
}

// peerPath — як саме йде трафік до піра: direct / DERP / немає шляху.
func peerPath(p *ipnstate.PeerStatus) string {
	switch {
	case p.CurAddr != "":
		return "DIRECT " + p.CurAddr
	case p.Relay != "":
		return "RELAY(DERP:" + p.Relay + ")"
	default:
		return "НЕМАЄ ШЛЯХУ"
	}
}

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return "ніколи"
	}
	return fmt.Sprintf("%s тому", fmtDur(time.Since(t)))
}

func peerIP(p *ipnstate.PeerStatus) string {
	for _, ip := range p.TailscaleIPs {
		if ip.Is4() {
			return ip.String()
		}
	}
	if len(p.TailscaleIPs) > 0 {
		return p.TailscaleIPs[0].String()
	}
	return "?"
}

// logNodeStatus пише стан ноди: BackendState, health, домашній DERP, і
// пірів (лише активні/з трафіком/з focus — гість бачить усю мережу, всіх
// не виводимо).
func logNodeStatus(role, tag string, ts *tsnet.Server, focus []string) {
	if ts == nil {
		return
	}
	lc, err := ts.LocalClient()
	if err != nil {
		launchlog.Error(compNode, "[%s/%s] статус: LocalClient: %v", role, tag, err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sw := startWatch()
	st, err := lc.Status(ctx)
	if err != nil {
		launchlog.Error(compNode, "[%s/%s] статус ноди недоступний (%s): %v", role, tag, sw, err)
		return
	}

	var ips []string
	for _, ip := range st.TailscaleIPs {
		ips = append(ips, ip.String())
	}
	home := "—"
	if st.Self != nil && st.Self.Relay != "" {
		home = st.Self.Relay
	}
	var direct, relay, none int
	for _, p := range st.Peer {
		switch {
		case p.CurAddr != "":
			direct++
		case p.Relay != "":
			relay++
		default:
			none++
		}
	}
	lvl := launchlog.Info
	if st.BackendState != "Running" || len(st.Health) > 0 {
		lvl = launchlog.Warn
	}
	lvl(compNode, "[%s/%s] стан=%s ip=%s homeDERP=%s піри=%d (direct=%d relay=%d без_шляху=%d) health=%v",
		role, tag, st.BackendState, strings.Join(ips, ","), home, len(st.Peer), direct, relay, none, st.Health)

	focusSet := make(map[string]bool, len(focus))
	for _, f := range focus {
		focusSet[f] = true
	}
	peers := make([]*ipnstate.PeerStatus, 0, len(st.Peer))
	for _, p := range st.Peer {
		if p.Active || p.RxBytes+p.TxBytes > 0 || focusSet[peerIP(p)] {
			peers = append(peers, p)
		}
	}
	sort.Slice(peers, func(i, j int) bool { return peers[i].HostName < peers[j].HostName })
	const maxShown = 20
	for i, p := range peers {
		if i == maxShown {
			launchlog.Debug(compNode, "[%s/%s]   … ще %d пірів приховано", role, tag, len(peers)-maxShown)
			break
		}
		launchlog.Debug(compNode, "[%s/%s]   пір %s %s online=%v active=%v шлях=%s rx=%s tx=%s handshake=%s lastSeen=%s",
			role, tag, p.HostName, peerIP(p), p.Online, p.Active, peerPath(p),
			fmtBytes(p.RxBytes), fmtBytes(p.TxBytes), fmtTime(p.LastHandshake), fmtTime(p.LastSeen))
	}
}

// pingPeer міряє Disco-пінг і описує ШЛЯХ (direct endpoint чи DERP-регіон).
// Повертає рядок для логу і RTT у мс (0 — не вдалось).
func pingPeer(ts *tsnet.Server, ip string, timeout time.Duration) (desc string, ms int) {
	lc, err := ts.LocalClient()
	if err != nil {
		return "LocalClient: " + err.Error(), 0
	}
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return "некоректний IP " + ip, 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	sw := startWatch()
	res, err := lc.Ping(ctx, addr, tailcfg.PingDisco)
	if err != nil {
		return fmt.Sprintf("пінг %s: ПОМИЛКА за %s: %v", ip, sw, err), 0
	}
	if res == nil {
		return fmt.Sprintf("пінг %s: порожня відповідь", ip), 0
	}
	if res.Err != "" {
		return fmt.Sprintf("пінг %s: ПОМИЛКА від ноди: %s", ip, res.Err), 0
	}
	ms = int(res.LatencySeconds*1000 + 0.5)
	if ms <= 0 {
		ms = 1
	}
	path := "?"
	switch {
	case res.Endpoint != "":
		path = "DIRECT " + res.Endpoint
	case res.DERPRegionID != 0:
		path = fmt.Sprintf("RELAY(DERP %d/%s)", res.DERPRegionID, res.DERPRegionCode)
	}
	return fmt.Sprintf("пінг %s: %dms шлях=%s", ip, ms, path), ms
}

// diagDialFailure — після невдалого діалу до хоста з'ясовує ПРИЧИНУ: чи є
// хост у netmap, онлайн він, який шлях, чи проходить Disco-пінг. Запускати
// у горутині: не затримує відповідь гравцю.
func diagDialFailure(role, tag string, ts *tsnet.Server, hostIP, hostPort string, dialErr error) {
	launchlog.Warn(compNode, "[%s/%s] ДІАГНОСТИКА збою діалу %s:%s (%v)", role, tag, hostIP, hostPort, dialErr)
	if ts == nil {
		return
	}
	lc, err := ts.LocalClient()
	if err != nil {
		launchlog.Error(compNode, "[%s/%s] діагностика: LocalClient: %v", role, tag, err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	st, err := lc.Status(ctx)
	cancel()
	if err != nil {
		launchlog.Error(compNode, "[%s/%s] діагностика: статус: %v", role, tag, err)
	} else {
		var found *ipnstate.PeerStatus
		for _, p := range st.Peer {
			for _, ip := range p.TailscaleIPs {
				if ip.String() == hostIP {
					found = p
				}
			}
		}
		switch {
		case st.BackendState != "Running":
			launchlog.Error(compNode, "[%s/%s] нода у стані %q (не Running) — вона втратила зв'язок із Headscale; health=%v", role, tag, st.BackendState, st.Health)
		case found == nil:
			launchlog.Error(compNode, "[%s/%s] хоста %s НЕМАЄ в netmap (пірів видно: %d) ⇒ або хост уже вийшов/його нода видалена (адреса в lobby застаріла), або ACL Headscale не пускає цю ноду до нього", role, tag, hostIP, len(st.Peer))
		default:
			launchlog.Warn(compNode, "[%s/%s] хост %s %s у netmap: online=%v active=%v шлях=%s handshake=%s lastSeen=%s rx=%s tx=%s",
				role, tag, found.HostName, hostIP, found.Online, found.Active, peerPath(found),
				fmtTime(found.LastHandshake), fmtTime(found.LastSeen), fmtBytes(found.RxBytes), fmtBytes(found.TxBytes))
			if !found.Online {
				launchlog.Error(compNode, "[%s/%s] Headscale вважає хоста OFFLINE ⇒ його лаунчер закрито або втрачено інтернет", role, tag)
			}
		}
	}
	desc, _ := pingPeer(ts, hostIP, 5*time.Second)
	launchlog.Warn(compNode, "[%s/%s] %s", role, tag, desc)
	if hint := explainDialErr(dialErr); hint != "" {
		launchlog.Warn(compNode, "[%s/%s] ПОЯСНЕННЯ: %s", role, tag, hint)
	}
}

// watchNode запускає «пульс» ноди: кожні interval пише стан (Info-підсумок +
// Debug-пірів). Повертає функцію зупинки (ідемпотентну).
func watchNode(role, tag string, ts *tsnet.Server, interval time.Duration, focus func() []string) (stop func()) {
	done := make(chan struct{})
	var once sync.Once
	go func() {
		defer crashguard.Recover("trace_ts.go:261")
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				var f []string
				if focus != nil {
					f = focus()
				}
				logNodeStatus(role, tag, ts, f)
			}
		}
	}()
	return func() { once.Do(func() { close(done) }) }
}
