package remotectl

import (
	"testing"
	"time"
)

func TestConsoleRights(t *testing.T) {
	s := Open(t.TempDir())
	now := time.Now()
	const o, f, srv = "owner1", "friend1", "srv1"
	if err := s.SetGrantFull(o, srv, f, Rights{Console: true}, now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if d := s.Authorize(o, f, srv, OpConsoleOpen, true, 0, now); !d.Allowed {
		t.Fatalf("перегляд має бути дозволено: %v", d)
	}
	if d := s.Authorize(o, f, srv, OpConsoleExec, true, 0, now); d.Allowed || d.Code != CodeNotAllowed {
		t.Fatalf("exec без права: %v", d)
	}
	if d := s.Authorize(o, f, srv, OpStart, true, 0, now); d.Allowed {
		t.Fatal("консоль не дає права запуску")
	}
	// Exec без Console скидається.
	_ = s.SetGrantFull(o, srv, f, Rights{Exec: true, Start: true}, now.UnixMilli())
	if d := s.Authorize(o, f, srv, OpConsoleExec, true, 0, now); d.Allowed {
		t.Fatal("Exec без Console має скидатись")
	}
	_ = s.SetGrantFull(o, srv, f, Rights{Console: true, Exec: true}, now.UnixMilli())
	if d := s.Authorize(o, f, srv, OpConsoleExec, true, 0, now); !d.Allowed {
		t.Fatalf("exec з правом: %v", d)
	}
	if d := s.Authorize(o, f, srv, OpConsoleExec, false, 0, now); d.Code != CodeNotFriend {
		t.Fatalf("не друг: %v", d)
	}
	s.SetPaused(o, true)
	if d := s.Authorize(o, f, srv, OpConsoleOpen, true, 0, now); d.Code != CodeDisabled {
		t.Fatalf("пауза: %v", d)
	}
}

func TestExecRateLimit(t *testing.T) {
	s := Open(t.TempDir())
	now := time.Now()
	_ = s.SetGrantFull("o", "s", "f", Rights{Console: true, Exec: true}, now.UnixMilli())
	ok := 0
	for i := 0; i < 200; i++ {
		if s.Authorize("o", "f", "s", OpConsoleExec, true, 0, now).Allowed {
			ok++
		}
	}
	if ok != maxExecPerFriendPerMin {
		t.Fatalf("дозволено %d, ліміт %d", ok, maxExecPerFriendPerMin)
	}
}

func TestCheckCommand(t *testing.T) {
	bad := []string{"", "   ", "say hi\nstop", "say\rhi", "say \x00", string(make([]byte, 300))}
	for _, b := range bad {
		if _, code := CheckCommand(b, true); code != CodeBadCommand {
			t.Fatalf("%q має відхилятись", b)
		}
	}
	for _, c := range []string{"op Steve", "/OP steve", "minecraft:op x", "deop a", "whitelist add b", "save-off"} {
		if _, code := CheckCommand(c, false); code != CodePrivileged {
			t.Fatalf("%q має бути привілейованою", c)
		}
		if _, code := CheckCommand(c, true); code != "" {
			t.Fatalf("%q дозволено при AllowPrivileged: %s", c, code)
		}
	}
	for _, c := range []string{"stop", "/STOP", "minecraft:stop", "restart"} {
		cmd, code := CheckCommand(c, false)
		if code != "" || !cmd.IsStop {
			t.Fatalf("%q має класифікуватись як stop: %+v %s", c, cmd, code)
		}
	}
	cmd, code := CheckCommand("  /say привіт  ", false)
	if code != "" || cmd.Text != "say привіт" || cmd.Verb != "say" || cmd.IsStop {
		t.Fatalf("%+v %s", cmd, code)
	}
}
