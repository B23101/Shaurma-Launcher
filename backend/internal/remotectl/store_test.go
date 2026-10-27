package remotectl

import (
	"testing"
	"time"
)

const (
	me = "aaaaaaaa-0000-0000-0000-000000000001"
	f1 = "bbbbbbbb-0000-0000-0000-000000000002"
	f2 = "cccccccc-0000-0000-0000-000000000003"
)

var t0 = time.Unix(1_800_000_000, 0)

func TestDefaultDenyAndGrantFlow(t *testing.T) {
	s := Open(t.TempDir())
	if d := s.Authorize(me, f1, "s1", OpStart, true, 0, t0); d.Allowed || d.Code != CodeNotAllowed {
		t.Fatalf("за замовчуванням має бути відмова: %+v", d)
	}
	if err := s.SetGrant(me, "s1", f1, true, false, t0.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if d := s.Authorize(me, f1, "s1", OpStart, true, 0, t0); !d.Allowed {
		t.Fatalf("start мав бути дозволений: %+v", d)
	}
	if d := s.Authorize(me, f1, "s1", OpStop, true, 0, t0.Add(10*time.Second)); d.Allowed || d.Code != CodeNotAllowed {
		t.Fatalf("stop без права: %+v", d)
	}
	if d := s.Authorize(me, f2, "s1", OpStart, true, 0, t0.Add(20*time.Second)); d.Allowed {
		t.Fatal("інший друг без права")
	}
	if d := s.Authorize(me, f1, "s2", OpStart, true, 0, t0.Add(30*time.Second)); d.Allowed {
		t.Fatal("інший сервер без права")
	}
	if d := s.Authorize(me, f1, "s1", "restart", true, 0, t0.Add(40*time.Second)); d.Allowed {
		t.Fatal("невідома дія")
	}
	if d := s.Authorize("other-owner-id", f1, "s1", OpStart, true, 0, t0.Add(50*time.Second)); d.Allowed {
		t.Fatal("права іншого акаунта-власника не діють")
	}
}

func TestNotFriendAndPaused(t *testing.T) {
	s := Open(t.TempDir())
	_ = s.SetGrant(me, "s1", f1, true, true, t0.UnixMilli())
	if d := s.Authorize(me, f1, "s1", OpStart, false, 0, t0); d.Allowed || d.Code != CodeNotFriend {
		t.Fatalf("не друг: %+v", d)
	}
	_ = s.SetPaused(me, true)
	if d := s.Authorize(me, f1, "s1", OpStart, true, 0, t0); d.Allowed || d.Code != CodeDisabled {
		t.Fatalf("призупинено: %+v", d)
	}
	if len(s.Shared(me)) != 0 {
		t.Fatal("при паузі нічого не публікується")
	}
	_ = s.SetPaused(me, false)
	if len(s.Shared(me)) != 1 {
		t.Fatal("після зняття паузи сервер знову публікується")
	}
}

func TestStaleGrantAfterReFriending(t *testing.T) {
	s := Open(t.TempDir())
	_ = s.SetGrant(me, "s1", f1, true, true, t0.UnixMilli())
	// Дружбу створено ПІСЛЯ видачі права (колишнього друга додали знову) — право недійсне.
	since := t0.Add(48 * time.Hour).UnixMilli()
	if d := s.Authorize(me, f1, "s1", OpStart, true, since, t0.Add(49*time.Hour)); d.Allowed {
		t.Fatal("право, видане до повторної дружби, не має воскресати")
	}
	if len(s.View(me).Servers) != 0 {
		t.Fatal("застаріле право має бути видалене")
	}
	// Невеликий розсинхрон годинників не ламає свіжі права.
	_ = s.SetGrant(me, "s1", f2, true, false, t0.UnixMilli())
	if d := s.Authorize(me, f2, "s1", OpStart, true, t0.UnixMilli()+60_000, t0.Add(time.Hour)); !d.Allowed {
		t.Fatalf("допуск розсинхрону має діяти: %+v", d)
	}
}

func TestRateLimits(t *testing.T) {
	s := Open(t.TempDir())
	_ = s.SetGrant(me, "s1", f1, true, true, t0.UnixMilli())
	if d := s.Authorize(me, f1, "s1", OpStart, true, 0, t0); !d.Allowed {
		t.Fatal(d)
	}
	if d := s.Authorize(me, f1, "s1", OpStop, true, 0, t0.Add(time.Second)); d.Allowed || d.Code != CodeRateLimited {
		t.Fatalf("надто швидко на тому ж сервері: %+v", d)
	}
	allowed := 1
	for i := 1; i < 15; i++ { // усі в межах однієї хвилини
		d := s.Authorize(me, f1, "s1", OpStart, true, 0, t0.Add(time.Duration(i)*4*time.Second))
		if d.Allowed {
			allowed++
		}
	}
	if allowed != maxPerFriendPerMin {
		t.Fatalf("за хвилину дозволено %d, очікував %d", allowed, maxPerFriendPerMin)
	}
}

func TestRemoveFriendServerAndLimitsAndPersistence(t *testing.T) {
	dir := t.TempDir()
	s := Open(dir)
	_ = s.SetGrant(me, "s1", f1, true, true, 1)
	_ = s.SetGrant(me, "s2", f1, true, false, 1)
	_ = s.SetGrant(me, "s2", f2, true, false, 1)
	_ = s.SetAllowStopWithPlayers(me, "s2", true)

	s2 := Open(dir) // перечитуємо з диска
	if v := s2.View(me); len(v.Servers) != 2 {
		t.Fatalf("не збереглось: %+v", v)
	}
	n, _ := s2.RemoveFriend(me, f1)
	if n != 2 {
		t.Fatalf("мало прибрати 2 права, прибрано %d", n)
	}
	v := s2.View(me)
	if len(v.Servers) != 1 || v.Servers[0].ServerID != "s2" || len(v.Servers[0].Grants) != 1 || !v.Servers[0].AllowStopWithPlayers {
		t.Fatalf("після RemoveFriend: %+v", v)
	}
	_ = s2.RemoveServer(me, "s2")
	if len(s2.View(me).Servers) != 0 {
		t.Fatal("RemoveServer")
	}
	for i := 0; i < MaxServers; i++ {
		if err := s2.SetGrant(me, string(rune('a'+i)), f1, true, false, 1); err != nil {
			t.Fatal(err)
		}
	}
	if err := s2.SetGrant(me, "overflow", f1, true, false, 1); err != ErrTooMany {
		t.Fatalf("ліміт серверів: %v", err)
	}
	if err := s2.SetGrant(me, "a", me, true, false, 1); err != ErrBadInput {
		t.Fatal("не можна видати право самому собі")
	}
	// Зміна прав не оновлює Added (інакше можна було б «оновлювати» застарілий дозвіл).
	_ = s2.SetGrant(me, "a", f1, true, true, 999)
	if g := s2.View(me).Servers[0].Grants[0]; g.Added != 1 || !g.Stop {
		t.Fatalf("Added має зберігатись: %+v", g)
	}
}

func TestAuditNewestFirstFilterAndCap(t *testing.T) {
	s := Open(t.TempDir())
	for i := 0; i < MaxAudit+20; i++ {
		srv := "s1"
		if i%2 == 1 {
			srv = "s2"
		}
		s.AddAudit(me, AuditEntry{TS: int64(i), Friend: f1, Server: srv, Op: OpStart, Result: "ok"})
	}
	all := s.Audit(me, "", 5)
	if len(all) != 5 || all[0].TS != int64(MaxAudit+19) {
		t.Fatalf("новіші мають бути першими: %+v", all)
	}
	for _, e := range s.Audit(me, "s1", MaxAudit) {
		if e.Server != "s1" {
			t.Fatal("фільтр за сервером")
		}
	}
	if n := len(s.Audit(me, "", MaxAudit)); n != MaxAudit {
		t.Fatalf("журнал має обрізатись до %d, а є %d", MaxAudit, n)
	}
}
