package main

// Єдиний вхід у лаунчер: «Microsoft» або «Google» — і БІЛЬШЕ нічого, жодного ніка наперед.
//
//	Google           → OAuth (лише openid) → акаунт Шаурми цієї пошти є? ТАК: просто входимо
//	                   (відновлюємо на цьому ПК). НІ: питаємо унікальний нік → неліцензійний акаунт.
//	Microsoft+ліцензія → акаунт Шаурми (ліцензійний, нік і UUID з Mojang) є? ТАК: входимо; НІ: створюємо.
//	Microsoft без ліцензії → вхід у Microsoft ВДАЛОСЯ, але Minecraft Java нема: пропонуємо нік
//	                   (унікальний, зарезервований у Шаурмі) → неліцензійний акаунт, прив'язаний до цього Microsoft.
//
// Нік неліцензійного акаунта унікальний у Шаурмі (без урахування регістру) і не може збігатися з
// іменем гравця Mojang — це ключ скіна в грі. Остаточне рішення завжди за сервером; перевірка
// в UI (CheckShaurmaNick) — лише підказка.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"shaurma-launcher-backend/internal/auth"
	"shaurma-launcher-backend/internal/shaurmaid"
)

// pendingSignIn — особу підтверджено, акаунта Шаурми ще нема: чекаємо нік. Живе лише в пам'яті
// (id_token не пишеться на диск) і не довше signInTTL.
type pendingSignIn struct {
	sess      *auth.Session // сторінка в браузері: чекає на нік, далі — «реєстрацію завершено»
	provider  string
	p         *shaurmaid.Pending
	idToken   string
	noLicense string
	at        time.Time
}

const signInTTL = 20 * time.Minute // id_token Google/Microsoft живе ~1 год; беремо з запасом

var (
	signInMu      sync.Mutex
	signInPending *pendingSignIn
)

func setSignInPending(p *pendingSignIn) {
	signInMu.Lock()
	signInPending = p
	signInMu.Unlock()
}

func (a *App) CancelSignIn() {
	a.CancelShaurmaLogin()
	signInMu.Lock()
	pend := signInPending
	signInPending = nil
	signInMu.Unlock()
	if pend != nil {
		pend.sess.Close() // звільняємо спільний порт і закриваємо сторінку в браузері
	}
}

func offlineLauncherID(nick string) string { return "offline-" + nick }

// SignInGoogle — вхід через Google. Блокує до повернення з браузера (до 4 хв; CancelSignIn).
func (a *App) SignInGoogle() (SignInResult, error) {
	if shaurmaHWID() == "" {
		return SignInResult{}, errors.New("не вдалося визначити ідентифікатор цього ПК")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	oidcMu.Lock()
	if oidcCancel != nil {
		oidcCancel()
	}
	oidcCancel = cancel
	oidcMu.Unlock()
	defer cancel()

	p, err := a.shaurmaManager().Prepare()
	if err != nil {
		return SignInResult{}, a.shaurmaUserError(err)
	}
	idToken, sess, err := a.oidcIDTokenSession(ctx, "google", p.Nonce())
	if err != nil {
		return SignInResult{}, a.shaurmaUserError(err)
	}
	r, err := a.signInWithIdentity("google", p, idToken, "", sess)
	finishSignInPage(sess, r, err)
	return r, err
}

// signInMicrosoftWithoutLicense — Microsoft пустив, а Minecraft Java на акаунті нема.
func (a *App) signInMicrosoftWithoutLicense(nl *auth.NoLicenseError, sess *auth.Session) (SignInResult, error) {
	p := a.takeShaurmaPending()
	if p == nil || nl.IDToken == "" || shaurmaHWID() == "" {
		return SignInResult{}, nl // без «паспорта» цього входу нема з чим працювати
	}
	return a.signInWithIdentity("microsoft", p, nl.IDToken, nl.Reason, sess)
}

// signInWithIdentity — спільне ядро: особа підтверджена (id_token з nonce заготовки p).
//
// sess — сесія сторінки в браузері; при need_nick вона лишається живою до CompleteSignIn.
func (a *App) signInWithIdentity(provider string, p *shaurmaid.Pending, idToken, noLicense string, sess *auth.Session) (SignInResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	rec, err := a.shaurmaManager().Recover(ctx, p, provider, idToken)
	switch {
	case err == nil:
		return a.completeRecoveredSignIn(rec, provider)
	case isDeviceLimit(err):
		// Ліміт пристроїв: id_token ще дійсний — запам'ятовуємо його й просимо обрати, кого вилучити.
		return a.deferForDeviceLimit(err, p, provider, idToken, func(r *shaurmaid.Recovered) (SignInResult, error) {
			return a.completeRecoveredSignIn(r, provider)
		})
	case errors.Is(err, shaurmaid.ErrNoAccount):
		setSignInPending(&pendingSignIn{sess: sess, provider: provider, p: p, idToken: idToken, noLicense: noLicense, at: time.Now()})
		return SignInResult{Status: "need_nick", Provider: provider, NoLicense: noLicense}, nil
	}
	return SignInResult{}, a.shaurmaUserError(err)
}

// completeRecoveredSignIn — завершення входу, коли акаунт Шаурми цієї особи вже існує: пристрій
// додано до акаунта, лаунчер-акаунт створюється з ніка комірки.
func (a *App) completeRecoveredSignIn(rec *shaurmaid.Recovered, provider string) (SignInResult, error) {
	cell := rec.Cell()
	if cell.Licensed() {
		// Google такого не дає; Microsoft — лише якщо Minecraft щойно «зник» з акаунта.
		return SignInResult{}, errors.New("акаунт Шаурми цього Microsoft — ліцензійний, але Minecraft Java на ньому зараз не знайдено. Перевірте акаунт і спробуйте ще раз")
	}
	acc, err := a.pirate.Login(cell.Nickname)
	if err != nil {
		return SignInResult{}, fmt.Errorf("нік акаунта Шаурми «%s» не підходить для гри: %v", cell.Nickname, err)
	}
	if err := a.shaurmaManager().Adopt(acc.ID, rec); err != nil {
		return SignInResult{}, a.shaurmaUserError(err)
	}
	if err := a.cfg.AddAccount(*acc); err != nil {
		return SignInResult{}, err
	}
	a.finishShaurmaSignIn()
	return SignInResult{Status: "signed_in", Provider: provider, Nickname: cell.Nickname, Existing: true}, nil
}

// CompleteSignIn — друга половина входу для НОВОЇ особи: користувач обрав нік. Нік мусить бути
// вільним у Шаурмі й у Mojang (перевіряє сервер). При помилці ніка вхід НЕ скидається — можна
// спробувати інший нік (id_token ще дійсний).
func (a *App) CompleteSignIn(nickname string) (SignInResult, error) {
	nickname = strings.TrimSpace(nickname)
	signInMu.Lock()
	pend := signInPending
	signInMu.Unlock()
	if pend == nil || time.Since(pend.at) > signInTTL {
		if pend != nil {
			pend.sess.Finish(auth.Outcome{Phase: "error", Provider: pend.provider, Message: "Сесія входу завершилась."})
		}
		setSignInPending(nil)
		return SignInResult{}, errors.New("сесія входу завершилась — увійдіть ще раз")
	}
	acc, err := a.pirate.Login(nickname) // правила нікa гри: 3–16, a-zA-Z0-9_
	if err != nil {
		return SignInResult{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	opts := shaurmaid.RegisterOpts{Nickname: nickname, Provider: pend.provider, Type: shaurmaid.TypeUnlicensed}
	if _, err := a.shaurmaManager().RegisterWithToken(ctx, acc.ID, opts, pend.p, pend.idToken); err != nil {
		var ae *shaurmaid.APIError
		if errors.As(err, &ae) && (ae.Code == "account_exists" || ae.Code == "bad_identity" || ae.Code == "stale_request") {
			setSignInPending(nil) // продовжувати нема сенсу: треба новий вхід
			pend.sess.Finish(auth.Outcome{Phase: "error", Provider: pend.provider, Message: a.shaurmaUserError(err).Error()})
		}
		// Інакше (нік зайнятий тощо) сторінка в браузері лишається на «оберіть нік» — можна спробувати інший.
		return SignInResult{}, a.shaurmaUserError(err)
	}
	setSignInPending(nil)
	if err := a.cfg.AddAccount(*acc); err != nil {
		pend.sess.Finish(auth.Outcome{Phase: "error", Provider: pend.provider, Message: err.Error()})
		return SignInResult{}, err
	}
	a.finishShaurmaSignIn()
	res := SignInResult{Status: "signed_in", Provider: pend.provider, Nickname: nickname}
	finishSignInPage(pend.sess, res, nil) // НОВИЙ неліцензійний акаунт → «реєстрацію завершено» + можливості
	return res, nil
}

func (a *App) finishShaurmaSignIn() {
	a.invalidatePackCache()
	a.emit("shaurma-account:changed", "")
	a.socialOnAccountChanged()
}

// CheckShaurmaNick — підказка під час вводу: чи вільний нік (у Шаурмі й у Mojang).
// Остаточне рішення приймає сервер при створенні акаунта.
func (a *App) CheckShaurmaNick(nickname string) (NickCheckResult, error) {
	nickname = strings.TrimSpace(nickname)
	if _, err := a.pirate.Login(nickname); err != nil {
		return NickCheckResult{Available: false, Reason: "invalid", Message: err.Error()}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	r, err := a.shaurmaManager().NickAvailable(ctx, nickname)
	if err != nil {
		var ae *shaurmaid.APIError
		if errors.As(err, &ae) && ae.Code == "mojang_unavailable" {
			return NickCheckResult{}, errors.New("не вдалося перевірити нік у Mojang — спробуйте за хвилину")
		}
		return NickCheckResult{}, a.shaurmaUserError(err)
	}
	out := NickCheckResult{Available: r.Available, Reason: r.Reason}
	switch r.Reason {
	case "taken":
		out.Message = "Цей нік уже зайнятий іншим акаунтом Шаурми"
	case "mojang":
		out.Message = "Цей нік належить гравцю Minecraft з ліцензією"
	case "invalid":
		out.Message = "Нік: 3–16 символів, лише латиниця, цифри та _"
	}
	return out, nil
}
