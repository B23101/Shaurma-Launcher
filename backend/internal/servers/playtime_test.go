package servers

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPlaytimeTicksFormats(t *testing.T) {
	cases := map[string]int64{
		`{"stats":{"minecraft:custom":{"minecraft:play_time":1200}}}`:      1200,
		`{"stats":{"minecraft:custom":{"minecraft:play_one_minute":600}}}`: 600,
		`{"stat.playOneMinute":400,"stat.jump":3}`:                         400,
		`{"stats":{"minecraft:custom":{"minecraft:jump":5}}}`:              0,
		`not json`: 0,
	}
	for in, want := range cases {
		if got := playtimeTicks([]byte(in)); got != want {
			t.Errorf("%s: got %d want %d", in, got, want)
		}
	}
}

func TestReadPlayerPlaytimes(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "server.properties"), "level-name=mapa\n")
	write(t, filepath.Join(dir, "usercache.json"),
		`[{"name":"Steve","uuid":"11111111-1111-1111-1111-111111111111","expiresOn":"x"}]`)
	st := filepath.Join(dir, "mapa", "stats")
	write(t, filepath.Join(st, "11111111-1111-1111-1111-111111111111.json"),
		`{"stats":{"minecraft:custom":{"minecraft:play_time":72000}}}`) // 1 год
	write(t, filepath.Join(st, "22222222-2222-2222-2222-222222222222.json"),
		`{"stats":{"minecraft:custom":{"minecraft:play_time":24000}}}`) // 20 хв, нік невідомий
	write(t, filepath.Join(st, "33333333-3333-3333-3333-333333333333.json"),
		`{"stats":{}}`) // нуль → пропуск

	got := ReadPlayerPlaytimes(dir)
	if len(got) != 2 {
		t.Fatalf("want 2 players, got %+v", got)
	}
	if got[0].Name != "Steve" || got[0].Seconds != 3600 {
		t.Errorf("first: %+v", got[0])
	}
	if got[1].Name != "22222222…" || got[1].Seconds != 1200 {
		t.Errorf("second: %+v", got[1])
	}
}

func TestReadPlayerPlaytimesLevelNameEscape(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "server.properties"), "level-name=../../etc\n")
	if got := ReadPlayerPlaytimes(dir); len(got) != 0 {
		t.Fatalf("expected empty, got %+v", got)
	}
}

func TestMergeOnline(t *testing.T) {
	list := []PlayerPlaytime{{Name: "Steve", Seconds: 10}}
	got := mergeOnline(list, []PlayerPresence{
		{Name: "steve", JoinedAt: 5}, {Name: "Alex", JoinedAt: 7},
	})
	if len(got) != 2 || !got[0].Online || got[0].SessionStart != 5 || got[1].Name != "Alex" || !got[1].Online {
		t.Fatalf("%+v", got)
	}
}
