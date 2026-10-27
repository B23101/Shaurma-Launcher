// recover.go — відновлення керування сервером після краша лаунчера.
//
// ПРОБЛЕМА. Сервер запускається як окремий java-процес (cmd.Start), і
// краш/примусове закриття лаунчера його НЕ вбиває: гра й далі працює, порт
// лишається зайнятим. Лаунчер же весь свій стан тримає в пам'яті
// (Manager.running), тож після перезапуску він «не знає» про живий процес:
// кнопка «Запустити» вела в перевірку порту й падала з «порт 25565
// зайнятий: невідомий процес», а власний сервер ставав для лаунчера чужим.
//
// РІШЕННЯ. Під час старту сервера поруч із його текою пишемо маленький
// runtime-запис (PID + час створення процесу + порт + ядро) і ПРИБИРАЄМО
// його, коли процес завершується. На старті лаунчера кожен такий запис
// перевіряється: якщо PID живий, час створення ЗБІГАЄТЬСЯ (захист від
// перевикористання PID) і це справді java — процес «усиновлюється»:
// Manager знову вважає сервер запущеним, а збір статистики (GC-лог і
// status-ping порту) продовжується, бо обидва джерела не залежать від
// втраченого pipe.
//
// ЧОГО НЕ МОЖНА ВІДНОВИТИ: stdin java (канал помер разом із лаунчером) —
// команди консолі повертають чесну помилку замість тихого «нічого не
// сталося». Вивід консолі відновлюється з logs/latest.log
// (recover_console.go): історія + живе «хвостіння».
package servers

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"shaurma-launcher-backend/internal/crashguard"
)

// runtimeRecord — те, що лаунчер лишає на диску про живий сервер.
type runtimeRecord struct {
	PID       int    `json:"pid"`
	CreatedNs int64  `json:"createdNs"` // час створення процесу (unix ns) — захист від перевикористання PID
	StartedAt int64  `json:"startedAt"` // unix ms, коли сервер стартував (для uptime)
	Core      string `json:"core"`
	Port      int    `json:"port"`
	Java      string `json:"java,omitempty"`
}

// runtimeRecordPath — <тека сервера>/.shaurma/server_runtime.json. Тека
// .shaurma вже використовується під GC-лог, launch_config.json і
// network.json, тож нічого нового на диску не з'являється.
func runtimeRecordPath(dir string) string {
	return filepath.Join(dir, ".shaurma", "server_runtime.json")
}

func writeRuntimeRecord(dir string, rec runtimeRecord) {
	path := runtimeRecordPath(dir)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return
	}
	_ = os.WriteFile(path, b, 0644)
}

func readRuntimeRecord(dir string) (runtimeRecord, bool) {
	b, err := os.ReadFile(runtimeRecordPath(dir))
	if err != nil {
		return runtimeRecord{}, false
	}
	var rec runtimeRecord
	if err := json.Unmarshal(b, &rec); err != nil || rec.PID <= 0 {
		return runtimeRecord{}, false
	}
	return rec, true
}

func removeRuntimeRecord(dir string) {
	_ = os.Remove(runtimeRecordPath(dir))
}

// recoveredStdin — заглушка stdin для «усиновленого» процесу: канал помер
// разом із лаунчером, писати команди нікуди. Write завжди повертає
// помилку, щоб виклики (SendCommand, graceful Stop) дали користувачу
// зрозумілу відповідь, а не паніку на nil-інтерфейсі.
type recoveredStdin struct{}

var errConsoleLost = errRecoveredConsole{}

type errRecoveredConsole struct{}

func (errRecoveredConsole) Error() string {
	return "консоль сервера недоступна: процес лишився від попереднього запуску лаунчера — команди передати неможливо (зупиніть сервер кнопкою «Стоп»)"
}

func (recoveredStdin) Write([]byte) (int, error) { return 0, errConsoleLost }
func (recoveredStdin) Close() error              { return nil }

// RecoverOrphanedServers — «усиновлення» живих java-процесів після
// перезапуску лаунчера. Повертає ID серверів, керування якими відновлено.
//
// portOf — порт сервера «як є зараз» (server.properties має пріоритет над
// записом у runtime.json, бо гравець міг змінити його вручну).
func (a *API) RecoverOrphanedServers() []string {
	return a.manager.AdoptOrphanedServers(a.store.LoadAll(), func(s *Server) int {
		return a.effectiveServerPort(s)
	})
}

// AdoptOrphanedServers перевіряє runtime-записи всіх серверів і бере під
// контроль ті java-процеси, що справді лишились живими. Застарілі записи
// (процес завершився, PID перевикористано іншим процесом) прибираються.
func (m *Manager) AdoptOrphanedServers(servers map[string]*Server, portOf func(*Server) int) []string {
	var adopted []string
	for id, srv := range servers {
		rec, ok := readRuntimeRecord(srv.Dir)
		if !ok {
			continue
		}
		if m.adoptRecord(id, srv, rec, portOf, isJavaImage) {
			adopted = append(adopted, id)
		}
	}
	return adopted
}

// adoptRecord усиновлює ОДИН сервер: перевіряє ідентичність процесу,
// реєструє його в Manager, вмикає статистику й спостереження за виходом.
// imageOK — перевірка образу процесу (в історії тесту інжектиться
// заглушка, бо в тесті не запускається справжня java).
// Повертає true, якщо керування відновлено.
func (m *Manager) adoptRecord(id string, srv *Server, rec runtimeRecord, portOf func(*Server) int, imageOK func(string) bool) bool {
	snap := snapshotProcess(rec.PID)
	if !snap.Alive || !snap.matches(rec) || !imageOK(snap.Image) {
		// Процес завершився (штатно чи ні), або PID устиг перейти до
		// іншого процесу — запис більше не описує живий сервер.
		removeRuntimeRecord(srv.Dir)
		return false
	}

	m.mu.Lock()
	if _, exists := m.running[id]; exists {
		m.mu.Unlock()
		return false
	}
	port := rec.Port
	if portOf != nil {
		if p := portOf(srv); p > 0 {
			port = p
		}
	}
	proc, err := os.FindProcess(rec.PID)
	if err != nil {
		m.mu.Unlock()
		removeRuntimeRecord(srv.Dir)
		return false
	}
	started := time.UnixMilli(rec.StartedAt)
	pi := &processInfo{
		cmd:       &exec.Cmd{Process: proc},
		stdin:     recoveredStdin{},
		startTime: started,
		status:    ServerStatus{Status: "running", PID: rec.PID, Recovered: true},
		core:      rec.Core,
		dir:       srv.Dir,
		exited:    make(chan struct{}),
		recovered: true,
	}
	m.running[id] = pi
	m.mu.Unlock()

	// Статистика продовжується з тих самих джерел, що не залежать від
	// pipe: GC-лог (java дописує його сама) і status-ping порту.
	StartStatsCollection(id, pi, port, srv.Dir, srv.RAMMB)

	// Консоль: вивід сервера пише сам сервер у logs/shaurma-console.log
	// через успадкований дескриптор (console_file.go), тож після перезапуску
	// показуємо ЙОГО хвіст (уся історія, включно з тим, що було написано,
	// поки лаунчера не існувало) і далі стежимо за файлом. Ввід команд
	// недоступний — stdin помер разом із лаунчером.
	buf := m.EnsureBuffer(id)
	logOffset := m.loadRecoveredHistory(id, srv.Dir, buf)
	m.NotifyInstallLine(id, "[SYSTEM] Керування відновлено після перезапуску лаунчера (PID "+strconv.Itoa(rec.PID)+"). Сервер працює далі; вивід консолі повністю відновлено з logs/"+ServerConsoleLogName+", а команди недоступні — зупиніть сервер кнопкою «Стоп».")
	m.notifyStatus(id, pi.status)

	go func() {
		defer crashguard.Recover("m.followLogFile")
		m.followLogFile(id, pi, buf, consoleSourcePath(srv.Dir), logOffset)
	}()

	{
		_f, _a0, _a1, _a2 := m.watchAdopted, id, rec.PID, rec.CreatedNs
		go func() {
			defer crashguard.Recover("m.watchAdopted")
			_f(_a0, _a1, _a2)
		}()
	}
	return true
}

// matches — чи знімок процесу відповідає запису. Час створення порівнюємо
// ЛИШЕ коли він є в обох (на macOS /proc немає — там довіряємо PID+java).
func (s processSnapshot) matches(rec runtimeRecord) bool {
	if rec.CreatedNs == 0 || s.CreateNs == 0 {
		return true
	}
	return s.CreateNs == rec.CreatedNs
}

// watchAdopted стежить за «усиновленим» процесом опитуванням (pipe, з якого
// читав watchProcess, для нього не існує). Коли процес зникає — робимо ту
// саму фіналізацію, що й watchProcess: знімаємо з running, зупиняємо
// статистику, повідомляємо статус і прибираємо runtime-запис.
func (m *Manager) watchAdopted(serverID string, pid int, createdNs int64) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		m.mu.RLock()
		pi, ok := m.running[serverID]
		m.mu.RUnlock()
		if !ok {
			return
		}
		snap := snapshotProcess(pid)
		if snap.Alive && (createdNs == 0 || snap.CreateNs == 0 || snap.CreateNs == createdNs) {
			continue
		}
		m.finalizeAdopted(serverID, pi)
		return
	}
}

// finalizeAdopted — завершення відновленого сервера (аналог хвоста
// watchProcess, але без Wait(): процес нам не належить).
func (m *Manager) finalizeAdopted(serverID string, pi *processInfo) {
	m.mu.Lock()
	cur, ok := m.running[serverID]
	if !ok || cur != pi {
		m.mu.Unlock()
		return
	}
	pi.mu.Lock()
	// Фінальний статус — завжди «stopped» (штатно чи ні, код виходу чужого
	// процесу ми не бачимо). Раніше при prev=="stopping" статус лишався
	// «stopping» — UI після кнопки «Стоп» на відновленому сервері висів
	// на «Зупиняється…» назавжди.
	pi.status.Status = "stopped"
	pi.status.Recovered = false
	s := pi.status
	pi.mu.Unlock()
	delete(m.running, serverID)
	m.mu.Unlock()

	StopStatsCollection(serverID)
	ClearOnlinePlayers(serverID)
	m.notifyPlayer(serverID, PlayerEvent{Action: "reset", At: time.Now().UnixMilli(), Count: 0, List: []string{}})
	m.notifyStatus(serverID, s)
	removeRuntimeRecord(pi.dir)
	select {
	case <-pi.exited:
	default:
		close(pi.exited)
	}
}

