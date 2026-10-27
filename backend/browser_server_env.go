package main

import (
	"shaurma-launcher-backend/internal/crashguard"
	"strconv"
	"strings"
	"sync"
	"time"

	"shaurma-launcher-backend/internal/model"
)

// ── Серверна ціль: відсів суто клієнтських модів CurseForge ────────────
//
// Modrinth має фасети client_side/server_side, тож там ця задача
// вирішується прямо в пошуку (createEnvironmentFacets). CurseForge публічним
// API НЕ віддає поля «Environment» проєкту (на сайті воно є, в REST — ні:
// /mods/search і /mods/{id} його не повертають). Єдиний доступний сигнал —
// рядки "Client"/"Server", які автор може вказати в gameVersions КОНКРЕТНОГО
// ФАЙЛУ (поруч із версією гри та лоадером).
//
// Сигнал ненадійний в обидва боки (це підтверджує практика itzg/docker-
// minecraft-server: CF_EXCLUDE_MODS для клієнтських, що не позначені, і
// CF_FORCE_INCLUDE_MODS для серверних, помилково позначених "Client").
// Тому правило консервативне: мод ховаємо ТІЛЬКИ коли всі його файли під
// (mcVersion, loader) явно мають "Client" і жоден не має "Server". Немає
// тегів / мережева помилка / порожній список файлів → мод ЛИШАЄМО (краще
// показати зайвий, ніж сховати робочий).

// cfFileEnv — що файл каже про середовище.
type cfFileEnv int

const (
	cfEnvUnknown cfFileEnv = iota // немає ні Client, ні Server
	cfEnvClientOnly
	cfEnvServerCapable // є "Server" (з "Client" чи без)
)

func cfEnvOfGameVersions(gameVersions []string) cfFileEnv {
	hasClient, hasServer := false, false
	for _, v := range gameVersions {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "client":
			hasClient = true
		case "server":
			hasServer = true
		}
	}
	switch {
	case hasServer:
		return cfEnvServerCapable
	case hasClient:
		return cfEnvClientOnly
	default:
		return cfEnvUnknown
	}
}

// cfProjectIsClientOnly — true лише коли ВСІ файли явно клієнтські.
// Один файл із "Server" або без тегів робить мод «не доведено клієнтським».
func cfProjectIsClientOnly(filesGameVersions [][]string) bool {
	if len(filesGameVersions) == 0 {
		return false
	}
	for _, gv := range filesGameVersions {
		if cfEnvOfGameVersions(gv) != cfEnvClientOnly {
			return false
		}
	}
	return true
}

type cfEnvCacheEntry struct {
	clientOnly bool
	at         time.Time
}

var (
	cfEnvCacheMu sync.Mutex
	cfEnvCache   = map[string]cfEnvCacheEntry{}
)

const cfEnvCacheTTL = 10 * time.Minute

// BrowserSearchCurseForgeForServer — пошук CurseForge для СЕРВЕРНОЇ цілі:
// той самий пошук, що BrowserSearchCurseForge, але без суто клієнтських
// модів. Окремий метод (а не новий параметр), щоб не міняти сигнатуру
// існуючого й не чіпати інших викликачів.
func (a *App) BrowserSearchCurseForgeForServer(query, projectType, mcVersion, loader, categoryIds, sort string, limit int, packID string) BrowserSearchResult {
	res := a.BrowserSearchCurseForge(query, projectType, mcVersion, loader, categoryIds, sort, limit, packID)
	// Плагіни й не-моди не фільтруємо: для плагінів CF не шукається взагалі,
	// а ресурспаки/шейдери сервер не тримає.
	if projectType != "mod" || len(res.CurseForge) == 0 {
		return res
	}
	res.CurseForge = a.dropClientOnlyCurseForge(res.CurseForge, mcVersion, loader)
	return res
}

// dropClientOnlyCurseForge перевіряє файли кожного хіта паралельно (пул
// із 6 воркерів, rate limiter CF усе одно тримає 4/с) і повертає лише ті,
// що не доведено клієнтськими.
func (a *App) dropClientOnlyCurseForge(entries []model.BrowserEntry, mcVersion, loader string) []model.BrowserEntry {
	keep := make([]bool, len(entries))
	sem := make(chan struct{}, 6)
	var wg sync.WaitGroup
	for i, e := range entries {
		keep[i] = true
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, e model.BrowserEntry) {
			defer crashguard.Recover("browser_server_env.go:110")
			defer wg.Done()
			defer func() { <-sem }()
			if a.cfEntryIsClientOnly(e, mcVersion, loader) {
				keep[i] = false
			}
		}(i, e)
	}
	wg.Wait()
	out := make([]model.BrowserEntry, 0, len(entries))
	for i, e := range entries {
		if keep[i] {
			out = append(out, e)
		}
	}
	return out
}

func (a *App) cfEntryIsClientOnly(e model.BrowserEntry, mcVersion, loader string) bool {
	id, err := strconv.Atoi(e.ProjectID)
	if err != nil {
		return false
	}
	key := e.ProjectID + "|" + mcVersion + "|" + strings.ToLower(loader)
	cfEnvCacheMu.Lock()
	if c, ok := cfEnvCache[key]; ok && time.Since(c.at) < cfEnvCacheTTL {
		cfEnvCacheMu.Unlock()
		return c.clientOnly
	}
	cfEnvCacheMu.Unlock()

	files, err := a.cfClient().GetModFiles(id, mcVersion, loader)
	if err != nil {
		return false // не змогли перевірити — не ховаємо (fail-open)
	}
	gvs := make([][]string, 0, len(files))
	for _, f := range files {
		gvs = append(gvs, f.GameVersions)
	}
	clientOnly := cfProjectIsClientOnly(gvs)

	cfEnvCacheMu.Lock()
	cfEnvCache[key] = cfEnvCacheEntry{clientOnly: clientOnly, at: time.Now()}
	cfEnvCacheMu.Unlock()
	return clientOnly
}
