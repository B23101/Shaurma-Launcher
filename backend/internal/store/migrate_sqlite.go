package store

import (
	"database/sql"
	"encoding/json"
	"time"

	_ "modernc.org/sqlite"

	"shaurma-launcher-backend/internal/model"
)

// sqliteStr читає один рядок з SQLite.
func sqliteStr(db *sql.DB, query string) (string, bool, error) {
	var raw string
	err := db.QueryRow(query).Scan(&raw)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	return raw, true, err
}

// sqliteStrMap читає map[key]string з SQLite (2 колонки).
func sqliteStrMap(db *sql.DB, query string) (map[string]string, error) {
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// sqliteAccounts читає акаунти з SQLite.
func sqliteAccounts(db *sql.DB) ([]model.Account, error) {
	rows, err := db.Query(`SELECT id, username, type, access_token, ms_access_token,
		refresh_token, expires_at, uuid, is_licensed FROM accounts ORDER BY rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var accounts []model.Account
	for rows.Next() {
		var a model.Account
		var licensed int
		if err := rows.Scan(&a.ID, &a.Username, &a.Type, &a.AccessToken,
			&a.MsAccessToken, &a.RefreshToken, &a.ExpiresAt, &a.UUID, &licensed); err != nil {
			return nil, err
		}
		a.IsLicensed = licensed != 0
		accounts = append(accounts, a)
	}
	return accounts, rows.Err()
}

// sqlitePlaytime читає playtime з SQLite.
func sqlitePlaytime(db *sql.DB) (map[string]int64, error) {
	rows, err := db.Query(`SELECT instance_id, seconds FROM playtime`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	pt := map[string]int64{}
	for rows.Next() {
		var id string
		var sec int64
		if err := rows.Scan(&id, &sec); err != nil {
			return nil, err
		}
		pt[id] = sec
	}
	return pt, rows.Err()
}

// sqliteSessions читає playtime sessions з SQLite.
func sqliteSessions(db *sql.DB) ([]PlaytimeSession, error) {
	rows, err := db.Query(`SELECT instance_id, account_id, account_name, started_at, seconds FROM playtime_sessions ORDER BY started_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PlaytimeSession
	for rows.Next() {
		var rec PlaytimeSession
		var started int64
		if err := rows.Scan(&rec.InstanceID, &rec.AccountID, &rec.AccountName, &started, &rec.Seconds); err != nil {
			return nil, err
		}
		rec.StartedAt = time.Unix(started, 0)
		out = append(out, rec)
	}
	return out, rows.Err()
}

// sqliteInstanceConfigs читає instance configs з SQLite.
func sqliteInstanceConfigs(db *sql.DB) (map[string]model.InstanceConfig, error) {
	rows, err := db.Query(`SELECT id, config FROM instance_configs`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	all := map[string]model.InstanceConfig{}
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		var cfg model.InstanceConfig
		if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
			continue
		}
		all[id] = cfg
	}
	return all, rows.Err()
}

// sqliteBuildsRegistry читає builds registry з SQLite.
func sqliteBuildsRegistry(db *sql.DB) ([]RegistryEntry, error) {
	rows, err := db.Query(`SELECT id, kind, installed_version, installed_at FROM builds_registry`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RegistryEntry
	for rows.Next() {
		var e RegistryEntry
		if err := rows.Scan(&e.ID, &e.Kind, &e.InstalledVersion, &e.InstalledAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// sqliteGalleryMeta читає gallery meta з SQLite.
func sqliteGalleryMeta(db *sql.DB) ([]GalleryMetaEntry, error) {
	rows, err := db.Query(`SELECT pack_id, file_name, data FROM gallery_meta`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GalleryMetaEntry
	for rows.Next() {
		var e GalleryMetaEntry
		if err := rows.Scan(&e.PackID, &e.FileName, &e.Data); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// sqliteAICache читає AI cache з SQLite.
func sqliteAICache(db *sql.DB) (map[string]aiCacheEntry, error) {
	rows, err := db.Query(`SELECT cache_key, kind, data, created_at FROM browser_ai_cache`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]aiCacheEntry{}
	for rows.Next() {
		var e aiCacheEntry
		var created int64
		if err := rows.Scan(&e.CacheKey, &e.Kind, &e.Data, &created); err != nil {
			return nil, err
		}
		e.CreatedAt = time.Unix(created, 0)
		out[e.CacheKey] = e
	}
	return out, rows.Err()
}
