package main

import "testing"

func TestAIReviewHelpers(t *testing.T) {
	if len(aiBaselineMods("forge")) != 3 || len(aiBaselineMods("fabric")) != 3 || aiBaselineMods("vanilla") != nil {
		t.Fatal("baseline за лоадером")
	}
	if min, rec := aiReviewRAMHint(65); min != 4 || rec != 6 {
		t.Fatalf("RAM для 65 модів: %d/%d", min, rec)
	}
	if !aiPreRe.MatchString("Create-DnDesire-1.20.1-0.1b.Release-Early-Dev.jar") {
		t.Fatal("early-dev має розпізнаватись")
	}
	if aiPreRe.MatchString("create-1.20.1-6.0.8.jar") {
		t.Fatal("стабільна версія не має бути позначена")
	}
	if tl, ok := aiAgentTools["review_build"]; !ok || tl.write {
		t.Fatal("review_build має бути інструментом лише для читання")
	}
}
