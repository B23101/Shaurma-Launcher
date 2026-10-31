package custompack

import (
	"net/http"
	"testing"
)

func TestCurseForgeCDNHostBoundary(t *testing.T) {
	for _, raw := range []string{"https://evil.test/?forgecdn.net", "https://forgecdn.net.evil.test/file", "https://forgecdn.net@evil.test/file", "http://edge.forgecdn.net/file", "https://edge.forgecdn.net:444/file"} {
		if IsCurseForgeCDNURL(raw) {
			t.Errorf("untrusted URL accepted: %s", raw)
		}
	}
	for _, raw := range []string{"https://edge.forgecdn.net/file", "https://media.forgecdn.net/file"} {
		if !IsCurseForgeCDNURL(raw) {
			t.Errorf("CDN rejected: %s", raw)
		}
	}
}

func TestDownloadRedirectStripsAPIKey(t *testing.T) {
	first, _ := http.NewRequest(http.MethodGet, "https://edge.forgecdn.net/file", nil)
	next, _ := http.NewRequest(http.MethodGet, "https://evil.test/file", nil)
	next.Header.Set("x-api-key", "test-value")
	if err := protectDownloadRedirect(next, []*http.Request{first}); err != nil {
		t.Fatal(err)
	}
	if next.Header.Get("x-api-key") != "" {
		t.Fatal("API key forwarded across origins")
	}
	downgrade, _ := http.NewRequest(http.MethodGet, "http://edge.forgecdn.net/file", nil)
	if protectDownloadRedirect(downgrade, []*http.Request{first}) == nil {
		t.Fatal("HTTPS downgrade accepted")
	}
}
