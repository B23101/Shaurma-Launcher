package autostart

import "testing"

func TestQuotedCommand(t *testing.T) {
	got := quotedCommand(`C:\Program Files\Shaurma\shell.exe`, []string{"--autostart", "--background"})
	want := `"C:\Program Files\Shaurma\shell.exe" --autostart --background`
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
	if got := quotedCommand(`C:\a\shell.exe`, nil); got != `"C:\a\shell.exe"` {
		t.Errorf("без аргументів: %q", got)
	}
}

func TestExecLine(t *testing.T) {
	if got := execLine("/opt/shaurma/shell", []string{"--autostart", "--background"}); got != "/opt/shaurma/shell --autostart --background" {
		t.Errorf("got %q", got)
	}
	if got := execLine("/opt/shaurma/shell", nil); got != "/opt/shaurma/shell" {
		t.Errorf("без аргументів: %q", got)
	}
}
