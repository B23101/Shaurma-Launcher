package main

import (
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"
)

// Тести експорту списку модів (порт Prism ExportToModList):
// перевіряємо, що кожен формат коректно збирає рядки з опційними полями.

func TestExportModsMarkdown(t *testing.T) {
	rows := []modRow{
		{name: "Sodium", version: "0.5.8", authors: "jellysquid3", fileName: "sodium-0.5.8.jar", url: "https://modrinth.com/mod/sodium"},
		{name: "Iris", version: "1.6.17", authors: "coderbot", fileName: "iris-1.6.17.jar", url: ""},
	}
	opts := ModListExportOptions{Format: "markdown", Authors: true, Url: true, Version: true, FileName: false}
	out := exportModsMarkdown(rows, opts)
	lines := strings.Split(out, "\n")
	if len(lines) != 2 {
		t.Fatalf("очікували 2 рядки, отримали %d: %q", len(lines), out)
	}
	if !strings.Contains(lines[0], "[Sodium](https://modrinth.com/mod/sodium)") {
		t.Errorf("перший рядок має містити markdown-лінк: %q", lines[0])
	}
	if !strings.Contains(lines[0], "[0.5.8]") || !strings.Contains(lines[0], "by jellysquid3") {
		t.Errorf("перший рядок має містити версію та автора: %q", lines[0])
	}
	// Без URL — просто назва без лінка.
	if !strings.Contains(lines[1], "Iris") || strings.Contains(lines[1], "(") {
		t.Errorf("другий рядок без URL не має містити дужок: %q", lines[1])
	}
}

func TestExportModsHTML(t *testing.T) {
	rows := []modRow{{name: "Sodium <b>", version: "", authors: "", fileName: "", url: ""}}
	out := exportModsHTML(rows, ModListExportOptions{Format: "html"})
	// HTML-екранування: <b> не має потрапити в розмітку як тег.
	if strings.Contains(out, "<b>") || !strings.Contains(out, "&lt;b&gt;") {
		t.Errorf("HTML має екранувати назву мода: %q", out)
	}
	if !strings.Contains(out, "<li>") || !strings.HasPrefix(out, "<html>") {
		t.Errorf("HTML-структура неправильна: %q", out)
	}
}

func TestExportModsJSON(t *testing.T) {
	rows := []modRow{{name: "Sodium", version: "0.5.8", authors: "jellysquid3", fileName: "sodium.jar", url: "https://modrinth.com/mod/sodium"}}
	out, err := exportModsJSON(rows, ModListExportOptions{Format: "json", Authors: true, Version: true, FileName: true, Url: true})
	if err != nil {
		t.Fatalf("exportModsJSON: %v", err)
	}
	var parsed []map[string]string
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("невалідний JSON: %v\n%s", err, out)
	}
	if len(parsed) != 1 || parsed[0]["name"] != "Sodium" || parsed[0]["version"] != "0.5.8" {
		t.Errorf("JSON-вміст неправильний: %+v", parsed)
	}
	// Без опцій — лише name.
	out2, _ := exportModsJSON(rows, ModListExportOptions{Format: "json"})
	var parsed2 []map[string]string
	json.Unmarshal([]byte(out2), &parsed2)
	if len(parsed2[0]) != 1 {
		t.Errorf("без опцій має бути лише name: %+v", parsed2[0])
	}
}

func TestExportModsCSV(t *testing.T) {
	rows := []modRow{{name: "Sodium", version: "0.5.8", authors: "jellysquid3", fileName: "sodium.jar", url: ""}}
	out, err := exportModsCSV(rows, ModListExportOptions{Format: "csv", Authors: true, Version: true})
	if err != nil {
		t.Fatalf("exportModsCSV: %v", err)
	}
	r := csv.NewReader(strings.NewReader(out))
	recs, err := r.ReadAll()
	if err != nil {
		t.Fatalf("невалідний CSV: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("очікували заголовок+рядок, отримали %d", len(recs))
	}
	if recs[0][0] != "name" || recs[0][1] != "version" || recs[0][2] != "author" {
		t.Errorf("заголовок неправильний: %v", recs[0])
	}
	if recs[1][0] != "Sodium" || recs[1][1] != "0.5.8" {
		t.Errorf("рядок неправильний: %v", recs[1])
	}
}

func TestExportModsPlain(t *testing.T) {
	rows := []modRow{{name: "Sodium", version: "0.5.8", authors: "jellysquid3", fileName: "", url: "https://modrinth.com/mod/sodium"}}
	out := exportModsPlain(rows, ModListExportOptions{Format: "plain", Authors: true, Version: true, Url: true})
	if !strings.Contains(out, "Sodium (https://modrinth.com/mod/sodium) [0.5.8] by jellysquid3") {
		t.Errorf("plain-рядок неправильний: %q", out)
	}
}
