package p2p

import "testing"

// Ці тести фіксують контракт вимикача P2P (Налаштування → Основне →
// «Вимкнути «LAN через інтернет» (P2P)»), який раніше працював наполовину:
//
//  1. Старт лаунчера піднімав слухач НАВІТЬ при вимкненій фічі (виклик ішов
//     повз обгортку App, яка читає налаштування);
//  2. після Stop сервіс уже неможливо було підняти назад — стан тримався
//     на sync.Once, який «згорає» назавжди.
//
// Тут перевіряємо саме другу частину (стан і ідемпотентність), НЕ запускаючи
// мережу: підняття гостьового сервісу створює зовнішній listener на
// guestExternalAddr (127.0.0.2:25565), тож у юніт-тесті це було б флакі і
// залежало б від мережі.

func newTestAPI(t *testing.T) *API {
	t.Helper()
	return NewAPI(t.TempDir(), func() string { return "token-1234567890abcdef" }, func() string { return "https://example.invalid" })
}

func TestStopGuestServiceIsIdempotent(t *testing.T) {
	api := newTestAPI(t)
	// Зупинка того, що не запускалось — не помилка: шлях виконується при
	// вимкненні фічі, коли слухача могло й не бути.
	if err := api.StopP2PGuestService(); err != nil {
		t.Fatalf("StopP2PGuestService на незапущеному сервісі: %v", err)
	}
	if err := api.SetGuestEnabled(false); err != nil {
		t.Fatalf("SetGuestEnabled(false) на незапущеному сервісі: %v", err)
	}
	api.mu.Lock()
	started := api.guestStarted
	guest := api.guest
	api.mu.Unlock()
	if started || guest != nil {
		t.Error("після вимкнення стан має бути скинутий: інакше фічу не можна буде увімкнути назад")
	}
}

// TestStopGuestServiceAllowsRestart — головна регресія: після Stop прапорець
// має повернутись у «не запущено», щоб наступний Ensure реально створив
// сервіс заново (раніше sync.Once робив це неможливим).
func TestStopGuestServiceAllowsRestart(t *testing.T) {
	api := newTestAPI(t)
	api.mu.Lock()
	api.guestStarted = true // імітуємо «колись запущений» сервіс
	api.mu.Unlock()

	if err := api.StopP2PGuestService(); err != nil {
		t.Fatalf("StopP2PGuestService: %v", err)
	}
	api.mu.Lock()
	started := api.guestStarted
	api.mu.Unlock()
	if started {
		t.Error("після Stop прапорець guestStarted має бути false — інакше SetGuestEnabled(true) ніколи не підніме слухач знову")
	}
}
