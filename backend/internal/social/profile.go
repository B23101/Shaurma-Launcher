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

	// IsFriend=false — профіль не-друга (лише нік, @handle і спільні зв'язки).
	IsFriend bool `json:"isFriend"`
	// Спільні друзі/групи: кількість — завжди, імена й назви — якщо власник профілю дозволив.
	MutualCount       int            `json:"mutualCount"`
	MutualFriends     []MutualFriend `json:"mutualFriends,omitempty"`
	MutualGroupsCount int            `json:"mutualGroupsCount"`
	MutualGroups      []MutualGroup  `json:"mutualGroups,omitempty"`

	// Дії над профілем — рішення сервера за налаштуваннями власника профілю.
	CanMessage     bool `json:"canMessage"`
	CanRequest     bool `json:"canRequest"`
	RequestPending bool `json:"requestPending,omitempty"`
	// Presence — статус не-друга; є лише якщо власник показує його всім.
	Presence *StrangerPresence `json:"presence,omitempty"`
}

// StrangerPresence — статус «у мережі» людини, що відкрила його для всіх.
type StrangerPresence struct {
	Status     string `json:"status"` // online | away | in_game | offline
	PackName   string `json:"packName,omitempty"`
	PackColor  string `json:"packColor,omitempty"`
	DropInOpen bool   `json:"dropInOpen,omitempty"`
}

// sanitizePresence лишає лише відомі статуси й чистить текст; нічого, що можна використати як адресу сервера.
func sanitizePresence(p *StrangerPresence) *StrangerPresence {
	if p == nil || !inSet(p.Status, "online", "away", "in_game", "offline") {
		return nil
	}
	return &StrangerPresence{Status: p.Status, PackName: CleanText(p.PackName, MaxPackNameLen),
		PackColor: SanitizeColor(p.PackColor), DropInOpen: p.DropInOpen}
}

// MutualFriend / MutualGroup — елементи списків «спільні друзі» та «спільні групи».
type MutualFriend struct {
	AccountID string `json:"accountId"`
	Nickname  string `json:"nickname"`
}
type MutualGroup struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

const maxMutualShown = 12

// sanitizeMutual очищає списки від сміття сервера (довжини, керуючі символи, чужі id).
func sanitizeMutual(p *FriendProfile) {
	if p.MutualCount < 0 {
		p.MutualCount = 0
	}
	if p.MutualGroupsCount < 0 {
		p.MutualGroupsCount = 0
	}
	fr := make([]MutualFriend, 0, len(p.MutualFriends))
	for _, f := range p.MutualFriends {
		if len(fr) >= maxMutualShown || !ValidAccountID(f.AccountID) {
			continue
		}
		nick := CleanText(f.Nickname, MaxNicknameLen)
		if nick == "" {
			nick = "?"
		}
		fr = append(fr, MutualFriend{AccountID: f.AccountID, Nickname: nick})
	}
	p.MutualFriends = fr
	gr := make([]MutualGroup, 0, len(p.MutualGroups))
	for _, g := range p.MutualGroups {
		title := CleanText(g.Title, 60)
		if len(gr) >= maxMutualShown || g.ID == "" || len(g.ID) > 64 || title == "" {
			continue
		}
		gr = append(gr, MutualGroup{ID: g.ID, Title: title})
	}
	p.MutualGroups = gr
}

// ProfileVisibility — хто бачить мій профіль: friends | nobody.
const (
	ProfileVisFriends = "friends"
	ProfileVisNobody  = "nobody"
)

func validProfileVis(v string) bool { return v == "everyone" || v == ProfileVisFriends || v == ProfileVisNobody }

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
	sanitizeMutual(&p)
	p.Presence = sanitizePresence(p.Presence)
	if p.Hidden {
		// Навіть якщо старий/зламаний сервер щось додав — прихований профіль лишається порожнім,
		// у тому числі без спільних друзів і груп.
		return FriendProfile{AccountID: p.AccountID, Nickname: p.Nickname, Handle: p.Handle, Since: p.Since, Hidden: true,
			IsFriend: p.IsFriend, CanMessage: p.CanMessage, CanRequest: p.CanRequest, RequestPending: p.RequestPending}, nil
	}
	if !p.IsFriend {
		// Профіль не-друга: лише те, що не потребує дружби.
		return FriendProfile{AccountID: p.AccountID, Nickname: p.Nickname, Handle: p.Handle,
			MutualCount: p.MutualCount, MutualFriends: p.MutualFriends,
			MutualGroupsCount: p.MutualGroupsCount, MutualGroups: p.MutualGroups,
			CanMessage: p.CanMessage, CanRequest: p.CanRequest, RequestPending: p.RequestPending,
			Presence: p.Presence}, nil
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

// GetMutualVisibility читає, кому я показую імена спільних друзів і назви спільних груп.
func (c *Client) GetMutualVisibility() (string, error) {
	var r struct {
		ShowMutualTo string `json:"showMutualTo"`
	}
	if err := c.call(http.MethodGet, "/friends/presence-pref", nil, &r); err != nil {
		return "", err
	}
	if !validProfileVis(r.ShowMutualTo) {
		return ProfileVisFriends, nil
	}
	return r.ShowMutualTo, nil
}

// SetMutualVisibility зберігає це налаштування (часткове оновлення).
func (c *Client) SetMutualVisibility(v string) error {
	if !validProfileVis(v) {
		return ErrBadInput
	}
	return c.call(http.MethodPost, "/friends/presence-pref", map[string]string{"showMutualTo": v}, nil)
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

// MutualVisibility — кому я показую спільних друзів і групи.
func (s *Service) MutualVisibility() (string, error) {
	if !s.Running() {
		return "", ErrNotLoggedIn
	}
	return s.client.GetMutualVisibility()
}

// SetMutualVisibility — змінити це налаштування.
func (s *Service) SetMutualVisibility(v string) error {
	if !s.Running() {
		return ErrNotLoggedIn
	}
	return s.client.SetMutualVisibility(v)
}
