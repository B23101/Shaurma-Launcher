// health.go — ЖИВА діагностика хостингу для UI: не «тунель піднято», а «гість реально зможе зайти».
//
// Раніше статус «онлайн» означав лише, що tsnet-нода піднялась. Гості при цьому могли не заходити
// з десятка причин (сервер не слухає порт, lobby віддає старий IP, кімнату вимкнено, голосовий
// мод мовчить), і лаунчер не казав жодної. Тут збираємо ці сигнали в одну структуру.
package p2p

import (
	"context"
	"errors"
	"net"
	"strings"
	"time"
)

// selfCheckTTL — як довго тримаємо результат самоперевірки (це мережевий запит до lobby).
const selfCheckTTL = 20 * time.Second

// SelfCheckResult — чи резолвиться НАША адреса в lobby так, як бачить її гість.
type SelfCheckResult struct {
	// Checked — перевірку взагалі виконано (false = ще не було / lobby недоступний для перевірки).
	Checked bool `json:"checked"`
	// OK — lobby віддає гостям саме наші поточні IP/порти.
	OK bool `json:"ok"`
	// At — unix мс перевірки.
	At int64 `json:"at"`
	// Problem — що саме не так (людською мовою); порожньо, якщо OK.
	Problem string `json:"problem,omitempty"`
}

// SelfCheck резолвить власну адресу так, як це робить гість, і порівнює з тим, що ми слухаємо.
// Результат кешується на selfCheckTTL; force обходить кеш.
func (s *HostSession) SelfCheck(force bool) SelfCheckResult {
	s.selfMu.Lock()
	if !force && !s.selfAt.IsZero() && time.Since(s.selfAt) < selfCheckTTL {
		r := s.selfRes
		s.selfMu.Unlock()
		return r
	}
	s.selfMu.Unlock()

	res := SelfCheckResult{At: time.Now().UnixMilli()}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	room, err := s.lobby.JoinAddress(ctx, s.Subdomain)
	switch {
	case err != nil:
		res.Problem = withHint(err, firstNonEmpty(explainLobbyErr(err), "не вдалося опитати lobby"))
		// Мережевий збій самої перевірки не доводить, що гості не зайдуть, — не вважаємо це вироком.
		res.Checked = errorsIsNotFound(err)
	default:
		res.Checked = true
		want, wantUDP := s.hostIP, s.UDPPort()
		switch {
		case room.HostTailscaleIP != want:
			res.Problem = "lobby віддає гостям IP «" + room.HostTailscaleIP + "», а тунель слухає на " + want + " — гості підключаються в порожнечу (перезапусти P2P)"
		case room.HostPort != s.hostPort:
			res.Problem = "lobby віддає гостям порт " + room.HostPort + ", а тунель слухає на " + s.hostPort
		case room.UDPPort != wantUDP:
			res.Problem = "голосовий UDP у lobby (" + orDash(room.UDPPort) + ") не збігається з тунелем (" + orDash(wantUDP) + ")"
		default:
			res.OK = true
		}
	}
	s.selfMu.Lock()
	s.selfAt, s.selfRes = time.Now(), res
	s.selfMu.Unlock()
	return res
}

func errorsIsNotFound(err error) bool { return errors.Is(err, ErrAddressNotFound) }

func orDash(v string) string {
	if v == "" {
		return "вимкнено"
	}
	return v
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// HostLive — повна жива картина одного хоста для вкладки «Мережа».
type HostLive struct {
	Active    bool   `json:"active"`
	Subdomain string `json:"subdomain"`
	Address   string `json:"address"`
	// Alive — tsnet-нода й listener живі (false = сесія вмирає/мертва).
	Alive     bool   `json:"alive"`
	TunnelIP  string `json:"tunnelIp,omitempty"`
	LocalPort string `json:"localPort,omitempty"`
	UptimeSec int64  `json:"uptimeSec"`
	Guests    int    `json:"guests"`
	Access    string `json:"access,omitempty"`
	// Disabled — кімнату вимкнено адміном (гості отримують 403).
	Disabled bool `json:"disabled,omitempty"`
	// PublishedAt/PublishError — остання публікація адреси в lobby.
	PublishedAt  int64  `json:"publishedAt,omitempty"`
	PublishError string `json:"publishError,omitempty"`
	// LocalGameOK/LocalGameErr — чи відповідає Minecraft на 127.0.0.1:<порт> прямо зараз.
	LocalGameOK  bool   `json:"localGameOk"`
	LocalGameErr string `json:"localGameErr,omitempty"`
	// UDP.
	UDPPort  string   `json:"udpPort,omitempty"`
	UDPError string   `json:"udpError,omitempty"`
	UDP      UDPStats `json:"udp"`
	// Self — самоперевірка резолву адреси в lobby.
	Self SelfCheckResult `json:"self"`
}

// Live збирає знімок. withSelfCheck=false не ходить у мережу (дешеве опитування раз на кілька секунд).
func (s *HostSession) Live(withSelfCheck, forceSelf bool) HostLive {
	alive := true
	select {
	case <-s.closed:
		alive = false
	default:
	}
	l := HostLive{
		Active:       true,
		Subdomain:    s.Subdomain,
		Address:      s.Address,
		Alive:        alive,
		TunnelIP:     s.hostIP,
		LocalPort:    s.LocalPort,
		UptimeSec:    int64(time.Since(s.started).Seconds()),
		Guests:       len(s.Connections()),
		Access:       s.Access(),
		Disabled:     s.Disabled,
		PublishedAt:  s.pubAt.Load(),
		UDPPort:      s.UDPPort(),
		UDP:          s.UDPStatsSnapshot(),
		LocalGameErr: "",
	}
	if e := s.UDPError(); e != nil {
		l.UDPError = e.Error()
	}
	if v, _ := s.pubErr.Load().(string); v != "" {
		l.PublishError = v
	}
	if c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", s.LocalPort), 800*time.Millisecond); err != nil {
		l.LocalGameErr = explainLocalDial(err)
	} else {
		_ = c.Close()
		l.LocalGameOK = true
	}
	if withSelfCheck {
		l.Self = s.SelfCheck(forceSelf)
	} else {
		s.selfMu.Lock()
		l.Self = s.selfRes
		s.selfMu.Unlock()
	}
	return l
}

func explainLocalDial(err error) string {
	m := strings.ToLower(err.Error())
	if strings.Contains(m, "refused") || strings.Contains(m, "actively") {
		return "Minecraft-сервер не слухає цей порт (не запущений, ще вантажиться або працює на іншому порту)"
	}
	if strings.Contains(m, "timeout") || strings.Contains(m, "deadline") {
		return "Minecraft-сервер не відповів вчасно (завис або перевантажений)"
	}
	return err.Error()
}
