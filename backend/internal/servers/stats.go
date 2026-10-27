// stats.go — збір статистики запущеного сервера в реальному часі.
// RAM (RSS процесу), CPU (%), кількість гравців (з логів та /list), uptime.
package servers

import (
	"shaurma-launcher-backend/internal/crashguard"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// LiveStats — поточна статистика сервера.
//
// Два РІЗНІ джерела пам'яті (не плутати):
//   • RAMMB — RSS процесу (скільки фізичної пам'яті тримає java.exe). Для
//     JVM це ближче до «скільки виділено/закомічено», ніж до «скільки
//     реально зайнято об'єктами»;
//   • HeapUsedMB/HeapCommittedMB — купа З САМОГО ЗВІТУ JVM (GC-лог). Це
//     те число, яке користувач бачить у `jcmd GC.heap_info`/GC-лозі.
//     HeapSource="jvm" коли дані вже є, "rss" — коли JVM ще не залогувала
//     жодного збирання (тоді UI чесно показує лише RSS).
type LiveStats struct {
	RAMMB int64 `json:"ramMB"` // RSS пам'яті процесу в МБ
	// CPUPercent — завантаження процесора у ВІДСОТКАХ ВІД УСІЄЇ МАШИНИ
	// (0-100), як у диспетчері задачі, а не «відсотках одного ядра». Раніше
	// тут було 0-100*cores: багатопотокова JVM штатно тримає 200-800 %,
	// тож будь-який UI зі смугою 0-100 (ServerMetricsPanel) обрізав значення
	// по Math.min(100, …) і завжди показував «CPU 100 %». Ділення на
	// кількість ядер робиться в collectLoop.
	CPUPercent float64 `json:"cpuPercent"`
	// Пам'ять купи JVM (з GC-логу самої JVM).
	HeapUsedMB      float64 `json:"heapUsedMB"`      // жива купа після останнього збирання
	HeapCommittedMB float64 `json:"heapCommittedMB"` // закомічено JVM (0 = колектор не друкує, див. ZGC)
	HeapMaxMB       int64   `json:"heapMaxMB"`       // -Xmx сервера
	HeapSource      string  `json:"heapSource"`      // "jvm" | "rss"
	GCCount         int     `json:"gcCount"`         // кількість збирань зі старту
	GCLastPauseMs   float64 `json:"gcLastPauseMs"`   // пауза останнього збирання
	GCPauseTotalMs  float64 `json:"gcPauseTotalMs"`  // сумарний час пауз
	GCLastAtMs      int64   `json:"gcLastAt"`        // коли було останнє збирання (unix ms)
	PlayerCount     int     `json:"playerCount"`     // кількість гравців
	MaxPlayers      int     `json:"maxPlayers"`      // max-players з server.properties
	UptimeSec       int64   `json:"uptimeSec"`       // час роботи
	PID             int     `json:"pid"`             // PID процесу
}

// statsCollector збирає статистику сервера кожну секунду.
type statsCollector struct {
	mu          sync.RWMutex
	running     map[string]*processInfo
	stats       map[string]*LiveStats
	playerCount map[string]*atomic.Int32
	maxCount    map[string]*atomic.Int32 // max-players (з пінгу або server.properties)
	stopCh      map[string]chan struct{}
}

var globalStats = &statsCollector{
	running:     make(map[string]*processInfo),
	stats:       make(map[string]*LiveStats),
	playerCount: make(map[string]*atomic.Int32),
	maxCount:    make(map[string]*atomic.Int32),
	stopCh:      make(map[string]chan struct{}),
}

// StartCollection починає збір статистики для сервера.
// port — порт, на який слухає сервер (для живих status-ping-ів, що дають
// ТОЧНУ кількість гравців, а не здогад з логів), dir — тека сервера
// (початкове max-players береться з server.properties), heapMaxMB — -Xmx
// сервера (максимум купи для UI).
func StartStatsCollection(serverID string, pi *processInfo, port int, dir string, heapMaxMB int) {
	stop := make(chan struct{})
	pc := &atomic.Int32{}
	mc := &atomic.Int32{}
	mc.Store(int32(ServerPropertiesMaxPlayers(dir)))

	globalStats.mu.Lock()
	globalStats.running[serverID] = pi
	globalStats.stats[serverID] = &LiveStats{
		PID:          pi.cmd.Process.Pid,
		MaxPlayers:   int(mc.Load()),
		HeapMaxMB:    int64(heapMaxMB),
		HeapSource:   "rss",
	}
	globalStats.playerCount[serverID] = pc
	globalStats.maxCount[serverID] = mc
	globalStats.stopCh[serverID] = stop
	globalStats.mu.Unlock()

	{
		_f, _a0, _a1, _a2, _a3 := collectLoop, serverID, pi, stop, NewGCLogReader(dir)
		go func() {
			defer crashguard.Recover("collectLoop")
			_f(_a0, _a1, _a2, _a3)
		}()
	}
	if port > 0 {
		{
			_f, _a0, _a1, _a2 := pingLoop, serverID, port, stop
			go func() {
				defer crashguard.Recover("pingLoop")
				_f(_a0, _a1, _a2)
			}()
		}
	}
}

// StopCollection зупиняє збір статистики.
func StopStatsCollection(serverID string) {
	globalStats.mu.Lock()
	ch, ok := globalStats.stopCh[serverID]
	delete(globalStats.running, serverID)
	delete(globalStats.stats, serverID)
	delete(globalStats.playerCount, serverID)
	delete(globalStats.maxCount, serverID)
	delete(globalStats.stopCh, serverID)
	globalStats.mu.Unlock()
	if ok {
		close(ch)
	}
}

// GetStats повертає поточну статистику сервера.
func GetStats(serverID string) LiveStats {
	globalStats.mu.RLock()
	defer globalStats.mu.RUnlock()
	s, ok := globalStats.stats[serverID]
	if !ok {
		return LiveStats{}
	}
	s.PlayerCount = int(globalStats.playerCount[serverID].Load())
	s.MaxPlayers = int(globalStats.maxCount[serverID].Load())
	return *s
}

// UpdatePlayerCount оновлює кількість гравців з рядка логу.
func UpdatePlayerCount(serverID string, line string) {
	// Шукаємо патерни:
	// "There are X of a max of Y players online"
	// "[Server] INFO: There are X of a max of Y players online"
	// "Server: X/Y players"
	// "player joined" / "player left" — для inkrementації
	globalStats.mu.RLock()
	pc, ok := globalStats.playerCount[serverID]
	globalStats.mu.RUnlock()
	if !ok {
		return
	}

	// Повний підрахунок з /list
	if idx := strings.Index(line, " of a max of "); idx > 0 {
		prefix := line[:idx]
		// Знаходимо останнє число перед " of a max of "
		words := strings.Fields(prefix)
		if len(words) > 0 {
			if n, err := strconv.Atoi(words[len(words)-1]); err == nil {
				pc.Store(int32(n))
				return
			}
		}
	}

	// "X/Y players"
	if idx := strings.Index(line, "/"); idx > 0 {
		suffix := line[idx+1:]
		if strings.HasPrefix(suffix, " ") {
			suffix = suffix[1:]
		}
		if end := strings.Index(suffix, " player"); end > 0 {
			val := suffix[:end]
			if n, err := strconv.Atoi(val); err == nil {
				// Це maxPlayers, а не поточна кількість.
				_ = n
			}
		}
	}
}

// pingLoop кожні 5 секунд робить реальний status-ping сервера (той самий,
// що клієнт Minecraft у списку серверів) і оновлює playerCount/maxCount.
// Це надійніше за парсинг логів: рядки "X of a max of Y players online"
// зʼявляються лише коли хтось заходить/виходить, тому лічильник застрягав
// на 0 навіть на заповненому сервері.
func pingLoop(serverID string, port int, stop <-chan struct{}) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			st, err := PingMCStatus(port, 1500*time.Millisecond)
			if err != nil || !st.Online {
				// Сервер ще стартує / уже зупиняється / не слухає порт —
				// залишаємо останнє відоме значення, не обнуляємо.
				continue
			}
			globalStats.mu.RLock()
			pc := globalStats.playerCount[serverID]
			mc := globalStats.maxCount[serverID]
			_, ok := globalStats.stats[serverID]
			globalStats.mu.RUnlock()
			if !ok {
				return
			}
			if pc != nil {
				pc.Store(int32(st.PlayersOnline))
			}
			if mc != nil && st.MaxPlayers > 0 {
				mc.Store(int32(st.MaxPlayers))
			}
		}
	}
}

// collectLoop збирає RAM/CPU кожну секунду і дочитує GC-лог JVM
// (gc — інкрементальний читач: за тик читає лише нові рядки).
func collectLoop(serverID string, pi *processInfo, stop <-chan struct{}, gc *GCLogReader) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	var lastCPU time.Duration
	var lastCPUSet bool
	var lastWall time.Time

	for {
		select {
		case <-stop:
			return
		case now := <-ticker.C:
			globalStats.mu.RLock()
			s, ok := globalStats.stats[serverID]
			globalStats.mu.RUnlock()
			if !ok || pi.cmd.Process == nil {
				return
			}

			// Uptime
			pi.mu.Lock()
			start := pi.startTime
			pi.mu.Unlock()
			s.UptimeSec = int64(now.Sub(start).Seconds())

			// RAM (RSS процесу — «скільки тримає java.exe»).
			s.RAMMB = getProcessRAM(pi.cmd.Process.Pid)

			// Пам'ять КУПИ — з власного GC-звіту JVM (не з ОС).
			gc.Poll()
			sample, gcCount, pauseTotal, valid := gc.Snapshot()
			if valid {
				s.HeapUsedMB = sample.UsedMB
				s.HeapCommittedMB = sample.CommittedMB
				s.HeapSource = "jvm"
				s.GCCount = gcCount
				s.GCLastPauseMs = sample.PauseMs
				s.GCPauseTotalMs = pauseTotal
				if !sample.At.IsZero() {
					s.GCLastAtMs = sample.At.UnixMilli()
				}
			} else {
				// Ще жодного збирання в лозі (сервер тільки стартував) —
				// лишаємо RSS як єдине доступне джерело.
				s.HeapSource = "rss"
			}

			// CPU
			if lastWall.IsZero() {
				lastWall = now
			}
			elapsed := now.Sub(lastWall).Seconds()
			if elapsed > 0.5 {
				currentCPU := getProcessCPU(pi.cmd.Process.Pid)
				if lastCPUSet {
					cpuDelta := currentCPU - lastCPU
					// cpuDelta — час CPU процесу по ВСІХ потоках, elapsed — реальний
					// час. Їх відношення дає частку ОДНОГО ядра (0-100*cores).
					perCore := float64(cpuDelta) / float64(time.Duration(int64(elapsed*float64(time.Second)))) * 100
					// Ділимо на кількість ядер — отримуємо чесні 0-100 % від машини.
					cores := float64(runtime.NumCPU())
					if cores < 1 {
						cores = 1
					}
					s.CPUPercent = perCore / cores
					if s.CPUPercent > 100 {
						s.CPUPercent = 100
					}
				}
				lastCPU = currentCPU
				lastCPUSet = true
				lastWall = now
			}
		}
	}
}

// getProcessRAM та getProcessCPU визначені в stats_windows.go / stats_unix.go

// DetectPlayerCountFromLog аналізує рядок логу та повертає кількість гравців (якщо знайдено).
func DetectPlayerCountFromLog(line string) (int, bool) {
	// "There are X of a max of Y players online"
	if strings.Contains(line, "of a max of") && strings.Contains(line, "players online") {
		words := strings.Fields(line)
		for i, w := range words {
			if w == "are" && i+1 < len(words) {
				if n, err := strconv.Atoi(words[i+1]); err == nil {
					return n, true
				}
			}
		}
	}

	// "X player(s) online"
	if strings.Contains(line, "player") && strings.Contains(line, "online") {
		words := strings.Fields(line)
		for _, w := range words {
			if n, err := strconv.Atoi(w); err == nil {
				return n, true
			}
		}
	}

	return 0, false
}

// ServerPropertiesMaxPlayers читає max-players з server.properties.
func ServerPropertiesMaxPlayers(dir string) int {
	data, err := os.ReadFile(fmt.Sprintf("%s/server.properties", dir))
	if err != nil {
		return 20 // default
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "max-players=") {
			val := strings.TrimPrefix(line, "max-players=")
			if n, err := strconv.Atoi(strings.TrimSpace(val)); err == nil && n > 0 {
				return n
			}
		}
	}
	return 20
}
