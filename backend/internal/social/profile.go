package social

// profile.go — профіль ДРУГА (вікно «Профіль») і приватність власного профілю.
//
// Рішення «що показувати» приймає СЕРВЕР (vps/internal/friends/profile.go): якщо друг закрив
// профіль, сервер не віддає ні біо, ні колір, ні статистику — тут вони просто відсутні, а не
// відфільтровані. Клієнт лише очищає текст від керуючих символів і нічого не вгадує.

import (
	"errors"
	"net/http"
	"net/url"
)

// FriendProfile — відповідь GET /friends/profile/{id}.
// Hidden=true: друг закрив профіль — решта полів порожні/відсутні.
// Вказівники: nil = сервер цього не знає (напр. друг вимкнув хмарний ігровий час) → UI показує «—».
type FriendProfile struct {
	AccountID    string `json:"accountId"`
	Nickname     string `json:"nickname"`
	Handle       string `json:"handle,omitempty"`
	Since        int64  `json:"since"`
	Hidden       bool   `json:"hidden"`
	Bio          string `json:"bio,omitempty"`
	Color        string `json:"color,omitempty"`
	CreatedAt    *int64 `json:"createdAt,omitempty"`
	GameSeconds  *int64 `json:"gameSeconds,omitempty"`
	BuildsCount  *int   `json:"buildsCount,omitempty"`
	FriendsCount *int   `json:"friendsCount,omitempty"`
}

// ProfileVisibility — хто бачить мій профіль: friends | nobody.
const (
	ProfileVisFriends = "friends"
	ProfileVisNobody  = "nobody"
)

func validProfileVis(v string) bool { return v == ProfileVisFriends || v == ProfileVisNobody }

// GetFriendProfile читає профіль друга. Помилки сервера: 403 not_friends, 404 user_not_found.
func (c *Client) GetFriendProfile(accountID string) (FriendProfile, error) {
	if !ValidAccountID(accountID) {
		return FriendProfile{}, ErrBadInput
	}
	var p FriendProfile
	if err := c.call(http.MethodGet, "/friends/profile/"+url.PathEscape(accountID), nil, &p); err != nil {
		return FriendProfile{}, err
	}
	p.AccountID = accountID
	p.Nickname = CleanText(p.Nickname, MaxNicknameLen)
	p.Handle = CleanText(p.Handle, 64)
	if p.Hidden {
		// Навіть якщо старий/зламаний сервер щось додав — прихований профіль лишається порожнім.
		return FriendProfile{AccountID: p.AccountID, Nickname: p.Nickname, Handle: p.Handle, Since: p.Since, Hidden: true}, nil
	}
	p.Bio = SanitizeBio(p.Bio)
	p.Color = SanitizeColor(p.Color)
	return p, nil
}

// GetProfileVisibility читає, кому я показую свій профіль. Старий сервер (без поля) = friends.
func (c *Client) GetProfileVisibility() (string, error) {
	var r struct {
		ShowProfileTo string `json:"showProfileTo"`
	}
	if err := c.call(http.MethodGet, "/friends/presence-pref", nil, &r); err != nil {
		var ae *APIError
		if errors.As(err, &ae) && ae.Status == http.StatusNotFound {
			return "", ErrNotSupported
		}
		return "", err
	}
	if !validProfileVis(r.ShowProfileTo) {
		return ProfileVisFriends, nil
	}
	return r.ShowProfileTo, nil
}

// SetProfileVisibility зберігає приватність профілю (часткове оновлення: інші поля не чіпає).
func (c *Client) SetProfileVisibility(v string) error {
	if !validProfileVis(v) {
		return ErrBadInput
	}
	return c.call(http.MethodPost, "/friends/presence-pref", map[string]string{"showProfileTo": v}, nil)
}

// FriendProfile — профіль друга через активну сесію.
func (s *Service) FriendProfile(accountID string) (FriendProfile, error) {
	if !s.Running() {
		return FriendProfile{}, ErrNotLoggedIn
	}
	return s.client.GetFriendProfile(accountID)
}

// ProfileVisibility — поточна приватність мого профілю.
func (s *Service) ProfileVisibility() (string, error) {
	if !s.Running() {
		return "", ErrNotLoggedIn
	}
	return s.client.GetProfileVisibility()
}

// SetProfileVisibility — змінити приватність мого профілю.
func (s *Service) SetProfileVisibility(v string) error {
	if !s.Running() {
		return ErrNotLoggedIn
	}
	return s.client.SetProfileVisibility(v)
}
