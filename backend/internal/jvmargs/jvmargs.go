// Package jvmargs — санітизація JVM-аргументів перед запуском гри.
//
// Два рівні (за зразком Millida Launcher):
//
//	A — чорний список для аргументів із ПРОГРАМНИХ джерел (PackArgs зі збірки):
//	    відкидає прапорці, що виконують код або перенаправляють завантаження.
//	    Значення НЕ перевіряє — лише клас прапорця.
//	B — білий список для аргументів, які пише користувач, імпортована збірка
//	    чи міграція з Prism (JavaArgs / JavaArgsOverride): пропускає лише
//	    відомі безпечні прапорці з перевіреними значеннями.
//
// Нічого тут не торкається ExtraJVMArgs (-javaagent authlib-injector — наш
// власний аргумент) та аргументів з version.json (-p, --add-modules для Forge).
package jvmargs

import (
	"regexp"
	"strings"
)

// Rejection — відхилений аргумент і причина (для консолі та редактора).
type Rejection struct {
	Arg    string `json:"arg"`
	Reason string `json:"reason"`
}

const (
	reasonBlocked  = "небезпечний прапорець (виконує код або перенаправляє завантаження)"
	reasonNotAllow = "немає в списку дозволених прапорців"
	reasonBadValue = "недопустиме значення"
	reasonAtFile   = "аргумент-файл (@…) не підтримується"
)

var (
	reHeapSize = regexp.MustCompile(`^\d{1,12}[kKmMgGtT]?$`)
	reNumber   = regexp.MustCompile(`^\d{1,12}(\.\d{1,4})?[kKmMgGtT]?$`)
	rePropVal  = regexp.MustCompile(`^[A-Za-z0-9._-]{0,64}$`)
	rePropRelx = regexp.MustCompile(`^[A-Za-z0-9._:/-]{0,64}$`)
	reAddSpec  = regexp.MustCompile(`^[A-Za-z0-9_.$]+/[A-Za-z0-9_.$]+=(ALL-UNNAMED|[A-Za-z0-9_.$]+(,[A-Za-z0-9_.$]+)*)$`)
	reModList  = regexp.MustCompile(`^(ALL-UNNAMED|[A-Za-z0-9_.$]+(,[A-Za-z0-9_.$]+)*)$`)
)

func lower(s string) string { return strings.ToLower(s) }

// BlockedForPack — рівень A: чи небезпечний цей прапорець.
func BlockedForPack(arg string) bool {
	a := lower(strings.TrimSpace(arg))
	if a == "" {
		return false
	}
	if blockedExact[a] {
		return true
	}
	for _, p := range blockedPrefixes {
		if strings.HasPrefix(a, p) {
			return true
		}
	}
	return false
}

// UserAllowed — рівень B: чи можна пропустити аргумент користувача.
// Рівень A застосовується першим, тож небезпечне відхиляється навіть якби
// воно випадково потрапило у білий список.
func UserAllowed(arg string) bool { return userReason(arg) == "" }

// userReason повертає причину відхилення або "" якщо аргумент дозволений.
func userReason(arg string) string {
	if arg == "" || arg != strings.TrimSpace(arg) {
		return reasonNotAllow
	}
	if strings.HasPrefix(arg, "@") {
		return reasonAtFile
	}
	if !strings.HasPrefix(arg, "-") {
		return reasonNotAllow // ім'я головного класу, позиційні аргументи тощо
	}
	if BlockedForPack(arg) {
		return reasonBlocked
	}
	low := lower(arg)

	switch {
	case strings.HasPrefix(low, "-xmx"), strings.HasPrefix(low, "-xms"),
		strings.HasPrefix(low, "-xss"), strings.HasPrefix(low, "-xmn"):
		if reHeapSize.MatchString(arg[4:]) {
			return ""
		}
		return reasonBadValue

	case strings.HasPrefix(low, "-xx:"):
		body := arg[4:]
		if len(body) > 1 && (body[0] == '+' || body[0] == '-') {
			if allowedBoolFlags[lower(body[1:])] {
				return ""
			}
			return reasonNotAllow
		}
		name, val, hasEq := strings.Cut(body, "=")
		if !hasEq {
			return reasonNotAllow
		}
		if !allowedValueFlags[lower(name)] {
			return reasonNotAllow
		}
		if reNumber.MatchString(val) {
			return ""
		}
		return reasonBadValue

	case strings.HasPrefix(low, "-d"):
		name, val, hasEq := strings.Cut(arg[2:], "=")
		if !allowedProps[lower(name)] {
			return reasonNotAllow
		}
		re := rePropVal
		if propValueRelaxed[lower(name)] {
			re = rePropRelx
		}
		if !hasEq || re.MatchString(val) {
			return ""
		}
		return reasonBadValue

	case strings.HasPrefix(low, "--add-opens="), strings.HasPrefix(low, "--add-exports="):
		_, spec, _ := strings.Cut(arg, "=")
		if reAddSpec.MatchString(spec) {
			return ""
		}
		return reasonBadValue

	case strings.HasPrefix(low, "--enable-native-access="):
		_, spec, _ := strings.Cut(arg, "=")
		if reModList.MatchString(spec) {
			return ""
		}
		return reasonBadValue

	case strings.HasPrefix(low, "--sun-misc-unsafe-memory-access="):
		switch arg[len("--sun-misc-unsafe-memory-access="):] {
		case "allow", "warn", "debug", "deny":
			return ""
		}
		return reasonBadValue
	}

	switch low {
	case "-server", "-client", "-xnoclassgc", "-xverify:none", "-noverify":
		return ""
	}
	return reasonNotAllow
}

// SanitizeUser розбирає рядок користувацьких аргументів (strings.Fields, як і
// раніше в launcher.go) і лишає лише дозволене. Двотокенна форма
// «--add-opens модуль/пакет=ціль» теж підтримується.
func SanitizeUser(raw string) (kept []string, rejected []Rejection) {
	toks := strings.Fields(raw)
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if (t == "--add-opens" || t == "--add-exports") && i+1 < len(toks) {
			if reAddSpec.MatchString(toks[i+1]) {
				kept = append(kept, t, toks[i+1])
			} else {
				rejected = append(rejected, Rejection{Arg: t + " " + toks[i+1], Reason: reasonBadValue})
			}
			i++
			continue
		}
		if r := userReason(t); r != "" {
			rejected = append(rejected, Rejection{Arg: t, Reason: r})
			continue
		}
		kept = append(kept, t)
	}
	return kept, rejected
}

// FilterPack — рівень A для аргументів збірки (build.LaunchArgs). Токени без
// «-» на початку (--server <host>, позиційні ігрові аргументи) НЕ чіпаємо: це
// game-аргументи, які йдуть ПІСЛЯ головного класу. Виняток — «@файл».
func FilterPack(args []string) (kept []string, rejected []Rejection) {
	for _, a := range args {
		t := strings.TrimSpace(a)
		if t == "" {
			continue
		}
		switch {
		case strings.HasPrefix(t, "@"):
			rejected = append(rejected, Rejection{Arg: t, Reason: reasonAtFile})
		case strings.HasPrefix(t, "-") && BlockedForPack(t):
			rejected = append(rejected, Rejection{Arg: t, Reason: reasonBlocked})
		default:
			kept = append(kept, t)
		}
	}
	return kept, rejected
}
