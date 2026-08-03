package java

import (
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// ── Java-резолвінг за версією Minecraft ──────────────────────────────────
//
// Портуємо рекомендовану версію Java зі старого лаунчера (JavaResolver.java)
// та Prism Launcher: кожна версія Minecraft вимагає свою Java.
//   - MC < 1.17            → Java 8
//   - MC 1.17 – 1.20.4     → Java 17
//   - MC 1.20.5+ (1.21.x)  → Java 21
//   - майбутні snapshot    → Java 25 (java-runtime-epsilon), якщо
//     version.json не вказує javaVersion.majorVersion явно
//
// RecommendedMajor використовується для вибору Java перед запуском;
// DetectMajor — щоб перевірити, яку версію має користувацький java.exe і
// попередити, чи вона підходить для збірки.

var versionRe = regexp.MustCompile(`version "(\d+)(?:\.(\d+))?`)

// DetectMajor визначає major-версію Java за шляхом до виконуваного файлу
// (запускає «<exe> -version» і парсить вивід). Не валідна/недоступна —
// повертає 0 (невідомо), ніколи не помилка.
func DetectMajor(javaExe string) int {
	if javaExe == "" {
		return 0
	}
	out, err := exec.Command(javaExe, "-version").CombinedOutput()
	if err != nil {
		return 0
	}
	m := versionRe.FindStringSubmatch(string(out))
	if m == nil {
		return 0
	}
	major, err1 := strconv.Atoi(m[1])
	if err1 != nil {
		return 0
	}
	// Стара нотація: 1.8.0_51 → 8
	if major == 1 && len(m) > 2 && m[2] != "" {
		if minor, err2 := strconv.Atoi(m[2]); err2 == nil {
			major = minor
		}
	}
	return major
}

// RecommendedMajor визначає рекомендовану major-версію Java для вказаної
// версії Minecraft. Підтримує обидва формати: «1.21.8» (стандартний) і
// «26.1.2» (snapshot/internal ID нової нумерації Mojang).
func RecommendedMajor(mcVersion string) int {
	if mcVersion == "" {
		return 17
	}
	parts := strings.Split(mcVersion, ".")

	// Спеціальні випадки: «25w14craftmine», «1.21-pre1», «1.21.5-pre1».
	// Виділяємо числовий префікс з першого сегмента.
	if len(parts) < 2 {
		first := parts[0]
		var numStr strings.Builder
		for i := 0; i < len(first); i++ {
			c := first[i]
			if c >= '0' && c <= '9' {
				numStr.WriteByte(c)
			} else if numStr.Len() > 0 {
				break
			}
		}
		if numStr.Len() > 0 {
			if major, err := strconv.Atoi(numStr.String()); err == nil {
				return mapSnapshotMajor(major)
			}
		}
		return 17
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		// Fallback для нестандартних назв.
		first := regexp.MustCompile(`[^0-9]`).Split(mcVersion, 2)[0]
		if first != "" {
			if m, err := strconv.Atoi(first); err == nil {
				return mapSnapshotMajor(m)
			}
		}
		return 17
	}

	// Стандартний формат «1.X.Y».
	if major == 1 {
		minor := 0
		if len(parts) > 1 {
			minor = parseLeadingInt(parts[1])
		}
		patch := 0
		if len(parts) > 2 {
			patch = parseLeadingInt(parts[2])
		}
		// 1.21+ або 1.20.5+ = Java 21
		if minor >= 21 {
			return 21
		}
		if minor == 20 && patch >= 5 {
			return 21
		}
		// 1.17–1.20.4 = Java 17
		if minor >= 17 {
			return 17
		}
		// <1.17 = Java 8
		return 8
	}

	// Snapshot/internal формат «26.1.2», «25.0.3».
	return mapSnapshotMajor(major)
}

// mapSnapshotMajor мапує мажорну версію snapshot/internal формату на Java.
// >= 30 → Java 30; >= 25 → Java 25; >= 21 → Java 21; >= 17 → Java 17; < 17 → Java 8.
//
// ПРИМІТКА: це лише FALLBACK-евристика на випадок, коли у version.json
// немає поля javaVersion.majorVersion (див. VersionJSON.JavaVersion у
// installer.go) — а це поле є пріоритетним джерелом і має
// перевизначати результат цієї функції там, де воно доступне (так само
// робить і Prism: для Quilt 26.2 Mojang вказує в json компоненту
// java-runtime-epsilon → Java 25, і Prism її й ставить — це підтверджено
// логом реального запуску, гра на Java 25 працює коректно).
//
// Для майбутніх версій (30+) ця функція повертає саме той major, який
// вказаний у версії MC. Якщо Mojang додасть Java 30 як "java-runtime-zeta",
// version.json міститиме javaVersion.component = "java-runtime-zeta" і
// majorVersion = 30, і лаунчер автоматично його підтримає.
func mapSnapshotMajor(major int) int {
	// Для нових версій (30+) повертаємо як є - component буде з version.json
	if major >= 30 {
		return major
	}
	if major >= 25 {
		return 25
	}
	if major >= 21 {
		return 21
	}
	if major >= 17 {
		return 17
	}
	return 8
}

// parseLeadingInt витягує число з початку рядка («21w» → 21, «5-pre1» → 5).
func parseLeadingInt(s string) int {
	var numStr strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= '0' && c <= '9' {
			numStr.WriteByte(c)
		} else if numStr.Len() > 0 {
			break
		}
	}
	if numStr.Len() == 0 {
		return 0
	}
	n, _ := strconv.Atoi(numStr.String())
	return n
}
