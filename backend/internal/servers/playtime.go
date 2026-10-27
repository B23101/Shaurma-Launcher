// playtime.go — «скільки кожен гравець загалом награв на сервері».
//
// Реєстр players.go знає лише тих, хто онлайн ЗАРАЗ (і час поточного
// сеансу). Накопичений час за весь період ванільний сервер сам веде у
// <світ>/stats/<uuid>.json — це те саме, що показує F3+Esc → «Статистика →
// Час гри». Тут ми лише читаємо ці файли, нічого не пишемо і не
// відстежуємо самі, тому працює і для гравців, що були ДО появи лаунчера.
//
// ── ФОРМАТИ ─────────────────────────────────────────────────────────────
//
//	· 1.17+     {"stats":{"minecraft:custom":{"minecraft:play_time": N}}}
//	· 1.13–1.16 {"stats":{"minecraft:custom":{"minecraft:play_one_minute": N}}}
//	            (назва оманлива — це теж ТІКИ гри, 20 тік = 1 с)
//	· до 1.13   {"stat.playOneMinute": N}
//
// ── ЩО НЕ ВРАХОВАНО ─────────────────────────────────────────────────────
// Сервер скидає stats на диск не миттєво, а при виході гравця та
// автозбереженні світу. Тому для гравця, що онлайн, значення тут може
// відставати від реальності на частину поточного сеансу — UI показує
// сеанс окремо (з реєстру онлайн-гравців). Проксі (Velocity) світів не має:
// там список буде порожнім — це нормально.
package servers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// PlayerPlaytime — накопичений час одного гравця на сервері.
type PlayerPlaytime struct {
	Name string `json:"name"`
	UUID string `json:"uuid"`
	// Seconds — загальний час гри (з stats світу), у секундах.
	Seconds int64 `json:"seconds"`
	// LastSeen — коли сервер востаннє оновив статистику гравця (unix ms).
	// Це момент виходу/автозбереження, а не точний «востаннє онлайн».
	LastSeen int64 `json:"lastSeen,omitempty"`
	// Online / SessionStart — заповнює API з реєстру онлайн-гравців.
	Online       bool  `json:"online,omitempty"`
	SessionStart int64 `json:"sessionStart,omitempty"`
}

const ticksPerSecond = 20

// readLevelName повертає level-name із server.properties (за замовч. "world").
func readLevelName(serverDir string) string {
	levelName := "world"
	data, err := os.ReadFile(filepath.Join(serverDir, "server.properties"))
	if err != nil {
		return levelName
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "level-name=") {
			if v := strings.TrimSpace(strings.TrimPrefix(line, "level-name=")); v != "" {
				levelName = v
			}
			break
		}
	}
	return levelName
}

// playtimeTicks дістає тіки гри з вмісту stats-файлу (див. формати вгорі).
// Повертає максимум з усіх відомих ключів — на випадок, якщо файл мігрував
// між версіями гри і містить обидва.
func playtimeTicks(data []byte) int64 {
	var raw struct {
		Stats map[string]map[string]int64 `json:"stats"`
	}
	var best int64
	if json.Unmarshal(data, &raw) == nil {
		custom := raw.Stats["minecraft:custom"]
		for _, key := range []string{"minecraft:play_time", "minecraft:play_one_minute"} {
			if v := custom[key]; v > best {
				best = v
			}
		}
	}
	// Старий плоский формат: тип значення інший, тож парсимо окремо.
	var legacy map[string]json.RawMessage
	if json.Unmarshal(data, &legacy) == nil {
		if rawV, ok := legacy["stat.playOneMinute"]; ok {
			var v int64
			if json.Unmarshal(rawV, &v) == nil && v > best {
				best = v
			}
		}
	}
	return best
}

// loadUserNames будує мапу uuid(без дефісів, нижній регістр) → нік із
// usercache.json, whitelist.json та ops.json. Перший знайдений нік
// лишається (usercache свіжіший за решту, тому читається першим).
func loadUserNames(serverDir string) map[string]string {
	out := make(map[string]string)
	for _, f := range []string{"usercache.json", "whitelist.json", "ops.json"} {
		data, err := os.ReadFile(filepath.Join(serverDir, f))
		if err != nil {
			continue
		}
		var list []struct {
			Name string `json:"name"`
			UUID string `json:"uuid"`
		}
		if json.Unmarshal(data, &list) != nil {
			continue
		}
		for _, e := range list {
			id := normUUID(e.UUID)
			if id == "" || e.Name == "" {
				continue
			}
			if _, ok := out[id]; !ok {
				out[id] = e.Name
			}
		}
	}
	return out
}

func normUUID(s string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), "-", ""))
}

// ReadPlayerPlaytimes читає stats-файли основного світу сервера й повертає
// гравців, відсортованих за часом гри (найбільше — першим). Нульовий час
// пропускається (гравець лише «зайшов і вийшов» до першого збереження).
func ReadPlayerPlaytimes(serverDir string) []PlayerPlaytime {
	levelName := readLevelName(serverDir)
	// level-name — ввід користувача: не даємо вийти за межі теки сервера.
	statsDir := filepath.Join(serverDir, filepath.Clean("/"+levelName), "stats")
	entries, err := os.ReadDir(statsDir)
	if err != nil {
		return []PlayerPlaytime{}
	}
	names := loadUserNames(serverDir)

	out := make([]PlayerPlaytime, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
			continue
		}
		uuid := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		path := filepath.Join(statsDir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		secs := playtimeTicks(data) / ticksPerSecond
		if secs <= 0 {
			continue
		}
		name := names[normUUID(uuid)]
		if name == "" {
			// Ніка немає в кешах (кеш протухає) — показуємо хоч початок UUID.
			name = uuid
			if len(name) > 8 {
				name = name[:8] + "…"
			}
		}
		var last int64
		if fi, err := e.Info(); err == nil {
			last = fi.ModTime().UnixMilli()
		}
		out = append(out, PlayerPlaytime{Name: name, UUID: uuid, Seconds: secs, LastSeen: last})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Seconds != out[j].Seconds {
			return out[i].Seconds > out[j].Seconds
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

// mergeOnline додає до списку позначку «онлайн + початок сеансу» і гравців,
// які онлайн, але ще не мають stats-файлу (щойно вперше зайшли).
func mergeOnline(list []PlayerPlaytime, online []PlayerPresence) []PlayerPlaytime {
	idx := make(map[string]int, len(list))
	for i, p := range list {
		idx[strings.ToLower(p.Name)] = i
	}
	for _, o := range online {
		if i, ok := idx[strings.ToLower(o.Name)]; ok {
			list[i].Online = true
			list[i].SessionStart = o.JoinedAt
			continue
		}
		list = append(list, PlayerPlaytime{Name: o.Name, Online: true, SessionStart: o.JoinedAt})
	}
	return list
}

// PlayerPlaytimes — накопичений час гри кожного гравця сервера
// (stats світу + позначка «зараз онлайн» з реєстру players.go).
func (a *API) PlayerPlaytimes(serverID string) ([]PlayerPlaytime, error) {
	srv, err := a.store.Get(serverID)
	if err != nil {
		return nil, err
	}
	return mergeOnline(ReadPlayerPlaytimes(srv.Dir), ListOnlinePlayers(serverID)), nil
}
