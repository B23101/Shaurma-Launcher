package social

// budget.go — запобіжник денних запитів до Cloudflare Worker.
//
// Free-tier Worker має жорстку стелю 100 000 запитів/добу на ВЕСЬ акаунт
// (роздача файлів + друзі/чат). Один лише неперервний фон без обмежень
// (напр. цикл опитування з невеликим інтервалом або баг, що робить запит
// у циклі) міг би вичерпати квоту за години й «покласти» роздачу збірок.
//
// Тут — НЕ розподіл 100k між користувачами (клієнт не знає глобального
// споживання), а запобіжник на ОДИН процес-клієнт: скільки запитів він має
// право зробити за добу, перш ніж почне повертати бюджетну помилку. Нормальні
// операції (відкрити чат, надіслати повідомлення, оновити друзів) укладаються
// в ліміт із великим запасом; аномалія — впирається.
//
// Оцінка «нормального» споживання одного клієнта:
//   - фон синхронізації друзів: PollEvery (5 хв за замовчуванням) × 2 запити
//     (list + invites) ≈ 576/добу;
//   - історія чату: ~1 запит на відкриття діалогу + по 1 на «докрутити»;
//   - ключ друга: 1 запит на співрозмовника, далі з кеша;
//   - надсилання повідомлення: 1 запит Worker (через VPS-проксі) на кожне —
//     це та плата, яку не прибрати, і саме її стримує Worker-ліміт 30/хв.
// Тож 5000/добу дає великий запас для легітимного використання, але не дає
// збою непомітно з'їсти квоту акаунта.

import (
	"sync"
	"time"
)

// DefaultDailyRequestBudget — стеля запитів до Worker на один клієнт за добу.
const DefaultDailyRequestBudget = 5000

// Budget — ковзне вікно запитів для одного клієнта. Безпечний для горутин.
type Budget struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	start  time.Time
	used   int
	now    func() time.Time // підмінюється в тестах
}

// NewBudget створює ліміт. limit<=0 → DefaultDailyRequestBudget, window<=0 → 24 год.
func NewBudget(limit int, window time.Duration) *Budget {
	if limit <= 0 {
		limit = DefaultDailyRequestBudget
	}
	if window <= 0 {
		window = 24 * time.Hour
	}
	return &Budget{limit: limit, window: window, now: time.Now}
}

// Allow споживає один запит і повідомляє, чи можна його робити. Вікно
// автоматично скидається, коли минуло.
func (b *Budget) Allow() bool {
	if b == nil {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	if b.start.IsZero() || now.Sub(b.start) >= b.window {
		b.start = now
		b.used = 0
	}
	if b.used >= b.limit {
		return false
	}
	b.used++
	return true
}

// Remaining — скільки запитів лишилось у поточному вікні (для діагностики).
func (b *Budget) Remaining() int {
	if b == nil {
		return DefaultDailyRequestBudget
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	if b.start.IsZero() || now.Sub(b.start) >= b.window {
		return b.limit
	}
	if b.used >= b.limit {
		return 0
	}
	return b.limit - b.used
}
