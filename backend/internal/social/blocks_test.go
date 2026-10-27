package social

import (
	"errors"
	"io"
	"net/http"
	"testing"
)

func TestClientBlocksRoundTrip(t *testing.T) {
	var gotPath, gotBody string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.Method + " " + r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		switch r.URL.Path {
		case "/friends/blocks":
			w.Write([]byte(`{"blocks":[{"accountId":"` + idMe + `","nickname":"Bob","since":5},{"accountId":"bad","nickname":"x"}]}`))
		case "/friends/peer/" + idMe:
			w.Write([]byte(`{"accountId":"` + idMe + `","nickname":"Bob","handle":"bob_1"}`))
		default:
			w.Write([]byte(`{"ok":true}`))
		}
	})
	if err := c.BlockAccount(idMe); err != nil || gotPath != "POST /friends/block" || gotBody == "" {
		t.Fatalf("block: %v %q %q", err, gotPath, gotBody)
	}
	if err := c.UnblockAccount(idMe); err != nil || gotPath != "POST /friends/unblock" {
		t.Fatalf("unblock: %v %q", err, gotPath)
	}
	list, err := c.ListBlocks()
	if err != nil || len(list) != 1 || list[0].Nickname != "Bob" {
		t.Fatalf("list (невалідний id має відсіятись): %v %+v", err, list)
	}
	p, err := c.GetPeerProfile(idMe)
	if err != nil || p.Handle != "bob_1" {
		t.Fatalf("peer: %v %+v", err, p)
	}
	if !errors.Is(c.BlockAccount("nope"), ErrBadInput) || !errors.Is(c.UnblockAccount(""), ErrBadInput) {
		t.Error("невалідний accountId пішов на сервер")
	}
}

func TestClientSearchUsers(t *testing.T) {
	var gotQuery string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Write([]byte(`{"users":[{"accountId":"` + idMe + `","nickname":"Bob","handle":"bob_1"},{"accountId":"bad","nickname":"x"}]}`))
	})
	if hits, err := c.SearchUsers("b"); err != nil || len(hits) != 0 || gotQuery != "" {
		t.Fatalf("короткий запит не мав іти на сервер: %v %v %q", err, hits, gotQuery)
	}
	hits, err := c.SearchUsers("bo b")
	if err != nil || len(hits) != 1 || hits[0].Handle != "bob_1" {
		t.Fatalf("пошук: %v %+v", err, hits)
	}
	if gotQuery != "q=bo+b" {
		t.Errorf("запит не екрановано: %q", gotQuery)
	}
}
