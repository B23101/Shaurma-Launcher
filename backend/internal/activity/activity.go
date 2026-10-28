// Package activity — єдиний реєстр довгих операцій лаунчера (передачі
// друзям, встановлення ядра сервера, міграції, експорт, бекапи…). Із нього
// живиться індикатор активності у верхній панелі та прогрес у панелі
// завдань Windows. Реєстр нічого не знає про Wails: App оновлює його з
// подій прогресу й сам розсилає знімок.
package activity

import (
	"sort"
	"sync"
	"time"
)

// Стани операції.
const (
	StateRunning = "running"
	StatePaused  = "paused"
	// StateWaiting — операція відкрита, але зараз нічого не робить (напр.
	// передача чекає, поки друг прийме). Видно у списку, але не в таскбарі.
	StateWaiting = "waiting"
	StateError   = "error"
	StateDone    = "done"
)

// Linger — скільки завершена операція ще видна у списку (підсумок не губиться).
const Linger = 8 * time.Second

// Stale — операцію без оновлень довше за цей час вважаємо покинутою
// (подія завершення загубилась) і прибираємо, щоб таскбар не «висів».
const Stale = 2 * time.Minute

// Activity — одна довга операція.
type Activity struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Title string `json:"title"`
	// Detail — поточний крок / файл / причина помилки.
	Detail string `json:"detail,omitempty"`
	// Percent — 0..100; -1 — невідомо (нескінченний бар).
	Percent int    `json:"percent"`
	State   string `json:"state"`
	// Target — id об'єкта (збірка, сервер, передача) для переходу з UI.
	Target    string `json:"target,omitempty"`
	StartedAt int64  `json:"startedAt"`
	UpdatedAt int64  `json:"updatedAt"`
}

// Finished — операція вже завершилась (успіхом чи помилкою).
func (a Activity) Finished() bool { return a.State == StateDone || a.State == StateError }

// Registry — потокобезпечний набір операцій.
type Registry struct {
	mu    sync.Mutex
	items map[string]*Activity
	now   func() time.Time
}

// New створює порожній реєстр.
func New() *Registry {
	return &Registry{items: map[string]*Activity{}, now: time.Now}
}

// Has — чи є операція з таким id (для ліниво обчислюваних назв).
func (r *Registry) Has(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.items[id]
	return ok
}

// Update додає або оновлює операцію. Порожні Kind/Title/Target не
// затирають уже відомі. Повертає true, якщо щось змінилось.
func (r *Registry) Update(a Activity) bool {
	if a.ID == "" {
		return false
	}
	if a.State == "" {
		a.State = StateRunning
	}
	if a.Percent > 100 {
		a.Percent = 100
	}
	if a.Percent < -1 {
		a.Percent = -1
	}
	ms := r.now().UnixMilli()
	r.mu.Lock()
	defer r.mu.Unlock()
	cur, ok := r.items[a.ID]
	if !ok {
		a.StartedAt, a.UpdatedAt = ms, ms
		r.items[a.ID] = &a
		return true
	}
	if a.Kind == "" {
		a.Kind = cur.Kind
	}
	if a.Title == "" {
		a.Title = cur.Title
	}
	if a.Target == "" {
		a.Target = cur.Target
	}
	a.StartedAt = cur.StartedAt
	// Завершена операція знову стає активною — це новий запуск (повтор).
	if cur.Finished() && !a.Finished() {
		a.StartedAt = ms
	}
	// UpdatedAt оновлюється завжди (операція жива), але змінами не рахується.
	a.UpdatedAt = cur.UpdatedAt
	changed := *cur != a
	a.UpdatedAt = ms
	*cur = a
	return changed
}

// Finish позначає операцію завершеною (errText != "" — помилкою). Невідомий
// id ігнорується: завершувати нема чого.
func (r *Registry) Finish(id, errText string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	cur, ok := r.items[id]
	if !ok || cur.Finished() {
		return false
	}
	cur.State, cur.Percent = StateDone, 100
	if errText != "" {
		cur.State, cur.Percent, cur.Detail = StateError, -1, errText
	}
	cur.UpdatedAt = r.now().UnixMilli()
	return true
}

// Remove прибирає операцію без підсумку (напр. користувач відхилив передачу).
func (r *Registry) Remove(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.items[id]; !ok {
		return false
	}
	delete(r.items, id)
	return true
}

// Snapshot повертає поточний список (від найстаршої до найновішої),
// попередньо прибравши завершені довше за Linger і покинуті довше за Stale.
// next — коли наступна завершена операція має зникнути (0 — таких немає):
// викликач перемальовує UI в цей момент.
func (r *Registry) Snapshot() (list []Activity, next time.Duration) {
	now := r.now()
	ms := now.UnixMilli()
	r.mu.Lock()
	defer r.mu.Unlock()
	list = make([]Activity, 0, len(r.items))
	for id, it := range r.items {
		age := time.Duration(ms-it.UpdatedAt) * time.Millisecond
		if (it.Finished() && age >= Linger) || (!it.Finished() && age >= Stale) {
			delete(r.items, id)
			continue
		}
		if it.Finished() {
			if left := Linger - age; next == 0 || left < next {
				next = left
			}
		}
		list = append(list, *it)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].StartedAt != list[j].StartedAt {
			return list[i].StartedAt < list[j].StartedAt
		}
		return list[i].ID < list[j].ID
	})
	return list, next
}

// Aggregate зводить операції до одного стану панелі завдань: найбільший %
// серед активних (indeterminate, якщо жодна не знає свого %), інакше
// найбільший % серед призупинених, інакше помилка, якщо щойно була.
type Aggregate struct {
	Running, Paused, Error bool
	// RunningPercent — -1, якщо жодна активна не знає свого %.
	RunningPercent int
	PausedPercent  int
}

// Add враховує одну операцію (state + percent) в агрегаті.
func (g *Aggregate) Add(state string, percent int) {
	switch state {
	case StateRunning:
		if !g.Running || percent > g.RunningPercent {
			g.RunningPercent = percent
		}
		g.Running = true
	case StatePaused:
		if !g.Paused || percent > g.PausedPercent {
			g.PausedPercent = percent
		}
		g.Paused = true
	case StateError:
		g.Error = true
	}
}
