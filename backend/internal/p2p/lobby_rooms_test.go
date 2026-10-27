package p2p

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type seenReq struct {
	method, path, query string
	hdr                 http.Header
	body                map[string]any
}

// recServer відповідає заданим статусом/тілом і запам'ятовує останній запит.
func recServer(t *testing.T, status int, body string) (*LobbyClient, *seenReq) {
	t.Helper()
	seen := &seenReq{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		seen.method, seen.path, seen.query, seen.hdr = r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Clone()
		seen.body = nil
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &seen.body)
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return NewLobbyClient(srv.URL, "token-1234567890abcdef"), seen
}

func TestAuthHeadersSentOnlyWithBearer(t *testing.T) {
	c, seen := recServer(t, 200, `{"rooms":[]}`)
	if _, err := c.Shared(context.Background()); err != nil {
		t.Fatal(err)
	}
	if seen.hdr.Get("Authorization") != "" || seen.hdr.Get("X-Shaurma-HWID") != "" {
		t.Fatalf("без WithAuth заголовків JWT бути не має: %v", seen.hdr)
	}
	c.WithAuth(func() (string, string) { return "jwt.value.x", "hwid-1" })
	if _, err := c.Shared(context.Background()); err != nil {
		t.Fatal(err)
	}
	if seen.hdr.Get("Authorization") != "Bearer jwt.value.x" || seen.hdr.Get("X-Shaurma-HWID") != "hwid-1" {
		t.Fatalf("JWT/HWID не додано: %v", seen.hdr)
	}
	// Порожній bearer (акаунта немає) → запит анонімний, а не «Bearer ».
	c.WithAuth(func() (string, string) { return "", "hwid-1" })
	_, _ = c.Shared(context.Background())
	if seen.hdr.Get("Authorization") != "" {
		t.Fatalf("порожній JWT не має слатись: %v", seen.hdr)
	}
}

func TestClaimAddressAccessBody(t *testing.T) {
	c, seen := recServer(t, 200, `{"subdomain":"prlool-482917"}`)
	r, err := c.ClaimAddressAccess(context.Background(), "prlool", strings.Repeat("a", 32), "friends")
	if err != nil || r.Subdomain != "prlool-482917" {
		t.Fatalf("%v %+v", err, r)
	}
	if seen.body["access"] != "friends" || seen.body["baseName"] != "prlool" {
		t.Fatalf("тіло: %v", seen.body)
	}
	if _, err := c.ClaimAddress(context.Background(), "prlool", strings.Repeat("a", 32)); err != nil {
		t.Fatal(err)
	}
	if _, has := seen.body["access"]; has {
		t.Fatalf("без access поле не шлемо: %v", seen.body)
	}
}

func TestHostUpdateLeaseFields(t *testing.T) {
	c, seen := recServer(t, 200, `{"ok":true,"address":"x.play.shaurma-launcher.org","disabled":false,"access":"friends","leaseExpiresAt":1234}`)
	acc := "friends"
	out, err := c.HostUpdate(context.Background(), HostUpdateRequest{
		Subdomain: "x-111111", SessionToken: strings.Repeat("c", 64), Access: &acc, HostIP: "100.64.0.5", HostPort: "25565",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Access != "friends" || out.LeaseExpiresAt != 1234 {
		t.Fatalf("%+v", out)
	}
	if seen.body["sessionToken"] != strings.Repeat("c", 64) || seen.body["access"] != "friends" {
		t.Fatalf("тіло: %v", seen.body)
	}
	if _, has := seen.body["ownerSecret"]; has && seen.body["ownerSecret"] != "" {
		t.Fatalf("співхост не має слати ownerSecret: %v", seen.body)
	}
	// Access=nil → ключа в тілі немає (режим не змінюється).
	_, _ = c.HostUpdate(context.Background(), HostUpdateRequest{Subdomain: "x-111111", HostIP: "100.64.0.5", HostPort: "25565"})
	if _, has := seen.body["access"]; has {
		t.Fatalf("nil Access має не потрапляти в тіло: %v", seen.body)
	}
}

func TestTypedErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   error
		check  func(*LobbyError) bool
	}{
		{"busy", 409, `{"error":"p2p_room_busy","hostName":"Steve","errorMessage":"Цю кімнату зараз хостить інший гравець: Steve."}`, ErrRoomBusy,
			func(e *LobbyError) bool { return e.HostName == "Steve" }},
		{"lease_required", 409, `{"error":"p2p_lease_required","errorMessage":"Спершу візьміть оренду."}`, ErrLeaseRequired, nil},
		{"lease_lost", 403, `{"error":"p2p_lease_lost","errorMessage":"Оренду хоста втрачено."}`, ErrLeaseLost, nil},
		{"revoked", 403, `{"error":"p2p_host_revoked","errorMessage":"Власник забрав у вас доступ."}`, ErrHostRevoked, nil},
		{"not_allowed", 403, `{"error":"p2p_not_allowed","errorMessage":"Ви не в списку друзів."}`, ErrNotAllowed, nil},
		{"tunnel_disabled", 403, `{"error":"p2p_tunnel_disabled","errorMessage":"Цей P2P-тунель вимкнено адміністратором мережі."}`, ErrTunnelDisabled, nil},
		{"owner_only", 403, `{"error":"p2p_owner_only","errorMessage":"Лише власник."}`, ErrOwnerOnly, nil},
		{"account_required", 401, `{"error":"p2p_account_required","errorMessage":"Потрібен акаунт."}`, ErrAccountRequired, nil},
		{"limit", 429, `{"error":"p2p_limit_reached","limit":3,"scope":"device","errorMessage":"Забагато кімнат."}`, ErrLimitReached,
			func(e *LobbyError) bool { return e.Limit == 3 && e.Scope == "device" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := recServer(t, tc.status, tc.body)
			_, err := c.HostStart(context.Background(), "x-111111", "")
			if !errors.Is(err, tc.want) {
				t.Fatalf("errors.Is(%v, %v) = false", err, tc.want)
			}
			le, ok := AsLobbyError(err)
			if !ok || le.Status != tc.status {
				t.Fatalf("AsLobbyError: %v %v", le, ok)
			}
			if tc.check != nil && !tc.check(le) {
				t.Fatalf("поля: %+v", le)
			}
			if strings.Contains(err.Error(), "HTTP ") && tc.status != 429 {
				t.Errorf("людське повідомлення без «HTTP n»: %q", err.Error())
			}
			if errors.Is(err, ErrAddressNotFound) {
				t.Error("не 404")
			}
		})
	}
	// 404 і далі = ErrAddressNotFound, а не типізований код.
	c, _ := recServer(t, 404, `{"error":"Адресу не знайдено"}`)
	if _, err := c.JoinAddress(context.Background(), "x-111111"); !errors.Is(err, ErrAddressNotFound) {
		t.Fatalf("404: %v", err)
	}
}

func TestHostStartStopKick(t *testing.T) {
	c, seen := recServer(t, 200, `{"ok":true,"address":"x-111111.play.shaurma-launcher.org","shared":true,"sessionToken":"`+strings.Repeat("d", 64)+`","leaseTtl":45,"leaseExpiresAt":99,"access":"list"}`)
	r, err := c.HostStart(context.Background(), "x-111111", "")
	if err != nil || !r.Shared || r.LeaseTTL != 45 || r.SessionToken != strings.Repeat("d", 64) {
		t.Fatalf("%v %+v", err, r)
	}
	if seen.path != "/p2p/host-start" {
		t.Fatal(seen.path)
	}
	if _, has := seen.body["ownerSecret"]; has {
		t.Fatalf("співхост без ownerSecret: %v", seen.body)
	}
	_, _ = c.HostStart(context.Background(), "x-111111", strings.Repeat("a", 32))
	if seen.body["ownerSecret"] != strings.Repeat("a", 32) {
		t.Fatalf("власник шле ownerSecret: %v", seen.body)
	}
	if err := c.HostStop(context.Background(), "x-111111", strings.Repeat("d", 64)); err != nil || seen.path != "/p2p/host-stop" || seen.body["sessionToken"] == "" {
		t.Fatalf("%v %s %v", err, seen.path, seen.body)
	}
	c2, _ := recServer(t, 200, `{"ok":true,"kicked":true}`)
	if k, err := c2.LeaseKick(context.Background(), "x-111111", strings.Repeat("a", 32)); err != nil || !k {
		t.Fatalf("%v %v", k, err)
	}
}

func TestAdmit(t *testing.T) {
	tk := strings.Repeat("e", 64)
	c, seen := recServer(t, 200, `{"ok":true,"accountId":"acc1"}`)
	ok, err := c.Admit(context.Background(), "x-111111", tk, "", strings.Repeat("d", 64))
	if err != nil || !ok {
		t.Fatalf("%v %v", ok, err)
	}
	if seen.body["sessionToken"] != strings.Repeat("d", 64) || seen.body["ownerSecret"] != nil {
		t.Fatalf("співхост: %v", seen.body)
	}
	_, _ = c.Admit(context.Background(), "x-111111", tk, strings.Repeat("a", 32), strings.Repeat("d", 64))
	if seen.body["ownerSecret"] == nil || seen.body["sessionToken"] != nil {
		t.Fatalf("власник шле ownerSecret, а не токен: %v", seen.body)
	}
	// 403 {"ok":false} → false без помилки.
	c2, _ := recServer(t, 403, `{"ok":false}`)
	if ok, err := c2.Admit(context.Background(), "x-111111", tk, "s", ""); ok || err != nil {
		t.Fatalf("відмова: %v %v", ok, err)
	}
	// Втрачена оренда хоста — це помилка, а не «гостя не пускати».
	c3, _ := recServer(t, 403, `{"error":"p2p_lease_lost","errorMessage":"Оренда недійсна."}`)
	if ok, err := c3.Admit(context.Background(), "x-111111", tk, "", "t"); ok || !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("lease_lost: %v %v", ok, err)
	}
}

func TestCohostAndACLRequests(t *testing.T) {
	secret := strings.Repeat("a", 32)
	c, seen := recServer(t, 200, `{"cohosts":[{"id":"u1","nick":"Alex","addedAt":5}],"host":{"accountId":"u1","nick":"Alex","leaseExpiresAt":7},"max":3}`)
	l, err := c.Cohosts(context.Background(), "x-111111", secret)
	if err != nil || len(l.Cohosts) != 1 || l.Host == nil || l.Host.Nick != "Alex" || l.Max != 3 {
		t.Fatalf("%v %+v", err, l)
	}
	if seen.method != "GET" || seen.hdr.Get("X-P2P-Owner-Secret") != secret || strings.Contains(seen.query, secret) {
		t.Fatalf("секрет лише в заголовку: %s %s %v", seen.method, seen.query, seen.hdr)
	}
	if seen.query != "address=x-111111" {
		t.Fatal(seen.query)
	}

	c, seen = recServer(t, 200, `{"ok":true,"results":[{"key":"Alex","op":"add","status":"not_friend"}],"cohosts":[]}`)
	r, err := c.CohostApply(context.Background(), "x-111111", secret, []ACLEntry{{Nick: "Alex"}}, nil)
	if err != nil || len(r.Results) != 1 || r.Results[0].Status != "not_friend" {
		t.Fatalf("%v %+v", err, r)
	}
	if seen.method != "POST" || seen.path != "/p2p/cohost" || seen.body["ownerSecret"] != secret {
		t.Fatalf("%v", seen)
	}

	c, _ = recServer(t, 200, `{"allow":[{"id":"u2","nick":"Bob"}],"deny":[]}`)
	acl, err := c.ACL(context.Background(), "x-111111", secret)
	if err != nil || len(acl.Allow) != 1 || acl.Allow[0].Nick != "Bob" || acl.Deny == nil && false {
		t.Fatalf("%v %+v", err, acl)
	}
	c, seen = recServer(t, 200, `{"ok":true,"results":[{"key":"Bob","op":"add","status":"ok"}]}`)
	ar, err := c.ACLApply(context.Background(), "x-111111", secret, []ACLEntry{{Nick: "Bob", Kind: "deny"}}, nil)
	if err != nil || ar.Results[0].Status != "ok" {
		t.Fatalf("%v %+v", err, ar)
	}
	adds, _ := seen.body["add"].([]any)
	if len(adds) != 1 || adds[0].(map[string]any)["kind"] != "deny" {
		t.Fatalf("add: %v", seen.body)
	}
}

func TestSharedAndJoinTicket(t *testing.T) {
	c, _ := recServer(t, 200, `{"rooms":[{"subdomain":"x-1","address":"x-1.play.shaurma-launcher.org","ownerNick":"Own","access":"friends","busy":true,"mine":false,"hostNick":"Alex"}]}`)
	rooms, err := c.Shared(context.Background())
	if err != nil || len(rooms) != 1 || !rooms[0].Busy || rooms[0].HostNick != "Alex" || rooms[0].OwnerNick != "Own" {
		t.Fatalf("%v %+v", err, rooms)
	}
	tk := strings.Repeat("f", 64)
	c, _ = recServer(t, 200, `{"ip":"100.64.0.9","port":"25565","udpPort":"","ticket":"`+tk+`","ticketExpiresAt":1700000000000,"access":"friends"}`)
	ri, err := c.JoinAddress(context.Background(), "X-1")
	if err != nil || ri.Ticket != tk || ri.TicketExpiresAt != 1700000000000 || ri.Access != "friends" {
		t.Fatalf("%v %+v", err, ri)
	}
	// Режим all: квитка немає — поля порожні, сумісність зі старим вузлом.
	c, _ = recServer(t, 200, `{"ip":"100.64.0.9","port":"25565"}`)
	ri, err = c.JoinAddress(context.Background(), "x-1")
	if err != nil || ri.Ticket != "" || ri.TicketExpiresAt != 0 {
		t.Fatalf("%v %+v", err, ri)
	}
}
