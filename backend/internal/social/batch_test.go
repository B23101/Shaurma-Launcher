package social

import (
	"bytes"
	"io"
	"net/http"
	"testing"
)

func TestRefreshBundle_BatchAndFallback(t *testing.T) {
	calls := map[string]int{}
	batchOK := true
	do := func(method, path string, body []byte) (*http.Response, error) {
		calls[path]++
		resp := func(code int, s string) (*http.Response, error) {
			return &http.Response{StatusCode: code, Body: io.NopCloser(bytes.NewBufferString(s))}, nil
		}
		switch path {
		case "/friends/batch":
			if !batchOK {
				return resp(404, `{"error":"not found"}`)
			}
			return resp(200, `{"r":[{"p":"/friends/list","s":200,"b":{"friends":[{"accountId":"a1b2c3d4-0000-4000-8000-000000000001","nickname":"Bob"}]}},{"p":"/friends/invites","s":200,"b":{"invites":[]}}]}`)
		case "/friends/list":
			return resp(200, `{"friends":[]}`)
		case "/friends/invites":
			return resp(200, `{"invites":[]}`)
		}
		return resp(404, `{}`)
	}
	c := NewClient(do)
	f, _, _, ferr, ierr, _ := c.RefreshBundle(false)
	if ferr != nil || ierr != nil || calls["/friends/batch"] != 1 || calls["/friends/list"] != 0 {
		t.Fatalf("batch: %v %v %v", ferr, ierr, calls)
	}
	if len(f) != 1 && ValidAccountID("a1b2c3d4-0000-4000-8000-000000000001") {
		t.Fatalf("друзі з батчу: %+v", f)
	}
	batchOK = false
	batchOffUntil.Store(0)
	_, _, _, ferr, ierr, _ = c.RefreshBundle(false)
	if ferr != nil || ierr != nil || calls["/friends/list"] != 1 {
		t.Fatalf("fallback: %v %v %v", ferr, ierr, calls)
	}
	_, _, _, _, _, _ = c.RefreshBundle(false) // 404 запам'ятано — батч не пробуємо 10 хв
	if calls["/friends/batch"] != 2 {
		t.Fatalf("після 404 батч мав вимкнутись: %v", calls)
	}
}
