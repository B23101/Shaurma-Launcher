package main

// remote_notify_shaurma.go — спливаючі сповіщення власнику, коли друг
// дистанційно керує його сервером (старт, стоп, P2P, консоль) або коли
// така спроба заблокована політикою. Джерело — remotehost.Host.audit.
//
// Сповіщення йде через pushNotify (віджет у треї), поважає налаштування
// «Сповіщення»/«Не турбувати» і показується незалежно від того, чи активне
// головне вікно: це подія безпеки, власник має її бачити. Нік і назва
// сервера приходять ззовні, тому очищаються від керуючих символів.

import (
	"fmt"
	"sync"
	"time"

	"shaurma-launcher-backend/internal/remotectl"
	"shaurma-launcher-backend/internal/remotehost"
	"shaurma-launcher-backend/internal/social"
)

// Не частіше одного сповіщення на (друг, сервер, дія, результат) за цей інтервал.
const remoteNotifyThrottle = 10 * time.Second

var remoteNotifySeen = struct {
	mu sync.Mutex
	m  map[string]time.Time
}{m: map[string]time.Time{}}

func remoteNotifyAllow(key string, now time.Time) bool {
	remoteNotifySeen.mu.Lock()
	defer remoteNotifySeen.mu.Unlock()
	for k, t := range remoteNotifySeen.m { // не дати мапі рости безмежно
		if now.Sub(t) > time.Minute {
			delete(remoteNotifySeen.m, k)
		}
	}
	if t, ok := remoteNotifySeen.m[key]; ok && now.Sub(t) < remoteNotifyThrottle {
		return false
	}
	remoteNotifySeen.m[key] = now
	return true
}

// remoteActionText повертає текст сповіщення або "" (подія не варта тосту).
func remoteActionText(op, result, who, srv string) string {
	switch result {
	case remotectl.CodeNotAllowed, remotectl.CodeNotFriend, remotectl.CodeDisabled,
		remotectl.CodeRateLimited, remotectl.CodePrivileged, remotectl.CodePlayers:
		return fmt.Sprintf("%s намагався керувати «%s» (%s) — заблоковано", who, srv, op)
	}
	switch op {
	case social.RemoteOpStart:
		if result == "accepted" {
			return fmt.Sprintf("%s запускає сервер «%s»", who, srv)
		}
	case social.RemoteOpStop:
		if result == "accepted" {
			return fmt.Sprintf("%s зупиняє сервер «%s»", who, srv)
		}
	case social.RemoteOpP2POn:
		if result == "ok" {
			return fmt.Sprintf("%s увімкнув P2P-адресу «%s»", who, srv)
		}
	case social.RemoteOpP2POff:
		if result == "ok" {
			return fmt.Sprintf("%s вимкнув P2P-адресу «%s»", who, srv)
		}
	case social.RemoteOpConsoleOpen:
		if result == "ok" {
			return fmt.Sprintf("%s відкрив консоль «%s»", who, srv)
		}
	}
	return ""
}

// notifyRemoteHostAction — обробник події remotehost.EventHostAction.
func (a *App) notifyRemoteHostAction(payload any) {
	ev, ok := payload.(remotehost.HostAction)
	if !ok || a.cfg == nil || !a.notificationsAllowed() {
		return
	}
	who := cleanNotifyText(ev.Nick)
	if who == "" {
		who = "Друг"
	}
	srv := cleanNotifyText(ev.Name)
	if srv == "" {
		srv = cleanNotifyText(ev.Server)
	}
	body := remoteActionText(ev.Op, ev.Result, who, srv)
	if body == "" {
		return
	}
	now := time.Now()
	if !remoteNotifyAllow(ev.Friend+"|"+ev.Server+"|"+ev.Op+"|"+ev.Result, now) {
		return
	}
	a.pushNotify(NotifyPayload{
		Kind:  "info",
		Title: "Віддалене керування",
		Body:  body,
		TS:    ev.TS,
		ID:    fmt.Sprintf("remote-%d", now.UnixNano()),
	})
}
