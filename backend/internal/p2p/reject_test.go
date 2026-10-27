package p2p

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"math/rand"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// ── допоміжне: мінімальний «клієнт Minecraft» ──────────────────────────────

func readMCPacket(t *testing.T, r io.Reader) (id byte, body []byte) {
	t.Helper()
	_, b, err := readFrame(r, 1<<20)
	if err != nil {
		t.Fatalf("readFrame: %v", err)
	}
	return b[0], b[1:]
}

func readMCString(t *testing.T, b []byte) string {
	t.Helper()
	n, err := readVarintFrom(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	hdr := len(appendVarint(nil, n))
	return string(b[hdr : hdr+n])
}

func plainText(js string) string {
	var c struct {
		Text  string `json:"text"`
		Extra []struct {
			Text string `json:"text"`
		} `json:"extra"`
	}
	_ = json.Unmarshal([]byte(js), &c)
	out := c.Text
	for _, e := range c.Extra {
		out += e.Text
	}
	return out
}

func withTexts(t *testing.T, lang string, texts map[string]string) {
	t.Helper()
	SetRejectLanguage(lang)
	SetRejectTexts(texts)
	t.Cleanup(func() { SetRejectTexts(nil); SetRejectLanguage("en") })
}

// ── пакети ─────────────────────────────────────────────────────────────────

func TestLoginDisconnectIsWellFormed(t *testing.T) {
	withTexts(t, "uk_ua", nil)
	pkt := buildLoginDisconnect(RejectText(RejOnlyFriends))
	id, body := readMCPacket(t, bytes.NewReader(pkt))
	if id != 0x00 {
		t.Fatalf("id=%d", id)
	}
	js := readMCString(t, body)
	if !json.Valid([]byte(js)) {
		t.Fatalf("не JSON: %s", js)
	}
	txt := plainText(js)
	if !strings.Contains(txt, "лише друзів") || !strings.Contains(txt, rejectTitle) {
		t.Fatalf("текст: %q", txt)
	}
}

func TestStatusResponseEchoesProtocolAndPong(t *testing.T) {
	withTexts(t, "en", nil)
	cli, srv := net.Pipe()
	defer cli.Close()
	go func() {
		defer srv.Close()
		serveStatus(srv, 767, RejectText(RejHostOffline), false)
	}()
	_ = cli.SetDeadline(time.Now().Add(3 * time.Second))
	_, _ = cli.Write([]byte{0x01, 0x00}) // Status Request
	id, body := readMCPacket(t, cli)
	if id != 0x00 {
		t.Fatalf("id=%d", id)
	}
	var st struct {
		Version struct {
			Protocol int64 `json:"protocol"`
		} `json:"version"`
	}
	if err := json.Unmarshal([]byte(readMCString(t, body)), &st); err != nil || st.Version.Protocol != 767 {
		t.Fatalf("protocol=%d err=%v", st.Version.Protocol, err)
	}
	ping := append([]byte{0x09, 0x01}, make([]byte, 8)...)
	binary.BigEndian.PutUint64(ping[2:], 0xDEADBEEF12345678)
	_, _ = cli.Write(ping)
	id, body = readMCPacket(t, cli)
	if id != 0x01 || binary.BigEndian.Uint64(body) != 0xDEADBEEF12345678 {
		t.Fatalf("pong id=%d body=%x", id, body)
	}
}

func TestStatusAfterRequestSeen(t *testing.T) {
	withTexts(t, "en", nil)
	cli, srv := net.Pipe()
	defer cli.Close()
	go func() { defer srv.Close(); serveStatus(srv, 5, "x", true) }()
	_ = cli.SetDeadline(time.Now().Add(3 * time.Second))
	if id, _ := readMCPacket(t, cli); id != 0x00 {
		t.Fatalf("очікували Status Response без запиту, id=%d", id)
	}
}

func TestRejectClientLoginOverTCP(t *testing.T) {
	withTexts(t, "en", nil)
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	go func() {
		c, _ := ln.Accept()
		defer c.Close()
		RejectClient(c, 767, 2, RejHostOffline, false)
	}()
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	// Клієнт шле Login Start, який сервер не читає: без lingerClose це дало б RST і втрату пакета.
	_, _ = c.Write(append([]byte{0x10, 0x00, 0x08}, []byte("Steve123 ")...))
	_ = c.SetDeadline(time.Now().Add(3 * time.Second))
	id, body := readMCPacket(t, c)
	if id != 0x00 || !strings.Contains(plainText(readMCString(t, body)), "offline") {
		t.Fatalf("id=%d body=%q", id, body)
	}
}

// ── безпека тексту ────────────────────────────────────────────────────────

func TestTextIsDataNotMarkup(t *testing.T) {
	// Користувач може імпортувати власну мову; у ній не повинно бути способу додати
	// у відповідь клік-події, команди чи формат-коди — лише «текст».
	evil := `"},"clickEvent":{"action":"run_command","value":"/op me"},"x":{"y":"§4§lЧервоне` + "\x00\x07"
	withTexts(t, "en", map[string]string{"hostOffline": evil})
	js := readMCString(t, func() []byte {
		_, b := readMCPacket(t, bytes.NewReader(buildLoginDisconnect(RejectText(RejHostOffline))))
		return b
	}())
	var generic map[string]any
	if err := json.Unmarshal([]byte(js), &generic); err != nil {
		t.Fatalf("JSON зламано ін'єкцією: %v\n%s", err, js)
	}
	if strings.Contains(js, "clickEvent\":{") {
		t.Fatalf("clickEvent потрапив у структуру: %s", js)
	}
	if strings.ContainsRune(RejectText(RejHostOffline), '§') || strings.ContainsAny(RejectText(RejHostOffline), "\x00\x07") {
		t.Fatalf("керівні символи/§ не вирізано: %q", RejectText(RejHostOffline))
	}
}

func TestSetRejectTextsBounds(t *testing.T) {
	withTexts(t, "uk", nil)
	long := strings.Repeat("я", 5000)
	SetRejectTexts(map[string]string{"hostOffline": long, "unknownKey": "x", "notAllowed": "   "})
	if n := len([]rune(RejectText(RejHostOffline))); n > 241 {
		t.Fatalf("текст не обмежено: %d", n)
	}
	if RejectText(RejNotAllowed) != builtinRejectTexts["uk"]["notAllowed"] {
		t.Fatalf("порожній переклад мав лишити вбудований")
	}
	big := map[string]string{}
	for i := 0; i < 1000; i++ {
		big[string(rune('a'+i%26))+strings.Repeat("k", i)] = "x"
	}
	SetRejectTexts(big) // забагато ключів → відкинути все
	if RejectText(RejHostOffline) != builtinRejectTexts["uk"]["hostOffline"] {
		t.Fatalf("надто великий набір мав бути проігноровано")
	}
}

func TestBuiltinTextsComplete(t *testing.T) {
	for _, lang := range []string{"uk", "en"} {
		for _, k := range RejectKeys() {
			if strings.TrimSpace(builtinRejectTexts[lang][k]) == "" {
				t.Errorf("немає тексту %s/%s", lang, k)
			}
		}
	}
	for r := RejHostOffline; r < rejMax; r++ {
		if RejectKey(r) == "" {
			t.Errorf("немає ключа для %d", r)
		}
	}
	if RejectKey(rejMax) != "" || RejectKey(RejNone) != "" {
		t.Error("службові значення мають давати порожній ключ")
	}
}

// ── маркер відмови хоста ──────────────────────────────────────────────────

func sniff(b []byte) (RejectReason, bool, error) {
	return sniffHostReject(bufio.NewReader(bytes.NewReader(b)))
}

func TestHostMarkerRoundTrip(t *testing.T) {
	for _, want := range []RejectReason{RejOnlyFriends, RejOnlyList, RejGameNotRunning, RejTicketRejected, RejCheckUnavailable} {
		got, ok, err := sniff(hostRejectMarker(want))
		if err != nil || !ok || got != want {
			t.Fatalf("want %v got %v ok=%v err=%v", want, got, ok, err)
		}
	}
}

func TestHostCannotForceArbitraryReason(t *testing.T) {
	// Хост — стороння сторона. Він може лише вибрати з дозволеного набору; будь-що інше
	// (внутрішня помилка, «увійдіть в акаунт», невідомий байт) стає нейтральним «не відповідає».
	for _, bad := range []RejectReason{RejInternal, RejAccountRequired, RejHostOfflineAnon, RejBadAddress, RejNetworkDown, RejectReason(0), RejectReason(200), RejectReason(255)} {
		got, ok, _ := sniff(hostRejectMarker(bad))
		if !ok || got != RejHostNoResponse {
			t.Fatalf("код %d → %v ok=%v, очікували HostNoResponse", bad, got, ok)
		}
	}
	// І надіслати їх сам лаунчер-хост теж не зможе.
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	if SendHostReject(a, RejInternal) {
		t.Fatal("SendHostReject мав відмовитись від недозволеного коду")
	}
}

func TestSniffDoesNotEatRealTraffic(t *testing.T) {
	// Звичайний пакет сервера (Status/Login) не повинен зачіпатись.
	real := buildLoginDisconnect("hello")
	_, ok, err := sniff(real)
	if ok || err != nil {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	br := bufio.NewReader(bytes.NewReader(real))
	_, _, _ = sniffHostReject(br)
	got, _ := io.ReadAll(br)
	if !bytes.Equal(got, real) {
		t.Fatal("sniff з'їв байти легітимного пакета")
	}
	// Легітимний фрейм, що ПОЧИНАЄТЬСЯ з 0xFF, але не є маркером.
	big := append([]byte{0xFF, 0x7F}, bytes.Repeat([]byte{1}, 20000)...)
	br = bufio.NewReader(bytes.NewReader(big))
	if _, ok, _ := sniffHostReject(br); ok {
		t.Fatal("великий фрейм сприйнято за маркер")
	}
	got, _ = io.ReadAll(br)
	if !bytes.Equal(got, big) {
		t.Fatal("великий фрейм спотворено")
	}
}

func TestSniffPartialAndGarbage(t *testing.T) {
	// Обрізаний маркер, порожній потік, випадкове сміття — жодної паніки.
	if _, ok, _ := sniff(hostRejectMagic[:4]); ok {
		t.Fatal("обрізаний маркер")
	}
	if _, ok, err := sniff(nil); ok || err == nil {
		t.Fatal("порожній потік має дати помилку")
	}
	rnd := rand.New(rand.NewSource(1))
	for i := 0; i < 5000; i++ {
		b := make([]byte, rnd.Intn(64))
		rnd.Read(b)
		if len(b) > 0 && rnd.Intn(2) == 0 {
			copy(b, hostRejectMagic[:])
		}
		sniff(b)
	}
}

func TestReadFrameRejectsHostileInput(t *testing.T) {
	cases := [][]byte{
		{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x0F}, // довжина −1
		{0xFF, 0xFF, 0xFF, 0xFF, 0x7F},       // величезна
		{0x80, 0x80, 0x80, 0x80, 0x80, 0x80}, // нескінченне продовження
		{0x00},                               // нульова довжина
		{0x41},                               // довжина 65 > ліміт 64
		{0x05, 0x01},                         // обрізаний
	}
	for i, c := range cases {
		if _, _, err := readFrame(bytes.NewReader(c), 64); err == nil {
			t.Errorf("кейс %d: очікували помилку", i)
		}
	}
	rnd := rand.New(rand.NewSource(2))
	for i := 0; i < 5000; i++ {
		b := make([]byte, rnd.Intn(80))
		rnd.Read(b)
		readFrame(bytes.NewReader(b), 64)
	}
}

func TestServeStatusSurvivesGarbageClient(t *testing.T) {
	rnd := rand.New(rand.NewSource(3))
	for i := 0; i < 40; i++ {
		cli, srv := net.Pipe()
		done := make(chan struct{})
		go func() { defer close(done); serveStatus(srv, 1, "x", false); srv.Close() }()
		b := make([]byte, rnd.Intn(40))
		rnd.Read(b)
		go func() { cli.Write(b); cli.Close() }()
		select {
		case <-done:
		case <-time.After(6 * time.Second):
			t.Fatal("serveStatus завис на сміттєвому клієнті")
		}
	}
}

// ── sniff-обгортка між хостом і грою ──────────────────────────────────────

func TestSniffConnMarkerSilentAndPassThrough(t *testing.T) {
	run := func(serverBytes []byte, closeSilently bool) (reasons []RejectReason, got []byte) {
		a, b := net.Pipe()
		go func() {
			if len(serverBytes) > 0 {
				b.Write(serverBytes)
			}
			if closeSilently || len(serverBytes) > 0 {
				b.Close()
			}
		}()
		sc := newRejectSniffConn(a, func(r RejectReason) { reasons = append(reasons, r) })
		got, _ = io.ReadAll(sc)
		a.Close()
		return
	}
	if r, got := run(hostRejectMarker(RejOnlyFriends), false); len(r) != 1 || r[0] != RejOnlyFriends || len(got) != 0 {
		t.Fatalf("маркер: %v %q", r, got)
	}
	if r, got := run(nil, true); len(r) != 1 || r[0] != RejHostNoResponse || len(got) != 0 {
		t.Fatalf("мовчазне закриття: %v %q", r, got)
	}
	payload := buildLoginDisconnect("server says hi")
	if r, got := run(payload, false); len(r) != 0 || !bytes.Equal(got, payload) {
		t.Fatalf("прозорий режим: %v", r)
	}
}

// ── ліміти ────────────────────────────────────────────────────────────────

func TestRejectLimitsAreIndependentAndBounded(t *testing.T) {
	var held int
	for i := 0; i < hostRejectMax; i++ {
		if !hostRejectAcquire() {
			t.Fatalf("слот %d має бути вільним", i)
		}
		held++
	}
	if hostRejectAcquire() {
		t.Fatal("ліміт хоста не діє")
	}
	// Локальні відповіді гравцеві не залежать від зайнятості хостових слотів.
	if !rejectAcquire() {
		t.Fatal("ліміт локальних відповідей залежить від хостового")
	}
	rejectRelease()
	for ; held > 0; held-- {
		hostRejectRelease()
	}
}

// ── резолв: зливання запитів і кеш ────────────────────────────────────────

func TestResolveGateCoalescesAndCaches(t *testing.T) {
	var g resolveGate
	var calls int
	var mu sync.Mutex
	gate := make(chan struct{})
	fn := func() (*RoomInfo, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		<-gate
		return nil, errors.New("offline")
	}
	ttl := func(error) time.Duration { return time.Minute }
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); g.do("a", resolveFresh, ttl, fn) }()
	}
	time.Sleep(50 * time.Millisecond)
	close(gate)
	wg.Wait()
	if calls != 1 {
		t.Fatalf("50 паралельних запитів дали %d викликів до VPS", calls)
	}
	// Пінг тепер бере з негативного кешу — без нового виклику.
	_, err := g.do("a", resolvePing, ttl, func() (*RoomInfo, error) { calls++; return &RoomInfo{}, nil })
	if err == nil || calls != 1 {
		t.Fatalf("пінг мав узяти вердикт із кешу (calls=%d err=%v)", calls, err)
	}
	// Реальний вхід кеш ПРОПУСКАЄ — гравець не чекає, поки застаріє вердикт пінгу.
	info, err := g.do("a", resolveFresh, ttl, func() (*RoomInfo, error) { calls++; return &RoomInfo{HostPort: "25565"}, nil })
	if err != nil || info.HostPort != "25565" || calls != 2 {
		t.Fatalf("вхід мав піти до VPS: calls=%d err=%v", calls, err)
	}
	// Успіх стирає негативний запис.
	if _, err := g.do("a", resolvePing, ttl, func() (*RoomInfo, error) { t.Fatal("мав бути позитивний кеш"); return nil, nil }); err != nil {
		t.Fatalf("після успіху пінг має бути успішним: %v", err)
	}
}

func TestResolveGateNegativeCacheExpires(t *testing.T) {
	var g resolveGate
	now := time.Unix(1000, 0)
	g.now = func() time.Time { return now }
	calls := 0
	fn := func() (*RoomInfo, error) { calls++; return nil, errors.New("x") }
	ttl := func(error) time.Duration { return 15 * time.Second }
	g.do("k", resolvePing, ttl, fn)
	g.do("k", resolvePing, ttl, fn)
	if calls != 1 {
		t.Fatalf("calls=%d", calls)
	}
	now = now.Add(16 * time.Second)
	g.do("k", resolvePing, ttl, fn)
	if calls != 2 {
		t.Fatalf("кеш мав застаріти, calls=%d", calls)
	}
}

func TestResolveGatePingFloodIsCapped(t *testing.T) {
	// Локальний процес засипає лаунчер РІЗНИМИ адресами: до VPS піде не більше resolvePingMaxFlights
	// паралельних запитів, решта відсікається без звернення до VPS.
	var g resolveGate
	block := make(chan struct{})
	var mu sync.Mutex
	inflight, peak := 0, 0
	fn := func() (*RoomInfo, error) {
		mu.Lock()
		inflight++
		if inflight > peak {
			peak = inflight
		}
		mu.Unlock()
		<-block
		mu.Lock()
		inflight--
		mu.Unlock()
		return nil, errors.New("x")
	}
	ttl := func(error) time.Duration { return 0 }
	var wg sync.WaitGroup
	busy := 0
	var bm sync.Mutex
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := g.do(strings.Repeat("k", i+1), resolvePing, ttl, fn)
			if errors.Is(err, errResolveBusy) {
				bm.Lock()
				busy++
				bm.Unlock()
			}
		}(i)
	}
	time.Sleep(200 * time.Millisecond)
	close(block)
	wg.Wait()
	if peak > resolvePingMaxFlights {
		t.Fatalf("паралельних запитів до VPS: %d (ліміт %d)", peak, resolvePingMaxFlights)
	}
	if busy == 0 {
		t.Fatal("надлишок не було відсічено")
	}
}

func TestResolveGatePanicDoesNotHangWaiters(t *testing.T) {
	var g resolveGate
	done := make(chan struct{})
	go func() {
		g.do("p", resolveFresh, func(error) time.Duration { return 0 }, func() (*RoomInfo, error) { panic("boom") })
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("паніка в резолві повісила запит")
	}
}

// ── гейт хоста: коди причин ───────────────────────────────────────────────

func gateConn(preamble string) (net.Conn, net.Conn) {
	a, b := net.Pipe()
	go func() { b.Write([]byte(preamble)); b.Close() }()
	return a, b
}

func TestGateReasonsAndSilenceOnGarbage(t *testing.T) {
	deny := func(string) (AdmitResult, error) { return AdmitResult{OK: false}, nil }
	down := func(string) (AdmitResult, error) { return AdmitResult{}, errors.New("vps down") }

	for _, tc := range []struct {
		name  string
		admit AdmitFunc
		mode  string
		want  RejectReason
	}{
		{"друзі", deny, "friends", RejOnlyFriends},
		{"список", deny, "list", RejOnlyList},
		{"акаунти", deny, "accounts", RejTicketRejected},
		{"режим невідомий", deny, "", RejTicketRejected},
		{"вузол недоступний", down, "friends", RejCheckUnavailable},
	} {
		g := NewP2PGate(tc.admit)
		mode := tc.mode
		g.Mode = func() string { return mode }
		a, b := gateConn("SHP2P1 " + strings.Repeat("ab", 32) + "\n")
		_, _, ok, reason := g.AcceptReason(a)
		a.Close()
		b.Close()
		if ok || reason != tc.want {
			t.Errorf("%s: ok=%v reason=%v, очікували %v", tc.name, ok, reason, tc.want)
		}
	}

	// Без коректної преамбули — повна тиша (жодної відповіді сканеру/чужому клієнту).
	g := NewP2PGate(deny)
	for _, junk := range []string{"GET / HTTP/1.1\r\n\r\n", "\x00\x01\x02", "SHP2P1 \n", "SHP2P1 " + strings.Repeat("A", 5000) + "\n"} {
		a, b := gateConn(junk)
		_ = a.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
		_, _, ok, reason := g.AcceptReason(a)
		a.Close()
		b.Close()
		if ok || reason != RejNone {
			t.Errorf("сміття %.12q → ok=%v reason=%v (має бути тиша)", junk, ok, reason)
		}
	}
}
