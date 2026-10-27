package main

// ── Згода на надсилання звітів про помилки лаунчера ─────────────────────
// Після входу користувачу (один раз — прапорець ErrorReportsPromptShown у
// БД) показується модалка з поясненням, що саме передається (лише джерело +
// суть запису журналу помилок, без особистих даних) і питанням, чи
// погоджується він, щоб його помилки лаунчера аналізував розробник. Вибір
// («Так» / «Ні») записується у settings.ErrorReportsEnabled; сам факт
// показу — у settings.ErrorReportsPromptShown, після чого вікно більше не
// спливає автоматично (перемикач лишається у Налаштуваннях).

// ErrorReportsStatus — стан системи звітів про помилки для пост-логін вікна.
type ErrorReportsStatus struct {
	ShouldShow  bool `json:"shouldShow"`
	PromptShown bool `json:"promptShown"`
	HasAccounts bool `json:"hasAccounts"`
	Enabled     bool `json:"enabled"`
}

// ErrorReportsStatus повертає стан для пост-логін вікна. ShouldShow=true
// лише якщо: є акаунти і вікно ще не показувалось.
func (a *App) ErrorReportsStatus() ErrorReportsStatus {
	s := a.cfg.GetSettings()
	hasAccounts := len(a.cfg.GetAccounts()) > 0
	return ErrorReportsStatus{
		ShouldShow:  !s.ErrorReportsPromptShown && hasAccounts,
		PromptShown: s.ErrorReportsPromptShown,
		HasAccounts: hasAccounts,
		Enabled:     s.ErrorReportsEnabled,
	}
}

// ErrorReportsSetConsent записує вибір користувача (enabled=true/false) і
// одразу позначає вікно показаним — незалежно від відповіді, повторно воно
// більше не спливає (як і у сценарії міграції лаунчерів).
func (a *App) ErrorReportsSetConsent(enabled bool) error {
	s := a.cfg.GetSettings()
	s.ErrorReportsEnabled = enabled
	s.ErrorReportsPromptShown = true
	return a.cfg.UpdateSettings(s)
}

// ErrorReportsDismiss позначає вікно показаним БЕЗ зміни поточного значення
// ErrorReportsEnabled (для випадку, коли користувач закрив модалку хрестиком
// / Escape, не натиснувши жодної кнопки — трактуємо як «поки не вирішив»,
// але вдруге вікно вже не набридає).
func (a *App) ErrorReportsDismiss() error {
	s := a.cfg.GetSettings()
	if !s.ErrorReportsPromptShown {
		s.ErrorReportsPromptShown = true
		return a.cfg.UpdateSettings(s)
	}
	return nil
}
