package ai

import (
	"strings"
	"testing"
)

// Тести контексту краш-діагнозу: головний захист від хибних діагнозів —
// ШІ має знати, ЧИ був краш взагалі (crash-report + ERROR-рядки), і не
// звинувачувати моди за попередження.

// TestPromptNoCrashFacts — без crash-report і без ERROR-рядків промт має
// чесно сказати моделі, що краш не зафіксовано, і містити жорстке правило
// «попередження — не краш».
func TestPromptNoCrashFacts(t *testing.T) {
	cc := CrashContext{
		MCVersion:      "1.20.1",
		Loader:         "fabric",
		LogWindow:      "[WARN] Mod X: config missing, using defaults",
		HasCrashReport: false,
		HasErrorLines:  false,
	}
	p := buildSystemPrompt("uk", cc)

	if !strings.Contains(p, "Crash-report Minecraft: НЕ ЗНАЙДЕНО") {
		t.Errorf("промт має повідомити, що crash-report не знайдено")
	}
	if !strings.Contains(p, "Рядки ERROR у консолі: НЕМАЄ") {
		t.Errorf("промт має повідомити, що ERROR-рядків немає")
	}
	if !strings.Contains(p, "попередження (WARN) — це НЕ краш") {
		t.Errorf("промт має містити правило «WARN — не краш»")
	}
}

// TestPromptCrashFacts — при знайденому crash-report і ERROR-рядках промт
// підтверджує їх наявність.
func TestPromptCrashFacts(t *testing.T) {
	cc := CrashContext{
		MCVersion:      "1.20.1",
		Loader:         "forge",
		LogWindow:      "java.lang.NullPointerException\n\tat mod.class.Main.method(Main.java:42)",
		HasCrashReport: true,
		HasErrorLines:  true,
	}
	p := buildSystemPrompt("uk", cc)

	if !strings.Contains(p, "Crash-report Minecraft: ЗНАЙДЕНО") {
		t.Errorf("промт має повідомити, що crash-report знайдено")
	}
	if !strings.Contains(p, "Рядки ERROR у консолі: Є") {
		t.Errorf("промт має повідомити, що ERROR-рядки є")
	}
}

// TestPromptNoCrashRulePresent — правило «не звинувачуй мод без явного
// exception/stack trace» є у промті ЗАВЖДИ (і для краш-випадку також).
func TestPromptNoCrashRulePresent(t *testing.T) {
	cc := CrashContext{
		MCVersion:      "1.20.1",
		Loader:         "vanilla",
		HasCrashReport: true,
		HasErrorLines:  true,
	}
	p := buildSystemPrompt("uk", cc)

	if !strings.Contains(p, "Щоб назвати мод винуватцем, у даних МАЄ бути явний exception") {
		t.Errorf("промт має містити правило про явний exception для звинувачення мода")
	}
}
