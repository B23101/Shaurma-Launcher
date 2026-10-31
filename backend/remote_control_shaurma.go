package main

// remote_control_shaurma.go — склейка віддаленого керування серверами друзів з App.
//
// Уся логіка безпеки живе в пакетах (і має тести): remotectl (права власника), remotehost
// (Host/Client), remotechan (прямий зашифрований канал), social (кадри VPS і перевірка мандата).
// Тут — лише адаптери до серверного менеджера й соціальної служби та методи для фронтенду.

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"shaurma-launcher-backend/internal/crashguard"
	"shaurma-launcher-backend/internal/model"
	"shaurma-launcher-backend/internal/remotechan"
	"shaurma-launcher-backend/internal/remotectl"
	"shaurma-launcher-backend/internal/remotehost"
	"shaurma-launcher-backend/internal/remotepipe"
	"shaurma-launcher-backend/internal/servers"
	"shaurma-launcher-backend/internal/social"
)

var (
	errRemoteOff     = errors.New("remote: social service is not available")
	errRemoteTimeout = errors.New("remote: VPS did not answer in time")
	errRemoteTunnels = errors.New("remote: too many open tunnels")
)

// remoteMaxPipes — стеля одночасних тунелів (і встановлених, і тих, що ще в рукостисканні).
const remoteMaxPipes = 16

type remoteRT struct {
	once     sync.Once
	store    *remotectl.Store
	host     *remotehost.Host
	client   *remotehost.Client
	hostPriv ed25519.PrivateKey
	hostPub  string // base64, реєструється у VPS разом із серверами

	mu      sync.Mutex
	vpsPub  ed25519.PublicKey
	pin     social.KeyPin
	lists   map[string]social.RemoteOwnerList
	keyWarn bool

	// Тунелі через VPS (net.Conn для remotechan) і очікування мандатів p2p_open. Окремий mutex:
	// закриття тунелю викликає onClose, який бере pmu, тож тримати mu поруч не можна.
	pmu     sync.Mutex
	pipes   map[string]*remotepipe.Conn // "peer|tunnel"
	waiters map[string]chan social.P2PMandateFrame
}

// vpsKey — закріплений ключ VPS (nil, доки не підтверджений).
func (r *remoteRT) vpsKey() ed25519.PublicKey {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.vpsPub
}

func (a *App) remote() *remoteRT {
	a.remoteInit.Do(func() {
		rt := &remoteRT{lists: map[string]social.RemoteOwnerList{}, pipes: map[string]*remotepipe.Conn{}, waiters: map[string]chan social.P2PMandateFrame{}}
		dir := a.cfg.Dir()
		rt.store = remotectl.Open(dir)
		rt.store.SetNetworks(remoteNetworks{a}) // право на проксі → сервери його мережі
		rt.hostPriv, rt.hostPub = loadOrCreateHostKey(filepath.Join(dir, "remote_host_key"))
		keyFile := filepath.Join(dir, "remote_vps_key")
		rt.pin = social.KeyPin{
			Load: func() string { b, _ := os.ReadFile(keyFile); return strings.TrimSpace(string(b)) },
			Save: func(s string) { _ = os.WriteFile(keyFile, []byte(s), 0o600) },
		}
		if cur := rt.pin.Load(); cur != "" { // ключ VPS, закріплений раніше
			if raw, err := base64.StdEncoding.DecodeString(cur); err == nil && len(raw) == ed25519.PublicKeySize {
				rt.vpsPub = raw
			}
		}
		svc := func() *social.Service { return a.socialService() }
		emit := func(event string, payload any) { a.emit(event, payload) } // a.emit — variadic
		rt.host = remotehost.NewHost(remotehost.HostDeps{
			Self:    func() string { return svc().RemoteSelf() },
			VPSKey:  rt.vpsKey,
			Crypto:  remoteCryptoAdapter{svc},
			Net:     remoteNetAdapter{svc},
			Servers: remoteServersAdapter{a},
			Friends: remoteFriendsAdapter{svc},
			Store:   rt.store,
			Emit:    emit,
		})
		rt.client = remotehost.NewClient(remoteCryptoAdapter{svc}, remoteNetAdapter{svc}, emit)
		rt.client.Now = time.Now
		a.remoteRT = rt
		go func() { // пачки рядків консолі + перевірка, що права живі
			defer crashguard.Recover("remote_control_shaurma.go:flush")
			for range time.Tick(400 * time.Millisecond) {
				rt.host.Flush()
			}
		}()
	})
	return a.remoteRT
}

func loadOrCreateHostKey(path string) (ed25519.PrivateKey, string) {
	if b, err := os.ReadFile(path); err == nil {
		if raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(b))); err == nil && len(raw) == ed25519.SeedSize {
			k := ed25519.NewKeyFromSeed(raw)
			return k, base64.StdEncoding.EncodeToString(k.Public().(ed25519.PublicKey))
		}
	}
	seed := make([]byte, ed25519.SeedSize)
	if _, err := rand.Read(seed); err != nil {
		panic("remote: crypto/rand: " + err.Error())
	}
	_ = os.WriteFile(path, []byte(base64.StdEncoding.EncodeToString(seed)), 0o600)
	k := ed25519.NewKeyFromSeed(seed)
	return k, base64.StdEncoding.EncodeToString(k.Public().(ed25519.PublicKey))
}

// ── адаптери ───────────────────────────────────────────────────────────────

type remoteCryptoAdapter struct{ svc func() *social.Service }

func (c remoteCryptoAdapter) EncryptFor(id, text string) (string, string, error) {
	cc := c.svc().RemoteCrypto()
	if cc == nil {
		return "", "", errRemoteOff
	}
	return cc.EncryptFor(id, text)
}
func (c remoteCryptoAdapter) DecryptFrom(id, ct, n string) (string, error) {
	cc := c.svc().RemoteCrypto()
	if cc == nil {
		return "", errRemoteOff
	}
	return cc.DecryptFrom(id, ct, n)
}

type remoteNetAdapter struct{ svc func() *social.Service }

func (n remoteNetAdapter) SendCmd(reqID, owner, srv, op, sid, ct, nonce string) error {
	return n.svc().SendRemoteCmd(reqID, owner, srv, op, sid, ct, nonce)
}
func (n remoteNetAdapter) SendReply(reqID, to, ct, nonce string) error {
	return n.svc().SendRemoteReply(reqID, to, ct, nonce)
}
func (n remoteNetAdapter) SendStream(sid, to, ct, nonce string) error {
	return n.svc().SendRemoteStream(sid, to, ct, nonce)
}
func (n remoteNetAdapter) SendP2POpen(reqID, owner, srv, eph string) error {
	return n.svc().SendRemoteP2POpen(reqID, owner, srv, eph)
}
func (n remoteNetAdapter) SendPipe(tunnel, to string, data []byte) error {
	return n.svc().SendRemotePipe(tunnel, to, data)
}

type remoteFriendsAdapter struct{ svc func() *social.Service }

func (f remoteFriendsAdapter) Friend(id string) (bool, int64) { return f.svc().RemoteFriend(id) }
func (f remoteFriendsAdapter) Nick(id string) string          { return f.svc().RemoteNick(id) }

type remoteServersAdapter struct{ a *App }

func (s remoteServersAdapter) Exists(id string) (string, bool) {
	v, err := s.a.srvAPI.GetServer(id)
	if err != nil {
		return "", false
	}
	return v.Name, true
}
func (s remoteServersAdapter) State(id string) string { return s.a.srvAPI.ServerStatus(id).Status }
func (s remoteServersAdapter) Players(id string) int  { return len(s.a.srvAPI.OnlinePlayers(id)) }
func (s remoteServersAdapter) Start(id string) error  { return s.a.StartServer(id) }

// P2P — публічна P2P-адреса сервера (та сама, що вмикає власник у вкладці «Мережа»).
func (s remoteServersAdapter) P2P(id string, on bool) error {
	if on {
		_, err := s.a.P2PStartForServer(id)
		return err
	}
	sub := s.a.p2pSubdomainFor(id)
	if sub == "" {
		return nil
	}
	err := s.a.StopP2PHost(sub)
	return err
}
func (s remoteServersAdapter) Stop(id string) error { return s.a.srvAPI.StopServer(id, true) }
func (s remoteServersAdapter) SendCommand(id, cmd string) error {
	return s.a.srvAPI.SendCommand(id, cmd)
}
func (s remoteServersAdapter) Tail(id string, n int) []remotehost.Line {
	snap := s.a.srvAPI.Manager().GetServerConsoleSnapshot(id)
	if len(snap) > n {
		snap = snap[len(snap)-n:]
	}
	out := make([]remotehost.Line, 0, len(snap))
	for _, l := range snap {
		out = append(out, toRemoteLine(l))
	}
	return out
}

func toRemoteLine(l model.ConsoleLine) remotehost.Line {
	return remotehost.Line{ID: l.ID, T: l.Time, L: l.Level, X: l.Text}
}

// ── хуки соціальної служби ────────────────────────────────────────────────

// remoteFlat — усі доступні мені сервери друзів одним списком, БЕЗ тих, що насправді живуть у цьому
// ж лаунчері (доступ видав інший мій акаунт Shaurma): такий сервер уже є в бібліотеці як свій,
// його можна просто запустити, а не керувати ним «через друга».
func (a *App) remoteFlat(r *remoteRT) []social.RemoteServer {
	r.mu.Lock()
	all := []social.RemoteServer{}
	for _, l := range r.lists {
		all = append(all, l.Servers...)
	}
	r.mu.Unlock()
	self := a.socialService().RemoteSelf()
	flat := make([]social.RemoteServer, 0, len(all)) // не nil: nil-слайс у JS стає null
	for _, s := range all {
		if r.store.GrantedOnThisLauncher(s.OwnerID, self, s.ID) {
			if _, err := a.srvAPI.GetServer(s.ID); err == nil {
				continue
			}
		}
		flat = append(flat, s)
	}
	return flat
}

func (a *App) remoteHooks() *social.RemoteHooks {
	rt := func() *remoteRT { return a.remote() }
	emitServers := func() { a.emit("remote:servers", a.remoteFlat(rt())) }
	return &social.RemoteHooks{
		OnSnapshot: func(lists []social.RemoteOwnerList) {
			r := rt()
			r.mu.Lock()
			r.lists = map[string]social.RemoteOwnerList{}
			for _, l := range lists {
				r.lists[l.Owner] = l
			}
			r.mu.Unlock()
			emitServers()
		},
		OnList: func(l social.RemoteOwnerList) {
			r := rt()
			r.mu.Lock()
			if len(l.Servers) == 0 {
				delete(r.lists, l.Owner)
			} else {
				r.lists[l.Owner] = l
			}
			r.mu.Unlock()
			emitServers()
		},
		OnCmd:    func(in social.RemoteIncoming) { go rt().host.Handle(in) },
		OnResult: func(f social.RemoteResultFrame) { rt().client.HandleResult(f) },
		OnStream: func(f social.RemoteStreamFrame) { rt().client.HandleStream(f) },
		OnStreamEnd: func(sid, reason string) {
			rt().client.HandleStreamEnd(sid, reason)
			rt().host.EndSession(sid)
		},
		// Мандат VPS на пряме підключення: віддаємо тому, хто його чекає в remoteConnectSecure.
		OnP2PMandate: func(f social.P2PMandateFrame) {
			r := rt()
			r.pmu.Lock()
			w := r.waiters[f.ReqID]
			r.pmu.Unlock()
			if w != nil {
				select {
				case w <- f:
				default:
				}
			}
		},
		// Шматок тунелю: або мій (я друг, що дзвонить — тунель уже відкритий), або новий для мене
		// (я власник, до мене дзвонять).
		OnPipe:    func(f social.PipeFrame) { a.remotePipeIn(f) },
		OnPipeEnd: func(tunnel, _ string) { rt().pipeEnd(tunnel) },
		OnVPSKey: func(b64 string) {
			r := rt()
			pub, err := r.pin.Check(b64)
			r.mu.Lock()
			defer r.mu.Unlock()
			if err != nil {
				if !r.keyWarn {
					r.keyWarn = true
					// Ключ VPS змінився: віддалені команди відхиляються, доки власник не підтвердить.
					a.emit("remote:key_changed", map[string]any{"message": "Ключ VPS змінився — віддалене керування призупинено"})
				}
				return
			}
			r.vpsPub = pub
		},
	}
}

// ── прямий зашифрований канал через тунель VPS ────────────────────────────
//
// VPS лише пересилає шматки байтів тунелю (rs_pipe). Із них remotepipe робить net.Conn, поверх
// якого remotechan виконує рукостискання (Dial — у друга, Accept — у власника) і шифрує кадри.
// Id тунелю = reqId запиту rs_p2p_open; VPS реєструє тунель до видачі мандата.

func newTunnelID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic("remote: crypto/rand: " + err.Error())
	}
	return "t" + hex.EncodeToString(b) // 25 символів: підходить під ^[A-Za-z0-9_-]{8,64}$ і на VPS
}

// remoteSendPipe відправляє шматок тунелю; коротко перечікує, якщо черга WS тимчасово повна.
func (a *App) remoteSendPipe(tunnel, to string, data []byte) error {
	var err error
	for i := 0; i < 20; i++ {
		err = a.socialService().SendRemotePipe(tunnel, to, data)
		if !errors.Is(err, social.ErrQueueFull) {
			return err
		}
		time.Sleep(100 * time.Millisecond)
	}
	return err
}

// remoteNewPipe створює й реєструє тунель із peer. false — дубль або забагато тунелів.
func (a *App) remoteNewPipe(rt *remoteRT, peer, tunnel string) (*remotepipe.Conn, bool) {
	key := peer + "|" + tunnel
	rt.pmu.Lock()
	defer rt.pmu.Unlock()
	if _, dup := rt.pipes[key]; dup || len(rt.pipes) >= remoteMaxPipes {
		return nil, false
	}
	pc := remotepipe.New(tunnel, peer, social.MaxPipeChunk, a.remoteSendPipe, func(c *remotepipe.Conn) {
		rt.pmu.Lock()
		if rt.pipes[key] == c {
			delete(rt.pipes, key)
		}
		rt.pmu.Unlock()
	})
	rt.pipes[key] = pc
	return pc, true
}

// remotePipeIn — вхідний шматок тунелю. Невідомий тунель може відкрити лише друг у власника, що
// щось роздає; усе інше (мандат, ключі, список друзів) перевіряє remotechan.Accept.
func (a *App) remotePipeIn(f social.PipeFrame) {
	rt := a.remote()
	key := f.From + "|" + f.Tunnel
	rt.pmu.Lock()
	pc := rt.pipes[key]
	rt.pmu.Unlock()
	if pc == nil {
		if len(rt.store.Shared(a.socialService().RemoteSelf())) == 0 {
			return
		}
		var ok bool
		if pc, ok = a.remoteNewPipe(rt, f.From, f.Tunnel); !ok {
			return
		}
		go a.remoteServePipe(rt, pc)
	}
	pc.Push(f.Data) // поза pmu: Push при переповненні закриває тунель → onClose бере pmu
}

// remoteServePipe — сторона власника: рукостискання, далі обслуговування команд друга.
func (a *App) remoteServePipe(rt *remoteRT, pc *remotepipe.Conn) {
	defer crashguard.Recover("remote_control_shaurma.go:pipe")
	defer pc.Close()
	ch, who, md, err := remotechan.Accept(pc, remotechan.AcceptParams{
		Self:     a.socialService().RemoteSelf(),
		VPSKey:   rt.vpsKey,
		HostKey:  rt.hostPriv,
		IsFriend: func(id string) bool { ok, _ := a.socialService().RemoteFriend(id); return ok },
	})
	if err != nil || who != pc.Peer() {
		return
	}
	rt.host.ServeChannel(ch, who, md) // блокує до закриття каналу
}

// pipeEnd закриває тунелі з цим id (VPS повідомив про завершення: права зникли, сплив термін…).
func (r *remoteRT) pipeEnd(tunnel string) {
	r.pmu.Lock()
	var victims []*remotepipe.Conn
	for _, pc := range r.pipes {
		if pc.Tunnel() == tunnel {
			victims = append(victims, pc)
		}
	}
	r.pmu.Unlock()
	for _, pc := range victims {
		_ = pc.Close()
	}
}

// remoteConnectSecure — сторона друга: мандат від VPS → рукостискання з власником → UseChannel.
// Повертає помилку, якщо прямий канал не вдалося встановити (консоль тоді йде через VPS-релей).
func (a *App) remoteConnectSecure(ownerID, serverID string, timeout time.Duration) error {
	rt := a.remote()
	if rt.client.HasChannel(ownerID, serverID) {
		return nil
	}
	svc := a.socialService()
	eph, err := remotechan.NewEphemeral()
	if err != nil {
		return err
	}
	tunnel := newTunnelID()
	wait := make(chan social.P2PMandateFrame, 1)
	rt.pmu.Lock()
	rt.waiters[tunnel] = wait
	rt.pmu.Unlock()
	defer func() {
		rt.pmu.Lock()
		delete(rt.waiters, tunnel)
		rt.pmu.Unlock()
	}()
	if err := svc.SendRemoteP2POpen(tunnel, ownerID, serverID, eph.Hash()); err != nil {
		return err
	}
	var f social.P2PMandateFrame
	select {
	case f = <-wait:
	case <-time.After(timeout):
		return errRemoteTimeout
	}
	if f.Code != "" || f.Mandate == nil || f.Owner != ownerID {
		return fmt.Errorf("remote: direct channel refused: %q", f.Code)
	}
	pc, ok := a.remoteNewPipe(rt, ownerID, tunnel)
	if !ok {
		return errRemoteTunnels
	}
	ch, err := remotechan.Dial(pc, remotechan.DialParams{
		Self: svc.RemoteSelf(), Owner: ownerID, ServerID: serverID,
		Mandate: *f.Mandate, Ephemeral: eph, VPSKey: rt.vpsKey(),
	})
	if err != nil {
		_ = pc.Close()
		return err
	}
	go func() {
		defer crashguard.Recover("remote_control_shaurma.go:channel")
		rt.client.UseChannel(ownerID, serverID, ch) // блокує до закриття каналу
		_ = pc.Close()
	}()
	for end := time.Now().Add(time.Second); !rt.client.HasChannel(ownerID, serverID); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(end) {
			_ = pc.Close()
			return errors.New("remote: channel did not register")
		}
	}
	return nil
}

// remoteSyncShares публікує на VPS, що я дозволяю друзям на моїх серверах (і мій ключ каналу).
func (a *App) remoteSyncShares() {
	defer crashguard.Recover("remote_control_shaurma.go:sync")
	rt := a.remote()
	svc := a.socialService()
	self := svc.RemoteSelf()
	var out []social.RemoteShare
	for _, sv := range rt.store.Shared(self) {
		v, err := a.srvAPI.GetServer(sv.ServerID)
		if err != nil {
			continue
		}
		sh := social.RemoteShare{ServerID: sv.ServerID, Name: v.Name, State: a.srvAPI.ServerStatus(sv.ServerID).Status, Core: v.CoreLabel, HostKey: rt.hostPub}
		// Вигляд — той самий, що в бібліотеці власника: колір, пресет-іконка, мініатюра картинки.
		sh.Color, sh.Icon = v.Color, v.Icon
		if img := v.LauncherIconData; img != "" {
			sh.Img = remoteIconThumb(img)
		} else if v.Icon == "" && v.IconData != "" {
			sh.Img = remoteIconThumb(v.IconData)
		}
		// Мережа проксі: друг бачить сервери мережі на сторінці проксі.
		if parents := a.srvAPI.NetworkParents(sv.ServerID); len(parents) > 0 {
			sh.Network = parents[0]
		}
		if sub := a.p2pSubdomainFor(sv.ServerID); sub != "" {
			if st := a.GetP2PHostStatus(sub); st.Active {
				sh.P2P, sh.P2PAddr = true, st.Address
			}
		}
		for _, g := range sv.Grants {
			sh.Grants = append(sh.Grants, social.RemoteShareGrant{AccountID: g.AccountID, Start: g.Start, Stop: g.Stop, Console: g.Console, Exec: g.Exec})
		}
		out = append(out, sh)
	}
	svc.SetRemoteShares(out)
	if len(out) == 0 {
		rt.host.EndAll()
	}
}

// remoteNetworks — мережі проксі для remotectl: право «Сервери мережі» на
// проксі поширюється на його сервери.
type remoteNetworks struct{ a *App }

func (n remoteNetworks) Parents(serverID string) []string { return n.a.srvAPI.NetworkParents(serverID) }
func (n remoteNetworks) Members(proxyID string) []string  { return n.a.srvAPI.NetworkMemberIDs(proxyID) }

// remoteOnP2PChanged — P2P-адресу сервера піднято чи зупинено: друзі бачать новий стан.
func (a *App) remoteOnP2PChanged() {
	if a.remoteRT == nil {
		return
	}
	go a.remoteSyncShares()
}

// remoteOnNetworkChanged — склад мережі змінився: друзі з правом на проксі
// мають одразу бачити (і лише ті) сервери мережі.
func (a *App) remoteOnNetworkChanged() {
	if a.remoteRT == nil {
		return
	}
	go a.remoteSyncShares()
}

// remoteOnLine / remoteOnStatus — викликаються з колбеків серверного менеджера (app.go).
func (a *App) remoteOnLine(serverID string, l model.ConsoleLine) {
	if a.remoteRT == nil { // ще не ініціалізовано — ніхто не підписаний
		return
	}
	a.remoteRT.host.OnLine(serverID, toRemoteLine(l))
}

func (a *App) remoteOnStatus(serverID string, st servers.ServerStatus) {
	if a.remoteRT == nil {
		return
	}
	a.socialService().SetRemoteShareState(serverID, st.Status)
}

// remoteOnIdentityChanged — змінився (або вийшов) акаунт Shaurma. Сервери друзів, команди в дорозі
// й відкриті консолі належали ІНШІЙ особі: забуваємо їх, щоб новий акаунт не бачив чужого доступу.
func (a *App) remoteOnIdentityChanged() {
	rt := a.remoteRT
	if rt == nil {
		return
	}
	rt.mu.Lock()
	rt.lists = map[string]social.RemoteOwnerList{}
	rt.mu.Unlock()
	rt.client.Reset()
	rt.host.ForgetAll()
	rt.pmu.Lock()
	pipes := make([]*remotepipe.Conn, 0, len(rt.pipes))
	for _, pc := range rt.pipes {
		pipes = append(pipes, pc)
	}
	rt.pmu.Unlock()
	for _, pc := range pipes {
		_ = pc.Close() // onClose бере pmu — тому поза замком
	}
	a.emit("remote:servers", []social.RemoteServer{})
}

// remoteOnServerDeleted / remoteOnServerRenamed — права друзів ідуть за сервером: видалений сервер
// забуваємо (і знімаємо з VPS), перейменований (id = ім'я теки) — переносимо на новий id.
func (a *App) remoteOnServerDeleted(serverID string) {
	if a.remote().store.ForgetServer(serverID) {
		go a.remoteSyncShares()
	}
}

func (a *App) remoteOnServerRenamed(oldID, newID string) {
	if a.remote().store.RenameServer(oldID, newID) {
		go a.remoteSyncShares()
	}
}

// remoteOnFriendRemoved — друга видалено: забираємо всі його права й закриваємо його консолі.
func (a *App) remoteOnFriendRemoved(friendID string) {
	rt := a.remote()
	if n, _ := rt.store.RemoveFriend(a.socialService().RemoteSelf(), friendID); n > 0 {
		go a.remoteSyncShares()
	}
}

// ── API для фронтенду: ВЛАСНИК ────────────────────────────────────────────

func (a *App) GetRemoteAccess() remotectl.View {
	return a.remote().store.View(a.socialService().RemoteSelf())
}

func (a *App) SetRemoteGrant(serverID, friendID string, start, stop, console, exec, network bool) error {
	rt, self := a.remote(), a.socialService().RemoteSelf()
	if ok, _ := a.socialService().RemoteFriend(friendID); !ok {
		return errors.New("це не ваш друг")
	}
	if _, err := a.srvAPI.GetServer(serverID); err != nil {
		return err
	}
	if err := rt.store.SetGrantFull(self, serverID, friendID, remotectl.Rights{Start: start, Stop: stop, Console: console, Exec: exec, Network: network}, time.Now().UnixMilli()); err != nil {
		return err
	}
	a.remoteSyncShares()
	return nil
}

func (a *App) SetRemoteAllowStopWithPlayers(serverID string, v bool) error {
	return a.remote().store.SetAllowStopWithPlayers(a.socialService().RemoteSelf(), serverID, v)
}

func (a *App) SetRemoteAllowPrivileged(serverID string, v bool) error {
	return a.remote().store.SetAllowPrivileged(a.socialService().RemoteSelf(), serverID, v)
}

// SetRemotePaused — аварійний вимикач: усі віддалені дії блокуються, сесії консолі закриваються.
func (a *App) SetRemotePaused(v bool) error {
	rt := a.remote()
	if err := rt.store.SetPaused(a.socialService().RemoteSelf(), v); err != nil {
		return err
	}
	if v {
		rt.host.EndAll()
	}
	a.remoteSyncShares()
	return nil
}

func (a *App) GetRemoteAudit(serverID string, limit int) []remotectl.AuditEntry {
	return a.remote().store.Audit(a.socialService().RemoteSelf(), serverID, limit)
}

// ── API для фронтенду: ДРУГ ───────────────────────────────────────────────

func (a *App) GetRemoteServers() []social.RemoteServer {
	return a.remoteFlat(a.remote())
}

func (a *App) RemoteStartServer(ownerID, serverID string) (string, error) {
	return a.remote().client.Command(ownerID, serverID, social.RemoteOpStart)
}

// RemoteSetP2P вмикає/вимикає публічну P2P-адресу сервера друга (права Старт/Стоп).
func (a *App) RemoteSetP2P(ownerID, serverID string, on bool) (string, error) {
	op := social.RemoteOpP2POff
	if on {
		op = social.RemoteOpP2POn
	}
	return a.remote().client.Command(ownerID, serverID, op)
}

func (a *App) RemoteStopServer(ownerID, serverID string) (string, error) {
	return a.remote().client.Command(ownerID, serverID, social.RemoteOpStop)
}

// RemoteOpenConsole повертає id сесії; рядки приходять подією "remote:console".
// Спершу пробуємо наскрізно зашифрований тунель (вмісту не бачить навіть VPS); не вийшло — консоль
// працює через VPS-релей (його авторизація та сама), а UI про це чесно попереджає подією "remote:secure".
func (a *App) RemoteOpenConsole(ownerID, serverID string) (string, error) {
	rt := a.remote()
	secure := rt.client.HasChannel(ownerID, serverID)
	if !secure {
		secure = a.remoteConnectSecure(ownerID, serverID, 6*time.Second) == nil
	}
	a.emit("remote:secure", map[string]any{"ownerId": ownerID, "serverId": serverID, "secure": secure})
	sid, _, err := rt.client.OpenConsole(ownerID, serverID)
	return sid, err
}

func (a *App) RemoteConsoleExec(sid, cmd string) error {
	_, err := a.remote().client.Exec(sid, cmd)
	return err
}

func (a *App) RemoteCloseConsole(sid string) error { return a.remote().client.CloseConsole(sid) }
