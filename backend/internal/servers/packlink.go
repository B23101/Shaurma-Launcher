// packlink.go — зв'язок серверів зі збірками (IP, автозапуск).
package servers

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// PackServerLink — зв'язок збірки з сервером.
type PackServerLink struct {
	PackID        string `json:"packId"`
	PackName      string `json:"packName"`
	ServerID      string `json:"serverId"`
	ServerName    string `json:"serverName"`
	ServerAddr    string `json:"serverAddr"`    // 127.0.0.1:port
	LaunchWithPack bool  `json:"launchWithPack"` // запускати сервер при старті збірки
	StopWithPack   bool  `json:"stopWithPack"`   // зупиняти сервер при зупинці збірки
	// JoinOnLaunch — після входу в гру одразу підключитися до цього сервера
	// (автоприєднання на один запуск, збережене автоприєднання збірки не
	// змінюється).
	JoinOnLaunch bool `json:"joinOnLaunch"`
}

// PackLinkStore — зберігання зв'язків.
type PackLinkStore struct {
	dir  string
	links []PackServerLink
}

// NewPackLinkStore створює store.
func NewPackLinkStore(dir string) *PackLinkStore {
	s := &PackLinkStore{dir: dir}
	s.load()
	return s
}

// GetLinksForServer повертає зв'язки збірок з сервером.
func (s *PackLinkStore) GetLinksForServer(serverID string) []PackServerLink {
	var result []PackServerLink
	for _, l := range s.links {
		if l.ServerID == serverID {
			result = append(result, l)
		}
	}
	return result
}

// GetLinksForPack повертає зв'язки збірки з серверами.
func (s *PackLinkStore) GetLinksForPack(packID string) []PackServerLink {
	var result []PackServerLink
	for _, l := range s.links {
		if l.PackID == packID {
			result = append(result, l)
		}
	}
	return result
}

// AddLink додає зв'язок.
func (s *PackLinkStore) AddLink(link PackServerLink) error {
	// Перевірка дублікату.
	for i, l := range s.links {
		if l.PackID == link.PackID && l.ServerID == link.ServerID {
			s.links[i] = link
			return s.save()
		}
	}
	s.links = append(s.links, link)
	return s.save()
}

// RemoveLink видаляє зв'язок.
func (s *PackLinkStore) RemoveLink(packID, serverID string) error {
	for i, l := range s.links {
		if l.PackID == packID && l.ServerID == serverID {
			s.links = append(s.links[:i], s.links[i+1:]...)
			return s.save()
		}
	}
	return nil
}

// ServersNeedingLaunch повертає ID серверів які треба запустити зі збіркою.
func (s *PackLinkStore) ServersNeedingLaunch(packID string) []string {
	var ids []string
	for _, l := range s.links {
		if l.PackID == packID && l.LaunchWithPack {
			ids = append(ids, l.ServerID)
		}
	}
	return ids
}

// ServersNeedingStop повертає ID серверів які треба зупинити зі збіркою.
func (s *PackLinkStore) ServersNeedingStop(packID string) []string {
	var ids []string
	for _, l := range s.links {
		if l.PackID == packID && l.StopWithPack {
			ids = append(ids, l.ServerID)
		}
	}
	return ids
}

func (s *PackLinkStore) load() {
	path := filepath.Join(s.dir, "pack-links.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	json.Unmarshal(data, &s.links)
}

func (s *PackLinkStore) save() error {
	path := filepath.Join(s.dir, "pack-links.json")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.links, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// FormatServerAddress повертає адресу сервера "127.0.0.1:port".
func FormatServerAddress(srv *Server) string {
	return fmt.Sprintf("127.0.0.1:%d", srv.Port)
}
