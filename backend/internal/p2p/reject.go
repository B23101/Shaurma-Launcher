// reject.go — пояснення гравцеві, ЧОМУ його не пустили (замість «Connection reset»).
//
// ── ІДЕЯ ──────────────────────────────────────────────────────────────────
// Усе, що йде до P2P-сервера, спершу завершується в лаунчері самого гостя
// (127.0.0.2:25565), тож гра говорить саме з ним. Коли вхід неможливий,
// лаунчер гостя сам відповідає Minecraft-пакетом:
//   - вхід у гру (nextState 2) → Login Disconnect із причиною (екран відмови);
//   - список серверів (nextState 1) → Status Response з причиною в MOTD +
//     правильна відповідь на ping (замість сірого «Can't connect»).
//
// Текст формує лаунчер ГОСТЯ його мовою: хост може бути офлайн, а його мова
// гравцеві нічого не каже. VPS у цьому не бере участі — причину гість уже знає
// з відповіді /p2p/join, що й так робиться (нових запитів немає); якщо лежить
// сам VPS — гість теж це бачить і каже про це сам.
//
// ── ВІДМОВИ ХОСТА ─────────────────────────────────────────────────────────
// Коли відмовляє ХОСТ (квиток не прийнято, режим «лише друзі», світ не
// запущено), текст у тунель не пишеться: хост шле 7 байтів — маркер + код
// причини. Маркер — це VarInt-«довжина» -1, яку справжній Minecraft-сервер
// ніколи не надішле першою (клієнт відкидає від'ємну довжину), тож його не
// сплутати з грою. Гість упізнає маркер за першими байтами відповіді і
// сам формує текст СВОЄЮ мовою. Хост без підтримки просто закриває сокет —
// гість у такому разі скаже «Хост не відповідає».
//
// ── НАВАНТАЖЕННЯ ──────────────────────────────────────────────────────────
// Відповідь — кілька сотень байтів, збирається локально, пишеться з
// дедлайном і одразу закривається; одночасних відповідей не більше
// rejectMaxInflight (решта просто закриваються, як раніше).
//
// Файл залежить лише від stdlib — логіку можна перевіряти без tsnet/wails.
package p2p

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
)

// RejectReason — машинна причина відмови. Значення йдуть у тунель одним
// байтом (маркер хоста), тож порядок констант НЕ МІНЯТИ — лише дописувати
// в кінець перед rejMax.
type RejectReason uint8

const (
	RejNone             RejectReason = iota
	RejHostOffline                   // хост не в мережі
	RejHostOfflineAnon               // те саме, але гість без акаунта (кімната може бути «лише для акаунтів»)
	RejNotAllowed                    // власник не пускає (загальна відмова, зокрема чорний список)
	RejOnlyFriends                   // режим «лише друзі»
	RejOnlyList                      // режим «лише список»
	RejOnlyAccounts                  // режим «лише акаунти Шаурми»
	RejAccountRequired               // потрібен акаунт Шаурми
	RejTunnelDisabled                // тунель вимкнено адміном
	RejRateLimited                   // забагато спроб
	RejLobbyUnreachable              // сервер Шаурми (VPS) недоступний
	RejNetworkDown                   // не вдалося підняти власну P2P-мережу
	RejNoSlot                        // вичерпано слоти одночасних кімнат
	RejHostNoResponse                // хост не відповів / закрив з'єднання мовчки
	RejGameNotRunning                // у хоста не запущено світ/сервер
	RejTicketRejected                // хост не прийняв квиток доступу
	RejCheckUnavailable              // хост не зміг перевірити доступ (вузол недоступний)
	RejBadAddress                    // адреса введена не у форматі <код>.play.<суфікс>
	RejInternal                      // внутрішня помилка лаунчера
	rejMax                           // службова межа — завжди остання
)

// rejectKeys — ключі текстів (і ключі перекладу у фронтенді: p2p.rej.<ключ>).
var rejectKeys = [...]string{
	RejNone:             "",
	RejHostOffline:      "hostOffline",
	RejHostOfflineAnon:  "hostOfflineAnon",
	RejNotAllowed:       "notAllowed",
	RejOnlyFriends:      "onlyFriends",
	RejOnlyList:         "onlyList",
	RejOnlyAccounts:     "onlyAccounts",
	RejAccountRequired:  "accountRequired",
	RejTunnelDisabled:   "tunnelDisabled",
	RejRateLimited:      "rateLimited",
	RejLobbyUnreachable: "lobbyUnreachable",
	RejNetworkDown:      "networkDown",
	RejNoSlot:           "noSlot",
	RejHostNoResponse:   "hostNoResponse",
	RejGameNotRunning:   "gameNotRunning",
	RejTicketRejected:   "ticketRejected",
	RejCheckUnavailable: "checkUnavailable",
	RejBadAddress:       "badAddress",
	RejInternal:         "internal",
}

// RejectKey — стабільний ключ причини ("hostOffline", …); для логів і перекладів.
func RejectKey(r RejectReason) string {
	if r == RejNone || r >= rejMax {
		return ""
	}
	return rejectKeys[r]
}

// RejectKeys — усі ключі текстів (фронтенд за ними формує переклади).
func RejectKeys() []string {
	out := make([]string, 0, int(rejMax)-1)
	for r := RejHostOffline; r < rejMax; r++ {
		out = append(out, rejectKeys[r])
	}
	return out
}

// builtinRejectTexts — вбудовані тексти. Запасний варіант на випадок, коли
// фронтенд ще не передав переклад (лаунчер стартує згорнутим) або мова не
// має ключів. Мови, яких тут немає, отримують англійську.
var builtinRejectTexts = map[string]map[string]string{
	"uk": {
		"hostOffline":      "Хост зараз не в мережі. Спробуйте пізніше.",
		"hostOfflineAnon":  "Хост не в мережі, або сервер лише для акаунтів. Увійдіть в акаунт Шаурми і спробуйте ще раз.",
		"notAllowed":       "Власник сервера не пускає вас на цей сервер.",
		"onlyFriends":      "Хост пускає на цей сервер лише друзів і запрошених гравців.",
		"onlyList":         "Хост пускає на цей сервер лише гравців зі свого списку.",
		"onlyAccounts":     "Цей сервер лише для гравців з акаунтом Шаурми. Увійдіть в акаунт.",
		"accountRequired":  "Потрібен акаунт Шаурми: увійдіть в акаунт у лаунчері.",
		"tunnelDisabled":   "Цей P2P-тунель вимкнено адміністратором мережі.",
		"rateLimited":      "Забагато спроб підключення. Зачекайте кілька хвилин.",
		"lobbyUnreachable": "Сервер Шаурми зараз недоступний. Перевірте інтернет і спробуйте пізніше.",
		"networkDown":      "Не вдалося підняти P2P-мережу. Перевірте інтернет і спробуйте ще раз.",
		"noSlot":           "Забагато одночасних P2P-серверів. Вийдіть з одного й спробуйте знову.",
		"hostNoResponse":   "Хост не відповідає. Можливо, він закрив світ або втратив зв'язок.",
		"gameNotRunning":   "Світ або сервер хоста ще не запущено. Спробуйте трохи пізніше.",
		"ticketRejected":   "Хост не підтвердив ваш доступ. Зайдіть у кімнату ще раз.",
		"checkUnavailable": "Хост не зміг перевірити доступ. Спробуйте за хвилину.",
		"badAddress":       "Невірна P2P-адреса. Перевірте, чи ввели її повністю.",
		"internal":         "Внутрішня помилка лаунчера. Спробуйте ще раз.",
	},
	"en": {
		"hostOffline":      "The host is offline right now. Try again later.",
		"hostOfflineAnon":  "The host is offline, or this server is for account holders only. Sign in to your Shaurma account and try again.",
		"notAllowed":       "The server owner does not let you join this server.",
		"onlyFriends":      "The host only lets friends and invited players join this server.",
		"onlyList":         "The host only lets players from their own list join this server.",
		"onlyAccounts":     "This server is for Shaurma account holders only. Please sign in.",
		"accountRequired":  "A Shaurma account is required: sign in in the launcher.",
		"tunnelDisabled":   "This P2P tunnel was disabled by the network administrator.",
		"rateLimited":      "Too many connection attempts. Wait a few minutes.",
		"lobbyUnreachable": "The Shaurma server is unreachable right now. Check your internet and try later.",
		"networkDown":      "Could not start the P2P network. Check your internet and try again.",
		"noSlot":           "Too many P2P servers open at once. Leave one and try again.",
		"hostNoResponse":   "The host is not responding. They may have closed the world or lost connection.",
		"gameNotRunning":   "The host's world or server is not running yet. Try again in a moment.",
		"ticketRejected":   "The host did not confirm your access. Join the room again.",
		"checkUnavailable": "The host could not verify your access. Try again in a minute.",
		"badAddress":       "Invalid P2P address. Check that you typed it in full.",
		"internal":         "Internal launcher error. Please try again.",
	},
}

// maxRejectTexts — скільки ключів приймаємо від фронтенду за раз (ключів у нас 18).
const maxRejectTexts = 64

var (
	rejMu       sync.RWMutex
	rejLang     = "en"
	rejOverride map[string]string
)

// SetRejectLanguage вибирає вбудований словник за кодом мови лаунчера
// ("uk", "uk_ua", "en", "auto", імпортована мова…): українська → uk, решта → en.
// Викликається при старті й зміні налаштувань; переклади фронтенду (SetRejectTexts)
// мають пріоритет над вбудованими.
func SetRejectLanguage(lang string) {
	l := "en"
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(lang)), "uk") {
		l = "uk"
	}
	rejMu.Lock()
	rejLang = l
	rejMu.Unlock()
}

// SetRejectTexts приймає переклад від фронтенду: ключ → текст на поточній мові
// лаунчера (разом з імпортованими мовами). Невідомі ключі й порожні тексти
// відкидаються; nil скидає переклад до вбудованого.
func SetRejectTexts(texts map[string]string) {
	var clean map[string]string
	if len(texts) > 0 && len(texts) <= maxRejectTexts {
		known := map[string]struct{}{}
		for _, k := range RejectKeys() {
			known[k] = struct{}{}
		}
		clean = make(map[string]string, len(texts))
		for k, v := range texts {
			if _, ok := known[k]; !ok {
				continue
			}
			if v = sanitizeRejectText(v); v != "" {
				clean[k] = v
			}
		}
	}
	rejMu.Lock()
	rejOverride = clean
	rejMu.Unlock()
}

// RejectText — текст причини поточною мовою лаунчера.
func RejectText(r RejectReason) string {
	key := RejectKey(r)
	if key == "" {
		key = rejectKeys[RejInternal]
	}
	rejMu.RLock()
	defer rejMu.RUnlock()
	if v, ok := rejOverride[key]; ok {
		return v
	}
	if v, ok := builtinRejectTexts[rejLang][key]; ok {
		return v
	}
	return builtinRejectTexts["en"][key]
}

// sanitizeRejectText прибирає керівні символи й обмежує довжину (екран відмови
// не повинен розпухати від чужого/зіпсованого перекладу).
func sanitizeRejectText(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return ' '
		case r == '§': // легасі-коди форматування Minecraft: текст має бути «просто текстом»
			return -1
		case unicode.IsControl(r):
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if rs := []rune(s); len(rs) > 240 {
		s = strings.TrimSpace(string(rs[:240])) + "…"
	}
	return s
}

// ── Minecraft-пакети ──────────────────────────────────────────────────────

const rejectTitle = "Shaurma P2P"

func appendVarint(b []byte, v int) []byte {
	u := uint32(v)
	for {
		if u&^0x7F == 0 {
			return append(b, byte(u))
		}
		b = append(b, byte(u&0x7F)|0x80)
		u >>= 7
	}
}

func appendMCString(b []byte, s string) []byte {
	b = appendVarint(b, len(s))
	return append(b, s...)
}

func mcFrame(payload []byte) []byte {
	out := appendVarint(make([]byte, 0, len(payload)+5), len(payload))
	return append(out, payload...)
}

type chatPart struct {
	Text  string `json:"text"`
	Color string `json:"color,omitempty"`
	Bold  bool   `json:"bold,omitempty"`
}

type chatComp struct {
	Text  string     `json:"text"`
	Extra []chatPart `json:"extra,omitempty"`
}

// buildLoginDisconnect — пакет Login Disconnect (стан Login, id 0x00, JSON-чат).
// Однаковий для всіх версій від 1.7.
func buildLoginDisconnect(text string) []byte {
	js, _ := json.Marshal(chatComp{Extra: []chatPart{
		{Text: rejectTitle + "\n", Color: "gold", Bold: true},
		{Text: text, Color: "white"},
	}})
	return mcFrame(appendMCString([]byte{0x00}, string(js)))
}

// buildStatusResponse — Status Response (id 0x00) з причиною в MOTD. protocol —
// версія клієнта з хендшейку: тоді гра не додає червоне «Застарілий клієнт».
func buildStatusResponse(protocol int64, text string) []byte {
	if rs := []rune(text); len(rs) > 120 {
		text = string(rs[:120]) + "…"
	}
	js, _ := json.Marshal(map[string]any{
		"version": map[string]any{"name": rejectTitle, "protocol": protocol},
		"players": map[string]any{"max": 0, "online": 0},
		"description": chatComp{Extra: []chatPart{
			{Text: "● ", Color: "red"},
			{Text: text, Color: "gray"},
		}},
	})
	return mcFrame(appendMCString([]byte{0x00}, string(js)))
}

func buildPong(payload []byte) []byte {
	return mcFrame(append([]byte{0x01}, payload...))
}

// readVarintFrom читає VarInt побайтово (без буфера — щоб не з'їсти зайвого).
func readVarintFrom(r io.Reader) (int, error) {
	var val uint32
	var one [1]byte
	for i := 0; i < 5; i++ {
		if _, err := io.ReadFull(r, one[:]); err != nil {
			return 0, err
		}
		val |= uint32(one[0]&0x7F) << (7 * i)
		if one[0]&0x80 == 0 {
			return int(int32(val)), nil
		}
	}
	return 0, errors.New("p2p: varint завеликий")
}

var errBadFrame = errors.New("p2p: некоректний фрейм")

// readFrame читає один Minecraft-фрейм. raw — з префіксом довжини (для
// пересилання «як є»), body — без нього.
func readFrame(r io.Reader, maxLen int) (raw, body []byte, err error) {
	n, err := readVarintFrom(r)
	if err != nil {
		return nil, nil, err
	}
	if n <= 0 || n > maxLen {
		return nil, nil, errBadFrame
	}
	body = make([]byte, n)
	if _, err = io.ReadFull(r, body); err != nil {
		return nil, nil, err
	}
	raw = append(appendVarint(make([]byte, 0, n+5), n), body...)
	return raw, body, nil
}

// ── Відповідь клієнту ─────────────────────────────────────────────────────

const (
	rejectIOTimeout   = 4 * time.Second
	rejectMaxInflight = 128 // відповіді гравцеві (лише локальні з'єднання з 127.0.0.2)
	hostRejectMax     = 32  // відповіді хоста гостям із мережі — окремий, менший ліміт
)

// Два незалежні ліміти: сторонній пір не може вичерпати ліміт локальних відповідей
// власному гравцеві (і навпаки). Коли ліміт вичерпано — з'єднання просто закривається, як було раніше.
var (
	rejectSem     = make(chan struct{}, rejectMaxInflight)
	hostRejectSem = make(chan struct{}, hostRejectMax)
)

func rejectAcquire() bool {
	select {
	case rejectSem <- struct{}{}:
		return true
	default:
		return false
	}
}

func rejectRelease() { <-rejectSem }

func hostRejectAcquire() bool {
	select {
	case hostRejectSem <- struct{}{}:
		return true
	default:
		return false
	}
}

func hostRejectRelease() { <-hostRejectSem }

// hostMarkerAllowed — які причини взагалі може повідомляти ХОСТ. Хост — стороння сторона: код від нього
// лише ВИБИРАЄ один зі СВОЇХ текстів гостя (ніколи не текст), і то лише з цього набору. Усе інше
// («внутрішня помилка лаунчера», «увійдіть в акаунт», …) хост нав'язувати гравцеві не може.
func hostMarkerAllowed(r RejectReason) bool {
	switch r {
	case RejNotAllowed, RejOnlyFriends, RejOnlyList, RejOnlyAccounts,
		RejGameNotRunning, RejTicketRejected, RejCheckUnavailable:
		return true
	}
	return false
}

func halfClose(c net.Conn) {
	if cw, ok := c.(interface{ CloseWrite() error }); ok {
		_ = cw.CloseWrite()
	}
}

// lingerClose: закриваємо запис і ненадовго дочитуємо вхідне, інакше ядро
// відповість RST (на вхідних даних, яких ми не прочитали) і клієнт може
// втратити наш пакет ще до його показу.
func lingerClose(c net.Conn) {
	halfClose(c)
	_ = c.SetReadDeadline(time.Now().Add(time.Second))
	_, _ = io.Copy(io.Discard, io.LimitReader(c, 8192))
}

// serveStatus відповідає на пінг списку серверів: Status Response + Pong.
// requestSeen — Status Request клієнта вже прочитано (і переслано) кимось іншим.
func serveStatus(c net.Conn, protocol int64, text string, requestSeen bool) {
	_ = c.SetDeadline(time.Now().Add(rejectIOTimeout))
	wrote := false
	if requestSeen {
		if _, err := c.Write(buildStatusResponse(protocol, text)); err != nil {
			return
		}
		wrote = true
	}
	for i := 0; i < 3; i++ {
		_, body, err := readFrame(c, 64)
		if err != nil {
			return
		}
		switch body[0] {
		case 0x00: // Status Request
			if !wrote {
				if _, err := c.Write(buildStatusResponse(protocol, text)); err != nil {
					return
				}
				wrote = true
			}
		case 0x01: // Ping → Pong з тим самим числом
			if !wrote {
				if _, err := c.Write(buildStatusResponse(protocol, text)); err != nil {
					return
				}
			}
			if len(body) >= 9 {
				_, _ = c.Write(buildPong(body[1:9]))
			}
			return
		default:
			return
		}
	}
}

// writeReject пише відповідь і НЕ чекає/не закриває (для місць, де з'єднання
// далі веде інша горутина). Повертає false, якщо відповісти не вдалося.
func writeReject(c net.Conn, protocol, nextState int64, reason RejectReason, requestSeen bool) bool {
	if reason == RejNone || !rejectAcquire() {
		return false
	}
	defer rejectRelease()
	text := RejectText(reason)
	if nextState == 1 {
		serveStatus(c, protocol, text, requestSeen)
		return true
	}
	_ = c.SetWriteDeadline(time.Now().Add(rejectIOTimeout))
	_, err := c.Write(buildLoginDisconnect(text))
	return err == nil
}

// RejectClient відповідає клієнту причиною відмови й м'яко завершує
// з'єднання (lingerClose). Закриття сокета лишається за викликачем.
func RejectClient(c net.Conn, protocol, nextState int64, reason RejectReason, requestSeen bool) bool {
	if reason == RejNone || !rejectAcquire() {
		return false
	}
	defer rejectRelease()
	text := RejectText(reason)
	if nextState == 1 {
		serveStatus(c, protocol, text, requestSeen)
		lingerClose(c)
		return true
	}
	_ = c.SetWriteDeadline(time.Now().Add(rejectIOTimeout))
	_, err := c.Write(buildLoginDisconnect(text))
	lingerClose(c)
	return err == nil
}

// ── Відмова хоста: маркер у тунелі ────────────────────────────────────────

// hostRejectMagic — 5 байтів 0xFF + 0x0F = VarInt −1 (від'ємна довжина фрейму):
// справжній Minecraft-сервер такого першим не шле, клієнт би його відкинув.
var hostRejectMagic = [6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x0F}

const hostRejectLen = len(hostRejectMagic) + 1

func hostRejectMarker(r RejectReason) []byte {
	return append(append(make([]byte, 0, hostRejectLen), hostRejectMagic[:]...), byte(r))
}

// SendHostReject — хост повідомляє гостя про причину відмови й закриває запис.
// Закриття сокета лишається за викликачем (defer guest.Close()).
func SendHostReject(c net.Conn, r RejectReason) bool {
	if !hostMarkerAllowed(r) || !hostRejectAcquire() {
		return false
	}
	defer hostRejectRelease()
	_ = c.SetWriteDeadline(time.Now().Add(rejectIOTimeout / 2))
	_, err := c.Write(hostRejectMarker(r))
	lingerClose(c)
	return err == nil
}

// sniffHostReject дивиться на ПЕРШІ байти відповіді хоста, нічого не з'їдаючи,
// якщо це не маркер. marker=true — маркер знято з потоку. err != nil — хост
// закрив/обірвав з'єднання, не надіславши жодного байта.
func sniffHostReject(br *bufio.Reader) (reason RejectReason, marker bool, err error) {
	first, err := br.Peek(1)
	if err != nil {
		return RejNone, false, err
	}
	if first[0] != hostRejectMagic[0] {
		return RejNone, false, nil
	}
	head, err := br.Peek(hostRejectLen)
	if err != nil || string(head[:len(hostRejectMagic)]) != string(hostRejectMagic[:]) {
		return RejNone, false, nil // схоже на великий легітимний фрейм — віддаємо як є
	}
	reason = RejectReason(head[len(hostRejectMagic)])
	if !hostMarkerAllowed(reason) {
		// Невідомий або недозволений для хоста код: показуємо нейтральне «хост не відповідає».
		reason = RejHostNoResponse
	}
	_, _ = br.Discard(hostRejectLen)
	return reason, true, nil
}

// rejectSniffConn — обгортка над з'єднанням до хоста/кімнати: перше читання
// перевіряє маркер відмови. Якщо це маркер (або хост мовчки закрив) —
// викликає onReject (той пише клієнту Minecraft-пакет) і віддає EOF, далі
// звичайний pipe штатно завершується. Інакше байти йдуть як є.
type rejectSniffConn struct {
	net.Conn
	br       *bufio.Reader
	onReject func(RejectReason)
	checked  bool // лише з горутини читання
	rejected atomic.Bool
}

func newRejectSniffConn(c net.Conn, onReject func(RejectReason)) *rejectSniffConn {
	return &rejectSniffConn{Conn: c, br: bufio.NewReaderSize(c, 4096), onReject: onReject}
}

// Rejected — чи було відправлено відмову клієнту.
func (c *rejectSniffConn) Rejected() bool { return c.rejected.Load() }

func (c *rejectSniffConn) Read(p []byte) (int, error) {
	if !c.checked {
		c.checked = true
		reason, marker, err := sniffHostReject(c.br)
		switch {
		case marker:
			c.rejected.Store(true)
			c.onReject(reason)
			return 0, io.EOF
		case err != nil && !errors.Is(err, net.ErrClosed):
			c.rejected.Store(true)
			c.onReject(RejHostNoResponse)
			return 0, io.EOF
		}
	}
	return c.br.Read(p)
}

func (c *rejectSniffConn) CloseWrite() error {
	if cw, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite()
	}
	return nil
}

// rejectForMode — причина для відмови хоста за режимом доступу його кімнати.
func rejectForMode(mode string) RejectReason {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "friends":
		return RejOnlyFriends
	case "list":
		return RejOnlyList
	}
	return RejTicketRejected
}
