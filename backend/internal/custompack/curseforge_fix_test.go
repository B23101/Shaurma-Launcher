package custompack

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ── Баг 1: повний опис CurseForge (сторінка контенту показувала лише
// summary замість цілої статті) ────────────────────────────────────────────
// CurseForge /mods/{id} інколи віддає description порожнім — канонічне
// джерело повної статті — GET /mods/{id}/description. GetProjectDetail має
// фолбечити на нього.

func TestCurseForgeGetProjectDetailFallsBackToDescriptionEndpoint(t *testing.T) {
	var descHits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/mods/123":
			// Головний ендпоінт: description порожній (реальна поведінка
			// CurseForge для багатьох модпаків).
			fmt.Fprint(w, `{"data":{"id":123,"name":"Create - Forge Frontier","summary":"Короткий опис, 2 речення."}}`)
		case "/mods/123/description":
			descHits++
			fmt.Fprint(w, `{"data":"<p>Повна стаття: automation, exploration, custom progression, unique planets...</p><p>Другий абзац.</p>"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	oldBase, oldKey := curseForgeAPIBase, curseForgeAPIKey
	curseForgeAPIBase, curseForgeAPIKey = srv.URL, "test-key"
	defer func() { curseForgeAPIBase, curseForgeAPIKey = oldBase, oldKey }()

	c := NewCurseForgeClient()
	d, err := c.GetProjectDetail(123)
	if err != nil {
		t.Fatalf("GetProjectDetail: %v", err)
	}
	if !strings.Contains(d.Description, "Повна стаття") {
		t.Fatalf("опис має прийти з /description ендпоінта, отримано: %q", d.Description)
	}
	if descHits != 1 {
		t.Fatalf("/description мав бути викликаний рівно 1 раз, отримано %d", descHits)
	}
}

// БАГ 8: /mods/{id} віддає ЛИШЕ короткий анонс (2 речення) у description,
// а повна стаття приходить з /mods/{id}/description — навіть коли inline-
// опис НЕ порожній (типовий випадок «Create - Forge Frontier»: сторінка
// контенту показувала 2 речення замість цілої статті). Тому /description
// має завжди мати пріоритет, а короткий inline-опис лишається лише
// фолбеком (коли /description віддав порожнє або помилку).
func TestCurseForgeGetProjectDetailPrefersFullDescription(t *testing.T) {
	var descHits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/mods/123":
			// Короткий, але НЕ порожній inline-опис — саме той випадок, який
			// раніше показував 2 речення замість статті.
			fmt.Fprint(w, `{"data":{"id":123,"name":"Create - Forge Frontier","summary":"short","description":"<p>Короткий анонс у 2 речення.</p>"}}`)
		case "/mods/123/description":
			descHits++
			fmt.Fprint(w, `{"data":"<p>Повна стаття: automation, exploration, custom progression...</p>"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	oldBase, oldKey := curseForgeAPIBase, curseForgeAPIKey
	curseForgeAPIBase, curseForgeAPIKey = srv.URL, "test-key"
	defer func() { curseForgeAPIBase, curseForgeAPIKey = oldBase, oldKey }()

	c := NewCurseForgeClient()
	d, err := c.GetProjectDetail(123)
	if err != nil {
		t.Fatalf("GetProjectDetail: %v", err)
	}
	if !strings.Contains(d.Description, "Повна стаття") {
		t.Fatalf("опис має прийти з /description (повна стаття), отримано: %q", d.Description)
	}
	if descHits != 1 {
		t.Fatalf("/description мав бути викликаний рівно 1 раз, отримано %d", descHits)
	}
}

// Фолбек: якщо /description віддав порожнє або помилку — inline-опис з
// /mods/{id} зберігається (сторінка не лишається без тексту).
func TestCurseForgeGetProjectDetailKeepsInlineWhenDescriptionEmpty(t *testing.T) {
	var descHits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/mods/123":
			fmt.Fprint(w, `{"data":{"id":123,"name":"X","summary":"short","description":"<p>Повний інлайн-опис.</p>"}}`)
		case "/mods/123/description":
			descHits++
			fmt.Fprint(w, `{"data":""}`) // повна стаття порожня — фолбек на inline
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	oldBase, oldKey := curseForgeAPIBase, curseForgeAPIKey
	curseForgeAPIBase, curseForgeAPIKey = srv.URL, "test-key"
	defer func() { curseForgeAPIBase, curseForgeAPIKey = oldBase, oldKey }()

	c := NewCurseForgeClient()
	d, err := c.GetProjectDetail(123)
	if err != nil {
		t.Fatalf("GetProjectDetail: %v", err)
	}
	if d.Description != "<p>Повний інлайн-опис.</p>" {
		t.Fatalf("порожній /description → inline-опис має зберегтись, отримано: %q", d.Description)
	}
	if descHits != 1 {
		t.Fatalf("/description мав бути викликаний 1 раз, отримано %d", descHits)
	}
}

// ── Баг 2: rate limit CurseForge API («розхешувалось 2-30 модів і зависло») ─
// Глобальний спейсер має розводити запити в часі (Prism-стиль), а 429 з
// Retry-After — чекати вказаний сервером час.

func TestCFRateLimiterPacesRequests(t *testing.T) {
	l := newCFRateLimiter(10) // інтервал 100мс
	start := time.Now()
	for i := 0; i < 6; i++ {
		if err := l.wait(context.Background()); err != nil {
			t.Fatalf("wait: %v", err)
		}
	}
	elapsed := time.Since(start)
	// 6 слотів по 100мс → щонайменше ~500мс (з запасом на навантажену CI).
	if elapsed < 400*time.Millisecond {
		t.Fatalf("лімітер пропустив послідовні запити надто швидко: %v", elapsed)
	}
}

func TestCFRateLimiterPacesParallelRequests(t *testing.T) {
	l := newCFRateLimiter(10) // інтервал 100мс
	var wg sync.WaitGroup
	start := time.Now()
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := l.wait(context.Background()); err != nil {
				t.Errorf("wait: %v", err)
			}
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)
	// 8 паралельних waiters → останній фінішує не раніше ~700мс.
	if elapsed < 600*time.Millisecond {
		t.Fatalf("паралельні запити не розведені в часі: %v", elapsed)
	}
}

func TestCFRateLimiterWaitHonorsCancel(t *testing.T) {
	l := newCFRateLimiter(1) // інтервал 1с — перший слот миттєво, далі довго
	_ = l.wait(context.Background()) // займаємо перший слот
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- l.wait(ctx) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("wait мав повернути помилку після скасування")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("wait не розблокувався по ctx.Done()")
	}
}

func TestCurseForge429RespectsRetryAfter(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomicInc(&calls)
		if n <= 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		fmt.Fprint(w, `{"data":{"id":1,"fileName":"mod.jar","downloadUrl":"https://example.com/mod.jar"}}`)
	}))
	defer srv.Close()

	oldBase, oldKey := curseForgeAPIBase, curseForgeAPIKey
	curseForgeAPIBase, curseForgeAPIKey = srv.URL, "test-key"
	defer func() { curseForgeAPIBase, curseForgeAPIKey = oldBase, oldKey }()

	c := NewCurseForgeClient()
	start := time.Now()
	info, err := c.GetModFile(1, 1)
	if err != nil {
		t.Fatalf("GetModFile: %v", err)
	}
	if info.DownloadURL != "https://example.com/mod.jar" {
		t.Fatalf("невірний URL: %s", info.DownloadURL)
	}
	// 429 з Retry-After: 1 → друга спроба не раніше ніж через ~1с.
	if elapsed := time.Since(start); elapsed < 900*time.Millisecond {
		t.Fatalf("Retry-After не був поважаний: %v", elapsed)
	}
}

func atomicInc(p *int32) int32 {
	return atomic.AddInt32(p, 1)
}
