package social

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// Керуючі символи, нульова ширина і bidi-override (U+202E тощо). Тим
// підміняють текст в інтерфейсі («Pack» → «kcaP», невидимі частини назви).
// Той самий набір, що HIDDEN_CHARS_RE у worker/friends.js.
func isHidden(r rune) bool {
	switch {
	case r <= 0x1f, r >= 0x7f && r <= 0x9f:
		return true
	case r >= 0x200b && r <= 0x200f:
		return true
	case r >= 0x202a && r <= 0x202e:
		return true
	case r >= 0x2066 && r <= 0x2069:
		return true
	case r == 0xfeff:
		return true
	}
	return false
}

// CleanMessage — як CleanText, але ЗБЕРІГАЄ переноси рядків (\n). Для тіла чат-
// повідомлень: картки «поділитися збіркою/сервером» і посилання на моди тримають
// маркер в ОСТАННЬОМУ рядку, а CleanText вирізав усі \n (вони <= 0x1f), тож
// маркер зливався з текстом і картка в чаті показувалась сирим текстом.
// \r\n і \r нормалізуються до \n; табуляція та інші керуючі символи — як і раніше
// вирізаються.
func CleanMessage(s string, maxRunes int) string {
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "")
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	var b strings.Builder
	for _, r := range s {
		if r != '\n' && isHidden(r) {
			continue
		}
		b.WriteRune(r)
	}
	out := strings.TrimSpace(b.String())
	if utf8.RuneCountInString(out) > maxRunes {
		out = strings.TrimSpace(string([]rune(out)[:maxRunes]))
	}
	return out
}

// CleanText вирізає приховані символи, обрізає пробіли й обмежує довжину в
// СИМВОЛАХ (не байтах). Недійсний UTF-8 замінюється, а не пропускається.
func CleanText(s string, maxRunes int) string {
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "")
	}
	var b strings.Builder
	n := 0
	for _, r := range s {
		if isHidden(r) {
			continue
		}
		b.WriteRune(r)
		n++
	}
	out := strings.TrimSpace(b.String())
	if utf8.RuneCountInString(out) > maxRunes {
		out = string([]rune(out)[:maxRunes])
		out = strings.TrimSpace(out)
	}
	return out
}

// accountIDRe — дзеркало isValidAccountId у worker/friends.js.
var accountIDRe = regexp.MustCompile(`^[0-9a-f-]{8,64}$`)

// ValidAccountID — чи схожий рядок на accountId. Не доводить існування, лише
// відсікає сміття ДО того, як ми покладемо його в URL чи тіло запиту.
func ValidAccountID(s string) bool { return accountIDRe.MatchString(s) }

var colorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// SanitizeColor повертає колір лише якщо це #RRGGBB, інакше "".
func SanitizeColor(s string) string {
	if colorRe.MatchString(s) {
		return strings.ToLower(s)
	}
	return ""
}

// inviteIDRe — id інвайту (crypto.randomUUID() на Worker). Перевіряємо перед
// вставкою в шлях, щоб «../» чи «?x=» ніколи не потрапили в URL.
var inviteIDRe = regexp.MustCompile(`^[0-9a-fA-F-]{8,64}$`)

func ValidInviteID(s string) bool { return inviteIDRe.MatchString(s) }

// SanitizeStatus — лише відомі статуси; усе інше → "" (подію ігноруємо).
func SanitizeStatus(s string) string {
	switch s {
	case StatusOnline, StatusAway, StatusInGame, StatusOffline:
		return s
	}
	return ""
}

// SanitizePrivacyValue перевіряє значення за білим списком.
func inSet(v string, allowed ...string) bool {
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
}

// NormalizePrivacy підставляє дефолт у кожне поле, значення якого не з
// білого списку (напр. Worker віддав щось несподіване).
func NormalizePrivacy(p Privacy) Privacy {
	d := DefaultPrivacy()
	if !inSet(p.ShowStatusTo, "everyone", "friends", "nobody") {
		p.ShowStatusTo = d.ShowStatusTo
	}
	if !inSet(p.ShowServerTo, "everyone", "friends", "nobody") {
		p.ShowServerTo = d.ShowServerTo
	}
	if !inSet(p.FriendRequest, "everyone", "friends_of_friends", "nobody") {
		p.FriendRequest = d.FriendRequest
	}
	if !inSet(p.WhoCanMessage, "friends", "everyone", "nobody") {
		p.WhoCanMessage = d.WhoCanMessage
	}
	return p
}

// ValidatePrivacy — сувора перевірка введення користувача (на відміну від
// NormalizePrivacy, що м'яко лагодить дані ззовні).
func ValidatePrivacy(p Privacy) error {
	if !inSet(p.ShowStatusTo, "everyone", "friends", "nobody") ||
		!inSet(p.ShowServerTo, "everyone", "friends", "nobody") ||
		!inSet(p.FriendRequest, "everyone", "friends_of_friends", "nobody") ||
		(p.WhoCanMessage != "" && !inSet(p.WhoCanMessage, "friends", "everyone", "nobody")) {
		return ErrBadInput
	}
	return nil
}
