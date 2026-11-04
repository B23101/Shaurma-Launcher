package newupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"shaurma-trust"
)

// ErrNotFound — ассета/релізу немає (нормально: R2 ще не опубліковано).
var ErrNotFound = errors.New("не знайдено")

// source — усі мережеві звернення нової системи. Підміняється в тестах.
type source struct {
	githubBase string // https://github.com
	repo       string
	signalURL  string
	allowHost  func(string) bool
	client     *http.Client
	// serverDate — заголовок Date останньої відповіді (детекція збитого годинника).
	serverDate time.Time
}

func newSource(ks *trust.Keyset) *source {
	s := &source{githubBase: "https://github.com", repo: DefaultRepo, signalURL: DefaultSignalURL}
	hosts := DefaultAssetHosts
	if ks != nil {
		if ks.Sources.Repo != "" {
			s.repo = ks.Sources.Repo
		}
		s.signalURL = ks.Sources.SignalURL
		if len(ks.Sources.AssetHosts) > 0 {
			hosts = ks.Sources.AssetHosts
		}
	}
	s.allowHost = func(h string) bool {
		h = strings.ToLower(h)
		if h == "github.com" {
			return true
		}
		for _, a := range hosts {
			if h == a {
				return true
			}
		}
		return false
	}
	s.client = s.httpClient(30 * time.Second)
	if testSourceHook != nil {
		testSourceHook(s)
	}
	return s
}

// testSourceHook — лише для тестів (фейковий GitHub/worker на httptest).
var testSourceHook func(*source)

// UseTestEndpoints — для наскрізних тестів інших пакетів (core): GitHub і
// worker замінюються на тестовий сервер. Повертає функцію відновлення.
func UseTestEndpoints(githubBase, testHost string, client func(*http.Client)) func() {
	prev := testSourceHook
	testSourceHook = func(s *source) {
		s.githubBase = githubBase
		allow := s.allowHost
		s.allowHost = func(h string) bool { return h == testHost || allow(h) }
		c := s.httpClient(5 * time.Second)
		client(c)
		s.client = c
	}
	return func() { testSourceHook = prev }
}

// SetClockForTest — фіксований час для тестів інших пакетів.
func SetClockForTest(t time.Time) func() {
	prev := now
	now = func() time.Time { return t }
	return func() { now = prev }
}

func (s *source) httpClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "https" {
				return fmt.Errorf("редирект на небезпечну схему %q відхилено", req.URL.Scheme)
			}
			if len(via) >= 5 {
				return errors.New("забагато редиректів")
			}
			if !s.allowHost(req.URL.Hostname()) {
				return fmt.Errorf("редирект на недозволений хост %q відхилено", req.URL.Hostname())
			}
			return nil
		},
	}
}

// AssetURL будує адресу ассета релізу САМ (§5.4): з репозиторію і тега.
// Порожній тег — останній реліз (GitHub не рахує це в ліміт API).
func (s *source) AssetURL(tag, name string) string {
	repo := s.repo
	if tag == "" {
		return s.githubBase + "/" + repo + "/releases/latest/download/" + url.PathEscape(name)
	}
	return s.githubBase + "/" + repo + "/releases/download/" + url.PathEscape(tag) + "/" + url.PathEscape(name)
}

// get читає відповідь з межею розміру.
func (s *source) get(ctx context.Context, rawURL string, max int64, hdr map[string]string) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" {
		return nil, fmt.Errorf("відхилено небезпечну адресу %q", rawURL)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "ShaurmaLauncher/updater")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if d, err := http.ParseTime(resp.Header.Get("Date")); err == nil {
		s.serverDate = d
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, u.Host+u.Path)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, errors.New("відповідь завелика")
	}
	return data, nil
}

// Asset — ассет релізу і його .sig.
func (s *source) Asset(ctx context.Context, tag, name string) (payload []byte, sig string, err error) {
	payload, err = s.get(ctx, s.AssetURL(tag, name), trust.MaxDocSize, nil)
	if err != nil {
		return nil, "", err
	}
	sigBytes, err := s.get(ctx, s.AssetURL(tag, name+trust.SigSuffix), 4096, nil)
	if err != nil {
		return nil, "", fmt.Errorf("%s%s: %w", name, trust.SigSuffix, err)
	}
	return payload, strings.TrimSpace(string(sigBytes)), nil
}

// Signal питає новий worker про версію. Помилка — не біда: клієнт іде на
// GitHub напряму.
func (s *source) Signal(ctx context.Context, channel, current, platform string) (*trust.Signal, error) {
	if s.signalURL == "" {
		return nil, errors.New("signal_url не задано")
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	q := url.Values{"channel": {channel}, "current": {current}, "platform": {platform}}
	data, err := s.get(ctx, s.signalURL+"/v1/update/check?"+q.Encode(), 16<<10, map[string]string{"Accept": "application/json"})
	if err != nil {
		return nil, err
	}
	var sig trust.Signal
	if err := json.Unmarshal(data, &sig); err != nil {
		return nil, err
	}
	if err := sig.Validate(); err != nil {
		return nil, err
	}
	return &sig, nil
}

// Report — звіт серверу про відхилене оновлення (best-effort, без помилок).
func (s *source) Report(version, reason string) {
	if s.signalURL == "" {
		return
	}
	body, _ := json.Marshal(map[string]string{"version": version, "reason": reason})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.signalURL+"/v1/update/report", strings.NewReader(string(body)))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "ShaurmaLauncher/updater")
	if resp, err := s.client.Do(req); err == nil {
		resp.Body.Close()
	}
}
