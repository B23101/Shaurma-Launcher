//go:build shaurma

package main

// Друзі Shaurma (ФАЗА 1 SOCIAL_MASTERPLAN.md, без чату) — тонка обгортка над
// internal/social. Уся логіка, санітизація й тести — там; тут лише:
//   - звʼязок із коміркою акаунта Шаурми активного акаунта лаунчера (social_identity_shaurma.go);
//   - життєвий цикл (старт при вході, стоп при виході);
//   - методи, які бачить фронтенд.
//
// Це shaurma-only: у base-збірці скін-акаунта (а отже й JWT) немає.

import (
	"shaurma-launcher-backend/internal/crashguard"
	"fmt"
	"strings"
	"sync"

	"shaurma-launcher-backend/internal/social"
)

// socialWSURL — WebSocket shaurma-social (Cloudflare Tunnel перед VPS).
// ТІЛЬКИ wss://: на ньому лежить JWT гравця (перший кадр), тож відкритий
// транспорт неприпустимий — PresenceClient.checkURL відхилить будь-що інше.
//
// ЗВІРТЕ ШЛЯХ із вашим shaurma-social: тут «/ws» — припущення (код VPS-сервісу
// не входив до архіву).
const socialWSURL = "wss://social.shaurma-launcher.org/ws"

var (
	socialMu  sync.Mutex
	socialSvc *social.Service
	// socialKey — ключ особистості, під якою створено socialSvc («google»,
	// «ms-<launcherAccountId>» або ""). Якщо активний акаунт змінився —
	// сервіс замінюється.
	socialKey string
)

// currentSocialKeyLocked — ключ особистості поточного сервісу (під замком).
func (a *App) currentSocialKeyLocked() string {
	socialMu.Lock()
	defer socialMu.Unlock()
	return socialKey
}

// socialService повертає сервіс ПОТОЧНОЇ особистості. Якщо активний акаунт
// змінився (інший ліцензійний, або перейшли на офлайн/Google) — старий
// сервіс зупиняється, а пам'ять про друзів і чат стирається: списки двох
// акаунтів ніколи не змішуються.
func (a *App) socialService() *social.Service {
	id := a.currentSocialIdent()

	socialMu.Lock()
	if socialSvc != nil && socialKey == id.key {
		svc := socialSvc
		socialMu.Unlock()
		return svc
	}
	old := socialSvc
	svc := social.NewService(social.Deps{
		Request: a.socialRequestFor(id),
		Token:   a.socialTokenFor(id),
		HWID:    shaurmaHWID, // токен, вкрадений на інший ПК, WebSocket не відкриє
		WSURL:   socialWSURLNow(),
		// Ключі чату — окремо на кожну особистість (див. social_identity_shaurma.go).
		KeyDir: a.socialKeyDirFor(id),
		// «Простий» режим: ключ чату зберігає сервер (зашифрованим), тож новий ПК, вихід з пристроїв
		// і перевстановлення ОС історію чатів не ламають.
		KeyServer: a.chatKeyServerFor(id),
		// Тут ЛИШЕ пересилка події у фронт. Другий споживач — віджет сповіщень
		// у трей (notifyFromSocial): він мусить бачити подію ПІСЛЯ того, як
		// отримав її головне вікно, тож пересилаємо саме тут, в одному місці.
		Emit: func(event string, payload any) {
			a.emit(event, payload)
			// Вузол забрав хост-роль (kick/відкликання співхоста) — зупиняємо тунель негайно,
			// не чекаючи 403 на наступному heartbeat (≤15 с).
			if ev, ok := payload.(social.P2PHostRevokedEvent); ok && event == "p2p:host_revoked" && a.p2pAPI != nil {
				a.p2pAPI.RevokeHostByAddress(ev.Address, ev.Reason)
			}
			// Службові повідомлення передач збірок (відмова/скасування) — share_shaurma.go.
			a.shareOnSocial(event, payload)
			a.notifyFromSocial(event, payload)
		},
		// Віддалене керування серверами друзів: усі кадри rs_* йдуть у remote_control_shaurma.go.
		Remote: a.remoteHooks(),
		// Логи без токенів і без вмісту: пакет social сам нічого чутливого не пише.
		Logf: func(string, ...any) {},
	})
	st := a.cfg.GetSettings()
	mode := st.PresenceMode
	if mode == "" && st.NotifyDnd {
		mode = "dnd"
	}
	svc.SetManualPresence(mode)
	socialSvc = svc
	socialKey = id.key
	socialMu.Unlock()

	if old != nil {
		old.Stop() // поза замком: Stop може емітити події
		notifySeenInvites.mu.Lock()
		notifySeenInvites.m = map[string]bool{}
		notifySeenInvites.mu.Unlock()
		// Інший акаунт: сервери друзів і консолі попереднього тут не показуємо.
		a.remoteOnIdentityChanged()
	}
	return svc
}

// startSocialIfLoggedIn запускає соціальну систему, якщо є з-під чого
// (Microsoft- або Google-особистість). Безпечно викликати скільки завгодно.
func (a *App) startSocialIfLoggedIn() {
	if a.currentSocialIdent().key == "" {
		return
	}
	svc := a.socialService()
	if svc.Running() {
		return
	}
	_ = svc.Start() // помилка = некоректний WS URL; тоді просто лишаємось без presence
	go a.remoteSyncShares() // VPS нічого не пам'ятає між сесіями: публікуємо дозволи й ключ каналу знову
}

// socialOnAccountChanged — активний акаунт лаунчера змінився: одразу
// перемикаємо особистість і віддаємо фронтенду список НОВОГО акаунта.
func (a *App) socialOnAccountChanged() {
	go func() {
		defer crashguard.Recover("social_friends_shaurma.go:108")
		st := a.GetFriendsState()
		a.emit("friends:update", st)
	}()
}

// stopSocial — вихід з акаунта: зупинити й стерти стан у памʼяті.
func (a *App) stopSocial() {
	socialMu.Lock()
	svc := socialSvc
	socialMu.Unlock()
	if svc != nil {
		svc.Stop()
	}
	a.remoteOnIdentityChanged()
}

// ── Хуки присутності (викликаються поруч із Discord RPC) ─────────────────

func (a *App) socialGameStarted(id, name, color string) {
	a.socialService().GameStarted(id, name, color)
}

func (a *App) socialGameExited(id string) {
	socialMu.Lock()
	svc := socialSvc
	socialMu.Unlock()
	if svc != nil {
		svc.GameExited(id)
	}
}

// ── Методи для фронтенду ─────────────────────────────────────────────────

// GetFriendsState — знімок для UI. Якщо сервіс ще не запущено, а вхід є —
// запускає (напр. після оновлення лаунчера). Без входу повертає loggedIn=false.
func (a *App) GetFriendsState() social.State {
	a.startSocialIfLoggedIn()
	id := a.currentSocialIdent()
	st := a.socialService().Snapshot()
	if id.key == "" {
		// Акаунт без комірки (лише нік): ні друзів, ні чату, ні P2P, ні скінів.
		st.LoggedIn = false
		st.Diag = "no_shaurma_account"
		return st
	}
	if st.LoggedIn {
		st.Me = a.socialMeFor(id)
	}
	return st
}

// RefreshFriends — примусове перечитування (кнопка «Оновити»).
func (a *App) RefreshFriends() social.State {
	a.startSocialIfLoggedIn()
	if svc := a.socialService(); svc.Running() {
		svc.Refresh()
	}
	return a.GetFriendsState()
}

// SendFriendInvite — запит у друзі за ID акаунта Шаурми (унікальний; нік не унікальний,
// тому за ним друзів не шукають).
func (a *App) SendFriendInvite(accountID string) error {
	a.startSocialIfLoggedIn()
	return a.socialService().SendInvite(accountID)
}

// SendFriendInviteByHandle — запит у друзі за @handle (публічний унікальний ідентифікатор).
func (a *App) SendFriendInviteByHandle(handle string) error {
	a.startSocialIfLoggedIn()
	return a.socialService().SendInviteByHandle(handle)
}

// RespondFriendInvite — прийняти/відхилити вхідний запит.
func (a *App) RespondFriendInvite(inviteID string, accept bool) error {
	return a.socialService().Respond(inviteID, accept)
}

// ListSentFriendInvites — мої ВИХІДНІ запити в друзі (чекають відповіді); скасовуються
// через RespondFriendInvite(id, false). Поля from* у відповіді — адресат запиту.
func (a *App) ListSentFriendInvites() ([]social.Invite, error) {
	return a.socialService().SentInvites()
}

// RemoveFriend — видалити друга.
func (a *App) RemoveFriend(accountID string) error {
	if err := a.socialService().Remove(accountID); err != nil {
		return err
	}
	a.remoteOnFriendRemoved(accountID)
	return nil
}

// SetFriendsPrivacy — зберегти приватність (три «друзівські» перемикачі).
func (a *App) SetFriendsPrivacy(p social.Privacy) error {
	return a.socialService().SetPrivacy(p)
}

// ── Чат ────────────────────────────────────────────────────────────
//
// Текст шифрується ЛОКАЛЬНО в internal/social; сервер бачить лише ciphertext.
// Події: chat:message / chat:ack / chat:typing / chat:read / chat:server_invite /
// chat:deleted (видалено для всіх) / chat:edited (змінено текст) / chat:pins (змінились закріплені) /
// chat:reactions (змінились реакції одного повідомлення).

// SendChatMessage надсилає повідомлення другові. Повертає clientMessageId
// (за ним UI зіставляє оптимістичну бульбашку з підтвердженням chat:ack).
func (a *App) SendChatMessage(peerAccountID, text string) (string, error) {
	a.startSocialIfLoggedIn()
	return a.socialService().SendChatMessage(peerAccountID, text)
}

// RetryChatMessage — «Повторити» з ТИМ САМИМ clientMessageId (VPS дедуплікує: дубля не буде).
func (a *App) RetryChatMessage(peerAccountID, text, clientMessageID string) (string, error) {
	a.startSocialIfLoggedIn()
	return a.socialService().SendChatMessageWithID(peerAccountID, text, clientMessageID)
}

// SendChatMessageReply — повідомлення-відповідь на конкретне повідомлення (як у Telegram).
// replyToID = id повідомлення, на яке відповідаємо (0 = звичайне повідомлення).
func (a *App) SendChatMessageReply(peerAccountID, text string, replyToID int64) (string, error) {
	a.startSocialIfLoggedIn()
	return a.socialService().SendChatMessageReply(peerAccountID, text, replyToID)
}

// ReactChatMessage — реакція на повідомлення (один емодзі на акаунт, як у Telegram:
// повтор того самого прибирає, інша — замінює). Порожній emoji = прибрати свою реакцію.
// Подія chat:reactions несе повний актуальний список реакцій повідомлення.
func (a *App) ReactChatMessage(peerAccountID string, messageID int64, emoji string) error {
	a.startSocialIfLoggedIn()
	return a.socialService().ReactChatMessage(peerAccountID, messageID, emoji)
}

// GetChatSendState — справжній стан відправки: pending | acked | failed | unknown. UI показує
// «не надіслано» лише при failed.
func (a *App) GetChatSendState(clientMessageID string) social.SendState {
	return a.socialService().ChatSendState(clientMessageID)
}

// GetChatHistory повертає сторінку історії (уже розшифровану локально).
// before=0 → остання сторінка. Ходить у Worker, тож викликати на вимогу
// (відкриття діалогу / «докрутити»), а не за таймером.
func (a *App) GetChatHistory(peerAccountID string, before int64, limit int) (social.ChatPage, error) {
	a.startSocialIfLoggedIn()
	return a.socialService().ChatHistory(peerAccountID, before, limit)
}

// GetChatHistoryAfter — сторінка НОВІШИХ за after (unix мс) повідомлень, за зростанням. Для
// прокрутки вниз після переходу до старого повідомлення (закріплене тощо).
func (a *App) GetChatHistoryAfter(peerAccountID string, after int64, limit int) (social.ChatPage, error) {
	a.startSocialIfLoggedIn()
	return a.socialService().ChatHistoryAfter(peerAccountID, after, limit)
}

// DeleteChatMessage — видалити СВОЄ повідомлення в обох: воно зникає з серверу і в друга
// (подія chat:deleted прийде обом). Чужі повідомлення так не видаляються — їх UI ховає лише
// локально.
func (a *App) DeleteChatMessage(peerAccountID string, messageID int64) error {
	a.startSocialIfLoggedIn()
	return a.socialService().DeleteChatMessage(peerAccountID, messageID)
}

// EditChatMessage — змінити текст СВОГО повідомлення (в обох). Подія chat:edited несе нову версію.
func (a *App) EditChatMessage(peerAccountID string, messageID int64, text string) error {
	a.startSocialIfLoggedIn()
	return a.socialService().EditChatMessage(peerAccountID, messageID, text)
}

// PinChatMessage / UnpinChatMessage — закріпити або відкріпити повідомлення (для обох). Подія
// chat:pins несе повний актуальний список закріплених.
func (a *App) PinChatMessage(peerAccountID string, messageID int64) error {
	a.startSocialIfLoggedIn()
	return a.socialService().PinChatMessage(peerAccountID, messageID, true)
}

func (a *App) UnpinChatMessage(peerAccountID string, messageID int64) error {
	a.startSocialIfLoggedIn()
	return a.socialService().PinChatMessage(peerAccountID, messageID, false)
}

// SendChatTyping — індикатор друку.
func (a *App) SendChatTyping(peerAccountID string) error {
	return a.socialService().SendTyping(peerAccountID)
}

// MarkChatRead — позначити прочитаним до id.
func (a *App) MarkChatRead(peerAccountID string, upToMessageID int64) error {
	return a.socialService().MarkRead(peerAccountID, upToMessageID)
}

// SendChatServerInvite — «Зайти на сервер» у чаті (адреса з P2P-модуля).
func (a *App) SendChatServerInvite(peerAccountID, serverAddr, packID, packName string, ttlMs int64) error {
	return a.socialService().SendServerInvite(peerAccountID, serverAddr, packID, packName, ttlMs)
}

// ── Віджет сповіщень у трей (notify_window.go) ────────────────────────
//
// Ці два обробники — ЄДИНЕ місце, де соціальні події стають тостами.
// Умова показу (сповіщення НЕ показуються):
//   - вимкнені в Налаштування → Сповіщення (notifyEnabled) або увімкнено
//     «Не турбувати» (notifyDnd) — див. notificationsAllowed;
//   - головне вікно активне — користувач і так дивиться на лаунчер.
//
// Обидва обробники навмисно працюють БЕЗ залежності від того, чи
// відкрита шухляда «Друзі» чи діалог: рішення про показ ухвалює
// виключно активність вікна — це передбачувано для користувача.

// notifySeenInvites — запрошення, про які ми вже повідомили (щоб
// повторне friends:update не спамило тостами).
var notifySeenInvites = struct {
	mu sync.Mutex
	m  map[string]bool
}{m: map[string]bool{}}

// currentSocialService — існуючий сервіс або nil (БЕЗ створення).
// Потрібно саме тут: notifyFromSocial викликається зсередини emit-циклу,
// тож створювати сервіс заново (як це робить socialService()) не можна.
func currentSocialService() *social.Service {
	socialMu.Lock()
	defer socialMu.Unlock()
	return socialSvc
}

// socialView — що користувач зараз ДИВИТЬСЯ у лаунчері: відкрита шухляда «Друзі»
// і, можливо, конкретний діалог. Звітує фронтенд (SetSocialView).
var socialView = struct {
	mu     sync.Mutex
	drawer bool
	peer   string
}{}

// SetSocialView — фронтенд повідомляє, чи відкрита шухляда «Друзі» і з ким відкритий
// діалог (порожньо — список). Потрібно, щоб тост НЕ дублював те, що користувач і так
// бачить, але з'являвся, коли він у лаунчері, а діалог/список закриті.
func (a *App) SetSocialView(drawerOpen bool, peerAccountID string) {
	socialView.mu.Lock()
	socialView.drawer, socialView.peer = drawerOpen, peerAccountID
	socialView.mu.Unlock()
}

// socialViewCovers — чи користувач просто зараз бачить цю подію на екрані лаунчера.
// Тост потрібен у всіх інших випадках: вікно згорнуте/неактивне (гра, інша програма),
// шухляда закрита або відкритий діалог з ІНШИМ другом.
func socialViewCovers(peerAccountID string) bool {
	if !notifyMainActive.Load() {
		return false
	}
	socialView.mu.Lock()
	defer socialView.mu.Unlock()
	if !socialView.drawer {
		return false
	}
	if peerAccountID == "" { // запит у друзі: достатньо відкритої шухляди
		return true
	}
	return socialView.peer == peerAccountID
}

// socialWindowActive — активність головного вікна → статус «відійшов» (internal/social).
func (a *App) socialWindowActive(active bool) {
	if svc := currentSocialService(); svc != nil {
		svc.SetWindowActive(active)
	}
}

// notifyFromSocial — розподіл подій соціальної системи на тости віджета.
func (a *App) notifyFromSocial(event string, payload any) {
	if !a.notificationsAllowed() {
		return
	}
	switch event {
	case "chat:message":
		ev, ok := payload.(social.ChatEvent)
		if !ok || ev.ChatID == "" || ev.From == "" {
			return
		}
		// Не шумимо про речі, які не можна показати: тип не text (серверне
		// запрошення — окрема подія), текст не розшифрувався (у тості не
		// буде «сміття») або порожній plaintext.
		if ev.BodyType != "" && ev.BodyType != "text" {
			return
		}
		if ev.DecryptFailed || ev.Plaintext == "" {
			return
		}
		// Службовий рядок передачі збірки — UI показує власний статус, тост зайвий.
		if shareIsControlText(ev.Plaintext) {
			return
		}
		if socialViewCovers(ev.From) {
			return // діалог з цим другом відкритий на екрані — тост зайвий
		}
		// Мути чатів (chatmute_shaurma.go): DND (глушить усі чати), загальний
		// мут чатів або мут саме цього діалогу. Сповіщення не показується,
		// але ПОВІДОМЛЕННЯ не втрачається — воно досі лежить у чаті (бейдж
		// непрочитаного в списку діалогів оновлюється, як і раніше).
		if a.IsChatMuted(ev.From) {
			return
		}
		nick := a.socialNickname(ev.From)
		if nick == "" {
			// Не друг: сервер доставив повідомлення лише тому, що отримувач дозволив
			// писати всім (і жоден не блокував іншого), тож сповіщення доречне.
			nick = a.strangerNickname(ev.From)
		}
		if nick == "" {
			return // нік невідомий — не показуємо
		}
		a.pushNotify(NotifyPayload{
			Kind:      "chat",
			Title:     nick,
			Body:      notifyPreviewText(ev.Plaintext),
			AccountID: ev.From,
			TS:        ev.TS, // VPS шле unix-МІЛІСЕКУНДИ (раніше тут було ×1000 — час у тості ламався)
			ID:        fmt.Sprintf("chat-%d", ev.ID),
		})

	case "friends:update":
		st, ok := payload.(social.State)
		if !ok {
			return
		}
		covered := socialViewCovers("")
		notifySeenInvites.mu.Lock()
		for _, inv := range st.Invites {
			if notifySeenInvites.m[inv.ID] {
				continue
			}
			notifySeenInvites.m[inv.ID] = true
			if covered {
				continue // шухляда «Друзі» відкрита — запит і так видно в списку
			}
			nick := cleanNotifyText(inv.FromNickname)
			if nick == "" {
				nick = inv.FromAccountID
			}
			// Сповіщення не залежить від наявності accountId друга — на кліку
			// відкриється шухляда «Друзі» (без конкретного діалогу).
			a.pushNotify(NotifyPayload{
				Kind:      "invite",
				Title:     nick,
				Body:      "", // текст підставляє фронтенд за ключем i18n
				AccountID: "",
				TS:        inv.CreatedAt,
				ID:        "invite-" + inv.ID,
			})
		}
		// Прибрати з кешу ті запрошення, яких уже немає (відповіли/відкликані) —
		// інакше повторний запит від того самого гравця більше б не показувався.
		live := make(map[string]bool, len(st.Invites))
		for _, inv := range st.Invites {
			live[inv.ID] = true
		}
		for id := range notifySeenInvites.m {
			if !live[id] {
				delete(notifySeenInvites.m, id)
			}
		}
		notifySeenInvites.mu.Unlock()
	}
}

// socialNickname — нік друга за accountId або "" якщо це не друг.
// Не створює сервіс (див. currentSocialService).
func (a *App) socialNickname(accountID string) string {
	svc := currentSocialService()
	if svc == nil || accountID == "" {
		return ""
	}
	st := svc.Snapshot()
	for _, f := range st.Friends {
		if f.AccountID == accountID {
			return cleanNotifyText(f.Nickname)
		}
	}
	return ""
}

// cleanNotifyText — мінімальна санітизація для тоста: прибрати керуючі/
// bidi-символи (аналог CleanText у internal/social, тільки без пакетної
// залежності від розміру). Друга лінія — фронтенд однаково не робить {@html}.
func cleanNotifyText(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r < 0x20, r == 0x7f, r >= 0x80 && r <= 0x9f,
			r >= 0x200b && r <= 0x200f, r >= 0x202a && r <= 0x202e,
			r >= 0x2066 && r <= 0x2069, r == 0xfeff:
			// керуючий символ — пропускаємо
		default:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

// notifyPreviewText — текст для тоста. Повідомлення «поділився модом» — це
// посилання з службовим фрагментом (#shaurma=source:id, див. modShare.ts на
// фронтенді); у тості показуємо «📦 назва-мода» замість сирого URL.
// Усе інше повертається як є.
func notifyPreviewText(plain string) string {
	lines := strings.Split(strings.TrimSpace(plain), "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	if !strings.HasPrefix(last, "https://") || !strings.Contains(last, "#shaurma=") {
		return plain
	}
	bare := last[:strings.Index(last, "#shaurma=")]
	bare = strings.TrimRight(bare, "/")
	slug := bare[strings.LastIndex(bare, "/")+1:]
	if slug == "" || len(slug) > 64 {
		return plain
	}
	return "\U0001F4E6 " + slug
}
