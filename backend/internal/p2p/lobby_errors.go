// lobby_errors.go — типізовані помилки відповідей вузла лобі. Раніше всі помилки були рядками;
// UI та хост-цикл мають відрізняти «кімнату хостить інший» від «оренду втрачено» тощо.
package p2p

import (
	"errors"
	"net/http"
)

// Машинні коди поля error у відповідях вузла.
const (
	CodeRoomBusy        = "p2p_room_busy"        // 409: кімнату зараз хостить інший
	CodeLeaseRequired   = "p2p_lease_required"   // 409: спільна кімната, спершу host-start
	CodeLeaseLost       = "p2p_lease_lost"       // 403: оренда втрачена
	CodeHostRevoked     = "p2p_host_revoked"     // 403: власник забрав доступ співхоста
	CodeOwnerOnly       = "p2p_owner_only"       // 403: лише власник
	CodeNotAllowed      = "p2p_not_allowed"      // 403: join заборонено режимом/списками
	CodeTunnelDisabled  = "p2p_tunnel_disabled"  // 403: тунель вимкнено адміном
	CodeLimitReached    = "p2p_limit_reached"    // 429: ліміт кімнат/оренд (scope account/device/network)
	CodeAccountRequired = "p2p_account_required" // 401: потрібен акаунт Шаурми
	CodeBadAccess       = "p2p_bad_access"       // 400: невідомий режим доступу
)

// Сентінели для errors.Is.
var (
	ErrRoomBusy        = errors.New("p2p: кімнату хостить інший гравець")
	ErrLeaseRequired   = errors.New("p2p: потрібна оренда хоста")
	ErrLeaseLost       = errors.New("p2p: оренду хоста втрачено")
	ErrHostRevoked     = errors.New("p2p: доступ співхоста забрано")
	ErrOwnerOnly       = errors.New("p2p: лише для власника кімнати")
	ErrNotAllowed      = errors.New("p2p: доступ до кімнати заборонено")
	ErrTunnelDisabled  = errors.New("p2p: тунель вимкнено адміністратором")
	ErrLimitReached    = errors.New("p2p: досягнуто ліміт кімнат")
	ErrAccountRequired = errors.New("p2p: потрібен акаунт Шаурми")
)

var codeSentinels = map[string]error{
	CodeRoomBusy: ErrRoomBusy, CodeLeaseRequired: ErrLeaseRequired, CodeLeaseLost: ErrLeaseLost,
	CodeHostRevoked: ErrHostRevoked, CodeOwnerOnly: ErrOwnerOnly, CodeNotAllowed: ErrNotAllowed,
	CodeTunnelDisabled: ErrTunnelDisabled, CodeLimitReached: ErrLimitReached, CodeAccountRequired: ErrAccountRequired,
}

func isTypedCode(code string) bool { _, ok := codeSentinels[code]; return ok }

// LobbyError — помилка відповіді вузла (HTTP ≥ 300).
type LobbyError struct {
	Status  int
	Code    string // поле error (машинний код або англ. текст для старих відповідей)
	Message string // errorMessage, інакше error
	Text    string // готовий текст для користувача й логів
	// Додаткові поля окремих кодів.
	HostName string // p2p_room_busy: хто хостить
	Limit    int    // p2p_limit_reached: ліміт
	Scope    string // p2p_limit_reached: account | device | network
	// Access — p2p_not_allowed: режим доступу кімнати (friends | list | accounts), якщо VPS його
	// повідомив. Порожній — режим невідомий або гравця не пустив чорний список: тоді
	// режим не розкриваємо (reject.go).
	Access string
}

func (e *LobbyError) Error() string { return e.Text }

// Is зіставляє помилку з сентінелами: ErrAddressNotFound (404) і кодами p2p_*.
func (e *LobbyError) Is(target error) bool {
	if target == ErrAddressNotFound {
		return e.Status == http.StatusNotFound
	}
	if s, ok := codeSentinels[e.Code]; ok {
		return target == s
	}
	return false
}

// AsLobbyError витягує *LobbyError з ланцюжка помилок.
func AsLobbyError(err error) (*LobbyError, bool) {
	var le *LobbyError
	if errors.As(err, &le) {
		return le, true
	}
	return nil, false
}
