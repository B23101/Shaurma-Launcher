// Package procmetrics збирає метрику ресурсів гри: CPU і RAM усього дерева
// процесів (Java + дочірні), піки, середні, розмір теки збірки та останній
// знімок сесії. Лише стандартна бібліотека.
package procmetrics

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sync"
	"time"
)

// MaxSamples — ліміт історії; при переповненні історія проріджується удвічі,
// тож графік завжди охоплює всю сесію.
const MaxSamples = 240

type procInfo struct {
	pid, ppid int
	cpuNs     uint64 // накопичений CPU-час
	rss       uint64 // байти
}

// Sample — одна точка: CPU у % від усієї потужності машини, RAM у МБ.
type Sample struct {
	T     int64   `json:"t"` // unix ms
	CPU   float64 `json:"cpu"`
	RAMMB float64 `json:"ramMb"`
	Procs int     `json:"procs"`
}

// Snapshot — стан метрик сесії (серіалізується для UI та на диск).
type Snapshot struct {
	PackID    string   `json:"packId"`
	Running   bool     `json:"running"`
	StartedAt int64    `json:"startedAt"`
	EndedAt   int64    `json:"endedAt"`
	Cores     int      `json:"cores"`
	Samples   []Sample `json:"samples"`
	CurCPU    float64  `json:"curCpu"`
	CurRAMMB  float64  `json:"curRamMb"`
	PeakCPU   float64  `json:"peakCpu"`
	PeakRAMMB float64  `json:"peakRamMb"`
	AvgCPU    float64  `json:"avgCpu"`
	AvgRAMMB  float64  `json:"avgRamMb"`
	DiskMB    int64    `json:"diskMb"`
}

// treeTotals сумує CPU/RAM процесу root і всіх його нащадків.
func treeTotals(list []procInfo, root int) (cpuNs, rss uint64, n int) {
	children := map[int][]int{}
	byPID := map[int]procInfo{}
	for _, p := range list {
		byPID[p.pid] = p
		children[p.ppid] = append(children[p.ppid], p.pid)
	}
	if _, ok := byPID[root]; !ok {
		return 0, 0, 0
	}
	seen := map[int]bool{}
	stack := []int{root}
	for len(stack) > 0 {
		pid := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[pid] {
			continue
		}
		seen[pid] = true
		p := byPID[pid]
		cpuNs += p.cpuNs
		rss += p.rss
		n++
		stack = append(stack, children[pid]...)
	}
	return
}

// Tracker стежить за одним деревом процесів.
type Tracker struct {
	mu       sync.Mutex
	packID   string
	root     int
	cores    int
	snap     Snapshot
	lastCPU  uint64
	lastWall time.Time
	primed   bool
	sumCPU   float64
	sumRAM   float64
	count    int
	list     func() []procInfo
	now      func() time.Time
}

// New створює трекер для кореневого PID (процес гри).
func New(packID string, rootPID int) *Tracker {
	cores := runtime.NumCPU()
	t := &Tracker{packID: packID, root: rootPID, cores: cores, list: listProcs, now: time.Now}
	t.snap = Snapshot{PackID: packID, Running: true, StartedAt: t.now().UnixMilli(), Cores: cores, Samples: []Sample{}}
	return t
}

// Sample знімає одну точку. Перший виклик лише «заряджає» лічильник CPU.
// Повертає false, коли дерево процесів зникло.
func (t *Tracker) Sample() bool {
	list := t.list()
	cpuNs, rss, n := treeTotals(list, t.root)
	t.mu.Lock()
	defer t.mu.Unlock()
	if n == 0 {
		return false
	}
	now := t.now()
	ram := float64(rss) / (1024 * 1024)
	if !t.primed {
		t.primed, t.lastCPU, t.lastWall = true, cpuNs, now
		t.record(Sample{T: now.UnixMilli(), CPU: 0, RAMMB: ram, Procs: n})
		return true
	}
	wall := now.Sub(t.lastWall).Seconds()
	cpu := 0.0
	if wall > 0 && cpuNs >= t.lastCPU {
		cpu = float64(cpuNs-t.lastCPU) / 1e9 / wall / float64(t.cores) * 100
	}
	if cpu > 100 {
		cpu = 100
	}
	t.lastCPU, t.lastWall = cpuNs, now
	t.record(Sample{T: now.UnixMilli(), CPU: round1(cpu), RAMMB: round1(ram), Procs: n})
	return true
}

func round1(v float64) float64 { return float64(int(v*10+0.5)) / 10 }

func (t *Tracker) record(s Sample) {
	t.snap.Samples = append(t.snap.Samples, s)
	if len(t.snap.Samples) > MaxSamples {
		kept := t.snap.Samples[:0:0]
		for i, x := range t.snap.Samples {
			if i%2 == 0 {
				kept = append(kept, x)
			}
		}
		t.snap.Samples = kept
	}
	t.snap.CurCPU, t.snap.CurRAMMB = s.CPU, s.RAMMB
	if s.CPU > t.snap.PeakCPU {
		t.snap.PeakCPU = s.CPU
	}
	if s.RAMMB > t.snap.PeakRAMMB {
		t.snap.PeakRAMMB = s.RAMMB
	}
	t.sumCPU += s.CPU
	t.sumRAM += s.RAMMB
	t.count++
	t.snap.AvgCPU = round1(t.sumCPU / float64(t.count))
	t.snap.AvgRAMMB = round1(t.sumRAM / float64(t.count))
}

// Run семплює з інтервалом, доки не завершиться ctx або процес.
func (t *Tracker) Run(ctx context.Context, every time.Duration, onSample func(Snapshot)) {
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		alive := t.Sample()
		if onSample != nil {
			onSample(t.Snapshot())
		}
		if !alive {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// Snapshot повертає копію поточного стану.
func (t *Tracker) Snapshot() Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := t.snap
	s.Samples = append([]Sample(nil), t.snap.Samples...)
	return s
}

// Finish позначає сесію завершеною й повертає фінальний знімок.
func (t *Tracker) Finish() Snapshot {
	t.mu.Lock()
	t.snap.Running = false
	t.snap.EndedAt = t.now().UnixMilli()
	t.mu.Unlock()
	return t.Snapshot()
}

// SetDiskMB записує розмір теки збірки.
func (t *Tracker) SetDiskMB(mb int64) {
	t.mu.Lock()
	t.snap.DiskMB = mb
	t.mu.Unlock()
}

// ── диск ───────────────────────────────────────────────────────────────────

// DirSizeMB рахує розмір теки (симлінки не розкриваються; не більше maxFiles файлів).
func DirSizeMB(root string, maxFiles int) int64 {
	var total int64
	count := 0
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if !d.IsDir() {
			if count++; maxFiles > 0 && count > maxFiles {
				return filepath.SkipAll
			}
			if info, e := d.Info(); e == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total / (1024 * 1024)
}

// ── збереження останньої сесії ─────────────────────────────────────────────

var safeID = regexp.MustCompile(`^[\w.\-]{1,120}$`)

func snapPath(stateDir, packID string) string {
	if !safeID.MatchString(packID) || packID == "." || packID == ".." {
		return ""
	}
	return filepath.Join(stateDir, "metrics", packID+".json")
}

// SaveSnapshot зберігає знімок як «останню сесію» збірки.
func SaveSnapshot(stateDir string, s Snapshot) {
	p := snapPath(stateDir, s.PackID)
	if p == "" || os.MkdirAll(filepath.Dir(p), 0o755) != nil {
		return
	}
	s.Running = false
	data, err := json.Marshal(s)
	if err == nil {
		_ = os.WriteFile(p, data, 0o644)
	}
}

// LoadSnapshot читає останню збережену сесію (ok=false — немає).
func LoadSnapshot(stateDir, packID string) (Snapshot, bool) {
	p := snapPath(stateDir, packID)
	if p == "" {
		return Snapshot{}, false
	}
	data, err := os.ReadFile(p)
	if err != nil || len(data) > 4<<20 {
		return Snapshot{}, false
	}
	var s Snapshot
	if json.Unmarshal(data, &s) != nil {
		return Snapshot{}, false
	}
	s.Running = false
	return s, true
}
