package social

// blocks.go — блокування акаунтів і профіль співрозмовника-не-друга.
//
// Блокування на сервері розриває дружбу й забороняє нові запити та
// повідомлення в обидва боки; історія чату лишається доступною для читання.

import (
	"net/http"
	"net/url"
)

// BlockedAccount — акаунт зі списку блокувань.
type BlockedAccount struct {
	AccountID string `json:"accountId"`
	Nickname  string `json:"nickname"`
	Handle    string `json:"handle,omitempty"`
	Since     int64  `json:"since"`
}

// PeerProfile — мінімальний профіль співрозмовника, який не є другом.
type PeerProfile struct {
	AccountID string `json:"accountId"`
	Nickname  string `json:"nickname"`
	Handle    string `json:"handle,omitempty"`
}

// BlockAccount блокує акаунт (і розриває дружбу, якщо вона була).
func (c *Client) BlockAccount(accountID string) error {
	if !ValidAccountID(accountID) {
		return ErrBadInput
	}
	return c.call(http.MethodPost, "/friends/block", map[string]string{"accountId": accountID}, nil)
}

// UnblockAccount знімає блокування.
func (c *Client) UnblockAccount(accountID string) error {
	if !ValidAccountID(accountID) {
		return ErrBadInput
	}
	return c.call(http.MethodPost, "/friends/unblock", map[string]string{"accountId": accountID}, nil)
}

// ListBlocks повертає список заблокованих мною акаунтів.
func (c *Client) ListBlocks() ([]BlockedAccount, error) {
	var r struct {
		Blocks []BlockedAccount `json:"blocks"`
	}
	if err := c.call(http.MethodGet, "/friends/blocks", nil, &r); err != nil {
		return nil, err
	}
	out := make([]BlockedAccount, 0, len(r.Blocks))
	for _, b := range r.Blocks {
		if !ValidAccountID(b.AccountID) {
			continue
		}
		b.Nickname = CleanText(b.Nickname, MaxNicknameLen)
		b.Handle = CleanText(b.Handle, 64)
		out = append(out, b)
	}
	return out, nil
}

// GetPeerProfile — нік і @handle співрозмовника (для чату з не-другом).
func (c *Client) GetPeerProfile(accountID string) (PeerProfile, error) {
	if !ValidAccountID(accountID) {
		return PeerProfile{}, ErrBadInput
	}
	var p PeerProfile
	if err := c.call(http.MethodGet, "/friends/peer/"+accountID, nil, &p); err != nil {
		return PeerProfile{}, err
	}
	p.AccountID = accountID
	p.Nickname = CleanText(p.Nickname, MaxNicknameLen)
	p.Handle = CleanText(p.Handle, 64)
	return p, nil
}

// Block блокує акаунт і одразу прибирає його з локального списку друзів.
func (s *Service) Block(accountID string) error {
	if !s.Running() {
		return ErrNotLoggedIn
	}
	if err := s.client.BlockAccount(accountID); err != nil {
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

// Unblock знімає блокування (дружба сама не повертається).
func (s *Service) Unblock(accountID string) error {
	if !s.Running() {
		return ErrNotLoggedIn
	}
	return s.client.UnblockAccount(accountID)
}

// Blocks — список заблокованих акаунтів.
func (s *Service) Blocks() ([]BlockedAccount, error) {
	if !s.Running() {
		return nil, ErrNotLoggedIn
	}
	return s.client.ListBlocks()
}

// PeerProfile — профіль співрозмовника, що не є другом.
func (s *Service) PeerProfile(accountID string) (PeerProfile, error) {
	if !s.Running() {
		return PeerProfile{}, ErrNotLoggedIn
	}
	return s.client.GetPeerProfile(accountID)
}

// UserHit — результат пошуку гравця для «Додати друга».
type UserHit struct {
	AccountID string `json:"accountId"`
	Nickname  string `json:"nickname"`
	Handle    string `json:"handle,omitempty"`
}

// SearchUsers — префіксний пошук за ніком або @handle (від 2 символів). Сервер
// сам відсіює друзів, заблокованих і тих, хто закрив запити в друзі.
func (c *Client) SearchUsers(query string) ([]UserHit, error) {
	q := CleanText(query, 32)
	if len([]rune(q)) < 2 {
		return []UserHit{}, nil
	}
	var r struct {
		Users []UserHit `json:"users"`
	}
	if err := c.call(http.MethodGet, "/friends/search?q="+url.QueryEscape(q), nil, &r); err != nil {
		return nil, err
	}
	out := make([]UserHit, 0, len(r.Users))
	for _, u := range r.Users {
		if !ValidAccountID(u.AccountID) || len(out) >= 8 {
			continue
		}
		u.Nickname = CleanText(u.Nickname, MaxNicknameLen)
		u.Handle = CleanText(u.Handle, 64)
		out = append(out, u)
	}
	return out, nil
}

// SearchUsers — пошук гравців (вимагає входу).
func (s *Service) SearchUsers(query string) ([]UserHit, error) {
	if !s.Running() {
		return nil, ErrNotLoggedIn
	}
	return s.client.SearchUsers(query)
}
