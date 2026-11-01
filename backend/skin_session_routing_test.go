package main

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"shaurma-launcher-backend/internal/model"
)

func TestSkinSessionOnlyReachesMojang(t *testing.T) {
	for _, tc := range []struct{ method, path, host, target, body string }{
		{"POST", "/sessionserver/session/minecraft/join", "sessionserver.mojang.com", "/session/minecraft/join", `{"accessToken":"synthetic-session","selectedProfile":"test","serverId":"test"}`},
		{"GET", "/sessionserver/session/minecraft/hasJoined?username=Player&serverId=test", "sessionserver.mojang.com", "/session/minecraft/hasJoined", ""},
		{"POST", "/minecraftservices/player/certificates", "api.minecraftservices.com", "/player/certificates", ""},
	} {
		t.Run(tc.path, func(t *testing.T) {
			calls := 0
			p := &skinProxy{port: 12345, upstream: "https://skin-test.invalid/api/yggdrasil"}
			p.client = &http.Client{Transport: skinTestTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Scheme != "https" || r.URL.Host != tc.host || r.URL.Path != tc.target {
					t.Fatalf("wrong auth destination: %s", r.URL)
				}
				body, _ := io.ReadAll(r.Body)
				if string(body) != tc.body || r.Method != tc.method {
					t.Fatal("session request was changed")
				}
				if r.Header.Get("Authorization") != "Bearer synthetic-session" {
					t.Fatal("official service lost authorization")
				}
				if tc.method == "GET" && r.URL.Query().Get("serverId") != "test" {
					t.Fatal("session query was lost")
				}
				return &http.Response{StatusCode: 204, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}, nil
			})}
			req := httptest.NewRequest(tc.method, "http://127.0.0.1:12345"+tc.path, strings.NewReader(tc.body))
			req.Header.Set("Authorization", "Bearer synthetic-session")
			rec := httptest.NewRecorder()
			p.ServeHTTP(rec, req)
			if rec.Code != 204 || calls != 1 {
				t.Fatalf("status=%d calls=%d", rec.Code, calls)
			}
		})
	}
}

func TestSkinSessionDoesNotFollowCredentialRedirect(t *testing.T) {
	for _, status := range []int{301, 302, 303, 307, 308} {
		calls := 0
		p := &skinProxy{port: 12345, client: &http.Client{Transport: skinTestTransport(func(r *http.Request) (*http.Response, error) {
			calls++
			if calls > 1 || r.URL.Host != "sessionserver.mojang.com" {
				t.Fatal("credentials followed redirect")
			}
			return &http.Response{StatusCode: status, Header: http.Header{"Location": {"https://skin-test.invalid/capture"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
		})}}
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, httptest.NewRequest("POST", "http://127.0.0.1:12345/sessionserver/session/minecraft/join", strings.NewReader(`{"accessToken":"synthetic-session"}`)))
		if rec.Code != 502 || calls != 1 {
			t.Fatalf("redirect %d: status=%d calls=%d", status, rec.Code, calls)
		}
	}
}

func TestSkinProxyRejectsUnknownAndCredentialBodies(t *testing.T) {
	p := &skinProxy{port: 12345, client: &http.Client{Transport: skinTestTransport(func(*http.Request) (*http.Response, error) {
		t.Fatal("rejected request reached network")
		return nil, errors.New("unexpected network")
	})}}
	for _, path := range []string{"/authserver/authenticate", "/sessionserver/session/minecraft/join/", "/api/profiles/minecraft"} {
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, httptest.NewRequest("POST", "http://127.0.0.1:12345"+path, strings.NewReader(`{"accessToken":"synthetic-session"}`)))
		if rec.Code != 400 && rec.Code != 404 {
			t.Fatalf("%s: %d", path, rec.Code)
		}
	}
}

func TestSkinProxyBatchDoesNotForwardCredentialsOrQuery(t *testing.T) {
	calls := 0
	p := &skinProxy{port: 12345, nicks: map[string]string{}, upstream: "https://skin-test.invalid/api/yggdrasil"}
	p.client = &http.Client{Transport: skinTestTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		body, _ := io.ReadAll(r.Body)
		if r.Header.Get("Authorization") != "" || r.URL.RawQuery != "" || string(body) != `["Player"]` {
			t.Fatal("profile lookup forwarded extra data")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("[]"))}, nil
	})}
	req := httptest.NewRequest("POST", "http://127.0.0.1:12345/api/profiles/minecraft?accessToken=synthetic-session", strings.NewReader(`["Player"]`))
	req.Header.Set("Authorization", "Bearer synthetic-session")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	if rec.Code != 200 || calls != 1 {
		t.Fatalf("status=%d calls=%d", rec.Code, calls)
	}
}

func TestSkinProxyFailureFallbackByAccount(t *testing.T) {
	for _, tc := range []struct {
		name    string
		account model.Account
		direct  bool
	}{
		{"offline", model.Account{Type: "offline"}, true},
		{"offline placeholder", model.Account{Type: "offline", AccessToken: "0"}, true},
		{"microsoft", model.Account{Type: "microsoft", AccessToken: "synthetic-session"}, false},
		{"licensed without token", model.Account{IsLicensed: true}, false},
		{"mislabelled game token", model.Account{Type: "offline", AccessToken: "synthetic-session"}, false},
		{"refresh token", model.Account{Type: "offline", RefreshToken: "synthetic-refresh"}, false},
		{"ms token", model.Account{Type: "offline", MsAccessToken: "synthetic-ms"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newSkinsTestApp(t, false)
			authlibLocalProxyFn = func(*App, string, string, []string) (string, string, error) {
				return "", "", errors.New("local proxy unavailable")
			}
			args := a.prepareOfflineAccount(tc.account, "build", t.TempDir())
			if tc.direct {
				if !hasAgent(args) || !strings.Contains(args[0], "=https://skin-test.invalid/api/yggdrasil/") {
					t.Fatalf("offline fallback missing: %v", args)
				}
			} else if len(args) != 0 {
				t.Fatalf("credential-bearing account got remote agent: %v", args)
			}
		})
	}
}

func TestSkinProxyDisabledMojangSkinFallbackKeepsBoundary(t *testing.T) {
	fx := newProxyFixture(t, nil, map[string]string{"Player": "https://textures.minecraft.net/texture/test"})
	fx.p.disableMojangFallback = true
	fx.p.learn("Player")
	getProfile(t, fx.p, offlineUUIDHex("Player"))
	if fx.mojangHits.Load() != 0 {
		t.Fatal("disabled skin fallback still queried Mojang")
	}
}

func TestSkinProxyRestartsAfterLocalServerStops(t *testing.T) {
	a := newSkinsTestApp(t, false)
	account := model.Account{Type: "microsoft", IsLicensed: true, AccessToken: "synthetic-session"}
	if args := a.prepareOfflineAccount(account, "build", t.TempDir()); !hasAgent(args) {
		t.Fatal("initial local proxy missing")
	}
	skinProxyMu.Lock()
	previous := theSkinProxy
	skinProxyMu.Unlock()
	if err := previous.srv.Close(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		previous.mu.Lock()
		dead := previous.dead
		previous.mu.Unlock()
		if dead {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("closed proxy not marked dead")
		}
		time.Sleep(time.Millisecond)
	}
	args := a.prepareOfflineAccount(account, "build", t.TempDir())
	if !hasAgent(args) || !strings.Contains(args[0], "=http://127.0.0.1:") {
		t.Fatalf("restart did not use loopback: %v", args)
	}
	skinProxyMu.Lock()
	current := theSkinProxy
	skinProxyMu.Unlock()
	if current == previous {
		t.Fatal("closed proxy reused")
	}
}

func TestSkinSessionMojangFailureNeverFallsBackToShaurma(t *testing.T) {
	calls := 0
	p := &skinProxy{port: 12345, upstream: "https://skin-test.invalid/api/yggdrasil"}
	p.client = &http.Client{Transport: skinTestTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "sessionserver.mojang.com" {
			t.Fatal("session sent to skin service")
		}
		return nil, errors.New("official service unavailable")
	})}
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httptest.NewRequest("POST", "http://127.0.0.1:12345/sessionserver/session/minecraft/join", strings.NewReader(`{"accessToken":"synthetic-session"}`)))
	if rec.Code != 502 || calls != 1 {
		t.Fatalf("status=%d calls=%d", rec.Code, calls)
	}
}
