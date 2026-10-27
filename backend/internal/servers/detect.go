// detect.go — детекція ядра Minecraft-сервера за файловими маркерами.
//
// Алгоритм перевіряє наявність характерних файлів/архітектур і повертає
// ядро, MC-версію, версію лоадера та впевненість (high/medium/low).
// Порядок перевірок відповідає пріоритету: Velocity → Purpur → Paper →
// Fabric → Quilt → NeoForge → Forge → Vanilla.
package servers

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

// reServerVersionLog — перший рядок логу: "Starting minecraft server version X.Y.Z"
var reServerVersionLog = regexp.MustCompile(`(?i)Starting minecraft server version\s+(\S+)`)

// DetectCore аналізує папку сервера за маркерами файлів і повертає результат.
func DetectCore(dir string) (CoreDetectResult, error) {
	result := CoreDetectResult{
		Messages: make([]string, 0),
		Port:     25565,
	}

	// Швидкі перевірки по порядку пріоритету.

	// 1. Velocity: velocity.toml + velocity.jar
	if fileExists(filepath.Join(dir, "velocity.toml")) && fileExists(filepath.Join(dir, "velocity.jar")) {
		result.Core = CoreVelocity
		// Java 25 безпечна для всіх гілок Velocity: 3.5.x (мін. 21) і
		// 4.x (мін. 25) — старіший 21 не запустить 4.x.
		result.JavaMajor = 25
		result.Confidence = "high"
		result.Messages = append(result.Messages, "знайдено velocity.toml + velocity.jar")
		result.ServerJar = "velocity.jar"
		result.Port = detectVelocityPort(dir)
		// MCVersion навмисно ПОРОЖНІЙ: Velocity — проксі, якому байдуже,
		// яка версія Minecraft (раніше тут хардкодилось "1.21", через що
		// імпортований проксі показував фейкову «версію гри»).
		result.MCVersion = ""
		return result, nil
	}

	// 2. Purpur: purpur.yml
	if fileExists(filepath.Join(dir, "purpur.yml")) {
		result.Core = CorePurpur
		result.Confidence = "high"
		result.Messages = append(result.Messages, "знайдено purpur.yml")
		result.ServerJar = "server.jar"
	}

	// 3. Paper: paper.yml / paper-server.yml (Paper 1.19+)
	if result.Core == "" && (fileExists(filepath.Join(dir, "paper.yml")) || fileExists(filepath.Join(dir, "paper-server.yml"))) {
		result.Core = CorePaper
		result.Confidence = "high"
		result.Messages = append(result.Messages, "знайдено paper.yml")
		result.ServerJar = "server.jar"
	}

	// 4. Fabric: fabric-server-launch.jar
	if result.Core == "" {
		for _, name := range []string{"fabric-server-launch.jar", "fabric-server-launcher.jar"} {
			if fileExists(filepath.Join(dir, name)) {
				result.Core = CoreFabric
				result.Confidence = "high"
				result.Messages = append(result.Messages, "знайдено " + name)
				result.ServerJar = name
				break
			}
		}
	}

	// 5. Quilt: quilt-server-launch.jar
	if result.Core == "" && fileExists(filepath.Join(dir, "quilt-server-launch.jar")) {
		result.Core = CoreQuilt
		result.Confidence = "high"
		result.Messages = append(result.Messages, "знайдено quilt-server-launch.jar")
		result.ServerJar = "quilt-server-launch.jar"
	}

	// 6. NeoForge: libraries/net/neoforged/neoforge/
	if result.Core == "" && dirExists(filepath.Join(dir, "libraries", "net", "neoforged", "neoforge")) {
		result.Core = CoreNeoForge
		result.Confidence = "high"
		ver := detectForgeVersionFromDir(dir, "neoforged", "neoforge")
		result.LoaderVer = ver
		result.Messages = append(result.Messages, "знайдено NeoForge libraries, версія: " + ver)
		result.ServerJar = "server.jar"
	}

	// 7. Forge: libraries/net/minecraftforge/forge/
	if result.Core == "" && dirExists(filepath.Join(dir, "libraries", "net", "minecraftforge", "forge")) {
		result.Core = CoreForge
		result.Confidence = "high"
		ver := detectForgeVersionFromDir(dir, "minecraftforge", "forge")
		result.LoaderVer = ver
		result.Messages = append(result.Messages, "знайдено Forge libraries, версія: " + ver)
		result.ServerJar = detectForgeServerJar(dir, ver)
	}

	// 8. Forge installer jar (без встановлення)
	if result.Core == "" {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			name := e.Name()
			if strings.HasPrefix(name, "forge-") && strings.HasSuffix(name, "-installer.jar") {
				result.Core = CoreForge
				result.Confidence = "low"
				result.InstallerJar = name
				result.Messages = append(result.Messages, "знайдено неінстальований installer: "+name)
				return result, nil
			}
		}
	}

	// 9. Vanilla: server.jar
	if result.Core == "" && fileExists(filepath.Join(dir, "server.jar")) {
		result.Core = CoreVanilla
		result.Confidence = "medium"
		result.Messages = append(result.Messages, "знайдено server.jar (vanilla або невідоме ядро)")
		result.ServerJar = "server.jar"
	}

	if result.Core == "" {
		return result, fmt.Errorf("у папці не знайдено серверне ядро: %s", dir)
	}

	// Версія MC: пріоритет — server.properties → logs/latest.log → jar version.json
	result.MCVersion = detectMCVersion(dir, result.Core)
	result.JavaMajor = JavaMajorForVersion(result.MCVersion)
	result.Port = detectPort(dir)

	// Для Paper/Purpur лоадер-версія = build number з jar (опційно)
	if result.Core == CorePaper || result.Core == CorePurpur {
		result.LoaderVer = detectPaperBuildFromJar(filepath.Join(dir, result.ServerJar))
	}

	return result, nil
}

// ── Допоміжні функції детекції ──────────────────────────────────────────

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// detectMCVersion визначає MC-версію за пріоритетом:
// 1) logs/latest.log (перший рядок "Starting minecraft server version X")
// 2) server.properties (немає версії — використовуємо дефолт)
func detectMCVersion(dir, core string) string {
	// Спроба 1: logs/latest.log
	logPath := filepath.Join(dir, "logs", "latest.log")
	if data, err := os.ReadFile(logPath); err == nil {
		scanner := bufio.NewScanner(bytes.NewReader(data))
		for scanner.Scan() {
			line := scanner.Text()
			if m := reServerVersionLog.FindStringSubmatch(line); m != nil {
				return m[1]
			}
		}
	}

	// Спроба 2: version.json з jar (vanilla/paper/purpur)
	jar := "server.jar"
	if core == CoreVelocity {
		jar = "velocity.jar"
	}
	if ver := extractVersionJSONFromJar(filepath.Join(dir, jar)); ver != "" {
		return ver
	}

	// Fallback: невідомо
	return ""
}

// extractVersionJSONFromJar читає version.json зсередини jar-файлу.
func extractVersionJSONFromJar(jarPath string) string {
	zr, err := zip.OpenReader(jarPath)
	if err != nil {
		return ""
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name == "version.json" || f.Name == "META-INF/version.json" {
			rc, err := f.Open()
			if err != nil {
				continue
			}
			var v struct {
				ID string `json:"id"`
			}
			if err := json.NewDecoder(rc).Decode(&v); err == nil && v.ID != "" {
				rc.Close()
				return v.ID
			}
			rc.Close()
		}
	}
	return ""
}

// detectPort читає server-port з server.properties.
func detectPort(dir string) int {
	props := parsePropertiesFile(filepath.Join(dir, "server.properties"))
	if v, ok := props["server-port"]; ok {
		if p, err := strconv.Atoi(v); err == nil && p > 0 && p < 65536 {
			return p
		}
	}
	return 25565
}

// detectVelocityPort читає bind з velocity.toml ( simplicity: шукаємо :PORT ).
func detectVelocityPort(dir string) int {
	data, err := os.ReadFile(filepath.Join(dir, "velocity.toml"))
	if err != nil {
		return 25565
	}
	re := regexp.MustCompile(`bind\s*=\s*"[^"]*:(\d+)"`)
	if m := re.FindSubmatch(data); m != nil {
		if p, err := strconv.Atoi(string(m[1])); err == nil {
			return p
		}
	}
	return 25565
}

// detectForgeVersionFromDir знаходить версію Forge/NeoForge зі шляху бібліотек.
func detectForgeVersionFromDir(dir, org, project string) string {
	libDir := filepath.Join(dir, "libraries", "net", org, project)
	entries, err := os.ReadDir(libDir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.IsDir() {
			// Формат: "1.20.1-47.4.10" або "21.1.95"
			return e.Name()
		}
	}
	return ""
}

// detectForgeServerJar визначає головний jar для Forge.
func detectForgeServerJar(dir, forgeVer string) string {
	// Для MC ≥ 1.17: використовуємо @libraries/.../win_args.txt або unix_args.txt
	argsName := "win_args.txt"
	if runtime.GOOS != "windows" {
		argsName = "unix_args.txt"
	}
	argsPath := filepath.Join(dir, "libraries", "net", "minecraftforge", "forge", forgeVer, argsName)
	if fileExists(argsPath) {
		return "" // не використовуємо -jar, а @file
	}
	// Для MC < 1.17: forge-<ver>-universal.jar
	universal := filepath.Join(dir, fmt.Sprintf("forge-%s-universal.jar", forgeVer))
	if fileExists(universal) {
		return filepath.Base(universal)
	}
	return "server.jar"
}

// detectPaperBuildFromJar намагається витягти build number з jar-імені.
func detectPaperBuildFromJar(jarPath string) string {
	base := filepath.Base(jarPath)
	// paper-1.21.4-123.jar → "123"
	re := regexp.MustCompile(`paper-\d+\.\d+(?:\.\d+)?-(\d+)\.jar`)
	if m := re.FindStringSubmatch(base); m != nil {
		return m[1]
	}
	// purpur-1.21.4-123.jar
	re2 := regexp.MustCompile(`purpur-\d+\.\d+(?:\.\d+)?-(\d+)\.jar`)
	if m := re2.FindStringSubmatch(base); m != nil {
		return m[1]
	}
	return ""
}

// ── server.properties parser (спільний з properties.go) ─────────────────

// parsePropertiesFile читає Java properties файл (ключ=значення).
func parsePropertiesFile(path string) map[string]string {
	out := make(map[string]string)
	data, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		if idx := strings.IndexByte(line, '='); idx > 0 {
			key := strings.TrimSpace(line[:idx])
			val := strings.TrimSpace(line[idx+1:])
			out[key] = val
		}
	}
	return out
}
