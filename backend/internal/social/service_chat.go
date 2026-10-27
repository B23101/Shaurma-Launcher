package social

// service_chat.go — чат на рівні Service: шифрування/розшифровка локально,
// відправка через WS, історія через Worker. Усе, що йде назовні, вже
// зашифроване; розшифрований текст існує лише в пам'яті цього процесу.

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"
)

// ChatEvent — подія нового повідомлення для UI. Ciphertext/nonce НЕ включаємо:
// UI показує або розшифрований текст, або позначку «не вдалось».
type ChatEvent struct {
	ChatID        string `json:"chatId"`
	From          string `json:"from"`
	ID            int64  `json:"id"`
	TS            int64  `json:"ts"`
	Plaintext     string `json:"plaintext,omitempty"`
	DecryptFailed bool          `json:"decryptFailed,omitempty"`
	BodyType      string        `json:"bodyType,omitempty"` // server_invite → див. chat:server_invite
	// ReplyToID — відповідь на повідомлення (0 = ні); UI показує цитату.
	ReplyToID int64 `json:"replyToId,omitempty"`
	// Mine — це МОЄ повідомлення, надіслане з іншого мого пристрою (From = я): показати як вихідне.
	Mine bool `json:"mine,omitempty"`
}

// ErrChatDisabled — у збірці немає ключів чату (KeyDir не задано).
var ErrChatDisabled = ErrNotSupported

// onIncomingMessage викликається з WS-горутини: розшифровує (локально) текст і
// віддає в UI подію.
func (s *Service) onIncomingMessage(m IncomingMessage) {
	ev := ChatEvent{ChatID: m.ChatID, From: m.From, ID: m.ID, TS: m.TS, BodyType: m.BodyType, ReplyToID: m.ReplyToID}
	s.mu.Lock()
	me := s.st.Me
	s.mu.Unlock()
	// Ехо МОГО повідомлення з іншого мого пристрою: співрозмовник — інша сторона діалогу, а лічильник
	// непрочитаного не росте (я ж сам це написав).
	mine := me != "" && m.From == me
	peer := m.From
	if mine {
		peer = OtherPartyInChat(m.ChatID, me)
		ev.Mine = true
	}
	if m.BodyType == "text" && s.crypto != nil {
		if peer == "" {
			ev.DecryptFailed = true
		} else if pt, err := s.crypto.DecryptFrom(peer, m.Ciphertext, m.Nonce); err == nil {
			ev.Plaintext = pt
		} else {
			ev.DecryptFailed = true
		}
	}
	// Непрочитане рахуємо тут, на рівні сервісу, а не в UI: діалог може бути
	// закритий (або вікно згорнуте) — лічильник і бейдж мають працювати завжди.
	// Якщо діалог відкритий, UI одразу шле MarkRead і лічильник скидається.
	if m.BodyType == "text" && m.From != "" && !mine {
		s.mu.Lock()
		if s.st.Unread == nil {
			s.st.Unread = map[string]int{}
		}
		s.st.Unread[m.From]++
		snap := copyUnread(s.st.Unread)
		s.mu.Unlock()
		s.d.Emit("chat:message", ev)
		s.d.Emit("friends:unread", snap)
		return
	}
	s.d.Emit("chat:message", ev)
}

// SendChatMessage шифрує текст для друга й ставить у WS-чергу. Повертає
// clientMessageId — за ним UI зіставить оптимістичну бульбашку з ack.
// ── Стан відправки ───────────────────────────────────────────────────────
// «Не надіслано» показується ЛИШЕ коли сервер явно відмовив (або локально не вдалось поставити
// в чергу). Мережа/перепідключення/повільне підтвердження — це «очікує», а не помилка: інакше UI
// писав «не надіслано», хоча повідомлення доходило пізніше.

const sendStateTTL = 30 * time.Minute

type SendState struct {
	Status string `json:"status"` // "pending" | "acked" | "failed" | "unknown"
	ChatID string `json:"chatId,omitempty"`
	ID     int64  `json:"id,omitempty"`
	TS     int64  `json:"ts,omitempty"`
	Code   string `json:"code,omitempty"`
	at     time.Time
}

func (s *Service) setSendState(cid string, st SendState) {
	st.at = time.Now()
	s.sendMu.Lock()
	if s.sends == nil {
		s.sends = map[string]SendState{}
	}
	s.sends[cid] = st
	for k, v := range s.sends { // GC: пам'ять не росте
		if time.Since(v.at) > sendStateTTL {
			delete(s.sends, k)
		}
	}
	s.sendMu.Unlock()
}

// ChatSendState — актуальний стан відправленого повідомлення за clientMessageId.
func (s *Service) ChatSendState(cid string) SendState {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	if st, ok := s.sends[cid]; ok {
		return st
	}
	return SendState{Status: "unknown"}
}

func (s *Service) onMessageAck(a MessageAck) {
	s.setSendState(a.ClientMessageID, SendState{Status: "acked", ChatID: a.ChatID, ID: a.ID, TS: a.TS})
	s.d.Emit("chat:ack", a)
}

func (s *Service) onMessageFailed(cid, code string) {
	s.setSendState(cid, SendState{Status: "failed", Code: code})
	s.d.Emit("chat:failed", map[string]string{"clientMessageId": cid, "code": code})
}

func (s *Service) SendChatMessage(peerID, text string) (string, error) {
	return s.SendChatMessageWithID(peerID, text, "")
}

// SendChatMessageReply — як SendChatMessage, але з відповіддю на повідомлення
// (replyToID > 0; 0 = звичайне). UI показує цитату в бульбашці.
func (s *Service) SendChatMessageReply(peerID, text string, replyToID int64) (string, error) {
	return s.sendChatMessageInner(peerID, text, "", replyToID)
}

// SendChatMessageWithID — як SendChatMessage, але з наперед заданим clientMessageId. «Повторити» мусить
// слати з ТИМ САМИМ ID: VPS дедуплікує за ним, тож навіть якщо перша спроба таки дійшла, дубля не буде.
func (s *Service) SendChatMessageWithID(peerID, text, clientMsgID string) (string, error) {
	return s.sendChatMessageInner(peerID, text, clientMsgID, 0)
}

func (s *Service) sendChatMessageInner(peerID, text, clientMsgID string, replyToID int64) (string, error) {
	if !s.Running() {
		return "", ErrNotLoggedIn
	}
	if s.crypto == nil {
		return "", ErrChatDisabled
	}
	if replyToID < 0 {
		return "", ErrBadInput
	}
	s.mu.Lock()
	me, pc := s.st.Me, s.pc
	s.mu.Unlock()
	if me == "" {
		return "", ErrNotLoggedIn
	}
	if pc == nil {
		return "", ErrNotConnected
	}
	chatID, err := ConversationID(me, peerID)
	if err != nil {
		return "", err
	}
	// Антиспам: до шифрування й черги, щоб потік не навантажував ні CPU, ні VPS/Worker.
	if err := s.limiter.allowMessage(chatID, text); err != nil {
		return "", err
	}
	ct, nonce, err := s.crypto.EncryptFor(peerID, text)
	if err != nil {
		return "", err
	}
	if clientMsgID == "" {
		clientMsgID = randomClientMsgID()
	} else if len(clientMsgID) > 64 || CleanText(clientMsgID, 64) != clientMsgID {
		return "", ErrBadInput
	}
	s.setSendState(clientMsgID, SendState{Status: "pending", ChatID: chatID})
	if err := pc.SendChatMessageReply(chatID, ct, nonce, clientMsgID, replyToID); err != nil {
		s.setSendState(clientMsgID, SendState{Status: "failed", Code: "local_queue"})
		return "", err
	}
	return clientMsgID, nil
}

// SendTyping — індикатор друку (дружній до бюджету: VPS дроселює сам).
func (s *Service) SendTyping(peerID string) error {
	s.mu.Lock()
	me, pc := s.st.Me, s.pc
	s.mu.Unlock()
	if pc == nil || me == "" {
		return ErrNotConnected
	}
	chatID, err := ConversationID(me, peerID)
	if err != nil {
		return err
	}
	if !s.limiter.allowTyping(chatID) {
		return nil // надто часто — просто пропускаємо, друг нічого не помітить
	}
	return pc.SendTyping(chatID)
}

// SendServerInvite — запрошення «Зайти на сервер» (адреса вже від P2P-модуля).
func (s *Service) SendServerInvite(peerID, serverAddr, packID, packName string, ttlMs int64) error {
	if !s.Running() {
		return ErrNotLoggedIn
	}
	s.mu.Lock()
	me, pc := s.st.Me, s.pc
	s.mu.Unlock()
	if pc == nil || me == "" {
		return ErrNotConnected
	}
	chatID, err := ConversationID(me, peerID)
	if err != nil {
		return err
	}
	return pc.SendServerInvite(chatID, serverAddr, packID, packName, ttlMs)
}

// MarkRead — позначити прочитаним до id (у межах бюджету: рідкісна подія).
func (s *Service) MarkRead(peerID string, upToMessageID int64) error {
	s.mu.Lock()
	me, pc := s.st.Me, s.pc
	s.mu.Unlock()
	if pc == nil || me == "" {
		return ErrNotConnected
	}
	chatID, err := ConversationID(me, peerID)
	if err != nil {
		return err
	}
	if err := pc.SendMarkRead(chatID, upToMessageID); err != nil {
		return err
	}
	s.mu.Lock()
	_, had := s.st.Unread[peerID]
	delete(s.st.Unread, peerID)
	snap := copyUnread(s.st.Unread)
	s.mu.Unlock()
	if had {
		s.d.Emit("friends:unread", snap)
	}
	return nil
}

// ChatHistory читає сторінку історії й РОЗШИФРОВУЄ її локально. Кожне
// повідомлення, яке не вдалось розшифрувати, лишається з прапорцем
// DecryptFailed — UI покаже «Не вдалося розшифрувати», а не впаде.
func (s *Service) ChatHistory(peerID string, before int64, limit int) (ChatPage, error) {
	return s.chatHistory(peerID, before, 0, limit)
}

// ChatHistoryAfter — сторінка НОВІШИХ за after (unix ms) повідомлень, за зростанням. Для
// прокрутки вниз після переходу до старого повідомлення (закріплене тощо). Лише через WS.
func (s *Service) ChatHistoryAfter(peerID string, after int64, limit int) (ChatPage, error) {
	if after <= 0 {
		return ChatPage{}, ErrBadInput
	}
	return s.chatHistory(peerID, 0, after, limit)
}

func (s *Service) chatHistory(peerID string, before, after int64, limit int) (ChatPage, error) {
	if !s.Running() {
		return ChatPage{}, ErrNotLoggedIn
	}
	s.mu.Lock()
	me := s.st.Me
	s.mu.Unlock()
	if me == "" {
		return ChatPage{}, ErrNotLoggedIn
	}
	chatID, err := ConversationID(me, peerID)
	if err != nil {
		return ChatPage{}, err
	}
	// Джерело правди — VPS: він віддає і ще не збережені у Worker повідомлення.
	// HTTP до Worker — лише запасний шлях, коли WS недоступний (без after/закріплених).
	s.mu.Lock()
	pc := s.pc
	s.mu.Unlock()
	var page ChatPage
	if pc != nil {
		page, err = pc.RequestHistory(chatID, before, after, limit)
	} else {
		err = ErrNotConnected
	}
	if err != nil {
		if after > 0 {
			return ChatPage{}, err
		}
		page, err = s.client.ChatHistory(chatID, before, limit)
		if err != nil {
			return ChatPage{}, err
		}
	}
	s.decryptMessages(me, peerID, page.Messages)
	s.decryptMessages(me, peerID, page.Pinned)
	if page.Pinned == nil {
		page.Pinned = []ChatMessage{}
	}
	return page, nil
}

// decryptMessages розшифровує повідомлення діалогу me↔peer на місці.
func (s *Service) decryptMessages(me, peerID string, msgs []ChatMessage) {
	if s.crypto == nil {
		return
	}
	for i := range msgs {
		m := &msgs[i]
		if m.BodyType != "text" {
			continue
		}
		// Відправник у межах цього чату: якщо це я — співрозмовник peerID.
		other := m.From
		if other == me {
			other = peerID
		}
		if !ValidAccountID(other) {
			m.DecryptFailed = true
			continue
		}
		pt, derr := s.crypto.DecryptFrom(other, m.Ciphertext, m.Nonce)
		if derr != nil {
			m.DecryptFailed = true
			continue
		}
		m.Plaintext = pt
	}
}

// DeleteChatMessage видаляє СВОЄ повідомлення в обох (і з серверу). Чужі видалити «для всіх»
// не можна — UI ховає їх лише локально.
func (s *Service) DeleteChatMessage(peerID string, messageID int64) error {
	chatID, pc, err := s.chatOpTarget(peerID)
	if err != nil {
		return err
	}
	return pc.RequestOp("delete_message", chatID, messageID, nil)
}

// EditChatMessage змінює текст СВОГО повідомлення в обох. Текст шифрується локально, як і при
// відправці; сервер лише підміняє шифротекст. Подія chat:edited прийде обом (і моїм іншим пристроям).
func (s *Service) EditChatMessage(peerID string, messageID int64, text string) error {
	if s.crypto == nil {
		return ErrChatDisabled
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return ErrBadInput
	}
	chatID, pc, err := s.chatOpTarget(peerID)
	if err != nil {
		return err
	}
	ct, nonce, err := s.crypto.EncryptFor(peerID, text)
	if err != nil {
		return err
	}
	return pc.RequestOp("edit_message", chatID, messageID, map[string]any{"ciphertext": ct, "nonce": nonce})
}

// PinChatMessage закріплює (pin=true) або відкріплює повідомлення — для обох учасників.
func (s *Service) PinChatMessage(peerID string, messageID int64, pin bool) error {
	chatID, pc, err := s.chatOpTarget(peerID)
	if err != nil {
		return err
	}
	return pc.RequestOp("pin_message", chatID, messageID, map[string]any{"pin": pin})
}

// ReactChatMessage ставить/прибирає реакцію на повідомлення (один емодзі на акаунт).
// Порожній emoji = прибрати свою реакцію. Повертається після op_result від сервера.
func (s *Service) ReactChatMessage(peerID string, messageID int64, emoji string) error {
	chatID, pc, err := s.chatOpTarget(peerID)
	if err != nil {
		return err
	}
	if messageID <= 0 || len([]rune(emoji)) > 16 {
		return ErrBadInput
	}
	return pc.RequestOp("react_message", chatID, messageID, map[string]any{"emoji": CleanText(emoji, 64)})
}

func (s *Service) chatOpTarget(peerID string) (string, *PresenceClient, error) {
	if !s.Running() {
		return "", nil, ErrNotLoggedIn
	}
	s.mu.Lock()
	me, pc := s.st.Me, s.pc
	s.mu.Unlock()
	if me == "" {
		return "", nil, ErrNotLoggedIn
	}
	if pc == nil {
		return "", nil, ErrNotConnected
	}
	chatID, err := ConversationID(me, peerID)
	if err != nil {
		return "", nil, err
	}
	return chatID, pc, nil
}

// onMessageDeleted — повідомлення видалено «для всіх»: UI прибирає його зі стрічки.
func (s *Service) onMessageDeleted(e DeletedEvent) {
	s.d.Emit("chat:deleted", e)
}

// onMessageEdited розшифровує нову версію тексту й віддає її в UI (подія chat:edited).
func (s *Service) onMessageEdited(e EditedEvent) {
	s.mu.Lock()
	me := s.st.Me
	s.mu.Unlock()
	peer := OtherPartyInChat(e.ChatID, me)
	switch {
	case s.crypto == nil || !ValidAccountID(peer):
		e.DecryptFailed = true
	default:
		// Автор правки — я або співрозмовник; ключ діалогу той самий, тож розшифровуємо з боку peer.
		if pt, err := s.crypto.DecryptFrom(peer, e.Ciphertext, e.Nonce); err == nil {
			e.Plaintext = pt
		} else {
			e.DecryptFailed = true
		}
	}
	s.d.Emit("chat:edited", e)
}

// onPinsUpdated розшифровує закріплені й віддає в UI повний актуальний список.
func (s *Service) onPinsUpdated(e PinsEvent) {
	s.mu.Lock()
	me := s.st.Me
	s.mu.Unlock()
	if peer := OtherPartyInChat(e.ChatID, me); peer != "" {
		s.decryptMessages(me, peer, e.Pinned)
	}
	if e.Pinned == nil {
		e.Pinned = []ChatMessage{}
	}
	s.d.Emit("chat:pins", e)
}

// onReactionsUpdated — віддає в UI ПОВНИЙ актуальний список реакцій одного
// повідомлення (емодзі — відкритий текст, розшифровка не потрібна).
func (s *Service) onReactionsUpdated(e ReactionsEvent) {
	if e.Reactions == nil {
		e.Reactions = []Reaction{}
	}
	s.d.Emit("chat:reactions", e)
}

// ErrNotConnected — WS-з'єднання не активне.
var ErrNotConnected = errNotConnected{}

type errNotConnected struct{}

func (errNotConnected) Error() string { return "social:not_connected" }

// randomClientMsgID — ідемпотентний id повідомлення (ретрай не дублює запис).
// Формат відповідає Worker: /^[a-zA-Z0-9_-]{1,64}$/.
func randomClientMsgID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}
	return hex.EncodeToString(buf)
}
