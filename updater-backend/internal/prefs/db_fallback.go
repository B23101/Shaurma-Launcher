package prefs

import (
	"database/sql"
	_ "modernc.org/sqlite"
)

// readSettingsFromDB читає settings зі старого shaurma.db (фолбек).
func readSettingsFromDB(dbPath string) (string, bool) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return "", false
	}
	defer db.Close()

	// busy_timeout — щоб читання не падало з SQLITE_BUSY, якщо лаунчер
	// саме щось пише у WAL-режимі.
	if _, err := db.Exec(`PRAGMA busy_timeout = 3000`); err != nil {
		return "", false
	}
	var value string
	if err := db.QueryRow(`SELECT value FROM settings WHERE id = 1`).Scan(&value); err != nil {
		return "", false
	}
	return value, true
}
