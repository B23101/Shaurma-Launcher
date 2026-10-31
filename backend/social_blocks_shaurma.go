package main

// Блокування акаунтів і чат з не-друзями (Wails-методи).
// Логіка і перевірки — в internal/social/blocks.go та на сервері.

import (
	"sync"

	"shaurma-launcher-backend/internal/social"
)

// BlockAccount — заблокувати акаунт: дружба розривається, нові запити та
// повідомлення між вами заборонені. Історія чату лишається доступною.
func (a *App) BlockAccount(accountID string) error {
	return a.socialService().Block(accountID)
}

// UnblockAccount — зняти блокування (дружба не повертається сама).
func (a *App) UnblockAccount(accountID string) error {
	return a.socialService().Unblock(accountID)
}

// GetBlockedAccounts — список заблокованих мною акаунтів.
func (a *App) GetBlockedAccounts() ([]social.BlockedAccount, error) {
	return a.socialService().Blocks()
}

// GetChatPeerProfile — нік і @handle співрозмовника, який не є другом.
func (a *App) GetChatPeerProfile(accountID string) (social.PeerProfile, error) {
	return a.socialService().PeerProfile(accountID)
}

// strangerNicks — кеш ніків не-друзів для тостів (accountId → нік), щоб не
// ходити на сервер за кожним повідомленням.
var strangerNicks sync.Map

// strangerNickname повертає нік співрозмовника, що не є другом ("" — невідомо).
func (a *App) strangerNickname(accountID string) string {
	if v, ok := strangerNicks.Load(accountID); ok {
		return v.(string)
	}
	svc := currentSocialService()
	if svc == nil {
		return ""
	}
	p, err := svc.PeerProfile(accountID)
	if err != nil {
		return ""
	}
	nick := cleanNotifyText(p.Nickname)
	if nick != "" {
		strangerNicks.Store(accountID, nick)
	}
	return nick
}

// SearchFriendCandidates — підказки для поля «Додати друга» (нік або @handle, від 2 символів).
func (a *App) SearchFriendCandidates(query string) ([]social.UserHit, error) {
	a.startSocialIfLoggedIn()
	return a.socialService().SearchUsers(query)
}

// GetFriendProfile — профіль друга для вікна «Профіль». Приватність застосовує СЕРВЕР: якщо друг
// закрив профіль, повертається hidden=true без біо/кольору/статистики.
func (a *App) GetFriendProfile(accountID string) (social.FriendProfile, error) {
	return a.socialService().FriendProfile(accountID)
}

// GetProfileVisibility — кому я показую свій профіль: "friends" | "nobody".
func (a *App) GetProfileVisibility() (string, error) {
	return a.socialService().ProfileVisibility()
}

// SetProfileVisibility — змінити, кому я показую свій профіль ("friends" | "nobody"). Діє на сервері.
func (a *App) SetProfileVisibility(v string) error {
	return a.socialService().SetProfileVisibility(v)
}
