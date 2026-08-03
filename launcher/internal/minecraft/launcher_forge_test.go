package minecraft

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"shaurma-launcher-wails/internal/model"
)

// TestForgeJsonArgs — запуск Forge 1.20.1 вимагає повного набору аргументів
// з version.json (модульний шлях -p, --add-modules ALL-MODULE-PATH,
// --add-opens, -DlegacyClassPath.file, --launchTarget/--fml.*). Перевіряємо,
// що jsonChainArgs їх збирає, підставляє placeholders, а аргументи, які
// лаунчер передає сам (RAM, нативки, --username), і нерозв'язані
// placeholders — пропускає.
func TestForgeJsonArgs(t *testing.T) {
	vjson := &VersionJSON{ID: "1.20.1-forge-47.4.10"}
	vjson.Arguments.JVM = []interface{}{
		"-DignoreList=bootstraplauncher,securejarhandler,${version_name}.jar",
		"-DlibraryDirectory=${library_directory}",
		"-p",
		"${library_directory}/cpw/mods/bootstraplauncher/1.1.2/bootstraplauncher-1.1.2.jar${classpath_separator}${library_directory}/cpw/mods/securejarhandler/2.1.10/securejarhandler-2.1.10.jar",
		"--add-modules", "ALL-MODULE-PATH",
		"--add-opens", "java.base/java.lang.invoke=cpw.mods.securejarhandler",
		"-Djava.net.preferIPv6Addresses=system",
		"-Dlog4j.configurationFile=${path_argument}", // нерозв'язаний → пропускаємо
		"-Djava.library.path=${natives_directory}",   // передає лаунчер → пропускаємо
		"-Xmx4G",                                     // передає лаунчер → пропускаємо
	}
	vjson.Arguments.Game = []interface{}{
		"--launchTarget", "forgeclient",
		"--fml.forgeVersion", "47.4.10",
		"--fml.mcVersion", "1.20.1",
		"--username", "${auth_player_name}", // передає лаунчер → пропускаємо
	}

	mcBase := t.TempDir()
	cfg := LaunchConfig{
		Instance:     model.Instance{MCVersion: "1.20.1"},
		GameDir:      t.TempDir(),
		VersionJSON:  vjson,
		WindowWidth:  854,
		WindowHeight: 480,
		Account:      model.Account{Username: "Steve", UUID: "u", AccessToken: "t", Type: "mojang"},
	}
	jvm, game := jsonChainArgs(cfg, mcBase, "1.20.1-forge-47.4.10", t.TempDir(), t.TempDir(), "5", 854, 480, "cp1"+string(os.PathListSeparator)+"cp2", true)

	if !chainUsesBootstrap(versionChain(vjson, mcBase)) {
		t.Error("1.20.1 Forge має бути визначений як новий тип (bootstrap)")
	}

	joined := strings.Join(jvm, " ")
	for _, want := range []string{
		"-DlegacyClassPath.file=",
		"--add-modules",
		"ALL-MODULE-PATH",
		"-p",
		"java.base/java.lang.invoke=cpw.mods.securejarhandler",
		"-DignoreList=bootstraplauncher,securejarhandler,1.20.1-forge-47.4.10.jar",
		"-DlibraryDirectory=",
		"-Djava.net.preferIPv6Addresses=system",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("JVM args не містять %q: %s", want, joined)
		}
	}
	for _, bad := range []string{"${", "-Xmx", "-Djava.library.path=", "log4j.configurationFile"} {
		if strings.Contains(joined, bad) {
			t.Errorf("JVM args не мають містити %q: %s", bad, joined)
		}
	}

	gameJoined := strings.Join(game, " ")
	if !strings.Contains(gameJoined, "--launchTarget") || !strings.Contains(gameJoined, "forgeclient") ||
		!strings.Contains(gameJoined, "--fml.forgeVersion") || !strings.Contains(gameJoined, "47.4.10") {
		t.Errorf("Game args не містять bootstrap-аргументи: %s", gameJoined)
	}
	if strings.Contains(gameJoined, "--username") || strings.Contains(gameJoined, "${") {
		t.Errorf("Game args не мають містити --username/${: %s", gameJoined)
	}

	// Файл класшляху має бути записаний поруч із версією.
	cpFile := filepath.Join(mcBase, "versions", "1.20.1-forge-47.4.10", "1.20.1-forge-47.4.10-classpath.txt")
	if _, err := os.Stat(cpFile); err != nil {
		t.Errorf("classpath file не записано: %s (%v)", cpFile, err)
	}
}

// TestLegacyForgeArgs — Forge 1.16.5 (Java 8, cpw.mods.modlauncher): у json
// НЕМАЄ -p/--add-modules/--add-opens, тож ланцюг не має вважатись bootstrap,
// але -DlegacyClassPath.file, -DignoreList і --launchTarget/--fml.* мають
// збиратись як завжди.
func TestLegacyForgeArgs(t *testing.T) {
	vjson := &VersionJSON{ID: "1.16.5-forge-36.2.39"}
	vjson.Arguments.JVM = []interface{}{
		"-DignoreList=client,launcher,${version_name}.jar",
		"-DlibraryDirectory=${library_directory}",
		"-Dfml.ignoreInvalidMinecraftCertificates=true",
		"-DnativesDirectory=${natives_directory}",
		"-Dlog4j.configurationFile=${path_argument}", // нерозв'язаний → пропускаємо
	}
	vjson.Arguments.Game = []interface{}{
		"--launchTarget", "forgeclient",
		"--fml.forgeVersion", "36.2.39",
		"--fml.mcVersion", "1.16.5",
	}

	mcBase := t.TempDir()
	cfg := LaunchConfig{
		Instance:    model.Instance{MCVersion: "1.16.5"},
		GameDir:     t.TempDir(),
		VersionJSON: vjson,
		Account:     model.Account{Username: "Steve", UUID: "u", AccessToken: "t", Type: "mojang"},
	}

	chain := versionChain(vjson, mcBase)
	if chainUsesBootstrap(chain) {
		t.Error("1.16.5 Forge не має вважатись bootstrap-типом (немає -p/--add-modules)")
	}

	jvm, game := jsonChainArgs(cfg, mcBase, "1.16.5-forge-36.2.39", t.TempDir(), t.TempDir(), "1.16", 854, 480, "cp1", true)
	joined := strings.Join(jvm, " ")
	for _, want := range []string{"-DlegacyClassPath.file=", "-DignoreList=client,launcher,1.16.5-forge-36.2.39.jar", "-Dfml.ignoreInvalidMinecraftCertificates=true", "-DnativesDirectory="} {
		if !strings.Contains(joined, want) {
			t.Errorf("JVM args не містять %q: %s", want, joined)
		}
	}
	if strings.Contains(joined, "--add-modules") || strings.Contains(joined, " -p ") || strings.Contains(joined, "${") {
		t.Errorf("JVM args не мають містити модульні прапорці/плейсхолдери: %s", joined)
	}

	g := strings.Join(game, " ")
	if !strings.Contains(g, "--launchTarget forgeclient") || !strings.Contains(g, "--fml.mcVersion 1.16.5") {
		t.Errorf("Game args не містять modlauncher-аргументи: %s", g)
	}
}

// TestClientIdXuidPair — пропущені лаунчером флаги (--username тощо) не мають
// «проковтувати» наступний флаг як значення: --clientId/${clientid} і
// --xuid/${auth_xuid} прибираються повністю, без сирого "" у потоці.
func TestClientIdXuidPair(t *testing.T) {
	vjson := &VersionJSON{ID: "26.2"}
	vjson.Arguments.Game = []interface{}{
		"--username", "${auth_player_name}",
		"--clientId", "${clientid}",
		"--xuid", "${auth_xuid}",
		"--versionType", "${version_type}",
		"--launchTarget", "forgeclient",
	}
	cfg := LaunchConfig{
		Instance:    model.Instance{MCVersion: "1.20.6"},
		GameDir:     t.TempDir(),
		VersionJSON: vjson,
		Account:     model.Account{Username: "Steve", UUID: "u", AccessToken: "t", Type: "microsoft"},
	}

	_, game := jsonChainArgs(cfg, t.TempDir(), "26.2", t.TempDir(), t.TempDir(), "26", 854, 480, "cp1", false)
	g := strings.Join(game, " ")
	if strings.Contains(g, "--clientId") || strings.Contains(g, "--xuid") || strings.Contains(g, "--username") || strings.Contains(g, "--versionType") || strings.Contains(g, "${") {
		t.Errorf("Game args не мають містити керовані флаги: %s", g)
	}
	if !strings.Contains(g, "--launchTarget forgeclient") {
		t.Errorf("Game args мають зберегти --launchTarget: %s", g)
	}
}

// TestDuplicateFlagPairs — NeoForge має ДВА однакові --add-opens і ДВА
// --add-exports. Дедуп ЗА ПАРОЮ флаг/значення має зберегти обидві пари
// (різні значення) з ПРАВИЛЬНОЮ послідовністю, а не прибрати флаг і
// лишити його значення сиротою — інакше java сприйняв би значення як
// ім'я головного класу ("Could not find or load main class
// java.base.java.lang.invoke=cpw.mods.securejarhandler").
func TestDuplicateFlagPairs(t *testing.T) {
	vjson := &VersionJSON{ID: "neoforge-21.1.233"}
	vjson.Arguments.JVM = []interface{}{
		"-p",
		"${library_directory}/cpw/mods/bootstraplauncher/2.0.2/bootstraplauncher-2.0.2.jar",
		"--add-modules", "ALL-MODULE-PATH",
		"--add-opens", "java.base/java.util.jar=cpw.mods.securejarhandler",
		"--add-opens", "java.base/java.lang.invoke=cpw.mods.securejarhandler",
		"--add-exports", "java.base/sun.security.util=cpw.mods.securejarhandler",
		"--add-exports", "jdk.naming.dns/com.sun.jndi.dns=java.naming",
	}
	cfg := LaunchConfig{
		Instance:    model.Instance{MCVersion: "1.21.1", Loader: "neoforge"},
		GameDir:     t.TempDir(),
		VersionJSON: vjson,
		Account:     model.Account{Username: "Steve", UUID: "u", AccessToken: "t", Type: "mojang"},
	}
	jvm, _ := jsonChainArgs(cfg, t.TempDir(), "neoforge-21.1.233", t.TempDir(), t.TempDir(), "1.21", 854, 480, "cp1", true)

	// Пари флаг/значення мають йти ПОСЛІДОВНО — інакше java сприйме
	// значення без флага як ім'я головного класу.
	want := strings.Join([]string{
		"--add-modules", "ALL-MODULE-PATH",
		"--add-opens", "java.base/java.util.jar=cpw.mods.securejarhandler",
		"--add-opens", "java.base/java.lang.invoke=cpw.mods.securejarhandler",
		"--add-exports", "java.base/sun.security.util=cpw.mods.securejarhandler",
		"--add-exports", "jdk.naming.dns/com.sun.jndi.dns=java.naming",
	}, " ")
	if !strings.Contains(strings.Join(jvm, " "), want) {
		t.Errorf("JVM пари флаг/значення порушені: %s", strings.Join(jvm, " "))
	}
	if strings.Contains(strings.Join(jvm, " "), "${") {
		t.Errorf("JVM містить нерозв'язаний placeholder")
	}
}

// TestRulesArgs — правила version.json {rules:[{os:{name:...}}], value:...}
// мають оцінюватись проти поточної ОС: windows-правило (HeapDumpPath)
// потрапляє у JVM на Windows, osx/x86-правила — пропускаються, features-
// правила (--demo тощо) — пропускаються цілком.
func TestRulesArgs(t *testing.T) {
	vjson := &VersionJSON{ID: "26.2"}
	vjson.Arguments.JVM = []interface{}{
		map[string]interface{}{
			"rules": []interface{}{map[string]interface{}{"action": "allow", "os": map[string]interface{}{"name": "windows"}}},
			"value": "-XX:HeapDumpPath=MojangTricksIntelDriversForPerformance_javaw.exe_minecraft.exe.heapdump",
		},
		map[string]interface{}{
			"rules": []interface{}{map[string]interface{}{"action": "allow", "os": map[string]interface{}{"name": "osx"}}},
			"value": "-XstartOnFirstThread",
		},
		map[string]interface{}{
			"rules": []interface{}{map[string]interface{}{"action": "allow", "arch": "x86"}},
			"value": "-Xss1M",
		},
		map[string]interface{}{
			"rules": []interface{}{map[string]interface{}{"action": "allow", "features": map[string]interface{}{"is_demo_user": true}}},
			"value": []interface{}{"--demo"},
		},
		"-Dplain=1",
	}
	cfg := LaunchConfig{
		Instance:    model.Instance{MCVersion: "1.20.6"},
		GameDir:     t.TempDir(),
		VersionJSON: vjson,
		Account:     model.Account{Username: "Steve", UUID: "u", AccessToken: "t", Type: "mojang"},
	}
	jvm, _ := jsonChainArgs(cfg, t.TempDir(), "26.2", t.TempDir(), t.TempDir(), "26", 854, 480, "cp1", false)
	j := strings.Join(jvm, " ")
	if !strings.Contains(j, "-Dplain=1") {
		t.Errorf("JVM має містити простий рядок: %s", j)
	}
	if runtime.GOOS == "windows" {
		if !strings.Contains(j, "HeapDumpPath=") {
			t.Errorf("JVM має містити windows HeapDumpPath: %s", j)
		}
	} else if strings.Contains(j, "HeapDumpPath=") {
		t.Errorf("JVM не має містити windows HeapDumpPath на не-Windows: %s", j)
	}
	for _, bad := range []string{"-XstartOnFirstThread", "-Xss1M", "--demo"} {
		if strings.Contains(j, bad) {
			t.Errorf("JVM не має містити %q: %s", bad, j)
		}
	}
}

// TestFabricEmuArg — для Fabric json містить -DFabricMcEmu=..., і цей аргумент
// МАЄ потрапити у JVM (раніше json-аргументи збирались лише для forge).
// Файл класшляху для не-forge лоадерів не пишемо.
func TestFabricEmuArg(t *testing.T) {
	vjson := &VersionJSON{ID: "fabric-loader-0.19.1-1.20.1"}
	vjson.Arguments.JVM = []interface{}{
		"-DFabricMcEmu= net.minecraft.client.main.Main ",
	}
	vjson.MainClass = "net.fabricmc.loader.impl.launch.knot.KnotClient"
	cfg := LaunchConfig{
		Instance:    model.Instance{MCVersion: "1.20.1", Loader: "fabric"},
		GameDir:     t.TempDir(),
		VersionJSON: vjson,
		Account:     model.Account{Username: "Steve", UUID: "u", AccessToken: "t", Type: "mojang"},
	}

	mcBase := t.TempDir()
	jvm, _ := jsonChainArgs(cfg, mcBase, "fabric-loader-0.19.1-1.20.1", t.TempDir(), t.TempDir(), "1.20", 854, 480, "cp1", false)
	joined := strings.Join(jvm, " ")
	if !strings.Contains(joined, "-DFabricMcEmu=") {
		t.Errorf("JVM args мають містити -DFabricMcEmu: %s", joined)
	}

	// Файл класшляху не пишемо (не forge).
	if _, err := os.Stat(filepath.Join(mcBase, "versions", "fabric-loader-0.19.1-1.20.1", "fabric-loader-0.19.1-1.20.1-classpath.txt")); err == nil {
		t.Error("для fabric не має бути classpath-файла")
	}
}
