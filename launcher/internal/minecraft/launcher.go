package minecraft

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"shaurma-launcher-wails/internal/model"
)

// classpathVersionID — ID loader-профілю (напр. "1.20.1-forge-47.4.10"),
// під яким ensureLoaderClientJar зберігає копію клієнтського jar у
// versions/<id>/<id>.jar. BuildClassPath спершу шукає jar саме там
// (він збігається з ${version_name}.jar у -DignoreList), а фолбек — на
// ванилла-jar найглибшої батьківської версії. Глобальна змінна пакета:
// встановлюється у Launch перед збором класшляху.
var classpathVersionID string

type Launcher struct {
	mu           sync.Mutex
	process      *exec.Cmd
	running      bool
	instance     string
	startAt      time.Time
	resolvedJava string // резолвнутий шлях до java (для SINST_JAVA у post-exit)
	launchArgs   string // повний список аргументів запуску (для SINST_JAVA_ARGS у post-exit)
	onOutput     func(line string)
	onExit       func(exitCode int)
	onPlaytime   func(instanceID string, seconds int64)
}

func NewLauncher() *Launcher {
	return &Launcher{}
}

func (l *Launcher) SetOutputHandler(f func(string)) { l.onOutput = f }
func (l *Launcher) SetExitHandler(f func(int))      { l.onExit = f }

// SetPlaytimeHandler реєструє обробник запису ігрового часу: викликається
// після виходу з гри з ID збірки та проведеними секундами.
func (l *Launcher) SetPlaytimeHandler(f func(instanceID string, seconds int64)) { l.onPlaytime = f }

type LaunchConfig struct {
	Instance    model.Instance
	JavaPath    string
	MaxRAM      int
	// MinRAM — стартовий розмір купи (-Xms), МБ. Якщо 0 — рахується як
	// MaxRAM/2 (стара поведінка за замовчуванням).
	MinRAM      int
	JavaArgs    string
	Account     model.Account
	GameDir     string
	// MinecraftDir — СПІЛЬНА база Minecraft (versions/libraries/assets), у
	// яку інсталер качає версії, бібліотеки й індекси асетів (зазвичай
	// <dataDir>/minecraft). Класшлях і --assetsDir рахуються ЗВІДСИ, а не з
	// GameDir: у GameDir (тека конкретної збірки, installations/<id>/.minecraft)
	// лежать лише файли самої збірки (моди/конфіги/saves). Якщо порожній —
	// фолбек на GameDir (стара поведінка).
	MinecraftDir string
	VersionJSON  *VersionJSON
	MainClass    string
	SaveLogs     bool

	// Вікно гри
	Fullscreen   bool
	WindowWidth  int
	WindowHeight int

	// Команди (pre-launch / wrapper / post-exit)
	PreLaunchCommand string
	WrapperCommand   string
	PostExitCommand  string
	EnvVars          string

	// ── Автоприєднання ──
	// AutoJoinType: "world" (одиночна гра, вимагає AutoJoinWorld — назва
	// теки в saves/) або "server" (мультиплеєр, AutoJoinServer — host[:port]).
	// Реалізовано через офіційні прапорці Minecraft --quickPlaySingleplayer
	// / --quickPlayMultiplayer (є з 1.20; підтримується Mojang launcher і
	// Prism). Для версій, де quickPlay недоступний, ці прапорці Minecraft
	// просто ігнорує — це safe no-op, а не помилка запуску.
	AutoJoinType   string
	AutoJoinWorld  string
	AutoJoinServer string
}

func (l *Launcher) Launch(cfg LaunchConfig) error {
	l.mu.Lock()
	if l.running {
		l.mu.Unlock()
		return fmt.Errorf("already running")
	}
	l.running = true
	l.instance = cfg.Instance.ID
	l.mu.Unlock()

	javaPath := cfg.JavaPath
	if javaPath == "" {
		var err error
		javaPath, err = exec.LookPath("java")
		if err != nil {
			l.mu.Lock()
			l.running = false
			l.mu.Unlock()
			return fmt.Errorf("java not found: %w", err)
		}
	}
	// Якщо шлях до Java вказано, але файлу на диску немає — повертаємо
	// зрозумілу помилку. Вбудовану Java лаунчер встановлює сам на рівні
	// app.LaunchInstance (EnsureInstalled), тому сюди це не потрапляє,
	// якщо все налаштовано правильно.
	if strings.TrimSpace(javaPath) != "" && javaPath != "java" {
		if _, err := os.Stat(javaPath); os.IsNotExist(err) {
			l.mu.Lock()
			l.running = false
			l.mu.Unlock()
			return fmt.Errorf("java not found at %s", javaPath)
		}
	}

	ram := cfg.MaxRAM
	if ram < 1024 {
		ram = 2048
	}

	// База Minecraft: спільна тека інсталера (versions/libraries/assets).
	mcBase := cfg.MinecraftDir
	if mcBase == "" {
		mcBase = cfg.GameDir // фолбек: усе в теці збірки (стара поведінка)
	}
	assetsDir := filepath.Join(mcBase, "assets")

	// Ім'я версії: для лоадерів — повний ID профілю (напр.
	// 1.20.1-forge-47.4.10), інакше — версія Minecraft. Від нього
	// залежить тека нативок: інсталер розпаковує їх у
	// versions/<ID>/natives, тож і JVM має шукати їх там само.
	versionName := cfg.Instance.MCVersion + "-Shaurma"
	if cfg.VersionJSON != nil && cfg.VersionJSON.ID != "" {
		versionName = cfg.VersionJSON.ID
	}
	nativesDir := filepath.Join(mcBase, "versions", versionName, "natives")
	os.MkdirAll(nativesDir, 0755)

	// Forge/NeoForge 1.17+ (cpw.mods.bootstraplauncher): ігровий jar
	// класшляху має бути versions/<loader-id>/<loader-id>.jar — копія
	// клієнта ванилла, що збігається з ${version_name}.jar у -DignoreList
	// (див. ensureLoaderClientJar). Інакше на класшлях потрапляє ванилла
	// versions/<mc>/<mc>.jar, bootstraplauncher модулює його як _1._20._1
	// (чи _1._21._1 тощо) і старт падає з "Module minecraft contains
	// package ..., module _1._X._Y exports package ... to minecraft" —
	// split-package між автоматичним модулем ванилла-jar і модулем гри
	// minecraft, який FML/NeoForge будує з forge/neoforge-*-client.jar.
	//
	// РАНІШЕ тут стояла додаткова умова "лише forge" (strings.EqualFold
	// (cfg.Instance.Loader, "forge")), яка припускала, що NeoForge "сам
	// будує union FS" і цього конфлікту не має. Це припущення виявилось
	// хибним: NeoForge 1.17+ так само працює через
	// cpw.mods.bootstraplauncher (видно в трейсі краху:
	// "cpw.mods.bootstraplauncher@2.0.2/...BootstrapLauncher.run") і так
	// само підпадає під той самий split-package механізм. chainUsesBootstrap
	// вже й так правильно детектує потребу в цьому фіксі за реальними
	// JVM-аргументами (-p/--add-modules) конкретної версії, незалежно від
	// назви лоадера — тому зайва прив'язка до "forge" лише вимикала фікс
	// саме там, де він був потрібен (NeoForge), не додаючи жодної користі.
	if chainUsesBootstrap(versionChain(cfg.VersionJSON, mcBase)) {
		ensureLoaderClientJar(mcBase, cfg.VersionJSON)
		if cfg.VersionJSON != nil && cfg.VersionJSON.ID != "" {
			classpathVersionID = cfg.VersionJSON.ID
		}
	}
	cp := BuildClassPath(mcBase, cfg.VersionJSON, classpathVersionID)

	mainClass := cfg.MainClass
	if mainClass == "" && cfg.VersionJSON != nil {
		mainClass = cfg.VersionJSON.MainClass
	}
	if mainClass == "" {
		mainClass = "net.minecraft.client.main.Main"
	}

	assetIndex := cfg.Instance.MCVersion
	if cfg.VersionJSON != nil && cfg.VersionJSON.Assets != "" {
		assetIndex = cfg.VersionJSON.Assets
	}

	width, height := cfg.WindowWidth, cfg.WindowHeight
	if width <= 0 {
		width = 854
	}
	if height <= 0 {
		height = 480
	}

	minRAM := cfg.MinRAM
	if minRAM <= 0 {
		minRAM = ram / 2
	}
	if minRAM > ram {
		minRAM = ram
	}
	args := []string{
		fmt.Sprintf("-Xms%dM", minRAM),
		fmt.Sprintf("-Xmx%dM", ram),
	}
	if cfg.JavaArgs != "" {
		args = append(args, strings.Fields(cfg.JavaArgs)...)
	}
	// Аргументи з version.json застосовуємо для БУДЬ-ЯКОЇ версії/лоадера
	// (як у Prism/Mojang): Forge/NeoForge отримують модульний шлях -p,
	// --add-modules/--add-opens/--add-exports і --launchTarget/--fml.*,
	// Fabric/Quilt — -DFabricMcEmu=..., нові ванилла-версії (1.20.5+) —
	// --sun-misc-unsafe-memory-access тощо. Власний набір --add-opens
	// (forgeModuleOpens) додаємо ЛИШЕ для forge-подібних лоадерів нового
	// типу (bootstrap: у json є -p/--add-modules) — Forge 1.16.5 і нижче
	// працює на Java 8, де цих прапорців не існує, і JVM падає з
	// "Unrecognized option: --add-opens".
	var jsonJVMArgs, jsonGameArgs []string
	if cfg.VersionJSON != nil {
		chain := versionChain(cfg.VersionJSON, mcBase)
		jsonJVMArgs, jsonGameArgs = jsonChainArgs(cfg, mcBase, versionName, nativesDir, assetsDir, assetIndex, width, height, cp, isForgeLike(cfg.Instance.Loader))
		if isForgeLike(cfg.Instance.Loader) && chainUsesBootstrap(chain) {
			args = append(args, forgeModuleOpens()...)
			// Forge/NeoForge на MC 1.21+: Gson 2.8.0 всередині
			// forge-transformers встановлює final-поля через рефлексію —
			// без цих --add-opens падає на старті з
			// InaccessibleObjectException. Портовано з
			// MinecraftInstance.getJvmArguments старого лаунчера (той
			// самий набір, застосований лише для Forge/NeoForge 1.21+).
			if isMCVersion21OrHigher(cfg.Instance.MCVersion) {
				args = append(args,
					"--add-opens", "java.base/java.lang=ALL-UNNAMED",
					"--add-opens", "java.base/java.util=ALL-UNNAMED",
					"--add-opens", "java.base/java.lang.reflect=ALL-UNNAMED",
					"--add-opens", "java.base/java.text=ALL-UNNAMED",
					"--add-opens", "java.desktop/java.awt=ALL-UNNAMED",
					"--add-opens", "java.desktop/java.awt.font=ALL-UNNAMED",
				)
			}
		}
		args = append(args, jsonJVMArgs...)
	}
	args = append(args,
		"-Djava.library.path="+nativesDir,
		"-Dminecraft.launcher.brand=shaurma-launcher",
		"-Dminecraft.launcher.version=2.0.0",
		"-cp", cp,
		mainClass,
	)
	// Game-аргументи з version.json (--launchTarget forgeclient, --fml.*)
	// одразу після головного класу — їх читає BootstrapLauncher.
	args = append(args, jsonGameArgs...)
	args = append(args,
		"--username", cfg.Account.Username,
		"--version", versionName,
		"--gameDir", cfg.GameDir,
		"--assetsDir", assetsDir,
		"--assetIndex", assetIndex,
		"--uuid", cfg.Account.UUID,
		"--accessToken", cfg.Account.AccessToken,
		"--userType", "mojang",
		"--versionType", "release",
		"--width", fmt.Sprintf("%d", width),
		"--height", fmt.Sprintf("%d", height),
	)

	// Для Microsoft-акаунтів Minecraft очікує --userType msa. Шукаємо
	// флаг за ім'ям, а не за позицією від кінця списку (позиційний індекс
	// ламався щоразу, коли змінювалась кількість аргументів — напр.
	// після додавання --fullscreen). Виконуємо ДО збору env, щоб
	// SINST_JAVA_ARGS містив правильний userType.
	if cfg.Account.Type == "microsoft" || strings.Contains(cfg.Account.Type, "msa") {
		for i := 0; i+1 < len(args); i++ {
			if args[i] == "--userType" {
				args[i+1] = "msa"
				break
			}
		}
	}

	// Повноекранний режим (налаштування «Запускати Minecraft у
	// повноекранному режимі»). Може не підтримуватись усіма версіями.
	if cfg.Fullscreen {
		args = append(args, "--fullscreen")
	}

	// Автоприєднання: одразу після завантаження гра заходить у світ або
	// конектиться на сервер, без ручного кліку в меню. Використовує
	// офіційні прапорці Mojang (додані у 1.20 разом з quick-play API);
	// на старіших версіях гра просто не розпізнає прапорець і ігнорує
	// його — запуск не ламається.
	switch cfg.AutoJoinType {
	case "world":
		if w := strings.TrimSpace(cfg.AutoJoinWorld); w != "" {
			args = append(args, "--quickPlaySingleplayer", w)
		}
	case "server":
		if s := strings.TrimSpace(cfg.AutoJoinServer); s != "" {
			args = append(args, "--quickPlayMultiplayer", s)
		}
	}

	// Змінні середовища з налаштувань (KEY=VALUE, по одному в рядку)
	// + стандартні SINST_*-змінні для команд pre/post. Ключі SINST_*
	// користувач перевизначити не може — резервовані лаунчером.
	//
	// ВАЖЛИВО: прибираємо успадкований CLASSPATH з середовища ОС. Якщо в
	// системі користувача є глобальна змінна CLASSPATH (частий «сміттєвий»
	// артефакт старих інсталяцій Java/IDE/попередніх лаунчерів — саме тому
	// в неї міг потрапити шлях виду ...\.shaurm\minecraft\libraries\...),
	// то Forge/NeoForge BootstrapLauncher читає її як fallback-джерело
	// класшляху і віддає ЦІЛИЙ рядок (усі jar через ';') у Paths.get(...),
	// що падає з InvalidPathException на другому "C:" в рядку. Fabric/Quilt
	// і ванілла цю змінну не читають, тому в них усе працює, а Forge/
	// NeoForge — ні. Prism і офіційний Mojang-лаунчер із тієї ж причини
	// ніколи не пробрасують системний CLASSPATH у дочірній процес.
	env := filterOutClassPathEnv(os.Environ())
	env = append(env, "MALLOC_ARENA_MAX=4")
	env = appendUserEnv(env, cfg.EnvVars)
	env = append(env, forgeSpecEnv(cfg)...)
	env = append(env,
		"SINST_NAME="+cfg.Instance.Name,
		"SINST_ID="+cfg.Instance.ID,
		"SINST_DIR="+filepath.Dir(cfg.GameDir),
		"SINST_MC_DIR="+cfg.GameDir,
		"SINST_JAVA="+javaPath,
		"SINST_JAVA_ARGS="+strings.Join(args, " "),
	)

	// Команда перед запуском: виконується ДО старту гри, в робочій теці
	// лаунчера, зі змінними середовища. Якщо завершилась з помилкою —
	// запуск гри скасовується.
	if strings.TrimSpace(cfg.PreLaunchCommand) != "" {
		if err := runShellCommand(cfg.PreLaunchCommand, env, filepath.Dir(cfg.GameDir)); err != nil {
			l.mu.Lock()
			l.running = false
			l.mu.Unlock()
			return fmt.Errorf("pre-launch command: %w", err)
		}
	}

	// Для Forge/NeoForge запускаємо через startWithRetry: якщо конкретна
	// збірка JVM не розпізнає один із наших модульних/сучасних прапорців
	// (--sun-misc-unsafe-memory-access, --enable-native-access,
	// --patch-module — трапляється на старіших білдах Java, яких
	// користувач міг поставити вручну чи які лишились з попередньої
	// версії лаунчера), процес одразу падає з "Unrecognized option" ще ДО
	// того, як встигла завантажитись гра. Портовано з
	// MinecraftLauncher.startWithRetry старого лаунчера: короткий (1.5с)
	// грейс-період на старті, і якщо в цей час у виводі з'явилась ознака
	// нерозпізнаного прапорця — перезапускаємо БЕЗ проблемних флагів,
	// без зайвого втручання в інші лоадери (Vanilla/Fabric/Quilt тут не
	// зачіпаються — там таких прапорців у аргументах взагалі нема).
	if isForgeLike(cfg.Instance.Loader) {
		cmd, stdout, stderr, err := l.startWithRetry(javaPath, args, cfg, 2)
		if err != nil {
			l.mu.Lock()
			l.running = false
			l.mu.Unlock()
			return err
		}
		l.finishLaunch(cmd, stdout, stderr, cfg, javaPath, args)
		return nil
	}

	var cmd *exec.Cmd
	if strings.TrimSpace(cfg.WrapperCommand) != "" {
		// Команда-обгортка: запускаємо через неї сам java-процес
		// (наприклад «optirun»).
		cmd = exec.Command(strings.TrimSpace(cfg.WrapperCommand), append([]string{javaPath}, args...)...)
	} else {
		cmd = exec.Command(javaPath, args...)
	}
	cmd.Dir = cfg.GameDir
	cmd.Env = env

	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	l.process = cmd
	l.startAt = time.Now()
	l.resolvedJava = javaPath
	l.launchArgs = strings.Join(args, " ")

	if err := cmd.Start(); err != nil {
		l.mu.Lock()
		l.running = false
		l.mu.Unlock()
		return fmt.Errorf("start failed: %w", err)
	}

	l.finishLaunch(cmd, stdout, stderr, cfg, javaPath, args)
	return nil
}

// finishLaunch підключає логування виводу процесу і запускає очікування
// завершення — спільний хвіст для звичайного і forge-retry шляхів запуску.
func (l *Launcher) finishLaunch(cmd *exec.Cmd, stdout, stderr io.Reader, cfg LaunchConfig, javaPath string, args []string) {
	l.mu.Lock()
	l.process = cmd
	l.startAt = time.Now()
	l.resolvedJava = javaPath
	l.launchArgs = strings.Join(args, " ")
	l.mu.Unlock()

	// SaveLogs (налаштування «Зберігати логи») — чи писати latest.log.
	// Якщо вимкнено — вивід гри все одно йде у консоль лаунчера, але
	// файл лога на диску не створюється.
	if cfg.SaveLogs {
		logFile := filepath.Join(cfg.GameDir, "logs", "latest.log")
		os.MkdirAll(filepath.Dir(logFile), 0755)
		logWriter, err := os.Create(logFile)
		if err == nil {
			// stdout і stderr пишуть в ОДИН файл з двох горутин — ділимо
			// його через syncWriter, інакше це конкурентний доступ до
			// *os.File (data race за -race; на практиці рядки могли б
			// переплітатись на рівні окремих write()-ів).
			sw := &syncWriter{w: logWriter}
			go l.teeOutput(stdout, sw)
			go l.teeOutput(stderr, sw)
		} else {
			go l.readOutput(stdout)
			go l.readOutput(stderr)
		}
	} else {
		go l.readOutput(stdout)
		go l.readOutput(stderr)
	}

	go l.waitExit(cmd, cfg)
}

// startWithRetry запускає JVM-процес Forge/NeoForge і, якщо в перші ~1.5с
// виводу з'являється ознака нерозпізнаного JVM-прапорця, перезапускає БЕЗ
// проблемних флагів. Портовано з MinecraftLauncher.startWithRetry старого
// лаунчера (той самий грейс-період, ті самі ознаки помилки, той самий
// набір прапорців-кандидатів на видалення).
func (l *Launcher) startWithRetry(javaPath string, args []string, cfg LaunchConfig, retriesLeft int) (*exec.Cmd, io.Reader, io.Reader, error) {
	if retriesLeft <= 0 {
		return nil, nil, nil, fmt.Errorf("JVM не змогла запуститись після кількох спроб — несумісні JVM аргументи")
	}

	var cmd *exec.Cmd
	env := filterOutClassPathEnv(os.Environ())
	env = append(env, "MALLOC_ARENA_MAX=4")
	env = appendUserEnv(env, cfg.EnvVars)
	env = append(env, forgeSpecEnv(cfg)...)
	env = append(env,
		"SINST_NAME="+cfg.Instance.Name,
		"SINST_ID="+cfg.Instance.ID,
		"SINST_DIR="+filepath.Dir(cfg.GameDir),
		"SINST_MC_DIR="+cfg.GameDir,
		"SINST_JAVA="+javaPath,
		"SINST_JAVA_ARGS="+strings.Join(args, " "),
	)
	if strings.TrimSpace(cfg.WrapperCommand) != "" {
		cmd = exec.Command(strings.TrimSpace(cfg.WrapperCommand), append([]string{javaPath}, args...)...)
	} else {
		cmd = exec.Command(javaPath, args...)
	}
	cmd.Dir = cfg.GameDir
	cmd.Env = env

	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		return nil, nil, nil, fmt.Errorf("start failed: %w", err)
	}

	// Підписуємось на потоки негайно (як у старому лаунчері — жодної
	// паузи перед стартом читання, інакше короткоживучі процеси можуть
	// втратити перші рядки виводу до того, як ми почнемо читати).
	var bufMu sync.Mutex
	var earlyBuf strings.Builder
	captureAndForward := func(line string) {
		bufMu.Lock()
		if earlyBuf.Len() < 50_000 {
			earlyBuf.WriteString(line)
			earlyBuf.WriteByte('\n')
		}
		bufMu.Unlock()
		if l.onOutput != nil {
			l.onOutput(line)
		}
	}
	stdoutR, stdoutW := io.Pipe()
	stderrR, stderrW := io.Pipe()
	go pumpAndForward(stdout, stdoutW, captureAndForward, "")
	go pumpAndForward(stderr, stderrW, captureAndForward, "[ERR] ")

	exited := make(chan struct{})
	go func() {
		cmd.Wait()
		close(exited)
	}()

	exitedEarly := false
	select {
	case <-exited:
		exitedEarly = true
	case <-time.After(1500 * time.Millisecond):
	}

	bufMu.Lock()
	earlySnapshot := earlyBuf.String()
	bufMu.Unlock()

	unrecognized := exitedEarly && (strings.Contains(earlySnapshot, "Unrecognized option") ||
		strings.Contains(earlySnapshot, "Unsupported") ||
		strings.Contains(earlySnapshot, "Could not create the Java Virtual Machine"))

	if unrecognized {
		l.log(fmt.Sprintf("JVM не розпізнала аргументи, пробуємо без проблемних флагів (лишилось спроб: %d)", retriesLeft-1))
		var filtered []string
		for _, a := range args {
			if strings.Contains(a, "sun-misc-unsafe-memory-access") ||
				strings.Contains(a, "--enable-native-access") ||
				strings.Contains(a, "--patch-module") {
				l.log("Видаляємо проблемний JVM аргумент: " + a)
				continue
			}
			filtered = append(filtered, a)
		}
		return l.startWithRetry(javaPath, filtered, cfg, retriesLeft-1)
	}

	// Немає ознак несумісності — повертаємо процес і потоки, вже
	// підписані та закриті на PipeWriter стороні (finishLaunch читає їх
	// далі як звичайні io.Reader, так само, як стандартний cmd.StdoutPipe()).
	return cmd, stdoutR, stderrR, nil
}

// pumpAndForward читає r рядок за рядком, форвардить кожен рядок (з
// префіксом) через sink і одночасно пише "сирий" рядок у w (io.PipeWriter),
// щоб finishLaunch міг далі підключити ті самі дані до SaveLogs/консолі
// звичайним шляхом. Закриває w по завершенню (EOF або помилка читання).
func pumpAndForward(r io.Reader, w *io.PipeWriter, sink func(string), prefix string) {
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		sink(prefix + line)
		w.Write([]byte(line + "\n"))
	}
	w.Close()
}

// log передає діагностичне повідомлення в консоль гри (onOutput), якщо
// обробник підключено — так само, як Installer.log робить для
// install-етапу. Використовується для retry-повідомлень у startWithRetry.
// syncWriter — м'ютекс-захищений writer, який дозволяє кільком горутинам
// (teeOutput для stdout/stderr) писати в один файл latest.log без гонки.
// Рядки не переплітаються на рівні окремих Write()-ів.
type syncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

func (l *Launcher) log(msg string) {
	if l.onOutput != nil {
		l.onOutput("[launcher] " + msg)
	}
}

func (l *Launcher) teeOutput(r io.Reader, w io.Writer) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		w.Write([]byte(line + "\n"))
		if l.onOutput != nil {
			l.onOutput(line)
		}
	}
}

func (l *Launcher) readOutput(r io.Reader) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if l.onOutput != nil {
			l.onOutput(line)
		}
	}
}

func (l *Launcher) waitExit(cmd *exec.Cmd, cfg LaunchConfig) {
	exitCode := 0
	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}

	// Ігровий час: рахуємо тривалість сесії та передаємо обробнику
	// (якщо запис часу ввімкнено у налаштуваннях — рішення на app.go).
	if l.onPlaytime != nil && !l.startAt.IsZero() {
		secs := int64(time.Since(l.startAt).Seconds())
		l.onPlaytime(l.instance, secs)
	}

	// Команда після виходу: виконується ПІСЛЯ завершення гри, з тими ж
	// змінними середовища (включно зі змінними користувача), що й гра.
	if strings.TrimSpace(cfg.PostExitCommand) != "" {
		env := append(os.Environ(), "MALLOC_ARENA_MAX=4", "SINST_EXIT_CODE="+fmt.Sprintf("%d", exitCode))
		env = appendUserEnv(env, cfg.EnvVars)
		env = append(env,
			"SINST_NAME="+cfg.Instance.Name,
			"SINST_ID="+cfg.Instance.ID,
			"SINST_DIR="+filepath.Dir(cfg.GameDir),
			"SINST_MC_DIR="+cfg.GameDir,
			"SINST_JAVA="+l.resolvedJava,
			// Той самий повний список аргументів, що й у pre-launch
			// (збережений при запуску), щоб обидві команди бачили
			// однаковий SINST_JAVA_ARGS.
			"SINST_JAVA_ARGS="+l.launchArgs,
		)
		runShellCommand(cfg.PostExitCommand, env, filepath.Dir(cfg.GameDir))
	}

	l.mu.Lock()
	l.running = false
	l.process = nil
	l.mu.Unlock()
	if l.onExit != nil {
		l.onExit(exitCode)
	}
}

// appendUserEnv додає змінні середовища користувача (KEY=VALUE, по одному
// в рядку) до env. Порожні рядки, коментарі та резервовані ключі SINST_*
// пропускаються — їх задає сам лаунчер.
func appendUserEnv(env []string, raw string) []string {
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		if strings.HasPrefix(strings.SplitN(line, "=", 2)[0], "SINST_") {
			continue
		}
		env = append(env, line)
	}
	return env
}

// runShellCommand виконує командний рядок через системну оболонку
// (cmd /C на Windows, sh -c на Linux/macOS) у вказаній теці з env.
// Помилка повертається, якщо команда завершилась ненульовим кодом.
func runShellCommand(command string, env []string, dir string) error {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/C", command)
	} else {
		cmd = exec.Command("sh", "-c", command)
	}
	cmd.Dir = dir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s (output: %s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (l *Launcher) Stop() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.process == nil {
		return nil
	}
	if runtime.GOOS == "windows" {
		l.process.Process.Signal(os.Kill)
	} else {
		l.process.Process.Signal(os.Interrupt)
	}
	return nil
}

// forgeModuleOpens — набір --add-opens, які Forge/NeoForge потребує на
// Java 17+: FML робить setAccessible на приватних полях JDK, що модульна
// система забороняє без явного відкриття пакетів. Безпечно на будь-якій
// Java 17+, на старіших просто ігнорується як unknown-option лише там,
// де FML і так не працює (Forge 1.20.1+ вимагає Java 17+).
func forgeModuleOpens() []string {
	return []string{
		"--add-opens=java.base/java.lang.invoke=ALL-UNNAMED",
		"--add-opens=java.base/java.lang=ALL-UNNAMED",
		"--add-opens=java.base/java.util=ALL-UNNAMED",
		"--add-opens=java.base/java.util.concurrent=ALL-UNNAMED",
		"--add-opens=java.base/java.net=ALL-UNNAMED",
		"--add-opens=java.base/java.io=ALL-UNNAMED",
		"--add-opens=java.base/java.nio=ALL-UNNAMED",
		"--add-opens=java.base/sun.nio.ch=ALL-UNNAMED",
		"--add-opens=java.base/sun.security.util=ALL-UNNAMED",
		"--add-opens=java.base/jdk.internal.reflect=ALL-UNNAMED",
	}
}

// isForgeLike визначає, чи лоадер базується на FML (Forge / NeoForge) —
// саме їм потрібні --add-opens при запуску на сучасних JDK.
func isForgeLike(loader string) bool {
	l := strings.ToLower(strings.TrimSpace(loader))
	return l == "forge" || l == "neoforge"
}

// forgeSpecEnv повертає FORGE_SPEC=<forgeVersion> для Forge-family профілів,
// які несуть game-аргумент --fml.forgeVersion (бутстрап-формат Forge 1.13+;
// NeoForge такого аргумента не має, legacy Forge 1.12 і раніше теж).
//
// Навіщо: ForgeVersion.<clinit> (ForgeVersion.java:47-48) читає
// JarVersionLookupHandler.getSpecificationVersion(ForgeVersion.class), тобто
// Class.getPackage().getSpecificationVersion() з MANIFEST.MF-секції пакета
// net/minecraftforge/versions/forge/. Це працює для бібліотек зі звичайного
// classpath, але в бутстрап-форматі universal jar іде у legacy classpath
// bootstraplauncher'а (-DlegacyClassPath.file), і його класи завантажуються
// через securejarhandler БЕЗ прокидання manifest-секцій у Package — тому
// spec == null, попри те, що секція в jar-і на місці. Forge передбачив
// fallback на змінну середовища FORGE_SPEC (саме для таких кейсів); без неї
// 1.17.1/1.18-Forge падає "Missing forge spec, cannot continue". Значення
// беремо з профілю (той самий --fml.forgeVersion, що йде в game-аргументи),
// тому для NeoForge і vanilla/Fabric/Quilt функція порожня.
func forgeSpecEnv(cfg LaunchConfig) []string {
	if cfg.VersionJSON == nil {
		return nil
	}
	if ver := fmlForgeVersion(cfg.VersionJSON); ver != "" {
		return []string{"FORGE_SPEC=" + ver}
	}
	return nil
}

// isMCVersion21OrHigher перевіряє, чи версія Minecraft >= 1.21 — портовано
// з MinecraftInstance.isVersion21OrHigher старого лаунчера (та сама логіка
// парсингу "1.MINOR[.PATCH]").
func isMCVersion21OrHigher(mcVersion string) bool {
	mcVersion = strings.TrimSpace(mcVersion)
	if mcVersion == "" {
		return false
	}
	parts := strings.Split(mcVersion, ".")
	if len(parts) < 2 {
		return false
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return false
	}
	return parts[0] == "1" && minor >= 21
}

// jsonChainArgs повертає JVM- і game-аргументи з УСЬОГО version.json
// ланцюга (loader + батьківські ванилла-версії), з підстановкою
// ${placeholders} і дедупом — загальний механізм для всіх лоадерів:
//   · Forge/NeoForge 1.17+ — -p, --add-modules ALL-MODULE-PATH,
//     --add-opens/--add-exports, -DignoreList/-DmergeModules/
//     -DlibraryDirectory і game-арги --launchTarget/--fml.*;
//   · Forge 1.16.5- — -DignoreList/-DmergeModules/-DlibraryDirectory
//     (без модульних прапорців — там їх немає, Java 8);
//   · Fabric/Quilt — -DFabricMcEmu=...;
//   · ванилла 1.20.5+ — --sun-misc-unsafe-memory-access тощо.
// Для forge-подібних лоадерів (writeClasspath=true) додатково ПИШЕ файл
// класшляху versions/<id>/<id>-classpath.txt і повертає
// -DlegacyClassPath.file= (його читає cpw.mods.bootstraplauncher на 1.17+
// та cpw.mods.modlauncher на 1.16.5-, щоб зібрати шар гри). Аргументи,
// які лаунчер передає сам (RAM, -cp, нативки, бренд, стандартні
// --username тощо), і нерозв'язані ${placeholders} — пропускаються.
func jsonChainArgs(cfg LaunchConfig, mcBase, versionName, nativesDir, assetsDir, assetIndex string, width, height int, cp string, writeClasspath bool) (jvmArgs, gameArgs []string) {
	if cfg.VersionJSON == nil {
		return nil, nil
	}

	// Файл класшляху для -DlegacyClassPath.file. КРИТИЧНО: формат — по
	// одному шляху НА РЯДОК (через "\n"), а не через ';'/':' як у -cp!
	// cpw.mods.bootstraplauncher читає цей файл через
	// Files.readAllLines(...) (список РЯДКІВ, кожен — окремий jar) і потім
	// робить Paths.get() на кожному елементі ОКРЕМО. Якщо записати сюди
	// той самий рядок, що йде в -cp (jar-и через ';'), readAllLines()
	// поверне список з ОДНОГО елемента — усього класшляху одразу — і
	// Paths.get() впаде з InvalidPathException на другому "C:" в рядку
	// (саме це й було причиною крашів Forge/NeoForge: клас-файл писався
	// у форматі -cp замість формату "по рядку").
	cpFile := ""
	if writeClasspath && versionName != "" {
		cpFile = filepath.Join(mcBase, "versions", versionName, versionName+"-classpath.txt")
		cpLines := strings.Split(cp, string(os.PathListSeparator))
		if err := os.MkdirAll(filepath.Dir(cpFile), 0755); err == nil {
			os.WriteFile(cpFile, []byte(strings.Join(cpLines, "\n")), 0644)
		}
	}

	sub := map[string]string{
		"library_directory":   filepath.Join(mcBase, "libraries"),
		"classpath_separator": string(os.PathListSeparator),
		"version_name":        versionName,
		"natives_directory":   nativesDir,
		"launcher_name":       "shaurma-launcher",
		"launcher_version":    "2.0.0",
		"game_directory":      cfg.GameDir,
		"assets_root":         assetsDir,
		"assets_index_name":   assetIndex,
		"auth_player_name":    cfg.Account.Username,
		"auth_uuid":           cfg.Account.UUID,
		"auth_access_token":   cfg.Account.AccessToken,
		"user_type":           "mojang",
		"version_type":        "release",
		"resolution_width":    fmt.Sprintf("%d", width),
		"resolution_height":   fmt.Sprintf("%d", height),
		"auth_xuid":           "",
		"clientid":            "",
	}
	if cfg.Account.Type == "microsoft" || strings.Contains(cfg.Account.Type, "msa") {
		sub["user_type"] = "msa"
	}

	// Спершу розгортаємо аргументи всього ланцюга InheritsFrom у плоский
	// список рядків (правила-об'єкти оцінюються через expandJsonArgs), потім
	// ідемо з дедупом ЗА ПАРОЮ флаг/значення. Це критично: дедуп за одним
	// рядком розриває пари — NeoForge має ДВА однакові --add-opens, і якщо
	// прибрати лише флаг, його значення (java.base/java.lang.invoke=...)
	// лишиться сиротою, і java сприйме його як ім'я головного класу
	// ("Could not find or load main class java.base.java.lang.invoke=...").
	var rawJ, rawG []string
	for cur := cfg.VersionJSON; cur != nil; cur = cur.inheritsFrom(mcBase) {
		for _, raw := range cur.Arguments.JVM {
			rawJ = append(rawJ, expandJsonArgs(raw)...)
		}
		for _, raw := range cur.Arguments.Game {
			rawG = append(rawG, expandJsonArgs(raw)...)
		}
		// Legacy-формат (MC ≤ 1.12.2): усі game-аргументи лежать у полі
		// minecraftArguments рядком через пробіл. Критично для Forge
		// 1.12.2: саме там задається --tweakClass
		// net.minecraftforge.fml.common.launcher.FMLTweaker — без нього
		// launchwrapper стартує з одним VanillaTweaker і гра тихо виходить
		// (код 0). Строкових значень із пробілами тут не буває, тому
		// strings.Fields достатньо. Керовані флаги (--username тощо) і
		// дедупікацію далі обробляє спільний цикл нижче.
		if cur.MinecraftArguments != "" {
			rawG = append(rawG, strings.Fields(cur.MinecraftArguments)...)
		}
	}

	seenJ := map[string]bool{}
	for i := 0; i < len(rawJ); i++ {
		s := rawJ[i]
		key := argPairKey(s, rawJ, i)
		if seenJ[key] {
			continue
		}
		seenJ[key] = true
		out, resolved := substituteJsonArg(s, sub)
		// Порожні значення (напр. ${classpath} — його передаємо самі)
		// і нерозв'язані плейсхолдери пропускаємо; порожній рядок у
		// JVM-аргументах java сприйняв би як ім'я головного класу.
		if !resolved || out == "" || skipManagedJVMArg(s) {
			continue
		}
		jvmArgs = append(jvmArgs, out)
	}

	seenG := map[string]bool{}
	for i := 0; i < len(rawG); i++ {
		s := rawG[i]
		key := argPairKey(s, rawG, i)
		if seenG[key] {
			continue
		}
		seenG[key] = true
		if skipManagedGameArg(s) {
			// Флаг, який лаунчер передає сам (--username тощо):
			// пропускаємо і його значення (наступний елемент, якщо він не
			// флаг) — інакше воно стане значенням НАСТУПНОГО флага (напр.
			// --clientId проковтне --xuid) або порожнім позиційним аргументом.
			if i+1 < len(rawG) && !isFlagToken(rawG[i+1]) {
				seenG[argPairKey(rawG[i+1], rawG, i+1)] = true
				i++
			}
			continue
		}
		out, resolved := substituteJsonArg(s, sub)
		if !resolved {
			continue
		}
		// out може бути "" — це значення флага (--clientId "");
		// порожні рядки парсер Minecraft ігнорує як позиційні, тож
		// пара флаг/значення зберігається коректно.
		gameArgs = append(gameArgs, out)
	}

	if cpFile != "" {
		jvmArgs = append([]string{"-DlegacyClassPath.file=" + cpFile}, jvmArgs...)
	}
	return jvmArgs, gameArgs
}

// expandJsonArgs розбирає один елемент аргументів version.json (рядок або
// об'єкт {rules, value}) у список рядків, які застосовуються на поточній
// ОС. Об'єкти з rules оцінюються через rulesMatch (див. нижче); якщо
// правила не збіглися або містять features (які ми не оцінюємо) —
// повертається nil і аргумент пропускається.
func expandJsonArgs(raw interface{}) []string {
	switch v := raw.(type) {
	case string:
		return []string{v}
	case map[string]interface{}:
		rules, _ := v["rules"].([]interface{})
		if len(rules) > 0 && !rulesMatch(rules) {
			return nil
		}
		switch val := v["value"].(type) {
		case string:
			return []string{val}
		case []interface{}:
			var out []string
			for _, item := range val {
				if s, ok := item.(string); ok {
					out = append(out, s)
				}
			}
			return out
		}
	}
	return nil
}

// rulesMatch оцінює набір rules version.json проти поточної ОС (та сама
// семантика, що у Mojang/Prism): застосовується останнє правило, що
// збіглося; якщо жодне не збіглося — аргумент не застосовується. Правила
// з "features" (is_demo_user, has_custom_resolution тощо) ми не оцінюємо —
// такий аргумент пропускається цілком (відповідні фічі лаунчер реалізує
// сам: --width/--height/--quickPlay*).
func rulesMatch(rules []interface{}) bool {
	matched := false
	include := false
	for _, r := range rules {
		rm, ok := r.(map[string]interface{})
		if !ok {
			continue
		}
		if _, hasFeatures := rm["features"]; hasFeatures {
			return false
		}
		if !osRuleMatches(rm) {
			continue
		}
		matched = true
		include = rm["action"] == nil || rm["action"] == "allow"
	}
	return matched && include
}

// osRuleMatches перевіряє збіг os{name,arch} та arch-правила з поточною
// ОС/архітектурою. Правила з os.version ігноруємо (не збігаються).
func osRuleMatches(rm map[string]interface{}) bool {
	if osRaw, ok := rm["os"].(map[string]interface{}); ok {
		if name, ok := osRaw["name"].(string); ok && name != "" {
			me := runtime.GOOS
			if me == "darwin" {
				me = "osx"
			}
			if name != me {
				return false
			}
		}
		if arch, ok := osRaw["arch"].(string); ok && arch != "" && !archMatches(arch) {
			return false
		}
		if version, ok := osRaw["version"].(string); ok && version != "" {
			return false
		}
	}
	if arch, ok := rm["arch"].(string); ok && arch != "" && !archMatches(arch) {
		return false
	}
	return true
}

// archMatches зіставляє ім'я архітектури з rules (x86/x86_64/arm/arm64)
// з runtime.GOARCH.
func archMatches(arch string) bool {
	switch runtime.GOARCH {
	case "amd64":
		return arch == "x86_64"
	case "386":
		return arch == "x86"
	case "arm64":
		return arch == "arm64" || arch == "aarch64"
	case "arm":
		return arch == "arm"
	}
	return false
}

// isFlagToken — рядок, що виглядає як JVM/CLI-флаг (починається з "-").
// Значення флага (напр. модульний шлях після -p) флагом не є.
func isFlagToken(s string) bool {
	return len(s) > 0 && s[0] == '-'
}

// argPairKey — ключ дедупу для позиції у плоскому списку аргументів: для
// флага, за яким іде значення (наступний елемент не флаг), ключ включає
// і флаг, і значення. Це зберігає пари --add-opens X / --add-opens Y як
// РІЗНІ (дедуп лише точних дублікатів) і не лишає значення без флага.
func argPairKey(s string, list []string, i int) string {
	if isFlagToken(s) && i+1 < len(list) && !isFlagToken(list[i+1]) {
		return s + "\x00" + list[i+1]
	}
	return s
}

// substituteJsonArg підставляє ${placeholders} з sub. Другий результат —
// false, якщо після підстановки лишився нерозв'язаний ${...} (аргумент
// пропускається). Розв'язаний порожній рядок повертається як є — рішення
// про пропуск приймає викликач (JVM vs game-аргументи).
func substituteJsonArg(s string, sub map[string]string) (string, bool) {
	out := s
	for k, v := range sub {
		out = strings.ReplaceAll(out, "${"+k+"}", v)
	}
	if strings.Contains(out, "${") {
		return "", false
	}
	return out, true
}

// filterOutClassPathEnv видаляє з переданого середовища будь-який запис
// CLASSPATH=... (перевірка регістронезалежна — на Windows імена змінних
// середовища регістронезалежні, тож CLASSPATH/Classpath/classpath — це
// одна й та сама змінна). Лаунчер сам будує -cp і -DlegacyClassPath.file,
// тож жодна зовнішня CLASSPATH йому не потрібна і, гірше, ламає Forge/
// NeoForge (див. коментар при виклику в Launch).
func filterOutClassPathEnv(environ []string) []string {
	out := make([]string, 0, len(environ))
	for _, e := range environ {
		if idx := strings.IndexByte(e, '='); idx >= 0 && strings.EqualFold(e[:idx], "CLASSPATH") {
			continue
		}
		out = append(out, e)
	}
	return out
}

// skipManagedJVMArg — JVM-аргументи, які лаунчер передає сам; з json не
// дублюємо, щоб не було конфліктів/дублів у команді java.
func skipManagedJVMArg(a string) bool {
	for _, p := range []string{"-Xms", "-Xmx", "-cp", "--classpath", "-Djava.library.path", "-Dminecraft.launcher.brand", "-Dminecraft.launcher.version", "-DlegacyClassPath"} {
		if strings.HasPrefix(a, p) {
			return true
		}
	}
	return false
}

// skipManagedGameArg — стандартні game-аргументи, які лаунчер передає сам
// (включно з --clientId/--xuid — лаунчер їх не зберігає, а для запуску
// вони не потрібні: authlib обходиться --accessToken).
func skipManagedGameArg(a string) bool {
	for _, p := range []string{"--username", "--version", "--gameDir", "--assetsDir", "--assetIndex", "--uuid", "--accessToken", "--clientId", "--xuid", "--userType", "--versionType", "--width", "--height", "--fullscreen", "--quickPlay"} {
		if strings.HasPrefix(a, p) {
			return true
		}
	}
	return false
}

// versionChain повертає весь ланцюг version.json (loader + батьківські
// версії), з захистом від циклів. Спершу — сам loader, потім батьки.
func versionChain(vjson *VersionJSON, baseDir string) []*VersionJSON {
	var chain []*VersionJSON
	seen := map[string]bool{}
	for cur := vjson; cur != nil; cur = cur.inheritsFrom(baseDir) {
		if seen[cur.ID] {
			break
		}
		seen[cur.ID] = true
		chain = append(chain, cur)
	}
	return chain
}

// chainUsesBootstrap визначає, чи ланцюг лоадера належить «новому» типу
// запуску FML (Java 17+): cpw.mods.bootstraplauncher потребує модульного
// шляху -p та --add-modules ALL-MODULE-PATH у JVM-аргументах. Forge 1.16.5
// і нижче (cpw.mods.modlauncher, Java 8) цих прапорців у json не мають.
func chainUsesBootstrap(chain []*VersionJSON) bool {
	for _, v := range chain {
		for _, raw := range v.Arguments.JVM {
			if s, ok := raw.(string); ok && (s == "-p" || s == "--add-modules") {
				return true
			}
		}
	}
	return false
}

func (l *Launcher) IsRunning() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.running
}

func (l *Launcher) RunningInstance() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.instance
}

// PID повертає ідентифікатор процесу гри (0, якщо гра не запущена або
// процес ще не стартував). Використовується для збереження зв'язку з
// грою на диск: якщо лаунчер уб'ють і перезапустять, PID дозволяє
// зрозуміти, що гра досі запущена (і час гри не пропаде).
func (l *Launcher) PID() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.process != nil && l.process.Process != nil {
		return l.process.Process.Pid
	}
	return 0
}

// AdoptProcess приймає ВЖЕ ЗАПУЩЕНИЙ процес гри (після перезапуску
// лаунчера: PID відновлено з running-game.json, а сам процес запущений
// поза нашим exec.Cmd). Після цього Launcher вважає гру «запущеною»:
// PID() повертає збережений PID, а Stop() може реально вбити процес по
// сигналу. Власного моніторингу виходу (waitExit) тут НЕМАЄ — це чужий
// процес без нашого exec.Cmd; стеження за завершенням веде викликач
// (app.restoreRunningGame поллить isProcessAlive і чистить сесію/файл
// стану, коли гра закриється).
// Повертає false, якщо процес недоступний (PID <= 0 або os.FindProcess
// не зміг його відкрити) — викликач тоді прибирає файл стану.
func (l *Launcher) AdoptProcess(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	l.mu.Lock()
	// Використовуємо exec.Cmd лише як контейнер для *os.Process — нічого
	// з нього не запускається і не чекається; Stop() працює через
	// l.process.Process.Signal, як і для звичайного запуску.
	l.process = &exec.Cmd{Process: proc}
	l.running = true
	l.startAt = time.Now()
	l.mu.Unlock()
	return true
}