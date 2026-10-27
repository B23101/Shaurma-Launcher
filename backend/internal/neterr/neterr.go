// Package neterr перетворює сирі мережеві помилки Go (dial tcp ...
// i/o timeout, no such host, connection refused, x509, context deadline
// exceeded ...) у текст, зрозумілий звичайному користувачу, замість
// технічного "Post \"https://...\": context deadline exceeded".
package neterr

import (
	"context"
	"errors"
	"net"
	"strings"
)

// IsNetwork повідомляє, чи є err мережевою проблемою (DNS, тайм-аут,
// connection refused, TLS-збій, deadline exceeded) — на відміну від
// логічної помилки застосунку (акаунт забанений, невірний код, 4xx/5xx
// від сервера з конкретною причиною тощо). Використовується там, де
// потрібно розрізнити "немає інтернету — можна впасти в офлайн" від
// "щось справді не так з акаунтом — треба показати помилку як є".
func IsNetwork(err error) bool {
	if err == nil {
		return false
	}

	var netErr net.Error
	var dnsErr *net.DNSError
	var opErr *net.OpError

	switch {
	case errors.As(err, &dnsErr):
		return true
	case errors.As(err, &netErr) && netErr.Timeout():
		return true
	case errors.As(err, &opErr) && strings.Contains(opErr.Err.Error(), "connection refused"):
		return true
	case errors.Is(err, context.DeadlineExceeded):
		return true
	}
	return false
}

// Friendly перетворює err на людське повідомлення українською, якщо
// розпізнає мережеву проблему. Якщо помилка невпізнана (наприклад,
// логічна помилка застосунку) — повертає err без змін, щоб не
// втратити корисний контекст для діагностики.
//
// what — коротко, що саме не вдалося (напр. "увійти в акаунт Microsoft",
// "оновити сесію Microsoft", "завантажити список збірок"), підставляється
// у шаблон повідомлення.
func Friendly(err error, what string) error {
	if err == nil {
		return nil
	}

	var netErr net.Error
	var dnsErr *net.DNSError
	var opErr *net.OpError

	switch {
	case errors.As(err, &dnsErr):
		return errors.New("Не вдалося " + what + ": сервер не знайдено (помилка DNS). " +
			"Перевір інтернет-з'єднання або спробуй змінити DNS-сервер (наприклад, на 1.1.1.1) у налаштуваннях Windows.")

	case errors.As(err, &netErr) && netErr.Timeout():
		return errors.New("Не вдалося " + what + ": сервер не відповів вчасно (тайм-аут з'єднання). " +
			"Перевір інтернет, вимкни VPN/проксі, якщо він увімкнений, і спробуй ще раз.")

	case errors.As(err, &opErr) && strings.Contains(opErr.Err.Error(), "connection refused"):
		return errors.New("Не вдалося " + what + ": сервер відмовив у з'єднанні. " +
			"Можливо, він тимчасово недоступний, або з'єднання блокує фаєрвол/антивірус.")

	case strings.Contains(err.Error(), "certificate") || strings.Contains(err.Error(), "x509"):
		return errors.New("Не вдалося " + what + ": помилка перевірки безпечного з'єднання (сертифікат). " +
			"Перевір, що системний час і дата виставлені правильно, і що антивірус не підміняє SSL-сертифікати.")

	case errors.Is(err, context.DeadlineExceeded):
		return errors.New("Не вдалося " + what + ": запит зайняв надто багато часу і був перерваний. " +
			"Перевір інтернет-з'єднання та спробуй ще раз.")
	}

	return err
}
