package social

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// TestChatEventJSONKeys фіксує WIRE-формат подій емісії (App.emit →
// json.Marshal). Фронтенд (SocialChatAck/... у types.ts) читає camelCase:
// втрата json-тегів на цих структурах ламає ack — повідомлення доходить,
// але UI через 15 с показує «не надіслано». Тест ловить саме це.
func TestChatEventJSONKeys(t *testing.T) {
	cases := []struct {
		name string
		val  any
		keys []string
	}{
		{"ack", MessageAck{ClientMessageID: "m", ChatID: "a_b", ID: 9, TS: 6},
			[]string{"clientMessageId", "chatId", "id", "ts"}},
		{"typing", TypingEvent{ChatID: "a_b", AccountID: "aaaa1111"},
			[]string{"chatId", "accountId"}},
		{"read", ReadEvent{ChatID: "a_b", UpToMessageID: 5},
			[]string{"chatId", "upToMessageId"}},
		{"invite", ServerInviteEvent{ChatID: "a_b", From: "aaaa1111", ServerAddr: "h:1", PackID: "p", PackName: "n", InviteID: "i"},
			[]string{"chatId", "from", "serverAddr", "packId", "packName", "inviteId"}},
	}
	for _, tc := range cases {
		b, err := json.Marshal(tc.val)
		if err != nil {
			t.Fatalf("%s: marshal: %v", tc.name, err)
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatalf("%s: unmarshal: %v", tc.name, err)
		}
		for _, k := range tc.keys {
			if _, ok := m[k]; !ok {
				t.Errorf("%s: немає ключа %q у %s (фронтенд його не побачить)", tc.name, k, string(b))
			}
		}
	}
}

// TestChatOverSameSocket перевіряє, що чат іде ТИМ САМИМ WS-з'єднанням, що й
// presence: вихідні кадри ставляться в чергу, вхідні доставляються в колбеки,
// а події від не-друзів відкидаються.
func TestChatOverSameSocket(t *testing.T) {
	h := newWSHarness(t)
	friend, me := "aaaa1111", "bbbb2222"
	chatID := friend + "_" + me

	var (
		gotMsg    chan IncomingMessage
		gotAck    chan MessageAck
		gotTyping chan TypingEvent
		gotInvite chan ServerInviteEvent
	)
	gotMsg = make(chan IncomingMessage, 4)
	gotAck = make(chan MessageAck, 4)
	gotTyping = make(chan TypingEvent, 4)
	gotInvite = make(chan ServerInviteEvent, 4)

	pc := NewPresenceClient(PresenceConfig{
		URL:   h.url(),
		Token: func() (string, error) { return "jwt", nil },
		OnMessage: func(m IncomingMessage) {
			gotMsg <- m
		},
		OnMessageAck: func(a MessageAck) { gotAck <- a },
		OnTyping:     func(e TypingEvent) { gotTyping <- e },
		OnServerInvite: func(e ServerInviteEvent) {
			gotInvite <- e
		},
		allowInsecure: true,
	})
	if err := pc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer pc.Stop()

	// Чекаємо hello, щоб сокет точно був піднятий.
	waitFor(t, "hello", func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		return len(h.hellos) == 1
	})
	pc.SetFriends([]string{friend})

	// Вихідний кадр: send_message.
	if err := pc.SendChatMessage(chatID, "Y2lwaGVy", "bm9uY2U", "msg-1"); err != nil {
		t.Fatalf("SendChatMessage: %v", err)
	}
	f := h.waitFrame(t, "send_message")
	if f["chatId"] != chatID || f["ciphertext"] != "Y2lwaGVy" || f["nonce"] != "bm9uY2U" {
		t.Fatalf("кадр send_message некоректний: %v", f)
	}

	// Вхідні кадри від ДРУГА доставляються.
	send := func(m string) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		h.mu.Lock()
		c := h.conns[0]
		h.mu.Unlock()
		_ = c.Write(ctx, websocket.MessageText, []byte(m))
	}
	send(`{"type":"new_message","chatId":"` + chatID + `","from":"` + friend + `","bodyType":"text","ciphertext":"c","nonce":"nn","ts":5,"id":7}`)
	select {
	case m := <-gotMsg:
		if m.ChatID != chatID || m.From != friend || m.ID != 7 || m.Ciphertext != "c" {
			t.Fatalf("OnMessage отримав %+v", m)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("new_message не доставлено")
	}

	send(`{"type":"message_ack","chatId":"` + chatID + `","clientMessageId":"msg-1","id":9,"ts":6}`)
	select {
	case a := <-gotAck:
		if a.ClientMessageID != "msg-1" || a.ID != 9 {
			t.Fatalf("ack %+v", a)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("message_ack не доставлено")
	}

	send(`{"type":"friend_typing","chatId":"` + chatID + `","accountId":"` + friend + `"}`)
	select {
	case e := <-gotTyping:
		if e.AccountID != friend {
			t.Fatalf("typing %+v", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("friend_typing не доставлено")
	}

	send(`{"type":"server_invite_incoming","chatId":"` + chatID + `","from":"` + friend + `","serverAddr":"h.example:25565","packId":"p1","packName":"Збірка","inviteId":"inv1"}`)
	select {
	case e := <-gotInvite:
		if e.ServerAddr != "h.example:25565" || e.PackName != "Збірка" || e.InviteID != "inv1" {
			t.Fatalf("invite %+v", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server_invite_incoming не доставлено")
	}

	// Вхідні кадри від НЕ-друга ігноруються.
	send(`{"type":"new_message","chatId":"` + chatID + `","from":"cccc3333","bodyType":"text","ciphertext":"x","nonce":"nn","ts":9,"id":10}`)
	select {
	case m := <-gotMsg:
		t.Fatalf("повідомлення від не-друга прийнято: %+v", m)
	case <-time.After(400 * time.Millisecond):
	}
}

func TestSendChatValidation(t *testing.T) {
	pc := NewPresenceClient(PresenceConfig{})
	if err := pc.SendChatMessage("сміття", "c", "n", ""); err != ErrBadInput {
		t.Fatalf("невалідний chatId: %v", err)
	}
	if err := pc.SendChatMessage("aaaa1111_bbbb2222", "", "n", ""); err != ErrBadInput {
		t.Fatalf("порожній ciphertext: %v", err)
	}
	if err := pc.SendServerInvite("aaaa1111_bbbb2222", "bad addr!!", "", "", 0); err != ErrBadInput {
		t.Fatalf("невалідна адреса: %v", err)
	}
	if err := pc.SendMarkRead("aaaa1111_bbbb2222", 0); err != ErrBadInput {
		t.Fatalf("нульовий upTo: %v", err)
	}
	if _, ok := sanitizeServerAddr("host.example:25565"); !ok {
		t.Fatal("валідна адреса відкинута")
	}
	if _, ok := sanitizeServerAddr("evil host"); ok {
		t.Fatal("адреса з пробілом прийнята")
	}
}

// Кілька пристроїв: кадри device_revoked / device_added / devices_changed / chat_read_elsewhere і
// ехо власного повідомлення з іншого мого пристрою.
func TestPresenceMultiDeviceFrames(t *testing.T) {
	h := newWSHarness(t)
	me, friend := "bbbb2222", "aaaa1111"
	chatID := friend + "_" + me
	// JWT без підпису перевірки: AccountIDFromJWT читає лише sub.
	jwt := fakeJWT(me)

	msgs := make(chan IncomingMessage, 4)
	added := make(chan DeviceAddedEvent, 2)
	changed := make(chan struct{}, 2)
	elsewhere := make(chan ReadEvent, 2)
	revoked := make(chan string, 2)
	pc := NewPresenceClient(PresenceConfig{
		URL: h.url(), Token: func() (string, error) { return jwt, nil },
		OnMessage:        func(m IncomingMessage) { msgs <- m },
		OnDeviceAdded:    func(e DeviceAddedEvent) { added <- e },
		OnDevicesChanged: func() { changed <- struct{}{} },
		OnReadElsewhere:  func(e ReadEvent) { elsewhere <- e },
		OnDeviceRevoked:  func(r string) { revoked <- r },
		allowInsecure:    true,
	})
	if err := pc.Start(); err != nil {
		t.Fatal(err)
	}
	defer pc.Stop()
	waitFor(t, "hello", func() bool { h.mu.Lock(); defer h.mu.Unlock(); return len(h.hellos) == 1 })
	pc.SetFriends([]string{friend})

	// Ехо МОГО повідомлення (from = я) у діалозі з другом — приймаємо.
	h.push(t, `{"type":"new_message","chatId":"`+chatID+`","from":"`+me+`","bodyType":"text","ciphertext":"c","nonce":"nn","ts":5,"id":8}`)
	select {
	case m := <-msgs:
		if m.From != me || m.ID != 8 {
			t.Fatalf("%+v", m)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ехо власного повідомлення не доставлено")
	}
	// …але «ехо» від імені стороннього акаунта чи в чужому діалозі — ні.
	h.push(t, `{"type":"new_message","chatId":"cccc3333_dddd4444","from":"`+me+`","bodyType":"text","ciphertext":"c","nonce":"nn","ts":5,"id":9}`)
	select {
	case m := <-msgs:
		t.Fatalf("повідомлення в чужому діалозі прийнято: %+v", m)
	case <-time.After(300 * time.Millisecond):
	}

	h.push(t, `{"type":"device_added","deviceId":"dv-1","name":"Ноутбук\u202e","clientName":"Shaurma Launcher","os":"Linux","country":"PL","at":99}`)
	select {
	case e := <-added:
		if e.Name != "Ноутбук" || e.Country != "PL" || e.DeviceID != "dv-1" {
			t.Fatalf("%+v", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("device_added не доставлено")
	}
	h.push(t, `{"type":"devices_changed","reason":"revoked"}`)
	select {
	case <-changed:
	case <-time.After(2 * time.Second):
		t.Fatal("devices_changed не доставлено")
	}
	h.push(t, `{"type":"chat_read_elsewhere","chatId":"`+chatID+`","upToMessageId":42}`)
	select {
	case e := <-elsewhere:
		if e.UpToMessageID != 42 {
			t.Fatalf("%+v", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("chat_read_elsewhere не доставлено")
	}

	// device_revoked: колбек + зупинка перепідключення (стан Off, без нових hello).
	h.push(t, `{"type":"device_revoked","deviceId":"dv-me","reason":"user"}`)
	select {
	case r := <-revoked:
		if r != "user" {
			t.Fatalf("%q", r)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("device_revoked не доставлено")
	}
	waitFor(t, "зупинка після device_revoked", func() bool { return pc.State() == ConnOff })
	time.Sleep(600 * time.Millisecond)
	h.mu.Lock()
	n := len(h.hellos)
	h.mu.Unlock()
	if n != 1 {
		t.Fatalf("після device_revoked не можна перепідключатись: %d hello", n)
	}
}

// fakeJWT — токен із потрібним sub (підпис не перевіряється клієнтом: його перевіряє сервер).
func fakeJWT(sub string) string {
	b := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	return b(`{"alg":"HS256"}`) + "." + b(`{"sub":"`+sub+`"}`) + "." + b("sig")
}

// p2p_host_revoked: власник забрав у співхоста роль хоста — колбек отримує адресу й причину,
// кадр без адреси ігнорується.
func TestPresenceP2PHostRevokedFrame(t *testing.T) {
	h := newWSHarness(t)
	me := "bbbb2222"
	jwt := fakeJWT(me)
	got := make(chan P2PHostRevokedEvent, 2)
	pc := NewPresenceClient(PresenceConfig{
		URL: h.url(), Token: func() (string, error) { return jwt, nil },
		OnP2PHostRevoked: func(e P2PHostRevokedEvent) { got <- e },
		allowInsecure:    true,
	})
	if err := pc.Start(); err != nil {
		t.Fatal(err)
	}
	defer pc.Stop()
	waitFor(t, "hello", func() bool { h.mu.Lock(); defer h.mu.Unlock(); return len(h.hellos) == 1 })

	h.push(t, `{"type":"p2p_host_revoked","reason":"kicked"}`) // без адреси
	select {
	case e := <-got:
		t.Fatalf("кадр без адреси не має доходити: %+v", e)
	case <-time.After(300 * time.Millisecond):
	}
	h.push(t, `{"type":"p2p_host_revoked","address":"prlool-482917.play.shaurma-launcher.org","reason":"cohost_removed"}`)
	select {
	case e := <-got:
		if e.Address != "prlool-482917.play.shaurma-launcher.org" || e.Reason != "cohost_removed" {
			t.Fatalf("%+v", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("p2p_host_revoked не доставлено")
	}
}
