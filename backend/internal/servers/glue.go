// glue.go — «склеювання» Velocity-мережі: окрім списку серверів у
// velocity.toml, реальне зʼєднання вимагає player-info-forwarding.
//
// За офіційною документацією Velocity/Paper (docs.papermc.io/velocity/
// player-information-forwarding): щоб гравці через проксі потрапляли на
// backend з правильними UUID/скінами, треба
//  1. velocity.toml: [player-info-forwarding] mode = "modern";
//  2. тека проксі: файл forwarding.secret (спільний секрет);
//  3. кожен Paper/Purpur backend:
//     · server.properties: online-mode=false (автентифікує проксі);
//     · 1.19.3+: config/paper-global.yml: proxies.velocity.enabled=true,
//     proxies.velocity.online-mode=true, proxies.velocity.secret="…";
//     · 1.18.2 і нижче: paper.yml: settings.velocity-support.{enabled,
//     online-mode, secret}.
//
// Ці файли дописуються АКУРАТНО (лаунчер лише додає свої ключі, чужі
// рядки лишає), тому повторні застосування безпечні. Paper при першому
// старті сам розгорне повний конфіг на основі наших ключів — вони
// збережуться (Paper зливає дефолти з існуючим файлом).
package servers

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
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

// velocityForwardingSupported каже, чи ядро backend-а вміє modern forwarding
// нативно (без додаткових модів): Paper та його форки (Purpur).
func velocityForwardingSupported(core string) bool {
	return core == "paper" || core == "purpur"
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

// applyBackendForwarding налаштовує ОДИН Paper/Purpur backend під modern
// forwarding: online-mode=false у server.properties + секрет у
// config/paper-global.yml (1.19.3+) або paper.yml settings.velocity-support
// (1.18.2 і нижче). Повертає помилку, але не критичну для списку мережі.
func applyBackendForwarding(srv *Server, secret string) error {
	if srv == nil || srv.Dir == "" {
		return fmt.Errorf("сервер без теки")
	}
	_ = os.MkdirAll(srv.Dir, 0755)

	// 1) server.properties: online-mode=false (проксі автентифікує).
	if err := WriteServerProperties(srv.Dir, []PropUpdate{{Key: "online-mode", Value: "false"}}); err != nil {
		return fmt.Errorf("server.properties (%s): %w", srv.Name, err)
	}

	old := !mcVersionAtLeast(srv.MCVersion, 1, 19)
	var file string
	var block string
	if old {
		// Paper ≤1.18.2: paper.yml → settings.velocity-support
		file = filepath.Join(srv.Dir, "paper.yml")
		block = "settings:\n  velocity-support:\n    enabled: true\n    online-mode: true\n    secret: " + quoteYAML(secret) + "\n"
	} else {
		// Paper 1.19.3+/1.19+: config/paper-global.yml → proxies.velocity
		file = filepath.Join(srv.Dir, "config", "paper-global.yml")
		block = "proxies:\n  velocity:\n    enabled: true\n    online-mode: true\n    secret: " + quoteYAML(secret) + "\n"
	}
	if err := upsertYAMLKeys(file, block, "secret: "); err != nil {
		return fmt.Errorf("%s (%s): %w", filepath.Base(file), srv.Name, err)
	}
	return nil
}

// quoteYAML — простий скаляр у лапках (секрет — hex, без спецсимволів,
// але лапки не завадять).
func quoteYAML(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// upsertYAMLKeys створює (якщо нема) або дописує в існуючий YAML-файл
// наш блок ключів: якщо в файлі вже є рядок, що містить marker (напр.
// "secret: "), ми не чіпаємо файл (значить, користувач/Paper уже
// налаштували своє); інакше — якщо файл порожній або згенерований
// сервером, дописуємо блок у кінець. Для файлу, який ще не існує,
// створюємо його лише з нашим блоком (Paper зіллє дефолти при старті).
func upsertYAMLKeys(path, block, marker string) error {
	if _, err := os.Stat(path); err == nil {
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		if strings.Contains(string(data), marker) {
			return nil // уже налаштовано
		}
		// Дописуємо блок у кінець (Paper збереже чужі ключі).
		out := append(data, []byte(block)...)
		return writeFileAtomic(path, out, 0644)
	}
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	return writeFileAtomic(path, []byte(block), 0644)
}

// ApplyNetworkGlue налаштовує forwarding для всієї мережі: секрет +
// velocity.toml на проксі + конфіг кожного Paper/Purpur backend-а.
// idempotent — можна викликати на кожному збереженні/старті мережі.
func ApplyNetworkGlue(proxy *Server, servers map[string]*Server, net *Network) error {
	if proxy == nil {
		return fmt.Errorf("проксі не знайдено")
	}
	secret, err := ensureForwardingSecret(proxy.Dir)
	if err != nil {
		return err
	}
	for _, ns := range net.Servers {
		if ns.IsProxy {
			continue
		}
		srv := servers[ns.ServerID]
		if srv == nil {
			continue
		}
		if !velocityForwardingSupported(srv.Core) {
			// Vanilla/Fabric/Forge потребують додаткових модів для
			// forwarding — не чіпаємо їхні конфіги, мережа все одно
			// працює в «none»-режимі для таких backend-ів.
			log.Printf("[servers] backend %s (%s) не підтримує modern forwarding нативно — пропускаємо конфіг", srv.Name, srv.Core)
			continue
		}
		if err := applyBackendForwarding(srv, secret); err != nil {
			log.Printf("[servers] glue %s: %v", srv.Name, err)
		}
	}
	return nil
}
