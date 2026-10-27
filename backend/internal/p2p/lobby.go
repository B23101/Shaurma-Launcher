// lobby.go — HTTP-клієнт лобі-API (Worker /p2p/*), v3: адреса =
// baseName + випадковий 6-значний суфікс, що одночасно слугує "PIN"-ом
// (щоб приєднатись, треба знати ПОВНУ адресу, не лише базову назву).
// Той самий токен-gate (X-Shaurma-Token + User-Agent ShaurmaLauncher/),
// що й решта запитів лаунчера до воркера.
package p2p

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"strings"
	"time"

	"shaurma-launcher-backend/internal/launchlog"
)

const lobbyUserAgent = "ShaurmaLauncher/2.0 (+p2p)"

// ErrAddressNotFound — lobby відповів 404. Для host-update це означає, що
// застовбленої адреси в базі більше немає (протухла за 30 днів або її
// звільнили). Викликач (StartP2PHost) ловить її, щоб АВТОМАТИЧНО зайняти
// свіжу адресу замість того, щоб показувати гравцю глухий кут.
var ErrAddressNotFound = errors.New("p2p: адресу не знайдено в lobby")

// LobbyClient — тонкий HTTP-клієнт над Worker-ендпоінтами /p2p/*.
type LobbyClient struct {
	baseURL string
	token   string
	hc      *http.Client
	// auth — джерело JWT акаунта й HWID пристрою (nil/порожньо = анонімний запит).
	// На вузлі JWT необов'язковий, але без нього не працюють режими доступу,
	// співхости, ліміти на акаунт і квитки допуску.
	auth AuthFunc
}

// AuthFunc повертає JWT (Bearer) і HWID пристрою. Порожній bearer — запит анонімний.
type AuthFunc func() (bearer, hwid string)

// WithAuth підключає JWT+HWID до всіх запитів клієнта.
func (c *LobbyClient) WithAuth(fn AuthFunc) *LobbyClient {
	c.auth = fn
	return c
}

func NewLobbyClient(baseURL, token string) *LobbyClient {
	return &LobbyClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		hc:      &http.Client{Timeout: 15 * time.Second},
	}
}

// Status — відповідь GET /p2p/status: чи може P2P взагалі працювати на
// цьому сервері. Потрібен UI, щоб не показувати кнопки, які гарантовано
// впадуть (немає Headscale — немає P2P).
type Status struct {
	Configured bool   `json:"configured"`
	RoomDomain string `json:"roomDomain,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

// FetchStatus питає worker, чи налаштований Headscale. Помилка мережі НЕ
// означає «P2P недоступний назавжди» — викликач має трактувати її як
// «невідомо» і не блокувати фічу через тимчасовий збій.
func (c *LobbyClient) FetchStatus(ctx context.Context) (Status, error) {
	var out Status
	err := c.doJSON(ctx, http.MethodGet, "/p2p/status", nil, &out)
	return out, err
}

// ClaimResponse — успішна відповідь /p2p/claim.
type ClaimResponse struct {
	// Subdomain — ПОВНА адреса разом із випадковим суфіксом, напр.
	// "prlool-482917". Це і є те, що хост копіює й надсилає друзям —
	// повний рядок, суфікс приховувати чи ховати не потрібно: він і є
	// захистом (щоб приєднатись, треба знати його цілком).
	Subdomain string `json:"subdomain"`
}

// ClaimAddress застовблює НОВУ адресу на основі базової назви (напр.
// "prlool") — Worker сам генерує до неї випадковий 6-значний цифровий
// суфікс і повертає повну унікальну адресу. Кожен виклик створює новий
// запис (нову адресу); попередні адреси того самого власника (якщо
// були) просто протухнуть самі за 30-денним cooldown, якщо їх не
// оновлювати через HostUpdate.
func (c *LobbyClient) ClaimAddress(ctx context.Context, baseName, ownerSecret string) (ClaimResponse, error) {
	return c.ClaimAddressAccess(ctx, baseName, ownerSecret, "")
}

// ClaimAddressAccess — те саме, але з початковим режимом доступу
// (all/accounts/friends/list; порожньо = типовий для сервера).
func (c *LobbyClient) ClaimAddressAccess(ctx context.Context, baseName, ownerSecret, access string) (ClaimResponse, error) {
	var out ClaimResponse
	req := map[string]string{"baseName": baseName, "ownerSecret": ownerSecret}
	if access != "" {
		req["access"] = access
	}
	err := c.doJSON(ctx, http.MethodPost, "/p2p/claim", req, &out)
	return out, err
}

// HostUpdateRequest — тіло POST /p2p/host-update.
type HostUpdateRequest struct {
	Subdomain   string `json:"subdomain"` // повна адреса з суфіксом
	OwnerSecret string `json:"ownerSecret"`
	HostIP      string `json:"ip"`
	HostPort    string `json:"port"`
	// OwnerName — публічний нік акаунта (для адмінки Pack Manager).
	OwnerName string `json:"ownerName,omitempty"`
	// UDPPort — порт голосового моду, який ми тунелюємо ('' = UDP-галочка
	// знята). HostPort тут саме TCP-порт гри; голосові моди слухають свій.
	UDPPort  string `json:"udpPort,omitempty"`
	RoomCode string `json:"roomCode,omitempty"` // діагностичний, гість його не бачить
	// SessionToken — токен оренди (host-start). Обов'язковий у спільній кімнаті; кожен
	// host-update із ним продовжує оренду (heartbeat, ≤15 с при TTL 45 с).
	SessionToken string `json:"sessionToken,omitempty"`
	// Access — новий режим доступу (лише власник). nil = не змінювати.
	Access *string `json:"access,omitempty"`
}

// HostUpdateResponse — відповідь /p2p/host-update.
type HostUpdateResponse struct {
	Address string `json:"address"` // "<subdomain>.play.shaurma-launcher.org"
	// Disabled — тунель вимкнено адміном із Pack Manager. Хостинг ми не
	// валимо (гравець не має бачити помилку за чужі дії), але власнику
	// показуємо, що кімнату вимкнено.
	Disabled bool `json:"disabled,omitempty"`
	// Access — чинний режим доступу кімнати (all/accounts/friends/list).
	Access string `json:"access,omitempty"`
	// LeaseExpiresAt — коли спливе оренда (unix мс; 0 = кімната без оренди).
	LeaseExpiresAt int64 `json:"leaseExpiresAt,omitempty"`
}

// HostUpdate оновлює поточні IP/port під уже застовбленою адресою —
// викликається при КОЖНОМУ StartHost() (нова ephemeral tsnet-нода, нова
// IP), сама адреса при цьому не змінюється.
func (c *LobbyClient) HostUpdate(ctx context.Context, req HostUpdateRequest) (HostUpdateResponse, error) {
	var out HostUpdateResponse
	err := c.doJSON(ctx, http.MethodPost, "/p2p/host-update", req, &out)
	return out, err
}

// HostOffline повідомляє лобі, що хостинг зупинено: IP/порт під адресою
// стираються, і гість отримує чесне «Хост зараз не в мережі» замість
// підключення до IP ноди, якої вже не існує (noda ефемерна і зникає разом
// із сесією). Best-effort: якщо мережа недоступна, сесію все одно закриваємо.
func (c *LobbyClient) HostOffline(ctx context.Context, subdomain, ownerSecret string) error {
	return c.HostOfflineLease(ctx, subdomain, ownerSecret, "")
}

// HostOfflineLease — offline зі sessionToken оренди: у спільній кімнаті лише так офлайн
// не зітре IP співхоста, який хостить зараз.
func (c *LobbyClient) HostOfflineLease(ctx context.Context, subdomain, ownerSecret, sessionToken string) error {
	req := map[string]any{"subdomain": subdomain, "ownerSecret": ownerSecret, "offline": true}
	if sessionToken != "" {
		req["sessionToken"] = sessionToken
	}
	return c.doJSON(ctx, http.MethodPost, "/p2p/host-update", req, nil)
}

// Release явно звільняє адресу (власник сам вирішив видалити свій
// сервер) — потребує того самого ownerSecret, яким її застовбили.
func (c *LobbyClient) Release(ctx context.Context, subdomain, ownerSecret string) error {
	req := map[string]string{"subdomain": subdomain, "ownerSecret": ownerSecret}
	return c.doJSON(ctx, http.MethodPost, "/p2p/release", req, nil)
}

// RoomInfo — відповідь на приєднання (резолв повної адреси в IP+порт хоста).
type RoomInfo struct {
	HostTailscaleIP string `json:"ip"`
	HostPort        string `json:"port"`
	// UDPPort — порт UDP-тунелю голосового чату, який опублікував хост
	// ('' = хост не вмикав UDP-галочку / база без міграції). Порожній порт
	// означає, що гість не піднімає UDP-слухач: голос не тунелюється.
	UDPPort string `json:"udpPort,omitempty"`
	// Ticket/TicketExpiresAt/Access — для режимів friends/accounts/list вузол видає квиток допуску
	// (діє поки живий хост, unix мс). Гість шле його хосту в преамбулі SHP2P1 (p2p_gate.go).
	Ticket          string `json:"ticket,omitempty"`
	TicketExpiresAt int64  `json:"ticketExpiresAt,omitempty"`
	Access          string `json:"access,omitempty"`
	// Subdomain — ПОВНА адреса кімнати, якою гість скористався при
	// JoinAddress. Сервер цього поля НЕ повертає (json:"-") — його
	// проставляє сам клієнт одразу після успішного JoinAddress, щоб
	// GuestService міг згодом знайти потрібну кімнату (і її tsnet-ноду)
	// в мапі rooms за цим самим ключем, коли прийде голосовий UDP-пакет.
	Subdomain string `json:"-"`
}

// JoinAddress резолвить ПОВНУ адресу друга (разом із 6-значним суфіксом
// — саме тому, що гравець ввів її повністю в Minecraft Direct Connect,
// вона й слугує одночасно кодом і захистом) у поточні IP+порт його хоста.
func (c *LobbyClient) JoinAddress(ctx context.Context, subdomain string) (RoomInfo, error) {
	var out RoomInfo
	path := "/p2p/join/" + strings.ToLower(strings.TrimSpace(subdomain))
	err := c.doJSON(ctx, http.MethodGet, path, nil, &out)
	return out, err
}

// authKeyResponse — відповідь POST /p2p/auth-key.
type authKeyResponse struct {
	AuthKey string `json:"authKey"`
}

// RequestAuthKey запитує одноразовий ephemeral Headscale auth key. Worker
// сам звертається до Headscale Admin API своїм секретом
// (env.HEADSCALE_API_KEY, ніколи не потрапляє в клієнт) — лаунчер
// отримує лише готовий, вже короткоживучий ключ.
func (c *LobbyClient) RequestAuthKey(ctx context.Context) (string, error) {
	var out authKeyResponse
	if err := c.doJSON(ctx, http.MethodPost, "/p2p/auth-key", nil, &out); err != nil {
		return "", err
	}
	if out.AuthKey == "" {
		return "", fmt.Errorf("p2p: lobby повернув порожній auth key")
	}
	return out.AuthKey, nil
}

func (c *LobbyClient) doJSON(ctx context.Context, method, path string, body, out any) error {
	return c.doJSONH(ctx, method, path, nil, body, out)
}

// doJSONH — doJSON з додатковими заголовками (напр. X-P2P-Owner-Secret для GET: секрет не кладемо в URL).
func (c *LobbyClient) doJSONH(ctx context.Context, method, path string, hdr map[string]string, body, out any) error {
	rid := newTraceID("L")
	sw := startWatch()
	tm := &httpTimings{}

	var rd io.Reader
	bodyLog := "—"
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(data)
		bodyLog = snippet(data, 300) // секрети редагуються
	}
	req, err := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, tm.clientTrace()), method, c.baseURL+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", lobbyUserAgent)
	req.Header.Set("X-Shaurma-Token", c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.auth != nil {
		if bearer, hwid := c.auth(); bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
			if hwid != "" {
				req.Header.Set("X-Shaurma-HWID", hwid)
			}
		}
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}

	launchlog.Debug(compLobby, "[%s] → %s %s%s тіло=%s токен=%s", rid, method, c.baseURL, path, bodyLog, maskSecret(c.token))

	resp, err := c.hc.Do(req)
	if err != nil {
		wrapped := fmt.Errorf("p2p: запит до lobby: %w", err)
		launchlog.Error(compLobby, "[%s] ✗ %s %s: ПОМИЛКА ЗАПИТУ за %s: %v | %s  ⇒ %s",
			rid, method, path, sw, err, tm, explainLobbyErr(wrapped))
		return wrapped
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	checkClockSkew(resp.Header, rid)

	if resp.StatusCode >= 300 {
		// Worker віддає ДВА поля: error — машинний код (напр.
		// p2p_not_configured), errorMessage — людське пояснення. Раніше
		// читався лише error, і користувач бачив невиразне «HTTP 500»
		// замість причини («на сервері немає Headscale»).
		var apiErr struct {
			Error        string `json:"error"`
			ErrorMessage string `json:"errorMessage"`
			HostName     string `json:"hostName"`
			Limit        int    `json:"limit"`
			Scope        string `json:"scope"`
			Access       string `json:"access"`
		}
		_ = json.Unmarshal(data, &apiErr)
		msg := apiErr.ErrorMessage
		if msg == "" {
			msg = apiErr.Error
		}

		var text string
		switch {
		case resp.StatusCode == http.StatusNotFound:
			text = fmt.Sprintf("%s: %s", msg, ErrAddressNotFound.Error())
		case resp.StatusCode == http.StatusTooManyRequests:
			// 429 — це НЕ лише per-IP rate-limit на /p2p/join. Той самий код віддають /p2p/claim і
			// host-start (p2p_limit_reached) — пріоритет повідомлення сервера.
			if msg != "" {
				text = "p2p: " + msg
			} else {
				text = "p2p: забагато спроб, зачекайте кілька хвилин і спробуйте ще раз"
			}
		case apiErr.Error == "p2p_not_configured":
			text = "p2p: «LAN через інтернет» не налаштований на сервері — " + msg
		case msg != "":
			text = fmt.Sprintf("p2p: %s (HTTP %d)", msg, resp.StatusCode)
		default:
			text = fmt.Sprintf("p2p: lobby HTTP %d", resp.StatusCode)
		}
		// Відомі коди (p2p_room_busy, p2p_lease_lost, …) мають людське повідомлення без «(HTTP n)».
		if isTypedCode(apiErr.Error) && apiErr.ErrorMessage != "" && resp.StatusCode != http.StatusTooManyRequests {
			text = "p2p: " + apiErr.ErrorMessage
		}
		if apiErr.Error == CodeLimitReached {
			// Машинна мітка для UI (скоуп і ліміт): фронтенд прибирає її з тексту й показує свій переклад.
			text += fmt.Sprintf(" [[limit:%s:%d]]", apiErr.Scope, apiErr.Limit)
		}
		rerr := &LobbyError{
			Status: resp.StatusCode, Code: apiErr.Error, Message: msg, Text: text,
			HostName: apiErr.HostName, Limit: apiErr.Limit, Scope: apiErr.Scope,
			Access: apiErr.Access,
		}
		// 404 на join/host-update — очікувана ситуація (адреса протухла), але
		// у лозі це все одно має бути видно з кодом помилки сервера.
		launchlog.Warn(compLobby, "[%s] ✗ %s %s → HTTP %d (%s) код=%q повідомлення=%q тіло=%s | %s ⇒ %s",
			rid, method, path, resp.StatusCode, sw, apiErr.Error, msg, snippet(data, 400), tm, explainLobbyErr(rerr))
		return rerr
	}

	lvl := launchlog.Info
	if path == "/p2p/status" {
		lvl = launchlog.Debug // UI опитує його постійно
	}
	lvl(compLobby, "[%s] ✓ %s %s → HTTP %d (%s, %s) тіло=%s | %s",
		rid, method, path, resp.StatusCode, sw, fmtBytes(int64(len(data))), snippet(data, 400), tm)

	if out == nil || len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		launchlog.Error(compLobby, "[%s] відповідь lobby не є валідним JSON для %s: %v | тіло=%s", rid, path, err, snippet(data, 200))
		return err
	}
	return nil
}
