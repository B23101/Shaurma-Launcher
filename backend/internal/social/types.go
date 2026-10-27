// Package social — клієнт соціальної системи Shaurma (ФАЗА 1, лише ДРУЗІ).
//
// Що тут є:
//   - Client: HTTP до Cloudflare Worker (/friends/*) — друзі, інвайти,
//     налаштування приватності. Це «холодні» дані, джерело правди — D1.
//   - Presence: WebSocket до shaurma-social на VPS — хто онлайн і в якій
//     збірці. Це «жива» інформація в пам'яті, втрата її безпечна.
//
// Чого тут СВІДОМО немає (робиться окремою фазою): чат, E2E-ключі,
// запрошення на сервер, share-pack, tray-віджет.
//
// ── МОДЕЛЬ ДОВІРИ (чому код такий параноїдальний) ───────────────────────
//  1. Worker і VPS — НЕ довірені для відображення. Усе, що від них прийшло
//     (нік, назва збірки, колір, статус), проходить санітизацію (sanitize.go):
//     керуючі символи й bidi-override вирізаються, довжини обмежені, колір —
//     лише #RRGGBB, статус — лише з білого списку. UI додатково не використовує
//     {@html}, тож навіть ненавмисний пропуск не стає XSS.
//  2. Подія presence про акаунт, якого НЕМАЄ у списку друзів, відкидається:
//     скомпрометований VPS не може підсунути «друга» чи засмітити пам'ять.
//  3. JWT живе лише в пам'яті Go-процесу. У UI він не потрапляє ніколи, у URL
//     не кладеться (тільки перший кадр WS), у лог не пишеться.
//  4. Лише wss://. Жодного InsecureSkipVerify. Перевірка сертифіката — типова.
//  5. Усі читання обмежені за розміром (HTTP 1 МіБ, WS-кадр 16 КіБ) і за часом.
package social

import "time"

// Статуси присутності. Усе поза цим списком від VPS відкидається.
const (
	StatusOnline  = "online"
	StatusAway    = "away" // лаунчер відкритий, але гравець відійшов
	StatusInGame  = "in_game"
	StatusOffline = "offline"
	// StatusInvisible — ЛОКАЛЬНИЙ стан власного віджета («Невидимий»). На VPS не шлеться:
	// друзі бачать мене офлайн (кадр left_game), а я лишаюсь підключеним до чатів.
	StatusInvisible = "invisible"
)

// Friend — друг із Worker (/friends/list) + живий стан із VPS.
type Friend struct {
	AccountID string   `json:"accountId"`
	Nickname  string   `json:"nickname"`
	Handle    string   `json:"handle,omitempty"` // @handle (без «@»); порожньо, якщо ще не виданий
	Bio       string   `json:"bio,omitempty"`    // візитка (≤ MaxBioRunes символів), уже очищена SanitizeBio
	Color     string   `json:"color,omitempty"`  // #RRGGBB колір фону профілю; порожньо — стандартний
	Since     int64    `json:"since"`            // unix ms
	Presence  Presence `json:"presence"`
}

// Presence — живий стан друга. Нульове значення = «офлайн / невідомо».
type Presence struct {
	Status    string `json:"status"`              // online | away | in_game | offline
	PackName  string `json:"packName,omitempty"`  // лише коли in_game
	PackColor string `json:"packColor,omitempty"` // #RRGGBB
	UpdatedAt int64  `json:"updatedAt"`           // unix ms, коли ми це отримали
	// LastSeenAt — unix ms, коли друг востаннє був у мережі (лише для
	// offline). 0 = невідомо: друг приховав статус, старий сервер або
	// друг ще жодного разу не заходив після оновлення. UI тоді нічого не пише.
	LastSeenAt int64 `json:"lastSeenAt,omitempty"`
}

// Invite — вхідний запит у друзі.
type Invite struct {
	ID            string `json:"id"`
	FromAccountID string `json:"fromAccountId"`
	FromNickname  string `json:"fromNickname"`
	FromHandle    string `json:"fromHandle,omitempty"`
	CreatedAt     int64  `json:"createdAt"`
}

// Privacy — перемикачі приватності, які стосуються САМЕ друзів (розділ 1.8
// плану). Поля чату/сповіщень (read_receipts, notif_*, autodelete) сюди не
// входять — вони з'являться разом із чатом, і Worker їх не втрачає: він
// оновлює лише ті поля, що прийшли (COALESCE).
type Privacy struct {
	ShowStatusTo  string `json:"showStatusTo"`   // everyone | friends | nobody
	ShowServerTo  string `json:"showServerTo"`   // everyone | friends | nobody
	FriendRequest string `json:"friendRequests"` // everyone | friends_of_friends | nobody
	// WhoCanMessage — хто може писати мені в чат: friends (типово) | everyone | nobody.
	// Порожнє значення (старий клієнт/сервер) трактується як friends.
	WhoCanMessage string `json:"whoCanMessage"`
}

// DefaultPrivacy — те, що Worker застосовує, коли рядка в presence_prefs ще
// немає (friends.js / friends-schema.sql). Тримати синхронно з ними.
func DefaultPrivacy() Privacy {
	return Privacy{ShowStatusTo: "friends", ShowServerTo: "friends", FriendRequest: "everyone", WhoCanMessage: "friends"}
}

// ConnState — стан з'єднання з VPS для індикатора в UI.
type ConnState string

const (
	ConnOff        ConnState = "off"        // не запущено (немає сесії / вимкнено)
	ConnConnecting ConnState = "connecting" // йде підключення або реконект
	ConnOnline     ConnState = "online"     // hello прийнято
)

// reauthEvery — змінна лише для тестів.
var reauthEvery = 8 * time.Minute

// Ліміти. Узгоджені з worker/friends.js (MAX_FRIENDS=500, NICKNAME_MAX_LEN=32).
const (
	MaxNicknameLen = 32
	MaxPackNameLen = 48
	MaxBioRunes    = 120
	MaxFriends     = 1001 // = MAX_STATE_FRIENDS у Worker
	maxHTTPBody    = 1 << 20
	// 256 КіБ: сторінка історії (до 50 повідомлень) приходить одним кадром.
	// Кадри presence/typing крихітні; VPS сам тримає history_page ≤ 240 КіБ.
	maxWSFrame = 256 << 10
	// reauthEvery — як часто шлемо новий hello з свіжим JWT. Access-токен живе
	// 15 хв, а VPS розриває сесію в момент його спливання; без оновлення кожне
	// розірвання = перепідключення + запит стану до Worker.
	historyTimeout    = 15 * time.Second
	httpTimeout       = 15 * time.Second
	wsDialTimeout     = 15 * time.Second
	wsWriteTimeout    = 10 * time.Second
	wsPingEvery       = 25 * time.Second
	wsHelloTimeout    = 10 * time.Second
	backoffMin        = 1 * time.Second
	backoffMax        = 60 * time.Second
	pendingTTL        = 60 * time.Second
	maxOutboundPerSec = 5 // VPS: 5 msg/s на з'єднання (план 1.5.4) — не перевищуємо
)
