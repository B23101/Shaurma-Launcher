package main

import (
	"path/filepath"
	"strings"

	"shaurma-launcher-backend/internal/modsource"
	"shaurma-launcher-backend/internal/servers"
)

// ── Таргет менеджера модів: збірка або сервер ───────────────────────────
//
// Усі методи менеджера модів (скан, оновлення з резервною копією, версії,
// дерево залежностей, джерела, експорт, міграція, порівняння, імпорт jar)
// приймають один ID «таргету». Звичайний ID — це збірка. ID з префіксом
// "srv:" — тека mods/ сервера, "srvp:" — тека plugins/ сервера. Так один
// і той самий код (і один і той самий UI — PackModsSection) обслуговує і
// збірки, і сервери, замість паралельної копії менеджера для серверів.
const (
	modTargetServerMods    = "srv:"
	modTargetServerPlugins = "srvp:"
	// modTargetNewServer — сервер, що ще створюється (майстер сервера):
	// аналіз міграції вже знає, що ціль — сервер, але теки ще немає.
	modTargetNewServer = modTargetServerMods
)

// serverModTarget — сервер, на який вказує ID таргету (ok=false — це збірка).
type serverModTarget struct {
	srv        servers.Server // копія запису зі сховища (без іконок і статусу)
	contentDir string         // "mods" | "plugins"
}

func (a *App) serverForModTarget(id string) (serverModTarget, bool) {
	var serverID, dir string
	switch {
	case strings.HasPrefix(id, modTargetServerPlugins):
		serverID, dir = strings.TrimPrefix(id, modTargetServerPlugins), modsource.NamePlugins
	case strings.HasPrefix(id, modTargetServerMods):
		serverID, dir = strings.TrimPrefix(id, modTargetServerMods), modsource.NameMods
	default:
		return serverModTarget{}, false
	}
	if a.srvAPI == nil || serverID == "" {
		return serverModTarget{}, false
	}
	// Store().GetNoErr, а не GetServer: таргет резолвиться на кожен виклик
	// менеджера модів, а GetServer ще читає іконки й шукає Java.
	srv, ok := a.srvAPI.Store().GetNoErr(serverID)
	if !ok || srv.Dir == "" {
		return serverModTarget{}, false
	}
	return serverModTarget{srv: *srv, contentDir: dir}, true
}

// isServerModTarget — чи ID таргету вказує на сервер (без звернення до диска).
func isServerModTarget(id string) bool {
	return strings.HasPrefix(id, modTargetServerMods) || strings.HasPrefix(id, modTargetServerPlugins)
}

// modTargetLoader — лоадер, під який шукаються моди/плагіни сервера
// (той самий мапінг ядра, що й у «Знайти онлайн»).
func (t serverModTarget) loader() string {
	kind := "mods"
	if t.contentDir == modsource.NamePlugins {
		kind = "plugins"
	}
	_, loader, _, err := serverBrowserTarget(t.srv.Core, kind)
	if err != nil {
		return ""
	}
	return loader
}

// serverModsDir — тека контенту сервера для таргету.
func (t serverModTarget) modsDir() string {
	return filepath.Join(t.srv.Dir, t.contentDir)
}

// modTargetEnv — версія гри й лоадер таргету (збірки чи сервера).
func (a *App) modTargetEnv(id string) (mcVersion, loader string) {
	if t, ok := a.serverForModTarget(id); ok {
		return t.srv.MCVersion, t.loader()
	}
	if cp, ok := a.customPacks.Get(id); ok {
		return cp.MCVersion, cp.Loader
	}
	if a.packs != nil {
		if entry, err := a.packs.GetPackByID(id); err == nil {
			return entry.MCVersion, entry.LoaderType
		}
	}
	return "", ""
}
