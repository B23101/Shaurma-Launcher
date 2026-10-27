package nodes

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func three() []Node {
	var o []Node
	for i := 1; i <= 3; i++ {
		o = append(o, Node{ID: fmt.Sprintf("vps-%d", i), URL: fmt.Sprintf("https://n%d.example", i), Weight: 100, Roles: []string{"gateway"}})
	}
	return o
}

// Ті самі вектори, що у vps/internal/cluster/cluster_test.go.
func TestOwnersMatchServer(t *testing.T) {
	exp := map[string]string{"alpha": "vps-1", "bravo": "vps-1", "charlie": "vps-2", "delta": "vps-2", "echo": "vps-1"}
	for k, want := range exp {
		if got := Rank(k, three(), "")[0].ID; got != want {
			t.Errorf("owner(%s)=%s, сервер каже %s — алгоритми розійшлись", k, got, want)
		}
	}
}

type fixture struct {
	worker *httptest.Server
	hits   map[string]*atomic.Int64
	down   map[string]*atomic.Bool
	pool   *Pool
}

// Вузли — це httptest-сервери; щоб обійти вимогу https:// у Refresh, список
// ставимо напряму (Refresh перевіряється окремим тестом).
func newFixture(t *testing.T, n int) *fixture {
	f := &fixture{hits: map[string]*atomic.Int64{}, down: map[string]*atomic.Bool{}}
	var list []Node
	for i := 1; i <= n; i++ {
		id := fmt.Sprintf("vps-%d", i)
		hits, down := &atomic.Int64{}, &atomic.Bool{}
		f.hits[id], f.down[id] = hits, down
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
			if down.Load() {
				http.Error(w, "x", 503)
				return
			}
			io.WriteString(w, "node")
		}))
		t.Cleanup(srv.Close)
		list = append(list, Node{ID: id, URL: srv.URL, Weight: 100, Roles: []string{"gateway"}})
	}
	f.worker = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "worker") }))
	t.Cleanup(f.worker.Close)
	f.pool = &Pool{WorkerBase: f.worker.URL, Headers: func() http.Header { return http.Header{} }, nodes: list, fetched: time.Now()}
	return f
}

func get(p *Pool, key string) (string, error) {
	resp, err := p.Do(key, func(base string) (*http.Request, error) {
		return http.NewRequest("GET", base+"/api/skin/by-nick/x", nil)
	})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b), nil
}

func TestSameKeyAlwaysSameNode(t *testing.T) {
	f := newFixture(t, 3)
	for i := 0; i < 20; i++ {
		get(f.pool, "Steve")
	}
	busy := 0
	for _, h := range f.hits {
		if h.Load() > 0 {
			busy++
		}
	}
	if busy != 1 {
		t.Fatalf("один ключ мав би потрапляти на один вузол (кеш!), а зачепив %d", busy)
	}
}

func TestFailoverToNextThenError(t *testing.T) {
	f := newFixture(t, 2)
	order := Rank("Steve", f.pool.nodes, "gateway")
	f.down[order[0].ID].Store(true)
	if out, _ := get(f.pool, "Steve"); out != "node" || f.hits[order[1].ID].Load() != 1 {
		t.Fatalf("мав відповісти другий вузол: %q", out)
	}
	// Перший у карантині — наступний запит одразу йде на живий.
	before := f.hits[order[0].ID].Load()
	get(f.pool, "Steve")
	if f.hits[order[0].ID].Load() != before {
		t.Fatal("вузол у карантині не повинен отримувати запити")
	}
	f.down[order[1].ID].Store(true)
	f.pool.badUntil = nil
	if out, _ := get(f.pool, "Steve"); out == "worker" || out == "node" {
		t.Fatalf("усі вузли лежать → остання відповідь вузла або помилка, без запасного шляху; відповіло %q", out)
	}
}

func TestNoNodesIsError(t *testing.T) {
	f := newFixture(t, 0)
	if out, err := get(f.pool, "k"); err == nil || out == "worker" {
		t.Fatalf("порожній список → помилка: %q %v", out, err)
	}
}

func TestRefreshFiltersUnsafeURLs(t *testing.T) {
	w := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/nodes" || r.Header.Get("X-Shaurma-Token") != "tok" {
			http.NotFound(rw, r)
			return
		}
		io.WriteString(rw, `{"nodes":[
		 {"id":"a","url":"https://ok.example.org/","weight":100,"roles":["gateway"]},
		 {"id":"b","url":"http://plain.example.org","weight":100,"roles":["gateway"]},
		 {"id":"c","url":"https://user@evil.example.org","weight":100,"roles":["gateway"]},
		 {"id":"d","url":"https://x.example.org/p?q=1","weight":100,"roles":["gateway"]}]}`)
	}))
	defer w.Close()
	p := &Pool{WorkerBase: w.URL, Headers: func() http.Header { return http.Header{"X-Shaurma-Token": {"tok"}} }}
	if err := p.Refresh(); err != nil {
		t.Fatal(err)
	}
	if len(p.nodes) != 1 || p.nodes[0].URL != "https://ok.example.org" {
		t.Fatalf("небезпечні URL мали відсіятись: %+v", p.nodes)
	}
}

func TestSocialWSURL(t *testing.T) {
	p := &Pool{nodes: []Node{{ID: "a", Roles: []string{"gateway"}}, {ID: "b", WSURL: "wss://s.example/ws", Roles: []string{"social"}}}}
	if u, ok := p.SocialWSURL(); !ok || !strings.HasPrefix(u, "wss://") {
		t.Fatal(u)
	}
}

func TestGatewayBasesOrderAndLimit(t *testing.T) {
	p := &Pool{WorkerBase: "https://w.example", Headers: func() http.Header { return http.Header{} }}
	p.nodes, p.fetched = three(), time.Now()
	got := p.GatewayBases("ygg")
	if len(got) != 2 {
		t.Fatalf("очікувалось 2 бази (MaxTries), а є %d: %v", len(got), got)
	}
	if got[0] != Rank("ygg", three(), "gateway")[0].URL {
		t.Fatalf("перша база не збігається з рангом: %v", got)
	}
	for _, b := range got {
		if strings.Contains(b, "w.example") {
			t.Fatal("Worker не має бути серед шлюзів")
		}
	}
	p.nodes = nil
	if len(p.GatewayBases("ygg")) != 0 {
		t.Fatal("без вузлів список має бути порожнім")
	}
}
