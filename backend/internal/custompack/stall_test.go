package custompack

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// helperServer — test HTTP server serving `content`; supports Range (206)
// for resume testing.
type helperServer struct {
	srv    *httptest.Server
	reqs   atomic.Int64
	stall  chan struct{} // closed to release the stalled first request
	mu     sync.Mutex
	doStall bool // first full request: send half, then stall
	started bool
}

func newHelperServer(content []byte, stallFirst bool) *helperServer {
	h := &helperServer{doStall: stallFirst, stall: make(chan struct{})}
	h.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.reqs.Add(1)
		if r.Header.Get("Range") == "" {
			h.mu.Lock()
			first := !h.started
			h.started = true
			stall := h.doStall && first
			h.mu.Unlock()
			if stall {
				// Simulate a stalled connection: send half the content,
				// flush, then hang forever (the CDN stops mid-transfer).
				w.Header().Set("Content-Type", "application/octet-stream")
				w.WriteHeader(http.StatusOK)
				w.Write(content[:len(content)/2])
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
				<-h.stall // block until test releases
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			w.WriteHeader(http.StatusOK)
			w.Write(content)
			return
		}
		// Range request: serve from the requested offset (resume).
		rangeVal := r.Header.Get("Range")
		parts := strings.Split(strings.TrimPrefix(rangeVal, "bytes="), "-")
		start, _ := strconv.ParseInt(parts[0], 10, 64)
		if start >= int64(len(content)) {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Range", "bytes "+parts[0]+"-"+strconv.FormatInt(int64(len(content))-1, 10)+"/"+strconv.FormatInt(int64(len(content)), 10))
		w.WriteHeader(http.StatusPartialContent)
		w.Write(content[start:])
	}))
	return h
}

// TestDownloadOneFileStalledConnectionRecovers — репродукція скарги
// «качається, потім зависає на одному моді, швидкість 0.0 МБ/с і ніколи
// не відвисає»: CDN обриває передачу посеред файлу (stall). Раніше
// downloadOneFile висів назавжди (io.Copy блокувався до 5-хв HTTP-таймауту).
// Тепер stallReader обриває читання через downloadStallTimeout, retry-цикл
// перезапускає файл з місця зупинки (Range + наявний .part) і файл докачується.
func TestDownloadOneFileStalledConnectionRecovers(t *testing.T) {
	content := bytes.Repeat([]byte("0123456789abcdef"), 2048)
	srv := newHelperServer(content, true)
	// ВАЖЛИВО порядок defer (LIFO): спершу звільняємо stall-каналом handler,
	// що блокується, і лише потім srv.Close() — інакше Close дочекається
	// блокування handler і тест зависне.
	defer srv.srv.Close()
	defer close(srv.stall)

	// Short stall timeout so the test doesn't wait 30s.
	oldTimeout := downloadStallTimeout
	downloadStallTimeout = 150 * time.Millisecond
	t.Cleanup(func() { downloadStallTimeout = oldTimeout })

	dir := t.TempDir()
	task := importTask{relPath: "mods/stalled.jar", url: srv.srv.URL}
	client := &http.Client{Timeout: 10 * time.Second}

	var downloaded int64
	_, err := downloadOneFile(context.Background(), client, task, dir, func(delta int64) {
		downloaded += delta
	})
	if err != nil {
		t.Fatalf("stalled download must recover via retry, got: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "mods", "stalled.jar"))
	if err != nil {
		t.Fatalf("final file missing: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("final content mismatch: got %d bytes, want %d", len(got), len(content))
	}
	// Request 1 = full (stalled), request 2 = Range resume. Must be ≥2.
	if n := srv.reqs.Load(); n < 2 {
		t.Fatalf("expected ≥2 HTTP requests (full + resume), got %d", n)
	}
	// After the stall, only the tail was downloaded over the wire.
	if downloaded <= 0 {
		t.Fatal("expected progress bytes reported via onBytes")
	}
}

// TestDownloadOneFileExhaustsRetries — коли сервер стабільно віддає 500 на
// кожну спробу, downloadOneFile вичерпує спроби і повертає зрозумілу помилку
// замість вічного зависання (кількість запитів = downloadMaxAttempts).
func TestDownloadOneFileExhaustsRetries(t *testing.T) {
	var reqs atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqs.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	oldAttempts := downloadMaxAttempts
	downloadMaxAttempts = 2
	t.Cleanup(func() { downloadMaxAttempts = oldAttempts })

	dir := t.TempDir()
	task := importTask{relPath: "mods/broken.jar", url: srv.URL}
	_, err := downloadOneFile(context.Background(), &http.Client{Timeout: 10 * time.Second}, task, dir, nil)
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if !strings.Contains(err.Error(), "спроби") {
		t.Fatalf("error should mention exhausted attempts, got: %v", err)
	}
	if reqs.Load() != 2 {
		t.Fatalf("expected exactly %d requests (2 attempts), got %d", downloadMaxAttempts, reqs.Load())
	}
}

// TestDownloadOneFileRetriesOn429 — сервер віддає 429 (rate limit) на першу
// спробу, потім успішно. downloadOneFile має повторити (це й був прояв
// «0.0 МБ/с назавжди» на CurseForge CDN при тротлінгу).
func TestDownloadOneFileRetriesOn429(t *testing.T) {
	content := []byte("rate-limited then served")
	var reqs atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if reqs.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write(content)
	}))
	defer srv.Close()

	oldAttempts := downloadMaxAttempts
	downloadMaxAttempts = 3
	t.Cleanup(func() { downloadMaxAttempts = oldAttempts })

	dir := t.TempDir()
	task := importTask{relPath: "mods/rl.jar", url: srv.URL}
	_, err := downloadOneFile(context.Background(), &http.Client{Timeout: 10 * time.Second}, task, dir, nil)
	if err != nil {
		t.Fatalf("429 should be retried and succeed, got: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "mods", "rl.jar"))
	if string(got) != string(content) {
		t.Fatalf("content mismatch: %q", got)
	}
	if reqs.Load() != 2 {
		t.Fatalf("expected 2 requests (429 + success), got %d", reqs.Load())
	}
}

// TestDownloadOneFileFatalOn404 — 404 не повторюється (fatal), бо повторний
// запит не виправить відсутність файлу — важливо, щоб retry-цикл не крутив
// даремні запити до CDN.
func TestDownloadOneFileFatalOn404(t *testing.T) {
	var reqs atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqs.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	oldAttempts := downloadMaxAttempts
	downloadMaxAttempts = 3
	t.Cleanup(func() { downloadMaxAttempts = oldAttempts })

	dir := t.TempDir()
	task := importTask{relPath: "mods/missing.jar", url: srv.URL}
	_, err := downloadOneFile(context.Background(), &http.Client{Timeout: 10 * time.Second}, task, dir, nil)
	if err == nil {
		t.Fatal("expected error on 404")
	}
	if reqs.Load() != 1 {
		t.Fatalf("404 must be fatal (no retries), got %d requests", reqs.Load())
	}
}

// TestDownloadOneFile416CompletePartSkipsRedownload — сценарій рев'юера:
// попередня спроба скачала ВЕСЬ файл у .part і впала на таймауті до
// перейменування. Нова спроба шле Range на повний розмір → сервер віддає 416.
// Файл уже повний: downloadOneFile має підтвердити SHA1 .part і перейменувати
// БЕЗ повторного качання.
func TestDownloadOneFile416CompletePartSkipsRedownload(t *testing.T) {
	content := bytes.Repeat([]byte("0123456789abcdef"), 512)
	var fullReqs atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") == "" {
			fullReqs.Add(1)
			w.WriteHeader(http.StatusOK)
			w.Write(content)
			return
		}
		// Range на повний розмір або далі — не задовольняємо.
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
	}))
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "mods", "full.jar")
	os.MkdirAll(filepath.Dir(dest), 0755)
	// Попередня спроба: повний вміст у .part (сервер уже віддав усе).
	if err := os.WriteFile(dest+".part", content, 0644); err != nil {
		t.Fatal(err)
	}

	task := importTask{relPath: "mods/full.jar", url: srv.URL, sha1: sha1Hex(content)}
	_, err := downloadOneFile(context.Background(), &http.Client{Timeout: 10 * time.Second}, task, dir, nil)
	if err != nil {
		t.Fatalf("416 with complete .part should succeed via SHA1 verify+rename: %v", err)
	}
	got, _ := os.ReadFile(dest)
	if !bytes.Equal(got, content) {
		t.Fatalf("content mismatch: %d bytes", len(got))
	}
	// Повного качання бути не мало — лише один Range-запит, що дав 416.
	if fullReqs.Load() != 0 {
		t.Fatalf("expected 0 full re-downloads (416 + verified .part), got %d", fullReqs.Load())
	}
}

// TestStallReaderWrapsProgress — прогрес-колбек отримує байти і після
// stall-обриву (потрібно для живого UI-прогресу, а не стрибка 0→100%).
func TestStallReaderWrapsProgress(t *testing.T) {
	content := bytes.Repeat([]byte("x"), 65536)
	// Wrap progressReader manually to verify byte counting through stallReader.
	var counted int64
	pr := &progressReader{r: io.NopCloser(bytes.NewReader(content)), onBytes: func(n int64) { counted += n }}
	sr := newStallReader(context.Background(), pr, time.Hour) // never stalls here
	defer sr.Close()
	written, err := io.Copy(io.Discard, sr)
	if err != nil {
		t.Fatalf("copy through stallReader failed: %v", err)
	}
	if written != int64(len(content)) || counted != int64(len(content)) {
		t.Fatalf("bytes lost through stallReader: written=%d counted=%d want=%d", written, counted, len(content))
	}
}

// TestStallReaderCancelUnblocksImmediately — скасування користувачем
// (кнопка «Скасувати»/«Пауза» під час імпорту) має розблокувати
// заблокований Read МИТТЄВО, а не чекати downloadStallTimeout. Раніше
// stallReader не знав про ctx — скасування під час завислого з'єднання
// «зависало» на 30с, поки таймер стілу не спрацьовував.
func TestStallReaderCancelUnblocksImmediately(t *testing.T) {
	// Блокувальний reader: Read висить, доки його не закриють (стіл).
	pr, pw := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	sr := newStallReader(ctx, pr, time.Hour) // таймер стілу ніколи не спрацює — лише ctx
	defer sr.Close()

	errCh := make(chan error, 1)
	go func() {
		_, err := io.Copy(io.Discard, sr)
		errCh <- err
	}()

	// Даємо горутині почати читання (блоковане), потім скасовуємо.
	time.Sleep(50 * time.Millisecond)
	start := time.Now()
	cancel()
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("io.Copy should return an error after cancel")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancel did not unblock the stalled read within 2s")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("cancel took %v — має бути миттєвим", elapsed)
	}
	// Зачищаємо pipe, щоб не лишити висячі горутини тесту.
	pw.Close()
}
