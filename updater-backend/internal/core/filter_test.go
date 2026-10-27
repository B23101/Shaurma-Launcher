package core

import (
	"os"
	"path/filepath"
	"testing"

	"shrm-updater-backend/internal/manifest"
)

// TestIsLockedComponent — файли поточного процесу (updater.dll — ця сама
// DLL; ShaurmLauncher.exe/ShaurmLauncherBase.exe — виконуваний exe shell'а)
// НЕ замінюються на місці: install.Apply кладе їх поруч як <name>.new, а
// застосовує сам shell (applyPendingUpdates / applyShellSelfUpdate). Усе
// інше (backend.dll, frontend.zip) оновлюється безпосередньо.
func TestIsLockedComponent(t *testing.T) {
	if !manifest.IsLockedComponent("updater.dll") {
		t.Fatal("updater.dll (завантажена зараз DLL) має бути locked-компонентом -> .new")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	if !manifest.IsLockedComponent(filepath.Base(exe)) {
		t.Fatalf("виконуваний зараз exe (%s) має бути locked-компонентом -> .new", filepath.Base(exe))
	}
	if manifest.IsLockedComponent("backend.dll") {
		t.Fatal("backend.dll під час фази оновлення НЕ завантажена — має оновлюватись на місці")
	}
	if manifest.IsLockedComponent("frontend.zip") {
		t.Fatal("frontend.zip має оновлюватись на місці")
	}
}

// TestMissingRequiredComponentsSkipsShell — шлях відновлення втрачених
// файлів (missingRequiredComponents) свідомо НЕ чіпає shell exe: відсутній
// shell — це робота bootstrap/інсталятора (качка через .new безглузда,
// коли файлу немає що замінювати). Сам Diff при бампі версії при цьому
// ВКЛЮЧАЄ shell (він оновлюється через self-update .new) — це перевіряє
// TestDiffIncludesShell.
func TestMissingRequiredComponentsSkipsShell(t *testing.T) {
	dir := t.TempDir()
	// Створюємо на диску всі файли, крім shell — тоді missing має бути
	// ПОРОЖНІМ (shell пропускається, решта на місці).
	remote := &manifest.Manifest{Components: []manifest.Component{
		{Path: "ShaurmLauncher.exe"},
		{Path: "ShaurmLauncherBase.exe"},
		{Path: "backend.dll"},
		{Path: "frontend.zip", Kind: "archive", ExtractTo: "frontend"},
	}}
	for _, c := range remote.Components {
		if c.Kind == "archive" {
			// Для архівів missingRequiredComponents дивиться на НЕПОРОЖНЮ
			// теку extractTo (порожня = нічого не розпаковано).
			dir := filepath.Join(dir, c.ExtractTo)
			os.MkdirAll(dir, 0o755)
			os.WriteFile(filepath.Join(dir, "index.html"), []byte("x"), 0o644)
			continue
		}
		os.WriteFile(filepath.Join(dir, c.Path), []byte("x"), 0o644)
	}
	missing := missingRequiredComponents(dir, remote)
	if len(missing) != 0 {
		t.Fatalf("відсутні shell-компоненти не мають потрапляти у відновлення, отримано: %v", missing)
	}

	// А якщо немає backend.dll — він МАЄ потрапити у відновлення.
	os.Remove(filepath.Join(dir, "backend.dll"))
	missing = missingRequiredComponents(dir, remote)
	if len(missing) != 1 || missing[0].Path != "backend.dll" {
		t.Fatalf("брак backend.dll має дати рівно його у відновленні, отримано: %v", missing)
	}
}

// TestDiffIncludesShell — при новішій віддаленій версії зі зміненим
// shell.exe Diff має ВКЛЮЧИТИ shell у список качки (на відміну від старої
// моделі filterOutSelfComponents). Далі install.Apply сам вирішить, що
// shell — locked-компонент, і поставить його як .new.
func TestDiffIncludesShell(t *testing.T) {
	local := &manifest.Manifest{Version: "1.0.0", Components: []manifest.Component{
		{Path: "ShaurmLauncher.exe", SHA256: "aaaa", Size: 1},
		{Path: "backend.dll", SHA256: "bbbb", Size: 1},
	}}
	remote := &manifest.Manifest{Version: "1.0.1", Components: []manifest.Component{
		{Path: "ShaurmLauncher.exe", SHA256: "cccc", Size: 2}, // змінився
		{Path: "backend.dll", SHA256: "bbbb", Size: 1},        // той самий
	}}
	diff := manifest.Diff(local, remote)
	found := false
	for _, c := range diff {
		if c.Path == "ShaurmLauncher.exe" {
			found = true
		}
	}
	if !found {
		t.Fatalf("змінений shell.exe має бути в Diff (self-update через .new): %v", diff)
	}
	if len(diff) != 1 {
		t.Fatalf("незмінений backend.dll не має качатись заново: %v", diff)
	}
}
