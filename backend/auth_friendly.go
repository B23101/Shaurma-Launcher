package main

import (
	"errors"
	"fmt"

	"shaurma-launcher-backend/internal/auth"
)

// authReloginError — зрозуміла для гравця помилка «сесію Microsoft відхилено».
// Зберігає оригінал через Unwrap, тож errors.Is(err, auth.ErrRefreshRejected)
// і технічні деталі (AADSTS-код у тексті оригіналу) не втрачаються для логів.
type authReloginError struct{ cause error }

func (e *authReloginError) Error() string {
	return "Сесія акаунта Microsoft закінчилась або була скасована — це не поломка, ваші збірки та світи в безпеці. " +
		"Що зробити: відкрийте Налаштування → Акаунти, видаліть цей акаунт Microsoft і додайте його знову (увійдіть у Microsoft), " +
		"після цього запустіть гру ще раз. Якщо гра одиночна — можна одразу зіграти з офлайн-акаунтом. " +
		"Технічна причина: " + e.cause.Error()
}

func (e *authReloginError) Unwrap() error { return e.cause }

// wrapRefreshErr перетворює відмову refresh-токена на дружнє повідомлення;
// будь-яку іншу помилку лишає як є, лише з префіксом ctx.
func wrapRefreshErr(ctx string, err error) error {
	if errors.Is(err, auth.ErrRefreshRejected) {
		return &authReloginError{cause: err}
	}
	return fmt.Errorf("%s: %w", ctx, err)
}
