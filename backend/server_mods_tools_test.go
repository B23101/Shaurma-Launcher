package main

import (
	"testing"

	"shaurma-launcher-backend/internal/model"
)

func mod(id, name, fileName, version string, enabled bool) model.ModEntry {
	return model.ModEntry{ID: id, Name: name, FileName: fileName, Version: version, Enabled: enabled}
}

// Зіставлення модів між серверами має триматися на ID з jar-метаданих
// (перейменований файл — той самий мод), а без ID — на нормалізованому імені
// файлу (з .jar і .disabled знятими): інакше вимкнений мод одного сервера
// вважався б «відсутнім» на іншому.
func TestModCompareKey(t *testing.T) {
	cases := []struct {
		name string
		in   model.ModEntry
		want string
	}{
		{"ID має приоритет", mod("sodium", "Sodium", "sodium-0.5.8.jar", "0.5.8", true), "id:sodium"},
		{"ID превалює над іменем файлу", mod("Sodium", "Інша назва", "x.jar", "1", true), "id:sodium"},
		{"без ID — ім'я файлу без .jar", mod("", "", "example-1.0.jar", "1.0", true), "name:example-1.0"},
		{"без ID — вимкнений файл збігається з увімкненим", mod("", "", "example-1.0.jar.disabled", "1.0", false), "name:example-1.0"},
		{"без ID — назва замість файлу", mod("", "Cool Mod", "file.jar", "1", true), "name:cool mod"},
	}
	for _, c := range cases {
		if got := modCompareKey(c.in); got != c.want {
			t.Errorf("%s: modCompareKey = %q, очікували %q", c.name, got, c.want)
		}
	}
}

func TestCompareModSets(t *testing.T) {
	a := []model.ModEntry{
		mod("fabric-api", "Fabric API", "fabric-api-0.92.jar", "0.92", true),
		mod("sodium", "Sodium", "sodium-0.5.8.jar", "0.5.8", true),
		mod("", "Local thing", "local-thing.jar", "1.0", false),
	}
	b := []model.ModEntry{
		// Той самий мод, інша версія і інше ім'я файлу — мусить бути «в обох».
		mod("sodium", "Sodium", "sodium-0.6.0.jar", "0.6.0", true),
		mod("fabric-api", "Fabric API", "fabric-api-0.92.jar", "0.92", true),
		mod("lithium", "Lithium", "lithium-0.15.jar", "0.15", true),
	}
	onlyA, onlyB, both, mismatch := compareModSets(a, b)

	if len(onlyA) != 1 || onlyA[0].Name != "Local thing" {
		t.Fatalf("тільки A = %+v, очікували один мод «Local thing»", onlyA)
	}
	if len(onlyB) != 1 || onlyB[0].Name != "Lithium" {
		t.Fatalf("тільки B = %+v, очікували один мод «Lithium»", onlyB)
	}
	if len(both) != 2 {
		t.Fatalf("спільних = %d, очікували 2 (%+v)", len(both), both)
	}
	if mismatch != 1 {
		t.Fatalf("розбіжних версій = %d, очікували 1", mismatch)
	}
	for _, e := range both {
		if e.Name == "Sodium" {
			if !e.VersionMismatch {
				t.Error("Sodium має бути позначений як різні версії")
			}
			if e.Version != "0.5.8" || e.OtherVersion != "0.6.0" {
				t.Errorf("версії Sodium: %q vs %q", e.Version, e.OtherVersion)
			}
		}
		if e.Name == "Fabric API" && e.VersionMismatch {
			t.Error("однакова версія Fabric API не має позначатись як розбіжність")
		}
	}
}

// Порожні набори: результат — порожні (а не nil) зрізи. nil серіалізується
// в JSON як null, і фронтенд робить list.length — той самий клас бага, що
// вже ловили в ScanModsFull.
func TestCompareModSets_EmptyIsNotNil(t *testing.T) {
	onlyA, onlyB, both, mismatch := compareModSets(nil, nil)
	if onlyA == nil || onlyB == nil || both == nil {
		t.Fatalf("зрізи мають бути порожніми, а не nil: %v %v %v", onlyA, onlyB, both)
	}
	if mismatch != 0 {
		t.Fatalf("mismatch = %d, очікували 0", mismatch)
	}
}
