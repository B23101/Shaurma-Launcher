// diag.go — людські пояснення причин збоїв P2P для launcher.log.
//
// Принцип: у лозі має бути НЕ «щось не вийшло», а ЩО САМЕ не працює, НА
// ЯКОМУ кроці і ЩО З ЦИМ РОБИТИ. Тому кожна точка відмови в guest/node
// пише сирий текст помилки + пояснення з цього файлу.
package p2p

import (
	"context"
	"errors"
	"strings"
)

// explainListenErr пояснює, чому не вдалося зайняти адресу:порт (net.Listen).
// Текстові збіги — навмисно: на Windows код WSAEADDRINUSE (10048) не
// збігається з syscall.EADDRINUSE, тож errors.Is там не спрацює.
func explainListenErr(addr string, err error) string {
	if err == nil {
		return ""
	}
	m := strings.ToLower(err.Error())
	switch {
	case strings.Contains(m, "address already in use"),
		strings.Contains(m, "only one usage of each socket address"),
		strings.Contains(m, "10048"):
		return "ПОРТ ЗАЙНЯТИЙ: " + addr + " уже слухає інша програма (локальний Minecraft-сервер на 25565, " +
			"друга копія лаунчера, VPN/проксі). Закрийте її — поки порт зайнятий, гості отримують «Connection refused»"
	case strings.Contains(m, "forbidden by its access permissions"),
		strings.Contains(m, "permission denied"),
		strings.Contains(m, "10013"):
		return "ОС ЗАБОРОНИЛА слухати " + addr + ": порт зарезервований (Windows excluded port range / Hyper-V / WSL — " +
			"перевірте `netsh interface ipv4 show excludedportrange protocol=tcp`) або блокує антивірус/файрвол"
	case strings.Contains(m, "cannot assign requested address"),
		strings.Contains(m, "can't assign requested address"),
		strings.Contains(m, "10049"):
		return "У ОС НЕМАЄ адреси 127.0.0.2 (типово для macOS): виконайте `sudo ifconfig lo0 alias 127.0.0.2 up`. " +
			"Без неї P2P-адреси неможливо відкрити"
	}
	return "невідома причина відмови ОС при відкритті порту " + addr
}

// explainLobbyErr пояснює збій запиту до lobby (резолв адреси хоста,
// auth key). Сирий текст помилки логується окремо.
func explainLobbyErr(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, ErrAddressNotFound) {
		return "адреси НЕМАЄ в lobby: її ніколи не створювали, вона протухла (30 днів без активності) або гравець ввів її з помилкою"
	}
	m := strings.ToLower(err.Error())
	switch {
	case strings.Contains(m, "не налаштований на сервері"):
		return "на сервері P2P не налаштований (Worker без Headscale) — проблема на боці сервера, а не гравця"
	case strings.Contains(m, "забагато спроб"):
		return "lobby обмежив запити з цього IP (rate-limit) — зачекати кілька хвилин"
	case strings.Contains(m, "http 401"), strings.Contains(m, "http 403"):
		return "lobby ВІДХИЛИВ токен лаунчера (401/403): токен застарів/недійсний — оновіть лаунчер або перезайдіть в акаунт"
	case strings.Contains(m, "http 5"):
		return "помилка на боці сервера lobby (5xx) — повторити пізніше / перевірити Worker"
	case strings.Contains(m, "запит до lobby"):
		switch {
		case errors.Is(err, context.DeadlineExceeded), strings.Contains(m, "timeout"), strings.Contains(m, "deadline exceeded"):
			return "lobby НЕ ВІДПОВІВ вчасно: у гравця проблеми з інтернетом, або провайдер/VPN/файрвол блокує cdn-домен лаунчера"
		case strings.Contains(m, "no such host"), strings.Contains(m, "lookup"):
			return "НЕ ВДАЛОСЯ РЕЗОЛВНУТИ домен lobby (DNS): DNS провайдера/антивіруса блокує домен, спробуйте інший DNS (1.1.1.1 / 8.8.8.8)"
		case strings.Contains(m, "connection refused"), strings.Contains(m, "reset"), strings.Contains(m, "tls"), strings.Contains(m, "certificate"):
			return "З'ЄДНАННЯ З lobby обірване/заблоковане (файрвол, провайдер, TLS-перехоплення антивірусом)"
		}
		return "немає зв'язку з сервером lobby"
	}
	return ""
}

// explainDialErr пояснює збій tsnet-діалу до хоста.
func explainDialErr(err error) string {
	if err == nil {
		return ""
	}
	m := strings.ToLower(err.Error())
	switch {
	case strings.Contains(m, "connection refused"), strings.Contains(m, "actively refused"):
		return "хост У МЕРЕЖІ, але Minecraft-сервер на його порту НЕ слухає — сервер у хоста не запущений (або запущений на іншому порту)"
	case errors.Is(err, context.DeadlineExceeded), strings.Contains(m, "timeout"), strings.Contains(m, "deadline exceeded"):
		return "ТАЙМ-АУТ: хост не відповідає в tailnet — його лаунчер закрито/завис, адреса в lobby застаріла, або між нами не встановився маршрут (файрвол/NAT блокує UDP)"
	case strings.Contains(m, "no route"), strings.Contains(m, "unreachable"):
		return "маршруту до хоста НЕМАЄ: хост уже вийшов із Headscale-мережі (його лаунчер закрито)"
	}
	return ""
}

// explainNodeErr пояснює збій підйому гостьової tsnet-ноди.
func explainNodeErr(err error) string {
	if err == nil {
		return ""
	}
	m := strings.ToLower(err.Error())
	switch {
	case strings.Contains(m, "auth key"):
		return "не вдалося отримати auth key з lobby — без нього нода не зареєструється в Headscale: " + explainLobbyErr(err)
	case errors.Is(err, context.DeadlineExceeded), strings.Contains(m, "deadline exceeded"), strings.Contains(m, "timeout"):
		return "Headscale (vpn.shaurma-launcher.org) не відповів за відведений час: заблоковано провайдером/файрволом (потрібен HTTPS до vpn.shaurma-launcher.org і UDP) або сервер лежить"
	case strings.Contains(m, "no such host"), strings.Contains(m, "lookup"):
		return "DNS не резолвить vpn.shaurma-launcher.org — змініть DNS або перевірте, що домен не блокується"
	}
	return ""
}

// withHint склеює сирий текст помилки з поясненням (якщо воно є).
func withHint(err error, hint string) string {
	if hint == "" {
		return err.Error()
	}
	return err.Error() + "  ⇒ " + hint
}
