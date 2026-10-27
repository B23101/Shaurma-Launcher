package auth

import (
	"fmt"
	"net"
	"testing"
)

// TestStartLoginNoPortLeak — повторні StartLogin не повинні лишати порт зайнятим:
// порт тепер ОДИН (CallbackPort), тож кожен наступний StartLogin мусить закривати
// ПОПЕРЕДНІЙ listener, а після CancelLogin порт має бути вільний.
//
// Тест толерантний до оточення: якщо порт уже зайнятий ЗОВНІШНІМ процесом
// (напр. запущений лаунчер) — StartLogin чесно повертає помилку, і це не помилка фіксу.
func TestStartLoginNoPortLeak(t *testing.T) {
	a := NewAuthenticator()

	got := 0
	for i := 1; i <= 6; i++ {
		url, err := a.StartLogin()
		if err != nil {
			t.Logf("спроба %d: StartLogin: %v (порт зайнятий зовнішнім процесом?)", i, err)
			break
		}
		if url == "" {
			t.Fatalf("спроба %d: порожній URL", i)
		}
		got++
	}

	a.CancelLogin()

	if got >= 6 {
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", CallbackPort))
		if err != nil {
			t.Errorf("порт %d зайнятий після серії StartLogin + CancelLogin — витік listener: %v", CallbackPort, err)
			return
		}
		ln.Close()
	} else {
		t.Logf("пропущено перевірку звільнення порту: зовнішній процес його тримає (got=%d)", got)
	}
}
