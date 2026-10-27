package custompack

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestDownloadOneFileSkipsCompleted перевіряє резюме-фікс «пауза→продовження
// не перекачує вже скачане»: якщо файл уже ПОВНІСТЮ на диску з правильним
// SHA1 — downloadOneFile повертає успіх МИТТЄВО, без жодного мережевого
// запиту (URL тут навмисно недосяжний — якби запит пішов, тест впав би на
// таймауті).
func TestDownloadOneFileSkipsCompleted(t *testing.T) {
	content := []byte("already downloaded mod jar content for resume-skip test")
	dir := t.TempDir()
	dest := filepath.Join(dir, "mods", "done.jar")
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, content, 0644); err != nil {
		t.Fatal(err)
	}

	task := importTask{
		relPath:  "mods/done.jar",
		url:      "http://127.0.0.1:1/unreachable.jar", // мережі НЕ має бути взагалі
		sha1:     sha1Hex(content),
		fileSize: int64(len(content)),
	}
	client := &http.Client{Timeout: 2 * time.Second}
	n, err := downloadOneFile(context.Background(), client, task, dir, nil)
	if err != nil {
		t.Fatalf("вже скачаний файл мав бути пропущений, отримали помилку: %v", err)
	}
	if n != int64(len(content)) {
		t.Fatalf("очікували %d байт, отримали %d", len(content), n)
	}
}

// TestResolveBlockedModsManualAndSkip перевіряє, що рішення «скачати вручну»
// (manual — нове, як у Prism) і «пропустити» (skip) на рівні конвеєра
// поводяться однаково: задачі НЕзмінні, помилки немає — імпорт продовжується
// з рештою модів (раніше після вибору у діалозі конвеєр міг «зависнути» на
// очікуванні, бо рішення не доходило до виконання).
func TestResolveBlockedModsManualAndSkip(t *testing.T) {
	blocked := []BlockedCFMod{{
		ProjectID: 1, FileID: 2, FileName: "blocked.jar",
		WebsiteURL: "https://www.curseforge.com/minecraft/mc-mods/1/download/2",
		Required:   true,
	}}
	base := []importTask{{relPath: "mods/ok.jar", url: "https://example.com/ok.jar"}}

	for _, decision := range []BlockedModsDecision{BlockedDecisionManual, BlockedDecisionSkip} {
		handler := func(_ context.Context, _ []BlockedCFMod, _ bool) (BlockedModsDecision, error) { return decision, nil }
		tasks, _, err := resolveBlockedMods(context.Background(), handler, false, blocked, append([]importTask{}, base...), nil)
		if err != nil {
			t.Fatalf("decision %s: не очікували помилки, отримали: %v", decision, err)
		}
		if len(tasks) != len(base) {
			t.Fatalf("decision %s: задачі мали лишитись без змін (%d), отримали %d", decision, len(base), len(tasks))
		}
	}
}

// TestResolveBlockedModsNilHandlerRequired — без хендлера (мовчазний Modrinth
// fallback) обов'язковий заблокований мод без знайденого дубліката валить
// імпорт з помилкою, що містить пряме посилання для ручного скачування.
func TestResolveBlockedModsNilHandlerRequired(t *testing.T) {
	blocked := []BlockedCFMod{{
		FileName:   "required.jar",
		SHA1:       "abc123",
		WebsiteURL: "https://www.curseforge.com/minecraft/mc-mods/99/download/100",
		Required:   true,
	}}
	_, _, err := resolveBlockedMods(context.Background(), nil, false, blocked, nil, nil)
	if err == nil {
		t.Fatal("обов'язковий заблокований мод без Modrinth-дубліката мав дати помилку")
	}
	if !strings.Contains(err.Error(), "https://www.curseforge.com/minecraft/mc-mods/99/download/100") {
		t.Fatalf("помилка мала містити посилання для ручного скачування: %v", err)
	}
}

// TestResolveBlockedModsNilHandlerOptionalSkipped — опційний заблокований мод
// без дубліката тихо пропускається (імпорт не падає).
func TestResolveBlockedModsNilHandlerOptionalSkipped(t *testing.T) {
	blocked := []BlockedCFMod{{FileName: "optional.jar", SHA1: "abc123", Required: false}}
	tasks, _, err := resolveBlockedMods(context.Background(), nil, false, blocked, nil, nil)
	if err != nil {
		t.Fatalf("опційний заблокований мод мав тихо пропуститись: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("задач не мало бути, отримали %d", len(tasks))
	}
}

// TestResolveBlockedModsAutoModrinth — з useModrinthForBlocked=true першого
// діалогу НЕ має бути взагалі: Modrinth fallback іде одразу (мовчки), а
// діалог з'являється лише ДРУГИМ раундом для модів, яких на Modrinth не
// знайшлось — і саме з allowModrinth=false (лише ручне встановлення).
func TestResolveBlockedModsAutoModrinth(t *testing.T) {
	oldResolver := modrinthResolver
	modrinthResolver = func(ctx context.Context, sha1hex string) (*ModrinthMirror, error) {
		if sha1hex == "found" {
			return &ModrinthMirror{URL: "https://cdn.modrinth.com/found.jar", Filename: "found.jar"}, nil
		}
		return nil, nil
	}
	defer func() { modrinthResolver = oldResolver }()

	blocked := []BlockedCFMod{
		{FileName: "found.jar", SHA1: "found", Required: true},
		{FileName: "missing.jar", SHA1: "missing", WebsiteURL: "https://www.curseforge.com/minecraft/mc-mods/1/download/2", Required: true},
	}
	calls := 0
	handler := func(_ context.Context, mods []BlockedCFMod, allowModrinth bool) (BlockedModsDecision, error) {
		calls++
		// Єдиний виклик — ДРУГИЙ раунд: без Modrinth, лише ті, що не знайшлись.
		if allowModrinth {
			t.Fatalf("першого діалогу (allowModrinth=true) не мало бути при авто-Modrinth")
		}
		if len(mods) != 1 || mods[0].FileName != "missing.jar" {
			t.Fatalf("у другому раунді мав бути лише незнайдений мод, отримали %+v", mods)
		}
		return BlockedDecisionManual, nil
	}
	tasks, _, err := resolveBlockedMods(context.Background(), handler, true, blocked, nil, nil)
	if err != nil {
		t.Fatalf("не очікували помилки при manual: %v", err)
	}
	if calls != 1 {
		t.Fatalf("очікували рівно 1 виклик діалогу (другий раунд), отримали %d", calls)
	}
	if len(tasks) != 1 || tasks[0].relPath != "mods/found.jar" {
		t.Fatalf("знайдений на Modrinth мод мав додатись у задачі: %+v", tasks)
	}
}

// TestResolveBlockedModsSecondRoundManualOnly — коли користувач у першому
// діалозі обрав «modrinth», а частина модів на Modrinth не знайшлась, діалог
// має показатись ВДРУГЕ для незнайдених з allowModrinth=false (без кнопки
// Modrinth), а «manual» у другому раунді продовжує імпорт без них.
func TestResolveBlockedModsSecondRoundManualOnly(t *testing.T) {
	oldResolver := modrinthResolver
	modrinthResolver = func(ctx context.Context, sha1hex string) (*ModrinthMirror, error) {
		return nil, nil // нічого не знаходиться
	}
	defer func() { modrinthResolver = oldResolver }()

	blocked := []BlockedCFMod{{FileName: "missing.jar", SHA1: "missing", Required: true}}
	calls := 0
	handler := func(_ context.Context, _ []BlockedCFMod, allowModrinth bool) (BlockedModsDecision, error) {
		calls++
		if calls == 1 {
			if !allowModrinth {
				t.Fatal("перший діалог мав дозволяти Modrinth")
			}
			return BlockedDecisionModrinth, nil
		}
		if allowModrinth {
			t.Fatal("другий діалог НЕ мав пропонувати Modrinth (вже спробований)")
		}
		return BlockedDecisionManual, nil
	}
	tasks, _, err := resolveBlockedMods(context.Background(), handler, false, blocked, nil, nil)
	if err != nil {
		t.Fatalf("manual у другому раунді не мав дати помилку: %v", err)
	}
	if calls != 2 {
		t.Fatalf("очікували 2 виклики діалогу, отримали %d", calls)
	}
	if len(tasks) != 0 {
		t.Fatalf("задач не мало бути (manual = без заблокованих): %+v", tasks)
	}
}

// TestResolveBlockedModsSecondRoundCancel — «скасувати» у ДРУГОМУ раунді
// (після невдалого Modrinth) скасовує весь імпорт зі зрозумілою помилкою.
func TestResolveBlockedModsSecondRoundCancel(t *testing.T) {
	oldResolver := modrinthResolver
	modrinthResolver = func(ctx context.Context, sha1hex string) (*ModrinthMirror, error) {
		return nil, nil
	}
	defer func() { modrinthResolver = oldResolver }()

	blocked := []BlockedCFMod{{FileName: "missing.jar", SHA1: "missing", Required: true}}
	handler := func(_ context.Context, _ []BlockedCFMod, allowModrinth bool) (BlockedModsDecision, error) {
		if allowModrinth {
			return BlockedDecisionModrinth, nil
		}
		return BlockedDecisionCancel, nil
	}
	_, _, err := resolveBlockedMods(context.Background(), handler, false, blocked, nil, nil)
	if err != nil {
		t.Fatalf("«скасувати» у другому раунді мало повернути nil, отримано: %v", err)
	}
}
