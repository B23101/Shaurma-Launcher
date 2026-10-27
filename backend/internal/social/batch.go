package social

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync/atomic"
	"time"
)

// batchOffUntil — до цього моменту (unix ms) мікс-запит не пробуємо: вузол його не знає (404).
var batchOffUntil atomic.Int64

// RefreshBundle — друзі + вхідні запити (+ приватність) ОДНИМ HTTP-запитом /friends/batch.
// Кожна частина розбирається тим самим кодом, що й окремий запит (ті самі перевірки).
// Старий вузол без /friends/batch → прозоро по одному запиту, як раніше.
func (c *Client) RefreshBundle(withPrivacy bool) (friends []Friend, invites []Invite, priv Privacy, ferr, ierr, perr error) {
	paths := []string{"/friends/list", "/friends/invites"}
	if withPrivacy {
		paths = append(paths, "/friends/presence-pref")
	}
	if replay, err := c.batch(paths); err == nil {
		local := &Client{do: replay}
		friends, ferr = local.ListFriends()
		invites, ierr = local.ListInvites()
		if withPrivacy {
			priv, perr = local.GetPrivacy()
		}
		return
	}
	friends, ferr = c.ListFriends()
	invites, ierr = c.ListInvites()
	if withPrivacy {
		priv, perr = c.GetPrivacy()
	}
	return
}

var errBatchUnsupported = errors.New("social:batch_unsupported")

type batchItem struct {
	Path   string          `json:"p"`
	Status int             `json:"s"`
	Body   json.RawMessage `json:"b"`
}

// batch виконує мікс-запит і повертає Requester, що «відтворює» відповіді кожного шляху.
func (c *Client) batch(paths []string) (Requester, error) {
	if c == nil || c.do == nil {
		return nil, ErrNotLoggedIn
	}
	if time.Now().UnixMilli() < batchOffUntil.Load() {
		return nil, errBatchUnsupported
	}
	if c.budget != nil && !c.budget.Allow() {
		return nil, ErrBudgetExhausted
	}
	body, _ := json.Marshal(map[string][]string{"get": paths})
	resp, err := c.do(http.MethodPost, "/friends/batch", body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxHTTPBody+1))
	if err != nil || len(data) > maxHTTPBody {
		return nil, errBatchUnsupported
	}
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
		batchOffUntil.Store(time.Now().Add(10 * time.Minute).UnixMilli())
		return nil, errBatchUnsupported
	}
	if resp.StatusCode != http.StatusOK {
		return nil, errBatchUnsupported
	}
	var out struct {
		R []batchItem `json:"r"`
	}
	if json.Unmarshal(data, &out) != nil || len(out.R) != len(paths) {
		return nil, errBatchUnsupported
	}
	got := map[string]batchItem{}
	for i, it := range out.R {
		if it.Path != paths[i] || it.Status < 100 || it.Status > 599 {
			return nil, errBatchUnsupported // відповідь не на те, що питали — не довіряємо
		}
		got[it.Path] = it
	}
	return func(method, path string, _ []byte) (*http.Response, error) {
		it, ok := got[path]
		if !ok || method != http.MethodGet {
			return nil, errBatchUnsupported
		}
		b := []byte(it.Body)
		if b == nil {
			b = []byte("{}")
		}
		return &http.Response{StatusCode: it.Status, Body: io.NopCloser(bytes.NewReader(b))}, nil
	}, nil
}
