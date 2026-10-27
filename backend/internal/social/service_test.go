package social

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// fakeWorker — мінімальний Worker: друзі, інвайти, приватність.
type fakeWorker struct {
	mu        sync.Mutex
	friends   []string // JSON-фрагменти
	invites   string
	privacy   string
	privCode  int
	privPosts []map[string]any
	calls     []string
	removeErr int
	listCode  int // ненульовий → /friends/list віддає цей HTTP-статус
}

func (w *fakeWorker) handler(rw http.ResponseWriter, r *http.Request) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls = append(w.calls, r.Method+" "+r.URL.Path)
	switch {
	case r.URL.Path == "/friends/list":
		if w.listCode != 0 {
			rw.WriteHeader(w.listCode)
			rw.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		rw.Write([]byte(`{"friends":[` + strings.Join(w.friends, ",") + `]}`))
	case r.URL.Path == "/friends/invites":
		rw.Write([]byte(`{"invites":` + w.invites + `}`))
	case r.URL.Path == "/friends/presence-pref" && r.Method == "GET":
		if w.privCode != 0 {
			rw.WriteHeader(w.privCode)
			rw.Write([]byte(`{"error":"x"}`))
			return
		}
		rw.Write([]byte(w.privacy))
	case r.URL.Path == "/friends/presence-pref":
		var m map[string]any
		json.NewDecoder(r.Body).Decode(&m)
		w.privPosts = append(w.privPosts, m)
		rw.Write([]byte(`{"ok":true}`))
	case r.URL.Path == "/friends/remove":
		if w.removeErr != 0 {
			rw.WriteHeader(w.removeErr)
			rw.Write([]byte(`{"error":"forbidden"}`))
			return
		}
		var m map[string]string
		json.NewDecoder(r.Body).Decode(&m)
		var kept []string
		for _, f := range w.friends {
			if !strings.Contains(f, m["friendId"]) {
				kept = append(kept, f)
			}
		}
		w.friends = kept
		rw.Write([]byte(`{"ok":true}`))
	case strings.HasPrefix(r.URL.Path, "/friends/invite/") && strings.HasSuffix(r.URL.Path, "/resp"):
		w.invites = `[]`
		w.friends = append(w.friends, `{"accountId":"`+idStr+`","nickname":"Carol","since":3}`)
		rw.Write([]byte(`{"ok":true}`))
	default:
		rw.WriteHeader(404)
		rw.Write([]byte(`{"error":"Not Found"}`))
	}
}

func fr(id, nick string) string {
	return `{"accountId":"` + id + `","nickname":"` + nick + `","since":1}`
}

type svcEnv struct {
	w   *fakeWorker
	ws  *wsHarness
	svc *Service
	mu  sync.Mutex
	evs []string
}

func (e *svcEnv) events() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.evs...)
}

func newSvcEnv(t *testing.T) *svcEnv {
	t.Helper()
	e := &svcEnv{w: &fakeWorker{
		friends: []string{fr(idFriend, "bob"), fr("44444444-aaaa-bbbb-cccc-000000000004", "Alice")},
		invites: `[]`,
		privacy: `{"showStatusTo":"friends","showServerTo":"friends","friendRequests":"everyone"}`,
	}}
	hs := httptest.NewServer(http.HandlerFunc(e.w.handler))
	t.Cleanup(hs.Close)
	e.ws = newWSHarness(t)
	e.svc = NewService(Deps{
		Request: func(method, path string, body []byte) (*http.Response, error) {
			req, _ := http.NewRequest(method, hs.URL+path, strings.NewReader(string(body)))
			return http.DefaultClient.Do(req)
		},
		Token: func() (string, error) { return jwtWith(idMe), nil }, // sub=idMe → Service знає «хто я» (chatId)
		WSURL: e.ws.url(),
		Emit: func(ev string, _ any) {
			e.mu.Lock()
			e.evs = append(e.evs, ev)
			e.mu.Unlock()
		},
		PollEvery:       time.Hour, // тести викликають Refresh вручну
		allowInsecureWS: true,
	})
	t.Cleanup(e.svc.Stop)
	return e
}

func TestServiceInitialStateSortedAndPrivacyLoaded(t *testing.T) {
	e := newSvcEnv(t)
	if err := e.svc.Start(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "друзі завантажені", func() bool { return len(e.svc.Snapshot().Friends) == 2 && e.svc.Snapshot().PrivacyLoaded })
	s := e.svc.Snapshot()
	if !s.LoggedIn || s.Friends[0].Nickname != "Alice" || s.Friends[1].Nickname != "bob" {
		t.Errorf("сортування за ніком без регістру: %+v", s.Friends)
	}
	if s.Error != "" {
		t.Errorf("помилка: %s", s.Error)
	}
}

func TestServiceHidesPackUntilPrivacyLoaded(t *testing.T) {
	e := newSvcEnv(t)
	e.w.privCode = 503 // приватність не читається
	e.svc.GameStarted("b1", "SecretPack", "#ff0000")
	e.svc.Start()
	f := e.ws.waitFrame(t, "set_status")
	if f["packName"] != "" || f["status"] != "online" {
		t.Errorf("до читання приватності збірка не має витікати: %v", f)
	}
	if e.svc.Snapshot().PrivacyLoaded {
		t.Error("PrivacyLoaded має бути false, якщо сервер не відповів")
	}

	// Сервер ожив — наступний Refresh читає приватність і відкриває збірку.
	e.w.mu.Lock()
	e.w.privCode = 0
	e.w.mu.Unlock()
	e.svc.Refresh()
	waitFor(t, "збірка стала видимою після читання приватності", func() bool {
		e.ws.mu.Lock()
		defer e.ws.mu.Unlock()
		for _, fr := range e.ws.frames {
			if fr["type"] == "set_status" && fr["packName"] == "SecretPack" {
				return true
			}
		}
		return false
	})
}

func TestServiceRespectsServerShowToNobody(t *testing.T) {
	e := newSvcEnv(t)
	e.w.privacy = `{"showStatusTo":"friends","showServerTo":"nobody","friendRequests":"everyone"}`
	e.svc.GameStarted("b1", "Pack", "#00ff00")
	e.svc.Start()
	waitFor(t, "privacy", func() bool { return e.svc.Snapshot().PrivacyLoaded })
	time.Sleep(150 * time.Millisecond)
	e.ws.mu.Lock()
	defer e.ws.mu.Unlock()
	for _, f := range e.ws.frames {
		if f["type"] == "set_status" && f["packName"] != "" {
			t.Errorf("showServerTo=nobody, а збірка пішла: %v", f)
		}
	}
}

func TestServiceInviteEventOnlyWhenNew(t *testing.T) {
	e := newSvcEnv(t)
	e.svc.Start()
	waitFor(t, "перший refresh", func() bool { return len(e.svc.Snapshot().Friends) == 2 })
	count := func() int {
		n := 0
		for _, ev := range e.events() {
			if ev == "friends:invite" {
				n++
			}
		}
		return n
	}
	if count() != 0 {
		t.Fatal("без запитів події быть не має")
	}
	e.w.mu.Lock()
	e.w.invites = `[{"id":"aaaaaaaa-1111","fromAccountId":"` + idStr + `","fromNickname":"Carol","createdAt":1}]`
	e.w.mu.Unlock()
	e.svc.Refresh()
	e.svc.Refresh() // той самий запит удруге — події не буде
	if count() != 1 {
		t.Errorf("подія про новий запит має бути рівно одна, є %d", count())
	}
	if len(e.svc.Snapshot().Invites) != 1 {
		t.Error("запит не в стані")
	}
}

func TestServiceRespondAcceptsAndRefreshes(t *testing.T) {
	e := newSvcEnv(t)
	e.w.invites = `[{"id":"aaaaaaaa-1111","fromAccountId":"` + idStr + `","fromNickname":"Carol","createdAt":1}]`
	e.svc.Start()
	waitFor(t, "запит", func() bool { return len(e.svc.Snapshot().Invites) == 1 })
	waitFor(t, "WS online", func() bool { return e.svc.Snapshot().Conn == ConnOnline })
	e.w.mu.Lock()
	listCalls := 0
	for _, c := range e.w.calls {
		if c == "GET /friends/list" {
			listCalls++
		}
	}
	e.w.mu.Unlock()
	if err := e.svc.Respond("aaaaaaaa-1111", true); err != nil {
		t.Fatal(err)
	}
	// Запит зникає миттєво, а Worker НЕ опитується: нового друга VPS пришле знімком.
	if len(e.svc.Snapshot().Invites) != 0 {
		t.Error("прийнятий запит має зникнути одразу")
	}
	e.w.mu.Lock()
	after := 0
	for _, c := range e.w.calls {
		if c == "GET /friends/list" {
			after++
		}
	}
	e.w.mu.Unlock()
	if after != listCalls {
		t.Errorf("при живому WS Worker не опитується: було %d, стало %d", listCalls, after)
	}
	e.ws.push(t, `{"type":"friends_snapshot","friends":[`+
		`{"accountId":"`+idFriend+`","nickname":"bob","status":"offline"},`+
		`{"accountId":"`+idStr+`","nickname":"Carol","since":3,"status":"online"}],"invites":[],"unread":[],`+
		`"privacy":{"showStatusTo":"friends","showServerTo":"friends","friendRequests":"everyone"}}`)
	waitFor(t, "новий друг зі знімка", func() bool { return len(e.svc.Snapshot().Friends) == 2 })
}

// Регресія на дедлок: видалення друга, який зараз онлайн, викликає OnPresence
// усередині SetFriends — це не має відбуватись під s.mu.
func TestServiceRemoveOnlineFriendNoDeadlock(t *testing.T) {
	e := newSvcEnv(t)
	e.ws.onAccept = func(ctx context.Context, c *websocket.Conn) {
		c.Write(ctx, websocket.MessageText, []byte(`{"type":"friend_presence","accountId":"`+idFriend+`","status":"online"}`))
	}
	e.svc.Start()
	waitFor(t, "bob онлайн", func() bool {
		for _, f := range e.svc.Snapshot().Friends {
			if f.AccountID == idFriend && f.Presence.Status == StatusOnline {
				return true
			}
		}
		return false
	})
	done := make(chan error, 1)
	go func() { done <- e.svc.Remove(idFriend) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("ДЕДЛОК при видаленні онлайн-друга")
	}
	for _, f := range e.svc.Snapshot().Friends {
		if f.AccountID == idFriend {
			t.Error("друг лишився в списку")
		}
	}
}

func TestServiceSetPrivacyFailureKeepsOld(t *testing.T) {
	e := newSvcEnv(t)
	e.svc.Start()
	waitFor(t, "privacy", func() bool { return e.svc.Snapshot().PrivacyLoaded })
	if err := e.svc.SetPrivacy(Privacy{"friends", "nobody", "friends_of_friends", "friends"}); err != nil {
		t.Fatal(err)
	}
	if p := e.svc.Snapshot().Privacy; p.ShowServerTo != "nobody" || p.FriendRequest != "friends_of_friends" {
		t.Errorf("не застосовано: %+v", p)
	}
	if len(e.w.privPosts) != 1 || len(e.w.privPosts[0]) != 4 {
		t.Errorf("на Worker мають піти рівно 4 поля: %v", e.w.privPosts)
	}
	if err := e.svc.SetPrivacy(Privacy{"friends", "friends", "NOPE", "friends"}); err == nil {
		t.Error("невалідне значення пройшло")
	}
	if p := e.svc.Snapshot().Privacy; p.ShowServerTo != "nobody" {
		t.Errorf("невдале збереження зіпсувало стан: %+v", p)
	}
}

func TestServiceActionsRequireRunning(t *testing.T) {
	e := newSvcEnv(t)
	for _, err := range []error{e.svc.SendInvite("Bob"), e.svc.Respond("aaaaaaaa-1111", true), e.svc.Remove(idFriend), e.svc.SetPrivacy(DefaultPrivacy())} {
		if err != ErrNotLoggedIn {
			t.Errorf("без Start дії мають давати ErrNotLoggedIn, а є %v", err)
		}
	}
	if len(e.w.calls) != 0 {
		t.Errorf("без сесії на Worker не мало піти запитів: %v", e.w.calls)
	}
}

func TestServiceStopWipesState(t *testing.T) {
	e := newSvcEnv(t)
	e.svc.Start()
	waitFor(t, "друзі", func() bool { return len(e.svc.Snapshot().Friends) == 2 })
	e.svc.Stop()
	e.svc.Stop() // ідемпотентно
	s := e.svc.Snapshot()
	if s.LoggedIn || len(s.Friends) != 0 || len(s.Invites) != 0 || s.Me != "" || s.Conn != ConnOff {
		t.Errorf("після Stop стан має бути порожнім: %+v", s)
	}
	if e.svc.Running() {
		t.Error("Running після Stop")
	}
	if err := e.svc.Start(); err != nil { // можна запустити знову (повторний вхід)
		t.Fatal(err)
	}
	waitFor(t, "друзі після рестарту", func() bool { return len(e.svc.Snapshot().Friends) == 2 })
}

func TestServiceNetworkErrorKeepsOldFriends(t *testing.T) {
	e := newSvcEnv(t)
	var fail atomic.Bool
	e.svc.client = NewClient(func(method, path string, body []byte) (*http.Response, error) {
		if fail.Load() {
			return nil, ErrBadInput // будь-яка помилка
		}
		hs := httptest.NewRecorder()
		e.w.handler(hs, httptest.NewRequest(method, path, strings.NewReader(string(body))))
		return hs.Result(), nil
	})
	e.svc.Start()
	waitFor(t, "друзі", func() bool { return len(e.svc.Snapshot().Friends) == 2 })
	fail.Store(true)
	e.svc.Refresh()
	s := e.svc.Snapshot()
	if len(s.Friends) != 2 {
		t.Errorf("збій мережі не повинен стирати список: %d", len(s.Friends))
	}
	if s.Error == "" {
		t.Error("код помилки має бути виставлений")
	}
	fail.Store(false)
	e.svc.Refresh()
	if e.svc.Snapshot().Error != "" {
		t.Error("помилка має зникнути після успішного refresh")
	}
}

func TestServiceGameStatusMultipleBuilds(t *testing.T) {
	e := newSvcEnv(t)
	e.svc.Start()
	e.ws.waitFrame(t, "set_status")
	e.svc.GameStarted("a", "Old", "#111111")
	time.Sleep(5 * time.Millisecond)
	e.svc.GameStarted("b", "New", "#222222")
	waitFor(t, "найновіша збірка", func() bool {
		e.ws.mu.Lock()
		defer e.ws.mu.Unlock()
		last := e.ws.frames[len(e.ws.frames)-1]
		return last["packName"] == "New"
	})
	e.svc.GameExited("b")
	waitFor(t, "повернення до Old", func() bool {
		e.ws.mu.Lock()
		defer e.ws.mu.Unlock()
		return e.ws.frames[len(e.ws.frames)-1]["packName"] == "Old"
	})
	e.svc.GameExited("a")
	waitFor(t, "online без збірки", func() bool {
		e.ws.mu.Lock()
		defer e.ws.mu.Unlock()
		l := e.ws.frames[len(e.ws.frames)-1]
		return l["status"] == "online" && l["packName"] == ""
	})
}

// Регресія: разова відмова 401 (токен ще оновлювався) скидала LoggedIn у false
// назавжди — навіть коли наступний Refresh успішний. Тепер сесія «оживає».
func TestServiceLoggedInRecoversAfterTransient401(t *testing.T) {
	e := newSvcEnv(t)
	e.w.mu.Lock()
	e.w.listCode = 401
	e.w.mu.Unlock()
	if err := e.svc.Start(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "401 → LoggedIn=false", func() bool {
		s := e.svc.Snapshot()
		return !s.LoggedIn && s.Error == "not_logged_in"
	})

	e.w.mu.Lock()
	e.w.listCode = 0
	e.w.mu.Unlock()
	e.svc.Refresh()

	s := e.svc.Snapshot()
	if !s.LoggedIn || s.Error != "" || len(s.Friends) != 2 {
		t.Errorf("після успішного Refresh сесія має ожити: loggedIn=%v err=%q friends=%d", s.LoggedIn, s.Error, len(s.Friends))
	}
}
