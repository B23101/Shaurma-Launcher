package social

import (
	"errors"
	"shaurma-launcher-backend/internal/crashguard"
	"sort"
	"sync"
	"time"
)

// State — повний знімок для UI. Серіалізується в JSON як є.
type State struct {
	LoggedIn bool      `json:"loggedIn"`
	Conn     ConnState `json:"conn"`
	Me       string    `json:"me,omitempty"` // мій accountId (з JWT, лише для відображення)
	Friends  []Friend  `json:"friends"`
	Invites  []Invite  `json:"invites"`
	Privacy  Privacy   `json:"privacy"`
	// Unread — непрочитані повідомлення за accountId співрозмовника (лише > 0).
	// Джерело правди — VPS; тут дзеркало для бейджів у UI.
	Unread map[string]int `json:"unread"`
	// MeStatus — МІЙ статус присутності для віджета акаунта: "online" |
	// "away" | "in_game" | "offline". UI малює підпис статусу САМЕ з нього
	// (разом із Conn), а не з припущення «я завжди в мережі»: раніше віджет
	// показував «В мережі» навіть коли акаунт не ввійшов або зв'язку не було.
	MeStatus string `json:"meStatus,omitempty"`
	// PrivacyLoaded=false — серверні налаштування ще не прочитано, UI показує
	// дефолти й не дозволяє «вдавати», що вони підтверджені.
	PrivacyLoaded bool `json:"privacyLoaded"`
	// Error — код останнього збою синхронізації друзів ("network", "not_logged_in"…),
	// порожньо, якщо все гаразд. Це КОД, а не текст сервера.
	Error string `json:"error,omitempty"`
	// Diag — короткий технічний код причини збою входу (без токенів). Додає
	// шар App; пакет social його не заповнює.
	Diag string `json:"diag,omitempty"`
}

// PresenceEvent — легка подія для UI (без пересилання всього State).
type PresenceEvent struct {
	AccountID string   `json:"accountId"`
	Presence  Presence `json:"presence"`
}

// Deps — усе, що Service отримує ззовні. Нічого з мережі/диска він не знає.
type Deps struct {
	Request Requester
	Token   TokenFunc
	// HWID — апаратний хеш ПК для hello (див. PresenceConfig.HWID).
	HWID  func() string
	WSURL string
	Emit  func(event string, payload any)
	Logf  func(string, ...any)

	// KeyDir — тека ключів E2E (напр. <дані>/.shaurma/keys). Порожньо — чат
	// вимкнено (E2E-ключів немає, лишається тільки presence/друзі).
	KeyDir string
	// KeyServer — серверні конверти ключів чату («Простий» режим). nil — ключ лише локальний
	// (як до оновлення). Задається разом із KeyDir.
	KeyServer KeyServer

	// PollEvery — як часто перечитувати друзів/запити через Worker, КОЛИ WS до VPS
	// недоступний (запасний режим). Коли WS живий, стан приходить пушем від VPS і
	// Worker не опитується взагалі. За замовчуванням 5 хв.
	PollEvery time.Duration

	// Remote — обробники віддаленого керування серверами (nil — функція вимкнена).
	Remote *RemoteHooks

	// AwayAfter — через скільки неактивності вікна лаунчера статус стає «відійшов».
	AwayAfter time.Duration

	allowInsecureWS bool
}

type gameInfo struct {
	name, color string
	at          time.Time
}

// Service — життєвий цикл соціальної системи: HTTP-стан друзів + WS-presence.
type Service struct {
	sendMu sync.Mutex
	sends  map[string]SendState

	// Антиспам чату (див. sendlimit.go).
	limiter sendLimiter

	d      Deps
	client *Client

	mu      sync.Mutex
	st      State
	pc      *PresenceClient
	crypto  *ChatCrypto
	keySent bool
	keyBusy bool
	stop    chan struct{}
	stopped chan struct{}
	games   map[string]gameInfo
	// Статус «відійшов»: вікно лаунчера неактивне довше за AwayAfter.
	winActive bool
	away      bool
	// manualAway — «Відійшов», поставлений вручну (тримається, доки користувач не змінить;
	// на відміну від away, який скидається поверненням у вікно). invisible — «Невидимий».
	manualAway bool
	// manualDnd — «Не турбувати», поставлений вручну: друзі бачать «в мережі» без збірки.
	manualDnd bool
	invisible bool
	awayTimer *time.Timer
	// ack — щоб не емітити однакове двічі.
	lastInviteCount int
	// lastMeStatus — останній ВІДДАНИЙ у UI власний статус (щоб не спамити
	// friends:update при кожному тіку, а лише на реальну зміну).
	lastMeStatus string
}

func NewService(d Deps) *Service {
	if d.Logf == nil {
		d.Logf = func(string, ...any) {}
	}
	if d.Emit == nil {
		d.Emit = func(string, any) {}
	}
	if d.AwayAfter <= 0 {
		d.AwayAfter = 5 * time.Minute
	}
	if d.PollEvery <= 0 {
		// Бюджет Worker (100k/добу): 60 с давали ~2880 запитів/добу лише на
		// фон друзів — це ~34 користувачі 24/7 на весь акаунт. 5 хв дають
		// ~576/добу (list+invites), лишаючи запас на чат і роздачу файлів.
		// Presence — не полінг, а WS/VPS, тож на живість це не впливає.
		d.PollEvery = 5 * time.Minute
	}
	s := &Service{
		d:         d,
		client:    NewClient(d.Request).WithBudget(NewBudget(DefaultDailyRequestBudget, 24*time.Hour)),
		games:     map[string]gameInfo{},
		winActive: true,
		st:        State{Conn: ConnOff, Friends: []Friend{}, Invites: []Invite{}, Privacy: DefaultPrivacy(), Unread: map[string]int{}},
	}
	if d.KeyDir != "" {
		s.crypto = NewChatCrypto(NewKeyStore(d.KeyDir), s.client)
		if d.KeyServer != nil {
			s.crypto.WithKeyServer(d.KeyServer, func() string {
				s.mu.Lock()
				defer s.mu.Unlock()
				return s.st.Me
			})
		}
	}
	return s
}

// Running — чи запущено фоновий цикл.
func (s *Service) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stop != nil
}

// Start запускає presence і фонове опитування. Ідемпотентно.
func (s *Service) Start() error {
	s.mu.Lock()
	if s.stop != nil {
		s.mu.Unlock()
		return nil
	}
	pc := NewPresenceClient(PresenceConfig{
		URL:   s.d.WSURL,
		Token: s.d.Token,
		HWID:  s.d.HWID,
		OnPresence: func(id string, p Presence, known bool) {
			s.mu.Lock()
			for i := range s.st.Friends {
				if s.st.Friends[i].AccountID == id {
					s.st.Friends[i].Presence = p
				}
			}
			s.mu.Unlock()
			s.d.Emit("friends:presence", PresenceEvent{AccountID: id, Presence: p})
		},
		OnState: func(c ConnState) {
			s.mu.Lock()
			s.st.Conn = c
			s.mu.Unlock()
			s.d.Emit("friends:conn", c)
		},
		OnSnapshot:            s.onSnapshot,
		OnMessage:             s.onIncomingMessage,
		OnMessageAck:          s.onMessageAck,
		OnMessageFailed:       s.onMessageFailed,
		OnTyping:              func(e TypingEvent) { s.d.Emit("chat:typing", e) },
		OnRead:                func(e ReadEvent) { s.d.Emit("chat:read", e) },
		OnServerInvite:        func(e ServerInviteEvent) { s.d.Emit("chat:server_invite", e) },
		OnServerInviteRevoked: func(id string) { s.d.Emit("chat:server_invite_revoked", id) },
		OnP2PHostRevoked:      func(e P2PHostRevokedEvent) { s.d.Emit("p2p:host_revoked", e) },
		OnMessageDeleted:      s.onMessageDeleted,
		OnMessageEdited:       s.onMessageEdited,
		OnPinsUpdated:         s.onPinsUpdated,
		OnReactionsUpdated:    s.onReactionsUpdated,
		OnDeviceRevoked:       s.onDeviceRevoked,
		OnDeviceAdded:         func(e DeviceAddedEvent) { s.d.Emit("account:device_added", e) },
		OnDevicesChanged:      func() { s.d.Emit("account:devices_changed", nil) },
		OnReadElsewhere:       s.onReadElsewhere,
		OnRemoteSnapshot:      s.d.remoteHooks().OnSnapshot,
		OnRemoteList:          s.d.remoteHooks().OnList,
		OnRemoteCmd:           s.d.remoteHooks().OnCmd,
		OnRemoteResult:        s.d.remoteHooks().OnResult,
		OnRemoteStream:        s.d.remoteHooks().OnStream,
		OnRemoteStreamEnd:     s.d.remoteHooks().OnStreamEnd,
		OnRemoteKey:           s.d.remoteHooks().OnVPSKey,
		OnRemoteP2PMandate:    s.d.remoteHooks().OnP2PMandate,
		OnRemotePipe:          s.d.remoteHooks().OnPipe,
		OnRemotePipeEnd:       s.d.remoteHooks().OnPipeEnd,
		Logf:                  s.d.Logf,
		allowInsecure:         s.d.allowInsecureWS,
	})
	// ЗАХИСТ ВІД ВИТОКУ ДО ЗАВАНТАЖЕННЯ ПРИВАТНОСТІ: доки справжні налаштування
	// не прочитано, не показуємо збірку ні в якому разі (status=online без деталей).
	// Лише локально: це не вибір користувача, на сервер його слати не можна.
	pc.SetPolicyLocal(Privacy{ShowStatusTo: "friends", ShowServerTo: "nobody", FriendRequest: "everyone"})
	s.pc = pc
	pc.SetInvisible(s.invisible)
	s.st.LoggedIn = true
	s.st.PrivacyLoaded = false
	// stop створюємо ДО syncGameLocked: воно повідомляє UI власний статус, а
	// «працює/не працює» визначається саме за s.stop (без нього статус був би
	// хибно «offline» на першому ж кадрі).
	s.stop = make(chan struct{})
	s.stopped = make(chan struct{})
	s.syncGameLocked()
	stop, stopped := s.stop, s.stopped
	s.mu.Unlock()

	if err := pc.Start(); err != nil {
		s.mu.Lock()
		s.pc, s.stop, s.stopped = nil, nil, nil
		s.st.LoggedIn = false
		s.emitMeStatusLocked()
		s.mu.Unlock()
		return err
	}
	{
		_f, _a0, _a1 := s.loop, stop, stopped
		go func() {
			defer crashguard.Recover("s.loop")
			_f(_a0, _a1)
		}()
	}

	// Ідентичність беремо з підписаного JWT, а не з клієнтського payload —
	// вона потрібна для канонічного chatId (ConversationID).
	if tok, err := s.d.Token(); err == nil {
		if id := AccountIDFromJWT(tok); id != "" {
			s.mu.Lock()
			s.st.Me = id
			s.mu.Unlock()
		}
	}
	// Публікація власного публічного ключа — best-effort (1 запит, без неї
	// друзі просто не змогли б писати). Якщо не вдалось — спробуємо в Refresh.
	{
		_f := s.publishOwnKey
		go func() {
			defer crashguard.Recover("s.publishOwnKey")
			_f()
		}()
	}
	return nil
}

// publishOwnKey готує ключі чату (бере з сервера / створює лише коли це безпечно) і публікує публічну
// частину. Якщо сервер/мережа тимчасово недоступні — повторює з наростаючою паузою, ПОКИ сервіс працює:
// чат не пише без ключа, але й нового ключа «про всяк випадок» не створює.
func (s *Service) publishOwnKey() {
	// «Простий» режим чату: ключі пристроїв більше не використовуються (нові повідомлення не шифруються
	// на пристрої), тож нічого не створюємо й не публікуємо — інакше новий пристрій міг би затерти
	// чужий опублікований ключ і зламати читання старих E2E-повідомлень.
	if s.crypto == nil || chatPlainMode {
		return
	}
	s.mu.Lock()
	if s.keySent || s.keyBusy {
		s.mu.Unlock()
		return
	}
	s.keyBusy = true
	stop := s.stop
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.keyBusy = false; s.mu.Unlock() }()

	wait := 5 * time.Second
	for {
		err := s.crypto.PublishOwnKey()
		s.emitKeyStatus()
		if err == nil {
			s.mu.Lock()
			s.keySent = true
			s.mu.Unlock()
			return
		}
		if errors.Is(err, ErrDeviceRevoked) {
			s.onDeviceRevoked("revoked")
			return
		}
		s.d.Logf("social: ключі чату ще недоступні (%s), повторю", errorCode(err))
		if stop == nil {
			return
		}
		select {
		case <-stop:
			return
		case <-time.After(wait):
		}
		if wait < 5*time.Minute {
			wait *= 2
		}
	}
}

func (s *Service) emitKeyStatus() {
	if s.crypto != nil {
		s.d.Emit("chat:keys", s.ChatKeyStatus())
	}
}

// ChatKeyStatus — стан ключів чату для UI (ready | pending | need_recovery).
func (s *Service) ChatKeyStatus() KeyStatus {
	if s.crypto == nil || chatPlainMode {
		return KeyStatus{State: KeysReady} // ключі чату не потрібні: чат завжди готовий
	}
	return s.crypto.Status()
}

// RotateChatKey створює нову версію ключа чату (після «Це не я»/екстреної ротації).
func (s *Service) RotateChatKey(version int) error {
	if s.crypto == nil {
		return ErrChatDisabled
	}
	err := s.crypto.Rotate(version)
	s.emitKeyStatus()
	return err
}

// ResetChatKeys — користувач погодився почати чат з чистого аркуша (стан need_recovery).
func (s *Service) ResetChatKeys() error {
	if s.crypto == nil {
		return ErrChatDisabled
	}
	err := s.crypto.ResetKeys()
	s.emitKeyStatus()
	if err == nil {
		{
			_f := s.publishOwnKey
			go func() {
				defer crashguard.Recover("s.publishOwnKey")
				_f()
			}()
		}
	}
	return err
}

// onDeviceRevoked — сервер повідомив: ЦЕ пристрій вилучено. Зупиняємо соціальну систему, показуємо
// причину, але НІЧОГО локально не видаляємо (ключі, налаштування лишаються до явного рішення).
func (s *Service) onDeviceRevoked(reason string) {
	s.mu.Lock()
	s.st.LoggedIn, s.st.Error = false, "device_revoked"
	s.st.Conn = ConnOff
	snap := s.st
	// Статус завжди свіжий: s.st.MeStatus оновлюється лише в emitMeStatusLocked,
	// тож «сирий» знімок перебивав у UI актуальний статус старим (мерехтіння).
	snap.MeStatus = s.myStatusLocked()
	s.mu.Unlock()
	s.d.Emit("account:device_revoked", map[string]string{"reason": reason})
	s.d.Emit("friends:update", snap)
}

// onReadElsewhere — діалог прочитано на іншому моєму пристрої: скидаємо лічильник тут.
func (s *Service) onReadElsewhere(e ReadEvent) {
	s.mu.Lock()
	peer := OtherPartyInChat(e.ChatID, s.st.Me)
	var snap map[string]int
	if peer != "" && s.st.Unread[peer] > 0 {
		delete(s.st.Unread, peer)
		snap = copyUnread(s.st.Unread)
	}
	s.mu.Unlock()
	if snap != nil {
		s.d.Emit("friends:unread", snap)
	}
	s.d.Emit("chat:read_elsewhere", e)
}

// Stop зупиняє все й ЧИСТИТЬ стан у пам'яті (вихід з акаунта не лишає списку
// друзів у процесі). Ідемпотентно.
func (s *Service) Stop() {
	s.mu.Lock()
	stop, stopped, pc := s.stop, s.stopped, s.pc
	s.stop, s.stopped, s.pc = nil, nil, nil
	s.mu.Unlock()
	if stop == nil {
		return
	}
	close(stop)
	<-stopped
	if pc != nil {
		pc.Stop()
	}
	s.mu.Lock()
	s.st = State{Conn: ConnOff, Friends: []Friend{}, Invites: []Invite{}, Privacy: DefaultPrivacy(), Unread: map[string]int{}}
	s.lastInviteCount = 0
	// Сервіс зупинено — власний статус знову «не в мережі», і UI має
	// дізнатись про це одразу (див. myStatusLocked).
	s.lastMeStatus = StatusOffline
	s.away = false
	if s.awayTimer != nil {
		s.awayTimer.Stop()
		s.awayTimer = nil
	}
	s.mu.Unlock()
	s.d.Emit("friends:update", s.Snapshot())
}

// Snapshot — копія стану (безпечно віддавати в JSON).
func (s *Service) Snapshot() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshotLocked()
}

// snapshotLocked — той самий знімок, але виклик з-під уже взяного s.mu
// (потрібно тим місцям, які самі шлють friends:update).
func (s *Service) snapshotLocked() State {
	out := s.st
	out.Friends = append([]Friend(nil), s.st.Friends...)
	out.Invites = append([]Invite(nil), s.st.Invites...)
	out.Unread = copyUnread(s.st.Unread)
	out.MeStatus = s.myStatusLocked()
	return out
}

// myStatusLocked — МІЙ статус присутності для UI. Виклик лише з-під s.mu.
// Сервіс не працює / presence-клієнта немає — чесно кажемо "offline":
// показувати «в мережі», коли жодного з'єднання немає, — це та сама
// проблема, на яку скаржився користувач («статус мережі не визначається»).
func (s *Service) myStatusLocked() string {
	if s.stop == nil || s.pc == nil {
		return StatusOffline
	}
	if s.invisible {
		return StatusInvisible
	}
	return s.pc.Status()
}

// SetManualPresence застосовує ручний режим присутності: "" (в мережі — статус визначає лаунчер),
// "away", "dnd" або "invisible". Ручний режим перекриває автоматичний «у грі»: друзі не бачать збірку.
// Для друзів «dnd» — це «в мережі» без збірки (окремого статусу в протоколі немає).
func (s *Service) SetManualPresence(mode string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.manualAway = mode == "away"
	s.manualDnd = mode == "dnd"
	s.invisible = mode == "invisible"
	if s.pc != nil {
		s.pc.SetInvisible(s.invisible)
		s.syncGameLocked()
	}
}

// emitMeStatusLocked — якщо власний статус змінився (у грі / відійшов /
// у мережі / офлайн), шлемо UI свіжий знімок одразу, не чекаючи наступного
// опитування. Виклик лише з-під s.mu.
func (s *Service) emitMeStatusLocked() {
	st := s.myStatusLocked()
	s.st.MeStatus = st
	if st == s.lastMeStatus {
		return
	}
	s.lastMeStatus = st
	s.d.Emit("friends:update", s.snapshotLocked())
}

func copyUnread(in map[string]int) map[string]int {
	out := make(map[string]int, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// ── Фоновий цикл ────────────────────────────────────────────────────────

func (s *Service) loop(stop, stopped chan struct{}) {
	defer close(stopped)
	s.Refresh()
	t := time.NewTicker(s.d.PollEvery)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			// Коли WS живий, стан приходить від VPS пушем — Worker не опитуємо.
			s.mu.Lock()
			wsOK := s.st.Conn == ConnOnline
			s.mu.Unlock()
			if !wsOK {
				s.Refresh()
			}
		}
	}
}

// onSnapshot — повний стан від VPS (друзі, запити, приватність, непрочитане).
// Викликається з WS-горутини після hello і після кожної зміни на боці VPS
// (прийнято/видалено друга, новий запит, зміна приватності).
func (s *Service) onSnapshot(sn Snapshot) {
	s.mu.Lock()
	if s.stop == nil {
		s.mu.Unlock()
		return
	}
	sort.SliceStable(sn.Friends, func(i, j int) bool { return friendLess(sn.Friends[i], sn.Friends[j]) })
	s.st.Friends = sn.Friends
	s.st.Invites = sn.Invites
	s.st.Privacy, s.st.PrivacyLoaded = sn.Privacy, true
	s.st.LoggedIn, s.st.Error = true, ""
	unread := map[string]int{}
	for _, u := range sn.Unread {
		if peer := OtherPartyInChat(u.ChatID, s.st.Me); peer != "" {
			unread[peer] = u.Count
		}
	}
	s.st.Unread = unread
	newInvites := len(sn.Invites) > s.lastInviteCount
	s.lastInviteCount = len(sn.Invites)
	pc, policy := s.pc, s.st.Privacy
	snap := s.st
	// Статус завжди свіжий: s.st.MeStatus оновлюється лише в emitMeStatusLocked,
	// тож «сирий» знімок перебивав у UI актуальний статус старим (мерехтіння).
	snap.MeStatus = s.myStatusLocked()
	snap.Friends = append([]Friend(nil), s.st.Friends...)
	snap.Invites = append([]Invite(nil), s.st.Invites...)
	snap.Unread = copyUnread(unread)
	unreadEv := copyUnread(unread) // копія ПІД замком: мапа s.st.Unread далі змінюється іншими горутинами
	s.mu.Unlock()

	if pc != nil {
		// Значення від самого сервера: повертати його назад кадром не треба.
		pc.SetPolicyLocal(policy)
	}
	s.d.Emit("friends:update", snap)
	s.d.Emit("friends:unread", unreadEv)
	if newInvites {
		s.d.Emit("friends:invite", len(snap.Invites))
	}
}

// Refresh перечитує друзів, запити й (якщо ще не читали) приватність.
// Помилки не фатальні: стан лишається попереднім, код помилки йде в UI.
// Нові вузли віддають усе одним мікс-запитом (/friends/batch); старі — по одному.
func (s *Service) Refresh() {
	s.mu.Lock()
	loaded := s.st.PrivacyLoaded
	s.mu.Unlock()
	friends, invites, priv, ferr, ierr, perr := s.client.RefreshBundle(!loaded)

	s.mu.Lock()
	if s.stop == nil { // Stop() встиг раніше — нічого не публікуємо
		s.mu.Unlock()
		return
	}
	errCode := ""
	if err := firstErr(ferr, ierr); err != nil {
		errCode = errorCode(err)
		if errors.Is(err, ErrNotLoggedIn) || errors.Is(err, ErrDeviceRevoked) {
			s.st.LoggedIn = false
		}
	} else {
		// Обидва запити пройшли → сесія жива. Раніше LoggedIn скидався в false
		// при будь-якій разовій відмові (напр. токен ще оновлювався під час
		// старту) і НІКОЛИ не повертався в true, поки сервіс не перезапустять:
		// ліцензійний гравець назавжди бачив «Потрібен вхід у скін-акаунт».
		s.st.LoggedIn = true
	}
	if ferr == nil {
		live := map[string]Presence{}
		if s.pc != nil {
			live = s.pc.Snapshot()
		}
		for i := range friends {
			if p, ok := live[friends[i].AccountID]; ok {
				friends[i].Presence = p
			}
		}
		sort.SliceStable(friends, func(i, j int) bool { return friendLess(friends[i], friends[j]) })
		s.st.Friends = friends
	}
	if ierr == nil {
		s.st.Invites = invites
	}
	if !loaded && perr == nil {
		s.st.Privacy = priv
		s.st.PrivacyLoaded = true
	}
	s.st.Error = errCode
	// SetFriends/SetPolicy викликаємо ПІСЛЯ відпускання s.mu: SetFriends синхронно
	// кличе OnPresence, а той бере s.mu — під замком це був би дедлок.
	pc := s.pc
	var friendIDs []string
	setFriends := ferr == nil
	if setFriends {
		friendIDs = make([]string, len(s.st.Friends))
		for i, f := range s.st.Friends {
			friendIDs[i] = f.AccountID
		}
	}
	policy, applyPolicy := s.st.Privacy, s.st.PrivacyLoaded
	newInvites := ierr == nil && len(s.st.Invites) > s.lastInviteCount
	if ierr == nil {
		s.lastInviteCount = len(s.st.Invites)
	}
	snap := s.st
	// Статус завжди свіжий: s.st.MeStatus оновлюється лише в emitMeStatusLocked,
	// тож «сирий» знімок перебивав у UI актуальний статус старим (мерехтіння).
	snap.MeStatus = s.myStatusLocked()
	snap.Friends = append([]Friend(nil), s.st.Friends...)
	snap.Invites = append([]Invite(nil), s.st.Invites...)
	s.mu.Unlock()

	if pc != nil {
		if setFriends {
			pc.SetFriends(friendIDs)
		}
		if applyPolicy {
			pc.SetPolicyLocal(policy)
		}
	}
	s.d.Emit("friends:update", snap)
	if newInvites {
		s.d.Emit("friends:invite", len(snap.Invites))
	}
}

func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

// chatPlainMode — нові повідомлення йдуть у «простому» режимі (без ключів пристроїв).
const chatPlainMode = true

// errorCode перетворює помилку в стабільний код для UI. Тексти серверів і
// мережевих бібліотек у UI не потрапляють ніколи.
func errorCode(err error) string {
	var ae *APIError
	switch {
	case errors.Is(err, ErrClockSkew):
		return "clock_skew"
	case errors.Is(err, ErrDeviceRevoked):
		return "device_revoked"
	case errors.Is(err, ErrKeysPending):
		return "keys_pending"
	case errors.Is(err, ErrKeysNeedRecovery):
		return "keys_need_recovery"
	case errors.Is(err, ErrNotLoggedIn):
		return "not_logged_in"
	case errors.Is(err, ErrBadInput):
		return "bad_input"
	case errors.Is(err, ErrNotSupported):
		return "not_supported"
	case errors.Is(err, ErrBudgetExhausted):
		return "budget_exhausted"
	case errors.Is(err, ErrNotConnected):
		return "not_connected"
	case errors.Is(err, ErrKeyNotFound):
		return "key_not_found"
	case errors.Is(err, ErrChatSpam):
		return "chat_spam"
	case errors.As(err, &ae):
		return ae.Code
	}
	return "network"
}

// ErrorCode — публічна версія для обгортки App.
func ErrorCode(err error) string {
	if err == nil {
		return ""
	}
	return errorCode(err)
}

// friendLess: у грі → онлайн → офлайн, усередині групи за ніком без регістру.
func friendLess(a, b Friend) bool {
	ra, rb := statusRank(a.Presence.Status), statusRank(b.Presence.Status)
	if ra != rb {
		return ra < rb
	}
	return lowerKey(a.Nickname) < lowerKey(b.Nickname)
}

func statusRank(s string) int {
	switch s {
	case StatusInGame:
		return 0
	case StatusOnline:
		return 1
	}
	return 2
}

func lowerKey(s string) string {
	r := []rune(s)
	for i, c := range r {
		if c >= 'A' && c <= 'Z' {
			r[i] = c + 32
		} else if c >= 'А' && c <= 'Я' {
			r[i] = c + 32
		}
	}
	return string(r)
}

// ── Дії користувача ─────────────────────────────────────────────────────

// Усі дії: виконати на Worker → одразу перечитати стан. Перечитування
// синхронне, щоб UI отримав уже актуальний знімок у відповіді.

// SendInviteByHandle — запит у друзі за @handle.
func (s *Service) SendInviteByHandle(handle string) error {
	if !s.Running() {
		return ErrNotLoggedIn
	}
	return s.client.SendInviteByHandle(handle)
}

func (s *Service) SendInvite(accountID string) error {
	if !s.Running() {
		return ErrNotLoggedIn
	}
	if err := s.client.SendInvite(accountID); err != nil {
		return err
	}
	return nil
}

func (s *Service) SentInvites() ([]Invite, error) {
	if !s.Running() {
		return nil, ErrNotLoggedIn
	}
	return s.client.ListSentInvites()
}

func (s *Service) Respond(inviteID string, accept bool) error {
	if !s.Running() {
		return ErrNotLoggedIn
	}
	if err := s.client.RespondInvite(inviteID, accept); err != nil {
		return err
	}
	s.afterFriendAction(func(st *State) {
		kept := st.Invites[:0:0]
		for _, i := range st.Invites {
			if i.ID != inviteID {
				kept = append(kept, i)
			}
		}
		st.Invites = kept
	}, accept)
	return nil
}

// afterFriendAction оновлює UI після дії з друзями. Коли WS живий, VPS сам надішле
// свіжий знімок (Worker його про це сповістив) — зайвого запиту до Worker немає:
// лише миттєва локальна правка, щоб UI не чекав. Якщо ж push не прийшов за 4 с
// (best-effort у Worker) — одноразове перечитування. Без WS — звичайний Refresh.
func (s *Service) afterFriendAction(patch func(*State), expectNewFriend bool) {
	s.mu.Lock()
	wsOK := s.st.Conn == ConnOnline
	if wsOK {
		patch(&s.st)
		s.lastInviteCount = len(s.st.Invites)
	}
	snap := s.st
	// Статус завжди свіжий: s.st.MeStatus оновлюється лише в emitMeStatusLocked,
	// тож «сирий» знімок перебивав у UI актуальний статус старим (мерехтіння).
	snap.MeStatus = s.myStatusLocked()
	snap.Friends = append([]Friend(nil), s.st.Friends...)
	snap.Invites = append([]Invite(nil), s.st.Invites...)
	snap.Unread = copyUnread(s.st.Unread)
	before := len(s.st.Friends)
	s.mu.Unlock()
	if !wsOK {
		s.Refresh()
		return
	}
	s.d.Emit("friends:update", snap)
	if expectNewFriend {
		time.AfterFunc(4*time.Second, func() {
			s.mu.Lock()
			missed := s.stop != nil && len(s.st.Friends) <= before
			s.mu.Unlock()
			if missed {
				s.Refresh()
			}
		})
	}
}

func (s *Service) Remove(accountID string) error {
	if !s.Running() {
		return ErrNotLoggedIn
	}
	if err := s.client.RemoveFriend(accountID); err != nil {
		return err
	}
	s.afterFriendAction(func(st *State) {
		kept := st.Friends[:0:0]
		for _, f := range st.Friends {
			if f.AccountID != accountID {
				kept = append(kept, f)
			}
		}
		st.Friends = kept
		delete(st.Unread, accountID)
	}, false)
	return nil
}

// SetPrivacy зберігає на Worker, і ЛИШЕ після успіху застосовує локально.
// Якщо збереження не вдалось, локальна політика лишається попередньою — UI
// не повинен показувати «нікому», поки сервер усе ще каже «друзям».
func (s *Service) SetPrivacy(p Privacy) error {
	if !s.Running() {
		return ErrNotLoggedIn
	}
	if err := ValidatePrivacy(p); err != nil {
		return err
	}
	if err := s.client.SetPrivacy(p); err != nil {
		return err
	}
	s.mu.Lock()
	s.st.Privacy = p
	s.st.PrivacyLoaded = true
	if s.pc != nil {
		s.pc.SetPolicy(p)
	}
	s.mu.Unlock()
	s.d.Emit("friends:update", s.Snapshot())
	return nil
}

// ── Присутність у грі ───────────────────────────────────────────────────

// GameStarted / GameExited викликаються з наявних хуків запуску збірки (тих
// самих місць, де вже викликається Discord RPC). Можна викликати ДО Start —
// статус відправиться, щойно з'явиться з'єднання.
func (s *Service) GameStarted(buildID, packName, packColor string) {
	if buildID == "" {
		return
	}
	s.mu.Lock()
	s.games[buildID] = gameInfo{name: CleanText(packName, MaxPackNameLen), color: SanitizeColor(packColor), at: time.Now()}
	s.syncGameLocked()
	s.mu.Unlock()
}

func (s *Service) GameExited(buildID string) {
	s.mu.Lock()
	delete(s.games, buildID)
	s.syncGameLocked()
	s.mu.Unlock()
}

// syncGameLocked: статус = найновіша з запущених збірок (кілька одночасно — ок).
// Наприкінці завжди повідомляємо UI, якщо статус змінився: віджет акаунта в
// sidebar показує «у грі / відійшов / у мережі» одразу, а не після опитування.
func (s *Service) syncGameLocked() {
	defer s.emitMeStatusLocked()
	if s.pc == nil {
		return
	}
	// Ручний статус сильніший за автоматичний «у грі».
	if s.manualAway {
		s.pc.SetStatus(StatusAway, "", "")
		return
	}
	if s.manualDnd {
		s.pc.SetStatus(StatusOnline, "", "")
		return
	}
	var cur *gameInfo
	for _, g := range s.games {
		g := g
		if cur == nil || g.at.After(cur.at) {
			cur = &g
		}
	}
	if cur == nil {
		if s.away || s.manualAway {
			s.pc.SetStatus(StatusAway, "", "")
		} else {
			s.pc.SetStatus(StatusOnline, "", "")
		}
		return
	}
	s.pc.SetStatus(StatusInGame, cur.name, cur.color)
}

// SetWindowActive викликається при зміні активності вікна лаунчера. Неактивне
// довше за AwayAfter (і гравець не в грі) → статус «відійшов»; повернувся —
// одразу «в мережі». У грі статус завжди «у грі», незалежно від вікна.
func (s *Service) SetWindowActive(active bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.winActive = active
	if s.awayTimer != nil {
		s.awayTimer.Stop()
		s.awayTimer = nil
	}
	if active {
		if s.away {
			s.away = false
			s.syncGameLocked()
		}
		return
	}
	if s.stop == nil {
		return
	}
	s.awayTimer = time.AfterFunc(s.d.AwayAfter, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.winActive || s.stop == nil || s.away {
			return
		}
		s.away = true
		s.syncGameLocked()
	})
}

// ErrClockSkew — годинник цього ПК розходиться із сервером: «Виправте дату й час».
var ErrClockSkew = errors.New("social:clock_skew")
