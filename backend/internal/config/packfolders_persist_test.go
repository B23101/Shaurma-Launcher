package config

import (
	"testing"

	"shaurma-launcher-backend/internal/model"
)

// TestPackFolderDeletePersists перевіряє, що видалення папки кастомних
// збірок реально зберігається в SQLite і не «повертається» після
// перезапуску (симуляція: новий Manager.Load() читає БД наново).
func TestPackFolderDeletePersists(t *testing.T) {
	dir := t.TempDir()

	m := NewManager(dir)
	if err := m.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer func() { _ = m.st.Close() }()

	// 1. Створюємо папку.
	s := m.GetSettings()
	s.PackFolders = []model.PackFolder{{
		ID: "fld_test1", Name: "Тест папка", Color: "#ff8a00", PackIds: []string{"pack-1"},
	}}
	if err := m.UpdateSettings(s); err != nil {
		t.Fatalf("UpdateSettings (create): %v", err)
	}
	if got := m.GetSettings().PackFolders; len(got) != 1 {
		t.Fatalf("після створення очікувалось 1 папку, отримано %d: %+v", len(got), got)
	}

	// 2. Видаляємо папку (порожній список).
	s = m.GetSettings()
	s.PackFolders = []model.PackFolder{}
	if err := m.UpdateSettings(s); err != nil {
		t.Fatalf("UpdateSettings (delete): %v", err)
	}
	if got := m.GetSettings().PackFolders; len(got) != 0 {
		t.Fatalf("після видалення очікувалось 0 папок, отримано %d: %+v", len(got), got)
	}

	// 3. «Перезапуск»: новий менеджер на тій самій теці — читаємо з диска.
	m2 := NewManager(dir)
	if err := m2.Load(); err != nil {
		t.Fatalf("Load (restart): %v", err)
	}
	defer func() { _ = m2.st.Close() }()
	if got := m2.GetSettings().PackFolders; len(got) != 0 {
		t.Fatalf("ПІСЛЯ ПЕРЕЗАПУСКУ очікувалось 0 папок (видалення має зберегтись), отримано %d: %+v", len(got), got)
	}
}

// TestReorderAccountsPersists перевіряє, що зміна порядку акаунтів
// зберігається і лишається після «перезапуску».
func TestReorderAccountsPersists(t *testing.T) {
	dir := t.TempDir()

	m := NewManager(dir)
	if err := m.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer func() { _ = m.st.Close() }()

	// Додаємо три акаунти (AddAccount зберігає в БД).
	for _, a := range []model.Account{
		{ID: "acc-1", Username: "first"},
		{ID: "acc-2", Username: "second"},
		{ID: "acc-3", Username: "third"},
	} {
		if err := m.AddAccount(a); err != nil {
			t.Fatalf("AddAccount(%s): %v", a.ID, err)
		}
	}
	got := m.GetAccounts()
	if len(got) != 3 || got[0].ID != "acc-1" {
		t.Fatalf("очікувалось [acc-1 acc-2 acc-3], отримано: %+v", ids(got))
	}

	// Переставляємо: third, first, second.
	if err := m.ReorderAccounts([]string{"acc-3", "acc-1", "acc-2"}); err != nil {
		t.Fatalf("ReorderAccounts: %v", err)
	}
	got = m.GetAccounts()
	if len(got) != 3 || got[0].ID != "acc-3" || got[2].ID != "acc-2" {
		t.Fatalf("після реордера очікувалось [acc-3 acc-1 acc-2], отримано: %+v", ids(got))
	}

	// «Перезапуск»: порядок має зберегтись.
	m2 := NewManager(dir)
	if err := m2.Load(); err != nil {
		t.Fatalf("Load (restart): %v", err)
	}
	defer func() { _ = m2.st.Close() }()
	got = m2.GetAccounts()
	if len(got) != 3 || got[0].ID != "acc-3" || got[2].ID != "acc-2" {
		t.Fatalf("ПІСЛЯ ПЕРЕЗАПУСКУ очікувалось [acc-3 acc-1 acc-2], отримано: %+v", ids(got))
	}
}

func ids(accs []model.Account) []string {
	out := make([]string, len(accs))
	for i, a := range accs {
		out[i] = a.ID
	}
	return out
}
