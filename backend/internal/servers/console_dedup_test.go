package servers

import "testing"

// Регресія: notifyLine додавав рядок у буфер ПОВТОРНО — readOutput/
// NotifyInstallLine уже робили buf.AppendLevel(line), а notifyLine ще раз
// викликав AppendRaw(line). Через це знімок консолі показував КОЖЕН рядок
// двічі (саме звідси скарга «подвійне [SYSTEM] Сервер: stopped (PID: ...)»).
//
// Тест фіксує, що один виклик NotifyInstallLine кладе у буфер рівно один
// рядок.
func TestNotifyInstallLineStoredOnce(t *testing.T) {
	m := NewManager()
	m.NotifyInstallLine("srv-1", "[SYSTEM] Сервер: stopped (PID: 38020)")

	snap := m.GetServerConsoleSnapshot("srv-1")
	if len(snap) != 1 {
		t.Fatalf("очікували 1 рядок у буфері консолі, отримали %d: %+v", len(snap), snap)
	}
	if snap[0].Text != "[SYSTEM] Сервер: stopped (PID: 38020)" {
		t.Fatalf("текст рядка зіпсовано: %q", snap[0].Text)
	}
}
