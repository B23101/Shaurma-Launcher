package main

import (
	"strings"
	"testing"

	"shaurma-launcher-backend/internal/model"
)

func repairOpt(id, date, typ string, current bool) model.ModVersionOption {
	return model.ModVersionOption{ID: id, VersionNumber: id, DatePublished: date, VersionType: typ,
		URL: "https://example.invalid/" + id + ".jar", Filename: id + ".jar", IsCurrent: current}
}

func TestPickRepairModVersion(t *testing.T) {
	// Найновіша стабільна версія новіша за поточну.
	got, why := pickRepairModVersion([]model.ModVersionOption{
		repairOpt("3.0-beta", "2026-03-01", "beta", false),
		repairOpt("2.0", "2026-02-01", "release", false),
		repairOpt("1.0", "2026-01-01", "release", true),
	}, true)
	if why != "" || got.ID != "2.0" {
		t.Fatalf("want 2.0, got %q (%s)", got.ID, why)
	}
	// Поточна вже найновіша — відмова з поясненням.
	if _, why := pickRepairModVersion([]model.ModVersionOption{
		repairOpt("2.0", "2026-02-01", "release", true),
		repairOpt("1.0", "2026-01-01", "release", false),
	}, true); !strings.Contains(why, "найновішу") {
		t.Fatalf("expected latest-already, got %q", why)
	}
	// Лише beta/alpha — беремо їх як запасний варіант.
	if got, why := pickRepairModVersion([]model.ModVersionOption{
		repairOpt("2.0-beta", "2026-02-01", "beta", false),
		repairOpt("1.0", "2026-01-01", "release", true),
	}, true); why != "" || got.ID != "2.0-beta" {
		t.Fatalf("beta fallback: %q %s", got.ID, why)
	}
	// Фолбек CurseForge (не strict) автоматично не ставимо.
	if _, why := pickRepairModVersion([]model.ModVersionOption{repairOpt("9.9", "2026-09-01", "release", false)}, false); why == "" {
		t.Fatal("non-strict list must be refused")
	}
	// Порожній список.
	if _, why := pickRepairModVersion(nil, true); why == "" {
		t.Fatal("empty list must be refused")
	}
	// Опція без посилання ігнорується.
	bad := repairOpt("5.0", "2026-05-01", "release", false)
	bad.URL = ""
	if _, why := pickRepairModVersion([]model.ModVersionOption{bad}, true); why == "" {
		t.Fatal("option without URL must be skipped")
	}
}
