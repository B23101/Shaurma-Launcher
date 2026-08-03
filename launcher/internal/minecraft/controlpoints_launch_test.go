package minecraft

// Контрольні точки Forge/NeoForge: РЕАЛЬНИЙ запуск гри тим самим шляхом,
// що й лаунчер (Launcher.Launch з повним LaunchConfig як в app.go), для
// кожного встановленого лоадер-профілю у спільній базі %APPDATA%\.shaurm\
// minecraft. Виконується ТІЛЬКИ при SHAURMA_LAUNCH_TEST=1 (або "dry"):
//   SHAURMA_LAUNCH_TEST=1          — реальний запуск кожної контрольної точки
//   SHAURMA_LAUNCH_TEST=dry        — лише збір команд java без запуску
//   SHAURMA_LAUNCH_TIMEOUT=180     — таймаут очікування меню (с), за замовч. 120
//   SHAURMA_LAUNCH_ONLY=1.17.1     — запустити лише одну контрольну точку (ID)
//
// Успіх = процес гри живий після таймауту (вікно відкрилось, меню
// завантажується). Провал = процес завершився раніше (код виходу ≠ 0 або
// fatal-маркер у виводі). Повний вивід пишеться у
// %APPDATA%\.shaurm\test-launch\<id>\console.log, latest.log — туди ж у
// <id>\.minecraft\logs\latest.log (SaveLogs=true).

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"shaurma-launcher-wails/internal/java"
	"shaurma-launcher-wails/internal/model"
)

type controlPoint struct {
	// ID — назва профілю в versions/ (напр. "1.12.2-forge-14.23.5.2864").
	ID string
	// Loader — "forge" | "neoforge" (для isForgeLike у Launcher).
	Loader string
}

// controlPoints — версії, які юзер уже встановив у лаунчері як контрольні
// точки для Forge/NeoForge. Те, що вже запускається, не чіпаємо — кожну
// контрольну точку додаємо сюди лише коли треба перевірити/полагодити.
var controlPoints = []controlPoint{
	{ID: "1.12.2-forge-14.23.5.2864", Loader: "forge"},
	{ID: "1.17.1-forge-37.0.0", Loader: "forge"},
	{ID: "26.2-forge-65.1.0", Loader: "forge"},
	{ID: "neoforge-0.25w14craftmine.3-beta", Loader: "neoforge"},
	{ID: "neoforge-21.1.233", Loader: "neoforge"},
}

// fatalMarkers — рядки у виводі гри, які означають, що запуск уже не вдасться
// (процес ще може бути живим, але вікно/меню не з'являться). Потрібні для
// швидкого переривання на провал, щоб не чекати весь таймаут.
var fatalMarkers = []string{
	"Exception in thread",
	"FAILED TO LAUNCH",
	"Could not find or load main class",
	"Could not create the Java Virtual Machine",
	"Unrecognized option",
	"Error: Unable to access jarfile",
	"ClassNotFoundException",
	"NoClassDefFoundError",
	"ModLauncher failed to launch",
	"ModLauncher error",
	"severe problem during mod loading",
	"java.lang.module.FindException",
	"InaccessibleObjectException",
	"InvalidPathException",
	"Unsupported class file major version",
	"UnsupportedClassVersionError",
	"java.lang.reflect.InvocationTargetException",
}

// successMarkers — ознаки того, що гра дійшла до меню (не тільки "вікно є").
var successMarkers = []string{
	"Setting user:",
	"Backend library: LWJGL",
	"Sound engine started",
	"Reloading ResourceManager",
	"Forge Mod Loader has successfully loaded",
	"ModLauncher" + " starting",
}

// TestControlPointLaunches — реальний запуск контрольних точок Forge/NeoForge.
// Скіпається без SHAURMA_LAUNCH_TEST (щоб звичайний go test не запускав гру).
func TestControlPointLaunches(t *testing.T) {
	if os.Getenv("SHAURMA_LAUNCH_TEST") == "" {
		t.Skip("встановіть SHAURMA_LAUNCH_TEST=1 (або =dry для збору команд) для реального запуску контрольних точок")
	}
	base := filepath.Join(os.Getenv("APPDATA"), ".shaurm")
	mcBase := filepath.Join(base, "minecraft")
	if _, err := os.Stat(filepath.Join(mcBase, "versions")); err != nil {
		t.Skip("спільну базу Minecraft не знайдено: " + mcBase)
	}

	only := os.Getenv("SHAURMA_LAUNCH_ONLY")
	for _, cp := range controlPoints {
		if only != "" && !strings.Contains(cp.ID, only) {
			continue
		}
		cp := cp
		t.Run(cp.ID, func(t *testing.T) {
			runControlPoint(t, base, mcBase, cp)
		})
	}
}

// runControlPoint — одна контрольна точка: валідація, резолв Java, запуск,
// моніторинг, вердикт.
func runControlPoint(t *testing.T, base, mcBase string, cp controlPoint) {
	// 1. Профіль на диску.
	jsonPath := filepath.Join(mcBase, "versions", cp.ID, cp.ID+".json")
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("профіль не знайдено: %s", jsonPath)
	}
	var vjson VersionJSON
	if err := json.Unmarshal(data, &vjson); err != nil {
		t.Fatalf("парсинг профілю: %v", err)
	}

	// 2. Батьківська ванилла-версія (vanilla-база для RecommendedMajor).
	chain := versionChain(&vjson, mcBase)
	if vjson.InheritsFrom != "" && len(chain) < 2 {
		t.Fatalf("БАЗА ЗАБЛОКОВАНА: батьківська ванилла-версія %q не встановлена в versions/ — потрібно встановити її спершу (EnsureVersion/EnsureLoader)", vjson.InheritsFrom)
	}
	baseMC := chain[len(chain)-1].ID

	// 2a. Догочити встановлення тим самим кодом, що й лаунчер: app.go перед
	// запуском викликає a.inst.EnsureVersion(launchVersion), який качає
	// відсутні бібліотеки ВСЬОГО ланцюга InheritsFrom (vanilla-батько +
	// loader), клієнтський jar, асети і нативки. Без цього запуск впаде на
	// першій же бібліотеці, якої бракує на диску (напр. LWJGL 3.2.1 для
	// 1.17.1-forge) — і це буде хибний вердикт, бо справжній лаунчер
	// завжди робить EnsureVersion перед стартом.
	installer := NewInstaller(mcBase)
	if _, err := installer.EnsureVersion(cp.ID); err != nil {
		t.Fatalf("EnsureVersion(%s) не зміг догочити встановлення: %v", cp.ID, err)
	}
	// EnsureVersion міг перезаписати meta-файл (докачані libraries тощо) —
	// перечитаємо json, щоб launch/classpath бачили актуальний стан.
	data, err = os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("перечитати профіль після EnsureVersion: %s", jsonPath)
	}
	if err := json.Unmarshal(data, &vjson); err != nil {
		t.Fatalf("перепарсити профіль після EnsureVersion: %v", err)
	}

	// 3. Резолв Java — точно як app.go: RecommendedMajor + override
	//    EffectiveJavaMajor з version.json (джерело істини). Для 1.17.x
	//    (Forge 37.0.0) vanilla-батько декларує Java 16 — її і беремо,
	//    БЕЗ підняття до 17: нові збірки JDK 17 зламали старий
	//    bootstraplauncher через ManifestEntryVerifier (NoSuchMethodError),
	//    а Java 16 не зачеплена.
	recommended := java.RecommendedMajor(baseMC)
	effective := EffectiveJavaMajor(&vjson, mcBase)
	if effective > 0 && effective != recommended {
		recommended = effective
	}
	javaPath := filepath.Join(base, "java", fmt.Sprintf("java-%d", recommended), "bin", "javaw.exe")
	if _, err := os.Stat(javaPath); err != nil {
		t.Fatalf("Java %d не встановлена за версією гри: %s (потрібна за RecommendedMajor/EffectiveJavaMajor для %s)", recommended, javaPath, baseMC)
	}

	// 4. Робоча тека гри (окрема під тест — не чіпаємо реальні installations/).
	gameDir := filepath.Join(base, "test-launch", cp.ID, ".minecraft")
	os.MkdirAll(gameDir, 0755)

	// 5. Офлайн-акаунт (з profiles.json або фолбек).
	account := loadOfflineAccount(base)

	// 6. LaunchConfig точно як у app.LaunchInstance.
	inst := model.Instance{
		ID:            cp.ID,
		Name:          cp.ID,
		MCVersion:     cp.ID, // launchVersion = loader-профіль
		Loader:        cp.Loader,
		LoaderVersion: "",
	}
	cfg := LaunchConfig{
		Instance:    inst,
		JavaPath:    javaPath,
		MaxRAM:      4096,
		MinRAM:      0,
		JavaArgs:    "",
		Account:     account,
		GameDir:     gameDir,
		MinecraftDir: mcBase,
		VersionJSON: &vjson,
		SaveLogs:    true,
		WindowWidth: 854,
		WindowHeight: 480,
	}

	// dry-run: лише вивести команду java, не запускати.
	if strings.EqualFold(os.Getenv("SHAURMA_LAUNCH_TEST"), "dry") {
		printDryCommand(t, cfg, mcBase, cp)
		return
	}

	// 7. Запуск + моніторинг.
	ok, why, tail := launchAndMonitor(t, cfg)
	if !ok {
		t.Errorf("ПРОВАЛ: %s\n--- останні %d рядків виводу ---\n%s", why, len(tail), strings.Join(tail, "\n"))
		return
	}
	t.Logf("УСПІХ: %s", why)
}

// launchAndMonitor запускає гру через Launcher.Launch і чекає вердикту:
//   - процес завершився раніше таймауту        → провал (з tail виводу)
//   - fatal-маркер у виводі + процес завершився → провал
//   - процес живий до таймауту (меню)          → успіх, прибираємо процес
func launchAndMonitor(t *testing.T, cfg LaunchConfig) (ok bool, why string, tail []string) {
	timeout := 120 * time.Second
	if v := os.Getenv("SHAURMA_LAUNCH_TIMEOUT"); v != "" {
		if secs, err := time.ParseDuration(v + "s"); err == nil {
			timeout = secs
		}
	}

	l := NewLauncher()
	var mu sync.Mutex
	var lines []string
	consoleLog := filepath.Join(filepath.Dir(cfg.GameDir), "console.log")
	os.MkdirAll(filepath.Dir(consoleLog), 0755)
	f, _ := os.Create(consoleLog)

	exited := make(chan int, 2)
	l.SetExitHandler(func(code int) { exited <- code })
	l.SetOutputHandler(func(line string) {
		mu.Lock()
		lines = append(lines, line)
		if len(lines) > 4000 {
			lines = lines[len(lines)-4000:]
		}
		mu.Unlock()
		f.WriteString(line + "\n")
	})

	start := time.Now()
	if err := l.Launch(cfg); err != nil {
		return false, fmt.Sprintf("Launcher.Launch повернув помилку: %v", err), tailLines(&mu, &lines)
	}
	t.Logf("стартував процес гри (PID %d), чекаю меню до %s...", l.PID(), timeout)

	deadline := time.After(timeout)
	seenFatal := ""
	for {
		select {
		case code := <-exited:
			mu.Lock()
			snap := append([]string(nil), lines...)
			mu.Unlock()
			// Швидкий провал з fatal-маркером має приємніший діагноз.
			if f := firstFatalMarker(snap); f != "" {
				return false, fmt.Sprintf("процес завершився (код %d) з fatal-маркером %q через %s", code, f, time.Since(start)), tailLines(&mu, &lines)
			}
			// Меню вже було досягнуте (success-маркер у виводі), а процес
			// вийшов пізніше — юзер сам закрив вікно гри. Це успіх.
			if m := firstSuccessMarker(snap); m != "" {
				return true, fmt.Sprintf("меню було досягнуте (маркер %q), потім процес завершився (код %d) через %s — вікно закрили", m, code, time.Since(start)), nil
			}
			return false, fmt.Sprintf("процес завершився (код %d) через %s ДО завантаження меню", code, time.Since(start)), tailLines(&mu, &lines)
		case <-deadline:
			mu.Lock()
			snap := append([]string(nil), lines...)
			mu.Unlock()
			if m := firstSuccessMarker(snap); m != "" {
				l.Stop()
				why = fmt.Sprintf("меню досягнуто (маркер %q), процес живий %s — зупинено", m, time.Since(start))
			} else {
				l.Stop()
				why = fmt.Sprintf("процес живий %s (вікно відкрилось), маркер меню не спіймано — зупинено", time.Since(start))
			}
			return true, why, nil
		case <-time.After(500 * time.Millisecond):
			if seenFatal == "" {
				mu.Lock()
				snap := append([]string(nil), lines...)
				mu.Unlock()
				seenFatal = firstFatalMarker(snap)
			}
			// Fatal-маркер без завершення процесу: гра ще "жива", але вже
			// зрозуміло що меню не буде — не чекаємо весь таймаут.
			if seenFatal != "" {
				return false, fmt.Sprintf("fatal-маркер %q у виводі (процес ще живий)", seenFatal), tailLines(&mu, &lines)
			}
		}
	}
}

func tailLines(mu *sync.Mutex, lines *[]string) []string {
	mu.Lock()
	defer mu.Unlock()
	n := len(*lines)
	if n > 80 {
		n = 80
	}
	return append([]string(nil), (*lines)[len(*lines)-n:]...)
}

func firstFatalMarker(lines []string) string {
	for _, line := range lines {
		for _, m := range fatalMarkers {
			if strings.Contains(line, m) {
				return m
			}
		}
	}
	return ""
}

func firstSuccessMarker(lines []string) string {
	for _, line := range lines {
		for _, m := range successMarkers {
			if strings.Contains(line, m) {
				return m
			}
		}
	}
	return ""
}

// printDryCommand збирає і друкує команду java тим самим кодом, що Launcher.Launch
// (BuildClassPath + jsonChainArgs), без запуску.
func printDryCommand(t *testing.T, cfg LaunchConfig, mcBase string, cp controlPoint) {
	// Те саме, що робить Launch до запуску процесу.
	if chainUsesBootstrap(versionChain(cfg.VersionJSON, mcBase)) {
		ensureLoaderClientJar(mcBase, cfg.VersionJSON)
		if cfg.VersionJSON != nil && cfg.VersionJSON.ID != "" {
			classpathVersionID = cfg.VersionJSON.ID
		}
	}
	cpv := BuildClassPath(mcBase, cfg.VersionJSON, classpathVersionID)
	versionName := cfg.VersionJSON.ID
	if versionName == "" {
		versionName = cfg.Instance.MCVersion + "-Shaurma"
	}
	nativesDir := filepath.Join(mcBase, "versions", versionName, "natives")
	assetIndex := cfg.Instance.MCVersion
	if cfg.VersionJSON != nil && cfg.VersionJSON.Assets != "" {
		assetIndex = cfg.VersionJSON.Assets
	}
	jvm, game := jsonChainArgs(cfg, mcBase, versionName, nativesDir, filepath.Join(mcBase, "assets"), assetIndex, 854, 480, cpv, isForgeLike(cfg.Instance.Loader))

	parts := []string{cfg.JavaPath, "-Xms2048M", "-Xmx4096M"}
	parts = append(parts, jvm...)
	parts = append(parts, "-Djava.library.path="+nativesDir, "-cp", cpv, cfg.VersionJSON.MainClass)
	parts = append(parts, game...)
	parts = append(parts, "--username", cfg.Account.Username, "--version", versionName, "--gameDir", cfg.GameDir,
		"--assetsDir", filepath.Join(mcBase, "assets"), "--assetIndex", assetIndex, "--uuid", cfg.Account.UUID,
		"--accessToken", cfg.Account.AccessToken, "--userType", "mojang", "--versionType", "release",
		"--width", "854", "--height", "480")
	t.Logf("DRY команда (%s):\n%s\n", cp.ID, strings.Join(parts, "\n  "))
	t.Logf("DRY підсумок: %d JVM-аргів, %d game-аргів, %d classpath-елементів", len(jvm), len(game), len(strings.Split(cpv, string(os.PathListSeparator))))
}

// loadOfflineAccount повертає перший OFFLINE-акаунт з profiles.json
// (fallback — тестовий Steve).
func loadOfflineAccount(base string) model.Account {
	acc := model.Account{Username: "Steve", UUID: "00000000-0000-0000-0000-000000000000", AccessToken: "0", Type: "offline"}
	data, err := os.ReadFile(filepath.Join(base, "profiles.json"))
	if err != nil {
		return acc
	}
	var list []struct {
		AccountType string `json:"account_type"`
		Username    string `json:"username"`
		UUID        string `json:"uuid"`
		AccessToken string `json:"access_token"`
	}
	if json.Unmarshal(data, &list) != nil {
		return acc
	}
	for _, a := range list {
		if strings.EqualFold(a.AccountType, "OFFLINE") && a.Username != "" {
			acc.Username = a.Username
			acc.UUID = a.UUID
			acc.AccessToken = a.AccessToken
			if acc.AccessToken == "" {
				acc.AccessToken = "0"
			}
			return acc
		}
	}
	return acc
}
