package minecraft

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// ── Кеш мета-відповідей (TTL 30 хв, docs/dev/LOADER_VALIDATION.md) ──────────────

// TestLoaderValidatorFetchCache — повторний fetch тієї ж URL не йде в
// мережу: успішна відповідь кешується, друга спроба береться з кешу.
func TestLoaderValidatorFetchCache(t *testing.T) {
	var mu sync.Mutex
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		mu.Unlock()
		w.Write([]byte(`[{"loader":{"version":"1.0.0"}}]`))
	}))
	defer srv.Close()

	v := NewLoaderValidator()
	body1, err := v.fetchWithRetry(srv.URL, 1)
	if err != nil {
		t.Fatalf("перший fetch: %v", err)
	}
	body2, err := v.fetchWithRetry(srv.URL, 1)
	if err != nil {
		t.Fatalf("другий fetch (з кешу): %v", err)
	}
	if string(body1) != string(body2) {
		t.Fatalf("тіло з кешу відрізняється від оригіналу: %q vs %q", body1, body2)
	}
	mu.Lock()
	if requests != 1 {
		t.Fatalf("очікували 1 мережевий запит (другий — з кешу), отримали %d", requests)
	}
	mu.Unlock()
}

// TestLoaderValidatorFetchCacheNoNegativeCache — НЕвдалі відповіді (404)
// не кешуються: повторний fetch знову йде в мережу (тимчасовий збій не
// «заморожує» валідацію на 30 хв).
func TestLoaderValidatorFetchCacheNoNegativeCache(t *testing.T) {
	var mu sync.Mutex
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		mu.Unlock()
		http.NotFound(w, r)
	}))
	defer srv.Close()

	v := NewLoaderValidator()
	if _, err := v.fetchWithRetry(srv.URL, 1); err == nil {
		t.Fatal("очікували помилку для 404 (перша спроба)")
	}
	if _, err := v.fetchWithRetry(srv.URL, 1); err == nil {
		t.Fatal("очікували помилку для 404 (друга спроба)")
	}
	mu.Lock()
	if requests != 2 {
		t.Fatalf("404 не має кешуватись: очікували 2 запити, отримали %d", requests)
	}
	mu.Unlock()
}

// TestLoaderValidatorValidationUsesCachedList — дві валідації РІЗНИХ версій
// того самого Minecraft користуються ОДНИМ закешованим списком версій:
// список качається один раз, друга валідація не йде в мережу.
func TestLoaderValidatorValidationUsesCachedList(t *testing.T) {
	old := fabricMetaURLs
	defer func() { fabricMetaURLs = old }()

	var mu sync.Mutex
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		mu.Unlock()
		w.Write([]byte(`[
			{"loader":{"version":"0.15.0"}},
			{"loader":{"version":"0.14.0"}}
		]`))
	}))
	defer srv.Close()
	fabricMetaURLs = []string{srv.URL + "/"}

	v := NewLoaderValidator()
	if err := v.ValidateLoaderVersion("fabric", "1.20.1", "0.15.0"); err != nil {
		t.Fatalf("0.15.0 має бути валідною: %v", err)
	}
	if err := v.ValidateLoaderVersion("fabric", "1.20.1", "0.14.0"); err != nil {
		t.Fatalf("0.14.0 має бути валідною: %v", err)
	}
	mu.Lock()
	if requests != 1 {
		t.Fatalf("очікували 1 запит списку (друга валідація з кешу), отримали %d", requests)
	}
	mu.Unlock()
}

// TestLoaderValidatorFabric перевіряє валідацію версій Fabric.
// Тестуємо як існуючі версії (мають пройти), так і неіснуючі (мають впасти
// з детальною помилкою).
func TestLoaderValidatorFabric(t *testing.T) {
	validator := NewLoaderValidator()

	tests := []struct {
		name          string
		mcVersion     string
		loaderVersion string
		shouldPass    bool
		errorContains string
	}{
		{
			name:          "рекомендована версія завжди валідна",
			mcVersion:     "1.20.1",
			loaderVersion: "recommended",
			shouldPass:    true,
		},
		{
			name:          "порожня версія (auto-resolve)",
			mcVersion:     "1.20.1",
			loaderVersion: "",
			shouldPass:    true,
		},
		{
			name:          "неіснуюча версія має впасти",
			mcVersion:     "1.20.1",
			loaderVersion: "99.99.99",
			shouldPass:    false,
			errorContains: "не існує",
		},
		{
			name:          "неіснуюча MC версія",
			mcVersion:     "99.99.99",
			loaderVersion: "0.15.0",
			shouldPass:    false,
			errorContains: "не підтримує",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateLoaderVersion("fabric", tt.mcVersion, tt.loaderVersion)
			
			if tt.shouldPass {
				if err != nil {
					t.Errorf("очікували успіх, отримали помилку: %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("очікували помилку, отримали успіх")
				} else if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("помилка не містить очікуваний текст %q: %v", tt.errorContains, err)
				}
			}
		})
	}
}

// TestLoaderValidatorQuilt перевіряє валідацію версій Quilt.
func TestLoaderValidatorQuilt(t *testing.T) {
	validator := NewLoaderValidator()

	tests := []struct {
		name          string
		mcVersion     string
		loaderVersion string
		shouldPass    bool
	}{
		{
			name:          "рекомендована версія",
			mcVersion:     "1.20.1",
			loaderVersion: "recommended",
			shouldPass:    true,
		},
		{
			name:          "неіснуюча версія",
			mcVersion:     "1.20.1",
			loaderVersion: "99.99.99",
			shouldPass:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateLoaderVersion("quilt", tt.mcVersion, tt.loaderVersion)
			
			if tt.shouldPass && err != nil {
				t.Errorf("очікували успіх, отримали помилку: %v", err)
			}
			if !tt.shouldPass && err == nil {
				t.Errorf("очікували помилку, отримали успіх")
			}
		})
	}
}

// TestLoaderValidatorForge перевіряє валідацію версій Forge через Maven.
func TestLoaderValidatorForge(t *testing.T) {
	validator := NewLoaderValidator()

	tests := []struct {
		name          string
		mcVersion     string
		loaderVersion string
		shouldPass    bool
		errorContains string
	}{
		{
			name:          "рекомендована версія",
			mcVersion:     "1.20.1",
			loaderVersion: "recommended",
			shouldPass:    true,
		},
		{
			name:          "неіснуюча версія Forge",
			mcVersion:     "1.20.1",
			loaderVersion: "99.99.99",
			shouldPass:    false,
			errorContains: "не існує",
		},
		{
			name:          "MC версія без підтримки Forge",
			mcVersion:     "99.99.99",
			loaderVersion: "1.0.0",
			shouldPass:    false,
			errorContains: "не підтримує",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateLoaderVersion("forge", tt.mcVersion, tt.loaderVersion)
			
			if tt.shouldPass {
				if err != nil {
					t.Errorf("очікували успіх, отримали помилку: %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("очікували помилку, отримали успіх")
				} else if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("помилка не містить %q: %v", tt.errorContains, err)
				}
			}
		})
	}
}

// TestLoaderValidatorNeoForge перевіряє валідацію версій NeoForge.
func TestLoaderValidatorNeoForge(t *testing.T) {
	validator := NewLoaderValidator()

	tests := []struct {
		name          string
		mcVersion     string
		loaderVersion string
		shouldPass    bool
	}{
		{
			name:          "рекомендована версія",
			mcVersion:     "1.21.1",
			loaderVersion: "recommended",
			shouldPass:    true,
		},
		{
			name:          "неіснуюча версія",
			mcVersion:     "1.21.1",
			loaderVersion: "99.99.99",
			shouldPass:    false,
		},
		// РЕГРЕСІЙНИЙ ТЕСТ бага 1.20.1: NeoForge для 1.20.1 нумерується
		// 47.1.x (без префікса "20.1."), стара префіксна хевристика
		// повертала порожній список і NeoForge не встановлювався.
		{
			name:          "NeoForge 1.20.1 (47.1.x) має бути валідним",
			mcVersion:     "1.20.1",
			loaderVersion: "47.1.106",
			shouldPass:    true,
		},
		{
			name:          "NeoForge 1.20.1 неіснуюча версія",
			mcVersion:     "1.20.1",
			loaderVersion: "20.1.999",
			shouldPass:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateLoaderVersion("neoforge", tt.mcVersion, tt.loaderVersion)
			
			if tt.shouldPass && err != nil {
				t.Errorf("очікували успіх, отримали помилку: %v", err)
			}
			if !tt.shouldPass && err == nil {
				t.Errorf("очікували помилку, отримали успіх")
			}
		})
	}
}

// TestLoaderValidatorForge1201 перевіряє, що Forge 1.20.1 валідується
// через Prism-meta (recommended прапорець 47.4.10), а не префіксно.
func TestLoaderValidatorForge1201(t *testing.T) {
	validator := NewLoaderValidator()

	err := validator.ValidateLoaderVersion("forge", "1.20.1", "47.4.10")
	if err != nil {
		t.Errorf("Forge 47.4.10 для 1.20.1 має бути валідним (recommended у Prism-meta), отримали: %v", err)
	}

	err = validator.ValidateLoaderVersion("forge", "1.20.1", "47.2.0")
	if err != nil {
		t.Errorf("Forge 47.2.0 для 1.20.1 існує у Prism-meta, отримали: %v", err)
	}
}

// TestLoaderValidatorVanilla перевіряє, що vanilla завжди валідна.
func TestLoaderValidatorVanilla(t *testing.T) {
	validator := NewLoaderValidator()

	err := validator.ValidateLoaderVersion("vanilla", "1.20.1", "")
	if err != nil {
		t.Errorf("vanilla має завжди бути валідна, отримали помилку: %v", err)
	}

	err = validator.ValidateLoaderVersion("", "1.20.1", "")
	if err != nil {
		t.Errorf("порожній лоадер має бути валідний, отримали помилку: %v", err)
	}
}

// TestLoaderValidatorErrorMessages перевіряє якість повідомлень про помилки.
// Prism Launcher показує детальні помилки з переліком доступних версій —
// перевіряємо, що наш валідатор робить те саме.
func TestLoaderValidatorErrorMessages(t *testing.T) {
	validator := NewLoaderValidator()

	// Тест неіснуючої версії Fabric — має показати доступні версії
	err := validator.ValidateLoaderVersion("fabric", "1.20.1", "99.99.99")
	if err == nil {
		t.Fatal("очікували помилку для неіснуючої версії")
	}

	errMsg := err.Error()
	
	// Перевіряємо наявність ключових елементів у повідомленні
	requiredParts := []string{
		"Fabric",
		"1.20.1",
		"99.99.99",
	}

	for _, part := range requiredParts {
		if !strings.Contains(errMsg, part) {
			t.Errorf("повідомлення про помилку має містити %q, отримали: %s", part, errMsg)
		}
	}

	t.Logf("Повідомлення про помилку: %s", errMsg)
}

// TestLoaderValidatorFallback перевіряє роботу fallback URLs.
// Якщо основний URL недоступний — має спробувати резервні.
func TestLoaderValidatorFallback(t *testing.T) {
	t.Skip("Тест для інтеграційного тестування з мережею")
	
	validator := NewLoaderValidator()

	// Навіть якщо один з URLs недоступний, валідація має працювати
	err := validator.ValidateLoaderVersion("fabric", "1.20.1", "recommended")
	if err != nil {
		t.Errorf("валідація має працювати через fallback URLs: %v", err)
	}
}

// TestLoaderValidatorUnsupportedLoader перевіряє обробку непідтримуваних лоадерів.
func TestLoaderValidatorUnsupportedLoader(t *testing.T) {
	validator := NewLoaderValidator()

	err := validator.ValidateLoaderVersion("unknown_loader", "1.20.1", "1.0.0")
	if err == nil {
		t.Error("очікували помилку для непідтримуваного лоадера")
	}

	if !strings.Contains(err.Error(), "непідтримуваний") {
		t.Errorf("помилка має вказувати на непідтримуваний лоадер: %v", err)
	}
}
