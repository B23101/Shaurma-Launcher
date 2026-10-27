package social

// chat.go — чат (ФАЗА 1, розділи 1.3-1.4 плану).
//
// Розподіл: «холодна» історія й ключі — у Worker/D1 (тут, через Client);
// «жива» доставка — по WS через VPS (окремо). УВАГА: нові повідомлення йдуть
// «простим режимом» (див. chatcrypto.go, plainNonce): це НЕ наскрізне шифрування,
// сервер бачить текст; захист у дорозі — TLS. Старі NaCl-повідомлення лише читаються.
//
// Бюджет Worker (100k/добу): історія читається ЛИШЕ на вимогу (відкриття
// діалогу / «докрутити»), публічний ключ друга — один раз із кешем у
// пам'яті, надсилання — 1 запит на повідомлення.

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// ChatMessage — одне повідомлення. Поля з мережі (ciphertext/nonce) та
// локально обчислений plaintext лежать РАЗОМ, але plaintext НІКОЛИ не
// серіалізується в те, що йде на сервер (його просто не шлють).
type ChatMessage struct {
	ID            int64         `json:"id"`
	From          string        `json:"from"`
	BodyType      string        `json:"bodyType"` // text | server_invite
	Ciphertext    string        `json:"ciphertext"`
	Nonce         string        `json:"nonce"`
	CreatedAt     int64         `json:"createdAt"`
	Plaintext     string        `json:"plaintext,omitempty"`     // лише локально
	DecryptFailed bool          `json:"decryptFailed,omitempty"` // не вдалось розшифрувати
	Invite        *ServerInvite `json:"invite,omitempty"`
	// ReplyToID — id повідомлення, на яке відповідають (0 = ні). UI показує
	// цитату; якщо ціль видалена — «повідомлення видалене».
	ReplyToID int64 `json:"replyToId,omitempty"`
	// EditedAt — unix мс останньої правки тексту автором (0 = не редагувалось).
	EditedAt int64 `json:"editedAt,omitempty"`
	// Reactions — реакції учасників діалогу (емодзі — відкритий текст,
	// сервер переносить їх як метадані, зміст повідомлень не торкається).
	Reactions []Reaction `json:"reactions,omitempty"`
}

// Reaction — одна реакція на повідомлення (By = accountId того, хто відреагував).
type Reaction struct {
	By    string `json:"by"`
	Emoji string `json:"emoji"`
}

// ServerInvite — структуроване запрошення «Зайти на сервер» (не шифрується:
// сервер сам має знати адресу/термін, щоб ревокувати, план 1.3.3).
type ServerInvite struct {
	ID         string `json:"id"`
	ServerAddr string `json:"serverAddr"`
	PackID     string `json:"packId,omitempty"`
	PackName   string `json:"packName,omitempty"`
	ExpiresAt  int64  `json:"expiresAt"`
	Revoked    bool   `json:"revoked"`
	Active     bool   `json:"active"`
}

// ChatPage — сторінка історії.
type ChatPage struct {
	Messages []ChatMessage `json:"messages"`
	HasMore  bool          `json:"hasMore"`
	// Pinned — закріплені повідомлення діалогу (повні, за порядком закріплення). Лише на
	// найновішій сторінці (before=0, after=0); у решти порожньо.
	Pinned []ChatMessage `json:"pinned"`
	// Reads — курсори «прочитано» учасників (лише найновіша сторінка): до якого id дочитав кожен.
	// UI бере курсор співрозмовника, щоб галочки «прочитано» переживали перезапуск.
	Reads []ReadMark `json:"reads,omitempty"`
}

// ReadMark — акаунт By дочитав діалог до повідомлення UpTo (включно).
type ReadMark struct {
	By   string `json:"by"`
	UpTo int64  `json:"upTo"`
}

// ConversationID повертає КАНОНІЧНИЙ chatId — лексикографічно менший
// accountId першим, як canonicalPair у worker/friends.js (`x < y`). Обидві
// сторони мусять рахувати однаково, інакше це були б два різні діалоги.
// accountId — лише [0-9a-f-], тож порядок Go і JS збігається.
func ConversationID(a, b string) (string, error) {
	if !ValidAccountID(a) || !ValidAccountID(b) || a == b {
		return "", ErrBadInput
	}
	if a < b {
		return a + "_" + b, nil
	}
	return b + "_" + a, nil
}

// OtherPartyInChat розбирає chatId і повертає співрозмовника для self ("" якщо
// self не учасник). Дзеркало otherPartyInChatID на VPS — для валідації перед
// відправкою, щоб не слати кадр із чужим chatId.
func OtherPartyInChat(chatID, self string) string {
	for i := 0; i < len(chatID); i++ {
		if chatID[i] == '_' {
			left, right := chatID[:i], chatID[i+1:]
			switch self {
			case left:
				if ValidAccountID(right) {
					return right
				}
			case right:
				if ValidAccountID(left) {
					return left
				}
			}
			return ""
		}
	}
	return ""
}

// ChatHistory читає сторінку історії. before=0 → остання сторінка (Worker
// трактує 0 як «зараз+1»). limit обмежується Worker'ом (1..100).
func (c *Client) ChatHistory(chatID string, before int64, limit int) (ChatPage, error) {
	if _, err := splitChatID(chatID); err != nil {
		return ChatPage{}, ErrBadInput
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	q := url.Values{}
	q.Set("limit", itoa(limit))
	if before > 0 {
		q.Set("before", itoa64(before))
	}
	path := "/friends/chat/" + url.PathEscape(chatID) + "?" + q.Encode()

	var r struct {
		Messages []struct {
			ID          int64  `json:"id"`
			FromAccount string `json:"fromAccountId"`
			BodyType    string `json:"bodyType"`
			Ciphertext  string `json:"ciphertext"`
			Nonce       string `json:"nonce"`
			CreatedAt   int64  `json:"createdAt"`
			ReplyToID   int64  `json:"replyToId"`
			EditedAt    int64  `json:"editedAt"`
			Reactions   []struct {
				MessageID int64  `json:"messageId"`
				By        string `json:"by"`
				Emoji     string `json:"emoji"`
			} `json:"reactions"`
			Invite *struct {
				ID         string `json:"id"`
				ServerAddr string `json:"serverAddr"`
				PackID     string `json:"packId"`
				PackName   string `json:"packName"`
				ExpiresAt  int64  `json:"expiresAt"`
				Revoked    bool   `json:"revoked"`
				Active     bool   `json:"active"`
			} `json:"invite"`
		} `json:"messages"`
		HasMore   bool `json:"hasMore"`
		Reactions []struct {
			MessageID int64  `json:"messageId"`
			By        string `json:"by"`
			Emoji     string `json:"emoji"`
		} `json:"reactions"`
	}
	if err := c.call(http.MethodGet, path, nil, &r); err != nil {
		return ChatPage{}, err
	}
	out := ChatPage{Messages: make([]ChatMessage, 0, len(r.Messages)), HasMore: r.HasMore}
	for _, m := range r.Messages {
		if !ValidAccountID(m.FromAccount) {
			continue
		}
		bt := "text"
		if m.BodyType == "server_invite" {
			bt = "server_invite"
		}
		msg := ChatMessage{
			ID: m.ID, From: m.FromAccount, BodyType: bt,
			Ciphertext: m.Ciphertext, Nonce: m.Nonce, CreatedAt: m.CreatedAt, ReplyToID: m.ReplyToID, EditedAt: m.EditedAt,
		}
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
		out.Messages = append(out.Messages, msg)
	}
	// Груповий блок reactions (найновіша сторінка від Worker) — розкладаємо по повідомленнях.
	if len(r.Reactions) > 0 {
		byID := map[int64]int{}
		for i := range out.Messages {
			byID[out.Messages[i].ID] = i
		}
		for _, rw := range r.Reactions {
			if i, ok := byID[rw.MessageID]; ok && ValidAccountID(rw.By) && rw.Emoji != "" {
				out.Messages[i].Reactions = append(out.Messages[i].Reactions, Reaction{By: rw.By, Emoji: CleanText(rw.Emoji, 64)})
			}
		}
	}
	return out, nil
}

// RegisterOwnPublicKey публікує/ротує публічний ключ на Worker.
func (c *Client) RegisterOwnPublicKey(publicKeyB64 string) error {
	if _, err := DecodePublicKey(publicKeyB64); err != nil {
		return ErrBadInput
	}
	return c.call(http.MethodPost, "/friends/key", map[string]string{"publicKey": publicKeyB64}, nil)
}

// FetchPeerPublicKey читає публічний ключ друга (404 → ErrKeyNotFound).
func (c *Client) FetchPeerPublicKey(accountID string) (PublicKey, error) {
	if !ValidAccountID(accountID) {
		return PublicKey{}, ErrBadInput
	}
	var r struct {
		PublicKey string `json:"publicKey"`
	}
	err := c.call(http.MethodGet, "/friends/key/"+url.PathEscape(accountID), nil, &r)
	if err != nil {
		var ae *APIError
		if errors.As(err, &ae) && ae.Status == http.StatusNotFound {
			return PublicKey{}, ErrKeyNotFound
		}
		return PublicKey{}, err
	}
	return DecodePublicKey(r.PublicKey)
}

// FetchPeerKeyHistory читає ВСІ опубліковані публічні ключі акаунта (нові першими) — потрібно, щоб
// розшифрувати старі повідомлення після ротації ключа співрозмовником. Старий сервер (404/405) →
// порожній список без помилки.
func (c *Client) FetchPeerKeyHistory(accountID string) ([]PublicKey, error) {
	if !ValidAccountID(accountID) {
		return nil, ErrBadInput
	}
	var r struct {
		Keys []struct {
			PublicKey string `json:"publicKey"`
		} `json:"keys"`
	}
	err := c.call(http.MethodGet, "/friends/keys/"+url.PathEscape(accountID), nil, &r)
	if err != nil {
		var ae *APIError
		if errors.As(err, &ae) && (ae.Status == http.StatusNotFound || ae.Status == http.StatusMethodNotAllowed) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]PublicKey, 0, len(r.Keys))
	for _, k := range r.Keys {
		if pk, err := DecodePublicKey(k.PublicKey); err == nil {
			out = append(out, pk)
		}
	}
	return out, nil
}

// ErrKeyNotFound — у друга ще немає опублікованого ключа E2E.
var ErrKeyNotFound = errors.New("social:key_not_found")

// ── Дрібне ──────────────────────────────────────────────────────────────

// splitChatID перевіряє, що chatId — рівно "A_B" з двома валідними accountId.
func splitChatID(chatID string) (string, error) {
	i := strings.IndexByte(chatID, '_')
	if i < 0 || i != strings.LastIndexByte(chatID, '_') {
		return "", ErrBadInput
	}
	a, b := chatID[:i], chatID[i+1:]
	if !ValidAccountID(a) || !ValidAccountID(b) {
		return "", ErrBadInput
	}
	return chatID, nil
}

func itoa(n int) string     { return fmt.Sprintf("%d", n) }
func itoa64(n int64) string { return fmt.Sprintf("%d", n) }

// ── Події чату (транспорт; вміст лишається зашифрованим) ────────────────

// IncomingMessage — нове повідомлення з WS. Ciphertext/Nonce розшифровує
// викликач (ChatCrypto), тут лише байти.
type IncomingMessage struct {
	ChatID     string
	From       string
	BodyType   string
	Ciphertext string
	Nonce      string
	TS         int64
	ID         int64
	ReplyToID  int64 // відповідь на повідомлення (0 = ні)
}

// MessageAck — підтвердження, що наше повідомлення лягло в D1 (з id/часом).
//
// json-теги camelCase ОБОВ'ЯЗКОВІ: ці структури серіалізуються напряму в
// події "chat:ack" (App.emit → json.Marshal), а фронтенд (SocialChatAck)
// читає саме camelCase-поля. Без тегів Go віддав би "ClientMessageID"/"ChatID",
// UI не знайшов би ack за chatId/clientMessageId — і повідомлення, яке
// насправді дійшло, через 15 с позначалось як «не надіслано».
type MessageAck struct {
	ClientMessageID string `json:"clientMessageId"`
	ChatID          string `json:"chatId"`
	ID              int64  `json:"id"`
	TS              int64  `json:"ts"`
}

type TypingEvent struct {
	ChatID    string `json:"chatId"`
	AccountID string `json:"accountId"`
}

type ReadEvent struct {
	ChatID        string `json:"chatId"`
	UpToMessageID int64  `json:"upToMessageId"`
}

// DeletedEvent — повідомлення видалено «для всіх» (серіалізується в подію chat:deleted).
type DeletedEvent struct {
	ChatID    string `json:"chatId"`
	MessageID int64  `json:"messageId"`
	By        string `json:"by"`
}

// EditedEvent — автор змінив текст повідомлення (серіалізується в подію chat:edited).
// Ciphertext/Nonce приходять із мережі й розшифровуються локально; назовні — лише Plaintext.
type EditedEvent struct {
	ChatID        string `json:"chatId"`
	MessageID     int64  `json:"messageId"`
	By            string `json:"by"`
	EditedAt      int64  `json:"editedAt"`
	Plaintext     string `json:"plaintext,omitempty"`
	DecryptFailed bool   `json:"decryptFailed,omitempty"`
	Ciphertext    string `json:"-"`
	Nonce         string `json:"-"`
}

// PinsEvent — актуальний ПОВНИЙ список закріплених діалогу (подія chat:pins). Action:
// pin | unpin | removed (закріплене повідомлення видалили); MessageID — про яке мова.
type PinsEvent struct {
	ChatID    string        `json:"chatId"`
	Pinned    []ChatMessage `json:"pinned"`
	By        string        `json:"by"`
	Action    string        `json:"action"`
	MessageID int64         `json:"messageId"`
}

// ReactionsEvent — актуальний ПОВНИЙ список реакцій одного повідомлення
// (подія chat:reactions). Action: "set" | "removed".
type ReactionsEvent struct {
	ChatID    string     `json:"chatId"`
	MessageID int64      `json:"messageId"`
	Reactions []Reaction `json:"reactions"`
	By        string     `json:"by"`
	Emoji     string     `json:"emoji"`
	Action    string     `json:"action"`
}

// ServerInviteEvent — вхідне запрошення «Зайти на сервер» (структуроване,
// не шифрується — план 1.3.3).
type ServerInviteEvent struct {
	ChatID     string `json:"chatId"`
	From       string `json:"from"`
	ServerAddr string `json:"serverAddr"`
	PackID     string `json:"packId"`
	PackName   string `json:"packName"`
	InviteID   string `json:"inviteId"`
}

// ErrQueueFull — вихідна черга WS переповнена (з'єднання відновлюється).
var ErrQueueFull = errors.New("social:queue_full")

var clientMsgIDRe = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

func validClientMsgID(s string) bool { return clientMsgIDRe.MatchString(s) }

// serverAddrRe — дзеркало validServerAddr у worker/friends.js: hostname/IPv4
// з необов'язковим портом. Довільний рядок у кадрі був би ін'єкцією в UI.
var serverAddrRe = regexp.MustCompile(`^[A-Za-z0-9.\-]{1,63}(:\d{1,5})?$`)

func sanitizeServerAddr(s string) (string, bool) {
	s = CleanText(s, 64)
	if !serverAddrRe.MatchString(s) {
		return "", false
	}
	return s, true
}
