// Package voicecfg — визначає, які голосові чати (Simple Voice Chat, Plasmo Voice)
// встановлені на локальному сервері, і який UDP-порт вони РЕАЛЬНО слухатимуть.
//
// Навіщо: P2P-тунель голосу форвардить UDP на 127.0.0.1:<порт>. Якщо порт у
// лаунчері не збігається з портом у конфігу голосового моду — голос мовчки не
// працює. Тут ми читаємо конфіги самих модів і рахуємо ефективний порт:
//
//   - Simple Voice Chat: voicechat-server.properties, ключ port=
//     (-1 = порт Minecraft-сервера; за замовчуванням 24454);
//   - Plasmo Voice:      config.toml / server.toml, секція [host], ключ port
//     (0 = порт Minecraft-сервера).
//
// Пакет не залежить від решти лаунчера (лише stdlib), тож легко тестується.
package voicecfg

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Kind — тип голосового рішення.
type Kind string

const (
	KindSVC    Kind = "svc"    // Simple Voice Chat
	KindPlasmo Kind = "plasmo" // Plasmo Voice
)

// DefaultSVCPort — порт Simple Voice Chat, якщо конфіг ще не згенеровано.
const DefaultSVCPort = 24454

// Solution — одне знайдене голосове рішення.
type Solution struct {
	Kind Kind   `json:"kind"`
	Name string `json:"name"`
	// Where — де лежить jar: "mod" (mods/) або "plugin" (plugins/).
	Where string `json:"where"`
	// Enabled — jar не вимкнений (.disabled).
	Enabled bool `json:"enabled"`
	// File — ім'я jar-файлу.
	File string `json:"file"`
	// ConfigPath — шлях до знайденого конфігу ('' = конфіг ще не створено).
	ConfigPath string `json:"configPath,omitempty"`
	// RawPort — значення з конфігу як є (0/-1 мають спеціальний зміст). Валідне лише коли PortSet.
	RawPort int  `json:"rawPort"`
	PortSet bool `json:"portSet"`
	// Port — ЕФЕКТИВНИЙ UDP-порт (з урахуванням -1/0 → порт сервера).
	Port int `json:"port"`
	// SameAsServer — порт у конфігу означає «як порт Minecraft-сервера».
	SameAsServer bool `json:"sameAsServer"`
	// BindAddr — bind_address / host.ip, якщо задано не-універсальну адресу.
	BindAddr string `json:"bindAddr,omitempty"`
}

// Detect сканує mods/ і plugins/ сервера. serverPort — порт Minecraft (для -1/0).
func Detect(serverDir string, serverPort int) []Solution {
	var out []Solution
	for _, where := range []string{"mods", "plugins"} {
		entries, err := os.ReadDir(filepath.Join(serverDir, where))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			low := strings.ToLower(name)
			enabled := true
			if strings.HasSuffix(low, ".disabled") {
				enabled = false
				low = strings.TrimSuffix(low, ".disabled")
			}
			if !strings.HasSuffix(low, ".jar") {
				continue
			}
			kind, label, ok := classify(low)
			if !ok {
				continue
			}
			w := "mod"
			if where == "plugins" {
				w = "plugin"
			}
			s := Solution{Kind: kind, Name: label, Where: w, Enabled: enabled, File: name}
			fillConfig(&s, serverDir, serverPort)
			out = append(out, s)
		}
	}
	return out
}

// classify розпізнає голосовий мод за іменем jar. Аддони (voicechat-interaction,
// plasmo-voice-addons тощо) НЕ рахуються окремим голосовим сервером.
func classify(lowJar string) (Kind, string, bool) {
	if strings.Contains(lowJar, "addon") || strings.Contains(lowJar, "interaction") ||
		strings.Contains(lowJar, "proxy") || strings.Contains(lowJar, "velocity") ||
		strings.Contains(lowJar, "bungee") {
		return "", "", false
	}
	switch {
	case strings.Contains(lowJar, "plasmovoice") || strings.Contains(lowJar, "plasmo-voice") || strings.Contains(lowJar, "plasmo_voice"):
		return KindPlasmo, "Plasmo Voice", true
	case strings.Contains(lowJar, "voicechat") || strings.Contains(lowJar, "voice-chat") || strings.Contains(lowJar, "voice_chat"):
		return KindSVC, "Simple Voice Chat", true
	}
	return "", "", false
}

func fillConfig(s *Solution, serverDir string, serverPort int) {
	switch s.Kind {
	case KindSVC:
		s.Port = DefaultSVCPort
		for _, p := range []string{
			filepath.Join(serverDir, "config", "voicechat", "voicechat-server.properties"),
			filepath.Join(serverDir, "plugins", "voicechat", "voicechat-server.properties"),
		} {
			kv, ok := readProperties(p)
			if !ok {
				continue
			}
			s.ConfigPath = p
			if v, has := kv["port"]; has {
				if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
					s.RawPort, s.PortSet = n, true
				}
			}
			if v := strings.TrimSpace(kv["bind_address"]); v != "" && !isAnyAddr(v) {
				s.BindAddr = v
			}
			break
		}
		if s.PortSet {
			if s.RawPort == -1 {
				s.Port, s.SameAsServer = serverPort, true
			} else if s.RawPort > 0 && s.RawPort <= 65535 {
				s.Port = s.RawPort
			}
		}
	case KindPlasmo:
		s.Port, s.SameAsServer = serverPort, true // дефолт Plasmo: port = 0 → порт сервера
		for _, p := range []string{
			filepath.Join(serverDir, "config", "plasmovoice", "config.toml"),
			filepath.Join(serverDir, "plugins", "PlasmoVoice", "config.toml"),
			filepath.Join(serverDir, "plugins", "plasmovoice", "config.toml"),
			filepath.Join(serverDir, "config", "plasmovoice", "server.toml"),
		} {
			port, ip, found, ok := readPlasmoHost(p)
			if !ok {
				continue
			}
			s.ConfigPath = p
			if ip != "" && !isAnyAddr(ip) {
				s.BindAddr = ip
			}
			if found {
				s.RawPort, s.PortSet = port, true
				if port <= 0 {
					s.Port, s.SameAsServer = serverPort, true
				} else if port <= 65535 {
					s.Port, s.SameAsServer = port, false
				}
			}
			break
		}
	}
}

func isAnyAddr(a string) bool {
	a = strings.Trim(strings.TrimSpace(a), `"'`)
	return a == "" || a == "0.0.0.0" || a == "::" || a == "127.0.0.1" || a == "localhost"
}

func readProperties(path string) (map[string]string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	kv := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' || line[0] == '!' {
			continue
		}
		i := strings.IndexAny(line, "=:")
		if i <= 0 {
			continue
		}
		kv[strings.TrimSpace(line[:i])] = strings.TrimSpace(line[i+1:])
	}
	return kv, true
}

// readPlasmoHost читає port/ip з секції [host] (або [host.internal]) TOML-конфігу
// Plasmo Voice. Мінімальний парсер: нам потрібні лише два ключі.
func readPlasmoHost(path string) (port int, ip string, found bool, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, "", false, false
	}
	defer f.Close()
	section := ""
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(stripTomlComment(sc.Text()))
		if line == "" {
			continue
		}
		if line[0] == '[' {
			section = strings.ToLower(strings.Trim(line, "[] \t"))
			continue
		}
		if section != "host" && section != "host.internal" {
			continue
		}
		i := strings.Index(line, "=")
		if i <= 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(line[:i]))
		val := strings.Trim(strings.TrimSpace(line[i+1:]), `"'`)
		switch key {
		case "port":
			if n, err := strconv.Atoi(val); err == nil {
				port, found = n, true
			}
		case "ip":
			ip = val
		}
	}
	return port, ip, found, true
}

func stripTomlComment(s string) string {
	inStr := false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			inStr = !inStr
		case '#':
			if !inStr {
				return s[:i]
			}
		}
	}
	return s
}

// Severity — важливість проблеми для UI.
type Severity string

const (
	SevInfo  Severity = "info"
	SevWarn  Severity = "warn"
	SevError Severity = "error"
)

// Issue — одна конкретна проблема/підказка голосу. Code стабільний — UI мапить на текст.
type Issue struct {
	Code     string   `json:"code"`
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
}

// Verdict — підсумок для вкладки «Мережа».
type Verdict struct {
	Solutions []Solution `json:"solutions"`
	// AutoPort — порт, який лаунчер має тунелювати, якщо користувач не задав свій (0 = невідомо).
	AutoPort int `json:"autoPort"`
	// EffectivePort — порт, який реально піде в тунель (з урахуванням вибору користувача).
	EffectivePort int     `json:"effectivePort"`
	Issues        []Issue `json:"issues"`
}

// Evaluate порівнює налаштування користувача з тим, що реально встановлено.
// udpOn/userPort — збережені налаштування (userPort 0 = авто).
func Evaluate(sols []Solution, serverPort int, udpOn bool, userPort int, isProxy bool) Verdict {
	v := Verdict{Solutions: sols, Issues: []Issue{}}
	if v.Solutions == nil {
		v.Solutions = []Solution{}
	}
	if isProxy {
		return v
	}
	var active []Solution
	for _, s := range sols {
		if s.Enabled {
			active = append(active, s)
		}
	}
	if len(active) > 0 {
		v.AutoPort = active[0].Port
	} else {
		v.AutoPort = serverPort
	}
	v.EffectivePort = userPort
	if userPort <= 0 {
		v.EffectivePort = v.AutoPort
	}
	add := func(code string, sev Severity, msg string) { v.Issues = append(v.Issues, Issue{code, sev, msg}) }

	if len(active) == 0 {
		if len(sols) > 0 {
			add("voice_disabled_mod", SevWarn, "Голосовий мод є, але вимкнений (.disabled) — голосу на сервері не буде.")
		}
		if udpOn {
			add("udp_no_voice_mod", SevWarn, "UDP-тунель увімкнено, але на сервері не знайдено Simple Voice Chat / Plasmo Voice — тунелю нема що проксіювати.")
		}
		return v
	}
	if !udpOn {
		add("voice_found_udp_off", SevWarn, active[0].Name+" встановлено, але голос через P2P вимкнено — друзі зайдуть, та не почують одне одного. Увімкни UDP.")
	}
	if len(active) > 1 {
		if active[0].Port == active[1].Port {
			add("voice_conflict", SevError, "Увімкнено два голосові моди ("+active[0].Name+" і "+active[1].Name+") на одному UDP-порту "+strconv.Itoa(active[0].Port)+" — один із них не запуститься. Вимкни один.")
		} else {
			add("voice_two_mods", SevWarn, "Увімкнено два голосові моди — тунелюється лише один порт. Вимкни непотрібний.")
		}
	}
	for _, s := range active {
		if s.ConfigPath == "" {
			add("voice_no_config", SevInfo, s.Name+": конфіг ще не створено (сервер не запускався з цим модом) — очікуваний порт "+strconv.Itoa(s.Port)+".")
		}
		if s.BindAddr != "" {
			add("voice_bind", SevWarn, s.Name+": голос прив'язано до "+s.BindAddr+" — тунель шле на 127.0.0.1 і може не дістатися. Постав bind на порожній/0.0.0.0.")
		}
	}
	if udpOn && userPort > 0 && userPort != v.AutoPort {
		add("udp_port_mismatch", SevError, "Порт тунелю ("+strconv.Itoa(userPort)+") не збігається з портом "+active[0].Name+" ("+strconv.Itoa(v.AutoPort)+") — голос не працюватиме. Постав «Авто».")
	}
	if udpOn && active[0].SameAsServer {
		add("voice_same_port", SevInfo, active[0].Name+" використовує порт сервера ("+strconv.Itoa(serverPort)+", значення "+strconv.Itoa(active[0].RawPort)+" у конфігу) — тунель підхопить його автоматично.")
	}
	return v
}
