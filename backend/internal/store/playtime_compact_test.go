package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCompact(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, InstanceFileName)
	now := time.Now()
	old1 := now.AddDate(0, 0, -200)
	old2 := old1.Add(time.Hour)
	recent := now.AddDate(0, 0, -5)
	in := map[string]any{"name": "x", "playtimeSessions": []PlaytimeSessionEntry{
		{StartedAt: old1, Seconds: 100, AccountName: "a"},
		{StartedAt: old2, Seconds: 50, AccountName: "a"},
		{StartedAt: recent, Seconds: 7, AccountName: "a"},
	}}
	b, _ := json.Marshal(in)
	os.WriteFile(p, b, 0644)
	cut := now.Add(-PlaytimeCompactAfter)
	n, err := compactInstancePlaytime(p, cut)
	if err != nil || n != 2 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	n, _ = compactInstancePlaytime(p, cut)
	if n != 0 {
		t.Fatalf("not idempotent: %d", n)
	}
	var out struct {
		Name string
		S    []PlaytimeSessionEntry `json:"playtimeSessions"`
		R    []PlaytimeRollupEntry  `json:"playtimeRollups"`
	}
	raw, _ := os.ReadFile(p)
	json.Unmarshal(raw, &out)
	if out.Name != "x" || len(out.S) != 1 || out.S[0].Seconds != 7 {
		t.Fatalf("bad sessions %+v", out)
	}
	var sec, ses int64
	for _, r := range out.R {
		sec += r.Seconds
		ses += r.Sessions
	}
	if sec != 150 || ses != 2 {
		t.Fatalf("rollup sum %d/%d %+v", sec, ses, out.R)
	}
}
