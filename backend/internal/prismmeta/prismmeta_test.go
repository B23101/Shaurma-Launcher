package prismmeta

import (
	"testing"
)

// sampleIndex — тестовий індекс у форматі meta.prismlauncher.org (тільки
// поля, які використовує пакет). Відтворює реальні сценарії: Forge 1.20.1
// з recommended-прапорцем, NeoForge 1.20.1 (47.1.x, рекомендованих немає),
// пре-релізи, різні часи випуску.
func sampleIndex() []IndexEntry {
	return []IndexEntry{
		{Version: "47.4.10", Recommended: true, ReleaseTime: "2025-08-01T00:00:00Z",
			Requires: []Require{{UID: "net.minecraft", Equals: "1.20.1"}}},
		{Version: "47.4.9", ReleaseTime: "2025-07-01T00:00:00Z",
			Requires: []Require{{UID: "net.minecraft", Equals: "1.20.1"}}},
		{Version: "47.4.10-beta.1", ReleaseTime: "2025-06-01T00:00:00Z",
			Requires: []Require{{UID: "net.minecraft", Equals: "1.20.1"}}},
		// інша MC-версія — фільтр має її викинути
		{Version: "48.0.1", Recommended: true, ReleaseTime: "2025-09-01T00:00:00Z",
			Requires: []Require{{UID: "net.minecraft", Equals: "1.20.2"}}},
		// NeoForge 1.20.1: версії 47.1.x БЕЗ префікса "20.1." — саме цей
		// кейс ламала стара префіксна хевристика
		{Version: "47.1.136", ReleaseTime: "2024-07-15T00:00:00Z",
			Requires: []Require{{UID: "net.minecraft", Equals: "1.20.1"}}},
		{Version: "47.1.0", ReleaseTime: "2023-07-15T00:00:00Z",
			Requires: []Require{{UID: "net.minecraft", Equals: "1.20.1"}}},
	}
}

func TestFilterByMinecraft(t *testing.T) {
	got := FilterByMinecraft(sampleIndex(), "1.20.1")
	want := []string{"47.4.10", "47.4.9", "47.4.10-beta.1", "47.1.136", "47.1.0"}
	if len(got) != len(want) {
		t.Fatalf("очікували %d версій для 1.20.1, отримали %d: %v", len(want), len(got), versionsOf(got))
	}
	for i, v := range want {
		if got[i].Version != v {
			t.Errorf("позиція %d: очікували %s, отримали %s", i, v, got[i].Version)
		}
	}

	// MC-версія без жодної сумісної версії
	if got := FilterByMinecraft(sampleIndex(), "99.99.99"); len(got) != 0 {
		t.Errorf("для неіснуючої MC-версії список має бути порожнім, отримали %v", versionsOf(got))
	}
}

func TestRecommendedVersion(t *testing.T) {
	entries := sampleIndex()

	// Forge-кейс: recommended-прапорець є → вертає його
	forge := FilterByMinecraft(entries, "1.20.1")
	rec := RecommendedVersion(forge)
	if rec != "47.4.10" {
		t.Errorf("Forge 1.20.1: очікували рекомендовану 47.4.10, отримали %q", rec)
	}

	// NeoForge-кейс: recommended-прапорців немає → найновіша за часом.
	// Індекс завжди приходить відсортованим (SortByReleaseTimeDesc), тому
	// сортуємо і тут — як у реальному потоці Client.Index().
	neo := FilterByMinecraft([]IndexEntry{
		{Version: "47.1.0", ReleaseTime: "2023-07-15T00:00:00Z",
			Requires: []Require{{UID: "net.minecraft", Equals: "1.20.1"}}},
		{Version: "47.1.136", ReleaseTime: "2024-07-15T00:00:00Z",
			Requires: []Require{{UID: "net.minecraft", Equals: "1.20.1"}}},
	}, "1.20.1")
	SortByReleaseTimeDesc(neo)
	if rec := RecommendedVersion(neo); rec != "47.1.136" {
		t.Errorf("NeoForge: очікували найновішу 47.1.136, отримали %q", rec)
	}

	if rec := RecommendedVersion(nil); rec != "" {
		t.Errorf("порожній список → порожня рекомендована, отримали %q", rec)
	}
}

func TestSortByReleaseTimeDesc(t *testing.T) {
	entries := []IndexEntry{
		{Version: "old", ReleaseTime: "2020-01-01T00:00:00Z"},
		{Version: "new", ReleaseTime: "2024-01-01T00:00:00Z"},
		{Version: "mid", ReleaseTime: "2022-01-01T00:00:00Z"},
		// без часу — має йти в кінець (нульовий час) і впорядкуватись FlexVer
		{Version: "z-final"},
		{Version: "a-final"},
	}
	SortByReleaseTimeDesc(entries)
	want := []string{"new", "mid", "old", "z-final", "a-final"}
	for i, v := range want {
		if entries[i].Version != v {
			t.Errorf("позиція %d: очікували %s, отримали %s", i, v, entries[i].Version)
		}
	}
}

func TestIsStableVersion(t *testing.T) {
	stable := []string{"47.4.10", "21.1.233", "0.16.5"}
	for _, v := range stable {
		if !IsStableVersion(v) {
			t.Errorf("%s має бути стабільною", v)
		}
	}
	unstable := []string{"47.4.10-beta.1", "21.1.0-alpha", "1.0.0-rc.2", "1.20 Pre-Release 1", "1.20.5-pre1"}
	for _, v := range unstable {
		if IsStableVersion(v) {
			t.Errorf("%s має бути нестабільною (пре-реліз)", v)
		}
	}
}

func versionsOf(entries []IndexEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Version
	}
	return out
}
