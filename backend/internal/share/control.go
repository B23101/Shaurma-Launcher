package share

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// Службові повідомлення про стан передачі — фінальні події, які мусять дійти до іншої
// сторони навіть тоді, коли P2P-тунелю вже/ще немає (відправник закрив лаунчер, друг
// відмовився, не відкриваючи тунель). Їдуть ЗВИЧАЙНИМ зашифрованим повідомленням чату з
// останнім рядком
//
//	shaurma-share-ctl:v1:<base64url(json Control)>
//
// тож VPS нічого не треба знати, а старі версії лаунчера просто бачать читабельний
// текст над рядком. Нові — ховають рядок і застосовують подію до своєї передачі.
//
// Події:
//
//	одержувач → відправник: declined | cancelled | failed (+ r: код причини)
//	відправник → одержувач: cancelled (+ r: cancelled_sender | timeout)

const ControlPrefix = "shaurma-share-ctl:v1:"

// Події службових повідомлень.
const (
	CtlDeclined  = "declined"
	CtlCancelled = "cancelled"
	CtlFailed    = "failed"
)

// Control — службове повідомлення передачі.
type Control struct {
	V      int    `json:"v"`
	SID    string `json:"sid"`
	Event  string `json:"e"`
	Reason string `json:"r,omitempty"`
	// Name — назва збірки/сервера (щоб одержувач, який ще не відкривав картку, мав що показати).
	Name string `json:"n,omitempty"`
}

func isHex32(s string) bool { return len(s) == 32 && isHexLower(s) }

func isHexLower(s string) bool {
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func (c Control) valid() bool {
	if c.V != 1 || !isHex32(c.SID) || len(c.Name) > 200 || !utf8.ValidString(c.Name) {
		return false
	}
	switch c.Event {
	case CtlDeclined, CtlCancelled, CtlFailed:
		return true
	}
	return false
}

// EncodeControl — текст повідомлення: людський рядок (для старих версій) + службовий рядок.
func EncodeControl(c Control, human string) string {
	c.V = 1
	if c.Reason != "" {
		c.Reason = CleanReason(c.Reason)
	}
	raw, _ := json.Marshal(c)
	line := ControlPrefix + base64.RawURLEncoding.EncodeToString(raw)
	human = strings.TrimSpace(strings.ReplaceAll(human, ControlPrefix, ""))
	if human == "" {
		return line
	}
	return human + "\n" + line
}

// ParseControl розпізнає службове повідомлення (маркер — останнім «словом» тексту).
func ParseControl(text string) (Control, bool) {
	if len(text) > 2000 {
		return Control{}, false
	}
	t := strings.TrimSpace(text)
	i := strings.LastIndex(t, ControlPrefix)
	if i < 0 {
		return Control{}, false
	}
	enc := t[i+len(ControlPrefix):]
	if enc == "" || strings.ContainsAny(enc, " \t\r\n") {
		return Control{}, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(enc)
	if err != nil {
		return Control{}, false
	}
	var c Control
	if json.Unmarshal(raw, &c) != nil || !c.valid() {
		return Control{}, false
	}
	if c.Reason != "" {
		c.Reason = CleanReason(c.Reason)
	}
	return c, true
}
