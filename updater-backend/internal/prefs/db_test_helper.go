package prefs

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// seedDB створює мінімальну базу зі схемою лаунчера (фолбек-тести).
func seedDB(t *testing.T, dir string, settingsJSON string) {
	t.Helper()
	path := filepath.Join(dir, "shaurma.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE settings (
		id    INTEGER PRIMARY KEY CHECK (id = 1),
		value TEXT NOT NULL
	)`); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO settings (id, value) VALUES (1, ?)`, settingsJSON); err != nil {
		t.Fatalf("insert: %v", err)
	}
}
