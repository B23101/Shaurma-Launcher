//go:build shaurma

package main

// Соціальна «особистість» — друзі, чат, ключі E2E, P2P.
//
// ОДИН акаунт лаунчера = ОДНА окрема особистість (комірка). Нічого спільного між акаунтами
// немає: ні друзів, ні чату, ні ключів E2E. Раніше вхід у Google створював «глобальну»
// сесію, і друзі однакові в усіх акаунтів — це прибрано разом із усією старою системою.
// Акаунт, у якого комірки немає (лише нік), особистості не має: ні друзів, ні чату, ні
// P2P, ні скінів.
//
// Дані комірки:
//   ключ Ed25519 і accountId — у захищеному сховищі ОС (internal/shaurmaid);
//   ключі E2E — <дані>/.shaurma/social/cell-<launcherAccountId>/keys/identity.json.

import (
	"errors"
	"net/http"
	"path/filepath"
	"strings"

	"shaurma-launcher-backend/internal/social"
)

// socialIdent — хто зараз «я» для соціальної системи.
type socialIdent struct {
	key        string // "cell-<launcherAccountId>" або "" (особистості немає)
	launcherID string // ID активного акаунта лаунчера
}

// currentSocialIdent визначає особистість за АКТИВНИМ акаунтом лаунчера й нічим іншим.
func (a *App) currentSocialIdent() socialIdent {
	a.purgeLegacyAccountData()
	acc := a.cfg.ActiveAccount()
	if sanitizeIdentityID(acc.ID) == "" {
		return socialIdent{}
	}
	if _, ok := a.shaurmaCell(acc.ID); !ok {
		return socialIdent{}
	}
	return socialIdent{key: "cell-" + acc.ID, launcherID: acc.ID}
}

func (a *App) socialRequestFor(id socialIdent) social.Requester {
	if id.key == "" {
		return func(string, string, []byte) (*http.Response, error) { return nil, social.ErrNotLoggedIn }
	}
	lid := id.launcherID
	return func(method, path string, body []byte) (*http.Response, error) {
		return a.shaurmaRequest(lid, method, path, body)
	}
}

func (a *App) socialTokenFor(id socialIdent) func() (string, error) {
	if id.key == "" {
		return func() (string, error) { return "", social.ErrNotLoggedIn }
	}
	lid := id.launcherID
	return func() (string, error) { return a.shaurmaToken(lid) }
}

// socialMeFor — мій accountId (з локальної комірки, без мережі).
func (a *App) socialMeFor(id socialIdent) string {
	if id.key == "" {
		return ""
	}
	if c, ok := a.shaurmaCell(id.launcherID); ok {
		return c.AccountID
	}
	return ""
}

// socialKeyDirFor — тека ключів E2E цієї особистості ("" → чат вимкнено).
func (a *App) socialKeyDirFor(id socialIdent) string {
	if id.key == "" {
		return ""
	}
	d := a.socialDirFor(id.launcherID)
	if d == "" {
		return ""
	}
	return filepath.Join(d, "keys")
}

// p2pIdentity — «власник» P2P-адрес: комірка активного акаунта. Без комірки (акаунт лише з
// ніком) P2P недоступний.
func (a *App) p2pIdentity() (string, string, error) {
	id := a.currentSocialIdent()
	if id.key == "" {
		return "", "", errors.New("P2P доступний лише з акаунтом Шаурми — створіть його для цього акаунта")
	}
	c, ok := a.shaurmaCell(id.launcherID)
	if !ok || c.AccountID == "" || strings.TrimSpace(c.Nickname) == "" {
		return "", "", errors.New("акаунт Шаурми пошкоджений — створіть його знову")
	}
	return c.AccountID, strings.TrimSpace(c.Nickname), nil
}

// p2pAuth — JWT і HWID активного акаунта для запитів P2P-лобі. Вузол приймає /p2p/* і без них (анонім),
// але режими доступу, співхости, ліміти на акаунт і квитки допуску працюють лише з валідним JWT.
// Немає акаунта або сесія не оновилась → ("", "") і запит іде анонімно.
func (a *App) p2pAuth() (string, string) {
	id := a.currentSocialIdent()
	if id.key == "" {
		return "", ""
	}
	tok, err := a.shaurmaToken(id.launcherID)
	if err != nil || tok == "" {
		return "", ""
	}
	return tok, shaurmaHWID()
}
