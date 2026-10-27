package shaurmaid

import (
	"crypto/ed25519"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"math/bits"
	"strconv"
	"strings"
)

var b64 = base64.RawURLEncoding

type stdCrypto struct{}

// NewCrypto — Ed25519 зі стандартної бібліотеки.
func NewCrypto() CryptoService { return stdCrypto{} }

func (stdCrypto) NewIdentity() (*Identity, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	slot := make([]byte, 16)
	if _, err := rand.Read(slot); err != nil {
		return nil, err
	}
	return &Identity{PublicKey: b64.EncodeToString(pub), Slot: b64.EncodeToString(slot), private: priv}, nil
}

func (stdCrypto) Sign(id *Identity, message string) (string, error) {
	if id == nil || len(id.private) != ed25519.PrivateKeySize {
		return "", errors.New("shaurmaid: no private key")
	}
	return b64.EncodeToString(ed25519.Sign(ed25519.PrivateKey(id.private), []byte(message))), nil
}

// PowInput — дзеркало accounts.PowInput на VPS (known-answer тест у обох модулях).
func PowInput(pub string, ts int64, counter string) string {
	return "shaurma-pow-v1:" + pub + ":" + strconv.FormatInt(ts, 10) + ":" + counter
}

func (stdCrypto) SolvePoW(pub string, ts int64, want int) (string, error) {
	for i := int64(0); i < 1<<34; i++ {
		c := strconv.FormatInt(i, 10)
		h := sha256.Sum256([]byte(PowInput(pub, ts, c)))
		n := 0
		for _, b := range h {
			if b == 0 {
				n += 8
				continue
			}
			n += bits.LeadingZeros8(b)
			break
		}
		if n >= want {
			return c, nil
		}
	}
	return "", errors.New("shaurmaid: pow not found")
}

// OIDCNonce — дзеркало accounts.OIDCNonce: прив'язує id_token до ключа й комірки.
func OIDCNonce(pub, slot string) string {
	h := sha256.Sum256([]byte("shaurma-oidc-v1:" + pub + ":" + slot))
	return b64.EncodeToString(h[:])
}

// Повідомлення, що підписуються (дзеркало accounts.RegisterMessage / LoginMessage).
func registerMessage(nick, pub, slot, provider, typ, uuid, idToken string, ts int64, nonce, hwid string) string {
	th := sha256.Sum256([]byte(idToken))
	return "shaurma-register-v3\n" + nick + "\n" + pub + "\n" + slot + "\n" + provider + "\n" + typ + "\n" + uuid + "\n" +
		hexOf(th[:]) + "\n" + strconv.FormatInt(ts, 10) + "\n" + nonce + "\n" + hwid
}

// OfflineUUID — UUID офлайн-профілю Minecraft з ніка (32 hex, без дефісів): дзеркало
// accounts.OfflineUUIDHex на VPS. Неліцензійний акаунт передає саме його, сервер звіряє.
func OfflineUUID(nick string) string {
	b := md5.Sum([]byte("OfflinePlayer:" + nick))
	b[6] = b[6]&0x0f | 0x30
	b[8] = b[8]&0x3f | 0x80
	return hexOf(b[:])
}

// NormalizeUUID — 32 hex у нижньому регістрі без дефісів ("" — некоректний UUID).
func NormalizeUUID(s string) string {
	s = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), "-", ""))
	if len(s) != 32 {
		return ""
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return ""
		}
	}
	return s
}

// recoverMessage — дзеркало accounts.RecoverMessage на VPS.
func recoverMessage(pub, slot, provider, idToken string, ts int64, nonce, hwid string) string {
	th := sha256.Sum256([]byte(idToken))
	return "shaurma-recover-v1\n" + pub + "\n" + slot + "\n" + provider + "\n" + hexOf(th[:]) + "\n" +
		strconv.FormatInt(ts, 10) + "\n" + nonce + "\n" + hwid
}

// recoverMessageV2 — v2 підписує ще й revokeDeviceId (кого вилучити, якщо ліміт пристроїв) і
// назву пристрою: руйнівне поле не можна підмінити в дорозі.
func recoverMessageV2(pub, slot, provider, idToken string, ts int64, nonce, hwid, revokeDeviceID, deviceName string) string {
	th := sha256.Sum256([]byte(idToken))
	return "shaurma-recover-v2\n" + pub + "\n" + slot + "\n" + provider + "\n" + hexOf(th[:]) + "\n" +
		strconv.FormatInt(ts, 10) + "\n" + nonce + "\n" + hwid + "\n" + revokeDeviceID + "\n" + deviceName
}

func loginMessage(accountID, hwid string, ts int64, nonce string) string {
	return "shaurma-login-v1\n" + accountID + "\n" + hwid + "\n" + strconv.FormatInt(ts, 10) + "\n" + nonce
}

func hexOf(b []byte) string {
	const d = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, c := range b {
		out[i*2], out[i*2+1] = d[c>>4], d[c&15]
	}
	return string(out)
}

func randomNonce() (string, error) {
	b := make([]byte, 18)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return b64.EncodeToString(b), nil
}

// Export/ParseIdentity — серіалізація для LocalSecureStorage (лише всередині лаунчера).
func (i *Identity) Export() []byte {
	return []byte(i.PublicKey + "|" + i.Slot + "|" + b64.EncodeToString(i.private))
}

func ParseIdentity(raw []byte) (*Identity, error) {
	p := strings.Split(string(raw), "|")
	if len(p) != 3 {
		return nil, errors.New("shaurmaid: bad identity blob")
	}
	priv, err := b64.DecodeString(p[2])
	if err != nil || len(priv) != ed25519.PrivateKeySize {
		return nil, errors.New("shaurmaid: bad identity key")
	}
	return &Identity{PublicKey: p[0], Slot: p[1], private: priv}, nil
}
