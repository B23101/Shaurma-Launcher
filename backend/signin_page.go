package main

import "shaurma-launcher-backend/internal/auth"

// finishSignInPage повідомляє сторінці в браузері підсумок входу (див. auth.Loopback): «вхід
// виконано» / «реєстрацію завершено» / «оберіть нік у лаунчері» / помилка.
// nil-сесія безпечна (base-збірка, вхід без браузера).
//
// Реєстрацією вважаємо створення НОВОГО акаунта Шаурми: status=signed_in і !Existing.
func finishSignInPage(sess *auth.Session, r SignInResult, err error) {
	if sess == nil {
		return
	}
	switch {
	case err != nil:
		sess.Finish(auth.Outcome{Phase: "error", Message: err.Error()})
	case r.Status == "need_nick":
		sess.Finish(auth.Outcome{Phase: "need_nick", Provider: r.Provider})
	default:
		sess.Finish(auth.Outcome{
			Phase:      "done",
			Provider:   r.Provider,
			Registered: !r.Existing,
			Licensed:   r.Licensed,
			Nickname:   r.Nickname,
		})
	}
}
