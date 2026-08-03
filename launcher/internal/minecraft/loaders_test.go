package minecraft

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGradlePath(t *testing.T) {
	cases := map[string]string{
		"net.minecraftforge:forge:1.20.1-47.2.0": "net/minecraftforge/forge/1.20.1-47.2.0/forge-1.20.1-47.2.0.jar",
		"net.neoforged:neoforge:21.1.95":         "net/neoforged/neoforge/21.1.95/neoforge-21.1.95.jar",
		"net.minecraft:client:1.20.1":            "net/minecraft/client/1.20.1/client-1.20.1.jar",
		"cpw.mods:securejarhandler:2.1.44":       "cpw/mods/securejarhandler/2.1.44/securejarhandler-2.1.44.jar",
	}
	for in, want := range cases {
		if got := gradlePath(in); got != want {
			t.Errorf("gradlePath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGradlePathClassifier(t *testing.T) {
	got := gradlePath("net.neoforged:neoforge:21.1.95:universal")
	want := "net/neoforged/neoforge/21.1.95/neoforge-21.1.95-universal.jar"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestGradlePathExtensionOverride -- регресійний тест на баг, який ламав
// встановлення NeoForge: "@zip" у "group:artifact:version@zip" -- це
// стандартна maven-нотація "override розширення файлу" (Gradle/Maven:
// group:artifact:version[:classifier][@extension]), а НЕ частина рядка
// версії. До фіксу "@zip" лишався приліпленим до version і псував і шлях
// каталогу, і саме розширення файлу (виходило .jar замість .zip), через що
// installertools отримував "Input does not exist" -- шукав .jar там, де
// на диску лежав .zip з версією без "@zip" у назві директорії.
func TestGradlePathExtensionOverride(t *testing.T) {
	got := gradlePath("net.neoforged:neoform:25w14craftmine-20250401.222524@zip")
	want := "net/neoforged/neoform/25w14craftmine-20250401.222524/neoform-25w14craftmine-20250401.222524.zip"
	if got != want {
		t.Errorf("gradlePath() = %q, want %q", got, want)
	}
}

// TestGradlePathExtensionOverrideWithClassifier -- "@ext" разом з
// класифікатором: класифікатор все ще має йти в назву файлу ПЕРЕД
// розширенням, а "@ext" не повинен потрапити ані в classifier, ані у version.
func TestGradlePathExtensionOverrideWithClassifier(t *testing.T) {
	got := gradlePath("org.gradle.test.classifiers:service:1.0:jdk15@jar")
	want := "org/gradle/test/classifiers/service/1.0/service-1.0-jdk15.jar"
	if got != want {
		t.Errorf("gradlePath() = %q, want %q", got, want)
	}
}

func TestLoaderVersionID(t *testing.T) {
	cases := []struct {
		loader, mc, ver string
		want            string
	}{
		{"fabric", "1.21.1", "0.16.9", "fabric-loader-0.16.9-1.21.1"},
		{"quilt", "1.20.4", "0.26.4", "quilt-loader-0.26.4-1.20.4"},
		{"forge", "1.20.1", "47.2.0", "1.20.1-47.2.0"},
		{"neoforge", "1.21.1", "21.1.95", "1.21.1-21.1.95"},
		{"vanilla", "1.21.1", "", "1.21.1"},
	}
	for _, c := range cases {
		if got := LoaderVersionID(c.loader, c.mc, c.ver); got != c.want {
			t.Errorf("LoaderVersionID(%s,%s,%s) = %q, want %q", c.loader, c.mc, c.ver, got, c.want)
		}
	}
}

// TestGradlePathHasExtension -- регресійний маркер (замінює попередню
// протилежну поведінку): gradlePath() ТЕПЕР самостійно повертає повний
// шлях, включно з розширенням файлу (типово ".jar", якщо у maven-координаті
// немає "@ext"). Раніше функція навмисно повертала шлях БЕЗ розширення, а
// кожен викликач сам дописував ".jar" -- саме це й спричиняло баг з "@zip"
// (див. TestGradlePathExtensionOverride): "@zip" псував version ще ДО
// того, як викликач додавав свій хардкоджений ".jar" зверху.
func TestGradlePathHasExtension(t *testing.T) {
	got := gradlePath("net.minecraftforge:installertools:1.4.1")
	if !strings.HasSuffix(got, ".jar") {
		t.Errorf("gradlePath() має повертати розширення .jar за замовчуванням, отримали: %q", got)
	}
}

// TestExtractBundledJarsNonJarExtension -- регресійний тест: раніше
// extractBundledJars фільтрував "maven/.../*" за суфіксом ".jar", тож будь-
// який бандлений артефакт з ІНШИМ розширенням (напр. ".zip" -- саме так
// виглядає "net.neoforged:neoform:...@zip", коли він трапляється бандленим
// прямо в installer.jar, а не лише завантажується окремо через
// install_profile.json.libraries) тихо ІГНОРУВАВСЯ: файл ніколи не
// потрапляв на диск, і processor, якому він потрібен як вхід, згодом падав
// з "Input does not exist" -- хоча дані фізично лежали в installer.jar.
func TestExtractBundledJarsNonJarExtension(t *testing.T) {
	tmpDir := t.TempDir()
	inst := NewInstaller(tmpDir)

	// Будуємо тестовий installer.jar-подібний zip з трьома файлами під
	// "maven/": звичайний .jar, бандлений .zip (як neoform-архів) і .txt
	// (як окремо задекларований mappings-файл). Усі три мають опинитись
	// у libraries/ після extractBundledJars.
	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)
	files := map[string]string{
		"maven/net/minecraftforge/forge/1.20.1-47.2.0/forge-1.20.1-47.2.0.jar":                     "jar-content",
		"maven/net/neoforged/neoform/25w14craftmine-20250401.222524/neoform-25w14craftmine-20250401.222524.zip": "zip-content",
		"maven/net/minecraft/client/1.20.1-20230612.114412/client-1.20.1-20230612.114412-mappings.txt":          "txt-content",
	}
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}

	zipPath := filepath.Join(tmpDir, "fake-installer.jar")
	if err := os.WriteFile(zipPath, buf.Bytes(), 0644); err != nil {
		t.Fatalf("write test zip: %v", err)
	}
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatalf("open test zip: %v", err)
	}
	defer zr.Close()

	if err := inst.extractBundledJars(zr, "test-version"); err != nil {
		t.Fatalf("extractBundledJars: %v", err)
	}

	for name := range files {
		rel := strings.TrimPrefix(name, "maven/")
		dest := filepath.Join(tmpDir, "libraries", filepath.FromSlash(rel))
		if fi, err := os.Stat(dest); err != nil || fi.Size() == 0 {
			t.Errorf("очікував, що %s буде скопійовано в %s, але: %v", name, dest, err)
		}
	}
}

// TestResolveForgeLibraryPathURLModern -- сучасна форма (1.13+):
// downloads.artifact.{path,url} задані явно -- беремо їх як є, без спроби
// побудувати щось через gradlePath.
func TestResolveForgeLibraryPathURLModern(t *testing.T) {
	lib := forgeLibrary{Name: "net.minecraftforge:forge:1.20.1-47.2.0"}
	lib.Downloads.Artifact.Path = "custom/path/forge.jar"
	lib.Downloads.Artifact.URL = "https://example.test/custom/path/forge.jar"

	path, url, ok := resolveForgeLibraryPathURL(lib)
	if !ok {
		t.Fatal("очікував ok=true для повністю заданої бібліотеки")
	}
	if path != "custom/path/forge.jar" {
		t.Errorf("path = %q, want %q", path, "custom/path/forge.jar")
	}
	if url != "https://example.test/custom/path/forge.jar" {
		t.Errorf("url = %q, want %q", url, "https://example.test/custom/path/forge.jar")
	}
}

// TestResolveForgeLibraryPathURLLegacyWithRepo -- старіша форма
// install_profile.json (лише "name" + "url" репозиторію, БЕЗ секції
// "downloads" узагалі -- напр. старі Forge-профілі 1.7.10-1.12). Раніше
// такі бібліотеки мовчки пропускались (перевірка була лише на
// Downloads.Artifact.URL == ""), тепер шлях і кінцевий URL будуються через
// gradlePath(name) + заданий repo URL.
func TestResolveForgeLibraryPathURLLegacyWithRepo(t *testing.T) {
	lib := forgeLibrary{Name: "com.typesafe:config:1.2.1"}
	lib.URL = "http://files.minecraftforge.net/maven/"

	path, url, ok := resolveForgeLibraryPathURL(lib)
	if !ok {
		t.Fatal("очікував ok=true для старої форми бібліотеки з repo url")
	}
	wantPath := "com/typesafe/config/1.2.1/config-1.2.1.jar"
	wantURL := "http://files.minecraftforge.net/maven/com/typesafe/config/1.2.1/config-1.2.1.jar"
	if path != wantPath {
		t.Errorf("path = %q, want %q", path, wantPath)
	}
	if url != wantURL {
		t.Errorf("url = %q, want %q", url, wantURL)
	}
}

// TestResolveForgeLibraryPathURLLegacyNoRepo -- старіша форма без ЖОДНОГО
// repo url (лише "name") -- має впасти на дефолтний Maven-репозиторій
// Forge, а не мовчки пропустити бібліотеку.
func TestResolveForgeLibraryPathURLLegacyNoRepo(t *testing.T) {
	lib := forgeLibrary{Name: "net.sf.jopt-simple:jopt-simple:4.5"}

	path, url, ok := resolveForgeLibraryPathURL(lib)
	if !ok {
		t.Fatal("очікував ok=true навіть без repo url (дефолтний maven)")
	}
	wantPath := "net/sf/jopt-simple/jopt-simple/4.5/jopt-simple-4.5.jar"
	wantURLSuffix := "/net/sf/jopt-simple/jopt-simple/4.5/jopt-simple-4.5.jar"
	if path != wantPath {
		t.Errorf("path = %q, want %q", path, wantPath)
	}
	if !strings.HasSuffix(url, wantURLSuffix) {
		t.Errorf("url = %q, want suffix %q", url, wantURLSuffix)
	}
}

// TestResolveForgeLibraryPathURLInvalidName -- "name", що не є валідними
// maven-координатами (менше 2 двокрапок): пропускаємо без паніки, а не
// намагаємось побудувати сміттєвий шлях.
func TestResolveForgeLibraryPathURLInvalidName(t *testing.T) {
	lib := forgeLibrary{Name: "garbage-no-colons"}
	_, _, ok := resolveForgeLibraryPathURL(lib)
	if ok {
		t.Error("очікував ok=false для невалідного імені бібліотеки")
	}
}

// TestResolveForgeLibraryPathURLEmptyName -- порожнє ім'я бібліотеки
// (захисна перевірка від сміттєвих/неповних записів у install_profile.json).
func TestResolveForgeLibraryPathURLEmptyName(t *testing.T) {
	_, _, ok := resolveForgeLibraryPathURL(forgeLibrary{})
	if ok {
		t.Error("очікував ok=false для порожнього імені бібліотеки")
	}
}
