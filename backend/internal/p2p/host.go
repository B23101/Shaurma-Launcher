// host.go — сторона Host: піднімає ephemeral-ноду в нашій Headscale-мережі,
// слухає на її Tailscale IP, проксіює трафік у локальний Minecraft-сервер
// (127.0.0.1:<port>), публікує свіжі IP/port під СТАЛОЮ адресою власника і
// показує гравцю, ХТО зайшов і з яким пінгом (див. tracker.go).
//
// TCP-порт = порт сервера (MinecraftPort). UDP-порт (UDPPort) за замовчуванням
// теж порт сервера, але користувач може задати будь-який. Ніде не підміняються.
package p2p

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"shaurma-launcher-backend/internal/crashguard"
	"sync"
	"sync/atomic"
	"time"

	"shaurma-launcher-backend/internal/launchlog"

	"tailscale.com/tsnet"
)

// pingInterval — як часто міряємо мережевий RTT до кожного під'єднаного
// гостя. 5 с — таблиця лишається «живою», а регулярний disco-трафік
// утримує NAT-мапінг і пряму лінію (без падінь у DERP і стрибків пінгу).
const pingInterval = 5 * time.Second

// nodePulseInterval — як часто пишемо стан tsnet-ноди (direct/relay, health,
// пірі) у launcher.log.
const nodePulseInterval = 30 * time.Second

// HostSession — активна хост-сесія. Живе, доки викликач не покличе
// Close() (закриття вкладки/вимкнення лаунчера/кнопка "Стоп"). Адреса НЕ
// звільняється при Close() — лишається застовбленою за власником.
type HostSession struct {
	Subdomain string
	Address   string // "<subdomain>.play.shaurma-launcher.org"

	// LocalPort — порт гри на ЦЬОМУ ПК (127.0.0.1:<LocalPort>), куди
	// сесія проксіює трафік. Потрібен гостю, щоб побачити, що адресу
	// хостить САМЕ цей лаунчер, і піти на локальну гру напряму, а не
	// крутити пакети через tsnet до самого себе (див. GuestService).
	LocalPort string

	ts       *tsnet.Server
	listener net.Listener
	stateDir string // тека стану ЦІЄЇ сесії — видаляється при Close()

	// tracker — хто зараз під'єднаний і який у нього пінг (для UI гравця).
	tracker *connectionTracker

	// gate — тимчасове запрошення: пускати лише з квитком від VPS (tinvite_gate.go).
	gate atomic.Pointer[HostGate]

	// p2pGate — допуск за квитками вузла, коли режим доступу кімнати != all (p2p_gate.go; nil = відкрито).
	p2pGate atomic.Pointer[P2PGate]
	// lobby/ownerSecret/sessionToken — чим хост доводить себе вузлу в admit: власник — ownerSecret,
	// співхост — sessionToken оренди.
	lobby        *LobbyClient
	ownerSecret  string
	authMu       sync.Mutex
	sessionToken string

	// Параметри для heartbeat оренди (host-update кожні leaseBeat) і колбек «хост-роль втрачено».
	access                      atomic.Value // string: чинний режим доступу (для UI)
	hostIP, hostPort, ownerName string
	onRevoked                   func(*HostSession, error)

	// Disabled — тунель вимкнено адміном (lobby повернув disabled:true).
	// Хостинг живе, але гості за цією адресою отримають 403, тож UI
	// має показати власнику, що кімнату вимкнено.
	Disabled bool

	// UDP-тунель для голосових чатів (nil, якщо галочка вимкнена або UDP
	// не вдалося підняти — TCP-тунель від цього не залежить).
	// udpMu захищає ВСІ udp*-поля нижче: UDP можна вмикати/вимикати на живій сесії (SetUDP),
	// а статус читається з UI паралельно.
	udpMu     sync.Mutex
	udpPC     net.PacketConn
	udpRelay  *udpRelay
	udpPort   string // порт UDP-тунелю, опублікований у лобі ('' = UDP off)
	voicePort string // локальний порт голосового моду, куди форвардимо
	udpErr    error  // чому UDP-тунель не піднявся (nil = ок або вимкнено)
	// lastUDPStats — підсумок попереднього релея (щоб лічильники не обнулялись при перемиканні порту).
	lastUDPStats UDPStats

	// Стан останньої публікації в lobby (host-update): для чесного статусу в UI.
	pubAt  atomic.Int64 // unix мс останнього УСПІШНОГО host-update
	pubErr atomic.Value // string: помилка останньої спроби ('' = ок)
	// selfMu/selfAt/selfRes — кеш самоперевірки «адреса резолвиться в мене» (SelfCheck).
	selfMu  sync.Mutex
	selfAt  time.Time
	selfRes SelfCheckResult

	// id — наскрізний ID сесії для логів ("h3"); started — коли піднято;
	// stopWatch — зупинка періодичного «пульсу» ноди.
	id        string
	started   time.Time
	stopWatch func()

	closeOnce sync.Once
	closed    chan struct{}
}

// StartHost піднімає tsnet-ноду, слухає на її Tailscale-адресі, проксіює
// все на 127.0.0.1:minecraftPort і публікує щойно отриману IP під
// cfg.Subdomain через /p2p/host-update. Адреса має бути ВЖЕ застовблена
// раніше (через API.ClaimP2PAddress) — StartHost сам адресу не створює.
func StartHost(ctx context.Context, cfg HostConfig) (*HostSession, error) {
	hid := newTraceID("h")
	total := startWatch()
	launchlog.Info(compHost, "[%s] ▶ СТАРТ хостингу: адреса=%s tcp=%s udp=%q власник=%q control=%s lobby=%s authKey=%s ownerSecret=%s токен=%s",
		hid, cfg.Subdomain, cfg.MinecraftPort, cfg.UDPPort, cfg.OwnerName, cfg.ControlURL, cfg.LobbyBaseURL,
		maskSecret(cfg.AuthKey), maskSecret(cfg.OwnerSecret), maskSecret(cfg.LobbyToken))

	if cfg.MinecraftPort == "" {
		launchlog.Error(compHost, "[%s] відмова: не вказано порт Minecraft-сервера", hid)
		return nil, fmt.Errorf("p2p: не вказано порт Minecraft-сервера")
	}
	if cfg.Subdomain == "" {
		launchlog.Error(compHost, "[%s] відмова: не вказано адресу", hid)
		return nil, fmt.Errorf("p2p: не вказано адресу (спочатку застовбіть її через ClaimP2PAddress)")
	}
	if cfg.AuthKey == "" {
		launchlog.Error(compHost, "[%s] відмова: порожній auth key", hid)
		return nil, fmt.Errorf("p2p: відсутній auth key (запросіть його через LobbyClient.RequestAuthKey)")
	}

	// Чи слухає вже хтось локально на порту гри: якщо ні — гості підключаться
	// до тунелю, а доходити їм буде нікуди («Connection refused» у гостя).
	if c, derr := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", cfg.MinecraftPort), 1500*time.Millisecond); derr != nil {
		launchlog.Warn(compHost, "[%s] УВАГА: локальна гра на 127.0.0.1:%s зараз НЕ відповідає (%v) — гості не зайдуть, доки мир/сервер не відкрито (Open to LAN / запуск сервера)", hid, cfg.MinecraftPort, derr)
	} else {
		_ = c.Close()
		launchlog.Info(compHost, "[%s] локальна гра на 127.0.0.1:%s відповідає — ок", hid, cfg.MinecraftPort)
	}

	// Нова унікальна тека стану на КОЖНУ сесію — інакше пред'являвся б
	// node key давно видаленої ephemeral-ноди, і Headscale відповідав
	// «node not found» (див. коментар у state.go).
	stateDir, err := freshNodeStateDir(cfg.StateDir, "host")
	if err != nil {
		launchlog.Error(compHost, "[%s] не вдалося створити теку стану: %v", hid, err)
		return nil, err
	}
	launchlog.Debug(compHost, "[%s] тека стану ноди: %s", hid, stateDir)

	ts, err := upFreshNode(ctx, nodeUpConfig{
		Role:        "host",
		Tag:         hid,
		Prefix:      "shaurma-host-",
		StateDir:    stateDir,
		AuthKey:     cfg.AuthKey,
		AuthKeyFunc: cfg.AuthKeyFunc,
		ControlURL:  cfg.ControlURL,
		OnFreshState: func() error {
			dropNodeStateDir(stateDir)
			d, derr := freshNodeStateDir(cfg.StateDir, "host")
			if derr != nil {
				return derr
			}
			stateDir = d
			return nil
		},
	})
	if err != nil {
		dropNodeStateDir(stateDir)
		launchlog.Error(compHost, "[%s] ✗ СТАРТ провалено на підйомі ноди (разом %s): %v", hid, total, err)
		return nil, err
	}

	ip4, err := firstIPv4(ts)
	if err != nil {
		ts.Close()
		launchlog.Error(compHost, "[%s] ✗ нода без IPv4: %v", hid, err)
		return nil, err
	}
	launchlog.Info(compHost, "[%s] tailnet-адреса хоста: %s", hid, ip4)

	lsw := startWatch()
	ln, err := ts.Listen("tcp", net.JoinHostPort(ip4, cfg.MinecraftPort))
	if err != nil {
		ts.Close()
		launchlog.Error(compHost, "[%s] ✗ ts.Listen tcp %s:%s: %v", hid, ip4, cfg.MinecraftPort, err)
		return nil, fmt.Errorf("p2p: не вдалося слухати на %s:%s: %w", ip4, cfg.MinecraftPort, err)
	}
	launchlog.Info(compHost, "[%s] TCP слухає в tailnet на %s:%s (%s) → форвард на 127.0.0.1:%s", hid, ip4, cfg.MinecraftPort, lsw, cfg.MinecraftPort)

	// ── UDP-тунель для голосових чатів (необов'язковий) ────────────────
	// Галочка в лаунчері → сюди приходить порт голосового моду (UI сам
	// вирішує, який саме — дефолт там дорівнює порту сервера, бо це
	// найпоширеніший режим SVC (voicechat-server.properties: port=-1)).
	// Тунель слухає на порту ГРИ (однаковість портів), а локально
	// форвардить рівно туди, куди сказали в UI, без прихованих підмін.
	// Не піднявся — не валимо хостинг гри: голос просто не тунелюється,
	// і це буде видно в статусі.
	var udpPC net.PacketConn
	var udpRelay *udpRelay
	var udpListenErr error
	udpPort := ""
	if cfg.UDPPort != "" {
		// UDP-порт задає користувач (дефолт у UI = порт сервера) і НІДЕ
		// не підміняється: слухаємо на ньому в tailnet і форвардимо на
		// ТОЙ САМИЙ порт локально (127.0.0.1:<UDPPort>).
		udpPC, err = ts.ListenPacket("udp", net.JoinHostPort(ip4, cfg.UDPPort))
		if err != nil {
			launchlog.Error(compHost, "[%s] UDP %s:%s недоступний (голос буде без тунелю): %v", hid, ip4, cfg.UDPPort, err)
			udpPC = nil
			udpListenErr = fmt.Errorf("UDP %s:%s: %w", ip4, cfg.UDPPort, err)
		} else {
			udpPort = cfg.UDPPort
			udpRelay = newUDPRelay().withLabel("host " + hid)
			launchlog.Info(compHost, "[%s] UDP: тунель піднято на %s:%s, форвард у локальний мод на 127.0.0.1:%s", hid, ip4, cfg.UDPPort, cfg.UDPPort)
		}
	} else {
		launchlog.Debug(compHost, "[%s] UDP: вимкнено (порт не задано) — тунель не піднімаю", hid)
	}

	s := &HostSession{
		Subdomain: cfg.Subdomain,
		LocalPort: cfg.MinecraftPort,
		ts:        ts,
		listener:  ln,
		stateDir:  stateDir,
		tracker:   newConnectionTracker(),
		udpPC:     udpPC,
		udpRelay:  udpRelay,
		udpPort:   udpPort,
		voicePort: cfg.UDPPort,
		udpErr:    udpListenErr,
		closed:    make(chan struct{}),
		id:        hid,
		started:   time.Now(),
	}

	client := NewLobbyClient(cfg.LobbyBaseURL, cfg.LobbyToken).WithAuth(cfg.LobbyAuth)
	s.lobby, s.ownerSecret, s.sessionToken = client, cfg.OwnerSecret, cfg.SessionToken
	s.hostIP, s.hostPort, s.ownerName, s.onRevoked = ip4, cfg.MinecraftPort, cfg.OwnerName, cfg.OnRevoked
	hsw := startWatch()
	update, err := client.HostUpdate(ctx, HostUpdateRequest{
		Subdomain:    cfg.Subdomain,
		OwnerSecret:  cfg.OwnerSecret,
		OwnerName:    cfg.OwnerName,
		HostIP:       ip4,
		HostPort:     cfg.MinecraftPort,
		UDPPort:      udpPort,
		RoomCode:     randomSuffix(), // діагностичний код, гість його не бачить і не вводить
		SessionToken: cfg.SessionToken,
	})
	if err != nil {
		ln.Close()
		if udpPC != nil {
			_ = udpPC.Close()
		}
		ts.Close()
		dropNodeStateDir(stateDir)
		launchlog.Error(compHost, "[%s] ✗ СТАРТ провалено на публікації адреси в lobby (%s, разом %s): %v  ⇒ %s",
			hid, hsw, total, err, explainLobbyErr(err))
		return nil, fmt.Errorf("p2p: не вдалося опублікувати адресу: %w", err)
	}
	s.Address = update.Address
	s.pubAt.Store(time.Now().UnixMilli())
	s.Disabled = update.Disabled
	s.SetAccess(update.Access) // до acceptLoop: жоден гість не пройде повз гейт
	launchlog.Info(compHost, "[%s] адреса опублікована в lobby за %s: %s → %s:%s udp=%q disabled=%v",
		hid, hsw, update.Address, ip4, cfg.MinecraftPort, udpPort, update.Disabled)
	if update.Disabled {
		launchlog.Warn(compHost, "[%s] УВАГА: тунель ВИМКНЕНО адміном — гості отримуватимуть 403", hid)
	}

	{
		_f, _a0 := s.acceptLoop, cfg.MinecraftPort
		go func() {
			defer crashguard.Recover("s.acceptLoop")
			_f(_a0)
		}()
	}
	if udpPC != nil {
		{
			_f, _a0, _a1, _a2 := s.udpLoop, udpPC, udpRelay, cfg.UDPPort
			go func() {
				defer crashguard.Recover("s.udpLoop")
				_f(_a0, _a1, _a2)
			}()
		}
	}
	{
		_f := s.pingLoop
		go func() {
			defer crashguard.Recover("s.pingLoop")
			_f()
		}()
	}
	if cfg.SessionToken != "" {
		go func() {
			defer crashguard.Recover("s.leaseLoop")
			s.leaseLoop()
		}()
	}
	{
		_f := s.gateLoop
		go func() {
			defer crashguard.Recover("s.gateLoop")
			_f()
		}()
	}
	s.stopWatch = watchNode("host", hid, ts, nodePulseInterval, nil)
	launchlog.Info(compHost, "[%s] ✔ ХОСТИНГ ПРАЦЮЄ (старт зайняв %s): %s", hid, total, update.Address)
	return s, nil
}

// SetAccess застосовує режим доступу кімнати (з відповіді host-update): all або порожній — гейт вимкнено,
// friends/accounts/list — гості мусять пред'явити квиток вузла (преамбула SHP2P1). Режими між собою гейт
// не міняє: рішення щоразу приймає вузол у admit.
func (s *HostSession) SetAccess(access string) {
	s.access.Store(access)
	if access == "" || access == "all" {
		s.p2pGate.Store(nil)
		return
	}
	if s.p2pGate.Load() == nil {
		pg := NewP2PGate(s.admit)
		pg.Mode = s.Access // від режиму залежить текст відмови гостю (reject.go)
		s.p2pGate.CompareAndSwap(nil, pg)
	}
}

// leaseBeat — період heartbeat оренди (TTL оренди на вузлі 45 с).
const leaseBeat = 15 * time.Second

// Access — чинний режим доступу кімнати (порожній = all).
func (s *HostSession) Access() string {
	v, _ := s.access.Load().(string)
	return v
}

// SessionToken — поточний токен оренди (” = кімната без співхостів).
func (s *HostSession) SessionToken() string {
	s.authMu.Lock()
	defer s.authMu.Unlock()
	return s.sessionToken
}

// leaseLoop продовжує оренду спільної кімнати: host-update зі sessionToken кожні leaseBeat.
//   - p2p_lease_required → оренда зникла на вузлі: беремо нову (host-start) і повторюємо;
//   - p2p_lease_lost / p2p_host_revoked / p2p_owner_only → хост-роль забрано, зупиняємо тунель;
//   - мережеві збої ігноруємо (блимання не має виганяти гравців; оренда має запас 45 с).
func (s *HostSession) leaseLoop() {
	ticker := time.NewTicker(leaseBeat)
	defer ticker.Stop()
	for {
		select {
		case <-s.closed:
			return
		case <-ticker.C:
		}
		err := s.beatLease()
		if errors.Is(err, ErrLeaseRequired) {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			var st HostStartResponse
			st, err = s.lobby.HostStart(ctx, s.Subdomain, s.ownerSecret)
			cancel()
			if err == nil && st.SessionToken != "" {
				s.SetSessionToken(st.SessionToken)
				err = s.beatLease()
			}
		}
		if errors.Is(err, ErrLeaseLost) || errors.Is(err, ErrHostRevoked) || errors.Is(err, ErrOwnerOnly) || errors.Is(err, ErrRoomBusy) {
			launchlog.Warn(compHost, "[%s] хост-роль втрачено: %v", s.id, err)
			if s.onRevoked != nil {
				s.onRevoked(s, err)
			}
			return
		}
		if err != nil {
			launchlog.Debug(compHost, "[%s] heartbeat оренди: %v", s.id, err)
		}
	}
}

func (s *HostSession) beatLease() error {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	resp, err := s.lobby.HostUpdate(ctx, HostUpdateRequest{
		Subdomain:    s.Subdomain,
		OwnerSecret:  s.ownerSecret,
		OwnerName:    s.ownerName,
		HostIP:       s.hostIP,
		HostPort:     s.hostPort,
		UDPPort:      s.UDPPort(),
		SessionToken: s.SessionToken(),
	})
	if err != nil {
		s.pubErr.Store(err.Error())
		return err
	}
	s.pubAt.Store(time.Now().UnixMilli())
	s.pubErr.Store("")
	if resp.Access != "" {
		s.SetAccess(resp.Access) // власник міг змінити режим доступу
	}
	return nil
}

// SetSessionToken оновлює токен оренди, яким співхост доводить себе в admit.
func (s *HostSession) SetSessionToken(token string) {
	s.authMu.Lock()
	s.sessionToken = token
	s.authMu.Unlock()
}

// admit питає вузол про квиток гостя від імені цього хоста.
func (s *HostSession) admit(ticket string) (AdmitResult, error) {
	s.authMu.Lock()
	token := s.sessionToken
	s.authMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	return s.lobby.AdmitTicket(ctx, s.Address, ticket, s.ownerSecret, token)
}

// gateLoop раз на gateRecheckEvery перевіряє вже підключених гостей (розірвана дружба/чорний список
// діють без перепідключення, поки чинний їхній квиток).
func (s *HostSession) gateLoop() {
	ticker := time.NewTicker(gateRecheckEvery)
	defer ticker.Stop()
	for {
		select {
		case <-s.closed:
			return
		case <-ticker.C:
			if g := s.p2pGate.Load(); g != nil {
				g.Recheck()
			}
		}
	}
}

// Connections повертає список гостей, під'єднаних до цієї сесії (для UI).
func (s *HostSession) Connections() []HostConnection { return s.tracker.guests() }

// udpLoop читає датаграми з tailnet-адреси ноди (порт UDP-тунелю) і віддає їх у
// ЛОКАЛЬНИЙ голосовий мод того самого гравця. Кожне джерело (гість)
// отримує власну сесію — UDP тут двобічний.
//
// pc/relay передаються аргументами (а не читаються з s), бо UDP можна перемкнути на живій
// сесії: старий цикл закінчується, коли SetUDP закриває його pc.
func (s *HostSession) udpLoop(pc net.PacketConn, relay *udpRelay, voicePort string) {
	buf := make([]byte, udpDatagramSize)
	for {
		n, src, err := pc.ReadFrom(buf)
		if err != nil {
			select {
			case <-s.closed:
				return
			default:
			}
			s.udpMu.Lock()
			stale := s.udpPC != pc // нас замінили/вимкнули навмисно
			s.udpMu.Unlock()
			if !stale {
				launchlog.Error(compHost, "[%s] UDP читання з tailnet: %v", s.id, err)
			}
			return
		}
		payload := append([]byte(nil), buf[:n]...)
		// Форвардимо ТІЛЬКИ на локальну гру (127.0.0.1) порту моду.
		// Зовнішні адреси тут неможливі — цей код не вміє ходити в інтернет.
		relay.forward(pc, src, payload, func() (net.Conn, error) {
			return net.DialTimeout("udp", net.JoinHostPort("127.0.0.1", voicePort), 5*time.Second)
		})
	}
}

// UDPPort — порт, на якому ЗАРАЗ піднято UDP-тунель (” = вимкнено або не вдалося).
func (s *HostSession) UDPPort() string {
	s.udpMu.Lock()
	defer s.udpMu.Unlock()
	return s.udpPort
}

// UDPError — чому UDP-тунель не піднявся (nil = ок або вимкнено).
func (s *HostSession) UDPError() error {
	s.udpMu.Lock()
	defer s.udpMu.Unlock()
	return s.udpErr
}

// UDPStatsSnapshot — сумарні лічильники голосу за життя сесії.
func (s *HostSession) UDPStatsSnapshot() UDPStats {
	s.udpMu.Lock()
	defer s.udpMu.Unlock()
	st := s.udpRelay.stats()
	st.Sessions += s.lastUDPStats.Sessions
	st.PktsOut += s.lastUDPStats.PktsOut
	st.PktsIn += s.lastUDPStats.PktsIn
	return st
}

// SetUDP вмикає/вимикає/перемикає UDP-тунель голосу НА ЖИВІЙ сесії — без перезапуску
// tsnet-ноди (раніше потрібно було зупиняти й знову запускати тунель). port == ""
// вимикає. Новий порт одразу публікується в lobby; якщо публікація не вдалась —
// змінений стан відкочується, щоб UI не бачив «UDP працює», коли гості про нього не знають.
func (s *HostSession) SetUDP(port string) error {
	select {
	case <-s.closed:
		return fmt.Errorf("p2p: сесія вже зупинена")
	default:
	}
	s.udpMu.Lock()
	if port == s.udpPort && (port != "" || s.udpErr == nil) && (port == "" || s.udpPC != nil) {
		s.udpMu.Unlock()
		return nil // нічого не міняється
	}
	prevPC, prevRelay, prevPort, prevVoice, prevErr, prevStats := s.udpPC, s.udpRelay, s.udpPort, s.voicePort, s.udpErr, s.lastUDPStats

	var newPC net.PacketConn
	var newRelay *udpRelay
	if port != "" {
		// Старий сокет мусимо закрити ДО нового ListenPacket (порт може збігатись), але
		// лише якщо порти однакові; інакше тримаємо старий, доки новий не підніметься.
		if port == prevPort && prevPC != nil {
			_ = prevPC.Close()
			prevPC = nil
		}
		pc, err := s.ts.ListenPacket("udp", net.JoinHostPort(s.hostIP, port))
		if err != nil {
			s.udpErr = fmt.Errorf("UDP %s:%s: %w", s.hostIP, port, err)
			s.udpMu.Unlock()
			launchlog.Error(compHost, "[%s] UDP %s:%s недоступний: %v", s.id, s.hostIP, port, err)
			return s.udpErr
		}
		newPC, newRelay = pc, newUDPRelay().withLabel("host "+s.id)
	}
	s.udpPC, s.udpRelay, s.udpPort, s.voicePort, s.udpErr = newPC, newRelay, port, port, nil
	if prevRelay != nil {
		st := prevRelay.stats()
		s.lastUDPStats.Sessions += st.Sessions
		s.lastUDPStats.PktsOut += st.PktsOut
		s.lastUDPStats.PktsIn += st.PktsIn
	}
	s.udpMu.Unlock()

	if newPC != nil {
		go func(pc net.PacketConn, rl *udpRelay, vp string) {
			defer crashguard.Recover("s.udpLoop")
			s.udpLoop(pc, rl, vp)
		}(newPC, newRelay, port)
	}
	// Публікуємо ПІСЛЯ старту слухача: гість, що побачив порт, не має влучити в порожнечу.
	if err := s.beatLease(); err != nil {
		launchlog.Error(compHost, "[%s] UDP %q: не вдалося опублікувати в lobby: %v — відкочую", s.id, port, err)
		s.udpMu.Lock()
		if newPC != nil {
			_ = newPC.Close()
		}
		if newRelay != nil {
			newRelay.Close()
		}
		s.udpPC, s.udpRelay, s.udpPort, s.voicePort, s.udpErr, s.lastUDPStats = nil, nil, "", "", prevErr, prevStats
		s.udpMu.Unlock()
		// Старий тунель (якщо був і ми його не закрили) лишається працювати: повертаємо його.
		if prevPC != nil && prevPort != "" {
			s.udpMu.Lock()
			s.udpPC, s.udpRelay, s.udpPort, s.voicePort = prevPC, prevRelay, prevPort, prevVoice
			s.udpMu.Unlock()
		} else if prevRelay != nil {
			prevRelay.Close()
		}
		return fmt.Errorf("p2p: не вдалося опублікувати UDP-порт у lobby: %w", err)
	}
	// Публікація вдалась — старий тунель (інший порт) можна закривати.
	if prevPC != nil {
		_ = prevPC.Close()
	}
	if prevRelay != nil {
		prevRelay.Close()
	}
	launchlog.Info(compHost, "[%s] UDP перемкнено на ходу: %q → %q (без перезапуску тунелю)", s.id, prevPort, port)
	return nil
}

func (s *HostSession) acceptLoop(minecraftPort string) {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.closed:
				return // штатне закриття
			default:
				launchlog.Error(compHost, "[%s] accept у tailnet: %v (listener більше не приймає гостей!)", s.id, err)
				return
			}
		}
		{
			_f, _a0, _a1 := s.proxyToLocalGame, conn, minecraftPort
			go func() {
				defer crashguard.Recover("s.proxyToLocalGame")
				_f(_a0, _a1)
			}()
		}
	}
}

func (s *HostSession) proxyToLocalGame(guest net.Conn, minecraftPort string) {
	defer guest.Close()

	if g := s.gate.Load(); g != nil {
		ticket, r, ok := readPreamble(guest, g.InviteID)
		if !ok {
			launchlog.Warn(compHost, "[%s] з'єднання з %s без дійсного квитка тимчасового запрошення — відхилено", s.id, addrString(guest.RemoteAddr()))
			return
		}
		if !g.allow(ticket) {
			launchlog.Warn(compHost, "[%s] квиток тимчасового запрошення від %s не прийнято — відхилено", s.id, addrString(guest.RemoteAddr()))
			SendHostReject(guest, RejTicketRejected) // преамбула коректна → гість зрозуміє причину
			return
		}
		guest = &bufConn{Conn: guest, r: r}
	} else if pg := s.p2pGate.Load(); pg != nil {
		gc, release, ok, deny := pg.AcceptReason(guest)
		if !ok {
			launchlog.Warn(compHost, "[%s] з'єднання з %s без дійсного квитка кімнати — відхилено (причина гостю: %q)", s.id, addrString(guest.RemoteAddr()), RejectKey(deny))
			SendHostReject(guest, deny)
			return
		}
		defer release()
		guest = gc
	}

	// Реєструємо гостя ДО проксіювання, щоб у UI він з'явився одразу.
	guestIP := hostOfIP(guest.RemoteAddr())
	id := s.tracker.add(addrString(guest.RemoteAddr()), guestIP)
	defer s.tracker.remove(id)
	cid := s.id + "/" + id
	launchlog.Info(compHost, "[%s] ← гість підключився з %s (зараз гостей: %d)", cid, addrString(guest.RemoteAddr()), len(s.tracker.guests()))

	// Обгортаємо з'єднання, щоб рахувати трафік для цього гостя. Якщо з
	// якоїсь причини не вийшло — проксіюємо як є, діагностика не критична.
	if tracked, ok := s.tracker.wrap(id, guest); ok {
		guest = tracked
	}

	dsw := startWatch()
	local, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", minecraftPort), 10*time.Second)
	if err != nil {
		launchlog.Error(compHost, "[%s] локальна гра недоступна на 127.0.0.1:%s (%s): %v  ⇒ Minecraft-сервер/LAN-світ не запущений або слухає інший порт; гість побачить причину замість обриву", cid, minecraftPort, dsw, err)
		SendHostReject(guest, RejGameNotRunning)
		return
	}
	defer local.Close()
	launchlog.Debug(compHost, "[%s] з'єднано з локальною грою 127.0.0.1:%s за %s", cid, minecraftPort, dsw)

	stats := pipeBothStats(guest, local)
	lvl := launchlog.Info
	extra := ""
	if stats.BtoA == 0 {
		// Локальна гра не віддала жодного байта — типово: порт зайнятий не
		// Minecraft'ом або сервер упав одразу після accept.
		lvl = launchlog.Warn
		extra = "  ⚠ локальна гра НЕ відповіла жодним байтом"
	}
	lvl(compHost, "[%s] → гість від'єднався: %s%s", cid, stats.describe("гість", "гра"), extra)
}

// pingLoop періодично міряє мережевий RTT до кожного активного гостя і
// записує його в трекер. Помилки ігноруються: пінг — діагностика, а не
// умова роботи тунелю.
func (s *HostSession) pingLoop() {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.closed:
			return
		case <-ticker.C:
			s.probePings()
		}
	}
}

func (s *HostSession) probePings() {
	targets := s.tracker.targets()
	if len(targets) == 0 {
		return
	}
	for _, t := range targets {
		id, ipStr := t[0], t[1]
		desc, ms := pingPeer(s.ts, ipStr, 5*time.Second)
		if ms == 0 {
			launchlog.Warn(compHost, "[%s/%s] %s", s.id, id, desc)
			continue
		}
		launchlog.Debug(compHost, "[%s/%s] %s", s.id, id, desc)
		s.tracker.setPing(id, ms)
	}
	// Зведення по гостях: хто скільки передав і як давно мовчить.
	for _, g := range s.tracker.guests() {
		idle := time.Since(time.UnixMilli(g.LastSeen))
		launchlog.Debug(compHost, "[%s/%s] гість %s: в мережі %s, тиша %s, ↓%s ↑%s, ping=%dms",
			s.id, g.ID, g.GuestIP, fmtDur(time.Since(time.UnixMilli(g.ConnectedAt))), fmtDur(idle),
			fmtBytes(g.BytesIn), fmtBytes(g.BytesOut), g.PingMs)
	}
}

// Close закриває поточну хост-сесію: гасить tsnet-ноду і listener. Адреса
// НЕ звільняється — залишається за власником (наступний StartHost() з тим
// самим Subdomain+OwnerSecret просто оновить IP заново).
func (s *HostSession) Close() error {
	var err error
	s.closeOnce.Do(func() {
		launchlog.Info(compHost, "[%s] ■ ЗУПИНКА хостингу %s (працював %s, гостей зараз: %d)",
			s.id, s.Subdomain, fmtDur(time.Since(s.started)), len(s.tracker.guests()))
		if s.stopWatch != nil {
			s.stopWatch()
		}
		close(s.closed)
		if s.listener != nil {
			_ = s.listener.Close()
		}
		s.udpMu.Lock()
		if s.udpRelay != nil {
			s.udpRelay.Close()
		}
		if s.udpPC != nil {
			_ = s.udpPC.Close()
		}
		s.udpMu.Unlock()
		if s.ts != nil {
			err = s.ts.Close()
			if err != nil {
				launchlog.Warn(compHost, "[%s] ts.Close: %v", s.id, err)
			}
		}
		// Нода ефемерна — її стан на диску більше не потрібен.
		dropNodeStateDir(s.stateDir)
	})
	return err
}

// addrString — безпечне перетворення net.Addr у рядок (nil → "").
func addrString(a net.Addr) string {
	if a == nil {
		return ""
	}
	return a.String()
}

// HostConfig — параметри для StartHost.
type HostConfig struct {
	MinecraftPort string   // порт локального LAN-світу (гра друкує його в чаті при "Open to LAN")
	StateDir      string   // тека лаунчера для tsnet state (.shaurma/p2p/)
	ControlURL    string   // адреса власного Headscale-сервера (https://vpn.shaurma-launcher.org)
	AuthKey       string   // одноразовий ephemeral auth key, отриманий від lobby API
	LobbyBaseURL  string   // адреса Worker lobby API
	LobbyToken    string   // той самий X-Shaurma-Token, що й для роздачі файлів
	LobbyAuth     AuthFunc // JWT+HWID акаунта (nil = анонімно)
	Subdomain     string   // вже застовблена повна адреса власника (з суфіксом)
	OwnerSecret   string   // локальний секрет власника (доводить право оновлювати цю адресу)
	// SessionToken — токен оренди (host-start). Співхост доводить ним себе вузлу в admit; власнику
	// вистачає OwnerSecret.
	SessionToken string
	// OnRevoked — викликається, коли вузол забрав хост-роль (оренду втрачено / співхоста відкликано).
	// Хост-сесію після цього треба зупинити (API.handleHostRevoked).
	OnRevoked func(*HostSession, error)
	// OwnerName — публічний нік акаунта, під яким підняли хостинг. Потрібен
	// лише адмінці Pack Manager («які саме гравці відкривали кімнати»).
	OwnerName string
	// UDPPort — порт UDP-тунелю: на ньому слухає tailnet-адреса хоста, його
	// публікуємо гостям і саме на нього (127.0.0.1:<UDPPort>) форвардимо
	// локально. Дефолт у UI = порт сервера, користувач може поставити
	// будь-який. Жодних підмін. Порожньо = UDP вимкнено.
	UDPPort string
	// AuthKeyFunc — джерело СВІЖОГО одноразового ключа для повторної спроби
	// реєстрації (див. upFreshNode). Порожньо — повторної спроби не буде.
	AuthKeyFunc func(ctx context.Context) (string, error)
}

// pipeBoth копіює трафік в обидва боки і чекає, доки обидва напрямки не
// завершаться. Статистику див. pipeBothStats (trace.go).
func pipeBoth(a, b net.Conn) { pipeBothStats(a, b) }

func firstIPv4(ts *tsnet.Server) (string, error) {
	lc, err := ts.LocalClient()
	if err != nil {
		return "", fmt.Errorf("p2p: local client: %w", err)
	}
	st, err := lc.StatusWithoutPeers(context.Background())
	if err != nil {
		return "", fmt.Errorf("p2p: статус ноди: %w", err)
	}
	for _, ip := range st.TailscaleIPs {
		if ip.Is4() {
			return ip.String(), nil
		}
	}
	return "", fmt.Errorf("p2p: нода не отримала IPv4-адресу")
}

func randomSuffix() string {
	b := make([]byte, 4)
	_, _ = io.ReadFull(cryptoRandReader{}, b)
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	out := make([]byte, len(b))
	for i, v := range b {
		out[i] = alphabet[int(v)%len(alphabet)]
	}
	return string(out)
}
