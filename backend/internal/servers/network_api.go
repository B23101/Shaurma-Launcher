// network_api.go — API мережі Velocity для фронтенду.
//
// Проксі — центр своєї мережі: конфіг у <тека проксі>/.shaurma/network.json
// (network.go), а velocity.toml і конфіги backend-ів лаунчер генерує з нього
// (applyNetwork). Звідси ж — ланцюги запуску (проксі ↔ сервери), синхронізація
// портів і назв, діагностика й автовиправлення.
package servers

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// NetworkMemberView — рядок списку мережі у UI: сервер + його роль у мережі.
type NetworkMemberView struct {
	ServerID  string `json:"serverId"`
	Name      string `json:"name"`
	Core      string `json:"core"`
	MCVersion string `json:"mcVersion"`
	Port      int    `json:"port"`
	Running   bool   `json:"running"`
	Status    string `json:"status"`
	// Роль у мережі (редагується у UI):
	InNetwork     bool   `json:"inNetwork"`
	Order         int    `json:"order"`
	DelaySec      int    `json:"delaySec"`
	Alias         string `json:"alias"`
	AutoStart     bool   `json:"autoStart"`
	StartsNetwork bool   `json:"startsNetwork"`
	InTry         bool   `json:"inTry"`
	ForcedHost    string `json:"forcedHost"`
	// OtherNetwork — назва проксі, в мережі якого сервер уже є (сервер
	// може належати лише одній мережі: у кожного проксі свій секрет).
	OtherNetwork string `json:"otherNetwork,omitempty"`
	// Forwarding — чим backend приймає дані гравців від проксі:
	// paper | fabric-proxy | pcf | "" (не вміє).
	Forwarding string `json:"forwarding"`
}

// NetworkSettings — налаштування мережі та самого проксі.
type NetworkSettings struct {
	ProxyPort            int           `json:"proxyPort"`
	Proxy                ProxySettings `json:"proxy"`
	StopMembersWithProxy bool          `json:"stopMembersWithProxy"`
	LockBackends         bool          `json:"lockBackends"`
}

// NetworkView — усе, що треба UI сторінки мережі проксі.
type NetworkView struct {
	ProxyServerID string              `json:"proxyServerId"`
	ProxyName     string              `json:"proxyName"`
	ProxyPort     int                 `json:"proxyPort"`
	ProxyRunning  bool                `json:"proxyRunning"`
	ProxyStatus   string              `json:"proxyStatus"`
	NetworkSaved  bool                `json:"networkSaved"`
	Members       []NetworkMemberView `json:"members"`  // інші сервери (кандидати + учасники)
	External      []ExternalServer    `json:"external"` // сервери мережі поза лаунчером
	Settings      NetworkSettings     `json:"settings"`
}

// NetworkIssue — один пункт діагностики мережі.
type NetworkIssue struct {
	Level    string `json:"level"` // error | warn | info | ok
	ServerID string `json:"serverId,omitempty"`
	Title    string `json:"title"`
	Detail   string `json:"detail,omitempty"`
	// Fixable — «Виправити» (NetworkRepair) це прибере.
	Fixable bool `json:"fixable,omitempty"`
}

// ── Допоміжне ───────────────────────────────────────────────────────────

func (a *API) proxyServer(id string) (*Server, error) {
	proxy, err := a.store.Get(id)
	if err != nil {
		return nil, err
	}
	if proxy.Core != CoreVelocity {
		return nil, fmt.Errorf("сервер %s не є Velocity-проксі", proxy.Name)
	}
	return proxy, nil
}

// loadNetwork — збережена мережа проксі; якщо її ще нема, а проксі вже має
// velocity.toml (готова мережа, додана з папки) — імпортує її з файлу, щоб
// лаунчер не перейменовував сервери й не губив налаштування. saved=false —
// мережі не було й нема що імпортувати (нова з налаштуваннями за
// замовчуванням).
func (a *API) loadNetwork(proxy *Server) (*Network, bool) {
	if net, err := LoadNetwork(proxy.Dir); err == nil {
		return net, true
	}
	if net, ok := a.importNetwork(proxy); ok {
		if err := SaveNetwork(proxy.Dir, net); err != nil {
			log.Printf("[network] імпорт %s: %v", proxy.Name, err)
		}
		return net, true
	}
	return NewNetwork(proxy), false
}

// importNetwork будує мережу з наявного velocity.toml: сервери з [servers]
// на 127.0.0.1 зіставляються з серверами лаунчера за портом (ім'я в мережі
// лишається те, що у файлі), решта стає зовнішніми записами.
func (a *API) importNetwork(proxy *Server) (*Network, bool) {
	data, err := os.ReadFile(filepath.Join(proxy.Dir, "velocity.toml"))
	if err != nil {
		return nil, false
	}
	d := parseTOMLDoc(string(data))
	entries, try := d.velocityServers()
	net := NewNetwork(proxy)
	net.Proxy = readProxySettings(d)
	inTry := map[string]bool{}
	for _, t := range try {
		inTry[t] = true
	}
	hosts := readForcedHosts(d)

	byPort := map[int]*Server{}
	for _, srv := range a.store.LoadAll() {
		if srv.Core != CoreVelocity {
			byPort[a.effectiveServerPort(srv)] = srv
		}
	}
	used := map[string]bool{}
	locked := true
	order := 0
	for _, e := range entries {
		alias, addr := e[0], e[1]
		if velocityDefaultExamples[alias] == addr {
			continue // приклад, який Velocity пише в новий конфіг
		}
		host, portStr, _ := strings.Cut(addr, ":")
		port, _ := strconv.Atoi(portStr)
		srv := byPort[port]
		if (host == "127.0.0.1" || host == "localhost") && srv != nil && !used[srv.ID] {
			used[srv.ID] = true
			order++
			net.Servers = append(net.Servers, NetworkServer{
				ServerID: srv.ID, Order: order, Alias: alias, AutoStart: true,
				InTry: inTry[alias], ForcedHost: hosts[alias],
			})
			if ip := parsePropertiesFile(filepath.Join(srv.Dir, "server.properties"))["server-ip"]; ip != "127.0.0.1" && ip != "localhost" {
				locked = false
			}
			continue
		}
		net.External = append(net.External, ExternalServer{Alias: alias, Address: addr, InTry: inTry[alias], ForcedHost: hosts[alias]})
	}
	if order == 0 && len(net.External) == 0 {
		return nil, false // порожній/дефолтний конфіг — імпортувати нічого
	}
	net.LockBackends = locked && order > 0
	log.Printf("[network] %s: імпортовано з velocity.toml (%d серверів лаунчера, %d зовнішніх)", proxy.Name, order, len(net.External))
	return net, true
}

// proxyNetwork — проксі з його мережею.
type proxyNetwork struct {
	proxy *Server
	net   *Network
}

// networksOf — мережі, в яких сервер є учасником.
func (a *API) networksOf(serverID string) []proxyNetwork {
	var out []proxyNetwork
	for _, srv := range a.store.LoadAll() {
		if srv.Core != CoreVelocity || srv.ID == serverID {
			continue
		}
		net, err := LoadNetwork(srv.Dir)
		if err != nil {
			continue
		}
		if _, ok := net.Member(serverID); ok {
			out = append(out, proxyNetwork{srv, net})
		}
	}
	return out
}

// netLocks — один запуск мережі одночасно (ланцюги запуску можуть
// спрацювати з кількох місць).
var netLocks sync.Map

func netLock(proxyID string) *sync.Mutex {
	m, _ := netLocks.LoadOrStore(proxyID, &sync.Mutex{})
	return m.(*sync.Mutex)
}

// SetNetworkNotifyCallback — куди слати повідомлення про фонові ланцюги
// запуску мережі (app.go → подія "server:network-event").
func (a *API) SetNetworkNotifyCallback(fn func(proxyID, serverID, message string)) {
	a.netNotifyMu.Lock()
	a.netNotify = fn
	a.netNotifyMu.Unlock()
}

func (a *API) notifyNetwork(proxyID, serverID, msg string) {
	log.Printf("[network] %s/%s: %s", proxyID, serverID, msg)
	a.netNotifyMu.RLock()
	fn := a.netNotify
	a.netNotifyMu.RUnlock()
	if fn != nil {
		fn(proxyID, serverID, msg)
	}
}

// ── Перегляд ─────────────────────────────────────────────────────────────

// NetworkGetView повертає стан мережі проксі: учасників, кандидатів і
// налаштування.
func (a *API) NetworkGetView(proxyServerID string) (NetworkView, error) {
	proxy, err := a.proxyServer(proxyServerID)
	if err != nil {
		return NetworkView{}, err
	}
	net, saved := a.loadNetwork(proxy)
	view := NetworkView{
		ProxyServerID: proxy.ID,
		ProxyName:     proxy.Name,
		ProxyPort:     proxy.Port,
		ProxyRunning:  a.manager.IsRunning(proxy.ID),
		ProxyStatus:   a.manager.GetStatus(proxy.ID).Status,
		NetworkSaved:  saved,
		Members:       []NetworkMemberView{},
		External:      append([]ExternalServer{}, net.External...),
		Settings: NetworkSettings{
			ProxyPort: proxy.Port, Proxy: net.Proxy,
			StopMembersWithProxy: net.StopMembersWithProxy, LockBackends: net.LockBackends,
		},
	}

	// В якій мережі вже є кожен сервер (крім цієї).
	otherNet := map[string]string{}
	all := a.store.LoadAll()
	for _, srv := range all {
		if srv.Core != CoreVelocity || srv.ID == proxy.ID {
			continue
		}
		if n, err := LoadNetwork(srv.Dir); err == nil {
			for _, m := range n.Members() {
				otherNet[m.ServerID] = srv.Name
			}
		}
	}

	ids := make([]string, 0, len(all))
	for id, srv := range all {
		if id != proxy.ID && srv.Core != CoreVelocity {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		srv := all[id]
		ns, in := net.Member(id)
		mv := NetworkMemberView{
			ServerID: srv.ID, Name: srv.Name, Core: srv.Core, MCVersion: srv.MCVersion,
			Port:      a.effectiveServerPort(srv),
			Running:   a.manager.IsRunning(srv.ID),
			Status:    a.manager.GetStatus(srv.ID).Status,
			InNetwork: in, Order: ns.Order, DelaySec: ns.DelaySec,
			Alias: ns.Alias, AutoStart: ns.AutoStart, StartsNetwork: ns.StartsNetwork,
			InTry: ns.InTry, ForcedHost: ns.ForcedHost,
			OtherNetwork: otherNet[id],
			Forwarding:   backendForwarding(srv),
		}
		if !in {
			mv.Alias = AliasFromName(srv.Name)
			mv.AutoStart, mv.InTry = true, true
		}
		view.Members = append(view.Members, mv)
	}
	// Учасники — першими, у порядку мережі.
	sort.SliceStable(view.Members, func(i, j int) bool {
		a, b := view.Members[i], view.Members[j]
		if a.InNetwork != b.InNetwork {
			return a.InNetwork
		}
		return a.InNetwork && a.Order < b.Order
	})
	return view, nil
}

// ── Збереження ───────────────────────────────────────────────────────────

// NetworkSave зберігає склад мережі (усі сервери з ролями, як їх
// редагував UI). Налаштування проксі не змінюються (NetworkSaveSettings).
func (a *API) NetworkSave(proxyServerID string, members []NetworkMemberView) error {
	proxy, err := a.proxyServer(proxyServerID)
	if err != nil {
		return err
	}
	net, _ := a.loadNetwork(proxy)

	var in []NetworkMemberView
	for _, m := range members {
		if m.InNetwork {
			in = append(in, m)
		}
	}
	sort.SliceStable(in, func(i, j int) bool { return in[i].Order < in[j].Order })

	taken := map[string]bool{}
	ports := map[int]string{}
	servers := []NetworkServer{{ServerID: proxy.ID, IsProxy: true}}
	for i, m := range in {
		srv, err := a.store.Get(m.ServerID)
		if err != nil {
			continue // видалений сервер просто випадає з мережі
		}
		if srv.Core == CoreVelocity {
			return fmt.Errorf("у мережу не можна додати інший проксі: %s", srv.Name)
		}
		for _, other := range a.networksOf(srv.ID) {
			if other.proxy.ID != proxy.ID {
				return fmt.Errorf("«%s» уже в мережі «%s» — сервер може належати лише одній мережі", srv.Name, other.proxy.Name)
			}
		}
		port := a.effectiveServerPort(srv)
		if port == proxy.Port {
			return fmt.Errorf("порт «%s» (%d) збігається з портом проксі — змініть порт сервера", srv.Name, port)
		}
		if other, dup := ports[port]; dup {
			return fmt.Errorf("«%s» і «%s» мають однаковий порт %d", other, srv.Name, port)
		}
		ports[port] = srv.Name

		alias := strings.TrimSpace(m.Alias)
		if alias == "" {
			alias = AliasFromName(srv.Name)
		} else if !ValidAlias(alias) {
			return fmt.Errorf("ім'я «%s» для «%s»: лише латиниця, цифри, «-» і «_», до 32 символів", m.Alias, srv.Name)
		}
		for _, ex := range net.External {
			if strings.EqualFold(ex.Alias, alias) {
				return fmt.Errorf("ім'я «%s» уже має зовнішній сервер мережі", alias)
			}
		}
		if taken[alias] && strings.TrimSpace(m.Alias) != "" {
			return fmt.Errorf("ім'я «%s» уже має інший сервер мережі", alias)
		}
		alias = uniqueAlias(alias, taken)

		servers = append(servers, NetworkServer{
			ServerID: srv.ID, Order: i + 1, DelaySec: max(0, m.DelaySec),
			Alias: alias, AutoStart: m.AutoStart, StartsNetwork: m.StartsNetwork,
			InTry: m.InTry, ForcedHost: strings.TrimSpace(m.ForcedHost),
		})
	}
	net.Servers = servers
	if err := SaveNetwork(proxy.Dir, net); err != nil {
		return err
	}
	return a.applyNetwork(proxy, net)
}

// linkExternals перетворює записи «поза лаунчером», що насправді вказують
// на сервер лаунчера (127.0.0.1:його порт), на повноцінних учасників мережі
// з тим самим ім'ям: тоді зміна порту сервера одразу потрапляє в проксі.
// Повертає, скільки записів прив'язано.
func (a *API) linkExternals(proxy *Server, net *Network) int {
	byPort := map[int]*Server{}
	for _, srv := range a.store.LoadAll() {
		if srv.Core == CoreVelocity {
			continue
		}
		if _, member := net.Member(srv.ID); member {
			continue
		}
		if others := a.networksOf(srv.ID); len(others) > 0 && others[0].proxy.ID != proxy.ID {
			continue // сервер уже в іншій мережі
		}
		byPort[a.effectiveServerPort(srv)] = srv
	}
	linked := 0
	kept := net.External[:0]
	for _, ex := range net.External {
		host, portStr, _ := strings.Cut(ex.Address, ":")
		port, _ := strconv.Atoi(portStr)
		srv := byPort[port]
		if srv == nil || (host != "127.0.0.1" && host != "localhost") {
			kept = append(kept, ex)
			continue
		}
		delete(byPort, port)
		net.Servers = append(net.Servers, NetworkServer{
			ServerID: srv.ID, Order: len(net.Members()) + 1, Alias: ex.Alias,
			InTry: ex.InTry, ForcedHost: ex.ForcedHost,
		})
		linked++
	}
	net.External = kept
	return linked
}

// NetworkSaveExternal зберігає сервери мережі поза лаунчером.
func (a *API) NetworkSaveExternal(proxyServerID string, list []ExternalServer) error {
	proxy, err := a.proxyServer(proxyServerID)
	if err != nil {
		return err
	}
	net, _ := a.loadNetwork(proxy)
	taken := map[string]bool{}
	for _, m := range net.Members() {
		taken[strings.ToLower(m.Alias)] = true
	}
	out := make([]ExternalServer, 0, len(list))
	for _, ex := range list {
		ex.Alias, ex.Address, ex.ForcedHost = strings.TrimSpace(ex.Alias), strings.TrimSpace(ex.Address), strings.TrimSpace(ex.ForcedHost)
		if !ValidAlias(ex.Alias) {
			return fmt.Errorf("ім'я «%s»: лише латиниця, цифри, «-» і «_», до 32 символів", ex.Alias)
		}
		if taken[strings.ToLower(ex.Alias)] {
			return fmt.Errorf("ім'я «%s» уже зайняте в мережі", ex.Alias)
		}
		host, port, ok := strings.Cut(ex.Address, ":")
		if p, err := strconv.Atoi(port); !ok || host == "" || err != nil || p < 1 || p > 65535 {
			return fmt.Errorf("адреса «%s»: потрібно host:порт, напр. 192.168.1.10:25565", ex.Address)
		}
		taken[strings.ToLower(ex.Alias)] = true
		out = append(out, ex)
	}
	net.External = out
	a.linkExternals(proxy, net)
	if err := SaveNetwork(proxy.Dir, net); err != nil {
		return err
	}
	return a.applyNetwork(proxy, net)
}

var validForwarding = map[string]bool{"modern": true, "legacy": true, "bungeeguard": true, "none": true}
var validPassthrough = map[string]bool{"disabled": true, "mods": true, "description": true, "all": true}

// NetworkSaveSettings зберігає налаштування проксі й мережі.
func (a *API) NetworkSaveSettings(proxyServerID string, s NetworkSettings) error {
	proxy, err := a.proxyServer(proxyServerID)
	if err != nil {
		return err
	}
	p := s.Proxy
	if !validForwarding[p.ForwardingMode] {
		return fmt.Errorf("невідомий режим передачі даних гравців: %s", p.ForwardingMode)
	}
	if !validPassthrough[p.PingPassthrough] {
		return fmt.Errorf("невідомий режим ping-passthrough: %s", p.PingPassthrough)
	}
	if p.CompressionLevel < -1 || p.CompressionLevel > 9 {
		return fmt.Errorf("рівень стиснення: від -1 до 9")
	}
	if p.QueryPort < 1 || p.QueryPort > 65535 {
		return fmt.Errorf("порт query: від 1 до 65535")
	}
	if s.ProxyPort < 1 || s.ProxyPort > 65535 {
		return fmt.Errorf("порт проксі: від 1 до 65535")
	}

	net, _ := a.loadNetwork(proxy)
	if s.ProxyPort != proxy.Port {
		for _, m := range net.Members() {
			if srv, ok := a.store.GetNoErr(m.ServerID); ok && a.effectiveServerPort(srv) == s.ProxyPort {
				return fmt.Errorf("порт %d уже має сервер мережі «%s»", s.ProxyPort, srv.Name)
			}
		}
		if check := CheckPortFree(s.ProxyPort, a.store.LoadAll(), proxy.ID, a.manager.IsRunning); !check.Free {
			return fmt.Errorf("порт %d зайнятий: %s", s.ProxyPort, check.Holder)
		}
		proxy.Port = s.ProxyPort
		if err := a.store.Save(proxy); err != nil {
			return err
		}
	}
	net.Proxy = p
	net.StopMembersWithProxy = s.StopMembersWithProxy
	net.LockBackends = s.LockBackends
	if err := SaveNetwork(proxy.Dir, net); err != nil {
		return err
	}
	return a.applyNetwork(proxy, net)
}

// applyNetwork переносить мережу у файли: velocity.toml, forwarding.secret і
// конфіги backend-ів. Запущений проксі перечитує конфіг сам (velocity reload)
// — нові сервери й порти діють без перезапуску; зміна порту самого проксі —
// лише після перезапуску.
func (a *API) applyNetwork(proxy *Server, net *Network) error {
	all := a.store.LoadAll()
	var backends []VelocityBackend
	for _, m := range net.Members() {
		srv, ok := all[m.ServerID]
		if !ok {
			continue
		}
		alias := m.Alias
		if alias == "" {
			alias = AliasFromName(srv.Name)
		}
		backends = append(backends, VelocityBackend{
			Alias: alias, Address: fmt.Sprintf("127.0.0.1:%d", a.effectiveServerPort(srv)),
			InTry: m.InTry, ForcedHost: m.ForcedHost,
		})
	}
	for _, ex := range net.External {
		backends = append(backends, VelocityBackend{Alias: ex.Alias, Address: ex.Address, InTry: ex.InTry, ForcedHost: ex.ForcedHost})
	}
	if err := ensureDir(proxy.Dir); err != nil {
		return err
	}
	cfgPath := filepath.Join(proxy.Dir, "velocity.toml")
	existing, _ := os.ReadFile(cfgPath)
	cfg := ApplyVelocityConfig(string(existing), proxy.Port, net.Proxy, backends)
	if err := writeFileAtomic(cfgPath, []byte(cfg), 0644); err != nil {
		return fmt.Errorf("velocity.toml: %w", err)
	}
	for id, msg := range ApplyNetworkGlue(proxy, all, net) {
		log.Printf("[network] glue %s: %s", id, msg)
	}
	if a.manager.IsRunning(proxy.ID) {
		_ = a.manager.SendCommand(proxy.ID, "velocity reload")
	}
	return nil
}

func ensureDir(dir string) error { return os.MkdirAll(dir, 0755) }

// SyncNetworksFor оновлює конфіги мереж, яких стосується сервер (змінився
// порт, назва чи сервер видалено) — проксі одразу бачить новий порт.
func (a *API) SyncNetworksFor(serverID string) {
	for _, pn := range a.networksOf(serverID) {
		if err := a.applyNetwork(pn.proxy, pn.net); err != nil {
			log.Printf("[network] синхронізація %s: %v", pn.proxy.Name, err)
		}
	}
}

// ── Запуск і зупинка ─────────────────────────────────────────────────────

// NetworkStart запускає мережу: проксі (якщо ще не працює), потім сервери
// з «запускати разом із проксі» у своєму порядку й з паузами. Збій одного
// сервера не зупиняє решту — помилки повертаються разом.
func (a *API) NetworkStart(proxyServerID string) error {
	proxy, err := a.proxyServer(proxyServerID)
	if err != nil {
		return err
	}
	lock := netLock(proxy.ID)
	if !lock.TryLock() {
		return nil // мережа вже запускається
	}
	defer lock.Unlock()

	net, _ := a.loadNetwork(proxy)
	if err := a.applyNetwork(proxy, net); err != nil {
		return err
	}
	if !a.manager.IsRunning(proxy.ID) {
		if err := a.prepareAndStart(proxy); err != nil {
			return fmt.Errorf("запуск проксі: %w", err)
		}
		waitProxyReady(a.manager, proxy.ID, 90*time.Second)
	}
	return a.startAutoMembers(proxy, net)
}

func (a *API) startAutoMembers(proxy *Server, net *Network) error {
	var errs []error
	for _, m := range net.Members() {
		srv, ok := a.store.GetNoErr(m.ServerID)
		if !ok || !m.AutoStart || a.manager.IsRunning(srv.ID) {
			continue
		}
		if m.DelaySec > 0 {
			time.Sleep(time.Duration(m.DelaySec) * time.Second)
		}
		if err := a.prepareAndStart(srv); err != nil {
			errs = append(errs, fmt.Errorf("«%s»: %w", srv.Name, err))
			a.notifyNetwork(proxy.ID, srv.ID, fmt.Sprintf("«%s» не запустився: %v", srv.Name, err))
		}
	}
	return errors.Join(errs...)
}

// NetworkStop зупиняє мережу: спершу сервери мережі (у зворотному порядку),
// потім проксі. Сервери поза мережею не зачіпаються.
func (a *API) NetworkStop(proxyServerID string) error {
	proxy, err := a.store.Get(proxyServerID)
	if err != nil {
		return err
	}
	var errs []error
	if net, err := LoadNetwork(proxy.Dir); err == nil {
		errs = append(errs, a.stopMembers(net)...)
	}
	if a.manager.IsRunning(proxy.ID) {
		if err := a.manager.Stop(proxy.ID, true); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", proxy.Name, err))
		}
	}
	return errors.Join(errs...)
}

// stopMembers зупиняє запущені сервери мережі у зворотному порядку.
func (a *API) stopMembers(net *Network) []error {
	var errs []error
	members := net.Members()
	stopped := false
	for i := len(members) - 1; i >= 0; i-- {
		srv, ok := a.store.GetNoErr(members[i].ServerID)
		if !ok || !a.manager.IsRunning(srv.ID) {
			continue
		}
		if err := a.manager.Stop(srv.ID, true); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", srv.Name, err))
		}
		stopped = true
	}
	if stopped {
		time.Sleep(2 * time.Second) // гравці переходять/виходять до зупинки проксі
	}
	return errs
}

// afterUserStart — ланцюги після запуску сервера користувачем:
//   - проксі → стартують сервери мережі з «запускати разом із проксі»;
//   - сервер із «запускає мережу» → стартує проксі (а з ним і решта).
func (a *API) afterUserStart(srv *Server) {
	if srv.Core == CoreVelocity {
		if net, err := LoadNetwork(srv.Dir); err == nil {
			go func() {
				if err := a.startAutoMembers(srv, net); err != nil {
					log.Printf("[network] %s: %v", srv.Name, err)
				}
			}()
		}
		return
	}
	for _, pn := range a.networksOf(srv.ID) {
		if m, _ := pn.net.Member(srv.ID); m.StartsNetwork {
			proxyID := pn.proxy.ID
			go func() {
				if err := a.NetworkStart(proxyID); err != nil {
					a.notifyNetwork(proxyID, srv.ID, err.Error())
				}
			}()
		}
	}
}

// beforeUserStop — проксі з «зупиняти сервери разом із проксі» спершу
// зупиняє свою мережу.
func (a *API) beforeUserStop(srv *Server) {
	if srv.Core != CoreVelocity {
		return
	}
	if net, err := LoadNetwork(srv.Dir); err == nil && net.StopMembersWithProxy {
		for _, err := range a.stopMembers(net) {
			a.notifyNetwork(srv.ID, "", err.Error())
		}
	}
}

// waitProxyReady чекає, поки проксі перейде в running (або час вийде).
func waitProxyReady(mgr *Manager, id string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		st := mgr.GetStatus(id)
		if st.Status == "running" || st.Status == "crashed" {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// ── Діагностика ──────────────────────────────────────────────────────────

// NetworkConfigText — поточний velocity.toml проксі (перегляд у UI).
func (a *API) NetworkConfigText(proxyServerID string) (string, error) {
	proxy, err := a.proxyServer(proxyServerID)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(proxy.Dir, "velocity.toml"))
	if err != nil {
		return "", fmt.Errorf("velocity.toml ще не створено — збережіть мережу")
	}
	return string(data), nil
}

// NetworkRepair виправляє все, що вміє: прибирає видалені сервери, дає
// коректні імена й заново пише velocity.toml та конфіги backend-ів.
func (a *API) NetworkRepair(proxyServerID string) error {
	proxy, err := a.proxyServer(proxyServerID)
	if err != nil {
		return err
	}
	net, _ := a.loadNetwork(proxy)
	taken := map[string]bool{}
	kept := []NetworkServer{{ServerID: proxy.ID, IsProxy: true}}
	for _, m := range net.Members() {
		srv, ok := a.store.GetNoErr(m.ServerID)
		if !ok {
			continue
		}
		if !ValidAlias(m.Alias) {
			m.Alias = AliasFromName(srv.Name)
		}
		m.Alias = uniqueAlias(m.Alias, taken)
		kept = append(kept, m)
	}
	net.Servers = kept
	a.linkExternals(proxy, net)
	if err := SaveNetwork(proxy.Dir, net); err != nil {
		return err
	}
	return a.applyNetwork(proxy, net)
}

var velocityServerLine = regexp.MustCompile(`(?m)^\s*"?([^"=\s]+)"?\s*=\s*"127\.0\.0\.1:(\d+)"`)

// NetworkDiagnose перевіряє мережу так, як її побачить гравець: конфіг
// проксі, секрет, forwarding і порти кожного backend-а, а запущені
// сервери ще й пінгує.
func (a *API) NetworkDiagnose(proxyServerID string) ([]NetworkIssue, error) {
	proxy, err := a.proxyServer(proxyServerID)
	if err != nil {
		return nil, err
	}
	var out []NetworkIssue
	add := func(level, serverID, title, detail string, fixable bool) {
		out = append(out, NetworkIssue{Level: level, ServerID: serverID, Title: title, Detail: detail, Fixable: fixable})
	}
	net, saved := a.loadNetwork(proxy)
	members := net.Members()
	if !saved || len(members) == 0 {
		add("info", "", "У мережі ще немає серверів", "Додайте Paper/Purpur або Fabric/Forge-сервер з проксі-модом.", false)
		return out, nil
	}

	proxyRunning := a.manager.IsRunning(proxy.ID)
	if !proxyRunning {
		if c := CheckPortFree(proxy.Port, a.store.LoadAll(), proxy.ID, a.manager.IsRunning); !c.Free {
			add("error", proxy.ID, fmt.Sprintf("Порт проксі %d зайнятий", proxy.Port), c.Holder, false)
		}
	} else if st, err := PingMCStatus(proxy.Port, 2*time.Second); err != nil || !st.Online {
		add("error", proxy.ID, "Проксі запущено, але він не відповідає", fmt.Sprintf("127.0.0.1:%d не приймає з'єднання.", proxy.Port), false)
	}

	// velocity.toml — те, що реально прочитає Velocity.
	cfgData, cfgErr := os.ReadFile(filepath.Join(proxy.Dir, "velocity.toml"))
	cfg := string(cfgData)
	if cfgErr != nil {
		add("error", proxy.ID, "Немає velocity.toml", "Конфіг проксі буде створено.", true)
	} else {
		if !strings.Contains(cfg, "config-version") {
			add("error", proxy.ID, "velocity.toml старого формату",
				"Без config-version Velocity перезаписує forwarding.secret при старті — секрет на серверах перестає збігатися.", true)
		}
		if !regexp.MustCompile(`player-info-forwarding-mode\s*=\s*"?` + regexp.QuoteMeta(net.Proxy.ForwardingMode)).MatchString(strings.ToLower(cfg)) {
			add("error", proxy.ID, "Режим передачі даних гравців у velocity.toml не збігається з мережею", "", true)
		}
	}
	listed := map[string]string{}
	for _, m := range velocityServerLine.FindAllStringSubmatch(cfg, -1) {
		listed[m[1]] = m[2]
	}
	secretData, _ := os.ReadFile(filepath.Join(proxy.Dir, "forwarding.secret"))
	secret := strings.TrimSpace(string(secretData))
	if secret == "" && net.Proxy.ForwardingMode != "none" {
		add("error", proxy.ID, "Немає forwarding.secret", "", true)
	}
	if !net.Proxy.OnlineMode {
		add("warn", proxy.ID, "Проксі в офлайн-режимі", "Зайти може будь-хто з будь-яким ніком — вмикайте лише для офлайн-акаунтів.", false)
	}

	anyTry := false
	ports := map[int]string{}
	for _, m := range members {
		srv, ok := a.store.GetNoErr(m.ServerID)
		if !ok {
			add("error", m.ServerID, "Сервер мережі видалено", "Його прибере «Виправити».", true)
			continue
		}
		name := "«" + srv.Name + "»"
		port := a.effectiveServerPort(srv)
		if m.InTry {
			anyTry = true
		}
		if !ValidAlias(m.Alias) {
			add("error", srv.ID, name+": некоректне ім'я в мережі", m.Alias, true)
		} else if listed[m.Alias] != fmt.Sprint(port) {
			add("error", srv.ID, name+": проксі знає інший порт", fmt.Sprintf("У мережі %d, у velocity.toml — %s.", port, orDash(listed[m.Alias])), true)
		}
		if port == proxy.Port {
			add("error", srv.ID, name+": порт збігається з портом проксі", fmt.Sprintf("Обидва на %d.", port), false)
		}
		if other, dup := ports[port]; dup {
			add("error", srv.ID, name+": той самий порт, що й «"+other+"»", fmt.Sprint(port), false)
		}
		ports[port] = srv.Name

		if net.Proxy.ForwardingMode == "modern" && srv.MCVersion != "" && !mcVersionAtLeast(srv.MCVersion, 1, 13) {
			add("error", srv.ID, name+": modern forwarding працює з 1.13", "Для старих версій оберіть режим legacy.", false)
		}
		switch backendForwarding(srv) {
		case fwdNone:
			if net.Proxy.ForwardingMode != "none" {
				hint := "Vanilla-сервер не приймає дані гравців від проксі — використайте Paper або Purpur."
				switch srv.Core {
				case "fabric", "quilt":
					hint = "Встановіть мод FabricProxy-Lite (вкладка «Моди» → «Знайти онлайн»), потім «Виправити»."
				case "forge", "neoforge":
					hint = "Встановіть мод Proxy Compatible Forge, потім «Виправити»."
				}
				add("error", srv.ID, name+": не приймає гравців від проксі", hint, false)
			}
		case fwdPaper:
			if secret != "" && net.Proxy.ForwardingMode == "modern" {
				file, base := paperForwardingPath(srv)
				data, _ := os.ReadFile(file)
				enabled, _ := getYAMLScalar(string(data), append(base, "enabled"))
				sec, _ := getYAMLScalar(string(data), append(base, "secret"))
				online, _ := getYAMLScalar(string(data), append(base, "online-mode"))
				if enabled != "true" || sec != secret {
					add("error", srv.ID, name+": forwarding у Paper не налаштовано", filepath.Base(file)+": velocity вимкнено або секрет інший.", true)
				} else if online != fmt.Sprint(net.Proxy.OnlineMode) {
					add("warn", srv.ID, name+": online-mode у Paper не збігається з проксі",
						"Проксі й сервер мають однаково перевіряти акаунти, інакше в офлайн-гравців інші UUID.", true)
				}
			}
		}
		props := parsePropertiesFile(filepath.Join(srv.Dir, "server.properties"))
		if props["online-mode"] != "false" && net.Proxy.ForwardingMode != "none" {
			add("error", srv.ID, name+": online-mode має бути false", "Гравців автентифікує проксі.", true)
		}
		if ip := props["server-ip"]; ip != "127.0.0.1" && ip != "localhost" {
			if net.LockBackends {
				add("warn", srv.ID, name+": доступний в обхід проксі", "Увімкнено «лише через проксі», але server-ip не 127.0.0.1.", true)
			} else {
				add("warn", srv.ID, name+": доступний в обхід проксі", "З online-mode=false будь-хто може зайти напряму з чужим ніком. Увімкніть «Вхід лише через проксі».", false)
			}
		}
		if a.manager.IsRunning(srv.ID) {
			if st, err := PingMCStatus(port, 2*time.Second); err != nil || !st.Online {
				add("error", srv.ID, name+": запущено, але не відповідає", fmt.Sprintf("127.0.0.1:%d не приймає з'єднання (ще вантажиться або впав).", port), false)
			} else {
				add("ok", srv.ID, name+": на зв'язку", fmt.Sprintf("Гравців: %d/%d · порт %d", st.PlayersOnline, st.MaxPlayers, port), false)
			}
		} else if m.InTry && !m.AutoStart && proxyRunning {
			add("warn", srv.ID, name+": сервер входу зупинено", "Проксі пускає гравців сюди, але сервер не запущений і не стартує з проксі.", false)
		}
	}
	localPorts := map[int]string{}
	for _, srv := range a.store.LoadAll() {
		if _, member := net.Member(srv.ID); !member && srv.Core != CoreVelocity {
			localPorts[a.effectiveServerPort(srv)] = srv.Name
		}
	}
	for _, ex := range net.External {
		if ex.InTry {
			anyTry = true
		}
		if h, ps, _ := strings.Cut(ex.Address, ":"); h == "127.0.0.1" || h == "localhost" {
			if p, _ := strconv.Atoi(ps); localPorts[p] != "" {
				add("warn", "", "«"+ex.Alias+"» — це сервер лаунчера «"+localPorts[p]+"»",
					"«Виправити» прив'яже його до мережі: зміна порту сервера одразу потрапить у проксі.", true)
				continue
			}
		}
		host, port, _ := strings.Cut(ex.Address, ":")
		if host == "127.0.0.1" || host == "localhost" {
			p, _ := strconv.Atoi(port)
			if st, err := PingMCStatus(p, 2*time.Second); err != nil || !st.Online {
				add("warn", "", "«"+ex.Alias+"» (поза лаунчером) не відповідає", ex.Address, false)
				continue
			}
		}
		add("info", "", "«"+ex.Alias+"» — сервер поза лаунчером", ex.Address+" · лаунчер ним не керує", false)
	}
	if !anyTry {
		add("warn", proxy.ID, "Жоден сервер не приймає вхід", "Проксі пустить гравців на перший сервер; позначте сервер входу явно.", false)
	}
	if len(out) == 0 || !hasProblems(out) {
		add("ok", proxy.ID, "Мережа налаштована", "Конфіг проксі, секрет і всі сервери узгоджені.", false)
	}
	return out, nil
}

func hasProblems(list []NetworkIssue) bool {
	for _, i := range list {
		if i.Level == "error" || i.Level == "warn" {
			return true
		}
	}
	return false
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// NetworkParents — проксі, у мережах яких є сервер (для прав друзів:
// remotectl поширює право на проксі на сервери мережі).
func (a *API) NetworkParents(serverID string) []string {
	var out []string
	for _, pn := range a.networksOf(serverID) {
		out = append(out, pn.proxy.ID)
	}
	return out
}

// NetworkMemberIDs — сервери лаунчера в мережі проксі.
func (a *API) NetworkMemberIDs(proxyID string) []string {
	proxy, err := a.store.Get(proxyID)
	if err != nil || proxy.Core != CoreVelocity {
		return nil
	}
	net, err := LoadNetwork(proxy.Dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, m := range net.Members() {
		if _, ok := a.store.GetNoErr(m.ServerID); ok {
			out = append(out, m.ServerID)
		}
	}
	return out
}
