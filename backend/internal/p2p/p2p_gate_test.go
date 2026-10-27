package p2p

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestP2PPreamble(t *testing.T) {
	ticket := strings.Repeat("b", 64)
	check := func(name string, data []byte, wantOK bool) {
		t.Helper()
		a, b := net.Pipe()
		go func() { _, _ = a.Write(data); a.Close() }()
		got, r, ok := readP2PPreamble(b)
		if ok != wantOK {
			t.Fatalf("%s: ok=%v", name, ok)
		}
		if ok {
			rest := make([]byte, 4)
			n, _ := r.Read(rest)
			if got != ticket || string(rest[:n]) != "\x10MC!"[:n] {
				t.Fatalf("%s: квиток/залишок: %q %q", name, got, rest[:n])
			}
		}
		b.Close()
	}
	check("валідна", append(P2PPreamble(ticket), []byte("\x10MC!")...), true)
	check("преамбула tinvite", Preamble(strings.Repeat("a", 32), ticket), false)
	check("квиток не hex", []byte(p2pPreambleMagic+strings.Repeat("z", 64)+"\n"), false)
	check("без \\n", []byte(p2pPreambleMagic+ticket+"x"), false)
	check("звичайний хендшейк", []byte("\x10\x00\xff\x05localhost..................................................................................................................."), false)
}

// Клієнт без преамбули (звичайний Minecraft, що чекає відповіді) не має висіти до таймауту.
func TestP2PPreambleFailsFast(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	go func() { _, _ = a.Write([]byte("\x10\x00\xff\x05localhost")) }()
	start := time.Now()
	if _, _, ok := readP2PPreamble(b); ok {
		t.Fatal("чужий початок має відхилятись")
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("відмова не миттєва: %s", d)
	}
}

// ── хост-гейт ─────────────────────────────────────────────────────────────

type fakeNode struct {
	mu       sync.Mutex
	verdicts map[string]AdmitResult
	errs     map[string]bool
	calls    map[string]int
}

func newFakeNode() *fakeNode {
	return &fakeNode{verdicts: map[string]AdmitResult{}, errs: map[string]bool{}, calls: map[string]int{}}
}

func (n *fakeNode) admit(ticket string) (AdmitResult, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.calls[ticket]++
	if n.errs[ticket] {
		return AdmitResult{}, errors.New("вузол недоступний")
	}
	return n.verdicts[ticket], nil
}

func (n *fakeNode) set(ticket string, r AdmitResult) {
	n.mu.Lock()
	n.verdicts[ticket], n.errs[ticket] = r, false
	n.mu.Unlock()
}

func (n *fakeNode) fail(ticket string) { n.mu.Lock(); n.errs[ticket] = true; n.mu.Unlock() }

func (n *fakeNode) count(ticket string) int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.calls[ticket]
}

func tk(c byte) string { return strings.Repeat(string(c), 64) }

// dialGate імітує гостя, що пише преамбулу, і повертає його кінець труби (для перевірки обриву).
func dialGate(t *testing.T, g *P2PGate, ticket string) (peer net.Conn, release func(), ok bool) {
	t.Helper()
	a, b := net.Pipe()
	go func() { _, _ = a.Write(P2PPreamble(ticket)) }()
	_, rel, ok := g.Accept(b)
	if !ok {
		a.Close()
		b.Close()
		return nil, nil, false
	}
	t.Cleanup(func() { a.Close(); b.Close() })
	return a, rel, true
}

// isClosed — хост закрив з'єднання (читання з іншого кінця труби дає EOF, а не таймаут).
func isClosed(c net.Conn) bool {
	_ = c.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
	_, err := c.Read(make([]byte, 1))
	return errors.Is(err, io.EOF)
}

func TestP2PGateAdmitAndCache(t *testing.T) {
	n := newFakeNode()
	now := time.Now()
	g := NewP2PGate(n.admit)
	g.now = func() time.Time { return now }
	exp := now.Add(10 * time.Minute).UnixMilli()
	n.set(tk('a'), AdmitResult{OK: true, AccountID: "acc-a", ExpiresAt: exp})
	n.set(tk('d'), AdmitResult{OK: false})
	n.fail(tk('e'))

	if _, _, ok := dialGate(t, g, tk('a')); !ok {
		t.Fatal("дійсний квиток має пройти")
	}
	if _, _, ok := dialGate(t, g, tk('a')); !ok || n.count(tk('a')) != 1 {
		t.Fatalf("повторний квиток — із кешу (викликів admit: %d)", n.count(tk('a')))
	}
	now = now.Add(61 * time.Second)
	if _, _, ok := dialGate(t, g, tk('a')); !ok || n.count(tk('a')) != 2 {
		t.Fatalf("після 60 с кеш протух (викликів admit: %d)", n.count(tk('a')))
	}

	if _, _, ok := dialGate(t, g, tk('d')); ok {
		t.Fatal("відмова вузла = обрив")
	}
	if _, _, ok := dialGate(t, g, tk('d')); ok || n.count(tk('d')) != 1 {
		t.Fatalf("відмову кешуємо коротко (викликів admit: %d)", n.count(tk('d')))
	}
	now = now.Add(11 * time.Second)
	_, _, _ = dialGate(t, g, tk('d'))
	if n.count(tk('d')) != 2 {
		t.Fatalf("після 10 с відмову перепитуємо (викликів admit: %d)", n.count(tk('d')))
	}

	if _, _, ok := dialGate(t, g, tk('e')); ok {
		t.Fatal("збій вузла = не пускати")
	}
	_, _, _ = dialGate(t, g, tk('e'))
	if n.count(tk('e')) != 2 {
		t.Fatal("збій вузла не кешується")
	}

	// Без преамбули в обмеженому режимі — відмова, вузол не питаємо.
	a, b := net.Pipe()
	go func() { _, _ = a.Write([]byte("\x10\x00\xff\x05localhost")) }()
	if _, _, ok := g.Accept(b); ok {
		t.Fatal("без преамбули пускати не можна")
	}
	a.Close()
	b.Close()
}

func TestP2PGateRecheck(t *testing.T) {
	n := newFakeNode()
	now := time.Now()
	g := NewP2PGate(n.admit)
	g.now = func() time.Time { return now }
	exp1 := now.Add(5 * time.Minute).UnixMilli()
	exp2 := now.Add(10 * time.Minute).UnixMilli()
	n.set(tk('1'), AdmitResult{OK: true, AccountID: "A", ExpiresAt: exp1})
	n.set(tk('2'), AdmitResult{OK: true, AccountID: "A", ExpiresAt: exp2}) // новіший квиток того ж акаунта
	n.set(tk('b'), AdmitResult{OK: true, AccountID: "B", ExpiresAt: exp2})

	a1, _, _ := dialGate(t, g, tk('1'))
	a2, _, _ := dialGate(t, g, tk('2'))
	b1, relB, ok := dialGate(t, g, tk('b'))
	if a1 == nil || a2 == nil || !ok {
		t.Fatal("гості мали пройти")
	}

	// Доступ чинний — ніхто не відпадає; акаунт перевіряється ОДНИМ, найсвіжішим квитком.
	g.Recheck()
	if isClosed(a1) || isClosed(a2) || isClosed(b1) {
		t.Fatal("чинний доступ не рвемо")
	}
	if n.count(tk('2')) < 2 || n.count(tk('1')) != 1 {
		t.Fatalf("акаунт A має перевірятись найсвіжішим квитком (1:%d 2:%d)", n.count(tk('1')), n.count(tk('2')))
	}

	// Збій вузла — не привід вигнати гравця.
	n.fail(tk('b'))
	g.Recheck()
	if isClosed(b1) {
		t.Fatal("збій вузла не має рвати з'єднання")
	}

	// Дружбу A розірвано → обидва його з'єднання рвуться, B лишається.
	n.set(tk('2'), AdmitResult{OK: false})
	n.set(tk('b'), AdmitResult{OK: true, AccountID: "B", ExpiresAt: exp2})
	g.Recheck()
	if !isClosed(a1) || !isClosed(a2) {
		t.Fatal("відмова вузла в recheck має рвати всі з'єднання акаунта")
	}
	if isClosed(b1) {
		t.Fatal("чужий акаунт не зачіпаємо")
	}
	// Повторний вхід тим самим квитком іде на вузол (кеш скинуто) і отримує відмову.
	if _, _, ok := dialGate(t, g, tk('2')); ok {
		t.Fatal("після відмови той самий квиток не пускає")
	}

	// Квиток B скоро спливе: admit із ним уже false і без розриву дружби, тож не перевіряємо (запас 45 с).
	before := n.count(tk('b'))
	n.set(tk('b'), AdmitResult{OK: false})
	now = time.UnixMilli(exp2).Add(-30 * time.Second)
	g.Recheck()
	if n.count(tk('b')) != before || isClosed(b1) {
		t.Fatal("квиток, що спливає, не перевіряємо й не рвемо")
	}
	now = time.UnixMilli(exp2).Add(-2 * time.Minute)
	g.Recheck()
	if !isClosed(b1) {
		t.Fatal("поки квиток чинний, відмова рве з'єднання")
	}
	relB() // після обриву release не має падати
}

func TestP2PGateReleaseUntracks(t *testing.T) {
	n := newFakeNode()
	now := time.Now()
	g := NewP2PGate(n.admit)
	g.now = func() time.Time { return now }
	n.set(tk('a'), AdmitResult{OK: true, AccountID: "A", ExpiresAt: now.Add(10 * time.Minute).UnixMilli()})
	_, rel, ok := dialGate(t, g, tk('a'))
	if !ok {
		t.Fatal("мав пройти")
	}
	rel()
	g.Recheck()
	if n.count(tk('a')) != 1 {
		t.Fatalf("відключеного гостя не перевіряємо (викликів admit: %d)", n.count(tk('a')))
	}
}

// ── гостьове сховище квитків ──────────────────────────────────────────────

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("не дочекались: %s", what)
}

func TestTicketStoreRememberGet(t *testing.T) {
	s := newTicketStore()
	t.Cleanup(s.stopAll)
	if s.get("x-111111") != nil {
		t.Fatal("порожньо")
	}
	s.remember("X-111111", tk('a'), 0)
	if got := string(s.get("x-111111")); got != string(P2PPreamble(tk('a'))) {
		t.Fatalf("преамбула: %q", got)
	}
	// Відповідь без квитка (режим all) прибирає старий: преамбула в гру без гейта зламала б хендшейк.
	s.remember("x-111111", "", 0)
	if s.get("x-111111") != nil {
		t.Fatal("порожній квиток має прибирати збережений")
	}
	s.remember("x-111111", tk('a'), 0)
	s.remember("x-111111", "не-hex", 0)
	if s.get("x-111111") != nil {
		t.Fatal("некоректний квиток не зберігаємо")
	}
	var nilStore *ticketStore
	nilStore.remember("x", tk('a'), 0)
	if nilStore.get("x") != nil {
		t.Fatal("nil-сховище безпечне")
	}
}

func TestTicketStoreExpiry(t *testing.T) {
	s := newTicketStore()
	t.Cleanup(s.stopAll)
	// Вузол каже, що квиток спливе за 60 мс (раніше за стандартні 10 хв) — прострочений не шлемо.
	s.remember("x-111111", tk('a'), time.Now().Add(60*time.Millisecond).UnixMilli())
	if s.get("x-111111") == nil {
		t.Fatal("свіжий квиток має віддаватись")
	}
	waitFor(t, "квиток протух", func() bool { return s.get("x-111111") == nil })
}

func newFastStore() *ticketStore {
	s := newTicketStore()
	s.ttl, s.lead, s.retry = 300*time.Millisecond, 150*time.Millisecond, 20*time.Millisecond
	return s
}

func TestTicketStoreRefreshesBeforeExpiry(t *testing.T) {
	s := newFastStore()
	t.Cleanup(s.stopAll)
	var mu sync.Mutex
	calls := 0
	s.alive = func(string) bool { return true }
	s.refresh = func(addr string) error {
		mu.Lock()
		calls++
		mu.Unlock()
		s.remember(addr, tk('b'), 0) // join видав новий квиток
		return nil
	}
	s.remember("x-111111", tk('a'), 0)
	waitFor(t, "квиток оновлено", func() bool { return string(s.get("x-111111")) == string(P2PPreamble(tk('b'))) })
	mu.Lock()
	defer mu.Unlock()
	if calls < 1 {
		t.Fatal("refresh не викликано")
	}
}

func TestTicketStoreRefreshRetriesOnError(t *testing.T) {
	s := newFastStore()
	t.Cleanup(s.stopAll)
	var mu sync.Mutex
	calls := 0
	s.alive = func(string) bool { return true }
	s.refresh = func(addr string) error {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n < 3 {
			return errors.New("лобі недоступне")
		}
		s.remember(addr, tk('c'), 0)
		return nil
	}
	s.remember("x-111111", tk('a'), 0)
	waitFor(t, "оновлення після повторів", func() bool { return string(s.get("x-111111")) == string(P2PPreamble(tk('c'))) })
}

func TestTicketStoreDropsWhenRoomGone(t *testing.T) {
	s := newFastStore()
	t.Cleanup(s.stopAll)
	refreshed := false
	s.alive = func(string) bool { return false }
	s.refresh = func(string) error { refreshed = true; return nil }
	s.remember("x-111111", tk('a'), 0)
	waitFor(t, "квиток забуто", func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return len(s.m) == 0
	})
	if refreshed {
		t.Fatal("для зниклої кімнати квиток не оновлюємо — це зайві запити до вузла")
	}
}

func TestTicketStoreStop(t *testing.T) {
	s := newFastStore()
	refreshed := make(chan struct{}, 1)
	s.alive = func(string) bool { return true }
	s.refresh = func(string) error { refreshed <- struct{}{}; return nil }
	s.remember("x-111111", tk('a'), 0)
	s.stopAll()
	if s.get("x-111111") != nil {
		t.Fatal("stopAll забуває квитки")
	}
	select {
	case <-refreshed:
		t.Fatal("після stopAll оновлень немає")
	case <-time.After(400 * time.Millisecond):
	}
}

// ── клієнт admit ──────────────────────────────────────────────────────────

func TestAdmitTicketResult(t *testing.T) {
	c, seen := recServer(t, 200, `{"ok":true,"accountId":"acc-1","expiresAt":1760000000000}`)
	res, err := c.AdmitTicket(context.Background(), "x-111111", tk('a'), "", tk('d'))
	if err != nil || !res.OK || res.AccountID != "acc-1" || res.ExpiresAt != 1760000000000 {
		t.Fatalf("%+v %v", res, err)
	}
	if seen.path != "/p2p/admit" || seen.body["sessionToken"] != tk('d') || seen.body["ownerSecret"] != nil {
		t.Fatalf("тіло запиту: %+v", seen)
	}
	c2, _ := recServer(t, 403, `{"ok":false}`)
	if res, err := c2.AdmitTicket(context.Background(), "x-111111", tk('a'), "secret-secret-secret", ""); err != nil || res.OK {
		t.Fatalf("403 {ok:false} = відмова без помилки: %+v %v", res, err)
	}
}
