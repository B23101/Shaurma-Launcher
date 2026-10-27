// api.go — Wails API-методи P2P-модуля. Реєструються в App (backend/app.go)
// і викликаються з фронтенду через Bridge.Invoke (backend/wails.ts).
//
// Тут зібрано ВСЮ державну логіку модуля: активні хости, гостьовий слухач,
// локальна книга зайнятих адрес (store.go) і UDP-тунель голосу. App-шар
// лише прокидає виклики й додає перевірку налаштувань/акаунта.
package p2p

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"shaurma-launcher-backend/internal/launchlog"
)

// ControlURL — адреса власного Headscale-сервера. Захардкоджено так само,
// як cdnBaseURL у internal/update: це публічна адреса контрольного сервера
// (секретом є лише auth key на кожну сесію).
const ControlURL = "https://vpn.shaurma-launcher.org"

// PlaySuffix — суфікс домену, який гравець вводить у Direct Connect:
// "<адреса>.play.shaurma-launcher.org". DNS-wildcard "*.play" резолвиться в
// guestExternalAddr (127.0.0.2), а порт — стандартний Minecraft-порт 25565.
const PlaySuffix = "play.shaurma-launcher.org"

// ClaimResult — стан після спроби застовбити адресу.
type ClaimResult struct {
	Subdomain string `json:"subdomain"` // "prlool-482917" (без суфікса .play...)
	Address   string `json:"address"`   // "<subdomain>.play.shaurma-launcher.org"
	// Reused — адресу взято з локальної книги (вона вже була застовблена),
	// а не зайнято заново. UI може написати «адреса вже була у вас».
	Reused bool `json:"reused"`
}

// HostStatus — стан хостингу для фронтенду (одна картка = одна адреса).
type HostStatus struct {
	Active bool `json:"active"`
	// Subdomain — повна адреса без суфікса ("prlool-482917"). Потрібна, щоб
	// фронтенд знав, яку саме картку зупиняти (мультихостинг).
	Subdomain string `json:"subdomain"`
	Address   string `json:"address"` // "<subdomain>.play.shaurma-launcher.org"
	// UDPPort — порт, на якому РЕАЛЬНО піднявся UDP-тунель. Порожній =
	// голос через P2P не тунелюється.
	UDPPort string `json:"udpPort,omitempty"`
	// UDPError — чому UDP-тунель НЕ піднявся (порт зайнятий тощо). Порожній
	// = ок або UDP не вмикали.
	UDPError string `json:"udpError,omitempty"`
	// PlayerCount — скільки гостей зараз під'єднано (для бейджа в UI).
	PlayerCount int    `json:"playerCount"`
	Error       string `json:"error,omitempty"`
	// Disabled — тунель вимкнено адміном (запит повернув disabled).
	Disabled bool `json:"disabled,omitempty"`
	// Access — чинний режим доступу кімнати (all/accounts/friends/list).
	Access string `json:"access,omitempty"`
	// ActiveCount/MaxCount — скільки P2P-серверів уже підняті і хард-ліміт,
	// щоб UI показав "2/3".
	ActiveCount int `json:"activeCount"`
	MaxCount    int `json:"maxCount"`
}

// API — контекст P2P-модуля.
type API struct {
	dataDir string
	token   func() string // update.Token
	baseURL func() string // update.CDNBaseURL
	// auth — JWT+HWID акаунта для запитів до вузла (SetAuth; nil = анонімно). Ставиться один раз
	// при старті лаунчера, тож читається без замка.
	auth AuthFunc

	mu sync.Mutex
	// hosts — активні хостингові сесії ЦЬОГО лаунчера, ключ — subdomain.
	// Мультихостинг: кілька збірок одночасно, кожна зі своєю tsnet-нодою.
	hosts       map[string]*HostSession
	ownerSecret string // кешований локальний секрет, читається лінькво

	// book — локальна книга зайнятих адрес (store.go). Завантажується
	// лінькво й тримається в пам'яті; кожна зміна одразу пишеться на диск.
	book       *addressBook
	bookLoaded bool

	// OnHostStopped — тунель зупинено не користувачем (оренду забрано / співхоста відкликано). App
	// перетворює це на подію для UI.
	OnHostStopped func(subdomain, reason string)

	guest        *GuestService
	guestStarted bool
	guestErr     error
}

// NewAPI створює P2P API.
func NewAPI(dataDir string, tokenFn, baseURLFn func() string) *API {
	return &API{
		dataDir: dataDir,
		token:   tokenFn,
		baseURL: baseURLFn,
		hosts:   make(map[string]*HostSession),
	}
}

// SetAuth підключає джерело JWT+HWID до всіх запитів лобі (виклик App при старті, до першого запиту).
func (a *API) SetAuth(fn AuthFunc) { a.auth = fn }

func (a *API) stateDir() string { return filepath.Join(a.dataDir, ".shaurma", "p2p") }

func (a *API) lobbyClient() *LobbyClient {
	return NewLobbyClient(a.baseURL(), a.token()).WithAuth(a.auth)
}

// ownerSecretValue повертає (і за потреби генерує+зберігає) локальний
// секрет власника. Кешується в пам'яті після першого читання диску.
func (a *API) ownerSecretValue() (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.ownerSecret != "" {
		return a.ownerSecret, nil
	}
	secret, err := loadOrCreateOwnerSecret(a.stateDir())
	if err != nil {
		return "", err
	}
	a.ownerSecret = secret
	return secret, nil
}

// bookLocked повертає книгу адрес, читаючи її з диска при першому зверненні.
// ЛИШЕ з-під a.mu.
func (a *API) bookLocked() (*addressBook, error) {
	if a.bookLoaded {
		return a.book, nil
	}
	book, err := loadAddressBook(a.stateDir())
	if err != nil {
		return nil, err
	}
	a.book = book
	a.bookLoaded = true
	return book, nil
}

// saveBookLocked пише книгу на диск. ЛИШЕ з-під a.mu.
func (a *API) saveBookLocked() error {
	if a.book == nil {
		return nil
	}
	return a.book.save(a.stateDir())
}

// P2PStatus — стан доступності P2P для UI.
type P2PStatus struct {
	Configured bool   `json:"configured"`
	RoomDomain string `json:"roomDomain,omitempty"`
	Reason     string `json:"reason,omitempty"`
	// DisabledByUser — вимкнено в налаштуваннях лаунчера.
	DisabledByUser bool `json:"disabledByUser"`
	// AccountOK — чи є активний Shaurma/ліцензійний акаунт. Потрібен ЛИШЕ для хостингу
	// (адреси прив'язуються до акаунта). Приєднання як гість працює і без акаунта.
	AccountOK     bool   `json:"accountOk"`
	AccountReason string `json:"accountReason,omitempty"`
}

// GetStatus повертає стан P2P. Мережева помилка НЕ трактується як
// «вимкнено»: інакше тимчасовий збій мережі виглядав би як відсутність фічі.
func (a *API) GetStatus(disabledByUser bool, accountOK bool, accountReason string) P2PStatus {
	st := P2PStatus{AccountOK: accountOK, AccountReason: accountReason}
	if disabledByUser {
		st.DisabledByUser = true
		st.Reason = "P2P вимкнено в налаштуваннях лаунчера (Налаштування → Основне)"
		return st
	}
	// accountOK стосується ЛИШЕ хостингу (адреси прив'язуються до акаунта). Приєднання (гість)
	// акаунта не потребує, тож стан сервера перевіряємо завжди — інакше гість без акаунта
	// бачив би «P2P недоступний» і не міг би під'єднатись.
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	remote, err := a.lobbyClient().FetchStatus(ctx)
	if err != nil {
		st.Configured = true
		st.RoomDomain = PlaySuffix
		st.Reason = "Не вдалося перевірити стан P2P: " + err.Error()
		return st
	}
	st.Configured = remote.Configured
	st.RoomDomain = remote.RoomDomain
	st.Reason = remote.Reason
	return st
}

// ── Книга зайнятих адрес ───────────────────────────────────────────────

// ListClaimedAddresses повертає адреси, зайняті під цим акаунтом (для UI:
// гравець бачить свої адреси й не мусить займати нову щораза).
func (a *API) ListClaimedAddresses(accountID string) []ClaimedAddress {
	a.mu.Lock()
	defer a.mu.Unlock()
	book, err := a.bookLocked()
	if err != nil {
		return nil
	}
	// Адреси — ВЛАСНІСТЬ ЛАУНЧЕРА (секрет власника лежить у ньому), а не акаунта: усі акаунти
	// бачать той самий список, тож перемикання акаунта не «губить» збережені адреси й не
	// ламає хостинг. accountID лишається в підписі для сумісності. Тимчасові (tmp0..tmp9) у
	// збережені не потрапляють — див. tempaddr.go.
	_ = accountID
	return book.listAll()
}

// ClaimP2PAddress застовблює адресу на основі базової назви. ЯКЩО адреса з
// цією базовою назвою вже зайнята ЦИМ САМИМ акаунтом — повертаємо її без
// жодного мережевого запиту (саме це виправляє баг «щораза нова адреса»).
func (a *API) ClaimP2PAddress(baseName, accountID, label string) (ClaimResult, error) {
	return a.ClaimP2PAddressAccess(baseName, accountID, label, "")
}

// ClaimP2PAddressAccess — те саме з початковим режимом доступу нової адреси (порожньо = типовий).
// Для вже наявної адреси режим не змінюється: його міняє SetAccess.
func (a *API) ClaimP2PAddressAccess(baseName, accountID, label, access string) (ClaimResult, error) {
	if existing, ok := a.findClaimed(baseName, accountID); ok {
		// Адреса вже наша — повертаємо її і ЗАКРІПЛЮЄМО за цим сервером
		// (label), щоб віджет саме цього сервера її й показував.
		a.bindClaimed(existing.Subdomain, label)
		return ClaimResult{Subdomain: existing.Subdomain, Address: existing.Address(), Reused: true}, nil
	}

	secret, err := a.ownerSecretValue()
	if err != nil {
		return ClaimResult{}, fmt.Errorf("p2p: локальний секрет: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	resp, err := a.lobbyClient().ClaimAddressAccess(ctx, baseName, secret, access)
	if err != nil {
		return ClaimResult{}, err
	}

	now := time.Now().UnixMilli()
	claimed := ClaimedAddress{
		Subdomain:  resp.Subdomain,
		BaseName:   baseName,
		AccountID:  accountID,
		Label:      label,
		CreatedAt:  now,
		LastUsedAt: now,
	}

	a.mu.Lock()
	book, berr := a.bookLocked()
	if berr == nil {
		book.upsert(claimed)
		_ = a.saveBookLocked()
	}
	a.mu.Unlock()

	return ClaimResult{Subdomain: claimed.Subdomain, Address: claimed.Address()}, nil
}

// findClaimed шукає вже зайняту адресу в книзі.
func (a *API) findClaimed(baseName, accountID string) (ClaimedAddress, bool) {
	baseName = strings.ToLower(strings.TrimSpace(baseName))
	a.mu.Lock()
	defer a.mu.Unlock()
	book, err := a.bookLocked()
	if err != nil {
		return ClaimedAddress{}, false
	}
	return book.find(baseName, accountID)
}

// touchClaimed оновлює LastUsedAt адреси в книзі (при повторному
// використанні).
func (a *API) touchClaimed(subdomain string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	book, err := a.bookLocked()
	if err != nil {
		return
	}
	book.touch(subdomain, time.Now())
	_ = a.saveBookLocked()
}

// bindClaimed закріплює адресу за конкретним сервером (label) і оновлює
// час використання. Саме мітка пов'язує збережену адресу з сервером, тож
// після перезапуску лаунчера віджет цього сервера одразу підхопить свою
// адресу, а не проситиме зайняти нову.
func (a *API) bindClaimed(subdomain, label string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	book, err := a.bookLocked()
	if err != nil {
		return
	}
	book.touch(subdomain, time.Now())
	book.bind(subdomain, label)
	_ = a.saveBookLocked()
}

// AssignClaimedAddress закріплює вже зайняту адресу за конкретним сервером
// (label). Використовується, коли гравець у списку збережених адрес обрав
// AssignClaimedAddress призначає вже застовбену адресу (з книги) конкретному
// серверу — коли гравець хоче використати для цього сервера не «свою»
// базову назву, а іншу вже зайняту адресу. Адреса належить ЛАУНЧЕРУ
// (owner-secret, спільний для всіх акаунтів цієї інсталяції), а не
// окремому акаунту, тож перевірки власності акаунта тут більше немає —
// див. коментар над addressBook.find() у store.go.
func (a *API) AssignClaimedAddress(subdomain, label, accountID string) error {
	subdomain = strings.ToLower(strings.TrimSpace(subdomain))
	if subdomain == "" {
		return fmt.Errorf("p2p: не вказано адресу")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	book, err := a.bookLocked()
	if err != nil {
		return err
	}
	addr, ok := book.get(subdomain)
	if !ok {
		return fmt.Errorf("p2p: адреси %s немає у книзі — спочатку застовбіть її", subdomain)
	}
	if IsTempAddress(addr) {
		return fmt.Errorf("p2p: %s — тимчасова адреса запрошення, закріпити її за сервером не можна", subdomain)
	}
	// Власність підтверджується секретом лаунчера (сервер), а не поточним акаунтом: адреса,
	// застовбана під іншим акаунтом цього ж лаунчера, призначається без обмежень.
	book.bind(subdomain, label)
	book.touch(subdomain, time.Now())
	return a.saveBookLocked()
}

// forgetClaimedErr — як forgetClaimed, але повертає помилку збереження книги.
// Порівняння без урахування регістру.
func (a *API) forgetClaimedErr(subdomain string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	book, err := a.bookLocked()
	if err != nil {
		return fmt.Errorf("p2p: книга адрес недоступна: %w", err)
	}
	if book.remove(subdomain) {
		if err := a.saveBookLocked(); err != nil {
			return fmt.Errorf("p2p: не вдалося зберегти книгу адрес: %w", err)
		}
	}
	return nil
}

// forgetClaimed прибирає адресу з книги (явний Release).
func (a *API) forgetClaimed(subdomain string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	book, err := a.bookLocked()
	if err != nil {
		return
	}
	if book.remove(subdomain) {
		_ = a.saveBookLocked()
	}
}

// ── Host ────────────────────────────────────────────────────────────────

// StartP2PHost піднімає хостинг LAN-світу на вказаному порту під уже
// застовбленою адресою.
//
// TCP-порт = minecraftPort (порт сервера). udpPort — порт UDP-тунелю
// (0 = UDP вимкнено); UI ставить порт сервера за замовчуванням, користувач
// може змінити на будь-який.
// accountID/ownerName — для прив'язки адреси й показу власника в адмінці.
//
// Якщо lobby відповів 404 (адреса протухла) — автоматично займаємо свіжу
// адресу з тією самою базовою назвою і пробуємо ще раз: гравцю не потрібно
// вручну перезаймати адресу після 30-денної паузи.
func (a *API) StartP2PHost(minecraftPort, subdomain string, udpPort int, accountID, ownerName string) (HostStatus, error) {
	if err := validateHostPorts(minecraftPort, udpPort); err != nil {
		return HostStatus{Error: err.Error()}, err
	}
	subdomain = strings.ToLower(strings.TrimSpace(subdomain))
	if subdomain == "" {
		return HostStatus{}, fmt.Errorf("p2p: не вказано адресу — спочатку викличте ClaimP2PAddress")
	}

	a.mu.Lock()
	if _, already := a.hosts[subdomain]; already {
		a.mu.Unlock()
		return HostStatus{}, fmt.Errorf("p2p: %s вже хоститься — спочатку зупиніть її", subdomain)
	}
	if len(a.hosts) >= MaxConcurrentRooms {
		count := len(a.hosts)
		a.mu.Unlock()
		return HostStatus{}, fmt.Errorf("p2p: досягнуто ліміту %d одночасних P2P-серверів (зараз активно: %d) — спочатку зупиніть один із них", MaxConcurrentRooms, count)
	}
	a.mu.Unlock()

	secret, err := a.ownerSecretValue()
	if err != nil {
		return HostStatus{}, fmt.Errorf("p2p: локальний секрет: %w", err)
	}

	session, err := a.startLeasedSession(minecraftPort, subdomain, udpPort, secret, ownerName)
	if err != nil {
		// Адреса протухла в lobby — займаємо свіжу з тією ж базою і
		// повторюємо один раз. Секрет і акаунт ті самі, тож для гравця це
		// виглядає як «просто запрацювало».
		if errors.Is(err, ErrAddressNotFound) {
			fresh, ferr := a.reclaimAddress(subdomain, accountID)
			if ferr != nil {
				return HostStatus{}, err
			}
			session, err = a.startLeasedSession(minecraftPort, fresh, udpPort, secret, ownerName)
			if err != nil {
				return HostStatus{}, err
			}
			subdomain = fresh
		} else {
			return HostStatus{}, err
		}
	}

	a.mu.Lock()
	// Друга перевірка ліміту/дублікату під замком — на випадок гонки.
	if _, already := a.hosts[subdomain]; already || len(a.hosts) >= MaxConcurrentRooms {
		a.mu.Unlock()
		session.Close()
		return HostStatus{}, fmt.Errorf("p2p: %s вже хоститься або досягнуто ліміту %d — спробуйте ще раз", subdomain, MaxConcurrentRooms)
	}
	a.hosts[subdomain] = session
	count := len(a.hosts)
	a.mu.Unlock()

	a.touchClaimed(subdomain)

	return HostStatus{
		Active:      true,
		Subdomain:   subdomain,
		Address:     session.Address,
		UDPPort:     session.UDPPort(),
		UDPError:    errString(session.UDPError()),
		Disabled:    session.Disabled,
		Access:      session.Access(),
		ActiveCount: count,
		MaxCount:    MaxConcurrentRooms,
	}, nil
}

// startLeasedSession бере оренду кімнати (host-start) і піднімає хост-сесію з її токеном. Для кімнати без
// співхостів оренда не потрібна (shared=false → токен порожній). 409 p2p_room_busy повертається як помилка
// «хостить <імʼя>». Якщо підйом не вдався — оренду віддаємо назад.
func (a *API) startLeasedSession(minecraftPort, subdomain string, udpPort int, secret, ownerName string) (*HostSession, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	lease, err := a.lobbyClient().HostStart(ctx, subdomain, secret)
	cancel()
	if err != nil {
		if le, ok := AsLobbyError(err); ok && errors.Is(err, ErrRoomBusy) && le.HostName != "" {
			return nil, fmt.Errorf("p2p: кімнату вже хостить %s", le.HostName)
		}
		return nil, err
	}
	token := ""
	if lease.Shared {
		token = lease.SessionToken
	}
	session, err := a.startHostSession(minecraftPort, subdomain, udpPort, secret, ownerName, token)
	if err != nil && token != "" {
		rctx, rcancel := context.WithTimeout(context.Background(), 8*time.Second)
		_ = a.lobbyClient().HostStop(rctx, subdomain, token)
		rcancel()
	}
	return session, err
}

// handleHostRevoked — вузол забрав хост-роль: зупиняємо тунель і повідомляємо UI.
func (a *API) handleHostRevoked(s *HostSession, cause error) {
	reason := "lease_lost"
	switch {
	case errors.Is(cause, ErrHostRevoked), errors.Is(cause, ErrOwnerOnly):
		reason = "revoked"
	case errors.Is(cause, ErrRoomBusy):
		reason = "room_busy"
	}
	a.dropHost(s, reason)
}

// RevokeHostByAddress — WS-кадр p2p_host_revoked: негайно зупиняє тунель кімнати (address — з суфіксом
// або без).
func (a *API) RevokeHostByAddress(address, reason string) {
	address = strings.ToLower(strings.TrimSpace(address))
	if address == "" {
		return
	}
	a.mu.Lock()
	var target *HostSession
	for sub, h := range a.hosts {
		if address == sub || address == strings.ToLower(h.Address) || strings.HasPrefix(address, sub+".") {
			target = h
			break
		}
	}
	a.mu.Unlock()
	if target != nil {
		if reason == "" {
			reason = "revoked"
		}
		a.dropHost(target, reason)
	}
}

func (a *API) dropHost(s *HostSession, reason string) {
	a.mu.Lock()
	if cur, ok := a.hosts[s.Subdomain]; !ok || cur != s {
		a.mu.Unlock()
		return
	}
	delete(a.hosts, s.Subdomain)
	cb := a.OnHostStopped
	a.mu.Unlock()
	// Роль уже не наша: offline/stop не робимо (стер би IP нового хоста), лише гасимо ноду.
	go func() {
		_ = s.Close()
		if cb != nil {
			cb(s.Subdomain, reason)
		}
	}()
}

// startHostSession — один підйом хост-сесії (auth key + StartHost).
func (a *API) startHostSession(minecraftPort, subdomain string, udpPort int, secret, ownerName, sessionToken string) (*HostSession, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()

	authKey, err := a.lobbyClient().RequestAuthKey(ctx)
	if err != nil {
		return nil, fmt.Errorf("p2p: не вдалося отримати доступ до мережі: %w", err)
	}

	return StartHost(ctx, HostConfig{
		MinecraftPort: minecraftPort,
		StateDir:      a.stateDir(),
		ControlURL:    ControlURL,
		AuthKey:       authKey,
		AuthKeyFunc: func(ctx context.Context) (string, error) {
			return a.lobbyClient().RequestAuthKey(ctx)
		},
		LobbyBaseURL: a.baseURL(),
		LobbyToken:   a.token(),
		LobbyAuth:    a.auth,
		Subdomain:    subdomain,
		OwnerSecret:  secret,
		OwnerName:    ownerName,
		UDPPort:      udpPortString(udpPort),
		SessionToken: sessionToken,
		OnRevoked:    a.handleHostRevoked,
	})
}

// reclaimAddress займає свіжу адресу з тією самою базовою назвою (для
// авто-відновлення після 404) і оновлює локальну книгу.
//
// Стару адресу на сервері явно звільняємо (best-effort): інакше вона
// (хоч і 404-ить на host-update/join) лишається в D1 з давнім
// last_active_at і ще до 30 днів рахується проти MAX_ACTIVE_CLAIMS цього
// ownerSecret. Кожне таке "тихе" авто-перезайняття без release назавжди
// займало один із 5 слотів — саме це й призводило до "p2p: забагато
// спроб" на цілком новому /p2p/claim, хоч активна була лише одна адреса.
func (a *API) reclaimAddress(oldSubdomain, accountID string) (string, error) {
	baseName := baseNameOf(oldSubdomain)
	secret, err := a.ownerSecretValue()
	if err != nil {
		return "", fmt.Errorf("p2p: локальний секрет: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	resp, err := a.lobbyClient().ClaimAddress(ctx, baseName, secret)
	if err != nil {
		return "", fmt.Errorf("p2p: не вдалося перезайняти адресу: %w", err)
	}

	// Best-effort: стара адреса вже 404-ила (протухла/видалена), тож
	// release тут майже завжди no-op на сервері. Якщо він сам впаде
	// (мережа, чи запис уже зник) — не валимо весь reclaim через це,
	// інакше гравець втратив би щойно отриману нову адресу через
	// побічну помилку прибирання старої.
	releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 8*time.Second)
	if rerr := a.lobbyClient().Release(releaseCtx, oldSubdomain, secret); rerr != nil {
		launchlog.Error("p2p-reclaim", "не вдалося звільнити протухлу адресу %s: %v", oldSubdomain, rerr)
	}
	releaseCancel()

	a.mu.Lock()
	book, berr := a.bookLocked()
	if berr == nil {
		book.remove(oldSubdomain)
		now := time.Now().UnixMilli()
		book.upsert(ClaimedAddress{
			Subdomain:  resp.Subdomain,
			BaseName:   baseName,
			AccountID:  accountID,
			CreatedAt:  now,
			LastUsedAt: now,
		})
		_ = a.saveBookLocked()
	}
	a.mu.Unlock()
	return resp.Subdomain, nil
}

// baseNameOf відрізає випадковий цифровий суфікс ("prlool-482917" → "prlool").
func baseNameOf(subdomain string) string {
	if i := strings.LastIndex(subdomain, "-"); i > 0 {
		return subdomain[:i]
	}
	return subdomain
}

// udpPortString — 0 = «не задано» (порожній рядок). Значення поза 1..65535
// сюди не доходять: validateHostPorts відхиляє їх ДО запуску з явною
// помилкою, а не мовчки вимикає UDP.
func udpPortString(port int) string {
	if port == 0 {
		return ""
	}
	return strconv.Itoa(port)
}

// validateHostPorts — строга перевірка портів з UI. Жодних тихих підмін:
// некоректний порт = помилка користувачу.
func validateHostPorts(minecraftPort string, udpPort int) error {
	mp, err := strconv.Atoi(strings.TrimSpace(minecraftPort))
	if err != nil || mp < 1 || mp > 65535 {
		return fmt.Errorf("p2p: некоректний TCP-порт %q (потрібно 1..65535)", minecraftPort)
	}
	if udpPort < 0 || udpPort > 65535 {
		return fmt.Errorf("p2p: некоректний UDP-порт %d (потрібно 1..65535, 0 = вимкнено)", udpPort)
	}
	return nil
}

// LocalHostPort повертає локальний TCP-порт гри, якщо цю адресу хостить
// САМЕ цей лаунчер (той самий ПК). Використовується гостьовим слухачем,
// щоб піти напряму на 127.0.0.1:<порт>, коли гравець відкриває свою ж
// P2P-адресу, а не крутити пакети через tsnet «до себе».
func (a *API) LocalHostPort(subdomain string) (string, bool) {
	subdomain = strings.ToLower(strings.TrimSpace(subdomain))
	a.mu.Lock()
	defer a.mu.Unlock()
	host, ok := a.hosts[subdomain]
	if !ok || host.LocalPort == "" {
		return "", false
	}
	return host.LocalPort, true
}

// LocalHostUDPPort — UDP-порт голосу, який публікує ЛОКАЛЬНА хост-сесія цієї
// адреси (той самий ПК). Порожньо/false — адреса чужа або голос вимкнено.
// Гість по ньому спрямовує голос напряму на 127.0.0.1:<порт> (без tsnet).
func (a *API) LocalHostUDPPort(subdomain string) (string, bool) {
	subdomain = strings.ToLower(strings.TrimSpace(subdomain))
	a.mu.Lock()
	defer a.mu.Unlock()
	host, ok := a.hosts[subdomain]
	if !ok {
		return "", false
	}
	if p := host.UDPPort(); p != "" {
		return p, true
	}
	return "", false
}

// StopP2PHost закриває конкретну хост-сесію за її subdomain (якщо є).
// Адреса лишається застовбленою (і в lobby, і в локальній книзі) — щоб
// звільнити, використовуйте ReleaseP2PAddress.
func (a *API) StopP2PHost(subdomain string) error {
	subdomain = strings.ToLower(strings.TrimSpace(subdomain))
	a.mu.Lock()
	host, ok := a.hosts[subdomain]
	if !ok {
		a.mu.Unlock()
		return nil
	}
	delete(a.hosts, subdomain)
	a.mu.Unlock()

	return a.stopHostSession(host)
}

// HasActiveSessions — чи лаунчер зараз обслуговує P2P: хоститься хоч одна кімната або гостьовий
// слухач тримає живі кімнати. Тоді закриття вікна має ховати лаунчер у трей, а не гасити процес.
func (a *API) HasActiveSessions() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.hosts) > 0 {
		return true
	}
	return a.guest != nil && a.guest.ActiveRoomCount() > 0
}

// StopAllP2PHosts зупиняє УСІ активні хостингові сесії (закриття лаунчера).
func (a *API) StopAllP2PHosts() error {
	a.mu.Lock()
	hosts := a.hosts
	a.hosts = make(map[string]*HostSession)
	a.mu.Unlock()

	var firstErr error
	for _, host := range hosts {
		if err := a.stopHostSession(host); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// stopHostSession — спершу повідомляємо лобі «IP більше не дійсний», і лише
// потім гасимо tsnet-ноду. Best-effort.
func (a *API) stopHostSession(host *HostSession) error {
	if host.Subdomain != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		if tok := host.SessionToken(); tok != "" {
			_ = a.lobbyClient().HostStop(ctx, host.Subdomain, tok) // віддає оренду і стирає IP
		} else if secret, err := a.ownerSecretValue(); err == nil {
			_ = a.lobbyClient().HostOffline(ctx, host.Subdomain, secret)
		}
		cancel()
	}
	return host.Close()
}

// GetP2PHostStatus повертає стан хостингу для конкретної адреси.
func (a *API) GetP2PHostStatus(subdomain string) HostStatus {
	subdomain = strings.ToLower(strings.TrimSpace(subdomain))
	a.mu.Lock()
	defer a.mu.Unlock()
	host, ok := a.hosts[subdomain]
	count := len(a.hosts)
	if !ok {
		// Навіть коли хостинг не запущено, показуємо ПОВНУ адресу: гравець
		// має бачити, яку саме адресу він роздаватиме, ще до старту (раніше
		// адреса з'являлась лише після StartP2PHost — виглядало як «адреса
		// загубилась»).
		st := HostStatus{Active: false, Subdomain: subdomain, ActiveCount: count, MaxCount: MaxConcurrentRooms}
		if subdomain != "" {
			st.Address = subdomain + "." + PlaySuffix
		}
		return st
	}
	return HostStatus{
		Active:      true,
		Subdomain:   host.Subdomain,
		Address:     host.Address,
		UDPPort:     host.UDPPort(),
		UDPError:    errString(host.UDPError()),
		Disabled:    host.Disabled,
		Access:      host.Access(),
		PlayerCount: len(host.Connections()),
		ActiveCount: count,
		MaxCount:    MaxConcurrentRooms,
	}
}

// ListP2PHosts повертає стан УСІХ активних хостингових сесій (мультихостинг).
func (a *API) ListP2PHosts() []HostStatus {
	a.mu.Lock()
	defer a.mu.Unlock()
	count := len(a.hosts)
	out := make([]HostStatus, 0, count)
	for _, host := range a.hosts {
		out = append(out, HostStatus{
			Active:      true,
			Subdomain:   host.Subdomain,
			Address:     host.Address,
			UDPPort:     host.UDPPort(),
			UDPError:    errString(host.UDPError()),
			Disabled:    host.Disabled,
			Access:      host.Access(),
			PlayerCount: len(host.Connections()),
			ActiveCount: count,
			MaxCount:    MaxConcurrentRooms,
		})
	}
	return out
}

// HostConnections повертає, ХТО зараз під'єднаний до конкретного хоста і з
// яким пінгом (для UI власника: "у мене в мережі: Steve, 42 ms").
func (a *API) HostConnections(subdomain string) []HostConnection {
	a.mu.Lock()
	host := a.hosts[strings.ToLower(strings.TrimSpace(subdomain))]
	a.mu.Unlock()
	if host == nil {
		return nil
	}
	return host.Connections()
}

// ── UDP-тунель гостя (голосовий чат) ─────────────────────────────────
// Гість нічого не вмикає і не налаштовує: щойно кімната, до якої він
// заходить, повідомляє, що хост опублікував UDP (room.UDPPort), гість
// сам піднімає локальні слухачі (guest.go: setLastRoom → ensureVoiceAuto
// / ensureTunnelListener). Лишається тільки статус для UI-діагностики.

// P2PUDPStatus — стан UDP-тунелю для UI гостя.
type P2PUDPStatus struct {
	Enabled bool `json:"enabled"`
	// VoicePort — локальний порт голосового моду (куди шле мод).
	VoicePort int `json:"voicePort"`
	// TunnelPort — порт тунелю (дорівнює порту сервера хоста).
	TunnelPort int `json:"tunnelPort,omitempty"`
	// RoomAddress — "<hostIP>:<tunnelPort>" останньої кімнати.
	RoomAddress string `json:"roomAddress,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

// SetP2PUDP вмикає/вимикає UDP-тунель голосового чату для гостьової
// сторони (галочка в UI) на явно вказаному порту. При enabled=true
// гарантує, що гостьовий сервіс піднятий, і перемикає слухач на
// voicePort; при enabled=false — просто гасить UDP-слухачі.
func (a *API) SetP2PUDP(enabled bool, voicePort int) error {
	if enabled {
		a.mu.Lock()
		err := a.ensureGuestLocked()
		guest := a.guest
		a.mu.Unlock()
		if err != nil {
			return err
		}
		return guest.SetVoicePort(true, voicePort)
	}

	a.mu.Lock()
	guest := a.guest
	a.mu.Unlock()
	if guest == nil {
		return nil
	}
	return guest.SetVoicePort(false, voicePort)
}

// GetP2PUDPStatus повертає стан UDP-тунелю гостя для UI.
func (a *API) GetP2PUDPStatus() P2PUDPStatus {
	a.mu.Lock()
	guest := a.guest
	a.mu.Unlock()
	if guest == nil {
		return P2PUDPStatus{}
	}

	on, voicePort, tunnelPort := guest.UDPStatus()
	if !on {
		return P2PUDPStatus{Enabled: false, Reason: "голос ще не тунелюється: зайдіть у гру по P2P-адресі — якщо хост публікує UDP, тунель піднімається автоматично"}
	}

	st := P2PUDPStatus{Enabled: true, VoicePort: voicePort, TunnelPort: tunnelPort}
	if room := guest.lastRoomSnapshot(); room != nil && room.HostTailscaleIP != "" && room.UDPPort != "" {
		st.RoomAddress = room.HostTailscaleIP + ":" + room.UDPPort
	} else {
		st.Reason = "голос поки не тунелюється: або хост цього сервера не публікує UDP, або ще не заходили в гру по P2P-адресі"
	}
	return st
}

// ReleaseP2PAddress явно звільняє адресу (гравець видаляє свій сервер) — і
// в lobby, і в локальній книзі, щоб вона не «воскресла» при наступному
// claim.
func (a *API) ReleaseP2PAddress(subdomain string) error {
	subdomain = strings.ToLower(strings.TrimSpace(subdomain))
	if subdomain == "" {
		return fmt.Errorf("p2p: не вказано адресу")
	}

	// 1) Зупиняємо хост, якщо він працює на цій адресі.
	a.mu.Lock()
	host, hosted := a.hosts[subdomain]
	if hosted {
		delete(a.hosts, subdomain)
	}
	a.mu.Unlock()
	if hosted {
		_ = a.stopHostSession(host)
	}

	// 2) ЛОКАЛЬНЕ видалення — першим і незалежно від мережі/секрету: раніше будь-яка
	// помилка lobby (таймаут, 403 після зміни секрету, 404 «вже немає») переривала дію, і
	// адреса «не видалялась». Тепер вона зникає з книги гарантовано.
	if err := a.forgetClaimedErr(subdomain); err != nil {
		return err
	}

	// 3) Звільнення на lobby — best-effort у фоні (до 15с), щоб UI не чекав. Якщо адреси
	// там уже немає (404) чи секрет не підходить — це не помилка для користувача:
	// локальний запис видалено, а протухла адреса на вузлі сама спливе за TTL.
	secret, serr := a.ownerSecretValue()
	if serr == nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			_ = a.lobbyClient().Release(ctx, subdomain, secret)
		}()
	}
	return nil
}

// ── Guest ───────────────────────────────────────────────────────────────

// EnsureP2PGuestService піднімає фоновий гостьовий слухач, якщо він ще не
// запущений. Викликається один раз при старті лаунчера.
func (a *API) EnsureP2PGuestService() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.ensureGuestLocked()
}

// ensureGuestLocked — власне запуск. Виклик ЛИШЕ з-під a.mu.
func (a *API) ensureGuestLocked() error {
	if a.guestStarted {
		return a.guestErr
	}
	a.guest = NewGuestService(GuestConfig{
		StateDir:     a.stateDir(),
		ControlURL:   ControlURL,
		PlaySuffix:   PlaySuffix,
		LobbyBaseURL: a.baseURL(),
		LobbyToken:   a.token(),
		LobbyAuth:    a.auth,
		AuthKeyFunc: func(ctx context.Context) (string, error) {
			return a.lobbyClient().RequestAuthKey(ctx)
		},
		LocalHostPort:    a.LocalHostPort,
		LocalHostUDPPort: a.LocalHostUDPPort,
	})
	a.guestErr = a.guest.Start()
	// Прапорець ставимо навіть при помилці (напр. порт зайнятий).
	a.guestStarted = true
	return a.guestErr
}

// StopP2PGuestService зупиняє гостьовий сервіс. Після цього слухач МОЖНА
// підняти назад — стан повністю скидається.
func (a *API) StopP2PGuestService() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.stopGuestLocked()
}

func (a *API) stopGuestLocked() error {
	guest := a.guest
	a.guest = nil
	a.guestStarted = false
	a.guestErr = nil
	if guest == nil {
		return nil
	}
	return guest.Stop()
}

// SetGuestEnabled застосовує вимикач у налаштуваннях НА ХОДУ.
func (a *API) SetGuestEnabled(enabled bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !enabled {
		return a.stopGuestLocked()
	}
	return a.ensureGuestLocked()
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// ── Жива діагностика та гаряче перемикання UDP ──────────────────────────

// HostLive — жива картина хоста (див. health.go). withSelfCheck додає запит до lobby
// («чи резолвиться моя адреса так, як бачить гість»); для частого опитування передавай false.
func (a *API) HostLive(subdomain string, withSelfCheck, force bool) HostLive {
	subdomain = strings.ToLower(strings.TrimSpace(subdomain))
	a.mu.Lock()
	host := a.hosts[subdomain]
	a.mu.Unlock()
	if host == nil {
		return HostLive{Active: false, Subdomain: subdomain}
	}
	return host.Live(withSelfCheck, force)
}

// SetHostUDP вмикає/вимикає/перемикає UDP-тунель голосу НА ЗАПУЩЕНОМУ хості — без
// перезапуску тунелю (udpPort 0 = вимкнути). Якщо хост не запущено — це не помилка: порт
// просто підхопиться наступним StartP2PHost зі збережених налаштувань.
func (a *API) SetHostUDP(subdomain string, udpPort int) (HostStatus, error) {
	if udpPort < 0 || udpPort > 65535 {
		return HostStatus{}, fmt.Errorf("p2p: некоректний UDP-порт %d (потрібно 1..65535, 0 = вимкнено)", udpPort)
	}
	subdomain = strings.ToLower(strings.TrimSpace(subdomain))
	a.mu.Lock()
	host := a.hosts[subdomain]
	a.mu.Unlock()
	if host == nil {
		return a.GetP2PHostStatus(subdomain), nil
	}
	if err := host.SetUDP(udpPortString(udpPort)); err != nil {
		return a.GetP2PHostStatus(subdomain), err
	}
	return a.GetP2PHostStatus(subdomain), nil
}
