//go:build shaurma

package main

// presence_mode_shaurma.go — ручний статус у віджеті акаунта (як у Discord):
// «В мережі» / «Відійшов» / «Не турбувати» / «Невидимий».
//
//   - «Відійшов» і «Невидимий» — статус ДЛЯ ДРУЗІВ (internal/social), зберігається в settings.
//   - «Не турбувати» — локальний режим сповіщень (NotifyDnd), друзям не показується.
//
// Режими взаємовиключні: вибір одного скидає інші, тож UI завжди показує один стан.

// SetPresenceMode — mode: "online" | "away" | "dnd" | "invisible". Повертає збережений режим.
func (a *App) SetPresenceMode(mode string) string {
	s := a.cfg.GetSettings()
	pm := ""
	dnd := false
	switch mode {
	case "away":
		pm = "away"
	case "invisible":
		pm = "invisible"
	case "dnd":
		dnd = true
	default:
		mode = "online"
	}
	s.PresenceMode = pm
	s.NotifyDnd = dnd
	_ = a.cfg.UpdateSettings(s)
	if svc := currentSocialService(); svc != nil {
		if dnd {
			svc.SetManualPresence("dnd")
		} else {
			svc.SetManualPresence(pm)
		}
	}
	return mode
}
