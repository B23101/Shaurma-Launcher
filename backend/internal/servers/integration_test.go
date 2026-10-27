//go:build integration

package servers

import (
	"bufio"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// jsonDecode декодує JSON з reader.
func jsonDecode(r io.Reader, v interface{}) error {
	return json.NewDecoder(r).Decode(v)
}

// xmlDecode декодує XML з reader.
func xmlDecode(r io.Reader, v interface{}) error {
	return xml.NewDecoder(r).Decode(v)
}

// TestInstallAndLaunchVanilla — тест: встановлення Vanilla + запуск + команда + зупинка.
// Запуск: go test -tags integration -run TestInstallAndLaunchVanilla -v -count=1 -timeout 600s
func TestInstallAndLaunchVanilla(t *testing.T) {
	testInstallAndLaunch(t, "vanilla", "")
}

// TestInstallAndLaunchPaper — тест Paper сервера.
func TestInstallAndLaunchPaper(t *testing.T) {
	testInstallAndLaunch(t, "paper", "1.21.4")
}

// TestInstallAndLaunchFabric — тест Fabric сервера.
func TestInstallAndLaunchFabric(t *testing.T) {
	testInstallAndLaunch(t, "fabric", "1.20.1")
}

// TestInstallAndLaunchForge — тест Forge сервера.
func TestInstallAndLaunchForge(t *testing.T) {
	testInstallAndLaunch(t, "forge", "1.20.1")
}

// testInstallAndLaunch — універсальний тест для будь-якого ядра.
func testInstallAndLaunch(t *testing.T, core, forcedMCVersion string) {
	t.Helper()

	// Знаходимо Java.
	javaPath, err := FindJavaPath(0)
	if err != nil {
		p, e := exec.LookPath("java")
		if e != nil {
			t.Skipf("Java не знайдена: %v", err)
		}
		javaPath = p
	}
	t.Logf("Java: %s", javaPath)

	// Визначаємо Java major — з ОБОХ форматів: "openjdk" та "java" (Oracle).
	javaMajor := detectJavaMajor(t, javaPath)
	t.Logf("Java major: %d", javaMajor)

	// MC-версія.
	mcVersion := forcedMCVersion
	if mcVersion == "" {
		mcVersion = pickMCVersion(javaMajor)
	}
	t.Logf("MC version: %s", mcVersion)

	dir := t.TempDir()
	ramMB := 2048

	// EULA — пишемо завжди.
	os.WriteFile(filepath.Join(dir, "eula.txt"), []byte("eula=true\n"), 0644)

	// ── КРОК 1: Встановлення ──
	t.Log("=== КРОК 1: Встановлення ===")
	switch core {
	case "vanilla":
		err = installVanilla(t, dir, mcVersion)
	case "paper":
		err = installPaper(t, dir, mcVersion)
	case "fabric":
		err = installFabric(t, dir, mcVersion)
	case "forge":
		err = installForge(t, dir, mcVersion)
	default:
		t.Fatalf("невідоме ядро: %s", core)
	}
	if err != nil {
		t.Fatalf("встановлення: %v", err)
	}

	// ── КРОК 2: Детекція ──
	t.Log("=== КРОК 2: Детекція ===")
	det, err := DetectCore(dir)
	if err != nil {
		t.Fatalf("DetectCore: %v", err)
	}
	t.Logf("Detected: core=%s mc=%s loaderVer=%s", det.Core, det.MCVersion, det.LoaderVer)

	// Визначаємо шлях до jar для запуску.
	jarPath := findLaunchJar(dir, core)

	// ── КРОК 3: Запуск ──
	t.Log("=== КРОК 3: Запуск ===")
	port := ScanFreePort(25565, nil, "", nil)

	// Формуємо Server вручну — з правильними даними.
	srv := &Server{
		ID:        "test-" + core,
		Name:      "Test " + core,
		Dir:       dir,
		Core:      core,
		MCVersion: mcVersion,
		LoaderVer: det.LoaderVer,
		JavaMajor: javaMajor,
		RAMMB:     ramMB,
		Port:      port,
	}

	// Перевіряємо що jar існує.
	if _, err := os.Stat(jarPath); os.IsNotExist(err) {
		// Для fabric використовуємо fabric-server-launch.jar
		if core == "fabric" {
			jarPath = filepath.Join(dir, "fabric-server-launch.jar")
		}
		if _, err := os.Stat(jarPath); os.IsNotExist(err) {
			// Знайдемо jar самі
			jarPath = findAnyJar(dir, core)
		}
		if _, err := os.Stat(jarPath); os.IsNotExist(err) {
			t.Fatalf("jar не знайдено: %s", jarPath)
		}
	}
	t.Logf("JAR: %s", jarPath)

	// Запускаємо процес напряму (не через Manager — для діагностики).
	args := buildCommand(srv)
	t.Logf("Command: java %s", strings.Join(args, " "))

	cmd := exec.Command(javaPath, args...)
	cmd.Dir = dir
	cmd.Stderr = cmd.Stdout // об'єднуємо

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("cmd.Start: %v", err)
	}
	t.Logf("PID: %d", cmd.Process.Pid)

	// Читаємо stdout і шукаємо "Done".
	done := make(chan string, 1) // "Done" line
	var allLines []string
	go func() {
		scanner := bufio.NewScanner(stdoutPipe)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			allLines = append(allLines, line)
			if len(allLines) <= 30 || strings.Contains(line, "Done") || strings.Contains(line, "ERROR") || strings.Contains(line, "Exception") {
				t.Logf("  [console] %s", line)
			}
			if strings.Contains(line, "Done") && strings.Contains(line, "!") {
				select {
				case done <- line:
				default:
				}
			}
		}
	}()

	// Чекаємо "Done" або crash.
	t.Log("Чекаємо сервер...")
	select {
	case d := <-done:
		t.Logf("✅ Сервер запущено! %s", d)
	case <-time.After(120 * time.Second):
		t.Logf("Останні рядки (%d):", len(allLines))
		start := 0
		if len(allLines) > 20 {
			start = len(allLines) - 20
		}
		for _, l := range allLines[start:] {
			t.Logf("  %s", l)
		}
		t.Fatal("сервер не стартував за 120 секунд")
	}

	// ── КРОК 4: Команда в консолі ──
	t.Log("=== КРОК 4: Команда в консолі ===")
	time.Sleep(2 * time.Second)
	sayCmd := fmt.Sprintf("say IntegrationTest%sOK", strings.Title(core))
	_, err = stdin.Write([]byte(sayCmd + "\n"))
	if err != nil {
		t.Errorf("stdin write: %v", err)
	}
	time.Sleep(3 * time.Second)

	foundSay := false
	for _, l := range allLines {
		if strings.Contains(l, "IntegrationTest") && strings.Contains(l, "OK") {
			foundSay = true
			break
		}
	}
	if foundSay {
		t.Log("✅ Команда 'say' виконалась!")
	} else {
		t.Log("⚠️ Команда 'say' не знайдена в логах")
	}

	// ── КРОК 5: Graceful stop ──
	t.Log("=== КРОК 5: Graceful Stop ===")
	_, err = stdin.Write([]byte("stop\n"))
	if err != nil {
		t.Logf("stdin stop error: %v (можливо вже закрито)", err)
	}

	// Чекаємо завершення.
	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	select {
	case <-waitCh:
		t.Log("✅ Сервер зупинено gracefully!")
	case <-time.After(30 * time.Second):
		t.Log("⚠️ Таймаут graceful stop, kill...")
		_ = cmd.Process.Kill()
		<-waitCh
	}

	// ── КРОК 6: Перевірка порту ──
	time.Sleep(2 * time.Second)
	portCheck := CheckPortFree(port, nil, "", nil)
	if portCheck.Free {
		t.Log("✅ Порт звільнився")
	} else {
		t.Logf("⚠️ Порт %d все ще зайнятий: %s", port, portCheck.Holder)
	}

	t.Log("=== ВСІ ТЕСТИ ЗАВЕРШЕНО ===")
}

// ── Підбір версії/ядра за Java ────────────────────────────────────────

func pickMCVersion(javaMajor int) string {
	switch {
	case javaMajor >= 25:
		return "1.21.4" // Java 25 рекомендовано для MC 1.21+
	case javaMajor >= 21:
		return "1.21.4"
	case javaMajor >= 17:
		return "1.20.1"
	case javaMajor >= 16:
		return "1.17.1"
	default:
		return "1.20.1"
	}
}

func detectJavaMajor(t *testing.T, javaPath string) int {
	t.Helper()
	out, err := exec.Command(javaPath, "-version").CombinedOutput()
	if err != nil {
		t.Logf("java -version error: %v", err)
		return 25
	}
	versionStr := strings.TrimSpace(string(out))
	t.Logf("java -version output: %s", versionStr)

	// Пробуємо обидва формати:
	// "openjdk version "25.0.2" ..." — OpenJDK
	// "java version "25.0.2" ..." — Oracle Java
	re := regexp.MustCompile(`(?:openjdk|java) version "(\d+)(?:\.(\d+))?`)
	matches := re.FindStringSubmatch(versionStr)
	if matches == nil {
		t.Logf("не вдалося розпарсити java version з %q", versionStr)
		return 25
	}

	var major, minor int
	fmt.Sscanf(matches[1], "%d", &major)
	if len(matches) > 2 && matches[2] != "" {
		fmt.Sscanf(matches[2], "%d", &minor)
	}

	if major == 1 {
		return minor // Java 8 = "1.8.x"
	}
	return major
}

// findLaunchJar шукає jar-файл для запуску сервера.
func findLaunchJar(dir, core string) string {
	switch core {
	case "fabric":
		p := filepath.Join(dir, "fabric-server-launch.jar")
		if _, err := os.Stat(p); err == nil {
			return p
		}
		// Стара назва
		p = filepath.Join(dir, "fabric-server-launcher.jar")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	case "forge":
		// Для Forge ≥1.17 jar не потрібен — використовуємо @libraries/win_args.txt
		// Але перевіряємо чи є jar.
		p := filepath.Join(dir, "server.jar")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	case "quilt":
		p := filepath.Join(dir, "quilt-server-launch.jar")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	// Default: server.jar
	return filepath.Join(dir, "server.jar")
}

// findAnyJar шукає будь-який .jar у папці.
func findAnyJar(dir, core string) string {
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".jar") {
			// Пріоритет за ядром
			switch core {
			case "paper":
				if strings.HasPrefix(e.Name(), "paper-") {
					return filepath.Join(dir, e.Name())
				}
			case "purpur":
				if strings.HasPrefix(e.Name(), "purpur-") {
					return filepath.Join(dir, e.Name())
				}
			case "fabric":
				if strings.HasPrefix(e.Name(), "fabric-") {
					return filepath.Join(dir, e.Name())
				}
			}
		}
	}
	// Будь-який jar
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".jar") {
			return filepath.Join(dir, e.Name())
		}
	}
	return filepath.Join(dir, "server.jar")
}

// ── Встановлення ──────────────────────────────────────────────────────

func installVanilla(t *testing.T, dir, mcVersion string) error {
	t.Helper()
	t.Logf("Завантажуємо version manifest для %s...", mcVersion)
	resp, err := http.Get("https://launchermeta.mojang.com/mc/game/version_manifest_v2.json")
	if err != nil {
		return fmt.Errorf("manifest: %w", err)
	}
	defer resp.Body.Close()

	var manifest struct {
		Versions []struct {
			ID  string `json:"id"`
			URL string `json:"url"`
		} `json:"versions"`
	}
	if err := jsonDecode(resp.Body, &manifest); err != nil {
		return fmt.Errorf("decode manifest: %w", err)
	}

	var versionURL string
	for _, v := range manifest.Versions {
		if v.ID == mcVersion {
			versionURL = v.URL
			break
		}
	}
	if versionURL == "" {
		return fmt.Errorf("версія %s не знайдена в manifest", mcVersion)
	}

	var versionMeta struct {
		Downloads struct {
			Server struct {
				URL string `json:"url"`
			} `json:"server"`
		} `json:"downloads"`
	}
	if err := fetchJSON(versionURL, &versionMeta); err != nil {
		return fmt.Errorf("version.json: %w", err)
	}

	t.Logf("Завантажуємо server.jar для %s...", mcVersion)
	if err := downloadFile(versionMeta.Downloads.Server.URL, filepath.Join(dir, "server.jar")); err != nil {
		return fmt.Errorf("download: %w", err)
	}
	t.Logf("✅ server.jar завантажено")
	return nil
}

func installPaper(t *testing.T, dir, mcVersion string) error {
	t.Helper()
	t.Logf("Завантажуємо Paper %s (v3 API)...", mcVersion)

	// PaperMC v3 API: fill.papermc.io
	// Build format: [{id, channel:"STABLE", downloads:{"server:default":{name, url, ...}}}
	type paperBuild struct {
		ID       int    `json:"id"`
		Channel  string `json:"channel"`
		Downloads map[string]struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		} `json:"downloads"`
	}

	url := fmt.Sprintf("https://fill.papermc.io/v3/projects/paper/versions/%s/builds", mcVersion)
	var builds []paperBuild
	if err := fetchJSONWithUA(url, &builds); err != nil {
		return fmt.Errorf("paper v3 api: %w", err)
	}

	// Шукаємо останній STABLE build.
	var downloadURL, buildName string
	var buildID int
	for _, b := range builds {
		if b.Channel == "STABLE" {
			dl, ok := b.Downloads["server:default"]
			if ok {
				downloadURL = dl.URL
				buildName = dl.Name
				buildID = b.ID
				break
			}
		}
	}
	if downloadURL == "" {
		return fmt.Errorf("немає STABLE builds для Paper %s", mcVersion)
	}

	t.Logf("Paper build %d: %s (%s)", buildID, buildName, downloadURL)
	if err := downloadFile(downloadURL, filepath.Join(dir, "server.jar")); err != nil {
		return fmt.Errorf("download: %w", err)
	}
	t.Logf("✅ Paper завантажено")
	return nil
}

func installFabric(t *testing.T, dir, mcVersion string) error {
	t.Helper()
	t.Log("Завантажуємо fabric-installer...")
	installerURL := "https://meta.fabricmc.net/v2/versions/installer"
	var installerMeta []struct {
		Version string `json:"version"`
		URL     string `json:"url"`
	}
	if err := fetchJSONWithUA(installerURL, &installerMeta); err != nil {
		return fmt.Errorf("fabric installer meta: %w", err)
	}
	if len(installerMeta) == 0 {
		return fmt.Errorf("немає fabric installer versions")
	}

	installerPath := filepath.Join(dir, "fabric-installer.jar")
	if err := downloadFile(installerMeta[0].URL, installerPath); err != nil {
		return fmt.Errorf("download installer: %w", err)
	}

	loaderURL := fmt.Sprintf("https://meta.fabricmc.net/v2/versions/loader/%s", mcVersion)
	var loaderEntries []struct {
		Loader struct {
			Version string `json:"version"`
		} `json:"loader"`
	}
	if err := fetchJSONWithUA(loaderURL, &loaderEntries); err != nil {
		return fmt.Errorf("fabric loader: %w", err)
	}
	if len(loaderEntries) == 0 {
		return fmt.Errorf("немає loader для Fabric %s", mcVersion)
	}
	loaderVer := loaderEntries[0].Loader.Version
	t.Logf("Fabric loader: %s", loaderVer)

	cmd := exec.Command("java", "-jar", installerPath, "server",
		"-dir", dir, "-mcversion", mcVersion, "-loader", loaderVer, "-downloadMinecraft")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("fabric installer: %v\n%s", err, out)
	}
	t.Logf("Fabric installer: %s", strings.TrimSpace(string(out)))

	launchJar := filepath.Join(dir, "fabric-server-launch.jar")
	if _, err := os.Stat(launchJar); os.IsNotExist(err) {
		return fmt.Errorf("fabric-server-launch.jar не створено після встановлення")
	}
	t.Logf("✅ Fabric встановлено")
	return nil
}

func installForge(t *testing.T, dir, mcVersion string) error {
	t.Helper()
	t.Logf("Завантажуємо Forge maven-metadata для %s...", mcVersion)
	metadataURL := "https://maven.minecraftforge.net/net/minecraftforge/forge/maven-metadata.xml"
	var xmlData struct {
		Versioning struct {
			Versions struct {
				Version []string `xml:"version"`
			} `xml:"versions"`
		} `xml:"versioning"`
	}
	if err := fetchXML(metadataURL, &xmlData); err != nil {
		return fmt.Errorf("forge metadata: %w", err)
	}

	prefix := mcVersion + "-"
	var latestForge string
	for _, v := range xmlData.Versioning.Versions.Version {
		if strings.HasPrefix(v, prefix) && !strings.Contains(v, "-beta") && !strings.Contains(v, "-rc") {
			latestForge = v
		}
	}
	if latestForge == "" {
		return fmt.Errorf("Forge не знайдено для MC %s", mcVersion)
	}
	t.Logf("Forge: %s", latestForge)

	installerURL := fmt.Sprintf("https://maven.minecraftforge.net/net/minecraftforge/forge/%s/forge-%s-installer.jar",
		latestForge, latestForge)
	installerPath := filepath.Join(dir, "forge-installer.jar")
	if err := downloadFile(installerURL, installerPath); err != nil {
		return fmt.Errorf("download forge installer: %w", err)
	}

	t.Logf("Запускаємо Forge installer --installServer...")
	cmd := exec.Command("java", "-jar", installerPath, "--installServer")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("forge installer: %v\n%s", err, out)
	}
	t.Logf("✅ Forge встановлено (output: %d bytes)", len(out))
	return nil
}

// ── Хелпери ────────────────────────────────────────────────────────────

func downloadFile(url, path string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, url)
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}

func fetchJSON(url string, v interface{}) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return jsonDecode(resp.Body, v)
}

func fetchXML(url string, v interface{}) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return xmlDecode(resp.Body, v)
}

// fetchJSONWithUA fetches JSON with a proper User-Agent (required by PaperMC v3 API).
func fetchJSONWithUA(url string, v interface{}) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "shaurma-launcher-integration-test/1.0.0 (github.com/shaurma-launcher)")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, url)
	}
	return jsonDecode(resp.Body, v)
}
