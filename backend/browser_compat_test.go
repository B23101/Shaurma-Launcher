package main

import (
	"testing"

	"shaurma-launcher-backend/internal/custompack"
)

// TestGetBrowserCompatiblePacksUnconfirmedOnCurseForgeError покриває баг
// «list версій CurseForge не завантажується → діалог встановлення каже
// "немає підтримуваної збірки" навіть коли збірка технічно підходить»
// (напр. мод fabric 1.20.1 + збірка fabric 1.20.1).
//
// Причина: GetBrowserCompatiblePacks трактував ПОМИЛКУ ЗАПИТУ
// (projectHasVersionFor повернув err != nil — тут: немає CurseForge
// API-ключа, так само повелась би мережева помилка/rate limit) так само,
// як ПІДТВЕРДЖЕНУ несумісність (ok=false, err=nil) — обидва випадки
// просто continue, і збірка мовчки зникала зі списку. Користувач бачив
// порожній діалог і не міг зрозуміти, що причина — не в самому моді.
//
// Очікувана поведінка (fail-open, як в browserEntryCompatibility поруч):
// коли перевірку не вдалось виконати, збірка ЛИШАЄТЬСЯ у списку з
// прапорцем Unconfirmed=true, а не зникає.
func TestGetBrowserCompatiblePacksUnconfirmedOnCurseForgeError(t *testing.T) {
	// Гарантуємо відсутність CurseForge API-ключа (детермінована помилка
	// запиту без потреби мережі/httptest).
	custompack.SetUserCurseForgeAPIKey("")
	t.Cleanup(func() { custompack.SetUserCurseForgeAPIKey("") })

	dir := t.TempDir()
	a, _ := newTestApp(t, dir, "fabric-pack", "1.20.1", struct{ Loader, Version string }{"fabric", "0.15.11"})

	packs := a.GetBrowserCompatiblePacks("curseforge", "123456", "mod", "mods", "")
	if len(packs) != 1 {
		t.Fatalf("GetBrowserCompatiblePacks() повернув %d збірок, хотіли 1 (непідтверджену), список: %+v", len(packs), packs)
	}
	if !packs[0].Unconfirmed {
		t.Fatalf("packs[0].Unconfirmed = false, хотіли true (перевірку не вдалось виконати — немає API-ключа)")
	}
	if packs[0].PackID != "fabric-pack" {
		t.Fatalf("packs[0].PackID = %q, хотіли fabric-pack", packs[0].PackID)
	}
}

// TestGetBrowserCompatiblePacksSkipsVanillaForMods перевіряє, що це
// виправлення не зламало сусідню логіку: ванільна збірка (без лоадера)
// для типу "mod" і далі виключається одразу, ще до мережевого запиту —
// сюди Unconfirmed узагалі не повинен потрапити.
func TestGetBrowserCompatiblePacksSkipsVanillaForMods(t *testing.T) {
	custompack.SetUserCurseForgeAPIKey("")
	t.Cleanup(func() { custompack.SetUserCurseForgeAPIKey("") })

	dir := t.TempDir()
	a, _ := newTestApp(t, dir, "vanilla-pack", "1.20.1")

	packs := a.GetBrowserCompatiblePacks("curseforge", "123456", "mod", "mods", "")
	if len(packs) != 0 {
		t.Fatalf("GetBrowserCompatiblePacks() для ванільної збірки+мода = %+v, хотіли порожній список", packs)
	}
}
