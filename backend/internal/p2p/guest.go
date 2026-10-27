// guest.go — сторона Guest у схемі «розумного маршрутизатора»: один
// зовнішній listener на 127.0.0.2:25565 (куди резолвиться DNS-wildcard
// "*.play.shaurma-launcher.org"), що читає домен із першого пакета і РОУТИТЬ
// (без підміни самих байтів пакета) з'єднання до потрібної кімнати.
// Кожна ОДНОЧАСНА кімната отримує окрему внутрішню 127.0.0.X (з пулу), і на
// ній так само слухає стандартний порт 25565 — тож будь-який клієнт
// (включно з модованим — див. Handshake.Address) підключається так, ніби це
// прямий, звичайний Minecraft-сервер.
//
// ── ЩО ЗМІНИЛОСЬ (швидкість і стабільність) ───────────────────────────────
// Раніше КОЖНА кімната піднімала ВЛАСНУ tsnet-ноду, причому ЛІНЬКО — при
// першому пакеті. Реєстрація ноди в Headscale займає 1-3 секунди, а клієнт
// Minecraft чекає на відповідь статусу значно менше — тому сервер у списку
// виглядав «закритим», хоч насправді був онлайн. Тепер:
//
//  1. ГОСТЬОВА НОДА ОДНА на весь сервіс (створюється один раз, лінько, і
//     перевикористовується всіма кімнатами) — усі хости в Headscale-мережі,
//     тож однієї ноди досить, щоб дістатись будь-кого, а підйом ноди більше
//     не лежить на критичному шляху пінгу.
//  2. Пінг (NextState == 1) більше НЕ займає слот кімнати: він резолвиться
//     транзитно (JoinAddress → dial → pipe) і не витрачає один із
//     MaxConcurrentRooms слотів. Раніше 4 пінги різних адрес вибивали ліміт
//     і «з'їдали» можливість приєднатись.
//  3. Кімнати мають idle-eviction: після roomIdleTimeout без жодного
//     активного з'єднання кімната закривається і слот повертається в пул.
package p2p

import (
	"shaurma-launcher-backend/internal/crashguard"
	"bufio"
	"context"
	"fmt"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"shaurma-launcher-backend/internal/launchlog"

	"tailscale.com/tsnet"
)

// guestExternalAddr — ЄДИНА адреса, куди резолвиться DNS-wildcard
// "*.play.shaurma-launcher.org" (A-запис) — саме сюди спершу приходить
// будь-яке P2P-підключення, незалежно від того, до якої кімнати гравець
// насправді йде. Порт стандартний, тому SRV-запис не потрібен узагалі.
const guestExternalAddr = "127.0.0.2:25565"

// guestExternalIP — IP зовнішнього входу. Той самий IP, що й у TCP-входу:
// клієнт (і гра, і голосовий мод) стукає на адресу з домену, тобто на
// 127.0.0.2, тож UDP-вхід теж мусить слухати саме тут, а не на 127.0.0.X
// кімнати. Порт UDP-входу = UDPPort, який опублікував хост (без підмін).
const guestExternalIP = "127.0.0.2"

// Внутрішній TCP-порт кімнати НЕ константа: кожна кімната слухає на тому
// порту, який опублікував хост (RoomInfo.HostPort) — див. guestRoom.tcpPort.
// Зовнішній вхід guestExternalAddr лишається фіксованим, бо DNS-wildcard
// резолвиться саме в нього і гравець вводить адресу без порту.


// roomIdleTimeout — скільки кімната живе БЕЗ жодного активного з'єднання.
// Потрібно, щоб пінг/разове підключення не тримало слот із пулу вічно:
// ліміт MaxConcurrentRooms — це ресурси (listener + запис), і вони мають
// звільнятись, коли гравець давно пішов.
const roomIdleTimeout = 5 * time.Minute

// nodeRetryAfter — якщо спільна гостьова нода не піднялась, не битимемо
// Headscale на кожному пакеті: повторюємо спробу не частіше, ніж раз на цей
// інтервал (мережа могла тимчасово відвалитись — тоді повторна спроба
// потрібна, але не 100 разів на секунду).
const nodeRetryAfter = 30 * time.Second

// GuestService — фоновий слухач гостьової сторони. Один екземпляр на весь
// час роботи лаунчера: гравець може заходити на різні "ABCD.play..." адреси
// без перезапуску сервісу, і тримати кілька таких з'єднань одночасно
// (до MaxConcurrentRooms), кожне на власній внутрішній 127.0.0.X.
type GuestService struct {
	cfg GuestConfig

	// tickets — адреса тимчасового запрошення → преамбула з квитком (tinvite_gate.go).
	tickets sync.Map
	// p2pTickets — квитки допуску кімнат із режимом доступу != all (p2p_gate.go): беруться з join,
	// оновлюються заздалегідь, преамбулу SHP2P1 додає preambleFor.
	p2pTickets *ticketStore
	// resolver — зливає паралельні /p2p/join і коротко кешує відмови для пінгу списку серверів
	// (resolve_gate.go): оновлення списку не б'є по VPS щоразу.
	resolver resolveGate

	mu       sync.Mutex
	listener net.Listener // зовнішній вхід, guestExternalAddr
	closed   chan struct{}
	stopOnce sync.Once

	// ── Спільна tsnet-нода ────────────────────────────────────────────
	// nodeMu захищає лінивий підйом ноди (він довгий, тож решта горутин
	// мають чекати, а не плодити паралельні ноди).
	nodeMu    sync.Mutex
	node      *tsnet.Server
	nodeDir   string
	nodeErr   error
	nodeErrAt time.Time
	nodeStop  func() // зупинка «пульсу» гостьової ноди (trace_ts.go)

	// dialFails — скільки діалів до хоста підряд провалилось; lastNodeReset — коли востаннє
	// перестворювали гостьову ноду (UnixNano). Див. noteDialResult.
	dialFails     atomic.Int32
	lastNodeReset atomic.Int64

	pool *localIPPool

	// createMu серіалізує СТВОРЕННЯ кімнат: два одночасні підключення до
	// тієї самої нової адреси не мають підняти дві кімнати й два listener'и
	// на одному 127.0.0.X.
	createMu sync.Mutex

	roomsMu sync.Mutex
	rooms   map[string]*guestRoom

	// ── UDP: слухач порту голосового моду ──────────────────────────────
	// Порт МОДА (де сам голосовий клієнт гравця шле/приймає пакети,
	// ── видалено: запасний спільний слухач "127.0.0.1:24454" ───────────
	// Раніше тут піднімався окремий сокет на дефолтному порту мода і
	// форвардив пакети в "останню активну кімнату" (lastRoom) — але
	// гравці налаштовують голосовий клієнт ходити прямо на IP:порт
	// тунелю кімнати (тепер спільний вхід 127.0.0.2:<UDPPort>, див. udpInbound нижче),
	// тож цей шлях ніхто не використовував, тільки бив по логах і тримав
	// зайвий порт зайнятим. udpEnabled лишається — потрібен лише щоб
	// SetVoicePort/UDPStatus (виклики з UI) не падали.
	udpMu      sync.Mutex
	udpEnabled bool // галочка UI (SetP2PUDP); тунель тепер завжди автоматичний

	// udpIn — UDP-входи на guestExternalIP:<порт>, по одному на порт. Як і
	// TCP-вхід, вони спільні для всіх кімнат і розкидають пакети по
	// кімнатах (див. udpInbound).
	udpInMu sync.Mutex
	udpIn   map[int]*udpInbound

	roomMu   sync.Mutex
	lastRoom *RoomInfo
}

// udpInbound — один UDP-сокет 127.0.0.2:<port>. У UDP немає хендшейку з
// доменом, тож кімнату визначаємо так: (1) джерело, яке вже має сесію,
// лишається у своїй кімнаті; (2) нове джерело йде в ОСТАННЮ активну
// кімнату, що публікує цей UDP-порт.
type udpInbound struct {
	port int
	pc   net.PacketConn

	mu      sync.Mutex
	bySrc   map[string]*guestRoom // джерело → кімната
	current *guestRoom            // остання активна кімната цього порту

	// Локальний маршрут: адресу хостить ЦЕЙ ЖЕ лаунчер → голос іде прямо на
	// localTarget (127.0.0.1:<порт>) без tsnet. preferLocal — що з двох
	// (локальний сервер чи чужа кімната) обрано останнім для НОВИХ джерел.
	localTarget string
	localRelay  *udpRelay
	localSrc    map[string]bool
	preferLocal bool

	dropGate *logGate // ліміт логів «пакет відкинуто» (голос шле десятки пакетів/с)
}

// udpNoTunnelGate — ліміт логів «хост не публікує UDP-тунель» на рівні пакета.
var udpNoTunnelGate = newLogGate(10 * time.Second)

// guestRoom — одна конкретна P2P-адреса (кімната), до якої гість
// підключився: власна внутрішня IP і TCP-listener на ній. tsnet-нода тут НЕ
// власна — усі кімнати діляться спільною гостьовою нодою (див. ensureNode).
type guestRoom struct {
	address   string
	localIP   string
	tcpPort   string // TCP-порт кімнати = HostPort хоста (не хардкод)
	listener  net.Listener
	closed    chan struct{}
	closeOnce sync.Once

	mu         sync.Mutex
	active     int
	lastActive time.Time
	// roomInfo — останні відомі HostTailscaleIP/HostPort/UDPPort для цієї
	// адреси. Кімната живе довго (до roomIdleTimeout), а хост тим часом
	// може перезапустити P2P і отримати нову tsnet-ноду з новою
	// tailnet-адресою — тож це поле НЕ константа з моменту створення
	// кімнати, а оновлюється перед кожним новим з'єднанням (див.
	// tunnelToHost), інакше гість довго довбеться в мертву адресу
	// (dial tcp ...: i/o timeout / connection refused).
	roomInfo RoomInfo

	// ── UDP-тунель голосового чату ЦІЄЇ кімнати ─────────────────────────
	// На відміну від попередньої версії (один спільний 127.0.0.1:<port>
	// на весь GuestService), тунельний слухач тепер сидить на r.localIP —
	// ТІЙ САМІЙ внутрішній loopback-адресі, що й TCP-listener цієї
	// кімнати (127.0.0.3, 127.0.0.4, ...). Так UDP і TCP цієї кімнати
	// завжди мають однаковий IP:порт, як і в TCP/UDP хоста (host.go:
	// ts.Listen/ts.ListenPacket на одному tailnet ip4:MinecraftPort).
	udpMu      sync.Mutex
	udpRelay   *udpRelay
	tunnelPort int
}

func (r *guestRoom) touch() {
	r.mu.Lock()
	r.lastActive = time.Now()
	r.mu.Unlock()
}

func (r *guestRoom) setRoomInfo(info RoomInfo) {
	r.mu.Lock()
	r.roomInfo = info
	r.mu.Unlock()
}

func (r *guestRoom) getRoomInfo() RoomInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.roomInfo
}

func (r *guestRoom) connOpened() {
	r.mu.Lock()
	r.active++
	r.lastActive = time.Now()
	r.mu.Unlock()
}

func (r *guestRoom) connClosed() {
	r.mu.Lock()
	if r.active > 0 {
		r.active--
	}
	r.lastActive = time.Now()
	r.mu.Unlock()
}

func (r *guestRoom) idleFor() (time.Duration, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return time.Since(r.lastActive), r.active == 0
}

// GuestConfig — параметри для NewGuestService.
type GuestConfig struct {
	StateDir     string // тека лаунчера для tsnet state (.shaurma/p2p/)
	ControlURL   string // адреса Headscale (https://vpn.shaurma-launcher.org)
	PlaySuffix   string // "play.shaurma-launcher.org" — без коду кімнати спереду
	LobbyBaseURL string
	LobbyToken   string
	LobbyAuth    AuthFunc // JWT+HWID акаунта: без нього вузол не видасть квиток (nil = анонімно)

	// AuthKeyFunc видає свіжий ephemeral auth key на вимогу (дзвонить у
	// Worker /p2p/auth-key). Функція, а не готовий ключ: ключ має бути щойно
	// виданий — не той, що міг протухнути.
	AuthKeyFunc func(ctx context.Context) (string, error)

	// LocalHostPort повертає локальний TCP-порт гри, якщо цю адресу хостить
	// САМЕ цей лаунчер (той самий ПК), і false — якщо адреса чужа.
	//
	// Навіщо: якщо гравець відкриває свою ж P2P-адресу на тій самій
	// машині, tsnet-діал "до себе" йде через Headscale-ноду, яка щойно
	// піднялась, і часто не встигає/не працює — гра бачить «сервер
	// недоступний». Прямий похід на 127.0.0.1:<порт> завжди працює і не
	// витрачає слот кімнати.
	LocalHostPort func(subdomain string) (string, bool)

	// LocalHostUDPPort — те саме для UDP-голосу: порт голосового мода локальної
	// хост-сесії цієї адреси. Якщо задано, голос такого гравця йде НАПРЯМУ на
	// 127.0.0.1:<порт>, а не лишається без тунелю (кімнату для локальної
	// адреси ми не створюємо, тож звичайний UDP-вхід для неї не піднімався).
	LocalHostUDPPort func(subdomain string) (string, bool)
}

// NewGuestService створює сервіс, ще нічого мережевого не роблячи.
func NewGuestService(cfg GuestConfig) *GuestService {
	g := &GuestService{
		cfg:    cfg,
		closed: make(chan struct{}),
		pool:   newLocalIPPool(),
		rooms:  make(map[string]*guestRoom),
		udpIn:  make(map[int]*udpInbound),
	}
	g.p2pTickets = newTicketStore()
	g.p2pTickets.refresh = func(address string) error {
		_, err := g.resolveHost(address, "tk")
		return err
	}
	g.p2pTickets.alive = func(address string) bool {
		_, ok := g.existingRoom(address)
		return ok
	}
	g.p2pTickets.stopped = func() bool {
		select {
		case <-g.closed:
			return true
		default:
			return false
		}
	}
	return g
}

// ActiveRoomCount — скільки P2P-кімнат гість тримає відкритими просто
// зараз (для показу "2/3" у статусі UI).
func (g *GuestService) ActiveRoomCount() int { return g.pool.count() }

// Start піднімає ЄДИНИЙ зовнішній TCP-listener на guestExternalAddr.
// Ідемпотентний (повторний Start — no-op), бо викликається з автостарту
// лаунчера й може випадково зазватись двічі.
func (g *GuestService) Start() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.listener != nil {
		return nil
	}
	logP2PEnvironment(g.cfg.ControlURL, g.cfg.LobbyBaseURL, g.cfg.PlaySuffix)
	ln, err := net.Listen("tcp", guestExternalAddr)
	if err != nil {
		launchlog.Error(compGuest, "СТАРТ гостьового входу %s ПРОВАЛЕНО: %v  ⇒ %s", guestExternalAddr, err, explainListenErr(guestExternalAddr, err))
		return fmt.Errorf("p2p: не вдалося слухати %s (порт зайнятий іншим процесом?): %w", guestExternalAddr, err)
	}
	g.listener = ln
	{
		_f, _a0 := g.acceptLoop, ln
		go func() {
			defer crashguard.Recover("g.acceptLoop")
			_f(_a0)
		}()
	}
	{
		_f := g.janitorLoop
		go func() {
			defer crashguard.Recover("g.janitorLoop")
			_f()
		}()
	}
	launchlog.Info(compGuest, "гостьовий вхід слухає на %s (play=*.%s, макс. кімнат %d, idle-таймаут кімнати %s)",
		guestExternalAddr, g.cfg.PlaySuffix, MaxConcurrentRooms, roomIdleTimeout)
	return nil
}

func (g *GuestService) acceptLoop(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-g.closed:
				return
			default:
				launchlog.Error(compGuest, "accept на %s: %v (вхід більше не приймає з'єднань!)", guestExternalAddr, err)
				return
			}
		}
		{
			_f, _a0 := g.routeConn, conn
			go func() {
				defer crashguard.Recover("g.routeConn")
				_f(_a0)
			}()
		}
	}
}

// routeConn — «розумний маршрутизатор»: читає перший пакет, з'ясовує
// кімнату з домену і ПЕРЕНАПРАВЛЯЄ (proxy на рівні TCP, без модифікації
// байтів) на внутрішній listener цієї кімнати. Той самий байт-у-байт
// фрейм переграється далі — гра про перенаправлення нічого не знає.
//
// Головне розділення: NextState==2 (логін) — реальний захід у гру, він
// займає слот кімнати; NextState==1 (status) — просте оновлення списку
// серверів, воно НЕ займає слот (див. routeStatus).
func (g *GuestService) routeConn(conn net.Conn) {
	defer conn.Close()
	cid := newTraceID("g")
	sw := startWatch()
	launchlog.Debug(compGuest, "[%s] ← вхідне з'єднання від %s", cid, conn.RemoteAddr())
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))

	br := bufio.NewReader(conn)
	hs, err := ReadHandshake(br)
	if err != nil {
		// Не Minecraft-хендшейк (сканер порту, помилковий клієнт) — не
		// error, щоб не спамити на кожен випадковий пінг.
		launchlog.Debug(compGuest, "[%s] не Minecraft handshake з %s (%s): %v", cid, conn.RemoteAddr(), sw, err)
		return
	}
	_ = conn.SetReadDeadline(time.Time{})
	// Усе, що клієнт устиг надіслати слідом за хендшейком (Status Request, Login Start),
	// лежить у br: далі читаємо ТІЛЬКИ через обгортку, інакше ці байти загубились би.
	tuneConn(conn)
	conn = &bufConn{Conn: conn, r: br}
	launchlog.Debug(compGuest, "[%s] handshake за %s: протокол=%d адреса=%q порт=%d стан=%s (%d Б)",
		cid, sw, hs.ProtocolVersion, hs.ServerAddress, hs.ServerPort, nextStateName(hs.NextState), len(hs.Raw))

	address, err := hs.Address(g.cfg.PlaySuffix)
	if err != nil {
		launchlog.Error(compGuest, "[%s] домен із handshake не розпізнано: %v  ⇒ гравець ввів адресу не у форматі <адреса>.%s", cid, err, g.cfg.PlaySuffix)
		g.rejectClient(conn, hs, RejBadAddress, cid, false)
		return
	}

	// Адресу хостить САМЕ цей лаунчер (гравець грає на тому ж ПК, де й
	// сервер) — маршрутизуємо одразу на локальну гру, без tsnet-тунелю
	// «до себе» та без зайняття слоту кімнати.
	if port, ok := g.localHostPort(address); ok {
		launchlog.Info(compGuest, "[%s] %s: адресу хостить ЦЕЙ ЖЕ лаунчер → пряме проксі на 127.0.0.1:%s (стан=%s)", cid, address, port, nextStateName(hs.NextState))
		if hs.NextState != 1 { // лише реальний вхід у гру, не пінг списку серверів
			g.ensureLocalUDP(address, cid)
		}
		g.routeLocal(conn, hs, port, cid)
		return
	}

	if hs.NextState == 1 {
		launchlog.Debug(compGuest, "[%s] %s: статус (пінг списку серверів), слот кімнати не займаю", cid, address)
		g.routeStatus(conn, hs, address, cid)
		return
	}
	launchlog.Info(compGuest, "[%s] %s: ВХІД У ГРУ (стан=%s, протокол=%d) від %s", cid, address, nextStateName(hs.NextState), hs.ProtocolVersion, conn.RemoteAddr())
	g.routeLogin(conn, hs, address, cid)
}

func nextStateName(n int64) string {
	switch n {
	case 1:
		return "1(status)"
	case 2:
		return "2(login)"
	case 3:
		return "3(transfer)"
	}
	return fmt.Sprintf("%d(?)", n)
}

// localHostPort звертається до колбеку API: чи хостить цю адресу цей же
// лаунчер (тоді — локальний порт гри).
func (g *GuestService) localHostPort(address string) (string, bool) {
	if g.cfg.LocalHostPort == nil {
		return "", false
	}
	return g.cfg.LocalHostPort(address)
}

// routeLocal — пряме проксі на локальну гру (без tsnet). Використовується,
// коли гравець і хост — один і той самий ПК: локальна гра завжди доступна
// на 127.0.0.1:<порт>, тож гнати пакети через Headscale-ноду "до себе"
// немає сенсу.
func (g *GuestService) routeLocal(conn net.Conn, hs Handshake, port, cid string) {
	dsw := startWatch()
	local, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", port), 5*time.Second)
	if err != nil {
		launchlog.Warn(compGuest, "[%s] локальна гра на 127.0.0.1:%s недоступна (%s): %v  ⇒ світ/сервер хоста на цьому ПК не запущений", cid, port, dsw, err)
		g.rejectClient(conn, hs, RejGameNotRunning, cid, false)
		return
	}
	defer local.Close()
	if _, err := local.Write(hs.Raw); err != nil {
		launchlog.Warn(compGuest, "[%s] локальний replay handshake: %v", cid, err)
		return
	}
	stats := pipeBothStats(conn, local)
	launchlog.Info(compGuest, "[%s] локальна сесія завершена: %s", cid, stats.describe("клієнт", "гра"))
}

// routeLogin — гравець реально заходить у гру. Піднімаємо (або перевико-
// ристовуємо) повноцінну кімнату зі своїм 127.0.0.X і тунелюємо через неї.
//
// Якщо зайти неможливо (хост офлайн, немає доступу, VPS недоступний, хост відмовив
// чи мовчки закрив з'єднання) — гравцеві йде Login Disconnect із причиною його
// мовою (reject.go), а не безіменний «Connection reset».
func (g *GuestService) routeLogin(conn net.Conn, hs Handshake, address, cid string) {
	room, err := g.roomFor(address, cid)
	if err != nil {
		launchlog.Error(compGuest, "[%s] %s: %v", cid, address, err)
		g.rejectClient(conn, hs, g.reasonFor(err), cid, false)
		return
	}

	dsw := startWatch()
	local, err := net.DialTimeout("tcp", net.JoinHostPort(room.localIP, room.tcpPort), 5*time.Second)
	if err != nil {
		launchlog.Error(compGuest, "[%s] внутрішній маршрут %s (%s:%s) недоступний (%s): %v  ⇒ кімнату скинуто, наступний вхід створить її наново", cid, address, room.localIP, room.tcpPort, dsw, err)
		g.evictRoom(room)
		g.rejectClient(conn, hs, RejInternal, cid, false)
		return
	}
	defer local.Close()
	tuneConn(local)

	room.connOpened()
	defer room.connClosed()

	if _, err := local.Write(withPreamble(g.preambleFor(address), hs.Raw)); err != nil {
		launchlog.Error(compGuest, "[%s] replay handshake у внутрішній маршрут %s: %v", cid, address, err)
		g.rejectClient(conn, hs, RejInternal, cid, false)
		return
	}
	// Перше читання відповіді хоста перевіряє маркер відмови (reject.go): хост
	// каже ПРИЧИНУ кодом, а текст формуємо тут — мовою лаунчера гравця.
	sniff := newRejectSniffConn(local, func(r RejectReason) {
		sent := writeReject(conn, hs.ProtocolVersion, hs.NextState, r, false)
		launchlog.Info(compGuest, "[%s] %s: хост не пустив: %s (надіслано=%v)", cid, address, RejectKey(r), sent)
	})
	stats := pipeBothStats(conn, sniff)
	lvl := launchlog.Info
	extra := ""
	if sniff.Rejected() {
		extra = "  ⇒ гравцеві показано причину відмови"
	} else if stats.BtoA == 0 {
		lvl = launchlog.Warn
		extra = "  ⚠ від хоста НЕ прийшло жодного байта — дивіться рядки [t…] про тунель до хоста"
	}
	lvl(compGuest, "[%s] %s: сесія завершена: %s%s", cid, address, stats.describe("клієнт", "кімната"), extra)
}

// routeStatus — оновлення списку серверів (ping). Слот кімнати НЕ займаємо:
// якщо кімната вже піднята — перевикористовуємо її (це і швидко, і чесно),
// інакше робимо транзитний маршрут і одразу його закриваємо.
//
// Саме це лікує «сервер виглядає закритим»: раніше кожен пінг піднімав
// важку кімнату (нова tsnet-нода) і клієнт не дочікувався відповіді; тепер
// на пінг відповідає вже піднята спільна нода, без реєстрацій і без
// витрачання одного з трьох слотів.
//
// Коли хост недоступний, замість сірого «Can't connect» гравець бачить у списку
// MOTD із причиною (reject.go). Вердикт кілька секунд кешується (resolve_gate.go),
// щоб оновлення списку не навантажувало VPS.
func (g *GuestService) routeStatus(conn net.Conn, hs Handshake, address, cid string) {
	sw := startWatch()
	if room, ok := g.existingRoom(address); ok {
		local, err := net.DialTimeout("tcp", net.JoinHostPort(room.localIP, room.tcpPort), 3*time.Second)
		if err != nil {
			launchlog.Debug(compGuest, "[%s] статус %s: внутрішній маршрут недоступний: %v", cid, address, err)
			g.evictRoom(room)
			g.rejectClient(conn, hs, RejHostNoResponse, cid, false)
			return
		}
		defer local.Close()
		room.connOpened()
		defer room.connClosed()
		g.statusTransit(conn, hs, local, address, cid)
		launchlog.Debug(compGuest, "[%s] статус %s через існуючу кімнату за %s", cid, address, sw)
		return
	}

	host, err := g.resolveHostPing(address, cid)
	if err != nil {
		// Хост офлайн / немає доступу / VPS недоступний — гравець бачить причину в списку.
		reason := g.reasonFor(err)
		launchlog.Debug(compGuest, "[%s] статус %s: %v  ⇒ %s", cid, address, err, explainLobbyErr(err))
		g.rejectClient(conn, hs, reason, cid, false)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	upstream, err := g.dialHost(ctx, host, cid)
	if err != nil {
		launchlog.Debug(compGuest, "[%s] статус %s: діал хоста не вдався: %v", cid, address, err)
		g.rejectClient(conn, hs, g.reasonFor(err), cid, false)
		return
	}
	defer upstream.Close()
	g.statusTransit(conn, hs, upstream, address, cid)
	launchlog.Debug(compGuest, "[%s] статус %s транзитом до хоста за %s", cid, address, sw)
}

// existingRoom повертає вже підняту кімнату для адреси, якщо вона є.
func (g *GuestService) existingRoom(address string) (*guestRoom, bool) {
	g.roomsMu.Lock()
	defer g.roomsMu.Unlock()
	r, ok := g.rooms[address]
	return r, ok
}

// roomFor повертає вже підняту кімнату для цієї адреси (ідемпотентно —
// повторні підключення перевикористовують і IP, і listener), або піднімає
// нову — в межах MaxConcurrentRooms.
func (g *GuestService) roomFor(address, cid string) (*guestRoom, error) {
	if r, ok := g.existingRoom(address); ok {
		r.touch()
		launchlog.Debug(compGuest, "[%s] кімната %s вже піднята (%s:%s) — перевикористовую", cid, address, r.localIP, r.tcpPort)
		return r, nil
	}

	g.createMu.Lock()
	defer g.createMu.Unlock()

	// Повторна перевірка під замком: поки ми чекали, інша горутина могла
	// підняти цю саму кімнату (див. createMu).
	if r, ok := g.existingRoom(address); ok {
		r.touch()
		launchlog.Debug(compGuest, "[%s] кімнату %s тим часом підняло інше з'єднання — перевикористовую", cid, address)
		return r, nil
	}

	csw := startWatch()
	launchlog.Info(compGuest, "[%s] створюю кімнату %s (зайнято слотів %d/%d)", cid, address, g.pool.count(), MaxConcurrentRooms)

	localIP, err := g.pool.acquire(address)
	if err != nil {
		// ErrPoolExhausted — гравець уже тримає MaxConcurrentRooms
		// одночасних P2P-з'єднань. Це свідомий хард-ліміт, а не збій мережі.
		launchlog.Error(compGuest, "[%s] немає вільного слота для %s: %v  ⇒ уже відкрито %d/%d P2P-кімнат; слот звільниться, коли одна простоїть %s без з'єднань", cid, address, err, g.pool.count(), MaxConcurrentRooms, roomIdleTimeout)
		return nil, err
	}
	launchlog.Debug(compGuest, "[%s] кімнаті %s виділено внутрішню адресу %s", cid, address, localIP)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	nsw := startWatch()
	if _, err := g.ensureNode(ctx); err != nil {
		g.pool.release(address)
		launchlog.Error(compGuest, "[%s] гостьова нода недоступна (%s): %v  ⇒ %s", cid, nsw, err, explainNodeErr(err))
		return nil, withReason(RejNetworkDown, fmt.Errorf("не вдалося підняти мережу: %w", err))
	}
	launchlog.Debug(compGuest, "[%s] гостьова нода готова за %s", cid, nsw)

	roomInfo, err := g.resolveHost(address, cid)
	if err != nil {
		g.pool.release(address)
		return nil, err
	}

	tcpPort := roomInfo.HostPort
	if p, perr := strconv.Atoi(tcpPort); perr != nil || p < 1 || p > 65535 {
		g.pool.release(address)
		launchlog.Error(compGuest, "[%s] хост %s опублікував некоректний TCP-порт %q", cid, address, roomInfo.HostPort)
		return nil, fmt.Errorf("хост %s опублікував некоректний TCP-порт %q", address, roomInfo.HostPort)
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(localIP, tcpPort))
	if err != nil {
		g.pool.release(address)
		launchlog.Error(compGuest, "[%s] не вдалося слухати %s:%s: %v  ⇒ %s", cid, localIP, tcpPort, err, explainListenErr(net.JoinHostPort(localIP, tcpPort), err))
		return nil, fmt.Errorf("не вдалося слухати на %s:%s: %w", localIP, tcpPort, err)
	}

	r := &guestRoom{
		address:    address,
		localIP:    localIP,
		tcpPort:    tcpPort,
		listener:   ln,
		closed:     make(chan struct{}),
		lastActive: time.Now(),
		roomInfo:   *roomInfo,
	}

	g.roomsMu.Lock()
	g.rooms[address] = r
	g.roomsMu.Unlock()

	g.setLastRoom(r, *roomInfo)

	{
		_f, _a0, _a1 := g.roomAcceptLoop, r, *roomInfo
		go func() {
			defer crashguard.Recover("g.roomAcceptLoop")
			_f(_a0, _a1)
		}()
	}
	launchlog.Info(compGuest, "[%s] ✔ кімната %s ГОТОВА за %s: внутрішній %s:%s → хост %s:%s (udp=%q)",
		cid, address, csw, localIP, tcpPort, roomInfo.HostTailscaleIP, roomInfo.HostPort, roomInfo.UDPPort)
	return r, nil
}

// resolveHostOnce — один справжній запит до lobby (обгортки resolveHost /
// resolveHostPing у reject_guest.go зливають паралельні виклики й кешують
// відмови для пінгу). tag — ID з'єднання для логів.
func (g *GuestService) resolveHostOnce(address, tag string) (*RoomInfo, error) {
	client := NewLobbyClient(g.cfg.LobbyBaseURL, g.cfg.LobbyToken).WithAuth(g.cfg.LobbyAuth)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	sw := startWatch()
	roomInfo, err := client.JoinAddress(ctx, address)
	if err != nil {
		launchlog.Warn(compGuest, "[%s] резолв %s НЕ вдався за %s: %v  ⇒ %s", tag, address, sw, err, explainLobbyErr(err))
		return nil, withReason(g.reasonForResolveErr(err), fmt.Errorf("адресу не знайдено: %w", err))
	}
	roomInfo.Subdomain = address // див. коментар біля поля RoomInfo.Subdomain
	g.p2pTickets.remember(address, roomInfo.Ticket, roomInfo.TicketExpiresAt)
	launchlog.Debug(compGuest, "[%s] резолв %s → хост %s:%s udp=%q за %s", tag, address, roomInfo.HostTailscaleIP, roomInfo.HostPort, roomInfo.UDPPort, sw)
	return &roomInfo, nil
}

// roomAcceptLoop приймає з'єднання на внутрішньому listener'і кімнати (їх
// подає routeConn через local Dial) і тунелює кожне до Host через СПІЛЬНУ
// гостьову ноду.
func (g *GuestService) roomAcceptLoop(r *guestRoom, roomInfo RoomInfo) {
	for {
		conn, err := r.listener.Accept()
		if err != nil {
			select {
			case <-r.closed:
				return
			default:
				launchlog.Error(compGuest, "внутрішній accept %s: %v (кімната більше не приймає з'єднань!) ⇒ скидаю її, наступний вхід створить наново", r.address, err)
				g.evictRoom(r)
				return
			}
		}
		{
			_f, _a0, _a1 := g.tunnelToHost, conn, r
			go func() {
				defer crashguard.Recover("g.tunnelToHost")
				_f(_a0, _a1)
			}()
		}
	}
}

// tunnelToHost тунелює одне з'єднання до Host. Перед кожним дзвінком
// перезапитуємо лобі за свіжими HostTailscaleIP/HostPort: кімната гостя
// живе довго (до roomIdleTimeout), і за цей час хост міг зупинити й
// заново запустити P2P — тоді в нього НОВА tsnet-нода з новою tailnet-
// адресою, а стара закешована адреса призводить до "i/o timeout" /
// "connection refused" аж до протухання кімнати. Якщо лобі недоступне —
// падаємо назад на останню відому адресу (best-effort), а не валимо
// з'єднання одразу.
func (g *GuestService) tunnelToHost(conn net.Conn, r *guestRoom) {
	defer conn.Close()
	r.touch()
	tid := newTraceID("t")
	sw := startWatch()

	prev := r.getRoomInfo()
	roomInfo := prev
	if fresh, err := g.resolveHost(r.address, tid); err == nil {
		if fresh.HostTailscaleIP != prev.HostTailscaleIP || fresh.HostPort != prev.HostPort || fresh.UDPPort != prev.UDPPort {
			launchlog.Warn(compGuest, "[%s] адреса хоста %s ЗМІНИЛАСЬ: %s:%s(udp %q) → %s:%s(udp %q) — хост перезапускав P2P; стара закешована адреса була б мертвою",
				tid, r.address, prev.HostTailscaleIP, prev.HostPort, prev.UDPPort, fresh.HostTailscaleIP, fresh.HostPort, fresh.UDPPort)
		}
		roomInfo = *fresh
		r.setRoomInfo(*fresh)
		// Хост міг УВІМКНУТИ UDP пізніше (галочку голосу можна перемкнути
		// без перезапуску P2P-хостингу) — тож на кожне свіже підключення
		// теж пере-перевіряємо: раптом з'явився UDPPort, якого не було на
		// момент створення кімнати.
		g.setLastRoom(r, *fresh)
	} else {
		launchlog.Warn(compGuest, "[%s] оновлення адреси хоста %s не вдалось, використовую останню відому %s:%s: %v", tid, r.address, prev.HostTailscaleIP, prev.HostPort, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	dsw := startWatch()
	upstream, err := g.dialHost(ctx, &roomInfo, tid)
	if err != nil {
		launchlog.Error(compGuest, "[%s] не вдалося з'єднатись з хостом %s (%s:%s) за %s (разом %s): %v  ⇒ %s",
			tid, r.address, roomInfo.HostTailscaleIP, roomInfo.HostPort, dsw, sw, err, explainDialErr(err))
		g.noteDialResult(false, tid)
		return
	}
	g.noteDialResult(true, tid)
	defer upstream.Close()
	launchlog.Info(compGuest, "[%s] ✓ тунель до хоста %s встановлено за %s (діал %s): %s → %s",
		tid, r.address, sw, dsw, addrOf(upstream.LocalAddr()), addrOf(upstream.RemoteAddr()))

	stats := pipeBothStats(conn, upstream)
	lvl := launchlog.Info
	extra := ""
	if stats.BtoA == 0 {
		lvl = launchlog.Warn
		extra = "  ⚠ хост (tsnet) НЕ відповів жодним байтом — перевірте, чи у хоста запущена гра на опублікованому порту"
	}
	lvl(compGuest, "[%s] тунель %s закрито: %s%s", tid, r.address, stats.describe("клієнт", "хост"), extra)
}

// dialHost відкриває TCP-з'єднання до хоста через спільну гостьову ноду.
// Хости завжди в тій самій Headscale-мережі, тож однієї ноди достатньо.
// tag — ID з'єднання для логів.
func (g *GuestService) dialHost(ctx context.Context, roomInfo *RoomInfo, tag string) (net.Conn, error) {
	ts, err := g.ensureNode(ctx)
	if err != nil {
		launchlog.Error(compGuest, "[%s] діал хоста: гостьова нода недоступна: %v  ⇒ %s", tag, err, explainNodeErr(err))
		return nil, withReason(RejNetworkDown, err)
	}
	hostAddr := net.JoinHostPort(roomInfo.HostTailscaleIP, roomInfo.HostPort)
	launchlog.Debug(compGuest, "[%s] діал tsnet tcp %s …", tag, hostAddr)
	conn, err := ts.Dial(ctx, "tcp", hostAddr)
	if err != nil {
		// Причину збою (немає в netmap / offline / немає шляху) з'ясовуємо у
		// фоні: гравець не чекає, а в лозі з'являється повна діагностика.
		{
			_f, _a0, _a1, _a2, _a3, _a4, _a5 := diagDialFailure, "guest", tag, ts, roomInfo.HostTailscaleIP, roomInfo.HostPort, err
			go func() {
				defer crashguard.Recover("diagDialFailure")
				_f(_a0, _a1, _a2, _a3, _a4, _a5)
			}()
		}
		return nil, withReason(RejHostNoResponse, err)
	}
	// Прогріваємо прямий шлях у фоні: менше стрибків пінгу на початку сесії.
	warmDirectPath(ts, roomInfo.HostTailscaleIP, tag)
	return conn, nil
}

// focusIPs — tailnet-адреси хостів усіх активних кімнат (для логу пірів).
func (g *GuestService) focusIPs() []string {
	g.roomsMu.Lock()
	rooms := make([]*guestRoom, 0, len(g.rooms))
	for _, r := range g.rooms {
		rooms = append(rooms, r)
	}
	g.roomsMu.Unlock()
	var out []string
	for _, r := range rooms {
		if ip := r.getRoomInfo().HostTailscaleIP; ip != "" {
			out = append(out, ip)
		}
	}
	return out
}

// ensureNode повертає спільну гостьову tsnet-ноду, піднімаючи її лінько при
// першому використанні. Невдала спроба кешується на nodeRetryAfter, щоб не
// бити Headscale на кожному пакеті.
func (g *GuestService) ensureNode(ctx context.Context) (*tsnet.Server, error) {
	g.nodeMu.Lock()
	defer g.nodeMu.Unlock()

	if g.node != nil {
		return g.node, nil
	}
	if g.nodeErr != nil && time.Since(g.nodeErrAt) < nodeRetryAfter {
		launchlog.Debug(compGuest, "гостьова нода: повтор підйому заблоковано ще на %s — віддаю кешовану помилку: %v",
			fmtDur(nodeRetryAfter-time.Since(g.nodeErrAt)), g.nodeErr)
		return nil, g.nodeErr
	}

	gid := newTraceID("n")
	total := startWatch()
	launchlog.Info(compGuest, "[%s] гостьової ноди немає — піднімаю (ліниво, спільна для всіх кімнат)", gid)

	ksw := startWatch()
	authKey, err := g.cfg.AuthKeyFunc(ctx)
	if err != nil {
		launchlog.Error(compGuest, "[%s] auth key НЕ отримано за %s: %v  ⇒ %s", gid, ksw, err, explainLobbyErr(err))
		g.cacheNodeErr(fmt.Errorf("отримання auth key: %w", err))
		return nil, g.nodeErr
	}
	launchlog.Debug(compGuest, "[%s] auth key отримано за %s: %s", gid, ksw, maskSecret(authKey))

	stateDir, err := freshNodeStateDir(g.cfg.StateDir, "guest")
	if err != nil {
		launchlog.Error(compGuest, "[%s] не вдалося створити теку стану: %v", gid, err)
		g.cacheNodeErr(err)
		return nil, g.nodeErr
	}

	ts, err := upFreshNode(ctx, nodeUpConfig{
		Role:        "guest",
		Tag:         gid,
		Prefix:      "shaurma-guest-",
		StateDir:    stateDir,
		AuthKey:     authKey,
		AuthKeyFunc: g.cfg.AuthKeyFunc,
		ControlURL:  g.cfg.ControlURL,
		OnFreshState: func() error {
			dropNodeStateDir(stateDir)
			d, derr := freshNodeStateDir(g.cfg.StateDir, "guest")
			if derr != nil {
				return derr
			}
			stateDir = d
			return nil
		},
	})
	if err != nil {
		dropNodeStateDir(stateDir)
		launchlog.Error(compGuest, "[%s] ✗ гостьова нода НЕ піднялась за %s: %v", gid, total, err)
		g.cacheNodeErr(err)
		return nil, g.nodeErr
	}

	g.node = ts
	g.nodeDir = stateDir
	g.nodeErr = nil
	g.nodeStop = watchNode("guest", gid, ts, nodePulseInterval, g.focusIPs)
	launchlog.Info(compGuest, "[%s] ✔ гостьова нода ГОТОВА за %s", gid, total)
	return ts, nil
}

// cacheNodeErr — ЛИШЕ з-під nodeMu.
func (g *GuestService) cacheNodeErr(err error) {
	g.nodeErr = err
	g.nodeErrAt = time.Now()
	launchlog.Warn(compGuest, "помилку підйому гостьової ноди закешовано на %s (наступні спроби до цього часу не б'ють Headscale): %v", nodeRetryAfter, err)
}

// dropNode гасить спільну ноду (Stop сервісу) і прибирає її стан.
func (g *GuestService) dropNode() {
	g.nodeMu.Lock()
	ts := g.node
	dir := g.nodeDir
	stop := g.nodeStop
	g.node = nil
	g.nodeDir = ""
	g.nodeErr = nil
	g.nodeStop = nil
	g.nodeMu.Unlock()
	if stop != nil {
		stop()
	}
	if ts != nil {
		launchlog.Info(compGuest, "гашу гостьову ноду")
		if err := ts.Close(); err != nil {
			launchlog.Warn(compGuest, "ts.Close гостьової ноди: %v", err)
		}
	}
	dropNodeStateDir(dir)
}

// janitorLoop закриває кімнати, які давно без активних з'єднань, щоб
// випадкові пінги й покинуті підключення не тримали слоти пулу вічно.
func (g *GuestService) janitorLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-g.closed:
			return
		case <-ticker.C:
			g.evictIdleRooms()
		}
	}
}

func (g *GuestService) evictIdleRooms() {
	g.roomsMu.Lock()
	var victims []*guestRoom
	for address, r := range g.rooms {
		if idle, noConns := r.idleFor(); noConns && idle > roomIdleTimeout {
			launchlog.Info(compGuest, "кімната %s (%s) без з'єднань %s > %s — закриваю, слот повертається в пул", address, r.localIP, fmtDur(idle), roomIdleTimeout)
			victims = append(victims, r)
			delete(g.rooms, address)
		}
	}
	g.roomsMu.Unlock()
	for _, r := range victims {
		g.closeRoom(r)
	}
}

// evictRoom прибирає кімнату з мапи й гасить її — якщо вона досі та сама. Потрібно, коли
// внутрішній маршрут помер (listener впав, порт занято): інакше «жива» мертва кімната
// перевикористовується для кожного наступного входу, і лікує це лише перезапуск лаунчера.
func (g *GuestService) evictRoom(r *guestRoom) {
	if r == nil {
		return
	}
	g.roomsMu.Lock()
	if cur, ok := g.rooms[r.address]; ok && cur == r {
		delete(g.rooms, r.address)
	}
	g.roomsMu.Unlock()
	g.closeRoom(r)
}

// noteDialResult веде лічильник невдалих діалів до хоста підряд. Після двох поспіль гостьову
// tsnet-ноду перестворюємо (нова реєстрація, чисті мережеві шляхи) — раніше «залипла» нода
// жила до рестарту лаунчера. Не частіше ніж раз на 20 с, щоб не штормити Headscale.
func (g *GuestService) noteDialResult(ok bool, tag string) {
	if ok {
		g.dialFails.Store(0)
		return
	}
	if g.dialFails.Add(1) < 2 {
		return
	}
	now := time.Now().UnixNano()
	last := g.lastNodeReset.Load()
	if last != 0 && time.Duration(now-last) < 20*time.Second {
		return
	}
	if !g.lastNodeReset.CompareAndSwap(last, now) {
		return
	}
	g.dialFails.Store(0)
	launchlog.Warn(compGuest, "[%s] діали до хоста провалились підряд — перестворюю гостьову tsnet-ноду (самовідновлення)", tag)
	g.dropNode()
}

// closeRoom гасить одну кімнату: TCP-listener, її per-room UDP-тунель
// (якщо був піднятий), слот у пулі IP і прибирає її з мапи. tsnet-нода НЕ
// чіпається — вона спільна для всіх кімнат.
func (g *GuestService) closeRoom(r *guestRoom) {
	r.closeOnce.Do(func() {
		launchlog.Debug(compGuest, "закриваю кімнату %s (%s:%s), слот у пулі звільняється", r.address, r.localIP, r.tcpPort)
		close(r.closed)
		_ = r.listener.Close()
		r.udpMu.Lock()
		if r.udpRelay != nil {
			r.udpRelay.Close()
		}
		r.udpRelay = nil
		r.udpMu.Unlock()
		g.pool.release(r.address)
	})
}

// setLastRoom — оновлює "останню активну кімнату" і, якщо хост публікує
// UDP (room.UDPPort), піднімає UDP-вхід на 127.0.0.2:<UDPPort> — тому ж IP,
// що й TCP-вхід (клієнт голосу шле на адресу з домену). Порт береться з
// UDPPort хоста без підмін (за замовчуванням = порт сервера). Вхід
// перекидає пакети в кімнату так само, як TCP-вхід — див. udpInbound.
func (g *GuestService) setLastRoom(r *guestRoom, room RoomInfo) {
	g.roomMu.Lock()
	g.lastRoom = &room
	g.roomMu.Unlock()
	// Гість НІЧОГО не вмикає сам — лише хост вирішує, публікувати UDP-тунель
	// чи ні (галочка при старті хостингу, room.UDPPort).
	if room.UDPPort == "" {
		return
	}
	p, err := strconv.Atoi(room.UDPPort)
	if err != nil || p < 1 || p > 65535 {
		return
	}
	g.ensureUDPInbound(r, p)
}

func (g *GuestService) lastRoomSnapshot() *RoomInfo {
	g.roomMu.Lock()
	defer g.roomMu.Unlock()
	if g.lastRoom == nil {
		return nil
	}
	r := *g.lastRoom
	return &r
}

// ── UDP-тунель для голосового чату ────────────────────────────────────

// ensureUDPInbound піднімає (ідемпотентно) UDP-вхід 127.0.0.2:<port> і
// робить кімнату r «поточною» для цього порту. Порт береться з UDPPort
// хоста без жодних підмін.
func (g *GuestService) ensureUDPInbound(r *guestRoom, port int) {
	in := g.inboundFor(port)
	if in == nil {
		return
	}

	in.mu.Lock()
	if in.preferLocal {
		// Перемкнулись із локального сервера на чужу кімнату: старі закріплення джерел скидаємо.
		in.localSrc = make(map[string]bool)
	}
	in.preferLocal = false
	if in.current != r {
		prev := "—"
		if in.current != nil {
			prev = in.current.address
		}
		launchlog.Info(compGuest, "UDP-вхід порту %d: поточна кімната %s → %s", port, prev, r.address)
	}
	in.current = r
	in.mu.Unlock()

	// relay кімнати потрібен для сесій цієї кімнати.
	r.udpMu.Lock()
	if r.udpRelay == nil {
		r.udpRelay = newUDPRelay().withLabel("guest " + r.address)
	}
	r.tunnelPort = port
	r.udpMu.Unlock()
}

// inboundFor повертає (створює при потребі) UDP-вхід guestExternalIP:<port>.
// nil — порт не вдалося зайняти (помилка вже залогована).
func (g *GuestService) inboundFor(port int) *udpInbound {
	g.udpInMu.Lock()
	defer g.udpInMu.Unlock()
	in := g.udpIn[port]
	if in == nil {
		pc, err := net.ListenPacket("udp", net.JoinHostPort(guestExternalIP, strconv.Itoa(port)))
		if err != nil {
			launchlog.Error(compGuest, "UDP: не вдалося слухати %s:%d: %v  ⇒ %s", guestExternalIP, port, err, explainListenErr(net.JoinHostPort(guestExternalIP, strconv.Itoa(port)), err))
			return nil
		}
		in = &udpInbound{port: port, pc: pc, bySrc: make(map[string]*guestRoom), localSrc: make(map[string]bool), dropGate: newLogGate(10 * time.Second)}
		g.udpIn[port] = in
		{
			_f, _a0 := g.udpInboundLoop, in
			go func() {
				defer crashguard.Recover("g.udpInboundLoop")
				_f(_a0)
			}()
		}
		launchlog.Info(compGuest, "UDP: вхід піднято на %s:%d (як і TCP-вхід, розкидає по кімнатах)", guestExternalIP, port)
	}
	return in
}

// ensureLocalUDP — гравець зайшов на адресу, яку хостить ЦЕЙ ЖЕ лаунчер.
// Голос такого гравця тунелювати не треба: пересилаємо його на локальний
// голосовий порт сервера (127.0.0.1:<порт>). Без цього голос залежав від того,
// що мод слухає 0.0.0.0, і ламався, коли одночасно відкрита чужа кімната з тим
// самим UDP-портом (вхід 127.0.0.2:<порт> перехоплював пакети).
func (g *GuestService) ensureLocalUDP(address, cid string) {
	if g.cfg.LocalHostUDPPort == nil {
		return
	}
	portStr, ok := g.cfg.LocalHostUDPPort(address)
	if !ok {
		return // голос на цьому сервері вимкнено
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return
	}
	in := g.inboundFor(port)
	if in == nil {
		// Порт зайнятий (напр. мод слухає саме на 127.0.0.2) — тоді клієнт і так
		// говорить із модом напряму, тунель не потрібен.
		return
	}
	in.mu.Lock()
	target := net.JoinHostPort("127.0.0.1", portStr)
	if in.localRelay == nil {
		in.localRelay = newUDPRelay().withLabel("local " + address)
	}
	if in.localTarget != target || !in.preferLocal {
		launchlog.Info(compGuest, "[%s] UDP-вхід порту %d: локальний сервер %s → голос напряму на %s", cid, port, address, target)
	}
	if !in.preferLocal {
		// Перемкнулись на локальний сервер: джерела, закріплені за чужою кімнатою, скидаємо.
		in.bySrc = make(map[string]*guestRoom)
	}
	in.localTarget = target
	in.preferLocal = true
	in.mu.Unlock()
}

func (g *GuestService) udpInboundLoop(in *udpInbound) {
	buf := make([]byte, udpDatagramSize)
	for {
		n, src, err := in.pc.ReadFrom(buf)
		if err != nil {
			select {
			case <-g.closed:
				return
			default:
			}
			launchlog.Error(compGuest, "UDP вхід %s:%d перестав читати: %v (голос більше не тунелюється!)", guestExternalIP, in.port, err)
			return
		}
		payload := append([]byte(nil), buf[:n]...)

		key := src.String()
		in.mu.Lock()
		// Локальний сервер: джерело вже закріплене за ним, або це нове джерело
		// і останнім обрано локальний маршрут.
		if in.localTarget != "" && (in.localSrc[key] || (in.bySrc[key] == nil && in.preferLocal)) {
			if !in.localSrc[key] {
				in.localSrc[key] = true
				launchlog.Info(compGuest, "UDP: нове джерело %s на порту %d → локальний сервер %s (перший пакет %d Б)", src, in.port, in.localTarget, n)
			}
			relay, target := in.localRelay, in.localTarget
			in.mu.Unlock()
			relay.forward(in.pc, src, payload, func() (net.Conn, error) {
				return net.DialTimeout("udp", target, 3*time.Second)
			})
			continue
		}
		r := in.bySrc[key]
		newSrc := false
		if r == nil || r.isClosedRoom() {
			r = in.current
			if r != nil {
				in.bySrc[key] = r
				newSrc = true
			}
		}
		in.mu.Unlock()
		if r == nil {
			if ok, skipped := in.dropGate.allow(); ok {
				launchlog.Warn(compGuest, "UDP: пакет від %s відкинуто — немає активної кімнати для порту %d (проковтнуто схожих: %d)", src, in.port, skipped)
			}
			continue
		}
		if newSrc {
			launchlog.Info(compGuest, "UDP: нове джерело %s на порту %d → кімната %s (перший пакет %d Б)", src, in.port, r.address, n)
		}
		g.forwardToRoom(r, in.pc, src, payload)
	}
}

func (r *guestRoom) isClosedRoom() bool {
	select {
	case <-r.closed:
		return true
	default:
		return false
	}
}

// closeUDPInbounds закриває всі UDP-входи (Stop сервісу).
func (g *GuestService) closeUDPInbounds() {
	g.udpInMu.Lock()
	ins := g.udpIn
	g.udpIn = make(map[int]*udpInbound)
	g.udpInMu.Unlock()
	for _, in := range ins {
		_ = in.pc.Close()
		in.mu.Lock()
		if in.localRelay != nil {
			in.localRelay.Close()
		}
		in.mu.Unlock()
	}
}

// ensureRoomTunnel лишено для сумісності викликів; UDP тепер іде через
// спільний вхід 127.0.0.2 (ensureUDPInbound).
func (g *GuestService) ensureRoomTunnel(r *guestRoom, port int) {
	g.ensureUDPInbound(r, port)
}

// forwardToRoom — пакет прийшов на тунельний слухач конкретної кімнати r,
// форвардимо саме в неї (адреса вже однозначна, бо сокет per-room).
func (g *GuestService) forwardToRoom(r *guestRoom, pc net.PacketConn, src net.Addr, payload []byte) {
	room := r.getRoomInfo()
	if room.HostTailscaleIP == "" || room.UDPPort == "" {
		if ok, skipped := udpNoTunnelGate.allow(); ok {
			launchlog.Warn(compGuest, "UDP: пакет від %s відкинуто — хост %s не публікує UDP-тунель (host=%q udpPort=%q, проковтнуто схожих: %d)", src, room.Subdomain, room.HostTailscaleIP, room.UDPPort, skipped)
		}
		return
	}
	r.udpMu.Lock()
	relay := r.udpRelay
	r.udpMu.Unlock()
	if relay == nil {
		return
	}
	relay.forward(pc, src, payload, func() (net.Conn, error) {
		return g.dialVoiceTunnel(room)
	})
}

// dialVoiceTunnel відкриває UDP-з'єднання до хоста через спільну гостьову
// ноду — tailnet IP і порт хоста (room.HostTailscaleIP:room.UDPPort).
func (g *GuestService) dialVoiceTunnel(room RoomInfo) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	ts, err := g.ensureNode(ctx)
	if err != nil {
		launchlog.Error(compGuest, "UDP: гостьова нода недоступна для голосу до %s: %v  ⇒ %s", room.Subdomain, err, explainNodeErr(err))
		return nil, err
	}
	conn, err := ts.Dial(ctx, "udp", net.JoinHostPort(room.HostTailscaleIP, room.UDPPort))
	if err != nil {
		launchlog.Error(compGuest, "UDP: не вдалося відкрити голосовий тунель до хоста %s (%s:%s): %v  ⇒ %s", room.Subdomain, room.HostTailscaleIP, room.UDPPort, err, explainDialErr(err))
		return nil, err
	}
	launchlog.Info(compGuest, "UDP: голосовий тунель до хоста %s (%s:%s) відкрито: %s → %s", room.Subdomain, room.HostTailscaleIP, room.UDPPort, addrOf(conn.LocalAddr()), addrOf(conn.RemoteAddr()))
	return conn, nil
}

// StopUDP більше нічого локально не слухає (запасний спільний слухач
// 127.0.0.1:<port> видалено — голосовий клієнт ходить прямо на IP:порт
// тунелю кімнати). Лишається лише для сумісності з UI: знімає галочку
// статусу. Per-room тунелі тут НЕ чіпаємо — вони гаснуть разом зі своєю
// кімнатою (closeRoom).
func (g *GuestService) StopUDP() error {
	g.udpMu.Lock()
	g.udpEnabled = false
	g.udpMu.Unlock()
	return nil
}

// SetVoicePort — лишається для сумісності з наявним UI (галочка+поле
// порту), але більше не піднімає жодного локального слухача: голос
// тепер тунелюється автоматично разом із кімнатою (setLastRoom →
// ensureRoomTunnel), на IP:порту самого сервера, а не на окремому
// 127.0.0.1:<port>. Тут лише зберігаємо стан галочки для UDPStatus.
func (g *GuestService) SetVoicePort(enabled bool, voicePort int) error {
	if enabled && (voicePort <= 0 || voicePort > 65535) {
		return fmt.Errorf("p2p: некоректний порт голосу: %d", voicePort)
	}
	g.udpMu.Lock()
	g.udpEnabled = enabled
	g.udpMu.Unlock()
	return nil
}

// UDPStatus повертає (увімкнено, порт тунелю) для UI. Порт голосу мода
// (127.0.0.1:X) більше не існує як окрема сутність — гравець ходить
// прямо на порт тунелю кімнати, тому другим значенням повертаємо той
// самий tunnelPort (щоб не ламати сигнатуру виклику в api.go).
func (g *GuestService) UDPStatus() (bool, int, int) {
	g.udpMu.Lock()
	enabled := g.udpEnabled
	g.udpMu.Unlock()

	tunnelPort := 0
	if room := g.lastRoomSnapshot(); room != nil {
		g.roomsMu.Lock()
		r, ok := g.rooms[room.Subdomain]
		g.roomsMu.Unlock()
		if ok {
			r.udpMu.Lock()
			tunnelPort = r.tunnelPort
			r.udpMu.Unlock()
		}
	}
	return enabled, tunnelPort, tunnelPort
}

// Stop зупиняє зовнішній listener, гасить УСІ активні кімнати і спільну
// tsnet-ноду. Викликається при закритті лаунчера.
func (g *GuestService) Stop() error {
	var err error
	g.stopOnce.Do(func() {
		close(g.closed)
		g.mu.Lock()
		if g.listener != nil {
			_ = g.listener.Close()
			g.listener = nil
		}
		g.mu.Unlock()

		g.udpMu.Lock()
		g.udpEnabled = false
		g.udpMu.Unlock()

		g.closeUDPInbounds()
		g.p2pTickets.stopAll()

		g.roomsMu.Lock()
		rooms := g.rooms
		g.rooms = make(map[string]*guestRoom)
		g.roomsMu.Unlock()
		for _, r := range rooms {
			g.closeRoom(r)
		}

		g.dropNode()
	})
	return err
}
