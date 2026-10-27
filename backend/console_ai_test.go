package main

import (
	"fmt"
	"testing"

	"shaurma-launcher-backend/internal/console"
	"shaurma-launcher-backend/internal/model"
)

func mkConsoleLine(level, text string) model.ConsoleLine {
	return model.ConsoleLine{Level: level, Text: text}
}

// lastNWarnError: лише WARN/ERROR, дублікати (навіть з різними часовими
// мітками) пишуться ОДИН раз (лишається найновіший), кап — n унікальних.
func TestLastNWarnError(t *testing.T) {
	lines := []model.ConsoleLine{
		mkConsoleLine(console.LevelInfo, "[10:00:00] [main/INFO] Starting game"),
		mkConsoleLine(console.LevelError, "[10:00:01] [main/ERROR] Boom!"),
		mkConsoleLine(console.LevelError, "[10:00:02] [main/ERROR] Boom!"), // дублікат (інша мітка)
		mkConsoleLine(console.LevelError, "[10:00:03] [main/ERROR] Boom!"), // ще дублікат
		mkConsoleLine(console.LevelWarn, "[10:00:04] [main/WARN] Careful"),
		mkConsoleLine(console.LevelSystem, "Процес гри завершено з кодом виходу 1"),
		mkConsoleLine(console.LevelError, "[10:00:05] [main/ERROR] Boom!"), // дублікат після warn
	}
	got := lastNWarnError(lines, 50)
	// Дублікати Boom! схлопнулись: лишився найновіший (10:00:05), warn на місці,
	// info/system відкинуті. Порядок хронологічний.
	want := "[10:00:04] [main/WARN] Careful\n[10:00:05] [main/ERROR] Boom!"
	if got != want {
		t.Fatalf("lastNWarnError:\n got: %q\nwant: %q", got, want)
	}
}

// Одна й та сама помилка 200 разів поспіль → в промт потрапляє ОДИН рядок
// (з текстом найновішого входження).
func TestLastNWarnErrorSpamCollapses(t *testing.T) {
	var spam []model.ConsoleLine
	for i := 0; i < 200; i++ {
		spam = append(spam, mkConsoleLine(console.LevelError, "[10:00:00] [main/ERROR] Loop error"))
	}
	if got := lastNWarnError(spam, 50); got != "[10:00:00] [main/ERROR] Loop error" {
		t.Fatalf("спам не схлопнувся: %q", got)
	}
}

// Кап на n: унікальних рядків більше n → лишаються ОСТАННІ n.
func TestLastNWarnErrorCap(t *testing.T) {
	var lines []model.ConsoleLine
	for i := 0; i < 100; i++ {
		lines = append(lines, mkConsoleLine(console.LevelError, fmt.Sprintf("[10:00:%02d] [main/ERROR] ERR%d", i%60, i)))
		lines = append(lines, mkConsoleLine(console.LevelInfo, fmt.Sprintf("[10:00:%02d] [main/INFO] INFO", i%60)))
	}
	got := lastNWarnError(lines, 50)
	// 100 унікальних ERR з перервами INFO → залишається 50 останніх.
	if n := len(splitLines(got)); n != 50 {
		t.Fatalf("очікували 50 рядків, отримали %d: %q", n, got)
	}
}

func splitLines(s string) []string {
	var out []string
	for i := 0; i < len(s); i++ {
		for j := i; j <= len(s); j++ {
			if j == len(s) || s[j] == '\n' {
				out = append(out, s[i:j])
				i = j
				break
			}
		}
	}
	return out
}

func TestConsoleLineDedupKey(t *testing.T) {
	if got := consoleLineDedupKey("[12:34:56] [main/ERROR] x"); got != "[main/ERROR] x" {
		t.Fatalf("часова мітка не зрізалась: %q", got)
	}
	if got := consoleLineDedupKey("plain text"); got != "plain text" {
		t.Fatalf("без мітки мало лишитись як є: %q", got)
	}
	if got := consoleLineDedupKey("[1:02:03] err"); got != "[1:02:03] err" {
		// Мітка має бути рівно [HH:MM:SS] — одноцифрова година не зрізається,
		// але й не ламає рядок.
		t.Fatalf("некоректна мітка мала лишитись як є: %q", got)
	}
}

// findCrashReportFile: основний маркер і fallback, порожньо коли немає.
func TestFindCrashReportFile(t *testing.T) {
	lines := []model.ConsoleLine{
		mkConsoleLine(console.LevelInfo, "[10:00:00] [main/INFO] hello"),
		mkConsoleLine(console.LevelError, "#@!@# Game crashed! Crash report saved to: #@!@# C:\\mc\\crash-reports\\crash-1.txt"),
	}
	if got := findCrashReportFile(lines); got != "C:\\mc\\crash-reports\\crash-1.txt" {
		t.Fatalf("основний маркер не знайдено: %q", got)
	}

	fallback := []model.ConsoleLine{
		mkConsoleLine(console.LevelWarn, "[10:00:01] [main/WARN] Crash report saved to: /tmp/crash-2.txt"),
	}
	if got := findCrashReportFile(fallback); got != "/tmp/crash-2.txt" {
		t.Fatalf("fallback-маркер не знайдено: %q", got)
	}

	if got := findCrashReportFile(nil); got != "" {
		t.Fatalf("порожній список мав дати \"\": %q", got)
	}
	if got := findCrashReportFile([]model.ConsoleLine{mkConsoleLine(console.LevelInfo, "nothing here")}); got != "" {
		t.Fatalf("без маркера мав бути \"\": %q", got)
	}
}

func TestSanitizeAIKind(t *testing.T) {
	cases := map[string]string{
		"missing_dependency":   model.AIKindMissingDependency,
		"MISSING_DEPENDENCY":   model.AIKindMissingDependency,
		"mod_conflict":         model.AIKindModConflict,
		"SINGLE_MOD_ERROR":     model.AIKindSingleModError,
		"env_corruption":       model.AIKindEnvCorruption,
		"java_version":         model.AIKindJavaVersion,
		"UNKNOWN":              model.AIKindUnknown,
		"":                     model.AIKindUnknown,
		"totally_made_up_kind": model.AIKindUnknown,
		// Сміття навколо відомого типу (крапка, зайві пробіли) → UNKNOWN.
		"missing_dependency. ": model.AIKindUnknown,
	}
	for in, want := range cases {
		if got := sanitizeAIKind(in); got != want {
			t.Fatalf("sanitizeAIKind(%q) = %q, хотіли %q", in, got, want)
		}
	}
}
