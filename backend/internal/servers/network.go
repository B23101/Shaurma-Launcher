// network.go — модель мережі Velocity і генерація velocity.toml.
//
// Мережа — це система серверів, а не «сервер з налаштуваннями»: проксі
// (Velocity) + backend-сервери (Paper/Purpur, Fabric/Forge з проксі-модом)
// зі своїми іменами в мережі, порядком входу, ланцюгами запуску й
// налаштуваннями самого проксі. Зберігається у <тека проксі>/.shaurma/
// network.json; velocity.toml лаунчер генерує з неї (network_api.go).
package servers

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// networkVersion — версія формату network.json. 0 — старий формат без
// автозапуску й налаштувань проксі (мігрується в LoadNetwork).
const networkVersion = 2

// velocityConfigVersion — версія формату velocity.toml, який ми пишемо.
// Без неї Velocity вважає файл старим (1.0) і запускає міграції, одна з
// яких ПЕРЕЗАПИСУЄ forwarding.secret випадковим значенням — після цього
// секрет на backend-ах не збігався, і гравців не пускало.
const velocityConfigVersion = "2.7"

// NetworkServer — сервер у мережі Velocity.
type NetworkServer struct {
	ServerID string `json:"serverId"`
	Order    int    `json:"order"`    // порядок запуску й входу (менше — раніше)
	DelaySec int    `json:"delaySec"` // пауза перед стартом (секунди)
	IsProxy  bool   `json:"isProxy"`
	// Alias — ім'я сервера в мережі: ключ у [servers] velocity.toml і
	// аргумент /server <alias>. Порожнє — генерується з назви.
	Alias string `json:"alias,omitempty"`
	// AutoStart — стартує разом із проксі (кнопка «Запустити мережу» і
	// звичайний старт проксі).
	AutoStart bool `json:"autoStart"`
	// StartsNetwork — запуск ЦЬОГО сервера піднімає проксі, а з ним і
	// решту серверів з AutoStart.
	StartsNetwork bool `json:"startsNetwork"`
	// InTry — сервер у списку try: туди проксі пускає гравця при вході
	// (у порядку Order) і куди переносить, якщо сервер упав.
	InTry bool `json:"inTry"`
	// ForcedHost — домен, з яким гравець одразу потрапляє на цей сервер
	// ([forced-hosts]), напр. survival.example.com.
	ForcedHost string `json:"forcedHost,omitempty"`
}

// ProxySettings — налаштування самого проксі (velocity.toml).
type ProxySettings struct {
	Motd                           string `json:"motd"`
	ShowMaxPlayers                 int    `json:"showMaxPlayers"`
	OnlineMode                     bool   `json:"onlineMode"`
	ForceKeyAuthentication         bool   `json:"forceKeyAuthentication"`
	PreventClientProxyConnections  bool   `json:"preventClientProxyConnections"`
	ForwardingMode                 string `json:"forwardingMode"` // modern | legacy | bungeeguard | none
	AnnounceForge                  bool   `json:"announceForge"`
	KickExistingPlayers            bool   `json:"kickExistingPlayers"`
	PingPassthrough                string `json:"pingPassthrough"` // disabled | mods | description | all
	FailoverOnUnexpectedDisconnect bool   `json:"failoverOnUnexpectedDisconnect"`
	CompressionThreshold           int    `json:"compressionThreshold"`
	CompressionLevel               int    `json:"compressionLevel"`
	LoginRatelimit                 int    `json:"loginRatelimit"`
	ConnectionTimeout              int    `json:"connectionTimeout"`
	ReadTimeout                    int    `json:"readTimeout"`
	HAProxyProtocol                bool   `json:"haproxyProtocol"`
	TCPFastOpen                    bool   `json:"tcpFastOpen"`
	LogCommandExecutions           bool   `json:"logCommandExecutions"`
	LogPlayerConnections           bool   `json:"logPlayerConnections"`
	QueryEnabled                   bool   `json:"queryEnabled"`
	QueryPort                      int    `json:"queryPort"`
}

// DefaultProxySettings — значення за замовчуванням Velocity 3.x.
func DefaultProxySettings() ProxySettings {
	return ProxySettings{
		Motd:                           "<#09add3>A Velocity Server",
		ShowMaxPlayers:                 500,
		OnlineMode:                     true,
		ForceKeyAuthentication:         true,
		ForwardingMode:                 "modern",
		PingPassthrough:                "disabled",
		FailoverOnUnexpectedDisconnect: true,
		CompressionThreshold:           256,
		CompressionLevel:               -1,
		LoginRatelimit:                 3000,
		ConnectionTimeout:              5000,
		ReadTimeout:                    30000,
		LogPlayerConnections:           true,
		QueryPort:                      25577,
	}
}

// Network — проксі та його backend-сервери.
type Network struct {
	ID      string          `json:"id"`
	Name    string          `json:"name"`
	Servers []NetworkServer `json:"servers"`
	Version int             `json:"version"`
	Proxy   ProxySettings   `json:"proxy"`
	// StopMembersWithProxy — зупинка проксі спершу зупиняє сервери мережі.
	StopMembersWithProxy bool `json:"stopMembersWithProxy"`
	// LockBackends — backend-и слухають лише 127.0.0.1: з online-mode=false
	// напряму в обхід проксі на них міг би зайти будь-хто з будь-яким ніком.
	LockBackends bool `json:"lockBackends"`
	// External — сервери мережі, яких немає в лаунчері (інша машина,
	// сервер, запущений окремо): лаунчер лише тримає їх у [servers].
	External []ExternalServer `json:"external,omitempty"`
}

// ExternalServer — запис [servers] поза лаунчером.
type ExternalServer struct {
	Alias      string `json:"alias"`
	Address    string `json:"address"` // host:port
	InTry      bool   `json:"inTry"`
	ForcedHost string `json:"forcedHost,omitempty"`
}

// NewNetwork — порожня мережа проксі з налаштуваннями за замовчуванням.
func NewNetwork(proxy *Server) *Network {
	return &Network{
		ID: proxy.ID, Name: proxy.Name, Version: networkVersion,
		Servers:              []NetworkServer{{ServerID: proxy.ID, IsProxy: true}},
		Proxy:                DefaultProxySettings(),
		StopMembersWithProxy: true,
		LockBackends:         true,
	}
}

// Members — backend-сервери мережі в порядку Order.
func (n *Network) Members() []NetworkServer {
	out := make([]NetworkServer, 0, len(n.Servers))
	for _, s := range n.Servers {
		if !s.IsProxy {
			out = append(out, s)
		}
	}
	sortByOrder(out)
	return out
}

// Member повертає запис backend-а мережі.
func (n *Network) Member(serverID string) (NetworkServer, bool) {
	for _, s := range n.Servers {
		if !s.IsProxy && s.ServerID == serverID {
			return s, true
		}
	}
	return NetworkServer{}, false
}

// SaveNetwork зберігає конфіг мережі у теці проксі.
func SaveNetwork(dir string, net *Network) error {
	net.Version = networkVersion
	return saveJSON(filepath.Join(dir, ".shaurma", "network.json"), net)
}

// LoadNetwork завантажує конфіг мережі (зі старого формату — з міграцією:
// раніше «Запустити мережу» запускала всіх і пускала гравців на всіх).
func LoadNetwork(dir string) (*Network, error) {
	var net Network
	if err := loadJSON(filepath.Join(dir, ".shaurma", "network.json"), &net); err != nil {
		return nil, err
	}
	if net.Version < 2 {
		for i := range net.Servers {
			net.Servers[i].AutoStart = true
			net.Servers[i].InTry = true
		}
		net.Proxy = DefaultProxySettings()
		net.StopMembersWithProxy = true
		net.Version = networkVersion
	}
	return &net, nil
}

// ── Імена серверів у мережі ─────────────────────────────────────────────

var aliasTranslit = map[rune]string{
	'а': "a", 'б': "b", 'в': "v", 'г': "h", 'ґ': "g", 'д': "d", 'е': "e", 'є': "ie", 'ж': "zh",
	'з': "z", 'и': "y", 'і': "i", 'ї': "i", 'й': "i", 'к': "k", 'л': "l", 'м': "m", 'н': "n",
	'о': "o", 'п': "p", 'р': "r", 'с': "s", 'т': "t", 'у': "u", 'ф': "f", 'х': "kh", 'ц': "ts",
	'ч': "ch", 'ш': "sh", 'щ': "shch", 'ь': "", 'ю': "iu", 'я': "ia", 'ы': "y", 'э': "e", 'ё': "e", 'ъ': "",
}

// AliasFromName — ім'я сервера в мережі з його назви: латиниця, цифри,
// «-» і «_» (так його зручно набирати в /server). «Виживання 2» → vyzhyvannia-2.
var trailingMCVersion = regexp.MustCompile(`[\s_-]+v?\d+\.\d+(\.\d+)?$`)

func AliasFromName(name string) string {
	// «hub 1.21.4» → hub: версія гри в назві теки імені в мережі не потрібна.
	if short := trailingMCVersion.ReplaceAllString(strings.TrimSpace(name), ""); short != "" {
		name = short
	}
	var sb strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' {
			sb.WriteRune(r)
			dash = false
		} else if tr, ok := aliasTranslit[r]; ok {
			sb.WriteString(tr)
			dash = false
		} else if !dash && sb.Len() > 0 {
			sb.WriteByte('-')
			dash = true
		}
	}
	out := strings.Trim(sb.String(), "-")
	if len(out) > 32 {
		out = strings.Trim(out[:32], "-")
	}
	return out
}

// ValidAlias — чи ім'я придатне для мережі: 1–32 символи [A-Za-z0-9_-]
// (Velocity шукає сервери без урахування регістру; лаунчер пропонує малі).
func ValidAlias(a string) bool {
	if a == "" || len(a) > 32 {
		return false
	}
	for _, r := range a {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

// uniqueAlias додає -2, -3… якщо ім'я вже зайняте.
func uniqueAlias(base string, taken map[string]bool) string {
	if base == "" {
		base = "server"
	}
	a := base
	for n := 2; taken[a]; n++ {
		a = fmt.Sprintf("%s-%d", base, n)
	}
	taken[a] = true
	return a
}

// ── velocity.toml ───────────────────────────────────────────────────────

// VelocityBackend — сервер у [servers] velocity.toml.
type VelocityBackend struct {
	Alias      string
	Address    string // host:port
	InTry      bool
	ForcedHost string
}

func tomlString(s string) string { return strconv.Quote(s) }

// Приклади, які Velocity сам пише в новий velocity.toml, — не сервери мережі.
var velocityDefaultExamples = map[string]string{
	"lobby": "127.0.0.1:30066", "factions": "127.0.0.1:30067", "minigames": "127.0.0.1:30068",
}

// ApplyVelocityConfig переносить мережу в velocity.toml (формат Velocity 3,
// config-version 2.7), змінюючи лише ключі, якими керує лаунчер. Імена
// серверів — у лапках: TOML не дозволяє кирилицю й пробіли в «голих»
// ключах. existing — поточний вміст файлу ("" — файлу ще нема).
func ApplyVelocityConfig(existing string, port int, p ProxySettings, backends []VelocityBackend) string {
	if port <= 0 {
		port = 25577
	}
	if strings.TrimSpace(existing) == "" {
		existing = "# Velocity: ключі мережі керуються з Shaurma Launcher, решту можна змінювати вручну.\n"
	}
	d := parseTOMLDoc(existing)
	q, b, n := tomlString, strconv.FormatBool, strconv.Itoa

	// Старий генератор лаунчера писав неіснуючу секцію — прибираємо.
	d.RemoveSection("player-info-forwarding")

	// Без config-version Velocity вважає файл версії 1.0 і при старті
	// перезаписує forwarding.secret випадковим значенням.
	d.SetIfMissing("", "config-version", q(velocityConfigVersion))
	host := "0.0.0.0"
	if cur, ok := d.GetString("", "bind"); ok {
		if i := strings.LastIndex(cur, ":"); i > 0 {
			host = cur[:i]
		}
	}
	d.Set("", "bind", q(fmt.Sprintf("%s:%d", host, port)))
	d.Set("", "motd", q(p.Motd))
	d.Set("", "show-max-players", n(p.ShowMaxPlayers))
	d.Set("", "online-mode", b(p.OnlineMode))
	d.Set("", "force-key-authentication", b(p.ForceKeyAuthentication))
	d.Set("", "prevent-client-proxy-connections", b(p.PreventClientProxyConnections))
	d.Set("", "player-info-forwarding-mode", q(p.ForwardingMode))
	d.Set("", "forwarding-secret-file", q("forwarding.secret"))
	d.Set("", "announce-forge", b(p.AnnounceForge))
	d.Set("", "kick-existing-players", b(p.KickExistingPlayers))
	d.Set("", "ping-passthrough", q(p.PingPassthrough))

	var body, try, hosts []string
	for _, be := range backends {
		body = append(body, q(be.Alias)+" = "+q(be.Address))
		if be.InTry {
			try = append(try, q(be.Alias))
		}
		if h := strings.TrimSpace(be.ForcedHost); h != "" {
			hosts = append(hosts, q(h)+" = ["+q(be.Alias)+"]")
		}
	}
	// Без try проксі не знає, куди пустити гравця; перший сервер — лобі.
	if len(try) == 0 && len(backends) > 0 {
		try = append(try, q(backends[0].Alias))
	}
	body = append(body, "try = ["+strings.Join(try, ", ")+"]")
	d.ReplaceSectionBody("servers", body)
	// forced-hosts — інлайн-таблицею на верхньому рівні (як у Velocity);
	// секція [forced-hosts] поруч з нею дала б дубль ключа.
	d.RemoveSection("forced-hosts")
	d.Set("", "forced-hosts", "{"+strings.Join(hosts, ", ")+"}")

	d.Set("advanced", "compression-threshold", n(p.CompressionThreshold))
	d.Set("advanced", "compression-level", n(p.CompressionLevel))
	d.Set("advanced", "login-ratelimit", n(p.LoginRatelimit))
	d.Set("advanced", "connection-timeout", n(p.ConnectionTimeout))
	d.Set("advanced", "read-timeout", n(p.ReadTimeout))
	d.Set("advanced", "haproxy-protocol", b(p.HAProxyProtocol))
	d.Set("advanced", "tcp-fast-open", b(p.TCPFastOpen))
	d.Set("advanced", "failover-on-unexpected-server-disconnect", b(p.FailoverOnUnexpectedDisconnect))
	d.Set("advanced", "log-command-executions", b(p.LogCommandExecutions))
	d.Set("advanced", "log-player-connections", b(p.LogPlayerConnections))
	d.Set("query", "enabled", b(p.QueryEnabled))
	d.Set("query", "port", n(p.QueryPort))
	return d.String()
}

// readProxySettings — налаштування проксі з наявного velocity.toml
// (поверх значень за замовчуванням) — для імпорту готової мережі.
func readProxySettings(d *tomlDoc) ProxySettings {
	p := DefaultProxySettings()
	str := func(sec, key string, dst *string) {
		if v, ok := d.GetString(sec, key); ok {
			*dst = v
		}
	}
	boolean := func(sec, key string, dst *bool) {
		if v, ok := d.Get(sec, key); ok {
			*dst = v == "true"
		}
	}
	num := func(sec, key string, dst *int) {
		if v, ok := d.Get(sec, key); ok {
			if i, err := strconv.Atoi(v); err == nil {
				*dst = i
			}
		}
	}
	str("", "motd", &p.Motd)
	num("", "show-max-players", &p.ShowMaxPlayers)
	boolean("", "online-mode", &p.OnlineMode)
	boolean("", "force-key-authentication", &p.ForceKeyAuthentication)
	boolean("", "prevent-client-proxy-connections", &p.PreventClientProxyConnections)
	str("", "player-info-forwarding-mode", &p.ForwardingMode)
	boolean("", "announce-forge", &p.AnnounceForge)
	boolean("", "kick-existing-players", &p.KickExistingPlayers)
	str("", "ping-passthrough", &p.PingPassthrough)
	num("advanced", "compression-threshold", &p.CompressionThreshold)
	num("advanced", "compression-level", &p.CompressionLevel)
	num("advanced", "login-ratelimit", &p.LoginRatelimit)
	num("advanced", "connection-timeout", &p.ConnectionTimeout)
	num("advanced", "read-timeout", &p.ReadTimeout)
	boolean("advanced", "haproxy-protocol", &p.HAProxyProtocol)
	boolean("advanced", "tcp-fast-open", &p.TCPFastOpen)
	boolean("advanced", "failover-on-unexpected-server-disconnect", &p.FailoverOnUnexpectedDisconnect)
	boolean("advanced", "log-command-executions", &p.LogCommandExecutions)
	boolean("advanced", "log-player-connections", &p.LogPlayerConnections)
	boolean("query", "enabled", &p.QueryEnabled)
	num("query", "port", &p.QueryPort)
	// Velocity пише режими великими літерами (MODERN, DISABLED).
	p.ForwardingMode = strings.ToLower(p.ForwardingMode)
	p.PingPassthrough = strings.ToLower(p.PingPassthrough)
	return p
}

var forcedHostRe = regexp.MustCompile(`"([^"]+)"\s*=\s*\[\s*"([^"]+)"`)

// readForcedHosts — ім'я сервера → домен (інлайн-таблиця або секція).
func readForcedHosts(d *tomlDoc) map[string]string {
	out := map[string]string{}
	src := ""
	if v, ok := d.Get("", "forced-hosts"); ok {
		src = v
	}
	if s, e, ok := d.sectionRange("forced-hosts"); ok {
		src += "\n" + strings.Join(d.lines[s:e], "\n")
	}
	for _, m := range forcedHostRe.FindAllStringSubmatch(src, -1) {
		out[m[2]] = m[1]
	}
	return out
}

// sortByOrder сортує сервери за порядком (стабільно).
func sortByOrder(servers []NetworkServer) {
	for i := 1; i < len(servers); i++ {
		for j := i; j > 0 && servers[j].Order < servers[j-1].Order; j-- {
			servers[j], servers[j-1] = servers[j-1], servers[j]
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
