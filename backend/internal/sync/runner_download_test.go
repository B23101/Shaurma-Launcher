package sync

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ── Баг 3: пауза/зупинка не працювали на завмерлому CDN («0.0 МБ/с,
// кнопки не реагують») ────────────────────────────────────────────────────
// Раніше downloadTask перевіряв прапорець паузи ЛИШЕ між читаннями —
// заблокований Read на мовчазному з'єднанні не можна було розірвати, тож
// Pause/Cancel «не працювали». Тепер pauseStallReader закриває тіло при
// запиті паузи — Read розблоковується і downloadTask повертає errTaskPaused.

func newTestRunner(t *testing.T, installDir string, client *http.Client, maxRetries int) *Runner {
	t.Helper()
	store := NewStateStore(t.TempDir())
	state := &InstallState{
		PackID:             "test-pack",
		Files:              map[string]FileRecord{},
		InstalledOnMissing: map[string]bool{},
	}
	return NewRunner("test-pack", "Test Pack", installDir, "http://worker.local", "tok",
		client, nil, state, store, maxRetries, nil)
}

// stallServer — сервер, що надсилає трохи даних і ЗАВМИРАЄ (більше ніколи
// нічого не шле). release закривається в teardown, щоб httptest.Server.Close
// не завис на обробнику.
func stallServer(t *testing.T) (*httptest.Server, *chan struct{}, *int32) {
	t.Helper()
	release := make(chan struct{})
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Length", "100000")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("partial-data-"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-release // завмираємо назавжди
	}))
	t.Cleanup(func() {
		close(release)
		srv.Close()
	})
	return srv, &release, &hits
}

func TestRunnerDownloadPauseUnblocksStalledConnection(t *testing.T) {
	srv, _, _ := stallServer(t)

	oldStall := syncStallTimeout
	syncStallTimeout = 30 * time.Second // лише пауза має розірвати читання
	defer func() { syncStallTimeout = oldStall }()

	dir := t.TempDir()
	r := newTestRunner(t, dir, &http.Client{}, 3)
	task := Task{URL: srv.URL, DestRelPath: "mods/test.jar", SHA256: strings.Repeat("a", 64)}

	done := make(chan error, 1)
	go func() { done <- r.downloadTask(context.Background(), task) }()

	// Даємо з'єднанню встановитись і завмерти.
	time.Sleep(300 * time.Millisecond)
	start := time.Now()
	r.Pause()

	select {
	case err := <-done:
		if err != errTaskPaused {
			t.Fatalf("очікували errTaskPaused, отримали: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("downloadTask не розблокувався після Pause — пауза на завмерлому CDN досі не працює")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("пауза спрацювала надто повільно: %v", elapsed)
	}
}

func TestRunnerDownloadCancelUnblocksStalledConnection(t *testing.T) {
	srv, _, _ := stallServer(t)

	oldStall := syncStallTimeout
	syncStallTimeout = 30 * time.Second
	defer func() { syncStallTimeout = oldStall }()

	dir := t.TempDir()
	r := newTestRunner(t, dir, &http.Client{}, 3)
	task := Task{URL: srv.URL, DestRelPath: "mods/test.jar", SHA256: strings.Repeat("a", 64)}

	done := make(chan error, 1)
	go func() { done <- r.downloadTask(context.Background(), task) }()

	time.Sleep(300 * time.Millisecond)
	r.Cancel()

	select {
	case err := <-done:
		if err != errTaskPaused {
			t.Fatalf("очікували errTaskPaused (cancel теж зупиняє гарячий цикл), отримали: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("downloadTask не розблокувався після Cancel")
	}
}

// TestRunnerDownloadStallTimesOutAndRetries — стіл з'єднання має обриватись
// через syncStallTimeout, а retry-цикл докачувати файл до кінця (а не
// висіти вічно на «0.0 МБ/с»).
func TestRunnerDownloadStallTimesOutAndRetries(t *testing.T) {
	oldStall := syncStallTimeout
	syncStallTimeout = 400 * time.Millisecond
	defer func() { syncStallTimeout = oldStall }()

	var reqs int32
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&reqs, 1) == 1 {
			// Перша спроба: трохи даних і стіл.
			w.Header().Set("Content-Length", "1000")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("AA"))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			<-release
			return
		}
		// Друга спроба: повний контент (ігноруємо Range — 200).
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(strings.Repeat("B", 1000)))
	}))
	t.Cleanup(func() {
		close(release)
		srv.Close()
	})

	dir := t.TempDir()
	r := newTestRunner(t, dir, &http.Client{}, 3)
	sum := sha256.Sum256([]byte(strings.Repeat("B", 1000)))
	task := Task{URL: srv.URL, DestRelPath: "mods/test.jar", SHA256: fmt.Sprintf("%x", sum), SizeBytes: 1000}

	start := time.Now()
	if err := r.downloadTask(context.Background(), task); err != nil {
		t.Fatalf("downloadTask: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("завантаження зайняло надто довго: %v", elapsed)
	}
	if atomic.LoadInt32(&reqs) < 2 {
		t.Fatalf("очікували щонайменше 2 спроби (стіл → ретрай), було %d", atomic.LoadInt32(&reqs))
	}

	data, err := os.ReadFile(filepath.Join(dir, "mods", "test.jar"))
	if err != nil {
		t.Fatalf("читання файлу: %v", err)
	}
	if len(data) != 1000 || string(data) != strings.Repeat("B", 1000) {
		t.Fatalf("файл пошкоджений: len=%d", len(data))
	}
}

// TestRunnerDownloadSHA1VerifyAfterResume — .part + Range-докачка зі
// стілу зберігає коректний хеш фінального файлу.
func TestRunnerDownloadSHA1VerifyAfterResume(t *testing.T) {
	oldStall := syncStallTimeout
	syncStallTimeout = 300 * time.Millisecond
	defer func() { syncStallTimeout = oldStall }()

	content := strings.Repeat("C", 500)
	var reqs int32
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&reqs, 1) == 1 {
			w.Header().Set("Content-Length", fmt.Sprint(len(content)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(content[:100]))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			<-release
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(content))
	}))
	t.Cleanup(func() {
		close(release)
		srv.Close()
	})

	sha1 := sha1Hex(content)

	dir := t.TempDir()
	r := newTestRunner(t, dir, &http.Client{}, 3)
	task := Task{URL: srv.URL, DestRelPath: "mods/verify.jar", SHA1: sha1}

	if err := r.downloadTask(context.Background(), task); err != nil {
		t.Fatalf("downloadTask: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "mods", "verify.jar"))
	if err != nil {
		t.Fatalf("читання файлу: %v", err)
	}
	if string(data) != content {
		t.Fatalf("вміст файлу не збігається")
	}
}

func sha1Hex(s string) string {
	h := sha1.New()
	io.WriteString(h, s)
	return fmt.Sprintf("%x", h.Sum(nil))
}
