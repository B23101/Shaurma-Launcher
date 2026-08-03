// Пакет sessions — менеджер незалежних ігрових процесів по акаунтах.
//
// Кожна (акаунт, збірка) пара має власний екземпляр minecraft.Launcher
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
	"sync"
	"time"

	"shaurma-launcher-wails/internal/minecraft"
)

// Key — унікальний ідентифікатор сесії (акаунт × збірка).
type Key struct {
	AccountID string
	BuildID   string
}

// Session — одна ігрова сесія.
type Session struct {
	AccountID   string
	BuildID     string
	AccountName string
	BuildName   string
	StartedAt   time.Time
	Launcher    *minecraft.Launcher
}

// Manager — реєстр активних сесій.
type Manager struct {
	mu       sync.Mutex
	sessions map[Key]*Session
}

func NewManager() *Manager {
	return &Manager{sessions: map[Key]*Session{}}
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
	s.StartedAt = time.Now()
	m.sessions[key] = s
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
	return s.Launcher.Stop()
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
