package minecraft

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestResolveLibraryArtifactFabricFormat — meta.fabricmc.net віддає libraries
// БЕЗ downloads, лише name + url репозиторію. resolveLibraryArtifact має
// побудувати Path і URL через maven-координати (gradlePath), інакше
// fabric-loader.jar ніколи не качається → KnotClient не знайдено.
func TestResolveLibraryArtifactFabricFormat(t *testing.T) {
	cases := []struct {
		name     string
		url      string
		wantPath string
		wantURL  string
	}{
		{
			name:     "net.fabricmc:fabric-loader:0.16.9",
			url:      "https://maven.fabricmc.net/",
			wantPath: "net/fabricmc/fabric-loader/0.16.9/fabric-loader-0.16.9.jar",
			wantURL:  "https://maven.fabricmc.net/net/fabricmc/fabric-loader/0.16.9/fabric-loader-0.16.9.jar",
		},
		{
			name:     "org.ow2.asm:asm:9.7.1",
			url:      "https://maven.fabricmc.net/",
			wantPath: "org/ow2/asm/asm/9.7.1/asm-9.7.1.jar",
			wantURL:  "https://maven.fabricmc.net/org/ow2/asm/asm/9.7.1/asm-9.7.1.jar",
		},
		{
			// Класифікатор (4-й сегмент) — додається до імені jar.
			name:     "net.fabricmc:sponge-mixin:0.15.4+mixin.0.8.7",
			url:      "https://maven.fabricmc.net/",
			wantPath: "net/fabricmc/sponge-mixin/0.15.4+mixin.0.8.7/sponge-mixin-0.15.4+mixin.0.8.7.jar",
		},
		{
			// Без url — фолбек на Maven Central.
			name:     "org.slf4j:slf4j-api:2.0.9",
			url:      "",
			wantPath: "org/slf4j/slf4j-api/2.0.9/slf4j-api-2.0.9.jar",
			wantURL:  "https://repo1.maven.org/maven2/org/slf4j/slf4j-api/2.0.9/slf4j-api-2.0.9.jar",
		},
	}
	for _, c := range cases {
		lib := LibraryJSON{Name: c.name, URL: c.url}
		if !resolveLibraryArtifact(&lib) {
			t.Fatalf("%s: resolveLibraryArtifact = false (очікували true)", c.name)
		}
		if lib.Downloads.Artifact.Path != c.wantPath {
			t.Errorf("%s: Path = %q, очікували %q", c.name, lib.Downloads.Artifact.Path, c.wantPath)
		}
		if c.wantURL != "" && lib.Downloads.Artifact.URL != c.wantURL {
			t.Errorf("%s: URL = %q, очікували %q", c.name, lib.Downloads.Artifact.URL, c.wantURL)
		}
	}
}

// TestResolveLibraryArtifactAlreadyHasDownloads — бібліотека з повним
// downloads.artifact (ванилла/Forge формат) не змінюється.
func TestResolveLibraryArtifactAlreadyHasDownloads(t *testing.T) {
	lib := LibraryJSON{
		Name: "net.minecraft:client:1.20.5",
		Downloads: struct {
			Artifact    Artifact            `json:"artifact"`
			Classifiers map[string]Artifact `json:"classifiers"`
		}{
			Artifact: Artifact{Path: "net/minecraft/client/1.20.5/client-1.20.5.jar", URL: "https://piston-data.mojang.com/x"},
		},
	}
	if !resolveLibraryArtifact(&lib) {
		t.Fatal("resolveLibraryArtifact = false для бібліотеки з downloads")
	}
	if lib.Downloads.Artifact.Path != "net/minecraft/client/1.20.5/client-1.20.5.jar" {
		t.Errorf("Path змінився: %q", lib.Downloads.Artifact.Path)
	}
	if lib.Downloads.Artifact.URL != "https://piston-data.mojang.com/x" {
		t.Errorf("URL змінився: %q", lib.Downloads.Artifact.URL)
	}
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0755)
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func loadTestVersion(t *testing.T, path string) *VersionJSON {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var v VersionJSON
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return &v
}

func containsPath(cp, want string) bool {
	for _, p := range strings.Split(cp, string(os.PathListSeparator)) {
		if p == want {
			return true
		}
	}
	return false
}

// TestBuildClassPathIncludesResolvedLoaderJar — loader jar, який ensureLibraries
// записав у vjson.Libraries через maven-фолбек, має потрапити на класшлях.
func TestBuildClassPathIncludesResolvedLoaderJar(t *testing.T) {
	baseDir := t.TempDir()
	// Створюємо meta-файли ланцюга: loader наслідує ванилла.
	parentDir := filepath.Join(baseDir, "versions", "1.20.5")
	writeJSON(t, filepath.Join(parentDir, "1.20.5.json"), VersionJSON{
		ID:           "1.20.5",
		MainClass:    "net.minecraft.client.main.Main",
		InheritsFrom: "",
		Libraries: []LibraryJSON{{
			Name: "com.mojang:logging:1.0.0",
			Downloads: struct {
				Artifact    Artifact            `json:"artifact"`
				Classifiers map[string]Artifact `json:"classifiers"`
			}{
				Artifact: Artifact{Path: "com/mojang/logging/1.0.0/logging-1.0.0.jar", URL: "https://x"},
			},
		}},
	})
	loaderDir := filepath.Join(baseDir, "versions", "fabric-loader-0.16.9-1.20.5")
	writeJSON(t, filepath.Join(loaderDir, "fabric-loader-0.16.9-1.20.5.json"), VersionJSON{
		ID:           "fabric-loader-0.16.9-1.20.5",
		MainClass:    "net.fabricmc.loader.impl.launch.knot.KnotClient",
		InheritsFrom: "1.20.5",
		Libraries: []LibraryJSON{{
			Name: "net.fabricmc:fabric-loader:0.16.9",
			URL:  "https://maven.fabricmc.net/",
		}},
	})

	vjson := loadTestVersion(t, filepath.Join(loaderDir, "fabric-loader-0.16.9-1.20.5.json"))
	// Імітуємо те, що робить ensureLibraries: резолвимо артефакт у масиві.
	for i := range vjson.Libraries {
		resolveLibraryArtifact(&vjson.Libraries[i])
	}

	cp := BuildClassPath(baseDir, vjson, "fabric-loader-0.16.9-1.20.5")
	want := filepath.Join(baseDir, "libraries", "net", "fabricmc", "fabric-loader", "0.16.9", "fabric-loader-0.16.9.jar")
	if !containsPath(cp, want) {
		t.Errorf("класшлях не містить loader jar %q\nкласшлях: %s", want, cp)
	}
	// Батьківська бібліотека теж має бути присутня.
	parentLib := filepath.Join(baseDir, "libraries", "com", "mojang", "logging", "1.0.0", "logging-1.0.0.jar")
	if !containsPath(cp, parentLib) {
		t.Errorf("класшлях не містить батьківську бібліотеку %q", parentLib)
	}
}

// TestBuildClassPathDedupesLibraryAcrossChain -- регресійний тест на баг
// "IllegalStateException: Duplicate key ...gson-2.10.1.jar" при запуску
// Forge/NeoForge: якщо одна й та сама бібліотека (за maven-координатами
// groupId:artifactId) описана і у vanilla-батьківському version.json, і у
// forge-нащадку -- у classpath має потрапити РІВНО ОДИН шлях до jar.
func TestBuildClassPathDedupesLibraryAcrossChain(t *testing.T) {
	baseDir := t.TempDir()
	gsonArtifact := Artifact{Path: "com/google/code/gson/gson/2.10.1/gson-2.10.1.jar", URL: "https://x"}

	parentDir := filepath.Join(baseDir, "versions", "1.21.1")
	writeJSON(t, filepath.Join(parentDir, "1.21.1.json"), VersionJSON{
		ID:        "1.21.1",
		MainClass: "net.minecraft.client.main.Main",
		Libraries: []LibraryJSON{{
			Name: "com.google.code.gson:gson:2.10.1",
			Downloads: struct {
				Artifact    Artifact            `json:"artifact"`
				Classifiers map[string]Artifact `json:"classifiers"`
			}{Artifact: gsonArtifact},
		}},
	})

	loaderDir := filepath.Join(baseDir, "versions", "neoforge-21.1.100")
	writeJSON(t, filepath.Join(loaderDir, "neoforge-21.1.100.json"), VersionJSON{
		ID:           "neoforge-21.1.100",
		MainClass:    "cpw.mods.bootstraplauncher.BootstrapLauncher",
		InheritsFrom: "1.21.1",
		Libraries: []LibraryJSON{{
			Name: "com.google.code.gson:gson:2.10.1",
			Downloads: struct {
				Artifact    Artifact            `json:"artifact"`
				Classifiers map[string]Artifact `json:"classifiers"`
			}{Artifact: gsonArtifact},
		}, {
			Name: "cpw.mods:bootstraplauncher:2.0.2",
			Downloads: struct {
				Artifact    Artifact            `json:"artifact"`
				Classifiers map[string]Artifact `json:"classifiers"`
			}{Artifact: Artifact{Path: "cpw/mods/bootstraplauncher/2.0.2/bootstraplauncher-2.0.2.jar", URL: "https://x"}},
		}},
	})

	vjson := loadTestVersion(t, filepath.Join(loaderDir, "neoforge-21.1.100.json"))
	cp := BuildClassPath(baseDir, vjson, "neoforge-21.1.100")

	gsonPath := filepath.Join(baseDir, "libraries", "com", "google", "code", "gson", "gson", "2.10.1", "gson-2.10.1.jar")
	count := 0
	for _, p := range strings.Split(cp, string(os.PathListSeparator)) {
		if p == gsonPath {
			count++
		}
	}
	if count != 1 {
		t.Errorf("gson.jar зустрічається в classpath %d раз(и), очікували 1\nкласшлях: %s", count, cp)
	}

	bootstrapPath := filepath.Join(baseDir, "libraries", "cpw", "mods", "bootstraplauncher", "2.0.2", "bootstraplauncher-2.0.2.jar")
	if !containsPath(cp, bootstrapPath) {
		t.Errorf("класшлях не містить bootstraplauncher jar %q", bootstrapPath)
	}
}

// TestBuildClassPathKeepsNewerLibraryVersion -- якщо батько й нащадок
// вказують РІЗНІ версії однієї бібліотеки, на класшлях має потрапити
// новіша версія, а не обидві.
func TestBuildClassPathKeepsNewerLibraryVersion(t *testing.T) {
	baseDir := t.TempDir()

	parentDir := filepath.Join(baseDir, "versions", "1.21.1")
	writeJSON(t, filepath.Join(parentDir, "1.21.1.json"), VersionJSON{
		ID:        "1.21.1",
		MainClass: "net.minecraft.client.main.Main",
		Libraries: []LibraryJSON{{
			Name: "org.ow2.asm:asm:9.6",
			Downloads: struct {
				Artifact    Artifact            `json:"artifact"`
				Classifiers map[string]Artifact `json:"classifiers"`
			}{Artifact: Artifact{Path: "org/ow2/asm/asm/9.6/asm-9.6.jar", URL: "https://x"}},
		}},
	})

	loaderDir := filepath.Join(baseDir, "versions", "forge-1.21.1-52.0.0")
	writeJSON(t, filepath.Join(loaderDir, "forge-1.21.1-52.0.0.json"), VersionJSON{
		ID:           "forge-1.21.1-52.0.0",
		MainClass:    "cpw.mods.bootstraplauncher.BootstrapLauncher",
		InheritsFrom: "1.21.1",
		Libraries: []LibraryJSON{{
			Name: "org.ow2.asm:asm:9.7.1",
			Downloads: struct {
				Artifact    Artifact            `json:"artifact"`
				Classifiers map[string]Artifact `json:"classifiers"`
			}{Artifact: Artifact{Path: "org/ow2/asm/asm/9.7.1/asm-9.7.1.jar", URL: "https://x"}},
		}},
	})

	vjson := loadTestVersion(t, filepath.Join(loaderDir, "forge-1.21.1-52.0.0.json"))
	cp := BuildClassPath(baseDir, vjson, "forge-1.21.1-52.0.0")

	newer := filepath.Join(baseDir, "libraries", "org", "ow2", "asm", "asm", "9.7.1", "asm-9.7.1.jar")
	older := filepath.Join(baseDir, "libraries", "org", "ow2", "asm", "asm", "9.6", "asm-9.6.jar")
	if !containsPath(cp, newer) {
		t.Errorf("класшлях не містить новішу asm 9.7.1: %s", cp)
	}
	if containsPath(cp, older) {
		t.Errorf("класшлях містить застарілу asm 9.6, хоча мала лишитись лише новіша: %s", cp)
	}
}

// TestBuildClassPathOldFormatNatives -- регресійний тест на баг, що ламав
// запуск Forge/NeoForge 1.20.1-: ОЛД-формат version.json декларує кожен
// natives-classifier ОКРЕМИМ записом ("org.lwjgl:lwjgl:3.3.1:natives-windows"),
// а не через downloads.classifiers. Колишній дедуп за "group:artifact" стирав
// основний jar (module org.lwjgl) на користь останнього natives-варіанту —
// класшлях лишався без lwjgl-3.3.1.jar, і BootstrapLauncher падав з
// "Module org.lwjgl not found, required by org.lwjgl.natives". Тепер:
//   · основний jar обов'язково на класшляху;
//   · на класшляху РІВНО ОДИН natives-джар — той, що відповідає поточній
//     платформі (всі natives декларують module org.lwjgl.natives, тож кілька
//     одразу = "duplicate module").
func TestBuildClassPathOldFormatNatives(t *testing.T) {
	baseDir := t.TempDir()

	lib := func(name, path string) LibraryJSON {
		return LibraryJSON{
			Name: name,
			Downloads: struct {
				Artifact    Artifact            `json:"artifact"`
				Classifiers map[string]Artifact `json:"classifiers"`
			}{Artifact: Artifact{Path: path, URL: "https://x"}},
		}
	}

	parentDir := filepath.Join(baseDir, "versions", "1.20.1")
	writeJSON(t, filepath.Join(parentDir, "1.20.1.json"), VersionJSON{
		ID:        "1.20.1",
		MainClass: "net.minecraft.client.main.Main",
		Libraries: []LibraryJSON{
			lib("org.lwjgl:lwjgl:3.3.1", "org/lwjgl/lwjgl/3.3.1/lwjgl-3.3.1.jar"),
			lib("org.lwjgl:lwjgl:3.3.1:natives-linux", "org/lwjgl/lwjgl/3.3.1/lwjgl-3.3.1-natives-linux.jar"),
			lib("org.lwjgl:lwjgl:3.3.1:natives-osx", "org/lwjgl/lwjgl/3.3.1/lwjgl-3.3.1-natives-osx.jar"),
			lib("org.lwjgl:lwjgl:3.3.1:natives-windows", "org/lwjgl/lwjgl/3.3.1/lwjgl-3.3.1-natives-windows.jar"),
			lib("org.lwjgl:lwjgl:3.3.1:natives-windows-arm64", "org/lwjgl/lwjgl/3.3.1/lwjgl-3.3.1-natives-windows-arm64.jar"),
			lib("org.lwjgl:lwjgl:3.3.1:natives-windows-x86", "org/lwjgl/lwjgl/3.3.1/lwjgl-3.3.1-natives-windows-x86.jar"),
		},
	})

	loaderDir := filepath.Join(baseDir, "versions", "1.20.1-forge-47.4.10")
	writeJSON(t, filepath.Join(loaderDir, "1.20.1-forge-47.4.10.json"), VersionJSON{
		ID:           "1.20.1-forge-47.4.10",
		MainClass:    "cpw.mods.bootstraplauncher.BootstrapLauncher",
		InheritsFrom: "1.20.1",
	})

	vjson := loadTestVersion(t, filepath.Join(loaderDir, "1.20.1-forge-47.4.10.json"))
	cp := BuildClassPath(baseDir, vjson, "1.20.1-forge-47.4.10")

	mainJar := filepath.Join(baseDir, "libraries", "org", "lwjgl", "lwjgl", "3.3.1", "lwjgl-3.3.1.jar")
	if !containsPath(cp, mainJar) {
		t.Errorf("класшлях не містить основний lwjgl-3.3.1.jar (module org.lwjgl): %s", cp)
	}

	// Чужі платформи (natives-linux/natives-osx) не мають потрапляти на
	// класшлях на жодній платформі.
	for _, other := range []string{"natives-linux", "natives-osx"} {
		bad := filepath.Join(baseDir, "libraries", "org", "lwjgl", "lwjgl", "3.3.1", "lwjgl-3.3.1-"+other+".jar")
		if containsPath(cp, bad) {
			t.Errorf("класшлях містить чужий natives %s: %s", other, cp)
		}
	}

	// На класшляху має бути РІВНО ОДИН natives-джар поточної платформи.
	var nativeJars int
	for _, p := range strings.Split(cp, string(os.PathListSeparator)) {
		if strings.Contains(p, "lwjgl-3.3.1-natives-") {
			nativeJars++
		}
	}
	if nativeJars != 1 {
		t.Errorf("на класшляху %d natives-джарів (очікували рівно 1), класшлях: %s", nativeJars, cp)
	}
}

// TestLibraryKeyIncludesClassifier -- дедуп-ключ бібліотеки має розрізняти
// основний jar і його classifier-варіанти ОЛД-формату.
func TestLibraryKeyIncludesClassifier(t *testing.T) {
	cases := map[string]string{
		"org.lwjgl:lwjgl:3.3.1":                              "org.lwjgl:lwjgl",
		"org.lwjgl:lwjgl:3.3.1:natives-windows":              "org.lwjgl:lwjgl:natives-windows",
		"org.lwjgl:lwjgl:3.3.1:natives-windows-x86":          "org.lwjgl:lwjgl:natives-windows-x86",
		"com.google.code.gson:gson:2.10.1":                   "com.google.code.gson:gson",
		"org.ow2.asm:asm:9.7.1:fatjar":                       "org.ow2.asm:asm:fatjar",
	}
	for name, want := range cases {
		if got := libraryKey(name); got != want {
			t.Errorf("libraryKey(%q) = %q, want %q", name, got, want)
		}
	}
}

// TestBuildClassPathKeepsNonNativesClassifier -- регресійний тест на баг, що
// ламав запуск Forge 1.20.1: фільтр ОЛД-форматних natives викидав з класшляху
// БУДЬ-ЯКУ бібліотеку з classifier, а не лише "natives-*". Зокрема
// "net.minecraftforge:mergetool:1.1.5:api" (classifier "api") — єдиний джар,
// що несе net.minecraftforge.api.distmarker.OnlyIn. Без нього ModLauncher
// падає "ClassNotFoundException: ...OnlyIn" → RuntimeDistCleaner не
// вантажиться → "InvalidLauncherSetupException: Invalid Services found fml"
// / "Dist Cleaner is missing, we need this to run". Тепер:
//   · класифікатори типу "api" обов'язково лишаються на класшляху;
//   · natives-фільтрація чужих платформ, як і раніше, працює.
func TestBuildClassPathKeepsNonNativesClassifier(t *testing.T) {
	baseDir := t.TempDir()

	lib := func(name, path string) LibraryJSON {
		return LibraryJSON{
			Name: name,
			Downloads: struct {
				Artifact    Artifact            `json:"artifact"`
				Classifiers map[string]Artifact `json:"classifiers"`
			}{Artifact: Artifact{Path: path, URL: "https://x"}},
		}
	}

	parentDir := filepath.Join(baseDir, "versions", "1.20.1")
	writeJSON(t, filepath.Join(parentDir, "1.20.1.json"), VersionJSON{
		ID:        "1.20.1",
		MainClass: "net.minecraft.client.main.Main",
		Libraries: []LibraryJSON{
			lib("org.lwjgl:lwjgl:3.3.1", "org/lwjgl/lwjgl/3.3.1/lwjgl-3.3.1.jar"),
			lib("org.lwjgl:lwjgl:3.3.1:natives-linux", "org/lwjgl/lwjgl/3.3.1/lwjgl-3.3.1-natives-linux.jar"),
			lib("org.lwjgl:lwjgl:3.3.1:natives-windows", "org/lwjgl/lwjgl/3.3.1/lwjgl-3.3.1-natives-windows.jar"),
		},
	})

	loaderDir := filepath.Join(baseDir, "versions", "1.20.1-forge-47.4.10")
	writeJSON(t, filepath.Join(loaderDir, "1.20.1-forge-47.4.10.json"), VersionJSON{
		ID:           "1.20.1-forge-47.4.10",
		MainClass:    "cpw.mods.bootstraplauncher.BootstrapLauncher",
		InheritsFrom: "1.20.1",
		Libraries: []LibraryJSON{
			lib("net.minecraftforge:mergetool:1.1.5:api", "net/minecraftforge/mergetool/1.1.5/mergetool-1.1.5-api.jar"),
			lib("net.minecraftforge:fmlloader:1.20.1-47.4.10", "net/minecraftforge/fmlloader/1.20.1-47.4.10/fmlloader-1.20.1-47.4.10.jar"),
		},
	})

	vjson := loadTestVersion(t, filepath.Join(loaderDir, "1.20.1-forge-47.4.10.json"))
	cp := BuildClassPath(baseDir, vjson, "1.20.1-forge-47.4.10")

	// mergetool-api.jar — не natives, має БУТИ на класшляху (там OnlyIn).
	apiJar := filepath.Join(baseDir, "libraries", "net", "minecraftforge", "mergetool", "1.1.5", "mergetool-1.1.5-api.jar")
	if !containsPath(cp, apiJar) {
		t.Errorf("класшлях не містить mergetool-1.1.5-api.jar (net.minecraftforge.api.distmarker.OnlyIn): %s", cp)
	}

	// Чужий natives (natives-linux) все ще не має потрапляти на класшлях.
	bad := filepath.Join(baseDir, "libraries", "org", "lwjgl", "lwjgl", "3.3.1", "lwjgl-3.3.1-natives-linux.jar")
	if containsPath(cp, bad) {
		t.Errorf("класшлях містить чужий natives-linux: %s", cp)
	}
}

// TestNativesClassifierMatches -- natives-classifier поточної платформи має
// збігатись, а всі решта — ні (щоб на класшлях не потрапляли чужі платформи).
func TestNativesClassifierMatches(t *testing.T) {
	base := nativesBaseKey()
	if base == "" {
		t.Skip("непідтримувана ОС для natives")
	}
	if !nativesClassifierMatches(base) {
		t.Errorf("основний natives %q має збігатись з поточною платформою", base)
	}
	// На кожній ОС хоч чужі платформи, хоч чужа архітектура — не збігаються.
	others := map[string]bool{"natives-linux": true, "natives-osx": true, "natives-windows": true, "natives-windows-x86": true, "natives-windows-arm64": true}
	delete(others, base)
	switch runtime.GOARCH {
	case "386":
		delete(others, base+"-x86")
	case "arm64":
		delete(others, base+"-arm64")
	}
	for cl := range others {
		if nativesClassifierMatches(cl) {
			t.Errorf("чужий natives %q не має збігатись з поточною платформою", cl)
		}
	}
}

// writeForgeChain створює тестовий ланцюг Forge 1.20.1: ванилла-батько з
// jar на диску і лоадер-профіль без власного jar (як у реального
// install_profile — downloads.client порожній).
func writeForgeChain(t *testing.T, baseDir string) *VersionJSON {
	t.Helper()
	parentDir := filepath.Join(baseDir, "versions", "1.20.1")
	writeJSON(t, filepath.Join(parentDir, "1.20.1.json"), VersionJSON{
		ID:        "1.20.1",
		MainClass: "net.minecraft.client.main.Main",
	})
	vanillaJar := filepath.Join(parentDir, "1.20.1.jar")
	if err := os.WriteFile(vanillaJar, []byte("vanilla-client-content"), 0644); err != nil {
		t.Fatalf("write vanilla jar: %v", err)
	}
	loaderDir := filepath.Join(baseDir, "versions", "1.20.1-forge-47.4.10")
	writeJSON(t, filepath.Join(loaderDir, "1.20.1-forge-47.4.10.json"), VersionJSON{
		ID:           "1.20.1-forge-47.4.10",
		MainClass:    "cpw.mods.bootstraplauncher.BootstrapLauncher",
		InheritsFrom: "1.20.1",
	})
	return loadTestVersion(t, filepath.Join(loaderDir, "1.20.1-forge-47.4.10.json"))
}

// TestEnsureLoaderClientJarCopiesVanillaJar -- регресійний тест на
// "java.lang.module.ResolutionException: Modules minecraft and _1._20._1
// export package net.minecraft.data": у Forge 1.20.1 install версійний jar
// versions/<loader-id>/<loader-id>.jar не створюється (downloads.client
// порожній), тому на класшлях падав ванилла versions/1.20.1/1.20.1.jar.
// bootstraplauncher не знаходить його в -DignoreList (там
// ${version_name}.jar = 1.20.1-forge-47.4.10.jar) і робить з нього
// автоматичний модуль _1._20._1 → split-package з модулем гри minecraft.
// ensureLoaderClientJar має наповнити лоадер-jar копією клієнта ванилла.
func TestEnsureLoaderClientJarCopiesVanillaJar(t *testing.T) {
	baseDir := t.TempDir()
	vjson := writeForgeChain(t, baseDir)

	ensureLoaderClientJar(baseDir, vjson)

	loaderJar := filepath.Join(baseDir, "versions", "1.20.1-forge-47.4.10", "1.20.1-forge-47.4.10.jar")
	data, err := os.ReadFile(loaderJar)
	if err != nil {
		t.Fatalf("лоадер-jar не створено: %v", err)
	}
	if string(data) != "vanilla-client-content" {
		t.Errorf("лоадер-jar не є копією ванилла: %q", data)
	}
}

// TestEnsureLoaderClientJarKeepsExistingJar -- якщо jar версії-лоадера вже
// існує (напр. NeoForge качає його через downloads.client), не перезаписуємо.
func TestEnsureLoaderClientJarKeepsExistingJar(t *testing.T) {
	baseDir := t.TempDir()
	vjson := writeForgeChain(t, baseDir)
	loaderJar := filepath.Join(baseDir, "versions", "1.20.1-forge-47.4.10", "1.20.1-forge-47.4.10.jar")
	os.MkdirAll(filepath.Dir(loaderJar), 0755)
	if err := os.WriteFile(loaderJar, []byte("existing"), 0644); err != nil {
		t.Fatalf("write loader jar: %v", err)
	}

	ensureLoaderClientJar(baseDir, vjson)

	data, _ := os.ReadFile(loaderJar)
	if string(data) != "existing" {
		t.Errorf("існуючий лоадер-jar перезаписано: %q", data)
	}
}

// TestEnsureLoaderClientJarSkipsVanilla -- для ванилла-версії (без
// InheritsFrom) ensureLoaderClientJar — no-op.
func TestEnsureLoaderClientJarSkipsVanilla(t *testing.T) {
	baseDir := t.TempDir()
	vjson := &VersionJSON{ID: "1.20.1", MainClass: "net.minecraft.client.main.Main"}

	ensureLoaderClientJar(baseDir, vjson)

	if _, err := os.Stat(filepath.Join(baseDir, "versions", "1.20.1", "1.20.1.jar")); !os.IsNotExist(err) {
		t.Errorf("ванилла-jar не мав створюватись ensureLoaderClientJar")
	}
}

// TestBuildClassPathUsesLoaderJarForForge -- коли лоадер-jar існує (після
// ensureLoaderClientJar), BuildClassPath кладе НА КЛАСШЛЯХ саме його
// (versions/<loader-id>/<loader-id>.jar — збігається з ${version_name}.jar
// у -DignoreList), а не ванилла versions/1.20.1/1.20.1.jar, який ставав
// конфліктним автоматичним модулем _1._20._1.
func TestBuildClassPathUsesLoaderJarForForge(t *testing.T) {
	baseDir := t.TempDir()
	vjson := writeForgeChain(t, baseDir)

	// Те саме, що робить launcher.go для Forge-бутстрапу.
	ensureLoaderClientJar(baseDir, vjson)
	cp := BuildClassPath(baseDir, vjson, "1.20.1-forge-47.4.10")

	loaderJar := filepath.Join(baseDir, "versions", "1.20.1-forge-47.4.10", "1.20.1-forge-47.4.10.jar")
	vanillaJar := filepath.Join(baseDir, "versions", "1.20.1", "1.20.1.jar")
	if !containsPath(cp, loaderJar) {
		t.Errorf("класшлях не містить лоадер-jar %q: %s", loaderJar, cp)
	}
	if containsPath(cp, vanillaJar) {
		t.Errorf("класшлях містить ванилла-jar %q (він стає модулем _1._20._1): %s", vanillaJar, cp)
	}
}
