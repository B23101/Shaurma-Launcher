package p2p

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Регресійний тест причини «невідомий вузол»: кожна сесія мусить отримати
// НОВУ теку стану. Якщо хтось поверне фіксований шлях (як було раніше —
// <base>/host і <base>/guest), тест упаде: tsnet пред'являв би Headscale
// node key давно видаленої ephemeral-ноди, і нода не піднімалась.
func TestFreshNodeStateDirIsUniquePerSession(t *testing.T) {
	base := t.TempDir()

	first, err := freshNodeStateDir(base, "host")
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := freshNodeStateDir(base, "host")
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if first == second {
		t.Fatalf("дві сесії отримали ту саму теку стану: %s", first)
	}
	for _, dir := range []string{first, second} {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			t.Fatalf("тека стану %s не створена: %v", dir, err)
		}
	}

	// Теки host і guest не мають перетинатись.
	guest, err := freshNodeStateDir(base, "guest")
	if err != nil {
		t.Fatalf("guest: %v", err)
	}
	if filepath.Dir(guest) != base {
		t.Fatalf("тека guest має лежати в %s, а не в %s", base, filepath.Dir(guest))
	}
}

// Старі теки сесій не мають накопичуватись на диску: лишаємо не більше
// nodeStateKeep найновіших.
func TestFreshNodeStateDirPrunesOldSessions(t *testing.T) {
	base := t.TempDir()

	for i := 0; i < 5; i++ {
		if _, err := freshNodeStateDir(base, "host"); err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
		// Розводимо теки за часом модифікації, інакше prune не має за що
		// впорядковувати (усі створені в межах однієї мілісекунди).
		time.Sleep(10 * time.Millisecond)
	}

	entries, err := os.ReadDir(base)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	if len(dirs) > nodeStateKeep {
		t.Fatalf("лишилось %d тек стану, очікували не більше %d: %v", len(dirs), nodeStateKeep, dirs)
	}
}

func TestDropNodeStateDir(t *testing.T) {
	base := t.TempDir()
	dir, err := freshNodeStateDir(base, "host")
	if err != nil {
		t.Fatalf("fresh: %v", err)
	}
	dropNodeStateDir(dir)
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("тека стану не прибрана: %v", err)
	}
	dropNodeStateDir("") // порожній шлях не має панікувати
}

// Помилка Headscale має перетворюватись на підказку для гравця, а не
// показуватись як голий «node not found».
func TestRegistrationHint(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{errors.New("control: RegisterRequest failed: node not found"), true},
		{errors.New("invalid key: this key is not reusable"), true},
		{errors.New("node key expired"), true},
		{errors.New("dial tcp: i/o timeout"), false},
		{nil, false},
	}
	for _, c := range cases {
		if got := registrationHint(c.err) != ""; got != c.want {
			t.Fatalf("registrationHint(%v) = %v, очікували %v", c.err, got, c.want)
		}
	}
}
