// ── Discord Rich Presence ────────────────────────────────────────────────
// Менеджер підтримує з'єднання з Discord через IPC (Windows named pipe
// \\.\pipe\discord-ipc-N) і публікує активність за запущеними збірками:
//
//	· одна збірка  → Details = шаблон користувача ({pack} = назва збірки),
//	  State = шаблон ({list} = «Майнкрафт · {loader} {mc}»);
//	· кілька збірок → Details = «{count} збірки», State = список назв
//	  ({pack} підставляється у множині, {list} перераховує збірки);
//	· жодної збірки → присутність очищається (Discord прибирає статус).
//
// З'єднання відновлюється автоматично (кожні ~5 с), якщо Discord закрили
// або перезапустили; після перепідключення остання активність пересилається
// заново. IPC-клієнт реалізовано на голому golang.org/x/sys/windows
// (без сторонніх залежностей) — див. ipc_windows.go / ipc_stub.go.
package discordrpc

import (
	"shaurma-launcher-backend/internal/crashguard"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	ipcOpHandshake = 0
	ipcOpFrame     = 1
)

// BuildInfo — опис запущеної збірки, з якої менеджер будує активність.
type BuildInfo struct {
	ID        string
	Name      string
	Loader    string
	MCVersion string
	// StartTS — Unix-час (секунди) старту сесії; для мульти-показу береться
	// найраніший з усіх (Discord показує «запущено N хвилин»).
	StartTS int64
}

// activity — активність для SET_ACTIVITY.
type activity struct {
	Details string
	State   string
	StartTS int64
}

func (a *activity) toPayload() map[string]any {
	p := map[string]any{"type": 0}
	if a.Details != "" {
		p["details"] = a.Details
	}
	if a.State != "" {
		p["state"] = a.State
	}
	if a.StartTS > 0 {
		p["timestamps"] = map[string]any{"start": a.StartTS}
	}
	return p
}

// ipcClient — одне живе з'єднання з Discord (обслуговується serve-циклом).
type ipcClient struct {
	conn *ipcConn
	m    *Manager

	readyOnce  sync.Once
	closedOnce sync.Once
	ready      chan struct{}
	closed     chan struct{}
}

func newIPCClient(m *Manager, conn *ipcConn) *ipcClient {
	return &ipcClient{conn: conn, m: m, ready: make(chan struct{}), closed: make(chan struct{})}
}

// run тримає з'єднання живим: handshake → READY → розсилка поточної
// активності → очікування закриття/стопу. Повертає помилку, коли
// з'єднання впало (serve-цикл перепідключиться).
func (c *ipcClient) run() error {
	hs, _ := json.Marshal(map[string]any{"v": 1, "client_id": c.m.appID})
	if err := c.conn.write(ipcOpHandshake, hs); err != nil {
		return err
	}
	{
		_f := c.readLoop
		go func() {
			defer crashguard.Recover("c.readLoop")
			_f()
		}()
	}
	select {
	case <-c.ready:
		// З'єднання встановлено — надсилаємо поточну активність.
		// serve() підключається ЛИШЕ коли lastActivity != nil, тому
		// тут завжди є що надсилати. На випадок race-condition (збірка
		// зупинилась між перевіркою serve і ready) — перевіряємо знову.
		go func() {
			defer crashguard.Recover("manager.go:94")
			c.m.mu.Lock()
			act := c.m.lastActivity
			c.m.mu.Unlock()
			if act != nil {
				c.send(act)
			} else {
				// Збірка зупинилась поки підключались — закриваємо
				{
					_f := c.conn.close
					go func() {
						defer crashguard.Recover("c.conn.close")
						_f()
					}()
				}
			}
		}()
	case <-c.closed:
		return fmt.Errorf("discord ipc closed during handshake")
	case <-c.m.stopCh:
		return nil
	case <-time.After(5 * time.Second):
		return fmt.Errorf("discord ipc READY timeout")
	}
	select {
	case <-c.closed:
		return fmt.Errorf("discord ipc closed")
	case <-c.m.stopCh:
		return nil
	}
}

// readLoop читає фрейми IPC. Підтримує часткові читання (збирає фрейми з
// буфера). Закриває канал closed, коли Discord від'єднався.
func (c *ipcClient) readLoop() {
	var pending []byte
	tmp := make([]byte, 8192)
	for {
		n, err := c.conn.read(tmp)
		if err != nil {
			c.closedOnce.Do(func() { close(c.closed) })
			return
		}
		pending = append(pending, tmp[:n]...)
		for len(pending) >= 8 {
			ln := int(binary.LittleEndian.Uint32(pending[4:8]))
			if len(pending) < 8+ln {
				break
			}
			op := binary.LittleEndian.Uint32(pending[0:4])
			frame := append([]byte(nil), pending[8:8+ln]...)
			pending = pending[8+ln:]
			if op == ipcOpFrame {
				var ev struct {
					Cmd string `json:"cmd"`
					Evt string `json:"evt"`
				}
				if json.Unmarshal(frame, &ev) == nil && ev.Cmd == "DISPATCH" && ev.Evt == "READY" {
					c.readyOnce.Do(func() { close(c.ready) })
				}
			}
		}
		if len(pending) > 1<<20 {
			pending = pending[:0]
		}
	}
}

// send пише SET_ACTIVITY (nil → очищення активності).
func (c *ipcClient) send(a *activity) {
	var payload map[string]any
	if a == nil {
		payload = map[string]any{"cmd": "SET_ACTIVITY", "args": map[string]any{"pid": c.m.pid, "activity": map[string]any{}}, "nonce": c.m.nonce()}
	} else {
		payload = map[string]any{"cmd": "SET_ACTIVITY", "args": map[string]any{"pid": c.m.pid, "activity": a.toPayload()}, "nonce": c.m.nonce()}
	}
	data, _ := json.Marshal(payload)
	_ = c.conn.write(ipcOpFrame, data)
}

// Manager — публічний інтерфейс Rich Presence для лаунчера.
type Manager struct {
	mu    sync.Mutex
	appID string
	pid   uint32

	enabled      bool
	detailsTpl   string
	stateTpl     string
	running      map[string]BuildInfo
	lastActivity *activity
	client       *ipcClient

	stopCh chan struct{}
	nonceN uint64
}

// New створює менеджер. appID — Application ID з Discord Developer Portal
// (підстав свій; присутність показується лише для зареєстрованого застосунку).
// enabled — увімкнено чи ні (можна змінити пізніше SetConfig).
// detailsTpl/stateTpl — шаблони з плейсхолдерами {pack}/{list}/{count}/
// {loader}/{mc}; порожні рядки беруть розумні дефолти.
func New(appID string, pid uint32, enabled bool, detailsTpl, stateTpl string) *Manager {
	if detailsTpl == "" {
		detailsTpl = "{pack}"
	}
	if stateTpl == "" {
		stateTpl = "{list}"
	}
	return &Manager{
		appID: appID, pid: pid, enabled: enabled,
		detailsTpl: detailsTpl, stateTpl: stateTpl,
		running: map[string]BuildInfo{},
		stopCh:  make(chan struct{}),
	}
}

// Start запускає serve-цикл (горутина). Викликати один раз.
func (m *Manager) Start() { go m.serve() }

// Stop завершує serve-цикл (при виході лаунчера). Discord сам прибирає
// присутність, коли процес-власник (pid) завершується.
func (m *Manager) Stop() {
	m.mu.Lock()
	m.enabled = false
	m.running = map[string]BuildInfo{}
	// Закриваємо ЖИВЕ з'єднання, якщо воно є: serve() міг бути всередині
	// cl.run() (readLoop блокується на читанні pipe). Без явного close()
	// pipe лишався відкритим, поки serve() не дійде до conn.close() після
	// виходу з cl.run() — а той вихід у найгіршому випадку чекає на
	// READY-timeout. CancelIoEx у conn.close() надійно розбудить readLoop.
	if m.client != nil {
		{
			_f := m.client.conn.close
			go func() {
				defer crashguard.Recover("m.client.conn.close")
				_f()
			}()
		}
	}
	m.client = nil
	m.mu.Unlock()
	select {
	case <-m.stopCh:
	default:
		close(m.stopCh)
	}
}

// serve — головний цикл: підключається до Discord ЛИШЕ коли є активні
// збірки (lastActivity != nil). Як тільки всі збірки завершились —
// відключається, щоб лаунчер не показувався у Discord без гри.
func (m *Manager) serve() {
	for {
		select {
		case <-m.stopCh:
			return
		default:
		}

		m.mu.Lock()
		enabled := m.enabled
		hasActivity := m.lastActivity != nil
		m.mu.Unlock()

		// Якщо вимкнено або немає активних збірок — не підключаємось.
		// Чекаємо і перевіряємо знову.
		if !enabled || !hasActivity {
			select {
			case <-m.stopCh:
				return
			case <-time.After(2 * time.Second):
			}
			continue
		}

		conn, err := ipcDial()
		if err != nil {
			select {
			case <-m.stopCh:
				return
			case <-time.After(5 * time.Second):
			}
			continue
		}
		cl := newIPCClient(m, conn)
		m.mu.Lock()
		m.client = cl
		m.mu.Unlock()
		_ = cl.run()
		m.mu.Lock()
		if m.client == cl {
			m.client = nil
		}
		m.mu.Unlock()
		conn.close()

		// Якщо після завершення з'єднання вже немає збірок —
		// не перепідключаємось, а просто чекаємо наступної збірки.
		select {
		case <-m.stopCh:
			return
		case <-time.After(5 * time.Second):
		}
	}
}

// SetConfig оновлює налаштування (увімкнено/шаблони) і одразу перераховує
// активність. Викликається з SaveSettings/CompleteSetup.
func (m *Manager) SetConfig(enabled bool, detailsTpl, stateTpl string) {
	m.mu.Lock()
	m.enabled = enabled
	if detailsTpl != "" {
		m.detailsTpl = detailsTpl
	}
	if stateTpl != "" {
		m.stateTpl = stateTpl
	}
	m.mu.Unlock()
	m.rebuild()
}

// AddBuild — збірка стартувала. Викликати при build:started (і при
// відновленні сесії після перезапуску лаунчера).
func (m *Manager) AddBuild(b BuildInfo) {
	m.mu.Lock()
	if b.StartTS == 0 {
		b.StartTS = time.Now().Unix()
	}
	m.running[b.ID] = b
	m.mu.Unlock()
	m.rebuild()
}

// RemoveBuild — збірка завершилась (build:exit).
func (m *Manager) RemoveBuild(id string) {
	m.mu.Lock()
	delete(m.running, id)
	m.mu.Unlock()
	m.rebuild()
}

// rebuild рахує активність з поточного стану і відправляє її (або
// відключається від Discord якщо збірок немає).
func (m *Manager) rebuild() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.enabled || len(m.running) == 0 {
		m.lastActivity = nil
		// Якщо з'єднання активне — закриваємо його. Discord автоматично
		// прибере присутність коли з'єднання закриється. Ми НЕ надсилаємо
		// SET_ACTIVITY з порожнім об'єктом перед закриттям, бо деякі клієнти
		// Discord ігнорують очищення і все одно показують останній стан —
		// надійніше просто відрубати з'єднання.
		if m.client != nil {
			{
				_f := m.client.conn.close
				go func() {
					defer crashguard.Recover("m.client.conn.close")
					_f()
				}()
			}
			m.client = nil
		}
		return
	}
	names := make([]string, 0, len(m.running))
	var loader, mc string
	var start int64
	for _, b := range m.running {
		names = append(names, b.Name)
		if loader == "" {
			loader = b.Loader
		}
		if mc == "" {
			mc = b.MCVersion
		}
		if b.StartTS > 0 && (start == 0 || b.StartTS < start) {
			start = b.StartTS
		}
	}
	sort.Strings(names)
	count := len(m.running)
	packVal := names[0]
	if count > 1 {
		packVal = fmt.Sprintf("%d збірки", count)
	}
	listVal := strings.TrimSpace(fmt.Sprintf("Майнкрафт %s %s", loader, mc))
	if count > 1 {
		listVal = strings.Join(names, ", ")
	}
	repl := map[string]string{
		"{pack}":   packVal,
		"{list}":   listVal,
		"{count}":  fmt.Sprintf("%d", count),
		"{loader}": loader,
		"{mc}":     mc,
	}
	m.lastActivity = &activity{
		Details: applyTpl(m.detailsTpl, repl),
		State:   applyTpl(m.stateTpl, repl),
		StartTS: start,
	}
	m.pushLocked()
}

// pushLocked надсилає поточну активність на живе з'єднання (якщо є).
// Викликається з м'ютексом.
func (m *Manager) pushLocked() {
	cl := m.client
	if cl == nil {
		return
	}
	act := m.lastActivity
	{
		_f, _a0 := cl.send, act
		go func() {
			defer crashguard.Recover("cl.send")
			_f(_a0)
		}()
	}
}

func (m *Manager) nonce() string {
	m.nonceN++
	return fmt.Sprintf("shrm-%d-%d", time.Now().UnixNano(), m.nonceN)
}

// applyTpl підставляє плейсхолдери у шаблон і прибирає зайві пробіли.
func applyTpl(tpl string, repl map[string]string) string {
	out := tpl
	for k, v := range repl {
		out = strings.ReplaceAll(out, k, v)
	}
	return strings.TrimSpace(out)
}
