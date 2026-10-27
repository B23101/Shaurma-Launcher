// Пакет sessions — менеджер незалежних ігрових процесів по акаунтах.
//
// Кожна (акаунт, збірка) пара має власний екземпляр лаунчера
// (той самодостатній: тримає свій exec.Cmd/стан). Завдяки цьому:
//   - та сама збірка може бути запущена на кількох акаунтах незалежно;
//   - на іншому акаунті збірка виглядає «не запущена», але sidebar
//     показує, на якому акаунті вона реально працює (RunningOn);
//   - зупинка/стан однієї сесії не зачіпає інші.
//
// Сам менеджер — тонкий реєстр; логіку запуску (збірка java-команди,
// хендлери виводу/виходу) тримає App, який створює Launcher і передає
// його в Start.
package sessions

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// Key — унікальний ідентифікатор сесії (акаунт × збірка).
type Key struct {
	AccountID string
	BuildID   string
}

// launcherLike — мінімальний контракт, якого потребує менеджер сесій від
// лаунчера гри. Реалізується *minecraft.Launcher; інтерфейс дозволяє
// тестувати реєстр легким фейком і не зав'язує sessions на конкретний тип.
type launcherLike interface {
	IsRunning() bool
	Stop() error
}

// Session — одна ігрова сесія.
type Session struct {
	AccountID   string
	BuildID     string
	AccountName string
	BuildName   string
	StartedAt   time.Time
	Launcher    launcherLike
}

// Manager — реєстр активних сесій.
type Manager struct {
	mu         sync.Mutex
	sessions   map[Key]*Session
	runningNow bool
	onState    []func(bool)
}

func NewManager() *Manager {
	return &Manager{sessions: map[Key]*Session{}}
}

// OnRunningStateChange реєструє callback, який викликається СИНХРОННО, коли
// змінюється агрегований стан «чи запущена хоч одна гра» (false→true або
// true→false). Використовується для зниження пріоритету процесу лаунчера
// під час гри і його відновлення після. Пакет sessions не знає про Windows
// і пріоритети — це чистий спостережувач, який App наповнює своїм хендлером.
// OnRunningStateChange реєструє callback, який викликається СИНХРОННО, коли
// змінюється агрегований стан «чи запущена хоч одна гра» (false→true або
// true→false). Використовується для зниження пріоритету процесу лаунчера
// під час гри і його відновлення після. Пакет sessions не знає про Windows
// і пріоритети — це чистий спостережувач, який App наповнює своїм хендлером.
func (m *Manager) OnRunningStateChange(fn func(bool)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.onState {
		if m.onState[i] == nil {
			m.onState[i] = fn
			return
		}
	}
	m.onState = append(m.onState, fn)
}

// Recheck перераховує агрегований стан «чи запущена гра» і, якщо він
// змінився, сповіщає спостерігачів. Потрібно, бо Start реєструє сесію ДО
// фактичного старту процесу (Launch викликається після) — на момент Start
// IsRunning ще false, тож перехід false→true має бути зафіксований пізніше,
// коли App упевнився, що гра справді запустилась.
func (m *Manager) Recheck() {
	m.mu.Lock()
	m.notifyRunning()
	m.mu.Unlock()
}

// notifyRunning перераховує поточний стан AnyRunning і, якщо він змінився,
// сповіщає всіх спостерігачів. Викликається з мутахісованих методів після
// кожної мутації реєстру. Нічого не робить, якщо стан не змінився (захист від
// зайвих викликів обробника під час звичайних Start/Remove).
func (m *Manager) notifyRunning() {
	running := false
	for _, s := range m.sessions {
		if s.Launcher != nil && s.Launcher.IsRunning() {
			running = true
			break
		}
	}
	if running == m.runningNow {
		return
	}
	m.runningNow = running
	fns := append([]func(bool){}, m.onState...)
	for _, fn := range fns {
		if fn != nil {
			fn(running)
		}
	}
}

// Start реєструє нову сесію. Помилка, якщо для цієї пари (акаунт, збірка)
// вже є активна сесія — дублювати запуск не можна.
func (m *Manager) Start(s *Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s == nil || s.Launcher == nil {
		return fmt.Errorf("nil session/launcher")
	}
	key := Key{AccountID: s.AccountID, BuildID: s.BuildID}
	if cur := m.sessions[key]; cur != nil && cur.Launcher.IsRunning() {
		return fmt.Errorf("build already running")
	}
	// StartedAt ставимо ЛИШЕ якщо викликач не задав свій (відновлення
	// сесії після рестарту лаунчера передає збережений час старту, щоб
	// таймер «у сесії» не обнулявся).
	if s.StartedAt.IsZero() {
		s.StartedAt = time.Now()
	}
	m.sessions[key] = s
	m.notifyRunning()
	return nil
}

// Get повертає активну сесію для пари (акаунт, збірка).
func (m *Manager) Get(accountID, buildID string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[Key{AccountID: accountID, BuildID: buildID}]
}

// IsRunning — чи запущена конкретна сесія.
func (m *Manager) IsRunning(accountID, buildID string) bool {
	s := m.Get(accountID, buildID)
	return s != nil && s.Launcher.IsRunning()
}

// RunningOnBuild повертає сесію, на якій збірка запущена на БУДЬ-ЯКОМУ
// акаунті (перша знайдена). Використовується для RunningOn у View.
func (m *Manager) RunningOnBuild(buildID string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sessions {
		if s.BuildID == buildID && s.Launcher.IsRunning() {
			return s
		}
	}
	return nil
}

// RunningOnBuildAccounts повертає ВСІ активні сесії, на яких збірка запущена
// (на будь-яких акаунтах). Та сама збірка може працювати на кількох акаунтах
// одночасно — цей список живить голови акаунтів у sidebar/картці/сторінці
// збірки. Відсортований за іменем акаунта (детермінований порядок в UI).
func (m *Manager) RunningOnBuildAccounts(buildID string) []*Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Session, 0, 2)
	for _, s := range m.sessions {
		if s.BuildID == buildID && s.Launcher.IsRunning() {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AccountName < out[j].AccountName })
	return out
}

// AnyRunning — чи запущена будь-яка сесія (будь-який акаунт/збірка).
// Використовується для трею («гра запущена») та IsGameRunning.
func (m *Manager) AnyRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sessions {
		if s.Launcher.IsRunning() {
			return true
		}
	}
	return false
}

// AnyRunningForAccount — чи запущена будь-яка збірка на вказаному акаунті.
func (m *Manager) AnyRunningForAccount(accountID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sessions {
		if s.AccountID == accountID && s.Launcher.IsRunning() {
			return true
		}
	}
	return false
}

// Remove виключає сесію з реєстру (викликається після виходу процесу).
func (m *Manager) Remove(accountID, buildID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, Key{AccountID: accountID, BuildID: buildID})
	m.notifyRunning()
}

// Stop зупиняє сесію (кидає сигнал процесу) і виключає з реєстру.
func (m *Manager) Stop(accountID, buildID string) error {
	m.mu.Lock()
	s := m.sessions[Key{AccountID: accountID, BuildID: buildID}]
	delete(m.sessions, Key{AccountID: accountID, BuildID: buildID})
	m.mu.Unlock()
	if s == nil || s.Launcher == nil {
		return fmt.Errorf("no running session for this build")
	}
	err := s.Launcher.Stop()
	m.notifyRunning()
	return err
}

// StopAll зупиняє всі активні сесії. Повертає список зупинених пар.
func (m *Manager) StopAll() []Key {
	m.mu.Lock()
	defer m.mu.Unlock()
	var keys []Key
	for k, s := range m.sessions {
		if s.Launcher != nil {
			_ = s.Launcher.Stop()
		}
		keys = append(keys, k)
		delete(m.sessions, k)
	}
	m.notifyRunning()
	return keys
}

// Snapshot повертає копію активних сесій (для стартового стану фронтенду).
func (m *Manager) Snapshot() []*Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, s)
	}
	return out
}
