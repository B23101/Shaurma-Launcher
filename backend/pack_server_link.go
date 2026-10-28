package main

import (
	"log"

	"shaurma-launcher-backend/internal/model"
	"shaurma-launcher-backend/internal/servers"
)

// ── Сервер разом зі збіркою ─────────────────────────────────────────────
//
// Зв'язок збірка ↔ сервер (servers.PackServerLink, pack-links.json):
// «Грати» запускає й сервер, а після входу в гру можна одразу підключитися
// до нього; після закриття гри сервер за бажанням зупиняється.

// startLinkedServers запускає сервери, прив'язані до збірки, і (якщо
// попрошено) підставляє автоприєднання до першого з них. joinFixed=true —
// адресу для цього запуску вже задано явно (ярлик, «Грати» на сервері),
// її не перекриваємо.
func (a *App) startLinkedServers(packID string, icfg *model.InstanceConfig, joinFixed bool) {
	if a.srvAPI == nil {
		return
	}
	for _, l := range a.srvAPI.GetPackServerLinks(packID) {
		if !l.LaunchWithPack {
			continue
		}
		srv, ok := a.srvAPI.Store().GetNoErr(l.ServerID)
		if !ok {
			continue
		}
		if !a.srvAPI.Manager().IsRunning(l.ServerID) {
			if err := a.StartServer(l.ServerID); err != nil {
				log.Printf("[pack-link] сервер %s не запущено: %v", l.ServerID, err)
				a.emit("server:linked-error", map[string]any{
					"packId": packID, "serverId": l.ServerID, "serverName": srv.Name, "message": err.Error(),
				})
				continue
			}
		}
		if l.JoinOnLaunch && !joinFixed {
			icfg.AutoJoinEnabled = true
			icfg.AutoJoinType = "server"
			icfg.AutoJoinServer = servers.FormatServerAddress(srv)
			joinFixed = true
		}
	}
}

// stopLinkedServers зупиняє сервери з «зупиняти разом зі збіркою», коли
// закрилась остання сесія цієї збірки.
func (a *App) stopLinkedServers(packID string) {
	if a.srvAPI == nil || a.sessionsM.RunningOnBuild(packID) != nil {
		return
	}
	for _, l := range a.srvAPI.GetPackServerLinks(packID) {
		if l.StopWithPack && a.srvAPI.Manager().IsRunning(l.ServerID) {
			if err := a.srvAPI.StopServer(l.ServerID, true); err != nil {
				log.Printf("[pack-link] сервер %s не зупинено: %v", l.ServerID, err)
			}
		}
	}
}
