package servers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// SanitizeServerFolder має ЗБЕРІГАТИ кирилицю та пробіли — це головна
// причина, чому це не slugify: раніше «Мій Сервер» ставало «server», і всі
// кириличні назви злипались в одну теку.
func TestSanitizeServerFolderKeepsCyrillicAndSpaces(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Мій Сервер", "Мій Сервер"},
		{"Сервер з друзями", "Сервер з друзями"},
		{"Мій   Сервер", "Мій Сервер"},          // подвійні пробіли згортаються
		{"  Хвіст  ", "Хвіст"},                  // обрізаємо краї
		{"Мій/Сервер", "МійСервер"},             // слеш — заборонено у Windows
		{"a\\b:c*d?e\"f<g>h|i", "abcdefghi"},    // решта заборонених
		{"test.", "test"},                       // кінцева крапка заборонена
		{"", "server"},                          // порожньо → дефолт
		{"   ", "server"},
		{"<||||>", "server"},
		{"Файл.txt", "Файл.txt"},                 // внутрішня крапка — ок
		{"Мій Сервер (v2)", "Мій Сервер (v2)"},   // дужки/пробіли — ок
		{"CON", "srv-CON"},                       // службове ім'я Windows
		{"nul", "srv-nul"},
		{"com1", "srv-com1"},
		{"Збірка-2026_01", "Збірка-2026_01"},
		{"сервер🚀тест", "сервер-тест"},          // емодзі → дефіс
	}
	for _, c := range cases {
		if got := SanitizeServerFolder(c.in); got != c.want {
			t.Errorf("SanitizeServerFolder(%q) = %q, хотіли %q", c.in, got, c.want)
		}
	}
}

// Довжина: Windows обмежує ім'я файлу/теки 255 символами, а лаунчер ще
// додає всередину .shaurma/server.json — довгу назву треба безпечно зрізати,
// інакше CreateServer падає з незрозумілою помилкою ОС.
func TestSanitizeServerFolderTrimsLongNames(t *testing.T) {
	long := strings.Repeat("я", 400)
	got := SanitizeServerFolder(long)
	if len([]rune(got)) != maxServerFolderRunes {
		t.Fatalf("довжина = %d рун, хотіли %d", len([]rune(got)), maxServerFolderRunes)
	}
}

// CheckServerFolder: вільний ID → Free, зайнятий → підказка про конфлікт.
func TestCheckServerFolder(t *testing.T) {
	dataDir := t.TempDir()
	api := NewAPI(dataDir, nil, nil)

	res := api.CheckServerFolder("Мій Сервер")
	if res.Folder != "Мій Сервер" || !res.Free {
		t.Fatalf("вільний ID розпізнано невірно: %+v", res)
	}

	// Створюємо теку сервера з server.json — ID стає зайнятим.
	dir := filepath.Join(dataDir, "servers", "Мій Сервер")
	if err := os.MkdirAll(filepath.Join(dir, ".shaurma"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".shaurma", "server.json"), []byte(`{"id":"abc"}`), 0644); err != nil {
		t.Fatal(err)
	}

	res = api.CheckServerFolder("Мій Сервер")
	if res.Free {
		t.Fatalf("зайнятий ID визнано вільним: %+v", res)
	}
	if !strings.Contains(res.Reason, "зайнятий") {
		t.Fatalf("немає пояснення про зайнятість: %q", res.Reason)
	}
}

// RenameServerFolder має фізично перейменувати теку і оновити Dir у
// server.json — інакше всі похідні шляхи (світи, моди, properties) ведуть
// у неіснуючу теку.
func TestRenameServerFolder(t *testing.T) {
	dataDir := t.TempDir()
	api := NewAPI(dataDir, nil, nil)

	oldDir := filepath.Join(dataDir, "servers", "Старий ID")
	srv := &Server{ID: "srv1", Name: "Тест", Dir: oldDir, Core: CorePaper, MCVersion: "1.20.1"}
	if err := api.store.Save(srv); err != nil {
		t.Fatal(err)
	}

	view, err := api.RenameServerFolder("srv1", "Новий ID")
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	newDir := filepath.Join(dataDir, "servers", "Новий ID")
	if view.Dir != newDir {
		t.Fatalf("Dir = %q, хотіли %q", view.Dir, newDir)
	}
	if _, err := os.Stat(newDir); err != nil {
		t.Fatalf("нової теки немає на диску: %v", err)
	}
	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Fatal("стара тека лишилась на диску")
	}

	// Store сканує теки: після rename сервер має знаходитись за новим Dir.
	reloaded := NewStore(dataDir)
	got, ok := reloaded.GetNoErr("srv1")
	if !ok {
		t.Fatal("сервер не знайдено після перескановування")
	}
	if got.Dir != newDir {
		t.Fatalf("після перезавантаження Dir = %q, хотіли %q", got.Dir, newDir)
	}

	// Зайнятий ID — відмова, а не злиття двох серверів в одну теку.
	other := &Server{ID: "srv2", Name: "Другий", Dir: filepath.Join(dataDir, "servers", "Захаровано")}
	if err := api.store.Save(other); err != nil {
		t.Fatal(err)
	}
	if _, err := api.RenameServerFolder("srv1", "Захаровано"); err == nil {
		t.Fatal("rename у зайнятий ID мав провалитись")
	}

	// Порожній ID — відмова.
	if _, err := api.RenameServerFolder("srv1", "   "); err == nil {
		t.Fatal("порожній ID мав провалитись")
	}
}

// CreateServer з власним ID: тека має називатись саме так (з кирилицею), а
// повторний ID — відхилятись.
func TestCreateServerUsesCustomFolderID(t *testing.T) {
	dataDir := t.TempDir()
	api := NewAPI(dataDir, nil, nil)

	view, err := api.CreateServer(NewServerRequest{
		Core: CorePaper, MCVersion: "1.20.1", Name: "Сервер Друзів",
		ID: "Мій Сервер", AcceptEULA: true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	want := filepath.Join(dataDir, "servers", "Мій Сервер")
	if view.Dir != want {
		t.Fatalf("Dir = %q, хотіли %q", view.Dir, want)
	}
	if _, err := os.Stat(filepath.Join(want, ".shaurma", "server.json")); err != nil {
		t.Fatalf("server.json не створено: %v", err)
	}

	if _, err := api.CreateServer(NewServerRequest{
		Core: CorePaper, MCVersion: "1.20.1", Name: "Інший", ID: "Мій Сервер", AcceptEULA: true,
	}); err == nil {
		t.Fatal("створення з зайнятим ID мало провалитись")
	}
}

// Без власного ID тека виводиться з назви і зберігає кирилицю (регресія до
// slugify, який робив із будь-якої кириличної назви «server»).
func TestCreateServerDerivesFolderFromCyrillicName(t *testing.T) {
	dataDir := t.TempDir()
	api := NewAPI(dataDir, nil, nil)

	view, err := api.CreateServer(NewServerRequest{
		Core: CorePaper, MCVersion: "1.20.1", Name: "Сервер Друзів", AcceptEULA: true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if filepath.Base(view.Dir) != "Сервер Друзів" {
		t.Fatalf("тека = %q, хотіли «Сервер Друзів»", filepath.Base(view.Dir))
	}
}
