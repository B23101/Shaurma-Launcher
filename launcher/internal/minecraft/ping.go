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
	Error      string `json:"error,omitempty"`
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

// parseMotd розбирає JSON description: рядок, об'єкт {text} або список extra.
func parseMotd(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return cleanMotd(s)
	}
	var obj struct {
		Text  string            `json:"text"`
		Extra []json.RawMessage `json:"extra"`
	}
	if json.Unmarshal(raw, &obj) != nil {
		return ""
	}
	out := obj.Text
	for _, e := range obj.Extra {
		var es string
		if json.Unmarshal(e, &es) == nil {
			out += es
			continue
		}
		var eo struct {
			Text string `json:"text"`
		}
		if json.Unmarshal(e, &eo) == nil {
			out += eo.Text
		}
	}
	return cleanMotd(out)
}

func cleanMotd(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(s, "§", ""))
}
