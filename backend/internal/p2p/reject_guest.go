// reject_guest.go — сторона гостя: перетворення помилок на причини відмови і
// відповідь гравцеві (див. reject.go).
package p2p

import (
	"bufio"
	"context"
	"errors"
	"net"
	"strings"
	"time"

	"shaurma-launcher-backend/internal/launchlog"
)

// rejectErr несе причину відмови разом із помилкою, щоб вона пережила
// будь-які обгортки fmt.Errorf("…: %w").
type rejectErr struct {
	reason RejectReason
	err    error
}

func (e *rejectErr) Error() string { return e.err.Error() }
func (e *rejectErr) Unwrap() error { return e.err }

func withReason(reason RejectReason, err error) error {
	if err == nil {
		return nil
	}
	return &rejectErr{reason: reason, err: err}
}

// anonymous — гравець не ввійшов в акаунт Шаурми (JWT порожній).
func (g *GuestService) anonymous() bool {
	if g.cfg.LobbyAuth == nil {
		return true
	}
	bearer, _ := g.cfg.LobbyAuth()
	return strings.TrimSpace(bearer) == ""
}

// reasonFor — причина відмови для будь-якої помилки входу.
func (g *GuestService) reasonFor(err error) RejectReason {
	if err == nil {
		return RejNone
	}
	var re *rejectErr
	if errors.As(err, &re) {
		return re.reason
	}
	if errors.Is(err, ErrPoolExhausted) {
		return RejNoSlot
	}
	if errors.Is(err, errResolveBusy) {
		return RejRateLimited
	}
	if le, ok := AsLobbyError(err); ok {
		return g.reasonForLobby(le)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return RejLobbyUnreachable
	}
	return RejInternal
}

// reasonForLobby — причина за відповіддю VPS. Режим доступу (access) VPS віддає
// лише тоді, коли гравця не пустив САМ режим (не чорний список), тож за ним
// можна чесно сказати «лише друзі» / «лише список». Для 404 різниці між
// «офлайн», «немає такої адреси» і «закрита кімната для анонімів» VPS
// навмисно не робить (захист від перебору адрес) — текст теж загальний.
func (g *GuestService) reasonForLobby(le *LobbyError) RejectReason {
	switch {
	case errors.Is(le, ErrTunnelDisabled):
		return RejTunnelDisabled
	case errors.Is(le, ErrNotAllowed):
		switch strings.ToLower(le.Access) {
		case "friends":
			return RejOnlyFriends
		case "list":
			return RejOnlyList
		case "accounts":
			return RejOnlyAccounts
		}
		return RejNotAllowed
	case errors.Is(le, ErrAccountRequired), le.Status == 401:
		return RejAccountRequired
	case le.Status == 429:
		return RejRateLimited
	case le.Status == 404:
		if g.anonymous() {
			return RejHostOfflineAnon
		}
		return RejHostOffline
	case le.Status == 403:
		return RejNotAllowed
	}
	return RejLobbyUnreachable
}

// reasonForResolveErr — причина для помилки самого запиту до VPS: це може бути
// відповідь VPS (LobbyError) або збій мережі/DNS/TLS/таймаут.
func (g *GuestService) reasonForResolveErr(err error) RejectReason {
	if le, ok := AsLobbyError(err); ok {
		return g.reasonForLobby(le)
	}
	return RejLobbyUnreachable
}

// negTTL — на скільки запам'ятати відмову для ПІНГУ списку серверів.
// Збій самого VPS короткий: щойно він піднявся, гравець має це побачити.
func (g *GuestService) negTTL(err error) time.Duration {
	switch g.reasonFor(err) {
	case RejHostOffline, RejHostOfflineAnon, RejNotAllowed, RejOnlyFriends, RejOnlyList, RejOnlyAccounts:
		return 15 * time.Second
	case RejTunnelDisabled:
		return 30 * time.Second
	case RejRateLimited:
		return 20 * time.Second
	case RejLobbyUnreachable:
		return 8 * time.Second
	}
	return 0
}

// rejectClient відповідає клієнту (гра) причиною відмови і м'яко завершує з'єднання.
func (g *GuestService) rejectClient(conn net.Conn, hs Handshake, reason RejectReason, cid string, requestSeen bool) {
	if reason == RejNone {
		return
	}
	sent := RejectClient(conn, hs.ProtocolVersion, hs.NextState, reason, requestSeen)
	launchlog.Info(compGuest, "[%s] відмова гравцеві: %s (надіслано=%v, стан=%s)", cid, RejectKey(reason), sent, nextStateName(hs.NextState))
}

// resolveHost дістає IP/порт хоста й ЗАВЖДИ питає VPS (паралельні запити
// на ту саму адресу зливаються в один).
func (g *GuestService) resolveHost(address, tag string) (*RoomInfo, error) {
	return g.resolver.do(ticketKey(address), resolveFresh, g.negTTL, func() (*RoomInfo, error) {
		return g.resolveHostOnce(address, tag)
	})
}

// resolveHostPing — те саме для пінгу списку серверів: дозволено короткий кеш
// (див. resolve_gate.go), тож оновлення списку не б'є по VPS щоразу.
func (g *GuestService) resolveHostPing(address, tag string) (*RoomInfo, error) {
	return g.resolver.do(ticketKey(address), resolvePing, g.negTTL, func() (*RoomInfo, error) {
		return g.resolveHostOnce(address, tag)
	})
}

// statusTransit — пінг списку серверів «наскрізь» до хоста/кімнати.
//
// Послідовно (а не наосліп через pipe): пересилаємо хендшейк і Status Request,
// дивимось на ПЕРШІ байти відповіді. Маркер відмови хоста або мовчазне закриття —
// і ми самі віддаємо MOTD з причиною та відповідаємо на ping. Інакше решта
// трафіку йде звичайним pipe.
func (g *GuestService) statusTransit(conn net.Conn, hs Handshake, upstream net.Conn, address, cid string) {
	sw := startWatch()
	tuneConn(upstream)
	if _, err := upstream.Write(withPreamble(g.preambleFor(address), hs.Raw)); err != nil {
		g.rejectClient(conn, hs, RejHostNoResponse, cid, false)
		return
	}
	// Справжній пінг списку серверів шле Status Request — рівно 01 00. Передача збірок/серверів
	// (internal/share) теж відкривається зі state=1 (щоб не займати слот кімнати), але далі
	// говорить по HTTP: перший байт «G»/«P» читався як довжина фрейму > 64, з'єднання тихо
	// закривалось і одержувач вічно висів на «Підключення…». Усе, що не схоже на Status
	// Request, віддаємо звичайним тунелем.
	if bc, ok := conn.(*bufConn); ok {
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		head, perr := bc.r.Peek(2)
		_ = conn.SetReadDeadline(time.Time{})
		if perr != nil {
			return // клієнт пішов, нічого не надіславши
		}
		if !(head[0] == 0x01 && head[1] == 0x00) {
			sniff := newRejectSniffConn(upstream, func(r RejectReason) {
				sent := writeReject(conn, hs.ProtocolVersion, hs.NextState, r, true)
				launchlog.Info(compGuest, "[%s] %s: хост не пустив: %s (надіслано=%v)", cid, address, RejectKey(r), sent)
			})
			stats := pipeBothStats(conn, sniff)
			launchlog.Debug(compGuest, "[%s] тунель (не пінг) %s за %s: %s", cid, address, sw, stats.describe("клієнт", "хост"))
			return
		}
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	raw, _, err := readFrame(conn, 64)
	_ = conn.SetReadDeadline(time.Time{})
	if err != nil {
		return // клієнт пішов, не попросивши статус
	}
	if _, err := upstream.Write(raw); err != nil {
		g.rejectClient(conn, hs, RejHostNoResponse, cid, true)
		return
	}
	br := bufio.NewReader(upstream)
	_ = upstream.SetReadDeadline(time.Now().Add(10 * time.Second))
	reason, marker, perr := sniffHostReject(br)
	_ = upstream.SetReadDeadline(time.Time{})
	switch {
	case marker:
		g.rejectClient(conn, hs, reason, cid, true)
		return
	case perr != nil:
		g.rejectClient(conn, hs, RejHostNoResponse, cid, true)
		return
	}
	stats := pipeBothStats(conn, &bufConn{Conn: upstream, r: br})
	launchlog.Debug(compGuest, "[%s] статус %s за %s: %s", cid, address, sw, stats.describe("клієнт", "хост"))
}
