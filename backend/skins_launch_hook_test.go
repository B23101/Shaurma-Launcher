package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shaurma-launcher-backend/internal/console"
	"shaurma-launcher-backend/internal/model"
	"shaurma-launcher-backend/internal/nodes"
)

type skinTestTransport func(*http.Request) (*http.Response, error)

func (f skinTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// The shared skin service applies to all account types when enabled.
// Tests use a local metadata transport and a synthetic, integrity-checked JAR.

// seedInjectorCache кладе на диск правдоподібний jar і metadata-кеш, щоб
// authlibJVMArgs не потребував мережі (в тесті вона недоступна/не потрібна:
// jar береться з кешу, а metadata — з кешу як фолбек при помилці мережі).
func seedInjectorCache(t *testing.T, a *App) (jarPath string) {
	t.Helper()
	dir := a.javaToolsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	jarPath = filepath.Join(dir, authlibInjectorJarName)
	// ≥ minAuthlibJarSize (50 КБ) — інакше authlibInjectorJar піде в мережу.
	fixture := make([]byte, minAuthlibJarSize+1024)
	copy(fixture, []byte{'P', 'K', 3, 4})
	sum := sha256.Sum256(fixture)
	previousDigest := authlibInjectorSHA256
	authlibInjectorSHA256 = hex.EncodeToString(sum[:])
	t.Cleanup(func() { authlibInjectorSHA256 = previousDigest })
	if err := os.WriteFile(jarPath, fixture, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "yggdrasil-metadata.b64"),
		[]byte("dGVzdC1tZXRhZGF0YS1mb3ItYXV0aGxpYi1pbmplY3Rvcg=="), 0o644); err != nil {
		t.Fatal(err)
	}
	return jarPath
}

func newSkinsTestApp(t *testing.T, disableSkins bool) *App {
	t.Helper()
	dir := t.TempDir()
	cfg := newTestConfig(t, dir)
	s := cfg.GetSettings()
	s.DisableSkins = disableSkins
	s.DisableMojangSkinFallback = true // Test the launch hook without starting a local proxy.
	if err := cfg.UpdateSettings(s); err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	a := &App{
		cfg:         cfg,
		consoleBufs: map[consoleKey]*console.Buffer{},
	}
	seedInjectorCache(t, a)
	originalPool, originalFetch := vpsPool(), authlibFetchFn
	nodePool = &nodes.Pool{
		WorkerBase: "https://skin-test.invalid",
		Headers:    func() http.Header { return http.Header{} },
		HTTP: &http.Client{Transport: skinTestTransport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"nodes":[{"id":"test","url":"https://skin-test.invalid","roles":["gateway"],"weight":1}]}`))}, nil
		})},
	}
	if err := nodePool.Refresh(); err != nil {
		t.Fatal(err)
	}
	authlibFetchFn = func(*App, string, bool) ([]byte, error) {
		return []byte(`{"signaturePublickey":"test-public-key","skinDomains":[]}`), nil
	}
	t.Cleanup(func() { nodePool, authlibFetchFn = originalPool, originalFetch })
	return a
}

func hasAgent(args []string) bool {
	for _, x := range args {
		if strings.HasPrefix(x, "-javaagent:") && strings.Contains(x, authlibInjectorJarName) {
			return true
		}
	}
	return false
}

// Both account types use the configured skin service; disabling it returns no arguments.
func TestPrepareOfflineAccount_InjectorForEnabledSkins(t *testing.T) {
	offline := map[string]model.Account{
		"offline (пірат)":           {ID: "a1", Username: "Pirate", IsLicensed: false, Type: "offline"},
		"Шаурма-скін офлайн акаунт": {ID: "a3", Username: "Shaurma", IsLicensed: false, IsShaurmaSkinAccount: true},
		"Microsoft": {ID: "a2", Username: "Licensed", IsLicensed: true, Type: "microsoft"},
	}
	for name, acc := range offline {
		t.Run(name, func(t *testing.T) {
			a := newSkinsTestApp(t, false)
			args := a.prepareOfflineAccount(acc, "build1", t.TempDir())
			if !hasAgent(args) {
				t.Fatalf("authlib-injector НЕ підключено для офлайн-акаунта «%s»: args=%v", name, args)
			}
			var hasPrefetched bool
			for _, x := range args {
				if strings.HasPrefix(x, "-Dauthlibinjector.yggdrasil.prefetched=") {
					hasPrefetched = true
				}
			}
			if !hasPrefetched {
				t.Fatalf("немає -Dauthlibinjector.yggdrasil.prefetched для «%s»: %v", name, args)
			}
		})
	}

	licensed := map[string]model.Account{
		"ліцензійний (Microsoft)": {ID: "a2", Username: "Licensed", IsLicensed: true, Type: "microsoft", AccessToken: "tok"},
		"ліцензійний без Type":    {ID: "a4", Username: "NoTok", IsLicensed: true},
	}
	for name, acc := range licensed {
		t.Run(name, func(t *testing.T) {
			a := newSkinsTestApp(t, false)
			args := a.prepareOfflineAccount(acc, "build1", t.TempDir())
			if !hasAgent(args) {
				t.Fatalf("увімкнена спільна система скінів не підключена для «%s»: args=%v", name, args)
			}
		})
	}
}

func TestPrepareOfflineAccount_DisableSkinsTurnsOffForEveryAccountType(t *testing.T) {
	unlicensed := []model.Account{
		{ID: "a1", Username: "Pirate", IsLicensed: false, Type: "offline"},
		{ID: "a3", Username: "Shaurma", IsShaurmaSkinAccount: true},
	}
	for _, acc := range unlicensed {
		a := newSkinsTestApp(t, true)
		if args := a.prepareOfflineAccount(acc, "build1", t.TempDir()); len(args) != 0 {
			t.Fatalf("DisableSkins=true, але для «%s» повернуто аргументи: %v", acc.Username, args)
		}
		// І про це є системний рядок у консолі збірки.
		lines := a.consoleBufFor(acc.ID, "build1").Snapshot()
		var said bool
		for _, l := range lines {
			if strings.Contains(l.Text, "Скіни вимкнено") {
				said = true
			}
		}
		if !said {
			t.Fatalf("для «%s» немає повідомлення про вимкнені скіни: %v", acc.Username, lines)
		}
	}

	// Disabling the shared service also leaves Microsoft accounts without an agent.
	lic := model.Account{ID: "a2", Username: "Licensed", IsLicensed: true, Type: "microsoft", AccessToken: "tok"}
	a := newSkinsTestApp(t, true)
	if args := a.prepareOfflineAccount(lic, "build1", t.TempDir()); len(args) != 0 {
		t.Fatalf("ліцензійний акаунт не має отримувати аргументів: %v", args)
	}
	var licensedMsg bool
	for _, l := range a.consoleBufFor(lic.ID, "build1").Snapshot() {
		if strings.Contains(l.Text, "Скіни вимкнено") {
			licensedMsg = true
		}
	}
	if !licensedMsg {
		t.Fatal("немає повідомлення про вимкнену систему скінів")
	}
}

// Якщо інжектор підготувати не вдалось (немає jar і мережі) на ОФЛАЙН-акаунті —
// це лише попередження в консолі, запуск не блокується.
func TestPrepareOfflineAccount_FailureIsWarningNotBlocker(t *testing.T) {
	dir := t.TempDir()
	cfg := newTestConfig(t, dir)
	a := &App{cfg: cfg, consoleBufs: map[consoleKey]*console.Buffer{}}
	// Детермінований збій без опори на мережу: підміняємо завантаження «мережа недоступна».
	orig := authlibFetchFn
	authlibFetchFn = func(*App, string, bool) ([]byte, error) { return nil, errors.New("offline") }
	t.Cleanup(func() { authlibFetchFn = orig })
	acc := model.Account{ID: "o", Username: "O", IsLicensed: false, Type: "offline"}
	if args := a.prepareOfflineAccount(acc, "b", t.TempDir()); len(args) != 0 {
		t.Fatalf("очікували порожні аргументи при збої, маємо %v", args)
	}
	var warned bool
	for _, l := range a.consoleBufFor(acc.ID, "b").Snapshot() {
		if l.Level == console.LevelWarn && strings.Contains(l.Text, "інжектор не підготовлено") {
			warned = true
		}
	}
	if !warned {
		t.Fatal("немає попередження в консолі про збій підготовки інжектора")
	}
}
