// Package creds — облікові дані запитів лаунчера до старого worker (CDN).
//
// Міст R1 і старі збірки несуть вшитий токен каналу (X-Shaurma-Token).
// Єдиний лаунчер (R2) токена не має (план «Єдиний лаунчер», §6): він
// позначає себе X-Shaurma-Client-Gen: 2, а після активації додає
// короткоживучий X-Shaurma-Entitlement, який видає новий worker. Усі місця,
// що ходять на старий worker, ставлять заголовки лише через Apply — тож
// перехід не розмазаний по коду.
package creds

import (
	"net/http"
	"sync/atomic"
)

// Заголовки. Strip прибирає секретні при редиректі на чужий хост.
const (
	HeaderToken       = "X-Shaurma-Token"
	HeaderEntitlement = "X-Shaurma-Entitlement"
	HeaderClientGen   = "X-Shaurma-Client-Gen"
)

var provider atomic.Pointer[func() string]

// SetEntitlementProvider — джерело токена доступу (access.Manager).
func SetEntitlementProvider(f func() string) {
	if f == nil {
		provider.Store(nil)
		return
	}
	provider.Store(&f)
}

// Entitlement — поточний токен доступу або "".
func Entitlement() string {
	if p := provider.Load(); p != nil {
		return (*p)()
	}
	return ""
}

// Apply ставить облікові дані: вшитий токен (якщо є), інакше ознаку єдиного
// лаунчера і, після активації, токен доступу.
func Apply(h http.Header, legacyToken string) {
	if legacyToken != "" {
		h.Set(HeaderToken, legacyToken)
		return
	}
	h.Set(HeaderClientGen, "2")
	if e := Entitlement(); e != "" {
		h.Set(HeaderEntitlement, e)
	}
}

// Strip прибирає секретні заголовки (редирект на інший хост).
func Strip(h http.Header) {
	h.Del(HeaderToken)
	h.Del(HeaderEntitlement)
}
