package main

// Мути чату (ФАЗА: «щоб не бачити повідомлень, коли граєш»).
//
// Три незалежних рівні, кожен зі своїм вимикачем (жодного змішування):
//   1. NotifyDnd (settings) — «Не турбувати»: тимчасовий режим ТИШІ для
//      ВСІХ сповіщень (чат, запит у друзі). Це вже працює (DND глушить усі
//      чати — саме це користувач і очікує).
//   2. ChatMuteAll (settings) — загальний мут ЧАТІВ: повідомлення з усіх
//      діалогів не стають тостами, але запити в друзі лишаються.
//   3. MutedChats (settings) — мут ОДНОГО діалогу (список accountId).
//      Переживає видалення з друзів: чат лишається мутнутим.
//
// ChatMuteSound — мут ЗВУКУ сповіщень чату. Звук поки що не грається
// (додається пізніше), але вимикач потрібен уже зараз: інакше мут «по
// звуку» довелось би мішати з мутом «по видимості».

import (
	"strings"

	"shaurma-launcher-backend/internal/model"
)

// chatMuteState — поточний список мутнутих діалогів + два перемикачі.
type chatMuteState struct {
	All       bool     `json:"all"`
	Sound     bool     `json:"sound"`
	MutedList []string `json:"muted"`
}

// GetChatMuteState повертає три рівні муту чатів (settings.json).
func (a *App) GetChatMuteState() chatMuteState {
	s := a.cfg.GetSettings()
	out := chatMuteState{All: s.ChatMuteAll, Sound: s.ChatMuteSound, MutedList: s.MutedChats}
	if out.MutedList == nil {
		out.MutedList = []string{}
	}
	return out
}

// isChatMutedAccount — мут за accountId зі СПИСКУ (без DND/ChatMuteAll:
// ті вище в IsChatMuted). Регістронезалежно — хеш акаунта з паспорта
// може прийти в іншому регістрі (рідко, але безпечно).
func isChatMutedAccount(s model.Settings, accountID string) bool {
	for _, id := range s.MutedChats {
		if strings.EqualFold(id, accountID) {
			return true
		}
	}
	return false
}

// SetChatMuteAll — загальний мут чатів.
func (a *App) SetChatMuteAll(on bool) chatMuteState {
	s := a.cfg.GetSettings()
	s.ChatMuteAll = on
	_ = a.cfg.UpdateSettings(s)
	return a.GetChatMuteState()
}

// SetChatMuteSound — мут звуку сповіщень чату (звук додається пізніше;
// перемикач вже працює і впливає на майбутній звуковий шлях).
func (a *App) SetChatMuteSound(on bool) chatMuteState {
	s := a.cfg.GetSettings()
	s.ChatMuteSound = on
	_ = a.cfg.UpdateSettings(s)
	return a.GetChatMuteState()
}

// SetChatMuted — мут/розмут одного діалогу. Працює незалежно від того,
// чи це зараз друг (мут переживає видалення з друзів).
func (a *App) SetChatMuted(accountID string, muted bool) chatMuteState {
	if accountID == "" {
		return a.GetChatMuteState()
	}
	s := a.cfg.GetSettings()
	list := make([]string, 0, len(s.MutedChats)+1)
	for _, id := range s.MutedChats {
		if id != accountID {
			list = append(list, id)
		}
	}
	if muted {
		list = append(list, accountID)
	}
	s.MutedChats = list
	_ = a.cfg.UpdateSettings(s)
	return a.GetChatMuteState()
}

// IsChatMuted — чи сповіщення цього діалогу мають бути приглушені.
// Викликається з notifyFromSocial (chat:message) — DND глушить усі чати,
// ChatMuteAll — загальний мут чатів, MutedChats — мут одного діалогу.
func (a *App) IsChatMuted(accountID string) bool {
	s := a.cfg.GetSettings()
	if s.NotifyDnd { // DND глушить усі чати — саме того хоче користувач
		return true
	}
	if s.ChatMuteAll {
		return true
	}
	return isChatMutedAccount(s, accountID)
}
