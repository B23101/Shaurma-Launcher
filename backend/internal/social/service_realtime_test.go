package social

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/coder/websocket"
)

const idMe2 = idMe

func snapFrame(extra string) string {
	return `{"type":"friends_snapshot","friends":[` +
		`{"accountId":"` + idFriend + `","nickname":"Bob","since":5,"status":"in_game","packName":"Pack","packColor":"#ff8800"},` +
		`{"accountId":"` + idStr + `","nickname":"Carol","status":"away"},` +
		`{"accountId":"44444444-aaaa-bbbb-cccc-000000000004","nickname":"Dan","status":"offline"}],` +
		`"invites":[{"id":"aaaaaaaa-1111","fromAccountId":"55555555-aaaa-bbbb-cccc-000000000005","fromNickname":"Eve","createdAt":9}],` +
		`"unread":[{"chatId":"` + idMe2 + `_` + idFriend + `","count":3,"lastId":10,"lastTs":20}],` +
		`"privacy":{"showStatusTo":"nobody","showServerTo":"friends","friendRequests":"everyone"}` + extra + `}`
}

// Головний регрес: друзі, що були онлайн ДО нашого підключення, мають показуватись
// одразу після hello — раніше кадр friends_snapshot ігнорувався.
func TestSnapshotShowsAlreadyOnlineFriendsInstantly(t *testing.T) {
	e := newSvcEnv(t)
	e.svc.Start()
	waitFor(t, "me", func() bool { return e.svc.Snapshot().Me != "" })
	e.ws.push(t, snapFrame(""))
	waitFor(t, "знімок застосовано", func() bool { return len(e.svc.Snapshot().Friends) == 3 })
	s := e.svc.Snapshot()
	got := map[string]Presence{}
	for _, f := range s.Friends {
		got[f.Nickname] = f.Presence
	}
	if got["Bob"].Status != StatusInGame || got["Bob"].PackName != "Pack" || got["Bob"].PackColor != "#ff8800" {
		t.Errorf("Bob: %+v", got["Bob"])
	}
	if got["Carol"].Status != StatusAway || got["Dan"].Status != StatusOffline {
		t.Errorf("away/offline: %+v %+v", got["Carol"], got["Dan"])
	}
	if len(s.Invites) != 1 || s.Invites[0].FromNickname != "Eve" {
		t.Errorf("запити зі знімка: %+v", s.Invites)
	}
	if !s.PrivacyLoaded || s.Privacy.ShowStatusTo != "nobody" {
		t.Errorf("приватність зі знімка: %+v loaded=%v", s.Privacy, s.PrivacyLoaded)
	}
	if s.Unread[idFriend] != 3 {
		t.Errorf("непрочитане зі знімка: %+v", s.Unread)
	}
	// Подальша зміна статусу ідея живими подіями: друг із білого списку приймається.
	e.ws.push(t, `{"type":"friend_presence","accountId":"`+idFriend+`","status":"online"}`)
	waitFor(t, "live-подія", func() bool {
		for _, f := range e.svc.Snapshot().Friends {
			if f.AccountID == idFriend {
				return f.Presence.Status == StatusOnline
			}
		}
		return false
	})
}

func TestSnapshotFromUntrustedServerIsSanitized(t *testing.T) {
	e := newSvcEnv(t)
	e.svc.Start()
	e.ws.push(t, `{"type":"friends_snapshot","friends":[`+
		`{"accountId":"../etc/passwd","nickname":"x","status":"online"},`+
		`{"accountId":"`+idFriend+`","nickname":"Al\u202Eice\u0000","status":"hacker"},`+
		`{"accountId":"`+idFriend+`","nickname":"dup","status":"online"}],`+
		`"invites":[{"id":"bad id!","fromAccountId":"`+idStr+`","fromNickname":"x","createdAt":1}],`+
		`"unread":[{"chatId":"garbage","count":5},{"chatId":"`+idMe2+`_`+idFriend+`","count":-2}],`+
		`"privacy":{"showStatusTo":"WRONG"}}`)
	waitFor(t, "знімок", func() bool { return len(e.svc.Snapshot().Friends) == 1 })
	s := e.svc.Snapshot()
	if s.Friends[0].Nickname != "Alice" || s.Friends[0].Presence.Status != StatusOffline {
		t.Errorf("нік має бути очищений, невідомий статус → offline: %+v", s.Friends[0])
	}
	if len(s.Invites) != 0 || len(s.Unread) != 0 {
		t.Errorf("сміття не проходить: %+v %+v", s.Invites, s.Unread)
	}
	if s.Privacy.ShowStatusTo != "friends" {
		t.Errorf("невалідна приватність → дефолт: %+v", s.Privacy)
	}
}

func TestUnreadCountsIncomingAndMarkReadResets(t *testing.T) {
	e := newSvcEnv(t)
	e.svc.Start()
	waitFor(t, "me", func() bool { return e.svc.Snapshot().Me != "" })
	e.ws.push(t, snapFrame(""))
	waitFor(t, "друзі", func() bool { return len(e.svc.Snapshot().Friends) == 3 })
	// Подія без діалогу в UI: лічильник росте в сервісі, а не в компоненті.
	e.svc.onIncomingMessage(IncomingMessage{ChatID: idMe2 + "_" + idFriend, From: idFriend, BodyType: "text", Ciphertext: "c", Nonce: "n", TS: 1, ID: 11})
	if got := e.svc.Snapshot().Unread[idFriend]; got != 4 {
		t.Fatalf("3 зі знімка + 1 нове = 4, є %d", got)
	}
	n := 0
	for _, ev := range e.events() {
		if ev == "friends:unread" {
			n++
		}
	}
	if n < 2 {
		t.Errorf("кожна зміна лічильника має емітити friends:unread, подій %d", n)
	}
	if err := e.svc.MarkRead(idFriend, 11); err != nil {
		t.Fatal(err)
	}
	if _, has := e.svc.Snapshot().Unread[idFriend]; has {
		t.Error("після MarkRead лічильник має зникнути")
	}
	f := e.ws.waitFrame(t, "mark_read")
	if f["chatId"] != idMe2+"_"+idFriend {
		t.Errorf("mark_read: %v", f)
	}
}

func TestHistoryGoesOverWebSocket(t *testing.T) {
	e := newSvcEnv(t)
	e.ws.onAccept = func(ctx context.Context, c *websocket.Conn) {}
	e.svc.Start()
	waitFor(t, "WS online", func() bool { return e.svc.Snapshot().Conn == ConnOnline && e.svc.Snapshot().Me != "" })
	chat := idMe2 + "_" + idFriend
	go func() {
		f := e.ws.waitFrame(t, "history")
		b, _ := json.Marshal(map[string]any{"type": "history_page", "reqId": f["reqId"], "chatId": chat, "hasMore": true,
			"messages": []map[string]any{
				{"id": 7, "from": idFriend, "bodyType": "text", "ciphertext": "Y2lwaGVy", "nonce": "bm9uY2U", "ts": 1234},
				{"id": 8, "from": "garbage", "bodyType": "text", "ciphertext": "x", "nonce": "y", "ts": 1235},
			}})
		e.ws.push(t, string(b))
	}()
	page, err := e.svc.pc.RequestHistory(chat, 0, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != 1 || page.Messages[0].ID != 7 || page.Messages[0].CreatedAt != 1234 || !page.HasMore {
		t.Fatalf("сторінка: %+v", page)
	}
}

func TestHistoryErrorFallsBackToWorkerHTTP(t *testing.T) {
	e := newSvcEnv(t)
	e.svc.Start()
	waitFor(t, "WS online", func() bool { return e.svc.Snapshot().Conn == ConnOnline && e.svc.Snapshot().Me != "" })
	go func() {
		f := e.ws.waitFrame(t, "history")
		e.ws.push(t, `{"type":"history_error","reqId":"`+f["reqId"].(string)+`","code":"upstream_error"}`)
	}()
	// Worker-мок на /friends/chat/ віддає 404 → помилка, але ГОЛОВНЕ: спроба HTTP відбулась.
	_, _ = e.svc.ChatHistory(idFriend, 0, 50)
	e.w.mu.Lock()
	defer e.w.mu.Unlock()
	hit := false
	for _, c := range e.w.calls {
		if len(c) > 17 && c[:17] == "GET /friends/chat" {
			hit = true
		}
	}
	if !hit {
		t.Error("при помилці VPS історія має піти запасним шляхом через Worker")
	}
}

func TestPeriodicRehelloKeepsSessionWithoutReconnect(t *testing.T) {
	old := reauthEvery
	reauthEvery = 80 * time.Millisecond
	defer func() { reauthEvery = old }()
	h := newWSHarness(t)
	pc := NewPresenceClient(PresenceConfig{URL: h.url(), Token: func() (string, error) { return "jwt-fresh", nil }, allowInsecure: true})
	if err := pc.Start(); err != nil {
		t.Fatal(err)
	}
	defer pc.Stop()
	waitFor(t, "hello-кадри", func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		n := 0
		for _, f := range h.frames {
			if f["type"] == "hello" {
				n++
			}
		}
		return n >= 2
	})
	h.mu.Lock()
	conns := len(h.conns)
	h.mu.Unlock()
	if conns != 1 {
		t.Fatalf("оновлення токена не має перепідключати: з'єднань %d", conns)
	}
}

func TestAwayAfterInactivityAndBackOnline(t *testing.T) {
	e := newSvcEnv(t)
	e.svc.d.AwayAfter = 60 * time.Millisecond
	e.svc.Start()
	waitFor(t, "WS online", func() bool { return e.svc.Snapshot().Conn == ConnOnline })
	lastStatus := func() string {
		e.ws.mu.Lock()
		defer e.ws.mu.Unlock()
		for i := len(e.ws.frames) - 1; i >= 0; i-- {
			if e.ws.frames[i]["type"] == "set_status" {
				return e.ws.frames[i]["status"].(string)
			}
		}
		return ""
	}
	e.svc.SetWindowActive(false)
	waitFor(t, "away", func() bool { return lastStatus() == "away" })
	// UI-віджет акаунта отримує власний статус через meStatus у знімку —
	// він має оновитись разом зі статусом на сервері, без очікування опитування.
	waitFor(t, "meStatus=away", func() bool { return e.svc.Snapshot().MeStatus == StatusAway })
	e.svc.SetWindowActive(true)
	waitFor(t, "online", func() bool { return lastStatus() == "online" })
	waitFor(t, "meStatus=online", func() bool { return e.svc.Snapshot().MeStatus == StatusOnline })

	// У грі статус «у грі» важливіший за «відійшов».
	e.svc.GameStarted("b1", "Pack", "#112233")
	e.svc.SetWindowActive(false)
	time.Sleep(200 * time.Millisecond)
	if s := lastStatus(); s != "in_game" {
		t.Errorf("у грі статус не має ставати away: %s", s)
	}
	if s := e.svc.Snapshot().MeStatus; s != StatusInGame {
		t.Errorf("meStatus у грі = %q, очікували %q", s, StatusInGame)
	}
}

// TestMeStatusOfflineWhenNotRunning — джерело правди для віджета акаунта:
// поки сервіс друзів не запущено (нема Shaurma-акаунта / нема звʼязку), мій
// статус має бути «не в мережі», а не хибне «в мережі».
func TestMeStatusOfflineWhenNotRunning(t *testing.T) {
	e := newSvcEnv(t)
	if s := e.svc.Snapshot().MeStatus; s != StatusOffline {
		t.Fatalf("meStatus до Start = %q, очікували %q", s, StatusOffline)
	}
	e.svc.Start()
	defer e.svc.Stop()
	waitFor(t, "meStatus=online", func() bool { return e.svc.Snapshot().MeStatus == StatusOnline })
	waitFor(t, "WS online", func() bool { return e.svc.Snapshot().Conn == ConnOnline })
	e.svc.Stop()
	if s := e.svc.Snapshot().MeStatus; s != StatusOffline {
		t.Errorf("meStatus після Stop = %q, очікували %q", s, StatusOffline)
	}
}

func TestPollingStopsWhileWebSocketIsLive(t *testing.T) {
	e := newSvcEnv(t)
	e.svc.d.PollEvery = 40 * time.Millisecond
	e.svc.Start()
	waitFor(t, "WS online", func() bool { return e.svc.Snapshot().Conn == ConnOnline })
	time.Sleep(100 * time.Millisecond)
	count := func() int {
		e.w.mu.Lock()
		defer e.w.mu.Unlock()
		n := 0
		for _, c := range e.w.calls {
			if c == "GET /friends/list" {
				n++
			}
		}
		return n
	}
	before := count()
	time.Sleep(300 * time.Millisecond) // ≈7 тактів опитування
	if after := count(); after != before {
		t.Fatalf("при живому WS Worker не опитується: %d → %d", before, after)
	}
}

// Видалення «для всіх»: UI викликає DeleteChatMessage, чекає op_result, а подія
// message_deleted від VPS іде у chat:deleted.
func TestDeleteChatMessageRoundTripAndDeletedEvent(t *testing.T) {
	e := newSvcEnv(t)
	e.svc.Start()
	waitFor(t, "WS online", func() bool { return e.svc.Snapshot().Conn == ConnOnline && e.svc.Snapshot().Me != "" })
	e.ws.push(t, snapFrame(""))
	waitFor(t, "друзі", func() bool { return len(e.svc.Snapshot().Friends) == 3 })
	chat := idMe2 + "_" + idFriend

	go func() {
		f := e.ws.waitFrame(t, "delete_message")
		if f["chatId"] != chat || f["messageId"].(float64) != 77 {
			t.Errorf("кадр delete_message: %v", f)
		}
		e.ws.push(t, `{"type":"op_result","reqId":"`+f["reqId"].(string)+`"}`)
	}()
	if err := e.svc.DeleteChatMessage(idFriend, 77); err != nil {
		t.Fatalf("видалення: %v", err)
	}

	e.ws.push(t, `{"type":"message_deleted","chatId":"`+chat+`","messageId":77,"by":"`+idMe2+`"}`)
	waitFor(t, "chat:deleted", func() bool {
		for _, ev := range e.events() {
			if ev == "chat:deleted" {
				return true
			}
		}
		return false
	})
}

// Відмова сервера (чужі повідомлення видаляти не можна) доходить до викликача кодом помилки.
func TestDeleteChatMessageServerRefusalSurfacesCode(t *testing.T) {
	e := newSvcEnv(t)
	e.svc.Start()
	waitFor(t, "WS online", func() bool { return e.svc.Snapshot().Conn == ConnOnline && e.svc.Snapshot().Me != "" })
	go func() {
		f := e.ws.waitFrame(t, "delete_message")
		e.ws.push(t, `{"type":"op_result","reqId":"`+f["reqId"].(string)+`","code":"forbidden"}`)
	}()
	err := e.svc.DeleteChatMessage(idFriend, 5)
	if err == nil || err.Error() != "social:forbidden" {
		t.Fatalf("очікував social:forbidden, отримав %v", err)
	}
}

func TestPinRoundTripAndPinsEventFromNonFriendIsIgnored(t *testing.T) {
	e := newSvcEnv(t)
	e.svc.Start()
	waitFor(t, "WS online", func() bool { return e.svc.Snapshot().Conn == ConnOnline && e.svc.Snapshot().Me != "" })
	e.ws.push(t, snapFrame(""))
	waitFor(t, "друзі", func() bool { return len(e.svc.Snapshot().Friends) == 3 })
	chat := idMe2 + "_" + idFriend

	go func() {
		f := e.ws.waitFrame(t, "pin_message")
		if f["pin"] != true || f["chatId"] != chat {
			t.Errorf("кадр pin_message: %v", f)
		}
		e.ws.push(t, `{"type":"op_result","reqId":"`+f["reqId"].(string)+`"}`)
	}()
	if err := e.svc.PinChatMessage(idFriend, 9, true); err != nil {
		t.Fatalf("закріплення: %v", err)
	}

	// Подія чужого діалогу (жоден учасник — не мій друг) відкидається.
	stranger := "88888888-aaaa-bbbb-cccc-000000000008_99999999-aaaa-bbbb-cccc-000000000009"
	e.ws.push(t, `{"type":"pins_updated","chatId":"`+stranger+`","pinned":[],"action":"pin","messageId":1}`)
	e.ws.push(t, `{"type":"pins_updated","chatId":"`+chat+`","pinned":[{"id":9,"from":"`+idFriend+`","bodyType":"text","ciphertext":"x","nonce":"y","ts":5}],"action":"pin","messageId":9,"by":"`+idMe2+`"}`)
	waitFor(t, "chat:pins", func() bool {
		n := 0
		for _, ev := range e.events() {
			if ev == "chat:pins" {
				n++
			}
		}
		return n >= 1
	})
	time.Sleep(100 * time.Millisecond)
	n := 0
	for _, ev := range e.events() {
		if ev == "chat:pins" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("подія стороннього діалогу не мала дійти: chat:pins ×%d", n)
	}
}

func TestHistoryAfterSendsAfterAndParsesPinned(t *testing.T) {
	e := newSvcEnv(t)
	e.svc.Start()
	waitFor(t, "WS online", func() bool { return e.svc.Snapshot().Conn == ConnOnline && e.svc.Snapshot().Me != "" })
	chat := idMe2 + "_" + idFriend
	go func() {
		f := e.ws.waitFrame(t, "history")
		if f["after"].(float64) != 500 {
			t.Errorf("after має дійти до VPS: %v", f)
		}
		e.ws.push(t, `{"type":"history_page","reqId":"`+f["reqId"].(string)+`","chatId":"`+chat+`","hasMore":false,`+
			`"messages":[{"id":3,"from":"`+idFriend+`","bodyType":"text","ciphertext":"x","nonce":"y","ts":600}],`+
			`"pinned":[{"id":1,"from":"`+idFriend+`","bodyType":"text","ciphertext":"x","nonce":"y","ts":100}]}`)
	}()
	page, err := e.svc.pc.RequestHistory(chat, 0, 500, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != 1 || page.Messages[0].ID != 3 || len(page.Pinned) != 1 || page.Pinned[0].ID != 1 {
		t.Fatalf("сторінка: %+v", page)
	}
	if _, err := e.svc.ChatHistoryAfter(idFriend, 0, 50); err != ErrBadInput {
		t.Fatalf("after<=0 некоректний: %v", err)
	}
}
