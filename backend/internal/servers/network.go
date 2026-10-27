// network.go — Velocity proxy-мережа: запуск/зупинка кількох серверів разом з Velocity.
package servers

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// NetworkServer — сервер у мережі Velocity.
type NetworkServer struct {
	ServerID string `json:"serverId"`
	Order    int    `json:"order"`    // порядок запуску (0 = перший після proxy)
	DelaySec int    `json:"delaySec"` // затримка перед стартом (секунди)
	IsProxy  bool   `json:"isProxy"`  // це Velocity?
}

// Network — група серверів, що працюють разом через Velocity.
type Network struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Servers  []NetworkServer `json:"servers"`
	FileName string          `json:"-"` // файл зберігання
}

// NetworkManager — менеджер мереж.
type NetworkManager struct {
	mgr *Manager
}

// NewNetworkManager створює менеджер мереж.
func NewNetworkManager(mgr *Manager) *NetworkManager {
	return &NetworkManager{mgr: mgr}
}

// StartNetwork запускає мережу: спочатку Velocity, потім backend-сервери з затримками.
func (nm *NetworkManager) StartNetwork(net *Network, javaPath string, servers map[string]*Server) error {
	// Знаходимо Velocity (проксі).
	var proxyServer *Server
	var backendServers []NetworkServer

	for _, ns := range net.Servers {
		if ns.IsProxy {
			if s, ok := servers[ns.ServerID]; ok {
				proxyServer = s
			}
		} else {
			backendServers = append(backendServers, ns)
		}
	}

	if proxyServer == nil {
		return fmt.Errorf("merежа %s: velocity сервер не знайдено", net.Name)
	}

	// Сортуємо backend за порядком.
	sortByOrder(backendServers)

	// Запускаємо Velocity.
	log.Printf("[network] запуск мережі %s: velocity", net.Name)
	if err := nm.mgr.Start(proxyServer, javaPath); err != nil {
		return fmt.Errorf("start velocity: %w", err)
	}

	// Запускаємо backend-сервери з затримками.
	for _, bs := range backendServers {
		if bs.DelaySec > 0 {
			log.Printf("[network] затримка %ds перед запуском %s", bs.DelaySec, bs.ServerID)
			time.Sleep(time.Duration(bs.DelaySec) * time.Second)
		}

		s, ok := servers[bs.ServerID]
		if !ok {
			log.Printf("[network] сервер %s не знайдено, пропускаємо", bs.ServerID)
			continue
		}

		log.Printf("[network] запуск backend %s", s.Name)
		if err := nm.mgr.Start(s, javaPath); err != nil {
			log.Printf("[network] помилка запуску %s: %v (продовжуємо)", s.Name, err)
		}
	}

	log.Printf("[network] мережа %s запущена (velocity + %d backend)", net.Name, len(backendServers))
	return nil
}

// StopNetwork зупиняє мережу: спочатку backend (у зворотному порядку), потім Velocity.
func (nm *NetworkManager) StopNetwork(net *Network, graceful bool) {
	// Знаходимо сервери.
	var proxyID string
	var backendIDs []string

	for _, ns := range net.Servers {
		if ns.IsProxy {
			proxyID = ns.ServerID
		} else {
			backendIDs = append(backendIDs, ns.ServerID)
		}
	}

	// Зупиняємо backend у зворотному порядку.
	for i := len(backendIDs) - 1; i >= 0; i-- {
		id := backendIDs[i]
		if nm.mgr.IsRunning(id) {
			log.Printf("[network] зупинка backend %s", id)
			_ = nm.mgr.Stop(id, graceful)
		}
	}

	// Даємо час backend закритися.
	time.Sleep(2 * time.Second)

	// Зупиняємо Velocity.
	if proxyID != "" && nm.mgr.IsRunning(proxyID) {
		log.Printf("[network] зупинка velocity %s", proxyID)
		_ = nm.mgr.Stop(proxyID, graceful)
	}

	log.Printf("[network] мережа %s зупинена", net.Name)
}

// NetworkStatus повертає статуси всіх серверів мережі.
func (nm *NetworkManager) NetworkStatus(net *Network) map[string]ServerStatus {
	result := make(map[string]ServerStatus)
	for _, ns := range net.Servers {
		result[ns.ServerID] = nm.mgr.GetStatus(ns.ServerID)
	}
	return result
}

// SaveNetwork зберігає конфіг мережі у файлі сервера.
func SaveNetwork(dir string, net *Network) error {
	return saveJSON(filepath.Join(dir, ".shaurma", "network.json"), net)
}

// LoadNetwork завантажує конфіг мережі з папки.
func LoadNetwork(dir string) (*Network, error) {
	var net Network
	err := loadJSON(filepath.Join(dir, ".shaurma", "network.json"), &net)
	if err != nil {
		return nil, err
	}
	return &net, nil
}

// GenerateVelocityConfig генерує velocity.toml у ФОРМАТІ, який реально
// вміє читати Velocity (перевірено проти default-velocity.toml):
//
//	· bind = "0.0.0.0:порт" — обов'язкова: без неї Velocity слухає
//	  дефолтний 25577, а detectVelocityPort не знає порт;
//	· [servers]: лише рядки "ім'я = \"127.0.0.1:порт\"" + ключ try
//	  (список імен у порядку спроби входу). try живе ВСЕРЕДИНІ [servers],
//	  як у дефолтному конфігу; try поза [servers] валить Velocity з
//	  ClassCastException, а не-рядковий ключ у [servers], крім try — з
//	  IllegalArgumentException;
//	· [forced-hosts] — порожня секція, яку Velocity очікує завжди.
func GenerateVelocityConfig(dir, bindAddr string, servers map[string]*Server, net *Network) error {
	// Backend-сервери у порядку запуску (як у net.Servers після SaveNetwork).
	type entry struct {
		name string
		port int
	}
	var backends []entry
	for _, ns := range net.Servers {
		if ns.IsProxy {
			continue
		}
		s, ok := servers[ns.ServerID]
		if !ok {
			continue
		}
		name := strings.ToLower(strings.ReplaceAll(s.Name, " ", "-"))
		backends = append(backends, entry{name: name, port: s.Port})
	}

	var sb strings.Builder
	sb.WriteString("# Generated by ShaurmLauncher\n")
	sb.WriteString("# Velocity proxy configuration\n\n")

	// bind — обов'язковий: без нього Velocity стартує на дефолтному
	// 25577, а не на порту, який користувач бачить в UI/налаштуваннях.
	if bindAddr == "" {
		bindAddr = "0.0.0.0:25565"
	}
	sb.WriteString(fmt.Sprintf("bind = \"%s\"\n\n", bindAddr))

	// Player-info forwarding: режим modern — правильні UUID/скіни гравців
	// на backend-ах. Секрет живе у файлі forwarding.secret біля проксі
	// (створює ApplyNetworkGlue); сам секрет у toml не дублюємо.
	sb.WriteString("[player-info-forwarding]\n")
	sb.WriteString("mode = \"modern\"\n\n")

	if len(backends) == 0 {
		sb.WriteString("# Мережа порожня — додайте backend-сервери у менеджері мережі.\n\n[servers]\ntry = []\n\n[forced-hosts]\n")
		return os.WriteFile(filepath.Join(dir, "velocity.toml"), []byte(sb.String()), 0644)
	}

	sb.WriteString("[servers]\n")
	for _, b := range backends {
		sb.WriteString(fmt.Sprintf("%s = \"127.0.0.1:%d\"\n", b.name, b.port))
	}

	// try — порядок спроби входу (перший сервер = основний «лобі»).
	// Живе ВСЕРЕДИНІ [servers] і є списком РЯДКІВ-імен, а не таблиць.
	sb.WriteString("try = [\n")
	for _, b := range backends {
		sb.WriteString(fmt.Sprintf("  \"%s\",\n", b.name))
	}
	sb.WriteString("]\n\n")

	// [forced-hosts] — порожня секція обов'язкова, Velocity її завжди читає.
	sb.WriteString("[forced-hosts]\n")
	return os.WriteFile(filepath.Join(dir, "velocity.toml"), []byte(sb.String()), 0644)
}

// sortByOrder сортує сервери за порядком.
func sortByOrder(servers []NetworkServer) {
	for i := 0; i < len(servers); i++ {
		for j := i + 1; j < len(servers); j++ {
			if servers[j].Order < servers[i].Order {
				servers[i], servers[j] = servers[j], servers[i]
			}
		}
	}
}

// saveJSON / loadJSON — JSON серіалізація.
func saveJSON(path string, v interface{}) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func loadJSON(path string, v interface{}) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}
