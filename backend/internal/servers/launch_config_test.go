package servers

import (
	"os"
	"path/filepath"
	"testing"
)

// writeNeoForgeInstall відтворює розкладку файлів, яку лишає
// neoforge-<ver>-installer.jar --installServer.
// Дзеркалить write_neoforge_install з modloader_launch.rs тестів (рядки 84-91).
func writeNeoForgeInstall(t *testing.T, serverDir, version string) {
	t.Helper()
	loaderDir := filepath.Join(serverDir, "libraries", "net", "neoforged", "neoforge", version)
	if err := os.MkdirAll(loaderDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(loaderDir, "unix_args.txt"), []byte("-p libraries/a.jar"), 0644); err != nil {
		t.Fatalf("write unix_args.txt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(loaderDir, "win_args.txt"), []byte("-p libraries/a.jar"), 0644); err != nil {
		t.Fatalf("write win_args.txt: %v", err)
	}
}

// (а) Порожній конфіг + лоадер на диску → пересканувати й зберегти.
// Дзеркалить rederives_a_config_that_carries_no_launch_override.
func TestResolveLaunchConfig_RederivesConfigThatCarriesNoLaunchOverride(t *testing.T) {
	dir := t.TempDir()
	writeNeoForgeInstall(t, dir, "21.1.77")
	if err := saveLaunchConfig(dir, vanillaLaunchConfig()); err != nil {
		t.Fatalf("saveLaunchConfig: %v", err)
	}

	cfg, err := resolveLaunchConfig(dir, CoreNeoForge, "21.1.77")
	if err != nil {
		t.Fatalf("resolveLaunchConfig: %v", err)
	}
	if cfg.ArgsFile == "" {
		t.Errorf("очікували знайдений args-файл, отримали порожній LaunchConfig: %+v", cfg)
	}
}

// (б) Те саме → результат персистентний при повторному читанні.
// Дзеркалить rederived_config_is_persisted.
func TestResolveLaunchConfig_RederivedConfigIsPersisted(t *testing.T) {
	dir := t.TempDir()
	writeNeoForgeInstall(t, dir, "21.1.77")
	if err := saveLaunchConfig(dir, vanillaLaunchConfig()); err != nil {
		t.Fatalf("saveLaunchConfig: %v", err)
	}

	if _, err := resolveLaunchConfig(dir, CoreNeoForge, "21.1.77"); err != nil {
		t.Fatalf("resolveLaunchConfig: %v", err)
	}

	reread, err := getLaunchConfig(dir)
	if err != nil {
		t.Fatalf("getLaunchConfig: %v", err)
	}
	if reread.ArgsFile == "" {
		t.Errorf("пересканований конфіг мав бути записаний назад на диск, отримали: %+v", reread)
	}
}

// (в) Конфіг вже щось називає (custom jar_path) → лишити без змін, не
// перезаписувати чужий вибір. Дзеркалить leaves_a_usable_config_alone.
func TestResolveLaunchConfig_LeavesAUsableConfigAlone(t *testing.T) {
	dir := t.TempDir()
	writeNeoForgeInstall(t, dir, "21.1.77")
	existing := LaunchConfig{JarPath: "custom-launch.jar"}
	if err := saveLaunchConfig(dir, existing); err != nil {
		t.Fatalf("saveLaunchConfig: %v", err)
	}

	cfg, err := resolveLaunchConfig(dir, CoreNeoForge, "21.1.77")
	if err != nil {
		t.Fatalf("resolveLaunchConfig: %v", err)
	}
	if cfg.JarPath != "custom-launch.jar" {
		t.Errorf("очікували незмінений custom-launch.jar, отримали JarPath=%q", cfg.JarPath)
	}
	if cfg.ArgsFile != "" {
		t.Errorf("ArgsFile мав лишитись порожнім (чужий вибір не переписується), отримали %q", cfg.ArgsFile)
	}
}

// Vanilla-сервер (без ядра) лишається vanilla — жодного пересканування
// не відбувається, коли core == "". Дзеркалить leaves_a_vanilla_server_vanilla.
func TestResolveLaunchConfig_LeavesAVanillaServerVanilla(t *testing.T) {
	dir := t.TempDir()
	if err := saveLaunchConfig(dir, vanillaLaunchConfig()); err != nil {
		t.Fatalf("saveLaunchConfig: %v", err)
	}

	cfg, err := resolveLaunchConfig(dir, "", "")
	if err != nil {
		t.Fatalf("resolveLaunchConfig: %v", err)
	}
	if cfg.ArgsFile != "" || cfg.JarPath != "" {
		t.Errorf("очікували порожній LaunchConfig для vanilla, отримали %+v", cfg)
	}
}

// (г) Нічого не встановлено на диску → лишити порожній конфіг, повернути
// (не) помилку — жодного вигаданого результату. Дзеркалить
// leaves_config_alone_when_nothing_is_installed: "Nothing on disk to derive
// from: keep the stored config rather than inventing one, so a genuinely
// broken install still reports as such."
func TestResolveLaunchConfig_LeavesConfigAloneWhenNothingIsInstalled(t *testing.T) {
	dir := t.TempDir()
	if err := saveLaunchConfig(dir, vanillaLaunchConfig()); err != nil {
		t.Fatalf("saveLaunchConfig: %v", err)
	}

	cfg, err := resolveLaunchConfig(dir, CoreNeoForge, "21.1.77")
	if err != nil {
		t.Fatalf("resolveLaunchConfig: %v", err)
	}
	if cfg.ArgsFile != "" || cfg.JarPath != "" {
		t.Errorf("нічого не встановлено на диску — конфіг мав лишитись порожнім, отримали %+v", cfg)
	}
}

// TestResolveLaunchConfig_ForgeLauncherJarTakesPriorityOverArgsFile —
// додатковий тест (не з оригіналу, специфічний для Forge — розділ 1.2):
// коли на корені є і launcher-jar (шим/universal), і args-файл під
// libraries/, launcher-jar має пріоритет, бо він самодостатній.
func TestResolveLaunchConfig_ForgeLauncherJarTakesPriorityOverArgsFile(t *testing.T) {
	dir := t.TempDir()
	loaderDir := filepath.Join(dir, "libraries", "net", "minecraftforge", "forge", "1.20.1-47.4.10")
	os.MkdirAll(loaderDir, 0755)
	os.WriteFile(filepath.Join(loaderDir, "unix_args.txt"), []byte("-p a.jar"), 0644)
	os.WriteFile(filepath.Join(dir, "forge-1.20.1-47.4.10-universal.jar"), []byte("fake"), 0644)

	if err := saveLaunchConfig(dir, vanillaLaunchConfig()); err != nil {
		t.Fatalf("saveLaunchConfig: %v", err)
	}

	cfg, err := resolveLaunchConfig(dir, CoreForge, "1.20.1-47.4.10")
	if err != nil {
		t.Fatalf("resolveLaunchConfig: %v", err)
	}
	if cfg.JarPath == "" {
		t.Errorf("очікували JarPath (launcher-jar пріоритетний), отримали %+v", cfg)
	}
	if cfg.ArgsFile != "" {
		t.Errorf("ArgsFile не мав заповнюватись, коли є launcher-jar, отримали %+v", cfg)
	}
}

// TestFindForgeLauncherJar_ExcludesInstaller — findForgeLauncherJar не
// має плутати installer-jar (файл, який треба ЗАПУСТИТИ) із
// launcher-jar (файлом, яким треба ЗАПУСКАТИ).
func TestFindForgeLauncherJar_ExcludesInstaller(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "forge-1.20.1-47.4.10-installer.jar"), []byte("fake"), 0644)

	if _, ok := findForgeLauncherJar(dir); ok {
		t.Error("findForgeLauncherJar не мав знаходити installer.jar")
	}

	os.WriteFile(filepath.Join(dir, "forge-1.20.1-47.4.10-universal.jar"), []byte("fake"), 0644)
	jar, ok := findForgeLauncherJar(dir)
	if !ok || jar != "forge-1.20.1-47.4.10-universal.jar" {
		t.Errorf("очікували universal.jar, отримали jar=%q ok=%v", jar, ok)
	}
}
