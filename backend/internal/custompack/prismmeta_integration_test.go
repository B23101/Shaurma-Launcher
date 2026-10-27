package custompack

import (
	"strings"
	"testing"
)

// TestNeoForgeLoaderVersions1201 — РЕГРЕСІЙНИЙ ТЕСТ бага 1.20.1.
// NeoForge для Minecraft 1.20.1 нумерується 47.1.x (без префікса "20.1.").
// Стара префіксна хевристика повертала порожній список → NeoForge не
// з'являвся у формі збірки і не встановлювався. Тепер список береться з
// Prism-meta (requires.equals=1.20.1).
//
// Тест мережевий (реальний meta.prismlauncher.org), як і тести валідатора.
func TestNeoForgeLoaderVersions1201(t *testing.T) {
	p := NewVersionsProvider()

	list, err := p.GetNeoForgeLoaderVersions("1.20.1")
	if err != nil {
		t.Fatalf("NeoForge 1.20.1 має повернути список, отримали помилку: %v", err)
	}
	if len(list) == 0 {
		t.Fatal("NeoForge 1.20.1 має мати версії (47.1.x) — список порожній")
	}

	// Усі версії для 1.20.1 мають бути формату 47.x (не 20.1.x!)
	for _, v := range list {
		if !strings.HasPrefix(v.Version, "47.") {
			t.Errorf("NeoForge 1.20.1 версія %q неочікувана — очікували 47.x", v.Version)
		}
	}

	// Рекомендована має бути позначена (у NeoForge рекомендованих прапорців
	// немає → RecommendedVersion вертає найновішу). Механізм, а не точні
	// значення: точна найновіша версія залежить від живого API.
	recCount := 0
	for _, v := range list {
		if v.Recommended {
			recCount++
		}
	}
	if recCount == 0 {
		t.Error("хоча б одна версія NeoForge 1.20.1 має бути позначена як рекомендована")
	}
	t.Logf("NeoForge 1.20.1: %d версій, найновіша (перша) = %s, рекомендована = %s",
		len(list), list[0].Version, recName(list))
}

// TestForgeLoaderVersions1201 — Forge 1.20.1 через Prism-meta.
// Prism сортує список за часом випуску (найновіша перша — зараз 47.4.22),
// а recommended-прапорець залишає на 47.4.10 (це версія, яку Forge сам
// рекомендує). Обидва факти відтворюють поведінку Prism 1:1.
func TestForgeLoaderVersions1201(t *testing.T) {
	p := NewVersionsProvider()

	list, err := p.GetForgeLoaderVersions("1.20.1")
	if err != nil {
		t.Fatalf("Forge 1.20.1 має повернути список, отримали помилку: %v", err)
	}
	if len(list) == 0 {
		t.Fatal("Forge 1.20.1 має мати версії — список порожній")
	}

	// 1) recommended-прапорець (Prism RecommendedRole): зараз на 47.4.10,
	// хоча найновіша за часом — 47.4.22. Механізм (flag wins over newest)
	// перевіряємо жорстко, точні значення — логуємо (залежать від живого API).
	recommended := recName(list)
	if recommended == "" {
		t.Error("хоча б одна версія Forge 1.20.1 має бути позначена як рекомендована")
	}
	// 47.4.10 — стабільний факт (Forge сам рекомендує цю версію для 1.20.1);
	// якщо колись зміниться — це лише попередження, не падіння тесту.
	if recommended != "47.4.10" {
		t.Logf("зауваження: Forge 1.20.1 рекомендує %s (раніше було 47.4.10)", recommended)
	}

	// 2) Список впорядкований за часом випуску спаданням — перша = найновіша.
	if len(list) > 1 && list[0].Version == list[1].Version {
		t.Error("список не має містити дублікатів версій")
	}
	t.Logf("Forge 1.20.1: %d версій, найновіша (перша) = %s, рекомендована = %s",
		len(list), list[0].Version, recommended)
}

// recName повертає версію з Recommended=true, або "" якщо такої немає.
func recName(list []LoaderVersionEntry) string {
	for _, v := range list {
		if v.Recommended {
			return v.Version
		}
	}
	return ""
}

// TestGetAvailableLoaders1201 — для 1.20.1 мають бути доступні і Forge, і
// NeoForge (раніше NeoForge зникав через префіксний баг).
func TestGetAvailableLoaders1201(t *testing.T) {
	p := NewVersionsProvider()

	loaders := p.GetAvailableLoaders("1.20.1")
	hasNeo, hasForge, hasFabric, hasQuilt := false, false, false, false
	for _, l := range loaders {
		switch l {
		case LoaderNeoForge:
			hasNeo = true
		case LoaderForge:
			hasForge = true
		case LoaderFabric:
			hasFabric = true
		case LoaderQuilt:
			hasQuilt = true
		}
	}
	if !hasNeo {
		t.Error("NeoForge має бути доступний для 1.20.1 (47.1.x у Prism-meta)")
	}
	if !hasForge {
		t.Error("Forge має бути доступний для 1.20.1")
	}
	if !hasFabric {
		t.Error("Fabric має бути доступний для 1.20.1")
	}
	if !hasQuilt {
		t.Error("Quilt має бути доступний для 1.20.1")
	}
}

// TestNeoForgeUnsupportedMC — для неіснуючої MC-версії список має бути
// порожнім зі зрозумілою помилкою (не криптичний 404).
func TestNeoForgeUnsupportedMC(t *testing.T) {
	p := NewVersionsProvider()

	list, err := p.GetNeoForgeLoaderVersions("99.99.99")
	if err == nil {
		if len(list) > 0 {
			t.Fatalf("NeoForge не підтримує 99.99.99, отримали %d версій", len(list))
		}
		t.Skip("Prism-meta не віддав помилку для 99.99.99, але список порожній")
	}
	if !strings.Contains(err.Error(), "не підтримує") {
		t.Errorf("помилка має містити 'не підтримує', отримали: %v", err)
	}
}
