package p2p

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// udpprefs.go — ПОСТІЙНЕ збереження UDP-налаштувань P2P (голосовий чат) по серверах.
//
// Раніше галочка «UDP» і порт жили лише в localStorage WebView: при іншому ключі
// (id сервера / порт), очищенні кешу чи перевстановленні налаштування «злітали», а
// автостарт хостингу читав взагалі інший ключ. Тепер джерело правди — файл у каталозі
// стану P2P (поруч з addresses.json), ключ — id сервера (або port-N).

const udpPrefsFileName = "udp-prefs.json"

// UDPPref — збережені UDP-налаштування одного сервера.
type UDPPref struct {
	Enabled bool `json:"enabled"`
	// Port — UDP-порт голосу. 0 = «як порт сервера» (дефолт, слідує за портом сервера).
	Port int `json:"port"`
}

type udpPrefsFile struct {
	Version int                `json:"version"`
	Servers map[string]UDPPref `json:"servers"`
}

func cleanPrefKey(k string) (string, error) {
	k = strings.TrimSpace(k)
	if k == "" || len(k) > 128 || strings.ContainsAny(k, "\x00\r\n") {
		return "", fmt.Errorf("p2p: некоректний ключ сервера")
	}
	return k, nil
}

func (a *API) readUDPPrefsLocked() udpPrefsFile {
	f := udpPrefsFile{Version: 1, Servers: map[string]UDPPref{}}
	data, err := os.ReadFile(filepath.Join(a.stateDir(), udpPrefsFileName))
	if err != nil {
		return f
	}
	var in udpPrefsFile
	if json.Unmarshal(data, &in) != nil || in.Servers == nil {
		return f
	}
	f.Servers = in.Servers
	return f
}

func (a *API) writeUDPPrefsLocked(f udpPrefsFile) error {
	dir := a.stateDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("p2p: не вдалося створити %s: %w", dir, err)
	}
	data, err := json.MarshalIndent(&f, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(dir, udpPrefsFileName)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("p2p: запис %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("p2p: заміна %s: %w", path, err)
	}
	return nil
}

// GetUDPPref повертає збережені UDP-налаштування сервера. found=false — користувач
// ще нічого не зберігав (UI може підхопити старе значення з localStorage).
func (a *API) GetUDPPref(serverKey string) (UDPPref, bool) {
	k, err := cleanPrefKey(serverKey)
	if err != nil {
		return UDPPref{}, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	p, ok := a.readUDPPrefsLocked().Servers[k]
	return p, ok
}

// SetUDPPref зберігає UDP-налаштування сервера (атомарний запис файлу).
func (a *API) SetUDPPref(serverKey string, enabled bool, port int) error {
	k, err := cleanPrefKey(serverKey)
	if err != nil {
		return err
	}
	if port < 0 || port > 65535 {
		return fmt.Errorf("p2p: UDP-порт поза діапазоном 1–65535")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	f := a.readUDPPrefsLocked()
	f.Servers[k] = UDPPref{Enabled: enabled, Port: port}
	return a.writeUDPPrefsLocked(f)
}
