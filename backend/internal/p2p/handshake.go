// handshake.go — розбір Minecraft Handshake-пакета (перший пакет з'єднання).
//
// Гість вводить у Multiplayer "ABCD.play.shaurma-launcher.org" — SRV-запис
// напрямляє гру на 127.0.0.1:45678 (наш локальний listener), а сам домен
// (з кодом кімнати всередині) приходить у полі "Server Address" хендшейку.
// Ми читаємо його БЕЗ модифікації підключення до реального сервера: пакет
// зчитується один раз у буфер і потім переграється (replay) у tsnet-тунель
// так, ніби гра сама писала прямо туди — гра про підміну нічого не знає.
package p2p

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
)

// ErrNotHandshake — перший пакет з'єднання не є валідним Minecraft
// Handshake (id != 0x00) або даних недостатньо, щоб його розібрати.
var ErrNotHandshake = errors.New("p2p: перший пакет не є Minecraft handshake")

// maxHandshakeFrame — верхня межа розміру самого хендшейк-фрейму. Реальний
// хендшейк — це кілька байт + короткий домен, тому будь-що більше за це
// свідчить про мотлох на порту (сканер/не-Minecraft клієнт), а не про
// легітимну гру — приймати й буферизувати такий "пакет" немає сенсу.
const maxHandshakeFrame = 2048

// maxServerAddressLen — Minecraft-протокол сам обмежує Server Address
// 255 UTF-16 code units; ми беремо трохи з запасом по байтах UTF-8.
const maxServerAddressLen = 512

// Handshake — розібраний перший пакет з'єднання.
type Handshake struct {
	ProtocolVersion int64
	ServerAddress   string // домен, який ввів гравець у Direct Connect
	ServerPort      uint16
	NextState       int64 // 1 = status, 2 = login

	// Raw — точні байти всього фрейму (довжина-префікс + payload), як їх
	// надіслала гра. Використовується для replay: записуємо RAW як є в
	// tsnet-з'єднання до хоста, щоб реальний Minecraft-сервер отримав
	// БІТ У БІТ те саме, що надіслав клієнт (включно з полями, які ми
	// самі не розбираємо і не повинні змінювати — Protocol Version і
	// NextState гра формує сама, підміняти їх не наша справа).
	Raw []byte
}

// Address — повна P2P-адреса, витягнута з ServerAddress вигляду
// "prlool-482917.play.shaurma-launcher.org" (регістронезалежно, з
// опційним портом ":25565" в кінці, який гра іноді додає в SRV-less
// клієнтах). Ця адреса — це і код, і "PIN" одночасно (власник ділиться
// нею повністю, разом із випадковим цифровим суфіксом).
//
// Повертає адресу в нижньому регістрі (той самий формат, що зберігає
// Worker) і помилку, якщо домен не відповідає очікуваному шаблону
// "<адреса>.play.<suffix>".
func (h Handshake) Address(playSuffix string) (string, error) {
	addr := h.ServerAddress
	// Forge/NeoForge (FML) дописують у це саме поле службовий хвіст через
	// нуль-байти: "example.com\x00FML3\x00" (а старіші версії — навіть
	// "\x00FML\x00<host:port гейтвея>"), щоб сервер міг розпізнати
	// модований клієнт ще до рукостискання. Ванільний клієнт \x00 сюди
	// ніколи не пише, тож відрізаємо все від першого нуль-байта —
	// інакше валідний домен від гравця на модпаку (Forge/NeoForge)
	// не проходив би перевірку суфікса і P2P ламався б лише для модпаків.
	if i := strings.IndexByte(addr, 0); i >= 0 {
		addr = addr[:i]
	}
	addr = strings.ToLower(strings.TrimSpace(addr))
	// Гра іноді лишає "host:port" у самому Server Address навіть коли SRV
	// вже спрямував з'єднання за портом зі SRV-запису — про всяк випадок
	// відрізаємо хвіст після останньої крапки з двокрапкою.
	if i := strings.LastIndex(addr, ":"); i > 0 && !strings.Contains(addr[i:], ".") {
		addr = addr[:i]
	}
	suffix := "." + strings.ToLower(strings.TrimPrefix(playSuffix, "."))
	if !strings.HasSuffix(addr, suffix) {
		return "", fmt.Errorf("p2p: адреса %q не належить до %s", h.ServerAddress, playSuffix)
	}
	subdomain := strings.TrimSuffix(addr, suffix)
	if subdomain == "" || strings.Contains(subdomain, ".") {
		return "", fmt.Errorf("p2p: не вдалося витягти адресу з %q", h.ServerAddress)
	}
	return subdomain, nil
}

// ReadHandshake читає перший пакет із з'єднання і розбирає його як
// Minecraft Handshake. Використовує bufio.Reader, тому виклик коду має
// далі продовжувати читати/писати той самий *bufio.Reader (а не сирий
// net.Conn), інакше буферизовані-але-непрочитані байти загубляться.
func ReadHandshake(r *bufio.Reader) (Handshake, error) {
	frameLen, lenBytes, err := readVarintCounted(r)
	if err != nil {
		return Handshake{}, fmt.Errorf("p2p: читання довжини пакета: %w", err)
	}
	if frameLen <= 0 || frameLen > maxHandshakeFrame {
		return Handshake{}, ErrNotHandshake
	}

	body := make([]byte, frameLen)
	if _, err := io.ReadFull(r, body); err != nil {
		return Handshake{}, fmt.Errorf("p2p: читання тіла пакета: %w", err)
	}

	raw := make([]byte, 0, len(lenBytes)+len(body))
	raw = append(raw, lenBytes...)
	raw = append(raw, body...)

	br := bufio.NewReader(newByteReader(body))
	packetID, err := readVarint(br)
	if err != nil || packetID != 0x00 {
		return Handshake{}, ErrNotHandshake
	}
	protocolVersion, err := readVarint(br)
	if err != nil {
		return Handshake{}, ErrNotHandshake
	}
	addrLen, err := readVarint(br)
	if err != nil || addrLen < 0 || addrLen > maxServerAddressLen {
		return Handshake{}, ErrNotHandshake
	}
	addrBytes := make([]byte, addrLen)
	if _, err := io.ReadFull(br, addrBytes); err != nil {
		return Handshake{}, ErrNotHandshake
	}
	var portBuf [2]byte
	if _, err := io.ReadFull(br, portBuf[:]); err != nil {
		return Handshake{}, ErrNotHandshake
	}
	nextState, err := readVarint(br)
	if err != nil || (nextState != 1 && nextState != 2) {
		return Handshake{}, ErrNotHandshake
	}

	return Handshake{
		ProtocolVersion: protocolVersion,
		ServerAddress:   string(addrBytes),
		ServerPort:      binary.BigEndian.Uint16(portBuf[:]),
		NextState:       nextState,
		Raw:             raw,
	}, nil
}

// ── VarInt-кодек (Minecraft-протокол: знаковий int32, two's complement) ──
// Дублює логіку internal/minecraft/ping.go навмисно: той пакет кодує
// хендшейк для вихідного ping-запиту, цей — розбирає вхідний хендшейк від
// гри; спільної залежності між ними немає сенсу городити заради 15 рядків.

func readVarint(r *bufio.Reader) (int64, error) {
	var val uint32
	for i := 0; i < 5; i++ {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		val |= uint32(b&0x7F) << (7 * i)
		if b&0x80 == 0 {
			return int64(int32(val)), nil
		}
	}
	return 0, fmt.Errorf("p2p: varint завеликий")
}

// readVarintCounted — те саме, що readVarint, але додатково повертає сирі
// байти, з яких значення прочитане (потрібно для точного replay фрейму).
func readVarintCounted(r *bufio.Reader) (int64, []byte, error) {
	var val uint32
	var raw []byte
	for i := 0; i < 5; i++ {
		b, err := r.ReadByte()
		if err != nil {
			return 0, nil, err
		}
		raw = append(raw, b)
		val |= uint32(b&0x7F) << (7 * i)
		if b&0x80 == 0 {
			return int64(int32(val)), raw, nil
		}
	}
	return 0, nil, fmt.Errorf("p2p: varint завеликий")
}

// byteReader — мінімальна io.Reader-обгортка над []byte (щоб не тягнути
// bytes.Reader лише заради цього і мати той самий стиль, що й вище).
type byteReader struct {
	b   []byte
	pos int
}

func newByteReader(b []byte) *byteReader { return &byteReader{b: b} }

func (r *byteReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.b) {
		return 0, io.EOF
	}
	n := copy(p, r.b[r.pos:])
	r.pos += n
	return n, nil
}