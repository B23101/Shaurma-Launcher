package download

import "sync"

const (
	// Дефолт ліміту одночасних завантажень (налаштовується у вкладці
	// «Завдання»; стеля пулу оновлюється через SetLimits).
	defaultMaxConcurrent = 10
)

// pool — СПІЛЬНИЙ на весь Engine пул воркерів. Усі черги sync.Runner
// (hash-моди і worker-файли) ділять один і той самий пул, тож дві
// паралельні синхронізації не подвоюють реальний мережевий паралелізм.
//
// Колись тут був «адаптивний» пул з евристикою росту за швидкістю
// (bytesPerSec), але поле НІКОЛИ не записувалось — евристика завжди
// вважала швидкість «покращеною» і щосекунди додавала воркер до стелі.
// Тепер — чесний фіксований ліміт: не більше max воркерів одночасно,
// воркери дорощуються ліниво до стелі.
type pool struct {
	mu      sync.Mutex
	max     int
	workers int
	ch      chan func()
}

func newPool(max int) *pool {
	return &pool{max: max, ch: make(chan func(), 256)}
}

// setCeiling оновлює верхню межу пулу в рантаймі, коли користувач змінює
// maxConcurrentDownloads у налаштуваннях.
func (p *pool) setCeiling(max int) {
	p.mu.Lock()
	p.max = max
	p.mu.Unlock()
}

func (p *pool) submit(fn func()) {
	p.mu.Lock()
	if p.workers < p.max {
		p.workers++
		go p.worker()
	}
	p.mu.Unlock()
	p.ch <- fn
}

func (p *pool) worker() {
	for fn := range p.ch {
		fn()
	}
}

// Engine — спільний пул воркерів завантажень лаунчера. Сам файли не
// качає: реальне завантаження виконує sync.Runner (двочерговий рушій V2),
// який бере в Engine спільний пул (Submit) і стелю паралелізму, а
// HTTP-клієнт — з api.ShaurmaClient (HTTPClientForSync). Колись Engine
// тримав власний http.Client/dialer і мертву сесійну систему (StartSession
// тощо) — усе це прибрано: client ніхто не читав, сесій ніхто не стартував.
type Engine struct {
	mu   sync.Mutex
	pool *pool
}

func NewEngine() *Engine {
	return &Engine{pool: newPool(defaultMaxConcurrent)}
}

// SetLimits оновлює ліміт одночасних завантажень (вкладка «Завдання»):
// стелю спільного пулу — одразу (впливає на всі черги, що вже виконуються).
// Ретраї та HTTP-таймаут тут більше не налаштовуються: sync.Runner читає
// MaxRetries напряму з налаштувань, а HTTP-клієнт живе в api.ShaurmaClient.
func (e *Engine) SetLimits(maxConcurrent int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if maxConcurrent > 0 {
		e.pool.setCeiling(maxConcurrent)
	}
}

// Submit віддає функцію в СПІЛЬНИЙ пул Engine — саме той механізм, який
// гарантує, що дві черги (hash-моди/worker-файли) чи дві одночасні
// синхронізації не подвоюють ліміт паралелізму.
func (e *Engine) Submit(fn func()) { e.pool.submit(fn) }
