package sync

import (
	"errors"
	"net"
	"strings"
)

// NetError — класифікована мережева помилка. IsNoInternet=true означає
// "мережі взагалі немає" (DNS не резолвиться, network unreachable, немає
// route to host) — на відміну від тимчасового збою сервера (5xx, timeout
// одного запиту), яке ретраїться мовчки. Це те саме розрізнення, що
// isNoInternet() у старому Go-движку (DOWNLOAD_SYNC_DESIGN.md, 3.6),
// перенесене в нову схему без змін самого принципу.
type NetError struct {
	IsNoInternet bool
	Err          error
}

func (e *NetError) Error() string {
	if e.IsNoInternet {
		return "немає з'єднання з інтернетом: " + e.Err.Error()
	}
	return e.Err.Error()
}

func (e *NetError) Unwrap() error { return e.Err }

// classifyNetErr обгортає помилку транспорту й визначає, чи це "немає
// інтернету взагалі", чи щось інше (тимчасовий збій, який має ретраїтись
// мовчки). Правило: DNS-помилки, "network is unreachable", "no route to
// host" і "connection refused" НА РІВНІ DialContext — це відсутність
// мережі. Timeout однієї операції чи розрив уже встановленого з'єднання
// (EOF, reset) — це вже НЕ "немає інтернету", а тимчасовий збій.
func classifyNetErr(err error) error {
	if err == nil {
		return nil
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return &NetError{IsNoInternet: true, Err: err}
	}

	var opErr *net.OpError
	if errors.As(err, &opErr) {
		msg := strings.ToLower(opErr.Error())
		if strings.Contains(msg, "network is unreachable") ||
			strings.Contains(msg, "no route to host") ||
			strings.Contains(msg, "connection refused") {
			return &NetError{IsNoInternet: true, Err: err}
		}
	}

	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "network is unreachable") ||
		strings.Contains(msg, "no route to host") {
		return &NetError{IsNoInternet: true, Err: err}
	}

	return &NetError{IsNoInternet: false, Err: err}
}

// IsNoInternet — зручний хелпер: чи класифікована помилка як "немає
// інтернету взагалі" (а не тимчасовий збій сервера/CDN).
func IsNoInternet(err error) bool {
	var ne *NetError
	if errors.As(err, &ne) {
		return ne.IsNoInternet
	}
	return false
}
