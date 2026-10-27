// lobby_rooms.go — клієнт нових ендпоінтів вузла: оренда хоста (host-start/stop), співхости, спільні
// кімнати, списки доступу, kick, admit. Контракт — vps/internal/p2p (cohost.go, acl.go, tickets.go).
package p2p

import (
	"context"
	"net/http"
	"net/url"
	"strings"
)

// ── оренда хоста ──────────────────────────────────────────────────────────

// HostStartResponse — відповідь POST /p2p/host-start. Shared=false: кімната без співхостів, оренда не
// потрібна (SessionToken порожній), хостинг іде як раніше лише з ownerSecret.
type HostStartResponse struct {
	OK             bool   `json:"ok"`
	Address        string `json:"address"`
	Shared         bool   `json:"shared"`
	SessionToken   string `json:"sessionToken"`
	LeaseTTL       int    `json:"leaseTtl"` // секунди (45)
	LeaseExpiresAt int64  `json:"leaseExpiresAt"`
	Access         string `json:"access"`
	Disabled       bool   `json:"disabled"`
}

// HostStart бере оренду кімнати. ownerSecret порожній — це співхост (потрібен JWT). 409 p2p_room_busy
// (хтось уже хостить) повертається як *LobbyError із HostName.
func (c *LobbyClient) HostStart(ctx context.Context, address, ownerSecret string) (HostStartResponse, error) {
	var out HostStartResponse
	req := map[string]string{"address": address}
	if ownerSecret != "" {
		req["ownerSecret"] = ownerSecret
	}
	err := c.doJSON(ctx, http.MethodPost, "/p2p/host-start", req, &out)
	return out, err
}

// HostStop віддає оренду (разом зі стиранням IP). 403 p2p_lease_lost = оренда вже завершена (не критично).
func (c *LobbyClient) HostStop(ctx context.Context, address, sessionToken string) error {
	req := map[string]string{"address": address, "sessionToken": sessionToken}
	return c.doJSON(ctx, http.MethodPost, "/p2p/host-stop", req, nil)
}

// LeaseKick — власник забирає хост у співхоста. Повертає true, якщо хтось справді був вигнаний.
func (c *LobbyClient) LeaseKick(ctx context.Context, address, ownerSecret string) (bool, error) {
	var out struct {
		OK     bool `json:"ok"`
		Kicked bool `json:"kicked"`
	}
	req := map[string]string{"address": address, "ownerSecret": ownerSecret}
	err := c.doJSON(ctx, http.MethodPost, "/p2p/lease/kick", req, &out)
	return out.Kicked, err
}

// ── режим доступу кімнати без хостингу ───────────────────────────────────

// RoomAccessSet змінює режим доступу кімнати, НЕ вимагаючи активної сесії хостингу (власник за
// ownerSecret-ом). Навіщо: гравці готують доступ заздалегідь — «зайдуть лише друзі» має
// зберігатися до старту сервера, а не лише поки кімната хоститься (раніше режим мінявся виключно
// через host-update, тому в UI перемикач був заблокований без запущеного сервера).
// Порожній access у відповіді = вузол старий і не знає цього ендпоінту (404 Not Found).
func (c *LobbyClient) RoomAccessSet(ctx context.Context, subdomain, ownerSecret, access string) error {
	req := map[string]string{"subdomain": subdomain, "ownerSecret": ownerSecret, "access": access}
	err := c.doJSON(ctx, http.MethodPost, "/p2p/room/access", req, nil)
	if err != nil {
		if le, ok := AsLobbyError(err); ok && le.Status == http.StatusNotFound {
			return errRoomAccessUnsupported
		}
	}
	return err
}

// RoomAccessGet читає поточний режим доступу кімнати (власник за ownerSecret-ом).
func (c *LobbyClient) RoomAccessGet(ctx context.Context, subdomain, ownerSecret string) (string, error) {
	var out struct {
		Access string `json:"access"`
	}
	err := c.doJSONH(ctx, http.MethodGet, "/p2p/room/access"+addressQuery(subdomain), ownerHeader(ownerSecret), nil, &out)
	if err != nil {
		if le, ok := AsLobbyError(err); ok && le.Status == http.StatusNotFound {
			return "", errRoomAccessUnsupported
		}
		return "", err
	}
	return out.Access, nil
}

// ── admit ─────────────────────────────────────────────────────────────────

// AdmitResult — відповідь POST /p2p/admit. ExpiresAt — коли спливає сам квиток (unix мс): після цього
// admit із ним завжди false, тож повторні перевірки підключених гостей мають зупинятись раніше.
type AdmitResult struct {
	OK        bool   `json:"ok"`
	AccountID string `json:"accountId"`
	ExpiresAt int64  `json:"expiresAt"`
}

// AdmitTicket питає вузол, чи пускати гостя з цим квитком (виклик хоста). Хост доводить себе ownerSecret-ом
// (власник) або sessionToken-ом оренди (співхост). OK=false при 403 {ok:false}; інші помилки — у err
// (гейт трактує їх як «не пускати»).
func (c *LobbyClient) AdmitTicket(ctx context.Context, address, ticket, ownerSecret, sessionToken string) (AdmitResult, error) {
	req := map[string]string{"address": address, "ticket": ticket}
	if ownerSecret != "" {
		req["ownerSecret"] = ownerSecret
	} else if sessionToken != "" {
		req["sessionToken"] = sessionToken
	}
	var out AdmitResult
	err := c.doJSON(ctx, http.MethodPost, "/p2p/admit", req, &out)
	if err != nil {
		if le, ok := AsLobbyError(err); ok && le.Status == http.StatusForbidden && le.Code == "" {
			return AdmitResult{}, nil
		}
		// 403 {"ok":false} не містить поля error → Code порожній; будь-який інший збій — помилка.
		return AdmitResult{}, err
	}
	return out, nil
}

// Admit — AdmitTicket без деталей: лише «пускати чи ні».
func (c *LobbyClient) Admit(ctx context.Context, address, ticket, ownerSecret, sessionToken string) (allowed bool, err error) {
	res, err := c.AdmitTicket(ctx, address, ticket, ownerSecret, sessionToken)
	return res.OK, err
}

// ── співхости ─────────────────────────────────────────────────────────────

// ACLEntry — запис для add/remove: повний id акаунта АБО кастомний @handle (VPS зводить його до того
// самого id); kind (allow|deny) — лише для ACL. Нік лишився для старих викликів, але нік не унікальний,
// тому інтерфейс його більше не передає.
type ACLEntry struct {
	ID     string `json:"id,omitempty"`
	Handle string `json:"handle,omitempty"`
	Nick   string `json:"nick,omitempty"`
	Kind   string `json:"kind,omitempty"`
}

// EntryResult — результат однієї операції add/remove.
type EntryResult struct {
	Key    string `json:"key"`
	Op     string `json:"op"`
	Status string `json:"status"` // ok, not_friend, self, limit_room, limit_account, not_found, …
}

// CohostRow — співхост кімнати.
type CohostRow struct {
	ID      string `json:"id"`
	Nick    string `json:"nick"`
	Handle  string `json:"handle,omitempty"`
	Color   string `json:"color,omitempty"`
	AddedAt int64  `json:"addedAt"`
}

// CohostHost — хто хостить кімнату зараз (nil, якщо ніхто).
type CohostHost struct {
	AccountID      string `json:"accountId"`
	Nick           string `json:"nick"`
	LeaseExpiresAt int64  `json:"leaseExpiresAt"`
}

// CohostList — GET /p2p/cohost.
type CohostList struct {
	Cohosts []CohostRow `json:"cohosts"`
	Host    *CohostHost `json:"host"`
	Max     int         `json:"max"`
}

// CohostApplyResult — відповідь POST /p2p/cohost.
type CohostApplyResult struct {
	OK      bool          `json:"ok"`
	Results []EntryResult `json:"results"`
	Cohosts []CohostRow   `json:"cohosts"`
}

func ownerHeader(ownerSecret string) map[string]string {
	return map[string]string{"X-P2P-Owner-Secret": ownerSecret}
}

func addressQuery(address string) string {
	return "?address=" + url.QueryEscape(strings.TrimSpace(address))
}

// Cohosts — список співхостів і поточний хост (лише власник).
func (c *LobbyClient) Cohosts(ctx context.Context, address, ownerSecret string) (CohostList, error) {
	var out CohostList
	err := c.doJSONH(ctx, http.MethodGet, "/p2p/cohost"+addressQuery(address), ownerHeader(ownerSecret), nil, &out)
	return out, err
}

// CohostApply додає/прибирає співхостів (лише друзі власника; ліміти — на сервері).
func (c *LobbyClient) CohostApply(ctx context.Context, address, ownerSecret string, add, remove []ACLEntry) (CohostApplyResult, error) {
	var out CohostApplyResult
	req := map[string]any{"address": address, "ownerSecret": ownerSecret, "add": add, "remove": remove}
	err := c.doJSON(ctx, http.MethodPost, "/p2p/cohost", req, &out)
	return out, err
}

// SharedRoom — кімната, де мене додано співхостом.
type SharedRoom struct {
	Subdomain string `json:"subdomain"`
	Address   string `json:"address"`
	OwnerNick string `json:"ownerNick"`
	// Хто саме дав цю адресу: id/@handle/колір власника кімнати (щоб у UI було видно, чия вона).
	OwnerID     string `json:"ownerId,omitempty"`
	OwnerHandle string `json:"ownerHandle,omitempty"`
	OwnerColor  string `json:"ownerColor,omitempty"`
	Access      string `json:"access"`
	Disabled  bool   `json:"disabled"`
	Busy      bool   `json:"busy"` // зараз хостить хтось інший
	Mine      bool   `json:"mine"` // зараз хостжу я
	HostNick  string `json:"hostNick"`
}

// Shared — кімнати, де мене додано співхостом (потрібен JWT).
func (c *LobbyClient) Shared(ctx context.Context) ([]SharedRoom, error) {
	var out struct {
		Rooms []SharedRoom `json:"rooms"`
	}
	err := c.doJSON(ctx, http.MethodGet, "/p2p/shared", nil, &out)
	return out.Rooms, err
}

// ── списки доступу ────────────────────────────────────────────────────────

// ACLItem — елемент білого/чорного списку.
type ACLItem struct {
	ID     string `json:"id"`
	Nick   string `json:"nick"`
	Handle string `json:"handle,omitempty"`
	Color  string `json:"color,omitempty"`
}

// ACLList — GET /p2p/acl: allow — білий список (режим list), deny — чорний.
type ACLList struct {
	Allow []ACLItem `json:"allow"`
	Deny  []ACLItem `json:"deny"`
}

// ACLApplyResult — відповідь POST /p2p/acl.
type ACLApplyResult struct {
	OK      bool          `json:"ok"`
	Results []EntryResult `json:"results"`
}

// ACL читає списки доступу власника (спільні для всіх його кімнат).
func (c *LobbyClient) ACL(ctx context.Context, address, ownerSecret string) (ACLList, error) {
	var out ACLList
	err := c.doJSONH(ctx, http.MethodGet, "/p2p/acl"+addressQuery(address), ownerHeader(ownerSecret), nil, &out)
	return out, err
}

// ACLApply додає/прибирає записи (id або нік; kind allow|deny у add).
func (c *LobbyClient) ACLApply(ctx context.Context, address, ownerSecret string, add, remove []ACLEntry) (ACLApplyResult, error) {
	var out ACLApplyResult
	req := map[string]any{"address": address, "ownerSecret": ownerSecret, "add": add, "remove": remove}
	err := c.doJSON(ctx, http.MethodPost, "/p2p/acl", req, &out)
	return out, err
}
