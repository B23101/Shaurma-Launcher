//go:build !shaurma

package main

import (
	"shaurma-launcher-backend/internal/model"
)

// prepareOfflineAccount — хук перед стартом гри (для акаунта БУДЬ-ЯКОГО типу:
// назва історична). Повертає додаткові JVM-аргументи для цього запуску.
//
// У base-редакції НІЧОГО не робить і нічого не додає: у цій збірці немає
// жодної скін-системи (відповідні файли скомпільовані лише з тегом
// `shaurma`), тож тут лише порожня заглушка, щоб спільний LaunchInstance
// міг викликати хук. Жодних згадок скін-фічі в цьому бінарнику: ні методів,
// ні рядків, ні API.
func (a *App) prepareOfflineAccount(account model.Account, buildID, gameDir string) []string {
	_ = account
	_ = buildID
	_ = gameDir
	return nil
}

// applyPresetToSkinServer — хук для ApplyWardrobePreset. У base-редакції
// скін-системи немає, тож нічого не обробляє: завжди handled=false, і
// застосування йде звичайним шляхом (Mojang).
func (a *App) applyPresetToSkinServer(accountID, presetID string) (bool, error) {
	_ = accountID
	_ = presetID
	return false, nil
}

// wireShaurmaAvatarResolver — хук для avatar.Service, який у Shaurma-
// редакції підключає показ скінів із сервера Shaurma для неліцензійних
// акаунтів. У base-редакції no-op: avatar.Service лишається зі своєю
// стандартною поведінкою (Mojang lookup за ніком).
func (a *App) wireShaurmaAvatarResolver() {}

// renameSkinNicknameIfMatches / releaseSkinNicknameIfMatches — хуки для
// App.RenameOfflineAccount / App.RemoveAccount. У base-редакції скін-
// системи немає (нема ні Google-сесії, ні серверного ніка), тож завжди
// no-op.
func (a *App) renameSkinNicknameIfMatches(oldUsername, newUsername string) error {
	_ = oldUsername
	_ = newUsername
	return nil
}

func (a *App) releaseSkinNicknameIfMatches(username string) {
	_ = username
}

// activeLookFromSkinServer / activeMatchingPresetFromSkinServer — хуки для
// App.GetWardrobeActiveLook / App.GetWardrobeActivePresetID. У base-редакції
// сервера скінів немає, тож завжди handled=false: використовується звичайний
// шлях через Mojang (для офлайн-акаунтів там теж нічого немає — nil, як і
// раніше).
func (a *App) activeLookFromSkinServer(accountID string) (*model.ActiveLook, bool, error) {
	_ = accountID
	return nil, false, nil
}

func (a *App) activeMatchingPresetFromSkinServer(accountID string) (string, bool, error) {
	_ = accountID
	return "", false, nil
}

// markShaurmaSkinAccounts — хук для App.GetAccounts. У base-редакції
// скін-системи немає, тож жоден акаунт ніколи не позначається як
// «Шаурма-акаунт»: поле IsShaurmaSkinAccount лишається false для всіх.
func (a *App) markShaurmaSkinAccounts(accounts []model.Account) {
	_ = accounts
}
