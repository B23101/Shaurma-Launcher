package main

import (
	"reflect"
	"testing"
	"time"
)

func TestRestartArgs(t *testing.T) {
	cases := []struct {
		name   string
		in     []string
		hidden bool
		want   []string
	}{
		{"вікно було видиме", []string{"--autostart"}, false, []string{"--autostart"}},
		{"вікно було видиме, старий --background знімається", []string{"--autostart", "--background"}, false, []string{"--autostart"}},
		{"вікно було сховане", []string{"--autostart"}, true, []string{"--autostart", "--background"}},
		{"сховане, --background не дублюється", []string{"--background"}, true, []string{"--background"}},
		{"без аргументів", nil, false, []string{}},
	}
	for _, c := range cases {
		got := restartArgs(c.in, c.hidden)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// TestSupStateNeverGivesUp — головна вимога до нагляду: він НІКОЛИ не
// «здається» (раніше після 5 збоїв за 10 хв робив os.Exit(1), і лаунчер
// переставав запускатись). Пауза між спробами зростає, але спроби не
// закінчуються.
func TestSupStateNeverGivesUp(t *testing.T) {
	st := newSupState()
	for i := 0; i < 200; i++ {
		delay, _ := st.afterCrash(5 * time.Second)
		if delay <= 0 {
			t.Fatalf("спроба %d: пауза %s — перезапуск без очікування", i, delay)
		}
		if delay > supBackoffMax {
			t.Fatalf("спроба %d: пауза %s перевищує межу %s", i, delay, supBackoffMax)
		}
	}
}

// TestSupStateBackoffGrows — пауза подвоюється до supBackoffMax і не далі.
//
// Друга пауза теж 2с, а не 4с: саме на ній вмикається безпечний режим
// (TestSupStateSafeModeAfterTwoFastCrashes), і він стартує з мінімальної
// паузи — щоб відновлений UI з'явився швидко, а не через півхвилини.
func TestSupStateBackoffGrows(t *testing.T) {
	st := newSupState()
	want := []time.Duration{2, 2, 4, 8, 16, 30, 30}
	for i, w := range want {
		delay, _ := st.afterCrash(5 * time.Second)
		if delay != w*time.Second {
			t.Fatalf("пауза #%d = %s, очікувалась %s", i+1, delay, w*time.Second)
		}
	}
}

// TestSupStateSafeModeAfterTwoFastCrashes — два збої підряд (кожен швидше за
// supHealthyRun) вмикають безпечний режим, і він тримається до здорової сесії.
func TestSupStateSafeModeAfterTwoFastCrashes(t *testing.T) {
	st := newSupState()
	if _, safe := st.afterCrash(3 * time.Second); safe {
		t.Fatal("безпечний режим увімкнувся після ПЕРШОГО збою")
	}
	if st.safeMode {
		t.Fatal("safeMode виставлено після першого збою")
	}
	delay, safe := st.afterCrash(3 * time.Second)
	if !safe || !st.safeMode {
		t.Fatalf("після другого збою підряд очікувався безпечний режим (safe=%v, st=%v)", safe, st.safeMode)
	}
	// Безпечний режим стартуємо без довгої паузи — щоб UI з'явився швидко.
	if delay != supBackoffMin {
		t.Fatalf("пауза перед безпечним режимом %s, очікувалась %s", delay, supBackoffMin)
	}
	if _, safe := st.afterCrash(3 * time.Second); safe {
		t.Fatal("безпечний режим «увімкнувся» вдруге — має лишатись увімкненим без повторної події")
	}
	if !st.safeMode {
		t.Fatal("безпечний режим вимкнувся сам без здорової сесії")
	}
}

// TestSupStateHealthyRunResets — сесія, що прожила supHealthyRun, скидає
// лічильник, паузу і безпечний режим: випадковий збій раз на годину не має
// наближати безпечний режим.
func TestSupStateHealthyRunResets(t *testing.T) {
	st := newSupState()
	for i := 0; i < 6; i++ {
		st.afterCrash(time.Second) // швидкі збої: пауза росте, safeMode увімкнено
	}
	if !st.safeMode {
		t.Fatal("передумова тесту: очікувався безпечний режим")
	}
	delay, _ := st.afterCrash(supHealthyRun + time.Second)
	if delay != supBackoffMin {
		t.Fatalf("після здорової сесії пауза %s, очікувалась %s", delay, supBackoffMin)
	}
	if st.safeMode {
		t.Fatal("після здорової сесії безпечний режим має вимкнутись")
	}
	if st.consecutive != 0 {
		t.Fatalf("після здорової сесії лічильник збоїв = %d, очікувався 0", st.consecutive)
	}
}
