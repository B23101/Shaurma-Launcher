//go:build !shaurma

package main

import (
	"shaurma-launcher-backend/internal/model"
	"shaurma-launcher-backend/internal/servers"
)

// У base-збірці соціальної системи немає — колбеки серверного менеджера нічого не роблять.
type remoteRT struct{}

func (a *App) remoteOnLine(string, model.ConsoleLine)      {}
func (a *App) remoteOnNetworkChanged()                     {}
func (a *App) remoteOnP2PChanged()                         {}
func (a *App) remoteOnStatus(string, servers.ServerStatus) {}
func (a *App) remoteOnServerDeleted(string)                {}
func (a *App) remoteOnServerRenamed(string, string)        {}
