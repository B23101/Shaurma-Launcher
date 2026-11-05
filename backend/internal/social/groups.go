package social

// groups.go — клієнт групових чатів. Лаунчер тут «тонкий»: шле кадри group_op, чекає group_result і
// віддає живі події group_* у UI як є. Логіка прав (хто адмін, хто що може) — на сервері; клієнт їй
// не довіряє для безпеки, лише для відображення.
//
// Виняток — передача збірки в групу: заявку «Отримати» від учасника отримує ЛАУНЧЕР ВІДПРАВНИКА
// (OnGroupShareClaim) і сам вирішує, чи віддавати картку (ключ видається кожному окремо).

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"
)

// Дозволені операції group_op (дзеркало сервера). Усе інше відкидаємо ще на клієнті.
var groupOps = map[string]bool{
	"link_create": true, "link_join": true, "p2p_authorize": true,
	"create": true, "add": true, "invite": true, "invite_respond": true, "invite_revoke": true,
	"leave": true, "kick": true, "transfer": true, "settings": true, "delete": true,
	"send": true, "edit": true, "delete_message": true, "pin": true, "react": true,
	"history": true, "read": true, "list": true, "share_claim": true,
}

// Поля, які можна передати в group_op. Службові (type, reqId, op) задаємо самі.
var groupOpFields = map[string]bool{
	"token": true, "revoke": true,
	"groupId": true, "title": true, "members": true, "account": true, "inviteId": true, "accept": true,
	"messageId": true, "kind": true, "body": true, "clientMessageId": true, "replyToId": true,
	"emoji": true, "pin": true, "before": true, "after": true, "limit": true, "settings": true,
}

// Живі події груп, що віддаються в UI.
var groupEvents = map[string]bool{
	"groups_snapshot": true, "group_changed": true, "group_removed": true, "group_message": true,
	"group_message_deleted": true, "group_message_edited": true, "group_pins": true,
	"group_reactions": true, "group_read_elsewhere": true, "group_typing": true,
	"group_invite_in": true, "group_invite_gone": true, "group_share_card_in": true,
	"group_share_result": true,
}

var (
	claimIDRe  = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)
	groupSIDRe = regexp.MustCompile(`^[a-f0-9]{32}$`)

	// ErrGroupOp — недопустима операція чи поле.
	ErrGroupOp = errors.New("social: bad group op")
)

// GroupShareClaim — учасник групи просить мою збірку (я — відправник картки).
type GroupShareClaim struct {
	ClaimID   string `json:"claimId"`
	GroupID   string `json:"groupId"`
	MessageID int64  `json:"messageId"`
	SID       string `json:"sid"`
	From      string `json:"from"`
}

type groupWire struct {
	Type      string          `json:"type"`
	ReqID     string          `json:"reqId"`
	Code      string          `json:"code"`
	ClaimID   string          `json:"claimId"`
	GroupID   string          `json:"groupId"`
	MessageID int64           `json:"messageId"`
	SID       string          `json:"sid"`
	From      string          `json:"from"`
	Raw       json.RawMessage `json:"-"`
}

// groupResult — що повертає group_result: код помилки або повні дані відповіді.
type groupResult struct {
	code string
	raw  json.RawMessage
}

const groupOpTimeout = 15 * time.Second

// GroupOp виконує операцію над групами й повертає JSON відповіді (group_result без службових полів
// розбирає UI). Помилка сервера повертається як «social:<code>».
func (p *PresenceClient) GroupOp(op string, fields map[string]any) (json.RawMessage, error) {
	if !groupOps[op] {
		return nil, ErrGroupOp
	}
	if p.State() != ConnOnline {
		return nil, ErrNotConnected
	}
	id := randomClientMsgID()
	if id == "" {
		return nil, ErrNotConnected
	}
	req := map[string]any{"type": "group_op", "reqId": id, "op": op}
	for k, v := range fields {
		if !groupOpFields[k] {
			return nil, ErrGroupOp
		}
		req[k] = v
	}
	frame, err := json.Marshal(req)
	if err != nil || len(frame) > maxWSFrame/2 {
		return nil, ErrBadInput
	}
	ch := make(chan groupResult, 1)
	p.histMu.Lock()
	p.grpReq[id] = ch
	p.histMu.Unlock()
	defer func() { p.histMu.Lock(); delete(p.grpReq, id); p.histMu.Unlock() }()

	if !p.enqueue(frame) {
		return nil, ErrQueueFull
	}
	p.poke()
	select {
	case r := <-ch:
		if r.code != "" {
			return nil, opError(r.code)
		}
		return r.raw, nil
	case <-time.After(groupOpTimeout):
		return nil, ErrNotConnected
	}
}

// SendGroupTyping — «друкує…» у групі (без відповіді; сервер сам обмежує частоту).
func (p *PresenceClient) SendGroupTyping(groupID string) error {
	if !ValidGroupID(groupID) {
		return ErrBadInput
	}
	frame, _ := json.Marshal(map[string]any{"type": "group_typing", "groupId": groupID})
	if !p.enqueue(frame) {
		return ErrQueueFull
	}
	p.poke()
	return nil
}

// SendGroupShareCard віддає картку (base64url, без префікса) заявнику; SendGroupShareDeny — відмовляє.
func (p *PresenceClient) SendGroupShareCard(claimID, card string) error {
	if !claimIDRe.MatchString(claimID) || card == "" || len(card) > 2000 {
		return ErrBadInput
	}
	frame, _ := json.Marshal(map[string]any{"type": "group_share_card", "claimId": claimID, "card": card})
	if !p.enqueue(frame) {
		return ErrQueueFull
	}
	p.poke()
	return nil
}

func (p *PresenceClient) SendGroupShareDeny(claimID, reason string) error {
	if !claimIDRe.MatchString(claimID) {
		return ErrBadInput
	}
	if reason != "busy" && reason != "declined" {
		reason = "unavailable"
	}
	frame, _ := json.Marshal(map[string]any{"type": "group_share_deny", "claimId": claimID, "reason": reason})
	if !p.enqueue(frame) {
		return ErrQueueFull
	}
	p.poke()
	return nil
}

func ValidGroupID(s string) bool {
	if len(s) != 33 || s[0] != 'g' {
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// handleGroupFrame — розбір кадрів group_* у readLoop. Повертає true, якщо кадр був груповий.
func (p *PresenceClient) handleGroupFrame(typ string, data []byte) bool {
	switch {
	case typ == "group_result":
		var w groupWire
		if json.Unmarshal(data, &w) != nil || w.ReqID == "" {
			return true
		}
		p.histMu.Lock()
		ch := p.grpReq[w.ReqID]
		p.histMu.Unlock()
		if ch != nil {
			select {
			case ch <- groupResult{code: strings.ToLower(CleanText(w.Code, 32)), raw: append(json.RawMessage(nil), data...)}:
			default:
			}
		}
		return true
	case typ == "group_share_claim_in":
		var c GroupShareClaim
		if json.Unmarshal(data, &c) != nil || !claimIDRe.MatchString(c.ClaimID) || !ValidGroupID(c.GroupID) ||
			!groupSIDRe.MatchString(c.SID) || !ValidAccountID(c.From) || c.MessageID <= 0 {
			return true
		}
		if p.cfg.OnGroupShareClaim != nil {
			p.cfg.OnGroupShareClaim(c)
		}
		return true
	case groupEvents[typ]:
		if p.cfg.OnGroupEvent != nil {
			p.cfg.OnGroupEvent(typ, append(json.RawMessage(nil), data...))
		}
		return true
	}
	return false
}
