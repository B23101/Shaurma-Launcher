// Package browsersearch реалізує «розумний пошук» браузера контенту:
// гравець вводить тему («меблі», «звірі», «оптимізація»), а не точну
// назву мода, і пошук розширює запит релевантними англійськими термінами
// — саме тими словами, якими автори модів зазвичай описують свій контент
// на Modrinth/CurseForge (обидва індекси — переважно англомовні).
//
// Навмисно НЕ через AI/LLM-виклик на кожен пошук: локальний словник дає
// нульову затримку (критично — «гравці не пробачають помилки пошуку»,
// зайва мережева затримка на кожен символ пошуку неприйнятна), працює
// офлайн і повністю передбачуваний. AI-функції лаунчера (browser_ai.go)
// навмисно лишаються опційними діями за кнопкою, а не в гарячому шляху
// пошуку.
package browsersearch

import "strings"

// synonymGroup — одна тематична група: кілька варіантів написання
// українською/розмовною мовою (ключі активації) → набір англійських
// термінів, якими це зазвичай назване на Modrinth/CurseForge.
type synonymGroup struct {
	keys  []string
	terms []string
}

// synonyms — словник тем. Кожна група відповідає за одну предметну
// область, з якою гравці типово асоціюють мод, але дослівно так не
// шукають в описах (гравець думає «хочу меблі», а мод називається
// «Macaw's Furniture» чи «Furniture Mod»). Список свідомо не намагається
// охопити взагалі все — розширюється за потреби, хибний негативний
// результат (запит не в словнику) просто йде як є, без розширення.
var synonyms = []synonymGroup{
	{keys: []string{"меблі", "мебель", "мебели", "меблев"}, terms: []string{"furniture", "decoration", "decor"}},
	{keys: []string{"декор", "прикраси", "убранство"}, terms: []string{"decoration", "decor", "furniture"}},
	{keys: []string{"звірі", "тварини", "звірят", "тварин"}, terms: []string{"animals", "mobs", "creatures"}},
	{keys: []string{"монстри", "вороги", "боси", "бос"}, terms: []string{"mobs", "monsters", "bosses"}},
	{keys: []string{"дракон", "дракони"}, terms: []string{"dragon", "dragons"}},
	{keys: []string{"броня", "обладунки", "зброя", "мечі", "меч"}, terms: []string{"armor", "weapons", "combat"}},
	{keys: []string{"магія", "маги", "закляття", "заклинання"}, terms: []string{"magic", "spells", "wizardry"}},
	{keys: []string{"техніка", "механізми", "автоматизація", "фабрика"}, terms: []string{"tech", "machines", "automation", "industrial"}},
	{keys: []string{"труби", "трубопровід"}, terms: []string{"pipes", "transport"}},
	{keys: []string{"електрика", "енергія", "генератор"}, terms: []string{"power", "energy", "electricity"}},
	{keys: []string{"будівлі", "будинки", "архітектура", "споруди"}, terms: []string{"structures", "buildings", "architecture"}},
	{keys: []string{"біоми", "ліс", "ліси", "світ", "генерація світу"}, terms: []string{"biomes", "world generation", "terrain"}},
	{keys: []string{"підземелля", "данж", "данжі", "рейд", "рейди"}, terms: []string{"dungeons", "dungeon", "structures"}},
	{keys: []string{"оптимізація", "фпс", "продуктивність", "лаги"}, terms: []string{"optimization", "performance", "fps"}},
	{keys: []string{"шейдери", "графіка", "візуал", "текстури", "текстур пак"}, terms: []string{"shaders", "graphics", "visual", "textures"}},
	{keys: []string{"інтерфейс", "гуі", "меню", "хад", "hud"}, terms: []string{"UI", "HUD", "interface", "GUI"}},
	{keys: []string{"карта", "мінімапа", "мінікарта"}, terms: []string{"map", "minimap"}},
	{keys: []string{"інвентар", "сумки", "сховище", "склад"}, terms: []string{"storage", "inventory", "backpack"}},
	{keys: []string{"рюкзак", "рюкзаки"}, terms: []string{"backpack", "backpacks"}},
	{keys: []string{"фермерство", "ферма", "врожай", "сільське господарство"}, terms: []string{"farming", "agriculture", "crops"}},
	{keys: []string{"кухня", "їжа", "готування", "рецепти"}, terms: []string{"food", "cooking", "recipes"}},
	{keys: []string{"транспорт", "машини", "автомобілі", "літаки"}, terms: []string{"vehicles", "transport", "cars", "planes"}},
	{keys: []string{"кораблі", "корабель", "човни", "човен"}, terms: []string{"boats", "ships"}},
	{keys: []string{"питомці", "приручення", "фаміліари", "компаньйони"}, terms: []string{"pets", "taming", "companions"}},
	{keys: []string{"квести", "завдання", "прогресія"}, terms: []string{"quests", "progression", "questing"}},
	{keys: []string{"економіка", "торгівля", "гроші", "валюта"}, terms: []string{"economy", "trading", "currency"}},
	{keys: []string{"мультиплеєр", "сервер", "адміністрування"}, terms: []string{"server", "multiplayer", "administration"}},
	{keys: []string{"захист", "безпека", "анти-грифінг", "античит"}, terms: []string{"protection", "security", "anti-grief"}},
	{keys: []string{"погода", "клімат", "сезони"}, terms: []string{"weather", "climate", "seasons"}},
	{keys: []string{"освітлення", "світло", "лампи", "ліхтарі"}, terms: []string{"lighting", "lights", "lamps"}},
	{keys: []string{"мінікарти", "координати"}, terms: []string{"minimap", "coordinates"}},
	{keys: []string{"бібліотека", "залежність", "api"}, terms: []string{"library", "api", "core"}},
	{keys: []string{"космос", "планети", "ракети"}, terms: []string{"space", "planets", "rockets"}},
	{keys: []string{"рибалка", "риба", "риболовля"}, terms: []string{"fishing", "fish"}},
	{keys: []string{"гірництво", "шахти", "руди", "видобуток"}, terms: []string{"mining", "ores", "mines"}},
	{keys: []string{"будівництво", "блоки", "будматеріали"}, terms: []string{"building", "blocks", "construction"}},
	{keys: []string{"музика", "звук", "звуки"}, terms: []string{"music", "sound", "audio"}},
	{keys: []string{"мапи текстур", "ресурспак", "скін"}, terms: []string{"resource pack", "textures", "skin"}},
}

// Expand розширює пошуковий запит релевантними англійськими термінами,
// якщо запит (повністю або якесь із його слів) знайдено в словнику тем.
// Повертає ОРИГІНАЛЬНИЙ запит без змін, якщо збігів не знайдено — так
// звичайний точний пошук (за назвою мода) ніколи не змінюється розумним
// розширенням; воно лише ДОДАЄ ширину для тематичних запитів.
//
// matched — чи спрацювало розширення (для позначки в UI "розумний
// пошук: показано результати за темою «меблі»").
func Expand(query string) (expanded string, matched bool, matchedTerms []string) {
	q := strings.TrimSpace(strings.ToLower(query))
	if q == "" {
		return query, false, nil
	}

	// 1) Точний збіг усієї фрази (найвищий пріоритет — «меблі» як єдине
	// слово запиту, найпоширеніший випадок розумного пошуку).
	for _, g := range synonyms {
		for _, k := range g.keys {
			if q == k {
				return buildExpanded(query, g.terms), true, g.terms
			}
		}
	}

	// 2) Збіг окремого слова всередині багатослівного запиту («хочу
	// меблі та освітлення» — обидва слова розширюються, дублікати термінів
	// прибираються нижче в buildExpanded через append+visited).
	words := strings.Fields(q)
	if len(words) <= 1 {
		return query, false, nil
	}
	var allTerms []string
	seenGroup := map[int]bool{}
	for _, w := range words {
		for gi, g := range synonyms {
			if seenGroup[gi] {
				continue
			}
			for _, k := range g.keys {
				if w == k {
					allTerms = append(allTerms, g.terms...)
					seenGroup[gi] = true
					break
				}
			}
		}
	}
	if len(allTerms) == 0 {
		return query, false, nil
	}
	return buildExpanded(query, allTerms), true, allTerms
}

// buildExpanded додає терміни до оригінального запиту без дублікатів.
// Оригінальний текст лишається ПЕРШИМ у рядку — і Modrinth, і CurseForge
// зважують збіг на початку запиту вище, тож якщо гравець вжив слово,
// що вже є в назві якогось мода (рідкісний, але можливий випадок), воно
// не втрачає пріоритету.
func buildExpanded(original string, terms []string) string {
	seen := map[string]bool{}
	parts := []string{original}
	for _, t := range terms {
		lt := strings.ToLower(t)
		if seen[lt] {
			continue
		}
		seen[lt] = true
		parts = append(parts, t)
	}
	return strings.Join(parts, " ")
}
