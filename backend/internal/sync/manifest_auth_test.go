package sync

import (
	"net/http"
	"strings"
	"testing"
)

// Google-токен іде в X-Shaurma-Auth як є — жодних мережевих викликів,
// жодних Microsoft/Minecraft-токенів.
func TestApplyIdentityHeaders_GoogleTokenAndOfflineNick(t *testing.T) {
	c := NewManifestClient("http://127.0.0.1:1", "tok") // недосяжний: мережі бути не повинно
	c.SetAccountsProvider(func() []AccountCred {
		return []AccountCred{{Token: "g.jwt.token"}, {Nick: "Steve"}}
	})
	req, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:1/x", nil)
	c.applyIdentityHeaders(req)

	got := req.Header.Get("X-Shaurma-Auth")
	if !strings.Contains(got, `"token":"g.jwt.token"`) || !strings.Contains(got, `"nick":"Steve"`) {
		t.Fatalf("X-Shaurma-Auth = %q", got)
	}
}

func TestApplyIdentityHeaders_EmptyCredsNoHeader(t *testing.T) {
	c := NewManifestClient("http://127.0.0.1:1", "tok")
	c.SetAccountsProvider(func() []AccountCred { return []AccountCred{{}} })
	req, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:1/x", nil)
	c.applyIdentityHeaders(req)
	if req.Header.Get("X-Shaurma-Auth") != "" {
		t.Fatalf("заголовок не мав виставлятись")
	}
}
