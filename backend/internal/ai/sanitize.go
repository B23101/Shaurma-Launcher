package ai

// sanitize.go — очищення приватних даних ПЕРЕД відправкою в ШІ (Groq / Gemini).
//
// ШІ-аналіз крашу не має отримувати нічого, що ідентифікує гравця або дає доступ
// кудись: нікнейми, адреси серверів і IP, токени, імʼя користувача Windows у
// шляхах, e-mail, UUID. Для діагнозу потрібні лише винятки, назви класів, модів
// і версії, тож усе інше замінюється на нейтральні плейсхолдери.
//
// Єдина точка входу — SanitizeCrashContext. Її викликає buildSystemPrompt, через
// який ідуть ОБИДВА клієнти, тому новий виклик ШІ не може обійти очищення.

import (
	"os"
	"regexp"
	"sort"
	"strings"
)

var (
	// Токени та секрети.
	reJWT       = regexp.MustCompile(`eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]*`)
	reBearer    = regexp.MustCompile(`(?i)\b(bearer|token)[ :=]+[A-Za-z0-9._~+/=-]{12,}`)
	reSessionID = regexp.MustCompile(`(?i)(session id is )\S+`)
	reSecretKV  = regexp.MustCompile(`(?i)(access[_-]?token|refresh[_-]?token|id[_-]?token|session[_-]?token|api[_-]?key|apikey|secret|password|passwd|authorization|xuid|client[_-]?id)(["']?\s*[:=]\s*["']?)[^\s"',;&]+`)
	// Аргументи запуску гри: --accessToken X, --username X, --uuid X тощо.
	reLaunchArg = regexp.MustCompile(`(?i)(--(?:accessToken|username|uuid|xuid|clientId|userProperties|server|port|quickPlayMultiplayer|quickPlayPath|quickPlaySingleplayer))([ =]+)(\S+)`)
	// Рядки журналу гри, де фігурує нік.
	reSettingUser = regexp.MustCompile(`(?i)(setting user:\s*)\S+`)

	// Особисті ідентифікатори.
	reEmail = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)+`)
	reUUID  = regexp.MustCompile(`\b[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\b`)
	reHash  = regexp.MustCompile(`\b[0-9a-fA-F]{32,}\b`)

	// Шляхи з імʼям користувача ОС.
	reWinUser   = regexp.MustCompile(`(?i)([A-Za-z]:[\\/]+Users[\\/]+)[^\\/\s"':*?<>|]+`)
	reUnixUser  = regexp.MustCompile(`(/(?:home|Users)/)[^/\s"':]+`)
	reUNCUser   = regexp.MustCompile(`(?i)(\\\\[^\\\s]+\\Users\\)[^\\\s"']+`)
	reEscapedWn = regexp.MustCompile(`(?i)([A-Za-z]:\\\\Users\\\\)[^\\\s"']+`)

	// Мережеві адреси. IPv4 — з перевіркою меж, щоб не зачепити версії модів (1.20.1.4).
	reIPv4 = regexp.MustCompile(`(^|[^\w.+\-])((?:\d{1,3}\.){3}\d{1,3})(:\d{1,5})?($|[^\w.\-])`)
	reIPv6 = regexp.MustCompile(`(?i)\b(?:[0-9a-f]{1,4}:){4,7}[0-9a-f]{1,4}\b`)
	// Хост, до якого підключається гра (контекст, щоб не чіпати імена Java-класів).
	reConnHost = regexp.MustCompile(`(?i)((?:connecting to|connection to|connect(?:ed)? to|disconnected from|lost connection to|server address|joining server|joined server)[:\s]+)([A-Za-z0-9._-]+)((?:[:,\s]+\d{2,5})?)`)
	// Адреси Шаурми (P2P-кімнати, кастомні сервери).
	reShaurmaHost = regexp.MustCompile(`(?i)\b[A-Za-z0-9.-]*shaurma-launcher\.org\b(?::\d{1,5})?`)
	// URL: прибираємо логін/пароль та query-параметри.
	reURLUserinfo = regexp.MustCompile(`(?i)(https?://)[^/\s@]+@`)
	reURLQuery    = regexp.MustCompile(`(https?://[^\s"'<>?#]+)\?[^\s"'<>]*`)
)

// SanitizeCrashContext повертає копію cc, у якій з усіх текстових полів прибрано
// приватні дані. cc.Private — додаткові точні значення (ніки акаунтів, імʼя
// користувача ОС, імʼя ПК, адреси своїх серверів), які теж замінюються.
func SanitizeCrashContext(cc CrashContext) CrashContext {
	priv := privateValues(cc.Private)
	out := cc
	out.LogWindow = Sanitize(cc.LogWindow, priv)
	out.Mods = make([]string, len(cc.Mods))
	for i, m := range cc.Mods {
		out.Mods[i] = Sanitize(m, priv)
	}
	out.Private = nil // самі значення далі не потрібні і в промт не потрапляють
	return out
}

// Sanitize очищає один текст. priv — вже підготовлений список приватних значень
// (див. privateValues); nil допустимий.
func Sanitize(s string, priv []string) string {
	if s == "" {
		return s
	}
	// 1) Точні значення гравця: нік, імʼя ОС-користувача, імʼя ПК, свої адреси.
	for _, p := range priv {
		s = replaceFold(s, p, "<hidden>")
	}
	// 2) Токени й секрети.
	s = reJWT.ReplaceAllString(s, "<token>")
	s = reSessionID.ReplaceAllString(s, "${1}<token>")
	s = reBearer.ReplaceAllString(s, "${1} <token>")
	s = reSecretKV.ReplaceAllString(s, "${1}${2}<hidden>")
	s = reLaunchArg.ReplaceAllString(s, "${1}${2}<hidden>")
	s = reSettingUser.ReplaceAllString(s, "${1}<player>")
	// 3) Шляхи з імʼям користувача.
	s = reWinUser.ReplaceAllString(s, "${1}<user>")
	s = reEscapedWn.ReplaceAllString(s, "${1}<user>")
	s = reUNCUser.ReplaceAllString(s, "${1}<user>")
	s = reUnixUser.ReplaceAllString(s, "${1}<user>")
	// 4) Адреси: URL, Шаурма, підключення, IP.
	s = reURLUserinfo.ReplaceAllString(s, "${1}")
	s = reURLQuery.ReplaceAllString(s, "${1}?<hidden>")
	s = reShaurmaHost.ReplaceAllString(s, "<server>")
	s = reConnHost.ReplaceAllString(s, "${1}<server>${3}")
	s = redactIPv4(redactIPv4(s)) // двічі: сусідні IP через один роздільник
	s = reIPv6.ReplaceAllString(s, "<ip>")
	// 5) Решта ідентифікаторів.
	s = reEmail.ReplaceAllString(s, "<email>")
	s = reUUID.ReplaceAllString(s, "<uuid>")
	s = reHash.ReplaceAllString(s, "<hash>")
	return s
}

// redactIPv4 міняє IPv4 (крім петлі й 0.0.0.0) на <ip>; кожен октет має бути ≤ 255.
func redactIPv4(s string) string {
	return reIPv4.ReplaceAllStringFunc(s, func(m string) string {
		sub := reIPv4.FindStringSubmatch(m)
		if len(sub) < 5 {
			return m
		}
		ip := sub[2]
		if strings.HasPrefix(ip, "0.") { // 0.x.x.x — не адреса хоста, а версія (sodium 0.5.11.4)
			return m
		}
		for _, part := range strings.Split(ip, ".") {
			if len(part) > 3 || (len(part) == 3 && part > "255") {
				return m
			}
			if len(part) > 1 && part[0] == '0' { // 01.02.03.04 — це не IP, а версія
				return m
			}
		}
		if ip == "127.0.0.1" || ip == "0.0.0.0" {
			return m
		}
		return sub[1] + "<ip>" + sub[4]
	})
}

// privateValues готує список значень до заміни: додає імʼя користувача ОС і імʼя
// ПК, відкидає надто короткі (щоб не псувати текст) і сортує від довших до
// коротших (спершу «Steve123», потім «Steve»).
func privateValues(extra []string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(v string) {
		v = strings.TrimSpace(v)
		if len([]rune(v)) < 3 || seen[strings.ToLower(v)] {
			return
		}
		seen[strings.ToLower(v)] = true
		out = append(out, v)
	}
	for _, v := range extra {
		add(v)
	}
	for _, env := range []string{"USERNAME", "USER", "LOGNAME"} {
		add(os.Getenv(env))
	}
	if h, err := os.Hostname(); err == nil {
		add(h)
	}
	sort.Slice(out, func(i, j int) bool { return len(out[i]) > len(out[j]) })
	return out
}

// replaceFold замінює всі входження old у s без урахування регістру.
func replaceFold(s, old, repl string) string {
	if old == "" {
		return s
	}
	ls, lo := strings.ToLower(s), strings.ToLower(old)
	if len(ls) != len(s) { // нестандартний юнікод змінює довжину при lower — безпечний запасний шлях
		return strings.ReplaceAll(s, old, repl)
	}
	var b strings.Builder
	i := 0
	for {
		j := strings.Index(ls[i:], lo)
		if j < 0 {
			b.WriteString(s[i:])
			break
		}
		b.WriteString(s[i : i+j])
		b.WriteString(repl)
		i += j + len(lo)
	}
	return b.String()
}
