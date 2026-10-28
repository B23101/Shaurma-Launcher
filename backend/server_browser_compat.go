package main

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"shaurma-launcher-backend/internal/custompack"
)

// ── Сумісність контенту з серверами (діалог «Встановити» браузера) ──────
//
// Раніше вкладка «Сервери» в діалозі встановлення показувала всі сервери з
// потрібним типом ядра без жодної перевірки: ні версії гри, ні того, чи мод
// узагалі працює на сервері. Тепер для кожного сервера перевіряємо, чи є
// версія під (mc, ядро) — один запит на унікальну пару, паралельно — і
// сторону мода: client_side/server_side з Modrinth (як Modrinth app) або
// мітки Client/Server у файлах CurseForge. Суто клієнтський мод (server_side
// = unsupported) на сервер ставити не можна.

// BrowserServerCompat — результат GetBrowserServerCompat.
type BrowserServerCompat struct {
	// ClientSide/ServerSide — required | optional | unsupported | unknown
	// (Modrinth); для CurseForge порожні.
	ClientSide string `json:"clientSide"`
	ServerSide string `json:"serverSide"`
	// ClientOnly — мод працює лише на клієнті (на сервер не ставиться).
	ClientOnly bool `json:"clientOnly"`
	// ServerOnly — мод лише серверний (у клієнтській збірці нічого не дає).
	ServerOnly bool                       `json:"serverOnly"`
	Servers    []BrowserServerCompatEntry `json:"servers"`
}

// BrowserServerCompatEntry — сумісність з одним сервером.
type BrowserServerCompatEntry struct {
	ServerID   string `json:"serverId"`
	Compatible bool   `json:"compatible"`
	// Unconfirmed — запит версій не вдався (мережа/ключ CurseForge): сервер
	// лишається доступним, встановлення перевірить ще раз.
	Unconfirmed bool `json:"unconfirmed,omitempty"`
	// Reason — чому несумісний: clientOnly | noVersion | unsupportedCore.
	Reason            string `json:"reason,omitempty"`
	AlreadyInstalled  bool   `json:"alreadyInstalled,omitempty"`
	InstalledFileName string `json:"installedFileName,omitempty"`
}

// errNoCurseForgeKey — у збірці лаунчера немає ключа CurseForge API.
var errNoCurseForgeKey = errors.New("CurseForge API-ключ не налаштовано")

// serverTargetFiles — версії під одну пару (mc, лоадер ядра).
type serverTargetFiles struct {
	fileName   string
	has        bool
	clientOnly bool
	err        error
}

// GetBrowserServerCompat перевіряє сумісність контенту з усіма серверами
// лаунчера. projectType — тип запису браузера (mod/plugin/…).
func (a *App) GetBrowserServerCompat(source, projectID, projectType string) BrowserServerCompat {
	out := BrowserServerCompat{Servers: []BrowserServerCompatEntry{}}
	kind := "mods"
	if projectType == "plugin" {
		kind = "plugins"
	}

	// Сторона мода з Modrinth — паралельно з переліком серверів.
	var sideWG sync.WaitGroup
	if source == "modrinth" && kind == "mods" {
		sideWG.Add(1)
		go func() {
			defer sideWG.Done()
			if d, err := a.modrinth.GetProjectDetail(projectID); err == nil && d != nil {
				out.ClientSide, out.ServerSide = d.ClientSide, d.ServerSide
			}
		}()
	}

	if a.srvAPI == nil {
		sideWG.Wait()
		out.ClientOnly = out.ServerSide == "unsupported"
		out.ServerOnly = out.ClientSide == "unsupported"
		return out
	}
	list, err := a.srvAPI.ListServers()
	if err != nil {
		sideWG.Wait()
		return out
	}

	type target struct {
		idx                      int
		projectType, loader, dir string
		key                      string
		unsupported              bool
	}
	targets := make([]target, 0, len(list))
	for i, srv := range list {
		k := normalizeServerKind(srv.Core, kind)
		pt, loader, dir, err := serverBrowserTarget(srv.Core, k)
		if err != nil {
			targets = append(targets, target{idx: i, unsupported: true})
			continue
		}
		targets = append(targets, target{idx: i, projectType: pt, loader: loader, dir: dir, key: srv.MCVersion + "|" + pt + "|" + loader})
	}

	// Один запит на унікальну пару (mc, тип, лоадер), паралельно.
	files := map[string]serverTargetFiles{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, browserCompatWorkers)
	seen := map[string]bool{}
	for _, tg := range targets {
		if tg.unsupported || seen[tg.key] {
			continue
		}
		seen[tg.key] = true
		mc := list[tg.idx].MCVersion
		wg.Add(1)
		sem <- struct{}{}
		go func(key, mc, pt, loader string) {
			defer wg.Done()
			defer func() { <-sem }()
			r := a.serverTargetFilesFor(source, projectID, pt, mc, loader)
			mu.Lock()
			files[key] = r
			mu.Unlock()
		}(tg.key, mc, tg.projectType, tg.loader)
	}
	wg.Wait()
	sideWG.Wait()

	out.ClientOnly = out.ServerSide == "unsupported"
	out.ServerOnly = out.ClientSide == "unsupported"
	for _, tg := range targets {
		srv := list[tg.idx]
		e := BrowserServerCompatEntry{ServerID: srv.ID}
		switch {
		case tg.unsupported:
			e.Reason = "unsupportedCore"
		case out.ClientOnly && tg.projectType == "mod":
			e.Reason = "clientOnly"
		default:
			r := files[tg.key]
			switch {
			case r.err != nil:
				e.Compatible, e.Unconfirmed = true, true
			case !r.has:
				e.Reason = "noVersion"
			case r.clientOnly && tg.projectType == "mod":
				e.Reason = "clientOnly"
			default:
				e.Compatible = true
				if r.fileName != "" {
					if st, err := os.Stat(filepath.Join(srv.Dir, tg.dir, r.fileName)); err == nil && !st.IsDir() {
						e.AlreadyInstalled, e.InstalledFileName = true, r.fileName
					}
				}
			}
		}
		out.Servers = append(out.Servers, e)
	}
	return out
}

// serverTargetFilesFor — чи є версія під (mc, лоадер) і чи вона суто
// клієнтська. Для CurseForge сторону беремо з міток Client/Server у
// gameVersions файлів: файли з мітками, серед яких жодного Server, —
// клієнтський мод.
func (a *App) serverTargetFilesFor(source, projectID, projectType, mc, loader string) serverTargetFiles {
	if source != "curseforge" {
		versions, err := a.GetBrowserProjectVersions(source, projectID, projectType, mc, loader)
		for retry := 1; err != nil && retry < 3; retry++ {
			versions, err = a.GetBrowserProjectVersions(source, projectID, projectType, mc, loader)
		}
		if err != nil {
			return serverTargetFiles{err: err}
		}
		r := serverTargetFiles{has: len(versions) > 0}
		if r.has {
			r.fileName = versions[0].FileName
		}
		return r
	}
	if !custompack.CurseForgeAvailable() {
		return serverTargetFiles{err: errNoCurseForgeKey}
	}
	id, err := strconv.Atoi(projectID)
	if err != nil {
		return serverTargetFiles{err: err}
	}
	cfFiles, err := a.cfClient().GetModFiles(id, mc, effectiveLoaderForType(projectType, loader))
	if err != nil {
		return serverTargetFiles{err: err}
	}
	r := serverTargetFiles{has: len(cfFiles) > 0}
	if r.has {
		r.fileName = cfFiles[0].FileName
	}
	tagged, server := false, false
	for _, f := range cfFiles {
		for _, gv := range f.GameVersions {
			switch strings.ToLower(strings.TrimSpace(gv)) {
			case "client":
				tagged = true
			case "server":
				tagged, server = true, true
			}
		}
	}
	r.clientOnly = tagged && !server
	return r
}
