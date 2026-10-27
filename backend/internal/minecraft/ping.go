package minecraft

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

// ── Перевірка доступності Minecraft-сервера (status ping) ───────────────
// Сучасний протокол (1.7+): TCP-з'єднання → handshake (next state = 1) →
// status request → JSON-відповідь з version/players/description. Без
// зовнішніх залежностей; за таймаут/помилку сервер вважається офлайн.

// ServerPing — результат перевірки сервера.
type ServerPing struct {
	Online     bool   `json:"online"`
	Version    string `json:"version,omitempty"`
	Protocol   int    `json:"protocol,omitempty"`
	Players    int    `json:"players,omitempty"`
	MaxPlayers int    `json:"maxPlayers,omitempty"`
	Motd       string `json:"motd,omitempty"`
	// Favicon — data URL іконки сервера ("data:image/png;base64,...."),
	// як її віддає сам сервер у status-відповіді. Порожньо, якщо сервер
	// іконку не задав — фронтенд тоді показує іконку зі servers.dat (якщо
	// збережена раніше) або дефолтну заглушку.
	Favicon string `json:"favicon,omitempty"`
	Error   string `json:"error,omitempty"`
}

// PingMinecraftServer перевіряє сервер за адресою (host або host:port,
// порт за замовчуванням 25565) протягом заданого таймауту.
func PingMinecraftServer(address string, timeout time.Duration) ServerPing {
	host, port := splitAddress(address)
	if _, err := strconv.Atoi(port); err != nil {
		return ServerPing{Online: false, Error: "невірний порт"}
	}
	if host == "" {
		return ServerPing{Online: false, Error: "порожня адреса"}
	}

	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), timeout)
	if err != nil {
		return ServerPing{Online: false, Error: "сервер недоступний"}
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))

	// Handshake пакет: id=0, protocol=-1 (сучасні сервери приймають будь-який),
	// host, port, next state=1 (status).
	p := int64(0)
	payload := appendVarint(nil, -1)
	payload = appendVarint(payload, int64(len(host)))
	payload = append(payload, host...)
	pp, _ := strconv.Atoi(port)
	payload = binary.BigEndian.AppendUint16(payload, uint16(pp))
	payload = appendVarint(payload, 1)

	pkt := appendVarint(nil, p)
	pkt = append(pkt, payload...)
	frame := appendVarint(nil, int64(len(pkt)))
	frame = append(frame, pkt...)
	if _, err := conn.Write(frame); err != nil {
		return ServerPing{Online: false, Error: "помилка запиту"}
	}
	// Status request: порожній пакет id=0.
	if _, err := conn.Write([]byte{0x01, 0x00}); err != nil {
		return ServerPing{Online: false, Error: "помилка запиту"}
	}

	reader := bufio.NewReader(conn)
	if _, err := readVarint(reader); err != nil { // довжина пакета
		return ServerPing{Online: false, Error: "некоректна відповідь"}
	}
	if _, err := readVarint(reader); err != nil { // id пакета
		return ServerPing{Online: false, Error: "некоректна відповідь"}
	}
	jsonLen, err := readVarint(reader)
	if err != nil || jsonLen <= 0 || jsonLen > 1<<20 {
		return ServerPing{Online: false, Error: "некоректна відповідь"}
	}
	buf := make([]byte, jsonLen)
	if _, err := io.ReadFull(reader, buf); err != nil {
		return ServerPing{Online: false, Error: "некоректна відповідь"}
	}

	var resp struct {
		Version struct {
			Name     string `json:"name"`
			Protocol int    `json:"protocol"`
		} `json:"version"`
		Players struct {
			Max    int `json:"max"`
			Online int `json:"online"`
		} `json:"players"`
		Description json.RawMessage `json:"description"`
		Favicon     string          `json:"favicon"`
	}
	if err := json.Unmarshal(buf, &resp); err != nil {
		return ServerPing{Online: false, Error: "некоректна відповідь"}
	}
	return ServerPing{
		Online:     true,
		Version:    resp.Version.Name,
		Protocol:   resp.Version.Protocol,
		Players:    resp.Players.Online,
		MaxPlayers: resp.Players.Max,
		Motd:       parseMotd(resp.Description),
		Favicon:    resp.Favicon,
	}
}

// splitAddress розбирає "host:port" / "host" / IPv6 у (host, port).
func splitAddress(address string) (string, string) {
	address = strings.TrimSpace(address)
	if h, p, err := net.SplitHostPort(address); err == nil {
		return h, p
	}
	if strings.Count(address, ":") == 1 {
		if i := strings.LastIndex(address, ":"); i > 0 {
			return address[:i], address[i+1:]
		}
	}
	return address, "25565"
}

// appendVarint кодує int64 у varint (протокол Minecraft).
func appendVarint(b []byte, v int64) []byte {
	u := uint32(v)
	for u >= 0x80 {
		b = append(b, byte(u)|0x80)
		u >>= 7
	}
	return append(b, byte(u))
}

func readVarint(r *bufio.Reader) (int64, error) {
	var val uint32
	for i := 0; i < 5; i++ {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		val |= uint32(b&0x7F) << (7 * i)
		if b&0x80 == 0 {
			// Minecraft varint — знаковий int32 (two's complement), тому
			// -1 (protocol version) розкодовується назад у -1.
			return int64(int32(val)), nil
		}
	}
	return 0, fmt.Errorf("varint завеликий")
}

// motdColorCodes — офіційні § -коди Minecraft для іменованих кольорів у
// JSON chat-компонентах ("color": "gold" тощо). Це стандартний набір
// з 16 кольорів гри; будь-який колір, якого тут нема (напр. hex
// "#RRGGBB" з 1.16+), лишаємо як є — фронтенд вміє малювати і сирий §-код,
// і hex через параметр --style, див. motdSegments нижче.
var motdColorCodes = map[string]string{
	"black": "§0", "dark_blue": "§1", "dark_green": "§2", "dark_aqua": "§3",
	"dark_red": "§4", "dark_purple": "§5", "gold": "§6", "gray": "§7",
	"dark_gray": "§8", "blue": "§9", "green": "§a", "aqua": "§b",
	"red": "§c", "light_purple": "§d", "yellow": "§e", "white": "§f",
}

// motdChatComponent — один JSON chat-компонент MOTD (сервер може прислати
// або рядок, або такий об'єкт, або масив таких об'єктів у "extra").
type motdChatComponent struct {
	Text          string            `json:"text"`
	Color         string            `json:"color"`
	Bold          bool              `json:"bold"`
	Italic        bool              `json:"italic"`
	Underlined    bool              `json:"underlined"`
	Strikethrough bool              `json:"strikethrough"`
	Obfuscated    bool              `json:"obfuscated"`
	Extra         []json.RawMessage `json:"extra"`
}

// parseMotd розбирає JSON description сервера (рядок, об'єкт {text,color,
// bold,...} або {text, extra:[...]}) у рядок з §-кодами кольору/стилю —
// той самий формат, який гра сама використовує в meню Multiplayer.
//
// РАНІШЕ тут усі §-коди вирізались (cleanMotd відрізав "§" повністю), а
// колір/жирність/курсив з JSON-полів color/bold/italic узагалі не
// читались — бралось лише поле text. Тобто інформація про кольори
// губилась ще на бекенді, ДО того, як MOTD доходив до UI — навіть
// ідеальний рендерер §-кодів на фронтенді нічого не міг намалювати,
// бо самих кодів у рядку вже не було. Тепер колір/стиль з JSON
// конвертується в §-коди (§-параграф-символ + код), а фронтенд
// (motdSegments у Svelte-компонентах) розбирає їх назад у span-и з
// кольором/жирністю/курсивом/підкресленням для відображення.
func parseMotd(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimRight(s, " \t\r\n")
	}
	var comp motdChatComponent
	if err := json.Unmarshal(raw, &comp); err != nil {
		return ""
	}
	var b strings.Builder
	writeMotdComponent(&b, comp)
	return strings.TrimRight(b.String(), " \t\r\n")
}

// writeMotdComponent серіалізує один chat-компонент (і рекурсивно його
// extra) у §-кодований текст: стильові §-коди йдуть ПЕРЕД текстом
// компонента, як і в оригінальному форматі гри, щоб парсер на фронтенді
// міг читати рядок зліва направо без зазирання наперед.
func writeMotdComponent(b *strings.Builder, c motdChatComponent) {
	if c.Color != "" {
		if code, ok := motdColorCodes[c.Color]; ok {
			b.WriteString(code)
		} else if strings.HasPrefix(c.Color, "#") {
			// Hex-колір (1.16+, напр. кастомний градієнт) — передаємо як
			// є у спеціальному §#RRGGBB маркері, фронтенд його розпізнає
			// окремо від класичних однознакових §-кодів.
			b.WriteString("§" + c.Color)
		}
	}
	if c.Bold {
		b.WriteString("§l")
	}
	if c.Italic {
		b.WriteString("§o")
	}
	if c.Underlined {
		b.WriteString("§n")
	}
	if c.Strikethrough {
		b.WriteString("§m")
	}
	if c.Obfuscated {
		b.WriteString("§k")
	}
	b.WriteString(c.Text)
	for _, e := range c.Extra {
		var es string
		if json.Unmarshal(e, &es) == nil {
			b.WriteString(es)
			continue
		}
		var ec motdChatComponent
		if json.Unmarshal(e, &ec) == nil {
			writeMotdComponent(b, ec)
		}
	}
}
