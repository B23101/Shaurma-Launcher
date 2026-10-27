// Package remotechan — захищений прямий канал між лаунчерами друзів (для консолі/керування
// сервером поза VPS-релеєм).
//
// ЩО ГАРАНТУЄ (і чому «чужий не вставить свої правила»):
//  1. ДОПУСК ВІД VPS. Власник приймає з'єднання лише з мандатом VPS (підпис Ed25519 закріпленого
//     ключа) на операцію p2p_open: «VPS щойно перевірив, що цей друг — друг, і має право на
//     консоль цього сервера». Мандат прив'язаний до ОДНОРАЗОВОГО ключа рукостискання друга
//     (хеш), тож його не можна перехопити й використати з іншим ключем чи пізніше.
//  2. ВЗАЄМНА АВТЕНТИФІКАЦІЯ ЧЕРЕЗ VPS. Друга засвідчує сам мандат (VPS підписав «це друг X» і
//     прив'язав до його одноразового ключа; довести володіння ключем можна лише завершивши
//     обмін). Власника засвідчує ключ Ed25519, який його лаунчер зареєстрував у VPS і який VPS
//     вклав у той самий мандат: клієнт перевіряє підпис рукостискання саме цим ключем. Тож
//     підмінити будь-яку сторону в мережі неможливо, навіть перехопивши чужий мандат.
//  3. ШИФРУВАННЯ Й ЦІЛІСНІСТЬ. Ключі сесії = X25519 (одноразові ключі, тож forward secrecy),
//     зв'язані з хешем усього рукостискання. Кожен кадр —
//     XChaCha20-Poly1305 з лічильником: підробка, повтор, перестановка чи відрізання кадру
//     рве з'єднання.
//
// Пакет не знає про мережу: працює над будь-яким net.Conn (tsnet, TCP, net.Pipe у тестах).
package remotechan

import (
	"bytes"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"golang.org/x/crypto/chacha20poly1305"

	"shaurma-launcher-backend/internal/social"
)

const (
	magic            = "SHRC1"
	handshakeTimeout = 10 * time.Second
	maxHello         = 4096
	// MaxMessage — межа одного повідомлення каналу.
	MaxMessage = 32 * 1024
	// MaxLifetime — жорстка стеля життя каналу; далі треба новий мандат від VPS.
	MaxLifetime = 30 * time.Minute

	// OpP2POpen — операція мандата VPS на пряме підключення.
	OpP2POpen = "p2p_open"
)

// Помилки рукостискання. Назовні (по мережі) нічого не пояснюємо — просто рвемо з'єднання.
var (
	ErrHandshake = errors.New("remotechan: handshake failed")
	ErrMandate   = errors.New("remotechan: mandate rejected")
	ErrAuth      = errors.New("remotechan: peer authentication failed")
	ErrClosed    = errors.New("remotechan: closed")
	ErrFrame     = errors.New("remotechan: bad frame")
)

// Ephemeral — одноразовий ключ рукостискання друга. Створюється ДО запиту мандата в VPS.
type Ephemeral struct{ priv *ecdh.PrivateKey }

// NewEphemeral генерує одноразовий X25519-ключ.
func NewEphemeral() (*Ephemeral, error) {
	k, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return &Ephemeral{priv: k}, nil
}

// Hash — значення для rs_p2p_open.eph і поля CT мандата: base64(SHA-256(публічний ключ)).
func (e *Ephemeral) Hash() string { return pubHash(e.priv.PublicKey().Bytes()) }

func pubHash(pub []byte) string {
	h := sha256.Sum256(pub)
	return base64.StdEncoding.EncodeToString(h[:])
}

type hello struct {
	Friend  string               `json:"friend"`
	Srv     string               `json:"srv"`
	Eph     []byte               `json:"eph"`
	Mandate social.RemoteMandate `json:"mandate"`
}

type helloReply struct {
	Eph []byte `json:"eph"`
	Sig []byte `json:"sig"` // Ed25519 власника над хешем рукостискання
}

// DialParams — параметри друга (ініціатора).
type DialParams struct {
	Self      string               // мій accountId
	Owner     string               // accountId власника (з ним говоримо)
	ServerID  string               // сервер власника
	Mandate   social.RemoteMandate // отриманий від VPS на rs_p2p_open
	Ephemeral *Ephemeral           // той самий ключ, хеш якого вже в мандаті
	VPSKey    ed25519.PublicKey    // закріплений ключ VPS: мандат перевіряємо ТАК САМО, як власник
	Now       func() time.Time
}

// AcceptParams — параметри власника.
type AcceptParams struct {
	Self     string
	VPSKey   func() ed25519.PublicKey
	HostKey  ed25519.PrivateKey         // приватна частина ключа, зареєстрованого у VPS
	IsFriend func(friendID string) bool // актуальний список друзів лаунчера
	Now      func() time.Time
}

// Conn — встановлений захищений канал. Повідомлення (не потік): Send/Recv.
type Conn struct {
	c        net.Conn
	sendMu   sync.Mutex
	sendAEAD interface {
		Seal(dst, nonce, plaintext, additionalData []byte) []byte
	}
	sendCtr  uint64
	recvMu   sync.Mutex
	recvAEAD interface {
		Open(dst, nonce, ciphertext, additionalData []byte) ([]byte, error)
	}
	recvCtr  uint64
	deadline time.Time
}

func hm(key []byte, parts ...[]byte) []byte {
	m := hmac.New(sha256.New, key)
	for _, p := range parts {
		var l [4]byte
		binary.BigEndian.PutUint32(l[:], uint32(len(p)))
		m.Write(l[:])
		m.Write(p)
	}
	return m.Sum(nil)
}

func writeFrame(w io.Writer, b []byte) error {
	var l [4]byte
	binary.BigEndian.PutUint32(l[:], uint32(len(b)))
	_, err := w.Write(append(l[:], b...))
	return err
}

func readFrame(r io.Reader, max int) ([]byte, error) {
	var l [4]byte
	if _, err := io.ReadFull(r, l[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(l[:])
	if n == 0 || int(n) > max {
		return nil, ErrFrame
	}
	b := make([]byte, n)
	_, err := io.ReadFull(r, b)
	return b, err
}

func derive(dh, transcript []byte) (c2h, h2c []byte) {
	prk := hm([]byte("SHAURMA-P2P-KEYS-V1"), dh, transcript)
	return hm(prk, []byte("c2h")), hm(prk, []byte("h2c"))
}

func newConn(c net.Conn, sendKey, recvKey []byte, now time.Time) (*Conn, error) {
	sa, err := chacha20poly1305.NewX(sendKey)
	if err != nil {
		return nil, err
	}
	ra, err := chacha20poly1305.NewX(recvKey)
	if err != nil {
		return nil, err
	}
	return &Conn{c: c, sendAEAD: sa, recvAEAD: ra, deadline: now.Add(MaxLifetime)}, nil
}

func nonce(ctr uint64) []byte {
	n := make([]byte, chacha20poly1305.NonceSizeX)
	binary.BigEndian.PutUint64(n[16:], ctr)
	return n
}

// checkMandate — спільна перевірка мандата p2p_open (і власником, і другом).
func checkMandate(pub ed25519.PublicKey, m social.RemoteMandate, now time.Time) error {
	if pub == nil || social.VerifyMandateSig(pub, m, now) != nil || m.Op != OpP2POpen || !m.HasCap('c') {
		return ErrMandate
	}
	return nil
}

func hostSigInput(th []byte) []byte { return append([]byte("SHAURMA-P2P-HOST\n"), th...) }

// Dial виконує рукостискання з боку друга. conn — вже встановлене транспортне з'єднання до власника.
func Dial(conn net.Conn, p DialParams) (*Conn, error) {
	if p.Ephemeral == nil {
		return nil, ErrHandshake
	}
	now := time.Now
	if p.Now != nil {
		now = p.Now
	}
	m := p.Mandate
	if checkMandate(p.VPSKey, m, now()) != nil || m.Owner != p.Owner || m.Friend != p.Self ||
		m.Srv != p.ServerID || m.CT != p.Ephemeral.Hash() {
		return nil, ErrMandate
	}
	hostPub, err := base64.StdEncoding.DecodeString(m.PK)
	if err != nil || len(hostPub) != ed25519.PublicKeySize {
		return nil, ErrMandate // VPS не вказав ключ власника — власника не можна автентифікувати
	}
	_ = conn.SetDeadline(time.Now().Add(handshakeTimeout))
	defer conn.SetDeadline(time.Time{}) //nolint:errcheck
	ephPub := p.Ephemeral.priv.PublicKey().Bytes()
	hb, _ := json.Marshal(hello{Friend: p.Self, Srv: p.ServerID, Eph: ephPub, Mandate: m})
	if _, err := conn.Write([]byte(magic)); err != nil {
		return nil, err
	}
	if err := writeFrame(conn, hb); err != nil {
		return nil, err
	}
	rb, err := readFrame(conn, maxHello)
	if err != nil {
		return nil, ErrHandshake
	}
	var rep helloReply
	if json.Unmarshal(rb, &rep) != nil || len(rep.Eph) != 32 {
		return nil, ErrHandshake
	}
	th := sha256.Sum256(append(append([]byte{}, hb...), rep.Eph...))
	if !ed25519.Verify(ed25519.PublicKey(hostPub), hostSigInput(th[:]), rep.Sig) {
		return nil, ErrAuth // це не той пристрій власника, якого знає VPS
	}
	peer, err := ecdh.X25519().NewPublicKey(rep.Eph)
	if err != nil {
		return nil, ErrHandshake
	}
	dh, err := p.Ephemeral.priv.ECDH(peer)
	if err != nil {
		return nil, ErrHandshake
	}
	c2h, h2c := derive(dh, th[:])
	if err := writeFrame(conn, hm(c2h, []byte("SHAURMA-P2P-CLIENT"), th[:])); err != nil {
		return nil, err
	}
	return newConn(conn, c2h, h2c, now())
}

// Accept виконує рукостискання з боку власника. Повертає канал і accountId друга.
// Усі відмови — ErrMandate/ErrAuth/ErrHandshake; викликач просто закриває з'єднання.
func Accept(conn net.Conn, p AcceptParams) (*Conn, string, social.RemoteMandate, error) {
	now := time.Now
	if p.Now != nil {
		now = p.Now
	}
	_ = conn.SetDeadline(time.Now().Add(handshakeTimeout))
	defer conn.SetDeadline(time.Time{}) //nolint:errcheck
	var empty social.RemoteMandate
	mg := make([]byte, len(magic))
	if _, err := io.ReadFull(conn, mg); err != nil || string(mg) != magic {
		return nil, "", empty, ErrHandshake
	}
	hb, err := readFrame(conn, maxHello)
	if err != nil {
		return nil, "", empty, ErrHandshake
	}
	var h hello
	if json.Unmarshal(hb, &h) != nil || len(h.Eph) != 32 || len(p.HostKey) != ed25519.PrivateKeySize {
		return nil, "", empty, ErrHandshake
	}
	// 1. мандат VPS: той самий друг, цей власник, цей сервер, саме цей одноразовий ключ,
	//    і VPS вказав САМЕ мій ключ каналу (інакше мандат видано для іншого пристрою).
	m := h.Mandate
	ownPub := p.HostKey.Public().(ed25519.PublicKey)
	if checkMandate(p.VPSKey(), m, now()) != nil || m.Owner != p.Self || m.Friend != h.Friend || m.Srv != h.Srv ||
		m.CT != pubHash(h.Eph) || m.PK != base64.StdEncoding.EncodeToString(ownPub) {
		return nil, "", empty, ErrMandate
	}
	// 2. друг — ще й за моїм локальним списком
	if !p.IsFriend(h.Friend) {
		return nil, "", empty, ErrAuth
	}
	eph, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, "", empty, ErrHandshake
	}
	ephPub := eph.PublicKey().Bytes()
	th := sha256.Sum256(append(append([]byte{}, hb...), ephPub...))
	rep, _ := json.Marshal(helloReply{Eph: ephPub, Sig: ed25519.Sign(p.HostKey, hostSigInput(th[:]))})
	if err := writeFrame(conn, rep); err != nil {
		return nil, "", empty, err
	}
	peer, err := ecdh.X25519().NewPublicKey(h.Eph)
	if err != nil {
		return nil, "", empty, ErrHandshake
	}
	dh, err := eph.ECDH(peer)
	if err != nil {
		return nil, "", empty, ErrHandshake
	}
	c2h, h2c := derive(dh, th[:])
	// 3. друг доводить володіння одноразовим ключем з мандата (без нього ключів сесії не вивести).
	tag, err := readFrame(conn, 64)
	if err != nil || subtle.ConstantTimeCompare(tag, hm(c2h, []byte("SHAURMA-P2P-CLIENT"), th[:])) != 1 {
		return nil, "", empty, ErrAuth
	}
	cc, err := newConn(conn, h2c, c2h, now())
	if err != nil {
		return nil, "", empty, ErrHandshake
	}
	return cc, h.Friend, m, nil
}

// Send шифрує й відправляє одне повідомлення.
func (c *Conn) Send(msg []byte) error {
	if len(msg) == 0 || len(msg) > MaxMessage {
		return ErrFrame
	}
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	if time.Now().After(c.deadline) {
		return ErrClosed
	}
	ct := c.sendAEAD.Seal(nil, nonce(c.sendCtr), msg, nil)
	c.sendCtr++
	return writeFrame(c.c, ct)
}

// Recv читає й розшифровує одне повідомлення. Будь-яка підробка/повтор/пропуск — помилка (з'єднання
// після цього непридатне й закривається).
func (c *Conn) Recv() ([]byte, error) {
	c.recvMu.Lock()
	defer c.recvMu.Unlock()
	if time.Now().After(c.deadline) {
		return nil, ErrClosed
	}
	ct, err := readFrame(c.c, MaxMessage+chacha20poly1305.Overhead)
	if err != nil {
		return nil, err
	}
	pt, err := c.recvAEAD.Open(nil, nonce(c.recvCtr), ct, nil)
	if err != nil {
		_ = c.c.Close()
		return nil, ErrAuth
	}
	c.recvCtr++
	return pt, nil
}

// Close закриває транспорт.
func (c *Conn) Close() error { return c.c.Close() }

// SetReadDeadline — для циклів з таймаутом простою.
func (c *Conn) SetReadDeadline(t time.Time) error { return c.c.SetReadDeadline(t) }

var _ = bytes.Equal
