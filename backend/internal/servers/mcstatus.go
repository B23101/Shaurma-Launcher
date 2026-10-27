// mcstatus.go — реальний Minecraft status-ping (той самий запит, що робить
// клієнт у списку серверів): handshake (state=status) → status request →
// JSON з players.online / players.max.
//
// Навіщо: раніше кількість гравців бралась ЛИШЕ з парсингу логів
// ("There are X of a max of Y players online"), а такі рядки зʼявляються
// непередбачувано (лише коли хтось заходить/виходить або викликаєш /list),
// тож лічильник застрягав на 0. Пінг — надійний спосіб: сервер відповідає
// кожні кілька секунд точною кількістю. Працює і для звичайних серверів,
// і для проксі Velocity (які також відповідають на status-ping).
package servers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"time"
)

// MCStatus — результат статус-пінгу.
type MCStatus struct {
	Online        bool // сервер відповів на запит
	PlayersOnline int  // players.online
	MaxPlayers    int  // players.max (якщо є в JSON)
}

// PingMCStatus підключається до 127.0.0.1:port і виконує мінімальний
// status-ping. Помилка означає, що сервер ще не слухає порт (стартує) або
// вже не слухає — у цьому разі стан Online=false.
func PingMCStatus(port int, timeout time.Duration) (MCStatus, error) {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), timeout)
	if err != nil {
		return MCStatus{}, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))

	// handshake → state=status. Версію протоколу сервери ігнорують для
	// status-запиту (та сама схема, що в будь-якому мінімальному пінгері).
	var body bytes.Buffer
	body.WriteByte(0x00) // packet id: handshake
	statusWriteVarInt(&body, 733)
	statusWriteVarInt(&body, len("127.0.0.1"))
	body.WriteString("127.0.0.1")
	body.WriteByte(byte(port >> 8))
	body.WriteByte(byte(port & 0xFF))
	body.WriteByte(0x01) // next state: status

	var frame bytes.Buffer
	statusWriteVarInt(&frame, body.Len())
	frame.Write(body.Bytes())
	statusWriteVarInt(&frame, 1) // status request: довжина 1
	frame.WriteByte(0x00)

	if _, err := conn.Write(frame.Bytes()); err != nil {
		return MCStatus{}, err
	}
	if _, err := statusReadVarInt(conn); err != nil { // довжина пакета
		return MCStatus{}, err
	}
	pid, err := statusReadVarInt(conn)
	if err != nil {
		return MCStatus{}, err
	}
	if pid != 0 {
		return MCStatus{}, fmt.Errorf("несподіваний packet id %d", pid)
	}
	jsonLen, err := statusReadVarInt(conn)
	if err != nil {
		return MCStatus{}, err
	}
	if jsonLen <= 0 || jsonLen > 1<<20 {
		return MCStatus{}, fmt.Errorf("некоректна довжина JSON: %d", jsonLen)
	}
	data := make([]byte, jsonLen)
	if _, err := io.ReadFull(conn, data); err != nil {
		return MCStatus{}, err
	}

	var payload struct {
		Players *struct {
			Online int `json:"online"`
			Max    int `json:"max"`
		} `json:"players"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		// Сервер відповів, але тіло не JSON (екзотичні проксі) — вважаємо,
		// що він онлайн, але кількість гравців невідома.
		return MCStatus{Online: true}, nil
	}
	st := MCStatus{Online: true}
	if payload.Players != nil {
		st.PlayersOnline = payload.Players.Online
		st.MaxPlayers = payload.Players.Max
	}
	return st, nil
}

// statusWriteVarInt пише varint (1-5 байт) у буфер.
func statusWriteVarInt(buf *bytes.Buffer, v int) {
	for {
		b := byte(v & 0x7F)
		v >>= 7
		if v != 0 {
			b |= 0x80
		}
		buf.WriteByte(b)
		if v == 0 {
			return
		}
	}
}

// statusReadVarInt читає varint з потоку.
func statusReadVarInt(r io.Reader) (int, error) {
	var v, shift int
	buf := make([]byte, 1)
	for {
		if _, err := io.ReadFull(r, buf); err != nil {
			return 0, err
		}
		v |= int(buf[0]&0x7F) << shift
		if buf[0]&0x80 == 0 {
			return v, nil
		}
		shift += 7
		if shift > 35 {
			return 0, fmt.Errorf("varint задовгий")
		}
	}
}
