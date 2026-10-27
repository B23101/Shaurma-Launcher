package core

import (
	"os"
	"path/filepath"
	"testing"
)

// TestOtherChannel — просте дзеркало shaurma<->base, обидва напрямки.
func TestOtherChannel(t *testing.T) {
	if otherChannel(ChannelShaurma) != ChannelBase {
		t.Errorf("otherChannel(shaurma) = %q, очікували base", otherChannel(ChannelShaurma))
	}
	if otherChannel(ChannelBase) != ChannelShaurma {
		t.Errorf("otherChannel(base) = %q, очікували shaurma", otherChannel(ChannelBase))
	}
}

// TestCleanupOtherEditionExe_RemovesOtherExe — сценарій із багу: в
// installDir лишився .exe ІНШОЇ редакції (перевстановили shaurma поверх
// base чи навпаки в ту саму теку). cleanupOtherEditionExe має прибрати
// саме його (і .previous/.new артефакти), не чіпаючи .exe ПОТОЧНОЇ
// редакції чи довільні спільні файли (JSON, backend.dll-аналог).
func TestCleanupOtherEditionExe_RemovesOtherExe(t *testing.T) {
	dir := t.TempDir()

	currentExe := filepath.Join(dir, launcherExeName(ChannelShaurma))
	otherExe := filepath.Join(dir, launcherExeName(ChannelBase))
	otherExePrevious := otherExe + ".previous"
	otherExeNew := otherExe + ".new"
	sharedJSON := filepath.Join(dir, "launcher-location.json")
	sharedComponent := filepath.Join(dir, "frontend.zip")

	for _, p := range []string{currentExe, otherExe, otherExePrevious, otherExeNew, sharedJSON, sharedComponent} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatalf("setup WriteFile(%s): %v", p, err)
		}
	}

	cleanupOtherEditionExe(dir, ChannelShaurma, Callbacks{})

	// Чужий .exe і його артефакти заміни — мають зникнути.
	for _, p := range []string{otherExe, otherExePrevious, otherExeNew} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("cleanupOtherEditionExe мав видалити %s, але файл лишився", p)
		}
	}

	// Свій .exe і спільні файли (JSON, компоненти) — НЕ повинні чіпатись.
	for _, p := range []string{currentExe, sharedJSON, sharedComponent} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("cleanupOtherEditionExe не мав чіпати %s, але файл зник: %v", p, err)
		}
	}
}

// TestCleanupOtherEditionExe_NoOtherEdition — якщо в теці лежить лише
// .exe поточної редакції (звичайна робота без перемикання) — функція не
// повинна нічого видаляти й не падати.
func TestCleanupOtherEditionExe_NoOtherEdition(t *testing.T) {
	dir := t.TempDir()
	currentExe := filepath.Join(dir, launcherExeName(ChannelBase))
	if err := os.WriteFile(currentExe, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup WriteFile: %v", err)
	}

	cleanupOtherEditionExe(dir, ChannelBase, Callbacks{})

	if _, err := os.Stat(currentExe); err != nil {
		t.Errorf("cleanupOtherEditionExe не мав чіпати єдиний наявний .exe: %v", err)
	}
}
