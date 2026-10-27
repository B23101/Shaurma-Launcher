package p2p

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Ці тести фіксують поведінку, через яку користувач бачив голе
// «Error: p2p: lobby HTTP 500» замість причини: worker віддає 503 з кодом
// p2p_not_configured (немає Headscale), і лаунчер МУСИТЬ показати зрозуміле
// пояснення, а не HTTP-код.

func lobbyServer(t *testing.T, status int, body string) *LobbyClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return NewLobbyClient(srv.URL, "token-1234567890abcdef")
}

func TestClaimNotConfiguredGivesHumanMessage(t *testing.T) {
	c := lobbyServer(t, http.StatusServiceUnavailable,
		`{"error":"p2p_not_configured","errorMessage":"P2P не налаштований на сервері: немає Headscale"}`)

	_, err := c.ClaimAddress(context.Background(), "prlool", strings.Repeat("a", 32))
	if err == nil {
		t.Fatal("очікували помилку")
	}
	msg := err.Error()
	if !strings.Contains(msg, "не налаштований") {
		t.Errorf("у тексті немає пояснення про неналаштований сервер: %q", msg)
	}
	if !strings.Contains(msg, "Headscale") {
		t.Errorf("у тексті немає причини (Headscale): %q", msg)
	}
	if strings.Contains(msg, "HTTP 503") || strings.Contains(msg, "HTTP 500") {
		t.Errorf("користувачу не має показуватись голий HTTP-код: %q", msg)
	}
}

func TestClaimUpstreamErrorIncludesCause(t *testing.T) {
	c := lobbyServer(t, http.StatusBadGateway,
		`{"error":"p2p_headscale_error","errorMessage":"Headscale відмовив у видачі ключа (HTTP 401)"}`)

	_, err := c.ClaimAddress(context.Background(), "prlool", strings.Repeat("a", 32))
	if err == nil {
		t.Fatal("очікували помилку")
	}
	if !strings.Contains(err.Error(), "Headscale відмовив") {
		t.Errorf("причина з errorMessage не дійшла до користувача: %q", err.Error())
	}
}

func TestClaimRateLimitedMessage(t *testing.T) {
	// 429 з поясненням сервера: показуємо саме його (раніше будь-який 429 підміняли на «забагато спроб»).
	c := lobbyServer(t, http.StatusTooManyRequests, `{"error":"Забагато активних адрес на цей лаунчер (максимум 5)."}`)
	_, err := c.ClaimAddress(context.Background(), "prlool", strings.Repeat("a", 32))
	if err == nil {
		t.Fatal("очікували помилку")
	}
	if !strings.Contains(err.Error(), "максимум 5") {
		t.Errorf("429 має нести повідомлення сервера: %q", err.Error())
	}

	// 429 без тіла (rate-limit на join) — загальна порада зачекати.
	c = lobbyServer(t, http.StatusTooManyRequests, ``)
	_, err = c.ClaimAddress(context.Background(), "prlool", strings.Repeat("a", 32))
	if err == nil || !strings.Contains(err.Error(), "зачекайте") {
		t.Errorf("429 без пояснення має давати пораду зачекати: %v", err)
	}
}

// TestHostUpdateForbiddenKeepsServerMessage — чужий ownerSecret: worker
// віддає 403 з поясненням. Клієнт має донести його, а не замінити на код.
func TestHostUpdateForbiddenKeepsServerMessage(t *testing.T) {
	c := lobbyServer(t, http.StatusForbidden, `{"error":"Невірний ownerSecret — це не ваша адреса"}`)

	_, err := c.HostUpdate(context.Background(), HostUpdateRequest{
		Subdomain: "prlool-482917", OwnerSecret: strings.Repeat("b", 32),
		HostIP: "100.64.0.5", HostPort: "25565",
	})
	if err == nil {
		t.Fatal("очікували помилку")
	}
	if !strings.Contains(err.Error(), "не ваша адреса") {
		t.Errorf("текст 403 загубився: %q", err.Error())
	}
}

func TestFetchStatusParsesNotConfigured(t *testing.T) {
	c := lobbyServer(t, http.StatusOK,
		`{"configured":false,"roomDomain":"play.shaurma-launcher.org","reason":"На сервері не налаштований Headscale"}`)

	st, err := c.FetchStatus(context.Background())
	if err != nil {
		t.Fatalf("FetchStatus: %v", err)
	}
	if st.Configured {
		t.Error("configured має бути false")
	}
	if st.Reason == "" {
		t.Error("reason має бути заповнений, щоб UI показав пояснення")
	}
	if st.RoomDomain != "play.shaurma-launcher.org" {
		t.Errorf("roomDomain: отримано %q", st.RoomDomain)
	}
}

// TestGetStatusNetworkErrorNotDisabled — мережева помилка НЕ має виглядати
// як «фічу вимкнено»: інакше тимчасовий збій мережі ховав би робочий P2P.
func TestGetStatusNetworkErrorNotDisabled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	baseURL := srv.URL
	srv.Close() // сервер «упав» — запит гарантовано не дійде

	api := NewAPI(t.TempDir(), func() string { return "token-1234567890abcdef" }, func() string { return baseURL })
	st := api.GetStatus(false, true, "")
	if st.DisabledByUser {
		t.Error("DisabledByUser має бути false (користувач нічого не вимикав)")
	}
	if !st.Configured {
		t.Error("при мережевій помилці фічу НЕ можна вважати вимкненою")
	}
	if st.Reason == "" {
		t.Error("причина має бути вказана")
	}
}

// TestGetStatusDisabledByUser — вимикач у налаштуваннях: без жодного
// мережевого запиту повертаємо «вимкнено» і причину.
func TestGetStatusDisabledByUser(t *testing.T) {
	called := false
	api := NewAPI(t.TempDir(), func() string { return "t" }, func() string { called = true; return "https://example.invalid" })

	st := api.GetStatus(true, true, "")
	if !st.DisabledByUser {
		t.Error("DisabledByUser має бути true")
	}
	if st.Configured {
		t.Error("вимкнена фіча не може бути configured")
	}
	if called {
		t.Error("при вимкненій фічі не має бути жодних мережевих запитів")
	}
}
