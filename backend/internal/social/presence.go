package social

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand"
	"net/url"
	"shaurma-launcher-backend/internal/crashguard"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// TokenFunc повертає СВІЖИЙ access-JWT (реалізація сама рефрешить, якщо він
// ось-ось спливе). ErrNotLoggedIn означає «сесії немає» — цикл зупиняється.
type TokenFunc func() (string, error)

// PresenceConfig — залежності WS-клієнта.
type PresenceConfig struct {
	URL   string    // wss://social.example/ws — лише wss
	Token TokenFunc // свіжий JWT на кожне підключення
	// HWID — апаратний хеш ПК (sha256 hex). Іде в hello разом із JWT: сервер звіряє його з hb,
	// «запеченим» у токен, і розриває з'єднання, якщо токен вкрадено на інший ПК.
	HWID func() string

	// Зворотні виклики. Викликаються з горутини WS; не блокувати надовго.
	OnPresence func(accountID string, p Presence, known bool) // known=false → друг зник з presence (офлайн)
	OnState    func(ConnState)
	// OnSnapshot — повний стан від VPS одразу після hello (і після кожної зміни
	// друзів/запитів): друзі з ніками й живим статусом, вхідні запити, приватність,
	// лічильники непрочитаного. Саме він замінив опитування Worker.
	OnSnapshot func(Snapshot)

	// Чат — той самий сокет, що й presence (щоб не плодити друге з'єднання
	// й не витрачати ресурси VPS). Вміст кадрів зашифрований (E2E), тут лише
	// транспорт.
	OnMessage             func(IncomingMessage)
	OnMessageAck          func(MessageAck)
	OnMessageFailed       func(clientMessageID, code string) // сервер явно відмовив у відправці
	OnTyping              func(TypingEvent)
	OnRead                func(ReadEvent)
	OnServerInvite        func(ServerInviteEvent)
	OnServerInviteRevoked func(inviteID string)
	// OnP2PHostRevoked — власник P2P-кімнати забрав у співхоста роль хоста: тунель треба зупинити
	// негайно, не чекаючи 403 на heartbeat (VPS шле кадр p2p_host_revoked на всі пристрої акаунта).
	OnP2PHostRevoked   func(P2PHostRevokedEvent)
	OnMessageDeleted   func(DeletedEvent)   // повідомлення видалено «для всіх»
	OnMessageEdited    func(EditedEvent)    // автор змінив текст повідомлення
	OnPinsUpdated      func(PinsEvent)      // змінився список закріплених
	OnReactionsUpdated func(ReactionsEvent) // змінились реакції одного повідомлення

	// Кілька пристроїв (SERVER → цей клієнт).
	OnDeviceRevoked  func(reason string)    // ЦЕЙ пристрій вилучено з акаунта (після цього — розрив)
	OnDeviceAdded    func(DeviceAddedEvent) // до акаунта додано інший пристрій («Це не ви?»)
	OnDevicesChanged func()                 // склад пристроїв змінився — оновіть екран «Пристрої»
	OnReadElsewhere  func(ReadEvent)        // діалог прочитано на іншому моєму пристрої

	// Віддалене керування серверами (remote.go).
	OnRemoteSnapshot func([]RemoteOwnerList) // повний перелік серверів друзів, доступних мені
	OnRemoteList     func(RemoteOwnerList)   // оновлення одного друга
	OnRemoteCmd      func(RemoteIncoming)    // мені (власнику) прийшла E2E-команда
	OnRemoteResult   func(RemoteResultFrame) // відповідь на мою команду
	// Консоль друга: шматок потоку / закриття сесії / публічний ключ VPS для перевірки мандатів.
	OnRemoteStream     func(RemoteStreamFrame)
	OnRemoteStreamEnd  func(sid, reason string)
	OnRemoteKey        func(pubKeyB64 string)
	OnRemoteP2PMandate func(P2PMandateFrame)
	OnRemotePipe       func(PipeFrame)
	OnRemotePipeEnd    func(tunnel, reason string)

	Logf func(format string, args ...any) // за замовчуванням — нічого (токени/вміст не логуються)

	// Лише для тестів: дозволити ws:// (httptest не вміє TLS без зусиль).
	allowInsecure bool
}

// PresenceClient — WS-клієнт presence. Безпечний для виклику з багатьох горутин.
type PresenceClient struct {
	cfg PresenceConfig

	mu      sync.Mutex
	self    string // мій accountId (з JWT)
	friends map[string]struct{}
	live    map[string]Presence // лише ті, хто не offline
	// pending — presence від акаунтів, яких ще немає у списку друзів. VPS шле
	// знімок одразу після hello, а список друзів із Worker може ще вантажитись.
	// Подія НЕ застосовується, доки акаунт справді не з'явиться в SetFriends,
	// тож сторонній акаунт так і лишається проігнорованим. Обмежено за
	// кількістю й часом — пам'ять не росте від зловмисного потоку.
	pending map[string]Presence
	state   ConnState

	// «Бажаний» власний статус. Після кожного (ре)конекту шлемо його знову —
	// VPS тримає presence в пам'яті й після рестарту нічого не пам'ятає.
	wantStatus string
	wantPack   string
	wantColor  string
	// invisible — ручний режим «Невидимий»: друзям шлемо left_game (як ShowStatusTo=nobody),
	// чат і решта функцій працюють. Після кожного (ре)конекту застосовується знову.
	invisible bool
	policy    Privacy
	dirty     bool

	cancel context.CancelFunc

	// Відповіді на history: reqId → канал, у який readLoop кладе сторінку.
	histMu  sync.Mutex
	histReq map[string]chan histResult
	// Відповіді op_result (видалити / закріпити): reqId → канал із кодом помилки ("" = ок).
	opReq map[string]chan string

	// Публікація моїх серверів друзям (remote.go); під p.mu.
	rshares remoteShares

	done chan struct{}
	kick chan struct{}

	// Черга вихідних чат-кадрів. Окремо від статусу (kick/dirty), бо
	// повідомлення НЕ можна загубити/перезаписати, як статус: кожен кадр
	// черги має бути відправлений. Обмежена за розміром (fail, не рости
	// безмежно), сигнал — outSignal (щоб writeLoop прокинувся).
	outMu     sync.Mutex
	out       [][]byte
	outSignal chan struct{}
}

// hwid — HWID для hello (порожній, якщо не задано: сервер такого клієнта не пустить).
func (p *PresenceClient) hwid() string {
	if p.cfg.HWID == nil {
		return ""
	}
	return p.cfg.HWID()
}

func NewPresenceClient(cfg PresenceConfig) *PresenceClient {
	if cfg.Logf == nil {
		cfg.Logf = func(string, ...any) {}
	}
	return &PresenceClient{
		cfg:        cfg,
		friends:    map[string]struct{}{},
		live:       map[string]Presence{},
		pending:    map[string]Presence{},
		state:      ConnOff,
		wantStatus: StatusOnline,
		policy:     DefaultPrivacy(),
		kick:       make(chan struct{}, 1),
		outSignal:  make(chan struct{}, 1),
		histReq:    map[string]chan histResult{},
		opReq:      map[string]chan string{},
		rshares:    newRemoteShares(),
	}
}

// checkURL — єдине місце, де вирішується, чи можна ходити на цю адресу.
func (p *PresenceClient) checkURL() error {
	u, err := url.Parse(p.cfg.URL)
	if err != nil || u.Host == "" {
		return errors.New("social: bad ws url")
	}
	if u.Scheme != "wss" && !(p.cfg.allowInsecure && u.Scheme == "ws") {
		return errors.New("social: ws url must be wss://")
	}
	return nil
}

// Start запускає цикл підключення. Повторний виклик без Stop — no-op.
func (p *PresenceClient) Start() error {
	if err := p.checkURL(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cancel != nil {
		select {
		case <-p.done:
			// Цикл сам завершився (напр. сесії не було) — можна стартувати знову.
		default:
			return nil
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	p.done = make(chan struct{})
	{
		_f, _a0, _a1 := p.run, ctx, p.done
		go func() {
			defer crashguard.Recover("p.run")
			_f(_a0, _a1)
		}()
	}
	return nil
}

// Stop зупиняє цикл, чистить стан у пам'яті й чекає завершення горутини.
func (p *PresenceClient) Stop() {
	p.mu.Lock()
	cancel, done := p.cancel, p.done
	p.cancel, p.done = nil, nil
	p.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	<-done
	p.mu.Lock()
	p.live = map[string]Presence{}
	p.pending = map[string]Presence{}
	p.mu.Unlock()
	// Скидаємо чергу: після виходу з акаунта не має лишитись жодного
	// ненадісланого кадру, який пішов би під чужою сесією.
	p.outMu.Lock()
	p.out = nil
	p.outMu.Unlock()
	p.setState(ConnOff)
}

// SetFriends задає білий список акаунтів, presence яких ми приймаємо.
// Тих, кого зі списку прибрали (видалений друг), одразу вилучає з live.
func (p *PresenceClient) SetFriends(ids []string) {
	set := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if ValidAccountID(id) && len(set) < MaxFriends {
			set[id] = struct{}{}
		}
	}
	var removed []string
	type applied struct {
		id string
		pr Presence
	}
	var promoted []applied
	p.mu.Lock()
	p.friends = set
	for id := range p.live {
		if _, ok := set[id]; !ok {
			delete(p.live, id)
			removed = append(removed, id)
		}
	}
	cut := time.Now().Add(-pendingTTL).UnixMilli()
	for id, pr := range p.pending {
		if pr.UpdatedAt < cut {
			delete(p.pending, id)
			continue
		}
		if _, ok := set[id]; ok {
			delete(p.pending, id)
			p.live[id] = pr
			promoted = append(promoted, applied{id, pr})
		}
	}
	p.mu.Unlock()
	for _, id := range removed {
		if p.cfg.OnPresence != nil {
			p.cfg.OnPresence(id, Presence{Status: StatusOffline}, false)
		}
	}
	for _, a := range promoted {
		if p.cfg.OnPresence != nil {
			p.cfg.OnPresence(a.id, a.pr, true)
		}
	}
}

// Snapshot — копія поточного живого стану друзів.
func (p *PresenceClient) Snapshot() map[string]Presence {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make(map[string]Presence, len(p.live))
	for k, v := range p.live {
		out[k] = v
	}
	return out
}

// State — поточний стан з'єднання.
func (p *PresenceClient) State() ConnState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state
}

// SetPolicy — приватність гравця. Це захист у глибину: VPS і так фільтрує за
// presence_prefs, але ми не відправляємо зайвого й самі.
//
// ОКРІМ того, шлемо кадр set_privacy: хаб тримає приватність у пам'яті й
// раніше оновлював її лише при реконекті — через це зміна в UI «не
// працювала» (друзі бачили людину онлайн після «Приховати статус»).
func (p *PresenceClient) SetPolicy(pol Privacy) {
	pol = NormalizePrivacy(pol)
	p.mu.Lock()
	changed := pol != p.policy
	p.policy = pol
	p.dirty = true
	p.mu.Unlock()
	if changed {
		b, _ := json.Marshal(map[string]string{
			"type":           "set_privacy",
			"showStatusTo":   pol.ShowStatusTo,
			"showServerTo":   pol.ShowServerTo,
			"friendRequests": pol.FriendRequest,
		})
		// Кадр кладеться в загальну чергу (вона має пріоритет над статусом).
		// Якщо черга повна — не страшно: VPS підхопить нові значення з БД
		// при наступному підключенні (HTTP-збереження вже зроблене).
		p.enqueue(b)
	}
	p.poke()
}

// SetPolicyLocal — те саме, що SetPolicy, але БЕЗ кадру set_privacy.
//
// Для значень, які НЕ є вибором користувача: безпечний дефолт на старті
// (showServerTo=nobody, поки справжні налаштування не прочитано) і копія
// приватності, яку щойно віддав сам сервер (snapshot / HTTP). Раніше їх
// теж слали кадром set_privacy, і хаб приймав їх як зміну користувача:
// дефолт «nobody» перетирав у пам'яті хабу справжнє «friends», сервер
// слав свіжий snapshot, UI показував «nobody», далі приходило справжнє
// значення — і перемикач «Збірка, у яку я граю» скакав друзі ↔ ніхто на
// кожному старті/оновленні. Клієнтська фільтрація статусу (p.policy)
// працює так само, як і в SetPolicy.
func (p *PresenceClient) SetPolicyLocal(pol Privacy) {
	pol = NormalizePrivacy(pol)
	p.mu.Lock()
	p.policy = pol
	p.dirty = true
	p.mu.Unlock()
	p.poke()
}

// SetStatus — власний статус: «в мережі» або «у грі» (назва збірки й колір).
func (p *PresenceClient) SetStatus(status, packName, packColor string) {
	status = SanitizeStatus(status)
	if status != StatusInGame {
		if status != StatusAway {
			status = StatusOnline
		}
		packName, packColor = "", ""
	}
	p.mu.Lock()
	p.wantStatus = status
	p.wantPack = CleanText(packName, MaxPackNameLen)
	p.wantColor = SanitizeColor(packColor)
	p.dirty = true
	p.mu.Unlock()
	p.poke()
}

// SetInvisible вмикає/вимикає режим «Невидимий» і негайно шле оновлений кадр.
func (p *PresenceClient) SetInvisible(v bool) {
	if p == nil {
		return
	}
	p.mu.Lock()
	changed := p.invisible != v
	p.invisible = v
	p.dirty = true
	p.mu.Unlock()
	if changed {
		p.poke()
	}
}

// Invisible — чи ввімкнено «Невидимий».
func (p *PresenceClient) Invisible() bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.invisible
}

// Status — поточний ВЛАСНИЙ статус ("online" | "away" | "in_game"), той
// самий, що шлеться на VPS у кадрі set_status. Потрібен віджету акаунта в
// sidebar: там підпис статусу має відображати РЕАЛЬНИЙ стан (у грі / відійшов
// / у мережі), а не статичний «В мережі». Порожній статус (клієнта ще немає) —
// StatusOffline: показувати «в мережі» до підключення було б неправдою.
func (p *PresenceClient) Status() string {
	if p == nil {
		return StatusOffline
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.wantStatus == "" {
		return StatusOffline
	}
	return p.wantStatus
}

func (p *PresenceClient) poke() {
	select {
	case p.kick <- struct{}{}:
	default:
	}
}

// maxOutboundQueue — скільки кадрів чату може чекати відправки. Переповнення
// → помилка виклику (краще чесно сказати «не надіслано», ніж мовчки з'їсти
// повідомлення під час відновлення з'єднання).
const maxOutboundQueue = 64

// enqueue ставить кадр у чергу на відправку. false — черга повна.
func (p *PresenceClient) enqueue(frame []byte) bool {
	p.outMu.Lock()
	if len(p.out) >= maxOutboundQueue {
		p.outMu.Unlock()
		return false
	}
	p.out = append(p.out, frame)
	p.outMu.Unlock()
	select {
	case p.outSignal <- struct{}{}:
	default:
	}
	return true
}

// takeOut повертає наступний кадр черги або nil.
func (p *PresenceClient) takeOut() []byte {
	p.outMu.Lock()
	defer p.outMu.Unlock()
	if len(p.out) == 0 {
		return nil
	}
	b := p.out[0]
	p.out = p.out[1:]
	return b
}

// SendChatMessage ставить зашифроване повідомлення в чергу. ciphertext/nonce
// уже готові (EncryptFor) — тут лише транспорт.
func (p *PresenceClient) SendChatMessage(chatID, ciphertext, nonce, clientMsgID string) error {
	return p.SendChatMessageReply(chatID, ciphertext, nonce, clientMsgID, 0)
}

// SendChatMessageReply — те саме, але з відповіддю на повідомлення
// (replyToID; 0 = звичайне повідомлення без цитати).
func (p *PresenceClient) SendChatMessageReply(chatID, ciphertext, nonce, clientMsgID string, replyToID int64) error {
	if _, err := splitChatID(chatID); err != nil {
		return ErrBadInput
	}
	if ciphertext == "" || len(ciphertext) > maxCiphertextB64Len || nonce == "" || len(nonce) > 40 {
		return ErrBadInput
	}
	if clientMsgID != "" && !validClientMsgID(clientMsgID) {
		return ErrBadInput
	}
	if replyToID < 0 {
		return ErrBadInput
	}
	frame := map[string]any{
		"type": "send_message", "chatId": chatID,
		"bodyType": "text", "ciphertext": ciphertext, "nonce": nonce,
		"clientMessageId": clientMsgID,
	}
	if replyToID > 0 {
		frame["replyToId"] = replyToID
	}
	b, _ := json.Marshal(frame)
	if !p.enqueue(b) {
		return ErrQueueFull
	}
	p.poke()
	return nil
}

// ReactMessage — реакція на повідомлення (один емодзі на акаунт, Telegram-поведінка).
// Порожній emoji = прибрати свою реакцію. Чекає op_result від сервера.
func (p *PresenceClient) ReactMessage(chatID string, messageID int64, emoji string) error {
	return p.RequestOp("react_message", chatID, messageID, map[string]any{"emoji": CleanText(emoji, 64)})
}

// SendTyping — індикатор друку (VPS сам дроселює 1/3с; ми не спамимо більше).
func (p *PresenceClient) SendTyping(chatID string) error {
	if _, err := splitChatID(chatID); err != nil {
		return ErrBadInput
	}
	frame, _ := json.Marshal(map[string]string{"type": "typing", "chatId": chatID})
	if !p.enqueue(frame) {
		return ErrQueueFull
	}
	p.poke()
	return nil
}

// SendMarkRead — позначити прочитаним до id (лише якщо в отримувача увімкнено).
func (p *PresenceClient) SendMarkRead(chatID string, upToMessageID int64) error {
	if _, err := splitChatID(chatID); err != nil || upToMessageID <= 0 {
		return ErrBadInput
	}
	frame, _ := json.Marshal(map[string]any{"type": "mark_read", "chatId": chatID, "upToMessageId": upToMessageID})
	if !p.enqueue(frame) {
		return ErrQueueFull
	}
	p.poke()
	return nil
}

// ── Знімок стану від VPS ────────────────────────────────────────────────

// Snapshot — санітизований friends_snapshot.
type Snapshot struct {
	Friends []Friend
	Invites []Invite
	Unread  []UnreadChat
	Privacy Privacy
}

// UnreadChat — лічильник непрочитаного в діалозі (chatId).
type UnreadChat struct {
	ChatID string
	Count  int
	LastID int64
	LastTS int64
}

type snapshotWire struct {
	Friends []struct {
		AccountID string `json:"accountId"`
		Nickname  string `json:"nickname"`
		Handle    string `json:"handle"`
		Since     int64  `json:"since"`
		Bio       string `json:"bio"`
		Color     string `json:"color"`
		Status    string `json:"status"`
		LastSeen  int64  `json:"lastSeen"`
		PackName  string `json:"packName"`
		PackColor string `json:"packColor"`
	} `json:"friends"`
	Invites []struct {
		ID            string `json:"id"`
		FromAccountID string `json:"fromAccountId"`
		FromNickname  string `json:"fromNickname"`
		FromHandle    string `json:"fromHandle"`
		CreatedAt     int64  `json:"createdAt"`
	} `json:"invites"`
	Unread []struct {
		ChatID string `json:"chatId"`
		Count  int    `json:"count"`
		LastID int64  `json:"lastId"`
		LastTS int64  `json:"lastTs"`
	} `json:"unread"`
	Privacy struct {
		ShowStatusTo   string `json:"showStatusTo"`
		ShowServerTo   string `json:"showServerTo"`
		FriendRequests string `json:"friendRequests"`
		WhoCanMessage  string `json:"whoCanMessage"`
	} `json:"privacy"`
}

// sanitizeLastSeen — «востаннє в мережі» від сервера: 0, якщо значення
// нерозумне (від'ємне або з майбутнього далі ніж на хвилину — розсинхрон
// годинників). Сервер — не довірена сторона для UI: нахабне значення не
// повинно малювати «через 3 роки».
func sanitizeLastSeen(v, nowMs int64) int64 {
	if v <= 0 || v > nowMs+60_000 {
		return 0
	}
	return v
}

// handleSnapshot — ГОЛОВНИЙ фікс «статус не в реальному часі»: раніше цей кадр
// ігнорувався, тож друзі, які вже були онлайн до нашого підключення, показувались
// офлайн, доки самі не змінять статус. Тут же беремо й список друзів, запити та
// лічильники — Worker для цього більше не опитується.
func (p *PresenceClient) handleSnapshot(data []byte) {
	var w snapshotWire
	if json.Unmarshal(data, &w) != nil {
		return
	}
	now := time.Now().UnixMilli()
	snap := Snapshot{Friends: []Friend{}, Invites: []Invite{}, Unread: []UnreadChat{}}
	friends := map[string]struct{}{}
	live := map[string]Presence{}
	for _, f := range w.Friends {
		if !ValidAccountID(f.AccountID) || len(friends) >= MaxFriends {
			continue
		}
		if _, dup := friends[f.AccountID]; dup {
			continue
		}
		friends[f.AccountID] = struct{}{}
		nick := CleanText(f.Nickname, MaxNicknameLen)
		if nick == "" {
			nick = "?"
		}
		pr := Presence{Status: StatusOffline, UpdatedAt: now, LastSeenAt: sanitizeLastSeen(f.LastSeen, now)}
		if st := SanitizeStatus(f.Status); st != "" && st != StatusOffline {
			pr.Status = st
			if st == StatusInGame {
				pr.PackName = CleanText(f.PackName, MaxPackNameLen)
				pr.PackColor = SanitizeColor(f.PackColor)
			}
			live[f.AccountID] = pr
		}
		snap.Friends = append(snap.Friends, Friend{AccountID: f.AccountID, Nickname: nick, Handle: SanitizeHandle(f.Handle), Bio: SanitizeBio(f.Bio), Color: SanitizeColor(f.Color), Since: f.Since, Presence: pr})
	}
	for _, i := range w.Invites {
		if !ValidInviteID(i.ID) || !ValidAccountID(i.FromAccountID) || len(snap.Invites) >= 100 {
			continue
		}
		nick := CleanText(i.FromNickname, MaxNicknameLen)
		if nick == "" {
			nick = "?"
		}
		snap.Invites = append(snap.Invites, Invite{ID: i.ID, FromAccountID: i.FromAccountID, FromNickname: nick, FromHandle: SanitizeHandle(i.FromHandle), CreatedAt: i.CreatedAt})
	}
	for _, u := range w.Unread {
		if _, err := splitChatID(u.ChatID); err != nil || u.Count <= 0 || len(snap.Unread) >= MaxFriends {
			continue
		}
		snap.Unread = append(snap.Unread, UnreadChat{ChatID: u.ChatID, Count: u.Count, LastID: u.LastID, LastTS: u.LastTS})
	}
	snap.Privacy = NormalizePrivacy(Privacy{ShowStatusTo: w.Privacy.ShowStatusTo, ShowServerTo: w.Privacy.ShowServerTo, FriendRequest: w.Privacy.FriendRequests, WhoCanMessage: w.Privacy.WhoCanMessage})

	// Білий список друзів і живий стан міняються РАЗОМ, до виклику Service —
	// інакше presence-подія між двома кроками була б відкинута як «не друг».
	p.mu.Lock()
	p.friends = friends
	p.live = live
	p.pending = map[string]Presence{}
	p.mu.Unlock()
	if p.cfg.OnSnapshot != nil {
		p.cfg.OnSnapshot(snap)
	}
}

// ── Історія через WS ─────────────────────────────────────────────────────

type histResult struct {
	page ChatPage
	err  error
}

// RequestHistory просить VPS сторінку діалогу (з його пам'яті; Worker VPS
// смикає сам і лише при першому відкритті). Повертає ЗАШИФРОВАНІ повідомлення.
func (p *PresenceClient) RequestHistory(chatID string, before, after int64, limit int) (ChatPage, error) {
	if _, err := splitChatID(chatID); err != nil {
		return ChatPage{}, ErrBadInput
	}
	if p.State() != ConnOnline {
		return ChatPage{}, ErrNotConnected
	}
	id := randomClientMsgID()
	if id == "" {
		return ChatPage{}, ErrNotConnected
	}
	ch := make(chan histResult, 1)
	p.histMu.Lock()
	p.histReq[id] = ch
	p.histMu.Unlock()
	defer func() { p.histMu.Lock(); delete(p.histReq, id); p.histMu.Unlock() }()

	req := map[string]any{"type": "history", "reqId": id, "chatId": chatID, "before": before, "limit": limit}
	if after > 0 {
		req["after"] = after
	}
	frame, _ := json.Marshal(req)
	if !p.enqueue(frame) {
		return ChatPage{}, ErrQueueFull
	}
	p.poke()
	select {
	case r := <-ch:
		return r.page, r.err
	case <-time.After(historyTimeout):
		return ChatPage{}, ErrNotConnected
	}
}

func (p *PresenceClient) resolveHistory(reqID string, r histResult) {
	p.histMu.Lock()
	ch := p.histReq[reqID]
	p.histMu.Unlock()
	if ch != nil {
		select {
		case ch <- r:
		default:
		}
	}
}

type histWireInvite struct {
	ID         string `json:"id"`
	ServerAddr string `json:"serverAddr"`
	PackID     string `json:"packId"`
	PackName   string `json:"packName"`
	ExpiresAt  int64  `json:"expiresAt"`
	Revoked    bool   `json:"revoked"`
	Active     bool   `json:"active"`
}

type histWireMsg struct {
	ID         int64           `json:"id"`
	From       string          `json:"from"`
	BodyType   string          `json:"bodyType"`
	Ciphertext string          `json:"ciphertext"`
	Nonce      string          `json:"nonce"`
	TS         int64           `json:"ts"`
	Invite     *histWireInvite `json:"invite"`
	ReplyToID  int64           `json:"replyToId"`
	EditedAt   int64           `json:"editedAt"`
	Reactions  []histReaction  `json:"reactions"`
}

type histReaction struct {
	By    string `json:"by"`
	Emoji string `json:"emoji"`
}

type histWire struct {
	ReqID    string        `json:"reqId"`
	ChatID   string        `json:"chatId"`
	Code     string        `json:"code"`
	HasMore  bool          `json:"hasMore"`
	Messages []histWireMsg `json:"messages"`
	Pinned   []histWireMsg `json:"pinned"`
	Reads    []struct {
		By   string `json:"by"`
		UpTo int64  `json:"upTo"`
	} `json:"reads"`
}

// wireToChat перетворює кадрові повідомлення в ChatMessage (ще зашифровані). Чужі/некоректні
// відправники відкидаються.
func wireToChat(in []histWireMsg) []ChatMessage {
	out := make([]ChatMessage, 0, len(in))
	for _, m := range in {
		if !ValidAccountID(m.From) {
			continue
		}
		bt := "text"
		if m.BodyType == "server_invite" {
			bt = "server_invite"
		}
		msg := ChatMessage{ID: m.ID, From: m.From, BodyType: bt, Ciphertext: m.Ciphertext, Nonce: m.Nonce, CreatedAt: m.TS, ReplyToID: m.ReplyToID, EditedAt: m.EditedAt}
		for _, rw := range m.Reactions {
			if ValidAccountID(rw.By) && rw.Emoji != "" {
				msg.Reactions = append(msg.Reactions, Reaction{By: rw.By, Emoji: CleanText(rw.Emoji, 64)})
			}
		}
		if m.Invite != nil {
			msg.Invite = &ServerInvite{
				ID: m.Invite.ID, ServerAddr: m.Invite.ServerAddr,
				PackID: CleanText(m.Invite.PackID, 64), PackName: CleanText(m.Invite.PackName, 64),
				ExpiresAt: m.Invite.ExpiresAt, Revoked: m.Invite.Revoked, Active: m.Invite.Active,
			}
		}
		out = append(out, msg)
	}
	return out
}

func (p *PresenceClient) handleHistoryPage(data []byte) {
	var w histWire
	if json.Unmarshal(data, &w) != nil || w.ReqID == "" {
		return
	}
	page := ChatPage{Messages: wireToChat(w.Messages), HasMore: w.HasMore, Pinned: wireToChat(w.Pinned)}
	for _, r := range w.Reads {
		if ValidAccountID(r.By) && r.UpTo > 0 {
			page.Reads = append(page.Reads, ReadMark{By: r.By, UpTo: r.UpTo})
		}
	}
	p.resolveHistory(w.ReqID, histResult{page: page})
}

// RequestOp відправляє кадр-операцію (delete_message / pin_message) і чекає op_result.
// Повертає код помилки сервера в errors (forbidden, not_found, too_many_pins, ...).
func (p *PresenceClient) RequestOp(frameType, chatID string, messageID int64, extra map[string]any) error {
	if _, err := splitChatID(chatID); err != nil || messageID <= 0 {
		return ErrBadInput
	}
	if p.State() != ConnOnline {
		return ErrNotConnected
	}
	id := randomClientMsgID()
	if id == "" {
		return ErrNotConnected
	}
	ch := make(chan string, 1)
	p.histMu.Lock()
	p.opReq[id] = ch
	p.histMu.Unlock()
	defer func() { p.histMu.Lock(); delete(p.opReq, id); p.histMu.Unlock() }()

	req := map[string]any{"type": frameType, "reqId": id, "chatId": chatID, "messageId": messageID}
	for k, v := range extra {
		req[k] = v
	}
	frame, _ := json.Marshal(req)
	if !p.enqueue(frame) {
		return ErrQueueFull
	}
	p.poke()
	select {
	case code := <-ch:
		if code == "" {
			return nil
		}
		return opError(code)
	case <-time.After(historyTimeout):
		return ErrNotConnected
	}
}

func (p *PresenceClient) resolveOp(reqID, code string) {
	p.histMu.Lock()
	ch := p.opReq[reqID]
	p.histMu.Unlock()
	if ch != nil {
		select {
		case ch <- code:
		default:
		}
	}
}

// opError — код відмови сервера у вигляді помилки «social:<code>» (фронтенд перекладає за кодом).
func opError(code string) error {
	code = strings.ToLower(CleanText(code, 32))
	if code == "" {
		code = "unknown"
	}
	return errors.New("social:" + code)
}

// SendServerInvite — запрошення «Зайти на сервер». serverAddr валідується
// локально (дзеркало validServerAddr Worker'а), щоб у кадр не потрапив
// довільний рядок.
func (p *PresenceClient) SendServerInvite(chatID, serverAddr, packID, packName string, ttlMs int64) error {
	if _, err := splitChatID(chatID); err != nil {
		return ErrBadInput
	}
	addr, ok := sanitizeServerAddr(serverAddr)
	if !ok {
		return ErrBadInput
	}
	frame, _ := json.Marshal(map[string]any{
		"type": "invite_to_server", "chatId": chatID, "serverAddr": addr,
		"packId": CleanText(packID, 64), "packName": CleanText(packName, 64),
		"ttlMs": ttlMs,
	})
	if !p.enqueue(frame) {
		return ErrQueueFull
	}
	p.poke()
	return nil
}

func (p *PresenceClient) setState(s ConnState) {
	p.mu.Lock()
	changed := p.state != s
	p.state = s
	p.mu.Unlock()
	if changed && p.cfg.OnState != nil {
		p.cfg.OnState(s)
	}
}

// ── Цикл підключення ────────────────────────────────────────────────────

func (p *PresenceClient) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	backoff := backoffMin
	for ctx.Err() == nil {
		p.setState(ConnConnecting)
		started := time.Now()
		err := p.session(ctx)
		if ctx.Err() != nil {
			return
		}
		if errors.Is(err, ErrNotLoggedIn) || errors.Is(err, ErrDeviceRevoked) {
			// Немає сесії / пристрій вилучено — крутитись марно. Зупиняємось; хост-код запустить
			// нас знову після входу.
			p.setState(ConnOff)
			return
		}
		p.setState(ConnConnecting)
		// Жива сесія довше за хвилину → наступний збій починає з малого кроку.
		if time.Since(started) > 30*time.Second {
			backoff = backoffMin
		}
		// Jitter, щоб після рестарту VPS тисячі клієнтів не вдарили одночасно.
		wait := backoff/2 + time.Duration(rand.Int63n(int64(backoff/2)+1))
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		if backoff *= 2; backoff > backoffMax {
			backoff = backoffMax
		}
	}
}

func (p *PresenceClient) session(ctx context.Context) error {
	token, err := p.cfg.Token()
	if err != nil {
		return err
	}
	p.setSelf(AccountIDFromJWT(token))
	dctx, dcancel := context.WithTimeout(ctx, wsDialTimeout)
	conn, _, err := websocket.Dial(dctx, p.cfg.URL, &websocket.DialOptions{
		// Стискання вимкнено: кадри крихітні, а CRIME-подібні атаки на
		// стиснення з токеном у потоці нам не потрібні.
		CompressionMode: websocket.CompressionDisabled,
	})
	dcancel()
	if err != nil {
		p.cfg.Logf("social: dial failed")
		return err
	}
	conn.SetReadLimit(maxWSFrame)
	defer conn.CloseNow()

	// JWT — ПЕРШИЙ кадр, а не частина URL: URL осідає в логах проксі/Cloudflare.
	hello, _ := json.Marshal(map[string]string{"type": "hello", "jwt": token, "hwid": p.hwid()})
	if err := p.write(ctx, conn, hello); err != nil {
		return err
	}
	p.setState(ConnOnline)

	sctx, scancel := context.WithCancel(ctx)
	defer scancel()
	errc := make(chan error, 2)

	go func() { errc <- p.readLoop(sctx, conn) }()
	go func() { errc <- p.writeLoop(sctx, conn) }()

	err = <-errc
	scancel()
	_ = conn.Close(websocket.StatusNormalClosure, "")
	<-errc
	return err
}

func (p *PresenceClient) write(ctx context.Context, conn *websocket.Conn, b []byte) error {
	wctx, cancel := context.WithTimeout(ctx, wsWriteTimeout)
	defer cancel()
	return conn.Write(wctx, websocket.MessageText, b)
}

// rateWait чекає, доки token-bucket дозволить відправку. Повертає false, якщо
// контекст скасовано (тоді writeLoop завершується).
func (p *PresenceClient) rateWait(ctx context.Context, allow func() bool) bool {
	for !allow() {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(200 * time.Millisecond):
		}
	}
	return true
}

func (p *PresenceClient) writeLoop(ctx context.Context, conn *websocket.Conn) error {
	ping := time.NewTicker(wsPingEvery)
	defer ping.Stop()
	// Оновлення токена БЕЗ перепідключення: VPS розриває сесію в мить спливання
	// access-JWT (15 хв). Свіжий hello того ж акаунта продовжує її.
	reauth := time.NewTicker(reauthEvery)
	defer reauth.Stop()
	// Токен-бакет на вихідні: 5 повідомлень/с — ліміт VPS (план 1.5.4).
	var sent []time.Time
	allow := func() bool {
		now := time.Now()
		cut := now.Add(-time.Second)
		kept := sent[:0]
		for _, t := range sent {
			if t.After(cut) {
				kept = append(kept, t)
			}
		}
		sent = kept
		if len(sent) >= maxOutboundPerSec {
			return false
		}
		sent = append(sent, now)
		return true
	}
	p.mu.Lock()
	p.dirty = true // після (ре)конекту завжди повторюємо власний статус
	p.mu.Unlock()
	p.resyncShares() // VPS нічого не пам'ятає між сесіями: повторюємо публікацію серверів
	for {
		// Пріоритет — чат-кадри: на відміну від статусу, їх не можна
		// «перезаписати», кожен мусить піти.
		if msg := p.takeOut(); msg != nil {
			if !p.rateWait(ctx, allow) {
				return ctx.Err()
			}
			if err := p.write(ctx, conn, msg); err != nil {
				return err
			}
			continue
		}
		// Публікація моїх серверів (стан бажаний, а не черга: можна «склеювати»).
		if msg := p.takeShare(); msg != nil {
			if !p.rateWait(ctx, allow) {
				return ctx.Err()
			}
			if err := p.write(ctx, conn, msg); err != nil {
				return err
			}
			continue
		}
		// Потім — статус присутності (може бути замінений на новіший).
		if msg := p.takePending(); msg != nil {
			if !p.rateWait(ctx, allow) {
				return ctx.Err()
			}
			if err := p.write(ctx, conn, msg); err != nil {
				return err
			}
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-p.kick:
		case <-p.outSignal:
		case <-ping.C:
			pctx, cancel := context.WithTimeout(ctx, wsWriteTimeout)
			err := conn.Ping(pctx)
			cancel()
			if err != nil {
				return err
			}
		case <-reauth.C:
			tok, terr := p.cfg.Token()
			if terr != nil {
				return terr // сесії більше немає — run() зупиниться на ErrNotLoggedIn
			}
			hello, _ := json.Marshal(map[string]string{"type": "hello", "jwt": tok, "hwid": p.hwid()})
			if err := p.write(ctx, conn, hello); err != nil {
				return err
			}
		}
	}
}

// takePending повертає наступне вихідне повідомлення або nil.
func (p *PresenceClient) takePending() []byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.dirty {
		return nil
	}
	p.dirty = false
	// Статус бачить лише той, кому гравець дозволив.
	if p.policy.ShowStatusTo == "nobody" || p.invisible {
		b, _ := json.Marshal(map[string]string{"type": "left_game"})
		return b
	}
	m := map[string]any{"type": "set_status", "status": p.wantStatus, "dropInOpen": false, "serverAddr": ""}
	if p.wantStatus == StatusInGame && p.policy.ShowServerTo != "nobody" {
		m["packName"] = p.wantPack
		m["packColor"] = p.wantColor
	} else {
		m["packName"], m["packColor"] = "", ""
		if p.wantStatus == StatusInGame {
			m["status"] = StatusOnline // «у грі» без деталей = просто «в мережі»
		}
	}
	b, _ := json.Marshal(m)
	return b
}

// DeviceAddedEvent — повідомлення «до вашого акаунта додано пристрій».
type DeviceAddedEvent struct {
	DeviceID   string `json:"deviceId"`
	Name       string `json:"name"`
	ClientName string `json:"clientName"`
	OS         string `json:"os"`
	Country    string `json:"country"`
	At         int64  `json:"at"`
}

// P2PHostRevokedEvent — кадр p2p_host_revoked: хост-роль у кімнати Address знято (reason: kicked,
// cohost_removed, room_deleted, …).
type P2PHostRevokedEvent struct {
	Address string `json:"address"`
	Reason  string `json:"reason"`
}

type inEvent struct {
	Type       string `json:"type"`
	Address    string `json:"address"`
	DeviceID   string `json:"deviceId"`
	Reason     string `json:"reason"`
	Name       string `json:"name"`
	ClientName string `json:"clientName"`
	OS         string `json:"os"`
	Country    string `json:"country"`
	At         int64  `json:"at"`
	AccountID  string `json:"accountId"`
	Status     string `json:"status"`
	LastSeen   int64  `json:"lastSeen"` // unix ms; лише з friend_presence offline
	PackName   string `json:"packName"`
	PackColor  string `json:"packColor"`
	Code       string `json:"code"`
	Message    string `json:"message"`

	// Чат (розділи 1.5.2): поля названі буквально як у VPS-протоколі.
	ChatID          string         `json:"chatId"`
	From            string         `json:"from"`
	BodyType        string         `json:"bodyType"`
	Ciphertext      string         `json:"ciphertext"`
	Nonce           string         `json:"nonce"`
	TS              int64          `json:"ts"`
	ID              int64          `json:"id"`
	ClientMessageID string         `json:"clientMessageId"`
	UpToMessageID   int64          `json:"upToMessageId"`
	ReqID           string         `json:"reqId"`
	MessageID       int64          `json:"messageId"`
	By              string         `json:"by"`
	Action          string         `json:"action"`
	Emoji           string         `json:"emoji"`
	ReplyToID       int64          `json:"replyToId"`
	EditedAt        int64          `json:"editedAt"`
	Reactions       []histReaction `json:"reactions"`
	Pinned          []histWireMsg  `json:"pinned"`
	InviteID        string         `json:"inviteId"`
	ServerAddr      string         `json:"serverAddr"`
	PackID          string         `json:"packId"`

	// Віддалене керування (remote.go).
	Owner    string         `json:"owner"`
	Online   bool           `json:"online"`
	Servers  []rsServerWire `json:"servers"`
	Owners   []rsOwnerWire  `json:"owners"`
	ServerID string         `json:"serverId"`
	Op       string         `json:"op"`
	SID      string         `json:"sid"`
	Key      string         `json:"key"`
	Tunnel   string         `json:"tunnel"`
	Data     string         `json:"data"`
	Mandate  *RemoteMandate `json:"mandate"`
}

func (p *PresenceClient) readLoop(ctx context.Context, conn *websocket.Conn) error {
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		if typ != websocket.MessageText {
			continue // бінарного ми не чекаємо — ігноруємо, а не парсимо
		}
		var ev inEvent
		if json.Unmarshal(data, &ev) != nil {
			continue
		}
		switch ev.Type {
		case "friends_snapshot":
			p.handleSnapshot(data)
		case "history_page":
			p.handleHistoryPage(data)
		case "history_error":
			var h histWire
			if json.Unmarshal(data, &h) == nil && h.ReqID != "" {
				p.resolveHistory(h.ReqID, histResult{err: ErrNotConnected})
			}
		case "friend_presence":
			p.handlePresence(ev)
		case "error":
			code := strings.ToLower(CleanText(ev.Code, 48))
			p.cfg.Logf("social: server error event: %s", code)
			if cid := CleanText(ev.ClientMessageID, 64); cid != "" && p.cfg.OnMessageFailed != nil {
				p.cfg.OnMessageFailed(cid, code)
				break
			}
			if strings.Contains(code, "auth") || strings.Contains(code, "token") ||
				strings.Contains(code, "jwt") || strings.Contains(code, "unauthor") || strings.Contains(code, "expired") {
				return errors.New("social: auth rejected")
			}
		case "new_message":
			// Відправник МУСИТЬ бути другом: скомпрометований VPS не може
			// підсунути «повідомлення» від стороннього акаунта. Виняток — ехо МОГО Ж повідомлення з
			// іншого мого пристрою (from = я), але лише в діалозі з моїм другом.
			if p.isFriend(ev.From) || (p.isSelf(ev.From) && p.isFriendChat(ev.ChatID)) {
				p.dispatchMessage(ev)
			}
		case "device_revoked":
			// Цей пристрій вилучено. Повідомляємо й ЗУПИНЯЄМО перепідключення: марно стукати з токеном,
			// який сервер більше не прийме. Локальні дані не чіпаємо.
			if p.cfg.OnDeviceRevoked != nil {
				p.cfg.OnDeviceRevoked(CleanText(ev.Reason, 32))
			}
			return ErrDeviceRevoked
		case "device_added":
			if p.cfg.OnDeviceAdded != nil {
				p.cfg.OnDeviceAdded(DeviceAddedEvent{DeviceID: CleanText(ev.DeviceID, 64), Name: CleanText(ev.Name, 40),
					ClientName: CleanText(ev.ClientName, 40), OS: CleanText(ev.OS, 32), Country: CleanText(ev.Country, 2), At: ev.At})
			}
		case "devices_changed":
			if p.cfg.OnDevicesChanged != nil {
				p.cfg.OnDevicesChanged()
			}
		case "chat_read_elsewhere":
			if p.cfg.OnReadElsewhere != nil && p.isFriendChat(ev.ChatID) {
				p.cfg.OnReadElsewhere(ReadEvent{ChatID: ev.ChatID, UpToMessageID: ev.UpToMessageID})
			}
		case "message_ack":
			if p.cfg.OnMessageAck != nil {
				p.cfg.OnMessageAck(MessageAck{
					ClientMessageID: CleanText(ev.ClientMessageID, 64),
					ChatID:          ev.ChatID, ID: ev.ID, TS: ev.TS,
				})
			}
		case "friend_typing":
			if p.isFriend(ev.AccountID) && p.cfg.OnTyping != nil {
				p.cfg.OnTyping(TypingEvent{ChatID: ev.ChatID, AccountID: ev.AccountID})
			}
		case "op_result":
			if ev.ReqID != "" {
				p.resolveOp(ev.ReqID, ev.Code)
			}
		case "message_deleted":
			if p.isFriendChat(ev.ChatID) && p.cfg.OnMessageDeleted != nil && ev.MessageID > 0 {
				p.cfg.OnMessageDeleted(DeletedEvent{ChatID: ev.ChatID, MessageID: ev.MessageID, By: ev.By})
			}
		case "message_edited":
			if p.isFriendChat(ev.ChatID) && p.cfg.OnMessageEdited != nil && ev.MessageID > 0 &&
				ev.Ciphertext != "" && len(ev.Ciphertext) <= maxCiphertextB64Len && ev.Nonce != "" && len(ev.Nonce) <= 40 {
				p.cfg.OnMessageEdited(EditedEvent{
					ChatID: ev.ChatID, MessageID: ev.MessageID, By: ev.By, EditedAt: ev.EditedAt,
					Ciphertext: ev.Ciphertext, Nonce: ev.Nonce,
				})
			}
		case "pins_updated":
			if p.isFriendChat(ev.ChatID) && p.cfg.OnPinsUpdated != nil {
				action := ev.Action
				if action != "pin" && action != "unpin" && action != "removed" {
					action = "pin"
				}
				p.cfg.OnPinsUpdated(PinsEvent{
					ChatID: ev.ChatID, Pinned: wireToChat(ev.Pinned),
					By: ev.By, Action: action, MessageID: ev.MessageID,
				})
			}
		case "message_read":
			if p.cfg.OnRead != nil {
				p.cfg.OnRead(ReadEvent{ChatID: ev.ChatID, UpToMessageID: ev.UpToMessageID})
			}
		case "reactions_updated":
			if p.isFriendChat(ev.ChatID) && p.cfg.OnReactionsUpdated != nil && ev.MessageID > 0 {
				action := ev.Action
				if action != "set" && action != "removed" {
					action = "set"
				}
				if !ValidAccountID(ev.By) {
					break
				}
				rws := make([]Reaction, 0, len(ev.Reactions))
				for _, rw := range ev.Reactions {
					if ValidAccountID(rw.By) && rw.Emoji != "" {
						rws = append(rws, Reaction{By: rw.By, Emoji: CleanText(rw.Emoji, 64)})
					}
				}
				p.cfg.OnReactionsUpdated(ReactionsEvent{
					ChatID: ev.ChatID, MessageID: ev.MessageID, Reactions: rws,
					By: ev.By, Emoji: CleanText(ev.Emoji, 64), Action: action,
				})
			}
		case "server_invite_incoming":
			// Своє ехо (хост розіслав запрошення) теж приймаємо, але лише у діалозі з другом —
			// так картка з'являється й у відкритому чаті відправника.
			if (p.isFriend(ev.From) || (p.isSelf(ev.From) && p.isFriendChat(ev.ChatID))) && p.cfg.OnServerInvite != nil {
				p.cfg.OnServerInvite(ServerInviteEvent{
					ChatID: ev.ChatID, From: ev.From, ServerAddr: ev.ServerAddr,
					PackID: CleanText(ev.PackID, 64), PackName: CleanText(ev.PackName, 64),
					InviteID: CleanText(ev.InviteID, 64),
				})
			}
		case "p2p_host_revoked":
			if p.cfg.OnP2PHostRevoked != nil && ev.Address != "" {
				p.cfg.OnP2PHostRevoked(P2PHostRevokedEvent{Address: CleanText(ev.Address, 128), Reason: CleanText(ev.Reason, 64)})
			}
		case "server_invite_revoked":
			if p.cfg.OnServerInviteRevoked != nil {
				p.cfg.OnServerInviteRevoked(CleanText(ev.InviteID, 64))
			}
		case "rs_snapshot":
			p.handleRemoteSnapshot(ev)
		case "rs_list":
			p.handleRemoteList(ev)
		case "rs_cmd_incoming":
			p.handleRemoteIncoming(ev)
		case "rs_cmd_result":
			p.handleRemoteResult(ev)
		case "rs_stream_in":
			p.handleRemoteStreamIn(ev)
		case "rs_stream_end":
			p.handleRemoteStreamEnd(ev)
		case "rs_vkey":
			p.handleRemoteKey(ev)
		case "rs_p2p_mandate":
			p.handleRemoteP2PMandate(ev)
		case "rs_pipe_in":
			p.handleRemotePipeIn(ev)
		case "rs_pipe_end":
			p.handleRemotePipeEnd(ev)
		default:
			// share_* — у цій фазі не обробляємо.
		}
	}
}

func (p *PresenceClient) handlePresence(ev inEvent) {
	if !ValidAccountID(ev.AccountID) {
		return
	}
	status := SanitizeStatus(ev.Status)
	if status == "" {
		return
	}
	p.mu.Lock()
	if _, ok := p.friends[ev.AccountID]; !ok {
		// Не друг (поки що). Не показуємо, але й не губимо: якщо за мить
		// SetFriends підтвердить дружбу — застосуємо. Ліміт — за розміром.
		if status != StatusOffline && len(p.pending) < MaxFriends {
			pr := Presence{Status: status, UpdatedAt: time.Now().UnixMilli()}
			if status == StatusInGame {
				pr.PackName = CleanText(ev.PackName, MaxPackNameLen)
				pr.PackColor = SanitizeColor(ev.PackColor)
			}
			p.pending[ev.AccountID] = pr
		} else if status == StatusOffline {
			delete(p.pending, ev.AccountID)
		}
		p.mu.Unlock()
		return
	}
	if status == StatusOffline {
		delete(p.live, ev.AccountID)
		p.mu.Unlock()
		if p.cfg.OnPresence != nil {
			now := time.Now().UnixMilli()
			p.cfg.OnPresence(ev.AccountID, Presence{Status: StatusOffline, UpdatedAt: now, LastSeenAt: sanitizeLastSeen(ev.LastSeen, now)}, false)
		}
		return
	}
	pr := Presence{Status: status, UpdatedAt: time.Now().UnixMilli()}
	if status == StatusInGame {
		pr.PackName = CleanText(ev.PackName, MaxPackNameLen)
		pr.PackColor = SanitizeColor(ev.PackColor)
	}
	p.live[ev.AccountID] = pr
	p.mu.Unlock()
	if p.cfg.OnPresence != nil {
		p.cfg.OnPresence(ev.AccountID, pr, true)
	}
}

// isFriend — чи є акаунт у білому списку друзів (VPS і так фільтрує, але
// клієнт не довіряє навіть йому: чуже повідомлення не показуємо).
// isFriendChat — чи діалог із другом (одна зі сторін chatId — друг). Скомпрометований VPS не
// може підсунути подію «чужого» діалогу.
func (p *PresenceClient) isFriendChat(chatID string) bool {
	i := strings.IndexByte(chatID, '_')
	if i < 0 {
		return false
	}
	return p.isFriend(chatID[:i]) || p.isFriend(chatID[i+1:])
}

// setSelf запам'ятовує мій accountId (з підписаного JWT): потрібен, щоб відрізнити «ехо» власного
// повідомлення з іншого мого пристрою від чужого.
func (p *PresenceClient) setSelf(id string) {
	p.mu.Lock()
	p.self = id
	p.mu.Unlock()
}

func (p *PresenceClient) isSelf(id string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return id != "" && p.self == id
}

func (p *PresenceClient) isFriend(id string) bool {
	if !ValidAccountID(id) {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	_, ok := p.friends[id]
	return ok
}

// dispatchMessage валідує форму incoming-повідомлення й віддає в UI.
func (p *PresenceClient) dispatchMessage(ev inEvent) {
	if p.cfg.OnMessage == nil {
		return
	}
	if _, err := splitChatID(ev.ChatID); err != nil {
		return
	}
	bt := "text"
	if ev.BodyType == "server_invite" {
		bt = "server_invite"
	}
	if bt == "text" {
		if ev.Ciphertext == "" || len(ev.Ciphertext) > maxCiphertextB64Len || ev.Nonce == "" || len(ev.Nonce) > 40 {
			return
		}
	}
	p.cfg.OnMessage(IncomingMessage{
		ChatID: ev.ChatID, From: ev.From, BodyType: bt,
		Ciphertext: ev.Ciphertext, Nonce: ev.Nonce, TS: ev.TS, ID: ev.ID, ReplyToID: ev.ReplyToID,
	})
}
