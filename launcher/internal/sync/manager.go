package sync

import (
	"context"
	"fmt"
	"sync"
)

// Manager тримає всі АКТИВНІ Runner-сесії одночасно — по одній на packID.
// Кожна збірка синхронізується у власній горутині зі своїм контекстом, усі
// вони діляться спільним Submitter (download.Engine.pool), тому кілька
// одночасних збірок НЕ подвоюють реальний мережевий паралелізм і не
// заважають одна одній лімітами.
//
// Це саме той рівень, що відповідає на "можна одночасно качати пару
// збірок без проблем": Manager гарантує унікальність запуску на packID
// (повторний Start для вже активної збірки — помилка, а не друга гонка),
// і дає єдину точку правди "чи щось зараз качається взагалі" для
// попередження при закритті лаунчера.
type Manager struct {
	mu      sync.Mutex
	active  map[string]*activeSync
	onEvent func(p RunnerProgress)
}

type activeSync struct {
	runner *Runner
	cancel context.CancelFunc
	done   chan struct{}
}

func NewManager(onEvent func(p RunnerProgress)) *Manager {
	return &Manager{
		active:  map[string]*activeSync{},
		onEvent: onEvent,
	}
}

// Start запускає синхронізацію збірки у фоні. Повертає помилку одразу,
// якщо ця збірка вже синхронізується (щоб не отримати дві паралельні
// гонки за одні й ті ж файли на диску). Повертає канал, що закривається,
// коли Run завершиться (для тих, хто хоче дочекатись синхронно, напр.
// команда "встановити і запустити відразу").
func (m *Manager) Start(runner *Runner, manifest *PackManifest, maxConcurrentDownloads int) (<-chan struct{}, error) {
	m.mu.Lock()
	if _, exists := m.active[runner.PackID]; exists {
		m.mu.Unlock()
		return nil, fmt.Errorf("збірка %s вже синхронізується", runner.PackID)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	as := &activeSync{runner: runner, cancel: cancel, done: done}
	m.active[runner.PackID] = as
	m.mu.Unlock()

	baseOnProgress := runner.OnProgress
	runner.OnProgress = func(p RunnerProgress) {
		if baseOnProgress != nil {
			baseOnProgress(p)
		}
		if m.onEvent != nil {
			m.onEvent(p)
		}
	}

	go func() {
		defer close(done)
		defer func() {
			m.mu.Lock()
			delete(m.active, runner.PackID)
			m.mu.Unlock()
		}()
		runner.Run(ctx, manifest, maxConcurrentDownloads)
	}()

	return done, nil
}

// Pause запитує м'яку зупинку для конкретної збірки (прогрес зберігається,
// кнопка в UI стане "Продовжити"). No-op, якщо збірка зараз не активна.
func (m *Manager) Pause(packID string) {
	m.mu.Lock()
	as, ok := m.active[packID]
	m.mu.Unlock()
	if ok {
		as.runner.Pause()
	}
}

// Cancel запитує повне скасування конкретної збірки (часткові файли
// видаляються, кнопка в UI повернеться на "Встановити"). No-op, якщо
// збірка зараз не активна.
func (m *Manager) Cancel(packID string) {
	m.mu.Lock()
	as, ok := m.active[packID]
	m.mu.Unlock()
	if ok {
		as.runner.Cancel()
	}
}

// PauseAll — викликається при закритті лаунчера/panic-recovery: усі
// активні синхронізації переводяться в Pause (НІКОЛИ в Cancel), щоб
// раптове закриття не сприймалось як "видали все, що встигли скачати".
// Той самий шлях спрацьовує і при непередбаченому завершенні процесу —
// на диску не лишається "гонки", бо .part-файли і так пишуться атомарно
// по чанках, а Stage у InstallState не StageComplete/StageNone.
func (m *Manager) PauseAll() {
	m.mu.Lock()
	runners := make([]*Runner, 0, len(m.active))
	for _, as := range m.active {
		runners = append(runners, as.runner)
	}
	m.mu.Unlock()
	for _, r := range runners {
		r.Pause()
	}
}

// WaitFor блокується, доки активна сесія packID не завершиться (якщо вона
// є). Потрібно при Resume після Pause: paused-runner ще «дрейнить» (докачує
// поточний чанк), і Start для тієї самої збірки одразу відхилив би повторний
// запуск помилкою «вже синхронізується» — а користувач побачив би зайвий
// тост помилки замість плавного продовження.
func (m *Manager) WaitFor(packID string) {
	m.mu.Lock()
	as, ok := m.active[packID]
	m.mu.Unlock()
	if ok {
		<-as.done
	}
}

// WaitAll блокується, доки всі активні синхронізації не завершаться
// (використовується при graceful shutdown, щоб дочекатись, поки поточний
// мережевий чанк дозапишеться на диск, перш ніж дозволити процесу вийти).
func (m *Manager) WaitAll() {
	m.mu.Lock()
	dones := make([]chan struct{}, 0, len(m.active))
	for _, as := range m.active {
		dones = append(dones, as.done)
	}
	m.mu.Unlock()
	for _, d := range dones {
		<-d
	}
}

// HasActive — true, якщо хоч одна збірка зараз синхронізується. Це і є
// умова показу попередження "закриття лаунчера може пошкодити файли" при
// спробі закрити вікно.
func (m *Manager) HasActive() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.active) > 0
}

// ActivePackIDs повертає список packID, що зараз синхронізуються (для
// тексту попередження — "качається: Шаурма RPG, Шаурма PvP").
func (m *Manager) ActivePackIDs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := make([]string, 0, len(m.active))
	for id := range m.active {
		ids = append(ids, id)
	}
	return ids
}

// IsActive — чи синхронізується конкретна збірка зараз.
func (m *Manager) IsActive(packID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.active[packID]
	return ok
}
