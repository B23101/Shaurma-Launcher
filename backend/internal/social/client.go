package social

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Requester виконує авторизований запит до Worker. У лаунчері це обгортка над
// skinAuthedRequest (вона сама оновлює access-JWT і додає X-Shaurma-Token), а
// у тестах — httptest. Шлях — завжди «/friends/...» без хоста: хост задає
// реалізація, тож цей пакет фізично не може відправити токен на чужий домен.
type Requester func(method, path string, body []byte) (*http.Response, error)

// Помилки рівня користувача. UI показує переклад за кодом, а НЕ сирий текст
// сервера — так нічого, що прислав Worker/проксі, не потрапляє на екран.
var (
	ErrBadInput        = errors.New("social:bad_input")
	ErrNotLoggedIn     = errors.New("social:not_logged_in")
	ErrNotSupported    = errors.New("social:not_supported")
	ErrBudgetExhausted = errors.New("social:budget_exhausted")
)

// APIError — відмова Worker із відомим кодом.
type APIError struct {
	Status int
	Code   string // з білого списку knownCodes або "unknown"
}

func (e *APIError) Error() string { return "social:" + e.Code }

// knownCodes — коди з worker/friends.js, які UI вміє показати. Усе інше →
// "unknown" (щоб довільний текст із відповіді не став повідомленням).
var knownCodes = map[string]bool{
	"gone": true, "not_invited": true, "too_many_guests": true, "invalid_account": true,
	"account_not_found": true, "invalid_nickname": true, "invalid_recipient": true,
	"invites_disabled": true, "not_friends_of_friends": true, "rate_limited": true,
	"already_friends": true, "friend_limit_reached": true, "invite_already_pending": true,
	"invalid_handle": true, "user_not_found": true,
	"invite_not_found": true, "invite_not_pending": true, "forbidden": true,
	"Unauthorized": true, "Bad Request": true,
	"friends_internal_error": true,
	// ключі чату / пристрої
	"key_managed": true, "device_revoked": true, "key_exists": true, "key_unavailable": true,
	"blocked": true, "block_limit_reached": true,
	"no_chat_key": true, "chat_key_disabled": true, "device_limit": true,
}

// Client — HTTP-клієнт друзів.
type Client struct {
	do     Requester
	budget *Budget
}

func NewClient(do Requester) *Client { return &Client{do: do} }

// WithBudget задає запобіжник денних запитів до Worker. nil → без обмеження
// (використовується в тестах і там, де ліміт не потрібен).
func (c *Client) WithBudget(b *Budget) *Client {
	if c != nil {
		c.budget = b
	}
	return c
}

// call виконує запит і розбирає JSON у out (може бути nil). Тіло читається
// з жорстким обмеженням — Worker, що віддав гігабайт, не з'їсть пам'ять.
func (c *Client) call(method, path string, in any, out any) error {
	if c == nil || c.do == nil {
		return ErrNotLoggedIn
	}
	// Запобіжник денної квоти Worker (100k). Рахуємо КОЖЕН вихідний HTTP-запит
	// (навіть той, що впаде): саме запити, а не успіхи, з'їдають квоту.
	if c.budget != nil && !c.budget.Allow() {
		return ErrBudgetExhausted
	}
	var body []byte
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return ErrBadInput
		}
		body = b
	}
	resp, err := c.do(method, path, body)
	if err != nil {
		// Тексти помилок авторизації походять із нашого ж коду (не з мережі).
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxHTTPBody+1))
	if err != nil {
		return fmt.Errorf("social:network")
	}
	if len(data) > maxHTTPBody {
		return fmt.Errorf("social:response_too_large")
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return parseAPIError(resp.StatusCode, data)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("social:bad_response")
	}
	return nil
}

func parseAPIError(status int, data []byte) error {
	var e struct {
		Error string `json:"error"`
	}
	code := "unknown"
	if json.Unmarshal(data, &e) == nil && knownCodes[e.Error] {
		code = e.Error
	}
	if status == http.StatusUnauthorized {
		if code == "device_revoked" {
			return ErrDeviceRevoked // окрема причина: «цей пристрій вилучено», а не просто «потрібен вхід»
		}
		return ErrNotLoggedIn
	}
	if status == http.StatusTooManyRequests {
		code = "rate_limited"
	}
	return &APIError{Status: status, Code: code}
}

// ListFriends повертає друзів (БЕЗ presence — його накладає Presence-клієнт).
func (c *Client) ListFriends() ([]Friend, error) {
	var r struct {
		Friends []struct {
			AccountID string `json:"accountId"`
			Nickname  string `json:"nickname"`
			Handle    string `json:"handle"`
			Since     int64  `json:"since"`
			Bio       string `json:"bio"`
			Color     string `json:"color"`
		} `json:"friends"`
	}
	if err := c.call(http.MethodGet, "/friends/list", nil, &r); err != nil {
		return nil, err
	}
	out := make([]Friend, 0, len(r.Friends))
	for _, f := range r.Friends {
		if !ValidAccountID(f.AccountID) {
			continue // сміття від сервера не показуємо
		}
		if len(out) >= MaxFriends {
			break
		}
		nick := CleanText(f.Nickname, MaxNicknameLen)
		if nick == "" {
			nick = "?"
		}
		out = append(out, Friend{AccountID: f.AccountID, Nickname: nick, Handle: SanitizeHandle(f.Handle), Bio: SanitizeBio(f.Bio), Color: SanitizeColor(f.Color), Since: f.Since, Presence: Presence{Status: StatusOffline}})
	}
	return out, nil
}

// ListInvites повертає ВХІДНІ запити в друзі.
func (c *Client) ListInvites() ([]Invite, error) {
	var r struct {
		Invites []struct {
			ID            string `json:"id"`
			FromAccountID string `json:"fromAccountId"`
			FromNickname  string `json:"fromNickname"`
			FromHandle    string `json:"fromHandle"`
			CreatedAt     int64  `json:"createdAt"`
		} `json:"invites"`
	}
	if err := c.call(http.MethodGet, "/friends/invites", nil, &r); err != nil {
		return nil, err
	}
	out := make([]Invite, 0, len(r.Invites))
	for _, i := range r.Invites {
		if !ValidInviteID(i.ID) || !ValidAccountID(i.FromAccountID) {
			continue
		}
		nick := CleanText(i.FromNickname, MaxNicknameLen)
		if nick == "" {
			nick = "?"
		}
		out = append(out, Invite{ID: i.ID, FromAccountID: i.FromAccountID, FromNickname: nick, FromHandle: SanitizeHandle(i.FromHandle), CreatedAt: i.CreatedAt})
		if len(out) >= 100 {
			break
		}
	}
	return out, nil
}

// ListSentInvites повертає ВИХІДНІ запити в друзі, що чекають відповіді. У Invite
// поля FromAccountID/FromNickname/FromHandle описують АДРЕСАТА (кому надіслано).
func (c *Client) ListSentInvites() ([]Invite, error) {
	var r struct {
		Invites []struct {
			ID            string `json:"id"`
			FromAccountID string `json:"fromAccountId"`
			FromNickname  string `json:"fromNickname"`
			FromHandle    string `json:"fromHandle"`
			CreatedAt     int64  `json:"createdAt"`
		} `json:"invites"`
	}
	if err := c.call(http.MethodGet, "/friends/invites/sent", nil, &r); err != nil {
		return nil, err
	}
	out := make([]Invite, 0, len(r.Invites))
	for _, i := range r.Invites {
		if !ValidInviteID(i.ID) || !ValidAccountID(i.FromAccountID) {
			continue
		}
		nick := CleanText(i.FromNickname, MaxNicknameLen)
		if nick == "" {
			nick = "?"
		}
		out = append(out, Invite{ID: i.ID, FromAccountID: i.FromAccountID, FromNickname: nick, FromHandle: SanitizeHandle(i.FromHandle), CreatedAt: i.CreatedAt})
		if len(out) >= 100 {
			break
		}
	}
	return out, nil
}

// SendInvite надсилає запит у друзі за ніком. Нік валідується тут і ще раз на
// Worker; ми НЕ приймаємо accountId від користувача — лише нік, тож ввести
// «чужий id» вручну неможливо.
func (c *Client) SendInvite(accountID string) error {
	// Друга додають лише за ID акаунта Шаурми: він унікальний, а нік — тільки відображення
	// і може збігатися в різних акаунтів. Пробіли/регістр, що лишились після копіювання, прибираємо.
	id := strings.ToLower(strings.TrimSpace(accountID))
	if !ValidAccountID(id) {
		return ErrBadInput
	}
	return c.call(http.MethodPost, "/friends/invite", map[string]string{"toAccountId": id}, nil)
}

// SendInviteByHandle надсилає запит у друзі за @handle (унікальний, лише точний збіг). Сервер не
// розрізняє «немає такого» і «не приймає запити» — в обох випадках user_not_found.
func (c *Client) SendInviteByHandle(handle string) error {
	h, ok := NormalizeHandle(handle)
	if !ok {
		return ErrBadInput
	}
	return c.call(http.MethodPost, "/friends/invite", map[string]string{"toHandle": h}, nil)
}

// RespondInvite приймає або відхиляє вхідний запит.
func (c *Client) RespondInvite(inviteID string, accept bool) error {
	if !ValidInviteID(inviteID) {
		return ErrBadInput
	}
	return c.call(http.MethodPost, "/friends/invite/"+url.PathEscape(inviteID)+"/resp", map[string]bool{"accept": accept}, nil)
}

// RemoveFriend видаляє дружбу.
func (c *Client) RemoveFriend(accountID string) error {
	if !ValidAccountID(accountID) {
		return ErrBadInput
	}
	return c.call(http.MethodPost, "/friends/remove", map[string]string{"friendId": accountID}, nil)
}

// GetPrivacy читає налаштування приватності з Worker. Старий Worker без
// GET /friends/presence-pref відповідає 404 → ErrNotSupported (UI тоді бере
// локальну копію й чесно про це каже).
func (c *Client) GetPrivacy() (Privacy, error) {
	var r struct {
		ShowStatusTo   string `json:"showStatusTo"`
		ShowServerTo   string `json:"showServerTo"`
		FriendRequests string `json:"friendRequests"`
		WhoCanMessage  string `json:"whoCanMessage"`
	}
	err := c.call(http.MethodGet, "/friends/presence-pref", nil, &r)
	if err != nil {
		var ae *APIError
		if errors.As(err, &ae) && ae.Status == http.StatusNotFound {
			return Privacy{}, ErrNotSupported
		}
		return Privacy{}, err
	}
	return NormalizePrivacy(Privacy{ShowStatusTo: r.ShowStatusTo, ShowServerTo: r.ShowServerTo, FriendRequest: r.FriendRequests, WhoCanMessage: r.WhoCanMessage}), nil
}

// SetPrivacy зберігає приватність. Шлемо ТІЛЬКИ три «друзівські» поля —
// решту (чат/сповіщення) Worker лишає як є (часткове оновлення).
func (c *Client) SetPrivacy(p Privacy) error {
	if err := ValidatePrivacy(p); err != nil {
		return err
	}
	if p.WhoCanMessage == "" {
		p.WhoCanMessage = "friends"
	}
	return c.call(http.MethodPost, "/friends/presence-pref", map[string]string{
		"showStatusTo":   p.ShowStatusTo,
		"showServerTo":   p.ShowServerTo,
		"friendRequests": p.FriendRequest,
		"whoCanMessage":  p.WhoCanMessage,
	}, nil)
}

// AccountIDFromJWT дістає `sub` з access-JWT БЕЗ перевірки підпису. Це лише
// для локального відображення («це я»); жодне рішення про права ним не
// приймається — підпис перевіряє Worker. Повертає "" для будь-якого сміття.
func AccountIDFromJWT(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return ""
	}
	raw, err := b64URLDecode(parts[1])
	if err != nil {
		return ""
	}
	var p struct {
		Sub string `json:"sub"`
	}
	if json.Unmarshal(raw, &p) != nil || !ValidAccountID(p.Sub) {
		return ""
	}
	return p.Sub
}
