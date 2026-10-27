package main

import (
	"strings"
	"testing"

	"shaurma-launcher-backend/internal/model"
)

func TestSlimModEntriesForUIKeepsOriginal(t *testing.T) {
	long := strings.Repeat("я", 500)
	in := []model.ModEntry{{
		FileName: "a.jar", Name: "A", Description: long,
		DependsOn: []string{"x"}, Provides: []string{"y"}, MissingDeps: []string{"z"},
		Loaders: []string{"fabric"}, Sha1: "abc",
	}}
	out := slimModEntriesForUI(in)

	if got := len([]rune(out[0].Description)); got != uiModDescMax+1 {
		t.Fatalf("опис не обрізано: %d рун", got)
	}
	if out[0].DependsOn != nil || out[0].Provides != nil || out[0].MissingDeps != nil ||
		out[0].Loaders != nil || out[0].Sha1 != "" {
		t.Fatal("важкі поля мали бути прибрані з копії")
	}
	// Оригінал (кеші бекенду) не чіпаємо.
	if in[0].Description != long || in[0].Sha1 != "abc" || len(in[0].Loaders) != 1 || len(in[0].DependsOn) != 1 {
		t.Fatal("оригінальний слайс мутовано")
	}
}

func TestSlimBridgeResultOnlyKnownMethods(t *testing.T) {
	list := []model.ModEntry{{Sha1: "abc"}}
	if r := slimBridgeResult("SomethingElse", list).([]model.ModEntry); r[0].Sha1 != "abc" {
		t.Fatal("чужий метод не має худнути")
	}
	if r := slimBridgeResult("ScanModsWithCachedUpdates", list).([]model.ModEntry); r[0].Sha1 != "" {
		t.Fatal("відомий метод мав схуднути")
	}
	if r := slimBridgeResult("ScanModsFull", "not a list"); r != "not a list" {
		t.Fatal("не-список має проходити без змін")
	}
}
