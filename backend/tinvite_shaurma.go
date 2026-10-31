// Тимчасові запрошення: гравець відкрив LAN-світ або локальний сервер → одним вікном
// запрошує друзів. Доступ — лише запрошеним (квиток від VPS перевіряє сам P2P-хост),
// живе лише поки відкритий світ; закрили світ — доступ і картки в чаті гаснуть самі.
package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"shaurma-launcher-backend/internal/crashguard"
	"shaurma-launcher-backend/internal/p2p"
	"shaurma-launcher-backend/internal/social"
)

const tempInviteTTL = 6 * time.Hour // стеля картки в чаті; реальний строк — поки живий світ

type tempInvite struct {
	ID        string             `json:"id"`
	Address   string             `json:"address"`
	Subdomain string             `json:"subdomain"`
	Meta      p2p.LocalServer    `json:"meta"`
	Guests    []string           `json:"guests"`
	PackID    string             `json:"packId"`
	PackName  string             `json:"packName"`
	stop      context.CancelFunc `json:"-"`
}

var (
	tinvMu sync.Mutex
	tinvs  = map[string]*tempInvite{}
)

// ScanLocalServers — «Аналізувати»: реальні LAN-світи й сервери на цьому ПК.
func (a *App) ScanLocalServers() []p2p.LocalServer {
	ctx, cancel := context.WithTimeout(context.Background(), 9*time.Second)
	defer cancel()
	return p2p.ScanLocalServers(ctx)
}

// ProbeLocalServer — перевірка порту, введеного вручну (захист від дурня: лише справжній Minecraft).
func (a *App) ProbeLocalServer(port int) (p2p.LocalServer, error) { return p2p.ProbeLocalServer(port) }

// ListTempInvites — мої живі тимчасові доступи (щоб запросити ще когось без нового доступу).
func (a *App) ListTempInvites() []tempInvite {
	tinvMu.Lock()
	defer tinvMu.Unlock()
	out := make([]tempInvite, 0, len(tinvs))
	for _, t := range tinvs {
		c := *t
		c.Guests = append([]string(nil), t.Guests...)
		out = append(out, c)
	}
	return out
}

// CreateTempInvite — новий тимчасовий доступ до світу на port і запрошення друзям у чат.
// Якщо для цього порту доступ уже є — нового не створюємо, лише запрошуємо.
func (a *App) CreateTempInvite(port int, friendIDs []string, packID, packName string) (tempInvite, error) {
	if a.p2pDisabled() {
		return tempInvite{}, errP2PDisabled()
	}
	if len(friendIDs) == 0 || len(friendIDs) > 20 {
		return tempInvite{}, errors.New("оберіть від 1 до 20 друзів")
	}
	tinvMu.Lock()
	for _, t := range tinvs {
		if t.Meta.Port == port {
			id := t.ID
			tinvMu.Unlock()
			return a.InviteToTempInvite(id, friendIDs)
		}
	}
	tinvMu.Unlock()
	meta, err := p2p.ProbeLocalServer(port)
	if err != nil {
		return tempInvite{}, fmt.Errorf("на порту %d немає відкритого світу Minecraft: %w", port, err)
	}
	accountID, _, err := a.p2pAccountCheck()
	if err != nil {
		return tempInvite{}, err
	}
	svc := a.socialService()
	if svc == nil {
		return tempInvite{}, social.ErrNotLoggedIn
	}
	sub, addr, err := a.freeTempAddress(accountID)
	if err != nil {
		return tempInvite{}, err
	}
	// UDP — той самий порт (не редагується: це тимчасовий доступ, а не гнучкий сервер).
	st, herr := a.StartP2PHost(strconv.Itoa(port), sub, port)
	if herr != nil || !st.Active {
		// Адресу вже записано в сховище тимчасових — не лишаємо «висіти» (займала б слот).
		_ = a.p2pAPI.ReleaseTempAddress(sub)
		return tempInvite{}, fmt.Errorf("не вдалося відкрити P2P-доступ: %v %s", herr, st.Error)
	}
	// Якщо адреса протухла в lobby, StartP2PHost сам перезайняв нову (інший суфікс) — далі
	// працюємо з РЕАЛЬНОЮ адресою хост-сесії, а не з тією, що повернув freeTempAddress.
	if st.Subdomain != "" && st.Subdomain != sub {
		sub = st.Subdomain
		if st.Address != "" {
			addr = st.Address
		}
	}
	id, err := svc.TempInviteCreate(social.TempInviteMeta{Address: addr, Port: port, Kind: meta.Kind, Motd: meta.Motd, Version: meta.Version, Players: meta.Players, Max: meta.Max})
	if err != nil {
		_ = a.p2pAPI.StopP2PHost(sub)
		var ae *social.APIError
		if errors.As(err, &ae) && ae.Status == 429 {
			return tempInvite{}, errors.New("з вашої мережі вже відкрито 3 тимчасові доступи — закрийте один")
		}
		return tempInvite{}, err
	}
	a.p2pAPI.SetHostGate(sub, &p2p.HostGate{InviteID: id, Admit: svc.TempInviteAdmit})
	ctx, cancel := context.WithCancel(context.Background())
	t := &tempInvite{ID: id, Address: addr, Subdomain: sub, Meta: meta, PackID: packID, PackName: packName, stop: cancel}
	tinvMu.Lock()
	tinvs[id] = t
	tinvMu.Unlock()
	go func() {
		defer crashguard.Recover("tempInviteWatch")
		a.watchTempInvite(ctx, t)
	}()
	return a.InviteToTempInvite(id, friendIDs)
}

// InviteToTempInvite — запросити ще друзів у вже відкритий доступ (одна кнопка).
func (a *App) InviteToTempInvite(id string, friendIDs []string) (tempInvite, error) {
	tinvMu.Lock()
	t, ok := tinvs[id]
	tinvMu.Unlock()
	if !ok {
		return tempInvite{}, errors.New("доступ уже закрито")
	}
	svc := a.socialService()
	if svc == nil {
		return tempInvite{}, social.ErrNotLoggedIn
	}
	if err := svc.TempInviteAddGuests(id, friendIDs); err != nil {
		return tempInvite{}, err
	}
	var sendErr error
	for _, f := range friendIDs {
		if err := svc.SendServerInvite(f, t.Address, t.PackID, t.PackName, tempInviteTTL.Milliseconds()); err != nil {
			sendErr = err
		} else {
			// Відкритий чат із цим другом підтягне нову картку одразу (запасний шлях
			// на випадок, коли VPS ще не розсилає ехо відправнику).
			a.emit("tinvite:invited", f)
		}
	}
	tinvMu.Lock()
	seen := map[string]bool{}
	for _, g := range t.Guests {
		seen[g] = true
	}
	for _, f := range friendIDs {
		if !seen[f] {
			t.Guests = append(t.Guests, f)
		}
	}
	c := *t
	c.Guests = append([]string(nil), t.Guests...)
	tinvMu.Unlock()
	return c, sendErr
}

// CloseTempInvite — закрити доступ зараз (картки в чатах друзів стануть «недоступне»).
func (a *App) CloseTempInvite(id string) error {
	tinvMu.Lock()
	t, ok := tinvs[id]
	tinvMu.Unlock()
	if ok {
		t.stop()
	}
	return nil
}

// TempInviteStates — живий стан карток у чаті (лаунчер опитує видимі картки).
func (a *App) TempInviteStates(addresses []string) ([]social.TempInviteState, error) {
	svc := a.socialService()
	if svc == nil {
		return nil, social.ErrNotLoggedIn
	}
	return svc.TempInviteStates(addresses)
}

// JoinTempInvite — гість: отримати квиток для адреси і ввімкнути його в P2P-гостьовому сервісі.
// Далі фронтенд запускає збірку з прямим підключенням до адреси.
func (a *App) JoinTempInvite(address string) error {
	svc := a.socialService()
	if svc == nil {
		return social.ErrNotLoggedIn
	}
	t, err := svc.TempInviteTicket(address)
	if err != nil {
		return errors.New("доступ закрито або вас не запрошено")
	}
	return a.p2pAPI.SetGuestTicket(t.Address, t.ID, t.Ticket)
}

// freeTempAddress — одна з трьох постійних «тимчасових» адрес акаунта, що зараз не хоститься.
func (a *App) freeTempAddress(accountID string) (string, string, error) {
	busy := map[string]bool{}
	for _, h := range a.p2pAPI.ListP2PHosts() {
		busy[h.Subdomain] = true
	}
	for i := 1; i <= 3; i++ {
		r, err := a.p2pAPI.ClaimP2PAddress("tmp"+strconv.Itoa(i), accountID, p2p.TempLabel)
		if err != nil {
			return "", "", err
		}
		if !busy[r.Subdomain] {
			return r.Subdomain, r.Address, nil
		}
	}
	return "", "", errors.New("одночасно можна відкрити не більше 3 тимчасових доступів")
}

// watchTempInvite: heartbeat 15 с; світ не відповідає 3 перевірки поспіль (15 с) або VPS
// каже «мертве» → закриваємо P2P-сесію і запрошення.
func (a *App) watchTempInvite(ctx context.Context, t *tempInvite) {
	defer func() {
		// Тимчасова адреса зникає разом із запрошенням: звільняємо її в lobby і забуваємо з книги
		// (ReleaseTempAddress сам зупиняє хост-сесію), а не лишаємо «збереженою».
		if err := a.p2pAPI.ReleaseTempAddress(t.Subdomain); err != nil {
			_ = a.p2pAPI.StopP2PHost(t.Subdomain)
		}
		if svc := a.socialService(); svc != nil {
			_ = svc.TempInviteClose(t.ID)
		}
		tinvMu.Lock()
		delete(tinvs, t.ID)
		tinvMu.Unlock()
		a.emit("tinvite:closed", t.ID)
	}()
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	fails, n := 0, 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(t.Meta.Port)), 2*time.Second)
		if err != nil {
			if fails++; fails >= 3 {
				return // світ закрили
			}
			continue
		}
		c.Close()
		fails = 0
		if n++; n%3 == 0 {
			svc := a.socialService()
			if svc == nil {
				continue
			}
			if alive, err := svc.TempInviteBeat(t.ID, a.p2pAPI.GetP2PHostStatus(t.Subdomain).PlayerCount); err == nil && !alive {
				return
			}
		}
	}
}
