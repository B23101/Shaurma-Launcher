package servers

import (
	"runtime"
	"strings"
	"testing"
)

// Рядки netstat -ano, зняті з реального Windows: перший — звичайний слухач,
// другий — ВИХІДНЕ з'єднання на локальному порту 65017 (саме воно валило
// сервер з «Address already in use: bind», хоча LISTENING-рядка немає).
const netstatSample = `Активні з'єднання

  Ім'я    Локальна адреса      Зовнішня адреса          Стан            PID
  TCP    0.0.0.0:25565        0.0.0.0:0                LISTENING       4711
  TCP    192.168.0.218:65017  184.35.10.149:443        ESTABLISHED     12568
  TCP    192.168.0.218:49999  140.82.1.1:443           TIME_WAIT       0
  UDP    0.0.0.0:65017        *:*                                      2222
`

func TestScanWindowsPortRows(t *testing.T) {
	lines := strings.Split(netstatSample, "\n")

	if busy, holder := scanWindowsPortRows(lines, 25565); !busy || holder != "PID 4711" {
		t.Fatalf("слухач 25565 не розпізнано: busy=%v holder=%q", busy, holder)
	}
	if busy, holder := scanWindowsPortRows(lines, 65017); !busy || !strings.Contains(holder, "12568") {
		t.Fatalf("вихідне з'єднання на 65017 не розпізнано: busy=%v holder=%q", busy, holder)
	}
	// TIME_WAIT — не привід блокувати старт (інакше лаунчер «вічно» бачив зайняте).
	if busy, holder := scanWindowsPortRows(lines, 49999); busy {
		t.Fatalf("TIME_WAIT хибно вважається зайнятістю: %q", holder)
	}
	if busy, _ := scanWindowsPortRows(lines, 25566); busy {
		t.Fatal("вільний 25566 вважається зайнятим")
	}
}

func TestScanFreePortSkipsEphemeral(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("ефемерний діапазон обмежуємо лише на Windows")
	}
	for i := 0; i < 10; i++ {
		if p := ScanFreePort(25600+i, nil, "", nil); inEphemeralPortRange(p) {
			t.Fatalf("автопідбір віддав ефемерний порт %d — його може зайняти будь-яке вихідне з'єднання", p)
		}
	}
	if p := ScanFreePort(65017, nil, "", nil); inEphemeralPortRange(p) {
		t.Fatalf("запит ефемерного 65017 дав ефемерний %d замість порту з безпечного діапазону", p)
	}
}
