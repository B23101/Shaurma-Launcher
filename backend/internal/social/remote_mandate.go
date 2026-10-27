package social

// remote_mandate.go — перевірка «мандата» VPS (див. vps/internal/hub/remote_mandate.go).
// Формат підпису ТОЧНО збігається з VPS; обидва боки мають спільний known-answer тест.

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"
)

const (
	mandateVersion = 1
	mandateDomain  = "SHAURMA-REMOTE-MANDATE-V1"
)

// Операції віддаленого керування.
const (
	RemoteOpConsoleOpen  = "console_open"
	RemoteOpConsoleExec  = "console_exec"
	RemoteOpConsoleClose = "console_close"
)

// Помилки перевірки мандата (для журналу; другові їх не показуємо).
var (
	ErrMandateMissing = errors.New("social:mandate_missing")
	ErrMandateKey     = errors.New("social:mandate_no_vps_key")
	ErrMandateBad     = errors.New("social:mandate_bad")
	ErrMandateBinding = errors.New("social:mandate_binding")
)

// RemoteMandate — підписане VPS свідоцтво дружби й права.
type RemoteMandate struct {
	V      int    `json:"v"`
	Owner  string `json:"owner"`
	Friend string `json:"friend"`
	Srv    string `json:"srv"`
	Op     string `json:"op"`
	Req    string `json:"req"`
	Caps   string `json:"caps"`
	CT     string `json:"ct"`
	SID    string `json:"sid,omitempty"`
	PK     string `json:"pk,omitempty"`
	IAT    int64  `json:"iat"`
	EXP    int64  `json:"exp"`
	Sig    string `json:"sig"`
}

// SigningBytes — канонічні байти (дзеркало VPS).
func (m RemoteMandate) SigningBytes() []byte {
	parts := []string{mandateDomain, strconv.Itoa(m.V), m.Owner, m.Friend, m.Srv, m.Op, m.Req, m.Caps, m.CT, m.SID, m.PK,
		strconv.FormatInt(m.IAT, 10), strconv.FormatInt(m.EXP, 10)}
	return []byte(strings.Join(parts, "\n"))
}

// EnvelopeHash — хеш конверта для поля CT.
func EnvelopeHash(ciphertext, nonce string) string {
	h := sha256.Sum256([]byte(ciphertext + "\n" + nonce))
	return base64.StdEncoding.EncodeToString(h[:])
}

// VerifyMandateSig перевіряє підпис і строк дії (без прив'язки до конкретної команди).
func VerifyMandateSig(pub ed25519.PublicKey, m RemoteMandate, now time.Time) error {
	if len(pub) != ed25519.PublicKeySize {
		return ErrMandateKey
	}
	if m.V != mandateVersion {
		return ErrMandateBad
	}
	sig, err := base64.StdEncoding.DecodeString(m.Sig)
	if err != nil || !ed25519.Verify(pub, m.SigningBytes(), sig) {
		return ErrMandateBad
	}
	n := now.UnixMilli()
	if n < m.IAT-30_000 || n > m.EXP {
		return ErrMandateBad
	}
	return nil
}

// HasCap — чи містить мандат літеру права (s=start x=stop c=console e=exec).
func (m RemoteMandate) HasCap(c byte) bool { return strings.IndexByte(m.Caps, c) >= 0 }

// VerifyIncoming перевіряє, що мандат підписано VPS і прив'язано САМЕ до цієї команди: власник —
// це ми, відправник — той, від кого прийшов кадр, і хеш конверта збігається з тим, що ми
// розшифровуємо. Операція/сервер у мандаті — вирішальні (відкриті поля кадру ігноруються).
func VerifyIncoming(pub ed25519.PublicKey, m *RemoteMandate, selfID string, in RemoteIncoming, now time.Time) error {
	if m == nil {
		return ErrMandateMissing
	}
	if err := VerifyMandateSig(pub, *m, now); err != nil {
		return err
	}
	if m.Owner != selfID || m.Friend != in.From || m.Req != in.ReqID || m.SID != in.SID ||
		m.CT != EnvelopeHash(in.Ciphertext, in.Nonce) {
		return ErrMandateBinding
	}
	return nil
}

// KeyPin — закріплений (TOFU) публічний ключ VPS. Перший побачений ключ запам'ятовується через
// Save; будь-яка зміна потім відхиляється, доки власник явно не скине довіру (Reset).
type KeyPin struct {
	Load func() string
	Save func(string)
}

// Check повертає ключ для перевірки або помилку, якщо ключ змінився.
func (k KeyPin) Check(offeredB64 string) (ed25519.PublicKey, error) {
	raw, err := base64.StdEncoding.DecodeString(offeredB64)
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return nil, ErrMandateKey
	}
	cur := ""
	if k.Load != nil {
		cur = k.Load()
	}
	if cur == "" {
		if k.Save != nil {
			k.Save(offeredB64)
		}
		return ed25519.PublicKey(raw), nil
	}
	if cur != offeredB64 {
		return nil, ErrMandateKey
	}
	return ed25519.PublicKey(raw), nil
}
