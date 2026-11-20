package social

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Регрес: presence-WS зупиняється назавжди на першому not_logged_in (напр. VPS відповів 401 через
// збій БД). HTTP-опитування друзів потім відновлюється, тож «друзі видно, а чат/групи — без входу»,
// доки не створять новий Service. Опитування має само піднімати WS, коли сесія знову жива.
func TestServiceRevivesStoppedWebSocket(t *testing.T) {
	w := &fakeWorker{
		friends: []string{fr(idFriend, "bob")},
		invites: `[]`,
		privacy: `{"showStatusTo":"friends","showServerTo":"friends","friendRequests":"everyone"}`,
	}
	hs := httptest.NewServer(http.HandlerFunc(w.handler))
	t.Cleanup(hs.Close)
	ws := newWSHarness(t)

	var broken atomic.Bool
	broken.Store(true)
	svc := NewService(Deps{
		Request: func(method, path string, body []byte) (*http.Response, error) {
			req, _ := http.NewRequest(method, hs.URL+path, strings.NewReader(string(body)))
			return http.DefaultClient.Do(req)
		},
		Token: func() (string, error) {
			if broken.Load() {
				return "", ErrNotLoggedIn
			}
			return jwtWith(idMe), nil
		},
		WSURL:           ws.url(),
		Emit:            func(string, any) {},
		PollEvery:       30 * time.Millisecond,
		allowInsecureWS: true,
	})
	t.Cleanup(svc.Stop)
	if err := svc.Start(); err != nil {
		t.Fatal(err)
	}

	// WS-цикл вийшов на not_logged_in і лишається вимкненим, поки сесії немає.
	time.Sleep(200 * time.Millisecond)
	if c := svc.Snapshot().Conn; c == ConnOnline {
		t.Fatalf("без сесії WS не має бути онлайн: %v", c)
	}

	// Сесія відновилась (сервер ожив) — без нового Service WS має піднятись сам.
	broken.Store(false)
	waitFor(t, "WS піднявся сам", func() bool { return svc.Snapshot().Conn == ConnOnline })
}
