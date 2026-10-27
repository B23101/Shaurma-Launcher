package social

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestParseRemoteEnvelope(t *testing.T) {
	ok := `{"v":1,"k":"cmd","id":"req_12345678","op":"start","srv":"srv-1","ts":1}`
	if e, good := parseRemoteEnvelope(ok, "cmd"); !good || e.Srv != "srv-1" || e.Op != "start" {
		t.Fatalf("валідний конверт відхилено: %+v", e)
	}
	bad := []string{
		``, `{`, `{"v":2,"k":"cmd","id":"req_12345678","op":"start","srv":"a"}`,
		`{"v":1,"k":"res","id":"req_12345678","op":"start","srv":"a"}`, // не той вид
		`{"v":1,"k":"cmd","id":"short","op":"start","srv":"a"}`,
		`{"v":1,"k":"cmd","id":"req_12345678","op":"rm","srv":"a"}`,
		`{"v":1,"k":"cmd","id":"req_12345678","op":"start","srv":"../x"}`,
		`{"v":1,"k":"cmd","id":"req_12345678","op":"start","srv":"a","pad":"` + strings.Repeat("x", MaxPlaintextBytes) + `"}`,
	}
	for i, b := range bad {
		if _, good := parseRemoteEnvelope(b, "cmd"); good {
			t.Errorf("#%d мало відхилитись: %.60s", i, b)
		}
	}
	e, _ := parseRemoteEnvelope(`{"v":1,"k":"res","id":"req_12345678","op":"stop","srv":"a","state":"hax","code":"  OK_X "}`, "res")
	if e.State != "stopped" {
		t.Errorf("невідомий стан має зводитись до stopped: %q", e.State)
	}
}

func TestTimeFresh(t *testing.T) {
	now := time.Now()
	if !timeFresh(now.UnixMilli(), now) || !timeFresh(now.Add(-4*time.Minute).UnixMilli(), now) || !timeFresh(now.Add(4*time.Minute).UnixMilli(), now) {
		t.Error("свіжий час відхилено")
	}
	if timeFresh(now.Add(-6*time.Minute).UnixMilli(), now) || timeFresh(now.Add(6*time.Minute).UnixMilli(), now) {
		t.Error("застарілий/майбутній час прийнято")
	}
}

func TestSharesQueueAndResync(t *testing.T) {
	p := NewPresenceClient(PresenceConfig{})
	a := strings.Repeat("a", 32)
	if !ValidAccountID(a) {
		t.Skip("формат id інший")
	}
	// Порожньо й нічого не публікували — rs_keep не потрібен (економимо ліміт кадрів).
	p.resyncShares()
	if m := p.takeShare(); m != nil {
		t.Fatalf("порожній перелік не має слати кадрів: %s", m)
	}
	p.SetShares([]RemoteShare{{ServerID: "s1", Name: "Світ", State: "bogus", Grants: []RemoteShareGrant{{AccountID: a, Start: true}, {AccountID: "bad id", Start: true}, {AccountID: a, Stop: true}}}})
	var f map[string]any
	if err := json.Unmarshal(p.takeShare(), &f); err != nil || f["type"] != "rs_set" || f["state"] != "stopped" {
		t.Fatalf("очікував rs_set зі станом stopped: %v", f)
	}
	if g := f["grants"].([]any); len(g) != 1 {
		t.Fatalf("дублікат/невалідний акаунт мають відсіятись: %v", g)
	}
	if p.takeShare() != nil {
		t.Fatal("більше кадрів немає")
	}
	// Зміна лише стану → дешевий rs_state.
	p.SetShareState("s1", "running")
	json.Unmarshal(p.takeShare(), &f)
	if f["type"] != "rs_state" || f["state"] != "running" {
		t.Fatalf("очікував rs_state: %v", f)
	}
	// Реконект: повний rs_set і rs_keep із переліком.
	p.resyncShares()
	json.Unmarshal(p.takeShare(), &f)
	if f["type"] != "rs_set" || f["state"] != "running" {
		t.Fatalf("після resync: %v", f)
	}
	json.Unmarshal(p.takeShare(), &f)
	if f["type"] != "rs_keep" {
		t.Fatalf("очікував rs_keep: %v", f)
	}
	// Зняття доступу → rs_clear.
	p.SetShares(nil)
	json.Unmarshal(p.takeShare(), &f)
	if f["type"] != "rs_clear" || f["serverId"] != "s1" {
		t.Fatalf("очікував rs_clear: %v", f)
	}
	// Після публікації порожній keep уже потрібен (VPS міг щось пам'ятати).
	p.resyncShares()
	json.Unmarshal(p.takeShare(), &f)
	if f["type"] != "rs_keep" {
		t.Fatalf("порожній keep після публікації: %v", f)
	}
}

func TestRemoteFramesRequireFriend(t *testing.T) {
	friend, stranger := strings.Repeat("a", 32), strings.Repeat("b", 32)
	var gotCmd []RemoteIncoming
	var gotList []RemoteOwnerList
	p := NewPresenceClient(PresenceConfig{
		OnRemoteCmd:  func(c RemoteIncoming) { gotCmd = append(gotCmd, c) },
		OnRemoteList: func(l RemoteOwnerList) { gotList = append(gotList, l) },
	})
	p.SetFriends([]string{friend})
	for _, from := range []string{friend, stranger} {
		p.handleRemoteIncoming(inEvent{From: from, ReqID: "req_12345678", Ciphertext: "abc", Nonce: "n"})
		p.handleRemoteList(inEvent{Owner: from, Online: true, Servers: []rsServerWire{{ID: "s1", Name: "x", CanStart: true}, {ID: "s2"}, {ID: "../bad", CanStop: true}}})
	}
	if len(gotCmd) != 1 || gotCmd[0].From != friend {
		t.Fatalf("команду від не-друга мають відкинути: %+v", gotCmd)
	}
	if len(gotList) != 1 || len(gotList[0].Servers) != 1 || gotList[0].Servers[0].ID != "s1" {
		t.Fatalf("список: %+v", gotList)
	}
	// Некоректний reqId / порожній шифротекст.
	p.handleRemoteIncoming(inEvent{From: friend, ReqID: "x", Ciphertext: "abc", Nonce: "n"})
	p.handleRemoteIncoming(inEvent{From: friend, ReqID: "req_12345678", Nonce: "n"})
	if len(gotCmd) != 1 {
		t.Fatal("некоректні кадри прийнято")
	}
}
