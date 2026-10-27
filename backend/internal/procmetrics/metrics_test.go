package procmetrics

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTreeTotals(t *testing.T) {
	list := []procInfo{
		{pid: 1, ppid: 0, cpuNs: 5, rss: 1}, // чужий
		{pid: 10, ppid: 1, cpuNs: 100, rss: 1 << 20},
		{pid: 11, ppid: 10, cpuNs: 50, rss: 2 << 20},
		{pid: 12, ppid: 11, cpuNs: 25, rss: 1 << 20},
		{pid: 13, ppid: 99, cpuNs: 7, rss: 7},
	}
	cpu, rss, n := treeTotals(list, 10)
	if cpu != 175 || rss != 4<<20 || n != 3 {
		t.Fatalf("%d %d %d", cpu, rss, n)
	}
	if _, _, n := treeTotals(list, 555); n != 0 {
		t.Fatal("missing root")
	}
}

func TestTrackerCPUAndPeaks(t *testing.T) {
	now := time.Unix(1000, 0)
	cpu := uint64(0)
	rss := uint64(100 << 20)
	tr := New("p", 10)
	tr.cores = 4
	tr.now = func() time.Time { return now }
	tr.list = func() []procInfo { return []procInfo{{pid: 10, cpuNs: cpu, rss: rss}} }
	tr.Sample()
	now = now.Add(2 * time.Second)
	cpu += 4_000_000_000 // 2 ядра з 4 за 2 с = 50%
	rss = 300 << 20
	tr.Sample()
	now = now.Add(2 * time.Second)
	cpu += 400_000_000 // 5%
	rss = 200 << 20
	tr.Sample()
	s := tr.Snapshot()
	if s.PeakCPU != 50 {
		t.Fatalf("peak cpu %v", s.PeakCPU)
	}
	if s.PeakRAMMB != 300 || s.CurRAMMB != 200 || s.CurCPU != 5 {
		t.Fatalf("%+v", s)
	}
	if fin := tr.Finish(); fin.Running || fin.EndedAt == 0 {
		t.Fatal("finish")
	}
	tr.list = func() []procInfo { return nil }
	if tr.Sample() {
		t.Fatal("gone process must stop")
	}
}

func TestDecimation(t *testing.T) {
	tr := New("p", 1)
	for i := 0; i < MaxSamples*3; i++ {
		tr.mu.Lock()
		tr.record(Sample{T: int64(i), RAMMB: float64(i)})
		tr.mu.Unlock()
	}
	s := tr.Snapshot()
	if len(s.Samples) > MaxSamples || s.Samples[0].T != 0 || s.PeakRAMMB != float64(MaxSamples*3-1) {
		t.Fatalf("len=%d", len(s.Samples))
	}
}

func TestPersistAndDisk(t *testing.T) {
	dir := t.TempDir()
	SaveSnapshot(dir, Snapshot{PackID: "pack1", PeakRAMMB: 1234, Running: true})
	got, ok := LoadSnapshot(dir, "pack1")
	if !ok || got.PeakRAMMB != 1234 || got.Running {
		t.Fatalf("%+v", got)
	}
	SaveSnapshot(dir, Snapshot{PackID: "../x"})
	if _, ok := LoadSnapshot(dir, "../x"); ok {
		t.Fatal("traversal")
	}
	os.WriteFile(filepath.Join(dir, "big"), make([]byte, 3<<20), 0o644)
	if mb := DirSizeMB(dir, 0); mb < 3 {
		t.Fatalf("size=%d", mb)
	}
}

func TestListProcsSelf(t *testing.T) {
	list := listProcs()
	if len(list) == 0 {
		t.Skip("no process list on this platform")
	}
	_, rss, n := treeTotals(list, os.Getpid())
	if n < 1 || rss == 0 {
		t.Fatalf("self not found n=%d rss=%d", n, rss)
	}
}
