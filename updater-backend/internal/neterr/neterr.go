// Package neterr перетворює сирі мережеві помилки Go (dial tcp ...
// i/o timeout, no such host, connection refused, x509 ...) у текст,
// зрозумілий звичайному користувачу, а не розробнику. Використовується
// скрізь, де HTTP-помилка апдейтера може дійти до UI як є.
package neterr

import (
	"context"
	"errors"
	"net"
	"strings"
)

// TrFunc — функція перекладу uk/en, як core.Callbacks.tr. Приймається як
// звичайна функція (а не інтерфейс на Callbacks), щоб уникнути
// циклічного імпорту internal/core <-> internal/download <-> internal/neterr.
type TrFunc func(uk, en string) string

// Friendly перетворює err на людське повідомлення, якщо розпізнає
// мережеву проблему. Якщо помилка невпізнана (наприклад, логічна
// помилка застосунку) — повертає err без змін, щоб не втратити
// корисний контекст.
func Friendly(err error, tr TrFunc) error {
	if err == nil {
		return nil
	}

	var netErr net.Error
	var dnsErr *net.DNSError
	var opErr *net.OpError

	switch {
	case errors.As(err, &dnsErr):
		return errors.New(tr(
			"Не вдалося знайти сервер оновлень (помилка DNS). Перевір інтернет-з'єднання або спробуй змінити DNS-сервер (наприклад, на 1.1.1.1) у налаштуваннях Windows.",
			"Could not resolve the update server (DNS error). Check your internet connection, or try changing your DNS server (e.g. to 1.1.1.1) in Windows network settings.",
		))

	case errors.As(err, &netErr) && netErr.Timeout():
		return errors.New(tr(
			"Сервер оновлень не відповів вчасно (тайм-аут з'єднання). Перевір інтернет, вимкни VPN/проксі, якщо він увімкнений, і спробуй ще раз.",
			"The update server didn't respond in time (connection timeout). Check your internet connection, disable any VPN/proxy if enabled, and try again.",
		))

	case errors.As(err, &opErr) && strings.Contains(opErr.Err.Error(), "connection refused"):
		return errors.New(tr(
			"Сервер оновлень відмовив у з'єднанні. Можливо, він тимчасово недоступний, або з'єднання блокує фаєрвол/антивірус.",
			"The update server refused the connection. It may be temporarily down, or a firewall/antivirus is blocking it.",
		))

	case strings.Contains(err.Error(), "certificate") || strings.Contains(err.Error(), "x509"):
		return errors.New(tr(
			"Помилка перевірки безпечного з'єднання (сертифікат). Перевір, що системний час і дата виставлені правильно, і що антивірус не підміняє SSL-сертифікати.",
			"Secure connection verification failed (certificate error). Check that your system clock/date is correct and that no antivirus is intercepting SSL certificates.",
		))

	case errors.Is(err, context.DeadlineExceeded):
		return errors.New(tr(
			"Запит зайняв надто багато часу і був перерваний. Перевір інтернет-з'єднання та спробуй ще раз.",
			"The request took too long and was cancelled. Check your internet connection and try again.",
		))
	}

	return err
}

// bilingual — короткий хелпер: uk-текст + en-текст в одному рядку, для
// місць без доступу до Callbacks.Lang (наприклад, internal/download,
// який якає файли без прив'язки до мови UI).
func bilingual(uk, en string) string {
	return uk + " / " + en
}

// FriendlyPlain — те саме, що Friendly, але без перекладача: повертає
// повідомлення одразу двома мовами через " / ". Для пакетів, які не
// мають доступу до налаштувань мови (core.Callbacks.Lang).
func FriendlyPlain(err error) error {
	return Friendly(err, bilingual)
}
