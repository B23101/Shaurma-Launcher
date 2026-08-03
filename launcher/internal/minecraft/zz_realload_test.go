package minecraft

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shaurma-launcher-wails/internal/model"
)

// TestRealForgeProfile — перевірка збору аргументів з РЕАЛЬНО встановленого
// профілю Forge 1.20.1 (якщо він є на диску). Не падає, якщо профілю нема.
func TestRealForgeProfile(t *testing.T) {
	base := filepath.Join(os.Getenv("APPDATA"), ".shaurm", "minecraft")
	id := "1.20.1-forge-47.4.10"
	jsonPath := filepath.Join(base, "versions", id, id+".json")
	if _, err := os.Stat(jsonPath); err != nil {
		t.Skip("forge профіль не знайдено: " + jsonPath)
	}
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	var vjson VersionJSON
	if err := json.Unmarshal(data, &vjson); err != nil {
		t.Fatal(err)
	}
	cfg := LaunchConfig{
		Instance:    model.Instance{MCVersion: "1.20.1", Loader: "forge"},
		GameDir:     base,
		VersionJSON: &vjson,
		Account:     model.Account{Username: "Steve", UUID: "u", AccessToken: "t", Type: "mojang"},
	}
	jvm, game := jsonChainArgs(cfg, base, id, filepath.Join(base, "versions", id, "natives"), filepath.Join(base, "assets"), "1.20.1", 854, 480, "cp", true)
	j := strings.Join(jvm, " ")
	for _, want := range []string{"-p", "--add-modules", "ALL-MODULE-PATH", "-DlegacyClassPath.file=", "--add-opens", "cpw.mods.securejarhandler", "-DignoreList=", "bootstraplauncher-1.1.2.jar"} {
		if !strings.Contains(j, want) {
			t.Errorf("JVM не містить %q", want)
		}
	}
	if strings.Contains(j, "${") {
		t.Errorf("JVM містить нерозв'язаний placeholder: %s", j)
	}
	g := strings.Join(game, " ")
	if !strings.Contains(g, "--launchTarget forgeclient") || !strings.Contains(g, "--fml.forgeVersion") {
		t.Errorf("Game args неповні: %s", g)
	}
}

// TestRealNeoForgeProfile — те саме для РЕАЛЬНО встановленого профілю
// NeoForge 1.21.1 (neoforge-21.1.233). Критично: профіль має ДВА --add-opens
// і ДВА --add-exports — пари флаг/значення мають зберегтись, інакше java
// сприйме значення як головний клас (минулий баг).
func TestRealNeoForgeProfile(t *testing.T) {
	base := filepath.Join(os.Getenv("APPDATA"), ".shaurm", "minecraft")
	id := "neoforge-21.1.233"
	jsonPath := filepath.Join(base, "versions", id, id+".json")
	if _, err := os.Stat(jsonPath); err != nil {
		t.Skip("neoforge профіль не знайдено: " + jsonPath)
	}
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	var vjson VersionJSON
	if err := json.Unmarshal(data, &vjson); err != nil {
		t.Fatal(err)
	}
	cfg := LaunchConfig{
		Instance:    model.Instance{MCVersion: "1.21.1", Loader: "neoforge"},
		GameDir:     base,
		VersionJSON: &vjson,
		Account:     model.Account{Username: "Steve", UUID: "u", AccessToken: "t", Type: "mojang"},
	}
	jvm, game := jsonChainArgs(cfg, base, id, filepath.Join(base, "versions", id, "natives"), filepath.Join(base, "assets"), "1.21", 854, 480, "cp", true)
	j := strings.Join(jvm, " ")
	if strings.Contains(j, "${") {
		t.Errorf("JVM містить нерозв'язаний placeholder: %s", j)
	}
	// Кожне значення --add-opens/--add-exports має йти одразу ПІСЛЯ свого
	// флага: у зібраному потоці значення без флага = "Could not find main
	// class java.base.java.lang.invoke=cpw.mods.securejarhandler".
	pairs := []string{
		"--add-opens java.base/java.util.jar=cpw.mods.securejarhandler",
		"--add-opens java.base/java.lang.invoke=cpw.mods.securejarhandler",
		"--add-exports java.base/sun.security.util=cpw.mods.securejarhandler",
	}
	for _, p := range pairs {
		if !strings.Contains(j, p) {
			t.Errorf("JVM не містить пару %q: %s", p, j)
		}
	}
	for _, mustNot := range []string{"bootstraplauncher-2.0.2.jar", "-p", "--add-modules", "ALL-MODULE-PATH", "-DlegacyClassPath.file=", "-DlibraryDirectory="} {
		if !strings.Contains(j, mustNot) {
			t.Errorf("JVM не містить %q: %s", mustNot, j)
		}
	}
	g := strings.Join(game, " ")
	if !strings.Contains(g, "--launchTarget forgeclient") || !strings.Contains(g, "--fml.mcVersion 1.21.1") {
		t.Errorf("Game args неповні: %s", g)
	}
}
