package social

import (
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestConversationIDCanonical(t *testing.T) {
	a, b := "aaaa1111", "bbbb2222"
	ab, err := ConversationID(a, b)
	if err != nil {
		t.Fatal(err)
	}
	ba, _ := ConversationID(b, a)
	if ab != ba || ab != a+"_"+b {
		t.Fatalf("неканонічний chatId: %q vs %q", ab, ba)
	}
	for _, c := range [][2]string{{a, a}, {"", b}, {"zzz", b}, {a, "x"}} {
		if _, err := ConversationID(c[0], c[1]); err == nil {
			t.Fatalf("прийнято некоректну пару %v", c)
		}
	}
}

func TestOtherPartyInChat(t *testing.T) {
	a, b := "aaaa1111", "bbbb2222"
	chat := a + "_" + b
	if got := OtherPartyInChat(chat, a); got != b {
		t.Fatalf("для A отримали %q", got)
	}
	if got := OtherPartyInChat(chat, b); got != a {
		t.Fatalf("для B отримали %q", got)
	}
	if got := OtherPartyInChat(chat, "cccc3333"); got != "" {
		t.Fatalf("сторонній отримав %q", got)
	}
	if got := OtherPartyInChat("сміття", a); got != "" {
		t.Fatalf("сміття отримало %q", got)
	}
}

func TestChatHistoryParsing(t *testing.T) {
	body := `{"hasMore":true,"messages":[
		{"id":2,"fromAccountId":"aaaa1111","bodyType":"text","ciphertext":"Y2lwaGVy","nonce":"bm9uY2U=","createdAt":20},
		{"id":1,"fromAccountId":"bbbb2222","bodyType":"server_invite","ciphertext":"","nonce":"","createdAt":10,
		 "invite":{"id":"inv-1","serverAddr":"host.example:25565","packId":"p1","packName":"Моя збірка","expiresAt":99,"revoked":false,"active":true}},
		{"id":3,"fromAccountId":"bad","bodyType":"text","ciphertext":"x","nonce":"y","createdAt":30}
	]}`
	c := NewClient(func(method, path string, b []byte) (*http.Response, error) {
		if method != http.MethodGet || !strings.HasPrefix(path, "/friends/chat/") {
			t.Fatalf("неочікуваний запит %s %s", method, path)
		}
		return stubResponse(200, body), nil
	})
	page, err := c.ChatHistory("aaaa1111_bbbb2222", 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	if !page.HasMore {
		t.Fatal("hasMore не розібрано")
	}
	// Сміттєвий fromAccountId ("bad") відкинуто.
	if len(page.Messages) != 2 {
		t.Fatalf("повідомлень %d, очікували 2", len(page.Messages))
	}
	inv := page.Messages[1]
	if inv.BodyType != "server_invite" || inv.Invite == nil || !inv.Invite.Active {
		t.Fatalf("invite не розібрано: %+v", inv)
	}
	if inv.Invite.ServerAddr != "host.example:25565" || inv.Invite.PackName != "Моя збірка" {
		t.Fatalf("поля invite: %+v", inv.Invite)
	}
}

// fakeKeyServer віддає публічний ключ одного з двох акаунтів і рахує запити.
func fakeKeyServer(self string, ownPub string, peers map[string]string, counter *int32) Requester {
	return func(method, path string, body []byte) (*http.Response, error) {
		if method == http.MethodGet && strings.HasPrefix(path, "/friends/key/") {
			atomic.AddInt32(counter, 1)
			id := strings.TrimPrefix(path, "/friends/key/")
			if pk, ok := peers[id]; ok {
				return stubResponse(200, `{"accountId":"`+id+`","publicKey":"`+pk+`"}`), nil
			}
			return stubResponse(404, `{"error":"key_not_found"}`), nil
		}
		return stubResponse(200, `{"ok":true}`), nil
	}
}

func TestChatCryptoEndToEnd(t *testing.T) {
	// Дві незалежні пари ключів (два гравці).
	ksA := NewKeyStore(filepath.Join(t.TempDir(), "a"))
	ksB := NewKeyStore(filepath.Join(t.TempDir(), "b"))
	cryptA := NewChatCrypto(ksA, NewClient(nil))
	cryptB := NewChatCrypto(ksB, NewClient(nil))
	if err := cryptA.Init(); err != nil {
		t.Fatal(err)
	}
	if err := cryptB.Init(); err != nil {
		t.Fatal(err)
	}
	aID, bID := "aaaa1111", "bbbb2222"
	keys := map[string]string{aID: cryptA.PublicKeyB64(), bID: cryptB.PublicKeyB64()}

	var calls int32
	cryptA.client = NewClient(fakeKeyServer(aID, keys[aID], keys, &calls))
	cryptB.client = NewClient(fakeKeyServer(bID, keys[bID], keys, &calls))

	// A → B.
	text := "привіт з E2E 🔐"
	ct, nonce, err := cryptA.encryptE2E(bID, text)
	if err != nil {
		t.Fatalf("EncryptFor: %v", err)
	}
	got, err := cryptB.DecryptFrom(aID, ct, nonce)
	if err != nil {
		t.Fatalf("DecryptFrom: %v", err)
	}
	if got != text {
		t.Fatalf("отримано %q", got)
	}

	// Ключ друга кешується: повторне шифрування не робить нових запитів.
	before := atomic.LoadInt32(&calls)
	if _, _, err := cryptA.encryptE2E(bID, "ще одне"); err != nil {
		t.Fatal(err)
	}
	if after := atomic.LoadInt32(&calls); after != before {
		t.Fatalf("повторне шифрування зробило %d зайвих запитів", after-before)
	}
}

func TestChatCryptoRejectsMissingKey(t *testing.T) {
	ks := NewKeyStore(filepath.Join(t.TempDir(), "k"))
	c := NewChatCrypto(ks, NewClient(nil))
	if err := c.Init(); err != nil {
		t.Fatal(err)
	}
	var calls int32
	c.client = NewClient(fakeKeyServer("aaaa1111", c.PublicKeyB64(), map[string]string{}, &calls))
	if _, _, err := c.encryptE2E("bbbb2222", "текст"); err != ErrKeyNotFound {
		t.Fatalf("очікували ErrKeyNotFound, отримали %v", err)
	}
}
