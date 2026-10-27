// players.go — хто з гравців зараз на сервері + події «приєднався/вийшов».
//
// Навіщо окремий реєстр, якщо вже є playerCount: раніше UI знав лише
// ЧИСЛО гравців (з пінгу/логів). Не було ні списку онлайн-гравців, ні
// сповіщень «Steve приєднався». Це той самий клас задачі, що зроблено для
// сервера: читаємо рядки логу та ведемо реєстр.
//
// ── ЧОМУ ПАРСИМО ЛОГ, А НЕ ЗАПИТУЄМО СЕРВЕР ─────────────────────────────
// Список гравців можна було б брати з status-ping (players.sample), але
// ванільний сервер віддає sample лише коли гравців небагато, а деякі ядра
// (і modи на кшталт hide-online-players) віддають порожній список імен.
// Лог же завжди містить «X joined the game» / «X left the game» і його
// пишуть ВСІ ядра: vanilla, Paper, Purpur, Forge, Fabric, Velocity.
//
// ── ЩО САМЕ ЛОВИМО ──────────────────────────────────────────────────────
//
//	· <нік> joined the game              — ваніль/Paper/Purpur/Forge/Fabric
//	· <нік> left the game                — те саме
//	· <нік> lost connection: <причина>   — кік/обрив (теж «вийшов», з причиною)
//	· [connected player] <нік> (…) has connected    — Velocity-проксі
//	· [connected player] <нік> (…) has disconnected — Velocity-проксі
//	· There are N of a max of M players online: a, b, c — /list: ресинк
//	  повного списку (напр. після перезапуску лаунчера, коли ми не бачили
//	  момент входу і реєстр порожній, а гравці вже на сервері).
//
// Ніки обмежені 1-16 символами [A-Za-z0-9_.] — це офіційні правила
// Minecraft (якщо сервер не оф-лайн з кастомними ніками, але тоді нік
// просто не розпізнається — гірше не стає).
package servers

import (
	"regexp"
	"strings"
	"sync"
	"time"
)

// PlayerPresence — один гравець онлайн.
type PlayerPresence struct {
	Name string `json:"name"`
	// JoinedAt — коли ми побачили вхід (unix ms). 0 — побачили лише через
	// /list-ресинк, точного часу входу немає.
	JoinedAt int64 `json:"joinedAt,omitempty"`
	// Source: "log" (бачили вхід) | "list" (відновлено з /list).
	Source string `json:"source,omitempty"`
}

// PlayerEvent — подія входу/виходу (їде фронтенду в server:player).
type PlayerEvent struct {
	Name   string `json:"name"`
	Action string `json:"action"` // join | leave
	At     int64  `json:"at"`     // unix ms
	Count  int    `json:"count"`  // скільки гравців онлайн ПІСЛЯ події
	// Reason — причина виходу, якщо її дав сервер (lost connection: …).
	Reason string `json:"reason,omitempty"`
	// List — повний список онлайн-ніків після події (фронтенд не має сам
	// збирати стан: пропущена подія = розбіжність списку з реальністю).
	List []string `json:"list"`
}

// ── Регулярки ──────────────────────────────────────────────────────────

const nickPat = `([A-Za-z0-9_.]{1,16})`

var (
	reJoinedGame = regexp.MustCompile(`\b` + nickPat + ` joined the game\b`)
	reLeftGame   = regexp.MustCompile(`\b` + nickPat + ` left the game\b`)
	reLostConn   = regexp.MustCompile(`\b` + nickPat + ` lost connection: (.*)$`)
	// Velocity: "[connected player] Steve (/1.2.3.4:5678) has connected".
	reVelocityPlayer = regexp.MustCompile(`\[connected player\] ` + nickPat + ` `)
	// /list: "There are 2 of a max of 20 players online: Steve, Alex".
	reListNames = regexp.MustCompile(`players online:?\s*(.*)$`)
)

// ── Реєстр онлайн-гравців ──────────────────────────────────────────────

type playerRegistry struct {
	mu sync.RWMutex
	// byServer: serverID → нік → присутність.
	byServer map[string]map[string]*PlayerPresence
	// order: serverID → порядок ніків (щоб список не «стрибав» у UI).
	order map[string][]string
}

var globalPlayers = &playerRegistry{
	byServer: make(map[string]map[string]*PlayerPresence),
	order:    make(map[string][]string),
}

// ClearOnlinePlayers скидає реєстр сервера. Викликається при зупинці
// процесу: після виходу всі гравці вже не онлайн, а без скидання список
// показував би «залишки» до наступного /list.
func ClearOnlinePlayers(serverID string) {
	globalPlayers.mu.Lock()
	delete(globalPlayers.byServer, serverID)
	delete(globalPlayers.order, serverID)
	globalPlayers.mu.Unlock()
}

// ListOnlinePlayers повертає онлайн-гравців у порядку входу.
func ListOnlinePlayers(serverID string) []PlayerPresence {
	globalPlayers.mu.RLock()
	defer globalPlayers.mu.RUnlock()
	names := globalPlayers.order[serverID]
	out := make([]PlayerPresence, 0, len(names))
	for _, n := range names {
		if p, ok := globalPlayers.byServer[serverID][n]; ok && p != nil {
			out = append(out, *p)
		}
	}
	return out
}

// OnlinePlayerNames — те саме, але лише ніки.
func OnlinePlayerNames(serverID string) []string {
	globalPlayers.mu.RLock()
	defer globalPlayers.mu.RUnlock()
	names := globalPlayers.order[serverID]
	out := make([]string, len(names))
	copy(out, names)
	return out
}

func addPlayer(serverID, name string, source string) int {
	globalPlayers.mu.Lock()
	defer globalPlayers.mu.Unlock()
	if globalPlayers.byServer[serverID] == nil {
		globalPlayers.byServer[serverID] = make(map[string]*PlayerPresence)
	}
	joined := int64(0)
	if source == "log" {
		joined = time.Now().UnixMilli()
	}
	if _, exists := globalPlayers.byServer[serverID][name]; !exists {
		globalPlayers.order[serverID] = append(globalPlayers.order[serverID], name)
	} else if joined == 0 {
		// Уже був у списку (напр. після ресинку) — не перетираємо час входу.
		joined = globalPlayers.byServer[serverID][name].JoinedAt
	}
	globalPlayers.byServer[serverID][name] = &PlayerPresence{Name: name, JoinedAt: joined, Source: source}
	return len(globalPlayers.order[serverID])
}

func removePlayer(serverID, name string) (bool, int) {
	globalPlayers.mu.Lock()
	defer globalPlayers.mu.Unlock()
	set := globalPlayers.byServer[serverID]
	if set == nil {
		return false, 0
	}
	if _, ok := set[name]; !ok {
		return false, len(globalPlayers.order[serverID])
	}
	delete(set, name)
	names := globalPlayers.order[serverID]
	for i, n := range names {
		if n == name {
			globalPlayers.order[serverID] = append(names[:i:i], names[i+1:]...)
			break
		}
	}
	return true, len(globalPlayers.order[serverID])
}

// resyncPlayers приводить реєстр до списку з /list: додає тих, кого ми
// пропустили, і прибирає тих, кого вже немає. Використовується, коли
// лаунчер приєднався до вже запущеного сервера.
func resyncPlayers(serverID string, names []string) {
	want := make(map[string]bool, len(names))
	for _, n := range names {
		want[n] = true
	}
	globalPlayers.mu.Lock()
	existing := make(map[string]bool, len(globalPlayers.order[serverID]))
	for _, n := range globalPlayers.order[serverID] {
		existing[n] = true
	}
	globalPlayers.mu.Unlock()

	for _, n := range names {
		if !existing[n] {
			addPlayer(serverID, n, "list")
		}
	}
	for n := range existing {
		if !want[n] {
			removePlayer(serverID, n)
		}
	}
}

// parsePlayerNames розбирає хвіст рядка /list («Steve, Alex») у ніки.
func parsePlayerNames(raw string) []string {
	raw = strings.TrimSpace(raw)
	raw = strings.Trim(raw, "[]")
	if raw == "" || raw == "[]" {
		return nil
	}
	out := make([]string, 0, 4)
	for _, part := range strings.Split(raw, ",") {
		name := strings.TrimSpace(part)
		// Іноді ядра дописують у дужках ping/статус — лишаємо тільки нік.
		if i := strings.IndexAny(name, " ([("); i > 0 {
			name = name[:i]
		}
		if name != "" {
			out = append(out, name)
		}
	}
	return out
}

// UpdatePlayersFromLog аналізує рядок логу сервера та, якщо це вхід/вихід
// гравця, повертає подію (name/action/reason) і count ПІСЛЯ неї.
//
// Порядок перевірок важливий: «lost connection» і «left the game» — обидва
// вихід, і вони не перетинаються з «joined the game».
func UpdatePlayersFromLog(serverID, line string) (PlayerEvent, bool) {
	if serverID == "" || line == "" {
		return PlayerEvent{}, false
	}

	// Вхід.
	if m := reJoinedGame.FindStringSubmatch(line); m != nil {
		n := addPlayer(serverID, m[1], "log")
		return PlayerEvent{Name: m[1], Action: "join", At: time.Now().UnixMilli(), Count: n, List: OnlinePlayerNames(serverID)}, true
	}
	if strings.Contains(line, "[connected player]") {
		if m := reVelocityPlayer.FindStringSubmatch(line); m != nil {
			if strings.Contains(line, "has disconnected") {
				if ok, n := removePlayer(serverID, m[1]); ok {
					return PlayerEvent{Name: m[1], Action: "leave", At: time.Now().UnixMilli(), Count: n, List: OnlinePlayerNames(serverID)}, true
				}
				return PlayerEvent{}, false
			}
			if strings.Contains(line, "has connected") {
				n := addPlayer(serverID, m[1], "log")
				return PlayerEvent{Name: m[1], Action: "join", At: time.Now().UnixMilli(), Count: n, List: OnlinePlayerNames(serverID)}, true
			}
		}
	}

	// Вихід.
	if m := reLeftGame.FindStringSubmatch(line); m != nil {
		if ok, n := removePlayer(serverID, m[1]); ok {
			return PlayerEvent{Name: m[1], Action: "leave", At: time.Now().UnixMilli(), Count: n, List: OnlinePlayerNames(serverID)}, true
		}
		return PlayerEvent{}, false
	}
	if m := reLostConn.FindStringSubmatch(line); m != nil {
		reason := strings.TrimSpace(m[2])
		// Голий «lost connection: Disconnected» — звичайний вихід, причину
		// в UI показувати немає сенсу.
		if strings.EqualFold(reason, "Disconnected") {
			reason = ""
		}
		if ok, n := removePlayer(serverID, m[1]); ok {
			return PlayerEvent{Name: m[1], Action: "leave", At: time.Now().UnixMilli(), Count: n, Reason: reason, List: OnlinePlayerNames(serverID)}, true
		}
		return PlayerEvent{}, false
	}

	// Ресинк зі /list. Подію НЕ шлемо (це не вхід/вихід конкретного гравця,
	// а звірка стану) — інакше на кожен /list UI показував би купу сповіщень.
	if m := reListNames.FindStringSubmatch(line); m != nil {
		names := parsePlayerNames(m[1])
		resyncPlayers(serverID, names)
		return PlayerEvent{}, false
	}

	return PlayerEvent{}, false
}
