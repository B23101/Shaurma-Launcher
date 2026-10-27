package main

import (
	"testing"

	"shaurma-launcher-backend/internal/api"
	"shaurma-launcher-backend/internal/custompack"
)

// ── Баг 4: іконка/знахідка CurseForge у міграції модів ────────────────
// bestCurseForgeMatch — чиста функція відбору найкращого збігу пошуку CF
// (викликається findModOnCurseForge → resolveMigrationByName). Раніше
// CFSearchHit.Logo мав json:"logoUrl" — поле, якого CurseForge API v1
// ніколи не віддає, тож LogoURL у результатах міграції був завжди
// порожнім і CF-моди лишались без іконки. Тепер лого читається з
// вкладеного logo:{url}.

func TestBestCurseForgeMatchExactAndPrefix(t *testing.T) {
	hits := []custompack.CFSearchHit{
		{ID: 1, Slug: "jei", Name: "Just Enough Items (JEI)"},
		{ID: 2, Slug: "create", Name: "Create"},
		{ID: 3, Slug: "jei-integration-terms", Name: "JEI Integration"},
	}
	// Точний збіг за slug.
	if got := bestCurseForgeMatch(hits, "JEI"); got == nil || got.ID != 1 {
		t.Fatalf("очікували точний збіг за slug (id=1), отримали: %+v", got)
	}
	// Точний збіг за нормалізованою назвою.
	if got := bestCurseForgeMatch(hits, "Just Enough Items"); got == nil || got.ID != 1 {
		t.Fatalf("очікували збіг за назвою (id=1), отримали: %+v", got)
	}
	// Префіксний збіг (неточна назва з джерела).
	if got := bestCurseForgeMatch(hits, "Just En"); got == nil || got.ID != 1 {
		t.Fatalf("очікували префіксний збіг (id=1), отримали: %+v", got)
	}
	// Немає збігу взагалі.
	if got := bestCurseForgeMatch(hits, "Totally Unknown Mod"); got != nil {
		t.Fatalf("очікували nil для невідомого мода, отримали: %+v", got)
	}
}

func TestBestCurseForgeMatchKeepsNestedLogo(t *testing.T) {
	hits := []custompack.CFSearchHit{
		{ID: 42, Slug: "create-frontier", Name: "Create - Forge Frontier", Logo: struct {
			URL string `json:"url"`
		}{URL: "https://media.forgecdn.net/avatars/42.png"}},
	}
	got := bestCurseForgeMatch(hits, "create-frontier")
	if got == nil {
		t.Fatal("очікували збіг, отримали nil")
	}
	// resolveMigrationByName читає Logo.URL у item.IconURL — іконка має
	// прийти з вкладеного лого, а не бути порожньою.
	if got.Logo.URL == "" {
		t.Fatal("Logo.URL порожній — CF-мод у міграції лишиться без іконки")
	}
	if got.Logo.URL != "https://media.forgecdn.net/avatars/42.png" {
		t.Fatalf("неочікуваний Logo.URL: %s", got.Logo.URL)
	}
}

// TestMigrationStatusOrder перевіряє порядок сортування рядків звіту
// міграції: знайдені зверху, потім оновлення, потім вже встановлені,
// потім не знайдені — як ModMigrationScreen у старому лаунчері.
func TestMigrationStatusOrder(t *testing.T) {
	cases := []struct {
		status string
		want   int
	}{
		{migrationStatusFound, 0},
		{migrationStatusUpdate, 1},
		{migrationStatusAlready, 2},
		{migrationStatusNotFound, 3},
		{"unknown", 3},
		{"", 3},
	}
	for _, c := range cases {
		if got := migrationStatusOrder(c.status); got != c.want {
			t.Errorf("migrationStatusOrder(%q) = %d, want %d", c.status, got, c.want)
		}
	}
}

// TestFullVersionCompatible перевіряє сумісність версії з цільовою MC і
// лоадером: точний MC-збіг, перетин лоадерів, порожні списки (невідомо),
// несумісний лоадер.
func TestFullVersionCompatible(t *testing.T) {
	cases := []struct {
		name       string
		v          *api.FullVersion
		mc, loader string
		want       bool
	}{
		{"nil", nil, "1.20.1", "fabric", false},
		{"точний збіг MC+лоадер",
			&api.FullVersion{GameVersions: []string{"1.20.1", "1.20.2"}, Loaders: []string{"fabric", "quilt"}},
			"1.20.1", "fabric", true},
		{"перетин лоадерів (quilt у збірці fabric)",
			&api.FullVersion{GameVersions: []string{"1.20.1"}, Loaders: []string{"fabric"}},
			"1.20.1", "quilt", false},
		{"пусті лоадери = невідомо, MC збігається",
			&api.FullVersion{GameVersions: []string{"1.20.1"}, Loaders: nil},
			"1.20.1", "forge", true},
		{"MC не збігається",
			&api.FullVersion{GameVersions: []string{"1.19.4"}, Loaders: []string{"fabric"}},
			"1.20.1", "fabric", false},
		{"пусті game_versions = невідомо",
			&api.FullVersion{GameVersions: nil, Loaders: []string{"fabric"}},
			"1.20.1", "fabric", true},
		{"vanilla лоадер збірки не фільтрує",
			&api.FullVersion{GameVersions: []string{"1.20.1"}, Loaders: []string{"fabric"}},
			"1.20.1", "vanilla", true},
	}
	for _, c := range cases {
		if got := fullVersionCompatible(c.v, c.mc, c.loader); got != c.want {
			t.Errorf("%s: fullVersionCompatible(mc=%s, loader=%s) = %v, want %v",
				c.name, c.mc, c.loader, got, c.want)
		}
	}
}
