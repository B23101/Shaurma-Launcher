package social

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// stubResponse — мінімальна HTTP-відповідь для Requester у тестах.
func stubResponse(status int, body string) *http.Response {
	rec := httptest.NewRecorder()
	rec.WriteHeader(status)
	_, _ = rec.WriteString(body)
	return rec.Result()
}

func TestBudgetAllowUntilLimitThenReset(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	b := NewBudget(3, time.Hour)
	b.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		if !b.Allow() {
			t.Fatalf("запит %d мав пройти", i)
		}
	}
	if b.Allow() {
		t.Fatal("4-й запит мав упертись у ліміт")
	}
	if b.Remaining() != 0 {
		t.Fatalf("Remaining=%d, очікували 0", b.Remaining())
	}
	// Вікно минуло — лічильник скидається.
	now = now.Add(time.Hour)
	if !b.Allow() {
		t.Fatal("після нового вікна запит мав пройти")
	}
	if b.Remaining() != 2 {
		t.Fatalf("Remaining=%d, очікували 2", b.Remaining())
	}
}

func TestBudgetNilIsUnlimited(t *testing.T) {
	var b *Budget
	if !b.Allow() {
		t.Fatal("nil-бюджет не має обмежувати")
	}
}

func TestClientBudgetBlocksNetwork(t *testing.T) {
	calls := 0
	c := NewClient(func(method, path string, body []byte) (*http.Response, error) {
		calls++
		return stubResponse(200, `{"friends":[]}`), nil
	}).WithBudget(NewBudget(1, time.Hour))

	if _, err := c.ListFriends(); err != nil {
		t.Fatalf("перший запит мав пройти: %v", err)
	}
	_, err := c.ListFriends()
	if err != ErrBudgetExhausted {
		t.Fatalf("другий запит мав упертись у бюджет, отримали %v", err)
	}
	if calls != 1 {
		t.Fatalf("мережевих викликів %d, очікували 1", calls)
	}
	if got := errorCode(err); got != "budget_exhausted" {
		t.Fatalf("errorCode=%q", got)
	}
}
