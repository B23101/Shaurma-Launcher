// api_rooms.go — прокидання нових можливостей вузла (оренда, співхости, спільні кімнати, списки доступу,
// kick) у API для Wails-методів App (p2p_api.go). Власник доводить себе локальним ownerSecret.
package p2p

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

func (a *API) roomCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 12*time.Second)
}

// StartCohostSession — співхост хостить ЧУЖУ кімнату: оренда береться лише за JWT (без ownerSecret).
func (a *API) StartCohostSession(minecraftPort, address string, udpPort int, ownerName string) (HostStatus, error) {
	if err := validateHostPorts(minecraftPort, udpPort); err != nil {
		return HostStatus{Error: err.Error()}, err
	}
	address = strings.ToLower(strings.TrimSpace(address))
	if address == "" {
		return HostStatus{}, fmt.Errorf("p2p: не вказано адресу кімнати")
	}
	a.mu.Lock()
	if _, already := a.hosts[address]; already || len(a.hosts) >= MaxConcurrentRooms {
		a.mu.Unlock()
		return HostStatus{}, fmt.Errorf("p2p: %s вже хоститься або досягнуто ліміту одночасних серверів", address)
	}
	a.mu.Unlock()

	ctx, cancel := a.roomCtx()
	lease, err := a.lobbyClient().HostStart(ctx, address, "")
	cancel()
	if err != nil {
		if le, ok := AsLobbyError(err); ok && le.HostName != "" {
			return HostStatus{}, fmt.Errorf("p2p: кімнату вже хостить %s", le.HostName)
		}
		return HostStatus{}, err
	}
	if !lease.Shared || lease.SessionToken == "" {
		return HostStatus{}, fmt.Errorf("p2p: ця кімната не спільна — хостити її може лише власник")
	}
	session, err := a.startHostSession(minecraftPort, address, udpPort, "", ownerName, lease.SessionToken)
	if err != nil {
		rctx, rcancel := a.roomCtx()
		_ = a.lobbyClient().HostStop(rctx, address, lease.SessionToken)
		rcancel()
		return HostStatus{}, err
	}
	a.mu.Lock()
	a.hosts[address] = session
	count := len(a.hosts)
	a.mu.Unlock()
	return HostStatus{Active: true, Subdomain: address, Address: session.Address, UDPPort: session.UDPPort(),
		UDPError: errString(session.UDPError()), Disabled: session.Disabled, Access: session.Access(), ActiveCount: count, MaxCount: MaxConcurrentRooms}, nil
}

// errRoomAccessUnsupported — вузол ще не має /p2p/room/access (старіша збірка VPS).
// Потрібно лише щоб пояснити причину в UI, а не щоб ламати решту логіки.
var errRoomAccessUnsupported = errors.New("вузол P2P не підтримує зміну режиму без хостингу — оновіть серверну частину")

// SetAccessMode міняє режим доступу кімнати (лише власник).
//
// Два шляхи:
//  1. Кімната зараз хоститься — як і раніше, через host-update активної
//     сесії (той запит є і heartbeat-ом оренди).
//  2. Кімнати не хоститься (сервер зупинено) — режим пишеться прямо в
//     запис кімнати (RoomAccessSet), тож хостинг підніметься з ним. Раніше
//     другого шляху не було і перемикач у лаунчері був заблокований, поки
//     сервер не запущено — саме це виглядало як баг.
func (a *API) SetAccessMode(subdomain, access string) error {
	subdomain = strings.ToLower(strings.TrimSpace(subdomain))
	a.mu.Lock()
	h := a.hosts[subdomain]
	a.mu.Unlock()
	secret, err := a.ownerSecretValue()
	if err != nil {
		return err
	}
	if h != nil {
		if err := a.updateHostSessionAccess(h, secret, access); err == nil {
			return nil
		}
		// Сесія могла вже померти (оренду забрано) або вузол старий — тоді
		// не втрачаємо намір гравця, а пишемо режим у саму кімнату.
	}
	ctx, cancel := a.roomCtx()
	defer cancel()
	return a.lobbyClient().RoomAccessSet(ctx, subdomain, secret, access)
}

// updateHostSessionAccess — старий шлях: режим іде через host-update чинної сесії.
func (a *API) updateHostSessionAccess(h *HostSession, secret, access string) error {
	ctx, cancel := a.roomCtx()
	defer cancel()
	resp, err := a.lobbyClient().HostUpdate(ctx, HostUpdateRequest{
		Subdomain: h.Subdomain, OwnerSecret: secret, OwnerName: h.ownerName, HostIP: h.hostIP, HostPort: h.hostPort,
		UDPPort: h.UDPPort(), SessionToken: h.SessionToken(), Access: &access,
	})
	if err != nil {
		return err
	}
	h.SetAccess(resp.Access)
	return nil
}

// RoomAccess читає режим доступу кімнати — і коли вона хоститься, і коли ні
// (для UI: перемикач має показувати справжній режим, а не «невідомо»).
// Помилку не вважаємо критичною — фронтенд просто лишить режим порожнім.
func (a *API) RoomAccess(subdomain string) (string, error) {
	subdomain = strings.ToLower(strings.TrimSpace(subdomain))
	a.mu.Lock()
	h := a.hosts[subdomain]
	a.mu.Unlock()
	if h != nil {
		return h.Access(), nil
	}
	secret, err := a.ownerSecretValue()
	if err != nil {
		return "", err
	}
	ctx, cancel := a.roomCtx()
	defer cancel()
	return a.lobbyClient().RoomAccessGet(ctx, subdomain, secret)
}

// KickHost — власник забирає хост у співхоста. true — хтось справді був вигнаний.
func (a *API) KickHost(address string) (bool, error) {
	secret, err := a.ownerSecretValue()
	if err != nil {
		return false, err
	}
	ctx, cancel := a.roomCtx()
	defer cancel()
	return a.lobbyClient().LeaseKick(ctx, address, secret)
}

func (a *API) Cohosts(address string) (CohostList, error) {
	secret, err := a.ownerSecretValue()
	if err != nil {
		return CohostList{}, err
	}
	ctx, cancel := a.roomCtx()
	defer cancel()
	return a.lobbyClient().Cohosts(ctx, address, secret)
}

func (a *API) CohostApply(address string, add, remove []ACLEntry) (CohostApplyResult, error) {
	secret, err := a.ownerSecretValue()
	if err != nil {
		return CohostApplyResult{}, err
	}
	ctx, cancel := a.roomCtx()
	defer cancel()
	return a.lobbyClient().CohostApply(ctx, address, secret, add, remove)
}

func (a *API) SharedRooms() ([]SharedRoom, error) {
	ctx, cancel := a.roomCtx()
	defer cancel()
	return a.lobbyClient().Shared(ctx)
}

func (a *API) ACLList(address string) (ACLList, error) {
	secret, err := a.ownerSecretValue()
	if err != nil {
		return ACLList{}, err
	}
	ctx, cancel := a.roomCtx()
	defer cancel()
	return a.lobbyClient().ACL(ctx, address, secret)
}

func (a *API) ACLApply(address string, add, remove []ACLEntry) (ACLApplyResult, error) {
	secret, err := a.ownerSecretValue()
	if err != nil {
		return ACLApplyResult{}, err
	}
	ctx, cancel := a.roomCtx()
	defer cancel()
	return a.lobbyClient().ACLApply(ctx, address, secret, add, remove)
}
