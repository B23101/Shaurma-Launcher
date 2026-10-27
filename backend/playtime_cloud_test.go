package main

import (
	"testing"
	"time"

	"shaurma-launcher-backend/internal/store"
)

func TestSnapshot(t *testing.T) {
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.Local)
	ss := []store.PlaytimeSession{
		{StartedAt: now.AddDate(0, 0, -1), Seconds: 60},
		{StartedAt: now.AddDate(0, 0, -1).Add(time.Hour), Seconds: 40},
		{StartedAt: now.AddDate(0, 0, -30), Seconds: 500},
		{StartedAt: now.AddDate(0, 0, -200), Seconds: 900, Sessions: 9},
	}
	r := buildPlaytimeSnapshot(ss, playtimeCloudState{}, now)
	if len(r.Days) != 1 || r.Days[0].S != 100 || r.Days[0].N != 2 {
		t.Fatalf("days %+v", r.Days)
	}
	var ms, mn int64
	for _, m := range r.Months {
		ms += m.S
		mn += int64(m.N)
	}
	if ms != 1400 || mn != 10 {
		t.Fatalf("months %+v", r.Months)
	}
	st := playtimeCloudState{Backfilled: true}
	r2 := buildPlaytimeSnapshot(ss, st, now)
	if len(r2.Months) != 0 {
		t.Fatal("backfill repeated")
	}
	if playtimeWindowDays(playtimeCloudState{LastSyncDay: "2020-01-01"}, now) != playtimeMaxWindowDays {
		t.Fatal("window cap")
	}
	if playtimeShouldSend(playtimeSyncReq{}, st, now) {
		t.Fatal("empty sent")
	}
}
