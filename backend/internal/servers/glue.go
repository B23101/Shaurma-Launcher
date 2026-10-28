// glue.go — «склеювання» Velocity-мережі: окрім списку серверів у
// velocity.toml, реальне зʼєднання вимагає player-info-forwarding.
//
// За документацією Velocity/Paper (docs.papermc.io/velocity/
// player-information-forwarding): щоб гравці через проксі потрапляли на
// backend з правильними UUID/скінами, треба
//  1. velocity.toml: player-info-forwarding-mode = "modern" і
//     forwarding-secret-file = "forwarding.secret" (network.go);
//  2. тека проксі: файл forwarding.secret (спільний секрет);
//  3. кожен backend:
//     · server.properties: online-mode=false (автентифікує проксі);
//     · Paper/Purpur 1.19+: config/paper-global.yml → proxies.velocity
//     {enabled, online-mode, secret}; ≤1.18.2: paper.yml →
//     settings.velocity-support;
//     · Fabric/Quilt: мод FabricProxy-Lite → config/FabricProxy-Lite.toml;
//     · Forge/NeoForge: мод Proxy Compatible Forge → config/pcf-common.toml.
//
// Ключі ставляться точково (yamlkv.go), решта конфігів не змінюється, тож
// повторні застосування безпечні.
package servers

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ensureForwardingSecret читає (або створює) forwarding.secret у теці
// проксі й повертає секрет. Секрет спільний: проксі + всі backend-и.
func ensureForwardingSecret(proxyDir string) (string, error) {
	path := filepath.Join(proxyDir, "forwarding.secret")
	if data, err := os.ReadFile(path); err == nil {
		s := strings.TrimSpace(string(data))
		if s != "" {
			return s, nil
		}
	}
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("генерація forwarding.secret: %w", err)
	}
	secret := hex.EncodeToString(raw)
	if err := writeFileAtomic(path, []byte(secret+"\n"), 0644); err != nil {
		return "", fmt.Errorf("запис forwarding.secret: %w", err)
	}
	log.Printf("[servers] створено forwarding.secret для мережі (%s)", filepath.Dir(path))
	return secret, nil
}

// Як backend приймає modern forwarding.
const (
	fwdPaper       = "paper"        // Paper/Purpur — нативно
	fwdFabricProxy = "fabric-proxy" // мод FabricProxy-Lite
	fwdPCF         = "pcf"          // мод Proxy Compatible Forge
	fwdNone        = ""             // не вміє (vanilla, або мод не встановлено)
)

// backendForwarding визначає, чим backend приймає forwarding.
func backendForwarding(srv *Server) string {
	switch srv.Core {
	case CorePaper, CorePurpur:
		return fwdPaper
	case "fabric", "quilt":
		if modJarPresent(srv.Dir, "fabricproxy") {
			return fwdFabricProxy
		}
	case "forge", "neoforge":
		if modJarPresent(srv.Dir, "proxy-compatible-forge", "proxycompatibleforge", "pcf-") {
			return fwdPCF
		}
	}
	return fwdNone
}

// modJarPresent — чи є в mods/ увімкнений jar, ім'я якого містить один із
// фрагментів (без урахування регістру й дефісів/підкреслень).
func modJarPresent(dir string, fragments ...string) bool {
	entries, err := os.ReadDir(filepath.Join(dir, "mods"))
	if err != nil {
		return false
	}
	norm := func(s string) string {
		return strings.NewReplacer("-", "", "_", "", " ", "").Replace(strings.ToLower(s))
	}
	for _, e := range entries {
		name := strings.ToLower(e.Name())
		if !strings.HasSuffix(name, ".jar") {
			continue
		}
		for _, f := range fragments {
			if strings.Contains(norm(name), norm(f)) {
				return true
			}
		}
	}
	return false
}

// mcVersionAtLeast — чи версія (major.minor…) не нижча за (major, minor).
func mcVersionAtLeast(version string, major, minor int) bool {
	v := strings.TrimSpace(version)
	parts := strings.Split(v, ".")
	if len(parts) < 2 {
		return false
	}
	var mj, mn int
	if _, err := fmt.Sscanf(parts[0], "%d", &mj); err != nil {
		return false
	}
	if _, err := fmt.Sscanf(parts[1], "%d", &mn); err != nil {
		return false
	}
	if mj != major {
		return mj > major
	}
	return mn >= minor
}

// paperForwardingPath — файл і шлях ключів velocity-налаштувань Paper.
// Спершу — який файл уже є (версію з теки не завжди вдається визначити),
// далі — за версією гри.
func paperForwardingPath(srv *Server) (file string, base []string) {
	global := filepath.Join(srv.Dir, "config", "paper-global.yml")
	legacy := filepath.Join(srv.Dir, "paper.yml")
	switch {
	case fileExists(global):
		return global, []string{"proxies", "velocity"}
	case fileExists(legacy) && !mcVersionAtLeast(srv.MCVersion, 1, 19):
		return legacy, []string{"settings", "velocity-support"}
	}
	if srv.MCVersion == "" || mcVersionAtLeast(srv.MCVersion, 1, 19) {
		return filepath.Join(srv.Dir, "config", "paper-global.yml"), []string{"proxies", "velocity"}
	}
	return filepath.Join(srv.Dir, "paper.yml"), []string{"settings", "velocity-support"}
}

// quoteYAML — скаляр у одинарних лапках.
func quoteYAML(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// setYAMLFile ставить кілька ключів у YAML-файл (створює файл, якщо нема).
func setYAMLFile(path string, base []string, kv [][2]string) error {
	data, _ := os.ReadFile(path)
	doc := string(data)
	for _, p := range kv {
		doc = setYAMLScalar(doc, append(append([]string{}, base...), p[0]), p[1])
	}
	if !strings.HasSuffix(doc, "\n") {
		doc += "\n"
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return writeFileAtomic(path, []byte(doc), 0644)
}

var tomlKeyRe = func(key string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^(\s*)` + regexp.QuoteMeta(key) + `\s*=.*$`)
}

// setTOMLFile ставить ключ=значення у простий TOML-конфіг мода (рядок з
// ключем замінюється, інакше дописується під section або в кінець).
func setTOMLFile(path, section, key, value string) error {
	data, _ := os.ReadFile(path)
	doc := string(data)
	re := tomlKeyRe(key)
	if re.MatchString(doc) {
		doc = re.ReplaceAllString(doc, "${1}"+key+" = "+value)
	} else {
		if section != "" && !strings.Contains(doc, "["+section+"]") {
			doc += "\n[" + section + "]\n"
		}
		if section != "" {
			i := strings.Index(doc, "["+section+"]") + len(section) + 2
			doc = doc[:i] + "\n\t" + key + " = " + value + doc[i:]
		} else {
			doc += key + " = " + value + "\n"
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return writeFileAtomic(path, []byte(doc), 0644)
}

// applyBackendForwarding налаштовує ОДИН backend під forwarding проксі.
// proxyOnline — чи проксі сам перевіряє акаунти Mojang (Paper має знати
// те саме, інакше офлайн-гравцям ламаються UUID); lock — слухати лише
// 127.0.0.1 (вхід тільки через проксі).
func applyBackendForwarding(srv *Server, secret, mode string, proxyOnline, lock bool) error {
	if srv == nil || srv.Dir == "" {
		return fmt.Errorf("сервер без теки")
	}
	_ = os.MkdirAll(srv.Dir, 0755)

	// server.properties: проксі автентифікує гравців сам.
	props := []PropUpdate{{Key: "online-mode", Value: "false"}}
	// Сервер ще не запускався — файл створиться з дефолтним 25565; порт
	// сервера з налаштувань має зберегтися, інакше він мовчки змінився б.
	if !fileExists(filepath.Join(srv.Dir, "server.properties")) && srv.Port > 0 {
		props = append(props, PropUpdate{Key: "server-port", Value: fmt.Sprint(srv.Port)})
	}
	if lock {
		props = append(props, PropUpdate{Key: "server-ip", Value: "127.0.0.1"})
	}
	if err := WriteServerProperties(srv.Dir, props); err != nil {
		return fmt.Errorf("server.properties (%s): %w", srv.Name, err)
	}

	modern := mode == "modern"
	switch backendForwarding(srv) {
	case fwdPaper:
		file, base := paperForwardingPath(srv)
		kv := [][2]string{
			{"enabled", fmt.Sprint(modern)},
			{"online-mode", fmt.Sprint(proxyOnline)},
			{"secret", quoteYAML(secret)},
		}
		if err := setYAMLFile(file, base, kv); err != nil {
			return fmt.Errorf("%s (%s): %w", filepath.Base(file), srv.Name, err)
		}
		// Legacy/BungeeGuard — через spigot.yml settings.bungeecord.
		if err := setYAMLFile(filepath.Join(srv.Dir, "spigot.yml"), []string{"settings"},
			[][2]string{{"bungeecord", fmt.Sprint(mode == "legacy" || mode == "bungeeguard")}}); err != nil {
			return fmt.Errorf("spigot.yml (%s): %w", srv.Name, err)
		}
	case fwdFabricProxy:
		f := filepath.Join(srv.Dir, "config", "FabricProxy-Lite.toml")
		if err := setTOMLFile(f, "", "secret", fmt.Sprintf("%q", secret)); err != nil {
			return err
		}
		return setTOMLFile(f, "", "hackOnlineMode", "true")
	case fwdPCF:
		f := filepath.Join(srv.Dir, "config", "pcf-common.toml")
		return setTOMLFile(f, "modernForwarding", "forwardingSecret", fmt.Sprintf("%q", secret))
	}
	return nil
}

// ApplyNetworkGlue налаштовує forwarding для всієї мережі: секрет +
// конфіг кожного backend-а. Повертає помилки по серверах (ID → текст).
func ApplyNetworkGlue(proxy *Server, servers map[string]*Server, net *Network) map[string]string {
	errs := map[string]string{}
	if proxy == nil {
		errs[""] = "проксі не знайдено"
		return errs
	}
	secret, err := ensureForwardingSecret(proxy.Dir)
	if err != nil {
		errs[proxy.ID] = err.Error()
		return errs
	}
	for _, ns := range net.Members() {
		srv := servers[ns.ServerID]
		if srv == nil {
			continue
		}
		if err := applyBackendForwarding(srv, secret, net.Proxy.ForwardingMode, net.Proxy.OnlineMode, net.LockBackends); err != nil {
			log.Printf("[servers] glue %s: %v", srv.Name, err)
			errs[srv.ID] = err.Error()
		}
	}
	return errs
}
