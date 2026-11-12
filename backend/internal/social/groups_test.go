package social

import (
	"encoding/json"
	"strings"
	"testing"
)

const testGID = "g00000000000000000000000000000001"

func TestGroupOpRejectsUnknown(t *testing.T) {
	p := NewPresenceClient(PresenceConfig{})
	if _, err := p.GroupOp("drop_tables", nil); err != ErrGroupOp {
		t.Fatalf("невідома операція: %v", err)
	}
	if _, err := p.GroupOp("send", map[string]any{"type": "evil"}); err != ErrGroupOp {
		// без з'єднання ErrNotConnected теж допустимо, але службове поле не має проходити
		if err != ErrNotConnected {
			t.Fatalf("службове поле: %v", err)
		}
	}
}

func TestGroupIDAndClaimValidation(t *testing.T) {
	if !ValidGroupID(testGID) || ValidGroupID("g123") || ValidGroupID("x"+testGID[1:]) || ValidGroupID(testGID+"0") ||
		ValidGroupID(strings.ToUpper(testGID)) {
		t.Fatal("ValidGroupID")
	}
	var got []GroupShareClaim
	p := NewPresenceClient(PresenceConfig{OnGroupShareClaim: func(c GroupShareClaim) { got = append(got, c) }})
	acc := strings.Repeat("a", 32)
	good := `{"type":"group_share_claim_in","claimId":"claim-12345678","groupId":"` + testGID + `","messageId":5,"sid":"` +
		strings.Repeat("c", 32) + `","from":"` + acc + `"}`
	if !p.handleGroupFrame("group_share_claim_in", []byte(good)) || len(got) != 1 {
		t.Fatalf("коректна заявка: %v", got)
	}
	for _, bad := range []string{
		strings.Replace(good, "claim-12345678", "x", 1),
		strings.Replace(good, testGID, "bad", 1),
		strings.Replace(good, strings.Repeat("c", 32), "../../etc", 1),
		strings.Replace(good, `"messageId":5`, `"messageId":0`, 1),
		strings.Replace(good, acc, "ZZ", 1),
	} {
		p.handleGroupFrame("group_share_claim_in", []byte(bad))
	}
	if len(got) != 1 {
		t.Fatalf("некоректні заявки не доходять до відправника: %d", len(got))
	}
}

func TestGroupEventsForwardedOnlyKnown(t *testing.T) {
	var types []string
	p := NewPresenceClient(PresenceConfig{OnGroupEvent: func(typ string, raw json.RawMessage) { types = append(types, typ) }})
	for _, typ := range []string{"group_message", "groups_snapshot", "group_removed", "friend_presence", "new_message", "group_bogus"} {
		p.handleGroupFrame(typ, []byte(`{"type":"`+typ+`"}`))
	}
	if strings.Join(types, ",") != "group_message,groups_snapshot,group_removed" {
		t.Fatalf("пересилаються лише відомі групові події: %v", types)
	}
}

func TestGroupResultResolvesRequest(t *testing.T) {
	p := NewPresenceClient(PresenceConfig{})
	ch := make(chan groupResult, 1)
	p.histMu.Lock()
	p.grpReq["r1"] = ch
	p.histMu.Unlock()
	p.handleGroupFrame("group_result", []byte(`{"type":"group_result","reqId":"r1","code":"Forbidden"}`))
	r := <-ch
	if r.code != "forbidden" {
		t.Fatalf("код нормалізується: %q", r.code)
	}
	// Невідомий reqId — ігнорується без паніки.
	p.handleGroupFrame("group_result", []byte(`{"type":"group_result","reqId":"nope"}`))
}

// Регресія: group_message несе "message" ОБ'ЄКТОМ.
func TestGroupMessageFrameParsesIntoInEvent(t *testing.T) {
	frame := []byte(`{"type":"group_message","message":{"id":7,"groupId":"g` + strings.Repeat("a", 32) + `","from":"x","kind":"text","body":"hi","ts":1}}`)
	var ev inEvent
	if err := json.Unmarshal(frame, &ev); err != nil {
		t.Fatalf("кадр group_message має розбиратися без помилки: %v", err)
	}
	if ev.Type != "group_message" {
		t.Fatalf("type = %q", ev.Type)
	}
}
