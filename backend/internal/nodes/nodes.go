// Package nodes — вибір VPS-вузла для запитів лаунчера (скіни, акаунти, друзі).
//
// Лаунчер бере список живих вузлів у Worker (GET /nodes), сам обирає власника
// за зваженим rendezvous-хешем (та сама функція, що й на сервері:
// vps/internal/cluster/ring.go — вектори в тестах збігаються) і йде туди.
// Збій вузла → наступний за рейтингом → у крайньому разі прямо Worker
// (читання скінів працюють, зміни Worker відхилить — і це правильно).
package nodes

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Node struct {
	ID     string   `json:"id"`
	URL    string   `json:"url"`
	WSURL  string   `json:"wsUrl"`
	Weight int      `json:"weight"`
	Roles  []string `json:"roles"`
}

func (n Node) hasRole(r string) bool {
	for _, x := range n.Roles {
		if x == r {
			return true
		}
	}
	return false
}

func unitHash(key, id string) float64 {
	h := sha256.Sum256([]byte(key + "\x00" + id))
	return (float64(binary.BigEndian.Uint64(h[:8])>>11) + 1) / (float64(uint64(1)<<53) + 1)
}

// Rank — вузли в порядку переваги для ключа (дзеркало cluster.Rank).
func Rank(key string, nodes []Node, role string) []Node {
	type sc struct {
		n Node
		s float64
	}
	var l []sc
	for _, n := range nodes {
		if n.Weight <= 0 || (role != "" && !n.hasRole(role)) {
			continue
		}
		l = append(l, sc{n, -float64(n.Weight) / math.Log(unitHash(key, n.ID))})
	}
	sort.SliceStable(l, func(i, j int) bool {
		if l[i].s != l[j].s {
			return l[i].s > l[j].s
		}
		return l[i].n.ID < l[j].n.ID
	})
	out := make([]Node, len(l))
	for i := range l {
		out[i] = l[i].n
	}
	return out
}

// Pool — кеш списку вузлів + виконання запитів із failover.
type Pool struct {
	WorkerBase string
	Headers    func() http.Header // UA + X-Shaurma-Token
	HTTP       *http.Client
	TTL        time.Duration // скільки живе список (за замовчуванням 5 хв)
	MaxTries   int           // скільки вузлів пробувати (за замовчуванням 2)
	Now        func() time.Time

	mu       sync.Mutex
	nodes    []Node
	fetched  time.Time
	badUntil map[string]time.Time // вузол «в карантині» після збою
}

func (p *Pool) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

func (p *Pool) client() *http.Client {
	if p.HTTP != nil {
		return p.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// Refresh тягне список. Збій не критичний: лишається старий (або порожній →
// запити йдуть прямо у Worker).
func (p *Pool) Refresh() error {
	req, err := http.NewRequest(http.MethodGet, p.WorkerBase+"/nodes", nil)
	if err != nil {
		return err
	}
	for k, v := range p.Headers() {
		req.Header[k] = v
	}
	hc := *p.client()
	hc.Timeout = 6 * time.Second
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errStatus(resp.StatusCode)
	}
	var body struct {
		Nodes []Node `json:"nodes"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&body); err != nil {
		return err
	}
	var ok []Node
	for _, n := range body.Nodes {
		// Довіряємо лише https і лише DNS-іменам: навіть скомпрометований
		// список не відправить токен на http:// чи на сиру IP-адресу.
		if strings.HasPrefix(n.URL, "https://") && !strings.ContainsAny(n.URL[8:], "@?# ") {
			n.URL = strings.TrimRight(n.URL, "/")
			ok = append(ok, n)
		}
	}
	p.mu.Lock()
	p.nodes, p.fetched = ok, p.now()
	p.mu.Unlock()
	return nil
}

type errStatus int

func (e errStatus) Error() string { return "nodes: HTTP " + http.StatusText(int(e)) }

func (p *Pool) ranked(key string) []Node {
	ttl := p.TTL
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	p.mu.Lock()
	stale := p.now().Sub(p.fetched) > ttl
	p.mu.Unlock()
	if stale {
		_ = p.Refresh() // не вдалося — користуємось тим, що є
		p.mu.Lock()
		if p.fetched.IsZero() { // ще жодного разу — не спамимо Worker кожним запитом
			p.fetched = p.now().Add(-ttl + 30*time.Second)
		}
		p.mu.Unlock()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	all := Rank(key, p.nodes, "gateway")
	var good, bad []Node
	for _, n := range all {
		if p.badUntil[n.ID].After(p.now()) {
			bad = append(bad, n)
		} else {
			good = append(good, n)
		}
	}
	return append(good, bad...)
}

func (p *Pool) markBad(id string) { p.markBadFor(id, 60*time.Second) }

func (p *Pool) markBadFor(id string, d time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.badUntil == nil {
		p.badUntil = map[string]time.Time{}
	}
	p.badUntil[id] = p.now().Add(d)
}

// retryAfter — секунди з Retry-After у межах 5–120 с (за замовчуванням 30).
func retryAfter(v string) time.Duration {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return 30 * time.Second
	}
	return time.Duration(min(max(n, 5), 120)) * time.Second
}

// Do виконує запит на вузол-власник ключа; при мережевій помилці чи 5xx пробує
// наступний. build створює свіжий *http.Request для заданої бази
// (тіло потрібно відтворювати щоразу).
func (p *Pool) Do(key string, build func(base string) (*http.Request, error)) (*http.Response, error) {
	tries := p.MaxTries
	if tries <= 0 {
		tries = 2
	}
	var lastResp *http.Response
	var lastErr error
	for i, n := range p.ranked(key) {
		if i >= tries {
			break
		}
		req, err := build(n.URL)
		if err != nil {
			return nil, err
		}
		resp, err := p.client().Do(req)
		if err == nil && resp.StatusCode < 500 {
			return resp, nil
		}
		// 503 + Retry-After — вузол перевантажений (shield): відпочиває стільки, скільки просить.
		if err == nil && resp.StatusCode == http.StatusServiceUnavailable {
			p.markBadFor(n.ID, retryAfter(resp.Header.Get("Retry-After")))
		} else {
			p.markBad(n.ID)
		}
		if lastResp != nil {
			lastResp.Body.Close()
		}
		lastResp, lastErr = resp, err
	}
	// Усі вузли відмовили: віддаємо останню відповідь або помилку (запасного шляху поза кластером немає).
	if lastResp != nil {
		return lastResp, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, errors.New("жоден вузол VPS недоступний")
}

func firstErr(a, b error) error {
	if a != nil {
		return a
	}
	return b
}

// Prime один раз підтягує список, якщо його ще не було (для місць, яким адреса
// потрібна до першого запиту, напр. створення соціального сервісу).
func (p *Pool) Prime() {
	p.mu.Lock()
	never := p.fetched.IsZero()
	p.mu.Unlock()
	if never {
		_ = p.Refresh()
	}
}

// SocialWSURL — адреса WebSocket вузла з роллю social (якщо відома).
func (p *Pool) SocialWSURL() (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, n := range p.nodes {
		if n.hasRole("social") && strings.HasPrefix(n.WSURL, "wss://") {
			return n.WSURL, true
		}
	}
	return "", false
}

// GatewayBases — бази (https-URL) вузлів-шлюзів у порядку переваги для ключа,
// не більше MaxTries (2 за замовчуванням). Порожньо, якщо кластер не відомий.
func (p *Pool) GatewayBases(key string) []string {
	tries := p.MaxTries
	if tries <= 0 {
		tries = 2
	}
	var out []string
	for i, n := range p.ranked(key) {
		if i >= tries {
			break
		}
		out = append(out, n.URL)
	}
	return out
}
