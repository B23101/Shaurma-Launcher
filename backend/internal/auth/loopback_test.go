package auth

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func get(t *testing.T, u string) (int, string) {
	t.Helper()
	resp, err := http.Get(u)
	if err != nil {
		t.Fatalf("GET %s: %v", u, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// Google і Microsoft ходять на ОДИН redirect_uri; callback віддає код, а сторінка
// опитує статус і отримує підсумок, який лаунчер виставив через Finish.
func TestLoopbackCallbackAndOutcome(t *testing.T) {
	s, err := Callback.Begin("st123", "google")
	if err != nil {
		t.Skipf("порт зайнятий зовнішнім процесом: %v", err)
	}
	defer s.Close()

	base := CallbackRedirectURI()
	if base != "http://localhost:29111/auth" {
		t.Fatalf("redirect_uri = %s", base)
	}
	root := strings.TrimSuffix(base, "/auth")

	// чужий state — ігнорується, вхід не завершується
	if code, _ := get(t, root+"/auth?state=bad&code=x"); code != http.StatusBadRequest {
		t.Fatalf("bad state: %d", code)
	}
	select {
	case r := <-s.Result():
		t.Fatalf("чужий state не повинен завершувати вхід: %+v", r)
	default:
	}

	code, page := get(t, root+"/auth?state=st123&code=CODE1")
	if code != 200 || !strings.Contains(page, `data-state="st123"`) {
		t.Fatalf("callback: %d", code)
	}
	r := <-s.Result()
	if r.Err != nil || r.Code != "CODE1" {
		t.Fatalf("result: %+v", r)
	}

	// pending → done(registered)
	_, body := get(t, root+"/auth/status?state=st123")
	var o Outcome
	_ = json.Unmarshal([]byte(body), &o)
	if o.Phase != "pending" {
		t.Fatalf("очікував pending, є %+v", o)
	}
	s.Finish(Outcome{Phase: "done", Registered: true, Licensed: false, Nickname: "Steve_1"})
	_, body = get(t, root+"/auth/status?state=st123")
	_ = json.Unmarshal([]byte(body), &o)
	if o.Phase != "done" || !o.Registered || o.Licensed || o.Nickname != "Steve_1" || o.Provider != "google" {
		t.Fatalf("підсумок: %+v", o)
	}
	// статус із чужим state недоступний
	if code, _ := get(t, root+"/auth/status?state=zzz"); code != http.StatusGone {
		t.Fatalf("status bad state: %d", code)
	}
}

func TestLoopbackErrorCallback(t *testing.T) {
	s, err := Callback.Begin("st9", "microsoft")
	if err != nil {
		t.Skipf("порт зайнятий зовнішнім процесом: %v", err)
	}
	defer s.Close()
	q := url.Values{"state": {"st9"}, "error": {"access_denied"}}
	get(t, "http://localhost:29111/auth?"+q.Encode())
	r := <-s.Result()
	if r.Err == nil {
		t.Fatal("очікував помилку")
	}
	_, body := get(t, "http://localhost:29111/auth/status?state=st9")
	var o Outcome
	_ = json.Unmarshal([]byte(body), &o)
	if o.Phase != "error" || o.Message != "Вхід скасовано." {
		t.Fatalf("підсумок: %+v", o)
	}
}

// Новий вхід замінює старий; Finish/Close старої сесії нічого не ламають.
func TestLoopbackReplaceSession(t *testing.T) {
	s1, err := Callback.Begin("a", "google")
	if err != nil {
		t.Skipf("порт зайнятий: %v", err)
	}
	s2, err := Callback.Begin("b", "microsoft")
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	time.Sleep(50 * time.Millisecond)
	s1.Finish(Outcome{Phase: "done"}) // no-op
	s1.Close()                        // не повинен закрити s2
	if code, _ := get(t, "http://localhost:29111/auth/status?state=b"); code != 200 {
		t.Fatalf("s2 мав лишитись живим: %d", code)
	}
}
