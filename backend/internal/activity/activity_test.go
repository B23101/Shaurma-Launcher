package activity

import (
	"testing"
	"time"
)

func newAt(t *time.Time) *Registry {
	r := New()
	r.now = func() time.Time { return *t }
	return r
}

func TestUpdateKeepsKnownFields(t *testing.T) {
	now := time.Unix(1000, 0)
	r := newAt(&now)
	if !r.Update(Activity{ID: "a", Kind: "share-in", Title: "Pack", Percent: 10}) {
		t.Fatal("new activity must report a change")
	}
	if r.Update(Activity{ID: "a", Percent: 10}) {
		t.Fatal("same progress must not report a change")
	}
	r.Update(Activity{ID: "a", Percent: 250})
	list, _ := r.Snapshot()
	if len(list) != 1 || list[0].Kind != "share-in" || list[0].Title != "Pack" || list[0].Percent != 100 || list[0].State != StateRunning {
		t.Fatalf("unexpected snapshot: %+v", list)
	}
}

func TestFinishLingerAndStale(t *testing.T) {
	now := time.Unix(1000, 0)
	r := newAt(&now)
	r.Update(Activity{ID: "done"})
	r.Update(Activity{ID: "stuck"})
	if !r.Finish("done", "") || r.Finish("done", "") || r.Finish("missing", "") {
		t.Fatal("finish must apply once and only to known ids")
	}
	now = now.Add(time.Second)
	list, next := r.Snapshot()
	if len(list) != 2 || next != Linger-time.Second {
		t.Fatalf("got %d items, next %v", len(list), next)
	}
	now = now.Add(Linger)
	if list, _ = r.Snapshot(); len(list) != 1 || list[0].ID != "stuck" {
		t.Fatalf("finished activity must disappear after linger: %+v", list)
	}
	now = now.Add(Stale)
	if list, _ = r.Snapshot(); len(list) != 0 {
		t.Fatalf("stale activity must disappear: %+v", list)
	}
}

func TestFinishWithError(t *testing.T) {
	now := time.Unix(1000, 0)
	r := newAt(&now)
	r.Update(Activity{ID: "x", Percent: 40})
	r.Finish("x", "boom")
	list, _ := r.Snapshot()
	if list[0].State != StateError || list[0].Detail != "boom" || list[0].Percent != -1 {
		t.Fatalf("unexpected: %+v", list[0])
	}
}

func TestAggregate(t *testing.T) {
	var g Aggregate
	g.Add(StateRunning, -1)
	g.Add(StateRunning, 30)
	g.Add(StatePaused, 70)
	g.Add(StateWaiting, 90)
	if !g.Running || g.RunningPercent != 30 || !g.Paused || g.PausedPercent != 70 || g.Error {
		t.Fatalf("unexpected aggregate: %+v", g)
	}
	var only Aggregate
	only.Add(StateRunning, -1)
	if only.RunningPercent != -1 {
		t.Fatalf("indeterminate must stay -1: %+v", only)
	}
}
