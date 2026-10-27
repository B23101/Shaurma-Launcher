//go:build shaurma

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"

	"shaurma-launcher-backend/internal/nodes"
	"shaurma-launcher-backend/internal/update"
)

// Пул VPS-вузлів для запитів до скінів/акаунтів/друзів. Створюється ліниво;
// якщо Worker не віддав жодного вузла (єдиний VPS ще не зареєстровано, мережа),
// усі запити йдуть прямо у Worker — поведінка як до кластера.
var (
	nodePoolOnce sync.Once
	nodePool     *nodes.Pool
)

func vpsPool() *nodes.Pool {
	nodePoolOnce.Do(func() {
		nodePool = &nodes.Pool{
			WorkerBase: update.CDNBaseURL(),
			Headers: func() http.Header {
				h := http.Header{}
				h.Set("User-Agent", "ShaurmaLauncher/2.0")
				h.Set("X-Shaurma-Token", update.Token())
				return h
			},
		}
	})
	return nodePool
}

// p2pLobbyBase — база лобі P2P (/p2p/*): тепер його обслуговує вузол кластера (Postgres), а не Worker.
// Беремо найкращий за ключем вузол; якщо кластер невідомий — запасно CDN (Worker віддасть 404, і лаунчер
// покаже зрозумілу помилку, а не зависне). Викликається на кожен запит, тож Prime дешевий.
func p2pLobbyBase() string {
	vpsPool().Prime()
	if bases := vpsPool().GatewayBases("p2p"); len(bases) > 0 {
		return strings.TrimRight(bases[0], "/")
	}
	return update.CDNBaseURL()
}

// nodeKeyFor — ключ розподілу. Скін за ніком завжди на одному вузлі (кеш
// працює); авторизовані виклики розкидаються за хешем токена (токен не
// зберігається й не логується — лише 4 байти його SHA-256).
func nodeKeyFor(path, bearer string) string {
	const pfx = "/api/skin/by-nick/"
	if strings.HasPrefix(path, pfx) {
		return "nick:" + strings.ToLower(path[len(pfx):])
	}
	if bearer != "" {
		s := sha256.Sum256([]byte(bearer))
		return "acc:" + hex.EncodeToString(s[:4])
	}
	return "path:" + path
}

// socialWSURLNow — адреса WebSocket вузла з роллю social, якщо Worker її віддав;
// інакше вбудована (як було). Підхоплюється при створенні соціального сервісу.
func socialWSURLNow() string {
	vpsPool().Prime()
	if u, ok := vpsPool().SocialWSURL(); ok {
		return u
	}
	return socialWSURL
}
