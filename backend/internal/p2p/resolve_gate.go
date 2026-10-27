// resolve_gate.go — захист VPS (lobby) від лавини запитів /p2p/join.
//
// Чому це потрібно. Minecraft оновлює список серверів постійно: кожне відкриття
// екрана, кожне «оновити», по одному пінгу на кожен збережений сервер. Для
// офлайн-хоста кожен такий пінг раніше ішов до VPS (/p2p/join), а VPS на
// невдалий join ще й навмисно затримує відповідь (failDelay) — тож сотня
// гравців зі збереженим офлайн-сервером = сотня повільних запитів. Тепер:
//
//  1. Одночасні запити на ту саму адресу зливаються в ОДИН (singleflight).
//  2. Для пінгу списку серверів (resolvePing) негативна відповідь («хост
//     офлайн», «немає доступу», VPS недоступний) запам'ятовується на кілька
//     секунд, а успішна — на 3 с. Реальний вхід в гру (resolveFresh) кеш
//     ПРОПУСКАЄ: гравець, що щойно натиснув «Підключитись», не мусить чекати,
//     поки застаріє вердикт попереднього пінгу.
//  3. Будь-який успішний resolve стирає негативний запис адреси.
//
// Нових запитів до VPS ця логіка не створює — лише прибирає зайві.
// Файл залежить від stdlib і типу RoomInfo.
package p2p

import (
	"errors"
	"sync"
	"time"
)

type resolveMode int

const (
	// resolveFresh — завжди питаємо VPS (лише зливаємо паралельні запити).
	resolveFresh resolveMode = iota
	// resolvePing — пінг списку серверів: дозволено короткий кеш.
	resolvePing
)

const (
	resolvePosTTL    = 3 * time.Second // успішна відповідь для пінгу
	resolveCacheSize = 256             // вище — кеш скидається (захист від розпухання)

	// resolvePingMaxFlights — скільки РІЗНИХ адрес пінг може резолвити паралельно. Далі — відмова
	// без запиту до VPS: локальний процес, що засипає лаунчер різними адресами, не перетворить його
	// на генератор навантаження для VPS (на VPS є і власні ліміти, це перший рубіж).
	resolvePingMaxFlights = 16
)

// errResolveBusy — забагато паралельних резолвів пінгу; запит до VPS не робився.
var errResolveBusy = errors.New("p2p: забагато одночасних запитів адрес")

type resolveNeg struct {
	err   error
	until time.Time
}

type resolvePos struct {
	info  RoomInfo
	until time.Time
}

type resolveFlight struct {
	done chan struct{}
	info *RoomInfo
	err  error
}

// resolveGate — нульове значення готове до роботи.
type resolveGate struct {
	mu      sync.Mutex
	neg     map[string]resolveNeg
	pos     map[string]resolvePos
	flights map[string]*resolveFlight
	now     func() time.Time // підміняється в тестах
}

func (r *resolveGate) clock() time.Time {
	if r.now != nil {
		return r.now()
	}
	return time.Now()
}

// do виконує fn для key з урахуванням режиму. negTTL повертає, на скільки
// запам'ятати саме цю помилку (0 — не запам'ятовувати).
func (r *resolveGate) do(key string, mode resolveMode, negTTL func(error) time.Duration, fn func() (*RoomInfo, error)) (*RoomInfo, error) {
	now := r.clock()
	r.mu.Lock()
	if mode == resolvePing {
		if n, ok := r.neg[key]; ok && now.Before(n.until) {
			r.mu.Unlock()
			return nil, n.err
		}
		if p, ok := r.pos[key]; ok && now.Before(p.until) {
			cp := p.info
			r.mu.Unlock()
			return &cp, nil
		}
	}
	if f, ok := r.flights[key]; ok {
		r.mu.Unlock()
		<-f.done
		if f.err != nil {
			return nil, f.err
		}
		cp := *f.info
		return &cp, nil
	}
	if mode == resolvePing && len(r.flights) >= resolvePingMaxFlights {
		r.mu.Unlock()
		return nil, errResolveBusy
	}
	f := &resolveFlight{done: make(chan struct{})}
	if r.flights == nil {
		r.flights = map[string]*resolveFlight{}
	}
	r.flights[key] = f
	r.mu.Unlock()

	f.info, f.err = safeResolve(fn)

	r.mu.Lock()
	delete(r.flights, key)
	end := r.clock()
	if f.err != nil {
		if ttl := negTTL(f.err); ttl > 0 {
			if r.neg == nil || len(r.neg) > resolveCacheSize {
				r.neg = map[string]resolveNeg{}
			}
			r.neg[key] = resolveNeg{err: f.err, until: end.Add(ttl)}
		}
		delete(r.pos, key)
	} else {
		delete(r.neg, key)
		if r.pos == nil || len(r.pos) > resolveCacheSize {
			r.pos = map[string]resolvePos{}
		}
		r.pos[key] = resolvePos{info: *f.info, until: end.Add(resolvePosTTL)}
	}
	r.mu.Unlock()
	close(f.done)

	if f.err != nil {
		return nil, f.err
	}
	cp := *f.info
	return &cp, nil
}

// forget стирає кеш адреси (гравець гарантовано пробує ще раз).
func (r *resolveGate) forget(key string) {
	r.mu.Lock()
	delete(r.neg, key)
	delete(r.pos, key)
	r.mu.Unlock()
}

var errResolvePanic = errors.New("p2p: збій під час резолву адреси")

// safeResolve — паніка в fn не має лишити запит «в польоті» назавжди: решта
// очікувальників зависла б на <-f.done.
func safeResolve(fn func() (*RoomInfo, error)) (info *RoomInfo, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			info, err = nil, errResolvePanic
		}
	}()
	return fn()
}
