package social

// comments.go — коментарі в профілі друга (клієнтська частина). Ліміти примусово тримає сервер
// (vps/internal/friends/comments.go); тут — санітизація відповіді (сервер не довірений для
// відображення, див. types.go) і швидка клієнтська перевірка довжини, щоб не слати очевидно
// негідні запити.

import (
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"
)

// MaxCommentRunes — найбільша довжина коментаря в символах (узгоджено з сервером).
const MaxCommentRunes = 40

// maxCommentsShown — скільки коментарів показуємо максимум (сервер віддає не більше 30).
const maxCommentsShown = 30

// CommentEmoji — набір реакцій під коментарем. Рядки мають збігатися із сервером символ-у-символ.
var CommentEmoji = []string{"👍", "❤️", "😂", "😮", "😢", "🔥"}

func validCommentEmoji(e string) bool {
	for _, v := range CommentEmoji {
		if v == e {
			return true
		}
	}
	return false
}

// ProfileComment — один коментар у профілі.
type ProfileComment struct {
	ID        int64  `json:"id"`
	AuthorID  string `json:"authorId"`
	Nickname  string `json:"nickname"`
	Handle    string `json:"handle,omitempty"`
	Text      string `json:"text"`
	CreatedAt int64  `json:"createdAt"`
	// Reactions — кількість реакцій за емодзі; Mine — моя реакція (порожньо, якщо немає).
	Reactions map[string]int `json:"reactions,omitempty"`
	Mine      string         `json:"mine,omitempty"`
	CanDelete bool           `json:"canDelete"`
}

// ProfileComments — відповідь GET /friends/comments/{id}.
type ProfileComments struct {
	Comments []ProfileComment `json:"comments"`
	// MyCount — мої живі коментарі в цьому профілі; CanPost — чи можна написати ще один.
	MyCount int  `json:"myCount"`
	CanPost bool `json:"canPost"`
}

func sanitizeComments(in ProfileComments) ProfileComments {
	out := ProfileComments{Comments: []ProfileComment{}, MyCount: in.MyCount, CanPost: in.CanPost}
	if out.MyCount < 0 {
		out.MyCount = 0
	}
	for _, c := range in.Comments {
		if len(out.Comments) >= maxCommentsShown {
			break
		}
		if c.ID <= 0 || !ValidAccountID(c.AuthorID) {
			continue
		}
		text := CleanText(c.Text, MaxCommentRunes)
		if text == "" {
			continue
		}
		nick := CleanText(c.Nickname, MaxNicknameLen)
		if nick == "" {
			nick = "(?)"
		}
		sc := ProfileComment{ID: c.ID, AuthorID: c.AuthorID, Nickname: nick, Handle: CleanText(c.Handle, 64),
			Text: text, CreatedAt: c.CreatedAt, CanDelete: c.CanDelete}
		for e, n := range c.Reactions {
			if n > 0 && validCommentEmoji(e) {
				if sc.Reactions == nil {
					sc.Reactions = map[string]int{}
				}
				sc.Reactions[e] = n
			}
		}
		if validCommentEmoji(c.Mine) {
			sc.Mine = c.Mine
		}
		out.Comments = append(out.Comments, sc)
	}
	return out
}

// ListProfileComments читає коментарі профілю. Помилки сервера: 403 not_friends.
func (c *Client) ListProfileComments(accountID string) (ProfileComments, error) {
	if !ValidAccountID(accountID) {
		return ProfileComments{}, ErrBadInput
	}
	var res ProfileComments
	if err := c.call(http.MethodGet, "/friends/comments/"+url.PathEscape(accountID), nil, &res); err != nil {
		return ProfileComments{}, err
	}
	return sanitizeComments(res), nil
}

// PostProfileComment залишає коментар у профілі друга. Довжину й порожнечу перевіряємо і тут, і на сервері.
func (c *Client) PostProfileComment(accountID, text string) error {
	if !ValidAccountID(accountID) {
		return ErrBadInput
	}
	t := strings.Join(strings.Fields(text), " ")
	t = CleanText(t, 1<<10)
	if t == "" {
		return &APIError{Status: http.StatusBadRequest, Code: "empty_text"}
	}
	if utf8.RuneCountInString(t) > MaxCommentRunes {
		return &APIError{Status: http.StatusBadRequest, Code: "text_too_long"}
	}
	return c.call(http.MethodPost, "/friends/comments", map[string]string{"profileId": accountID, "text": t}, nil)
}

// DeleteProfileComment видаляє коментар (свій або під моїм профілем).
func (c *Client) DeleteProfileComment(commentID int64) error {
	if commentID <= 0 {
		return ErrBadInput
	}
	return c.call(http.MethodPost, "/friends/comment-delete", map[string]int64{"commentId": commentID}, nil)
}

// ReactProfileComment ставить реакцію під коментарем; порожній emoji знімає мою реакцію.
func (c *Client) ReactProfileComment(commentID int64, emoji string) error {
	if commentID <= 0 || (emoji != "" && !validCommentEmoji(emoji)) {
		return ErrBadInput
	}
	return c.call(http.MethodPost, "/friends/comment-react", map[string]any{"commentId": commentID, "emoji": emoji}, nil)
}

// ProfileComments — коментарі профілю через активну сесію.
func (s *Service) ProfileComments(accountID string) (ProfileComments, error) {
	if !s.Running() {
		return ProfileComments{}, ErrNotLoggedIn
	}
	return s.client.ListProfileComments(accountID)
}

// PostProfileComment — залишити коментар через активну сесію.
func (s *Service) PostProfileComment(accountID, text string) error {
	if !s.Running() {
		return ErrNotLoggedIn
	}
	return s.client.PostProfileComment(accountID, text)
}

// DeleteProfileComment — видалити коментар через активну сесію.
func (s *Service) DeleteProfileComment(commentID int64) error {
	if !s.Running() {
		return ErrNotLoggedIn
	}
	return s.client.DeleteProfileComment(commentID)
}

// ReactProfileComment — поставити/зняти реакцію через активну сесію.
func (s *Service) ReactProfileComment(commentID int64, emoji string) error {
	if !s.Running() {
		return ErrNotLoggedIn
	}
	return s.client.ReactProfileComment(commentID, emoji)
}
