package main

// Бейдж непрочитаних повідомлень на іконці лаунчера в панелі завдань (як у Discord/Telegram).
// Число береться з тих самих лічильників, що й бейдж у інтерфейсі (подія friends:unread), але
// БЕЗ заглушених чатів (мут діалогу, загальний мут, DND). Shell отримує generic-команду
// window_op("main", "badge:<N>"): нинішня оболонка її ігнорує, тому на Windows оверлей малює сам backend
// (ITaskbarList3::SetOverlayIcon, taskbar_overlay_windows.go); команда лишається для майбутньої оболонки.

import (
	"strconv"
	"sync"
)

var taskbarBadge struct {
	mu     sync.Mutex
	unread map[string]int // останній знімок непрочитаного: акаунт співрозмовника → кількість
	shown  int            // що зараз показано (щоб не слати shell'у однакове значення)
}

// taskbarBadgeOnSocial — хук подій соціального сервісу.
func (a *App) taskbarBadgeOnSocial(event string, payload any) {
	if event != "friends:unread" {
		return
	}
	snap, _ := payload.(map[string]int)
	a.taskbarBadgeUpdate(snap)
}

// taskbarBadgeUpdate запам'ятовує знімок непрочитаного (nil — усе прочитано / вихід з акаунта).
func (a *App) taskbarBadgeUpdate(snap map[string]int) {
	cp := make(map[string]int, len(snap))
	for id, n := range snap {
		if n > 0 {
			cp[id] = n
		}
	}
	taskbarBadge.mu.Lock()
	taskbarBadge.unread = cp
	taskbarBadge.mu.Unlock()
	a.taskbarBadgeRefresh()
}

// taskbarBadgeRefresh перераховує число й шле його shell'у, якщо воно змінилось. Викликається
// також після зміни мутів: заглушений чат одразу перестає рахуватись.
func (a *App) taskbarBadgeRefresh() {
	s := a.cfg.GetSettings()
	taskbarBadge.mu.Lock()
	total := 0
	if !s.NotifyDnd && !s.ChatMuteAll {
		for id, n := range taskbarBadge.unread {
			if !isChatMutedAccount(s, id) {
				total += n
			}
		}
	}
	changed := total != taskbarBadge.shown
	taskbarBadge.shown = total
	taskbarBadge.mu.Unlock()
	if changed {
		bridgeWindowOp("main", "badge:"+strconv.Itoa(total))
		applyTaskbarOverlay(total)
	}
}

// taskbarBadgeReapply малює бейдж наново з поточним числом. Windows скидає оверлей, коли кнопку панелі
// завдань пересоздано (вікно ховали в трей, перезапуск Провідника), тож викликаємо при активації вікна.
func (a *App) taskbarBadgeReapply() {
	taskbarBadge.mu.Lock()
	n := taskbarBadge.shown
	taskbarBadge.mu.Unlock()
	if n > 0 {
		applyTaskbarOverlay(n)
	}
}

// applyTaskbarOverlay малює число на іконці. Якщо не вдалось (вікно ще не створене) — скидаємо «показане»,
// щоб наступна подія непрочитаного спробувала ще раз, а не вирішила, що там уже правильне число.
func applyTaskbarOverlay(n int) {
	if setTaskbarOverlay(n) {
		return
	}
	taskbarBadge.mu.Lock()
	if taskbarBadge.shown == n {
		taskbarBadge.shown = -1
	}
	taskbarBadge.mu.Unlock()
}
