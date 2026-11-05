package main

// Передача збірки/сервера В ГРУПУ (групові чати). Це розширення share_shaurma.go, а не окрема система.
//
// Потік:
//  1. Відправник ділиться в режимі групи (ShareStartRequest.GroupID): відкривається звичайна передача
//     БЕЗ одержувачів, а в чат групи йде картка build_offer — без жодних ключів доступу.
//  2. Учасник натискає «Отримати» → GroupShareClaim → сервер → лаунчер ВІДПРАВНИКА (group:share_claim).
//  3. Відправник сам вирішує: лише ДРУЗІ відправника; вільний слот; інакше — відмова. Кожному видається
//     окремий ключ (host.AddRecipient) і окремий гість тимчасового інвайту: P2P-передача в кожного своя.
//  4. Картка з ключем їде лише цьому учаснику; його лаунчер приймає її тим самим ShareAccept, що й у
//     приватному чаті. Одночасно — не більше share.MaxRecipients; завершені слоти звільняються.

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"shaurma-launcher-backend/internal/crashguard"
	"shaurma-launcher-backend/internal/share"
	"shaurma-launcher-backend/internal/social"
)

// groupWant — мої виданні заявки «Отримати»: claimId → від кого очікую картку. Картку, що не збігається
// із заявкою, ігноруємо (скомпрометований сервер не може підсунути чужу передачу).
type groupWantEntry struct {
	sender string
	groups string
	exp    time.Time
}

var groupWant = struct {
	mu    sync.Mutex
	want  map[string]groupWantEntry
	early map[string]social.GroupEvent // картка прийшла раніше, ніж я записав заявку
}{want: map[string]groupWantEntry{}, early: map[string]social.GroupEvent{}}

const groupWantTTL = 3 * time.Minute

type groupCardIn struct {
	ClaimID string `json:"claimId"`
	GroupID string `json:"groupId"`
	From    string `json:"from"`
	Card    string `json:"card"`
}

// shareGroupPost публікує в групі картку збірки (kind=build_offer).
func (a *App) shareGroupPost(o *shareOut, svc *social.Service) error {
	o.mu.Lock()
	gid, kind, name, sid := o.groupID, o.view.Kind, o.view.Name, o.view.ID
	files, total := o.view.Files, o.view.TotalBytes
	cardFn := o.cardFn
	o.mu.Unlock()
	if cardFn == nil {
		return errors.New("передача ще готується")
	}
	meta := cardFn("").Meta // ключ тут порожній і в картку групи НЕ потрапляє
	body, err := json.Marshal(map[string]any{"k": kind, "n": name, "f": files, "b": total, "sid": sid, "m": meta})
	if err != nil {
		return err
	}
	_, err = svc.GroupOp("send", map[string]any{
		"groupId": gid, "kind": "build_offer", "body": string(body), "clientMessageId": sid,
	})
	if err != nil {
		return errors.New("не вдалося надіслати картку в групу: " + err.Error())
	}
	return nil
}

// shareGroupOnSocial — події груп, що стосуються передач. true — подію оброблено (далі не йде).
func (a *App) shareGroupOnSocial(event string, payload any) bool {
	switch event {
	case "group:share_claim":
		if c, ok := payload.(social.GroupShareClaim); ok {
			go func() {
				defer crashguard.Recover("shareGroupClaim")
				a.shareGroupClaim(c)
			}()
		}
		return false // подію бачить і UI (не обов'язково, але безпечно)
	case "group:event":
		if ev, ok := payload.(social.GroupEvent); ok && ev.Type == "group_share_card_in" {
			go func() {
				defer crashguard.Recover("shareGroupCardIn")
				a.shareGroupCardIn(ev)
			}()
		}
	}
	return false
}

// shareGroupClaim — я відправник: учасник групи просить мою збірку.
func (a *App) shareGroupClaim(c social.GroupShareClaim) {
	svc := currentSocialService()
	if svc == nil {
		return
	}
	deny := func(reason string) { _ = svc.GroupShareDeny(c.ClaimID, reason) }

	shareMu.Lock()
	o := shareOuts[c.SID]
	shareMu.Unlock()
	if o == nil {
		deny("unavailable")
		return
	}
	o.mu.Lock()
	host, invID, cardFn, phase, gid := o.host, o.invID, o.cardFn, o.view.Phase, o.groupID
	o.mu.Unlock()
	if gid == "" || gid != c.GroupID || phase != "ready" || host == nil || cardFn == nil {
		deny("unavailable")
		return
	}
	// Передача файлів і P2P-доступ — лише ДРУЗЯМ відправника (так само, як у приватному чаті).
	nick, isFriend := shareFriendSet(svc)[c.From]
	if !isFriend {
		deny("declined")
		return
	}
	// Звільняємо слоти тих, хто вже завершив (щоб група більша за ліміт могла отримувати по черзі).
	for _, rs := range host.Snapshot() {
		if share.IsFinalState(rs.State) {
			host.Forget(rs.ID)
		}
	}
	key, err := host.AddRecipient(c.From)
	if err != nil {
		if errors.Is(err, share.ErrTooManyRecipients) {
			deny("busy")
		} else {
			deny("unavailable")
		}
		return
	}
	undo := func() {
		host.Revoke(c.From)
		host.Forget(c.From)
	}
	if err := svc.TempInviteAddGuests(invID, []string{c.From}); err != nil {
		undo()
		deny("unavailable")
		return
	}
	o.mu.Lock()
	o.keys[c.From] = key
	if o.view.Names == nil {
		o.view.Names = map[string]string{}
	}
	o.view.Names[c.From] = nick
	o.mu.Unlock()

	raw, _ := json.Marshal(cardFn(key))
	if err := svc.GroupShareCard(c.ClaimID, base64.RawURLEncoding.EncodeToString(raw)); err != nil {
		undo()
		deny("unavailable")
		return
	}
	a.emit("tinvite:invited", c.From)
}

// GroupShareClaim — «Отримати збірку» з картки в групі. senderID — автор картки (з повідомлення).
// Повертає claimId; результат приходить подіями group:event (group_share_result) та share:in.
func (a *App) GroupShareClaim(groupID string, messageID int64, senderID string) (string, error) {
	if !social.ValidGroupID(groupID) || messageID <= 0 || !social.ValidAccountID(senderID) {
		return "", social.ErrBadInput
	}
	if a.p2pDisabled() {
		return "", errP2PDisabled()
	}
	svc := a.socialService()
	if svc == nil {
		return "", social.ErrNotLoggedIn
	}
	if _, ok := shareFriendSet(svc)[senderID]; !ok {
		return "", errShareNotFriend // збірку віддають лише друзям відправника
	}
	raw, err := svc.GroupOp("share_claim", map[string]any{"groupId": groupID, "messageId": messageID})
	if err != nil {
		return "", err
	}
	var r struct {
		ClaimID string `json:"claimId"`
	}
	if json.Unmarshal(raw, &r) != nil || r.ClaimID == "" {
		return "", errors.New("сервер не прийняв заявку")
	}
	groupWant.mu.Lock()
	groupSweepLocked()
	groupWant.want[r.ClaimID] = groupWantEntry{sender: senderID, groups: groupID, exp: time.Now().Add(groupWantTTL)}
	early, ok := groupWant.early[r.ClaimID]
	delete(groupWant.early, r.ClaimID)
	groupWant.mu.Unlock()
	if ok {
		go a.shareGroupCardIn(early)
	}
	return r.ClaimID, nil
}

func groupSweepLocked() {
	now := time.Now()
	for k, w := range groupWant.want {
		if now.After(w.exp) {
			delete(groupWant.want, k)
		}
	}
	for k := range groupWant.early {
		if len(groupWant.early) > 8 {
			delete(groupWant.early, k)
		}
	}
}

// shareGroupCardIn — мені прийшла картка з особистим ключем: приймаємо лише відповідь на МОЮ заявку.
func (a *App) shareGroupCardIn(ev social.GroupEvent) {
	var c groupCardIn
	if json.Unmarshal(ev.Data, &c) != nil || c.ClaimID == "" || c.Card == "" {
		return
	}
	groupWant.mu.Lock()
	groupSweepLocked()
	w, ok := groupWant.want[c.ClaimID]
	if !ok {
		groupWant.early[c.ClaimID] = ev // можливо, GroupShareClaim ще не встиг записати заявку
		groupWant.mu.Unlock()
		return
	}
	delete(groupWant.want, c.ClaimID)
	groupWant.mu.Unlock()
	if c.From != w.sender || c.GroupID != w.groups {
		return
	}
	view, err := a.ShareAccept(c.Card, c.From, "")
	if err != nil {
		a.emit("group:share_error", map[string]string{"claimId": c.ClaimID, "error": err.Error()})
		return
	}
	a.emit("group:share_started", map[string]any{"claimId": c.ClaimID, "view": view})
}
