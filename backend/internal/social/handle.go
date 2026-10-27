package social

import (
	"regexp"
	"strings"
)

// Handle — публічний ідентифікатор для пошуку друзів (@bob_4821). Дзеркало VPS
// accounts.NormalizeHandle і worker/friends.js normalizeHandle: ті самі тестові вектори
// (handle_test.go ↔ vps/internal/accounts/handle_test.go ↔ worker/tests/test.mjs).
const (
	MinHandleLen = 3
	MaxHandleLen = 32
)

var handleRe = regexp.MustCompile(`^[a-z0-9_.]{3,32}$`)

// NormalizeHandle — обрізає пробіли, прибирає початкову «@», нижній регістр, перевіряє формат:
// [a-z0-9_.], 3–32, без «..» і «__», без «.»/«_» на краях.
func NormalizeHandle(raw string) (string, bool) {
	h := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(raw)), "@")
	if !handleRe.MatchString(h) || strings.Contains(h, "..") || strings.Contains(h, "__") {
		return "", false
	}
	if strings.ContainsAny(h[:1], "._") || strings.ContainsAny(h[len(h)-1:], "._") {
		return "", false
	}
	return h, true
}

// SanitizeHandle — handle від сервера для відображення: валідний або "".
// Сміття (в т.ч. приховані символи) не показуємо.
func SanitizeHandle(raw string) string {
	h, ok := NormalizeHandle(raw)
	if !ok || h != raw {
		return ""
	}
	return h
}
