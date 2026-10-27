package social

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

const (
	idMe     = "11111111-aaaa-bbbb-cccc-000000000001"
	idFriend = "22222222-aaaa-bbbb-cccc-000000000002"
	idStr    = "33333333-aaaa-bbbb-cccc-000000000003"
)

// ── Санітизація ─────────────────────────────────────────────────────────

func TestCleanText(t *testing.T) {
	cases := []struct {
		in   string
		max  int
		want string
	}{
		{"  Steve  ", 32, "Steve"},
		{"Pa\u202Eck", 32, "Pack"},       // bidi-override вирізано
		{"a\u200Bb\u200Fc", 32, "abc"},   // нульова ширина
		{"a\x00b\x1bc\x7fd", 32, "abcd"}, // керуючі
		{"ТестовийНік", 4, "Тест"},       // довжина в символах, не в байтах
		{"\ufeffX", 32, "X"},
		{"bad\xffutf", 32, "badutf"}, // недійсний UTF-8
		{"", 5, ""},
	}
	for _, c := range cases {
		if got := CleanText(c.in, c.max); got != c.want {
			t.Errorf("CleanText(%q,%d) = %q, want %q", c.in, c.max, got, c.want)
		}
	}
}

func TestValidators(t *testing.T) {
	for _, ok := range []string{idMe, "abcdef12", strings.Repeat("a", 64)} {
		if !ValidAccountID(ok) {
			t.Errorf("%q має бути валідним accountId", ok)
		}
	}
	for _, bad := range []string{"", "short", "UPPERCASE-ID-1234", "../etc/passwd", idMe + "?x=1", idMe + "/..", strings.Repeat("a", 65), "id with space"} {
		if ValidAccountID(bad) {
			t.Errorf("%q НЕ має бути валідним accountId", bad)
		}
	}
	if ValidInviteID("../../x") || ValidInviteID("a/b/c/d/e") || ValidInviteID("") {
		t.Error("ValidInviteID пропустив небезпечний рядок")
	}
	if SanitizeColor("#FF8a00") != "#ff8a00" || SanitizeColor("red") != "" || SanitizeColor("#12345") != "" || SanitizeColor("javascript:1") != "" {
		t.Error("SanitizeColor працює неправильно")
	}
	if SanitizeStatus("in_game") != "in_game" || SanitizeStatus("admin") != "" {
		t.Error("SanitizeStatus працює неправильно")
	}
}

func TestPrivacyValidation(t *testing.T) {
	if err := ValidatePrivacy(Privacy{"friends", "nobody", "everyone", "friends"}); err != nil {
		t.Errorf("валідне значення відхилено: %v", err)
	}
	if ValidatePrivacy(Privacy{"friends", "friends", "all", "friends"}) == nil || ValidatePrivacy(Privacy{"x", "friends", "everyone", "friends"}) == nil || ValidatePrivacy(Privacy{"friends", "friends", "everyone", "bogus"}) == nil {
		t.Error("невалідна приватність пройшла")
	}
	n := NormalizePrivacy(Privacy{"bogus", "everyone", "bogus", ""})
	if n.ShowStatusTo != "friends" || n.ShowServerTo != "everyone" || n.FriendRequest != "everyone" || n.WhoCanMessage != "friends" {
		t.Errorf("NormalizePrivacy: %+v", n)
	}
}

func jwtWith(sub string) string {
	enc := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	return enc(`{"alg":"HS256"}`) + "." + enc(`{"sub":"`+sub+`","typ":"access"}`) + ".sig"
}

func TestAccountIDFromJWT(t *testing.T) {
	if AccountIDFromJWT(jwtWith(idMe)) != idMe {
		t.Error("sub не витягнуто")
	}
	for _, bad := range []string{"", "a.b", "a.b.c.d", "x." + base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"../x"}`)) + ".s", jwtWith("BAD ID")} {
		if AccountIDFromJWT(bad) != "" {
			t.Errorf("сміття прийнято: %q", bad)
		}
	}
}

// ── HTTP-клієнт ─────────────────────────────────────────────────────────

func newTestClient(t *testing.T, h http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return NewClient(func(method, path string, body []byte) (*http.Response, error) {
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		return http.DefaultClient.Do(req)
	}), srv
}

func TestListFriendsSanitizesServerData(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/friends/list" || r.Method != "GET" {
			t.Errorf("не той запит: %s %s", r.Method, r.URL.Path)
		}
		w.Write([]byte(`{"friends":[
		 {"accountId":"` + idFriend + `","nickname":"Al\u202Eice","since":5},
		 {"accountId":"../evil","nickname":"Hacker","since":1},
		 {"accountId":"` + idStr + `","nickname":"   ","since":2}
		]}`))
	})
	fr, err := c.ListFriends()
	if err != nil {
		t.Fatal(err)
	}
	if len(fr) != 2 {
		t.Fatalf("очікували 2 валідних друзів, є %d: %+v", len(fr), fr)
	}
	if fr[0].Nickname != "Alice" {
		t.Errorf("bidi не вирізано: %q", fr[0].Nickname)
	}
	if fr[1].Nickname != "?" {
		t.Errorf("порожній нік має стати «?»: %q", fr[1].Nickname)
	}
	if fr[0].Presence.Status != StatusOffline {
		t.Error("новий друг має бути офлайн, доки VPS не скаже інакше")
	}
}

func TestListInvitesFiltersGarbage(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"invites":[
		 {"id":"aaaaaaaa-1111","fromAccountId":"` + idFriend + `","fromNickname":"Bob","createdAt":9},
		 {"id":"../x","fromAccountId":"` + idFriend + `","fromNickname":"Evil","createdAt":9}
		]}`))
	})
	inv, err := c.ListInvites()
	if err != nil || len(inv) != 1 || inv[0].FromNickname != "Bob" {
		t.Fatalf("invites: %+v err=%v", inv, err)
	}
}

func TestSendInviteByAccountIDOnly(t *testing.T) {
	var got map[string]string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(`{"inviteId":"x"}`))
	})
	// Нік не ідентифікує людину (він не унікальний) — за ним запросити не можна.
	for _, bad := range []string{"", "   ", "Steve", "Bob_01", "\u200b\u202e", strings.Repeat("a", 65), "zzzzzzzz-zzzz"} {
		if err := c.SendInvite(bad); !errors.Is(err, ErrBadInput) {
			t.Errorf("SendInvite(%q) мав відмовити, err=%v", bad, err)
		}
	}
	if got != nil {
		t.Fatal("на сервер не мало піти жодного запиту для невалідного ID")
	}
	const id = "0a1b2c3d-1111-4222-8333-444455556666"
	if err := c.SendInvite("  " + strings.ToUpper(id) + " \n"); err != nil {
		t.Fatal(err) // вставка з буфера: пробіли й регістр нормалізуються
	}
	if got["toAccountId"] != id || len(got) != 1 {
		t.Errorf("тіло запиту: %+v", got)
	}
}

func TestSendInviteByHandle(t *testing.T) {
	var got map[string]string
	calls := 0
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(`{"inviteId":"x"}`))
	})
	for _, bad := range []string{"", "ab", "bo..b", "боб", "bob smith", "../x"} {
		if err := c.SendInviteByHandle(bad); !errors.Is(err, ErrBadInput) {
			t.Errorf("SendInviteByHandle(%q) мав відмовити, err=%v", bad, err)
		}
	}
	if calls != 0 {
		t.Fatal("невалідний handle не має йти на сервер")
	}
	if err := c.SendInviteByHandle("  @Bob_4821 "); err != nil {
		t.Fatal(err)
	}
	if got["toHandle"] != "bob_4821" || len(got) != 1 {
		t.Errorf("тіло запиту: %+v", got)
	}
}

func TestSendInviteByHandleNotFound(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		w.Write([]byte(`{"error":"user_not_found"}`))
	})
	if err := c.SendInviteByHandle("bob_1"); err == nil || err.Error() != "social:user_not_found" {
		t.Errorf("err=%v", err)
	}
}

func TestListFriendsAndInvitesCarryHandle(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/list") {
			w.Write([]byte(`{"friends":[{"accountId":"` + idFriend + `","nickname":"Bob","handle":"bob_1","since":1},{"accountId":"` + idStr + `","nickname":"Eve","handle":"<b>x</b>","since":2}]}`))
			return
		}
		w.Write([]byte(`{"invites":[{"id":"aaaaaaaa-1111","fromAccountId":"` + idFriend + `","fromNickname":"Bob","fromHandle":"bob_1","createdAt":1}]}`))
	})
	fr, err := c.ListFriends()
	if err != nil || len(fr) != 2 || fr[0].Handle != "bob_1" || fr[1].Handle != "" {
		t.Fatalf("friends: %+v err=%v", fr, err)
	}
	inv, err := c.ListInvites()
	if err != nil || len(inv) != 1 || inv[0].FromHandle != "bob_1" {
		t.Fatalf("invites: %+v err=%v", inv, err)
	}
}

func TestAPIErrorMapping(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   string
	}{
		{409, `{"error":"already_friends"}`, "social:already_friends"},
		{403, `{"error":"invites_disabled"}`, "social:invites_disabled"},
		{404, `{"error":"account_not_found"}`, "social:account_not_found"},
		{500, `{"error":"<script>alert(1)</script>"}`, "social:unknown"}, // довільний текст не показуємо
		{500, `not json`, "social:unknown"},
		{429, `{}`, "social:rate_limited"},
	}
	for _, cs := range cases {
		c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(cs.status)
			w.Write([]byte(cs.body))
		})
		err := c.SendInvite("0a1b2c3d-1111-4222-8333-444455556666")
		if err == nil || err.Error() != cs.want {
			t.Errorf("status %d: err=%v, want %s", cs.status, err, cs.want)
		}
	}
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401) })
	if err := c.SendInvite("0a1b2c3d-1111-4222-8333-444455556666"); !errors.Is(err, ErrNotLoggedIn) {
		t.Errorf("401 → ErrNotLoggedIn, а є %v", err)
	}
}

func TestResponseSizeLimit(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"friends":[],"pad":"` + strings.Repeat("x", maxHTTPBody+10) + `"}`))
	})
	if _, err := c.ListFriends(); err == nil || !strings.Contains(err.Error(), "too_large") {
		t.Errorf("завелика відповідь мала бути відхилена, err=%v", err)
	}
}

func TestRespondAndRemovePaths(t *testing.T) {
	var path string
	var body map[string]any
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		body = nil
		json.NewDecoder(r.Body).Decode(&body)
		w.Write([]byte(`{"ok":true}`))
	})
	if err := c.RespondInvite("aaaaaaaa-1111", true); err != nil || path != "/friends/invite/aaaaaaaa-1111/resp" || body["accept"] != true {
		t.Errorf("respond: path=%s body=%v err=%v", path, body, err)
	}
	if !errors.Is(c.RespondInvite("../../admin", true), ErrBadInput) {
		t.Error("небезпечний id інвайту пройшов")
	}
	if err := c.RemoveFriend(idFriend); err != nil || path != "/friends/remove" || body["friendId"] != idFriend {
		t.Errorf("remove: path=%s body=%v err=%v", path, body, err)
	}
	if !errors.Is(c.RemoveFriend("nope"), ErrBadInput) {
		t.Error("невалідний accountId пройшов")
	}
}

func TestPrivacyRoundTrip(t *testing.T) {
	var posted map[string]any
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			w.Write([]byte(`{"showStatusTo":"nobody","showServerTo":"HACK","friendRequests":"friends_of_friends"}`))
			return
		}
		json.NewDecoder(r.Body).Decode(&posted)
		w.Write([]byte(`{"ok":true}`))
	})
	p, err := c.GetPrivacy()
	if err != nil {
		t.Fatal(err)
	}
	if p.ShowStatusTo != "nobody" || p.ShowServerTo != "friends" || p.FriendRequest != "friends_of_friends" {
		t.Errorf("GetPrivacy не нормалізував: %+v", p)
	}
	if err := c.SetPrivacy(Privacy{"friends", "nobody", "everyone", "everyone"}); err != nil {
		t.Fatal(err)
	}
	// Рівно чотири поля: чат-налаштування Worker не мають бути зачеплені.
	if len(posted) != 4 || posted["showServerTo"] != "nobody" || posted["whoCanMessage"] != "everyone" {
		t.Errorf("тіло SetPrivacy: %v", posted)
	}
	if !errors.Is(c.SetPrivacy(Privacy{"friends", "friends", "bogus", "friends"}), ErrBadInput) {
		t.Error("невалідна приватність пішла на сервер")
	}
}

func TestGetPrivacyOldWorker(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		w.Write([]byte(`{"error":"Not Found"}`))
	})
	if _, err := c.GetPrivacy(); !errors.Is(err, ErrNotSupported) {
		t.Errorf("404 → ErrNotSupported, а є %v", err)
	}
}

func TestClientWithoutRequester(t *testing.T) {
	if _, err := NewClient(nil).ListFriends(); !errors.Is(err, ErrNotLoggedIn) {
		t.Errorf("без сесії → ErrNotLoggedIn, а є %v", err)
	}
}

// ── WS: presence ───────────────────────────────────────────────────────

type wsHarness struct {
	srv      *httptest.Server
	mu       sync.Mutex
	hellos   []string // jwt із перших кадрів
	frames   []map[string]any
	conns    []*websocket.Conn
	onAccept func(ctx context.Context, c *websocket.Conn)
}

func newWSHarness(t *testing.T) *wsHarness {
	h := &wsHarness{}
	h.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		c.SetReadLimit(1 << 20)
		h.mu.Lock()
		h.conns = append(h.conns, c)
		h.mu.Unlock()
		ctx := r.Context()
		_, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		var hello map[string]any
		json.Unmarshal(data, &hello)
		h.mu.Lock()
		if hello["type"] == "hello" {
			h.hellos = append(h.hellos, hello["jwt"].(string))
		}
		cb := h.onAccept
		h.mu.Unlock()
		if cb != nil {
			go cb(ctx, c)
		}
		for {
			_, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			var m map[string]any
			json.Unmarshal(data, &m)
			h.mu.Lock()
			h.frames = append(h.frames, m)
			h.mu.Unlock()
		}
	}))
	t.Cleanup(h.srv.Close)
	return h
}

// push шле кадр від імені «VPS» у перше підключене з'єднання.
func (h *wsHarness) push(t *testing.T, frame string) {
	t.Helper()
	waitFor(t, "WS-з'єднання", func() bool { h.mu.Lock(); defer h.mu.Unlock(); return len(h.conns) > 0 })
	h.mu.Lock()
	c := h.conns[len(h.conns)-1]
	h.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.Write(ctx, websocket.MessageText, []byte(frame)); err != nil {
		t.Fatalf("push: %v", err)
	}
}

func (h *wsHarness) url() string { return "ws" + strings.TrimPrefix(h.srv.URL, "http") }

func (h *wsHarness) waitFrame(t *testing.T, typ string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		for i := len(h.frames) - 1; i >= 0; i-- {
			if h.frames[i]["type"] == typ {
				f := h.frames[i]
				h.mu.Unlock()
				return f
			}
		}
		h.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("не дочекались кадру %q; є: %v", typ, h.frames)
	return nil
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("не дочекались: %s", what)
}

type evLog struct {
	mu  sync.Mutex
	got map[string]Presence
}

func newPC(h *wsHarness, tok TokenFunc) (*PresenceClient, *evLog) {
	log := &evLog{got: map[string]Presence{}}
	pc := NewPresenceClient(PresenceConfig{
		URL:   h.url(),
		Token: tok,
		OnPresence: func(id string, p Presence, known bool) {
			log.mu.Lock()
			defer log.mu.Unlock()
			if known {
				log.got[id] = p
			} else {
				delete(log.got, id)
			}
		},
		allowInsecure: true,
	})
	return pc, log
}

func (l *evLog) get(id string) (Presence, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	p, ok := l.got[id]
	return p, ok
}

func TestPresenceRejectsInsecureURL(t *testing.T) {
	pc := NewPresenceClient(PresenceConfig{URL: "ws://example.com/ws", Token: func() (string, error) { return "t", nil }})
	if err := pc.Start(); err == nil {
		t.Fatal("ws:// без allowInsecure мав бути відхилений")
	}
	pc = NewPresenceClient(PresenceConfig{URL: "http://example.com", Token: func() (string, error) { return "t", nil }})
	if err := pc.Start(); err == nil {
		t.Fatal("http:// мав бути відхилений")
	}
	pc = NewPresenceClient(PresenceConfig{URL: "", Token: func() (string, error) { return "t", nil }})
	if err := pc.Start(); err == nil {
		t.Fatal("порожній URL мав бути відхилений")
	}
	pc = NewPresenceClient(PresenceConfig{URL: "wss://social.example.org/ws", Token: func() (string, error) { return "t", nil }})
	if err := pc.checkURL(); err != nil {
		t.Fatalf("wss:// має проходити: %v", err)
	}
}

func TestPresenceHelloFirstFrameAndStatus(t *testing.T) {
	h := newWSHarness(t)
	pc, _ := newPC(h, func() (string, error) { return "JWT-123", nil })
	pc.SetStatus(StatusInGame, "Шаурма Сурвайвл", "#FF8A00")
	if err := pc.Start(); err != nil {
		t.Fatal(err)
	}
	defer pc.Stop()

	waitFor(t, "hello", func() bool { h.mu.Lock(); defer h.mu.Unlock(); return len(h.hellos) == 1 })
	if h.hellos[0] != "JWT-123" {
		t.Errorf("jwt у hello: %q", h.hellos[0])
	}
	if strings.Contains(h.srv.URL, "JWT-123") {
		t.Error("jwt не має бути в URL")
	}
	f := h.waitFrame(t, "set_status")
	if f["status"] != "in_game" || f["packName"] != "Шаурма Сурвайвл" || f["packColor"] != "#ff8a00" {
		t.Errorf("set_status: %v", f)
	}
	if f["dropInOpen"] != false || f["serverAddr"] != "" {
		t.Errorf("адреса сервера/drop-in в цій фазі не відправляються: %v", f)
	}
	waitFor(t, "state online", func() bool { return pc.State() == ConnOnline })
}

func TestPresencePrivacyMinimization(t *testing.T) {
	h := newWSHarness(t)
	pc, _ := newPC(h, func() (string, error) { return "t", nil })
	pc.SetPolicy(Privacy{ShowStatusTo: "friends", ShowServerTo: "nobody", FriendRequest: "everyone"})
	pc.SetStatus(StatusInGame, "SecretPack", "#ff0000")
	pc.Start()
	defer pc.Stop()
	f := h.waitFrame(t, "set_status")
	if f["packName"] != "" || f["packColor"] != "" || f["status"] != "online" {
		t.Errorf("при showServerTo=nobody збірка не має витікати: %v", f)
	}

	pc.SetPolicy(Privacy{ShowStatusTo: "nobody", ShowServerTo: "nobody", FriendRequest: "everyone"})
	h.waitFrame(t, "left_game")
}

// Зміна приватності в UI має дійти до хабу кадром set_privacy ЗРАЗУ (HTTP
// лишає її в БД, а хаб тримає знімок у пам'яті й раніше оновлював його лише
// при реконекті — тому «Приховати статус» здавалося, що не працює).
func TestPresenceSetPrivacyFrameOnPolicyChange(t *testing.T) {
	h := newWSHarness(t)
	pc, _ := newPC(h, func() (string, error) { return "t", nil })
	// Початкова політика = за замовчуванням → кадр НЕ шлемо (нічого не змінилось).
	pc.SetPolicy(DefaultPrivacy())
	if err := pc.Start(); err != nil {
		t.Fatal(err)
	}
	defer pc.Stop()

	count := func() int {
		h.mu.Lock()
		defer h.mu.Unlock()
		n := 0
		for _, f := range h.frames {
			if f["type"] == "set_privacy" {
				n++
			}
		}
		return n
	}
	waitFor(t, "старт без кадру приватності", func() bool { return pc.State() == ConnOnline })
	if n := count(); n != 0 {
		t.Fatalf("без зміни політики кадру set_privacy бути не може, отримано %d", n)
	}

	pc.SetPolicy(Privacy{ShowStatusTo: "nobody", ShowServerTo: "friends", FriendRequest: "nobody"})
	f := h.waitFrame(t, "set_privacy")
	if f["showStatusTo"] != "nobody" || f["showServerTo"] != "friends" || f["friendRequests"] != "nobody" {
		t.Errorf("set_privacy: %v", f)
	}

	// Та сама політика ще раз → без дубля.
	pc.SetPolicy(Privacy{ShowStatusTo: "nobody", ShowServerTo: "friends", FriendRequest: "nobody"})
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if n := count(); n != 1 {
		t.Errorf("повторна та сама політика мала б дати рівно 1 кадр, отримано %d", n)
	}
}

func TestPresenceAcceptsOnlyFriendsAndSanitizes(t *testing.T) {
	h := newWSHarness(t)
	send := func(ctx context.Context, c *websocket.Conn, m string) { c.Write(ctx, websocket.MessageText, []byte(m)) }
	h.onAccept = func(ctx context.Context, c *websocket.Conn) {
		send(ctx, c, `{"type":"friend_presence","accountId":"`+idStr+`","status":"in_game","packName":"Stranger"}`) // не друг
		send(ctx, c, `{"type":"friend_presence","accountId":"../x","status":"online"}`)                             // сміття
		send(ctx, c, `{"type":"friend_presence","accountId":"`+idFriend+`","status":"admin"}`)                      // чужий статус
		send(ctx, c, `not json at all`)                                                                             // не JSON
		send(ctx, c, `{"type":"new_message","chatId":"x","ciphertext":"AAAA"}`)                                     // чат — ігноруємо
		send(ctx, c, `{"type":"friend_presence","accountId":"`+idFriend+`","status":"in_game","packName":"Pa\u202Eck\u200B","packColor":"url(x)"}`)
	}
	pc, log := newPC(h, func() (string, error) { return "t", nil })
	pc.SetFriends([]string{idFriend})
	pc.Start()
	defer pc.Stop()

	waitFor(t, "presence друга", func() bool { _, ok := log.get(idFriend); return ok })
	p, _ := log.get(idFriend)
	if p.Status != StatusInGame || p.PackName != "Pack" || p.PackColor != "" {
		t.Errorf("не санітизовано: %+v", p)
	}
	if _, ok := log.get(idStr); ok {
		t.Error("presence чужого акаунта мав бути відкинутий")
	}
	snap := pc.Snapshot()
	if len(snap) != 1 {
		t.Errorf("snapshot: %+v", snap)
	}
}

func TestPresenceOfflineAndFriendRemoval(t *testing.T) {
	h := newWSHarness(t)
	h.onAccept = func(ctx context.Context, c *websocket.Conn) {
		c.Write(ctx, websocket.MessageText, []byte(`{"type":"friend_presence","accountId":"`+idFriend+`","status":"online"}`))
	}
	pc, log := newPC(h, func() (string, error) { return "t", nil })
	pc.SetFriends([]string{idFriend})
	pc.Start()
	defer pc.Stop()
	waitFor(t, "online", func() bool { _, ok := log.get(idFriend); return ok })

	pc.SetFriends(nil) // друга видалили
	waitFor(t, "зник після видалення", func() bool { _, ok := log.get(idFriend); return !ok })
	if len(pc.Snapshot()) != 0 {
		t.Error("snapshot має бути порожнім")
	}
}

func TestPresenceOversizedFrameDropsConnection(t *testing.T) {
	h := newWSHarness(t)
	h.onAccept = func(ctx context.Context, c *websocket.Conn) {
		c.Write(ctx, websocket.MessageText, []byte(`{"type":"friend_presence","pad":"`+strings.Repeat("x", maxWSFrame*2)+`"}`))
	}
	pc, _ := newPC(h, func() (string, error) { return "t", nil })
	pc.Start()
	defer pc.Stop()
	// Після перевищення ліміту клієнт має розірвати з'єднання й перепідключитись
	// (а не прочитати 32 КіБ у пам'ять): другий hello — доказ реконекту.
	waitFor(t, "реконект після завеликого кадру", func() bool { h.mu.Lock(); defer h.mu.Unlock(); return len(h.hellos) >= 2 })
}

func TestPresenceReconnectResendsStatusWithFreshToken(t *testing.T) {
	h := newWSHarness(t)
	var n int
	var nm sync.Mutex
	tok := func() (string, error) {
		nm.Lock()
		defer nm.Unlock()
		n++
		return "tok-" + string(rune('0'+n)), nil
	}
	pc, _ := newPC(h, tok)
	pc.SetStatus(StatusInGame, "Pack", "#00ff00")
	pc.Start()
	defer pc.Stop()
	waitFor(t, "перше підключення", func() bool { h.mu.Lock(); defer h.mu.Unlock(); return len(h.hellos) == 1 })

	h.mu.Lock()
	h.frames = nil
	c := h.conns[0]
	h.mu.Unlock()
	c.Close(websocket.StatusGoingAway, "restart") // VPS перезапустився

	waitFor(t, "реконект", func() bool { h.mu.Lock(); defer h.mu.Unlock(); return len(h.hellos) >= 2 })
	if h.hellos[0] == h.hellos[1] {
		t.Errorf("на кожне підключення має братися СВІЖИЙ токен: %v", h.hellos)
	}
	f := h.waitFrame(t, "set_status")
	if f["packName"] != "Pack" {
		t.Errorf("після реконекту статус не відновлено: %v", f)
	}
}

func TestPresenceStopsWithoutSessionAndCanRestart(t *testing.T) {
	h := newWSHarness(t)
	var mu sync.Mutex
	loggedIn := false
	pc, _ := newPC(h, func() (string, error) {
		mu.Lock()
		defer mu.Unlock()
		if !loggedIn {
			return "", ErrNotLoggedIn
		}
		return "ok", nil
	})
	pc.Start()
	// Цикл має сам завершитись, а не крутитись у реконектах без сесії.
	finished := func() bool {
		pc.mu.Lock()
		defer pc.mu.Unlock()
		select {
		case <-pc.done:
			return true
		default:
			return false
		}
	}
	waitFor(t, "цикл завершився без сесії", finished)
	if pc.State() != ConnOff {
		t.Errorf("стан без сесії: %s", pc.State())
	}
	h.mu.Lock()
	if len(h.hellos) != 0 {
		t.Error("без токена підключатись не можна")
	}
	h.mu.Unlock()

	mu.Lock()
	loggedIn = true
	mu.Unlock()
	if err := pc.Start(); err != nil { // після входу Start знову працює
		t.Fatal(err)
	}
	defer pc.Stop()
	waitFor(t, "підключення після входу", func() bool { h.mu.Lock(); defer h.mu.Unlock(); return len(h.hellos) == 1 })
}

func TestPresenceAuthErrorForcesReconnect(t *testing.T) {
	h := newWSHarness(t)
	var first sync.Once
	h.onAccept = func(ctx context.Context, c *websocket.Conn) {
		first.Do(func() { c.Write(ctx, websocket.MessageText, []byte(`{"type":"error","code":"token_expired"}`)) })
	}
	pc, _ := newPC(h, func() (string, error) { return "t", nil })
	pc.Start()
	defer pc.Stop()
	waitFor(t, "повторне підключення з новим токеном", func() bool { h.mu.Lock(); defer h.mu.Unlock(); return len(h.hellos) >= 2 })
}

func TestStopIsIdempotentAndClearsState(t *testing.T) {
	h := newWSHarness(t)
	h.onAccept = func(ctx context.Context, c *websocket.Conn) {
		c.Write(ctx, websocket.MessageText, []byte(`{"type":"friend_presence","accountId":"`+idFriend+`","status":"online"}`))
	}
	pc, log := newPC(h, func() (string, error) { return "t", nil })
	pc.SetFriends([]string{idFriend})
	pc.Start()
	waitFor(t, "online", func() bool { _, ok := log.get(idFriend); return ok })
	pc.Stop()
	pc.Stop() // другий виклик — не панікує і не блокується
	if pc.State() != ConnOff || len(pc.Snapshot()) != 0 {
		t.Errorf("після Stop: state=%s snap=%v", pc.State(), pc.Snapshot())
	}
}

// VPS шле знімок presence одразу після hello — ДО того, як список друзів
// завантажився. Подія має дочекатись SetFriends, а чужа — так і лишитись
// проігнорованою.
func TestPresenceBeforeFriendsLoadedIsAppliedLater(t *testing.T) {
	h := newWSHarness(t)
	h.onAccept = func(ctx context.Context, c *websocket.Conn) {
		c.Write(ctx, websocket.MessageText, []byte(`{"type":"friend_presence","accountId":"`+idFriend+`","status":"in_game","packName":"P"}`))
		c.Write(ctx, websocket.MessageText, []byte(`{"type":"friend_presence","accountId":"`+idStr+`","status":"online"}`))
	}
	pc, log := newPC(h, func() (string, error) { return "t", nil })
	pc.Start() // друзів ще немає
	defer pc.Stop()
	waitFor(t, "події прийшли", func() bool {
		pc.mu.Lock()
		defer pc.mu.Unlock()
		return len(pc.pending) == 2
	})
	if _, ok := log.get(idFriend); ok {
		t.Fatal("до SetFriends нічого не показуємо")
	}
	pc.SetFriends([]string{idFriend}) // дружбу підтверджено
	waitFor(t, "застосовано після SetFriends", func() bool { p, ok := log.get(idFriend); return ok && p.PackName == "P" })
	if _, ok := log.get(idStr); ok {
		t.Error("чужий акаунт не мав потрапити в стан")
	}
	pc.mu.Lock()
	if _, left := pc.pending[idFriend]; left {
		t.Error("застосований запис має вийти з pending")
	}
	pc.mu.Unlock()
}

func TestPendingExpiresAndIsBounded(t *testing.T) {
	pc := NewPresenceClient(PresenceConfig{URL: "wss://x.example/ws", Token: func() (string, error) { return "", nil }})
	pc.pending[idFriend] = Presence{Status: StatusOnline, UpdatedAt: time.Now().Add(-2 * pendingTTL).UnixMilli()}
	pc.SetFriends([]string{idFriend})
	if len(pc.Snapshot()) != 0 {
		t.Error("прострочений pending не має застосовуватись")
	}
	for i := 0; i < MaxFriends+500; i++ {
		id := fmt.Sprintf("%08x-aaaa-bbbb", i+1)
		pc.handlePresence(inEvent{Type: "friend_presence", AccountID: id, Status: StatusOnline})
	}
	if len(pc.pending) != MaxFriends {
		t.Errorf("pending має впертись рівно в ліміт %d, а є %d", MaxFriends, len(pc.pending))
	}
}
