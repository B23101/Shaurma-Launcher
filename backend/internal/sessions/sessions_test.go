package sessions

import (
	"sync/atomic"
	"testing"
)

// Надтонкий лаунчер-стаб: дозволяє вимкнути/увімкнути стан running без
// реального процесу, щоб тестувати тільки логіку реєстру/спостережувачів.
type fakeLauncher struct {
	running atomic.Bool
}

func (f *fakeLauncher) IsRunning() bool { return f.running.Load() }
func (f *fakeLauncher) Stop() error     { return nil }

func TestManagerRunningStateNotifications(t *testing.T) {
	m := NewManager()
	var got []bool
	m.OnRunningStateChange(func(running bool) {
		got = append(got, running)
	})

	// До будь-якої сесії — без жодного виклику.
	if len(got) != 0 {
		t.Fatalf("очікували 0 викликів до сесій, маємо %d: %v", len(got), got)
	}

	l1 := &fakeLauncher{}
	l2 := &fakeLauncher{}
	m.Start(&Session{AccountID: "a1", BuildID: "b1", Launcher: l1})
	m.Start(&Session{AccountID: "a2", BuildID: "b2", Launcher: l2})

	// На момент Start процеси ще не запущені (як у реальному потоці: Start
	// до Launch) — жодного переходу не має бути.
	if len(got) != 0 {
		t.Fatalf("Start до Launch не має генерувати подій, маємо %d", len(got))
	}

	// Перша гра запустилась → false→true.
	l1.running.Store(true)
	m.Recheck()
	if len(got) != 1 || got[0] != true {
		t.Fatalf("очікували [true], маємо %v", got)
	}

	// Друга гра теж запущена — стан не змінився, повторних викликів немає.
	l2.running.Store(true)
	m.Recheck()
	if len(got) != 1 {
		t.Fatalf("повторний true не має генерувати подію, маємо %d", len(got))
	}

	// Перша завершилась, друга ще йде → жодного переходу (true→true).
	l1.running.Store(false)
	m.Remove("a1", "b1")
	if len(got) != 1 {
		t.Fatalf("true→true не має генерувати подію, маємо %d", len(got))
	}

	// Остання завершилась → true→false.
	l2.running.Store(false)
	m.Remove("a2", "b2")
	if len(got) != 2 || got[1] != false {
		t.Fatalf("очікували [true, false], маємо %v", got)
	}
}
