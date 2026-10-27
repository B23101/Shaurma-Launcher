// network_api.go — API-рівень менеджера мережі Velocity для фронтенду.
// Відмінність від network.go (нижчий шар): тут ми знаємо про store (список
// серверів), java.Installer і самі вирішуємо, яку Java використати для
// КОЖНОГО сервера мережі (Velocity 4.x → Java 25, backend 1.20.1 → Java 17
// тощо), тоді як network.go::StartNetwork приймав один javaPath на всіх.
//
// Конфіг мережі зберігається у <папка проксі>/.shaurma/network.json
// (формат Network з network.go) — проксі є «центром» своєї мережі.
package servers

import (
	"fmt"
	"log"
	"sort"
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
	// Роль у мережі (редагується у UI):
	InNetwork bool `json:"inNetwork"` // включений у мережу?
	Order     int  `json:"order"`     // порядок запуску серед backend-серверів
	DelaySec  int  `json:"delaySec"`  // пауза перед стартом цього сервера
}

// NetworkView — усе, що треба UI сторінки «Мережа» Velocity-сервера.
type NetworkView struct {
	ProxyServerID string              `json:"proxyServerId"`
	ProxyName     string              `json:"proxyName"`
	ProxyPort     int                 `json:"proxyPort"`    // порт проксі (з bind)
	ProxyRunning  bool                `json:"proxyRunning"`
	NetworkSaved  bool                `json:"networkSaved"` // network.json вже існує
	Members       []NetworkMemberView `json:"members"`      // інші сервери (кандидати + включені)
}

// ── NetworkView ──────────────────────────────────────────────────────────

// NetworkGetView повертає стан мережі для Velocity-сервера proxyServerID:
// проксі + список решти серверів з їхніми поточними ролями.
func (a *API) NetworkGetView(proxyServerID string) (NetworkView, error) {
	proxy, err := a.store.Get(proxyServerID)
	if err != nil {
		return NetworkView{}, err
	}
	if proxy.Core != CoreVelocity {
		return NetworkView{}, fmt.Errorf("сервер %s не є Velocity-проксі", proxy.Name)
	}

	view := NetworkView{
		ProxyServerID: proxy.ID,
		ProxyName:     proxy.Name,
		ProxyPort:     proxy.Port,
		ProxyRunning:  a.manager.IsRunning(proxy.ID),
	}

	// Включені сервери з network.json (якщо файл існує).
	net, _ := LoadNetwork(proxy.Dir)
	if net != nil {
		view.NetworkSaved = true
	}
	inNet := map[string]NetworkServer{}
	if net != nil {
		for _, ns := range net.Servers {
			if ns.IsProxy {
				continue
			}
			inNet[ns.ServerID] = ns
		}
	}

	all := a.store.LoadAll()
	ids := make([]string, 0, len(all))
	for id := range all {
		if id != proxy.ID {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)

	for _, id := range ids {
		srv := all[id]
		ns, ok := inNet[srv.ID]
		view.Members = append(view.Members, NetworkMemberView{
			ServerID:  srv.ID,
			Name:      srv.Name,
			Core:      srv.Core,
			MCVersion: srv.MCVersion,
			Port:      srv.Port,
			Running:   a.manager.IsRunning(srv.ID),
			InNetwork: ok,
			Order:     ns.Order,
			DelaySec:  ns.DelaySec,
		})
	}
	return view, nil
}

// NetworkSave зберігає склад мережі: приймає СПИСОК усіх серверів з ролями
// (як їх редагував UI) і пише network.json біля проксі.
func (a *API) NetworkSave(proxyServerID string, members []NetworkMemberView) error {
	proxy, err := a.store.Get(proxyServerID)
	if err != nil {
		return err
	}
	if proxy.Core != CoreVelocity {
		return fmt.Errorf("сервер %s не є Velocity-проксі", proxy.Name)
	}

	// Валідація: order не можуть повторюватись серед включених.
	seenOrder := map[int]string{}
	for _, m := range members {
		if !m.InNetwork {
			continue
		}
		if other, dup := seenOrder[m.Order]; dup {
			return fmt.Errorf("порядок %d задано двічі: %s і %s", m.Order, other, m.Name)
		}
		seenOrder[m.Order] = m.Name
		if srv, err := a.store.Get(m.ServerID); err == nil && srv.Core == CoreVelocity {
			return fmt.Errorf("у мережу не можна додати інший проксі: %s", srv.Name)
		}
	}

	net := &Network{ID: proxy.ID, Name: proxy.Name + " (мережа Velocity)"}
	// Проксі завжди першим у списку (isProxy).
	net.Servers = append(net.Servers, NetworkServer{ServerID: proxy.ID, IsProxy: true})
	// Включені backend-сервери, відсортовані за order.
	byOrder := make([]NetworkMemberView, 0)
	for _, m := range members {
		if m.InNetwork {
			byOrder = append(byOrder, m)
		}
	}
	sort.Slice(byOrder, func(i, j int) bool { return byOrder[i].Order < byOrder[j].Order })
	for _, m := range byOrder {
		net.Servers = append(net.Servers, NetworkServer{
			ServerID: m.ServerID,
			Order:    m.Order,
			DelaySec: m.DelaySec,
		})
	}
	if err := SaveNetwork(proxy.Dir, net); err != nil {
		return err
	} // Одразу переписуємо velocity.toml біля проксі: bind з портом проксі,
	// [servers] з усіма backend-серверами (host = 127.0.0.1:порт) і try +
	// fallback для маршрутизації входу. Без цього запущений проксі нікого
	// не знає і гравці падають на «No available servers» ще до входу.
	all := a.store.LoadAll()
	memberMap := make(map[string]*Server, len(all))
	for id, s := range all {
		memberMap[id] = s
	}
	bindAddr := "0.0.0.0:25565"
	if proxy.Port > 0 {
		bindAddr = fmt.Sprintf("0.0.0.0:%d", proxy.Port)
	}
	if err := GenerateVelocityConfig(proxy.Dir, bindAddr, memberMap, net); err != nil {
		log.Printf("[network] попередження: не вдалося оновити velocity.toml: %v", err)
	}
	// «Склеювання»: forwarding.secret + сучасний player-info-forwarding на
	// проксі та конфіг кожного Paper/Purpur backend-а (online-mode=false,
	// секрет у paper-global.yml / paper.yml) — без цього гравці не
	// потрапляють на backend з правильними UUID.
	if err := ApplyNetworkGlue(proxy, memberMap, net); err != nil {
		log.Printf("[network] попередження: glue мережі: %v", err)
	}
	return nil
}

// networkLoadOrError читає конфіг мережі; без нього запускати нічого.
func (a *API) networkLoadOrError(proxy *Server) (*Network, error) {
	net, err := LoadNetwork(proxy.Dir)
	if err != nil {
		return nil, fmt.Errorf("мережу не збережено: %v (спершу збережіть склад мережі)", err)
	}
	if len(net.Servers) == 0 {
		return nil, fmt.Errorf("мережа порожня")
	}
	return net, nil
}

// NetworkStart запускає мережу: проксі першим, потім backend-сервери у
// порядку order, з паузами delaySec. Кожен сервер стартує через
// prepareAndStart — зі СВОЄЮ Java та з лінивим доставленням ядра.
// Якщо проксі вже запущено (напр., з консолі) — стартуються лише
// backend-сервери, що ще не запущені.
func (a *API) NetworkStart(proxyServerID string) error {
	proxy, err := a.store.Get(proxyServerID)
	if err != nil {
		return err
	}
	if proxy.Core != CoreVelocity {
		return fmt.Errorf("сервер %s не є Velocity-проксі", proxy.Name)
	}
	net, err := a.networkLoadOrError(proxy)
	if err != nil {
		return err
	}

	// Пере-застосовуємо glue перед стартом: конфіги backend-ів могли бути
	// згенеровані сервером при першому запуску (Paper пише повний
	// paper-global.yml) — дописуємо наші ключі знову, щоб forwarding
	// працював завжди.
	if err := ApplyNetworkGlue(proxy, a.store.LoadAll(), net); err != nil {
		log.Printf("[network] попередження: glue перед стартом: %v", err)
	}

	proxyAlreadyUp := a.manager.IsRunning(proxy.ID)
	// Спершу проксі (якщо ще не запущений).
	if !proxyAlreadyUp {
		if err := a.prepareAndStart(proxy); err != nil {
			return fmt.Errorf("запуск проксі: %w", err)
		}
		// Чекаємо, поки Velocity реально піднявся (щоб backends не падали
		// на «сервер недоступний» у консолі проксі).
		waitProxyReady(a.manager, proxy.ID, 90*time.Second)
	}

	// Потім backend у порядку.
	for _, ns := range net.Servers {
		if ns.IsProxy {
			continue
		}
		srv, ok := a.store.GetNoErr(ns.ServerID)
		if !ok || a.manager.IsRunning(srv.ID) {
			continue
		}
		if ns.DelaySec > 0 {
			log.Printf("[network] пауза %ds перед %s", ns.DelaySec, srv.Name)
			time.Sleep(time.Duration(ns.DelaySec) * time.Second)
		}
		if err := a.prepareAndStart(srv); err != nil {
			// Відкат: зупиняємо вже запущені сервери мережі.
			_ = a.NetworkStop(proxyServerID)
			return fmt.Errorf("запуск сервера «%s»: %w (мережу зупинено)", srv.Name, err)
		}
	}
	return nil
}

// NetworkStop зупиняє мережу: спочатку backend (зворотний порядок), потім
// проксі (щоб гравці встигли вийти, а не впасти).
// NetworkStop зупиняє мережу: спочатку backend (зворотний порядок), потім
// проксі (щоб гравці встигли вийти, а не впасти). Без збереженого
// network.json не помиляється: зупиняє проксі й усі сервери, що реально
// запущені (кнопка «Зупинити мережу» має завжди працювати, навіть якщо
// конфіг прибрали або сервери запустили вручну).
func (a *API) NetworkStop(proxyServerID string) error {
	proxy, err := a.store.Get(proxyServerID)
	if err != nil {
		return err
	}

	// Порядок зупинки: backend, потім проксі. Джерело списку — конфіг
	// мережі (якщо є), плюс усі запущені сервери, що не є іншими проксі.
	net, _ := LoadNetwork(proxy.Dir)
	order := make([]NetworkServer, 0)
	seen := map[string]bool{proxy.ID: true}
	if net != nil {
		for _, ns := range net.Servers {
			if ns.IsProxy {
				continue
			}
			if !seen[ns.ServerID] {
				seen[ns.ServerID] = true
				order = append(order, ns)
			}
		}
	}
	// Додаємо запущені сервери поза конфігом.
	for id, srv := range a.store.LoadAll() {
		if seen[id] || id == proxy.ID || srv.Core == CoreVelocity {
			continue
		}
		if a.manager.IsRunning(id) {
			seen[id] = true
			order = append(order, NetworkServer{ServerID: id})
		}
	}

	var errs []string
	// Backend у зворотному порядку.
	for i := len(order) - 1; i >= 0; i-- {
		srv, ok := a.store.GetNoErr(order[i].ServerID)
		if !ok || !a.manager.IsRunning(srv.ID) {
			continue
		}
		if err := a.manager.Stop(srv.ID, true); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", srv.Name, err))
		}
	}

	// Невелика пауза, щоб backend закрився, потім проксі.
	if a.manager.IsRunning(proxy.ID) {
		time.Sleep(2 * time.Second)
		if err := a.manager.Stop(proxy.ID, true); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", proxy.Name, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("помилки зупинки: %v", errs)
	}
	return nil
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
