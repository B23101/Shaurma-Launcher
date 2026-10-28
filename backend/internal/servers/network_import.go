// network_import.go — додавання готової мережі з теки: проксі Velocity і
// сервери лежать у підтеках однієї папки (напр. CYBERCLUB/velocity,
// CYBERCLUB/hub, CYBERCLUB/survival). Лаунчер додає їх усі разом, а мережу
// бере з velocity.toml проксі — імена й порядок входу лишаються як були.
package servers

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// NetworkFolderServer — сервер, знайдений у підтеці.
type NetworkFolderServer struct {
	Dir       string `json:"dir"`
	Name      string `json:"name"`
	Core      string `json:"core"`
	MCVersion string `json:"mcVersion"`
	Port      int    `json:"port"`
	// Known — уже доданий у лаунчер (буде лише прив'язаний до мережі).
	Known bool `json:"known"`
	// Skip — чому сервер не буде додано ("" — додається).
	Skip string `json:"skip,omitempty"`
}

// NetworkFolderInfo — що лежить у теці мережі.
type NetworkFolderInfo struct {
	Dir      string                `json:"dir"`
	HasProxy bool                  `json:"hasProxy"`
	Servers  []NetworkFolderServer `json:"servers"`
}

func sameDir(a, b string) bool {
	ca, cb := filepath.Clean(a), filepath.Clean(b)
	return strings.EqualFold(ca, cb)
}

func (a *API) serverByDir(dir string) *Server {
	for _, s := range a.store.LoadAll() {
		if sameDir(s.Dir, dir) {
			return s
		}
	}
	return nil
}

// DetectNetworkFolder дивиться підтеки: які з них сервери і чи є проксі.
// Мережа — це ≥2 сервери, серед них Velocity.
func (a *API) DetectNetworkFolder(dir string) (NetworkFolderInfo, error) {
	info := NetworkFolderInfo{Dir: dir, Servers: []NetworkFolderServer{}}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return info, err
	}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		sub := filepath.Join(dir, e.Name())
		det, err := DetectCore(sub)
		if err != nil || det.Core == "" || det.Confidence == "low" {
			continue
		}
		fs := NetworkFolderServer{Dir: sub, Name: e.Name(), Core: det.Core, MCVersion: det.MCVersion, Port: det.Port}
		if known := a.serverByDir(sub); known != nil {
			fs.Known, fs.Name = true, known.Name
		} else if det.Core != CoreVelocity && !containsEULAtrue(readFileString(filepath.Join(sub, "eula.txt"))) {
			fs.Skip = "EULA не прийнято (eula.txt)"
		}
		if det.Core == CoreVelocity {
			info.HasProxy = true
		}
		info.Servers = append(info.Servers, fs)
	}
	// Проксі першим, далі за назвою.
	sort.SliceStable(info.Servers, func(i, j int) bool {
		pi, pj := info.Servers[i].Core == CoreVelocity, info.Servers[j].Core == CoreVelocity
		if pi != pj {
			return pi
		}
		return strings.ToLower(info.Servers[i].Name) < strings.ToLower(info.Servers[j].Name)
	})
	return info, nil
}

func readFileString(path string) string {
	data, _ := os.ReadFile(path)
	return string(data)
}

// NetworkImportResult — підсумок імпорту теки мережі.
type NetworkImportResult struct {
	ProxyID string   `json:"proxyId"`
	Added   int      `json:"added"`
	Errors  []string `json:"errors"`
}

// ImportNetworkFolder додає всі сервери теки (крім уже доданих і без EULA)
// і будує мережу проксі з його velocity.toml.
func (a *API) ImportNetworkFolder(dir string) (NetworkImportResult, error) {
	res := NetworkImportResult{Errors: []string{}}
	info, err := a.DetectNetworkFolder(dir)
	if err != nil {
		return res, err
	}
	if !info.HasProxy {
		return res, fmt.Errorf("у теці немає проксі Velocity (velocity.toml + velocity.jar)")
	}
	proxyID := ""
	// Спершу backend-и: імпорт мережі зіставляє їх із [servers] за портом.
	for _, pass := range []bool{false, true} {
		for _, fs := range info.Servers {
			if (fs.Core == CoreVelocity) != pass || fs.Skip != "" {
				continue
			}
			id := ""
			if known := a.serverByDir(fs.Dir); known != nil {
				id = known.ID
			} else {
				view, err := a.AddServerFromFolder(fs.Dir, fs.Name, 0, false)
				if err != nil {
					res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", fs.Name, err))
					continue
				}
				id = view.ID
				res.Added++
			}
			if fs.Core == CoreVelocity && proxyID == "" {
				proxyID = id
			}
		}
	}
	if proxyID == "" {
		return res, fmt.Errorf("проксі не додано: %s", strings.Join(res.Errors, "; "))
	}
	res.ProxyID = proxyID
	proxy, err := a.store.Get(proxyID)
	if err != nil {
		return res, err
	}
	// Мережу будуємо одразу (імпорт з velocity.toml), але файли не
	// переписуємо, поки користувач нічого не змінив і не запустив проксі.
	if _, err := LoadNetwork(proxy.Dir); err != nil {
		a.loadNetwork(proxy)
	}
	return res, nil
}
