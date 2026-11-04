package core

import (
	"reflect"
	"testing"

	"shrm-updater-backend/internal/manifest"
)

func TestFilterFlatpakBundle(t *testing.T) {
	list := []manifest.Component{{Path: "backend.so"}, {Path: "ShaurmaLauncher.flatpak"}, {Path: "frontend.zip", Kind: "archive"}}
	if got := filterFlatpakBundle(list, true); len(got) != 1 || got[0].Path != "ShaurmaLauncher.flatpak" {
		t.Fatalf("у Flatpak має лишитися тільки пакет: %+v", got)
	}
	if got := filterFlatpakBundle(list, false); len(got) != 2 || isFlatpakBundle(got[0]) || isFlatpakBundle(got[1]) {
		t.Fatalf("поза Flatpak пакет не качається: %+v", got)
	}
}

func TestParseFlatpakInfo(t *testing.T) {
	user := "[Application]\nname=com.shaurma.Launcher\nruntime=runtime/org.gnome.Platform/x86_64/50\n\n[Instance]\napp-path=/home/u/.local/share/flatpak/app/com.shaurma.Launcher/x86_64/master/abc/files\n"
	info, err := parseFlatpakInfo(user)
	if err != nil || info.AppID != "com.shaurma.Launcher" || info.System {
		t.Fatalf("user: %+v %v", info, err)
	}
	want := []string{"--host", "flatpak", "install", "--user", "--noninteractive", "-y", "--reinstall", "--bundle", "/x/a.flatpak"}
	if got := flatpakInstallArgs(info, "/x/a.flatpak"); !reflect.DeepEqual(got, want) {
		t.Fatalf("args: %v", got)
	}
	sys := "[Application]\nname=com.shaurma.Launcher\n[Instance]\napp-path=/var/lib/flatpak/app/com.shaurma.Launcher/x86_64/master/abc/files\n"
	if info, _ := parseFlatpakInfo(sys); !info.System || flatpakInstallArgs(info, "b")[3] != "--system" {
		t.Fatalf("system: %+v", info)
	}
	if _, err := parseFlatpakInfo("[Application]\nname=x; rm -rf /\n"); err == nil {
		t.Fatal("некоректний app-id має відхилятися")
	}
}
