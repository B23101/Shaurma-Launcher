package remotectl

import (
	"testing"
	"time"
)

type fakeNets map[string][]string // проксі → сервери мережі

func (f fakeNets) Members(proxy string) []string { return f[proxy] }
func (f fakeNets) Parents(id string) []string {
	var out []string
	for p, ms := range f {
		for _, m := range ms {
			if m == id {
				out = append(out, p)
			}
		}
	}
	return out
}

// Право «Сервери мережі» на проксі: друг запускає й зупиняє кожен сервер
// мережі (з правами проксі), але консоль серверів мережі не успадковується,
// а без цього права сервери мережі закриті.
func TestNetworkGrantInheritsStartStop(t *testing.T) {
	s := Open(t.TempDir())
	s.SetNetworks(fakeNets{"proxy": {"hub", "game"}})
	if err := s.SetGrantFull(me, "proxy", f1, Rights{Start: true, Console: true}, t0.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if d := s.Authorize(me, f1, "hub", OpStart, true, 0, t0); d.Allowed {
		t.Fatalf("без права «Сервери мережі» сервер мережі має бути закритим: %+v", d)
	}
	if err := s.SetGrantFull(me, "proxy", f1, Rights{Start: true, Console: true, Network: true}, t0.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if d := s.Authorize(me, f1, "hub", OpStart, true, 0, t0.Add(10*time.Second)); !d.Allowed {
		t.Fatalf("запуск сервера мережі мав бути дозволений: %+v", d)
	}
	if d := s.Authorize(me, f1, "game", OpStop, true, 0, t0.Add(20*time.Second)); d.Allowed {
		t.Fatalf("зупинки немає в праві на проксі: %+v", d)
	}
	if d := s.Authorize(me, f1, "hub", OpConsoleOpen, true, 0, t0.Add(30*time.Second)); d.Allowed {
		t.Fatalf("консоль сервера мережі не успадковується: %+v", d)
	}
	if d := s.Authorize(me, f2, "hub", OpStart, true, 0, t0.Add(40*time.Second)); d.Allowed {
		t.Fatalf("інший друг без права: %+v", d)
	}

	shared := map[string]bool{}
	for _, sv := range s.Shared(me) {
		for _, g := range sv.Grants {
			if g.AccountID == f1 && g.Start {
				shared[sv.ServerID] = true
			}
		}
	}
	if !shared["proxy"] || !shared["hub"] || !shared["game"] {
		t.Errorf("на VPS мають піти проксі й сервери мережі: %v", shared)
	}
}
