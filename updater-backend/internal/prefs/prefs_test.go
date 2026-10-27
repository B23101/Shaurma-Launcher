package prefs

import (
	"os"
	"path/filepath"
	"encoding/json"
	"testing"
)

// seedJSON створює shaurma.json з налаштуваннями.
// Settings — це JSON-рядок (не об'єкт), як у реальному shaurma.json.
func seedJSON(t *testing.T, settingsJSON string) string {
	t.Helper()
	dir := t.TempDir()
	// settings має бути JSON-рядком (рядок у JSON), тому кодуємо його.
	encoded, _ := json.Marshal(settingsJSON)
	data := []byte(`{"settings":` + string(encoded) + `}`)
	if err := os.WriteFile(filepath.Join(dir, JSONFileName), data, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return dir
}

func TestLoadFromJSON(t *testing.T) {
	dir := seedJSON(t, `{"autoUpdate":false,"updateChannel":"beta"}`)
	p := Load(dir)
	if p.AutoUpdate {
		t.Errorf("AutoUpdate = true, want false")
	}
	if p.UpdateChannel != "beta" {
		t.Errorf("UpdateChannel = %q, want beta", p.UpdateChannel)
	}
}

func TestLoadDefaultsWhenMissingField(t *testing.T) {
	dir := seedJSON(t, `{"autoUpdate":false}`)
	p := Load(dir)
	if p.AutoUpdate {
		t.Errorf("AutoUpdate = true, want false")
	}
	if p.UpdateChannel != "stable" {
		t.Errorf("UpdateChannel = %q, want stable (дефолт)", p.UpdateChannel)
	}
}

func TestLoadDefaultsWhenNoJSON(t *testing.T) {
	p := Load(t.TempDir())
	if !p.AutoUpdate || p.UpdateChannel != "stable" {
		t.Errorf("без JSON = %+v, want Defaults", p)
	}
}

func TestLoadDefaultsWhenBadJSON(t *testing.T) {
	dir := seedJSON(t, `{not json`)
	p := Load(dir)
	if !p.AutoUpdate || p.UpdateChannel != "stable" {
		t.Errorf("з битим JSON = %+v, want Defaults", p)
	}
}

func TestRawReadsLanguage(t *testing.T) {
	dir := seedJSON(t, `{"language":"en","accent":"green","theme":"light"}`)
	raw, ok := Raw(dir)
	if !ok {
		t.Fatalf("Raw: не знайдено")
	}
	if raw != `{"language":"en","accent":"green","theme":"light"}` {
		t.Errorf("Raw = %q", raw)
	}
}

func TestLoadFromOldDB(t *testing.T) {
	// Тест фолбеку: якщо shaurma.json немає, а shaurma.db є — читає з БД.
	dir := t.TempDir()
	seedDB(t, dir, `{"autoUpdate":false,"updateChannel":"beta"}`)
	p := Load(dir)
	if p.AutoUpdate {
		t.Errorf("AutoUpdate = true, want false (DB fallback)")
	}
	if p.UpdateChannel != "beta" {
		t.Errorf("UpdateChannel = %q, want beta (DB fallback)", p.UpdateChannel)
	}
}

func TestRawFromOldDB(t *testing.T) {
	dir := t.TempDir()
	seedDB(t, dir, `{"language":"uk","theme":"dark"}`)
	raw, ok := Raw(dir)
	if !ok {
		t.Fatalf("Raw з DB: не знайдено")
	}
	if raw != `{"language":"uk","theme":"dark"}` {
		t.Errorf("Raw з DB = %q", raw)
	}
}
