// p2p_voice.go — єдина точка істини для вкладки «Мережа» сервера:
//
//   - збережені налаштування голосу (UDP) + що РЕАЛЬНО встановлено на сервері
//     (Simple Voice Chat / Plasmo Voice і їхній порт з конфігів, де -1/0 = порт сервера);
//   - жива діагностика P2P-хоста: не «онлайн», а «гість реально зможе зайти» — і якщо
//     ні, то чому саме;
//   - автозапуск P2P разом із сервером (на бекенді, а не у відкритій сторінці UI — раніше
//     тунель не підіймався, якщо сервер стартував не зі сторінки сервера).
package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"shaurma-launcher-backend/internal/p2p"
	"shaurma-launcher-backend/internal/servers"
	"shaurma-launcher-backend/internal/voicecfg"
)

// P2PCheck — один рядок чек-листа «чому гості можуть/не можуть зайти».
type P2PCheck struct {
	ID     string `json:"id"`
	Level  string `json:"level"` // ok | info | warn | error
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
	// Fix — ідентифікатор дії, яку UI може запропонувати кнопкою:
	// "udp_on" | "udp_auto" | "start_p2p" | "restart_p2p" | "recheck".
	Fix string `json:"fix,omitempty"`
}

// P2PVoiceState — усе, що потрібно UI вкладки «Мережа» одним викликом.
type P2PVoiceState struct {
	ServerID   string `json:"serverId"`
	ServerPort int    `json:"serverPort"`
	Subdomain  string `json:"subdomain"`
	// Saved — налаштування UDP уже зберігались; Enabled/CustomPort — що збережено
	// (CustomPort 0 = «Авто»: порт підбирається з конфігу голосового моду).
	Saved      bool `json:"saved"`
	Enabled    bool `json:"enabled"`
	CustomPort int  `json:"customPort"`
	// Voice — що знайдено на сервері та які проблеми з портами.
	Voice voicecfg.Verdict `json:"voice"`
	// Host — жива картина тунелю.
	Host p2p.HostLive `json:"host"`
	// ServerStatus — стан процесу сервера (running/stopped/…).
	ServerStatus string `json:"serverStatus"`
	// Level/Headline — загальний вирок: off | ok | warn | error.
	Level    string     `json:"level"`
	Headline string     `json:"headline"`
	Checks   []P2PCheck `json:"checks"`
	// AutoStart / AutoStartError — автозапуск разом із сервером і чому він не спрацював.
	AutoStart      bool   `json:"autoStart"`
	AutoStartError string `json:"autoStartError,omitempty"`
}

var (
	p2pAutoMu   sync.Mutex
	p2pAutoLock = map[string]*sync.Mutex{}
	p2pAutoErr  = map[string]string{}
)

func p2pServerLock(id string) *sync.Mutex {
	p2pAutoMu.Lock()
	defer p2pAutoMu.Unlock()
	m := p2pAutoLock[id]
	if m == nil {
		m = &sync.Mutex{}
		p2pAutoLock[id] = m
	}
	return m
}

func setP2PAutoErr(id, msg string) {
	p2pAutoMu.Lock()
	defer p2pAutoMu.Unlock()
	if msg == "" {
		delete(p2pAutoErr, id)
	} else {
		p2pAutoErr[id] = msg
	}
}

func getP2PAutoErr(id string) string {
	p2pAutoMu.Lock()
	defer p2pAutoMu.Unlock()
	return p2pAutoErr[id]
}

// p2pSubdomainFor — адреса, застовблена під цей сервер (label == serverID).
func (a *App) p2pSubdomainFor(serverID string) string {
	if a.p2pAPI == nil {
		return ""
	}
	accountID, _, err := a.p2pAccountCheck()
	if err != nil {
		return ""
	}
	for _, addr := range a.p2pAPI.ListClaimedAddresses(accountID) {
		if addr.Label == serverID {
			return addr.Subdomain
		}
	}
	return ""
}

// p2pEffectiveUDP — UDP-порт, який треба тунелювати для сервера (0 = голос вимкнено).
func (a *App) p2pEffectiveUDP(srv servers.ServerView) int {
	enabled, custom := a.p2pReadPref(srv)
	if !enabled {
		return 0
	}
	v := voicecfg.Evaluate(voicecfg.Detect(srv.Dir, srv.Port), srv.Port, true, custom, srv.Core == "velocity")
	if v.EffectivePort < 1 || v.EffectivePort > 65535 {
		return 0
	}
	return v.EffectivePort
}

// p2pReadPref читає збережені налаштування. Старі налаштування, де порт дорівнював порту
// сервера (дефолт старого віджета), трактуємо як «Авто»: саме через них голос ламався,
// коли Simple Voice Chat слухав свій 24454, а лаунчер тунелював 25565.
func (a *App) p2pReadPref(srv servers.ServerView) (enabled bool, custom int) {
	if a.p2pAPI == nil {
		return false, 0
	}
	p, ok := a.p2pAPI.GetUDPPref(srv.ID)
	if !ok {
		return false, 0
	}
	custom = p.Port
	if custom == srv.Port {
		custom = 0
	}
	return p.Enabled, custom
}

// P2PGetVoiceState — повна картина для вкладки «Мережа». force=true змушує повторити
// самоперевірку lobby (кнопка «Перевірити»), інакше береться кеш на 20 с.
func (a *App) P2PGetVoiceState(serverID string, force bool) P2PVoiceState {
	st := P2PVoiceState{ServerID: serverID, Level: "off", Checks: []P2PCheck{}}
	srv, err := a.srvAPI.GetServer(serverID)
	if err != nil {
		st.Headline = "Сервер не знайдено"
		return st
	}
	st.ServerPort = srv.Port
	st.AutoStart = srv.AutoStartP2P
	st.AutoStartError = getP2PAutoErr(serverID)
	st.ServerStatus = a.srvAPI.Manager().GetStatus(serverID).Status
	st.Subdomain = a.p2pSubdomainFor(serverID)
	enabled, custom := a.p2pReadPref(srv)
	if a.p2pAPI != nil {
		_, st.Saved = a.p2pAPI.GetUDPPref(serverID)
	}
	st.Enabled, st.CustomPort = enabled, custom
	st.Voice = voicecfg.Evaluate(voicecfg.Detect(srv.Dir, srv.Port), srv.Port, enabled, custom, srv.Core == "velocity")
	if a.p2pAPI != nil && st.Subdomain != "" {
		st.Host = a.p2pAPI.HostLive(st.Subdomain, true, force)
	}
	a.p2pBuildChecks(&st, srv)
	return st
}

func (a *App) p2pBuildChecks(st *P2PVoiceState, srv servers.ServerView) {
	add := func(id, level, title, detail, fix string) {
		st.Checks = append(st.Checks, P2PCheck{ID: id, Level: level, Title: title, Detail: detail, Fix: fix})
	}
	h := st.Host
	serverUp := st.ServerStatus == "running"

	switch {
	case a.p2pDisabled():
		add("disabled", "error", "P2P вимкнено", "Функцію P2P вимкнено в цій збірці/налаштуваннях лаунчера.", "")
	case st.Subdomain == "":
		add("no_address", "warn", "Для сервера ще немає P2P-адреси", "Створи адресу — тоді друзі зможуть зайти без білого IP.", "")
	}

	if !h.Active {
		if st.Subdomain != "" && !a.p2pDisabled() {
			add("tunnel_off", "info", "P2P-тунель не запущено", "Друзі не зможуть зайти, поки тунель не запущено.", "start_p2p")
		}
	} else {
		if !h.Alive {
			add("tunnel_dead", "error", "Тунель мертвий", "Сесія P2P зупинилась. Перезапусти P2P.", "restart_p2p")
		}
		if h.Disabled {
			add("room_disabled", "error", "Кімнату вимкнено адміністратором", "Гості отримуватимуть відмову, доки кімнату не ввімкнуть знову.", "")
		}
		if !serverUp {
			add("server_down", "error", "Minecraft-сервер не запущено", "Тунель піднято, але гравцям нема куди заходити. Запусти сервер.", "")
		} else if !h.LocalGameOK {
			detail := h.LocalGameErr
			if ip := serverIPProp(srv.Dir); ip != "" && ip != "0.0.0.0" && ip != "127.0.0.1" {
				detail += ". У server.properties задано server-ip=" + ip + " — тунель стукає на 127.0.0.1, тож залиш server-ip порожнім."
			}
			add("game_unreachable", "error", "Сервер не відповідає на порту "+h.LocalPort, detail, "")
		} else {
			add("game_ok", "ok", "Minecraft-сервер відповідає на порту "+h.LocalPort, "", "")
		}
		if h.PublishError != "" {
			add("publish_failed", "error", "Адресу не вдалося опублікувати в lobby", h.PublishError, "restart_p2p")
		}
		switch {
		case h.Self.Checked && h.Self.OK:
			add("lobby_ok", "ok", "Адреса резолвиться: гості бачать твій тунель", "", "")
		case h.Self.Checked && !h.Self.OK:
			add("lobby_mismatch", "error", "Гості не дістануться до тебе", h.Self.Problem, "restart_p2p")
		case h.Self.Problem != "":
			add("lobby_unknown", "warn", "Не вдалося перевірити адресу в lobby", h.Self.Problem, "recheck")
		}
		if h.UDPError != "" {
			add("udp_listen", "error", "UDP-тунель голосу не піднявся", h.UDPError+". Найчастіше порт зайнятий іншою програмою.", "")
		}
		if h.UDPPort != "" && h.UDP.PktsOut >= 10 && h.UDP.PktsIn == 0 {
			add("udp_silent", "warn", "Гості шлють голос, але мод на сервері мовчить",
				"Через тунель уже пройшло "+strconv.FormatInt(h.UDP.PktsOut, 10)+" UDP-пакетів, відповідей 0. Перевір, що голосовий мод справді слухає порт "+h.UDPPort+".", "udp_auto")
		}
		if h.UDPPort != "" && h.UDPError == "" && !(h.UDP.PktsOut >= 10 && h.UDP.PktsIn == 0) {
			add("udp_ok", "ok", "Голосовий UDP тунелюється на порт "+h.UDPPort, "", "")
		}
		if n := h.Guests; n > 0 {
			add("guests", "info", "Гравців зараз через P2P: "+strconv.Itoa(n), "", "")
		}
	}

	// Голос: попередження про мод/порти.
	for _, is := range st.Voice.Issues {
		fix := ""
		switch is.Code {
		case "voice_found_udp_off":
			fix = "udp_on"
		case "udp_port_mismatch":
			fix = "udp_auto"
		}
		add("voice_"+is.Code, string(is.Severity), is.Message, "", fix)
	}

	if serverUp && whitelistOn(srv.Dir) {
		add("whitelist", "warn", "На сервері ввімкнено whitelist", "Друзі зможуть зайти лише якщо їхні ніки додано в whitelist (server.properties: white-list=true).", "")
	}

	// Автозапуск, що не спрацював.
	if st.AutoStartError != "" {
		add("autostart_failed", "warn", "Автозапуск P2P не спрацював", st.AutoStartError, "start_p2p")
	}

	// Вирок: найгірша перевірка. Коли тунель не запущено — «off», окрім жорстких блокерів.
	rank := func(l string) int {
		switch l {
		case "error":
			return 2
		case "warn":
			return 1
		}
		return 0
	}
	level, headline := "ok", "Усе гаразд: друзі можуть заходити"
	for _, c := range st.Checks {
		if rank(c.Level) > rank(level) {
			level, headline = c.Level, c.Title
		}
	}
	if !h.Active && level != "error" && st.Subdomain != "" {
		level, headline = "off", "P2P не запущено"
	}
	// «Усе гаразд» — лише якщо самоперевірка справді підтвердила резолв адреси.
	if level == "ok" && h.Active && !(h.Self.Checked && h.Self.OK) {
		level, headline = "warn", "Тунель піднято, але доступність для друзів ще не підтверджено"
	}
	st.Level, st.Headline = level, headline
}

func readServerProp(dir, key string) string {
	b, err := os.ReadFile(filepath.Join(dir, "server.properties"))
	if err != nil {
		return ""
	}
	for _, ln := range strings.Split(string(b), "\n") {
		ln = strings.TrimSpace(ln)
		if strings.HasPrefix(ln, key+"=") {
			return strings.TrimSpace(strings.TrimPrefix(ln, key+"="))
		}
	}
	return ""
}

func serverIPProp(dir string) string { return readServerProp(dir, "server-ip") }

func whitelistOn(dir string) bool {
	return strings.EqualFold(readServerProp(dir, "white-list"), "true")
}

// P2PSetVoice ЗБЕРІГАЄ налаштування голосу і одразу застосовує їх до запущеного тунелю —
// без перезапуску. customPort 0 = «Авто» (порт береться з конфігу голосового моду).
func (a *App) P2PSetVoice(serverID string, enabled bool, customPort int) (P2PVoiceState, error) {
	if a.p2pDisabled() {
		return a.P2PGetVoiceState(serverID, false), errP2PDisabled()
	}
	srv, err := a.srvAPI.GetServer(serverID)
	if err != nil {
		return P2PVoiceState{ServerID: serverID}, err
	}
	if customPort < 0 || customPort > 65535 {
		customPort = 0
	}
	l := p2pServerLock(serverID)
	l.Lock()
	defer l.Unlock()
	if err := a.p2pAPI.SetUDPPref(serverID, enabled, customPort); err != nil {
		return a.P2PGetVoiceState(serverID, false), err
	}
	var applyErr error
	if sub := a.p2pSubdomainFor(serverID); sub != "" {
		if a.p2pAPI.GetP2PHostStatus(sub).Active {
			_, applyErr = a.p2pAPI.SetHostUDP(sub, a.p2pEffectiveUDP(srv))
		}
	}
	return a.P2PGetVoiceState(serverID, true), applyErr
}

// P2PEnsureAuto — синхронізує тунель зі станом сервера за прапорцем «автозапуск»
// (викликається при зміні прапорця й при зміні статусу сервера).
func (a *App) P2PEnsureAuto(serverID string) {
	go a.p2pAutoSync(serverID)
}

func (a *App) p2pAutoSync(serverID string) {
	if a.p2pAPI == nil || a.p2pDisabled() {
		return
	}
	srv, err := a.srvAPI.GetServer(serverID)
	if err != nil || !srv.AutoStartP2P {
		return
	}
	l := p2pServerLock(serverID)
	l.Lock()
	defer l.Unlock()
	status := a.srvAPI.Manager().GetStatus(serverID).Status
	sub := a.p2pSubdomainFor(serverID)
	if sub == "" {
		setP2PAutoErr(serverID, "Немає P2P-адреси для цього сервера — створи її у вкладці «Мережа».")
		a.emit("p2p:autostart", map[string]any{"serverId": serverID, "ok": false, "error": getP2PAutoErr(serverID)})
		return
	}
	active := a.p2pAPI.GetP2PHostStatus(sub).Active
	switch status {
	case "running":
		want := a.p2pEffectiveUDP(srv)
		if active {
			// Тунель уже піднято (вручну чи раніше) — лише синхронізуємо голос, без перезапуску.
			_, _ = a.p2pAPI.SetHostUDP(sub, want)
			setP2PAutoErr(serverID, "")
			return
		}
		// Сервер уже «Done», але порт може ще кілька секунд не приймати з'єднання.
		waitLocalPort(srv.Port, 20*time.Second)
		if _, err := a.StartP2PHost(strconv.Itoa(srv.Port), sub, want); err != nil {
			setP2PAutoErr(serverID, err.Error())
			a.emit("p2p:autostart", map[string]any{"serverId": serverID, "ok": false, "error": err.Error()})
			return
		}
		setP2PAutoErr(serverID, "")
		a.emit("p2p:autostart", map[string]any{"serverId": serverID, "ok": true})
	case "stopped", "crashed", "error":
		if active {
			_ = a.StopP2PHost(sub)
		}
		setP2PAutoErr(serverID, "")
	}
}

func waitLocalPort(port int, max time.Duration) bool {
	deadline := time.Now().Add(max)
	for {
		c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 500*time.Millisecond)
		if err == nil {
			_ = c.Close()
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// P2PStartForServer піднімає (або перезапускає) P2P-тунель сервера з ПРАВИЛЬНИМ UDP-портом —
// його рахує бекенд із збережених налаштувань і конфігів голосового моду, а не UI.
func (a *App) P2PStartForServer(serverID string) (p2p.HostStatus, error) {
	if a.p2pDisabled() {
		return p2p.HostStatus{Error: errP2PDisabled().Error()}, errP2PDisabled()
	}
	srv, err := a.srvAPI.GetServer(serverID)
	if err != nil {
		return p2p.HostStatus{Error: err.Error()}, err
	}
	sub := a.p2pSubdomainFor(serverID)
	if sub == "" {
		e := fmt.Errorf("для цього сервера ще немає P2P-адреси — створи її")
		return p2p.HostStatus{Error: e.Error()}, e
	}
	l := p2pServerLock(serverID)
	l.Lock()
	defer l.Unlock()
	if a.p2pAPI.GetP2PHostStatus(sub).Active {
		_ = a.p2pAPI.StopP2PHost(sub)
	}
	res, err := a.StartP2PHost(strconv.Itoa(srv.Port), sub, a.p2pEffectiveUDP(srv))
	if err == nil {
		setP2PAutoErr(serverID, "")
	}
	return res, err
}
