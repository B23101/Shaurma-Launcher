package main

// notify_events_shaurma.go — багаті сповіщення віджета в треї: кожна подія має
// власний тип (Event), а вікно (NotifyPopup.svelte) малює для нього свій колір,
// іконку, мітки й кнопки. Тут: джерела подій (друзі, передачі, запрошення,
// сервери друзів, завантаження), санітизація полів і дії кнопок.
//
// Усе, що приходить ззовні (ніки, назви, адреси), очищається від керуючих
// символів і обрізається: вікно не робить {@html}, але ліміти тримаємо тут.

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"shaurma-launcher-backend/internal/social"
)

// notifyEventKinds — дозволені значення NotifyPayload.Event.
var notifyEventKinds = map[string]bool{
	"download_done": true, "install_failed": true, "action_needed": true,
	"friend_request": true,
	"share_build":    true, "share_server": true, "share_mod": true,
	"invite_temp": true, "invite_perm": true,
	"server_started": true, "server_stopped": true,
}

var (
	notifyColorRe    = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	notifyIconNameRe = regexp.MustCompile(`^ti-[a-z0-9-]{1,40}$`)
	notifyModFragRe  = regexp.MustCompile(`#shaurma=(modrinth|curseforge):([A-Za-z0-9_\-.]{1,40})\s*$`)
	notifyModMRRe    = regexp.MustCompile(`^https://(?:www\.)?modrinth\.com/(?:mod|modpack|resourcepack|shader|datapack|plugin)/([A-Za-z0-9_\-.]{1,64})/?(?:[?#].*)?$`)
	notifyModCFRe    = regexp.MustCompile(`^https://(?:www\.)?curseforge\.com/minecraft/[a-z\-]+/([A-Za-z0-9_\-.]{1,80})/?(?:[?#].*)?$`)
)

// Максимальний розмір data:-іконки в payload (байт). Більше — не показуємо:
// вікно тримає картки в пам'яті, а сповіщенню досить іконки-заглушки.
const notifyMaxIconBytes = 48 * 1024

// sanitizeNotifyRich чистить розширені поля картки (викликається з pushNotify).
func sanitizeNotifyRich(p *NotifyPayload) {
	if !notifyEventKinds[p.Event] {
		p.Event = ""
	}
	p.Sub = truncateNotifyRunes(cleanNotifyText(p.Sub), 60)
	chips := make([]string, 0, 4)
	for _, c := range p.Chips {
		c = truncateNotifyRunes(cleanNotifyText(c), 24)
		if c != "" && len(chips) < 4 {
			chips = append(chips, c)
		}
	}
	p.Chips = chips
	if !strings.HasPrefix(p.Icon, "data:image/") || len(p.Icon) > notifyMaxIconBytes {
		p.Icon = ""
	}
	if !notifyIconNameRe.MatchString(p.IconName) {
		p.IconName = ""
	}
	if !notifyColorRe.MatchString(p.Color) {
		p.Color = ""
	}
	p.PackID = truncateNotifyRunes(cleanNotifyText(p.PackID), 64)
	p.Ref = truncateNotifyRunes(cleanNotifyText(p.Ref), 800)
}

func notifyHumanBytes(n int64) string {
	const k = 1024.0
	f := float64(n)
	switch {
	case f >= k*k*k:
		return fmt.Sprintf("%.1f GB", f/(k*k*k))
	case f >= k*k:
		return fmt.Sprintf("%.0f MB", f/(k*k))
	case f >= k:
		return fmt.Sprintf("%.0f KB", f/k)
	}
	return fmt.Sprintf("%d B", n)
}

// ── Події з головного вікна (завантаження/встановлення) ──────────────────

// NotifyEventIn — вхід NotifyEvent. Event: download_done | install_failed | action_needed.
type NotifyEventIn struct {
	Event  string `json:"event"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	Sub    string `json:"sub"`
	PackID string `json:"packId"`
	Ref    string `json:"ref"`
}

var notifyEventThrottle = struct {
	mu sync.Mutex
	m  map[string]time.Time
}{m: map[string]time.Time{}}

// notifyThrottleAllow — не частіше одного сповіщення на ключ за інтервал.
func notifyThrottleAllow(key string, every time.Duration) bool {
	now := time.Now()
	notifyEventThrottle.mu.Lock()
	defer notifyEventThrottle.mu.Unlock()
	for k, t := range notifyEventThrottle.m { // мапа не росте безмежно
		if now.Sub(t) > time.Minute {
			delete(notifyEventThrottle.m, k)
		}
	}
	if t, ok := notifyEventThrottle.m[key]; ok && now.Sub(t) < every {
		return false
	}
	notifyEventThrottle.m[key] = now
	return true
}

// NotifyEvent показує типізоване сповіщення про завантаження/помилку/потрібну
// дію. Відрізняється від NotifyOS тим, що не ділить з ним загальний 6-секундний
// дебаунс (інакше «завершено» губилось би після щойно показаної помилки) і
// несе packId, за яким вікно бере іконку та колір збірки.
func (a *App) NotifyEvent(in NotifyEventIn) error {
	switch in.Event {
	case "download_done", "install_failed", "action_needed":
	default:
		return errors.New("невідомий тип сповіщення")
	}
	if a.cfg == nil || !a.notificationsAllowed() {
		return nil
	}
	body := cleanNotifyText(in.Body)
	if !notifyThrottleAllow(in.Event+"|"+in.PackID+"|"+truncateNotifyRunes(body, 40), 4*time.Second) {
		return nil
	}
	title := cleanNotifyText(in.Title)
	if title == "" {
		title = "Shaurma Launcher"
	}
	ref := in.Ref
	if ref == "" && in.Event == "install_failed" {
		ref = body // кнопка «Копіювати» віддає повний текст помилки
	}
	a.pushNotify(NotifyPayload{
		Kind: "event", Event: in.Event, Title: title, Body: body, Sub: in.Sub,
		PackID: in.PackID, Ref: ref,
		ID: fmt.Sprintf("%s-%d", in.Event, time.Now().UnixNano()),
	})
	return nil
}

// ── Дії кнопок ───────────────────────────────────────────────────────────

// NotifyAction виконує дію кнопки на картці. accept-friend/decline-friend
// відпрацьовують у бекенді (лаунчер навіть не відкривається); решта показує
// головне вікно і шле йому "notify:action" — App.svelte відкриває потрібне
// місце (модалку передачі, сторінку мода, сторінку збірки, вікно входу).
func (a *App) NotifyAction(action, ref, accountID string) error {
	switch action {
	case "accept-friend", "decline-friend":
		if strings.TrimSpace(ref) == "" {
			return errors.New("порожній запит")
		}
		return a.RespondFriendInvite(ref, action == "accept-friend")
	case "share-offer", "mod-view", "invite-play", "open-pack", "open-friends":
		a.revealMainWindow()
		bridgeWindowOp("main", "focus")
		setNotifyMainWindowActive(true)
		a.emit("notify:action", map[string]string{"action": action, "ref": ref, "accountId": accountID})
		return nil
	}
	return errors.New("невідома дія")
}

// ── Багаті повідомлення чату: картка збірки/сервера, мод ─────────────────

// richChatPayload розпізнає службові повідомлення чату. handled=true — це не
// звичайний текст (payload.Event=="" означає «нічого не показувати»).
func (a *App) richChatPayload(ev social.ChatEvent, nick string) (p NotifyPayload, handled bool) {
	text := strings.TrimSpace(ev.Plaintext)
	lines := strings.Split(text, "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	comment := strings.TrimSpace(strings.Join(lines[:len(lines)-1], "\n"))
	base := NotifyPayload{
		Kind: "event", Title: nick, AccountID: ev.From, TS: ev.TS,
		ID: fmt.Sprintf("chat-%d", ev.ID),
	}

	if strings.HasPrefix(last, shareCardPrefix) {
		c, err := parseShareCard(last)
		if err != nil {
			return NotifyPayload{}, true
		}
		if c.Dev != "" && c.Dev == a.shareDeviceTag() {
			return NotifyPayload{}, true // з цього ж лаунчера: приймати нічого
		}
		base.Event = "share_build"
		if c.Kind == "server" {
			base.Event = "share_server"
		}
		base.Sub = c.Name
		base.Ref = c.SID
		var chips []string
		if v := c.Meta["mc"]; v != "" {
			chips = append(chips, v)
		}
		if v := c.Meta["loader"]; v != "" && v != "vanilla" {
			chips = append(chips, v)
		}
		if v := c.Meta["core"]; v != "" {
			chips = append(chips, v)
		}
		if c.Files > 0 {
			chips = append(chips, fmt.Sprintf("%d файлів", c.Files))
		}
		if c.Bytes > 0 {
			chips = append(chips, notifyHumanBytes(c.Bytes))
		}
		base.Chips = chips
		base.Body = comment
		return base, true
	}

	if strings.HasPrefix(last, "https://") {
		bare := notifyModFragRe.ReplaceAllString(last, "")
		var source, id, slug string
		if m := notifyModFragRe.FindStringSubmatch(last); m != nil && (notifyModMRRe.MatchString(bare) || notifyModCFRe.MatchString(bare)) {
			source, id = m[1], m[2]
		} else if m := notifyModMRRe.FindStringSubmatch(last); m != nil {
			source, id = "modrinth", m[1]
		}
		if source != "" {
			bareTrim := strings.TrimRight(bare, "/")
			slug = bareTrim[strings.LastIndex(bareTrim, "/")+1:]
			base.Event = "share_mod"
			base.Sub = slug
			base.Ref = source + ":" + id
			base.Body = comment
			return base, true
		}
	}
	return NotifyPayload{}, false
}

// ── Запрошення на світ/сервер (тимчасові та постійні) ────────────────────

// notifyServerInvite — друг надіслав запрошення (chat:server_invite). Тимчасове
// чи постійне P2P — вирішує стан кімнати на вузлі (TempInviteStates).
func (a *App) notifyServerInvite(ev social.ServerInviteEvent, nick string) {
	addr := cleanNotifyText(ev.ServerAddr)
	if addr == "" {
		return
	}
	p := NotifyPayload{
		Kind: "event", Event: "invite_temp", Title: nick, AccountID: ev.From,
		Sub: cleanNotifyText(ev.PackName), Body: addr, Ref: addr,
		ID: "tinv-" + cleanNotifyText(ev.InviteID) + "-" + addr,
	}
	if sts, err := a.TempInviteStates([]string{addr}); err == nil && len(sts) > 0 {
		st := sts[0]
		if st.Permanent {
			p.Event = "invite_perm"
		}
		if st.Version != "" {
			p.Chips = append(p.Chips, st.Version)
		}
		if st.Max > 0 {
			p.Chips = append(p.Chips, fmt.Sprintf("%d/%d", st.Players, st.Max))
		}
		if p.Sub == "" {
			p.Sub = cleanNotifyText(st.Motd)
		}
	}
	a.pushNotify(p)
}

// ── Сервери друзів: запустили / зупинили ─────────────────────────────────

var remoteStateSeen = struct {
	mu sync.Mutex
	m  map[string]string
}{m: map[string]string{}}

// notifyRemoteBaseline запамʼятовує стани серверів без сповіщень (повний
// знімок після (пере)підключення — це не «друг щойно запустив»).
func notifyRemoteBaseline(lists []social.RemoteOwnerList) {
	remoteStateSeen.mu.Lock()
	defer remoteStateSeen.mu.Unlock()
	remoteStateSeen.m = map[string]string{}
	for _, l := range lists {
		for _, s := range l.Servers {
			remoteStateSeen.m[l.Owner+"|"+s.ID] = s.State
		}
	}
}

// notifyRemoteDiff порівнює оновлений перелік серверів друга з попереднім і
// показує «запустив»/«зупинив». Пропускає сервери, яких раніше не бачили.
func (a *App) notifyRemoteDiff(l social.RemoteOwnerList) {
	type change struct {
		s    social.RemoteServer
		kind string
	}
	var changes []change
	remoteStateSeen.mu.Lock()
	for _, s := range l.Servers {
		key := l.Owner + "|" + s.ID
		prev, known := remoteStateSeen.m[key]
		remoteStateSeen.m[key] = s.State
		if !known || prev == s.State || !l.Online {
			continue
		}
		switch {
		case s.State == "running" && prev != "running":
			changes = append(changes, change{s, "server_started"})
		case (s.State == "stopped" || s.State == "crashed" || s.State == "error") &&
			(prev == "running" || prev == "stopping" || prev == "starting"):
			changes = append(changes, change{s, "server_stopped"})
		}
	}
	remoteStateSeen.mu.Unlock()
	if len(changes) == 0 || a.cfg == nil || !a.notificationsAllowed() {
		return
	}
	nick := a.socialNickname(l.Owner)
	if nick == "" {
		return // лише друзі
	}
	for _, c := range changes {
		if !notifyThrottleAllow(c.kind+"|"+l.Owner+"|"+c.s.ID, 8*time.Second) {
			continue
		}
		p := NotifyPayload{
			Kind: "event", Event: c.kind, Title: nick, AccountID: l.Owner,
			Sub: cleanNotifyText(c.s.Name), Color: c.s.Color, Icon: c.s.Img, IconName: c.s.Icon,
			ID: fmt.Sprintf("srv-%s-%s-%s-%d", l.Owner, c.s.ID, c.kind, time.Now().UnixMilli()),
		}
		if c.kind == "server_started" {
			if c.s.Core != "" {
				p.Chips = append(p.Chips, c.s.Core)
			}
			if c.s.P2PAddr != "" {
				p.Ref = c.s.P2PAddr
				p.Chips = append(p.Chips, "P2P")
			}
		} else if c.s.State != "stopped" {
			p.Body = "crashed" // вікно підставляє переклад «аварійно»
		}
		a.pushNotify(p)
	}
}
